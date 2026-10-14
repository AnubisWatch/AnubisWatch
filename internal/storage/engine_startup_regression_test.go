package storage

import (
	"errors"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"os"
	"testing"
)

func TestAuditStartupWALClose(t *testing.T) {
	old := newWALSeam
	defer func() { newWALSeam = old }()
	var opened *writeAheadLog
	newWALSeam = func(path string) (*writeAheadLog, error) { w, err := old(path); opened = w; return w, err }
	cases := []struct {
		name    string
		enabled bool
		key     string
		fail    bool
		repeats int
	}{
		{"control-disabled", false, "", false, 1},
		{"missing-key", true, "", true, 1},
		{"valid-key", true, "audit-test-key", false, 1}, {"repeated-missing-key", true, "", true, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for i := 0; i < tc.repeats; i++ {
				opened = nil
				db, err := NewEngine(core.StorageConfig{Path: t.TempDir(), Encryption: core.EncryptionConfig{Enabled: tc.enabled, Key: tc.key}}, newTestLogger())
				if opened == nil {
					t.Fatal("WAL seam was not called")
				}
				defer opened.file.Close()
				if (err != nil) != tc.fail {
					t.Fatalf("startup error=%v expected failure=%v", err, tc.fail)
				}
				if db != nil {
					if err := db.Close(); err != nil {
						t.Fatal(err)
					}
				}
				_, statErr := opened.file.Stat()
				closed := errors.Is(statErr, os.ErrClosed)
				t.Logf("EXPECTED: WAL closed=true ACTUAL: WAL closed=%v", closed)
				if !closed {
					t.Error("startup left WAL open")
				}
			}
		})
	}

}
