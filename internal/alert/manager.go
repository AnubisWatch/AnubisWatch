package alert

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/cenkalti/backoff/v5"
)

// Manager handles alert routing and delivery
// The high priest who receives omens and dispatches messengers
type Manager struct {
	mu        sync.RWMutex
	channels  map[string]*core.AlertChannel
	rules     map[string]*core.AlertRule
	history   *core.AlertHistory
	incidents map[string]*core.Incident
	// failureStreak tracks consecutive non-alive judgments per (ruleID, soulID).
	// Used by the consecutive_failures condition to know when a streak crosses
	// its threshold. Reset to 0 by any non-failure judgment.
	failureStreak map[string]int

	// statusSince records when each soul first entered its current status, so
	// the status_for condition can honour its configured dwell time instead of
	// firing on the first matching judgment. Guarded by mu. Keyed by soulID and
	// holding only the latest status, so a status change resets the clock and
	// the map stays bounded by the number of souls rather than growing per rule.
	statusSince map[string]statusHold

	// Dispatchers
	dispatchers map[core.AlertChannelType]ChannelDispatcher

	// State
	running  bool
	stopping bool
	stopCh   chan struct{}
	stopDone chan struct{}
	queue    chan *core.AlertEvent

	// Worker tracking
	wg sync.WaitGroup

	// Statistics counters
	stats struct {
		totalAlerts        uint64
		sentAlerts         uint64
		failedAlerts       uint64
		acknowledgedAlerts uint64
		resolvedAlerts     uint64
		rateLimitedAlerts  uint64
		filteredAlerts     uint64
		lastAlertTime      time.Time
		verdictsBySeverity map[string]uint64
	}

	// Dependencies
	logger  *slog.Logger
	storage AlertStorage

	// Lifecycle hooks (see SetIncidentHook / SetAlertHook). Guarded by hookMu,
	// not mu, so a hook can call back into the manager without deadlocking.
	hookMu     sync.RWMutex
	onIncident func(*core.Incident)
	onAlert    func(*core.AlertEvent)
}

// SetIncidentHook registers fn to be called after an incident is opened,
// acknowledged, resolved (manually or automatically) or escalated. fn gets a
// snapshot it may keep; it runs outside the manager's locks and must not
// block. The API layer uses it to push live updates without alert importing
// api.
func (m *Manager) SetIncidentHook(fn func(*core.Incident)) {
	m.hookMu.Lock()
	m.onIncident = fn
	m.hookMu.Unlock()
}

// SetAlertHook registers fn to be called once for every alert event the
// worker dispatches to channels. fn gets a copy of the event; it runs on the
// dispatch worker and must not block.
func (m *Manager) SetAlertHook(fn func(*core.AlertEvent)) {
	m.hookMu.Lock()
	m.onAlert = fn
	m.hookMu.Unlock()
}

// incidentNotifier captures a snapshot for the incident hook. Callers must hold mu
// (read or write) so the copy is consistent, and call the returned func after
// releasing it.
func (m *Manager) incidentNotifier(incident *core.Incident) func() {
	m.hookMu.RLock()
	fn := m.onIncident
	m.hookMu.RUnlock()
	if fn == nil || incident == nil {
		return func() {}
	}
	snapshot := *incident
	snapshot.Events = append([]core.AlertEvent(nil), incident.Events...)
	snapshot.Notes = append([]core.IncidentNote(nil), incident.Notes...)
	return func() { fn(&snapshot) }
}

func (m *Manager) notifyAlert(event *core.AlertEvent) {
	m.hookMu.RLock()
	fn := m.onAlert
	m.hookMu.RUnlock()
	if fn == nil || event == nil {
		return
	}
	copied := *event
	fn(&copied)
}

// statusHold records when a soul first entered a given status.
type statusHold struct {
	Status core.SoulStatus
	Since  time.Time
}

// ChannelDispatcher sends notifications to a specific channel type
type ChannelDispatcher interface {
	Send(ctx context.Context, event *core.AlertEvent, channel *core.AlertChannel) error
	Validate(config map[string]any) error
}

// AlertStorage persists alert data
type AlertStorage interface {
	SaveChannel(channel *core.AlertChannel) error
	GetChannel(id string, workspace string) (*core.AlertChannel, error)
	ListChannels(workspace string) ([]*core.AlertChannel, error)
	DeleteChannel(id string, workspace string) error

	SaveRule(rule *core.AlertRule) error
	GetRule(id string, workspace string) (*core.AlertRule, error)
	ListRules(workspace string) ([]*core.AlertRule, error)
	DeleteRule(id string, workspace string) error

	SaveEvent(event *core.AlertEvent) error
	ListEvents(soulID string, limit int) ([]*core.AlertEvent, error)

	SaveIncident(incident *core.Incident) error
	GetIncident(id string) (*core.Incident, error)
	ListActiveIncidents() ([]*core.Incident, error)
}

type maintenanceWindowStorage interface {
	ListMaintenanceWindows() ([]*core.MaintenanceWindow, error)
}

// NewManager creates a new alert manager
func NewManager(storage AlertStorage, logger *slog.Logger) *Manager {
	m := &Manager{
		channels:      make(map[string]*core.AlertChannel),
		rules:         make(map[string]*core.AlertRule),
		history:       &core.AlertHistory{Entries: make(map[string]*core.AlertHistoryEntry)},
		incidents:     make(map[string]*core.Incident),
		failureStreak: make(map[string]int),
		statusSince:   make(map[string]statusHold),
		dispatchers:   make(map[core.AlertChannelType]ChannelDispatcher),
		stopCh:        make(chan struct{}),
		queue:         make(chan *core.AlertEvent, 1000),
		logger:        logger.With("component", "alert_manager"),
		storage:       storage,
	}
	m.stats.verdictsBySeverity = make(map[string]uint64)

	// Register built-in dispatchers
	m.registerDispatchers()

	return m
}

// Start starts the alert manager
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running || m.stopping {
		return nil
	}

	m.running = true
	m.stopDone = make(chan struct{})

	// Recreate stopCh if it was closed (allows restart after Stop)
	select {
	case <-m.stopCh:
		m.stopCh = make(chan struct{})
	default:
	}

	// Load channels and rules from storage
	if m.storage != nil {
		channels, err := m.storage.ListChannels("")
		if err != nil {
			m.logger.Warn("Failed to load channels", "error", err)
		} else {
			for _, ch := range channels {
				m.channels[ch.ID] = ch
			}
		}

		rules, err := m.storage.ListRules("")
		if err != nil {
			m.logger.Warn("Failed to load rules", "error", err)
		} else {
			for _, rule := range rules {
				m.rules[rule.ID] = rule
			}
		}

		incidents, err := m.storage.ListActiveIncidents()
		if err != nil {
			m.logger.Warn("Failed to load active incidents", "error", err)
		} else {
			for _, incident := range incidents {
				m.incidents[incident.ID] = incident
			}
		}

		m.loadDedupState()
	}

	// Start workers
	for i := 0; i < 5; i++ {
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.worker()
		}()
	}

	// Start escalation checker
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.escalationChecker()
	}()

	m.logger.Info("Alert manager started",
		"channels", len(m.channels),
		"rules", len(m.rules))

	return nil
}

