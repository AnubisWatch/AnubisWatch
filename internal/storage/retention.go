package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// RetentionManager handles data retention and cleanup
type RetentionManager struct {
	db        *CobaltDB
	config    core.RetentionConfig
	logger    *slog.Logger
	dataPath  string
	stopCh    chan struct{}
	stoppedCh chan struct{}
}

// NewRetentionManager creates a retention manager
func NewRetentionManager(db *CobaltDB, config core.RetentionConfig, dataPath string, logger *slog.Logger) *RetentionManager {
	return &RetentionManager{
		db:        db,
		config:    config,
		dataPath:  dataPath,
		logger:    logger.With("component", "retention"),
		stopCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
	}
}

// Start starts the background retention cleanup goroutine
func (rm *RetentionManager) Start() {
	go rm.retentionLoop()
}

// Stop gracefully stops the retention manager
func (rm *RetentionManager) Stop() {
	close(rm.stopCh)
	<-rm.stoppedCh
	rm.logger.Info("retention manager stopped")
}

// retentionLoop runs retention cleanup at regular intervals
func (rm *RetentionManager) retentionLoop() {
	defer close(rm.stoppedCh)

	// Run immediately on start
	rm.runCleanup()

	// Then every hour
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rm.runCleanup()
		case <-rm.stopCh:
			return
		}
	}
}

// runCleanup performs retention cleanup for all resolutions
func (rm *RetentionManager) runCleanup() {
	rm.logger.Info("starting retention cleanup")
	start := time.Now()

	// Clean raw data
	if rm.config.Raw.Duration > 0 {
		cutoff := time.Now().Add(-rm.config.Raw.Duration)
		if err := rm.purgeRawData(cutoff); err != nil {
			rm.logger.Error("failed to purge raw data", "err", err)
		}
	}

	// Clean 1-minute summaries
	if rm.config.Minute.Duration > 0 {
		cutoff := time.Now().Add(-rm.config.Minute.Duration)
		if err := rm.purgeSummaries("1min", cutoff); err != nil {
			rm.logger.Error("failed to purge 1min summaries", "err", err)
		}
	}

	// Clean 5-minute summaries
	if rm.config.FiveMin.Duration > 0 {
		cutoff := time.Now().Add(-rm.config.FiveMin.Duration)
		if err := rm.purgeSummaries("5min", cutoff); err != nil {
			rm.logger.Error("failed to purge 5min summaries", "err", err)
		}
	}

	// Clean 1-hour summaries
	if rm.config.Hour.Duration > 0 {
		cutoff := time.Now().Add(-rm.config.Hour.Duration)
		if err := rm.purgeSummaries("1hour", cutoff); err != nil {
			rm.logger.Error("failed to purge 1hour summaries", "err", err)
		}
	}

	// Clean 1-day summaries (unless unlimited)
	if rm.config.Day != "unlimited" {
		duration, err := time.ParseDuration(rm.config.Day)
		if err == nil {
			cutoff := time.Now().Add(-duration)
			if err := rm.purgeSummaries("1day", cutoff); err != nil {
				rm.logger.Error("failed to purge 1day summaries", "err", err)
			}
		}
	}

	rm.logger.Info("retention cleanup complete", "duration", time.Since(start))
}

