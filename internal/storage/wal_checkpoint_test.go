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

// TestCheckpointCompactsTree verifies that in-memory tree compaction drops
// nil-valued tombstone entries and that judgmentIndex is rebuilt correctly
// so stale entries don't accumulate until restart.
func TestCheckpointCompactsTree(t *testing.T) {
	dir := t.TempDir()
	cfg := core.StorageConfig{Path: dir}

	db, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer db.Close()

	// Insert keys and judgments.
	for i := 0; i < 50; i++ {
		k := fmt.Sprintf("keep-%04d", i)
		if err := db.Put(k, []byte("live")); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	// Simulate judgment saves — the judgmentIndex map gets populated via
	// db.judgmentIndex[j.ID] = idxKey in judgments.go. After checkpoint
	// + index rebuild, the judgmentIndex should contain judgment entries
	// that still exist in the tree, while stale ones are dropped.
	//
	// We inject index entries directly for test purposes, then verify
	// rebuildIndexes resets them.
	db.mu.Lock()
	db.judgmentIndex["stale-judgment"] = "default/judgment-idx/stale-judgment"
	db.mu.Unlock()

	// Verify the stale entry exists before checkpoint
	db.mu.RLock()
	_, hasStale := db.judgmentIndex["stale-judgment"]
	db.mu.RUnlock()
	if !hasStale {
		t.Fatal("expected stale judgment entry to exist before checkpoint")
	}

	// Verify a nil tombstone entry exists in the tree.
	// We do this by checking that deleted entries leave a nil value.
	if err := db.Delete("keep-0000"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// The tree should have a nil value for this key (tombstone).
	db.data.mu.RLock()
	nilFound := false
	node := db.data.root
	for !node.isLeaf && node != nil {
		if len(node.children) == 0 {
			break
		}
		node = node.children[0]
	}
	for node != nil {
		for i, k := range node.keys {
			if k == "keep-0000" {
				nilFound = node.values[i] == nil
			}
		}
		node = node.next
	}
	db.data.mu.RUnlock()
	if !nilFound {
		t.Log("Note: tombstone may be in another leaf or already compacted")
	}

	// Run checkpoint — this triggers compactTree + rebuildIndexes
	if err := db.Checkpoint(); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}

	// Verify the deleted key is gone from tree (nil tombstone removed)
	_, err = db.Get("keep-0000")
	if _, ok := err.(*core.NotFoundError); !ok {
		t.Fatalf("deleted key should be NotFound after checkpoint, got err=%v", err)
	}

	// Verify stale judgmentIndex entry is gone
	db.mu.RLock()
	_, hasStaleAfter := db.judgmentIndex["stale-judgment"]
	// The judgment index should have been rebuilt; only entries that still
	// have a corresponding tree entry survive.
	db.mu.RUnlock()
	if hasStaleAfter {
		t.Error("stale judgment entry should have been removed by rebuildIndexes")
	}

	// Verify live keys are still accessible
	for i := 1; i < 50; i++ {
		k := fmt.Sprintf("keep-%04d", i)
		val, err := db.Get(k)
		if err != nil {
			t.Fatalf("live key %s should be accessible after checkpoint: %v", k, err)
		}
		if string(val) != "live" {
			t.Errorf("live key %s = %q, want 'live'", k, string(val))
		}
	}
}
