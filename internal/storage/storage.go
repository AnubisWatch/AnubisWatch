package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func defaultWorkspace(workspaceID string) string {
	if workspaceID == "" {
		return "default"
	}
	return workspaceID
}

// SaveVerdict saves a verdict to storage
func (db *CobaltDB) SaveVerdict(ctx context.Context, v *core.Verdict) error {
	if v.ID == "" {
		v.ID = core.GenerateID()
	}

	workspaceID := v.WorkspaceID
	if workspaceID == "" {
		workspaceID = "default"
	}

	key := fmt.Sprintf("%s/verdicts/%s", workspaceID, v.ID)

	data, _ := json.Marshal(v)

	return db.Put(key, data)
}

// GetVerdict retrieves a verdict by ID
func (db *CobaltDB) GetVerdict(ctx context.Context, workspaceID, verdictID string) (*core.Verdict, error) {
	if workspaceID == "" {
		workspaceID = "default"
	}

	key := fmt.Sprintf("%s/verdicts/%s", workspaceID, verdictID)

	data, err := db.Get(key)
	if err != nil {
		return nil, err
	}

	var v core.Verdict
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("failed to unmarshal verdict: %w", err)
	}

	return &v, nil
}

// ListVerdicts returns verdicts for a workspace with optional filters
func (db *CobaltDB) ListVerdicts(ctx context.Context, workspaceID string, status core.VerdictStatus, limit int) ([]*core.Verdict, error) {
	if workspaceID == "" {
		workspaceID = "default"
	}

	prefix := fmt.Sprintf("%s/verdicts/", workspaceID)
	results, err := db.PrefixScan(prefix)
	if err != nil {
		return nil, err
	}

	verdicts := make([]*core.Verdict, 0, len(results))
	for _, data := range results {
		if data == nil {
			continue
		}
		var v core.Verdict
		if err := json.Unmarshal(data, &v); err != nil {
			db.logger.Warn("failed to unmarshal verdict", "err", err)
			continue
		}

		// Filter by status if specified
		if status != "" && v.Status != status {
			continue
		}

		verdicts = append(verdicts, &v)
	}

	// Sort all matching verdicts before limiting; PrefixScan returns a map, so
	// iteration order cannot determine which records make the result page.
	sort.Slice(verdicts, func(i, j int) bool {
		return verdicts[i].FiredAt.After(verdicts[j].FiredAt)
	})

	if limit > 0 && len(verdicts) > limit {
		verdicts = verdicts[:limit]
	}

	return verdicts, nil
}

// UpdateVerdictStatus updates a verdict's status
func (db *CobaltDB) UpdateVerdictStatus(ctx context.Context, workspaceID, verdictID string, status core.VerdictStatus) error {
	v, err := db.GetVerdict(ctx, workspaceID, verdictID)
	if err != nil {
		return err
	}

	v.Status = status
	if status == core.VerdictResolved {
		now := time.Now().UTC()
		v.ResolvedAt = &now
	}

	return db.SaveVerdict(ctx, v)
}

// AcknowledgeVerdict marks a verdict as acknowledged
func (db *CobaltDB) AcknowledgeVerdict(ctx context.Context, workspaceID, verdictID, user string) error {
	v, err := db.GetVerdict(ctx, workspaceID, verdictID)
	if err != nil {
		return err
	}

	v.Status = core.VerdictAcknowledged
	now := time.Now().UTC()
	v.AcknowledgedAt = &now
	v.AcknowledgedBy = user

	return db.SaveVerdict(ctx, v)
}

// GetActiveVerdicts returns all active (non-resolved) verdicts for a soul
func (db *CobaltDB) GetActiveVerdicts(ctx context.Context, workspaceID, soulID string) ([]*core.Verdict, error) {
	if workspaceID == "" {
		workspaceID = "default"
	}

	prefix := fmt.Sprintf("%s/verdicts/", workspaceID)
	results, err := db.PrefixScan(prefix)
	if err != nil {
		return nil, err
	}

	var verdicts []*core.Verdict
	for _, data := range results {
		if data == nil {
			continue
		}
		var v core.Verdict
		if err := json.Unmarshal(data, &v); err != nil {
			continue
		}
		if v.SoulID == soulID && v.Status != core.VerdictResolved {
			verdicts = append(verdicts, &v)
		}
	}

	return verdicts, nil
}

// SaveJourney saves a journey configuration
func (db *CobaltDB) SaveJourney(ctx context.Context, j *core.JourneyConfig) error {
	if j.ID == "" {
		j.ID = core.GenerateID()
	} else if err := validateResourceID(j.ID, "journey"); err != nil {
		return err
	}

	workspaceID := j.WorkspaceID
	if workspaceID == "" {
		workspaceID = "default"
	}
	j.WorkspaceID = workspaceID

	key := fmt.Sprintf("%s/journeys/%s", workspaceID, j.ID)

	data, _ := json.Marshal(j)

	// Update secondary index for O(1) GetJourneyNoCtx lookup.
	// db.mu guards journeyIndex from concurrent mutation
	// (e.g. parallel SaveJourney calls racing with rebuildSecondaryIndexes).
	db.mu.Lock()
	db.journeyIndex[j.ID] = workspaceID
	db.recordWorkspaceLocked(workspaceID)
	db.mu.Unlock()

	return db.Put(key, data)
}

// GetJourney retrieves a journey by ID
func (db *CobaltDB) GetJourney(ctx context.Context, workspaceID, journeyID string) (*core.JourneyConfig, error) {
	if workspaceID == "" {
		workspaceID = "default"
	}

	key := fmt.Sprintf("%s/journeys/%s", workspaceID, journeyID)

	data, err := db.Get(key)
	if err != nil {
		return nil, err
	}

	var j core.JourneyConfig
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("failed to unmarshal journey: %w", err)
	}

	return &j, nil
}

// ListJourneys returns all journeys in a workspace
func (db *CobaltDB) ListJourneys(ctx context.Context, workspaceID string) ([]*core.JourneyConfig, error) {
	if workspaceID == "" {
		workspaceID = "default"
	}

	prefix := fmt.Sprintf("%s/journeys/", workspaceID)
	results, err := db.PrefixScan(prefix)
	if err != nil {
		return nil, err
	}

	journeys := make([]*core.JourneyConfig, 0, len(results))
	for _, data := range results {
		if data == nil {
			continue
		}
		var j core.JourneyConfig
		if err := json.Unmarshal(data, &j); err != nil {
			db.logger.Warn("failed to unmarshal journey", "err", err)
			continue
		}
		journeys = append(journeys, &j)
	}

	return journeys, nil
}

