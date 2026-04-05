package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TimeSeriesStore provides optimized time-series storage
type TimeSeriesStore struct {
	db     *CobaltDB
	config core.TimeSeriesConfig
	logger *slog.Logger
}

// TimeResolution represents different time granularities
type TimeResolution string

const (
	ResolutionRaw  TimeResolution = "raw"
	Resolution1Min TimeResolution = "1min"
	Resolution5Min TimeResolution = "5min"
	Resolution1Hour TimeResolution = "1hour"
	Resolution1Day TimeResolution = "1day"
)

// JudgmentSummary aggregates multiple judgments into a time bucket
type JudgmentSummary struct {
	SoulID          string    `json:"soul_id"`
	WorkspaceID     string    `json:"workspace_id"`
	Resolution      string    `json:"resolution"`
	BucketTime      time.Time `json:"bucket_time"`
	Count           int       `json:"count"`
	SuccessCount    int       `json:"success_count"`
	FailureCount    int       `json:"failure_count"`
	MinLatency      float64   `json:"min_latency_ms"`
	MaxLatency      float64   `json:"max_latency_ms"`
	AvgLatency      float64   `json:"avg_latency_ms"`
	P50Latency      float64   `json:"p50_latency_ms"`
	P95Latency      float64   `json:"p95_latency_ms"`
	P99Latency      float64   `json:"p99_latency_ms"`
	UptimePercent   float64   `json:"uptime_percent"`
	PacketLossAvg   float64   `json:"packet_loss_avg,omitempty"`
}

// NewTimeSeriesStore creates a time-series store
func NewTimeSeriesStore(db *CobaltDB, config core.TimeSeriesConfig, logger *slog.Logger) *TimeSeriesStore {
	return &TimeSeriesStore{
		db:     db,
		config: config,
		logger: logger.With("component", "timeseries"),
	}
}

// SaveJudgment saves a judgment and updates summaries
func (ts *TimeSeriesStore) SaveJudgment(ctx context.Context, j *core.Judgment) error {
	// Save raw judgment first
	if err := ts.db.SaveJudgment(ctx, j); err != nil {
		return err
	}

	// Update 1-minute summary
	if err := ts.updateSummary(ctx, j, Resolution1Min); err != nil {
		ts.logger.Warn("failed to update 1min summary", "err", err)
	}

	return nil
}

// updateSummary updates the aggregated summary for a judgment
func (ts *TimeSeriesStore) updateSummary(ctx context.Context, j *core.Judgment, resolution TimeResolution) error {
	workspaceID := "default" // TODO: Extract from soul

	// Calculate bucket time
	bucketTime := truncateToResolution(j.Timestamp, resolution)

	key := fmt.Sprintf("%s/ts/%s/%s/%d", workspaceID, j.SoulID, resolution, bucketTime.Unix())

	// Get existing summary
	var summary JudgmentSummary
	data, err := ts.db.Get(key)
	if err == nil {
		// Update existing
		if err := json.Unmarshal(data, &summary); err != nil {
			ts.logger.Warn("failed to unmarshal summary", "err", err)
		}
	}

	// Update summary
	latencyMs := float64(j.Duration) / float64(time.Millisecond)

	summary.SoulID = j.SoulID
	summary.WorkspaceID = workspaceID
	summary.Resolution = string(resolution)
	summary.BucketTime = bucketTime
	summary.Count++

	if j.Status == core.SoulAlive {
		summary.SuccessCount++
	} else {
		summary.FailureCount++
	}

	// Update latency stats
	if summary.Count == 1 {
		summary.MinLatency = latencyMs
		summary.MaxLatency = latencyMs
		summary.AvgLatency = latencyMs
	} else {
		summary.MinLatency = math.Min(summary.MinLatency, latencyMs)
		summary.MaxLatency = math.Max(summary.MaxLatency, latencyMs)
		summary.AvgLatency = ((summary.AvgLatency * float64(summary.Count-1)) + latencyMs) / float64(summary.Count)
	}

	// Calculate uptime percentage
	summary.UptimePercent = float64(summary.SuccessCount) / float64(summary.Count) * 100

	// Update packet loss if available
	if j.Details != nil && j.Details.PacketLoss > 0 {
		summary.PacketLossAvg = ((summary.PacketLossAvg * float64(summary.Count-1)) + j.Details.PacketLoss) / float64(summary.Count)
	}

	// Save updated summary
	newData, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("failed to marshal summary: %w", err)
	}

	return ts.db.Put(key, newData)
}

