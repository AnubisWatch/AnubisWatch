package raft

// Regression coverage for the apply observer of a batch that failed.
//
// StorageFSM.Apply used to notify the observer with the commands of the
// successful prefix of a batch that failed part-way. That prefix was never
// durably applied: the FSM index stays behind the failed entry, so the whole
// batch is replayed by the next attempt. The observer therefore saw the same
// command twice (once per attempt) and, in between, described a state the
// FSM never committed to.
//
// The observer must only learn about commands whose entry was applied
// successfully, so its delivery stays exactly-once per command across
// retries.

import (
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TestFSMBatchObserverSeesEachCommandOnceAcrossRetries is the regression's
// core: a failed batch must not reach the observer, and the retry must
// deliver every command exactly once.
func TestFSMBatchObserverSeesEachCommandOnceAcrossRetries(t *testing.T) {
	store := &retryableBatchStore{InMemoryStorage: NewInMemoryStorage(), failKey: "middle", calls: make(map[string]int)}
	fsm := NewStorageFSM(store)

	var observed []string
	fsm.SetApplyObserver(func(cmd core.FSMCommand) { observed = append(observed, cmd.Key) })

	entry := batchEntry(t, 1, "node-a",
		core.FSMCommand{Op: core.FSMSet, Key: "first", Value: []byte("1")},
		core.FSMCommand{Op: core.FSMSet, Key: "middle", Value: []byte("2")},
		core.FSMCommand{Op: core.FSMSet, Key: "last", Value: []byte("3")},
	)

	// The batch fails on its second element, after "first" was written.
	if result := fsm.Apply(entry); result == nil {
		t.Fatal("the batch hid the storage error")
	}
	if fsm.LastApplied() != 0 {
		t.Fatalf("the failed batch advanced the applied index to %d", fsm.LastApplied())
	}
	if len(observed) != 0 {
		t.Fatalf("the observer saw %v for a batch that was not applied; the successful "+
			"prefix is replayed by the next attempt and must not be reported twice", observed)
	}

	// Storage recovers and the batch is retried.
	store.failKey = ""
	if result := fsm.Apply(entry); result != nil {
		t.Fatalf("the retry failed: %v", result)
	}
	if fsm.LastApplied() != 1 {
		t.Fatalf("the retry did not advance the applied index (got %d)", fsm.LastApplied())
	}

	want := []string{"first", "middle", "last"}
	if len(observed) != len(want) {
		t.Fatalf("the observer saw %v, want %v exactly once each", observed, want)
	}
	for i, key := range want {
		if observed[i] != key {
			t.Fatalf("the observer saw %v, want %v", observed, want)
		}
	}
}

// TestFSMBatchObserverKeepsReportingSuccessfulBatches is the nearby
// successful control: a batch that applies cleanly must still reach the
// observer, element by element and in order.
func TestFSMBatchObserverKeepsReportingSuccessfulBatches(t *testing.T) {
	fsm := NewStorageFSM(NewInMemoryStorage())

	var observed []string
	fsm.SetApplyObserver(func(cmd core.FSMCommand) { observed = append(observed, cmd.Key) })

	entry := batchEntry(t, 1, "node-a",
		core.FSMCommand{Op: core.FSMSet, Key: "first", Value: []byte("1")},
		core.FSMCommand{Op: core.FSMSet, Key: "last", Value: []byte("3")},
	)
	if result := fsm.Apply(entry); result != nil {
		t.Fatalf("a valid batch failed: %v", result)
	}
	if len(observed) != 2 || observed[0] != "first" || observed[1] != "last" {
		t.Fatalf("the observer saw %v, want [first last]", observed)
	}
}
