package storage

import (
	"context"
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
	// Find all workspaces
	prefix := "/judgments/"
	results, err := rm.db.PrefixScan("")
	if err != nil {
		return err
	}

	deleted := 0
	for key := range results {
		if !strings.Contains(key, prefix) {
			continue
		}

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
		if judgmentTime.Before(cutoff) {
			if err := rm.db.Delete(key); err != nil {
				rm.logger.Warn("failed to delete old judgment", "key", key, "err", err)
			} else {
				deleted++
			}
		}
	}

	rm.logger.Debug("purged raw judgments", "count", deleted, "cutoff", cutoff)
	return nil
}

// purgeSummaries removes aggregated summaries older than cutoff
func (rm *RetentionManager) purgeSummaries(resolution string, cutoff time.Time) error {
	// Find all time-series summaries
	prefix := fmt.Sprintf("/ts/")
	results, err := rm.db.PrefixScan("")
	if err != nil {
		return err
	}

	deleted := 0
	for key := range results {
		if !strings.Contains(key, prefix) || !strings.Contains(key, "/"+resolution+"/") {
			continue
		}

		// Parse timestamp from key: {workspace}/ts/{soul}/{resolution}/{timestamp}
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

	rm.logger.Debug("purged summaries", "resolution", resolution, "count", deleted, "cutoff", cutoff)
	return nil
}

// GetStorageStats returns storage statistics including disk usage
func (rm *RetentionManager) GetStorageStats(ctx context.Context) (*StorageStats, error) {
	// Scan all keys
	results, err := rm.db.PrefixScan("")
	if err != nil {
		return nil, err
	}

	stats := &StorageStats{
		TotalKeys: len(results),
		KeyCounts: make(map[string]int),
		TypeSizes: make(map[string]int64),
	}

	for key, data := range results {
		// Categorize by key prefix
		category := categorizeKey(key)
		stats.KeyCounts[category]++
		stats.TypeSizes[category] += int64(len(data))
		stats.TotalSize += int64(len(data))
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

// categorizeKey returns the category for a given key
func categorizeKey(key string) string {
	if strings.Contains(key, "/souls/") {
		return "souls"
	}
	if strings.Contains(key, "/judgments/") {
		return "judgments"
	}
	if strings.Contains(key, "/ts/") {
		return "timeseries"
	}
	if strings.Contains(key, "/verdicts/") {
		return "verdicts"
	}
	if strings.Contains(key, "/journeys/") {
		return "journeys"
	}
	if strings.Contains(key, "/channels/") {
		return "channels"
	}
	if strings.Contains(key, "system/") {
		return "system"
	}
	if strings.Contains(key, "raft/") {
		return "raft"
	}
	return "other"
}
