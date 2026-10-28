package main

// Regression coverage for the failure classification of the shared
// authentication handler.
//
// A committed authentication change is a committed write: the leader already
// accepted it, so every node must end up with the new shared state. The
// handler therefore has to say whether a failure is transient.
//
//   - A storage failure may still succeed on a later attempt, so it is
//     wrapped with raft.Retryable and the node retries the committed entry.
//   - A change that cannot be decoded, or that the state machine rejects, can
//     never be applied, so it stays a plain error and the entry is skipped.
//
// Without the retryable wrapper the committed authentication change was lost
// for good: the node classified the failure as deterministic, skipped the
// entry, and the shared state never converged.

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/auth"
	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// flakyAuthStorage is the storage the shared-authentication handler writes
// through. It fails the first write on demand and reports every attempt on a
// channel, so a test can order its assertions on real events.
type flakyAuthStorage struct {
	mu       sync.Mutex
	data     map[string][]byte
	failures int
	puts     int

	attempts chan string
}

func newFlakyAuthStorage(failures int) *flakyAuthStorage {
	return &flakyAuthStorage{data: map[string][]byte{}, failures: failures, attempts: make(chan string, 64)}
}

// Get returns the stored value, or reports the key as missing.
func (s *flakyAuthStorage) Get(key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.data[key]
	if !ok {
		return nil, &core.NotFoundError{Entity: "key", ID: key}
	}
	return value, nil
}

// Put records the value, or fails while the store still has failures left to
// report.
func (s *flakyAuthStorage) Put(key string, value []byte) error {
	s.mu.Lock()
	failing := s.failures > 0
	if failing {
		s.failures--
	} else {
		s.data[key] = value
	}
	s.puts++
	s.mu.Unlock()

	select {
	case s.attempts <- key:
	default:
	}
	if failing {
		return errors.New("transient storage failure")
	}
	return nil
}

// putCount reports how often a write was attempted.
func (s *flakyAuthStorage) putCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.puts
}

// TestClusterAuthStorageFailureIsRetriedAndConverges proves the shared
// authentication handler classifies a storage failure as transient: the node
// retries the committed entry and the shared state ends up stored.
//
// Before the fix the handler returned the storage error unwrapped, so the
// node skipped the entry and the committed change was lost.
func TestClusterAuthStorageFailureIsRetriedAndConverges(t *testing.T) {
	store := newFlakyAuthStorage(1)
	shared := &clusterAuthStore{store: store}

	var mu sync.Mutex
	calls := 0
	tn := newRetryTestNode(t, clusterAuthTable, func(cmd *core.FSMCommand) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return shared.apply(cmd)
	})
	tn.waitForLeader(t)

	change := auth.ClusterAuthChange{Expected: 0, WriteID: "write-1", Data: json.RawMessage(`{"users":{},"tokens":{}}`)}
	raw, err := json.Marshal(change)
	if err != nil {
		t.Fatalf("failed to encode the change: %v", err)
	}
	cmd := core.FSMCommand{Op: core.FSMSet, Table: clusterAuthTable, Key: clusterAuthStateKey, Value: raw}

	index, _, _, applyErr := tn.node.Apply(cmd, 15*time.Second)

	// The failure is surfaced, because the entry could not be applied on the
	// first attempt. The node retries it on its own.
	if applyErr == nil {
		t.Fatal("the transient storage failure was swallowed: the entry was skipped instead of retried")
	}

	// The gate: the retry applied the entry, so the observer saw the command.
	tn.waitForObserved(t, clusterAuthStateKey)
	tn.waitForApplied(t, index, "the retried entry never converged")

	// The shared state converged: the committed change is stored.
	state, err := shared.Load()
	if err != nil {
		t.Fatalf("failed to read the shared state: %v", err)
	}
	if state == nil || state.Revision != 1 {
		t.Fatalf("the shared state is %+v, want revision 1", state)
	}

	// The write was attempted twice: the failed attempt and the retry.
	if got := store.putCount(); got != 2 {
		t.Fatalf("the state was written %d times, want 2: the failed attempt was not retried", got)
	}
	mu.Lock()
	handlerCalls := calls
	mu.Unlock()
	if handlerCalls != 2 {
		t.Fatalf("the handler ran %d times, want 2: the failed attempt was not retried", handlerCalls)
	}
}

// TestClusterAuthDecodeFailureStaysDeterministic is the other half of the
// classification: a change that can never be decoded is deterministic, so it
// is not retried. The node skips the entry and keeps applying the log.
func TestClusterAuthDecodeFailureStaysDeterministic(t *testing.T) {
	store := newFlakyAuthStorage(0)
	shared := &clusterAuthStore{store: store}

	var mu sync.Mutex
	calls := 0
	tn := newRetryTestNode(t, clusterAuthTable, func(cmd *core.FSMCommand) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return shared.apply(cmd)
	})
	tn.waitForLeader(t)

	// The change cannot be decoded, so no attempt can apply it.
	cmd := core.FSMCommand{Op: core.FSMSet, Table: clusterAuthTable, Key: clusterAuthStateKey, Value: []byte("{not json")}
	index, _, _, applyErr := tn.node.Apply(cmd, 15*time.Second)
	if applyErr == nil {
		t.Fatal("an undecodable change must not be applied")
	}

	// The gate: the entry was consumed, so the applied index moved past it.
	tn.waitForApplied(t, index, "the rejected entry wedged the node")

	// Nothing was written: the change was never applied.
	if got := store.putCount(); got != 0 {
		t.Fatalf("the state was written %d times, want none", got)
	}
	mu.Lock()
	handlerCalls := calls
	mu.Unlock()
	if handlerCalls != 1 {
		t.Fatalf("the rejected entry was attempted %d times, want 1: it was retried", handlerCalls)
	}
}
