package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAuditEncryptedBackupRestore(t *testing.T) {
	cases := []struct {
		name              string
		encrypt, compress bool
		first, last       byte
		fallback, badKey  bool
	}{
		{"control-plain", false, false, 1, 1, false, false},
		{"encrypted-nonzero-salt", true, false, 1, 1, false, false},
		{"encrypted-gzip", true, true, 1, 1, false, false},
		{"salt-begins-json-brace", true, false, '{', 1, false, false},
		{"old-salt-last-byte-brace", true, false, 0, '{', false, false},
		{"salt-zero-prefix-control", true, false, 0, 1, false, false},
		{"plain-gzip-with-key", false, true, 1, 1, false, false},
		{"fallback-key", true, false, 1, 1, true, false},
		{"wrong-key", true, false, 1, 1, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewManager(&mockStorage{}, t.TempDir(), newTestLogger())
			if err := m.Init(); err != nil {
				t.Fatal(err)
			}
			backup := &Backup{Version: "1.0", CreatedAt: time.Unix(1000000000, 0).UTC(), BackupType: "full", Data: BackupData{Workspaces: []*core.Workspace{{ID: "roundtrip", Name: "Roundtrip"}}}}
			checksum, err := m.calculateChecksum(backup)
			if err != nil {
				t.Fatal(err)
			}
			backup.Checksum = checksum
			path := filepath.Join(m.backupsDir, "fixture.json")
			if tc.compress {
				path += ".gz"
			}
			key := []byte("fixed-local-audit-key")
			material := bytes.Repeat([]byte{1}, 44)
			material[12] = tc.first
			material[43] = tc.last
			err = func() error {
				old := rand.Reader
				rand.Reader = bytes.NewReader(material)
				defer func() { rand.Reader = old }()
				return m.writeBackupFile(backup, path, Options{Compress: tc.compress, Encrypt: tc.encrypt, EncryptionKey: key})
			}()
			if err != nil {
				t.Fatal(err)
			}
			keyPath := path + backupKeySuffix
			if tc.fallback {
				keyPath = filepath.Join(m.backupsDir, ".backup_key")
			}
			if tc.badKey {
				key = []byte("wrong-local-test-key")
			}
			if err := os.WriteFile(keyPath, key, 0600); err != nil {
				t.Fatal(err)
			}
			restored := &mockRestoreStorage{}
			err = m.Restore(context.Background(), restored, path, DefaultRestoreOptions())
			if tc.badKey {
				t.Logf("EXPECTED: restore error=true ACTUAL: restore error=%v", err != nil)
				if err == nil {
					t.Error("wrong key accepted")
				}
				return
			}
			t.Logf("EXPECTED: restore error=<nil> ACTUAL: restore error=%v", err)
			if err != nil {
				t.Error("encrypted backup roundtrip failed")
				return
			}
			if len(restored.workspaces) != 1 || restored.workspaces[0].ID != "roundtrip" {
				t.Fatal("roundtrip content changed")
			}
		})
	}

}
