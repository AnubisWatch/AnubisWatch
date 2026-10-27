package raft

import (
	"errors"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type retryableBatchStore struct {
	*InMemoryStorage
	failKey string
	calls   map[string]int
}

func (s *retryableBatchStore) Set(key string, value []byte) error {
	s.calls[key]++
	if key == s.failKey {
		return errors.New("disk unavailable")
	}
	return s.InMemoryStorage.Set(key, value)
}

func TestFSMBatchRetriesFailedWritesWithoutAdvancingIndex(t *testing.T) {
	for _, failKey := range []string{"first", "middle", "last"} {
		t.Run(failKey, func(t *testing.T) {
			store := &retryableBatchStore{InMemoryStorage: NewInMemoryStorage(), failKey: failKey, calls: make(map[string]int)}
			fsm := NewStorageFSM(store)
			entry := batchEntry(t, 1, "node", core.FSMCommand{Op: core.FSMSet, Key: "first", Value: []byte("1")}, core.FSMCommand{Op: core.FSMSet, Key: "middle", Value: []byte("2")}, core.FSMCommand{Op: core.FSMSet, Key: "last", Value: []byte("3")})
			if result := fsm.Apply(entry); result == nil {
				t.Fatal("batch hid storage error")
			}
			if fsm.LastApplied() != 0 {
				t.Fatalf("failed batch advanced index to %d", fsm.LastApplied())
			}
			store.failKey = ""
			if result := fsm.Apply(entry); result != nil {
				t.Fatalf("retry failed: %v", result)
			}
			for key, want := range map[string]string{"first": "1", "middle": "2", "last": "3"} {
				v, e := store.Get(key)
				if e != nil || string(v) != want {
					t.Fatalf("retry missed %s: %q %v", key, v, e)
				}
			}
			calls := store.calls["last"]
			if result := fsm.Apply(entry); result != nil {
				t.Fatal(result)
			}
			if store.calls["last"] != calls || fsm.LastApplied() != 1 {
				t.Fatal("successful retry is not idempotent")
			}
		})
	}
}

func TestNodeDoesNotAdvancePastFailedBatch(t *testing.T) {
	store := &retryableBatchStore{InMemoryStorage: NewInMemoryStorage(), failKey: "bad", calls: make(map[string]int)}
	fsm := NewStorageFSM(store)
	n, e := NewNode(newTestRaftNodeConfig(), NewInMemoryLogStore(), NewInMemorySnapshotStore(), fsm, newTestRaftLogger())
	if e != nil {
		t.Fatal(e)
	}
	entry := batchEntry(t, 1, "", core.FSMCommand{Op: core.FSMSet, Key: "good", Value: []byte("ok")}, core.FSMCommand{Op: core.FSMSet, Key: "bad", Value: []byte("retry")})
	n.log = append(n.log, *entry)
	n.commitIndex = 1
	n.processCommitted(1)
	if n.lastApplied != 0 || fsm.LastApplied() != 0 {
		t.Fatal("node acknowledged failed batch")
	}
	// Snapshot creation must not compact a committed but unapplied entry.
	n.snapshotThreshold = 1
	n.maybeTakeSnapshot()
	if n.logBase != 0 {
		t.Fatal("snapshot compacted unapplied batch")
	}
	store.failKey = ""
	n.processCommitted(1)
	if n.lastApplied != 1 || fsm.LastApplied() != 1 {
		t.Fatal("node did not retry committed batch")
	}
	v, e := store.Get("bad")
	if e != nil || string(v) != "retry" {
		t.Fatalf("missing retry: %q %v", v, e)
	}
}

// TestFSMBatchClassifiesElementFailures separates the failures a batch
// element can hit. A deterministic failure (a rejected command, a nested
// batch) can never be applied, so the element is skipped and the entry is
// consumed: retrying it would wedge the node at that index. A failure the
// handler reports as transient keeps the entry unapplied, so the node can
// retry it once the cause is gone.
func TestFSMBatchClassifiesElementFailures(t *testing.T) {
	fsm := NewStorageFSM(NewInMemoryStorage())
	fail := true
	fsm.SetCommandHandler("custom", func(cmd *core.FSMCommand) error {
		if fail {
			return errors.New("deterministic handler failure")
		}
		return nil
	})
	e := batchEntry(t, 1, "", core.FSMCommand{Op: core.FSMSet, Table: "custom", Key: "handler"})
	if r := fsm.Apply(e); r != nil {
		t.Fatalf("a deterministic element failure failed the whole entry: %v", r)
	}
	if fsm.LastApplied() != 1 {
		t.Fatalf("the entry was not consumed, index %d", fsm.LastApplied())
	}
	if r := fsm.Apply(batchEntry(t, 2, "", core.FSMCommand{Op: core.FSMBatch})); r != nil {
		t.Fatalf("a nested batch failed the whole entry: %v", r)
	}
	if fsm.LastApplied() != 2 {
		t.Fatalf("the entry was not consumed, index %d", fsm.LastApplied())
	}

	// A handler that reports a transient failure keeps the entry unapplied,
	// so the node retries it instead of losing the command.
	fsm.SetCommandHandler("custom", func(cmd *core.FSMCommand) error {
		return Retryable(errors.New("transient handler failure"))
	})
	e = batchEntry(t, 3, "", core.FSMCommand{Op: core.FSMSet, Table: "custom", Key: "handler"})
	if r := fsm.Apply(e); r == nil {
		t.Fatal("a transient element failure was acknowledged")
	}
	if fsm.LastApplied() != 2 {
		t.Fatalf("a transient failure advanced the index to %d", fsm.LastApplied())
	}
}
