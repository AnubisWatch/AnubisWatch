// Package backup provides backup and restore functionality for AnubisWatch data
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// RecordSource is implemented by stores that can list every raw record
// (CobaltDB does). Backups taken from one capture the whole keyspace, so
// dashboards, maintenance windows, incidents, subscriptions and anything added
// later are included without a typed section for each.
type RecordSource interface {
	PrefixScan(prefix string) (map[string][]byte, error)
}

// RecordSink is the restore-side counterpart of RecordSource.
type RecordSink interface {
	Put(key string, value []byte) error
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
	Version    string    `json:"version"`
	CreatedAt  time.Time `json:"created_at"`
	BackupType string    `json:"backup_type"` // full, incremental
	Checksum   string    `json:"checksum"`
	// Signature is an HMAC-SHA256 over the archive (checksum and signature
	// cleared), keyed from the backup key. Unlike Checksum it cannot be
	// recomputed by someone who edits the archive without the key. Archives
	// written before it existed have none and restore with a warning.
	Signature string         `json:"signature,omitempty"`
	Metadata  BackupMetadata `json:"metadata"`
	Data      BackupData     `json:"data"`
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
	Judgments     []*core.Judgment           `json:"judgments,omitempty"`
	AlertChannels []*core.AlertChannel       `json:"alert_channels"`
	AlertRules    []*core.AlertRule          `json:"alert_rules"`
	StatusPages   []*core.StatusPage         `json:"status_pages"`
	Journeys      []*core.JourneyConfig      `json:"journeys"`
	SystemConfig  map[string]json.RawMessage `json:"system_config,omitempty"`

	// Records holds every stored record by key (format 1.1). When present,
	// restore uses it instead of the typed sections above, which are kept
	// for readability and for `backup info`.
	Records map[string][]byte `json:"records,omitempty"`
	// Auth holds the local user accounts and admin password hash from
	// sessions.json. Login sessions and reset tokens are left out.
	Auth json.RawMessage `json:"auth,omitempty"`
}

const (
	backupVersionTyped   = "1.0"
	backupVersionRecords = "1.1"
)

// sessionsFile is where the local authenticator persists accounts.
const sessionsFile = "sessions.json"

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
	if err := os.MkdirAll(m.backupsDir, 0750); err != nil {
		return fmt.Errorf("failed to create backups directory: %w", err)
	}
	return nil
}

