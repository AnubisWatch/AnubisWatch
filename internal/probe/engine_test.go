package probe

import (
	"os"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"log/slog"
)

func newTestProbeLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
}

func TestCheckerRegistry(t *testing.T) {
	registry := NewCheckerRegistry()

	// Test that all expected checkers are registered
	expectedCheckers := []core.CheckType{
		core.CheckHTTP,
		core.CheckTCP,
		core.CheckUDP,
		core.CheckDNS,
		core.CheckSMTP,
		core.CheckIMAP,
		core.CheckICMP,
		core.CheckGRPC,
		core.CheckWebSocket,
		core.CheckTLS,
	}

	for _, checkType := range expectedCheckers {
		checker, ok := registry.Get(checkType)
		if !ok {
			t.Errorf("checker %s not found in registry", checkType)
			continue
		}
		if checker.Type() != checkType {
			t.Errorf("checker type mismatch: expected %s, got %s", checkType, checker.Type())
		}
	}

	// Test List
	allTypes := registry.List()
	if len(allTypes) != len(expectedCheckers) {
		t.Errorf("expected %d checkers, got %d", len(expectedCheckers), len(allTypes))
	}
}

func TestHTTPChecker_Validate(t *testing.T) {
	checker := NewHTTPChecker()

	// Valid HTTP soul
	validSoul := &core.Soul{
		ID:     "test-http",
		Name:   "Test HTTP",
		Type:   core.CheckHTTP,
		Target: "https://example.com",
		HTTP: &core.HTTPConfig{
			Method:      "GET",
			ValidStatus: []int{200},
		},
	}

	if err := checker.Validate(validSoul); err != nil {
		t.Errorf("Validate failed for valid soul: %v", err)
	}

	// Invalid - missing target
	invalidSoul := &core.Soul{
		ID:   "test-invalid",
		Name: "Invalid",
		Type: core.CheckHTTP,
	}

	if err := checker.Validate(invalidSoul); err == nil {
		t.Error("expected validation error for missing target")
	}
}

func TestTCPChecker_Validate(t *testing.T) {
	checker := NewTCPChecker()

	// Valid TCP soul
	validSoul := &core.Soul{
		ID:     "test-tcp",
		Name:   "Test TCP",
		Type:   core.CheckTCP,
		Target: "localhost:443",
		TCP:    &core.TCPConfig{},
	}

	if err := checker.Validate(validSoul); err != nil {
		t.Errorf("Validate failed for valid soul: %v", err)
	}

	// Invalid - missing port
	invalidSoul := &core.Soul{
		ID:     "test-invalid",
		Name:   "Invalid",
		Type:   core.CheckTCP,
		Target: "localhost",
	}

	if err := checker.Validate(invalidSoul); err == nil {
		t.Error("expected validation error for missing port")
	}
}

func TestDNSChecker_Validate(t *testing.T) {
	checker := NewDNSChecker()

	// Valid DNS soul
	validSoul := &core.Soul{
		ID:     "test-dns",
		Name:   "Test DNS",
		Type:   core.CheckDNS,
		Target: "8.8.8.8",
		DNS: &core.DNSConfig{
			RecordType: "A",
			Expected:   []string{"8.8.8.8"},
		},
	}

	if err := checker.Validate(validSoul); err != nil {
		t.Errorf("Validate failed for valid soul: %v", err)
	}
}

func TestTLSChecker_Validate(t *testing.T) {
	checker := NewTLSChecker()

	// Valid TLS soul
	validSoul := &core.Soul{
		ID:     "test-tls",
		Name:   "Test TLS",
		Type:   core.CheckTLS,
		Target: "https://example.com:443",
		TLS: &core.TLSConfig{
			ExpiryWarnDays: 30,
		},
	}

	if err := checker.Validate(validSoul); err != nil {
		t.Errorf("Validate failed for valid soul: %v", err)
	}
}