// QuerySummaries retrieves aggregated summaries for a time range
func (ts *TimeSeriesStore) QuerySummaries(ctx context.Context, workspaceID, soulID string, resolution TimeResolution, start, end time.Time) ([]*JudgmentSummary, error) {
	if workspaceID == "" {
		workspaceID = "default"
	}

	startKey := fmt.Sprintf("%s/ts/%s/%s/%d", workspaceID, soulID, resolution, truncateToResolution(start, resolution).Unix())
	endKey := fmt.Sprintf("%s/ts/%s/%s/%d", workspaceID, soulID, resolution, truncateToResolution(end, resolution).Unix()+1)

	results, err := ts.db.RangeScan(startKey, endKey)
	if err != nil {
		return nil, err
	}

	summaries := make([]*JudgmentSummary, 0, len(results))
	for _, data := range results {
		if data == nil {
			continue
		}
		var summary JudgmentSummary
		if err := json.Unmarshal(data, &summary); err != nil {
			ts.logger.Warn("failed to unmarshal summary", "err", err)
			continue
		}
		summaries = append(summaries, &summary)
	}

	return summaries, nil
}

// GetPurityFromSummaries calculates uptime from summaries (faster than raw)
func (ts *TimeSeriesStore) GetPurityFromSummaries(ctx context.Context, workspaceID, soulID string, window time.Duration) (float64, error) {
	end := time.Now()
	start := end.Add(-window)

	summaries, err := ts.QuerySummaries(ctx, workspaceID, soulID, Resolution1Min, start, end)
	if err != nil {
		return 0, err
	}

	if len(summaries) == 0 {
		return 0, nil
	}

	totalCount := 0
	successCount := 0
	for _, s := range summaries {
		totalCount += s.Count
		successCount += s.SuccessCount
	}

	if totalCount == 0 {
		return 0, nil
	}

	return float64(successCount) / float64(totalCount) * 100, nil
}

// truncateToResolution rounds a time down to the resolution boundary
func truncateToResolution(t time.Time, resolution TimeResolution) time.Time {
	switch resolution {
	case Resolution1Min:
		return t.Truncate(time.Minute)
	case Resolution5Min:
		return t.Truncate(5 * time.Minute)
	case Resolution1Hour:
		return t.Truncate(time.Hour)
	case Resolution1Day:
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	default:
		return t
	}
}

// StartCompaction starts the background compaction goroutine
func (ts *TimeSeriesStore) StartCompaction() {
	go ts.compactionLoop()
}

// compactionLoop runs compaction at regular intervals
func (ts *TimeSeriesStore) compactionLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := ts.runCompaction(); err != nil {
				ts.logger.Error("compaction failed", "err", err)
			}
		}
	}
}

// runCompaction compacts old data to coarser resolutions
func (ts *TimeSeriesStore) runCompaction() error {
	ts.logger.Debug("starting compaction")

	// Compact raw -> 1-minute summaries
	if err := ts.compactToResolution(ResolutionRaw, Resolution1Min, ts.config.Compaction.RawToMinute.Duration); err != nil {
		ts.logger.Warn("failed to compact to 1min", "err", err)
	}

	// Compact 1-minute -> 5-minute summaries
	if err := ts.compactToResolution(Resolution1Min, Resolution5Min, ts.config.Compaction.MinuteToFive.Duration); err != nil {
		ts.logger.Warn("failed to compact to 5min", "err", err)
	}

	// Compact 5-minute -> 1-hour summaries
	if err := ts.compactToResolution(Resolution5Min, Resolution1Hour, ts.config.Compaction.FiveToHour.Duration); err != nil {
		ts.logger.Warn("failed to compact to 1hour", "err", err)
	}

	// Compact 1-hour -> 1-day summaries
	if err := ts.compactToResolution(Resolution1Hour, Resolution1Day, ts.config.Compaction.HourToDay.Duration); err != nil {
		ts.logger.Warn("failed to compact to 1day", "err", err)
	}

	ts.logger.Debug("compaction complete")
	return nil
}

