package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// TestWALChecksum verifies that each WAL entry carries a CRC32 checksum and
// that a corrupted entry is detected and treated as a torn tail (discarding
// only the corrupt entry, not preceding good data).
func TestWALChecksum(t *testing.T) {
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

	// Verify entries have checksums by reading the WAL raw
	walPath := filepath.Join(dir, "wal.log")
	raw, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatalf("read WAL: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("WAL is empty")
	}
	// Check that the JSON contains base64-encoded value for "v-b".
	// json.Marshal encodes []byte as base64. "v-b" base64 is "di12".
	// Presence of "crc32" in the JSON verifies checksums are written.
	if !strings.Contains(string(raw), `"crc32":`) {
		t.Error("no WAL entry contains a crc32 field — checksums may not be written")
	}

	// Record the original content of entry "b" for comparison.
	// We'll corrupt the first byte of the length prefix of entry "b"
	// so it points past the end of file (length = 999999 = 0x0F423F).
	// Entries are length-prefixed: [4-byte BE length][JSON body].
	// We skip the first entry's length+body by reading its length,
	// then corrupt the next 4 bytes (entry "b"'s length prefix).
	if len(raw) < 8 {
		t.Fatal("WAL too short")
	}
	firstLen := int(raw[0])<<24 | int(raw[1])<<16 | int(raw[2])<<8 | int(raw[3])
	secondLenOff := 4 + firstLen // start of entry "b"'s length prefix
	if secondLenOff+4 > len(raw) {
		t.Fatalf("WAL too short for second entry: need %d, have %d", secondLenOff+4, len(raw))
	}
	// Verify this looks like a valid length
	secondLen := int(raw[secondLenOff])<<24 | int(raw[secondLenOff+1])<<16 |
		int(raw[secondLenOff+2])<<8 | int(raw[secondLenOff+3])
	if secondLen <= 0 || secondLen > 1024*1024 {
		t.Fatalf("unexpected second entry length %d — test setup broken", secondLen)
	}

	// Corrupt the second entry's body by flipping a byte inside it
	corruptionOff := secondLenOff + 4 + secondLen/2
	if corruptionOff >= len(raw) {
		t.Fatalf("corruption offset %d beyond end of WAL (%d)", corruptionOff, len(raw))
	}
	corrupted := make([]byte, len(raw))
	copy(corrupted, raw)
	corrupted[corruptionOff] ^= 0xFF // flip all bits in one byte

	if err := os.WriteFile(walPath, corrupted, 0600); err != nil {
		t.Fatalf("write corrupted WAL: %v", err)
	}

	// Reopen: recovery must detect the corrupted entry, warn, and skip it.
	// Entries "a" and "c" should survive; "b" should be lost.
	db2, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("reopen after corrupted WAL: %v", err)
	}
	defer db2.Close()

	if got, err := db2.Get("a"); err != nil {
		t.Errorf("key a lost after checksum recovery: %v", err)
	} else if string(got) != "v-a" {
		t.Errorf("key a = %q, want v-a", got)
	}

	if _, err := db2.Get("b"); err == nil {
		t.Error("key b should be missing after corruption recovery")
	}

	if got, err := db2.Get("c"); err != nil {
		t.Errorf("key c lost after checksum recovery: %v", err)
	} else if string(got) != "v-c" {
		t.Errorf("key c = %q, want v-c", got)
	}
}

// TestWALChecksumLegacy verifies that WAL entries without a crc32 field
// (legacy format) are accepted during recovery.
func TestWALChecksumLegacy(t *testing.T) {
	dir := t.TempDir()
	cfg := core.StorageConfig{Path: dir}

	db, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	// Write one entry, then manually write a legacy-style entry (no crc32)
	// directly to the WAL file.
	if err := db.Put("modern", []byte("modern-value")); err != nil {
		t.Fatalf("put: %v", err)
	}
	db.Close()

	// Append a legacy entry (no crc32 field) to the WAL
	walPath := filepath.Join(dir, "wal.log")
	f, err := os.OpenFile(walPath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("open WAL: %v", err)
	}
	legacy := walEntry{Op: "PUT", Key: "legacy", Value: []byte("legacy-value"), Time: 100}
	// Marshal manually via writeEntryLocked for the correct format. Since
	// writeEntryLocked now adds crc32, we need to write the raw JSON without
	// it. We marshal the entry, then remove the crc32 field, then write.
	data, _ := json.Marshal(legacy)
	// Remove crc32 if present (shouldn't be here since Crc32=0 and omitempty)
	length := []byte{
		byte(len(data) >> 24 & 0xff),
		byte(len(data) >> 16 & 0xff),
		byte(len(data) >> 8 & 0xff),
		byte(len(data) & 0xff),
	}
	if _, err := f.Write(length); err != nil {
		t.Fatalf("write legacy length: %v", err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatalf("write legacy data: %v", err)
	}
	f.Close()

	// Reopen: both modern and legacy entries should be recovered
	db2, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("reopen with legacy WAL: %v", err)
	}
	defer db2.Close()

	if got, err := db2.Get("modern"); err != nil {
		t.Errorf("modern key lost: %v", err)
	} else if string(got) != "modern-value" {
		t.Errorf("modern key = %q, want modern-value", got)
	}

	if got, err := db2.Get("legacy"); err != nil {
		t.Errorf("legacy key lost: %v", err)
	} else if string(got) != "legacy-value" {
		t.Errorf("legacy key = %q, want legacy-value", got)
	}
}

// TestWALChecksumCorruptLength verifies that bitrot in the length prefix is
// caught by the existing length-range check.
func TestWALChecksumCorruptLength(t *testing.T) {
	dir := t.TempDir()
	cfg := core.StorageConfig{Path: dir}

	db, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if err := db.Put("survivor", []byte("alive")); err != nil {
		t.Fatalf("put: %v", err)
	}
	db.Close()

	// Corrupt the length prefix of the first entry so it claims 999999 bytes
	walPath := filepath.Join(dir, "wal.log")
	raw, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatalf("read WAL: %v", err)
	}
	if len(raw) < 8 {
		t.Fatal("WAL too short")
	}
	// Overwrite first length prefix with a huge value
	raw[1] = 0x0F
	raw[2] = 0x42
	raw[3] = 0x3F
	if err := os.WriteFile(walPath, raw, 0600); err != nil {
		t.Fatalf("write corrupted WAL: %v", err)
	}

	// Recovery should still succeed (entries after the bad length prefix
	// are discarded as torn tail, but the entries before it... actually
	// the first entry IS the corrupted one, so the whole WAL is discarded).
	// The engine starts fresh.
	db2, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("reopen after corrupt length: %v", err)
	}
	defer db2.Close()
}

// containsJSONKey reports whether any JSON value in raw has the given key.
// This is a simple byte search, not a JSON parser — sufficient for testing.
func containsJSONKey(raw []byte, key string) bool {
	quoted := `"` + key + `"`
	return len(raw) > 0 && strings.Contains(string(raw), quoted)
}

// replaceFirst replaces the first occurrence of old with new in src.
func replaceFirst(src []byte, old, new string) []byte {
	s := string(src)
	idx := strings.Index(s, old)
	if idx < 0 {
		return src
	}
	return []byte(s[:idx] + new + s[idx+len(old):])
}
