package alert

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestAlertManagerRegistration(t *testing.T) {
	// Create mock storage
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}

	manager := NewManager(storage, newTestLogger())

	// Test channel registration
	channel := &core.AlertChannel{
		ID:      "test-channel",
		Name:    "Test Channel",
		Type:    core.ChannelWebHook,
		Enabled: true,
		Config: map[string]interface{}{
			"url": "https://example.com/webhook",
		},
	}

	if err := manager.RegisterChannel(channel); err != nil {
		t.Errorf("RegisterChannel failed: %v", err)
	}

	// Verify channel was registered
	channels := manager.ListChannels()
	if len(channels) != 1 {
		t.Errorf("Expected 1 channel, got %d", len(channels))
	}

	// Test rule registration
	rule := &core.AlertRule{
		ID:      "test-rule",
		Name:    "Test Rule",
		Enabled: true,
		Scope: core.RuleScope{
			Type: "all",
		},
		Conditions: []core.AlertCondition{
			{Type: "consecutive_failures", Threshold: 3},
		},
		Channels: []string{"test-channel"},
	}

	if err := manager.RegisterRule(rule); err != nil {
		t.Errorf("RegisterRule failed: %v", err)
	}

	// Verify rule was registered
	rules := manager.ListRules()
	if len(rules) != 1 {
		t.Errorf("Expected 1 rule, got %d", len(rules))
	}
}

func TestAlertManagerDelete(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}

	manager := NewManager(storage, newTestLogger())

	// Add and delete channel
	channel := &core.AlertChannel{
		ID:      "to-delete",
		Name:    "Delete Me",
		Type:    core.ChannelWebHook,
		Enabled: true,
	}
	manager.RegisterChannel(channel)
	manager.DeleteChannel("to-delete")

	if len(manager.ListChannels()) != 0 {
		t.Error("Channel was not deleted")
	}

	// Add and delete rule
	rule := &core.AlertRule{
		ID:      "to-delete",
		Name:    "Delete Me",
		Enabled: true,
		Scope:   core.RuleScope{Type: "all"},
		Conditions: []core.AlertCondition{
			{Type: "consecutive_failures", Threshold: 3},
		},
		Channels: []string{"channel-1"},
	}
	manager.RegisterRule(rule)
	manager.DeleteRule("to-delete")

	if len(manager.ListRules()) != 0 {
		t.Error("Rule was not deleted")
	}
}

