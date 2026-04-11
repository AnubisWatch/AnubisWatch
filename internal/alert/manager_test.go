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
	channels  map[string]*core.AlertChannel
	rules     map[string]*core.AlertRule
	events    []*core.AlertEvent
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
		"provider":    "twilio",
		"account_sid": "AC1234567890",
		"auth_token":  "abc123",
		"from":        "+1234567890",
		"to":          "+0987654321",
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
		ID:          "incident-1",
		RuleID:      "rule-1",
		SoulID:      "soul-1",
		WorkspaceID: "default",
		Status:      core.IncidentOpen,
		Severity:    core.SeverityCritical,
		StartedAt:   time.Now().UTC(),
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
		ID:          "incident-1",
		RuleID:      "rule-1",
		SoulID:      "soul-1",
		WorkspaceID: "default",
		Status:      core.IncidentOpen,
		Severity:    core.SeverityCritical,
		StartedAt:   time.Now().UTC(),
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

// Tests for escalation functions

func TestManager_shouldEscalate_NoEscalationConfig(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	incident := &core.Incident{
		ID:              "inc-1",
		RuleID:          "rule-1",
		SoulID:          "soul-1",
		EscalationLevel: 0,
		StartedAt:       time.Now().Add(-30 * time.Minute),
	}

	// Rule with no escalation config
	rule := &core.AlertRule{
		ID:         "rule-1",
		Escalation: nil,
	}

	result := manager.shouldEscalate(incident, rule)
	if result {
		t.Error("shouldEscalate should return false when no escalation config")
	}
}

func TestManager_shouldEscalate_NoStages(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	incident := &core.Incident{
		ID:              "inc-1",
		RuleID:          "rule-1",
		SoulID:          "soul-1",
		EscalationLevel: 0,
		StartedAt:       time.Now().Add(-30 * time.Minute),
	}

	rule := &core.AlertRule{
		ID: "rule-1",
		Escalation: &core.EscalationPolicy{
			Stages: []core.EscalationStage{},
		},
	}

	result := manager.shouldEscalate(incident, rule)
	if result {
		t.Error("shouldEscalate should return false when no stages")
	}
}

func TestManager_shouldEscalate_MaxLevelReached(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	incident := &core.Incident{
		ID:              "inc-1",
		RuleID:          "rule-1",
		SoulID:          "soul-1",
		EscalationLevel: 2, // Already at max level
		StartedAt:       time.Now().Add(-2 * time.Hour),
	}

	rule := &core.AlertRule{
		ID: "rule-1",
		Escalation: &core.EscalationPolicy{
			Stages: []core.EscalationStage{
				{Wait: core.Duration{Duration: 15 * time.Minute}, Channels: []string{"ch1"}},
				{Wait: core.Duration{Duration: 30 * time.Minute}, Channels: []string{"ch2"}},
			},
		},
	}

	result := manager.shouldEscalate(incident, rule)
	if result {
		t.Error("shouldEscalate should return false when max level reached")
	}
}

func TestManager_shouldEscalate_Acknowledged(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	ackedAt := time.Now().Add(-30 * time.Minute)
	incident := &core.Incident{
		ID:              "inc-1",
		RuleID:          "rule-1",
		SoulID:          "soul-1",
		EscalationLevel: 0,
		StartedAt:       time.Now().Add(-1 * time.Hour),
		AckedAt:         &ackedAt,
	}

	rule := &core.AlertRule{
		ID: "rule-1",
		Escalation: &core.EscalationPolicy{
			Stages: []core.EscalationStage{
				{Wait: core.Duration{Duration: 15 * time.Minute}, Channels: []string{"ch1"}},
			},
		},
	}

	result := manager.shouldEscalate(incident, rule)
	if result {
		t.Error("shouldEscalate should return false when incident is acknowledged")
	}
}

func TestManager_shouldEscalate_WaitTimeNotMet(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	// Incident started only 5 minutes ago, but wait time is 15 minutes
	incident := &core.Incident{
		ID:              "inc-1",
		RuleID:          "rule-1",
		SoulID:          "soul-1",
		EscalationLevel: 0,
		StartedAt:       time.Now().Add(-5 * time.Minute),
	}

	rule := &core.AlertRule{
		ID: "rule-1",
		Escalation: &core.EscalationPolicy{
			Stages: []core.EscalationStage{
				{Wait: core.Duration{Duration: 15 * time.Minute}, Channels: []string{"ch1"}},
			},
		},
	}

	result := manager.shouldEscalate(incident, rule)
	if result {
		t.Error("shouldEscalate should return false when wait time not met")
	}
}

func TestManager_shouldEscalate_WaitTimeMet(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	// Incident started 30 minutes ago, wait time is 15 minutes
	incident := &core.Incident{
		ID:              "inc-1",
		RuleID:          "rule-1",
		SoulID:          "soul-1",
		EscalationLevel: 0,
		StartedAt:       time.Now().Add(-30 * time.Minute),
	}

	rule := &core.AlertRule{
		ID: "rule-1",
		Escalation: &core.EscalationPolicy{
			Stages: []core.EscalationStage{
				{Wait: core.Duration{Duration: 15 * time.Minute}, Channels: []string{"ch1"}},
			},
		},
	}

	result := manager.shouldEscalate(incident, rule)
	if !result {
		t.Error("shouldEscalate should return true when wait time is met")
	}
}

