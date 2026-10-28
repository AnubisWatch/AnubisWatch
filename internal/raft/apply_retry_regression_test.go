package raft

// Regression coverage for the retry of a committed apply that failed.
//
// processCommitted returns on an apply error without advancing lastApplied.
// The run loop only re-enters processCommitted through notifyCommit, and
// notifyCommit only fires when the commit index ADVANCES (updateCommitIndex
// returns early when LeaderCommit does not grow). A follower whose storage
// failed once therefore stayed wedged at the failed index: the leader keeps
// heartbeating the same LeaderCommit value on an idle cluster, so nothing ever
// re-triggered the apply and lastApplied never moved. Snapshotting stalled
// with it, because maybeTakeSnapshot describes only the applied state.
//
// The node now schedules an explicit retry after a failed apply, so the
// applied index advances as soon as storage recovers.

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// flakyStore fails the first attempts to write one key and reports every
// write attempt on a channel, so a test can order its assertions on real
// events instead of sleeping.
type flakyStore struct {
	*InMemoryStorage
	failKey string

	mu       sync.Mutex
	failures int
	writes   map[string]int

	attempts chan string
}

func newFlakyStore(failKey string, failures int) *flakyStore {
	return &flakyStore{
		InMemoryStorage: NewInMemoryStorage(),
		failKey:         failKey,
		failures:        failures,
		writes:          make(map[string]int),
		attempts:        make(chan string, 64),
	}
}

func (s *flakyStore) Set(key string, value []byte) error {
	s.mu.Lock()
	s.writes[key]++
	failing := key == s.failKey && s.failures > 0
	if failing {
		s.failures--
	}
	s.mu.Unlock()

	select {
	case s.attempts <- key:
	default:
	}

	if failing {
		return errors.New("transient storage failure")
	}
	return s.InMemoryStorage.Set(key, value)
}

// writeCount reports how often key was written.
func (s *flakyStore) writeCount(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes[key]
}

// newFollowerTestNode creates a node that stays a follower for the whole test:
// an election would let the node commit on its own and hide the apply path
// that is under test.
func newFollowerTestNode(t *testing.T, fsm FSM) *Node {
	t.Helper()
	cfg := newTestRaftNodeConfig()
	cfg.ElectionTimeout = core.Duration{Duration: 30 * time.Second}
	cfg.HeartbeatTimeout = core.Duration{Duration: 10 * time.Second}
	cfg.CommitTimeout = core.Duration{Duration: 10 * time.Second}
	n, err := NewNode(cfg, NewInMemoryLogStore(), NewInMemorySnapshotStore(), fsm, newTestRaftLogger())
	if err != nil {
		t.Fatalf("failed to create test node: %v", err)
	}
	return n
}

// appendEntriesFor builds the leader request that replicates entry and
// commits it.
func appendEntriesFor(entry *core.RaftLogEntry, leaderCommit uint64) *core.AppendEntriesRequest {
	return &core.AppendEntriesRequest{
		Term:         entry.Term,
		LeaderID:     "leader-1",
		PrevLogIndex: entry.Index - 1,
		PrevLogTerm:  0,
		LeaderCommit: leaderCommit,
		Entries:      []core.RaftLogEntry{*entry},
	}
}

// waitForWrite blocks until the store reports a write of key.
func waitForWrite(t *testing.T, store *flakyStore, key, what string) {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case got := <-store.attempts:
			if got == key {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for the %s write of %q", what, key)
		}
	}
}

// waitApplyIdle blocks until the apply path has left its critical section, so
// the assertions run after the apply finished. It is a barrier, not a sleep.
func waitApplyIdle(t *testing.T, n *Node) bool {
	t.Helper()
	done := make(chan struct{})
	go func() {
		n.applyMu.Lock()
		n.applyMu.Unlock()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(15 * time.Second):
		return false
	}
}

