package raft

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

const testTransportSecret = "transport-auth-test-secret"

// signedTestFrame builds one authenticated frame from signer. mutate, when
// set, edits the auth fields after signing and the frame is re-signed unless
// keepMAC is set (to model tampering).
func signedTestFrame(signer *transportAuth, method, payload string, mutate func(*frameAuth), keepMAC bool) string {
	_, f := signer.sign(method, []byte(payload))
	if mutate != nil {
		mutate(f)
		if !keepMAC {
			f.mac = signer.requestMAC(method, f, []byte(payload))
		}
	}
	return fmt.Sprintf("%s\n%s%s %d %d %d %s\n%d\n%s", method, transportAuthPrefix, f.origin, f.epoch, f.seq, f.ts, f.mac, len(payload), payload)
}

// runAuthStream feeds input to an authenticated transport that only admits
// origin "jackal-a" and returns how many Heartbeats reached the handler.
func runAuthStream(t *testing.T, serverSecret, input string) int {
	t.Helper()
	transport, err := NewTCPTransport("unused", "unused", nil, newTestTransportLogger())
	if err != nil {
		t.Fatal(err)
	}
	transport.SetAuth("jackal-b", serverSecret, func(origin, _ string) bool { return origin == "jackal-a" })
	calls := 0
	transport.RegisterHandler("Heartbeat", func(cmd interface{}, response chan interface{}) {
		calls++
		response <- &core.HeartbeatResponse{Term: cmd.(*core.HeartbeatRequest).Term}
	})
	conn := &rpcStreamConn{input: bytes.NewReader([]byte(input))}
	transport.incoming[conn] = struct{}{}
	transport.handleConnection(conn)
	return calls
}

// Raft RPCs other than Forward used to be accepted from anyone who could reach
// the port. With a cluster secret every frame must be signed by a member,
// fresh, and not seen before; anything else is refused before the handler.
func TestTransportAuthRejectsUnsignedForgedAndReplayedRPCs(t *testing.T) {
	member := newTransportAuth("jackal-a", testTransportSecret, nil)
	outsider := newTransportAuth("jackal-x", testTransportSecret, nil)
	wrongSecret := newTransportAuth("jackal-a", "some-other-secret", nil)
	const hb = `{"term":3,"leader_id":"jackal-a"}`

	replayed := signedTestFrame(member, "Heartbeat", hb, nil, false)
	first := signedTestFrame(member, "Heartbeat", hb, nil, false)
	second := signedTestFrame(member, "Heartbeat", hb, nil, false)

	cases := []struct {
		name         string
		serverSecret string
		input        string
		want         int
	}{
		{"signed by a member", testTransportSecret, signedTestFrame(member, "Heartbeat", hb, nil, false), 1},
		{"two frames on one stream", testTransportSecret, signedTestFrame(member, "Heartbeat", hb, nil, false) + signedTestFrame(member, "Heartbeat", hb, nil, false), 2},
		{"out of order within the window", testTransportSecret, second + first, 2},
		{"unsigned", testTransportSecret, rpcTestFrame("Heartbeat", hb), 0},
		{"wrong secret", testTransportSecret, signedTestFrame(wrongSecret, "Heartbeat", hb, nil, false), 0},
		{"signed by a non-member", testTransportSecret, signedTestFrame(outsider, "Heartbeat", hb, nil, false), 0},
		{"replayed on the same stream", testTransportSecret, replayed + replayed, 1},
		{"origin rewritten", testTransportSecret, signedTestFrame(outsider, "Heartbeat", hb, func(f *frameAuth) { f.origin = "jackal-a" }, true), 0},
		{"method rewritten", testTransportSecret, strings.Replace(signedTestFrame(member, "PreVote", hb, nil, false), "PreVote", "Heartbeat", 1), 0},
		{"payload rewritten", testTransportSecret, strings.Replace(signedTestFrame(member, "Heartbeat", hb, nil, false), `"term":3`, `"term":9`, 1), 0},
		{"stale timestamp", testTransportSecret, signedTestFrame(member, "Heartbeat", hb, func(f *frameAuth) { f.ts = time.Now().Add(-2 * transportMaxSkew).UnixNano() }, false), 0},
		{"future timestamp", testTransportSecret, signedTestFrame(member, "Heartbeat", hb, func(f *frameAuth) { f.ts = time.Now().Add(2 * transportMaxSkew).UnixNano() }, false), 0},
		{"zero sequence", testTransportSecret, signedTestFrame(member, "Heartbeat", hb, func(f *frameAuth) { f.seq = 0 }, false), 0},
		{"signed frame to a node without a secret", "", signedTestFrame(member, "Heartbeat", hb, nil, false), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runAuthStream(t, tc.serverSecret, tc.input); got != tc.want {
				t.Fatalf("handler calls = %d, want %d", got, tc.want)
			}
		})
	}

	// A replay on a fresh connection is refused too: the window is per
	// sender, not per stream.
	transport, err := NewTCPTransport("unused", "unused", nil, newTestTransportLogger())
	if err != nil {
		t.Fatal(err)
	}
	transport.SetAuth("jackal-b", testTransportSecret, nil)
	calls := 0
	transport.RegisterHandler("Heartbeat", func(_ interface{}, response chan interface{}) {
		calls++
		response <- &core.HeartbeatResponse{}
	})
	frame := signedTestFrame(member, "Heartbeat", hb, nil, false)
	for i := 0; i < 2; i++ {
		conn := &rpcStreamConn{input: bytes.NewReader([]byte(frame))}
		transport.incoming[conn] = struct{}{}
		transport.handleConnection(conn)
	}
	if calls != 1 {
		t.Fatalf("a frame replayed on a new connection reached the handler: calls = %d, want 1", calls)
	}
}

