// Package backup provides backup and restore functionality for AnubisWatch data
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Manager handles backup and restore operations
type Manager struct {
	storage    BackupStorage
	logger     *slog.Logger
	dataDir    string
	backupsDir string
}

// BackupStorage interface for data access
type BackupStorage interface {
	// Souls
	ListSouls(ctx context.Context, workspaceID string, offset, limit int) ([]*core.Soul, error)
	ListWorkspaces(ctx context.Context) ([]*core.Workspace, error)

	// Alerting
	ListAlertChannels(workspaceID string) ([]*core.AlertChannel, error)
	ListAlertRules(workspaceID string) ([]*core.AlertRule, error)

	// Status Pages
	ListStatusPages() ([]*core.StatusPage, error)

	// Journeys
	ListJourneys(ctx context.Context, workspaceID string) ([]*core.JourneyConfig, error)

	// Judgments (limited recent history)
	ListJudgments(ctx context.Context, soulID string, start, end time.Time, limit int) ([]*core.Judgment, error)

	// System config
	GetSystemConfig(ctx context.Context, key string) ([]byte, error)
}

// RestoreStorage interface for restoring data
type RestoreStorage interface {
	SaveSoul(ctx context.Context, soul *core.Soul) error
	SaveWorkspace(ctx context.Context, ws *core.Workspace) error
	SaveAlertChannel(ch *core.AlertChannel) error
	SaveAlertRule(rule *core.AlertRule) error
	SaveStatusPage(page *core.StatusPage) error
	SaveJourney(ctx context.Context, j *core.JourneyConfig) error
	SaveSystemConfig(ctx context.Context, key string, value []byte) error
}

// Backup represents a complete system backup
//
// NOTE: Backup scope does NOT include the following:
//   - WAL files and raw data files (not needed for restore)
//   - Dashboard assets (rebuilt from source on restore)
//
// After restoring from backup, you must re-configure:
//   - Alert channel credentials that reference external APIs
//   - OIDC client secrets (stored in config file)
type Backup struct {
	Version    string         `json:"version"`
	CreatedAt  time.Time      `json:"created_at"`
	BackupType string         `json:"backup_type"` // full, incremental
	Checksum   string         `json:"checksum"`
	Metadata   BackupMetadata `json:"metadata"`
	Data       BackupData     `json:"data"`
}

// BackupMetadata contains backup information
type BackupMetadata struct {
	NodeID        string            `json:"node_id,omitempty"`
	ClusterID     string            `json:"cluster_id,omitempty"`
	Version       string            `json:"anubis_version"`
	Workspaces    int               `json:"workspaces_count"`
	Souls         int               `json:"souls_count"`
	AlertChannels int               `json:"alert_channels_count"`
	AlertRules    int               `json:"alert_rules_count"`
	StatusPages   int               `json:"status_pages_count"`
	Journeys      int               `json:"journeys_count"`
	CustomFields  map[string]string `json:"custom_fields,omitempty"`
}

// BackupData contains all backed up data
type BackupData struct {
	Workspaces    []*core.Workspace          `json:"workspaces"`
	Souls         []*core.Soul               `json:"souls"`
	AlertChannels []*core.AlertChannel       `json:"alert_channels"`
	AlertRules    []*core.AlertRule          `json:"alert_rules"`
	StatusPages   []*core.StatusPage         `json:"status_pages"`
	Journeys      []*core.JourneyConfig      `json:"journeys"`
	SystemConfig  map[string]json.RawMessage `json:"system_config,omitempty"`
}

// Options for backup operations
type Options struct {
	IncludeJudgments bool              // Include recent judgment history
	JudgmentDays     int               // How many days of judgment history to include
	Compress         bool              // Compress the backup
	Encrypt          bool              // Encrypt the backup
	EncryptionKey    []byte            // Encryption key (if encrypting)
	Metadata         map[string]string // Custom metadata
}

