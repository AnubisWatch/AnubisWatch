package storage

import (
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
)

func auditAliasCase(t *testing.T, encrypted bool, mode string) {
	t.Helper()
	cfg := core.StorageConfig{Path: t.TempDir()}
	cfg.Encryption.Enabled = encrypted
	cfg.Encryption.Key = "test-only-key-0123456789"
	db, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	input := []byte("original")
	completed := make(chan error, 1)
	go func() { completed <- db.Put("default/a", input) }()
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	// Mutation happens strictly after Put returns; no simultaneous buffer access.
	switch mode {
	case "put":
		input[0] = 'X'
	case "get":
		v, err := db.Get("default/a")
		if err != nil {
			t.Fatal(err)
		}
		v[0] = 'X'
	case "prefix":
		v, err := db.PrefixScan("default/")
		if err != nil {
			t.Fatal(err)
		}
		v["default/a"][0] = 'X'
	case "range":
		v, err := db.RangeScan("default/", "default0")
		if err != nil {
			t.Fatal(err)
		}
		v["default/a"][0] = 'X'
	}
	got, err := db.Get("default/a")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("encrypted=%v mode=%s EXPECTED: original ACTUAL: %s", encrypted, mode, got)
	if string(got) != "original" {
		t.Fatal("PROBLEM CONFIRMED")
	}
	for _, checkpoint := range []bool{false, true} {
		if checkpoint {
			if err := db.Checkpoint(); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		db, err = NewEngine(cfg, newTestLogger())
		if err != nil {
			t.Fatal(err)
		}
		got, err = db.Get("default/a")
		if err != nil || string(got) != "original" {
			t.Fatalf("replay: got=%s err=%v", got, err)
		}
	}
	if err := db.Put("default/empty", []byte{}); err != nil {
		t.Fatal(err)
	}
	if v, err := db.Get("default/empty"); err != nil || len(v) != 0 {
		t.Fatalf("empty: %v %v", v, err)
	}
	if err := db.Delete("default/empty"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Get("default/empty"); err == nil {
		t.Fatal("deleted value still readable")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestAuditStorageAlias(t *testing.T) {
	t.Run("encrypted-control", func(t *testing.T) { auditAliasCase(t, true, "put") })
	for _, mode := range []string{"put", "get", "prefix", "range"} {
		t.Run(mode, func(t *testing.T) { auditAliasCase(t, false, mode) })
	}
	if !t.Failed() {
		t.Log("FIX VERIFIED")
	}
}
