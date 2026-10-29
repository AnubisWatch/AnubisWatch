package raft

import (
	"encoding/json"
	"testing"
)

// durableMemStore stands in for CobaltDB: its state survives a restart.
type durableMemStore struct{ *InMemoryStorage }

func (durableMemStore) DurableState() bool { return true }

// On every restart a node rewrote its whole store from the latest snapshot
// and replayed the log after it. With a durable store that already applied
// past the snapshot that is wasted work -- minutes for a real dataset, during
// which the node could not rejoin. It is skipped; a node behind its snapshot
// (or with a non-durable store) still restores.
func TestRestartSkipsRedundantSnapshotRestore(t *testing.T) {
	for _, tc := range []struct {
		name        string
		durable     bool
		lastApplied uint64
		want        string
	}{
		{"durable and past the snapshot", true, 8, "live"},
		{"durable but behind the snapshot", true, 3, "snapshot"},
		{"not durable", false, 8, "snapshot"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mem := NewInMemoryStorage()
			_ = mem.Set("k", []byte("live"))
			var fsm *StorageFSM
			if tc.durable {
				fsm = NewStorageFSM(durableMemStore{mem})
			} else {
				fsm = NewStorageFSM(mem)
			}
			snapshots := NewInMemorySnapshotStore()
			payload, _ := json.Marshal(map[string][]byte{"k": []byte("snapshot")})
			sink, _ := snapshots.Create(1, 5, 1, nil)
			_, _ = sink.Write(payload)
			if err := sink.Close(); err != nil {
				t.Fatal(err)
			}
			n, err := NewNodeWithStableStore(newTestRaftNodeConfig(), NewInMemoryLogStore(), NewInMemoryStableStore(), snapshots, fsm, newTestRaftLogger())
			if err != nil {
				t.Fatal(err)
			}
			n.lastApplied, n.commitIndex = tc.lastApplied, tc.lastApplied
			if err := n.restoreLatestSnapshot(); err != nil {
				t.Fatal(err)
			}
			if got, _ := mem.Get("k"); string(got) != tc.want {
				t.Fatalf("store holds %q, want %q", got, tc.want)
			}
			if n.lastSnapshotIndex != 5 {
				t.Fatalf("lastSnapshotIndex = %d, want 5", n.lastSnapshotIndex)
			}
		})
	}
}