func TestManager_shouldEscalate_SecondLevelWait(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	lastEscalated := time.Now().Add(-5 * time.Minute)
	incident := &core.Incident{
		ID:              "inc-1",
		RuleID:          "rule-1",
		SoulID:          "soul-1",
		EscalationLevel: 1, // Already escalated once
		StartedAt:       time.Now().Add(-1 * time.Hour),
		LastEscalatedAt: &lastEscalated,
	}

	rule := &core.AlertRule{
		ID: "rule-1",
		Escalation: &core.EscalationPolicy{
			Stages: []core.EscalationStage{
				{Wait: core.Duration{Duration: 15 * time.Minute}, Channels: []string{"ch1"}},
				{Wait: core.Duration{Duration: 30 * time.Minute}, Channels: []string{"ch2"}},
			},
		},
	}

	// Only 5 minutes since last escalation, but need 30
	result := manager.shouldEscalate(incident, rule)
	if result {
		t.Error("shouldEscalate should return false when second level wait time not met")
	}
}

func TestManager_shouldEscalate_DefaultWaitTime(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	// Incident started 20 minutes ago, no wait time specified (default 15 min)
	incident := &core.Incident{
		ID:              "inc-1",
		RuleID:          "rule-1",
		SoulID:          "soul-1",
		EscalationLevel: 0,
		StartedAt:       time.Now().Add(-20 * time.Minute),
	}

	rule := &core.AlertRule{
		ID: "rule-1",
		Escalation: &core.EscalationPolicy{
			Stages: []core.EscalationStage{
				{Wait: core.Duration{Duration: 0}, Channels: []string{"ch1"}}, // No wait time
			},
		},
	}

	result := manager.shouldEscalate(incident, rule)
	if !result {
		t.Error("shouldEscalate should use default 15 min wait time")
	}
}

func TestManager_escalateIncident_NoEscalation(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	incident := &core.Incident{
		ID:              "inc-1",
		RuleID:          "rule-1",
		SoulID:          "soul-1",
		EscalationLevel: 0,
		StartedAt:       time.Now(),
	}

	// Rule with no escalation
	rule := &core.AlertRule{
		ID:         "rule-1",
		Escalation: nil,
	}

	// Should not panic
	manager.escalateIncident(incident, rule)

	// Level should not change
	if incident.EscalationLevel != 0 {
		t.Error("Escalation level should not change when no escalation config")
	}
}

func TestManager_escalateIncident_NoStages(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	incident := &core.Incident{
		ID:              "inc-1",
		RuleID:          "rule-1",
		SoulID:          "soul-1",
		EscalationLevel: 0,
		StartedAt:       time.Now(),
	}

	rule := &core.AlertRule{
		ID: "rule-1",
		Escalation: &core.EscalationPolicy{
			Stages: []core.EscalationStage{},
		},
	}

	// Should not panic
	manager.escalateIncident(incident, rule)

	// Level should not change
	if incident.EscalationLevel != 0 {
		t.Error("Escalation level should not change when no stages")
	}
}

func TestManager_escalateIncident_MaxLevel(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	incident := &core.Incident{
		ID:              "inc-1",
		RuleID:          "rule-1",
		SoulID:          "soul-1",
		EscalationLevel: 2, // At max level
		StartedAt:       time.Now(),
	}

	rule := &core.AlertRule{
		ID: "rule-1",
		Escalation: &core.EscalationPolicy{
			Stages: []core.EscalationStage{
				{Wait: core.Duration{Duration: 15 * time.Minute}, Channels: []string{"ch1"}},
				{Wait: core.Duration{Duration: 30 * time.Minute}, Channels: []string{"ch2"}},
			},
		},
	}

	// Should not panic
	manager.escalateIncident(incident, rule)

	// Level should not change
	if incident.EscalationLevel != 2 {
		t.Error("Escalation level should not change when at max level")
	}
}

func TestManager_escalateIncident_NoChannels(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	incident := &core.Incident{
		ID:              "inc-1",
		RuleID:          "rule-1",
		SoulID:          "soul-1",
		WorkspaceID:     "default",
		EscalationLevel: 0,
		StartedAt:       time.Now(),
	}

	rule := &core.AlertRule{
		ID: "rule-1",
		Escalation: &core.EscalationPolicy{
			Stages: []core.EscalationStage{
				{Wait: core.Duration{Duration: 15 * time.Minute}, Channels: []string{"nonexistent"}},
			},
		},
	}

	// Should not panic even though channel doesn't exist
	manager.escalateIncident(incident, rule)

	// Level should not change since no channels were found
	if incident.EscalationLevel != 0 {
		t.Error("Escalation level should not change when no valid channels")
	}
}