// Stop stops the alert manager
func (m *Manager) Stop() error {
	m.mu.Lock()
	if m.stopping {
		done := m.stopDone
		m.mu.Unlock()
		<-done
		return nil
	}
	if !m.running {
		m.mu.Unlock()
		return nil
	}

	m.running = false
	m.stopping = true
	stopCh := m.stopCh
	done := m.stopDone
	close(stopCh)
	m.mu.Unlock()

	// Workers may need m.mu while finishing a dispatch, so never wait while
	// holding it. The done channel also makes concurrent Stop calls idempotent.
	m.wg.Wait()

	m.mu.Lock()
	m.stopping = false
	close(done)
	m.mu.Unlock()
	return nil
}

// RegisterChannel adds or updates an alert channel
func (m *Manager) RegisterChannel(channel *core.AlertChannel) error {
	if err := channel.Validate(); err != nil {
		return err
	}
	if err := m.validateChannelConfig(channel); err != nil {
		return err
	}

	m.mu.Lock()
	previous, hadPrevious := m.channels[channel.ID]
	m.channels[channel.ID] = channel
	m.mu.Unlock()

	if m.storage != nil {
		if err := m.storage.SaveChannel(channel); err != nil {
			// In a cluster the save is the replicated write; keeping a channel
			// only this node knows about would make nodes disagree, so undo it
			// and let the caller report the failure.
			m.mu.Lock()
			if hadPrevious {
				m.channels[channel.ID] = previous
			} else {
				delete(m.channels, channel.ID)
			}
			m.mu.Unlock()
			m.logger.Warn("Failed to save channel", "error", err)
			return fmt.Errorf("save channel: %w", err)
		}
	}

	m.logger.Info("Channel registered",
		"id", channel.ID,
		"name", channel.Name,
		"type", channel.Type)

	return nil
}

// ValidateChannel reports whether RegisterChannel would accept channel, so a
// caller can reject a bad channel before persisting it elsewhere.
func (m *Manager) ValidateChannel(channel *core.AlertChannel) error {
	if err := channel.Validate(); err != nil {
		return err
	}
	return m.validateChannelConfig(channel)
}

func (m *Manager) validateChannelConfig(channel *core.AlertChannel) error {
	dispatcher, ok := m.dispatchers[channel.Type]
	if !ok {
		return fmt.Errorf("unsupported channel type: %s", channel.Type)
	}
	if err := dispatcher.Validate(channel.Config); err != nil {
		return fmt.Errorf("invalid %s channel config: %w", channel.Type, err)
	}
	return nil
}

// DeleteChannelWithWorkspace removes an alert channel only if it belongs to the given workspace
func (m *Manager) DeleteChannelWithWorkspace(id string, workspace string) error {
	m.mu.RLock()
	ch := m.channels[id]
	m.mu.RUnlock()

	if ch == nil {
		return fmt.Errorf("channel not found: %s", id)
	}
	if ch.WorkspaceID != "" && ch.WorkspaceID != workspace {
		return fmt.Errorf("channel %s does not belong to workspace %s", id, workspace)
	}

	return m.DeleteChannel(id)
}

// DeleteChannel removes an alert channel
func (m *Manager) DeleteChannel(id string) error {
	m.mu.RLock()
	ch := m.channels[id]
	m.mu.RUnlock()

	workspace := "default"
	if ch != nil && ch.WorkspaceID != "" {
		workspace = ch.WorkspaceID
	}

	m.mu.Lock()
	delete(m.channels, id)
	m.mu.Unlock()

	if m.storage != nil {
		if err := m.storage.DeleteChannel(id, workspace); err != nil {
			m.logger.Warn("Failed to delete channel", "error", err)
		}
	}

	return nil
}

// RegisterRule adds or updates an alert rule
func (m *Manager) RegisterRule(rule *core.AlertRule) error {
	m.mu.Lock()
	previous, hadPrevious := m.rules[rule.ID]
	m.rules[rule.ID] = rule
	m.mu.Unlock()

	if m.storage != nil {
		if err := m.storage.SaveRule(rule); err != nil {
			// Undo, as RegisterChannel does: an unsaved rule must not fire on
			// this node alone.
			m.mu.Lock()
			if hadPrevious {
				m.rules[rule.ID] = previous
			} else {
				delete(m.rules, rule.ID)
			}
			m.mu.Unlock()
			m.logger.Warn("Failed to save rule", "error", err)
			return fmt.Errorf("save rule: %w", err)
		}
	}

	m.logger.Info("Rule registered",
		"id", rule.ID,
		"name", rule.Name)

	return nil
}

// DeleteRuleWithWorkspace removes an alert rule only if it belongs to the given workspace
func (m *Manager) DeleteRuleWithWorkspace(id string, workspace string) error {
	m.mu.RLock()
	rule := m.rules[id]
	m.mu.RUnlock()

	if rule == nil {
		return fmt.Errorf("rule not found: %s", id)
	}
	if rule.WorkspaceID != "" && rule.WorkspaceID != workspace {
		return fmt.Errorf("rule %s does not belong to workspace %s", id, workspace)
	}

	return m.DeleteRule(id)
}

// DeleteRule removes an alert rule
func (m *Manager) DeleteRule(id string) error {
	m.mu.RLock()
	rule := m.rules[id]
	m.mu.RUnlock()

	workspace := "default"
	if rule != nil && rule.WorkspaceID != "" {
		workspace = rule.WorkspaceID
	}

	m.mu.Lock()
	delete(m.rules, id)
	m.mu.Unlock()

	if m.storage != nil {
		if err := m.storage.DeleteRule(id, workspace); err != nil {
			m.logger.Warn("Failed to delete rule", "error", err)
		}
	}

	return nil
}

