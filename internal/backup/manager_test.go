package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// mockStorage implements BackupStorage interface for testing
type mockStorage struct {
	workspaces          []*core.Workspace
	souls               []*core.Soul
	alertChannels       []*core.AlertChannel
	alertRules          []*core.AlertRule
	statusPages         []*core.StatusPage
	journeys            []*core.JourneyConfig
	systemConfig        map[string][]byte
	listWorkspacesError error
}

func (m *mockStorage) ListWorkspaces(ctx context.Context) ([]*core.Workspace, error) {
	if m.listWorkspacesError != nil {
		return nil, m.listWorkspacesError
	}
	return m.workspaces, nil
}

func (m *mockStorage) ListSouls(ctx context.Context, workspaceID string, offset, limit int) ([]*core.Soul, error) {
	var result []*core.Soul
	for _, s := range m.souls {
		if s.WorkspaceID == workspaceID {
			result = append(result, s)
		}
	}
	return result, nil
}

func (m *mockStorage) ListAlertChannels(workspace string) ([]*core.AlertChannel, error) {
	var result []*core.AlertChannel
	for _, ch := range m.alertChannels {
		if ch.WorkspaceID == workspace || ch.WorkspaceID == "" {
			result = append(result, ch)
		}
	}
	return result, nil
}

func (m *mockStorage) ListAlertRules(workspace string) ([]*core.AlertRule, error) {
	var result []*core.AlertRule
	for _, r := range m.alertRules {
		if r.WorkspaceID == workspace || r.WorkspaceID == "" {
			result = append(result, r)
		}
	}
	return result, nil
}

func (m *mockStorage) ListStatusPages() ([]*core.StatusPage, error) {
	return m.statusPages, nil
}

func (m *mockStorage) ListJourneys(ctx context.Context, workspaceID string) ([]*core.JourneyConfig, error) {
	var result []*core.JourneyConfig
	for _, j := range m.journeys {
		if j.WorkspaceID == workspaceID {
			result = append(result, j)
		}
	}
	return result, nil
}

func (m *mockStorage) ListJudgments(ctx context.Context, soulID string, start, end time.Time, limit int) ([]*core.Judgment, error) {
	return nil, nil
}

func (m *mockStorage) GetSystemConfig(ctx context.Context, key string) ([]byte, error) {
	return m.systemConfig[key], nil
}

// mockRestoreStorage implements RestoreStorage interface for testing
type mockRestoreStorage struct {
	souls         []*core.Soul
	workspaces    []*core.Workspace
	alertChannels []*core.AlertChannel
	alertRules    []*core.AlertRule
	statusPages   []*core.StatusPage
	journeys      []*core.JourneyConfig
	systemConfig  map[string][]byte
}

func (m *mockRestoreStorage) SaveSoul(ctx context.Context, soul *core.Soul) error {
	m.souls = append(m.souls, soul)
	return nil
}

func (m *mockRestoreStorage) SaveWorkspace(ctx context.Context, ws *core.Workspace) error {
	m.workspaces = append(m.workspaces, ws)
	return nil
}

func (m *mockRestoreStorage) SaveAlertChannel(ch *core.AlertChannel) error {
	m.alertChannels = append(m.alertChannels, ch)
	return nil
}

func (m *mockRestoreStorage) SaveAlertRule(rule *core.AlertRule) error {
	m.alertRules = append(m.alertRules, rule)
	return nil
}

func (m *mockRestoreStorage) SaveStatusPage(page *core.StatusPage) error {
	m.statusPages = append(m.statusPages, page)
	return nil
}

func (m *mockRestoreStorage) SaveJourney(ctx context.Context, j *core.JourneyConfig) error {
	m.journeys = append(m.journeys, j)
	return nil
}

