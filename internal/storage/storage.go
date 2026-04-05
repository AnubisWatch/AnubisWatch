package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

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

	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal verdict: %w", err)
	}

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

		if limit > 0 && len(verdicts) >= limit {
			break
		}
	}

	// Sort by fired time descending
	for i := 0; i < len(verdicts)-1; i++ {
		for j := i + 1; j < len(verdicts); j++ {
			if verdicts[i].FiredAt.Before(verdicts[j].FiredAt) {
				verdicts[i], verdicts[j] = verdicts[j], verdicts[i]
			}
		}
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
	}

	workspaceID := j.WorkspaceID
	if workspaceID == "" {
		workspaceID = "default"
	}

	key := fmt.Sprintf("%s/journeys/%s", workspaceID, j.ID)

	data, err := json.Marshal(j)
	if err != nil {
		return fmt.Errorf("failed to marshal journey: %w", err)
	}

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
	return db.Delete(key)
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

	key := fmt.Sprintf("%s/journey-runs/%s/%d", workspaceID, run.JourneyID, run.StartedAt)

	data, err := json.Marshal(run)
	if err != nil {
		return fmt.Errorf("failed to marshal journey run: %w", err)
	}

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
	for i := 0; i < len(runs)-1; i++ {
		for j := i + 1; j < len(runs); j++ {
			if runs[i].StartedAt < runs[j].StartedAt {
				runs[i], runs[j] = runs[j], runs[i]
			}
		}
	}

	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}

	return runs, nil
}

// SaveChannel saves an alert channel configuration
func (db *CobaltDB) SaveChannel(ctx context.Context, ch *core.ChannelConfig) error {
	workspaceID := "default"
	key := fmt.Sprintf("%s/channels/%s", workspaceID, ch.Name)

	data, err := json.Marshal(ch)
	if err != nil {
		return fmt.Errorf("failed to marshal channel: %w", err)
	}

	return db.Put(key, data)
}

// GetChannel retrieves a channel by name
func (db *CobaltDB) GetChannel(ctx context.Context, id string) (*core.ChannelConfig, error) {
	workspaceID := "default"
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
	workspaceID := "default"
	key := fmt.Sprintf("%s/channels/%s", workspaceID, id)
	return db.Delete(key)
}

// ListJudgments returns judgments for a soul within a time range
func (db *CobaltDB) ListJudgments(ctx context.Context, soulID string, start, end time.Time, limit int) ([]*core.Judgment, error) {
	prefix := fmt.Sprintf("default/judgments/%s/", soulID)
	results, err := db.PrefixScan(prefix)
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
		if j.Timestamp.After(start) && j.Timestamp.Before(end) {
			judgments = append(judgments, &j)
		}
	}

	if len(judgments) > limit {
		judgments = judgments[:limit]
	}
	return judgments, nil
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
	data, err := json.Marshal(rule)
	if err != nil {
		return fmt.Errorf("failed to marshal rule: %w", err)
	}
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
	data := map[string]interface{}{
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

	var state map[string]interface{}
	if err := json.Unmarshal(data, &state); err != nil {
		return 0, "", err
	}

	if term, ok := state["current_term"].(float64); ok {
		currentTerm = uint64(term)
	}
	if voted, ok := state["voted_for"].(string); ok {
		votedFor = voted
	}

	return currentTerm, votedFor, nil
}

