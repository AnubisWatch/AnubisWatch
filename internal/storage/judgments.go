package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// SaveJudgment saves a judgment to time-series storage
func (db *CobaltDB) SaveJudgment(ctx context.Context, j *core.Judgment) error {
	// Generate ULID if not set
	if j.ID == "" {
		j.ID = core.GenerateID()
	}

	workspaceID := "default" // TODO: Extract from context

	// Primary key: {workspace}/judgments/{soul}/{timestamp}
	ts := j.Timestamp.UnixNano()
	key := fmt.Sprintf("%s/judgments/%s/%d", workspaceID, j.SoulID, ts)

	data, err := json.Marshal(j)
	if err != nil {
		return fmt.Errorf("failed to marshal judgment: %w", err)
	}

	return db.Put(key, data)
}

// GetJudgment retrieves a judgment by soul ID and timestamp
func (db *CobaltDB) GetJudgment(ctx context.Context, workspaceID, soulID string, timestamp time.Time) (*core.Judgment, error) {
	if workspaceID == "" {
		workspaceID = "default"
	}

	ts := timestamp.UnixNano()
	key := fmt.Sprintf("%s/judgments/%s/%d", workspaceID, soulID, ts)

	data, err := db.Get(key)
	if err != nil {
		return nil, err
	}

	var j core.Judgment
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("failed to unmarshal judgment: %w", err)
	}

	return &j, nil
}

// QueryJudgments retrieves judgments for a soul within a time range
func (db *CobaltDB) QueryJudgments(ctx context.Context, workspaceID, soulID string, start, end time.Time, limit int) ([]*core.Judgment, error) {
	if workspaceID == "" {
		workspaceID = "default"
	}

	prefix := fmt.Sprintf("%s/judgments/%s/", workspaceID, soulID)
	startKey := fmt.Sprintf("%s%d", prefix, start.UnixNano())
	endKey := fmt.Sprintf("%s%d", prefix, end.UnixNano())

	results, err := db.RangeScan(startKey, endKey)
	if err != nil {
		return nil, err
	}

	judgments := make([]*core.Judgment, 0, len(results))
	for _, data := range results {
		if data == nil {
			continue
		}
		var j core.Judgment
		if err := json.Unmarshal(data, &j); err != nil {
			db.logger.Warn("failed to unmarshal judgment", "err", err)
			continue
		}
		judgments = append(judgments, &j)

		if limit > 0 && len(judgments) >= limit {
			break
		}
	}

	return judgments, nil
}

// GetLatestJudgment retrieves the most recent judgment for a soul
func (db *CobaltDB) GetLatestJudgment(ctx context.Context, workspaceID, soulID string) (*core.Judgment, error) {
	if workspaceID == "" {
		workspaceID = "default"
	}

	// Scan with prefix to find latest
	prefix := fmt.Sprintf("%s/judgments/%s/", workspaceID, soulID)
	results, err := db.PrefixScan(prefix)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, &core.NotFoundError{Entity: "judgment", ID: soulID}
	}

	// Find latest (keys are sorted, so last one is latest)
	var latest *core.Judgment
	var latestKey string
	for key, data := range results {
		if data == nil {
			continue
		}
		var j core.Judgment
		if err := json.Unmarshal(data, &j); err != nil {
			continue
		}
		if key > latestKey {
			latest = &j
			latestKey = key
		}
	}

	if latest == nil {
		return nil, &core.NotFoundError{Entity: "judgment", ID: soulID}
	}

	return latest, nil
}

// GetSoulPurity calculates uptime percentage for a soul over a time window
func (db *CobaltDB) GetSoulPurity(ctx context.Context, workspaceID, soulID string, window time.Duration) (float64, error) {
	end := time.Now()
	start := end.Add(-window)

	judgments, err := db.QueryJudgments(ctx, workspaceID, soulID, start, end, 0)
	if err != nil {
		return 0, err
	}

	if len(judgments) == 0 {
		return 0, nil
	}

	aliveCount := 0
	for _, j := range judgments {
		if j.Status == core.SoulAlive {
			aliveCount++
		}
	}

	return float64(aliveCount) / float64(len(judgments)) * 100, nil
}