// DefaultOptions returns default backup options
func DefaultOptions() Options {
	return Options{
		IncludeJudgments: true,
		JudgmentDays:     7,
		Compress:         true,
		Encrypt:          false,
	}
}

// NewManager creates a new backup manager
func NewManager(storage BackupStorage, dataDir string, logger *slog.Logger) *Manager {
	backupsDir := filepath.Join(dataDir, "backups")

	return &Manager{
		storage:    storage,
		logger:     logger.With("component", "backup"),
		dataDir:    dataDir,
		backupsDir: backupsDir,
	}
}

// Init initializes the backup manager (creates directories)
func (m *Manager) Init() error {
	if err := os.MkdirAll(m.backupsDir, 0755); err != nil {
		return fmt.Errorf("failed to create backups directory: %w", err)
	}
	return nil
}

// Create performs a full backup
func (m *Manager) Create(ctx context.Context, opts Options) (*Backup, string, error) {
	m.logger.Info("Starting backup", "type", "full")

	backup := &Backup{
		Version:    "1.0",
		CreatedAt:  time.Now().UTC(),
		BackupType: "full",
		Data:       BackupData{},
	}

	// Collect workspaces
	workspaces, err := m.storage.ListWorkspaces(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to list workspaces: %w", err)
	}
	backup.Data.Workspaces = workspaces
	backup.Metadata.Workspaces = len(workspaces)

	// Collect souls from all workspaces
	var allSouls []*core.Soul
	for _, ws := range workspaces {
		souls, err := m.storage.ListSouls(ctx, ws.ID, 0, 10000)
		if err != nil {
			m.logger.Warn("Failed to list souls for workspace", "workspace", ws.ID, "error", err)
			continue
		}
		allSouls = append(allSouls, souls...)
	}
	backup.Data.Souls = allSouls
	backup.Metadata.Souls = len(allSouls)

	// Collect alert channels across all workspaces
	var allChannels []*core.AlertChannel
	for _, ws := range workspaces {
		channels, err := m.storage.ListAlertChannels(ws.ID)
		if err != nil {
			m.logger.Warn("Failed to list alert channels for workspace", "workspace", ws.ID, "error", err)
			continue
		}
		allChannels = append(allChannels, channels...)
	}
	backup.Data.AlertChannels = allChannels
	backup.Metadata.AlertChannels = len(allChannels)

	// Collect alert rules across all workspaces
	var allRules []*core.AlertRule
	for _, ws := range workspaces {
		rules, err := m.storage.ListAlertRules(ws.ID)
		if err != nil {
			m.logger.Warn("Failed to list alert rules for workspace", "workspace", ws.ID, "error", err)
			continue
		}
		allRules = append(allRules, rules...)
	}
	backup.Data.AlertRules = allRules
	backup.Metadata.AlertRules = len(allRules)

	// Collect status pages
	pages, err := m.storage.ListStatusPages()
	if err != nil {
		m.logger.Warn("Failed to list status pages", "error", err)
	} else {
		backup.Data.StatusPages = pages
		backup.Metadata.StatusPages = len(pages)
	}

	// Collect journeys from all workspaces
	var allJourneys []*core.JourneyConfig
	for _, ws := range workspaces {
		journeys, err := m.storage.ListJourneys(ctx, ws.ID)
		if err != nil {
			m.logger.Warn("Failed to list journeys for workspace", "workspace", ws.ID, "error", err)
			continue
		}
		allJourneys = append(allJourneys, journeys...)
	}
	backup.Data.Journeys = allJourneys
	backup.Metadata.Journeys = len(allJourneys)

	// Collect system config
	backup.Data.SystemConfig = make(map[string]json.RawMessage)
	configKeys := []string{"cluster", "raft", "settings"}
	for _, key := range configKeys {
		data, err := m.storage.GetSystemConfig(ctx, key)
		if err == nil && data != nil {
			backup.Data.SystemConfig[key] = data
		}
	}

	// Set custom metadata
	backup.Metadata.CustomFields = opts.Metadata

	// Calculate checksum
	checksum, err := m.calculateChecksum(backup)
	if err != nil {
		return nil, "", fmt.Errorf("failed to calculate checksum: %w", err)
	}
	backup.Checksum = checksum

	// Generate filename
	timestamp := backup.CreatedAt.Format("20060102_150405")
	filename := fmt.Sprintf("anubis_backup_%s.json", timestamp)
	if opts.Compress {
		filename += ".gz"
	}
	filepath := filepath.Join(m.backupsDir, filename)

	// Write backup file
	if err := m.writeBackupFile(backup, filepath, opts); err != nil {
		return nil, "", fmt.Errorf("failed to write backup file: %w", err)
	}

	m.logger.Info("Backup completed",
		"file", filename,
		"workspaces", backup.Metadata.Workspaces,
		"souls", backup.Metadata.Souls,
		"channels", backup.Metadata.AlertChannels,
		"rules", backup.Metadata.AlertRules,
	)

	return backup, filepath, nil
}

