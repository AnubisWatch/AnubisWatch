package backup

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func newAuthTestManager(t *testing.T) *Manager {
	t.Helper()
	m := NewManager(&mockStorage{workspaces: []*core.Workspace{{ID: "ws", Name: "Original"}}}, t.TempDir(), newTestLogger())
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	return m
}

// The checksum was an unkeyed SHA-256 that anyone editing an archive could
// recompute, so a tampered archive (for example one carrying an attacker's
// admin password hash) restored cleanly.
func TestTamperedArchiveWithRecomputedChecksumIsRejected(t *testing.T) {
	cases := []struct {
		name    string
		encrypt bool
	}{{"plain", false}, {"encrypted", true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(backupKeyEnv, "")
			m := newAuthTestManager(t)
			opts := Options{Encrypt: tc.encrypt}
			_, path, err := m.Create(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			// Control: the untouched archive restores.
			if err := m.Restore(context.Background(), &mockRestoreStorage{}, path, DefaultRestoreOptions()); err != nil {
				t.Fatalf("untampered restore: %v", err)
			}

			b, err := m.readBackupFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if b.Signature == "" {
				t.Fatal("archive is not signed")
			}
			b.Data.Workspaces[0].Name = "Attacker"
			b.Data.Auth = json.RawMessage(`{"admin":"attacker-hash"}`)
			b.Checksum, err = m.calculateChecksum(b)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(b)
			tampered := filepath.Join(m.backupsDir, "anubis_tampered.json")
			if err := os.WriteFile(tampered, raw, 0600); err != nil {
				t.Fatal(err)
			}
			restored := &mockRestoreStorage{}
			err = m.Restore(context.Background(), restored, tampered, DefaultRestoreOptions())
			if err == nil || !strings.Contains(err.Error(), "signature") {
				t.Fatalf("tampered restore err = %v, want signature failure", err)
			}
			if len(restored.workspaces) != 0 {
				t.Fatal("tampered archive wrote data before being rejected")
			}
		})
	}
}

