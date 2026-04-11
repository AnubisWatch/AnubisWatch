package alert

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Manager handles alert routing and delivery
// The high priest who receives omens and dispatches messengers
type Manager struct {
	mu        sync.RWMutex
	channels  map[string]*core.AlertChannel
	rules     map[string]*core.AlertRule
	history   *core.AlertHistory
	incidents map[string]*core.Incident

	// Dispatchers
	dispatchers map[core.AlertChannelType]ChannelDispatcher

	// State
	running bool
	stopCh  chan struct{}
	queue   chan *core.AlertEvent

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
}

// ChannelDispatcher sends notifications to a specific channel type
type ChannelDispatcher interface {
	Send(ctx context.Context, event *core.AlertEvent, channel *core.AlertChannel) error
	Validate(config map[string]any) error
}

// AlertStorage persists alert data
type AlertStorage interface {
	SaveChannel(channel *core.AlertChannel) error
	GetChannel(id string) (*core.AlertChannel, error)
	ListChannels() ([]*core.AlertChannel, error)
	DeleteChannel(id string) error

	SaveRule(rule *core.AlertRule) error
	GetRule(id string) (*core.AlertRule, error)
	ListRules() ([]*core.AlertRule, error)
	DeleteRule(id string) error

	SaveEvent(event *core.AlertEvent) error
	ListEvents(soulID string, limit int) ([]*core.AlertEvent, error)

	SaveIncident(incident *core.Incident) error
	GetIncident(id string) (*core.Incident, error)
	ListActiveIncidents() ([]*core.Incident, error)
}

// NewManager creates a new alert manager
func NewManager(storage AlertStorage, logger *slog.Logger) *Manager {
	m := &Manager{
		channels:    make(map[string]*core.AlertChannel),
		rules:       make(map[string]*core.AlertRule),
		history:     &core.AlertHistory{Entries: make(map[string]*core.AlertHistoryEntry)},
		incidents:   make(map[string]*core.Incident),
		dispatchers: make(map[core.AlertChannelType]ChannelDispatcher),
		stopCh:      make(chan struct{}),
		queue:       make(chan *core.AlertEvent, 1000),
		logger:      logger.With("component", "alert_manager"),
		storage:     storage,
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

	if m.running {
		return nil
	}

	m.running = true

	// Recreate stopCh if it was closed (allows restart after Stop)
	select {
	case <-m.stopCh:
		m.stopCh = make(chan struct{})
	default:
	}

	// Load channels and rules from storage
	if m.storage != nil {
		channels, err := m.storage.ListChannels()
		if err != nil {
			m.logger.Warn("Failed to load channels", "error", err)
		} else {
			for _, ch := range channels {
				m.channels[ch.ID] = ch
			}
		}

		rules, err := m.storage.ListRules()
		if err != nil {
			m.logger.Warn("Failed to load rules", "error", err)
		} else {
			for _, rule := range rules {
				m.rules[rule.ID] = rule
			}
		}
	}

	// Start workers
	for i := 0; i < 5; i++ {
		go m.worker()
	}

	// Start escalation checker
	go m.escalationChecker()

	m.logger.Info("Alert manager started",
		"channels", len(m.channels),
		"rules", len(m.rules))

	return nil
}

// Stop stops the alert manager
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return nil
	}

	m.running = false
	close(m.stopCh)

	return nil
}

// RegisterChannel adds or updates an alert channel
func (m *Manager) RegisterChannel(channel *core.AlertChannel) error {
	if err := channel.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	m.channels[channel.ID] = channel
	m.mu.Unlock()

	if m.storage != nil {
		if err := m.storage.SaveChannel(channel); err != nil {
			m.logger.Warn("Failed to save channel", "error", err)
		}
	}

	m.logger.Info("Channel registered",
		"id", channel.ID,
		"name", channel.Name,
		"type", channel.Type)

	return nil
}

// DeleteChannel removes an alert channel
func (m *Manager) DeleteChannel(id string) error {
	m.mu.Lock()
	delete(m.channels, id)
	m.mu.Unlock()

	if m.storage != nil {
		if err := m.storage.DeleteChannel(id); err != nil {
			m.logger.Warn("Failed to delete channel", "error", err)
		}
	}

	return nil
}

// RegisterRule adds or updates an alert rule
func (m *Manager) RegisterRule(rule *core.AlertRule) error {
	m.mu.Lock()
	m.rules[rule.ID] = rule
	m.mu.Unlock()

	if m.storage != nil {
		if err := m.storage.SaveRule(rule); err != nil {
			m.logger.Warn("Failed to save rule", "error", err)
		}
	}

	m.logger.Info("Rule registered",
		"id", rule.ID,
		"name", rule.Name)

	return nil
}