// Create performs a full backup
func (m *Manager) Create(ctx context.Context, opts Options) (*Backup, string, error) {
	m.logger.Info("Starting backup", "type", "full")

	backup := &Backup{
		Version:    backupVersionTyped,
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

	// The implicit "default" workspace is never stored as a record, so a
	// fresh install has no workspace entries at all. Collect from it anyway,
	// otherwise the backup silently contains nothing.
	workspaceIDs := backupWorkspaceIDs(workspaces)

	// Collect souls from all workspaces
	var allSouls []*core.Soul
	for _, wsID := range workspaceIDs {
		// A zero limit requests all souls; full backups must not truncate a workspace.
		souls, err := m.storage.ListSouls(ctx, wsID, 0, 0)
		if err != nil {
			m.logger.Warn("Failed to list souls for workspace", "workspace", wsID, "error", err)
			continue
		}
		allSouls = append(allSouls, souls...)
	}
	backup.Data.Souls = allSouls
	backup.Metadata.Souls = len(allSouls)

	records, hasRecords, err := m.collectRecords(opts.IncludeJudgments)
	if err != nil {
		return nil, "", fmt.Errorf("failed to collect records: %w", err)
	}
	if hasRecords {
		backup.Version = backupVersionRecords
		backup.Data.Records = records
	}
	auth, err := m.collectAuth()
	if err != nil {
		return nil, "", fmt.Errorf("failed to read user accounts: %w", err)
	}
	backup.Data.Auth = auth

	// Collect recent judgment history when the operator asked for it. The
	// storage interface declares ListJudgments for exactly this purpose; the
	// option used to be accepted and silently ignored, so a backup taken with
	// --include-history contained no history and nothing reported the omission.
	if opts.IncludeJudgments {
		days := opts.JudgmentDays
		if days <= 0 {
			days = 7
		}
		end := time.Now()
		start := end.AddDate(0, 0, -days)

		var allJudgments []*core.Judgment
		for _, soul := range allSouls {
			// ListJudgments resolves its key prefix from the workspace in the
			// context, so it must be scoped per soul. Without this the prefix
			// was always "default/judgments/<soul>/" and every non-default
			// workspace's history was silently dropped from the archive.
			soulCtx := core.ContextWithWorkspaceID(ctx, soul.WorkspaceID)
			// A zero limit requests everything inside the window, so the day
			// range the operator chose is the only bound on the history.
			judgments, err := m.storage.ListJudgments(soulCtx, soul.ID, start, end, 0)
			if err != nil {
				m.logger.Warn("Failed to list judgments for soul", "soul", soul.ID, "error", err)
				continue
			}
			allJudgments = append(allJudgments, judgments...)
		}
		backup.Data.Judgments = allJudgments
	}

	// Collect alert channels across all workspaces
	var allChannels []*core.AlertChannel
	for _, wsID := range workspaceIDs {
		channels, err := m.storage.ListAlertChannels(wsID)
		if err != nil {
			m.logger.Warn("Failed to list alert channels for workspace", "workspace", wsID, "error", err)
			continue
		}
		allChannels = append(allChannels, channels...)
	}
	backup.Data.AlertChannels = allChannels
	backup.Metadata.AlertChannels = len(allChannels)

	// Collect alert rules across all workspaces
	var allRules []*core.AlertRule
	for _, wsID := range workspaceIDs {
		rules, err := m.storage.ListAlertRules(wsID)
		if err != nil {
			m.logger.Warn("Failed to list alert rules for workspace", "workspace", wsID, "error", err)
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
	for _, wsID := range workspaceIDs {
		journeys, err := m.storage.ListJourneys(ctx, wsID)
		if err != nil {
			m.logger.Warn("Failed to list journeys for workspace", "workspace", wsID, "error", err)
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
	backupID, err := core.GenerateULID()
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate backup ID: %w", err)
	}
	filename := fmt.Sprintf("anubis_backup_%s_%s.json", timestamp, backupID.String())
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

// backupWorkspaceIDs returns the stored workspace IDs plus the implicit
// default workspace, without duplicates.
func backupWorkspaceIDs(workspaces []*core.Workspace) []string {
	ids := []string{"default"}
	seen := map[string]bool{"default": true}
	for _, ws := range workspaces {
		if ws == nil || seen[ws.ID] {
			continue
		}
		seen[ws.ID] = true
		ids = append(ids, ws.ID)
	}
	return ids
}

// Restore restores data from a backup file
func (m *Manager) Restore(ctx context.Context, storage RestoreStorage, backupPath string, opts RestoreOptions) error {
	m.logger.Info("Starting restore", "file", backupPath)

	// Read backup file
	backup, usedKey, err := m.readBackupFileWithKey(backupPath)
	if err != nil {
		return fmt.Errorf("failed to read backup file: %w", err)
	}

	// Verify checksum
	if err := m.verifyChecksum(backup); err != nil {
		return fmt.Errorf("backup verification failed: %w", err)
	}
	if err := m.verifySignature(backup, usedKey); err != nil {
		return fmt.Errorf("backup verification failed: %w", err)
	}

	// Validate backup version
	if backup.Version != backupVersionTyped && backup.Version != backupVersionRecords {
		return fmt.Errorf("unsupported backup version: %s", backup.Version)
	}

	// A store that cannot take this archive must be rejected before
	// anything is written, and in particular before the local accounts are
	// replaced: a restore that cannot succeed must not touch the node.
	sink, hasRecordSink := storage.(RecordSink)
	if len(backup.Data.Records) > 0 && !hasRecordSink {
		return fmt.Errorf("backup version %s needs a store that accepts raw records", backup.Version)
	}
	if len(backup.Data.Judgments) > 0 && opts.IncludeSouls {
		if _, ok := storage.(JudgmentSink); !ok {
			return fmt.Errorf("store cannot restore judgment history")
		}
	}

	failed := 0
	if len(backup.Data.Records) > 0 {
		failedRecords, err := m.restoreRecords(sink, backup.Data.Records, opts)
		if err != nil {
			return err
		}
		failed += failedRecords
	} else {
		failedRecords, err := m.restoreTypedData(ctx, storage, backup, opts)
		if err != nil {
			return err
		}
		failed += failedRecords
	}

	failedJudgments, err := m.restoreJudgments(ctx, storage, backup.Data.Judgments, opts)
	if err != nil {
		return err
	}
	failed += failedJudgments

	// The accounts are replaced last, and only when everything else made it
	// in. A restore that failed part way through the data has to leave the
	// existing accounts and their login sessions in place, otherwise the
	// operator loses access to the very node they are trying to recover.
	if failed > 0 {
		m.logger.Error("Restore finished with errors; the existing user accounts and sessions were kept",
			"failed_records", failed)
		return &PartialRestoreError{FailedRecords: failed}
	}
	if opts.IncludeUsers && len(backup.Data.Auth) > 0 {
		if err := m.restoreAuth(backup.Data.Auth); err != nil {
			return fmt.Errorf("failed to restore user accounts: %w", err)
		}
		m.logger.Info("Restored user accounts")
	}

	m.logger.Info("Restore completed successfully")
	return nil
}

// restoreTypedData restores the typed sections of a backup (version 1.0),
// which are used when the archive carries no raw records. It returns the
// number of records that could not be written.
func (m *Manager) restoreTypedData(ctx context.Context, storage RestoreStorage, backup *Backup, opts RestoreOptions) (int, error) {
	failed := 0
	// Restore based on options
	if opts.IncludeWorkspaces {
		for _, ws := range backup.Data.Workspaces {
			if err := storage.SaveWorkspace(ctx, ws); err != nil {
				m.logger.Warn("Failed to restore workspace", "id", ws.ID, "error", err)
				if !opts.ContinueOnError {
					return failed, fmt.Errorf("failed to restore workspace %s: %w", ws.ID, err)
				}
				failed++
			}
		}
		m.logger.Info("Restored workspaces", "count", len(backup.Data.Workspaces))
	}

	if opts.IncludeSouls {
		for _, soul := range backup.Data.Souls {
			if err := storage.SaveSoul(ctx, soul); err != nil {
				m.logger.Warn("Failed to restore soul", "id", soul.ID, "error", err)
				if !opts.ContinueOnError {
					return failed, fmt.Errorf("failed to restore soul %s: %w", soul.ID, err)
				}
				failed++
			}
		}
		m.logger.Info("Restored souls", "count", len(backup.Data.Souls))
	}

	if opts.IncludeAlerts {
		for _, ch := range backup.Data.AlertChannels {
			if err := storage.SaveAlertChannel(ch); err != nil {
				m.logger.Warn("Failed to restore alert channel", "id", ch.ID, "error", err)
				if !opts.ContinueOnError {
					return failed, fmt.Errorf("failed to restore alert channel %s: %w", ch.ID, err)
				}
				failed++
			}
		}

		for _, rule := range backup.Data.AlertRules {
			if err := storage.SaveAlertRule(rule); err != nil {
				m.logger.Warn("Failed to restore alert rule", "id", rule.ID, "error", err)
				if !opts.ContinueOnError {
					return failed, fmt.Errorf("failed to restore alert rule %s: %w", rule.ID, err)
				}
				failed++
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
					return failed, fmt.Errorf("failed to restore status page %s: %w", page.ID, err)
				}
				failed++
			}
		}
		m.logger.Info("Restored status pages", "count", len(backup.Data.StatusPages))
	}

	if opts.IncludeJourneys {
		for _, journey := range backup.Data.Journeys {
			if err := storage.SaveJourney(ctx, journey); err != nil {
				m.logger.Warn("Failed to restore journey", "id", journey.ID, "error", err)
				if !opts.ContinueOnError {
					return failed, fmt.Errorf("failed to restore journey %s: %w", journey.ID, err)
				}
				failed++
			}
		}
		m.logger.Info("Restored journeys", "count", len(backup.Data.Journeys))
	}

	if opts.IncludeSystemConfig {
		for key, value := range backup.Data.SystemConfig {
			if err := storage.SaveSystemConfig(ctx, key, value); err != nil {
				m.logger.Warn("Failed to restore system config", "key", key, "error", err)
				if !opts.ContinueOnError {
					return failed, fmt.Errorf("failed to restore system config %s: %w", key, err)
				}
				failed++
			}
		}
	}

	return failed, nil
}

// RestoreOptions contains options for restore operations
type RestoreOptions struct {
	IncludeWorkspaces   bool
	IncludeSouls        bool
	IncludeAlerts       bool
	IncludeStatusPages  bool
	IncludeJourneys     bool
	IncludeSystemConfig bool
	IncludeUsers        bool
	ContinueOnError     bool // Attempt remaining records; still return PartialRestoreError on failures.
}

// PartialRestoreError reports a completed attempt with records missing from the
// restored data. ContinueOnError controls progress, not whether failure is hidden.
type PartialRestoreError struct {
	FailedRecords int
}

func (e *PartialRestoreError) Error() string {
	return fmt.Sprintf("restore incomplete: %d record(s) failed; existing user accounts and sessions were kept", e.FailedRecords)
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
		IncludeUsers:        true,
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

	// An encrypted archive's key is stored beside it; leaving it behind keeps
	// the secret for a removed archive on disk and orphans the file.
	if err := os.Remove(path + backupKeySuffix); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("backup deleted but failed to delete its key file: %w", err)
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
	// Create a copy without checksum (or signature) for hashing
	data := *backup
	data.Checksum = ""
	data.Signature = ""

	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to serialize backup for checksum: %w", err)
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
	// Get encryption key if needed
	var encryptionKey []byte
	var err error
	if opts.Encrypt {
		encryptionKey, _, err = m.getEncryptionKey(opts)
		if err != nil {
			return fmt.Errorf("failed to get encryption key: %w", err)
		}
	}

	// Authenticate the archive. Encrypted archives are signed with the key
	// that seals them; plain archives with this node's backup key.
	signingKey := encryptionKey
	if signingKey == nil {
		signingKey, err = m.signingKey(opts)
		if err != nil {
			return fmt.Errorf("failed to get backup signing key: %w", err)
		}
	}
	backup.Signature, err = signBackup(backup, signingKey)
	if err != nil {
		return err
	}

	// Serialize backup to JSON
	jsonData, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize backup: %w", err)
	}

	// Encrypt if requested
	if opts.Encrypt && encryptionKey != nil {
		encrypted, err := encryptData(encryptionKey, jsonData)
		if err != nil {
			return fmt.Errorf("failed to encrypt backup: %w", err)
		}
		jsonData = encrypted
	}

	// Write to a private temporary file first, then rename it into place.
	// This prevents corrupted backups if the process is interrupted. The
	// archive holds user accounts and password hashes, so the temporary
	// file is created with 0600 under a unique name: a fixed name would let
	// a leftover file (or a symlink planted by another user) be written
	// through, and os.Create would hand the archive the process umask.
	// A leftover "<name>.tmp" from an earlier run is not ours, so it is
	// left completely alone.
	tmpFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	file := tmpFile
	defer os.Remove(tmpPath)

	var writer io.Writer = file
	var gzipWriter *gzip.Writer
	if opts.Compress {
		gzipWriter = gzip.NewWriter(file)
		writer = gzipWriter
	}

	if _, err := writer.Write(jsonData); err != nil {
		file.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write backup data: %w", err)
	}

	// Close gzip writer first (flush remaining data), then file
	if gzipWriter != nil {
		if closeErr := gzipWriter.Close(); closeErr != nil {
			os.Remove(tmpPath)
			return closeErr
		}
	}
	if closeErr := file.Close(); closeErr != nil {
		os.Remove(tmpPath)
		return closeErr
	}

	// The key is deliberately NOT copied next to the archive: a key stored
	// beside the archive protects nothing once the backups directory is
	// copied off the node. Older releases wrote "<archive>.key"; those
	// sidecars are still read on restore and removed by Delete.

	// Atomic rename - replaces the file if it already exists
	return os.Rename(tmpPath, path)
}

func (m *Manager) readBackupFile(path string) (*Backup, error) {
	backup, _, err := m.readBackupFileWithKey(path)
	return backup, err
}

// readBackupFileWithKey reads an archive and also returns the key that
// decrypted it (nil for a plain archive).
func (m *Manager) readBackupFileWithKey(path string) (*Backup, []byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	var reader io.Reader = file

	// Detect if compressed
	if filepath.Ext(path) == ".gz" || isGzipped(file) {
		// Reset to start for the gzip reader (isGzipped read 2 bytes)
		if _, err := file.Seek(0, 0); err != nil {
			return nil, nil, err
		}

		gzipReader, err := gzip.NewReader(file)
		if err != nil {
			return nil, nil, err
		}
		defer gzipReader.Close()
		reader = gzipReader
	}

	// Read all data
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read backup data: %w", err)
	}

	// Try to detect if this is an encrypted backup
	// Encrypted backups start with random salt, which has no fixed prefix.
	var usedKey []byte
	if len(data) > 32 && !json.Valid(data) {
		// Looks encrypted: try every key this node knows, most specific
		// first. AES-GCM rejects a wrong key, so trying several is safe.
		candidates := m.decryptionKeyCandidates(path)
		if len(candidates) == 0 {
			return nil, nil, fmt.Errorf("backup is encrypted but no key found: set %s or restore %s", backupKeyEnv, m.keyFilePath())
		}
		var lastErr error
		for _, key := range candidates {
			decrypted, derr := decryptData(key, data)
			if derr == nil {
				data, usedKey = decrypted, key
				lastErr = nil
				break
			}
			lastErr = derr
		}
		if lastErr != nil {
			return nil, nil, fmt.Errorf("failed to decrypt backup: %w", lastErr)
		}
	}

	var backup Backup
	if err := json.Unmarshal(data, &backup); err != nil {
		return nil, nil, fmt.Errorf("failed to parse backup JSON: %w", err)
	}

	return &backup, usedKey, nil
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
	// Best-effort sniff: a read/seek failure just means "not gzip",
	// which the caller handles by treating the file as plain data.
	_, _ = file.Read(buf)
	_, _ = file.Seek(0, 0)
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
	// Clean both paths to resolve any . or .. components
	absPath = filepath.Clean(absPath)
	absDir = filepath.Clean(absDir)
	// Check that absPath is a strict child of absDir (not absDir itself).
	// Using filepath.Rel ensures the separator boundary is respected,
	// preventing /data/backupsEVIL from matching /data/backups.
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil {
		return false
	}
	// Reject ".", "..", and any ".."-prefixed relative path
	return !strings.HasPrefix(rel, "..") && rel != "."
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
		return fmt.Errorf("failed to serialize backup for export: %w", err)
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

	return tw.Close()
}

// ImportFromTar imports backup data from a tar archive
func (m *Manager) ImportFromTar(storage RestoreStorage, r io.Reader, opts RestoreOptions) error {
	tr := tar.NewReader(r)

	restored := false
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

		// Create temp file for restore. G306: backups carry the
		// entire config (secrets included); 0600 keeps multi-user
		// hosts from leaking them via /tmp races.
		tempFile := filepath.Join(m.backupsDir, "temp_restore.json")
		if err := os.WriteFile(tempFile, data, 0600); err != nil {
			return err
		}

		// Restore — clean up temp file immediately (not deferred, since we're in a loop)
		ctx := context.Background()
		restoreErr := m.Restore(ctx, storage, tempFile, opts)
		os.Remove(tempFile)
		if restoreErr != nil {
			return restoreErr
		}
		restored = true
	}

	if !restored {
		return fmt.Errorf("no valid backup entries found in tar archive")
	}

	return nil
}

// Encryption key file suffix (stored alongside backup)
const backupKeySuffix = ".key"

// deriveKey derives a 32-byte AES key from key material using SHA256.
// For backup encryption, we use a simpler derivation since the key
// is already high-entropy (either from env var or generated).
func deriveKey(keyMaterial []byte) ([]byte, error) {
	hash := sha256.Sum256(keyMaterial)
	return hash[:], nil
}

// backupKeyEnv names the operator-supplied backup key. A 64-character hex
// value is decoded to 32 bytes; anything else is used as raw bytes (the only
// form older releases understood). Archives sealed with the raw form of a hex
// value still decrypt because restore tries both.
const backupKeyEnv = "ANUBIS_BACKUP_ENCRYPTION_KEY"

// backupKeyFile is the auto-generated backup key, kept in the data directory
// (outside the backups directory) so copying the backups off the node does
// not copy the key with them. legacyBackupKeyFile is where releases before
// this one wrote it; it is migrated on first use.
const (
	backupKeyFile       = "backup.key"
	legacyBackupKeyFile = ".backup_key"
)

func (m *Manager) keyFilePath() string       { return filepath.Join(m.dataDir, backupKeyFile) }
func (m *Manager) legacyKeyFilePath() string { return filepath.Join(m.backupsDir, legacyBackupKeyFile) }

// keyCandidates returns the keys a piece of key material may stand for: the
// decoded bytes of a 64-character hex string first, then the raw bytes.
func keyCandidates(material []byte) [][]byte {
	trimmed := strings.TrimSpace(string(material))
	if len(trimmed) == 64 {
		if decoded, err := hex.DecodeString(trimmed); err == nil {
			return [][]byte{decoded, material}
		}
	}
	return [][]byte{material}
}

// getEncryptionKey returns the encryption key for backups.
// If opts.EncryptionKey is set, use it.
// Otherwise check ANUBIS_BACKUP_ENCRYPTION_KEY env var.
// If neither is set, use (or generate) the node's stored backup key.
// The second result is always empty: keys are no longer copied beside
// archives; it is kept for callers of the old signature.
func (m *Manager) getEncryptionKey(opts Options) ([]byte, string, error) {
	if len(opts.EncryptionKey) > 0 {
		return opts.EncryptionKey, "", nil
	}

	// Check environment variable
	if envKey := os.Getenv(backupKeyEnv); envKey != "" {
		return keyCandidates([]byte(envKey))[0], "", nil
	}

	key, err := m.loadOrCreateStoredKey()
	if err != nil {
		return nil, "", err
	}
	return key, "", nil
}

// signingKey returns the key a plain (unencrypted) archive is signed with.
func (m *Manager) signingKey(opts Options) ([]byte, error) {
	key, _, err := m.getEncryptionKey(opts)
	return key, err
}

// loadOrCreateStoredKey returns the node's stored backup key, migrating it
// from the legacy location inside the backups directory, or generates one.
func (m *Manager) loadOrCreateStoredKey() ([]byte, error) {
	path := m.keyFilePath()
	if data, err := os.ReadFile(path); err == nil {
		return keyCandidates(data)[0], nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read backup key: %w", err)
	}

	legacy := m.legacyKeyFilePath()
	if data, err := os.ReadFile(legacy); err == nil {
		if err := writePrivateFile(path, data); err != nil {
			return nil, fmt.Errorf("failed to migrate backup key from %s: %w", legacy, err)
		}
		if err := os.Remove(legacy); err != nil {
			m.logger.Warn("backup key migrated but the old copy could not be removed", "path", legacy, "error", err)
		}
		m.logger.Info("backup key moved out of the backups directory", "from", legacy, "to", path)
		return keyCandidates(data)[0], nil
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate encryption key: %w", err)
	}
	if err := os.MkdirAll(m.dataDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to store encryption key: %w", err)
	}
	if err := writePrivateFile(path, []byte(hex.EncodeToString(key)+"\n")); err != nil {
		return nil, fmt.Errorf("failed to store encryption key: %w", err)
	}
	m.logger.Warn("backup key generated; keep a copy somewhere other than this node",
		"key_file", path,
		"action", "set "+backupKeyEnv+" to the file's contents to restore on another node")
	return key, nil
}

// storedKeyCandidates returns every key this node can read without
// generating one: environment, data-dir key file, legacy key file.
func (m *Manager) storedKeyCandidates() [][]byte {
	var out [][]byte
	if envKey := os.Getenv(backupKeyEnv); envKey != "" {
		out = append(out, keyCandidates([]byte(envKey))...)
	}
	for _, p := range []string{m.keyFilePath(), m.legacyKeyFilePath()} {
		if data, err := os.ReadFile(p); err == nil && len(data) > 0 {
			out = append(out, keyCandidates(data)...)
		}
	}
	return out
}

// decryptionKeyCandidates lists the keys to try for an encrypted archive:
// a legacy "<archive>.key" sidecar first, then the node's keys.
func (m *Manager) decryptionKeyCandidates(path string) [][]byte {
	var out [][]byte
	if data, err := os.ReadFile(path + backupKeySuffix); err == nil && len(data) > 0 {
		out = append(out, data)
	}
	return append(out, m.storedKeyCandidates()...)
}

const signaturePrefix = "hmac-sha256:"

// signBackup returns the archive's HMAC-SHA256 signature under key.
func signBackup(backup *Backup, key []byte) (string, error) {
	data := *backup
	data.Checksum = ""
	data.Signature = ""
	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to serialize backup for signature: %w", err)
	}
	macKey := sha256.Sum256(append([]byte("anubiswatch-backup-signature-v1\x00"), key...))
	mac := hmac.New(sha256.New, macKey[:])
	mac.Write(jsonData)
	return signaturePrefix + hex.EncodeToString(mac.Sum(nil)), nil
}

