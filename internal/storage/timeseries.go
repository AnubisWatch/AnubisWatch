package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
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

	// TODO: Implement compaction logic
	// 1. Find raw data older than compaction threshold
	// 2. Aggregate into 1-minute summaries
	// 3. Find 1-minute data older than threshold
	// 4. Aggregate into 5-minute summaries
	// etc.

	return nil
}