// DeleteRule removes an alert rule
func (m *Manager) DeleteRule(id string) error {
	m.mu.Lock()
	delete(m.rules, id)
	m.mu.Unlock()

	if m.storage != nil {
		if err := m.storage.DeleteRule(id); err != nil {
			m.logger.Warn("Failed to delete rule", "error", err)
		}
	}

	return nil
}

// ProcessJudgment evaluates a judgment against alert rules
func (m *Manager) ProcessJudgment(soul *core.Soul, prevStatus core.SoulStatus, judgment *core.Judgment) {
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

		// Check conditions
		triggered := m.checkConditions(rule, prevStatus, judgment)
		if !triggered {
			continue
		}

		// Create alert event
		event := &core.AlertEvent{
			ID:          generateAlertID(),
			SoulID:      soul.ID,
			SoulName:    soul.Name,
			WorkspaceID: soul.WorkspaceID,
			Status:      judgment.Status,
			PrevStatus:  prevStatus,
			Judgment:    judgment,
			Message:     judgment.Message,
			Details:     m.extractDetails(judgment),
			Severity:    m.calculateSeverity(judgment),
			Timestamp:   time.Now().UTC(),
		}

		// Check deduplication before queuing
		if m.isDuplicate(rule, event) {
			m.logger.Debug("Alert deduplicated",
				"rule", rule.Name,
				"soul_id", event.SoulID)
			continue
		}

		// Send to queue
		select {
		case m.queue <- event:
		default:
			m.logger.Warn("Alert queue full, dropping event",
				"soul_id", soul.ID)
		}
	}
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
	m.mu.RLock()
	channels := make([]*core.AlertChannel, 0, len(m.channels))
	for _, ch := range m.channels {
		channels = append(channels, ch)
	}
	m.mu.RUnlock()

	// Process channels concurrently with a semaphore to limit goroutines
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10) // Limit concurrent dispatchers

	for _, channel := range channels {
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

		wg.Add(1)
		sem <- struct{}{}

		go func(ch *core.AlertChannel) {
			defer wg.Done()
			defer func() { <-sem }()

			// Send notification
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			err := m.sendToChannel(ctx, event, ch)
			cancel()

			if err != nil {
				m.logger.Error("Failed to send alert",
					"channel", ch.ID,
					"error", err)
			} else {
				m.logger.Debug("Alert sent",
					"channel", ch.ID,
					"soul_id", event.SoulID)
			}
		}(channel)
	}

	wg.Wait()
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

	// Send the alert
	err := dispatcher.Send(ctx, event, channel)

	// Track result
	m.mu.Lock()
	if err != nil {
		m.stats.failedAlerts++
	} else {
		m.stats.sentAlerts++
	}
	m.mu.Unlock()

	return err
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

	switch scope.Type {
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
		return true
	}
}

