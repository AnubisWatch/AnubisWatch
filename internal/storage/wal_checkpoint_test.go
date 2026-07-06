package storage

import (
	"fmt"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TestWALCheckpointCompactsAndPreserves verifies that Checkpoint compacts the
// WAL (dropping superseded writes and tombstones) while preserving live data
// across a subsequent reopen that recovers from the compacted WAL.
func TestWALCheckpointCompactsAndPreserves(t *testing.T) {
	dir := t.TempDir()
	cfg := core.StorageConfig{Path: dir}

	db, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	const keys = 200
	// Overwrite each key 10 times so the WAL holds ~2000 PUTs for 200 live keys.
	for round := 0; round < 10; round++ {
		for i := 0; i < keys; i++ {
			k := fmt.Sprintf("k-%04d", i)
			if err := db.Put(k, []byte(fmt.Sprintf("v-%d-%d", i, round))); err != nil {
				t.Fatalf("put: %v", err)
			}
		}
	}
	// Delete the first 50 keys (tombstones that compaction should drop).
	for i := 0; i < 50; i++ {
		if err := db.Delete(fmt.Sprintf("k-%04d", i)); err != nil {
			t.Fatalf("delete: %v", err)
		}
	}

	before := db.wal.currentSize()
	if err := db.Checkpoint(); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	after := db.wal.currentSize()
	if after >= before {
		t.Fatalf("checkpoint did not shrink WAL: before=%d after=%d", before, after)
	}

	assertState := func(when string, d *CobaltDB) {
		for i := 0; i < keys; i++ {
			k := fmt.Sprintf("k-%04d", i)
			got, err := d.Get(k)
			if i < 50 {
				if _, ok := err.(*core.NotFoundError); !ok {
					t.Fatalf("%s: deleted key %s should be NotFound, got val=%q err=%v", when, k, got, err)
				}
				continue
			}
			want := fmt.Sprintf("v-%d-9", i)
			if err != nil || string(got) != want {
				t.Fatalf("%s: key %s = %q err=%v, want %q", when, k, got, err, want)
			}
		}
	}

	assertState("after checkpoint", db)
	db.Close()

	// Reopen: recovery must rebuild identical state from the compacted WAL.
	db2, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()
	assertState("after reopen", db2)
}
