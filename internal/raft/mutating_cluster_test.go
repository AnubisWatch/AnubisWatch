package raft

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestTCPTransportSerializesAndReusesPeerConnection(t *testing.T) {
	transport, err := NewTCPTransport("127.0.0.1:0", "127.0.0.1:0", nil, newTestTransportLogger())
	if err != nil {
		t.Fatal(err)
	}
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	transport.connections["peer-1"] = client

	serverErr := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(server)
		for i := 0; i < 2; i++ {
			method, err := reader.ReadString('\n')
			if err != nil {
				serverErr <- err
				return
			}
			if strings.TrimSpace(method) != "AppendEntries" {
				serverErr <- fmt.Errorf("unexpected method %q", method)
				return
			}
			var length int
			if _, err := fmt.Fscanf(reader, "%d\n", &length); err != nil {
				serverErr <- err
				return
			}
			payload := make([]byte, length)
			if _, err := io.ReadFull(reader, payload); err != nil {
				serverErr <- err
				return
			}
			var request core.AppendEntriesRequest
			if err := json.Unmarshal(payload, &request); err != nil {
				serverErr <- err
				return
			}
			response, _ := json.Marshal(&core.AppendEntriesResponse{Term: request.Term, Success: true})
			if _, err := fmt.Fprintf(server, "%d\n", len(response)); err != nil {
				serverErr <- err
				return
			}
			if _, err := server.Write(response); err != nil {
				serverErr <- err
				return
			}
		}
		serverErr <- nil
	}()

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, term := range []uint64{1, 2} {
		wg.Add(1)
		go func(term uint64) {
			defer wg.Done()
			response, err := transport.SendAppendEntries("peer-1", &core.AppendEntriesRequest{Term: term})
			if err == nil && (response.Term != term || !response.Success) {
				err = fmt.Errorf("unexpected response: %+v", response)
			}
			errs <- err
		}(term)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

func TestTCPTransportRejectsOversizedResponse(t *testing.T) {
	transport, err := NewTCPTransport("127.0.0.1:0", "127.0.0.1:0", nil, newTestTransportLogger())
	if err != nil {
		t.Fatal(err)
	}
	client, server := net.Pipe()
	defer server.Close()
	transport.connections["peer-1"] = client

	go func() {
		reader := bufio.NewReader(server)
		_, _ = reader.ReadString('\n')
		var length int
		_, _ = fmt.Fscanf(reader, "%d\n", &length)
		_, _ = io.CopyN(io.Discard, reader, int64(length))
		_, _ = fmt.Fprintf(server, "%d\n", maxTransportPayload+1)
	}()

	_, err = transport.SendAppendEntries("peer-1", &core.AppendEntriesRequest{Term: 1})
	if err == nil || !strings.Contains(err.Error(), "invalid response length") {
		t.Fatalf("expected oversized response error, got %v", err)
	}
	transport.connMu.Lock()
	_, cached := transport.connections["peer-1"]
	transport.connMu.Unlock()
	if cached {
		t.Fatal("oversized response connection remained cached")
	}
}

func TestStorageFSMDoesNotAdvanceIndexOnApplyError(t *testing.T) {
	applyErr := errors.New("storage write failed")
	fsm := NewStorageFSM(&failingSetStorage{InMemoryStorage: NewInMemoryStorage(), err: applyErr})
	data, err := json.Marshal(core.FSMCommand{Op: core.FSMSet, Key: "key", Value: []byte("value")})
	if err != nil {
		t.Fatal(err)
	}

	result := fsm.Apply(&core.RaftLogEntry{Index: 1, Term: 1, Type: core.LogCommand, Data: data})
	if !errors.Is(result.(error), applyErr) {
		t.Fatalf("expected storage error, got %v", result)
	}
	if got := fsm.LastApplied(); got != 0 {
		t.Fatalf("failed apply advanced FSM index to %d", got)
	}
}

func TestNodeProcessCommittedPropagatesFSMError(t *testing.T) {
	applyErr := errors.New("apply failed")
	node := createTestNode(t)
	node.fsm = &failingFSM{err: applyErr}
	node.log = append(node.log, core.RaftLogEntry{Index: 1, Term: 3, Type: core.LogCommand})
	future := &applyFuture{done: make(chan struct{})}
	node.applyWaiters.Store(uint64(1), future)
	t.Cleanup(func() { node.applyWaiters.Delete(uint64(1)) })

	node.processCommitted(1)

	select {
	case <-future.done:
		if !errors.Is(future.err, applyErr) {
			t.Fatalf("expected apply error on future, got %v", future.err)
		}
	case <-time.After(time.Second):
		t.Fatal("apply future was not notified")
	}
	if node.lastApplied != 0 {
		t.Fatalf("failed apply advanced node index to %d", node.lastApplied)
	}
}

type failingSetStorage struct {
	*InMemoryStorage
	err error
}

func (s *failingSetStorage) Set(string, []byte) error { return s.err }

type failingFSM struct{ err error }

func (f *failingFSM) Apply(*core.RaftLogEntry) interface{} { return f.err }
func (f *failingFSM) Snapshot() (core.FSMCommand, error)   { return core.FSMCommand{}, nil }
func (f *failingFSM) Restore([]byte) error                 { return nil }