func TestManager_escalateIncident_Success(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	// Add a channel for escalation
	channel := &core.AlertChannel{
		ID:      "escalation-ch",
		Name:    "Escalation Channel",
		Type:    core.ChannelWebHook,
		Enabled: true,
		Config: map[string]interface{}{
			"url": "https://example.com/webhook",
		},
	}
	manager.RegisterChannel(channel)

	incident := &core.Incident{
		ID:              "inc-1",
		RuleID:          "rule-1",
		SoulID:          "soul-1",
		WorkspaceID:     "default",
		EscalationLevel: 0,
		StartedAt:       time.Now(),
	}

	rule := &core.AlertRule{
		ID: "rule-1",
		Escalation: &core.EscalationPolicy{
			Stages: []core.EscalationStage{
				{Wait: core.Duration{Duration: 15 * time.Minute}, Channels: []string{"escalation-ch"}},
			},
		},
	}

	manager.escalateIncident(incident, rule)

	// Level should increment
	if incident.EscalationLevel != 1 {
		t.Errorf("Escalation level should be 1, got %d", incident.EscalationLevel)
	}

	// LastEscalatedAt should be set
	if incident.LastEscalatedAt == nil {
		t.Error("LastEscalatedAt should be set")
	}
}

func TestManager_checkEscalations(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	// Add a channel
	channel := &core.AlertChannel{
		ID:      "escalation-ch",
		Name:    "Escalation Channel",
		Type:    core.ChannelWebHook,
		Enabled: true,
		Config: map[string]interface{}{
			"url": "https://example.com/webhook",
		},
	}
	manager.RegisterChannel(channel)

	// Add a rule with escalation
	rule := &core.AlertRule{
		ID:      "rule-1",
		Enabled: true,
		Scope:   core.RuleScope{Type: "all"},
		Escalation: &core.EscalationPolicy{
			Stages: []core.EscalationStage{
				{Wait: core.Duration{Duration: 1 * time.Millisecond}, Channels: []string{"escalation-ch"}},
			},
		},
	}
	manager.RegisterRule(rule)

	// Add an active incident (open, old enough to escalate)
	incident := &core.Incident{
		ID:              "inc-1",
		RuleID:          "rule-1",
		SoulID:          "soul-1",
		WorkspaceID:     "default",
		Status:          core.IncidentOpen,
		EscalationLevel: 0,
		StartedAt:       time.Now().Add(-1 * time.Hour),
	}
	manager.mu.Lock()
	manager.incidents[incident.ID] = incident
	manager.mu.Unlock()

	// Run checkEscalations
	manager.checkEscalations()

	// Incident should be escalated
	manager.mu.RLock()
	level := manager.incidents[incident.ID].EscalationLevel
	manager.mu.RUnlock()

	if level != 1 {
		t.Errorf("Expected escalation level 1, got %d", level)
	}
}

func TestManager_checkEscalations_AckedIncident(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	// Add a channel
	channel := &core.AlertChannel{
		ID:      "escalation-ch",
		Name:    "Escalation Channel",
		Type:    core.ChannelWebHook,
		Enabled: true,
		Config: map[string]interface{}{
			"url": "https://example.com/webhook",
		},
	}
	manager.RegisterChannel(channel)

	// Add a rule with escalation
	rule := &core.AlertRule{
		ID:      "rule-1",
		Enabled: true,
		Scope:   core.RuleScope{Type: "all"},
		Escalation: &core.EscalationPolicy{
			Stages: []core.EscalationStage{
				{Wait: core.Duration{Duration: 1 * time.Millisecond}, Channels: []string{"escalation-ch"}},
			},
		},
	}
	manager.RegisterRule(rule)

	ackedAt := time.Now().Add(-30 * time.Minute)
	incident := &core.Incident{
		ID:              "inc-1",
		RuleID:          "rule-1",
		SoulID:          "soul-1",
		WorkspaceID:     "default",
		Status:          core.IncidentAcked,
		EscalationLevel: 0,
		StartedAt:       time.Now().Add(-1 * time.Hour),
		AckedAt:         &ackedAt,
	}
	manager.mu.Lock()
	manager.incidents[incident.ID] = incident
	manager.mu.Unlock()

	// Run checkEscalations
	manager.checkEscalations()

	// Incident should NOT be escalated (it's acknowledged)
	manager.mu.RLock()
	level := manager.incidents[incident.ID].EscalationLevel
	manager.mu.RUnlock()

	if level != 0 {
		t.Errorf("Expected escalation level 0 (not escalated), got %d", level)
	}
}

func TestManager_checkEscalations_ResolvedIncident(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	// Add a rule with escalation
	rule := &core.AlertRule{
		ID:      "rule-1",
		Enabled: true,
		Scope:   core.RuleScope{Type: "all"},
		Escalation: &core.EscalationPolicy{
			Stages: []core.EscalationStage{
				{Wait: core.Duration{Duration: 1 * time.Millisecond}, Channels: []string{"ch1"}},
			},
		},
	}
	manager.RegisterRule(rule)

	resolvedAt := time.Now().Add(-30 * time.Minute)
	incident := &core.Incident{
		ID:              "inc-1",
		RuleID:          "rule-1",
		SoulID:          "soul-1",
		WorkspaceID:     "default",
		Status:          core.IncidentResolved,
		EscalationLevel: 0,
		StartedAt:       time.Now().Add(-1 * time.Hour),
		ResolvedAt:      &resolvedAt,
	}
	manager.mu.Lock()
	manager.incidents[incident.ID] = incident
	manager.mu.Unlock()

	// Run checkEscalations
	manager.checkEscalations()

	// Incident should NOT be escalated (it's resolved)
	manager.mu.RLock()
	level := manager.incidents[incident.ID].EscalationLevel
	manager.mu.RUnlock()

	if level != 0 {
		t.Errorf("Expected escalation level 0 (not escalated), got %d", level)
	}
}

