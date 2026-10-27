package storage

import (
	"context"
	"encoding/json"
	"errors"
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

	// summaryMu serializes read-modify-write aggregation for a bucket. A single
	// mutex is sufficient because summary updates are short and avoids lost
	// increments when concurrent judgments land in the same bucket.
	summaryMu sync.Mutex

	stopCh       chan struct{}
	stopMu       sync.Mutex // protects stopCh and compactionWG lifecycle
	compactionWG sync.WaitGroup

	// liveMarked caches which souls already have a live-since marker, so the
	// marker costs one lookup per soul rather than one per judgment.
	liveMarked sync.Map
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

	// Sources records, on a rollup, each source bucket it was built from,
	// keyed by the bucket's Unix time. Compaction compares it with the
	// current sources to fold in late data, and uses it in place of sources
	// that retention has deleted since.
	Sources map[string]SourceSnapshot `json:"sources,omitempty"`
}

// SourceSnapshot is the part of a source summary a rollup is computed from.
type SourceSnapshot struct {
	Count        int     `json:"count"`
	SuccessCount int     `json:"success_count"`
	MinLatency   float64 `json:"min_latency_ms"`
	MaxLatency   float64 `json:"max_latency_ms"`
	AvgLatency   float64 `json:"avg_latency_ms"`
}

func snapshotOf(s *JudgmentSummary) SourceSnapshot {
	return SourceSnapshot{
		Count: s.Count, SuccessCount: s.SuccessCount,
		MinLatency: s.MinLatency, MaxLatency: s.MaxLatency, AvgLatency: s.AvgLatency,
	}
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
	values, idxKey, err := ts.db.prepareJudgment(ctx, j)
	if err != nil {
		return err
	}
	ts.summaryMu.Lock()
	defer ts.summaryMu.Unlock()
	for _, resolution := range []TimeResolution{Resolution1Min, Resolution1Hour, Resolution1Day} {
		key, data, err := ts.prepareSummary(ctx, j, resolution)
		if err != nil {
			return err
		}
		values[key] = data
	}
	marker := liveSinceKey(j.WorkspaceID, j.SoulID)
	if _, cached := ts.liveMarked.Load(marker); !cached {
		if _, err := ts.db.Get(marker); err != nil {
			var missing *core.NotFoundError
			if !errors.As(err, &missing) {
				return err
			}
			// Use ingestion time: a backdated result must not certify older rollups.
			values[marker] = []byte(time.Now().UTC().Format(time.RFC3339Nano))
		}
	}
	if err := ts.db.putMany(values); err != nil {
		return err
	}
	ts.db.recordJudgmentIndex(j, idxKey)
	ts.liveMarked.Store(marker, struct{}{})
	return nil
}

// SaveJudgments saves judgments as SaveJudgment would one after another, in
// as few WAL frames (and fsyncs) as fit. A judgment that is already stored
// (or repeated in the batch) is skipped, so replaying a batch is safe. Frames
// split only between judgments, so each judgment lands with its summaries.
func (ts *TimeSeriesStore) SaveJudgments(ctx context.Context, judgments []*core.Judgment) error {
	ts.summaryMu.Lock()
	defer ts.summaryMu.Unlock()
	values := make(map[string][]byte)
	size := 0
	type indexed struct {
		j   *core.Judgment
		key string
	}
	var pending []indexed
	var markers []string
	seen := make(map[string]bool)
	get := func(key string) ([]byte, error) {
		if v, ok := values[key]; ok {
			return v, nil
		}
		return ts.db.Get(key)
	}
	flush := func() error {
		if len(values) == 0 {
			return nil
		}
		if err := ts.db.putMany(values); err != nil {
			return err
		}
		for _, p := range pending {
			ts.db.recordJudgmentIndex(p.j, p.key)
		}
		for _, m := range markers {
			ts.liveMarked.Store(m, struct{}{})
		}
		values, size, pending, markers = make(map[string][]byte), 0, nil, nil
		return nil
	}
	for _, j := range judgments {
		if j.ID != "" {
			ws := j.WorkspaceID
			if ws == "" {
				ws = "default"
			}
			if seen[ws+"/"+j.ID] {
				continue
			}
			seen[ws+"/"+j.ID] = true
			if _, err := ts.db.GetJudgmentByID(ctx, ws, j.ID); err == nil {
				continue
			}
		}
		group, idxKey, err := ts.db.prepareJudgment(ctx, j)
		if err != nil {
			return err
		}
		for _, resolution := range []TimeResolution{Resolution1Min, Resolution1Hour, Resolution1Day} {
			key, data, err := ts.prepareSummaryFrom(ctx, j, resolution, get)
			if err != nil {
				return err
			}
			group[key] = data
		}
		marker := liveSinceKey(j.WorkspaceID, j.SoulID)
		if _, cached := ts.liveMarked.Load(marker); !cached {
			if _, err := get(marker); err != nil {
				var missing *core.NotFoundError
				if !errors.As(err, &missing) {
					return err
				}
				group[marker] = []byte(time.Now().UTC().Format(time.RFC3339Nano))
			}
			markers = append(markers, marker)
		}
		groupSize := 0
		for k, v := range group {
			groupSize += walBatchCost(k, v)
		}
		if size > 0 && size+groupSize > walBatchBudget {
			if err := flush(); err != nil {
				return err
			}
		}
		for k, v := range group {
			values[k] = v
		}
		size += groupSize
		pending = append(pending, indexed{j, idxKey})
	}
	return flush()
}