// verifySignature authenticates an archive. decryptKey is the key that opened
// an encrypted archive (AES-GCM already authenticated it). A plain archive is
// checked against every key this node holds; when none is available, or the
// archive predates signatures, the restore proceeds with a warning because
// its integrity rests on an unkeyed checksum anyone can recompute.
func (m *Manager) verifySignature(backup *Backup, decryptKey []byte) error {
	if backup.Signature == "" {
		if decryptKey == nil {
			m.logger.Warn("backup is unsigned and unencrypted: its integrity is NOT authenticated; only restore archives you trust")
		}
		return nil
	}
	keys := [][]byte{decryptKey}
	if decryptKey == nil {
		keys = m.storedKeyCandidates()
	}
	if len(keys) == 0 {
		m.logger.Warn("backup signature cannot be verified on this node (no backup key); its integrity is NOT authenticated",
			"action", "set "+backupKeyEnv+" to the key of the node that wrote it")
		return nil
	}
	for _, key := range keys {
		sig, err := signBackup(backup, key)
		if err != nil {
			return err
		}
		if hmac.Equal([]byte(sig), []byte(backup.Signature)) {
			return nil
		}
	}
	return fmt.Errorf("backup signature does not match: the archive was modified or signed with a different backup key")
}

// writePrivateFile atomically writes data to path with mode 0600.
func writePrivateFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// encryptData encrypts data using AES-256-GCM.
// Returns: salt(32) + nonce(12) + ciphertext+tag.
func encryptData(key []byte, plaintext []byte) ([]byte, error) {
	// Generate random nonce
	nonce := make([]byte, 12) // AES-GCM standard nonce size
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Generate random salt
	salt := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	// Derive key with salt for this specific encryption
	saltKey := make([]byte, len(key)+len(salt))
	copy(saltKey, key)
	copy(saltKey[len(key):], salt)
	derivedKey, err := deriveKey(saltKey)
	if err != nil {
		return nil, err
	}
	defer func() { derivedKey[0] = 0 }()

	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Encrypt with derived key
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// Format: salt(32) + nonce(12) + ciphertext+tag
	result := make([]byte, len(salt)+len(nonce)+len(ciphertext))
	copy(result, salt)
	copy(result[len(salt):], nonce)
	copy(result[len(salt)+len(nonce):], ciphertext)
	return result, nil
}