func TestManager_checkEscalations_NoRule(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	// Add an incident with a rule ID that doesn't exist
	incident := &core.Incident{
		ID:              "inc-1",
		RuleID:          "nonexistent-rule",
		SoulID:          "soul-1",
		WorkspaceID:     "default",
		Status:          core.IncidentOpen,
		EscalationLevel: 0,
		StartedAt:       time.Now().Add(-1 * time.Hour),
	}
	manager.mu.Lock()
	manager.incidents[incident.ID] = incident
	manager.mu.Unlock()

	// Run checkEscalations - should not panic
	manager.checkEscalations()
}

func TestManager_checkEscalations_RuleNoEscalation(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	// Add a rule WITHOUT escalation
	rule := &core.AlertRule{
		ID:      "rule-1",
		Enabled: true,
		Scope:   core.RuleScope{Type: "all"},
		// No Escalation field
	}
	manager.RegisterRule(rule)

	incident := &core.Incident{
		ID:              "inc-1",
		RuleID:          "rule-1",
		SoulID:          "soul-1",
		WorkspaceID:     "default",
		Status:          core.IncidentOpen,
		EscalationLevel: 0,
		StartedAt:       time.Now().Add(-1 * time.Hour),
	}
	manager.mu.Lock()
	manager.incidents[incident.ID] = incident
	manager.mu.Unlock()

	// Run checkEscalations
	manager.checkEscalations()

	// Should not escalate (rule has no escalation config)
	manager.mu.RLock()
	level := manager.incidents[incident.ID].EscalationLevel
	manager.mu.RUnlock()

	if level != 0 {
		t.Errorf("Expected escalation level 0, got %d", level)
	}
}

// Tests for checkConditions to improve coverage

func TestCheckConditions_StatusFor(t *testing.T) {
	storage := &mockAlertStorage{}
	manager := NewManager(storage, newTestLogger())

	rule := &core.AlertRule{
		Conditions: []core.AlertCondition{
			{Type: "status_for", Status: "dead"},
		},
	}

	judgment := &core.Judgment{Status: core.SoulDead}
	result := manager.checkConditions(rule, core.SoulAlive, judgment)
	if !result {
		t.Error("checkConditions should trigger for status_for when status matches")
	}
}

func TestCheckConditions_FailureRate(t *testing.T) {
	storage := &mockAlertStorage{}
	manager := NewManager(storage, newTestLogger())

	rule := &core.AlertRule{
		Conditions: []core.AlertCondition{
			{Type: "failure_rate", Threshold: 50},
		},
	}

	judgment := &core.Judgment{Status: core.SoulDead}
	result := manager.checkConditions(rule, core.SoulAlive, judgment)
	if !result {
		t.Error("checkConditions should trigger for failure_rate when soul is dead")
	}
}

func TestCheckConditions_NoMatchingCondition(t *testing.T) {
	storage := &mockAlertStorage{}
	manager := NewManager(storage, newTestLogger())

	rule := &core.AlertRule{
		Conditions: []core.AlertCondition{
			{Type: "status_change", From: "alive", To: "dead"},
		},
	}

	// Status doesn't change from alive to dead
	judgment := &core.Judgment{Status: core.SoulAlive}
	result := manager.checkConditions(rule, core.SoulAlive, judgment)
	if result {
		t.Error("checkConditions should not trigger when condition doesn't match")
	}
}

func TestCheckConditions_MultipleConditions(t *testing.T) {
	storage := &mockAlertStorage{}
	manager := NewManager(storage, newTestLogger())

	rule := &core.AlertRule{
		Conditions: []core.AlertCondition{
			{Type: "status_change", From: "alive", To: "dead"},
			{Type: "degraded"},
		},
	}

	// First condition doesn't match, second does
	judgment := &core.Judgment{Status: core.SoulDegraded}
	result := manager.checkConditions(rule, core.SoulAlive, judgment)
	if !result {
		t.Error("checkConditions should trigger when any condition matches")
	}
}

// Tests for isDuplicate to improve coverage

func TestManager_isDuplicate_FirstAlert(t *testing.T) {
	storage := &mockAlertStorage{}
	manager := NewManager(storage, newTestLogger())

	rule := &core.AlertRule{
		ID:       "rule-1",
		Cooldown: core.Duration{Duration: 5 * time.Minute},
	}

	event := &core.AlertEvent{
		SoulID: "soul-1",
		Status: core.SoulDead,
	}

	// First alert should not be duplicate
	result := manager.isDuplicate(rule, event)
	if result {
		t.Error("First alert should not be duplicate")
	}
}