// liveSinceKey records when a soul's results started being counted into its
// hour and day summaries as they arrive. Every UTC hour or day starting at or
// after that time has a complete summary; earlier ones must be read from raw
// judgments.
// The key has four segments, so compaction and retention, which only parse
// {ws}/ts/{soul}/{resolution}/{bucket}, leave it alone.
func liveSinceKey(workspaceID, soulID string) string {
	return fmt.Sprintf("%s/ts/%s/live-since", workspaceID, soulID)
}

// prepareSummary builds an update without publishing it; caller holds summaryMu.
func (ts *TimeSeriesStore) prepareSummary(ctx context.Context, j *core.Judgment, resolution TimeResolution) (string, []byte, error) {
	return ts.prepareSummaryFrom(ctx, j, resolution, ts.db.Get)
}

// prepareSummaryFrom is prepareSummary reading the current summary through
// get, so a batch can fold several judgments into one bucket before writing.
func (ts *TimeSeriesStore) prepareSummaryFrom(ctx context.Context, j *core.Judgment, resolution TimeResolution, get func(string) ([]byte, error)) (string, []byte, error) {
	workspaceID := j.WorkspaceID
	if workspaceID == "" {
		workspaceID = core.WorkspaceIDFromContext(ctx)
	}

	// Calculate bucket time
	bucketTime := truncateToResolution(j.Timestamp, resolution)

	key := fmt.Sprintf("%s/ts/%s/%s/%d", workspaceID, j.SoulID, resolution, bucketTime.Unix())

	// Get existing summary
	var summary JudgmentSummary
	data, err := get(key)
	if err == nil {
		// Update existing
		if err := json.Unmarshal(data, &summary); err != nil {
			return "", nil, fmt.Errorf("failed to unmarshal summary: %w", err)
		}
	} else {
		var missing *core.NotFoundError
		if !errors.As(err, &missing) {
			return "", nil, err
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

	// Include zero-loss samples in the average over all judgments.
	packetLoss := 0.0
	if j.Details != nil {
		packetLoss = j.Details.PacketLoss
	}
	summary.PacketLossAvg = ((summary.PacketLossAvg * float64(summary.Count-1)) + packetLoss) / float64(summary.Count)

	// Save updated summary
	newData, err := json.Marshal(summary)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal summary: %w", err)
	}
	return key, newData, nil
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
		// UTC days, as the status page counts them. Hours are already
		// UTC-aligned (Truncate works on absolute time), so they nest.
		u := t.UTC()
		return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
	default:
		return t
	}
}

// StartCompaction starts the background compaction goroutine
func (ts *TimeSeriesStore) StartCompaction() {
	ts.stopMu.Lock()
	defer ts.stopMu.Unlock()
	if ts.stopCh != nil {
		return
	}

	ts.stopCh = make(chan struct{})
	ts.compactionWG.Add(1)
	go ts.compactionLoop(ts.stopCh)
}

// StopCompaction gracefully stops the compaction goroutine. Holding stopMu
// through the wait prevents a concurrent Start from adding to the WaitGroup
// until the previous lifecycle has fully exited.
func (ts *TimeSeriesStore) StopCompaction() {
	ts.stopMu.Lock()
	defer ts.stopMu.Unlock()
	if ts.stopCh == nil {
		return
	}

	close(ts.stopCh)
	ts.compactionWG.Wait()
	ts.stopCh = nil
}

