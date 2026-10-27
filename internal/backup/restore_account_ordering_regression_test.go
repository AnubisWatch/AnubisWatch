package backup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// The accounts in sessions.json are the last thing a restore may touch.
// Restoring them before the data is in place locks the operator out of a
// node whose data never arrived, and destroys the local accounts of a node
// whose restore then failed.
const existingSessionsJSON = `{"tokens":{"live-token":{"user_id":"u1"}},"users":{"old@example.com":{"id":"old"}},"admin_password_hash":"$2a$12$oldhash"}`

const backedUpAuthJSON = `{"users":{"new@example.com":{"id":"new"}},"admin_password_hash":"$2a$12$newhash"}`

// authOrderStore is a store that accepts raw records and judgment history.
type authOrderStore struct {
	mockRestoreStorage
	putErr      error
	judgmentErr error
	puts        []string
	judgments   []*core.Judgment
}

func (s *authOrderStore) Put(key string, value []byte) error {
	if s.putErr != nil {
		return s.putErr
	}
	s.puts = append(s.puts, key)
	return nil
}

func (s *authOrderStore) SaveJudgment(ctx context.Context, j *core.Judgment) error {
	if s.judgmentErr != nil {
		return s.judgmentErr
	}
	s.judgments = append(s.judgments, j)
	return nil
}

// authOrderNoSinkStore is a store that cannot accept raw records at all.
type authOrderNoSinkStore struct {
	mockRestoreStorage
}

func writeAuthOrderingBackup(t *testing.T, m *Manager, path string, auth string, records map[string][]byte, judgments []*core.Judgment) {
	t.Helper()
	backup := &Backup{
		Version:    backupVersionRecords,
		CreatedAt:  time.Unix(1000000000, 0).UTC(),
		BackupType: "full",
		Data: BackupData{
			Auth:      json.RawMessage(auth),
			Records:   records,
			Judgments: judgments,
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

func writeExistingSessions(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, sessionsFile)
	if err := os.WriteFile(path, []byte(existingSessionsJSON), 0600); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}
	return path
}

func assertSessionsUnchanged(t *testing.T, path string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sessions.json: %v", err)
	}
	if string(body) != existingSessionsJSON {
		t.Errorf("sessions.json was replaced by a failed restore: %s", body)
	}
}

func restoreAuthFixture(t *testing.T, store RestoreStorage, judgments []*core.Judgment) error {
	t.Helper()
	dir := t.TempDir()
	sessions := writeExistingSessions(t, dir)
	backupDir := t.TempDir()
	backupPath := filepath.Join(backupDir, "anubis_backup_fixture.json")

	m := NewManager(nil, dir, newTestLogger())
	writeAuthOrderingBackup(t, m, backupPath, backedUpAuthJSON,
		map[string][]byte{"default/souls/s1": []byte(`{"id":"s1"}`)}, judgments)

	err := m.Restore(context.Background(), store, backupPath, DefaultRestoreOptions())
	t.Cleanup(func() {
		if err != nil {
			assertSessionsUnchanged(t, sessions)
		}
	})
	return err
}

func TestRestoreKeepsAccountsWhenStoreCannotAcceptRecords(t *testing.T) {
	store := &authOrderNoSinkStore{}
	err := restoreAuthFixture(t, store, nil)
	if err == nil {
		t.Fatal("restore succeeded for a store that cannot accept raw records")
	}
	if !strings.Contains(err.Error(), "raw records") {
		t.Errorf("error = %v, want it to name the missing record sink", err)
	}
}

func TestRestoreKeepsAccountsWhenRecordWriteFails(t *testing.T) {
	store := &authOrderStore{putErr: errors.New("disk full")}
	err := restoreAuthFixture(t, store, nil)
	if err == nil {
		t.Fatal("restore succeeded although every record write failed")
	}
}

func TestRestoreKeepsAccountsWhenJudgmentRestoreFails(t *testing.T) {
	store := &authOrderStore{judgmentErr: errors.New("judgment index rebuild failed")}
	judgments := []*core.Judgment{{ID: "j1", SoulID: "s1", WorkspaceID: "default", Status: core.SoulAlive, Timestamp: time.Unix(1000000000, 0)}}
	err := restoreAuthFixture(t, store, judgments)
	if err == nil {
		t.Fatal("restore succeeded although the judgment restore failed")
	}
}

func TestRestoreReplacesAccountsAfterSuccessfulRestore(t *testing.T) {
	dir := t.TempDir()
	sessions := writeExistingSessions(t, dir)
	backupDir := t.TempDir()
	backupPath := filepath.Join(backupDir, "anubis_backup_fixture.json")

	m := NewManager(nil, dir, newTestLogger())
	writeAuthOrderingBackup(t, m, backupPath, backedUpAuthJSON,
		map[string][]byte{"default/souls/s1": []byte(`{"id":"s1"}`)}, nil)

	store := &authOrderStore{}
	if err := m.Restore(context.Background(), store, backupPath, DefaultRestoreOptions()); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(store.puts) != 1 {
		t.Errorf("restored records = %v, want one", store.puts)
	}

	body, err := os.ReadFile(sessions)
	if err != nil {
		t.Fatalf("read sessions.json: %v", err)
	}
	if !strings.Contains(string(body), "new@example.com") {
		t.Errorf("restored sessions.json is missing the backed up accounts: %s", body)
	}
	if !strings.Contains(string(body), "$2a$12$newhash") {
		t.Errorf("restored sessions.json is missing the admin password hash: %s", body)
	}
	if strings.Contains(string(body), "old@example.com") {
		t.Errorf("restored sessions.json still carries the previous accounts: %s", body)
	}
	if strings.Contains(string(body), "live-token") {
		t.Errorf("restored sessions.json carries live login tokens: %s", body)
	}
}

func TestRestoreKeepsAccountsWhenUsersAreNotRequested(t *testing.T) {
	dir := t.TempDir()
	sessions := writeExistingSessions(t, dir)
	backupDir := t.TempDir()
	backupPath := filepath.Join(backupDir, "anubis_backup_fixture.json")

	m := NewManager(nil, dir, newTestLogger())
	writeAuthOrderingBackup(t, m, backupPath, backedUpAuthJSON,
		map[string][]byte{"default/souls/s1": []byte(`{"id":"s1"}`)}, nil)

	opts := DefaultRestoreOptions()
	opts.IncludeUsers = false
	store := &authOrderStore{}
	if err := m.Restore(context.Background(), store, backupPath, opts); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(store.puts) != 1 {
		t.Errorf("restored records = %v, want one", store.puts)
	}
	assertSessionsUnchanged(t, sessions)
}

// A forced restore tolerates the records it could not write, but the
// accounts are still the operator's way back into the node: they must not be
// replaced while the data is incomplete.
func TestRestoreKeepsAccountsOnPartialRestore(t *testing.T) {
	dir := t.TempDir()
	sessions := writeExistingSessions(t, dir)
	backupDir := t.TempDir()
	backupPath := filepath.Join(backupDir, "anubis_backup_fixture.json")

	m := NewManager(nil, dir, newTestLogger())
	writeAuthOrderingBackup(t, m, backupPath, backedUpAuthJSON,
		map[string][]byte{"default/souls/s1": []byte(`{"id":"s1"}`)}, nil)

	opts := DefaultRestoreOptions()
	opts.ContinueOnError = true
	store := &authOrderStore{putErr: errors.New("disk full")}
	if err := m.Restore(context.Background(), store, backupPath, opts); err == nil {
		t.Fatal("Restore with missing records must report a partial restore")
	}
	assertSessionsUnchanged(t, sessions)
}
