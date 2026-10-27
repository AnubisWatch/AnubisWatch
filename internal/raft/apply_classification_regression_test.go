package raft

// Regression coverage for the classification of committed-apply failures.
//
// A committed entry can fail to apply for two very different reasons. A
// transient storage failure may still succeed on a later attempt, so the
// node retries it (see apply_retry_regression_test.go). A deterministic
// failure -- a rejected command, an unknown op, an undecodable payload --
// can never succeed, so retrying it forever wedges the node at that index
// and stalls snapshotting and log compaction with it.
//
// The node must therefore classify the failure: transient failures are
// retried, deterministic ones are skipped so the node keeps applying the
// rest of the log.

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// recordingHandler captures the messages the node and the FSM log, so a test
// can count how often a decision was made (for example how often an element
// was skipped) and gate its assertions on real events instead of timing.
type recordingHandler struct {
	mu       sync.Mutex
	messages []string
	events   chan string
}

func newRecordingHandler() *recordingHandler {
	return &recordingHandler{events: make(chan string, 256)}
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.messages = append(h.messages, r.Message)
	h.mu.Unlock()
	select {
	case h.events <- r.Message:
	default:
	}
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

// count reports how often msg was logged.
func (h *recordingHandler) count(msg string) int {
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

// waitForLog blocks until msg was logged. It is a gate, not a sleep.
func waitForLog(t *testing.T, h *recordingHandler, msg string) {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case got := <-h.events:
			if got == msg {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for the log message %q", msg)
		}
	}
}

// waitForObserved blocks until the apply observer reports key.
func waitForObserved(t *testing.T, observed <-chan string, key string) {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case got := <-observed:
			if got == key {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for the observer to report %q", key)
		}
	}
}

// appendEntriesAfter builds the leader request that replicates entry on top
// of the entry with the given term, which the leader must name so the
// follower can check its log against it.
func appendEntriesAfter(entry *core.RaftLogEntry, prevTerm uint64, leaderCommit uint64) *core.AppendEntriesRequest {
	return &core.AppendEntriesRequest{
		Term:         entry.Term,
		LeaderID:     "leader-1",
		PrevLogIndex: entry.Index - 1,
		PrevLogTerm:  prevTerm,
		LeaderCommit: leaderCommit,
		Entries:      []core.RaftLogEntry{*entry},
	}
}

// TestNodeSkipsDeterministicApplyFailureAndKeepsProgressing is the core of
// the classification: a command that can never be applied must not wedge the
// node. The entry is skipped, the node keeps applying the log, and the skip
// is decided exactly once (a retry would decide it again).
func TestNodeSkipsDeterministicApplyFailureAndKeepsProgressing(t *testing.T) {
	store := newFlakyStore("", 0)
	fsm := NewStorageFSM(store)
	logs := newRecordingHandler()
	fsm.SetLogger(slog.New(logs))

	// The handler rejects one command deterministically, the way a
	// replicated judgment that cannot be decoded is rejected.
	attempted := make(chan struct{}, 4)
	var callsMu sync.Mutex
	var calls int
	fsm.SetCommandHandler("judgments", func(cmd *core.FSMCommand) error {
		callsMu.Lock()
		calls++
		callsMu.Unlock()
		select {
		case attempted <- struct{}{}:
		default:
		}
		return errors.New("decode replicated judgment: invalid payload")
	})

	observedCh := make(chan string, 64)
	var observedMu sync.Mutex
	var observed []string
	fsm.SetApplyObserver(func(cmd core.FSMCommand) {
		observedMu.Lock()
		observed = append(observed, cmd.Key)
		observedMu.Unlock()
		observedCh <- cmd.Key
	})

	n := newFollowerTestNode(t, fsm)
	n.applyRetryBackoff = 2 * time.Millisecond
	if err := n.Start(); err != nil {
		t.Fatalf("failed to start node: %v", err)
	}
	defer n.Stop()

	entry := batchEntry(t, 1, "node-a",
		core.FSMCommand{Op: core.FSMSet, Key: "first", Value: []byte("1")},
		core.FSMCommand{Op: core.FSMSet, Table: "judgments", Key: "bad", Value: []byte("{}")},
		core.FSMCommand{Op: core.FSMSet, Key: "last", Value: []byte("3")},
	)

	// The leader commits index 1.
	n.handleAppendEntries(appendEntriesFor(entry, 1))

	// The rejected element was reached: the gate proves the apply ran.
	select {
	case <-attempted:
	case <-time.After(15 * time.Second):
		t.Fatal("the rejected element was never reached")
	}
	if !waitApplyIdle(t, n) {
		t.Fatal("the apply never finished")
	}

	// The node progressed: the entry was consumed instead of wedging it.
	n.mu.RLock()
	lastApplied := n.lastApplied
	n.mu.RUnlock()
	if lastApplied != 1 {
		t.Fatalf("lastApplied is %d, want 1: the deterministic failure wedged the node", lastApplied)
	}
	if fsm.LastApplied() != 1 {
		t.Fatalf("the FSM applied index is %d, want 1", fsm.LastApplied())
	}

	// The skip was decided exactly once. A retry would decide it again and
	// the rejected element would be attempted again.
	if got := logs.count("Skipping batch element that failed to apply"); got != 1 {
		t.Fatalf("the skip was decided %d times, want exactly 1", got)
	}
	callsMu.Lock()
	handlerCalls := calls
	callsMu.Unlock()
	if handlerCalls != 1 {
		t.Fatalf("the rejected element was attempted %d times, want 1", handlerCalls)
	}
	if count := store.writeCount("first"); count != 1 {
		t.Fatalf("key \"first\" was written %d times, want 1: the entry was replayed", count)
	}

	// The observer saw the applied commands exactly once each, and never the
	// rejected one.
	observedMu.Lock()
	got := append([]string(nil), observed...)
	observedMu.Unlock()
	want := []string{"first", "last"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("the observer saw %v, want %v", got, want)
	}

	// The node keeps applying the rest of the log.
	second := batchEntry(t, 2, "node-a",
		core.FSMCommand{Op: core.FSMSet, Key: "after", Value: []byte("4")},
	)
	n.handleAppendEntries(appendEntriesAfter(second, 1, 2))
	waitForObserved(t, observedCh, "after")
	// The observer runs inside the apply, so the barrier waits for the pass
	// that consumed the entry to finish.
	if !waitApplyIdle(t, n) {
		t.Fatal("the apply never finished")
	}

	n.mu.RLock()
	lastApplied = n.lastApplied
	n.mu.RUnlock()
	if lastApplied != 2 {
		t.Fatalf("lastApplied is %d, want 2: the node stopped applying the log", lastApplied)
	}
}

