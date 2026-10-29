package storage

import (
	"fmt"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Checkpoint used to hold writeMu while it rewrote and fsynced the whole
// live dataset, stalling every write (judgments included) for the duration.
// Writes made while the snapshot is written must proceed and must survive
// the compacted log, including deletes of keys that are in the snapshot.
func TestCheckpointDoesNotBlockWritesAndKeepsThem(t *testing.T) {
	dir := t.TempDir()
	cfg := core.StorageConfig{Path: dir}
	db, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		for round := 0; round < 3; round++ {
			if err := db.Put(fmt.Sprintf("k-%03d", i), []byte(fmt.Sprintf("v%d-%d", i, round))); err != nil {
				t.Fatal(err)
			}
		}
	}

	old := checkpointSnapshotSeam
	t.Cleanup(func() { checkpointSnapshotSeam = old })
	checkpointSnapshotSeam = func() {
		// writeMu is free here; with the old locking these calls deadlock.
		for i := 0; i < 20; i++ {
			if err := db.Put(fmt.Sprintf("during-%03d", i), []byte("new")); err != nil {
				t.Errorf("put during checkpoint: %v", err)
			}
		}
		if err := db.Put("k-000", []byte("overwritten-during")); err != nil {
			t.Errorf("overwrite during checkpoint: %v", err)
		}
		if err := db.Delete("k-001"); err != nil {
			t.Errorf("delete during checkpoint: %v", err)
		}
	}
	if err := db.Checkpoint(); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	checkpointSnapshotSeam = old

	check := func(db *CobaltDB, when string) {
		t.Helper()
		if v, err := db.Get("k-000"); err != nil || string(v) != "overwritten-during" {
			t.Fatalf("%s: k-000 = %q, %v; want the write made during the checkpoint", when, v, err)
		}
		if v, err := db.Get("k-001"); err == nil && v != nil {
			t.Fatalf("%s: k-001 = %q; want deleted during the checkpoint", when, v)
		}
		if v, err := db.Get("during-019"); err != nil || string(v) != "new" {
			t.Fatalf("%s: during-019 = %q, %v", when, v, err)
		}
		if v, err := db.Get("k-099"); err != nil || string(v) != "v99-2" {
			t.Fatalf("%s: k-099 = %q, %v", when, v, err)
		}
	}
	check(db, "before reopen")
	if err := db.Put("after", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close()
	check(db, "after reopen")
	if v, err := db.Get("after"); err != nil || string(v) != "x" {
		t.Fatalf("write after the checkpoint lost: %q, %v", v, err)
	}
}