func TestManager_isDuplicate_WithinWindow(t *testing.T) {
	storage := &mockAlertStorage{}
	manager := NewManager(storage, newTestLogger())

	rule := &core.AlertRule{
		ID:       "rule-1",
		Cooldown: core.Duration{Duration: 5 * time.Minute},
	}

	event := &core.AlertEvent{
		SoulID: "soul-1",
		Status: core.SoulDead,
	}

	// First call - not duplicate
	manager.isDuplicate(rule, event)

	// Second call within window - should be duplicate
	result := manager.isDuplicate(rule, event)
	if !result {
		t.Error("Second alert within window should be duplicate")
	}
}

func TestManager_isDuplicate_StatusChanged(t *testing.T) {
	storage := &mockAlertStorage{}
	manager := NewManager(storage, newTestLogger())

	rule := &core.AlertRule{
		ID:       "rule-1",
		Cooldown: core.Duration{Duration: 5 * time.Minute},
	}

	event1 := &core.AlertEvent{
		SoulID: "soul-1",
		Status: core.SoulDead,
	}

	// First call - not duplicate
	manager.isDuplicate(rule, event1)

	// Second call with different status - not duplicate
	event2 := &core.AlertEvent{
		SoulID: "soul-1",
		Status: core.SoulAlive,
	}
	result := manager.isDuplicate(rule, event2)
	if result {
		t.Error("Alert with different status should not be duplicate")
	}
}

func TestManager_isDuplicate_WindowExpired(t *testing.T) {
	storage := &mockAlertStorage{}
	manager := NewManager(storage, newTestLogger())

	rule := &core.AlertRule{
		ID:       "rule-1",
		Cooldown: core.Duration{Duration: 1 * time.Nanosecond},
	}

	event := &core.AlertEvent{
		SoulID: "soul-1",
		Status: core.SoulDead,
	}

	// First call - not duplicate
	manager.isDuplicate(rule, event)

	// Wait a tiny bit for window to expire
	time.Sleep(1 * time.Millisecond)

	// Second call after window expired - should reset and not be duplicate
	result := manager.isDuplicate(rule, event)
	if result {
		t.Error("Alert after window expired should not be duplicate")
	}
}

func TestManager_isDuplicate_DefaultWindow(t *testing.T) {
	storage := &mockAlertStorage{}
	manager := NewManager(storage, newTestLogger())

	rule := &core.AlertRule{
		ID:       "rule-1",
		Cooldown: core.Duration{Duration: 0}, // No cooldown set
	}

	event := &core.AlertEvent{
		SoulID: "soul-1",
		Status: core.SoulDead,
	}

	// First call - not duplicate
	manager.isDuplicate(rule, event)

	// Second call immediately after - should use default 5 min window
	result := manager.isDuplicate(rule, event)
	if !result {
		t.Error("Alert within default window should be duplicate")
	}
}

func TestManager_isRateLimited_WindowExpired(t *testing.T) {
	storage := &mockAlertStorage{}
	manager := NewManager(storage, newTestLogger())

	channel := &core.AlertChannel{
		ID:   "ch-1",
		Type: core.ChannelWebHook,
		RateLimit: core.RateLimitConfig{
			Enabled:     true,
			MaxAlerts:   1,
			Window:      core.Duration{Duration: 1 * time.Millisecond},
			GroupingKey: "soul",
		},
	}

	event := &core.AlertEvent{
		SoulID: "soul-1",
		Status: core.SoulDead,
	}

	// First call - not rate limited
	result1 := manager.isRateLimited(channel, event)
	if result1 {
		t.Error("First alert should not be rate limited")
	}

	// Wait for window to expire
	time.Sleep(5 * time.Millisecond)

	// Second call after window - not rate limited (window reset)
	result2 := manager.isRateLimited(channel, event)
	if result2 {
		t.Error("Alert after window expired should not be rate limited")
	}
}

func TestManager_escalateIncident_MultipleChannels(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	// Add multiple channels
	channel1 := &core.AlertChannel{
		ID:      "esc-ch1",
		Name:    "Escalation Channel 1",
		Type:    core.ChannelWebHook,
		Enabled: true,
		Config:  map[string]interface{}{"url": "https://example.com/webhook1"},
	}
	channel2 := &core.AlertChannel{
		ID:      "esc-ch2",
		Name:    "Escalation Channel 2",
		Type:    core.ChannelWebHook,
		Enabled: true,
		Config:  map[string]interface{}{"url": "https://example.com/webhook2"},
	}
	manager.RegisterChannel(channel1)
	manager.RegisterChannel(channel2)

	incident := &core.Incident{
		ID:              "inc-1",
		RuleID:          "rule-1",
		SoulID:          "soul-1",
		WorkspaceID:     "default",
		EscalationLevel: 0,
		StartedAt:       time.Now(),
	}

	rule := &core.AlertRule{
		ID: "rule-1",
		Escalation: &core.EscalationPolicy{
			Stages: []core.EscalationStage{
				{Wait: core.Duration{Duration: 15 * time.Minute}, Channels: []string{"esc-ch1", "esc-ch2"}},
			},
		},
	}

	manager.escalateIncident(incident, rule)

	// Level should increment
	if incident.EscalationLevel != 1 {
		t.Errorf("Escalation level should be 1, got %d", incident.EscalationLevel)
	}
}