// Restore restores data from a backup file
func (m *Manager) Restore(ctx context.Context, storage RestoreStorage, backupPath string, opts RestoreOptions) error {
	m.logger.Info("Starting restore", "file", backupPath)

	// Read backup file
	backup, err := m.readBackupFile(backupPath)
	if err != nil {
		return fmt.Errorf("failed to read backup file: %w", err)
	}

	// Verify checksum
	if err := m.verifyChecksum(backup); err != nil {
		return fmt.Errorf("backup verification failed: %w", err)
	}

	// Validate backup version
	if backup.Version != "1.0" {
		return fmt.Errorf("unsupported backup version: %s", backup.Version)
	}

	// Restore based on options
	if opts.IncludeWorkspaces {
		for _, ws := range backup.Data.Workspaces {
			if err := storage.SaveWorkspace(ctx, ws); err != nil {
				m.logger.Warn("Failed to restore workspace", "id", ws.ID, "error", err)
				if !opts.ContinueOnError {
					return fmt.Errorf("failed to restore workspace %s: %w", ws.ID, err)
				}
			}
		}
		m.logger.Info("Restored workspaces", "count", len(backup.Data.Workspaces))
	}

	if opts.IncludeSouls {
		for _, soul := range backup.Data.Souls {
			if err := storage.SaveSoul(ctx, soul); err != nil {
				m.logger.Warn("Failed to restore soul", "id", soul.ID, "error", err)
				if !opts.ContinueOnError {
					return fmt.Errorf("failed to restore soul %s: %w", soul.ID, err)
				}
			}
		}
		m.logger.Info("Restored souls", "count", len(backup.Data.Souls))
	}

	if opts.IncludeAlerts {
		for _, ch := range backup.Data.AlertChannels {
			if err := storage.SaveAlertChannel(ch); err != nil {
				m.logger.Warn("Failed to restore alert channel", "id", ch.ID, "error", err)
				if !opts.ContinueOnError {
					return fmt.Errorf("failed to restore alert channel %s: %w", ch.ID, err)
				}
			}
		}

		for _, rule := range backup.Data.AlertRules {
			if err := storage.SaveAlertRule(rule); err != nil {
				m.logger.Warn("Failed to restore alert rule", "id", rule.ID, "error", err)
				if !opts.ContinueOnError {
					return fmt.Errorf("failed to restore alert rule %s: %w", rule.ID, err)
				}
			}
		}
		m.logger.Info("Restored alerts",
			"channels", len(backup.Data.AlertChannels),
			"rules", len(backup.Data.AlertRules))
	}

	if opts.IncludeStatusPages {
		for _, page := range backup.Data.StatusPages {
			if err := storage.SaveStatusPage(page); err != nil {
				m.logger.Warn("Failed to restore status page", "id", page.ID, "error", err)
				if !opts.ContinueOnError {
					return fmt.Errorf("failed to restore status page %s: %w", page.ID, err)
				}
			}
		}
		m.logger.Info("Restored status pages", "count", len(backup.Data.StatusPages))
	}

	if opts.IncludeJourneys {
		for _, journey := range backup.Data.Journeys {
			if err := storage.SaveJourney(ctx, journey); err != nil {
				m.logger.Warn("Failed to restore journey", "id", journey.ID, "error", err)
				if !opts.ContinueOnError {
					return fmt.Errorf("failed to restore journey %s: %w", journey.ID, err)
				}
			}
		}
		m.logger.Info("Restored journeys", "count", len(backup.Data.Journeys))
	}

	if opts.IncludeSystemConfig {
		for key, value := range backup.Data.SystemConfig {
			if err := storage.SaveSystemConfig(ctx, key, value); err != nil {
				m.logger.Warn("Failed to restore system config", "key", key, "error", err)
			}
		}
	}

	m.logger.Info("Restore completed successfully")
	return nil
}

