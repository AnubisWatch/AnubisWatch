package raft

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// High-volume writes (judgments, incidents) are replicated as FSMBatch entries,
// some commands are applied by a per-table handler instead of the store, and
// the application observes every applied command to update in-memory state.

func batchEntry(t *testing.T, index uint64, origin string, cmds ...core.FSMCommand) *core.RaftLogEntry {
	t.Helper()
	value, err := json.Marshal(cmds)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(core.FSMCommand{Op: core.FSMBatch, Value: value, Origin: origin})
	if err != nil {
		t.Fatal(err)
	}
	return &core.RaftLogEntry{Index: index, Term: 1, Type: core.LogCommand, Data: data}
}

func TestFSMBatchAppliesInOrderAndSkipsFailedElements(t *testing.T) {
	store := NewInMemoryStorage()
	fsm := NewStorageFSM(store)

	var handled []string
	fsm.SetCommandHandler("judgments", func(cmd *core.FSMCommand) error {
		if cmd.Key == "bad" {
			return errors.New("undecodable judgment")
		}
		handled = append(handled, cmd.Key)
		return nil
	})
	var observed []core.FSMCommand
	fsm.SetApplyObserver(func(cmd core.FSMCommand) { observed = append(observed, cmd) })

	entry := batchEntry(t, 1, "node-a",
		core.FSMCommand{Op: core.FSMSet, Key: "default/souls/s1", Value: []byte("1")},
		core.FSMCommand{Op: core.FSMSet, Table: "judgments", Key: "j1", Value: []byte("{}")},
		core.FSMCommand{Op: core.FSMSet, Table: "judgments", Key: "bad"},
		core.FSMCommand{Op: core.FSMBatch, Key: "nested"},
		core.FSMCommand{Op: core.FSMDelete, Key: "default/souls/s1"},
	)
	if result := fsm.Apply(entry); result != nil {
		t.Fatalf("a batch with a bad element failed the whole entry: %v", result)
	}
	if fsm.LastApplied() != 1 {
		t.Fatalf("the batch entry was not recorded as applied (index %d)", fsm.LastApplied())
	}
	if _, err := store.Get("default/souls/s1"); err == nil {
		t.Fatal("batch elements were not applied in order: the delete ran before the set")
	}
	if len(handled) != 1 || handled[0] != "j1" {
		t.Fatalf("handler saw %v, want [j1]", handled)
	}
	if _, err := store.Get("j1"); err == nil {
		t.Fatal("a handled command was also written to the store")
	}

	keys := make([]string, 0, len(observed))
	for _, cmd := range observed {
		keys = append(keys, cmd.Key)
		if cmd.Origin != "node-a" {
			t.Fatalf("element %s lost the batch origin: %q", cmd.Key, cmd.Origin)
		}
	}
	if len(keys) != 3 || keys[0] != "default/souls/s1" || keys[1] != "j1" || keys[2] != "default/souls/s1" {
		t.Fatalf("observer saw %v; it must see exactly the applied elements, in order", keys)
	}
}

func TestFSMObservesSingleCommandsAndRestores(t *testing.T) {
	fsm := NewStorageFSM(NewInMemoryStorage())
	var observed []string
	restored := 0
	fsm.SetApplyObserver(func(cmd core.FSMCommand) { observed = append(observed, cmd.Key) })
	fsm.SetRestoreObserver(func() { restored++ })

	data, _ := json.Marshal(core.FSMCommand{Op: core.FSMSet, Key: "k", Value: []byte("v")})
	fsm.Apply(&core.RaftLogEntry{Index: 1, Term: 1, Type: core.LogCommand, Data: data})
	fsm.Apply(&core.RaftLogEntry{Index: 1, Term: 1, Type: core.LogCommand, Data: data}) // already applied
	if len(observed) != 1 || observed[0] != "k" {
		t.Fatalf("observer saw %v, want one k", observed)
	}

	snapshot, _ := json.Marshal(map[string][]byte{"k": []byte("v")})
	if err := fsm.Restore(snapshot); err != nil {
		t.Fatal(err)
	}
	if restored != 1 {
		t.Fatalf("restore observer ran %d times, want 1", restored)
	}
}
