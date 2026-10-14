package raft

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type snapshotFaultStorage struct {
	*InMemoryStorage
	failKey string
	readErr error
	listErr error
}

func (s *snapshotFaultStorage) Get(key string) ([]byte, error) {
	if key == s.failKey {
		return nil, s.readErr
	}
	return s.InMemoryStorage.Get(key)
}
func (s *snapshotFaultStorage) List(prefix string) ([]string, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.InMemoryStorage.List(prefix)
}
func TestStorageFSMSnapshotReadFailure(t *testing.T) {
	readErr := errors.New("injected snapshot read failure")
	newStore := func() *snapshotFaultStorage {
		store := &snapshotFaultStorage{InMemoryStorage: NewInMemoryStorage(), readErr: readErr}
		for _, key := range []string{"ws/souls/a", "ws/souls/b"} {
			if err := store.Set(key, []byte(key)); err != nil {
				t.Fatal(err)
			}
		}
		return store
	}
	t.Run("reject partial image and retry after recovery", func(t *testing.T) {
		store := newStore()
		fsm := NewStorageFSM(store)
		store.failKey = "ws/souls/b"
		for i := 0; i < 2; i++ {
			snapshot, err := fsm.Snapshot()
			if !errors.Is(err, readErr) {
				t.Fatalf("expected storage read error, got %v", err)
			}
			if len(snapshot.Value) != 0 {
				t.Fatalf("partial snapshot returned: %s", snapshot.Value)
			}
		}
		store.failKey = ""
		snapshot, err := fsm.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		restored := NewInMemoryStorage()
		if err := NewStorageFSM(restored).Restore(snapshot.Value); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"ws/souls/a", "ws/souls/b"} {
			value, err := restored.Get(key)
			if err != nil || string(value) != key {
				t.Fatalf("restored %q = %q, %v", key, value, err)
			}
		}
	})
	t.Run("failed snapshot does not compact log", func(t *testing.T) {
		store := newStore()
		store.failKey = "ws/souls/b"
		logs := NewInMemoryLogStore()
		snapshots := NewInMemorySnapshotStore()
		cfg := newTestRaftNodeConfig()
		cfg.SnapshotThreshold = 1
		node, err := NewNode(cfg, logs, snapshots, NewStorageFSM(store), newTestRaftLogger())
		if err != nil {
			t.Fatal(err)
		}
		if err := node.appendEntry(core.RaftLogEntry{Term: 1, Type: core.LogNoOp}); err != nil {
			t.Fatal(err)
		}
		node.commitIndex, node.lastApplied = 1, 1
		node.maybeTakeSnapshot()
		metas, err := snapshots.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(metas) != 0 || node.logBase != 0 || node.lastSnapshotIndex != 0 {
			t.Fatalf("failed snapshot published/compacted: snapshots=%v base=%d snapshotIndex=%d", metas, node.logBase, node.lastSnapshotIndex)
		}
		var entry core.RaftLogEntry
		if err := logs.GetLog(1, &entry); err != nil || entry.Index != 1 {
			t.Fatalf("recovery log discarded: %+v, %v", entry, err)
		}
	})
	t.Run("empty application image and ignored raft metadata", func(t *testing.T) {
		for _, metadata := range []bool{false, true} {
			store := &snapshotFaultStorage{InMemoryStorage: NewInMemoryStorage(), readErr: readErr, failKey: "raft/hard_state"}
			if metadata {
				if err := store.Set("raft/hard_state", []byte("metadata")); err != nil {
					t.Fatal(err)
				}
			}
			snapshot, err := NewStorageFSM(store).Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			var image map[string][]byte
			if err := json.Unmarshal(snapshot.Value, &image); err != nil {
				t.Fatal(err)
			}
			if len(image) != 0 {
				t.Fatalf("expected empty application image, got %v", image)
			}
		}
	})
	t.Run("list failure propagates", func(t *testing.T) {
		store := newStore()
		store.listErr = errors.New("injected list failure")
		snapshot, err := NewStorageFSM(store).Snapshot()
		if !errors.Is(err, store.listErr) || len(snapshot.Value) != 0 {
			t.Fatalf("expected list failure without snapshot, got %s, %v", snapshot.Value, err)
		}
	})
}
