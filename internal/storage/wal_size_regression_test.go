package storage

import (
	"bytes"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
)

func auditWALWriteBound(t *testing.T, size int, encrypt, reject bool) {
	t.Helper()
	cfg := core.StorageConfig{Path: t.TempDir(), Encryption: core.EncryptionConfig{Enabled: encrypt, Key: "local-fixture-key"}}
	db, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if db != nil {
			db.Close()
		}
	}()
	if err := db.Put("before", []byte("before")); err != nil {
		t.Fatal(err)
	}
	beforeSize := db.wal.currentSize()
	value := bytes.Repeat([]byte("v"), size)
	putErr := db.Put("candidate", value)
	afterSize := db.wal.currentSize()
	if err := db.Put("after", []byte("after")); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	_, tailErr := db.Get("after")
	t.Logf("bytes=%d encrypt=%v EXPECTED: rejected=%v tail preserved=true ACTUAL: rejected=%v tail preserved=%v", size, encrypt, reject, putErr != nil, tailErr == nil)
	if (putErr != nil) != reject || tailErr != nil {
		t.Fatal("regression mismatch")
	}
	if reject && beforeSize != afterSize {
		t.Fatal("rejected write appended WAL bytes")
	}
	for restart := 0; restart < 2; restart++ {
		if got, err := db.Get("before"); err != nil || string(got) != "before" {
			t.Fatal("preceding record lost")
		}
		got, err := db.Get("candidate")
		if reject {
			if err == nil {
				t.Fatal("rejected value persisted")
			}
		} else if err != nil || !bytes.Equal(got, value) {
			t.Fatal("accepted value lost")
		}
		if _, err := db.Get("after"); err != nil {
			t.Fatal("following record lost")
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		db, err = NewEngine(cfg, newTestLogger())
		if err != nil {
			t.Fatal(err)
		}
	}
}
func TestAuditWALWriteRecoveryBound(t *testing.T) {
	t.Run("small-control", func(t *testing.T) { auditWALWriteBound(t, 32, false, false) })
	t.Run("oversized", func(t *testing.T) { auditWALWriteBound(t, 1024*1024, false, true) })
	t.Run("near-bound-control", func(t *testing.T) { auditWALWriteBound(t, 700*1024, false, false) })
	t.Run("encrypted-oversized", func(t *testing.T) { auditWALWriteBound(t, 1024*1024, true, true) })
	t.Run("empty-control", func(t *testing.T) { auditWALWriteBound(t, 0, false, false) })
}