// decryptData decrypts data encrypted with encryptData.
// Expects format: salt(32) + nonce(12) + ciphertext+tag.
func decryptData(key []byte, data []byte) ([]byte, error) {
	if len(data) < 32+12+16 {
		return nil, fmt.Errorf("encrypted data too short")
	}

	salt := data[:32]
	nonce := data[32:44]
	ciphertext := data[44:]

	// Re-derive key with salt
	saltKey := make([]byte, len(key)+len(salt))
	copy(saltKey, key)
	copy(saltKey[len(key):], salt)
	derivedKey, err := deriveKey(saltKey)
	if err != nil {
		return nil, err
	}
	defer func() { derivedKey[0] = 0 }()

	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}

	return plaintext, nil
}

// GetStoredEncryptionKey returns the encryption key stored in the key file.
// With an empty keyFile it reads the data-directory key, falling back to the
// legacy location inside the backups directory. A 64-character hex file is
// decoded.
func (m *Manager) GetStoredEncryptionKey(keyFile string) ([]byte, error) {
	paths := []string{keyFile}
	if keyFile == "" {
		paths = []string{m.keyFilePath(), m.legacyKeyFilePath()}
	}
	var lastErr error
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			lastErr = err
			continue
		}
		return keyCandidates(data)[0], nil
	}
	return nil, fmt.Errorf("failed to read encryption key: %w", lastErr)
}
