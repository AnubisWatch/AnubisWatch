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

func auditEnvironmentKeyRestore(t *testing.T, mode string, compress bool) {
	t.Helper()
	key := []byte("fixed-local-environment-key")
	env := ""
	if mode != "sidecar" && mode != "default" {
		env = string(key)
	}
	t.Setenv("ANUBIS_BACKUP_ENCRYPTION_KEY", env)
	m := NewManager(&mockStorage{}, t.TempDir(), newTestLogger())
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	backup := &Backup{Version: "1.0", CreatedAt: time.Unix(1000000000, 0).UTC(), Data: BackupData{Workspaces: []*core.Workspace{{ID: "env-key", Name: "Local fixture"}}}}
	checksum, err := m.calculateChecksum(backup)
	if err != nil {
		t.Fatal(err)
	}
	backup.Checksum = checksum
	path := filepath.Join(m.backupsDir, "fixture.json")
	if compress {
		path += ".gz"
	}
	opts := Options{Encrypt: true, Compress: compress}
	if mode == "sidecar" || mode == "default" || mode == "sidecar-precedence" {
		opts.EncryptionKey = key
	}
	err = func() error {
		old := rand.Reader
		rand.Reader = bytes.NewReader(bytes.Repeat([]byte{1}, 44))
		defer func() { rand.Reader = old }()
		return m.writeBackupFile(backup, path, opts)
	}()
	if err != nil {
		t.Fatal(err)
	}
	switch mode {
	case "sidecar", "sidecar-precedence":
		if err := os.WriteFile(path+backupKeySuffix, key, 0600); err != nil {
			t.Fatal(err)
		}
	case "default":
		if err := os.WriteFile(filepath.Join(m.backupsDir, ".backup_key"), key, 0600); err != nil {
			t.Fatal(err)
		}
	case "env-over-default":
		if err := os.WriteFile(filepath.Join(m.backupsDir, ".backup_key"), []byte("different-key"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if mode == "wrong-env" || mode == "sidecar-precedence" {
		t.Setenv("ANUBIS_BACKUP_ENCRYPTION_KEY", "different-local-key")
	}
	restored := &mockRestoreStorage{}
	err = m.Restore(context.Background(), restored, path, DefaultRestoreOptions())
	wantError := mode == "wrong-env"
	t.Logf("mode=%s compressed=%v EXPECTED: restore error=%v ACTUAL: restore error=%v", mode, compress, wantError, err != nil)
	if (err != nil) != wantError {
		t.Fatal("regression mismatch")
	}
	if !wantError && (len(restored.workspaces) != 1 || restored.workspaces[0].ID != "env-key") {
		t.Fatal("restored data mismatch")
	}
	if mode == "env" || mode == "env-over-default" {
		if _, err := os.Stat(path + backupKeySuffix); !os.IsNotExist(err) {
			t.Fatal("environment key was persisted")
		}
	}
}
func TestAuditEnvironmentBackupKeyRestore(t *testing.T) {
	t.Run("sidecar-control", func(t *testing.T) { auditEnvironmentKeyRestore(t, "sidecar", false) })
	t.Run("environment-key", func(t *testing.T) { auditEnvironmentKeyRestore(t, "env", false) })
	t.Run("compressed-environment-key", func(t *testing.T) { auditEnvironmentKeyRestore(t, "env", true) })
	t.Run("default-key-control", func(t *testing.T) { auditEnvironmentKeyRestore(t, "default", false) })
	t.Run("environment-over-default", func(t *testing.T) { auditEnvironmentKeyRestore(t, "env-over-default", false) })
	t.Run("sidecar-precedence", func(t *testing.T) { auditEnvironmentKeyRestore(t, "sidecar-precedence", false) })
	t.Run("wrong-environment-key", func(t *testing.T) { auditEnvironmentKeyRestore(t, "wrong-env", false) })
}
