package main

// Shared harness for the replicated-write regression tests.
//
// A single-node Raft cluster applies the committed commands on its own, so the
// node's real apply, skip and retry machinery decides what happens to a
// command: a transient failure is retried, a deterministic one is skipped and
// the entry is consumed. The handler under test is registered for one command
// table, exactly as production registers it.

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/raft"
)

// countingHandler counts the log messages it records, so a test can assert
// that a decision (for example a skip) was made exactly once.
type countingHandler struct {
	mu       sync.Mutex
	messages []string
}

func (h *countingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *countingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.messages = append(h.messages, r.Message)
	h.mu.Unlock()
	return nil
}

func (h *countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *countingHandler) WithGroup(string) slog.Handler      { return h }

// count reports how often msg was logged.
func (h *countingHandler) count(msg string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	total := 0
	for _, m := range h.messages {
		if m == msg {
			total++
		}
	}
	return total
}

// retryTestNode is a single-node Raft cluster with one command table routed
// to a handler under test.
type retryTestNode struct {
	node  *raft.Node
	fsm   *raft.StorageFSM
	store *raft.InMemoryStorage
	logs  *countingHandler

	// observed reports the keys of the commands that reached the store, so a
	// test can wait for the apply to complete instead of guessing when it
	// did.
	observed chan string
}

// newRetryTestNode starts a single-node cluster that routes the commands of
// table to handler.
func newRetryTestNode(t *testing.T, table string, handler raft.CommandHandler) *retryTestNode {
	t.Helper()

	tn := &retryTestNode{logs: &countingHandler{}, store: raft.NewInMemoryStorage(), observed: make(chan string, 64)}
	tn.fsm = raft.NewStorageFSM(tn.store)
	tn.fsm.SetLogger(slog.New(tn.logs))
	tn.fsm.SetCommandHandler(table, handler)
	tn.fsm.SetApplyObserver(func(cmd core.FSMCommand) {
		select {
		case tn.observed <- cmd.Key:
		default:
		}
	})

	cfg := core.RaftConfig{
		NodeID:           "retry-node-1",
		BindAddr:         "127.0.0.1:0",
		AdvertiseAddr:    "127.0.0.1:0",
		ElectionTimeout:  core.Duration{Duration: 150 * time.Millisecond},
		HeartbeatTimeout: core.Duration{Duration: 50 * time.Millisecond},
		CommitTimeout:    core.Duration{Duration: 50 * time.Millisecond},
		MaxAppendEntries: 64,
	}
	n, err := raft.NewNode(cfg, raft.NewInMemoryLogStore(), raft.NewInMemorySnapshotStore(), tn.fsm, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("failed to create the test node: %v", err)
	}
	if err := n.Start(); err != nil {
		t.Fatalf("failed to start the test node: %v", err)
	}
	t.Cleanup(func() { _ = n.Stop() })
	tn.node = n
	return tn
}

// waitForLeader blocks until the node won its election.
func (tn *retryTestNode) waitForLeader(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if tn.node.IsLeader() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the node never became leader")
}

// waitForApplied blocks until the FSM consumed the given entry.
func (tn *retryTestNode) waitForApplied(t *testing.T, index uint64, what string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for tn.fsm.LastApplied() != index && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := tn.fsm.LastApplied(); got != index {
		t.Fatalf("the FSM applied index is %d, want %d: %s", got, index, what)
	}
}

// waitForObserved blocks until the apply observer reports key, which is the
// point where the command reached the store.
func (tn *retryTestNode) waitForObserved(t *testing.T, key string) {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case got := <-tn.observed:
			if got == key {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for the observer to report %q", key)
		}
	}
}
