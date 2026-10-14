package raft

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type rpcStreamConn struct {
	input  *bytes.Reader
	output bytes.Buffer
	closed atomic.Bool
}

func (c *rpcStreamConn) Read(p []byte) (int, error) { return c.input.Read(p) }
func (c *rpcStreamConn) Write(p []byte) (int, error) {
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	return c.output.Write(p)
}
func (c *rpcStreamConn) Close() error                   { c.closed.Store(true); return nil }
func (*rpcStreamConn) LocalAddr() net.Addr              { return nil }
func (*rpcStreamConn) RemoteAddr() net.Addr             { return nil }
func (*rpcStreamConn) SetDeadline(time.Time) error      { return nil }
func (*rpcStreamConn) SetReadDeadline(time.Time) error  { return nil }
func (*rpcStreamConn) SetWriteDeadline(time.Time) error { return nil }

func rpcTestFrame(method, payload string) string {
	return fmt.Sprintf("%s\n%d\n%s", method, len(payload), payload)
}

func TestTCPTransportFailedRPCClosesStream(t *testing.T) {
	goodFirst := rpcTestFrame("AppendEntries", `{"term":1}`)
	goodSecond := rpcTestFrame("AppendEntries", `{"term":2}`)
	cases := []struct {
		name, input string
		badResponse bool
		calls       int
		terms       []uint64
	}{
		{name: "valid reusable stream", input: goodFirst + goodSecond, calls: 2, terms: []uint64{1, 2}},
		{name: "one valid request", input: goodFirst, calls: 1, terms: []uint64{1}},
		{name: "empty stream"},
		{name: "invalid JSON", input: rpcTestFrame("AppendEntries", `{invalid}`) + goodSecond},
		{name: "unknown method", input: rpcTestFrame("Unknown", `{}`) + goodSecond},
		{name: "unserializable response", input: goodFirst + goodSecond, badResponse: true, calls: 1},
		{name: "invalid length", input: "AppendEntries\n0\n" + goodSecond},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			transport, err := NewTCPTransport("unused", "unused", nil, newTestTransportLogger())
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			transport.RegisterHandler("AppendEntries", func(cmd interface{}, response chan interface{}) {
				calls++
				term := cmd.(*core.AppendEntriesRequest).Term
				if tt.badResponse && term == 1 {
					response <- make(chan int)
					return
				}
				response <- &core.AppendEntriesResponse{Term: term, Success: true}
			})
			conn := &rpcStreamConn{input: bytes.NewReader([]byte(tt.input))}
			transport.incoming[conn] = struct{}{}
			transport.handleConnection(conn)
			if calls != tt.calls {
				t.Errorf("handler calls=%d, want %d", calls, tt.calls)
			}
			if !conn.closed.Load() {
				t.Error("stream not closed")
			}
			if len(transport.incoming) != 0 {
				t.Error("incoming stream not released")
			}
			reader := bufio.NewReader(bytes.NewReader(conn.output.Bytes()))
			for _, term := range tt.terms {
				var length int
				if _, err := fmt.Fscanf(reader, "%d\n", &length); err != nil {
					t.Fatal(err)
				}
				payload := make([]byte, length)
				if _, err := io.ReadFull(reader, payload); err != nil {
					t.Fatal(err)
				}
				var response core.AppendEntriesResponse
				if err := json.Unmarshal(payload, &response); err != nil {
					t.Fatal(err)
				}
				if response.Term != term || !response.Success {
					t.Fatalf("response=%+v, want term %d", response, term)
				}
			}
			remainder, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if len(remainder) != 0 {
				t.Errorf("unexpected following response: %s", remainder)
			}
		})
	}
}

type gatedRPCReadConn struct {
	rpcStreamConn
	entered, release chan struct{}
	first            bool
}

func (c *gatedRPCReadConn) Read(p []byte) (int, error) {
	n, err := c.input.Read(p)
	if !c.first {
		c.first = true
		close(c.entered)
		<-c.release
	}
	return n, err
}
func TestTCPTransportRejectsRPCDispatchAfterStop(t *testing.T) {
	for _, stopBeforeCompletion := range []bool{false, true} {
		t.Run(fmt.Sprintf("stop_before_read_completion_%t", stopBeforeCompletion), func(t *testing.T) {
			transport, err := NewTCPTransport("unused", "unused", nil, newTestTransportLogger())
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			transport.RegisterHandler("AppendEntries", func(_ interface{}, response chan interface{}) {
				calls.Add(1)
				response <- &core.AppendEntriesResponse{Term: 1, Success: true}
			})
			conn := &gatedRPCReadConn{
				rpcStreamConn: rpcStreamConn{input: bytes.NewReader([]byte(rpcTestFrame("AppendEntries", `{"term":1}`)))},
				entered:       make(chan struct{}), release: make(chan struct{}),
			}
			transport.incoming[conn] = struct{}{}
			done := make(chan struct{})
			go func() { transport.handleConnection(conn); close(done) }()
			<-conn.entered
			if stopBeforeCompletion {
				if err := transport.Stop(); err != nil {
					t.Fatal(err)
				}
			}
			close(conn.release)
			<-done
			if err := transport.Stop(); err != nil {
				t.Fatal(err)
			}
			want := int32(1)
			if stopBeforeCompletion {
				want = 0
			}
			if calls.Load() != want {
				t.Errorf("handler calls=%d, want %d", calls.Load(), want)
			}
			if !conn.closed.Load() {
				t.Error("connection not closed")
			}
			if len(transport.incoming) != 0 {
				t.Error("incoming connection still owned after stop")
			}
			// Repeated Stop and direct RPC entry must not dispatch on the closed owner.
			if err := transport.Stop(); err != nil {
				t.Fatal(err)
			}
			for _, method := range []string{"AppendEntries", "RequestVote", "PreVote", "InstallSnapshot", "Heartbeat"} {
				transport.RegisterHandler(method, func(_ interface{}, response chan interface{}) {
					calls.Add(1)
					response <- &core.AppendEntriesResponse{Term: 1, Success: true}
				})
				if _, err := transport.handleRPC(method, []byte(`{}`)); err == nil {
					t.Errorf("%s accepted after stop", method)
				}
			}
			if calls.Load() != want {
				t.Errorf("stopped transport dispatched additional handlers: %d", calls.Load())
			}
		})
	}
}