// ProcessJudgment evaluates a judgment against alert rules
func (m *Manager) ProcessJudgment(soul *core.Soul, prevStatus core.SoulStatus, judgment *core.Judgment) {
	if m.isSuppressedByMaintenance(soul, time.Now().UTC()) {
		m.mu.Lock()
		m.stats.filteredAlerts++
		m.mu.Unlock()
		m.logger.Debug("Alert suppressed by active maintenance window",
			"soul_id", soul.ID,
			"workspace", defaultWorkspace(soul.WorkspaceID))
		return
	}

	m.mu.RLock()
	rules := make([]*core.AlertRule, 0, len(m.rules))
	for _, rule := range m.rules {
		rules = append(rules, rule)
	}
	m.mu.RUnlock()

	// Check each rule
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}

		// Check if rule applies to this soul
		if !m.ruleApplies(rule, soul) {
			continue
		}

		// Update the per-(rule,soul) failure streak BEFORE evaluating
		// conditions — consecutive_failures reads from this. Any non-dead
		// status resets the streak so an alive judgment that breaks the
		// failure chain clears the counter.
		m.updateFailureStreak(rule.ID, soul.ID, judgment.Status)

		// Refresh the per-soul status clock before conditions run, so
		// status_for can measure how long the current status has held.
		m.noteStatus(soul.ID, judgment.Status)

		// Check conditions
		triggered := m.checkConditions(rule, prevStatus, judgment)
		if !triggered {
			// On a healthy judgment, auto-resolve any open incident for
			// this (rule, soul) pair if the rule has AutoResolve enabled.
			// This is the "recovery" path — rule didn't trigger, but we
			// might still have an open incident from a prior failure.
			if judgment.Status == core.SoulAlive && rule.AutoResolve {
				m.autoResolveOpenIncident(rule.ID, soul)
			}
			continue
		}

		// Create alert event
		severity := rule.Severity
		if severity == "" {
			severity = m.calculateSeverity(judgment)
		}
		event := &core.AlertEvent{
			ID:          generateAlertID(),
			RuleID:      rule.ID,
			SoulID:      soul.ID,
			SoulName:    soul.Name,
			WorkspaceID: soul.WorkspaceID,
			Status:      judgment.Status,
			PrevStatus:  prevStatus,
			Judgment:    judgment,
			Message:     judgment.Message,
			Details:     m.extractDetails(judgment),
			Severity:    severity,
			Timestamp:   time.Now().UTC(),
		}

		// Persist the incident record so operators can ack/resolve it via
		// /api/v1/incidents. Idempotent: if there's already an open
		// incident for this (rule, soul) pair, append the event to it
		// rather than creating a duplicate.
		m.recordIncident(rule, soul, event)

		// Check deduplication before queuing
		if m.isDuplicate(rule, event) {
			m.logger.Debug("Alert deduplicated",
				"rule", rule.Name,
				"soul_id", event.SoulID)
			continue
		}
		// Share the new dedup record, so a node that takes this soul over
		// keeps honouring the cooldown instead of re-sending the alert.
		m.persistDedup(event)

		// Send to queue
		select {
		case m.queue <- event:
		default:
			m.logger.Warn("Alert queue full, dropping event",
				"soul_id", soul.ID)
		}
	}
}

// recordIncident creates a new open Incident if none exists for the given
// (rule, soul) pair, or appends the event to the existing one. Always
// persists via storage so the REST API at /api/v1/incidents reflects
// reality. Without this, alert rules fire and notifications dispatch but
// the incident inbox stays empty forever.
func (m *Manager) recordIncident(rule *core.AlertRule, soul *core.Soul, event *core.AlertEvent) {
	m.mu.Lock()
	for _, inc := range m.incidents {
		if inc.RuleID == rule.ID && inc.SoulID == soul.ID && inc.Status != core.IncidentResolved {
			// Open incident already exists — append the event for audit.
			inc.Events = append(inc.Events, *event)
			incToSave := inc
			m.mu.Unlock()
			if m.storage != nil {
				if err := m.storage.SaveIncident(incToSave); err != nil {
					m.logger.Error("Failed to update incident with new event",
						"incident_id", incToSave.ID, "error", err)
				}
			}
			return
		}
	}
	incident := &core.Incident{
		ID:          generateIncidentID(),
		RuleID:      rule.ID,
		SoulID:      soul.ID,
		SoulName:    soul.Name,
		WorkspaceID: soul.WorkspaceID,
		Status:      core.IncidentOpen,
		Severity:    event.Severity,
		StartedAt:   event.Timestamp,
		Events:      []core.AlertEvent{*event},
	}
	m.incidents[incident.ID] = incident
	notify := m.incidentNotifier(incident)
	m.mu.Unlock()

	if m.storage != nil {
		if err := m.storage.SaveIncident(incident); err != nil {
			m.logger.Error("Failed to create incident",
				"soul_id", soul.ID, "rule_id", rule.ID, "error", err)
		}
	}
	notify()
	m.logger.Info("Incident opened",
		"incident_id", incident.ID,
		"soul_id", soul.ID,
		"rule_id", rule.ID,
		"severity", incident.Severity)
}

// autoResolveOpenIncident finds the open incident for the given (rule,
// soul) and marks it resolved with "auto-resolved" as the resolver,
// when the rule has AutoResolve=true and the soul has recovered.
func (m *Manager) autoResolveOpenIncident(ruleID string, soul *core.Soul) {
	m.mu.Lock()
	var target *core.Incident
	for _, inc := range m.incidents {
		if inc.RuleID == ruleID && inc.SoulID == soul.ID && inc.Status != core.IncidentResolved {
			target = inc
			break
		}
	}
	if target == nil {
		m.mu.Unlock()
		return
	}
	now := time.Now().UTC()
	target.Status = core.IncidentResolved
	target.ResolvedAt = &now
	target.ResolvedBy = "auto-resolved"
	m.stats.resolvedAlerts++
	incToSave := target
	notify := m.incidentNotifier(target)
	m.mu.Unlock()

	if m.storage != nil {
		if err := m.storage.SaveIncident(incToSave); err != nil {
			m.logger.Error("Failed to save auto-resolved incident",
				"incident_id", incToSave.ID, "error", err)
		}
	}
	notify()
	m.logger.Info("Incident auto-resolved",
		"incident_id", incToSave.ID,
		"soul_id", soul.ID,
		"rule_id", ruleID)
}

func (m *Manager) isSuppressedByMaintenance(soul *core.Soul, now time.Time) bool {
	if m.storage == nil {
		return false
	}
	storage, ok := m.storage.(maintenanceWindowStorage)
	if !ok {
		return false
	}
	windows, err := storage.ListMaintenanceWindows()
	if err != nil {
		m.logger.Warn("Failed to list maintenance windows", "error", err)
		return false
	}
	return core.SoulInMaintenance(windows, soul, now)
}

func defaultWorkspace(workspace string) string {
	if workspace == "" {
		return "default"
	}
	return workspace
}

// worker processes alert events
func (m *Manager) worker() {
	for {
		select {
		case <-m.stopCh:
			return
		case event := <-m.queue:
			m.dispatch(event)
		}
	}
}