// TestNodeSkipsEntryThatCanNeverBeDecoded covers the deterministic failure
// of a whole entry: the payload cannot be decoded, so no attempt can apply
// it. The node must skip the entry instead of retrying it forever.
func TestNodeSkipsEntryThatCanNeverBeDecoded(t *testing.T) {
	store := newFlakyStore("", 0)
	fsm := NewStorageFSM(store)
	logs := newRecordingHandler()

	cfg := newTestRaftNodeConfig()
	cfg.ElectionTimeout = core.Duration{Duration: 30 * time.Second}
	cfg.HeartbeatTimeout = core.Duration{Duration: 10 * time.Second}
	cfg.CommitTimeout = core.Duration{Duration: 10 * time.Second}
	n, err := NewNode(cfg, NewInMemoryLogStore(), NewInMemorySnapshotStore(), fsm, slog.New(logs))
	if err != nil {
		t.Fatalf("failed to create test node: %v", err)
	}
	n.applyRetryBackoff = 2 * time.Millisecond
	if err := n.Start(); err != nil {
		t.Fatalf("failed to start node: %v", err)
	}
	defer n.Stop()

	entry := &core.RaftLogEntry{Index: 1, Term: 1, Type: core.LogCommand, Data: []byte("{not json")}
	n.handleAppendEntries(appendEntriesFor(entry, 1))

	// The gate: the node decided to skip the entry it can never apply.
	waitForLog(t, logs, "Skipping committed entry that cannot be applied")
	// The log announces the decision before the applied index is published.
	// Join that apply pass before inspecting its durable outcome.
	if !waitApplyIdle(t, n) {
		t.Fatal("the skip decision did not finish applying")
	}

	n.mu.RLock()
	lastApplied := n.lastApplied
	n.mu.RUnlock()
	if lastApplied != 1 {
		t.Fatalf("lastApplied is %d, want 1: the undecodable entry wedged the node", lastApplied)
	}

	// The entry is consumed, so the node keeps applying the log.
	second := batchEntry(t, 2, "node-a",
		core.FSMCommand{Op: core.FSMSet, Key: "after", Value: []byte("4")},
	)
	n.handleAppendEntries(appendEntriesAfter(second, 1, 2))
	waitForWrite(t, store, "after", "successful")
	if !waitApplyIdle(t, n) {
		t.Fatal("the apply never finished")
	}
	n.mu.RLock()
	lastApplied = n.lastApplied
	n.mu.RUnlock()
	if lastApplied != 2 {
		t.Fatalf("lastApplied is %d, want 2: the node stopped applying the log", lastApplied)
	}
}