// compactionLoop runs compaction at regular intervals. The stop channel
// is captured by value (passed in from StartCompaction) so the goroutine
// never reads ts.stopCh directly — that field is reassigned by callers
// and reads against it raced with StopCompaction's close+nil under the
// race detector.
func (ts *TimeSeriesStore) compactionLoop(stopCh <-chan struct{}) {
	defer ts.compactionWG.Done()
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

// resolutionSpan is the length of one bucket at a resolution.
func resolutionSpan(res TimeResolution) time.Duration {
	switch res {
	case Resolution1Min:
		return time.Minute
	case Resolution5Min:
		return 5 * time.Minute
	case Resolution1Hour:
		return time.Hour
	case Resolution1Day:
		return 24 * time.Hour
	default:
		return 0
	}
}

// compactToResolution aggregates data from source resolution to target resolution.
//
// A target bucket is built once its whole span is older than the cutoff, and
// records a snapshot of each source it was built from. Later runs rebuild it
// only when a source has changed or appeared, i.e. data arrived late, and
// then use the recorded snapshot for any source retention has deleted since.
// Rebuilding blindly from the surviving sources on every run let retention
// corrupt rollups, losing counts and skewing uptime; never rebuilding dropped
// late data from every coarser level.
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
	// Current sources per target bucket, keyed by source bucket Unix time.
	aggregations := make(map[targetKey]map[string]*JudgmentSummary)
	tgtSpan := resolutionSpan(tgtRes)

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

			tKey := targetKey{
				workspaceID: workspaceID,
				soulID:      soulID,
				bucketTime:  truncateToResolution(bucketTime, tgtRes),
			}
			if tKey.bucketTime.Add(tgtSpan).After(cutoff) {
				continue // target bucket still open: some sources may be newer
			}

			var summary JudgmentSummary
			if err := json.Unmarshal(data, &summary); err != nil {
				ts.logger.Warn("failed to unmarshal summary for compaction", "err", err)
				continue
			}
			summary.BucketTime = bucketTime
			if aggregations[tKey] == nil {
				aggregations[tKey] = make(map[string]*JudgmentSummary)
			}
			aggregations[tKey][parts[4]] = &summary
		}
	}

	for tKey, current := range aggregations {
		tgtStoreKey := fmt.Sprintf("%s/ts/%s/%s/%d", tKey.workspaceID, tKey.soulID, tgtRes, tKey.bucketTime.Unix())
		sources, changed := ts.rollupSources(tgtStoreKey, current)
		if !changed {
			continue
		}
		if err := ts.aggregateAndSave(tKey.workspaceID, tKey.soulID, tgtRes, tKey.bucketTime, sources); err != nil {
			ts.logger.Warn("failed to save aggregated summary", "err", err)
			continue
		}
	}

	return nil
}

// rollupSources returns the sources to build the rollup at key from, and
// whether it needs (re)building: it does not exist yet, or a current source
// is new or differs from the snapshot recorded when it was built. Recorded
// sources no longer present, deleted by retention, are carried over from
// their snapshots so the rollup keeps what it already counted.
func (ts *TimeSeriesStore) rollupSources(key string, current map[string]*JudgmentSummary) ([]*JudgmentSummary, bool) {
	all := make([]*JudgmentSummary, 0, len(current))
	for _, src := range current {
		all = append(all, src)
	}

	data, err := ts.db.Get(key)
	if err != nil {
		return all, true // not built yet
	}
	var existing JudgmentSummary
	if err := json.Unmarshal(data, &existing); err != nil {
		ts.logger.Warn("failed to unmarshal existing rollup; rebuilding", "key", key, "err", err)
		return all, true
	}

	if existing.Sources == nil {
		// Built before snapshots were recorded: rebuild only if the current
		// sources hold more than it counted, and never from fewer.
		total := 0
		for _, src := range current {
			total += src.Count
		}
		return all, total > existing.Count
	}

	changed := false
	for unix, src := range current {
		if snap, ok := existing.Sources[unix]; !ok || snap != snapshotOf(src) {
			changed = true
			break
		}
	}
	if !changed {
		return nil, false
	}
	for unix, snap := range existing.Sources {
		if _, ok := current[unix]; ok {
			continue
		}
		sec, err := strconv.ParseInt(unix, 10, 64)
		if err != nil {
			continue
		}
		all = append(all, &JudgmentSummary{
			BucketTime: time.Unix(sec, 0), Count: snap.Count, SuccessCount: snap.SuccessCount,
			MinLatency: snap.MinLatency, MaxLatency: snap.MaxLatency, AvgLatency: snap.AvgLatency,
		})
	}
	return all, true
}

// weightedLatency represents a latency value with its weight (count)
type weightedLatency struct {
	value float64
	count int
}

// aggregateAndSave aggregates multiple source summaries into a target summary
// Uses weighted percentile algorithm to avoid O(N*M) memory expansion.
func (ts *TimeSeriesStore) aggregateAndSave(workspaceID, soulID string, resolution TimeResolution, bucketTime time.Time, sources []*JudgmentSummary) error {
	ts.summaryMu.Lock()
	defer ts.summaryMu.Unlock()

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

	target.Sources = make(map[string]SourceSnapshot, len(sources))
	for _, src := range sources {
		target.Sources[strconv.FormatInt(src.BucketTime.Unix(), 10)] = snapshotOf(src)
	}

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
