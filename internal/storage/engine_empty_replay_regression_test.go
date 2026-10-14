package storage

import (
	"bytes"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
)

func TestAuditEmptyValueDurability(t *testing.T) {
	cases := []struct {
		name                string
		value               []byte
		checkpoint, encrypt bool
		present             bool
	}{
		{"control-nonempty", []byte("value"), false, false, true},
		{"empty", []byte{}, false, false, true},
		{"empty-checkpoint", []byte{}, true, false, true}, {"nil-tombstone-control", nil, false, false, false}, {"encrypted-empty-control", []byte{}, false, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := core.StorageConfig{Path: t.TempDir(), Encryption: core.EncryptionConfig{Enabled: tc.encrypt, Key: "local-test-key"}}
			db, err := NewEngine(cfg, newTestLogger())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { db.Close() }()
			if err := db.Put("value", tc.value); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Get("value"); (err == nil) != tc.present {
				t.Fatalf("unexpected live presence: %v", err)
			}
			if err := db.Put("deleted", []byte("old")); err != nil {
				t.Fatal(err)
			}
			if err := db.Delete("deleted"); err != nil {
				t.Fatal(err)
			}
			for restart := 0; restart < 2; restart++ {
				if tc.checkpoint {
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
				value, err := db.Get("value")
				present := err == nil
				t.Logf("EXPECTED: present=%v ACTUAL: present=%v (restart=%d)", tc.present, present, restart)
				if present != tc.present || (present && !bytes.Equal(value, tc.value)) {
					t.Error("regression mismatch")
				}
				if _, err := db.Get("deleted"); err == nil {
					t.Fatal("deleted key revived")
				}
			}
		})
	}
}