func TestRuleApplies(t *testing.T) {
	storage := &mockAlertStorage{}
	manager := NewManager(storage, newTestLogger())

	soul := &core.Soul{
		ID:   "test-soul",
		Name: "Test Soul",
		Type: core.CheckHTTP,
		Tags: []string{"production", "api"},
	}

	tests := []struct {
		name     string
		scope    core.RuleScope
		expected bool
	}{
		{
			name:     "scope all",
			scope:    core.RuleScope{Type: "all"},
			expected: true,
		},
		{
			name:     "scope specific matching",
			scope:    core.RuleScope{Type: "specific", SoulIDs: []string{"test-soul"}},
			expected: true,
		},
		{
			name:     "scope specific not matching",
			scope:    core.RuleScope{Type: "specific", SoulIDs: []string{"other-soul"}},
			expected: false,
		},
		{
			name:     "scope tag matching",
			scope:    core.RuleScope{Type: "tag", Tags: []string{"production"}},
			expected: true,
		},
		{
			name:     "scope tag not matching",
			scope:    core.RuleScope{Type: "tag", Tags: []string{"staging"}},
			expected: false,
		},
		{
			name:     "scope type matching",
			scope:    core.RuleScope{Type: "type", SoulTypes: []string{"http"}},
			expected: true,
		},
		{
			name:     "scope type not matching",
			scope:    core.RuleScope{Type: "type", SoulTypes: []string{"tcp"}},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &core.AlertRule{Scope: tt.scope}
			result := manager.ruleApplies(rule, soul)
			if result != tt.expected {
				t.Errorf("ruleApplies() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCalculateSeverity(t *testing.T) {
	storage := &mockAlertStorage{}
	manager := NewManager(storage, newTestLogger())

	tests := []struct {
		name     string
		judgment *core.Judgment
		expected core.Severity
	}{
		{
			name: "dead soul",
			judgment: &core.Judgment{
				Status: core.SoulDead,
			},
			expected: core.SeverityCritical,
		},
		{
			name: "degraded soul",
			judgment: &core.Judgment{
				Status: core.SoulDegraded,
			},
			expected: core.SeverityWarning,
		},
		{
			name: "alive soul",
			judgment: &core.Judgment{
				Status: core.SoulAlive,
			},
			expected: core.SeverityInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.calculateSeverity(tt.judgment)
			if result != tt.expected {
				t.Errorf("calculateSeverity() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCheckConditions(t *testing.T) {
	storage := &mockAlertStorage{}
	manager := NewManager(storage, newTestLogger())

	tests := []struct {
		name       string
		conditions []core.AlertCondition
		prevStatus core.SoulStatus
		judgment   *core.Judgment
		expected   bool
	}{
		{
			name: "status change alive to dead",
			conditions: []core.AlertCondition{
				{Type: "status_change", From: "alive", To: "dead"},
			},
			prevStatus: core.SoulAlive,
			judgment:   &core.Judgment{Status: core.SoulDead},
			expected:   true,
		},
		{
			name: "status change no match",
			conditions: []core.AlertCondition{
				{Type: "status_change", From: "alive", To: "dead"},
			},
			prevStatus: core.SoulAlive,
			judgment:   &core.Judgment{Status: core.SoulAlive},
			expected:   false,
		},
		{
			name: "recovery detection",
			conditions: []core.AlertCondition{
				{Type: "recovery"},
			},
			prevStatus: core.SoulDead,
			judgment:   &core.Judgment{Status: core.SoulAlive},
			expected:   true,
		},
		{
			name: "degraded detection",
			conditions: []core.AlertCondition{
				{Type: "degraded"},
			},
			prevStatus: core.SoulAlive,
			judgment:   &core.Judgment{Status: core.SoulDegraded},
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &core.AlertRule{Conditions: tt.conditions}
			result := manager.checkConditions(rule, tt.prevStatus, tt.judgment)
			if result != tt.expected {
				t.Errorf("checkConditions() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// Mock storage for testing
type mockAlertStorage struct {
	channels map[string]*core.AlertChannel
	rules    map[string]*core.AlertRule
	events   []*core.AlertEvent
	incidents map[string]*core.Incident
}

func (m *mockAlertStorage) SaveChannel(ch *core.AlertChannel) error {
	m.channels[ch.ID] = ch
	return nil
}

func (m *mockAlertStorage) GetChannel(id string) (*core.AlertChannel, error) {
	ch, ok := m.channels[id]
	if !ok {
		return nil, nil
	}
	return ch, nil
}

func (m *mockAlertStorage) ListChannels() ([]*core.AlertChannel, error) {
	result := make([]*core.AlertChannel, 0, len(m.channels))
	for _, ch := range m.channels {
		result = append(result, ch)
	}
	return result, nil
}

func (m *mockAlertStorage) DeleteChannel(id string) error {
	delete(m.channels, id)
	return nil
}

func (m *mockAlertStorage) SaveRule(rule *core.AlertRule) error {
	m.rules[rule.ID] = rule
	return nil
}

func (m *mockAlertStorage) GetRule(id string) (*core.AlertRule, error) {
	rule, ok := m.rules[id]
	if !ok {
		return nil, nil
	}
	return rule, nil
}

func (m *mockAlertStorage) ListRules() ([]*core.AlertRule, error) {
	result := make([]*core.AlertRule, 0, len(m.rules))
	for _, rule := range m.rules {
		result = append(result, rule)
	}
	return result, nil
}

func (m *mockAlertStorage) DeleteRule(id string) error {
	delete(m.rules, id)
	return nil
}

func (m *mockAlertStorage) SaveEvent(event *core.AlertEvent) error {
	m.events = append(m.events, event)
	return nil
}

func (m *mockAlertStorage) ListEvents(soulID string, limit int) ([]*core.AlertEvent, error) {
	return m.events, nil
}

func (m *mockAlertStorage) SaveIncident(incident *core.Incident) error {
	if m.incidents == nil {
		m.incidents = make(map[string]*core.Incident)
	}
	m.incidents[incident.ID] = incident
	return nil
}

func (m *mockAlertStorage) GetIncident(id string) (*core.Incident, error) {
	if m.incidents == nil {
		return nil, nil
	}
	inc, ok := m.incidents[id]
	if !ok {
		return nil, nil
	}
	return inc, nil
}

func (m *mockAlertStorage) ListActiveIncidents() ([]*core.Incident, error) {
	if m.incidents == nil {
		return nil, nil
	}
	result := make([]*core.Incident, 0)
	for _, inc := range m.incidents {
		if inc.Status != core.IncidentResolved {
			result = append(result, inc)
		}
	}
	return result, nil
}

func TestSlackDispatcher_GetColor(t *testing.T) {
	dispatcher := &SlackDispatcher{logger: newTestLogger()}

	tests := []struct {
		name     string
		severity core.Severity
		status   core.SoulStatus
		expected string
	}{
		{"alive status", core.SeverityCritical, core.SoulAlive, "good"},
		{"dead critical", core.SeverityCritical, core.SoulDead, "danger"},
		{"degraded warning", core.SeverityWarning, core.SoulDegraded, "warning"},
		{"info default", core.SeverityInfo, core.SoulUnknown, "#439FE0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dispatcher.getColor(tt.severity, tt.status)
			if result != tt.expected {
				t.Errorf("getColor() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestSlackDispatcher_GetEmoji(t *testing.T) {
	dispatcher := &SlackDispatcher{logger: newTestLogger()}

	tests := []struct {
		name     string
		status   core.SoulStatus
		expected string
	}{
		{"alive", core.SoulAlive, "✅"},
		{"dead", core.SoulDead, "🔴"},
		{"degraded", core.SoulDegraded, "⚠️"},
		{"unknown", core.SoulUnknown, "ℹ️"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dispatcher.getEmoji(tt.status)
			if result != tt.expected {
				t.Errorf("getEmoji() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestSlackDispatcher_Validate(t *testing.T) {
	dispatcher := &SlackDispatcher{logger: newTestLogger()}

	// Valid config
	validConfig := map[string]interface{}{
		"webhook_url": "https://hooks.slack.com/services/xxx",
	}
	if err := dispatcher.Validate(validConfig); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}

	// Invalid config - missing webhook_url
	invalidConfig := map[string]interface{}{}
	if err := dispatcher.Validate(invalidConfig); err == nil {
		t.Error("Validate() expected error for missing webhook_url")
	}
}

func TestDiscordDispatcher_GetColor(t *testing.T) {
	dispatcher := &DiscordDispatcher{logger: newTestLogger()}

	tests := []struct {
		name     string
		severity core.Severity
		status   core.SoulStatus
		expected int
	}{
		{"alive", core.SeverityCritical, core.SoulAlive, 0x00ff00},
		{"dead critical", core.SeverityCritical, core.SoulDead, 0xff0000},
		{"degraded warning", core.SeverityWarning, core.SoulDegraded, 0xffa500},
		{"info default", core.SeverityInfo, core.SoulUnknown, 0x439FE0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dispatcher.getColor(tt.severity, tt.status)
			if result != tt.expected {
				t.Errorf("getColor() = 0x%x, want 0x%x", result, tt.expected)
			}
		})
	}
}

func TestDiscordDispatcher_Validate(t *testing.T) {
	dispatcher := &DiscordDispatcher{logger: newTestLogger()}

	// Valid config
	validConfig := map[string]interface{}{
		"webhook_url": "https://discord.com/api/webhooks/xxx",
	}
	if err := dispatcher.Validate(validConfig); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}

	// Invalid config
	invalidConfig := map[string]interface{}{}
	if err := dispatcher.Validate(invalidConfig); err == nil {
		t.Error("Validate() expected error for missing webhook_url")
	}
}

func TestTelegramDispatcher_Validate(t *testing.T) {
	dispatcher := &TelegramDispatcher{logger: newTestLogger()}

	// Valid config
	validConfig := map[string]interface{}{
		"bot_token": "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
		"chat_id":   "-123456789",
	}
	if err := dispatcher.Validate(validConfig); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}

	// Missing bot_token
	invalidConfig := map[string]interface{}{
		"chat_id": "-123456789",
	}
	if err := dispatcher.Validate(invalidConfig); err == nil {
		t.Error("Validate() expected error for missing bot_token")
	}

	// Missing chat_id
	invalidConfig2 := map[string]interface{}{
		"bot_token": "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
	}
	if err := dispatcher.Validate(invalidConfig2); err == nil {
		t.Error("Validate() expected error for missing chat_id")
	}
}

func TestNtfyDispatcher_Validate(t *testing.T) {
	dispatcher := &NtfyDispatcher{logger: newTestLogger()}

	// Valid config
	validConfig := map[string]interface{}{
		"topic": "my-topic",
	}
	if err := dispatcher.Validate(validConfig); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}

	// Missing topic
	invalidConfig := map[string]interface{}{}
	if err := dispatcher.Validate(invalidConfig); err == nil {
		t.Error("Validate() expected error for missing topic")
	}
}

func TestWebHookDispatcher_Validate(t *testing.T) {
	dispatcher := &WebHookDispatcher{logger: newTestLogger()}

	// Valid config
	validConfig := map[string]interface{}{
		"url": "https://example.com/webhook",
	}
	if err := dispatcher.Validate(validConfig); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}

	// Missing url
	invalidConfig := map[string]interface{}{}
	if err := dispatcher.Validate(invalidConfig); err == nil {
		t.Error("Validate() expected error for missing url")
	}
}

func TestEmailDispatcher_Validate(t *testing.T) {
	dispatcher := &EmailDispatcher{logger: newTestLogger()}

	// Valid config
	validConfig := map[string]interface{}{
		"smtp_host": "smtp.example.com",
		"smtp_port": "587",
		"from":      "alerts@example.com",
		"to":        "admin@example.com",
	}
	if err := dispatcher.Validate(validConfig); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}

	// Missing smtp_host
	invalidConfig := map[string]interface{}{
		"smtp_port": "587",
		"from":      "alerts@example.com",
		"to":        "admin@example.com",
	}
	if err := dispatcher.Validate(invalidConfig); err == nil {
		t.Error("Validate() expected error for missing smtp_host")
	}
}

func TestPagerDutyDispatcher_Validate(t *testing.T) {
	dispatcher := &PagerDutyDispatcher{logger: newTestLogger()}

	// Valid config
	validConfig := map[string]interface{}{
		"integration_key": "abc123def456",
	}
	if err := dispatcher.Validate(validConfig); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}

	// Missing integration_key
	invalidConfig := map[string]interface{}{}
	if err := dispatcher.Validate(invalidConfig); err == nil {
		t.Error("Validate() expected error for missing integration_key")
	}
}

func TestOpsGenieDispatcher_Validate(t *testing.T) {
	dispatcher := &OpsGenieDispatcher{logger: newTestLogger()}

	// Valid config
	validConfig := map[string]interface{}{
		"api_key": "abc123-def456-ghi789",
	}
	if err := dispatcher.Validate(validConfig); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}

	// Missing api_key
	invalidConfig := map[string]interface{}{}
	if err := dispatcher.Validate(invalidConfig); err == nil {
		t.Error("Validate() expected error for missing api_key")
	}
}

func TestSMSDispatcher_Validate(t *testing.T) {
	dispatcher := &SMSDispatcher{logger: newTestLogger()}

	// Valid config
	validConfig := map[string]interface{}{
		"provider": "twilio",
		"account_sid": "AC1234567890",
		"auth_token": "abc123",
		"from": "+1234567890",
		"to": "+0987654321",
	}
	if err := dispatcher.Validate(validConfig); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}

	// Missing provider
	invalidConfig := map[string]interface{}{
		"account_sid": "AC1234567890",
	}
	if err := dispatcher.Validate(invalidConfig); err == nil {
		t.Error("Validate() expected error for missing provider")
	}
}

func TestAlertEventCreation(t *testing.T) {
	event := &core.AlertEvent{
		ID:        "event-1",
		SoulID:    "soul-1",
		SoulName:  "Test Soul",
		Severity:  core.SeverityCritical,
		Status:    core.SoulDead,
		Message:   "Test alert message",
		Timestamp: time.Now().UTC(),
		Details: map[string]string{
			"response_time": "5000ms",
			"status_code":   "500",
		},
	}

	if event.ID == "" {
		t.Error("Event ID should not be empty")
	}
	if event.Severity != core.SeverityCritical {
		t.Errorf("Expected severity critical, got %s", event.Severity)
	}
}

// Additional tests for unique coverage

func TestHmacSha256_Consistency(t *testing.T) {
	data := []byte("test data")
	secret := "test-secret"

	sig1 := hmacSha256(data, secret)
	sig2 := hmacSha256(data, secret)

	if sig1 != sig2 {
		t.Error("HMAC should be consistent")
	}
}

func TestHmacSha256_DifferentInputs(t *testing.T) {
	secret := "test-secret"

	sig1 := hmacSha256([]byte("data1"), secret)
	sig2 := hmacSha256([]byte("data2"), secret)

	if sig1 == sig2 {
		t.Error("Different inputs should produce different HMACs")
	}
}

func TestAlertManager_Worker(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	// Start manager (starts workers)
	if err := manager.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer manager.Stop()

	// Send event to queue
	event := &core.AlertEvent{
		ID:       "test-event",
		SoulID:   "test-soul",
		SoulName: "Test Soul",
		Status:   core.SoulDead,
	}

	// Queue should accept event without blocking
	select {
	case manager.queue <- event:
		// OK
	default:
		t.Error("Queue should accept event")
	}

	// Give worker time to process
	time.Sleep(50 * time.Millisecond)
}

func TestAlertManager_StartStop(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	// Start
	if err := manager.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Second start should be no-op
	if err := manager.Start(); err != nil {
		t.Errorf("Second start should be no-op: %v", err)
	}

	// Stop
	if err := manager.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Second stop should be no-op
	if err := manager.Stop(); err != nil {
		t.Errorf("Second stop should be no-op: %v", err)
	}
}

func TestAlertManager_ListChannels(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	channel := &core.AlertChannel{
		ID:   "list-channel",
		Name: "List Channel",
		Type: core.ChannelWebHook,
	}
	manager.RegisterChannel(channel)

	channels := manager.ListChannels()
	if len(channels) != 1 {
		t.Errorf("Expected 1 channel, got %d", len(channels))
	}
}

func TestAlertManager_ListRules(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	rule := &core.AlertRule{
		ID:    "list-rule",
		Name:  "List Rule",
		Scope: core.RuleScope{Type: "all"},
	}
	manager.RegisterRule(rule)

	rules := manager.ListRules()
	if len(rules) != 1 {
		t.Errorf("Expected 1 rule, got %d", len(rules))
	}
}

func TestAlertManager_GetChannel(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	channel := &core.AlertChannel{
		ID:   "get-channel",
		Name: "Get Channel",
		Type: core.ChannelWebHook,
	}
	manager.RegisterChannel(channel)

	retrieved, err := manager.GetChannel("get-channel")
	if err != nil {
		t.Errorf("GetChannel failed: %v", err)
	}
	if retrieved == nil {
		t.Error("Expected channel to be found")
	}

	notFound, err := manager.GetChannel("nonexistent")
	if err == nil && notFound != nil {
		t.Error("Expected nil for nonexistent channel")
	}
}

func TestAlertManager_GetRule(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	rule := &core.AlertRule{
		ID:    "get-rule",
		Name:  "Get Rule",
		Scope: core.RuleScope{Type: "all"},
	}
	manager.RegisterRule(rule)

	retrieved, err := manager.GetRule("get-rule")
	if err != nil {
		t.Errorf("GetRule failed: %v", err)
	}
	if retrieved == nil {
		t.Error("Expected rule to be found")
	}

	notFound, err := manager.GetRule("nonexistent")
	if err == nil && notFound != nil {
		t.Error("Expected nil for nonexistent rule")
	}
}

func TestCalculateSeverity_Degraded(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	judgment := &core.Judgment{
		Status: core.SoulDegraded,
	}

	result := manager.calculateSeverity(judgment)
	if result != core.SeverityWarning {
		t.Errorf("calculateSeverity degraded = %v, want %v", result, core.SeverityWarning)
	}
}

func TestCheckConditions_EmptyConditions(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	rule := &core.AlertRule{Conditions: []core.AlertCondition{}}
	judgment := &core.Judgment{Status: core.SoulDead}

	result := manager.checkConditions(rule, core.SoulAlive, judgment)
	if result != false {
		t.Errorf("checkConditions with empty conditions = %v, want false", result)
	}
}

func TestCheckConditions_RecoveryFromDegraded(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	rule := &core.AlertRule{
		Conditions: []core.AlertCondition{
			{Type: "recovery"},
		},
	}
	// Recovery is defined as SoulDead -> SoulAlive
	judgment := &core.Judgment{
		Status: core.SoulAlive,
	}

	result := manager.checkConditions(rule, core.SoulDead, judgment)
	if result != true {
		t.Errorf("checkConditions recovery from dead = %v, want true", result)
	}
}

func TestSMSDispatcher_Validate_MissingTo(t *testing.T) {
	dispatcher := &SMSDispatcher{logger: newTestLogger()}

	// Missing to
	invalidConfig := map[string]interface{}{
		"provider": "twilio",
	}
	if err := dispatcher.Validate(invalidConfig); err == nil {
		t.Error("Validate() expected error for missing to")
	}
}

func (m *mockAlertStorage) Close() error {
	return nil
}

// Additional Manager method tests

func TestManager_ProcessJudgment(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	// Add a rule that applies to all souls
	rule := &core.AlertRule{
		ID:      "rule-1",
		Enabled: true,
		Scope: core.RuleScope{
			Type: "all",
		},
		Conditions: []core.AlertCondition{
			{Type: "consecutive_failures", Threshold: 1},
		},
		Channels: []string{},
	}
	storage.rules[rule.ID] = rule

	soul := &core.Soul{
		ID:          "test-soul",
		WorkspaceID: "default",
		Name:        "Test Soul",
		Type:        core.CheckHTTP,
	}

	judgment := &core.Judgment{
		Status:   core.SoulDead,
		Message:  "Test failure",
		Duration: time.Second * 5,
	}

	// Process judgment - should trigger alert
	manager.ProcessJudgment(soul, core.SoulAlive, judgment)

	// Check that alert was queued (may or may not trigger depending on condition logic)
	// The consecutive_failures condition requires consecutive failures
	// For this test, we just verify the method doesn't panic
}

func TestManager_sendToChannel(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	channel := &core.AlertChannel{
		ID:      "channel-1",
		Type:    core.ChannelWebHook,
		Enabled: true,
		Config: map[string]interface{}{
			"url":    "https://example.com/webhook",
			"method": "POST",
		},
	}
	storage.channels[channel.ID] = channel

	event := &core.AlertEvent{
		ID:        "alert-1",
		SoulID:    "test-soul",
		SoulName:  "Test Soul",
		Status:    core.SoulDead,
		Severity:  core.SeverityCritical,
		Message:   "Test alert",
		Timestamp: time.Now().UTC(),
	}

	// sendToChannel should not panic
	ctx := context.Background()
	manager.sendToChannel(ctx, event, channel)
}

func TestManager_generateAlertID(t *testing.T) {
	id := generateAlertID()
	if len(id) == 0 {
		t.Error("Expected non-empty alert ID")
	}
}

func TestManager_generateShortID(t *testing.T) {
	id := generateShortID()
	if len(id) == 0 {
		t.Error("Expected non-empty short ID")
	}
	if len(id) > 8 {
		t.Errorf("Expected short ID <= 8 chars, got %d", len(id))
	}
}

func TestManager_GetStats(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	// Add channels and rules
	storage.channels["ch1"] = &core.AlertChannel{ID: "ch1", Type: core.ChannelWebHook}
	storage.channels["ch2"] = &core.AlertChannel{ID: "ch2", Type: core.ChannelSlack}
	storage.rules["rule1"] = &core.AlertRule{ID: "rule1", Scope: core.RuleScope{Type: "all"}}

	stats := manager.GetStats()
	// Stats tracks alert counts, not channel/rule counts
	_ = stats
}

func TestManager_AcknowledgeIncident(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	incident := &core.Incident{
		ID:        "incident-1",
		RuleID:    "rule-1",
		SoulID:    "soul-1",
		WorkspaceID: "default",
		Status:    core.IncidentOpen,
		Severity:  core.SeverityCritical,
		StartedAt: time.Now().UTC(),
	}
	// Add incident directly to manager's internal map
	manager.mu.Lock()
	manager.incidents[incident.ID] = incident
	manager.mu.Unlock()

	err := manager.AcknowledgeIncident(incident.ID, "test-user")
	if err != nil {
		t.Fatalf("AcknowledgeIncident failed: %v", err)
	}

	// Verify status changed to Acknowledged
	manager.mu.RLock()
	status := manager.incidents[incident.ID].Status
	manager.mu.RUnlock()
	if status != core.IncidentAcked {
		t.Errorf("Expected status Acknowledged, got %s", status)
	}
}

func TestManager_ResolveIncident(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	incident := &core.Incident{
		ID:        "incident-1",
		RuleID:    "rule-1",
		SoulID:    "soul-1",
		WorkspaceID: "default",
		Status:    core.IncidentOpen,
		Severity:  core.SeverityCritical,
		StartedAt: time.Now().UTC(),
	}
	// Add incident directly to manager's internal map
	manager.mu.Lock()
	manager.incidents[incident.ID] = incident
	manager.mu.Unlock()

	err := manager.ResolveIncident(incident.ID, "test-user")
	if err != nil {
		t.Fatalf("ResolveIncident failed: %v", err)
	}

	// Verify status changed to Resolved
	manager.mu.RLock()
	status := manager.incidents[incident.ID].Status
	manager.mu.RUnlock()
	if status != core.IncidentResolved {
		t.Errorf("Expected status Resolved, got %s", status)
	}
}

func TestManager_ListActiveIncidents(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	now := time.Now().UTC()
	incidents := []*core.Incident{
		{ID: "i1", RuleID: "r1", SoulID: "s1", WorkspaceID: "default", Status: core.IncidentOpen, StartedAt: now},
		{ID: "i2", RuleID: "r2", SoulID: "s2", WorkspaceID: "default", Status: core.IncidentAcked, StartedAt: now},
		{ID: "i3", RuleID: "r3", SoulID: "s3", WorkspaceID: "default", Status: core.IncidentResolved, StartedAt: now},
	}
	// Add incidents directly to manager's internal map
	manager.mu.Lock()
	for _, i := range incidents {
		manager.incidents[i.ID] = i
	}
	manager.mu.Unlock()

	active := manager.ListActiveIncidents()
	// Should return Open and Acked, not Resolved
	if len(active) != 2 {
		t.Errorf("Expected 2 active incidents, got %d", len(active))
	}
}

func TestManager_ProcessJudgment_NoRules(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	soul := &core.Soul{
		ID:          "soul-1",
		Name:        "Test Soul",
		WorkspaceID: "default",
	}

	prevStatus := core.SoulAlive
	judgment := &core.Judgment{
		Status:  core.SoulDead,
		Message: "Test failed",
	}

	// Should not panic with no rules
	manager.ProcessJudgment(soul, prevStatus, judgment)
}

func TestManager_ProcessJudgment_DisabledRule(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	// Add a disabled rule
	rule := &core.AlertRule{
		ID:      "disabled-rule",
		Name:    "Disabled Rule",
		Enabled: false,
		Scope:   core.RuleScope{Type: "all"},
		Conditions: []core.AlertCondition{
			{Type: "status_change", From: "alive", To: "dead"},
		},
		Channels: []string{},
	}
	manager.RegisterRule(rule)

	soul := &core.Soul{
		ID:          "soul-1",
		Name:        "Test Soul",
		WorkspaceID: "default",
	}

	judgment := &core.Judgment{
		Status:  core.SoulDead,
		Message: "Test failed",
	}

	// Disabled rule should not trigger
	manager.ProcessJudgment(soul, core.SoulAlive, judgment)
}

func TestManager_ProcessJudgment_RuleNotApplicable(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	// Add a rule for specific soul that doesn't match
	rule := &core.AlertRule{
		ID:      "specific-rule",
		Name:    "Specific Rule",
		Enabled: true,
		Scope:   core.RuleScope{Type: "soul", SoulIDs: []string{"other-soul"}},
		Conditions: []core.AlertCondition{
			{Type: "status_change", From: "alive", To: "dead"},
		},
		Channels: []string{},
	}
	manager.RegisterRule(rule)

	soul := &core.Soul{
		ID:          "soul-1",
		Name:        "Test Soul",
		WorkspaceID: "default",
	}

	judgment := &core.Judgment{
		Status:  core.SoulDead,
		Message: "Test failed",
	}

	// Rule for other-soul should not apply to soul-1
	manager.ProcessJudgment(soul, core.SoulAlive, judgment)
}

func TestManager_ProcessJudgment_ConditionNotMet(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	// Add a rule with condition that won't be met
	rule := &core.AlertRule{
		ID:      "test-rule",
		Name:    "Test Rule",
		Enabled: true,
		Scope:   core.RuleScope{Type: "all"},
		Conditions: []core.AlertCondition{
			{Type: "status_change", From: "degraded", To: "dead"},
		},
		Channels: []string{},
	}
	manager.RegisterRule(rule)

	soul := &core.Soul{
		ID:          "soul-1",
		Name:        "Test Soul",
		WorkspaceID: "default",
	}

	// Transition from alive to dead, but rule expects degraded to dead
	judgment := &core.Judgment{
		Status:  core.SoulDead,
		Message: "Test failed",
	}

	manager.ProcessJudgment(soul, core.SoulAlive, judgment)
}

func TestManager_dispatch_NoChannels(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	event := &core.AlertEvent{
		ID:       "event-1",
		SoulID:   "soul-1",
		SoulName: "Test Soul",
		Status:   core.SoulDead,
		Message:  "Test failed",
	}

	// Should not panic with no channels
	manager.dispatch(event)
}

func TestManager_dispatch_ChannelNotMatching(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	// Add a channel that only notifies on specific events
	channel := &core.AlertChannel{
		ID:      "channel-1",
		Name:    "Test Channel",
		Type:    core.ChannelWebHook,
		Enabled: true,
		Filters: []core.AlertFilter{
			{Field: "status", Operator: "eq", Value: "resolved"}, // Only notify on resolved, not dead
		},
		Config: map[string]interface{}{
			"url": "https://example.com/webhook",
		},
	}
	manager.RegisterChannel(channel)

	event := &core.AlertEvent{
		ID:       "event-1",
		SoulID:   "soul-1",
		SoulName: "Test Soul",
		Status:   core.SoulDead,
		Message:  "Test failed",
	}

	// Channel only notifies on resolved, so this should be skipped
	manager.dispatch(event)
}

func TestManager_dispatch_RateLimitedChannel(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	// Add a webhook channel
	channel := &core.AlertChannel{
		ID:      "channel-1",
		Name:    "Test Channel",
		Type:    core.ChannelWebHook,
		Enabled: true,
		Config: map[string]interface{}{
			"url": "https://example.com/webhook",
		},
		RateLimit: core.RateLimitConfig{
			Enabled:   true,
			MaxAlerts: 1,
			Window:    core.Duration{Duration: time.Minute},
		},
	}
	manager.RegisterChannel(channel)

	event1 := &core.AlertEvent{
		ID:       "event-1",
		SoulID:   "soul-1",
		SoulName: "Test Soul",
		Status:   core.SoulDead,
		Message:  "Test failed",
	}

	event2 := &core.AlertEvent{
		ID:       "event-2",
		SoulID:   "soul-1",
		SoulName: "Test Soul",
		Status:   core.SoulDead,
		Message:  "Test failed again",
	}

	// First dispatch should go through
	manager.dispatch(event1)

	// Second dispatch should be rate limited
	manager.dispatch(event2)
}

func TestManager_dispatch_MultipleChannels(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	// Add multiple channels
	channel1 := &core.AlertChannel{
		ID:      "webhook-1",
		Name:    "Webhook Channel",
		Type:    core.ChannelWebHook,
		Enabled: true,
		Config: map[string]interface{}{
			"url": "https://example.com/webhook",
		},
	}

	channel2 := &core.AlertChannel{
		ID:      "invalid-1",
		Name:    "Invalid Channel",
		Type:    "invalid-type",
		Enabled: true,
	}

	manager.RegisterChannel(channel1)
	manager.RegisterChannel(channel2)

	event := &core.AlertEvent{
		ID:       "event-1",
		SoulID:   "soul-1",
		SoulName: "Test Soul",
		Status:   core.SoulDead,
		Message:  "Test failed",
	}

	// Should process both channels (one succeeds, one fails)
	manager.dispatch(event)
}

func TestManager_dispatch_DisabledChannel(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	// Add a disabled channel
	channel := &core.AlertChannel{
		ID:      "channel-1",
		Name:    "Disabled Channel",
		Type:    core.ChannelWebHook,
		Enabled: false,
		Config: map[string]interface{}{
			"url": "https://example.com/webhook",
		},
	}
	manager.RegisterChannel(channel)

	event := &core.AlertEvent{
		ID:       "event-1",
		SoulID:   "soul-1",
		SoulName: "Test Soul",
		Status:   core.SoulDead,
		Message:  "Test failed",
	}

	// Disabled channel should not be notified
	manager.dispatch(event)
}

func TestManager_sendToChannel_InvalidType(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	event := &core.AlertEvent{
		ID:       "event-1",
		SoulID:   "soul-1",
		SoulName: "Test Soul",
		Status:   core.SoulDead,
		Message:  "Test failed",
	}

	channel := &core.AlertChannel{
		ID:      "channel-1",
		Name:    "Test Channel",
		Type:    "invalid-type",
		Enabled: true,
	}

	ctx := context.Background()
	err := manager.sendToChannel(ctx, event, channel)

	if err == nil {
		t.Error("Expected error for invalid channel type")
	}
}

func TestManager_extractDetails_NilDetails(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	judgment := &core.Judgment{
		Status:  core.SoulDead,
		Message: "Test failed",
		// Details is nil
	}

	details := manager.extractDetails(judgment)

	if details == nil {
		t.Error("Expected details map to be created")
	}
}

func TestManager_calculateSeverity(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	tests := []struct {
		status   core.SoulStatus
		expected core.Severity
	}{
		{core.SoulDead, core.SeverityCritical},
		{core.SoulDegraded, core.SeverityWarning},
		{core.SoulAlive, core.SeverityInfo},
		{core.SoulUnknown, core.SeverityInfo},
	}

	for _, tt := range tests {
		judgment := &core.Judgment{
			Status: tt.status,
		}
		severity := manager.calculateSeverity(judgment)
		if severity != tt.expected {
			t.Errorf("For status %s, expected severity %s, got %s",
				tt.status, tt.expected, severity)
		}
	}
}