// dispatch sends an alert through appropriate channels
func (m *Manager) dispatch(event *core.AlertEvent) {
	m.notifyAlert(event)

	m.mu.RLock()
	channels := make([]*core.AlertChannel, 0, len(m.channels))
	if event.RuleID != "" {
		// A rule's alerts go only to the channels the rule names. A rule
		// deleted while its event was queued routes nowhere.
		if rule := m.rules[event.RuleID]; rule != nil {
			seen := make(map[string]struct{}, len(rule.Channels))
			for _, id := range rule.Channels {
				if _, dup := seen[id]; dup {
					continue
				}
				seen[id] = struct{}{}
				if ch := m.channels[id]; ch != nil {
					channels = append(channels, ch)
				}
			}
		}
	} else {
		for _, ch := range m.channels {
			channels = append(channels, ch)
		}
	}
	m.mu.RUnlock()

	if event.RuleID != "" && len(channels) == 0 {
		m.logger.Warn("Alert has no deliverable channel: rule names no existing channel",
			"rule", event.RuleID,
			"soul_id", event.SoulID)
	}

	// Process channels concurrently with a semaphore to limit goroutines
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10) // Limit concurrent dispatchers

	// attempted counts sends actually started and delivered those that
	// succeeded. Only a send that ran and failed releases the dedup entry
	// (see below).
	var deliveredMu sync.Mutex
	attempted, delivered := 0, 0

	for _, channel := range channels {
		// Workspace isolation: only dispatch to channels in the same workspace
		// as the event. Both empty → "default" for backwards compat.
		eventWs := event.WorkspaceID
		if eventWs == "" {
			eventWs = "default"
		}
		channelWs := channel.WorkspaceID
		if channelWs == "" {
			channelWs = "default"
		}
		if channelWs != eventWs {
			continue
		}

		if !channel.ShouldNotify(event) {
			m.mu.Lock()
			m.stats.filteredAlerts++
			m.mu.Unlock()
			continue
		}

		// Check rate limiting
		if m.isRateLimited(channel, event) {
			m.mu.Lock()
			m.stats.rateLimitedAlerts++
			m.mu.Unlock()
			m.logger.Debug("Rate limited",
				"channel", channel.ID,
				"soul_id", event.SoulID)
			continue
		}

		attempted++
		wg.Add(1)
		sem <- struct{}{}

		go func(ch *core.AlertChannel) {
			defer wg.Done()
			defer func() { <-sem }()

			// Each dispatcher gets private routing fields. Sharing event here lets
			// concurrent channels race while assigning ChannelID/ChannelType.
			channelEvent := *event
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			err := m.sendToChannel(ctx, &channelEvent, ch)
			cancel()

			if err != nil {
				m.logger.Error("Failed to send alert",
					"channel", ch.ID,
					"error", err)
			} else {
				deliveredMu.Lock()
				delivered++
				deliveredMu.Unlock()
				m.logger.Debug("Alert sent",
					"channel", ch.ID,
					"soul_id", event.SoulID)
			}
		}(channel)
	}

	wg.Wait()

	// An alert whose every send failed was never delivered, so it must not
	// consume the dedup window: isDuplicate's contract is to suppress alerts the
	// operator has already been told about. Leaving the entry in place meant one
	// transient channel outage swallowed the notification for the whole window
	// (rule cooldown, default 5 minutes), so nobody was told the service was down.
	//
	// When no send was attempted at all (the rule names no existing channel, or
	// every channel was filtered, disabled, in another workspace or rate
	// limited) retrying cannot succeed. Releasing then would rerun the whole
	// alert path, event and incident writes included, on every check instead of
	// once per cooldown, so the entry stays.
	deliveredMu.Lock()
	sent := delivered
	deliveredMu.Unlock()
	if attempted > 0 && sent == 0 {
		m.releaseDedup(event)
	}
}

// releaseDedup drops the entry isDuplicate recorded for this event, so an
// undelivered alert can be attempted again on the next judgment.
func (m *Manager) releaseDedup(event *core.AlertEvent) {
	if event == nil || event.RuleID == "" {
		return
	}
	key := dedupKey(event.RuleID, event.SoulID, event.Status)
	m.history.Mu.Lock()
	delete(m.history.Entries, key)
	m.history.Mu.Unlock()
	if store, ok := m.storage.(DedupStateStore); ok {
		if err := store.DeleteDedupState(defaultWorkspace(event.WorkspaceID), key); err != nil {
			m.logger.Warn("Failed to share released dedup state", "key", key, "error", err)
		}
	}
}

// sendToChannel sends an alert to a specific channel
func (m *Manager) sendToChannel(ctx context.Context, event *core.AlertEvent, channel *core.AlertChannel) error {
	dispatcher, ok := m.dispatchers[channel.Type]
	if !ok {
		return fmt.Errorf("no dispatcher for channel type: %s", channel.Type)
	}

	// Track attempt
	event.ChannelID = channel.ID
	event.ChannelType = channel.Type

	// Update statistics
	m.mu.Lock()
	m.stats.totalAlerts++
	m.stats.lastAlertTime = time.Now()
	m.stats.verdictsBySeverity[string(event.Severity)]++
	m.mu.Unlock()

	// Build retry policy from channel config
	policy := channel.RetryPolicy
	if policy.MaxRetries <= 0 {
		policy.MaxRetries = 1
	}
	initialWait := policy.InitialWait.Duration
	if initialWait <= 0 {
		initialWait = 1 * time.Second
	}
	maxWait := policy.MaxWait.Duration
	if maxWait <= 0 {
		maxWait = 5 * time.Minute
	}

	var lastErr error
	maxAttempts := uint(policy.MaxRetries)
	if policy.Backoff == "exponential" {
		ebo := backoff.NewExponentialBackOff()
		ebo.InitialInterval = initialWait
		ebo.MaxInterval = maxWait
		ebo.Reset()
		_, lastErr = backoff.Retry[error](ctx, func() (error, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, dispatcher.Send(ctx, event, channel)
		}, backoff.WithMaxTries(maxAttempts), backoff.WithBackOff(ebo))
	} else {
		// Linear retries: wait InitialWait between each attempt
		for attempt := uint(0); attempt < maxAttempts; attempt++ {
			// Check context before sending
			if ctx.Err() != nil {
				lastErr = ctx.Err()
				break
			}
			lastErr = dispatcher.Send(ctx, event, channel)
			if lastErr == nil {
				break
			}
			if attempt+1 < maxAttempts {
				// Wait before next retry, but stop early if context is cancelled
				select {
				case <-ctx.Done():
					lastErr = ctx.Err()
					goto done
				case <-time.After(initialWait):
					// continue to next attempt
				}
			}
		}
	}
done:

	// Track result
	m.mu.Lock()
	if lastErr != nil {
		m.stats.failedAlerts++
	} else {
		m.stats.sentAlerts++
	}
	m.mu.Unlock()

	return lastErr
}

// TestChannel sends a synthetic notification through a configured channel.
func (m *Manager) TestChannel(ctx context.Context, id string, workspace string) error {
	channel, err := m.GetChannel(id)
	if err != nil {
		return err
	}
	if channel == nil {
		return fmt.Errorf("channel not found: %s", id)
	}
	if workspace == "" {
		workspace = "default"
	}
	channelWorkspace := channel.WorkspaceID
	if channelWorkspace == "" {
		channelWorkspace = "default"
	}
	if channelWorkspace != workspace {
		return fmt.Errorf("channel %s does not belong to workspace %s", id, workspace)
	}

	event := &core.AlertEvent{
		ID:          core.GenerateID(),
		ChannelID:   id,
		SoulID:      "test",
		SoulName:    "AnubisWatch Test",
		WorkspaceID: channelWorkspace,
		Status:      core.SoulDegraded,
		Severity:    core.SeverityInfo,
		Message:     "This is a test notification from AnubisWatch.",
		Details: map[string]string{
			"channel": channel.Name,
			"purpose": "configuration test",
		},
		Timestamp: time.Now(),
	}

	return m.sendToChannel(ctx, event, channel)
}

// registerDispatchers registers all built-in channel dispatchers
func (m *Manager) registerDispatchers() {
	m.dispatchers[core.ChannelSlack] = &SlackDispatcher{logger: m.logger}
	m.dispatchers[core.ChannelDiscord] = &DiscordDispatcher{logger: m.logger}
	m.dispatchers[core.ChannelTelegram] = &TelegramDispatcher{logger: m.logger}
	m.dispatchers[core.ChannelSMS] = &SMSDispatcher{logger: m.logger}
	m.dispatchers[core.ChannelEmail] = &EmailDispatcher{logger: m.logger}
	m.dispatchers[core.ChannelPagerDuty] = &PagerDutyDispatcher{logger: m.logger}
	m.dispatchers[core.ChannelOpsGenie] = &OpsGenieDispatcher{logger: m.logger}
	m.dispatchers[core.ChannelNtfy] = &NtfyDispatcher{logger: m.logger}
	m.dispatchers[core.ChannelWebHook] = &WebHookDispatcher{logger: m.logger}
}