// RestoreOptions contains options for restore operations
type RestoreOptions struct {
	IncludeWorkspaces   bool
	IncludeSouls        bool
	IncludeAlerts       bool
	IncludeStatusPages  bool
	IncludeJourneys     bool
	IncludeSystemConfig bool
	ContinueOnError     bool
}

// DefaultRestoreOptions returns default restore options (restore everything)
func DefaultRestoreOptions() RestoreOptions {
	return RestoreOptions{
		IncludeWorkspaces:   true,
		IncludeSouls:        true,
		IncludeAlerts:       true,
		IncludeStatusPages:  true,
		IncludeJourneys:     true,
		IncludeSystemConfig: true,
		ContinueOnError:     false,
	}
}

// List returns a list of available backups
func (m *Manager) List() ([]BackupInfo, error) {
	entries, err := os.ReadDir(m.backupsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []BackupInfo{}, nil
		}
		return nil, fmt.Errorf("failed to read backups directory: %w", err)
	}

	var backups []BackupInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !isBackupFile(name) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Try to read metadata from backup
		path := filepath.Join(m.backupsDir, name)
		metadata := m.readBackupMetadata(path)

		backups = append(backups, BackupInfo{
			Filename:  name,
			Path:      path,
			Size:      info.Size(),
			CreatedAt: info.ModTime(),
			Metadata:  metadata,
		})
	}

	// Sort by creation time (newest first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})

	return backups, nil
}

// BackupInfo contains information about a backup file
type BackupInfo struct {
	Filename  string         `json:"filename"`
	Path      string         `json:"path"`
	Size      int64          `json:"size"`
	CreatedAt time.Time      `json:"created_at"`
	Metadata  BackupMetadata `json:"metadata,omitempty"`
}

// Delete removes a backup file
func (m *Manager) Delete(filename string) error {
	path := filepath.Join(m.backupsDir, filename)

	// Security check: ensure file is within backups directory
	if !isWithinDirectory(path, m.backupsDir) {
		return fmt.Errorf("invalid backup path")
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to delete backup: %w", err)
	}

	m.logger.Info("Backup deleted", "file", filename)
	return nil
}

// Get retrieves a backup by filename
func (m *Manager) Get(filename string) (*Backup, error) {
	path := filepath.Join(m.backupsDir, filename)

	// Security check
	if !isWithinDirectory(path, m.backupsDir) {
		return nil, fmt.Errorf("invalid backup path")
	}

	return m.readBackupFile(path)
}

// Helper methods

func (m *Manager) calculateChecksum(backup *Backup) (string, error) {
	// Create a copy without checksum for hashing
	data := *backup
	data.Checksum = ""

	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	hash := sha256.Sum256(jsonData)
	return hex.EncodeToString(hash[:]), nil
}

func (m *Manager) verifyChecksum(backup *Backup) error {
	expected := backup.Checksum
	actual, err := m.calculateChecksum(backup)
	if err != nil {
		return err
	}

	if expected != actual {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, actual)
	}

	return nil
}