// DeleteJourney removes a journey
func (db *CobaltDB) DeleteJourney(ctx context.Context, workspaceID, journeyID string) error {
	if workspaceID == "" {
		workspaceID = "default"
	}

	key := fmt.Sprintf("%s/journeys/%s", workspaceID, journeyID)
	if err := db.Delete(key); err != nil {
		return err
	}
	// Remove the secondary index entry so GetJourneyNoCtx doesn't
	// hand out a workspaceID for a deleted journey. db.mu guards
	// journeyIndex (and workspaceOrder).
	db.mu.Lock()
	delete(db.journeyIndex, journeyID)
	db.mu.Unlock()
	return nil
}

// SaveJourneyRun saves a journey execution result
func (db *CobaltDB) SaveJourneyRun(ctx context.Context, run *core.JourneyRun) error {
	if run.ID == "" {
		run.ID = core.GenerateID()
	}

	workspaceID := run.WorkspaceID
	if workspaceID == "" {
		workspaceID = "default"
	}

	legacyKey := fmt.Sprintf("%s/journey-runs/%s/%d", workspaceID, run.JourneyID, run.StartedAt)
	key := legacyKey + "/" + run.ID
	// Keep updates to existing legacy records in place; new runs include the ID
	// so different executions starting in the same millisecond cannot collide.
	if data, err := db.Get(legacyKey); err == nil {
		var legacy core.JourneyRun
		if json.Unmarshal(data, &legacy) == nil && legacy.ID == run.ID {
			key = legacyKey
		}
	}

	data, _ := json.Marshal(run)

	return db.Put(key, data)
}

// QueryJourneyRuns retrieves runs for a journey
func (db *CobaltDB) QueryJourneyRuns(ctx context.Context, workspaceID, journeyID string, limit int) ([]*core.JourneyRun, error) {
	if workspaceID == "" {
		workspaceID = "default"
	}

	prefix := fmt.Sprintf("%s/journey-runs/%s/", workspaceID, journeyID)
	results, err := db.PrefixScan(prefix)
	if err != nil {
		return nil, err
	}

	runs := make([]*core.JourneyRun, 0, len(results))
	for _, data := range results {
		if data == nil {
			continue
		}
		var run core.JourneyRun
		if err := json.Unmarshal(data, &run); err != nil {
			db.logger.Warn("failed to unmarshal journey run", "err", err)
			continue
		}
		runs = append(runs, &run)
	}

	// Sort by started time descending
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].StartedAt > runs[j].StartedAt
	})

	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}

	return runs, nil
}

// GetJourneyRun retrieves a single journey run by run ID
func (db *CobaltDB) GetJourneyRun(ctx context.Context, workspaceID, journeyID, runID string) (*core.JourneyRun, error) {
	if workspaceID == "" {
		workspaceID = "default"
	}

	prefix := fmt.Sprintf("%s/journey-runs/%s/", workspaceID, journeyID)
	results, err := db.PrefixScan(prefix)
	if err != nil {
		return nil, err
	}

	for _, data := range results {
		if data == nil {
			continue
		}
		var run core.JourneyRun
		if err := json.Unmarshal(data, &run); err != nil {
			continue
		}
		if run.ID == runID {
			return &run, nil
		}
	}

	return nil, fmt.Errorf("journey run %s not found", runID)
}

// SaveChannel saves an alert channel configuration
func (db *CobaltDB) SaveChannel(ctx context.Context, ch *core.ChannelConfig) error {
	workspaceID := core.WorkspaceIDFromContext(ctx)
	key := fmt.Sprintf("%s/channels/%s", workspaceID, ch.Name)

	data, _ := json.Marshal(ch)

	return db.Put(key, data)
}

// GetChannel retrieves a channel by name
func (db *CobaltDB) GetChannel(ctx context.Context, id string) (*core.ChannelConfig, error) {
	workspaceID := core.WorkspaceIDFromContext(ctx)
	key := fmt.Sprintf("%s/channels/%s", workspaceID, id)

	data, err := db.Get(key)
	if err != nil {
		return nil, err
	}

	var ch core.ChannelConfig
	if err := json.Unmarshal(data, &ch); err != nil {
		return nil, fmt.Errorf("failed to unmarshal channel: %w", err)
	}

	return &ch, nil
}

// ListChannels returns all channels in a workspace
func (db *CobaltDB) ListChannels(ctx context.Context, workspaceID string) ([]*core.ChannelConfig, error) {
	if workspaceID == "" {
		workspaceID = "default"
	}

	prefix := fmt.Sprintf("%s/channels/", workspaceID)
	results, err := db.PrefixScan(prefix)
	if err != nil {
		return nil, err
	}

	channels := make([]*core.ChannelConfig, 0, len(results))
	for _, data := range results {
		if data == nil {
			continue
		}
		var ch core.ChannelConfig
		if err := json.Unmarshal(data, &ch); err != nil {
			db.logger.Warn("failed to unmarshal channel", "err", err)
			continue
		}
		channels = append(channels, &ch)
	}

	return channels, nil
}

// DeleteChannel removes a channel
func (db *CobaltDB) DeleteChannel(ctx context.Context, id string) error {
	workspaceID := core.WorkspaceIDFromContext(ctx)
	key := fmt.Sprintf("%s/channels/%s", workspaceID, id)
	return db.Delete(key)
}

// ListJudgments returns judgments for a soul within a time range
func (db *CobaltDB) ListJudgments(ctx context.Context, soulID string, start, end time.Time, limit int) ([]*core.Judgment, error) {
	workspaceID := core.WorkspaceIDFromContext(ctx)
	prefix := fmt.Sprintf("%s/judgments/", workspaceID)
	if soulID != "" {
		prefix = fmt.Sprintf("%s/judgments/%s/", workspaceID, soulID)
	}

	// For one soul, read only the requested window: keys end in the judgment's
	// UnixNano, which sorts lexically while every bound has the same number of
	// digits. Scanning the whole prefix decoded the soul's entire history on
	// every call, however short the window.
	var results map[string][]byte
	var err error
	startKey := fmt.Sprintf("%s%d", prefix, start.UnixNano())
	endKey := fmt.Sprintf("%s%d", prefix, end.UnixNano())
	if soulID != "" && !start.Before(minRangeScanTime) && end.Before(maxRangeScanTime) && len(startKey) == len(endKey) {
		results, err = db.RangeScan(startKey, endKey)
	} else {
		results, err = db.PrefixScan(prefix)
	}
	if err != nil {
		return nil, err
	}

	judgments := make([]*core.Judgment, 0, len(results))
	for key, data := range results {
		if data == nil {
			continue
		}
		var j core.Judgment
		if err := json.Unmarshal(data, &j); err != nil {
			db.logger.Warn("failed to unmarshal judgment", "err", err)
			continue
		}
		if j.WorkspaceID == "" {
			j.WorkspaceID = strings.SplitN(key, "/", 2)[0]
		}
		if j.Timestamp.After(start) && j.Timestamp.Before(end) {
			judgments = append(judgments, &j)
		}
	}

	sort.Slice(judgments, func(i, j int) bool {
		if judgments[i].Timestamp.Equal(judgments[j].Timestamp) {
			return judgments[i].ID > judgments[j].ID
		}
		return judgments[i].Timestamp.After(judgments[j].Timestamp)
	})

	if limit > 0 && len(judgments) > limit {
		judgments = judgments[:limit]
	}
	return judgments, nil
}