// compactToResolution aggregates data from source resolution to target resolution
func (ts *TimeSeriesStore) compactToResolution(srcRes, tgtRes TimeResolution, threshold time.Duration) error {
	if threshold <= 0 {
		return nil // Compaction disabled for this resolution
	}

	cutoff := time.Now().Add(-threshold)
	srcBucket := truncateToResolution(cutoff, srcRes)
	tgtBucket := truncateToResolution(cutoff, tgtRes)

	ts.logger.Debug("compacting resolution",
		"from", srcRes, "to", tgtRes,
		"cutoff", cutoff,
		"src_bucket", srcBucket,
		"tgt_bucket", tgtBucket)

	// Find all souls with data in source resolution
	// Pattern: {workspace}/ts/{soul}/{resolution}/{timestamp}
	prefix := fmt.Sprintf("default/ts/")
	results, err := ts.db.PrefixScan(prefix)
	if err != nil {
		return err
	}

	// Group summaries by soul and target bucket
	type targetKey struct {
		workspaceID string
		soulID      string
		bucketTime  time.Time
	}
	aggregations := make(map[targetKey][]*JudgmentSummary)

	for key, data := range results {
		if !strings.Contains(key, "/ts/") || !strings.Contains(key, string(srcRes)) {
			continue
		}

		// Parse key: {workspace}/ts/{soul}/{resolution}/{timestamp}
		parts := strings.Split(key, "/")
		if len(parts) < 5 {
			continue
		}

		workspaceID := parts[0]
		soulID := parts[2]
		resolution := parts[3]
		tsUnix, err := strconv.ParseInt(parts[4], 10, 64)
		if err != nil {
			continue
		}

		if resolution != string(srcRes) {
			continue
		}

		bucketTime := time.Unix(tsUnix, 0)
		if bucketTime.After(srcBucket) {
			continue // Too recent, don't compact yet
		}

		var summary JudgmentSummary
		if err := json.Unmarshal(data, &summary); err != nil {
			ts.logger.Warn("failed to unmarshal summary for compaction", "err", err)
			continue
		}

		tKey := targetKey{
			workspaceID: workspaceID,
			soulID:      soulID,
			bucketTime:  truncateToResolution(bucketTime, tgtRes),
		}
		aggregations[tKey] = append(aggregations[tKey], &summary)
	}

	// Aggregate and save target summaries
	for tKey, summaries := range aggregations {
		if err := ts.aggregateAndSave(tKey.workspaceID, tKey.soulID, tgtRes, tKey.bucketTime, summaries); err != nil {
			ts.logger.Warn("failed to save aggregated summary", "err", err)
			continue
		}
	}

	return nil
}

// aggregateAndSave aggregates multiple source summaries into a target summary
func (ts *TimeSeriesStore) aggregateAndSave(workspaceID, soulID string, resolution TimeResolution, bucketTime time.Time, sources []*JudgmentSummary) error {
	key := fmt.Sprintf("%s/ts/%s/%s/%d", workspaceID, soulID, resolution, bucketTime.Unix())

	// Check if target already exists
	var target JudgmentSummary
	data, err := ts.db.Get(key)
	if err == nil {
		if err := json.Unmarshal(data, &target); err != nil {
			ts.logger.Warn("failed to unmarshal existing target summary", "err", err)
		}
	}

	// Aggregate sources
	target.SoulID = soulID
	target.WorkspaceID = workspaceID
	target.Resolution = string(resolution)
	target.BucketTime = bucketTime

	totalCount := 0
	successCount := 0
	var latencies []float64

	for _, src := range sources {
		totalCount += src.Count
		successCount += src.SuccessCount

		// Collect latency estimates
		if src.Count > 0 {
			// Weight by count for accurate averaging
			for i := 0; i < src.Count; i++ {
				latencies = append(latencies, src.AvgLatency)
			}
		}
	}

	target.Count = totalCount
	target.SuccessCount = successCount
	target.FailureCount = totalCount - successCount
	target.UptimePercent = float64(successCount) / float64(totalCount) * 100

	// Calculate aggregated latency stats
	if len(latencies) > 0 {
		sort.Float64s(latencies)
		target.MinLatency = latencies[0]
		target.MaxLatency = latencies[len(latencies)-1]

		sum := 0.0
		for _, l := range latencies {
			sum += l
		}
		target.AvgLatency = sum / float64(len(latencies))

		// Percentiles
		if len(latencies) >= 2 {
			p50Idx := int(float64(len(latencies)) * 0.50)
			p95Idx := int(float64(len(latencies)) * 0.95)
			p99Idx := int(float64(len(latencies)) * 0.99)

			if p50Idx >= len(latencies) {
				p50Idx = len(latencies) - 1
			}
			if p95Idx >= len(latencies) {
				p95Idx = len(latencies) - 1
			}
			if p99Idx >= len(latencies) {
				p99Idx = len(latencies) - 1
			}

			target.P50Latency = latencies[p50Idx]
			target.P95Latency = latencies[p95Idx]
			target.P99Latency = latencies[p99Idx]
		}
	}

	// Save target summary
	newData, err := json.Marshal(target)
	if err != nil {
		return fmt.Errorf("failed to marshal target summary: %w", err)
	}

	return ts.db.Put(key, newData)
}