// ruleApplies checks if a rule applies to a soul
func (m *Manager) ruleApplies(rule *core.AlertRule, soul *core.Soul) bool {
	scope := rule.Scope

	// When Type is unset, infer from which sub-field is populated. This
	// matches the API client's natural shape (`{"scope":{"soul_ids":[...]}}`)
	// and avoids a footgun where the rule is silently never matched.
	scopeType := scope.Type
	if scopeType == "" {
		switch {
		case len(scope.SoulIDs) > 0:
			scopeType = "specific"
		case len(scope.Tags) > 0:
			scopeType = "tag"
		case len(scope.SoulTypes) > 0:
			scopeType = "type"
		default:
			// Empty scope means "applies to every soul" (the simplest rule
			// shape), so default to all rather than silently rejecting.
			scopeType = "all"
		}
	}

	switch scopeType {
	case "all":
		return true
	case "specific":
		for _, id := range scope.SoulIDs {
			if id == soul.ID {
				return true
			}
		}
		return false
	case "tag":
		for _, tag := range scope.Tags {
			for _, soulTag := range soul.Tags {
				if tag == soulTag {
					return true
				}
			}
		}
		return false
	case "type":
		for _, t := range scope.SoulTypes {
			if string(soul.Type) == t {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// updateFailureStreak increments the per-(rule,soul) failure counter on a
// dead status and resets it on any other status. Bounded growth: capped at
// 1_000_000 so a permanently-down soul can't overflow int.
func (m *Manager) updateFailureStreak(ruleID, soulID string, status core.SoulStatus) {
	key := ruleID + ":" + soulID
	m.mu.Lock()
	defer m.mu.Unlock()
	if status == core.SoulDead {
		if m.failureStreak[key] < 1_000_000 {
			m.failureStreak[key]++
		}
	} else {
		delete(m.failureStreak, key)
	}
}

// failureStreakCount returns the current consecutive-failure count for the
// given rule+soul pair, used by checkConditions.
func (m *Manager) failureStreakCount(ruleID, soulID string) int {
	key := ruleID + ":" + soulID
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.failureStreak[key]
}

// noteStatus records when a soul first entered its current status, restarting
// the dwell clock whenever the status changes. Callers must invoke it once per
// judgment, before conditions are evaluated.
func (m *Manager) noteStatus(soulID string, status core.SoulStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Lazy-init: Manager is also built as a struct literal in some callers,
	// which bypasses NewManager's map initialisation.
	if m.statusSince == nil {
		m.statusSince = make(map[string]statusHold)
	}
	if hold, ok := m.statusSince[soulID]; ok && hold.Status == status {
		return
	}
	m.statusSince[soulID] = statusHold{Status: status, Since: time.Now()}
}

// statusHeldFor reports whether the soul has been in its current status for at
// least the requested dwell time. A non-positive dwell preserves the original
// instantaneous behaviour.
func (m *Manager) statusHeldFor(soulID string, status core.SoulStatus, dwell time.Duration) bool {
	if dwell <= 0 {
		return true
	}
	m.mu.RLock()
	hold, ok := m.statusSince[soulID]
	m.mu.RUnlock()
	if !ok || hold.Status != status {
		return false
	}
	return time.Since(hold.Since) >= dwell
}

// statusForCondition implements the status_for condition: the judgment's status
// must match, and it must have held for at least the configured Duration.
// AlertCondition.Duration is documented as "for status_for"
// (internal/core/verdict.go) and is settable through the REST API, so a rule
// asking to wait before paging must actually wait.
func (m *Manager) statusForCondition(cond core.AlertCondition, judgment *core.Judgment) bool {
	if string(judgment.Status) != cond.Status {
		return false
	}
	return m.statusHeldFor(judgment.SoulID, judgment.Status, cond.Duration.Duration)
}

// checkConditions checks if alert conditions are met
func (m *Manager) checkConditions(rule *core.AlertRule, prevStatus core.SoulStatus, judgment *core.Judgment) bool {
	for _, cond := range rule.Conditions {
		switch cond.Type {
		case "consecutive_failures":
			// failureStreak is updated by ProcessJudgment before this runs;
			// here we only read the accumulated count and compare to
			// threshold. The streak is keyed by ruleID+soulID so multiple
			// rules can independently track the same soul.
			if m.failureStreakCount(rule.ID, judgment.SoulID) >= cond.Threshold {
				return true
			}
		case "status_change":
			if string(prevStatus) == cond.From && string(judgment.Status) == cond.To {
				return true
			}
		case "status_for":
			if m.statusForCondition(cond, judgment) {
				return true
			}
		case "failure_rate":
			// Would need historical data
			if judgment.Status == core.SoulDead {
				return true
			}
		case "degraded":
			if judgment.Status == core.SoulDegraded {
				return true
			}
		case "recovery":
			if prevStatus == core.SoulDead && judgment.Status == core.SoulAlive {
				return true
			}
		case "anomaly":
			if m.checkAnomaly(cond, judgment) {
				return true
			}
		case "threshold":
			if m.evaluateCondition(cond, prevStatus, judgment) {
				return true
			}
		case "compound":
			if m.checkCompound(cond, prevStatus, judgment) {
				return true
			}
		}
	}

	return false
}

// isRateLimited checks if an alert is rate limited
func (m *Manager) isRateLimited(channel *core.AlertChannel, event *core.AlertEvent) bool {
	if !channel.RateLimit.Enabled {
		return false
	}

	// Build grouping key. The "ratelimit:" prefix lets the record be shared
	// like dedup records (persistHistoryEntry), so a node that takes a soul
	// over keeps counting the window instead of starting from zero.
	key := rateLimitKey(channel.ID, event.SoulID, channel.RateLimit.GroupingKey)

	limited, snapshot := m.rateLimitStep(key, channel, event)
	m.persistHistoryEntry(event.WorkspaceID, snapshot)
	return limited
}

func rateLimitKey(channelID, soulID, groupingKey string) string {
	return fmt.Sprintf("ratelimit:%s:%s:%s", channelID, soulID, groupingKey)
}

// rateLimitStep updates the rate-limit window for key and reports whether
// this alert exceeds it, returning a copy of the updated record.
func (m *Manager) rateLimitStep(key string, channel *core.AlertChannel, event *core.AlertEvent) (bool, core.AlertHistoryEntry) {
	m.history.Mu.Lock()
	defer m.history.Mu.Unlock()

	now := time.Now()
	entry, exists := m.history.Entries[key]
	if !exists {
		// First alert for this group
		entry = &core.AlertHistoryEntry{
			Key:        key,
			ChannelID:  channel.ID,
			Count:      1,
			FirstSent:  now,
			LastSent:   now,
			SoulStatus: event.Status,
		}
		m.history.Entries[key] = entry
		return false, *entry
	}

	// Check if window has expired
	if now.Sub(entry.FirstSent) > channel.RateLimit.Window.Duration {
		// Reset window
		entry.Count = 1
		entry.FirstSent = now
		entry.LastSent = now
		return false, *entry
	}

	// Check limit
	if entry.Count >= channel.RateLimit.MaxAlerts {
		entry.LastSent = now
		return true, *entry
	}

	// Increment count
	entry.Count++
	entry.LastSent = now
	return false, *entry
}

// isDuplicate checks if an identical alert was recently sent (deduplication)
func (m *Manager) isDuplicate(rule *core.AlertRule, event *core.AlertEvent) bool {
	// Build dedup key: rule + soul + status
	key := dedupKey(rule.ID, event.SoulID, event.Status)

	m.history.Mu.Lock()
	defer m.history.Mu.Unlock()

	entry, exists := m.history.Entries[key]
	if !exists {
		// First alert for this dedup key
		m.history.Entries[key] = &core.AlertHistoryEntry{
			Key:        key,
			ChannelID:  rule.ID,
			Count:      1,
			FirstSent:  time.Now(),
			LastSent:   time.Now(),
			SoulStatus: event.Status,
		}
		return false
	}

	// Check if dedup window has expired (use rule cooldown as dedup window)
	dedupWindow := rule.Cooldown.Duration
	if dedupWindow == 0 {
		dedupWindow = 5 * time.Minute // Default 5 minute dedup window
	}

	if time.Since(entry.LastSent) >= dedupWindow {
		// Window expired, allow alert
		entry.FirstSent = time.Now()
		entry.LastSent = time.Now()
		entry.Count = 1
		return false
	}

	// Within dedup window - check if status changed
	if entry.SoulStatus != event.Status {
		// Status changed, allow alert
		entry.FirstSent = time.Now()
		entry.LastSent = time.Now()
		entry.Count = 1
		entry.SoulStatus = event.Status
		return false
	}

	// Duplicate alert within window
	return true
}

// extractDetails extracts relevant details from a judgment
func (m *Manager) extractDetails(judgment *core.Judgment) map[string]string {
	details := make(map[string]string)

	if judgment.StatusCode > 0 {
		details["status_code"] = fmt.Sprintf("%d", judgment.StatusCode)
	}
	if judgment.Duration > 0 {
		details["duration"] = judgment.Duration.String()
	}
	if judgment.TLSInfo != nil {
		details["tls_protocol"] = judgment.TLSInfo.Protocol
		details["tls_cipher"] = judgment.TLSInfo.CipherSuite
		details["tls_expiry"] = fmt.Sprintf("%d days", judgment.TLSInfo.DaysUntilExpiry)
	}

	return details
}

// calculateSeverity determines the severity of an alert
func (m *Manager) calculateSeverity(judgment *core.Judgment) core.Severity {
	switch judgment.Status {
	case core.SoulDead:
		return core.SeverityCritical
	case core.SoulDegraded:
		return core.SeverityWarning
	default:
		return core.SeverityInfo
	}
}

// checkAnomaly checks if the current judgment deviates significantly from baseline.
// It uses stored alert history to compute mean and standard deviation, then
// triggers if the current value exceeds mean ± (stdDev * anomalyStdDev).
func (m *Manager) checkAnomaly(cond core.AlertCondition, judgment *core.Judgment) bool {
	// Determine which metric to check
	metric := cond.Metric
	if metric == "" {
		metric = "latency"
	}

	// Get current value
	var currentValue float64
	switch metric {
	case "latency", "latency_ms", "duration":
		currentValue = float64(judgment.Duration.Milliseconds())
	case "status_code":
		currentValue = float64(judgment.StatusCode)
	default:
		// Default: check if status is dead (anomaly in status)
		return judgment.Status == core.SoulDead
	}

	// Fetch historical events for this soul to compute baseline
	events, err := m.storage.ListEvents(judgment.SoulID, 100)
	if err != nil || len(events) < 3 {
		// Not enough history, use simple threshold fallback
		threshold, _ := cond.Value.(float64)
		if threshold == 0 {
			threshold = float64(cond.Threshold)
		}
		if threshold > 0 {
			return m.compareFloatValue(currentValue, cond.Operator, threshold)
		}
		// Default: 2 standard deviations, trigger if > 2x average
		return currentValue > 0
	}

	// Compute baseline statistics from history
	values := make([]float64, 0, len(events))
	for _, ev := range events {
		var val float64
		switch metric {
		case "latency", "latency_ms", "duration":
			if ev.Judgment != nil {
				val = float64(ev.Judgment.Duration.Milliseconds())
			}
		case "status_code":
			if ev.Judgment != nil {
				val = float64(ev.Judgment.StatusCode)
			}
		}
		if val > 0 {
			values = append(values, val)
		}
	}

	if len(values) < 2 {
		return false
	}

	// Calculate mean
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))

	// Calculate standard deviation (population std dev)
	var sqSum float64
	for _, v := range values {
		diff := v - mean
		sqSum += diff * diff
	}
	stdDev := 0.0
	if len(values) > 0 {
		variance := sqSum / float64(len(values))
		stdDev = math.Sqrt(variance)
	}

	// Determine threshold: mean ± (stdDev * anomalyStdDev)
	anomalyStdDev := cond.AnomalyStdDev
	if anomalyStdDev <= 0 {
		anomalyStdDev = 2.0 // Default: 2 standard deviations
	}

	upperBound := mean + (stdDev * anomalyStdDev)
	lowerBound := mean - (stdDev * anomalyStdDev)
	if lowerBound < 0 {
		lowerBound = 0
	}

	// Check if current value is outside bounds
	return currentValue > upperBound || (currentValue < lowerBound && lowerBound > 0)
}

// checkCompound evaluates compound conditions (AND/OR of sub-conditions).
func (m *Manager) checkCompound(cond core.AlertCondition, prevStatus core.SoulStatus, judgment *core.Judgment) bool {
	if len(cond.SubConditions) == 0 {
		return false
	}

	logic := cond.Logic
	if logic == "" {
		logic = "and"
	}

	matchedCount := 0
	for _, subCond := range cond.SubConditions {
		if m.evaluateCondition(subCond, prevStatus, judgment) {
			matchedCount++
		}
	}

	switch logic {
	case "and":
		return matchedCount == len(cond.SubConditions)
	case "or":
		return matchedCount > 0
	case "majority":
		return matchedCount > len(cond.SubConditions)/2
	case "at_least":
		threshold := cond.Threshold
		if threshold <= 0 {
			threshold = 1
		}
		return matchedCount >= threshold
	default:
		return matchedCount == len(cond.SubConditions)
	}
}

// evaluateCondition evaluates a single condition (used by compound).
func (m *Manager) evaluateCondition(cond core.AlertCondition, prevStatus core.SoulStatus, judgment *core.Judgment) bool {
	switch cond.Type {
	case "compound":
		return m.checkCompound(cond, prevStatus, judgment)
	case "status_change":
		return string(prevStatus) == cond.From && string(judgment.Status) == cond.To
	case "status_for":
		return m.statusForCondition(cond, judgment)
	case "failure_rate":
		return judgment.Status == core.SoulDead
	case "degraded":
		return judgment.Status == core.SoulDegraded
	case "recovery":
		return prevStatus == core.SoulDead && judgment.Status == core.SoulAlive
	case "anomaly":
		return m.checkAnomaly(cond, judgment)
	case "threshold":
		// Metric-based threshold. An unset metric means latency, as in
		// checkAnomaly. A metric this engine does not measure never fires:
		// reading it as latency made e.g. `cpu > 90` page on every check
		// slower than 90ms and stay silent on a real CPU problem.
		var currentValue float64
		switch cond.Metric {
		case "", "latency", "latency_ms", "duration":
			currentValue = float64(judgment.Duration.Milliseconds())
		case "status_code":
			currentValue = float64(judgment.StatusCode)
		case "tls_expiry_days":
			// Checks without certificate data cannot breach an expiry threshold.
			if judgment.TLSInfo == nil {
				return false
			}
			currentValue = float64(judgment.TLSInfo.DaysUntilExpiry)
		default:
			return false
		}
		threshold, _ := cond.Value.(float64)
		if threshold == 0 {
			threshold = float64(cond.Threshold)
		}
		return m.compareFloatValue(currentValue, cond.Operator, threshold)
	default:
		return false
	}
}

// compareFloatValue compares a float value against a threshold using the given operator.
func (m *Manager) compareFloatValue(actual float64, operator string, expected float64) bool {
	switch operator {
	case ">", "gt", "greater_than":
		return actual > expected
	case "<", "lt", "less_than":
		return actual < expected
	case ">=", "ge", "greater_equals":
		return actual >= expected
	case "<=", "le", "less_equals":
		return actual <= expected
	case "==", "eq", "equals":
		return actual == expected
	case "!=", "ne", "not_equals":
		return actual != expected
	default:
		return actual > expected
	}
}

// generateAlertID generates a unique alert ID
func generateAlertID() string {
	return fmt.Sprintf("alert_%d_%s", time.Now().Unix(), generateShortID())
}

func generateIncidentID() string {
	return fmt.Sprintf("inc_%d_%s", time.Now().Unix(), generateShortID())
}

func generateShortID() string {
	return fmt.Sprintf("%06x", time.Now().UnixNano()%0xFFFFFF)
}

// GetStats returns alert manager statistics
func (m *Manager) GetStats() core.AlertManagerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return core.AlertManagerStats{
		TotalAlerts:        m.stats.totalAlerts,
		SentAlerts:         m.stats.sentAlerts,
		FailedAlerts:       m.stats.failedAlerts,
		AcknowledgedAlerts: m.stats.acknowledgedAlerts,
		ResolvedAlerts:     m.stats.resolvedAlerts,
		RateLimitedAlerts:  m.stats.rateLimitedAlerts,
		FilteredAlerts:     m.stats.filteredAlerts,
		ActiveIncidents:    len(m.incidents),
		LastAlertTime:      m.stats.lastAlertTime,
		VerdictsBySeverity: m.copyVerdictsBySeverity(),
	}
}

// copyVerdictsBySeverity returns a copy of the verdicts by severity map
func (m *Manager) copyVerdictsBySeverity() map[string]uint64 {
	out := make(map[string]uint64, len(m.stats.verdictsBySeverity))
	for k, v := range m.stats.verdictsBySeverity {
		out[k] = v
	}
	return out
}

// AcknowledgeIncident acknowledges an incident
func (m *Manager) AcknowledgeIncident(incidentID, userID, workspace string) error {
	return m.updateIncident(incidentID, workspace, func(incident *core.Incident, now time.Time) {
		incident.Status = core.IncidentAcked
		incident.AckedAt = &now
		incident.AckedBy = userID
	}, func() { m.stats.acknowledgedAlerts++ })
}

// ResolveIncident marks an incident as resolved
func (m *Manager) ResolveIncident(incidentID, userID, workspace string) error {
	return m.updateIncident(incidentID, workspace, func(incident *core.Incident, now time.Time) {
		incident.Status = core.IncidentResolved
		incident.ResolvedAt = &now
		incident.ResolvedBy = userID
	}, func() { m.stats.resolvedAlerts++ })
}

// DurableIncidentStore is implemented by storage whose SaveIncident may only
// queue the write (the cluster adapter batches automatic incident updates).
// Operator actions use SaveIncidentDurable so a success response means the
// change is stored.
type DurableIncidentStore interface {
	SaveIncidentDurable(incident *core.Incident) error
}

// updateIncident applies an operator change (acknowledge, resolve) to a
// stored incident. The write happens outside m.mu — a replicated write can
// take seconds — and the in-memory incident only changes once it succeeded;
// a failed write is returned instead of being logged and reported as success.
func (m *Manager) updateIncident(incidentID, workspace string, mutate func(*core.Incident, time.Time), onApplied func()) error {
	m.mu.RLock()
	incident, ok := m.incidents[incidentID]
	if !ok {
		m.mu.RUnlock()
		return &core.NotFoundError{Entity: "incident", ID: incidentID}
	}
	// Fail-closed: require workspace match (never skip when empty). Another
	// workspace's incident is reported as missing, so its ID is not confirmed.
	if workspaceOrDefault(incident.WorkspaceID) != workspaceOrDefault(workspace) {
		m.mu.RUnlock()
		return &core.NotFoundError{Entity: "incident", ID: incidentID}
	}
	updated := *incident
	updated.Events = append([]core.AlertEvent(nil), incident.Events...)
	updated.Notes = append([]core.IncidentNote(nil), incident.Notes...)
	m.mu.RUnlock()

	mutate(&updated, time.Now())

	if m.storage != nil {
		var err error
		if durable, ok := m.storage.(DurableIncidentStore); ok {
			err = durable.SaveIncidentDurable(&updated)
		} else {
			err = m.storage.SaveIncident(&updated)
		}
		if err != nil {
			m.logger.Error("Failed to save incident", "incident_id", incidentID, "error", err)
			return fmt.Errorf("save incident: %w", err)
		}
	}

	m.mu.Lock()
	current, ok := m.incidents[incidentID]
	if !ok {
		current = &updated
		m.incidents[incidentID] = current
	} else {
		// Keep events appended while the write was in flight.
		updated.Events = current.Events
		*current = updated
	}
	onApplied()
	notify := m.incidentNotifier(current)
	m.mu.Unlock()
	notify()
	return nil
}

// ListActiveIncidents returns incidents that are still open or acknowledged
// (i.e. not yet resolved). Used by alerting/dashboarding callers that
// specifically want the "current incident inbox" view.
func (m *Manager) ListActiveIncidents() []*core.Incident {
	m.mu.RLock()
	defer m.mu.RUnlock()

	incidents := make([]*core.Incident, 0)
	for _, incident := range m.incidents {
		if incident.Status != core.IncidentResolved {
			incidents = append(incidents, incident)
		}
	}
	return incidents
}

// ListIncidents returns every incident the manager knows about — open,
// acknowledged, and resolved. The REST list endpoint uses this so a UI
// can show recovery history; callers that only want active incidents
// can keep using ListActiveIncidents.
func (m *Manager) ListIncidents() []*core.Incident {
	m.mu.RLock()
	defer m.mu.RUnlock()

	incidents := make([]*core.Incident, 0, len(m.incidents))
	for _, incident := range m.incidents {
		incidents = append(incidents, incident)
	}
	return incidents
}

// ListChannelsByWorkspace returns channels filtered by workspace
func (m *Manager) ListChannelsByWorkspace(workspace string) []*core.AlertChannel {
	m.mu.RLock()
	defer m.mu.RUnlock()

	channels := make([]*core.AlertChannel, 0)
	for _, ch := range m.channels {
		if ch.WorkspaceID == "" || ch.WorkspaceID == workspace {
			channels = append(channels, ch)
		}
	}
	return channels
}

// ListChannels returns all registered channels
func (m *Manager) ListChannels() []*core.AlertChannel {
	m.mu.RLock()
	defer m.mu.RUnlock()

	channels := make([]*core.AlertChannel, 0, len(m.channels))
	for _, ch := range m.channels {
		channels = append(channels, ch)
	}
	return channels
}

// ListRulesByWorkspace returns rules filtered by workspace
func (m *Manager) ListRulesByWorkspace(workspace string) []*core.AlertRule {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rules := make([]*core.AlertRule, 0)
	for _, rule := range m.rules {
		if rule.WorkspaceID == "" || rule.WorkspaceID == workspace {
			rules = append(rules, rule)
		}
	}
	return rules
}

// ListRules returns all registered rules
func (m *Manager) ListRules() []*core.AlertRule {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rules := make([]*core.AlertRule, 0, len(m.rules))
	for _, rule := range m.rules {
		rules = append(rules, rule)
	}
	return rules
}

// GetChannel returns a channel by ID
func (m *Manager) GetChannel(id string) (*core.AlertChannel, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ch, ok := m.channels[id]
	if !ok {
		return nil, fmt.Errorf("channel not found: %s", id)
	}
	return ch, nil
}

// GetRule returns a rule by ID
func (m *Manager) GetRule(id string) (*core.AlertRule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rule, ok := m.rules[id]
	if !ok {
		return nil, fmt.Errorf("rule not found: %s", id)
	}
	return rule, nil
}

// escalationChecker periodically checks for incidents that need escalation
func (m *Manager) escalationChecker() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.checkEscalations()
		}
	}
}