// Range scans over judgment keys are only used between these bounds, where
// UnixNano has 19 digits: keys of equal width sort lexically in time order,
// and time.Time values outside int64 nanoseconds (such as the zero Time used
// as "from the beginning") have no meaningful UnixNano.
var (
	minRangeScanTime = time.Unix(1_000_000_000, 0)       // 2001-09-09
	maxRangeScanTime = time.Unix(9_000_000_000, 0).UTC() // 2255
)

// TallyChecks counts a soul's checks in each [Start, End) range. Whole UTC
// hours that started after the soul's results began being counted into live
// hourly summaries are read from those summaries, one lookup per hour; only
// the partial hours at a range's edges and anything before live counting are
// read from raw judgments, each through a key-range scan of that part alone.
func (db *CobaltDB) TallyChecks(soulID string, ranges []core.TimeRange) ([]core.CheckTally, error) {
	workspaceID := "default"
	if soul, err := db.GetSoulNoCtx(soulID); err == nil {
		workspaceID = defaultWorkspace(soul.WorkspaceID)
	}

	var liveHour time.Time
	if raw, err := db.Get(liveSinceKey(workspaceID, soulID)); err == nil {
		if t, err := time.Parse(time.RFC3339Nano, string(raw)); err == nil {
			liveHour = t.Truncate(time.Hour)
			if liveHour.Before(t) {
				liveHour = liveHour.Add(time.Hour)
			}
		}
	}

	hourly := map[int64]*JudgmentSummary{}
	if !liveHour.IsZero() && len(ranges) > 0 {
		first, last := ranges[0].Start, ranges[0].End
		for _, r := range ranges[1:] {
			if r.Start.Before(first) {
				first = r.Start
			}
			if r.End.After(last) {
				last = r.End
			}
		}
		if first.Before(liveHour) {
			first = liveHour
		}
		prefix := fmt.Sprintf("%s/ts/%s/%s/", workspaceID, soulID, Resolution1Hour)
		results, err := db.RangeScan(
			fmt.Sprintf("%s%d", prefix, first.Truncate(time.Hour).Unix()),
			fmt.Sprintf("%s%d", prefix, last.Unix()+1))
		if err != nil {
			return nil, err
		}
		for key, data := range results {
			unix, err := strconv.ParseInt(key[strings.LastIndex(key, "/")+1:], 10, 64)
			if err != nil {
				continue
			}
			var summary JudgmentSummary
			if err := json.Unmarshal(data, &summary); err != nil {
				continue
			}
			hourly[unix] = &summary
		}
	}

	tallies := make([]core.CheckTally, len(ranges))
	addRaw := func(t *core.CheckTally, start, end time.Time) error {
		if !start.Before(end) {
			return nil
		}
		judgments, err := db.QueryJudgments(context.Background(), workspaceID, soulID, start, end, 0)
		if err != nil {
			return err
		}
		for _, j := range judgments {
			t.Checks++
			if j.Status == core.SoulAlive {
				t.Up++
			}
			t.LatencySumMs += float64(j.Duration) / float64(time.Millisecond)
		}
		return nil
	}

	for i, r := range ranges {
		t := &tallies[i]
		// Whole hours [a, b) inside the range and inside live counting.
		a := r.Start
		if !liveHour.IsZero() && a.Before(liveHour) {
			a = liveHour
		}
		if trunc := a.Truncate(time.Hour); trunc.Before(a) {
			a = trunc.Add(time.Hour)
		}
		b := r.End.Truncate(time.Hour)
		if liveHour.IsZero() || !a.Before(b) {
			if err := addRaw(t, r.Start, r.End); err != nil {
				return nil, err
			}
			continue
		}
		for h := a; h.Before(b); h = h.Add(time.Hour) {
			if s := hourly[h.Unix()]; s != nil {
				t.Checks += s.Count
				t.Up += s.SuccessCount
				t.LatencySumMs += s.AvgLatency * float64(s.Count)
			}
		}
		if err := addRaw(t, r.Start, a); err != nil {
			return nil, err
		}
		if err := addRaw(t, b, r.End); err != nil {
			return nil, err
		}
	}
	return tallies, nil
}

// GetRule retrieves an alert rule by ID
func (db *CobaltDB) GetRule(ctx context.Context, id string) (*core.AlertRule, error) {
	key := fmt.Sprintf("default/rules/%s", id)
	data, err := db.Get(key)
	if err != nil {
		return nil, err
	}
	var rule core.AlertRule
	if err := json.Unmarshal(data, &rule); err != nil {
		return nil, fmt.Errorf("failed to unmarshal rule: %w", err)
	}
	return &rule, nil
}

// ListRules returns all rules in a workspace
func (db *CobaltDB) ListRules(ctx context.Context, workspaceID string) ([]*core.AlertRule, error) {
	if workspaceID == "" {
		workspaceID = "default"
	}
	prefix := fmt.Sprintf("%s/rules/", workspaceID)
	results, err := db.PrefixScan(prefix)
	if err != nil {
		return nil, err
	}

	rules := make([]*core.AlertRule, 0, len(results))
	for _, data := range results {
		if data == nil {
			continue
		}
		var rule core.AlertRule
		if err := json.Unmarshal(data, &rule); err != nil {
			db.logger.Warn("failed to unmarshal rule", "err", err)
			continue
		}
		rules = append(rules, &rule)
	}
	return rules, nil
}

// SaveRule saves an alert rule
func (db *CobaltDB) SaveRule(ctx context.Context, rule *core.AlertRule) error {
	key := fmt.Sprintf("default/rules/%s", rule.ID)
	data, _ := json.Marshal(rule)
	return db.Put(key, data)
}

// DeleteRule removes an alert rule
func (db *CobaltDB) DeleteRule(ctx context.Context, id string) error {
	return db.Delete(fmt.Sprintf("default/rules/%s", id))
}

// System config storage

// SaveSystemConfig saves global system configuration
func (db *CobaltDB) SaveSystemConfig(ctx context.Context, key string, value []byte) error {
	sysKey := fmt.Sprintf("system/config/%s", key)
	return db.Put(sysKey, value)
}

// GetSystemConfig retrieves system configuration
func (db *CobaltDB) GetSystemConfig(ctx context.Context, key string) ([]byte, error) {
	sysKey := fmt.Sprintf("system/config/%s", key)
	return db.Get(sysKey)
}

// Node registry