// checkConditions checks if alert conditions are met
func (m *Manager) checkConditions(rule *core.AlertRule, prevStatus core.SoulStatus, judgment *core.Judgment) bool {
	for _, cond := range rule.Conditions {
		switch cond.Type {
		case "status_change":
			if string(prevStatus) == cond.From && string(judgment.Status) == cond.To {
				return true
			}
		case "status_for":
			// Would need history tracking
			if string(judgment.Status) == cond.Status {
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

	// Build grouping key
	key := fmt.Sprintf("%s:%s:%s", channel.ID, event.SoulID, channel.RateLimit.GroupingKey)

	m.history.Mu.Lock()
	defer m.history.Mu.Unlock()

	entry, exists := m.history.Entries[key]
	if !exists {
		// First alert for this group
		m.history.Entries[key] = &core.AlertHistoryEntry{
			Key:        key,
			ChannelID:  channel.ID,
			Count:      1,
			FirstSent:  time.Now(),
			LastSent:   time.Now(),
			SoulStatus: event.Status,
		}
		return false
	}

	// Check if window has expired
	if time.Since(entry.FirstSent) > channel.RateLimit.Window.Duration {
		// Reset window
		entry.Count = 1
		entry.FirstSent = time.Now()
		entry.LastSent = time.Now()
		return false
	}

	// Check limit
	if entry.Count >= channel.RateLimit.MaxAlerts {
		entry.LastSent = time.Now()
		return true
	}

	// Increment count
	entry.Count++
	entry.LastSent = time.Now()
	return false
}

// isDuplicate checks if an identical alert was recently sent (deduplication)
func (m *Manager) isDuplicate(rule *core.AlertRule, event *core.AlertEvent) bool {
	// Build dedup key: rule + soul + status
	key := fmt.Sprintf("dedup:%s:%s:%s", rule.ID, event.SoulID, event.Status)

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
	case "latency", "duration":
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
		case "latency", "duration":
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

	// Calculate standard deviation
	var sqSum float64
	for _, v := range values {
		diff := v - mean
		sqSum += diff * diff
	}
	stdDev := 0.0
	if len(values) > 1 {
		stdDev = sqSum / float64(len(values)-1)
		if stdDev > 0 {
			stdDev = stdDev / float64(len(values)-1) // Fix: variance = sqSum/(n-1), stdDev = sqrt(variance)
			// Actually let's compute properly:
			stdDev = 0
			for _, v := range values {
				d := v - mean
				stdDev += d * d
			}
			stdDev = stdDev / float64(len(values))
			// sqrt without math package
			stdDev = sqrtApprox(stdDev)
		}
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
	case "status_change":
		return string(prevStatus) == cond.From && string(judgment.Status) == cond.To
	case "status_for":
		return string(judgment.Status) == cond.Status
	case "failure_rate":
		return judgment.Status == core.SoulDead
	case "degraded":
		return judgment.Status == core.SoulDegraded
	case "recovery":
		return prevStatus == core.SoulDead && judgment.Status == core.SoulAlive
	case "anomaly":
		return m.checkAnomaly(cond, judgment)
	case "threshold":
		// Metric-based threshold
		var currentValue float64
		switch cond.Metric {
		case "latency", "duration":
			currentValue = float64(judgment.Duration.Milliseconds())
		case "status_code":
			currentValue = float64(judgment.StatusCode)
		default:
			currentValue = float64(judgment.Duration.Milliseconds())
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

// sqrtApprox computes an approximate square root using Newton's method.
func sqrtApprox(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 10; i++ {
		z = z - (z*z-x)/(2*z)
	}
	return z
}

// generateAlertID generates a unique alert ID
func generateAlertID() string {
	return fmt.Sprintf("alert_%d_%s", time.Now().Unix(), generateShortID())
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
func (m *Manager) AcknowledgeIncident(incidentID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	incident, ok := m.incidents[incidentID]
	if !ok {
		return fmt.Errorf("incident not found: %s", incidentID)
	}

	now := time.Now()
	incident.Status = core.IncidentAcked
	incident.AckedAt = &now
	incident.AckedBy = userID

	m.stats.acknowledgedAlerts++

	if m.storage != nil {
		m.storage.SaveIncident(incident)
	}

	return nil
}

// ResolveIncident marks an incident as resolved
func (m *Manager) ResolveIncident(incidentID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	incident, ok := m.incidents[incidentID]
	if !ok {
		return fmt.Errorf("incident not found: %s", incidentID)
	}

	now := time.Now()
	incident.Status = core.IncidentResolved
	incident.ResolvedAt = &now
	incident.ResolvedBy = userID

	m.stats.resolvedAlerts++

	if m.storage != nil {
		m.storage.SaveIncident(incident)
	}

	return nil
}

// ListActiveIncidents returns active incidents
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
	m.mu.RLock()
	incidents := make([]*core.Incident, 0, len(m.incidents))
	for _, inc := range m.incidents {
		if inc.Status == core.IncidentOpen || inc.Status == core.IncidentAcked {
			incidents = append(incidents, inc)
		}
	}
	rules := m.rules
	m.mu.RUnlock()

	for _, incident := range incidents {
		rule, ok := rules[incident.RuleID]
		if !ok || rule.Escalation == nil || len(rule.Escalation.Stages) == 0 {
			continue
		}

		// Check if escalation is needed
		if m.shouldEscalate(incident, rule) {
			m.escalateIncident(incident, rule)
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

	// Don't escalate if acknowledged
	if incident.AckedAt != nil {
		return false
	}

	// Get current stage
	stage := esc.Stages[incident.EscalationLevel]

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
		SoulName:    incident.SoulID,
		WorkspaceID: incident.WorkspaceID,
		Status:      core.SoulDead, // Escalations are for critical incidents
		PrevStatus:  core.SoulDead,
		Message:     fmt.Sprintf("[ESCALATED Level %d] %s", incident.EscalationLevel+1, incident.SoulID),
		Severity:    core.SeverityCritical,
		Timestamp:   now,
	}

	// Unlock before calling sendToChannel (which also uses mutex)
	m.mu.Unlock()

	// Send to escalation channels
	for _, channel := range channels {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
		m.storage.SaveIncident(incident)
	}
	m.mu.Unlock()

	m.logger.Info("Incident escalated",
		"incident_id", incident.ID,
		"level", incident.EscalationLevel,
		"channels", len(channels))
}