// Archives written before signatures existed restore only with an explicit
// opt-in; without it, stripping a signature would bypass verification.
func TestUnsignedLegacyArchiveStillRestores(t *testing.T) {
	t.Setenv(backupKeyEnv, "")
	m := newAuthTestManager(t)
	b := &Backup{Version: backupVersionTyped, CreatedAt: time.Unix(1e9, 0).UTC(), BackupType: "full",
		Data: BackupData{Workspaces: []*core.Workspace{{ID: "legacy", Name: "Legacy"}}}}
	var err error
	if b.Checksum, err = m.calculateChecksum(b); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(b)
	path := filepath.Join(m.backupsDir, "anubis_legacy.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	refused := &mockRestoreStorage{}
	err = m.Restore(context.Background(), refused, path, DefaultRestoreOptions())
	if !errors.Is(err, ErrUnauthenticatedBackup) {
		t.Fatalf("legacy restore without opt-in: err = %v, want ErrUnauthenticatedBackup", err)
	}
	if len(refused.workspaces) != 0 {
		t.Fatal("refused archive wrote data")
	}
	restored := &mockRestoreStorage{}
	opts := DefaultRestoreOptions()
	opts.AllowUnauthenticated = true
	if err := m.Restore(context.Background(), restored, path, opts); err != nil {
		t.Fatalf("legacy restore: %v", err)
	}
	if len(restored.workspaces) != 1 || restored.workspaces[0].ID != "legacy" {
		t.Fatalf("restored = %+v", restored.workspaces)
	}
}

// The auto-generated key used to live in backups/.backup_key, i.e. inside the
// directory that gets copied off the node. It now lives in the data dir and is
// migrated; archives encrypted under the old key stay restorable.
func TestLegacyBackupKeyIsMigratedAndOldArchivesRestore(t *testing.T) {
	t.Setenv(backupKeyEnv, "")
	m := newAuthTestManager(t)
	legacyKey := []byte("0123456789abcdef0123456789abcdef") // raw 32 bytes, as old releases wrote
	if err := os.WriteFile(m.legacyKeyFilePath(), legacyKey, 0600); err != nil {
		t.Fatal(err)
	}
	old := &Backup{Version: backupVersionTyped, CreatedAt: time.Unix(1e9, 0).UTC(), BackupType: "full",
		Data: BackupData{Workspaces: []*core.Workspace{{ID: "old", Name: "Old"}}}}
	var err error
	if old.Checksum, err = m.calculateChecksum(old); err != nil {
		t.Fatal(err)
	}
	plain, _ := json.Marshal(old)
	sealed, err := encryptData(legacyKey, plain)
	if err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(m.backupsDir, "anubis_old.json")
	if err := os.WriteFile(oldPath, sealed, 0600); err != nil {
		t.Fatal(err)
	}

	// A new encrypted backup migrates the key out of the backups directory.
	if _, _, err := m.Create(context.Background(), Options{Encrypt: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.legacyKeyFilePath()); !os.IsNotExist(err) {
		t.Fatalf("legacy key still inside backups dir: %v", err)
	}
	if got, err := os.ReadFile(m.keyFilePath()); err != nil || string(got) != string(legacyKey) {
		t.Fatalf("migrated key = %q, %v", got, err)
	}
	restored := &mockRestoreStorage{}
	if err := m.Restore(context.Background(), restored, oldPath, DefaultRestoreOptions()); err != nil {
		t.Fatalf("old archive after migration: %v", err)
	}
}

// ANUBIS_BACKUP_ENCRYPTION_KEY accepts a 64-character hex key (decoded to 32
// bytes) while archives sealed with the same string as raw bytes, as older
// releases did, still decrypt.
func TestHexBackupKeyCompatibleWithRawForm(t *testing.T) {
	hexKey := strings.Repeat("ab", 32)
	t.Setenv(backupKeyEnv, hexKey)
	m := newAuthTestManager(t)

	key, _, err := m.getEncryptionKey(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := hex.DecodeString(hexKey); string(key) != string(want) {
		t.Fatalf("hex env key not decoded: len=%d", len(key))
	}
	_, newPath, err := m.Create(context.Background(), Options{Encrypt: true})
	if err != nil {
		t.Fatal(err)
	}

	legacy := &Backup{Version: backupVersionTyped, CreatedAt: time.Unix(1e9, 0).UTC(), BackupType: "full"}
	if legacy.Checksum, err = m.calculateChecksum(legacy); err != nil {
		t.Fatal(err)
	}
	plain, _ := json.Marshal(legacy)
	sealed, err := encryptData([]byte(hexKey), plain)
	if err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(m.backupsDir, "anubis_rawhex.json")
	if err := os.WriteFile(oldPath, sealed, 0600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{newPath, oldPath} {
		if err := m.Restore(context.Background(), &mockRestoreStorage{}, p, DefaultRestoreOptions()); err != nil {
			t.Fatalf("restore %s: %v", filepath.Base(p), err)
		}
	}
}

// Removing the signature from a signed archive must not turn the tamper
// rejection into a successful restore.
func TestStrippedSignatureIsRefused(t *testing.T) {
	t.Setenv(backupKeyEnv, "")
	m := newAuthTestManager(t)
	b := &Backup{Version: backupVersionTyped, CreatedAt: time.Unix(1e9, 0).UTC(), BackupType: "full",
		Data: BackupData{Workspaces: []*core.Workspace{{ID: "evil", Name: "Evil"}}}}
	path := filepath.Join(m.backupsDir, "anubis_signed.json")
	if err := m.writeBackupFile(b, path, Options{}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stripped Backup
	if err := json.Unmarshal(raw, &stripped); err != nil {
		t.Fatal(err)
	}
	if stripped.Signature == "" {
		t.Fatal("control: archive was written unsigned")
	}
	stripped.Signature = ""
	if stripped.Checksum, err = m.calculateChecksum(&stripped); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(&stripped)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	restored := &mockRestoreStorage{}
	err = m.Restore(context.Background(), restored, path, DefaultRestoreOptions())
	if !errors.Is(err, ErrUnauthenticatedBackup) {
		t.Fatalf("stripped restore err = %v, want ErrUnauthenticatedBackup", err)
	}
	if len(restored.workspaces) != 0 {
		t.Fatal("stripped archive wrote data before being rejected")
	}
}