// checkEscalations checks all active incidents for escalation
func (m *Manager) checkEscalations() {
	// Resolve each incident's rule while holding the lock. Copying the map reference
	// (rules := m.rules) copies only the header: the reads below would then happen on
	// the live map, which RegisterRule and UnregisterRule write under this same mutex.
	// A rule edit concurrent with an escalation check is a data race on that map and
	// can abort the process with "fatal error: concurrent map read and map write".
	type escalationWork struct {
		incident *core.Incident
		rule     *core.AlertRule
	}

	m.mu.RLock()
	work := make([]escalationWork, 0, len(m.incidents))
	for _, inc := range m.incidents {
		if inc.Status != core.IncidentOpen && inc.Status != core.IncidentAcked {
			continue
		}
		rule, ok := m.rules[inc.RuleID]
		if !ok || rule.Escalation == nil || len(rule.Escalation.Stages) == 0 {
			continue
		}
		work = append(work, escalationWork{incident: inc, rule: rule})
	}
	m.mu.RUnlock()

	for _, w := range work {
		// Check if escalation is needed
		if m.shouldEscalate(w.incident, w.rule) {
			m.escalateIncident(w.incident, w.rule)
		}
	}
}

// shouldEscalate determines if an incident needs escalation
func (m *Manager) shouldEscalate(incident *core.Incident, rule *core.AlertRule) bool {
	esc := rule.Escalation
	if esc == nil || len(esc.Stages) == 0 {
		return false
	}

	// Check if we have more stages
	if incident.EscalationLevel >= len(esc.Stages) {
		return false
	}

	// Get current stage
	stage := esc.Stages[incident.EscalationLevel]

	// "not_acknowledged" (the default) stops at acknowledgement;
	// "not_resolved" keeps escalating an acknowledged incident until resolved.
	if incident.AckedAt != nil && stage.Condition != "not_resolved" {
		return false
	}

	// Calculate time since last escalation (or since started if never escalated)
	var timeSinceLastEvent time.Duration
	if incident.LastEscalatedAt != nil {
		timeSinceLastEvent = time.Since(*incident.LastEscalatedAt)
	} else {
		timeSinceLastEvent = time.Since(incident.StartedAt)
	}

	// Check wait time for this stage
	wait := stage.Wait.Duration
	if wait == 0 {
		wait = 15 * time.Minute // Default 15 minute escalation timeout
	}

	return timeSinceLastEvent >= wait
}

