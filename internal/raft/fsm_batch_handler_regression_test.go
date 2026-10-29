package raft

import (
	"errors"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Runs of one table inside a batch go to its batch handler in one call
// (replicated judgments are saved in one durable write); a lone command
// still goes to the per-command handler, and other commands keep their order.
func TestFSMBatchHandlerReceivesRunsOfItsTable(t *testing.T) {
	store := NewInMemoryStorage()
	fsm := NewStorageFSM(store)
	var single []string
	var runs [][]string
	fsm.SetCommandHandler("judgments", func(cmd *core.FSMCommand) error {
		single = append(single, cmd.Key)
		return nil
	})
	fsm.SetBatchCommandHandler("judgments", func(cmds []core.FSMCommand) ([]core.FSMCommand, error) {
		var keys []string
		for _, c := range cmds {
			keys = append(keys, c.Key)
		}
		runs = append(runs, keys)
		return cmds, nil
	})
	j := func(key string) core.FSMCommand {
		return core.FSMCommand{Op: core.FSMSet, Table: "judgments", Key: key, Value: []byte("{}")}
	}
	entry := batchEntry(t, 7, "node",
		j("j1"), j("j2"), j("j3"),
		core.FSMCommand{Op: core.FSMSet, Key: "plain", Value: []byte("v")},
		j("j4"),
		core.FSMCommand{Op: core.FSMDelete, Key: "plain"},
		j("j5"), j("j6"))
	if result := fsm.Apply(entry); result != nil {
		t.Fatalf("Apply = %v", result)
	}
	if len(runs) != 2 || len(runs[0]) != 3 || len(runs[1]) != 2 || runs[1][0] != "j5" {
		t.Fatalf("batch runs = %v, want [j1 j2 j3] and [j5 j6]", runs)
	}
	if len(single) != 1 || single[0] != "j4" {
		t.Fatalf("single-command handler saw %v, want [j4]", single)
	}
	if _, err := store.Get("plain"); err == nil {
		t.Fatal("the delete after the set was not applied in order")
	}
}

// A batch handler failure leaves the entry unapplied so the node retries it.
func TestFSMBatchHandlerFailureIsRetried(t *testing.T) {
	fsm := NewStorageFSM(NewInMemoryStorage())
	fsm.SetBatchCommandHandler("judgments", func(cmds []core.FSMCommand) ([]core.FSMCommand, error) {
		return nil, errors.New("disk unavailable")
	})
	j := core.FSMCommand{Op: core.FSMSet, Table: "judgments", Key: "j", Value: []byte("{}")}
	result := fsm.Apply(batchEntry(t, 9, "node", j, j))
	err, ok := result.(error)
	if !ok || !isRetryableApplyError(err) {
		t.Fatalf("Apply = %v, want a retryable error", result)
	}
}
