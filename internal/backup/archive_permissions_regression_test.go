package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A backup archive carries the local user accounts and the admin password
// hash, so it must be readable by its owner only. The archive used to be
// written through a fixed "<name>.tmp" path with os.Create, which applies
// the process umask (0644 on a default host) and keeps the permissions of
// any file that already sat at that path.
func archivePerm(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}

func writeArchiveFixture(t *testing.T, m *Manager, path string) {
	t.Helper()
	backup := &Backup{
		Version:    backupVersionRecords,
		CreatedAt:  time.Unix(1000000000, 0).UTC(),
		BackupType: "full",
		Data: BackupData{
			Auth: json.RawMessage(`{"users":{"admin@example.com":{"id":"u1"}},"admin_password_hash":"$2a$12$hash"}`),
		},
	}
	checksum, err := m.calculateChecksum(backup)
	if err != nil {
		t.Fatalf("calculateChecksum: %v", err)
	}
	backup.Checksum = checksum
	if err := m.writeBackupFile(backup, path, Options{}); err != nil {
		t.Fatalf("writeBackupFile: %v", err)
	}
}

func TestBackupArchiveIsReadableByOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(&mockStorage{}, dir, newTestLogger())
	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	path := filepath.Join(m.backupsDir, "anubis_backup_fixture.json")

	writeArchiveFixture(t, m, path)

	if got := archivePerm(t, path); got != 0600 {
		t.Errorf("archive mode = %04o, want 0600", got)
	}
	if _, err := m.readBackupFile(path); err != nil {
		t.Errorf("archive is not readable back: %v", err)
	}
}

// A temporary file left behind by an earlier run must not be reused: the
// new archive must not be written through it (that would truncate whatever
// the path points at) and the leftover file, which belongs to nobody in
// particular, must be left exactly as it was found.
func TestBackupArchiveDoesNotReuseStaleTempFile(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(&mockStorage{}, dir, newTestLogger())
	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	path := filepath.Join(m.backupsDir, "anubis_backup_fixture.json")
	stale := path + ".tmp"
	const staleBody = "stale archive from an earlier run"
	if err := os.WriteFile(stale, []byte(staleBody), 0666); err != nil {
		t.Fatalf("write stale temp: %v", err)
	}
	stalePerm := archivePerm(t, stale)

	writeArchiveFixture(t, m, path)

	body, err := os.ReadFile(stale)
	if err != nil {
		t.Fatalf("read stale temp: %v", err)
	}
	if string(body) != staleBody {
		t.Errorf("stale temp file was overwritten: %q", body)
	}
	if got := archivePerm(t, stale); got != stalePerm {
		t.Errorf("stale temp file mode changed to %04o, want %04o", got, stalePerm)
	}
	if got := archivePerm(t, path); got != 0600 {
		t.Errorf("archive mode = %04o, want 0600", got)
	}
}

// A symlink planted at the temporary path must never be followed: writing
// through it would hand the archive to whoever owns the link target.
func TestBackupArchiveDoesNotFollowSymlinkedTempFile(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(&mockStorage{}, dir, newTestLogger())
	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	path := filepath.Join(m.backupsDir, "anubis_backup_fixture.json")
	victim := filepath.Join(dir, "victim.json")
	const victimBody = "not ours"
	if err := os.WriteFile(victim, []byte(victimBody), 0644); err != nil {
		t.Fatalf("write victim: %v", err)
	}
	victimPerm := archivePerm(t, victim)
	if err := os.Symlink(victim, path+".tmp"); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}

	writeArchiveFixture(t, m, path)

	body, err := os.ReadFile(victim)
	if err != nil {
		t.Fatalf("read victim: %v", err)
	}
	if string(body) != victimBody {
		t.Errorf("symlink target was written through: %q", body)
	}
	if got := archivePerm(t, victim); got != victimPerm {
		t.Errorf("symlink target mode changed to %04o, want %04o", got, victimPerm)
	}
	if info, err := os.Lstat(path + ".tmp"); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("planted symlink was replaced: %v, %v", info, err)
	}
	if got := archivePerm(t, path); got != 0600 {
		t.Errorf("archive mode = %04o, want 0600", got)
	}
}

// The private temporary file used to build the archive must not survive a
// failed write.
func TestBackupArchiveLeavesNoTempFilesBehind(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(&mockStorage{}, dir, newTestLogger())
	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// A directory at the destination makes the final rename fail.
	path := filepath.Join(m.backupsDir, "anubis_backup_fixture.json")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	writeArchiveFixtureExpectingFailure(t, m, path)

	entries, err := os.ReadDir(m.backupsDir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}

func writeArchiveFixtureExpectingFailure(t *testing.T, m *Manager, path string) {
	t.Helper()
	backup := &Backup{Version: backupVersionRecords, CreatedAt: time.Unix(1000000000, 0).UTC(), BackupType: "full"}
	checksum, err := m.calculateChecksum(backup)
	if err != nil {
		t.Fatalf("calculateChecksum: %v", err)
	}
	backup.Checksum = checksum
	if err := m.writeBackupFile(backup, path, Options{}); err == nil {
		t.Fatalf("writeBackupFile succeeded, want failure")
	}
}

// The archive is written next to the key that decrypts it, and the key is a
// secret of the same strength as the password hashes inside the archive.
func TestBackupEncryptionKeySidecarIsReadableByOwnerOnly(t *testing.T) {
	t.Setenv("ANUBIS_BACKUP_ENCRYPTION_KEY", "")
	dir := t.TempDir()
	m := NewManager(&mockStorage{}, dir, newTestLogger())
	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	path := filepath.Join(m.backupsDir, "anubis_backup_fixture.json")
	backup := &Backup{Version: backupVersionRecords, CreatedAt: time.Unix(1000000000, 0).UTC(), BackupType: "full"}
	checksum, err := m.calculateChecksum(backup)
	if err != nil {
		t.Fatalf("calculateChecksum: %v", err)
	}
	backup.Checksum = checksum
	// No key is supplied, so the manager generates one and stores a copy
	// next to the archive.
	if err := m.writeBackupFile(backup, path, Options{Encrypt: true}); err != nil {
		t.Fatalf("writeBackupFile: %v", err)
	}
	if got := archivePerm(t, path+backupKeySuffix); got != 0600 {
		t.Errorf("key sidecar mode = %04o, want 0600", got)
	}
}
