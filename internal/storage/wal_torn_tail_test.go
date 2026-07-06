package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TestWALTornTailRecovery verifies that a crash leaving a torn/partial tail
// entry in the WAL does not abort recovery of the entries written before it.
// Previously recoverFromWAL returned an error on the first malformed entry,
// which discarded every preceding (successfully written) entry.
func TestWALTornTailRecovery(t *testing.T) {
	dir := t.TempDir()
	cfg := core.StorageConfig{Path: dir}

	db, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	for _, k := range []string{"a", "b", "c"} {
		if err := db.Put(k, []byte("v-"+k)); err != nil {
			t.Fatalf("put %s: %v", k, err)
		}
	}
	db.Close()

	// Simulate a crash mid-write: append a length prefix that promises far
	// more bytes than actually follow (a torn tail).
	walPath := filepath.Join(dir, "wal.log")
	f, err := os.OpenFile(walPath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("open wal: %v", err)
	}
	// 4-byte big-endian length claiming 4096 bytes, followed by only 3 bytes.
	if _, err := f.Write([]byte{0x00, 0x00, 0x10, 0x00, 'x', 'y', 'z'}); err != nil {
		t.Fatalf("write torn tail: %v", err)
	}
	f.Close()

	// Reopen: recovery must succeed and preserve a, b, c.
	db2, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("reopen after torn tail: %v", err)
	}
	defer db2.Close()

	for _, k := range []string{"a", "b", "c"} {
		got, err := db2.Get(k)
		if err != nil {
			t.Fatalf("key %s lost after torn-tail recovery: %v", k, err)
		}
		if string(got) != "v-"+k {
			t.Fatalf("key %s = %q, want v-%s", k, got, k)
		}
	}
}
