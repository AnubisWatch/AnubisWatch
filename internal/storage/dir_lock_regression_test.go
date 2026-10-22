package storage

import (
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// A second engine on a live data directory used to rewrite wal.log during
// recovery, orphaning the WAL file the first engine kept appending to: every
// later write by the running server vanished on restart.
func TestNewEngineRefusesDataDirAlreadyOpen(t *testing.T) {
	dir := t.TempDir()
	first, err := NewEngine(core.StorageConfig{Path: dir}, quietLogger())
	if err != nil {
		t.Fatalf("first NewEngine: %v", err)
	}

	second, err := NewEngine(core.StorageConfig{Path: dir}, quietLogger())
	if !errors.Is(err, ErrDataDirLocked) {
		if second != nil {
			_ = second.Close()
		}
		t.Fatalf("second NewEngine err = %v, want ErrDataDirLocked", err)
	}

	if err := first.Put("k", []byte("after-refused-open")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := NewEngine(core.StorageConfig{Path: dir}, quietLogger())
	if err != nil {
		t.Fatalf("reopen after Close: %v", err)
	}
	defer reopened.Close()
	got, err := reopened.Get("k")
	if err != nil || string(got) != "after-refused-open" {
		t.Fatalf("Get after reopen = %q, %v", got, err)
	}
}

func TestNewEngineFailureReleasesDataDirLock(t *testing.T) {
	dir := t.TempDir()
	_, err := NewEngine(core.StorageConfig{
		Path:       dir,
		Encryption: core.EncryptionConfig{Enabled: true},
	}, quietLogger())
	if err == nil {
		t.Fatal("NewEngine with empty encryption key succeeded")
	}

	db, err := NewEngine(core.StorageConfig{Path: dir}, quietLogger())
	if err != nil {
		t.Fatalf("NewEngine after failed open: %v", err)
	}
	_ = db.Close()
}