// End to end over TCP: peers sharing the secret exchange RPCs and verify the
// signed responses; a peer with another secret, or none, is refused.
func TestTransportAuthBetweenRealTransports(t *testing.T) {
	cases := []struct {
		name         string
		clientSecret string
		serverSecret string
		wantOK       bool
	}{
		{"same secret", testTransportSecret, testTransportSecret, true},
		{"different secret", "another-secret", testTransportSecret, false},
		{"client unsigned", "", testTransportSecret, false},
		{"server unsigned", testTransportSecret, "", false},
		{"neither signed", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, err := NewTCPTransport("127.0.0.1:0", "", nil, newTestTransportLogger())
			if err != nil {
				t.Fatal(err)
			}
			server.SetAuth("server", tc.serverSecret, func(origin, _ string) bool { return origin == "client" })
			server.RegisterHandler("RequestVote", func(cmd interface{}, response chan interface{}) {
				response <- &core.RequestVoteResponse{Term: cmd.(*core.RequestVoteRequest).Term, VoteGranted: true}
			})
			if err := server.Start(); err != nil {
				t.Fatal(err)
			}
			defer server.Stop()

			client, err := NewTCPTransport("127.0.0.1:0", "", nil, newTestTransportLogger())
			if err != nil {
				t.Fatal(err)
			}
			client.SetAuth("client", tc.clientSecret, nil)
			defer client.Stop()
			client.RegisterPeer("server", server.listener.Addr().String())

			for i := 0; i < 3; i++ {
				resp, err := client.SendRequestVote("server", &core.RequestVoteRequest{Term: uint64(i + 1), CandidateID: "client"})
				if tc.wantOK {
					if err != nil || !resp.VoteGranted || resp.Term != uint64(i+1) {
						t.Fatalf("RPC %d: resp=%+v err=%v, want a granted vote", i, resp, err)
					}
					continue
				}
				if err == nil {
					t.Fatalf("RPC %d succeeded; it must be refused", i)
				}
			}
		})
	}
}

// A response signed for another request is refused by the client, so a
// captured response cannot be replayed onto a later request.
func TestTransportAuthRejectsMisboundResponse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	serverAuth := newTransportAuth("server", testTransportSecret, nil)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		_, _ = conn.Read(buf)
		payload := []byte(`{"term":1,"success":true}`)
		// Signed, but for a different request (seq 999).
		mac := serverAuth.responseMAC(&frameAuth{origin: "client", epoch: 1, seq: 999}, payload)
		_, _ = fmt.Fprintf(conn, "%d\n%s\n%s", len(payload), mac, payload)
	}()

	client, err := NewTCPTransport("127.0.0.1:0", "", nil, newTestTransportLogger())
	if err != nil {
		t.Fatal(err)
	}
	client.SetAuth("client", testTransportSecret, nil)
	defer client.Stop()
	client.RegisterPeer("server", ln.Addr().String())
	_, err = client.SendHeartbeat("server", &core.HeartbeatRequest{Term: 1})
	if !errors.Is(err, errTransportAuth) {
		t.Fatalf("misbound response: err = %v, want a transport authentication failure", err)
	}
}

func TestReplayWindow(t *testing.T) {
	cases := []struct {
		name string
		seqs []uint64
		want []bool
	}{
		{"increasing", []uint64{1, 2, 3}, []bool{true, true, true}},
		{"duplicate", []uint64{5, 5}, []bool{true, false}},
		{"reordered", []uint64{10, 8, 9, 8}, []bool{true, true, true, false}},
		{"gap then fill", []uint64{1, 100, 50, 50}, []bool{true, true, true, false}},
		{"word boundary", []uint64{200, 136, 137, 72, 136}, []bool{true, true, true, true, false}},
		{"below the window", []uint64{replayWindowBits + 10, 10, 11}, []bool{true, false, true}},
		{"jump clears the window", []uint64{3, 3 + 2*replayWindowBits, 3 + replayWindowBits + 1}, []bool{true, true, true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var w replayWindow
			for i, seq := range tc.seqs {
				if got := w.accept(seq); got != tc.want[i] {
					t.Fatalf("accept(%d) at step %d = %v, want %v", seq, i, got, tc.want[i])
				}
			}
		})
	}
}

// The replay state stays bounded however many sender incarnations appear.
func TestTransportAuthReplayStateIsBounded(t *testing.T) {
	a := newTransportAuth("self", testTransportSecret, nil)
	now := time.Now()
	for epoch := uint64(1); epoch <= 100; epoch++ {
		if !a.claim("peer", epoch, 1, now) {
			t.Fatalf("first frame of epoch %d refused", epoch)
		}
	}
	if n := len(a.replay["peer"].epochs); n > maxEpochsPerOrigin {
		t.Fatalf("kept %d epochs for one origin, bound is %d", n, maxEpochsPerOrigin)
	}
	for i := 0; i < maxReplayOrigins+50; i++ {
		a.claim(fmt.Sprintf("origin-%d", i), 1, 1, now)
	}
	if n := len(a.replay); n > maxReplayOrigins {
		t.Fatalf("kept %d origins, bound is %d", n, maxReplayOrigins)
	}
}

func BenchmarkTransportAuthVerify(b *testing.B) {
	signer := newTransportAuth("jackal-a", testTransportSecret, nil)
	verifier := newTransportAuth("jackal-b", testTransportSecret, nil)
	payload := []byte(`{"term":3,"leader_id":"jackal-a","prev_log_index":100,"entries":[]}`)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, f := signer.sign("AppendEntries", payload)
		if err := verifier.verify("AppendEntries", f, payload, time.Now()); err != nil {
			b.Fatal(err)
		}
	}
}
