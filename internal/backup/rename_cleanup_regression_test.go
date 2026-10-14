package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func auditBackupRenameCleanup(t *testing.T, fail, compress, sentinel bool) {
	t.Helper()
	dir := t.TempDir()
	m := NewManager(&mockStorage{}, dir, newTestLogger())
	path := filepath.Join(dir, "backup.json")
	if fail {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		if sentinel {
			if err := os.WriteFile(filepath.Join(path, "keep"), []byte("destination"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := 0; i < 3; i++ {
		err := m.writeBackupFile(&Backup{Version: "1.0"}, path, Options{Compress: compress})
		if (err != nil) != fail {
			t.Fatalf("expected failure=%v got %v", fail, err)
		}
		_, statErr := os.Stat(path + ".tmp")
		exists := !os.IsNotExist(statErr)
		t.Logf("fail=%v compressed=%v iteration=%d EXPECTED: temporary exists=false ACTUAL: temporary exists=%v", fail, compress, i, exists)
		if exists {
			t.Fatal("regression mismatch")
		}
	}
	if sentinel {
		data, err := os.ReadFile(filepath.Join(path, "keep"))
		if err != nil || string(data) != "destination" {
			t.Fatal("destination changed")
		}
	}
	if !fail {
		if _, err := m.readBackupFile(path); err != nil {
			t.Fatal(err)
		}
	}
}
func TestAuditBackupRenameCleanup(t *testing.T) {
	t.Run("successful-control", func(t *testing.T) { auditBackupRenameCleanup(t, false, false, false) })
	t.Run("failed-rename", func(t *testing.T) { auditBackupRenameCleanup(t, true, false, false) })
	t.Run("compressed-failure", func(t *testing.T) { auditBackupRenameCleanup(t, true, true, false) })
	t.Run("preserve-destination", func(t *testing.T) { auditBackupRenameCleanup(t, true, false, true) })
	t.Run("compressed-control", func(t *testing.T) { auditBackupRenameCleanup(t, false, true, false) })
}
