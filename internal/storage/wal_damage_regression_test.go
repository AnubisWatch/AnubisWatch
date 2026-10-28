package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func openTestEngine(t *testing.T, dir string) *CobaltDB {
	t.Helper()
	db, err := NewEngine(core.StorageConfig{Path: dir}, quietLogger())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return db
}

func mustGet(t *testing.T, db *CobaltDB, key, want string) {
	t.Helper()
	got, err := db.Get(key)
	if err != nil || string(got) != want {
		t.Fatalf("Get(%q) = %q, %v; want %q", key, got, err, want)
	}
}

// A write that failed halfway (disk full) used to leave a partial entry in
// the WAL. Recovery treats a partial entry as the end of the log, so every
// write acknowledged after it was lost on restart.
func TestWALPartialWriteDoesNotHideLaterWrites(t *testing.T) {
	dir := t.TempDir()
	db := openTestEngine(t, dir)
	if err := db.Put("a", []byte("1")); err != nil {
		t.Fatalf("Put a: %v", err)
	}

	orig := walWriteSeam
	walWriteSeam = func(f *os.File, b []byte) (int, error) {
		n, _ := f.Write(b[:len(b)/2])
		return n, syscall.ENOSPC
	}
	if err := db.Put("b", []byte("2")); err == nil {
		t.Fatal("Put b succeeded despite injected short write")
	}
	walWriteSeam = orig

	if err := db.Put("c", []byte("3")); err != nil {
		t.Fatalf("Put c: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	db = openTestEngine(t, dir)
	defer db.Close()
	mustGet(t, db, "a", "1")
	mustGet(t, db, "c", "3")
	if _, err := db.Get("b"); err == nil {
		t.Fatal("failed write b is visible after restart")
	}
}

// After a failed fsync the WAL cannot be trusted; later writes must fail
// rather than be acknowledged.
func TestWALRefusesWritesAfterFailedSync(t *testing.T) {
	db := openTestEngine(t, t.TempDir())
	defer db.Close()

	orig := walSyncSeam
	walSyncSeam = func(*os.File) error { return syscall.EIO }
	err := db.Put("a", []byte("1"))
	walSyncSeam = orig
	if !errors.Is(err, syscall.EIO) {
		t.Fatalf("Put with failing fsync err = %v, want EIO", err)
	}
	if err := db.Put("b", []byte("2")); err == nil {
		t.Fatal("Put after failed fsync succeeded")
	}
}

// One flipped byte in the middle of the WAL used to end replay there; the
// recovery rewrite then dropped every later entry permanently. Silently
// skipping it instead could resurrect deleted keys, so startup now refuses
// unless the operator opts in to skipping.
func TestWALRecoverySkipsMidLogCorruption(t *testing.T) {
	dir := t.TempDir()
	db := openTestEngine(t, dir)
	for _, k := range []string{"k0", "k1", "k2", "k3", "k4"} {
		if err := db.Put(k, []byte("v-"+k)); err != nil {
			t.Fatalf("Put %s: %v", k, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	walPath := filepath.Join(dir, "wal.log")
	data, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt the length prefix of the entry holding k2.
	at := strings.Index(string(data), `"key":"k2"`)
	start := strings.LastIndex(string(data[:at]), "{") - 4
	data[start] = 0xff
	if err := os.WriteFile(walPath, data, 0600); err != nil {
		t.Fatal(err)
	}

	// By default mid-log damage stops startup and leaves the WAL untouched.
	if _, err := NewEngine(core.StorageConfig{Path: dir}, quietLogger()); err == nil ||
		!strings.Contains(err.Error(), WALSkipCorruptEnv) {
		t.Fatalf("NewEngine on mid-log damage err = %v, want refusal naming %s", err, WALSkipCorruptEnv)
	}
	if after, _ := os.ReadFile(walPath); string(after) != string(data) {
		t.Fatal("refused recovery modified the damaged WAL")
	}

	// The operator opt-in skips the damaged entry and keeps a copy.
	t.Setenv(WALSkipCorruptEnv, "1")
	db = openTestEngine(t, dir)
	if got := db.WALCorruptEntriesSkipped(); got != 1 {
		t.Fatalf("WALCorruptEntriesSkipped = %d, want 1", got)
	}
	for _, k := range []string{"k0", "k1", "k3", "k4"} {
		mustGet(t, db, k, "v-"+k)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	kept, _ := filepath.Glob(walPath + ".corrupt-*")
	if len(kept) != 1 {
		t.Fatalf("damaged WAL copies = %v, want exactly one", kept)
	}

	// The recovered state must also survive a second restart.
	db = openTestEngine(t, dir)
	defer db.Close()
	mustGet(t, db, "k4", "v-k4")
}

// A torn tail after a crash is routine and must not leave copies behind.
func TestWALTornTailKeepsNoCorruptCopy(t *testing.T) {
	dir := t.TempDir()
	db := openTestEngine(t, dir)
	if err := db.Put("a", []byte("1")); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	walPath := filepath.Join(dir, "wal.log")
	f, err := os.OpenFile(walPath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte{0, 0, 0, 40, '{', '"', 'o'})
	_ = f.Close()

	db = openTestEngine(t, dir)
	defer db.Close()
	mustGet(t, db, "a", "1")
	if kept, _ := filepath.Glob(walPath + ".corrupt-*"); len(kept) != 0 {
		t.Fatalf("torn tail produced damaged-WAL copies %v", kept)
	}
}

// A damaged DELETE in the middle of the log used to be skipped, bringing the
// deleted key back to life on restart. Recovery must refuse instead.
func TestWALMidLogCorruptDeleteDoesNotResurrectKey(t *testing.T) {
	dir := t.TempDir()
	db := openTestEngine(t, dir)
	if err := db.Put("secret", []byte("v")); err != nil {
		t.Fatal(err)
	}
	if err := db.Delete("secret"); err != nil {
		t.Fatal(err)
	}
	if err := db.Put("later", []byte("x")); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	walPath := filepath.Join(dir, "wal.log")
	data, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatal(err)
	}
	at := strings.Index(string(data), `"op":"DELETE"`)
	if at < 0 {
		t.Fatal("DELETE entry not found")
	}
	data[at+len(`"op":"DEL`)] = 'X' // still valid JSON, checksum no longer matches
	if err := os.WriteFile(walPath, data, 0600); err != nil {
		t.Fatal(err)
	}

	db, err = NewEngine(core.StorageConfig{Path: dir}, quietLogger())
	if err == nil {
		defer db.Close()
		if _, gerr := db.Get("secret"); gerr == nil {
			t.Fatal("deleted key resurrected after a corrupt DELETE was skipped")
		}
		t.Fatal("recovery accepted a WAL with a corrupt mid-log entry")
	}
}

// A damaged final entry is a torn tail: it is dropped and startup continues.
func TestWALChecksumMismatchAtTailIsTornTail(t *testing.T) {
	dir := t.TempDir()
	db := openTestEngine(t, dir)
	for _, k := range []string{"a", "b"} {
		if err := db.Put(k, []byte("v-"+k)); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()
	walPath := filepath.Join(dir, "wal.log")
	data, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatal(err)
	}
	at := strings.LastIndex(string(data), `"key":"b"`)
	data[at+len(`"key":"`)] = 'c'
	if err := os.WriteFile(walPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	db = openTestEngine(t, dir)
	defer db.Close()
	mustGet(t, db, "a", "v-a")
	if _, err := db.Get("b"); err == nil {
		t.Fatal("corrupt tail entry was applied")
	}
}
