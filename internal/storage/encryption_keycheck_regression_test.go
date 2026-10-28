package storage

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

const (
	keyA = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	keyB = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
)

func openKeyed(dir, key string) (*CobaltDB, error) {
	cfg := core.StorageConfig{Path: dir}
	if key != "" {
		cfg.Encryption = core.EncryptionConfig{Enabled: true, Key: key}
	}
	return NewEngine(cfg, quietLogger())
}

func TestEncryptionKeyLengthRule(t *testing.T) {
	cases := []struct {
		name string
		key  string
		ok   bool
	}{
		{"one byte", "x", false},
		{"fifteen bytes", "fifteen-bytes!!", false},
		{"init wizard 16 chars", "Ab3$dE6^gH9*jK2m", true},
		{"hex 32 bytes", keyA, true},
		{"long passphrase", "correct horse battery staple and more", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, err := openKeyed(t.TempDir(), tc.key)
			if (err == nil) != tc.ok {
				t.Fatalf("NewEngine(key=%q) err = %v, want ok=%v", tc.key, err, tc.ok)
			}
			if db != nil {
				_ = db.Close()
			}
		})
	}
	t.Run("weak key migration override", func(t *testing.T) {
		t.Setenv(core.AllowWeakEncryptionKeyEnv, "1")
		db, err := openKeyed(t.TempDir(), "x")
		if err != nil {
			t.Fatalf("override did not allow weak key: %v", err)
		}
		_ = db.Close()
	})
}

// The key string is HKDF input as-is; data written under a hex key must stay
// readable (no hex decoding was introduced).
func TestEncryptionKeyDerivationUnchanged(t *testing.T) {
	legacy := &encryptor{masterKey: []byte(keyA)}
	sealed, err := legacy.encrypt([]byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	enc, err := newEncryptor(keyA)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := enc.decrypt(sealed); err != nil || string(got) != "payload" {
		t.Fatalf("decrypt with same key string = %q, %v", got, err)
	}
}

func TestEncryptionKeyMismatchRefusesStartup(t *testing.T) {
	cases := []struct {
		name       string
		firstKey   string
		write      bool
		removeFile bool // simulate a store encrypted before the key check existed
		secondKey  string
		wantErr    string
	}{
		{"same key reopens", keyA, true, false, keyA, ""},
		{"wrong key with canary", keyA, true, false, keyB, "does not match"},
		{"wrong key empty store", keyA, false, false, keyB, "does not match"},
		{"encrypted store opened without key", keyA, true, false, "", "encryption is disabled"},
		{"legacy store wrong key", keyA, true, true, keyB, "decrypt"},
		{"legacy store right key", keyA, true, true, keyA, ""},
		{"plain store opened with key", "", true, false, keyA, "decrypt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			db, err := openKeyed(dir, tc.firstKey)
			if err != nil {
				t.Fatal(err)
			}
			if tc.write {
				if err := db.Put("default/souls/a", []byte(`{"id":"a"}`)); err != nil {
					t.Fatal(err)
				}
			}
			_ = db.Close()
			if tc.removeFile {
				if err := os.Remove(filepath.Join(dir, keyCheckFile)); err != nil {
					t.Fatal(err)
				}
			}
			walBefore, _ := os.ReadFile(filepath.Join(dir, "wal.log"))

			db2, err := openKeyed(dir, tc.secondKey)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("reopen: %v", err)
				}
				defer db2.Close()
				if tc.write {
					mustGet(t, db2, "default/souls/a", `{"id":"a"}`)
				}
				if _, err := os.Stat(filepath.Join(dir, keyCheckFile)); err != nil {
					t.Fatalf("key check not (re)created: %v", err)
				}
				return
			}
			if err == nil {
				db2.Close()
				t.Fatalf("reopen with mismatched key succeeded; want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.wantErr)
			}
			// The refused open must not have written anything under the new key.
			walAfter, _ := os.ReadFile(filepath.Join(dir, "wal.log"))
			if tc.write && len(walAfter) == 0 && len(walBefore) > 0 {
				t.Fatal("refused open emptied the WAL")
			}
			// And the original key must still work afterwards.
			db3, err := openKeyed(dir, tc.firstKey)
			if err != nil {
				t.Fatalf("original key no longer opens the store: %v", err)
			}
			defer db3.Close()
			if tc.write {
				mustGet(t, db3, "default/souls/a", `{"id":"a"}`)
			}
		})
	}
}

// The key check lives outside the keyspace: scans, checkpoints and restarts
// never see it.
func TestKeyCheckNotInKeyspace(t *testing.T) {
	dir := t.TempDir()
	db, err := openKeyed(dir, keyA)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Put("default/souls/a", []byte("v")); err != nil {
		t.Fatal(err)
	}
	if err := db.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	all, err := db.PrefixScan("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("PrefixScan(\"\") = %v, want only the stored record", all)
	}
	if ws := db.ListWorkspaceIDs(); len(ws) != 1 || ws[0] != "default" {
		t.Fatalf("workspaces = %v", ws)
	}
	_ = db.Close()
	db, err = openKeyed(dir, keyA)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mustGet(t, db, "default/souls/a", "v")
}

// /ready used Ping, which stayed green after an fsync failure even though
// every later write was refused until restart.
func TestPingReportsBrokenWAL(t *testing.T) {
	db := openTestEngine(t, t.TempDir())
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("Ping on healthy store: %v", err)
	}
	orig := walSyncSeam
	walSyncSeam = func(*os.File) error { return syscall.EIO }
	_ = db.Put("a", []byte("1"))
	walSyncSeam = orig
	if err := db.Ping(); err == nil {
		t.Fatal("Ping reports healthy after WAL fsync failure")
	}
}
