package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// SaveJudgment saves a judgment to time-series storage
func (db *CobaltDB) SaveJudgment(ctx context.Context, j *core.Judgment) error {
	values, idxKey, err := db.prepareJudgment(ctx, j)
	if err != nil {
		return err
	}
	if err := db.putMany(values); err != nil {
		return err
	}
	db.recordJudgmentIndex(j, idxKey)
	return nil
}

func (db *CobaltDB) prepareJudgment(ctx context.Context, j *core.Judgment) (map[string][]byte, string, error) {
	// Generate ULID if not set
	if j.ID == "" {
		j.ID = core.GenerateID()
	}
	if err := validateResourceID(j.ID, "judgment"); err != nil {
		return nil, "", err
	}

	workspaceID := j.WorkspaceID
	if workspaceID == "" {
		workspaceID = "default"
		if ctx != nil {
			workspaceID = core.WorkspaceIDFromContext(ctx)
		}
		j.WorkspaceID = workspaceID
	}

	// Identity, rather than clock resolution, distinguishes concurrent results.
	// Legacy timestamp-only records remain readable and are updated in place
	// when the same ID is saved again, avoiding duplicate history on upgrade.
	ts := j.Timestamp.UnixNano()
	legacyKey := fmt.Sprintf("%s/judgments/%s/%d", workspaceID, j.SoulID, ts)
	key := legacyKey + "/" + j.ID
	if raw, err := db.Get(legacyKey); err == nil {
		var previous core.Judgment
		if json.Unmarshal(raw, &previous) == nil && previous.ID == j.ID {
			key = legacyKey
		}
	} else {
		var missing *core.NotFoundError
		if !errors.As(err, &missing) {
			return nil, "", err
		}
	}

	data, err := json.Marshal(j)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal judgment: %w", err)
	}

	// Update secondary index for O(1) GetJudgmentNoCtx lookup
	// Index key: {workspace}/judgment-idx/{judgmentID} -> primary key
	idxKey := fmt.Sprintf("%s/judgment-idx/%s", workspaceID, j.ID)
	return map[string][]byte{key: data, idxKey: []byte(key)}, idxKey, nil
}

func (db *CobaltDB) recordJudgmentIndex(j *core.Judgment, idxKey string) {
	// Update in-memory index map and workspace index under db.mu so
	// concurrent readers/writers see a consistent view.
	db.mu.Lock()
	db.judgmentIndex[j.ID] = idxKey
	db.recordWorkspaceLocked(j.WorkspaceID)
	db.mu.Unlock()
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
		var missing *core.NotFoundError
		if !errors.As(err, &missing) {
			return nil, err
		}
		// Timestamp lookup is retained for old callers. When several IDs share
		// that instant, return the lexicographically greatest ID deterministically.
		matches, scanErr := db.PrefixScan(key + "/")
		if scanErr != nil {
			return nil, scanErr
		}
		var selected string
		for candidate, value := range matches {
			if value != nil && candidate > selected {
				selected, data = candidate, value
			}
		}
		if selected == "" {
			return nil, err
		}
	}

	var j core.Judgment
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("failed to unmarshal judgment: %w", err)
	}

	return &j, nil
}

// GetJudgmentByID reads the workspace-scoped durable ID index. Replication
// deduplicates by identity, including when distinct results share a timestamp.
func (db *CobaltDB) GetJudgmentByID(ctx context.Context, workspaceID, id string) (*core.Judgment, error) {
	workspaceID = defaultWorkspace(workspaceID)
	primary, err := db.Get(fmt.Sprintf("%s/judgment-idx/%s", workspaceID, id))
	if err != nil {
		return nil, err
	}
	data, err := db.Get(string(primary))
	if err != nil {
		return nil, err
	}
	var j core.Judgment
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, err
	}
	if j.ID != id || defaultWorkspace(j.WorkspaceID) != workspaceID {
		return nil, &core.NotFoundError{Entity: "judgment", ID: id}
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

	var results map[string][]byte
	var err error
	if start.UnixNano() < 0 || end.UnixNano() < 0 || len(startKey) != len(endKey) {
		// Decimal keys have lexical order only for positive, equal-width timestamps.
		results, err = db.PrefixScan(prefix)
	} else {
		results, err = db.RangeScan(startKey, endKey)
	}
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
		if j.Timestamp.Before(start) || !j.Timestamp.Before(end) {
			continue
		}
		judgments = append(judgments, &j)
	}

	// The scan results are a map, so collection order says nothing about time.
	// Sort newest-first and only then apply the limit (as ListJudgments does),
	// otherwise a limited query returns an arbitrary subset of the window.
	sort.Slice(judgments, func(i, k int) bool {
		if judgments[i].Timestamp.Equal(judgments[k].Timestamp) {
			return judgments[i].ID > judgments[k].ID
		}
		return judgments[i].Timestamp.After(judgments[k].Timestamp)
	})
	if limit > 0 && len(judgments) > limit {
		judgments = judgments[:limit]
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

	// Compare timestamps; decimal key strings do not preserve chronological order.
	var latest *core.Judgment
	for _, data := range results {
		if data == nil {
			continue
		}
		var j core.Judgment
		if err := json.Unmarshal(data, &j); err != nil {
			continue
		}
		if latest == nil || j.Timestamp.After(latest.Timestamp) || (j.Timestamp.Equal(latest.Timestamp) && j.ID > latest.ID) {
			latest = &j
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