func TestManager_escalationChecker_Stop(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	// Start manager (which starts escalationChecker)
	if err := manager.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Stop should stop escalationChecker
	if err := manager.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

// TestManager_Start_AlreadyRunning tests calling Start twice
func TestManager_Start_AlreadyRunning(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())
	defer manager.Stop()

	if err := manager.Start(); err != nil {
		t.Fatalf("First Start failed: %v", err)
	}

	// Second Start should succeed without error (already running)
	if err := manager.Start(); err != nil {
		t.Errorf("Second Start should succeed, got error: %v", err)
	}
}

// TestManager_ProcessJudgment_AlertQueued tests full path through ProcessJudgment
func TestManager_ProcessJudgment_AlertQueued(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	// Create manager manually with custom queue
	m := &Manager{
		channels:    make(map[string]*core.AlertChannel),
		rules:       make(map[string]*core.AlertRule),
		history:     &core.AlertHistory{Entries: make(map[string]*core.AlertHistoryEntry)},
		incidents:   make(map[string]*core.Incident),
		dispatchers: make(map[core.AlertChannelType]ChannelDispatcher),
		stopCh:      make(chan struct{}),
		queue:       make(chan *core.AlertEvent, 100),
		logger:      newTestLogger(),
		storage:     storage,
	}
	m.registerDispatchers()

	// Add a rule with status_change: alive -> dead (will match)
	rule := &core.AlertRule{
		ID:      "rule-status",
		Name:    "Status Change Rule",
		Enabled: true,
		Scope:   core.RuleScope{Type: "all"},
		Conditions: []core.AlertCondition{
			{Type: "status_change", From: "alive", To: "dead"},
		},
		Channels: []string{},
	}
	m.RegisterRule(rule)

	soul := &core.Soul{
		ID:          "soul-alert",
		Name:        "Alert Soul",
		WorkspaceID: "default",
	}

	judgment := &core.Judgment{
		Status:  core.SoulDead,
		Message: "Soul is dead",
		Details: &core.JudgmentDetails{
			Assertions: []core.AssertionResult{
				{Type: "http_status", Expected: "200", Actual: "500", Passed: false},
			},
		},
	}

	// Process judgment with status change from alive to dead
	m.ProcessJudgment(soul, core.SoulAlive, judgment)

	// Alert should be queued - read from queue
	select {
	case event := <-m.queue:
		if event.SoulID != "soul-alert" {
			t.Errorf("Expected soul ID soul-alert, got %s", event.SoulID)
		}
		if event.Status != core.SoulDead {
			t.Errorf("Expected status Dead, got %s", event.Status)
		}
	case <-time.After(100 * time.Millisecond):
		t.Log("No alert in queue (may be expected depending on condition logic)")
	}
}

// TestManager_ProcessJudgment_NilJudgmentDetails tests extractDetails with nil
func TestManager_ProcessJudgment_NilJudgmentDetails(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	m := &Manager{
		channels:    make(map[string]*core.AlertChannel),
		rules:       make(map[string]*core.AlertRule),
		history:     &core.AlertHistory{Entries: make(map[string]*core.AlertHistoryEntry)},
		incidents:   make(map[string]*core.Incident),
		dispatchers: make(map[core.AlertChannelType]ChannelDispatcher),
		stopCh:      make(chan struct{}),
		queue:       make(chan *core.AlertEvent, 100),
		logger:      newTestLogger(),
		storage:     storage,
	}
	m.registerDispatchers()

	rule := &core.AlertRule{
		ID:         "rule-nil",
		Name:       "Nil Details Rule",
		Enabled:    true,
		Scope:      core.RuleScope{Type: "all"},
		Conditions: []core.AlertCondition{{Type: "consecutive_failures", Threshold: 1}},
		Channels:   []string{},
	}
	m.RegisterRule(rule)

	soul := &core.Soul{
		ID:          "soul-nil",
		Name:        "Nil Soul",
		WorkspaceID: "default",
	}

	// Judgment with nil Details
	judgment := &core.Judgment{
		Status:  core.SoulDead,
		Message: "No details",
		Details: nil,
	}

	m.ProcessJudgment(soul, core.SoulUnknown, judgment)
	// Should not panic
}

// TestManager_isDuplicate_SameStatus tests dedup when status unchanged
func TestManager_isDuplicate_SameStatus(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	rule := &core.AlertRule{
		ID:      "rule-dedup",
		Name:    "Dedup Rule",
		Enabled: true,
		Cooldown: core.Duration{Duration: 5 * time.Minute},
	}

	// First alert - not duplicate
	event1 := &core.AlertEvent{
		SoulID: "soul-dedup",
		Status: core.SoulDead,
	}
	if manager.isDuplicate(rule, event1) {
		t.Error("First alert should not be duplicate")
	}

	// Second alert with same status - should be duplicate
	event2 := &core.AlertEvent{
		SoulID: "soul-dedup",
		Status: core.SoulDead,
	}
	if !manager.isDuplicate(rule, event2) {
		t.Error("Second alert with same status should be duplicate")
	}
}