// purgeRawData removes raw judgments older than cutoff
func (rm *RetentionManager) purgeRawData(cutoff time.Time) error {
	// Iterate known workspaces and scan each {ws}/judgments/ prefix.
	// Avoids the previous PrefixScan("") which walked the entire B+Tree.
	workspaces := rm.db.ListWorkspaceIDs()
	if len(workspaces) == 0 {
		workspaces = []string{"default"}
	}

	deleted := 0
	for _, ws := range workspaces {
		prefix := fmt.Sprintf("%s/judgments/", ws)
		results, err := rm.db.PrefixScan(prefix)
		if err != nil {
			return err
		}

		for key := range results {
			// Parse timestamp from key: {workspace}/judgments/{soul}/{timestamp}
			parts := strings.Split(key, "/")
			if len(parts) < 4 {
				continue
			}

			ts, err := strconv.ParseInt(parts[3], 10, 64)
			if err != nil {
				continue
			}

			judgmentTime := time.Unix(0, ts)
			if !judgmentTime.Before(cutoff) {
				continue
			}

			data, err := rm.db.Get(key)
			if err != nil {
				rm.logger.Warn("failed to read old judgment", "key", key, "err", err)
				continue
			}
			var judgment core.Judgment
			if err := json.Unmarshal(data, &judgment); err != nil {
				rm.logger.Warn("failed to decode old judgment", "key", key, "err", err)
				continue
			}

			if err := rm.db.Delete(key); err != nil {
				rm.logger.Warn("failed to delete old judgment", "key", key, "err", err)
				continue
			}

			// SaveJudgment creates both a durable ID index key and an in-memory
			// lookup entry. Retention must remove both after deleting the primary.
			if judgment.ID != "" {
				idxKey := fmt.Sprintf("%s/judgment-idx/%s", ws, judgment.ID)
				if err := rm.db.Delete(idxKey); err != nil {
					return fmt.Errorf("delete judgment index %q: %w", idxKey, err)
				}
				rm.db.mu.Lock()
				delete(rm.db.judgmentIndex, judgment.ID)
				rm.db.mu.Unlock()
			}
			deleted++
		}
	}

	rm.logger.Debug("purged raw judgments", "count", deleted, "cutoff", cutoff)
	return nil
}

// purgeSummaries removes aggregated summaries older than cutoff
func (rm *RetentionManager) purgeSummaries(resolution string, cutoff time.Time) error {
	// Iterate known workspaces and scan each {ws}/ts/{resolution}/ prefix.
	// Avoids the previous PrefixScan("") which walked the entire B+Tree.
	workspaces := rm.db.ListWorkspaceIDs()
	if len(workspaces) == 0 {
		workspaces = []string{"default"}
	}

	deleted := 0
	for _, ws := range workspaces {
		prefix := fmt.Sprintf("%s/ts/", ws)
		results, err := rm.db.PrefixScan(prefix)
		if err != nil {
			return err
		}

		for key := range results {
			// Only keys for the requested resolution: {ws}/ts/{soul}/{resolution}/{ts}
			if !strings.Contains(key, "/"+resolution+"/") {
				continue
			}
			parts := strings.Split(key, "/")
			if len(parts) < 5 {
				continue
			}

			ts, err := strconv.ParseInt(parts[4], 10, 64)
			if err != nil {
				continue
			}

			summaryTime := time.Unix(ts, 0)
			if summaryTime.Before(cutoff) {
				if err := rm.db.Delete(key); err != nil {
					rm.logger.Warn("failed to delete old summary", "key", key, "err", err)
				} else {
					deleted++
				}
			}
		}
	}

	rm.logger.Debug("purged summaries", "resolution", resolution, "count", deleted, "cutoff", cutoff)
	return nil
}

