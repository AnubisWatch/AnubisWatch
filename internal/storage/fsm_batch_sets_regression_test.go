package storage

import (
	"encoding/json"
	"os"
	"sync/atomic"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/raft"
)

// A replicated batch (judgments, alert state) applied each element with its
// own WAL fsync, so a follower catching up after a restart drained its
// backlog at a few entries per second. Consecutive sets are now one frame.
func TestFSMBatchOfSetsIsOneWALWriteWithSequentialResult(t *testing.T) {
	db, err := NewEngine(core.StorageConfig{Path: t.TempDir()}, newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Put("default/souls/gone", []byte(`{"id":"gone"}`)); err != nil {
		t.Fatal(err)
	}

	cmds := []core.FSMCommand{
		{Op: core.FSMSet, Key: "default/souls/a", Value: []byte(`{"id":"a","name":"first"}`)},
		{Op: core.FSMSet, Key: "default/souls/b", Value: []byte(`{"id":"b"}`)},
		{Op: core.FSMSet, Key: "default/souls/a", Value: []byte(`{"id":"a","name":"second"}`)},
		{Op: core.FSMDelete, Key: "default/souls/gone"},
		{Op: core.FSMSet, Key: "default/souls/gone", Value: []byte(`{"id":"gone","name":"back"}`)},
		{Op: core.FSMSet, Key: "default/souls/c", Value: []byte(`{"id":"c"}`)},
	}
	value, _ := json.Marshal(cmds)
	data, _ := json.Marshal(core.FSMCommand{Op: core.FSMBatch, Value: value})

	var syncs atomic.Int32
	old := walSyncSeam
	walSyncSeam = func(f *os.File) error { syncs.Add(1); return old(f) }
	defer func() { walSyncSeam = old }()

	fsm := raft.NewStorageFSM(db)
	if result := fsm.Apply(&core.RaftLogEntry{Index: 1, Term: 1, Type: core.LogCommand, Data: data}); result != nil {
		t.Fatalf("Apply = %v", result)
	}
	// Three sets, the delete, then two sets: three writes instead of six.
	if got := syncs.Load(); got != 3 {
		t.Fatalf("WAL fsyncs = %d, want 3 (one per run of sets plus the delete)", got)
	}

	for key, want := range map[string]string{
		"default/souls/a":    `{"id":"a","name":"second"}`,
		"default/souls/b":    `{"id":"b"}`,
		"default/souls/gone": `{"id":"gone","name":"back"}`,
		"default/souls/c":    `{"id":"c"}`,
	} {
		if got, err := db.Get(key); err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v; want %q", key, got, err, want)
		}
	}
	// Secondary indexes saw the batched sets.
	souls, err := db.ListSoulsNoCtx("default", 0, 10)
	if err != nil || len(souls) != 4 {
		t.Fatalf("ListSouls = %d souls, %v; want 4", len(souls), err)
	}
}
