package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Older releases kept an encrypted backup's key in "<archive>.key". Deleting
// the backup must delete that key too: otherwise the secret that unlocks a
// removed archive stays on disk indefinitely and the backups directory fills
// with orphaned key files.
func TestDeleteRemovesEncryptionKeySidecar(t *testing.T) {
	t.Setenv("ANUBIS_BACKUP_ENCRYPTION_KEY", "")
	m := NewManager(&mockStorage{}, t.TempDir(), newTestLogger())
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}

	// Control: an unencrypted backup deletes cleanly and has no sidecar.
	_, plain, err := m.Create(context.Background(), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(filepath.Base(plain)); err != nil {
		t.Fatalf("delete plain: %v", err)
	}

	opts := DefaultOptions()
	opts.Encrypt = true
	_, path, err := m.Create(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + backupKeySuffix); !os.IsNotExist(err) {
		t.Fatalf("new archives must not get a key sidecar: %v", err)
	}
	// Simulate an archive written by an older release.
	if err := os.WriteFile(path+backupKeySuffix, []byte("legacy-key"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(filepath.Base(path)); err != nil {
		t.Fatalf("delete encrypted: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("archive still exists: %v", err)
	}
	if _, err := os.Stat(path + backupKeySuffix); !os.IsNotExist(err) {
		t.Errorf("key sidecar left behind after delete: %v", err)
	}
}