// GetStorageStats returns storage statistics including disk usage
func (rm *RetentionManager) GetStorageStats(ctx context.Context) (*StorageStats, error) {
	// Iterate known workspaces and aggregate per-key stats across all of
	// their prefixes. Previously this did PrefixScan("") which loaded every
	// key-value pair into memory before categorising.
	workspaces := rm.db.ListWorkspaceIDs()
	if len(workspaces) == 0 {
		workspaces = []string{"default"}
	}

	stats := &StorageStats{
		KeyCounts: make(map[string]int),
		TypeSizes: make(map[string]int64),
	}

	// Walk each workspace's known resource prefixes. This still scans
	// every value once, but skips the workspace/<...> keys directly
	// and keeps the working set bounded per iteration.
	prefixes := []string{
		"workspaces/", // shared keyspace (not workspace-scoped)
		"system/",
		"raft/",
	}
	entityPrefixes := []string{
		"souls/",
		"judgments/",
		"ts/",
		"verdicts/",
		"journeys/",
		"journey-runs/",
		"channels/",
		"alerts/",
		"statuspages/",
		"dashboards/",
		"maintenance/",
		"workspace/", // alias kept for legacy keys if any
	}

	scan := func(prefix string) error {
		results, err := rm.db.PrefixScan(prefix)
		if err != nil {
			return err
		}
		for key, data := range results {
			category := categorizeKey(key)
			stats.KeyCounts[category]++
			stats.TypeSizes[category] += int64(len(data))
			stats.TotalSize += int64(len(data))
			stats.TotalKeys++
		}
		return nil
	}

	for _, p := range prefixes {
		if err := scan(p); err != nil {
			return nil, err
		}
	}
	for _, ws := range workspaces {
		for _, p := range entityPrefixes {
			if err := scan(ws + "/" + p); err != nil {
				return nil, err
			}
		}
	}
	// Cover legacy keys that don't carry a workspace prefix at all
	// (e.g. "/souls/workspace1/soul-1" used by some tests and older data
	// shapes). The leading slash makes the per-workspace scan above miss
	// them, so we explicitly scan a leading-slash variant of every entity
	// prefix.
	for _, p := range entityPrefixes {
		if err := scan("/" + p); err != nil {
			return nil, err
		}
	}

	// Add disk usage stats if data path is available
	if rm.dataPath != "" {
		diskStats, err := rm.getDiskUsage()
		if err != nil {
			rm.logger.Warn("failed to get disk usage", "err", err)
		} else {
			stats.DiskSize = diskStats.TotalBytes
			stats.DiskFiles = diskStats.FileCount
		}
	}

	return stats, nil
}

// diskUsageStats holds disk usage statistics
type diskUsageStats struct {
	TotalBytes int64
	FileCount  int64
}

// getDiskUsage calculates actual disk usage for the data directory
func (rm *RetentionManager) getDiskUsage() (*diskUsageStats, error) {
	stats := &diskUsageStats{}

	err := filepath.WalkDir(rm.dataPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			stats.TotalBytes += info.Size()
			stats.FileCount++
		}
		return nil
	})

	return stats, err
}

// StorageStats holds storage statistics
type StorageStats struct {
	TotalKeys int              `json:"total_keys"`
	TotalSize int64            `json:"total_size"`
	DiskSize  int64            `json:"disk_size,omitempty"`  // Actual disk usage
	DiskFiles int64            `json:"disk_files,omitempty"` // Number of files on disk
	KeyCounts map[string]int   `json:"key_counts"`
	TypeSizes map[string]int64 `json:"type_sizes"`
}

// categorizeKey returns the category for a given key. The buckets
// here MUST stay in sync with the entityPrefixes list in
// GetStorageStats (retention.go:236-249) — otherwise new resource
// types silently fall into the "other" bucket and operators can't
// see their storage footprint.
//
// Order matters: more-specific patterns come first so a key like
// `{ws}/alerts/channels/X` is bucketed as "alerts" rather than
// "channels", and `journey-runs` (hyphenated) is bucketed before
// the more general `journeys` check has a chance to miss it.
func categorizeKey(key string) string {
	switch {
	case strings.Contains(key, "/souls/"):
		return "souls"
	case strings.Contains(key, "/judgments/"):
		return "judgments"
	case strings.Contains(key, "/ts/"):
		return "timeseries"
	case strings.Contains(key, "/verdicts/"):
		return "verdicts"
	case strings.Contains(key, "/journey-runs/"):
		return "journey-runs"
	case strings.Contains(key, "/journeys/"):
		return "journeys"
	case strings.Contains(key, "/alerts/"):
		// must precede /channels/ — alerts subkeys contain /channels/
		return "alerts"
	case strings.Contains(key, "/statuspages/"):
		return "statuspages"
	case strings.Contains(key, "/dashboards/"):
		return "dashboards"
	case strings.Contains(key, "/maintenance/"):
		return "maintenance"
	case strings.Contains(key, "/channels/"):
		return "channels"
	case strings.Contains(key, "workspaces/") || strings.HasPrefix(key, "workspaces/"):
		return "workspaces"
	case strings.Contains(key, "workspace/") || strings.HasPrefix(key, "workspace/"):
		return "workspace" // legacy alias
	case strings.Contains(key, "system/"):
		return "system"
	case strings.Contains(key, "raft/"):
		return "raft"
	}
	return "other"
}