// TestNodeRetriesFailedCommittedApplyWithoutNewCommit is the regression's
// core: a follower whose storage failed once must apply the committed entry
// again on its own, without a new commit and without a new leader decision.
func TestNodeRetriesFailedCommittedApplyWithoutNewCommit(t *testing.T) {
	store := newFlakyStore("flaky", 1)
	fsm := NewStorageFSM(store)

	var observedMu sync.Mutex
	var observed []string
	fsm.SetApplyObserver(func(cmd core.FSMCommand) {
		observedMu.Lock()
		observed = append(observed, cmd.Key)
		observedMu.Unlock()
	})

	n := newFollowerTestNode(t, fsm)
	n.applyRetryBackoff = 2 * time.Millisecond
	if err := n.Start(); err != nil {
		t.Fatalf("failed to start node: %v", err)
	}
	defer n.Stop()

	entry := batchEntry(t, 1, "node-a",
		core.FSMCommand{Op: core.FSMSet, Key: "first", Value: []byte("1")},
		core.FSMCommand{Op: core.FSMSet, Key: "flaky", Value: []byte("2")},
		core.FSMCommand{Op: core.FSMSet, Key: "last", Value: []byte("3")},
	)

	// The leader commits index 1.
	n.handleAppendEntries(appendEntriesFor(entry, 1))

	// The first apply attempt fails on the flaky key.
	waitForWrite(t, store, "flaky", "failing")

	// The leader keeps heartbeating the same LeaderCommit value, exactly as
	// it does on an idle cluster. The commit index does not advance, so
	// nothing but an explicit retry may re-trigger the apply.
	n.handleAppendEntries(appendEntriesFor(entry, 1))

	// The runtime must retry the failed apply on its own.
	waitForWrite(t, store, "flaky", "retried")
	if !waitApplyIdle(t, n) {
		t.Fatal("the retried apply never finished")
	}

	n.mu.RLock()
	lastApplied := n.lastApplied
	n.mu.RUnlock()
	if lastApplied != 1 {
		t.Fatalf("lastApplied is %d, want 1: the failed apply was never retried", lastApplied)
	}
	if fsm.LastApplied() != 1 {
		t.Fatalf("the FSM applied index is %d, want 1", fsm.LastApplied())
	}
	for key, want := range map[string]string{"first": "1", "flaky": "2", "last": "3"} {
		value, err := store.Get(key)
		if err != nil || string(value) != want {
			t.Fatalf("key %q holds %q (%v), want %q", key, value, err, want)
		}
	}

	// The observer must see every command exactly once, even though the
	// batch was applied twice (the failed attempt and the retry).
	observedMu.Lock()
	got := append([]string(nil), observed...)
	observedMu.Unlock()
	want := []string{"first", "flaky", "last"}
	if len(got) != len(want) {
		t.Fatalf("the observer saw %v, want %v exactly once each", got, want)
	}
	for i, key := range want {
		if got[i] != key {
			t.Fatalf("the observer saw %v, want %v", got, want)
		}
	}
}

// TestNodeAppliesCommittedEntryWhenStorageIsHealthy is the nearby successful
// control: without a storage failure the entry applies on the first attempt
// and is neither retried nor reported twice.
func TestNodeAppliesCommittedEntryWhenStorageIsHealthy(t *testing.T) {
	store := newFlakyStore("", 0)
	fsm := NewStorageFSM(store)

	var observedMu sync.Mutex
	var observed []string
	fsm.SetApplyObserver(func(cmd core.FSMCommand) {
		observedMu.Lock()
		observed = append(observed, cmd.Key)
		observedMu.Unlock()
	})

	n := newFollowerTestNode(t, fsm)
	n.applyRetryBackoff = 2 * time.Millisecond
	if err := n.Start(); err != nil {
		t.Fatalf("failed to start node: %v", err)
	}
	defer n.Stop()

	entry := batchEntry(t, 1, "node-a",
		core.FSMCommand{Op: core.FSMSet, Key: "first", Value: []byte("1")},
		core.FSMCommand{Op: core.FSMSet, Key: "last", Value: []byte("3")},
	)
	n.handleAppendEntries(appendEntriesFor(entry, 1))

	waitForWrite(t, store, "last", "successful")
	if !waitApplyIdle(t, n) {
		t.Fatal("the apply never finished")
	}

	n.mu.RLock()
	lastApplied := n.lastApplied
	n.mu.RUnlock()
	if lastApplied != 1 {
		t.Fatalf("lastApplied is %d, want 1", lastApplied)
	}
	if fsm.LastApplied() != 1 {
		t.Fatalf("the FSM applied index is %d, want 1", fsm.LastApplied())
	}
	if count := store.writeCount("first"); count != 1 {
		t.Fatalf("key \"first\" was written %d times, want 1", count)
	}
	if count := store.writeCount("last"); count != 1 {
		t.Fatalf("key \"last\" was written %d times, want 1", count)
	}

	observedMu.Lock()
	got := append([]string(nil), observed...)
	observedMu.Unlock()
	want := []string{"first", "last"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("the observer saw %v, want %v", got, want)
	}
}
