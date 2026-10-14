package raft

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestNodeSnapshotRestartReplaysCommittedSuffix(t *testing.T) {
	tests := []struct {
		name        string
		tail        int
		uncommitted bool
		reuseFSM    bool
	}{
		{name: "snapshot boundary"},
		{name: "one committed update", tail: 1},
		{name: "multiple committed updates", tail: 3},
		{name: "uncommitted update excluded", tail: 2, uncommitted: true},
		{name: "existing FSM applied index reset", tail: 2, reuseFSM: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newTestRaftNodeConfig()
			cfg.ElectionTimeout.Duration = time.Hour
			cfg.HeartbeatTimeout.Duration = time.Minute
			cfg.CommitTimeout.Duration = time.Minute
			cfg.SnapshotThreshold = 1
			logs := NewInMemoryLogStore()
			stable := NewInMemoryStableStore()
			snapshots := NewInMemorySnapshotStore()
			store := NewInMemoryStorage()
			fsm := NewStorageFSM(store)
			node, err := NewNodeWithStableStore(cfg, logs, stable, snapshots, fsm, newTestRaftLogger())
			if err != nil {
				t.Fatal(err)
			}
			appendCommand := func(value string, commit bool) {
				data, err := json.Marshal(core.FSMCommand{Op: core.FSMSet, Key: "ws/souls/value", Value: []byte(value)})
				if err != nil {
					t.Fatal(err)
				}
				if err := node.appendEntry(core.RaftLogEntry{Term: 1, Type: core.LogCommand, Data: data}); err != nil {
					t.Fatal(err)
				}
				if commit {
					node.commitIndex = node.lastLogIndexLocked()
					node.processCommitted(node.commitIndex)
				}
			}
			appendCommand("snapshot", true)
			node.maybeTakeSnapshot()
			for i := 1; i <= tt.tail; i++ {
				appendCommand(fmt.Sprintf("committed-%d", i), true)
			}
			if tt.uncommitted {
				appendCommand("uncommitted", false)
			}
			wantValue := "snapshot"
			if tt.tail > 0 {
				wantValue = fmt.Sprintf("committed-%d", tt.tail)
			}
			wantIndex := uint64(1 + tt.tail)
			// Exercise two consecutive recoveries over the same durable stores.
			for restart := 0; restart < 2; restart++ {
				if !tt.reuseFSM {
					fsm = NewStorageFSM(store)
				}
				restored, err := NewNodeWithStableStore(cfg, logs, stable, snapshots, fsm, newTestRaftLogger())
				if err != nil {
					t.Fatal(err)
				}
				if err := restored.Start(); err != nil {
					t.Fatal(err)
				}
				if err := restored.Stop(); err != nil {
					t.Fatal(err)
				}
				value, err := store.Get("ws/souls/value")
				if err != nil || string(value) != wantValue {
					t.Fatalf("restart %d: expected %q, got %q, %v", restart, wantValue, value, err)
				}
				if restored.lastApplied != wantIndex || restored.commitIndex != wantIndex || fsm.LastApplied() != wantIndex {
					t.Fatalf("restart %d: expected applied/commit/FSM indexes %d; got %d/%d/%d", restart, wantIndex, restored.lastApplied, restored.commitIndex, fsm.LastApplied())
				}
			}
		})
	}
}