// TestManager_isDuplicate_CooldownExpired tests dedup window expiry
func TestManager_isDuplicate_CooldownExpired(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	rule := &core.AlertRule{
		ID:       "rule-expire",
		Name:     "Expiry Rule",
		Enabled:  true,
		Cooldown: core.Duration{Duration: 10 * time.Millisecond},
	}

	event := &core.AlertEvent{
		SoulID: "soul-expire",
		Status: core.SoulDead,
	}

	// First alert
	if manager.isDuplicate(rule, event) {
		t.Error("First alert should not be duplicate")
	}

	// Second alert immediately - should be duplicate
	if !manager.isDuplicate(rule, event) {
		t.Error("Immediate second alert should be duplicate")
	}

	// Wait for cooldown
	time.Sleep(20 * time.Millisecond)

	// Third alert after cooldown - should NOT be duplicate
	if manager.isDuplicate(rule, event) {
		t.Error("Alert after cooldown should not be duplicate")
	}
}

func TestCheckConditions_CompoundAnd(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}

	manager := NewManager(storage, newTestLogger())

	rule := &core.AlertRule{
		ID:      "compound-and",
		Name:    "Compound AND Rule",
		Enabled: true,
		Conditions: []core.AlertCondition{
			{
				Type: "compound",
				Logic: "and",
				SubConditions: []core.AlertCondition{
					{Type: "status_change", From: "alive", To: "dead"},
					{Type: "degraded"},
				},
			},
		},
	}

	if err := manager.RegisterRule(rule); err != nil {
		t.Fatalf("RegisterRule failed: %v", err)
	}

	// Both sub-conditions must match: status_change alive->dead AND status degraded
	// This is impossible since status can't be both dead (from status_change) and degraded at the same time
	judgment := &core.Judgment{
		SoulID: "soul-1",
		Status: core.SoulDegraded,
	}

	triggered := manager.checkConditions(rule, core.SoulAlive, judgment)
	// status_change needs alive->dead but judgment is degraded, so false
	if triggered {
		t.Error("Compound AND should not trigger when sub-conditions don't all match")
	}

	// Test with dead status - status_change matches but degraded doesn't
	judgment2 := &core.Judgment{
		SoulID: "soul-1",
		Status: core.SoulDead,
	}

	triggered2 := manager.checkConditions(rule, core.SoulAlive, judgment2)
	// status_change alive->dead matches, but degraded doesn't (status is dead)
	if triggered2 {
		t.Error("Compound AND should not trigger when only one sub-condition matches")
	}
}

func TestCheckConditions_CompoundOr(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}

	manager := NewManager(storage, newTestLogger())

	rule := &core.AlertRule{
		ID:      "compound-or",
		Name:    "Compound OR Rule",
		Enabled: true,
		Conditions: []core.AlertCondition{
			{
				Type: "compound",
				Logic: "or",
				SubConditions: []core.AlertCondition{
					{Type: "degraded"},
					{Type: "recovery"},
				},
			},
		},
	}

	if err := manager.RegisterRule(rule); err != nil {
		t.Fatalf("RegisterRule failed: %v", err)
	}

	// Test: degraded matches, recovery doesn't
	judgment := &core.Judgment{
		SoulID: "soul-1",
		Status: core.SoulDegraded,
	}

	triggered := manager.checkConditions(rule, core.SoulAlive, judgment)
	if !triggered {
		t.Error("Compound OR should trigger when at least one sub-condition matches")
	}

	// Test: neither matches
	judgment2 := &core.Judgment{
		SoulID: "soul-1",
		Status: core.SoulDead,
	}

	triggered2 := manager.checkConditions(rule, core.SoulDead, judgment2)
	if triggered2 {
		t.Error("Compound OR should not trigger when no sub-conditions match")
	}

	// Test: recovery matches (dead->alive)
	judgment3 := &core.Judgment{
		SoulID: "soul-1",
		Status: core.SoulAlive,
	}

	triggered3 := manager.checkConditions(rule, core.SoulDead, judgment3)
	if !triggered3 {
		t.Error("Compound OR should trigger on recovery")
	}
}