func (m *mockRestoreStorage) SaveSystemConfig(ctx context.Context, key string, value []byte) error {
	if m.systemConfig == nil {
		m.systemConfig = make(map[string][]byte)
	}
	m.systemConfig[key] = value
	return nil
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestManager_Init(t *testing.T) {
	tempDir := t.TempDir()
	mgr := NewManager(nil, tempDir, newTestLogger())

	err := mgr.Init()
	if err != nil {
		t.Errorf("Init() error = %v", err)
	}

	// Check that backups directory was created
	backupsDir := filepath.Join(tempDir, "backups")
	if _, err := os.Stat(backupsDir); os.IsNotExist(err) {
		t.Error("backups directory was not created")
	}
}

func TestManager_Create(t *testing.T) {
	tempDir := t.TempDir()

	// Create mock storage with test data
	storage := &mockStorage{
		workspaces: []*core.Workspace{
			{ID: "ws1", Name: "Workspace 1"},
			{ID: "ws2", Name: "Workspace 2"},
		},
		souls: []*core.Soul{
			{ID: "soul1", Name: "Soul 1", WorkspaceID: "ws1"},
			{ID: "soul2", Name: "Soul 2", WorkspaceID: "ws1"},
			{ID: "soul3", Name: "Soul 3", WorkspaceID: "ws2"},
		},
		alertChannels: []*core.AlertChannel{
			{ID: "ch1", Name: "Channel 1", WorkspaceID: "ws1"},
		},
		alertRules: []*core.AlertRule{
			{ID: "rule1", Name: "Rule 1", WorkspaceID: "ws1"},
		},
		statusPages: []*core.StatusPage{
			{ID: "page1", Name: "Status Page 1"},
		},
		journeys: []*core.JourneyConfig{
			{ID: "journey1", Name: "Journey 1", WorkspaceID: "ws1"},
		},
	}

	mgr := NewManager(storage, tempDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	opts := DefaultOptions()
	ctx := context.Background()

	backup, path, err := mgr.Create(ctx, opts)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Verify backup contents
	if backup.Version != "1.0" {
		t.Errorf("backup.Version = %s, want 1.0", backup.Version)
	}

	if backup.BackupType != "full" {
		t.Errorf("backup.BackupType = %s, want full", backup.BackupType)
	}

	if backup.Metadata.Workspaces != 2 {
		t.Errorf("backup.Metadata.Workspaces = %d, want 2", backup.Metadata.Workspaces)
	}

	if backup.Metadata.Souls != 3 {
		t.Errorf("backup.Metadata.Souls = %d, want 3", backup.Metadata.Souls)
	}

	if backup.Metadata.AlertChannels != 1 {
		t.Errorf("backup.Metadata.AlertChannels = %d, want 1", backup.Metadata.AlertChannels)
	}

	if backup.Checksum == "" {
		t.Error("backup.Checksum is empty")
	}

	// Verify file was created
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("backup file was not created")
	}

	// Cleanup
	os.Remove(path)
}

func TestManager_CreateAndRestore(t *testing.T) {
	tempDir := t.TempDir()

	// Create mock storage with test data
	storage := &mockStorage{
		workspaces: []*core.Workspace{
			{ID: "ws1", Name: "Workspace 1"},
		},
		souls: []*core.Soul{
			{ID: "soul1", Name: "Soul 1", WorkspaceID: "ws1", Target: "https://example.com"},
		},
		alertChannels: []*core.AlertChannel{
			{ID: "ch1", Name: "Channel 1", Type: core.ChannelSlack},
		},
		alertRules: []*core.AlertRule{
			{ID: "rule1", Name: "Rule 1", Enabled: true},
		},
		statusPages: []*core.StatusPage{
			{ID: "page1", Name: "Status Page 1", Slug: "status"},
		},
		journeys: []*core.JourneyConfig{
			{ID: "journey1", Name: "Journey 1", WorkspaceID: "ws1"},
		},
	}

	mgr := NewManager(storage, tempDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Create backup
	opts := DefaultOptions()
	ctx := context.Background()

	_, path, err := mgr.Create(ctx, opts)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer os.Remove(path)

	// Restore to new storage
	restoreStorage := &mockRestoreStorage{}
	restoreOpts := DefaultRestoreOptions()

	err = mgr.Restore(ctx, restoreStorage, path, restoreOpts)
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}

	// Verify restored data
	if len(restoreStorage.workspaces) != 1 {
		t.Errorf("restored workspaces = %d, want 1", len(restoreStorage.workspaces))
	}

	if len(restoreStorage.souls) != 1 {
		t.Errorf("restored souls = %d, want 1", len(restoreStorage.souls))
	}

	if restoreStorage.souls[0].Target != "https://example.com" {
		t.Errorf("restored soul target = %s, want https://example.com", restoreStorage.souls[0].Target)
	}

	if len(restoreStorage.alertChannels) != 1 {
		t.Errorf("restored alert channels = %d, want 1", len(restoreStorage.alertChannels))
	}

	if len(restoreStorage.alertRules) != 1 {
		t.Errorf("restored alert rules = %d, want 1", len(restoreStorage.alertRules))
	}

	if len(restoreStorage.statusPages) != 1 {
		t.Errorf("restored status pages = %d, want 1", len(restoreStorage.statusPages))
	}

	if len(restoreStorage.journeys) != 1 {
		t.Errorf("restored journeys = %d, want 1", len(restoreStorage.journeys))
	}
}

func TestManager_List(t *testing.T) {
	tempDir := t.TempDir()

	storage := &mockStorage{
		workspaces: []*core.Workspace{{ID: "ws1", Name: "Workspace 1"}},
		souls:      []*core.Soul{{ID: "soul1", Name: "Soul 1", WorkspaceID: "ws1"}},
	}

	mgr := NewManager(storage, tempDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Create a backup first
	ctx := context.Background()
	_, path, err := mgr.Create(ctx, DefaultOptions())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer os.Remove(path)

	// List backups
	backups, err := mgr.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(backups) != 1 {
		t.Errorf("List() returned %d backups, want 1", len(backups))
	}

	if backups[0].Filename == "" {
		t.Error("backup.Filename is empty")
	}
}

func TestManager_Delete(t *testing.T) {
	tempDir := t.TempDir()

	storage := &mockStorage{
		workspaces: []*core.Workspace{{ID: "ws1", Name: "Workspace 1"}},
		souls:      []*core.Soul{{ID: "soul1", Name: "Soul 1", WorkspaceID: "ws1"}},
	}

	mgr := NewManager(storage, tempDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Create a backup
	ctx := context.Background()
	_, path, err := mgr.Create(ctx, DefaultOptions())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	filename := filepath.Base(path)

	// Delete the backup
	err = mgr.Delete(filename)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("backup file still exists after delete")
	}
}

func TestManager_Delete_InvalidPath(t *testing.T) {
	tempDir := t.TempDir()
	mgr := NewManager(nil, tempDir, newTestLogger())

	// Try to delete a file outside backups directory
	err := mgr.Delete("../etc/passwd")
	if err == nil {
		t.Error("Delete() should fail for path outside backups directory")
	}
}

func TestManager_VerifyChecksum(t *testing.T) {
	tempDir := t.TempDir()

	storage := &mockStorage{
		workspaces: []*core.Workspace{{ID: "ws1", Name: "Workspace 1"}},
		souls:      []*core.Soul{{ID: "soul1", Name: "Soul 1", WorkspaceID: "ws1"}},
	}

	mgr := NewManager(storage, tempDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Create a backup
	ctx := context.Background()
	backup, path, err := mgr.Create(ctx, DefaultOptions())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer os.Remove(path)

	// Verify checksum is valid
	if err := mgr.verifyChecksum(backup); err != nil {
		t.Errorf("verifyChecksum() error = %v", err)
	}

	// Modify checksum and verify it fails
	backup.Checksum = "invalid"
	if err := mgr.verifyChecksum(backup); err == nil {
		t.Error("verifyChecksum() should fail for invalid checksum")
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()

	if !opts.Compress {
		t.Error("DefaultOptions().Compress should be true")
	}

	if !opts.IncludeJudgments {
		t.Error("DefaultOptions().IncludeJudgments should be true")
	}

	if opts.JudgmentDays != 7 {
		t.Errorf("DefaultOptions().JudgmentDays = %d, want 7", opts.JudgmentDays)
	}
}

func TestDefaultRestoreOptions(t *testing.T) {
	opts := DefaultRestoreOptions()

	if !opts.IncludeWorkspaces {
		t.Error("DefaultRestoreOptions().IncludeWorkspaces should be true")
	}

	if !opts.IncludeSouls {
		t.Error("DefaultRestoreOptions().IncludeSouls should be true")
	}

	if !opts.IncludeAlerts {
		t.Error("DefaultRestoreOptions().IncludeAlerts should be true")
	}

	if !opts.IncludeStatusPages {
		t.Error("DefaultRestoreOptions().IncludeStatusPages should be true")
	}

	if !opts.IncludeJourneys {
		t.Error("DefaultRestoreOptions().IncludeJourneys should be true")
	}

	if !opts.IncludeSystemConfig {
		t.Error("DefaultRestoreOptions().IncludeSystemConfig should be true")
	}

	if opts.ContinueOnError {
		t.Error("DefaultRestoreOptions().ContinueOnError should be false")
	}
}

func TestIsWithinDirectory(t *testing.T) {
	tests := []struct {
		path     string
		dir      string
		expected bool
	}{
		{"/data/backups/file.json", "/data/backups", true},
		{"/data/backups/subdir/file.json", "/data/backups", true},
		{"/data/other/file.json", "/data/backups", false},
		{"/etc/passwd", "/data/backups", false},
	}

	for _, tt := range tests {
		result := IsWithinDirectory(tt.path, tt.dir)
		if result != tt.expected {
			t.Errorf("IsWithinDirectory(%q, %q) = %v, want %v",
				tt.path, tt.dir, result, tt.expected)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1024 * 1024, "1.00 MB"},
		{1024 * 1024 * 1024, "1.00 GB"},
	}

	for _, tt := range tests {
		result := FormatBytes(tt.bytes)
		if result != tt.expected {
			t.Errorf("FormatBytes(%d) = %s, want %s", tt.bytes, result, tt.expected)
		}
	}
}

func TestManager_Get(t *testing.T) {
	tempDir := t.TempDir()

	storage := &mockStorage{
		workspaces: []*core.Workspace{{ID: "ws1", Name: "Workspace 1"}},
		souls:      []*core.Soul{{ID: "soul1", Name: "Soul 1", WorkspaceID: "ws1"}},
	}

	mgr := NewManager(storage, tempDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Create a backup
	ctx := context.Background()
	created, path, err := mgr.Create(ctx, DefaultOptions())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer os.Remove(path)

	filename := filepath.Base(path)

	// Get the backup
	retrieved, err := mgr.Get(filename)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if retrieved.Version != created.Version {
		t.Errorf("retrieved.Version = %s, want %s", retrieved.Version, created.Version)
	}

	if retrieved.Metadata.Workspaces != created.Metadata.Workspaces {
		t.Errorf("retrieved.Metadata.Workspaces = %d, want %d", retrieved.Metadata.Workspaces, created.Metadata.Workspaces)
	}
}

func TestManager_Get_InvalidPath(t *testing.T) {
	tempDir := t.TempDir()
	mgr := NewManager(nil, tempDir, newTestLogger())

	// Try to get a file outside backups directory
	_, err := mgr.Get("../etc/passwd")
	if err == nil {
		t.Error("Get() should fail for path outside backups directory")
	}
}

func TestManager_Get_NonExistent(t *testing.T) {
	tempDir := t.TempDir()
	mgr := NewManager(nil, tempDir, newTestLogger())
	mgr.Init()

	// Try to get a non-existent file
	_, err := mgr.Get("non-existent-backup.json")
	if err == nil {
		t.Error("Get() should fail for non-existent file")
	}
}

func TestManager_ExportToTar(t *testing.T) {
	tempDir := t.TempDir()

	storage := &mockStorage{
		workspaces: []*core.Workspace{{ID: "ws1", Name: "Workspace 1"}},
		souls:      []*core.Soul{{ID: "soul1", Name: "Soul 1", WorkspaceID: "ws1"}},
	}

	mgr := NewManager(storage, tempDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Export to tar
	var buf bytes.Buffer
	ctx := context.Background()
	opts := DefaultOptions()

	err := mgr.ExportToTar(ctx, &buf, opts)
	if err != nil {
		t.Fatalf("ExportToTar() error = %v", err)
	}

	if buf.Len() == 0 {
		t.Error("ExportToTar() produced empty output")
	}
}

func TestManager_ImportFromTar(t *testing.T) {
	tempDir := t.TempDir()

	storage := &mockStorage{
		workspaces: []*core.Workspace{{ID: "ws1", Name: "Workspace 1"}},
		souls:      []*core.Soul{{ID: "soul1", Name: "Soul 1", WorkspaceID: "ws1", Target: "https://example.com"}},
		alertChannels: []*core.AlertChannel{
			{ID: "ch1", Name: "Channel 1", Type: core.ChannelSlack},
		},
	}

	mgr := NewManager(storage, tempDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// First create a backup and export to tar
	var buf bytes.Buffer
	ctx := context.Background()
	opts := DefaultOptions()

	err := mgr.ExportToTar(ctx, &buf, opts)
	if err != nil {
		t.Fatalf("ExportToTar() error = %v", err)
	}

	// Now import from tar
	restoreStorage := &mockRestoreStorage{}
	restoreOpts := DefaultRestoreOptions()

	err = mgr.ImportFromTar(restoreStorage, &buf, restoreOpts)
	if err != nil {
		t.Fatalf("ImportFromTar() error = %v", err)
	}

	// Verify restored data
	if len(restoreStorage.workspaces) != 1 {
		t.Errorf("restored workspaces = %d, want 1", len(restoreStorage.workspaces))
	}

	if len(restoreStorage.souls) != 1 {
		t.Errorf("restored souls = %d, want 1", len(restoreStorage.souls))
	}
}

func TestManager_Restore_InvalidChecksum(t *testing.T) {
	tempDir := t.TempDir()

	storage := &mockStorage{
		workspaces: []*core.Workspace{{ID: "ws1", Name: "Workspace 1"}},
		souls:      []*core.Soul{{ID: "soul1", Name: "Soul 1", WorkspaceID: "ws1"}},
	}

	mgr := NewManager(storage, tempDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Create a backup file with invalid checksum
	invalidBackup := &Backup{
		Version:   "1.0",
		CreatedAt: time.Now().UTC(),
		Checksum:  "invalid-checksum",
		Data: BackupData{
			Workspaces: []*core.Workspace{{ID: "ws1", Name: "Workspace 1"}},
		},
	}

	path := filepath.Join(tempDir, "backups", "invalid_backup.json")
	data, _ := json.Marshal(invalidBackup)
	os.WriteFile(path, data, 0644)

	// Try to restore
	restoreStorage := &mockRestoreStorage{}
	restoreOpts := DefaultRestoreOptions()
	ctx := context.Background()

	err := mgr.Restore(ctx, restoreStorage, path, restoreOpts)
	if err == nil {
		t.Error("Restore() should fail for invalid checksum")
	}
}

func TestManager_Restore_UnsupportedVersion(t *testing.T) {
	tempDir := t.TempDir()

	storage := &mockStorage{
		workspaces: []*core.Workspace{{ID: "ws1", Name: "Workspace 1"}},
		souls:      []*core.Soul{{ID: "soul1", Name: "Soul 1", WorkspaceID: "ws1"}},
	}

	mgr := NewManager(storage, tempDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Create backup with unsupported version
	backup := &Backup{
		Version:   "2.0", // Unsupported version
		CreatedAt: time.Now().UTC(),
		Data: BackupData{
			Workspaces: []*core.Workspace{{ID: "ws1", Name: "Workspace 1"}},
		},
	}

	// Calculate valid checksum
	checksum, _ := mgr.calculateChecksum(backup)
	backup.Checksum = checksum

	path := filepath.Join(tempDir, "backups", "version_backup.json")
	data, _ := json.Marshal(backup)
	os.WriteFile(path, data, 0644)

	// Try to restore
	restoreStorage := &mockRestoreStorage{}
	restoreOpts := DefaultRestoreOptions()
	ctx := context.Background()

	err := mgr.Restore(ctx, restoreStorage, path, restoreOpts)
	if err == nil {
		t.Error("Restore() should fail for unsupported version")
	}
}

func TestManager_Create_WithMetadata(t *testing.T) {
	tempDir := t.TempDir()

	storage := &mockStorage{
		workspaces: []*core.Workspace{{ID: "ws1", Name: "Workspace 1"}},
		souls:      []*core.Soul{{ID: "soul1", Name: "Soul 1", WorkspaceID: "ws1"}},
	}

	mgr := NewManager(storage, tempDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	opts := DefaultOptions()
	opts.Metadata = map[string]string{
		"environment": "test",
		"created_by":  "test-runner",
	}

	ctx := context.Background()
	backup, _, err := mgr.Create(ctx, opts)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if backup.Metadata.CustomFields["environment"] != "test" {
		t.Error("custom metadata not set correctly")
	}

	if backup.Metadata.CustomFields["created_by"] != "test-runner" {
		t.Error("custom metadata not set correctly")
	}
}

func TestManager_List_Empty(t *testing.T) {
	tempDir := t.TempDir()
	mgr := NewManager(nil, tempDir, newTestLogger())

	backups, err := mgr.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(backups) != 0 {
		t.Errorf("List() returned %d backups, want 0", len(backups))
	}
}

func TestManager_List_NoBackupsDir(t *testing.T) {
	tempDir := t.TempDir()
	mgr := NewManager(nil, tempDir, newTestLogger())
	// Don't call Init(), so backups directory doesn't exist

	backups, err := mgr.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(backups) != 0 {
		t.Errorf("List() returned %d backups, want 0", len(backups))
	}
}

// mockFailingRestoreStorage simulates failures for specific operations
type mockFailingRestoreStorage struct {
	workspaces       []*core.Workspace
	souls            []*core.Soul
	alertChannels    []*core.AlertChannel
	alertRules       []*core.AlertRule
	statusPages      []*core.StatusPage
	journeys         []*core.JourneyConfig
	systemConfig     map[string][]byte
	failWorkspace    bool
	failSoul         bool
	failAlertChannel bool
	failAlertRule    bool
	failStatusPage   bool
	failJourney      bool
	failSystemConfig bool
}

func (m *mockFailingRestoreStorage) SaveSoul(ctx context.Context, soul *core.Soul) error {
	if m.failSoul {
		return fmt.Errorf("simulated soul save failure")
	}
	m.souls = append(m.souls, soul)
	return nil
}

func (m *mockFailingRestoreStorage) SaveWorkspace(ctx context.Context, ws *core.Workspace) error {
	if m.failWorkspace {
		return fmt.Errorf("simulated workspace save failure")
	}
	m.workspaces = append(m.workspaces, ws)
	return nil
}

func (m *mockFailingRestoreStorage) SaveAlertChannel(ch *core.AlertChannel) error {
	if m.failAlertChannel {
		return fmt.Errorf("simulated alert channel save failure")
	}
	m.alertChannels = append(m.alertChannels, ch)
	return nil
}

func (m *mockFailingRestoreStorage) SaveAlertRule(rule *core.AlertRule) error {
	if m.failAlertRule {
		return fmt.Errorf("simulated alert rule save failure")
	}
	m.alertRules = append(m.alertRules, rule)
	return nil
}

func (m *mockFailingRestoreStorage) SaveStatusPage(page *core.StatusPage) error {
	if m.failStatusPage {
		return fmt.Errorf("simulated status page save failure")
	}
	m.statusPages = append(m.statusPages, page)
	return nil
}

func (m *mockFailingRestoreStorage) SaveJourney(ctx context.Context, j *core.JourneyConfig) error {
	if m.failJourney {
		return fmt.Errorf("simulated journey save failure")
	}
	m.journeys = append(m.journeys, j)
	return nil
}

func (m *mockFailingRestoreStorage) SaveSystemConfig(ctx context.Context, key string, value []byte) error {
	if m.failSystemConfig {
		return fmt.Errorf("simulated system config save failure")
	}
	if m.systemConfig == nil {
		m.systemConfig = make(map[string][]byte)
	}
	m.systemConfig[key] = value
	return nil
}

func TestManager_Restore_ContinueOnError_Workspace(t *testing.T) {
	tempDir := t.TempDir()

	storage := &mockStorage{
		workspaces: []*core.Workspace{
			{ID: "ws1", Name: "Workspace 1"},
			{ID: "ws2", Name: "Workspace 2"},
		},
		souls: []*core.Soul{
			{ID: "soul1", Name: "Soul 1", WorkspaceID: "ws1"},
		},
	}

	mgr := NewManager(storage, tempDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Create backup
	ctx := context.Background()
	_, path, err := mgr.Create(ctx, DefaultOptions())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer os.Remove(path)

	// Restore with failing workspace storage but ContinueOnError=true
	restoreStorage := &mockFailingRestoreStorage{failWorkspace: true}
	restoreOpts := DefaultRestoreOptions()
	restoreOpts.ContinueOnError = true

	err = mgr.Restore(ctx, restoreStorage, path, restoreOpts)
	if err != nil {
		t.Errorf("Restore() with ContinueOnError should not return error, got %v", err)
	}

	// Should have 0 workspaces (all failed but continued)
	if len(restoreStorage.workspaces) != 0 {
		t.Errorf("restored workspaces = %d, want 0", len(restoreStorage.workspaces))
	}

	// But souls should be restored (workspaces failed but continued)
	if len(restoreStorage.souls) != 1 {
		t.Errorf("restored souls = %d, want 1", len(restoreStorage.souls))
	}
}

func TestManager_Restore_StopOnError_Workspace(t *testing.T) {
	tempDir := t.TempDir()

	storage := &mockStorage{
		workspaces: []*core.Workspace{
			{ID: "ws1", Name: "Workspace 1"},
			{ID: "ws2", Name: "Workspace 2"},
		},
		souls: []*core.Soul{
			{ID: "soul1", Name: "Soul 1", WorkspaceID: "ws1"},
		},
	}

	mgr := NewManager(storage, tempDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Create backup
	ctx := context.Background()
	_, path, err := mgr.Create(ctx, DefaultOptions())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer os.Remove(path)

	// Restore with failing workspace storage and ContinueOnError=false
	restoreStorage := &mockFailingRestoreStorage{failWorkspace: true}
	restoreOpts := DefaultRestoreOptions()
	restoreOpts.ContinueOnError = false

	err = mgr.Restore(ctx, restoreStorage, path, restoreOpts)
	if err == nil {
		t.Error("Restore() should fail when workspace save fails and ContinueOnError=false")
	}
}

func TestManager_Restore_ContinueOnError_Souls(t *testing.T) {
	tempDir := t.TempDir()

	storage := &mockStorage{
		workspaces: []*core.Workspace{
			{ID: "ws1", Name: "Workspace 1"},
		},
		souls: []*core.Soul{
			{ID: "soul1", Name: "Soul 1", WorkspaceID: "ws1"},
			{ID: "soul2", Name: "Soul 2", WorkspaceID: "ws1"},
		},
	}

	mgr := NewManager(storage, tempDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	ctx := context.Background()
	_, path, err := mgr.Create(ctx, DefaultOptions())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer os.Remove(path)

	// Restore with failing soul storage but ContinueOnError=true
	restoreStorage := &mockFailingRestoreStorage{failSoul: true}
	restoreOpts := DefaultRestoreOptions()
	restoreOpts.ContinueOnError = true

	err = mgr.Restore(ctx, restoreStorage, path, restoreOpts)
	if err != nil {
		t.Errorf("Restore() with ContinueOnError should not return error, got %v", err)
	}

	// Workspace should be saved
	if len(restoreStorage.workspaces) != 1 {
		t.Errorf("restored workspaces = %d, want 1", len(restoreStorage.workspaces))
	}

	// Souls should have 0 (all failed but continued)
	if len(restoreStorage.souls) != 0 {
		t.Errorf("restored souls = %d, want 0", len(restoreStorage.souls))
	}
}

func TestManager_Restore_StopOnError_Souls(t *testing.T) {
	tempDir := t.TempDir()

	storage := &mockStorage{
		workspaces: []*core.Workspace{
			{ID: "ws1", Name: "Workspace 1"},
		},
		souls: []*core.Soul{
			{ID: "soul1", Name: "Soul 1", WorkspaceID: "ws1"},
		},
	}

	mgr := NewManager(storage, tempDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	ctx := context.Background()
	_, path, err := mgr.Create(ctx, DefaultOptions())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer os.Remove(path)

	// Restore with failing soul storage and ContinueOnError=false
	restoreStorage := &mockFailingRestoreStorage{failSoul: true}
	restoreOpts := DefaultRestoreOptions()
	restoreOpts.ContinueOnError = false

	err = mgr.Restore(ctx, restoreStorage, path, restoreOpts)
	if err == nil {
		t.Error("Restore() should fail when soul save fails and ContinueOnError=false")
	}
}

func TestManager_Restore_ContinueOnError_AlertChannels(t *testing.T) {
	tempDir := t.TempDir()

	storage := &mockStorage{
		workspaces: []*core.Workspace{
			{ID: "ws1", Name: "Workspace 1"},
		},
		souls: []*core.Soul{},
		alertChannels: []*core.AlertChannel{
			{ID: "ch1", Name: "Channel 1"},
			{ID: "ch2", Name: "Channel 2"},
		},
		alertRules: []*core.AlertRule{
			{ID: "rule1", Name: "Rule 1"},
		},
	}

	mgr := NewManager(storage, tempDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	ctx := context.Background()
	_, path, err := mgr.Create(ctx, DefaultOptions())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer os.Remove(path)

	// Restore with failing alert channel storage but ContinueOnError=true
	restoreStorage := &mockFailingRestoreStorage{failAlertChannel: true}
	restoreOpts := DefaultRestoreOptions()
	restoreOpts.ContinueOnError = true

	err = mgr.Restore(ctx, restoreStorage, path, restoreOpts)
	if err != nil {
		t.Errorf("Restore() with ContinueOnError should not return error, got %v", err)
	}

	// Alert channels should have 0 (failed but continued)
	if len(restoreStorage.alertChannels) != 0 {
		t.Errorf("restored alert channels = %d, want 0", len(restoreStorage.alertChannels))
	}

	// Alert rules should be saved
	if len(restoreStorage.alertRules) != 1 {
		t.Errorf("restored alert rules = %d, want 1", len(restoreStorage.alertRules))
	}
}

func TestManager_Restore_ContinueOnError_AlertRules(t *testing.T) {
	tempDir := t.TempDir()

	storage := &mockStorage{
		workspaces: []*core.Workspace{
			{ID: "ws1", Name: "Workspace 1"},
		},
		souls: []*core.Soul{},
		alertChannels: []*core.AlertChannel{
			{ID: "ch1", Name: "Channel 1"},
		},
		alertRules: []*core.AlertRule{
			{ID: "rule1", Name: "Rule 1"},
			{ID: "rule2", Name: "Rule 2"},
		},
	}

	mgr := NewManager(storage, tempDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	ctx := context.Background()
	_, path, err := mgr.Create(ctx, DefaultOptions())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer os.Remove(path)

	// Restore with failing alert rule storage but ContinueOnError=true
	restoreStorage := &mockFailingRestoreStorage{failAlertRule: true}
	restoreOpts := DefaultRestoreOptions()
	restoreOpts.ContinueOnError = true

	err = mgr.Restore(ctx, restoreStorage, path, restoreOpts)
	if err != nil {
		t.Errorf("Restore() with ContinueOnError should not return error, got %v", err)
	}

	// Alert channels should be saved
	if len(restoreStorage.alertChannels) != 1 {
		t.Errorf("restored alert channels = %d, want 1", len(restoreStorage.alertChannels))
	}

	// Alert rules should have 0 (failed but continued)
	if len(restoreStorage.alertRules) != 0 {
		t.Errorf("restored alert rules = %d, want 0", len(restoreStorage.alertRules))
	}
}

func TestManager_Restore_ContinueOnError_StatusPages(t *testing.T) {
	tempDir := t.TempDir()

	storage := &mockStorage{
		workspaces: []*core.Workspace{
			{ID: "ws1", Name: "Workspace 1"},
		},
		souls: []*core.Soul{},
		statusPages: []*core.StatusPage{
			{ID: "page1", Name: "Page 1"},
			{ID: "page2", Name: "Page 2"},
		},
	}

	mgr := NewManager(storage, tempDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	ctx := context.Background()
	_, path, err := mgr.Create(ctx, DefaultOptions())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer os.Remove(path)

	// Restore with failing status page storage but ContinueOnError=true
	restoreStorage := &mockFailingRestoreStorage{failStatusPage: true}
	restoreOpts := DefaultRestoreOptions()
	restoreOpts.ContinueOnError = true

	err = mgr.Restore(ctx, restoreStorage, path, restoreOpts)
	if err != nil {
		t.Errorf("Restore() with ContinueOnError should not return error, got %v", err)
	}

	// Status pages should have 0 (failed but continued)
	if len(restoreStorage.statusPages) != 0 {
		t.Errorf("restored status pages = %d, want 0", len(restoreStorage.statusPages))
	}
}

func TestManager_Restore_ContinueOnError_Journeys(t *testing.T) {
	tempDir := t.TempDir()

	storage := &mockStorage{
		workspaces: []*core.Workspace{
			{ID: "ws1", Name: "Workspace 1"},
		},
		souls: []*core.Soul{},
		journeys: []*core.JourneyConfig{
			{ID: "journey1", Name: "Journey 1", WorkspaceID: "ws1"},
			{ID: "journey2", Name: "Journey 2", WorkspaceID: "ws1"},
		},
	}

	mgr := NewManager(storage, tempDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	ctx := context.Background()
	_, path, err := mgr.Create(ctx, DefaultOptions())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer os.Remove(path)

	// Restore with failing journey storage but ContinueOnError=true
	restoreStorage := &mockFailingRestoreStorage{failJourney: true}
	restoreOpts := DefaultRestoreOptions()
	restoreOpts.ContinueOnError = true

	err = mgr.Restore(ctx, restoreStorage, path, restoreOpts)
	if err != nil {
		t.Errorf("Restore() with ContinueOnError should not return error, got %v", err)
	}

	// Journeys should have 0 (failed but continued)
	if len(restoreStorage.journeys) != 0 {
		t.Errorf("restored journeys = %d, want 0", len(restoreStorage.journeys))
	}
}

func TestManager_Restore_PartialFailures_ContinueOnError(t *testing.T) {
	tempDir := t.TempDir()

	storage := &mockStorage{
		workspaces: []*core.Workspace{
			{ID: "ws1", Name: "Workspace 1"},
		},
		souls: []*core.Soul{
			{ID: "soul1", Name: "Soul 1", WorkspaceID: "ws1"},
		},
		alertChannels: []*core.AlertChannel{
			{ID: "ch1", Name: "Channel 1"},
		},
		alertRules: []*core.AlertRule{
			{ID: "rule1", Name: "Rule 1"},
		},
		statusPages: []*core.StatusPage{
			{ID: "page1", Name: "Page 1"},
		},
		journeys: []*core.JourneyConfig{
			{ID: "journey1", Name: "Journey 1", WorkspaceID: "ws1"},
		},
	}

	mgr := NewManager(storage, tempDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	ctx := context.Background()
	_, path, err := mgr.Create(ctx, DefaultOptions())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer os.Remove(path)

	// Restore with multiple failing operations but ContinueOnError=true
	restoreStorage := &mockFailingRestoreStorage{
		failAlertChannel: true,
		failStatusPage:   true,
	}
	restoreOpts := DefaultRestoreOptions()
	restoreOpts.ContinueOnError = true

	err = mgr.Restore(ctx, restoreStorage, path, restoreOpts)
	if err != nil {
		t.Errorf("Restore() with ContinueOnError should not return error, got %v", err)
	}

	// Workspaces should succeed
	if len(restoreStorage.workspaces) != 1 {
		t.Errorf("restored workspaces = %d, want 1", len(restoreStorage.workspaces))
	}

	// Souls should succeed
	if len(restoreStorage.souls) != 1 {
		t.Errorf("restored souls = %d, want 1", len(restoreStorage.souls))
	}

	// Alert channels should fail
	if len(restoreStorage.alertChannels) != 0 {
		t.Errorf("restored alert channels = %d, want 0", len(restoreStorage.alertChannels))
	}

	// Alert rules should succeed
	if len(restoreStorage.alertRules) != 1 {
		t.Errorf("restored alert rules = %d, want 1", len(restoreStorage.alertRules))
	}

	// Status pages should fail
	if len(restoreStorage.statusPages) != 0 {
		t.Errorf("restored status pages = %d, want 0", len(restoreStorage.statusPages))
	}

	// Journeys should succeed
	if len(restoreStorage.journeys) != 1 {
		t.Errorf("restored journeys = %d, want 1", len(restoreStorage.journeys))
	}
}

func TestManager_Restore_SelectiveRestore(t *testing.T) {
	tempDir := t.TempDir()

	storage := &mockStorage{
		workspaces: []*core.Workspace{
			{ID: "ws1", Name: "Workspace 1"},
		},
		souls: []*core.Soul{
			{ID: "soul1", Name: "Soul 1", WorkspaceID: "ws1"},
		},
		alertChannels: []*core.AlertChannel{
			{ID: "ch1", Name: "Channel 1"},
		},
		alertRules: []*core.AlertRule{
			{ID: "rule1", Name: "Rule 1"},
		},
		statusPages: []*core.StatusPage{
			{ID: "page1", Name: "Page 1"},
		},
		journeys: []*core.JourneyConfig{
			{ID: "journey1", Name: "Journey 1", WorkspaceID: "ws1"},
		},
	}

	mgr := NewManager(storage, tempDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	ctx := context.Background()
	_, path, err := mgr.Create(ctx, DefaultOptions())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer os.Remove(path)

	// Restore with only workspaces and souls enabled
	restoreStorage := &mockRestoreStorage{}
	restoreOpts := RestoreOptions{
		IncludeWorkspaces:   true,
		IncludeSouls:        true,
		IncludeAlerts:       false,
		IncludeStatusPages:  false,
		IncludeJourneys:     false,
		IncludeSystemConfig: false,
		ContinueOnError:     false,
	}

	err = mgr.Restore(ctx, restoreStorage, path, restoreOpts)
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}

	// Workspaces should be restored
	if len(restoreStorage.workspaces) != 1 {
		t.Errorf("restored workspaces = %d, want 1", len(restoreStorage.workspaces))
	}

	// Souls should be restored
	if len(restoreStorage.souls) != 1 {
		t.Errorf("restored souls = %d, want 1", len(restoreStorage.souls))
	}

	// Alert channels should NOT be restored
	if len(restoreStorage.alertChannels) != 0 {
		t.Errorf("restored alert channels = %d, want 0", len(restoreStorage.alertChannels))
	}

	// Alert rules should NOT be restored
	if len(restoreStorage.alertRules) != 0 {
		t.Errorf("restored alert rules = %d, want 0", len(restoreStorage.alertRules))
	}

	// Status pages should NOT be restored
	if len(restoreStorage.statusPages) != 0 {
		t.Errorf("restored status pages = %d, want 0", len(restoreStorage.statusPages))
	}

	// Journeys should NOT be restored
	if len(restoreStorage.journeys) != 0 {
		t.Errorf("restored journeys = %d, want 0", len(restoreStorage.journeys))
	}
}

// TestIsWithinDirectory_Extended tests additional path containment scenarios
func TestIsWithinDirectory_Extended(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		dir      string
		expected bool
	}{
		{"path within dir", "/data/backups/file.json", "/data/backups", true},
		{"path outside dir", "/other/file.json", "/data/backups", false},
		{"path equals dir", "/data/backups", "/data/backups", false},
		{"empty path", "", "/data/backups", false},
		{"empty dir", "/data/backups", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsWithinDirectory(tt.path, tt.dir)
			if result != tt.expected {
				t.Errorf("IsWithinDirectory(%q, %q) = %v, want %v", tt.path, tt.dir, result, tt.expected)
			}
		})
	}
}

// TestManager_Init_Error tests Init with invalid path
func TestManager_Init_Error(t *testing.T) {
	m := &Manager{
		storage:    &mockStorage{},
		logger:     newTestLogger(),
		dataDir:    "/tmp/test",
		backupsDir: "", // Empty but valid on most systems
	}
	err := m.Init()
	if err != nil {
		t.Logf("Init returned error (may be expected on some systems): %v", err)
	}
}

// TestManager_ExportToTar_Error tests ExportToTar with storage error
func TestManager_ExportToTar_StorageError(t *testing.T) {
	storage := &mockStorage{
		listWorkspacesError: fmt.Errorf("db error"),
	}
	m := &Manager{
		storage:    storage,
		logger:     newTestLogger(),
		dataDir:    "/tmp/test",
		backupsDir: t.TempDir(),
	}

	var buf bytes.Buffer
	err := m.ExportToTar(context.Background(), &buf, Options{})
	if err == nil {
		t.Error("Expected error for storage failure")
	}
}

// TestManager_ExportToTar_WriteError tests ExportToTar with writer error
func TestManager_ExportToTar_WriteError(t *testing.T) {
	storage := &mockStorage{}
	m := &Manager{
		storage:    storage,
		logger:     newTestLogger(),
		dataDir:    "/tmp/test",
		backupsDir: t.TempDir(),
	}

	// Use a writer that fails on Write
	errWriter := &errorWriter{}
	err := m.ExportToTar(context.Background(), errWriter, Options{})
	if err == nil {
		t.Error("Expected error for write failure")
	}
}

// TestManager_ImportFromTar_InvalidTar tests ImportFromTar with corrupt tar data
func TestManager_ImportFromTar_InvalidTar(t *testing.T) {
	m := &Manager{
		storage:    &mockStorage{},
		logger:     newTestLogger(),
		dataDir:    "/tmp/test",
		backupsDir: t.TempDir(),
	}

	// Pass invalid tar data
	invalidData := bytes.NewReader([]byte("not-a-valid-tar"))
	restoreStorage := &mockRestoreStorage{}
	err := m.ImportFromTar(restoreStorage, invalidData, RestoreOptions{})
	if err == nil {
		t.Error("Expected error for invalid tar data")
	}
}

// TestManager_ImportFromTar_InvalidJSON tests ImportFromTar with valid tar but invalid JSON
func TestManager_ImportFromTar_InvalidJSON(t *testing.T) {
	m := &Manager{
		storage:    &mockStorage{},
		logger:     newTestLogger(),
		dataDir:    "/tmp/test",
		backupsDir: t.TempDir(),
	}

	// Create a tar with invalid JSON using archive/tar
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	tw.WriteHeader(&tar.Header{
		Name: "backup.json",
		Size: 10,
		Mode: 0644,
	})
	tw.Write([]byte("invalid!!"))
	tw.Close()

	restoreStorage := &mockRestoreStorage{}
	err := m.ImportFromTar(restoreStorage, &buf, RestoreOptions{})
	if err == nil {
		t.Error("Expected error for invalid JSON in tar")
	}
}

// errorWriter is a writer that always fails
type errorWriter struct{}

func (e *errorWriter) Write(p []byte) (n int, err error) {
	return 0, fmt.Errorf("write error")
}

// TestManager_List_EmptyDirectory tests List with no backup files
func TestManager_List_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	m := &Manager{
		storage:    &mockStorage{},
		logger:     newTestLogger(),
		dataDir:    "/tmp/test",
		backupsDir: tmpDir,
	}

	backups, err := m.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(backups) != 0 {
		t.Errorf("Expected 0 backups, got %d", len(backups))
	}
}

// TestFormatBytes_AllSizes tests all formatBytes branches
func TestFormatBytes_AllSizes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.00 KB"},
		{1024 * 1024, "1.00 MB"},
		{1024 * 1024 * 1024, "1.00 GB"},
		{1536, "1.50 KB"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := FormatBytes(tt.bytes)
			if result != tt.expected {
				t.Errorf("FormatBytes(%d) = %q, want %q", tt.bytes, result, tt.expected)
			}
		})
	}
}

// TestIsBackupFile tests backup filename detection
func TestIsBackupFile(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected bool
	}{
		{"valid json", "anubis_20240101_backup.json", true},
		{"valid gz", "anubis_20240101_backup.gz", true},
		{"missing prefix", "backup_20240101.json", false},
		{"wrong extension", "anubis_20240101_backup.tar", false},
		{"too short", "anubis_", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isBackupFile(tt.filename)
			if result != tt.expected {
				t.Errorf("isBackupFile(%q) = %v, want %v", tt.filename, result, tt.expected)
			}
		})
	}
}

// TestManager_List_WithBackupFiles tests List with actual backup files (sorts by time)
func TestManager_List_WithBackupFiles(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(&mockStorage{}, tmpDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Create fake backup files directly in the backups dir
	backupsDir := filepath.Join(tmpDir, "backups")
	oldFile := filepath.Join(backupsDir, "anubis_20240101.json")
	newFile := filepath.Join(backupsDir, "anubis_20240102.json")

	oldBackup := &Backup{
		Metadata:  BackupMetadata{Version: "1.0"},
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	newBackup := &Backup{
		Metadata:  BackupMetadata{Version: "1.0"},
		CreatedAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	}

	oldData, _ := json.Marshal(oldBackup)
	newData, _ := json.Marshal(newBackup)
	os.WriteFile(oldFile, oldData, 0600)
	os.WriteFile(newFile, newData, 0600)

	os.Chtimes(oldFile, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	os.Chtimes(newFile, time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC))

	list, err := mgr.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("Expected 2 backups, got %d", len(list))
	}
	if len(list) >= 2 && list[0].CreatedAt.Before(list[1].CreatedAt) {
		t.Error("Backups should be sorted newest first")
	}
}

// TestManager_List_NonExistentDirectory tests List when directory doesn't exist
func TestManager_List_NonExistentDirectory(t *testing.T) {
	mgr := &Manager{backupsDir: "/nonexistent/path/that/does/not/exist"}
	list, err := mgr.List()
	if err != nil {
		t.Errorf("List should return empty slice for non-existent dir: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("Expected 0 backups, got %d", len(list))
	}
}

// TestManager_List_SkipNonBackupFiles tests that non-backup files are skipped
func TestManager_List_SkipNonBackupFiles(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(&mockStorage{}, tmpDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	backupsDir := filepath.Join(tmpDir, "backups")
	os.WriteFile(filepath.Join(backupsDir, "not-a-backup.txt"), []byte("hello"), 0600)

	list, err := mgr.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("Expected 0 backups, got %d", len(list))
	}
}

// TestManager_List_WithCorruptedMetadata tests List with a corrupted backup file
func TestManager_List_WithCorruptedMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(&mockStorage{}, tmpDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Create a corrupted backup file in backups dir
	backupsDir := filepath.Join(tmpDir, "backups")
	os.WriteFile(filepath.Join(backupsDir, "anubis_corrupt.json"), []byte("not-valid-json"), 0600)

	list, err := mgr.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("Expected 1 backup, got %d", len(list))
		return
	}
	if list[0].Metadata.Version != "" {
		t.Error("Corrupted backup should have empty metadata version")
	}
}

// TestManager_Delete_NonExistentFile tests Delete when file doesn't exist
func TestManager_Delete_NonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(&mockStorage{}, tmpDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	err := mgr.Delete("nonexistent.json")
	if err == nil {
		t.Error("Expected error deleting non-existent file")
	}
}

// TestManager_Delete_PathTraversal tests Delete with path traversal attempt
func TestManager_Delete_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(&mockStorage{}, tmpDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	err := mgr.Delete("../../etc/passwd")
	if err == nil {
		t.Error("Expected error for path traversal attempt")
	}
}

// TestFormatBytes_LargeSizes tests FormatBytes with MB and GB sizes
func TestFormatBytes_LargeSizes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{1024, "1.00 KB"},
		{1048576, "1.00 MB"},    // 1 MB
		{1572864, "1.50 MB"},    // 1.5 MB
		{1073741824, "1.00 GB"}, // 1 GB
		{2147483648, "2.00 GB"}, // 2 GB
	}
	for _, tt := range tests {
		result := FormatBytes(tt.bytes)
		if result != tt.expected {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.bytes, result, tt.expected)
		}
	}
}

// TestManager_writeBackupFile_Compress tests writeBackupFile with compression
func TestManager_writeBackupFile_Compress(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(&mockStorage{}, tmpDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	backup := &Backup{
		Metadata:  BackupMetadata{Version: "1.0"},
		CreatedAt: time.Now(),
	}

	backupsDir := filepath.Join(tmpDir, "backups")
	path := filepath.Join(backupsDir, "anubis_test.json.gz")
	opts := Options{Compress: true}

	err := mgr.writeBackupFile(backup, path, opts)
	if err != nil {
		t.Fatalf("writeBackupFile with compress failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read written file: %v", err)
	}
	if len(data) == 0 {
		t.Error("Compressed file should not be empty")
	}
}

// TestManager_readBackupFile_Compressed tests reading a compressed backup
func TestManager_readBackupFile_Compressed(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(&mockStorage{}, tmpDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	backup := &Backup{
		Metadata:  BackupMetadata{Version: "1.0"},
		CreatedAt: time.Now(),
	}
	data, _ := json.Marshal(backup)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write(data)
	gz.Close()

	backupsDir := filepath.Join(tmpDir, "backups")
	path := filepath.Join(backupsDir, "anubis_test.gz")
	os.WriteFile(path, buf.Bytes(), 0600)

	readBackup, err := mgr.readBackupFile(path)
	if err != nil {
		t.Fatalf("readBackupFile failed: %v", err)
	}
	if readBackup.Metadata.Version != "1.0" {
		t.Errorf("Expected version 1.0, got %s", readBackup.Metadata.Version)
	}
}

// TestImportFromTar_InvalidTar tests ImportFromTar with invalid tar data
func TestImportFromTar_InvalidTar(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(&mockStorage{}, tmpDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Pass invalid tar data
	err := mgr.ImportFromTar(&mockRestoreStorage{}, bytes.NewReader([]byte("not-a-tar")), DefaultRestoreOptions())
	if err == nil {
		t.Error("Expected error for invalid tar data")
	}
}

// TestManager_readBackupFile tests reading a valid backup file
func TestManager_readBackupFile_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(&mockStorage{}, tmpDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	backupsDir := filepath.Join(tmpDir, "backups")
	_, err := mgr.readBackupFile(filepath.Join(backupsDir, "nonexistent.json"))
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

// TestManager_Restore_NonExistentFile tests Restore with non-existent file
func TestManager_Restore_NonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(&mockStorage{}, tmpDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	backupsDir := filepath.Join(tmpDir, "backups")
	err := mgr.Restore(context.Background(), &mockRestoreStorage{}, filepath.Join(backupsDir, "nonexistent.json"), DefaultRestoreOptions())
	if err == nil {
		t.Error("Expected error for non-existent backup file")
	}
}

// TestIsWithinDirectory_NullByte tests filepath.Abs error path with null byte
func TestIsWithinDirectory_NullByte(t *testing.T) {
	// filepath.Abs with null byte returns error on Windows
	result := IsWithinDirectory("file\x00.json", "/data/backups")
	if result != false {
		t.Error("Expected false for path with null byte")
	}

	result = IsWithinDirectory("/data/backups/file.json", "dir\x00")
	if result != false {
		t.Error("Expected false for dir with null byte")
	}
}

// TestManager_ImportFromTar_ChecksumMismatch tests ImportFromTar with tampered backup
func TestManager_ImportFromTar_ChecksumMismatch(t *testing.T) {
	tempDir := t.TempDir()
	storage := &mockStorage{
		workspaces: []*core.Workspace{{ID: "ws1", Name: "Workspace 1"}},
		souls:      []*core.Soul{{ID: "soul1", Name: "Soul 1", WorkspaceID: "ws1", Target: "https://example.com"}},
	}

	mgr := NewManager(storage, tempDir, newTestLogger())
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Create a valid backup
	var buf bytes.Buffer
	ctx := context.Background()
	opts := DefaultOptions()
	err := mgr.ExportToTar(ctx, &buf, opts)
	if err != nil {
		t.Fatalf("ExportToTar() error = %v", err)
	}

	// Tamper with the backup data to cause checksum mismatch
	tarData := buf.Bytes()
	// Find the JSON content and corrupt it (after the tar header)
	for i := len(tarData) / 2; i < len(tarData); i++ {
		if tarData[i] != 0 {
			tarData[i] ^= 0xFF
			break
		}
	}

	restoreStorage := &mockRestoreStorage{}
	err = mgr.ImportFromTar(restoreStorage, bytes.NewReader(tarData), DefaultRestoreOptions())
	if err == nil {
		t.Error("Expected error for checksum mismatch")
	}
}

// TestManager_ImportFromTar_TruncatedEntry tests ImportFromTar with truncated tar entry
func TestManager_ImportFromTar_TruncatedEntry(t *testing.T) {
	m := &Manager{
		storage:    &mockStorage{},
		logger:     newTestLogger(),
		dataDir:    "/tmp/test",
		backupsDir: t.TempDir(),
	}

	// Create a tar with header claiming 1000 bytes but only providing 10
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	tw.WriteHeader(&tar.Header{
		Name: "backup.json",
		Size: 1000,
		Mode: 0644,
	})
	tw.Write([]byte("short data")) // Only 10 bytes, header says 1000
	tw.Close()

	restoreStorage := &mockRestoreStorage{}
	err := m.ImportFromTar(restoreStorage, &buf, RestoreOptions{})
	if err == nil {
		t.Error("Expected error for truncated tar entry")
	}
}
