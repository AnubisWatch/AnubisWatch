package raft

// Regression coverage for the applied-index write on the apply path.
//
// Persisting the hard state after every applied entry cost an fsync per entry
// on top of the FSM's own, which halved how fast a restarted follower caught
// up; a two-hour soak saw a follower stay seconds behind the leader under load
// until its read barriers timed out. The applied index is now written once per
// pass, and a failed write must not lose track of the entries already applied.

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// countingStableStore counts hard-state writes and can be told to fail them.
type countingStableStore struct {
	*InMemoryStableStore
	mu     sync.Mutex
	writes int
	fail   bool
}

func (s *countingStableStore) Set(key string, val []byte) error {
	if key == stableHardState {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.fail {
			return errors.New("disk full")
		}
		s.writes++
	}
	return s.InMemoryStableStore.Set(key, val)
}

func (s *countingStableStore) hardStateWrites() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes
}

func (s *countingStableStore) setFail(fail bool) {
	s.mu.Lock()
	s.fail = fail
	s.mu.Unlock()
}

func (s *countingStableStore) persistedApplied(t *testing.T) uint64 {
	t.Helper()
	data, err := s.InMemoryStableStore.Get(stableHardState)
	if err != nil {
		t.Fatalf("read hard state: %v", err)
	}
	var hs hardState
	if err := json.Unmarshal(data, &hs); err != nil {
		t.Fatalf("decode hard state: %v", err)
	}
	return hs.LastApplied
}

func newAppliedPersistNode(t *testing.T, entries int) (*Node, *countingStableStore) {
	t.Helper()
	stable := &countingStableStore{InMemoryStableStore: NewInMemoryStableStore()}
	n, err := NewNodeWithStableStore(newTestRaftNodeConfig(), NewInMemoryLogStore(), stable, NewInMemorySnapshotStore(), NewStorageFSM(newFlakyStore("", 0)), newTestRaftLogger())
	if err != nil {
		t.Fatalf("failed to create test node: %v", err)
	}
	for i := 1; i <= entries; i++ {
		data, err := json.Marshal(core.FSMCommand{Op: core.FSMSet, Key: "k", Value: []byte{byte(i)}})
		if err != nil {
			t.Fatal(err)
		}
		n.log = append(n.log, core.RaftLogEntry{Index: uint64(i), Term: 1, Type: core.LogCommand, Data: data})
	}
	n.commitIndex = uint64(entries)
	return n, stable
}

func TestProcessCommittedPersistsAppliedIndexOncePerPass(t *testing.T) {
	n, stable := newAppliedPersistNode(t, 10)
	n.processCommitted(10)

	if got := stable.hardStateWrites(); got != 1 {
		t.Fatalf("applying 10 entries wrote the hard state %d times, want 1", got)
	}
	if got := stable.persistedApplied(t); got != 10 {
		t.Fatalf("persisted lastApplied = %d, want 10", got)
	}
}

func TestProcessCommittedBoundsUnpersistedEntries(t *testing.T) {
	entries := 2*appliedPersistEvery + 1
	n, stable := newAppliedPersistNode(t, entries)
	n.processCommitted(uint64(entries))

	if got := stable.hardStateWrites(); got != 3 {
		t.Fatalf("applying %d entries wrote the hard state %d times, want 3", entries, got)
	}
	if got := stable.persistedApplied(t); got != uint64(entries) {
		t.Fatalf("persisted lastApplied = %d, want %d", got, entries)
	}
}

func TestProcessCommittedRetriesFailedAppliedIndexWrite(t *testing.T) {
	n, stable := newAppliedPersistNode(t, 3)
	stable.setFail(true)
	n.processCommitted(3)

	// The entries are in the store, so the in-memory index keeps them.
	n.mu.RLock()
	lastApplied := n.lastApplied
	n.mu.RUnlock()
	if lastApplied != 3 {
		t.Fatalf("lastApplied = %d after a failed hard-state write, want 3", lastApplied)
	}
	select {
	case <-n.applyRetryCh:
	default:
		t.Fatal("a failed hard-state write did not schedule a retry")
	}

	// Nothing is left to apply, yet the retry pass must still write the
	// index it could not write before.
	stable.setFail(false)
	n.processCommitted(3)
	if got := stable.persistedApplied(t); got != 3 {
		t.Fatalf("persisted lastApplied = %d after the retry, want 3", got)
	}
	n.mu.RLock()
	dirty := n.appliedDirty
	n.mu.RUnlock()
	if dirty {
		t.Fatal("appliedDirty still set after a successful write")
	}
}