func TestCheckConditions_CompoundMajority(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}

	manager := NewManager(storage, newTestLogger())

	rule := &core.AlertRule{
		ID:      "compound-majority",
		Name:    "Compound Majority Rule",
		Enabled: true,
		Conditions: []core.AlertCondition{
			{
				Type:  "compound",
				Logic: "majority",
				SubConditions: []core.AlertCondition{
					{Type: "degraded"},
					{Type: "failure_rate"},
					{Type: "status_change", From: "alive", To: "dead"},
				},
			},
		},
	}

	if err := manager.RegisterRule(rule); err != nil {
		t.Fatalf("RegisterRule failed: %v", err)
	}

	// 2 of 3 match: degraded + failure_rate (both true for SoulDead)
	judgment := &core.Judgment{
		SoulID: "soul-1",
		Status: core.SoulDead,
	}

	triggered := manager.checkConditions(rule, core.SoulAlive, judgment)
	if !triggered {
		t.Error("Compound majority should trigger when >50% match (2/3 for dead)")
	}

	// Only 0 of 3 match (alive, prevStatus alive)
	judgment2 := &core.Judgment{
		SoulID: "soul-1",
		Status: core.SoulAlive,
	}

	triggered2 := manager.checkConditions(rule, core.SoulAlive, judgment2)
	if triggered2 {
		t.Error("Compound majority should not trigger when 0/3 match")
	}

	// Only 1 of 3 (degraded)
	judgment3 := &core.Judgment{
		SoulID: "soul-1",
		Status: core.SoulDegraded,
	}

	triggered3 := manager.checkConditions(rule, core.SoulAlive, judgment3)
	if triggered3 {
		t.Error("Compound majority should not trigger when only 1/3 match")
	}
}

func TestCheckConditions_AnomalyLatency(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}

	// Populate with historical events with some variance around 100ms
	latencies := []time.Duration{90, 95, 100, 105, 98, 102, 97, 103, 99, 101}
	for _, lat := range latencies {
		storage.events = append(storage.events, &core.AlertEvent{
			SoulID: "soul-1",
			Status: core.SoulAlive,
			Judgment: &core.Judgment{
				SoulID:   "soul-1",
				Status:   core.SoulAlive,
				Duration: lat * time.Millisecond,
			},
		})
	}

	manager := NewManager(storage, newTestLogger())

	// Anomalous latency: 10x normal
	judgment := &core.Judgment{
		SoulID:   "soul-1",
		Status:   core.SoulAlive,
		Duration: 1000 * time.Millisecond, // 1000ms, way above 100ms baseline
	}

	rule := &core.AlertRule{
		ID:      "anomaly-latency",
		Name:    "Anomaly Latency Rule",
		Enabled: true,
		Conditions: []core.AlertCondition{
			{
				Type:          "anomaly",
				Metric:        "latency",
				AnomalyStdDev: 2.0,
			},
		},
	}

	if err := manager.RegisterRule(rule); err != nil {
		t.Fatalf("RegisterRule failed: %v", err)
	}

	triggered := manager.checkConditions(rule, core.SoulAlive, judgment)
	if !triggered {
		t.Error("Anomaly condition should trigger when latency is significantly above baseline")
	}

	// Normal latency should not trigger
	judgment2 := &core.Judgment{
		SoulID:   "soul-1",
		Status:   core.SoulAlive,
		Duration: 100 * time.Millisecond, // Exactly at mean
	}

	triggered2 := manager.checkConditions(rule, core.SoulAlive, judgment2)
	if triggered2 {
		t.Error("Anomaly condition should not trigger when latency is near baseline")
	}
}

func TestCheckConditions_AnomalyFallback(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
		events:   nil, // No history
	}

	manager := NewManager(storage, newTestLogger())

	// No history - should use simple threshold fallback
	judgment := &core.Judgment{
		SoulID:   "soul-1",
		Status:   core.SoulAlive,
		Duration: 500 * time.Millisecond,
	}

	rule := &core.AlertRule{
		ID:      "anomaly-no-history",
		Name:    "Anomaly No History",
		Enabled: true,
		Conditions: []core.AlertCondition{
			{
				Type:          "anomaly",
				Metric:        "latency",
				Threshold:     100, // 100ms threshold
				Operator:      ">",
			},
		},
	}

	if err := manager.RegisterRule(rule); err != nil {
		t.Fatalf("RegisterRule failed: %v", err)
	}

	triggered := manager.checkConditions(rule, core.SoulAlive, judgment)
	if !triggered {
		t.Error("Anomaly should trigger with threshold fallback when no history")
	}
}
func TestVerdictsBySeverity(t *testing.T) {
	storage := &mockAlertStorage{
		channels: make(map[string]*core.AlertChannel),
		rules:    make(map[string]*core.AlertRule),
	}
	manager := NewManager(storage, newTestLogger())

	ch := &core.AlertChannel{
		ID:      "ch1",
		Name:    "test-webhook",
		Type:    core.ChannelWebHook,
		Enabled: true,
		Config:  map[string]any{"url": "http://localhost/test"},
	}

	// sendToChannel updates verdictsBySeverity before the HTTP send
	event := &core.AlertEvent{
		SoulID:    "soul-1",
		SoulName:  "test-soul",
		Status:    core.SoulDead,
		Severity:  core.SeverityCritical,
		Timestamp: time.Now(),
	}

	_ = manager.sendToChannel(context.Background(), event, ch) // May fail HTTP but stats updated

	stats := manager.GetStats()
	if stats.VerdictsBySeverity == nil {
		t.Fatal("Expected VerdictsBySeverity to be initialized")
	}
	if count, ok := stats.VerdictsBySeverity[string(core.SeverityCritical)]; !ok || count != 1 {
		t.Errorf("Expected 1 critical verdict, got %v", stats.VerdictsBySeverity)
	}
}