// SaveRaftLogEntry saves a Raft log entry
func (db *CobaltDB) SaveRaftLogEntry(ctx context.Context, index uint64, term uint64, data []byte) error {
	entry := map[string]interface{}{
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

	var entry map[string]interface{}
	if err := json.Unmarshal(entryData, &entry); err != nil {
		return 0, nil, err
	}

	if t, ok := entry["term"].(float64); ok {
		term = uint64(t)
	}
	if d, ok := entry["data"].([]byte); ok {
		data = d
	}

	return term, data, nil
}

// Alert storage methods for alert manager

// SaveAlertChannel saves an alert channel
func (db *CobaltDB) SaveAlertChannel(ch *core.AlertChannel) error {
	key := fmt.Sprintf("default/alerts/channels/%s", ch.ID)
	data, err := json.Marshal(ch)
	if err != nil {
		return fmt.Errorf("failed to marshal channel: %w", err)
	}
	return db.Put(key, data)
}

// GetAlertChannel retrieves an alert channel by ID
func (db *CobaltDB) GetAlertChannel(id string) (*core.AlertChannel, error) {
	key := fmt.Sprintf("default/alerts/channels/%s", id)
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
func (db *CobaltDB) ListAlertChannels() ([]*core.AlertChannel, error) {
	prefix := "default/alerts/channels/"
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
func (db *CobaltDB) DeleteAlertChannel(id string) error {
	return db.Delete(fmt.Sprintf("default/alerts/channels/%s", id))
}

// SaveAlertRule saves an alert rule
func (db *CobaltDB) SaveAlertRule(rule *core.AlertRule) error {
	key := fmt.Sprintf("default/alerts/rules/%s", rule.ID)
	data, err := json.Marshal(rule)
	if err != nil {
		return fmt.Errorf("failed to marshal rule: %w", err)
	}
	return db.Put(key, data)
}

// GetAlertRule retrieves an alert rule by ID
func (db *CobaltDB) GetAlertRule(id string) (*core.AlertRule, error) {
	key := fmt.Sprintf("default/alerts/rules/%s", id)
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
func (db *CobaltDB) ListAlertRules() ([]*core.AlertRule, error) {
	prefix := "default/alerts/rules/"
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
func (db *CobaltDB) DeleteAlertRule(id string) error {
	return db.Delete(fmt.Sprintf("default/alerts/rules/%s", id))
}

// SaveAlertEvent saves an alert event
func (db *CobaltDB) SaveAlertEvent(event *core.AlertEvent) error {
	if event.ID == "" {
		event.ID = core.GenerateID()
	}
	key := fmt.Sprintf("default/alerts/events/%s/%d", event.SoulID, event.Timestamp.UnixNano())
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}
	return db.Put(key, data)
}

// ListAlertEvents returns alert events for a soul
func (db *CobaltDB) ListAlertEvents(soulID string, limit int) ([]*core.AlertEvent, error) {
	prefix := fmt.Sprintf("default/alerts/events/%s/", soulID)
	results, err := db.PrefixScan(prefix)
	if err != nil {
		return nil, err
	}

	events := make([]*core.AlertEvent, 0, len(results))
	for _, data := range results {
		if data == nil {
			continue
		}
		var event core.AlertEvent
		if err := json.Unmarshal(data, &event); err != nil {
			db.logger.Warn("failed to unmarshal event", "err", err)
			continue
		}
		events = append(events, &event)
	}

	// Sort by timestamp descending
	for i := 0; i < len(events)-1; i++ {
		for j := i + 1; j < len(events); j++ {
			if events[i].Timestamp.Before(events[j].Timestamp) {
				events[i], events[j] = events[j], events[i]
			}
		}
	}

	if limit > 0 && len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

// SaveIncident saves an incident
func (db *CobaltDB) SaveIncident(incident *core.Incident) error {
	key := fmt.Sprintf("default/alerts/incidents/%s", incident.ID)
	data, err := json.Marshal(incident)
	if err != nil {
		return fmt.Errorf("failed to marshal incident: %w", err)
	}
	return db.Put(key, data)
}

// GetIncident retrieves an incident by ID
func (db *CobaltDB) GetIncident(id string) (*core.Incident, error) {
	key := fmt.Sprintf("default/alerts/incidents/%s", id)
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

// ListActiveIncidents returns all non-resolved incidents
func (db *CobaltDB) ListActiveIncidents() ([]*core.Incident, error) {
	prefix := "default/alerts/incidents/"
	results, err := db.PrefixScan(prefix)
	if err != nil {
		return nil, err
	}

	incidents := make([]*core.Incident, 0, len(results))
	for _, data := range results {
		if data == nil {
			continue
		}
		var incident core.Incident
		if err := json.Unmarshal(data, &incident); err != nil {
			db.logger.Warn("failed to unmarshal incident", "err", err)
			continue
		}
		if incident.Status != core.IncidentResolved {
			incidents = append(incidents, &incident)
		}
	}
	return incidents, nil
}

// StatusPage repository methods

func (db *CobaltDB) SaveStatusPage(page *core.StatusPage) error {
	key := fmt.Sprintf("default/statuspages/%s", page.ID)
	data, err := json.Marshal(page)
	if err != nil {
		return err
	}
	return db.Put(key, data)
}

func (db *CobaltDB) GetStatusPage(id string) (*core.StatusPage, error) {
	key := fmt.Sprintf("default/statuspages/%s", id)
	data, err := db.Get(key)
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
	results, err := db.PrefixScan("default/statuspages/")
	if err != nil {
		return nil, err
	}
	pages := make([]*core.StatusPage, 0, len(results))
	for _, data := range results {
		var page core.StatusPage
		if err := json.Unmarshal(data, &page); err != nil {
			db.logger.Warn("failed to unmarshal status page", "err", err)
			continue
		}
		pages = append(pages, &page)
	}
	return pages, nil
}

func (db *CobaltDB) DeleteStatusPage(id string) error {
	key := fmt.Sprintf("default/statuspages/%s", id)
	return db.Delete(key)
}

func (db *CobaltDB) SaveStatusPageSubscription(sub *core.StatusPageSubscription) error {
	key := fmt.Sprintf("default/statuspages/subscriptions/%s", sub.ID)
	data, err := json.Marshal(sub)
	if err != nil {
		return err
	}
	return db.Put(key, data)
}

func (db *CobaltDB) GetSubscriptionsByPage(pageID string) ([]*core.StatusPageSubscription, error) {
	keyPrefix := fmt.Sprintf("default/statuspages/subscriptions/")
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

func (db *CobaltDB) DeleteStatusPageSubscription(subscriptionID string) error {
	key := fmt.Sprintf("default/statuspages/subscriptions/%s", subscriptionID)
	return db.Delete(key)
}

func (db *CobaltDB) GetUptimeHistory(soulID string, days int) ([]core.UptimeDay, error) {
	// Get judgments for the soul
	keyPrefix := fmt.Sprintf("default/judgments/%s/", soulID)
	results, err := db.PrefixScan(keyPrefix)
	if err != nil {
		return nil, err
	}

	// Collect judgments by date
	dayStats := make(map[string]struct {
		up    int
		total int
	})

	now := time.Now().UTC()
	for i := 0; i < days; i++ {
		day := now.AddDate(0, 0, -i).Format("2006-01-02")
		dayStats[day] = struct {
			up    int
			total int
		}{0, 0}
	}

	for _, data := range results {
		var judgment core.Judgment
		if err := json.Unmarshal(data, &judgment); err != nil {
			continue
		}
		day := judgment.Timestamp.Format("2006-01-02")
		if _, ok := dayStats[day]; ok {
			stats := dayStats[day]
			stats.total++
			if judgment.Status == core.SoulAlive {
				stats.up++
			}
			dayStats[day] = stats
		}
	}

	// Convert to UptimeDay slice
	uptimeDays := make([]core.UptimeDay, 0, len(dayStats))
	for date, stats := range dayStats {
		uptime := 0.0
		status := "operational"
		if stats.total > 0 {
			uptime = float64(stats.up) / float64(stats.total) * 100
			if uptime < 99 {
				status = "degraded"
			}
			if uptime < 95 {
				status = "down"
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
