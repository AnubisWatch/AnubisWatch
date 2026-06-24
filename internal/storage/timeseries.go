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
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TimeSeriesStore provides optimized time-series storage
type TimeSeriesStore struct {
	db     *CobaltDB
	config core.TimeSeriesConfig
	logger *slog.Logger
	stopCh chan struct{}
	stopMu sync.Mutex // protects stopCh
}

// TimeResolution represents different time granularities
type TimeResolution string

const (
	ResolutionRaw   TimeResolution = "raw"
	Resolution1Min  TimeResolution = "1min"
	Resolution5Min  TimeResolution = "5min"
	Resolution1Hour TimeResolution = "1hour"
	Resolution1Day  TimeResolution = "1day"
)

// JudgmentSummary aggregates multiple judgments into a time bucket
type JudgmentSummary struct {
	SoulID        string    `json:"soul_id"`
	WorkspaceID   string    `json:"workspace_id"`
	Resolution    string    `json:"resolution"`
	BucketTime    time.Time `json:"bucket_time"`
	Count         int       `json:"count"`
	SuccessCount  int       `json:"success_count"`
	FailureCount  int       `json:"failure_count"`
	MinLatency    float64   `json:"min_latency_ms"`
	MaxLatency    float64   `json:"max_latency_ms"`
	AvgLatency    float64   `json:"avg_latency_ms"`
	P50Latency    float64   `json:"p50_latency_ms"`
	P95Latency    float64   `json:"p95_latency_ms"`
	P99Latency    float64   `json:"p99_latency_ms"`
	UptimePercent float64   `json:"uptime_percent"`
	PacketLossAvg float64   `json:"packet_loss_avg,omitempty"`
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
	workspaceID := core.WorkspaceIDFromContext(ctx)

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
	ts.stopMu.Lock()
	ts.stopCh = make(chan struct{})
	stopCh := ts.stopCh
	ts.stopMu.Unlock()
	go ts.compactionLoop(stopCh)
}

// StopCompaction gracefully stops the compaction goroutine
func (ts *TimeSeriesStore) StopCompaction() {
	ts.stopMu.Lock()
	if ts.stopCh != nil {
		close(ts.stopCh)
		ts.stopCh = nil
	}
	ts.stopMu.Unlock()
}

// compactionLoop runs compaction at regular intervals. The stop channel
// is captured by value (passed in from StartCompaction) so the goroutine
// never reads ts.stopCh directly — that field is reassigned by callers
// and reads against it raced with StopCompaction's close+nil under the
// race detector.
func (ts *TimeSeriesStore) compactionLoop(stopCh <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			ts.logger.Info("compaction stopped")
			return
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

	ts.logger.Debug("compacting resolution",
		"from", srcRes, "to", tgtRes,
		"cutoff", cutoff,
		"src_bucket", srcBucket,
		// tgt_bucket is observability-only — the compaction code below
		// doesn't branch on it, but operators reading the log want to
		// see the target window this run is aiming at. If a future
		// change adds per-bucket work here, the value is
		// truncateToResolution(cutoff, tgtRes).
		"tgt_bucket", truncateToResolution(cutoff, tgtRes))

	// Iterate known workspaces and scan each {ws}/ts/{srcRes}/ prefix.
	// Avoids the previous PrefixScan("") which walked the entire B+Tree.
	workspaces := ts.db.ListWorkspaceIDs()
	if len(workspaces) == 0 {
		workspaces = []string{"default"}
	}

	// Group summaries by soul and target bucket
	type targetKey struct {
		workspaceID string
		soulID      string
		bucketTime  time.Time
	}
	aggregations := make(map[targetKey][]*JudgmentSummary)

	for _, ws := range workspaces {
		// Pattern: {workspace}/ts/{soul}/{srcRes}/{timestamp}
		prefix := fmt.Sprintf("%s/ts/", ws)
		results, err := ts.db.PrefixScan(prefix)
		if err != nil {
			return err
		}

		for key, data := range results {
			// Parse key: {workspace}/ts/{soul}/{resolution}/{timestamp}
			parts := strings.Split(key, "/")
			if len(parts) != 5 || parts[1] != "ts" || parts[3] != string(srcRes) {
				continue
			}

			workspaceID := parts[0]
			soulID := parts[2]
			tsUnix, err := strconv.ParseInt(parts[4], 10, 64)
			if err != nil {
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

// weightedLatency represents a latency value with its weight (count)
type weightedLatency struct {
	value float64
	count int
}

// aggregateAndSave aggregates multiple source summaries into a target summary
// Uses weighted percentile algorithm to avoid O(N*M) memory expansion.
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
	totalLatencySum := 0.0
	var weighted []weightedLatency
	globalMin := math.Inf(1)
	globalMax := math.Inf(-1)

	for _, src := range sources {
		totalCount += src.Count
		successCount += src.SuccessCount
		totalLatencySum += src.AvgLatency * float64(src.Count)

		if src.Count > 0 {
			weighted = append(weighted, weightedLatency{value: src.AvgLatency, count: src.Count})
			if src.MinLatency < globalMin {
				globalMin = src.MinLatency
			}
			if src.MaxLatency > globalMax {
				globalMax = src.MaxLatency
			}
		}
	}

	target.Count = totalCount
	target.SuccessCount = successCount
	target.FailureCount = totalCount - successCount
	target.UptimePercent = float64(successCount) / float64(totalCount) * 100

	// Calculate aggregated latency stats using weighted percentiles
	if len(weighted) > 0 {
		sort.Slice(weighted, func(i, j int) bool {
			return weighted[i].value < weighted[j].value
		})

		target.MinLatency = globalMin
		target.MaxLatency = globalMax
		target.AvgLatency = totalLatencySum / float64(totalCount)

		// Weighted percentile calculation
		if totalCount >= 2 {
			target.P50Latency = weightedPercentile(weighted, 0.50, totalCount)
			target.P95Latency = weightedPercentile(weighted, 0.95, totalCount)
			target.P99Latency = weightedPercentile(weighted, 0.99, totalCount)
		}
	}

	// Save target summary
	newData, err := json.Marshal(target)
	if err != nil {
		return fmt.Errorf("failed to marshal target summary: %w", err)
	}

	return ts.db.Put(key, newData)
}

// weightedPercentile computes the p-th percentile from weighted values
// without expanding the array. cumulative count determines boundaries.
func weightedPercentile(weighted []weightedLatency, percentile float64, totalCount int) float64 {
	targetRank := float64(totalCount) * percentile
	cumulative := 0
	for _, w := range weighted {
		cumulative += w.count
		if float64(cumulative) >= targetRank {
			return w.value
		}
	}
	// Fallback: return the largest value
	return weighted[len(weighted)-1].value
}