// SaveJackal registers a cluster node
func (db *CobaltDB) SaveJackal(ctx context.Context, nodeID, address, region string) error {
	data := map[string]string{
		"id":      nodeID,
		"address": address,
		"region":  region,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("system/jackals/%s", nodeID)
	return db.Put(key, jsonData)
}

// ListJackals returns all registered nodes
func (db *CobaltDB) ListJackals(ctx context.Context) (map[string]map[string]string, error) {
	prefix := "system/jackals/"
	results, err := db.PrefixScan(prefix)
	if err != nil {
		return nil, err
	}

	jackals := make(map[string]map[string]string)
	for key, data := range results {
		if data == nil {
			continue
		}
		var node map[string]string
		if err := json.Unmarshal(data, &node); err != nil {
			continue
		}
		id := strings.TrimPrefix(key, prefix)
		jackals[id] = node
	}

	return jackals, nil
}

// Raft state storage

// SaveRaftState saves Raft persistent state
func (db *CobaltDB) SaveRaftState(ctx context.Context, currentTerm uint64, votedFor string) error {
	data := map[string]any{
		"current_term": currentTerm,
		"voted_for":    votedFor,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	return db.Put("raft/state", jsonData)
}

// GetRaftState retrieves Raft persistent state
func (db *CobaltDB) GetRaftState(ctx context.Context) (currentTerm uint64, votedFor string, err error) {
	data, err := db.Get("raft/state")
	if err != nil {
		return 0, "", err
	}

	var state struct {
		CurrentTerm uint64 `json:"current_term"`
		VotedFor    string `json:"voted_for"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return 0, "", err
	}

	return state.CurrentTerm, state.VotedFor, nil
}

// SaveRaftLogEntry saves a Raft log entry
func (db *CobaltDB) SaveRaftLogEntry(ctx context.Context, index uint64, term uint64, data []byte) error {
	entry := map[string]any{
		"index": index,
		"term":  term,
		"data":  data,
	}

	jsonData, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("raft/log/%d", index)
	return db.Put(key, jsonData)
}

// GetRaftLogEntry retrieves a Raft log entry
func (db *CobaltDB) GetRaftLogEntry(ctx context.Context, index uint64) (term uint64, data []byte, err error) {
	key := fmt.Sprintf("raft/log/%d", index)
	entryData, err := db.Get(key)
	if err != nil {
		return 0, nil, err
	}

	var entry struct {
		Term uint64 `json:"term"`
		Data []byte `json:"data"`
	}
	if err := json.Unmarshal(entryData, &entry); err != nil {
		return 0, nil, err
	}

	return entry.Term, entry.Data, nil
}

// Alert storage methods for alert manager

// SaveAlertChannel saves an alert channel
func (db *CobaltDB) SaveAlertChannel(ch *core.AlertChannel) error {
	if err := validateResourceID(ch.ID, "channel"); err != nil {
		return err
	}
	ws := ch.WorkspaceID
	if ws == "" {
		ws = "default"
	}
	key := fmt.Sprintf("%s/alerts/channels/%s", ws, ch.ID)
	data, err := json.Marshal(ch)
	if err != nil {
		return fmt.Errorf("failed to marshal channel: %w", err)
	}
	if err := db.Put(key, data); err != nil {
		return err
	}
	// Update O(1) lookup index. db.mu guards channelIndex (and
	// workspaceOrder). The previous version skipped this and the
	// in-memory index only got repopulated by rebuildSecondaryIndexes
	// on restart, so a long-running cluster would return stale
	// channels via GetChannelNoCtx.
	db.mu.Lock()
	db.channelIndex[ch.ID] = ws
	db.recordWorkspaceLocked(ws)
	db.mu.Unlock()
	return nil
}

// GetAlertChannel retrieves an alert channel by ID
func (db *CobaltDB) GetAlertChannel(id string, workspace string) (*core.AlertChannel, error) {
	if workspace == "" {
		workspace = "default"
	}
	key := fmt.Sprintf("%s/alerts/channels/%s", workspace, id)
	data, err := db.Get(key)
	if err != nil {
		return nil, err
	}
	var ch core.AlertChannel
	if err := json.Unmarshal(data, &ch); err != nil {
		return nil, fmt.Errorf("failed to unmarshal channel: %w", err)
	}
	return &ch, nil
}

// ListAlertChannels returns all alert channels
func (db *CobaltDB) ListAlertChannels(workspace string) ([]*core.AlertChannel, error) {
	if workspace == "" {
		workspace = "default"
	}
	prefix := fmt.Sprintf("%s/alerts/channels/", workspace)
	results, err := db.PrefixScan(prefix)
	if err != nil {
		return nil, err
	}

	channels := make([]*core.AlertChannel, 0, len(results))
	for _, data := range results {
		if data == nil {
			continue
		}
		var ch core.AlertChannel
		if err := json.Unmarshal(data, &ch); err != nil {
			db.logger.Warn("failed to unmarshal channel", "err", err)
			continue
		}
		channels = append(channels, &ch)
	}
	return channels, nil
}

// DeleteAlertChannel removes an alert channel
func (db *CobaltDB) DeleteAlertChannel(id string, workspace string) error {
	if workspace == "" {
		workspace = "default"
	}
	key := fmt.Sprintf("%s/alerts/channels/%s", workspace, id)
	if err := db.Delete(key); err != nil {
		return err
	}
	// Remove the secondary index entry. db.mu guards channelIndex.
	db.mu.Lock()
	delete(db.channelIndex, id)
	db.mu.Unlock()
	return nil
}

// SaveAlertRule saves an alert rule
func (db *CobaltDB) SaveAlertRule(rule *core.AlertRule) error {
	if err := validateResourceID(rule.ID, "rule"); err != nil {
		return err
	}
	ws := rule.WorkspaceID
	if ws == "" {
		ws = "default"
	}
	key := fmt.Sprintf("%s/alerts/rules/%s", ws, rule.ID)
	data, err := json.Marshal(rule)
	if err != nil {
		return fmt.Errorf("failed to marshal rule: %w", err)
	}
	if err := db.Put(key, data); err != nil {
		return err
	}
	// Update O(1) lookup index. db.mu guards ruleIndex. Same fix
	// as SaveAlertChannel — without this, the in-memory ruleIndex
	// was only repopulated on restart via rebuildSecondaryIndexes.
	db.mu.Lock()
	db.ruleIndex[rule.ID] = ws
	db.recordWorkspaceLocked(ws)
	db.mu.Unlock()
	return nil
}

// GetAlertRule retrieves an alert rule by ID
func (db *CobaltDB) GetAlertRule(id string, workspace string) (*core.AlertRule, error) {
	if workspace == "" {
		workspace = "default"
	}
	key := fmt.Sprintf("%s/alerts/rules/%s", workspace, id)
	data, err := db.Get(key)
	if err != nil {
		return nil, err
	}
	var rule core.AlertRule
	if err := json.Unmarshal(data, &rule); err != nil {
		return nil, fmt.Errorf("failed to unmarshal rule: %w", err)
	}
	return &rule, nil
}

// ListAlertRules returns all alert rules
func (db *CobaltDB) ListAlertRules(workspace string) ([]*core.AlertRule, error) {
	if workspace == "" {
		workspace = "default"
	}
	prefix := fmt.Sprintf("%s/alerts/rules/", workspace)
	results, err := db.PrefixScan(prefix)
	if err != nil {
		return nil, err
	}

	rules := make([]*core.AlertRule, 0, len(results))
	for _, data := range results {
		if data == nil {
			continue
		}
		var rule core.AlertRule
		if err := json.Unmarshal(data, &rule); err != nil {
			db.logger.Warn("failed to unmarshal rule", "err", err)
			continue
		}
		rules = append(rules, &rule)
	}
	return rules, nil
}

// DeleteAlertRule removes an alert rule
func (db *CobaltDB) DeleteAlertRule(id string, workspace string) error {
	if workspace == "" {
		workspace = "default"
	}
	key := fmt.Sprintf("%s/alerts/rules/%s", workspace, id)
	if err := db.Delete(key); err != nil {
		return err
	}
	// Remove the secondary index entry. db.mu guards ruleIndex.
	db.mu.Lock()
	delete(db.ruleIndex, id)
	db.mu.Unlock()
	return nil
}

// SaveAlertEvent saves an alert event
func (db *CobaltDB) SaveAlertEvent(event *core.AlertEvent) error {
	if event.ID == "" {
		event.ID = core.GenerateID()
	}
	workspaceID := defaultWorkspace(event.WorkspaceID)
	event.WorkspaceID = workspaceID
	key := fmt.Sprintf("%s/alerts/events/%s/%d/%s", workspaceID, event.SoulID, event.Timestamp.UnixNano(), event.ID)
	data, _ := json.Marshal(event)
	if err := db.Put(key, data); err != nil {
		return err
	}
	// Update O(1) lookup index. db.mu guards both alertEventIndex and
	// workspaceOrder.
	db.mu.Lock()
	db.alertEventIndex[event.ID] = workspaceID
	db.recordWorkspaceLocked(workspaceID)
	db.mu.Unlock()
	return nil
}

// ListAlertEvents returns alert events for a soul
func (db *CobaltDB) ListAlertEvents(soulID string, limit int) ([]*core.AlertEvent, error) {
	// Iterate known workspaces and run a per-workspace PrefixScan instead
	// of a PrefixScan("") that walks the entire B+Tree.
	workspaces := db.ListWorkspaceIDs()
	if len(workspaces) == 0 {
		// Fallback for cold-start or empty indexes: try the "default"
		// workspace which is always present in the key format.
		workspaces = []string{"default"}
	}

	events := make([]*core.AlertEvent, 0)
	for _, ws := range workspaces {
		prefix := fmt.Sprintf("%s/alerts/events/", ws)
		if soulID != "" {
			// With no soul filter (the optional soul_id of the gRPC verdict
			// RPCs) the soul segment must be left out, or the prefix ends in
			// "//" and matches no key.
			prefix = fmt.Sprintf("%s/alerts/events/%s/", ws, soulID)
		}
		results, err := db.PrefixScan(prefix)
		if err != nil {
			return nil, err
		}
		for key, data := range results {
			if data == nil {
				continue
			}
			var event core.AlertEvent
			if err := json.Unmarshal(data, &event); err != nil {
				db.logger.Warn("failed to unmarshal event", "err", err)
				continue
			}
			if event.WorkspaceID == "" {
				event.WorkspaceID = strings.SplitN(key, "/", 2)[0]
			}
			events = append(events, &event)
		}
	}

	// Sort by timestamp descending
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.After(events[j].Timestamp)
	})

	if limit > 0 && len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

// SaveIncident saves an incident
func (db *CobaltDB) SaveIncident(incident *core.Incident) error {
	workspaceID := defaultWorkspace(incident.WorkspaceID)
	incident.WorkspaceID = workspaceID
	key := fmt.Sprintf("%s/alerts/incidents/%s", workspaceID, incident.ID)
	data, _ := json.Marshal(incident)
	if err := db.Put(key, data); err != nil {
		return err
	}
	db.mu.Lock()
	db.incidentIndex[incident.ID] = workspaceID
	db.recordWorkspaceLocked(workspaceID)
	db.mu.Unlock()
	return nil
}

// GetIncident retrieves an incident by ID using the secondary index (O(1))
// instead of a full PrefixScan("") sweep.
func (db *CobaltDB) GetIncident(id string) (*core.Incident, error) {
	db.mu.RLock()
	workspaceID, ok := db.incidentIndex[id]
	db.mu.RUnlock()
	if !ok {
		return nil, &core.NotFoundError{Entity: "incident", ID: id}
	}

	key := fmt.Sprintf("%s/alerts/incidents/%s", workspaceID, id)
	data, err := db.Get(key)
	if err != nil {
		return nil, err
	}
	var incident core.Incident
	if err := json.Unmarshal(data, &incident); err != nil {
		return nil, fmt.Errorf("failed to unmarshal incident: %w", err)
	}
	return &incident, nil
}

// ListActiveIncidents returns all non-resolved incidents across workspaces
// using the workspace index to avoid a full PrefixScan("") sweep.
func (db *CobaltDB) ListActiveIncidents() ([]*core.Incident, error) {
	workspaces := db.ListWorkspaceIDs()
	if len(workspaces) == 0 {
		workspaces = []string{"default"}
	}

	incidents := make([]*core.Incident, 0)
	for _, ws := range workspaces {
		prefix := fmt.Sprintf("%s/alerts/incidents/", ws)
		results, err := db.PrefixScan(prefix)
		if err != nil {
			return nil, err
		}
		for key, data := range results {
			if data == nil {
				continue
			}
			var incident core.Incident
			if err := json.Unmarshal(data, &incident); err != nil {
				db.logger.Warn("failed to unmarshal incident", "err", err)
				continue
			}
			if incident.WorkspaceID == "" {
				incident.WorkspaceID = strings.SplitN(key, "/", 2)[0]
			}
			if incident.Status != core.IncidentResolved {
				incidents = append(incidents, &incident)
			}
		}
	}
	return incidents, nil
}

// StatusPage repository methods

func (db *CobaltDB) SaveStatusPage(page *core.StatusPage) error {
	if err := validateResourceID(page.ID, "statuspage"); err != nil {
		return err
	}
	workspaceID := defaultWorkspace(page.WorkspaceID)
	page.WorkspaceID = workspaceID
	key := fmt.Sprintf("%s/statuspages/%s", workspaceID, page.ID)
	data, _ := json.Marshal(page)
	if err := db.Put(key, data); err != nil {
		return err
	}
	db.mu.Lock()
	db.statusPageIndex[page.ID] = workspaceID
	db.recordWorkspaceLocked(workspaceID)
	db.mu.Unlock()
	return nil
}

func (db *CobaltDB) GetStatusPage(id string) (*core.StatusPage, error) {
	data, err := db.findStatusPageData(id)
	if err != nil {
		return nil, err
	}
	var page core.StatusPage
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func (db *CobaltDB) GetStatusPageByDomain(domain string) (*core.StatusPage, error) {
	pages, err := db.ListStatusPages()
	if err != nil {
		return nil, err
	}
	for _, page := range pages {
		if page.CustomDomain == domain && page.Enabled {
			return page, nil
		}
	}
	return nil, &core.NotFoundError{Entity: "statuspage", ID: domain}
}

func (db *CobaltDB) GetStatusPageBySlug(slug string) (*core.StatusPage, error) {
	pages, err := db.ListStatusPages()
	if err != nil {
		return nil, err
	}
	for _, page := range pages {
		if page.Slug == slug && page.Enabled {
			return page, nil
		}
	}
	return nil, &core.NotFoundError{Entity: "statuspage", ID: slug}
}

func (db *CobaltDB) ListStatusPages() ([]*core.StatusPage, error) {
	// Iterate known workspaces and run a per-workspace PrefixScan instead
	// of a full PrefixScan("") sweep.
	workspaces := db.ListWorkspaceIDs()
	if len(workspaces) == 0 {
		workspaces = []string{"default"}
	}
	pages := make([]*core.StatusPage, 0)
	for _, ws := range workspaces {
		prefix := fmt.Sprintf("%s/statuspages/", ws)
		results, err := db.PrefixScan(prefix)
		if err != nil {
			return nil, err
		}
		for key, data := range results {
			// Skip nested resources (subscriptions) — they're under the
			// statuspages/ prefix but aren't status pages themselves.
			if strings.Contains(key, "/statuspages/subscriptions/") {
				continue
			}
			var page core.StatusPage
			if err := json.Unmarshal(data, &page); err != nil {
				db.logger.Warn("failed to unmarshal status page", "err", err)
				continue
			}
			pages = append(pages, &page)
		}
	}
	return pages, nil
}

func (db *CobaltDB) DeleteStatusPage(id string) error {
	db.mu.RLock()
	workspaceID, ok := db.statusPageIndex[id]
	db.mu.RUnlock()
	if !ok {
		return &core.NotFoundError{Entity: "statuspage", ID: id}
	}
	if err := db.Delete(fmt.Sprintf("%s/statuspages/%s", workspaceID, id)); err != nil {
		return err
	}
	db.mu.Lock()
	delete(db.statusPageIndex, id)
	db.mu.Unlock()
	return nil
}

// findStatusPageData retrieves the raw bytes for a status page using the
// secondary index (O(1) lookup) instead of a full PrefixScan("") sweep.
// Returns the raw value (decrypted if needed) ready for unmarshaling.
func (db *CobaltDB) findStatusPageData(id string) ([]byte, error) {
	db.mu.RLock()
	workspaceID, ok := db.statusPageIndex[id]
	db.mu.RUnlock()
	if !ok {
		return nil, &core.NotFoundError{Entity: "statuspage", ID: id}
	}
	return db.Get(fmt.Sprintf("%s/statuspages/%s", workspaceID, id))
}

func (db *CobaltDB) SaveStatusPageSubscription(sub *core.StatusPageSubscription) error {
	key := fmt.Sprintf("default/statuspages/subscriptions/%s", sub.ID)
	data, _ := json.Marshal(sub)
	return db.Put(key, data)
}

func (db *CobaltDB) GetSubscriptionsByPage(pageID string) ([]*core.StatusPageSubscription, error) {
	keyPrefix := "default/statuspages/subscriptions/"
	results, err := db.PrefixScan(keyPrefix)
	if err != nil {
		return nil, err
	}

	var subscriptions []*core.StatusPageSubscription
	for _, data := range results {
		var sub core.StatusPageSubscription
		if err := json.Unmarshal(data, &sub); err != nil {
			continue
		}
		if sub.PageID == pageID {
			subscriptions = append(subscriptions, &sub)
		}
	}
	return subscriptions, nil
}

// ConfirmedSubscriberCounts returns the number of confirmed subscriptions per
// status page ID, in one scan. Pending (unconfirmed) subscriptions are not
// counted. Pages without a confirmed subscriber are absent from the map.
func (db *CobaltDB) ConfirmedSubscriberCounts() (map[string]int, error) {
	results, err := db.PrefixScan("default/statuspages/subscriptions/")
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int)
	for _, data := range results {
		var sub core.StatusPageSubscription
		if err := json.Unmarshal(data, &sub); err != nil {
			continue
		}
		if sub.Confirmed && sub.PageID != "" {
			counts[sub.PageID]++
		}
	}
	return counts, nil
}

func (db *CobaltDB) DeleteStatusPageSubscription(subscriptionID string) error {
	key := fmt.Sprintf("default/statuspages/subscriptions/%s", subscriptionID)
	return db.Delete(key)
}

// GetUptimeHistory returns one entry per UTC day for the last days days.
//
// Days that started after the soul's results began being counted into day
// summaries as they arrive (TimeSeriesStore, liveSinceKey) are read from
// those summaries: one lookup per day. Only older days fall back to scanning
// raw judgments, so once raw retention has outlived them the soul's raw
// history is never scanned here.
func (db *CobaltDB) GetUptimeHistory(soulID string, days int) ([]core.UptimeDay, error) {
	workspaceID := "default"
	if soul, err := db.GetSoulNoCtx(soulID); err == nil {
		workspaceID = defaultWorkspace(soul.WorkspaceID)
	}

	type tally struct {
		up    int
		total int
	}
	dayStats := make(map[string]tally, days)

	var liveSince time.Time
	if raw, err := db.Get(liveSinceKey(workspaceID, soulID)); err == nil {
		if t, err := time.Parse(time.RFC3339Nano, string(raw)); err == nil {
			liveSince = t
		}
	}

	now := time.Now().UTC()
	rawDays := make(map[string]bool)
	for i := 0; i < days; i++ {
		d := now.AddDate(0, 0, -i)
		dayStart := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
		date := dayStart.Format("2006-01-02")
		dayStats[date] = tally{}

		if liveSince.IsZero() || dayStart.Before(liveSince) {
			rawDays[date] = true // not (fully) covered by live day summaries
			continue
		}
		key := fmt.Sprintf("%s/ts/%s/%s/%d", workspaceID, soulID, Resolution1Day, dayStart.Unix())
		data, err := db.Get(key)
		if err != nil {
			continue // no checks that day
		}
		var summary JudgmentSummary
		if err := json.Unmarshal(data, &summary); err != nil {
			continue
		}
		dayStats[date] = tally{up: summary.SuccessCount, total: summary.Count}
	}

	if len(rawDays) > 0 {
		keyPrefix := fmt.Sprintf("%s/judgments/%s/", workspaceID, soulID)
		results, err := db.PrefixScan(keyPrefix)
		if err != nil {
			return nil, err
		}
		for _, data := range results {
			var judgment core.Judgment
			if err := json.Unmarshal(data, &judgment); err != nil {
				continue
			}
			day := judgment.Timestamp.UTC().Format("2006-01-02")
			if rawDays[day] {
				stats := dayStats[day]
				stats.total++
				if judgment.Status == core.SoulAlive {
					stats.up++
				}
				dayStats[day] = stats
			}
		}
	}

	// Convert to UptimeDay slice
	uptimeDays := make([]core.UptimeDay, 0, len(dayStats))
	for date, stats := range dayStats {
		uptime := 0.0
		status := "unknown"
		if stats.total > 0 {
			uptime = float64(stats.up) / float64(stats.total) * 100
			status = "operational"
			if uptime < 99 {
				status = "degraded"
			}
			if uptime < 95 {
				status = "dead"
			}
		}
		uptimeDays = append(uptimeDays, core.UptimeDay{
			Date:   date,
			Status: status,
			Uptime: uptime,
		})
	}

	// Sort by date
	sort.Slice(uptimeDays, func(i, j int) bool {
		return uptimeDays[i].Date < uptimeDays[j].Date
	})

	return uptimeDays, nil
}

func (db *CobaltDB) SaveDashboard(dashboard *core.CustomDashboard) error {
	if err := validateResourceID(dashboard.ID, "dashboard"); err != nil {
		return err
	}
	workspaceID := defaultWorkspace(dashboard.WorkspaceID)
	dashboard.WorkspaceID = workspaceID
	key := fmt.Sprintf("%s/dashboards/%s", workspaceID, dashboard.ID)
	data, err := json.Marshal(dashboard)
	if err != nil {
		return fmt.Errorf("failed to marshal dashboard: %w", err)
	}
	if err := db.Put(key, data); err != nil {
		return err
	}
	db.mu.Lock()
	db.dashboardIndex[dashboard.ID] = workspaceID
	db.recordWorkspaceLocked(workspaceID)
	db.mu.Unlock()
	return nil
}

func (db *CobaltDB) GetDashboard(id string) (*core.CustomDashboard, error) {
	data, err := db.findDashboardData(id)
	if err != nil {
		return nil, err
	}
	var dashboard core.CustomDashboard
	if err := json.Unmarshal(data, &dashboard); err != nil {
		return nil, err
	}
	return &dashboard, nil
}

func (db *CobaltDB) ListDashboards() ([]*core.CustomDashboard, error) {
	// Iterate known workspaces and run a per-workspace PrefixScan instead
	// of a full PrefixScan("") sweep.
	workspaces := db.ListWorkspaceIDs()
	if len(workspaces) == 0 {
		workspaces = []string{"default"}
	}
	dashboards := make([]*core.CustomDashboard, 0)
	for _, ws := range workspaces {
		prefix := fmt.Sprintf("%s/dashboards/", ws)
		results, err := db.PrefixScan(prefix)
		if err != nil {
			return nil, err
		}
		for _, data := range results {
			var dashboard core.CustomDashboard
			if err := json.Unmarshal(data, &dashboard); err != nil {
				db.logger.Warn("failed to unmarshal dashboard", "err", err)
				continue
			}
			dashboards = append(dashboards, &dashboard)
		}
	}
	return dashboards, nil
}

func (db *CobaltDB) DeleteDashboard(id string) error {
	db.mu.RLock()
	workspaceID, ok := db.dashboardIndex[id]
	db.mu.RUnlock()
	if !ok {
		return &core.NotFoundError{Entity: "dashboard", ID: id}
	}
	if err := db.Delete(fmt.Sprintf("%s/dashboards/%s", workspaceID, id)); err != nil {
		return err
	}
	db.mu.Lock()
	delete(db.dashboardIndex, id)
	db.mu.Unlock()
	return nil
}

func (db *CobaltDB) findDashboardData(id string) ([]byte, error) {
	db.mu.RLock()
	workspaceID, ok := db.dashboardIndex[id]
	db.mu.RUnlock()
	if !ok {
		return nil, &core.NotFoundError{Entity: "dashboard", ID: id}
	}
	return db.Get(fmt.Sprintf("%s/dashboards/%s", workspaceID, id))
}

// MaintenanceWindow storage methods

func (db *CobaltDB) SaveMaintenanceWindow(w *core.MaintenanceWindow) error {
	if w.ID == "" {
		w.ID = core.GenerateID()
	} else if err := validateResourceID(w.ID, "maintenance"); err != nil {
		return err
	}
	workspaceID := defaultWorkspace(w.WorkspaceID)
	w.WorkspaceID = workspaceID
	key := fmt.Sprintf("%s/maintenance/%s", workspaceID, w.ID)
	data, err := json.Marshal(w)
	if err != nil {
		return fmt.Errorf("failed to marshal maintenance window: %w", err)
	}
	if err := db.Put(key, data); err != nil {
		return err
	}
	db.mu.Lock()
	db.maintenanceIndex[w.ID] = workspaceID
	db.recordWorkspaceLocked(workspaceID)
	db.mu.Unlock()
	return nil
}

func (db *CobaltDB) GetMaintenanceWindow(id string) (*core.MaintenanceWindow, error) {
	data, err := db.findMaintenanceWindowData(id)
	if err != nil {
		return nil, err
	}
	var w core.MaintenanceWindow
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("failed to unmarshal maintenance window: %w", err)
	}
	return &w, nil
}

func (db *CobaltDB) ListMaintenanceWindows() ([]*core.MaintenanceWindow, error) {
	// Iterate known workspaces and run a per-workspace PrefixScan instead
	// of a full PrefixScan("") sweep.
	workspaces := db.ListWorkspaceIDs()
	if len(workspaces) == 0 {
		workspaces = []string{"default"}
	}
	windows := make([]*core.MaintenanceWindow, 0)
	for _, ws := range workspaces {
		prefix := fmt.Sprintf("%s/maintenance/", ws)
		results, err := db.PrefixScan(prefix)
		if err != nil {
			return nil, err
		}
		for _, data := range results {
			if data == nil {
				continue
			}
			var w core.MaintenanceWindow
			if err := json.Unmarshal(data, &w); err != nil {
				db.logger.Warn("failed to unmarshal maintenance window", "err", err)
				continue
			}
			windows = append(windows, &w)
		}
	}
	return windows, nil
}

func (db *CobaltDB) DeleteMaintenanceWindow(id string) error {
	db.mu.RLock()
	workspaceID, ok := db.maintenanceIndex[id]
	db.mu.RUnlock()
	if !ok {
		return &core.NotFoundError{Entity: "maintenance", ID: id}
	}
	if err := db.Delete(fmt.Sprintf("%s/maintenance/%s", workspaceID, id)); err != nil {
		return err
	}
	db.mu.Lock()
	delete(db.maintenanceIndex, id)
	db.mu.Unlock()
	return nil
}

func (db *CobaltDB) findMaintenanceWindowData(id string) ([]byte, error) {
	db.mu.RLock()
	workspaceID, ok := db.maintenanceIndex[id]
	db.mu.RUnlock()
	if !ok {
		return nil, &core.NotFoundError{Entity: "maintenance", ID: id}
	}
	return db.Get(fmt.Sprintf("%s/maintenance/%s", workspaceID, id))
}

// ApplyFSMCommand applies a consensus-committed FSM command to CobaltDB and
// updates the relevant secondary index so subsequent typed lookups work.
// This is the write path used by the Raft FSM when clustering is active.
func (db *CobaltDB) ApplyFSMCommand(cmd *core.FSMCommand) error {
	switch cmd.Op {
	case core.FSMSet:
		if err := db.Set(cmd.Key, cmd.Value); err != nil {
			return err
		}
		db.updateIndexForSet(cmd.Key)

	case core.FSMDelete:
		db.updateIndexForDelete(cmd.Key)
		if err := db.Delete(cmd.Key); err != nil {
			return err
		}

	case core.FSMDeletePrefix:
		results, err := db.PrefixScan(cmd.Key)
		if err != nil {
			return err
		}
		for k := range results {
			db.updateIndexForDelete(k)
		}
		if err := db.DeletePrefix(cmd.Key); err != nil {
			return err
		}
	}
	return nil
}

// DurableState reports that CobaltDB's contents survive a restart (WAL).
func (db *CobaltDB) DurableState() bool { return true }

// ReplaceApplicationState replaces every key outside "raft/" with data, in
// batched WAL frames. Like the per-key restore it replaces, it is not atomic:
// a crash midway leaves a mix, which the node repairs by restoring again.
func (db *CobaltDB) ReplaceApplicationState(data map[string][]byte) error {
	keys, err := db.List("")
	if err != nil {
		return err
	}
	var stale []string
	for _, key := range keys {
		if strings.HasPrefix(key, "raft/") {
			continue
		}
		if _, kept := data[key]; !kept {
			stale = append(stale, key)
		}
	}
	for len(stale) > 0 {
		n := min(len(stale), 2048)
		if err := db.deleteMany(stale[:n]); err != nil {
			return err
		}
		stale = stale[n:]
	}
	values := make(map[string][]byte)
	size := 0
	for key, value := range data {
		if strings.HasPrefix(key, "raft/") {
			continue
		}
		cost := walBatchCost(key, value)
		if size > 0 && size+cost > walBatchBudget {
			if err := db.putMany(values); err != nil {
				return err
			}
			values, size = make(map[string][]byte), 0
		}
		values[key] = value
		size += cost
	}
	if len(values) > 0 {
		return db.putMany(values)
	}
	return nil
}

// ApplyFSMSets applies consecutive FSMSet commands like ApplyFSMCommand would,
// in as few WAL frames (and fsyncs) as fit. A later set of the same key wins,
// as it would applied in order.
func (db *CobaltDB) ApplyFSMSets(cmds []core.FSMCommand) error {
	values := make(map[string][]byte)
	size := 0
	flush := func() error {
		if len(values) == 0 {
			return nil
		}
		err := db.putMany(values)
		values, size = make(map[string][]byte), 0
		return err
	}
	for i := range cmds {
		cost := walBatchCost(cmds[i].Key, cmds[i].Value)
		if size > 0 && size+cost > walBatchBudget {
			if err := flush(); err != nil {
				return err
			}
		}
		values[cmds[i].Key] = cmds[i].Value
		size += cost
	}
	if err := flush(); err != nil {
		return err
	}
	for i := range cmds {
		db.updateIndexForSet(cmds[i].Key)
	}
	return nil
}

// indexedResource maps a key's type and remainder onto the secondary index
// they belong to, using the same layout rebuildIndexes parses: alert channels,
// rules, incidents and events are nested under "alerts", and an event's
// indexed ID is the last of its "{soul}/{ts}/{id}" segments. Replicated writes
// arrive here rather than through the Save* helpers, so without this mapping a
// channel, rule or judgment applied on a follower was stored but unreachable
// through the index-backed lookups.
func indexedResource(resourceType, resourceID string) (string, string) {
	if resourceType != "alerts" {
		return resourceType, resourceID
	}
	sub := strings.SplitN(resourceID, "/", 2)
	if len(sub) != 2 {
		return resourceType, resourceID
	}
	switch sub[0] {
	case "channels", "rules", "incidents":
		return sub[0], sub[1]
	case "events":
		eventParts := strings.SplitN(sub[1], "/", 3)
		if len(eventParts) == 3 {
			return "events", eventParts[2]
		}
	}
	return resourceType, resourceID
}

// updateIndexForSet updates the in-memory secondary index for a stored key
// using the same workspace/resourceType/resourceID parsing as rebuildIndexes.
func (db *CobaltDB) updateIndexForSet(key string) {
	parts := strings.SplitN(key, "/", 3)
	if len(parts) < 3 {
		return
	}
	workspaceID := parts[0]
	resourceType, resourceID := indexedResource(parts[1], parts[2])

	db.mu.Lock()
	defer db.mu.Unlock()
	db.recordWorkspaceLocked(workspaceID)
	switch resourceType {
	case "judgment-idx":
		db.judgmentIndex[resourceID] = key
	case "events":
		db.alertEventIndex[resourceID] = workspaceID
	case "souls":
		db.soulIndex[resourceID] = workspaceID
	case "channels":
		db.channelIndex[resourceID] = workspaceID
	case "rules":
		db.ruleIndex[resourceID] = workspaceID
	case "journeys":
		db.journeyIndex[resourceID] = workspaceID
	case "incidents":
		db.incidentIndex[resourceID] = workspaceID
	case "statuspages":
		if !strings.HasPrefix(resourceID, "subscriptions/") {
			db.statusPageIndex[resourceID] = workspaceID
		}
	case "dashboards":
		db.dashboardIndex[resourceID] = workspaceID
	case "maintenance":
		db.maintenanceIndex[resourceID] = workspaceID
	}
}

// updateIndexForDelete removes the key from the in-memory secondary index.
func (db *CobaltDB) updateIndexForDelete(key string) {
	parts := strings.SplitN(key, "/", 3)
	if len(parts) < 3 {
		return
	}
	resourceType, resourceID := indexedResource(parts[1], parts[2])

	db.mu.Lock()
	defer db.mu.Unlock()
	switch resourceType {
	case "judgment-idx":
		delete(db.judgmentIndex, resourceID)
	case "events":
		delete(db.alertEventIndex, resourceID)
	case "souls":
		delete(db.soulIndex, resourceID)
	case "channels":
		delete(db.channelIndex, resourceID)
	case "rules":
		delete(db.ruleIndex, resourceID)
	case "journeys":
		delete(db.journeyIndex, resourceID)
	case "incidents":
		delete(db.incidentIndex, resourceID)
	case "statuspages":
		if !strings.HasPrefix(resourceID, "subscriptions/") {
			delete(db.statusPageIndex, resourceID)
		}
	case "dashboards":
		delete(db.dashboardIndex, resourceID)
	case "maintenance":
		delete(db.maintenanceIndex, resourceID)
	}
}