func TestBaseCheckerHelpers(t *testing.T) {
	soul := &core.Soul{
		ID:   "test-soul",
		Name: "Test",
		Type: core.CheckHTTP,
	}

	// Test failJudgment
	failed := failJudgment(soul, &core.ValidationError{Field: "connection", Message: "connection refused"})
	if failed.Status != core.SoulDead {
		t.Errorf("expected status dead, got %s", failed.Status)
	}

	// Test successJudgment
	success := successJudgment(soul, 100*time.Millisecond, "OK")
	if success.Status != core.SoulAlive {
		t.Errorf("expected status alive, got %s", success.Status)
	}
	if success.Duration != 100*time.Millisecond {
		t.Errorf("expected duration 100ms, got %v", success.Duration)
	}

	// Test degradedJudgment
	degraded := degradedJudgment(soul, 5*time.Second, "slow response")
	if degraded.Status != core.SoulDegraded {
		t.Errorf("expected status degraded, got %s", degraded.Status)
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input    string
		max      int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
	}

	for _, tt := range tests {
		result := truncateString(tt.input, tt.max)
		if result != tt.expected {
			t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.max, result, tt.expected)
		}
	}
}

func TestBoolToString(t *testing.T) {
	if result := boolToString(true, "yes", "no"); result != "yes" {
		t.Errorf("boolToString(true) = %q, want 'yes'", result)
	}
	if result := boolToString(false, "yes", "no"); result != "no" {
		t.Errorf("boolToString(false) = %q, want 'no'", result)
	}
}

func TestParseDuration(t *testing.T) {
	if result := parseDuration("5s", time.Second); result != 5*time.Second {
		t.Errorf("parseDuration('5s') = %v, want 5s", result)
	}
	if result := parseDuration("invalid", 10*time.Second); result != 10*time.Second {
		t.Errorf("parseDuration('invalid') with default = %v, want 10s", result)
	}
	if result := parseDuration("", 5*time.Minute); result != 5*time.Minute {
		t.Errorf("parseDuration('') with default = %v, want 5m", result)
	}
}

func TestEngine_AssignSouls(t *testing.T) {
	registry := NewCheckerRegistry()
	engine := NewEngine(EngineOptions{
		Registry: registry,
		NodeID:   "test-node",
		Region:   "test-region",
		Logger:   newTestProbeLogger(),
	})

	souls := []*core.Soul{
		{
			ID:      "soul-1",
			Name:    "Soul 1",
			Type:    core.CheckHTTP,
			Target:  "https://example.com",
			Enabled: true,
			Weight:  core.Duration{Duration: 60 * time.Second},
			HTTP:    &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
		},
		{
			ID:      "soul-2",
			Name:    "Soul 2",
			Type:    core.CheckTCP,
			Target:  "localhost:443",
			Enabled: true,
			Weight:  core.Duration{Duration: 30 * time.Second},
			TCP:     &core.TCPConfig{},
		},
	}

	engine.AssignSouls(souls)

	// Souls should be assigned and running
	// Note: We can't easily test the actual running checkers without mocking
	// the storage and alerter interfaces
}

func TestEngine_RemoveSouls(t *testing.T) {
	registry := NewCheckerRegistry()
	engine := NewEngine(EngineOptions{
		Registry: registry,
		NodeID:   "test-node",
		Region:   "test-region",
		Logger:   newTestProbeLogger(),
	})

	// Assign initial souls
	souls := []*core.Soul{
		{
			ID:      "soul-1",
			Name:    "Soul 1",
			Type:    core.CheckHTTP,
			Target:  "https://example.com",
			Enabled: true,
			Weight:  core.Duration{Duration: 60 * time.Second},
			HTTP:    &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
		},
	}

	engine.AssignSouls(souls)

	// Remove souls by assigning empty list
	engine.AssignSouls([]*core.Soul{})

	// Soul should be stopped (ticker cancelled)
	// Note: Actual verification would require mocking
}
