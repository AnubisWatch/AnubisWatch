package alert

import (
	"log/slog"
	"os"
	"testing"

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

func (m *mockAlertStorage) Close() error {
	return nil
}