func (m *Manager) writeBackupFile(backup *Backup, path string, opts Options) error {
	// Write to a temp file first for atomic backup creation
	// This prevents corrupted backups if the process is interrupted
	tmpPath := path + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	var writer io.Writer = file
	var gzipWriter *gzip.Writer
	if opts.Compress {
		gzipWriter = gzip.NewWriter(file)
		writer = gzipWriter
	}

	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(backup)

	// Close gzip writer first (flush remaining data), then file
	if gzipWriter != nil {
		if closeErr := gzipWriter.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	if closeErr := file.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(tmpPath) // Clean up temp file
		return err
	}

	// Atomic rename - replaces the file if it already exists
	return os.Rename(tmpPath, path)
}

func (m *Manager) readBackupFile(path string) (*Backup, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var reader io.Reader = file

	// Detect if compressed
	if filepath.Ext(path) == ".gz" || isGzipped(file) {
		// Need to reopen for gzip
		file.Close()
		file, err = os.Open(path)
		if err != nil {
			return nil, err
		}
		defer file.Close()

		gzipReader, err := gzip.NewReader(file)
		if err != nil {
			return nil, err
		}
		defer gzipReader.Close()
		reader = gzipReader
	}

	var backup Backup
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(&backup); err != nil {
		return nil, err
	}

	return &backup, nil
}

func (m *Manager) readBackupMetadata(path string) BackupMetadata {
	backup, err := m.readBackupFile(path)
	if err != nil {
		return BackupMetadata{}
	}
	return backup.Metadata
}

func isBackupFile(name string) bool {
	return len(name) > 7 && name[:7] == "anubis_" && (filepath.Ext(name) == ".json" || filepath.Ext(name) == ".gz")
}

func isGzipped(file *os.File) bool {
	buf := make([]byte, 2)
	file.Read(buf)
	file.Seek(0, 0)
	return buf[0] == 0x1f && buf[1] == 0x8b
}

func isWithinDirectory(path, dir string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	return len(absPath) > len(absDir) && absPath[:len(absDir)] == absDir
}

// IsWithinDirectory checks if a path is within a directory (exported for testing)
func IsWithinDirectory(path, dir string) bool {
	return isWithinDirectory(path, dir)
}

// FormatBytes formats byte size to human-readable string (exported for testing)
func FormatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// ExportToTar exports backup data to a tar archive
func (m *Manager) ExportToTar(ctx context.Context, w io.Writer, opts Options) error {
	tw := tar.NewWriter(w)
	defer tw.Close()

	// Create backup in memory
	backup, _, err := m.Create(ctx, opts)
	if err != nil {
		return err
	}

	// Serialize backup
	data, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		return err
	}

	// Write to tar
	header := &tar.Header{
		Name:    fmt.Sprintf("anubis_backup_%s.json", backup.CreatedAt.Format("20060102_150405")),
		Size:    int64(len(data)),
		Mode:    0644,
		ModTime: backup.CreatedAt,
	}

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	if _, err := tw.Write(data); err != nil {
		return err
	}

	return nil
}

// ImportFromTar imports backup data from a tar archive
func (m *Manager) ImportFromTar(storage RestoreStorage, r io.Reader, opts RestoreOptions) error {
	tr := tar.NewReader(r)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if filepath.Ext(header.Name) != ".json" {
			continue
		}

		// Read backup data
		data := make([]byte, header.Size)
		if _, err := io.ReadFull(tr, data); err != nil {
			return err
		}

		var backup Backup
		if err := json.Unmarshal(data, &backup); err != nil {
			return err
		}

		// Verify checksum
		if err := m.verifyChecksum(&backup); err != nil {
			return err
		}

		// Create temp file for restore
		tempFile := filepath.Join(m.backupsDir, "temp_restore.json")
		if err := os.WriteFile(tempFile, data, 0644); err != nil {
			return err
		}
		defer os.Remove(tempFile)

		// Restore
		ctx := context.Background()
		return m.Restore(ctx, storage, tempFile, opts)
	}

	return nil
}