// escalateIncident escalates an incident to higher-level channels
func (m *Manager) escalateIncident(incident *core.Incident, rule *core.AlertRule) {
	m.mu.Lock()

	esc := rule.Escalation
	if esc == nil || len(esc.Stages) == 0 {
		m.mu.Unlock()
		return
	}

	// Check if we have more stages
	if incident.EscalationLevel >= len(esc.Stages) {
		m.mu.Unlock()
		return
	}

	// Get current stage
	stage := esc.Stages[incident.EscalationLevel]

	// Get escalation channels
	channels := make([]*core.AlertChannel, 0, len(stage.Channels))
	for _, chID := range stage.Channels {
		if ch, ok := m.channels[chID]; ok {
			channels = append(channels, ch)
		}
	}

	if len(channels) == 0 {
		m.mu.Unlock()
		m.logger.Warn("No escalation channels available",
			"incident_id", incident.ID,
			"rule_id", rule.ID)
		return
	}

	// Create escalation event
	now := time.Now()
	event := &core.AlertEvent{
		ID:          generateAlertID(),
		SoulID:      incident.SoulID,
		SoulName:    incident.SoulName,
		WorkspaceID: incident.WorkspaceID,
		Status:      core.SoulDead, // Escalations are for critical incidents
		PrevStatus:  core.SoulDead,
		Message:     fmt.Sprintf("[ESCALATED Level %d] %s", incident.EscalationLevel+1, incident.SoulName),
		Severity:    core.SeverityCritical,
		Timestamp:   now,
	}

	// Unlock before calling sendToChannel (which also uses mutex)
	m.mu.Unlock()

	// Send to escalation channels
	// Use separate context with timeout - escalation alerts should be timely but not blocking
	for _, channel := range channels {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := m.sendToChannel(ctx, event, channel)
		cancel()

		if err != nil {
			m.logger.Error("Failed to send escalation alert",
				"channel", channel.ID,
				"incident_id", incident.ID,
				"error", err)
		}
	}

	// Update incident
	m.mu.Lock()
	incident.EscalationLevel++
	incident.LastEscalatedAt = &now

	if m.storage != nil {
		if err := m.storage.SaveIncident(incident); err != nil {
			m.logger.Error("Failed to save escalated incident",
				"incident_id", incident.ID,
				"error", err)
		}
	}
	notify := m.incidentNotifier(incident)
	m.mu.Unlock()
	notify()

	m.logger.Info("Incident escalated",
		"incident_id", incident.ID,
		"level", incident.EscalationLevel,
		"channels", len(channels))
}

// workspaceOrDefault maps the empty workspace to "default".
func workspaceOrDefault(workspace string) string {
	if workspace == "" {
		return "default"
	}
	return workspace
}
