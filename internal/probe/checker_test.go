package probe

import (
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TestNewCheckerRegistry tests the registry creation
func TestNewCheckerRegistry(t *testing.T) {
	registry := NewCheckerRegistry()

	if registry == nil {
		t.Fatal("NewCheckerRegistry returned nil")
	}

	if registry.checkers == nil {
		t.Error("checkers map should be initialized")
	}

	// Verify all expected checkers are registered
	expectedTypes := []core.CheckType{
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

	for _, checkType := range expectedTypes {
		checker, exists := registry.Get(checkType)
		if !exists {
			t.Errorf("Expected %s checker to be registered", checkType)
		}
		if checker == nil {
			t.Errorf("Expected %s checker to not be nil", checkType)
		}
	}
}

// TestCheckerRegistry_Register tests registering a custom checker
func TestCheckerRegistry_Register(t *testing.T) {
	registry := &CheckerRegistry{
		checkers: make(map[core.CheckType]Checker),
	}

	// Register a mock checker
	mockChecker := NewHTTPChecker()
	registry.Register(mockChecker)

	retrieved, exists := registry.Get(core.CheckHTTP)
	if !exists {
		t.Error("Expected HTTP checker to be registered")
	}
	if retrieved != mockChecker {
		t.Error("Expected retrieved checker to be the same as registered")
	}
}

// TestCheckerRegistry_List tests listing registered checkers
func TestCheckerRegistry_List(t *testing.T) {
	registry := NewCheckerRegistry()

	types := registry.List()

	if len(types) != 10 {
		t.Errorf("Expected 10 checkers, got %d", len(types))
	}
}

// TestGlobalRegistry tests the global registry functions
func TestGlobalRegistry(t *testing.T) {
	// Test GetChecker
	checker := GetChecker(core.CheckHTTP)
	if checker == nil {
		t.Error("Expected HTTP checker from global registry")
	}

	// Test RegisterChecker
	newChecker := NewTCPChecker()
	RegisterChecker(newChecker)

	// Should not panic
}

// TestHelperFunctions tests utility functions
func TestHelperFunctions(t *testing.T) {
	// Test truncateString
	result := truncateString("short", 10)
	if result != "short" {
		t.Errorf("Expected 'short', got '%s'", result)
	}

	longStr := "this is a very long string that should be truncated"
	result = truncateString(longStr, 10)
	if len(result) != 10+3 { // 10 chars + "..."
		t.Errorf("Expected truncated length 13, got %d", len(result))
	}
	if result[10:] != "..." {
		t.Errorf("Expected '...' suffix, got '%s'", result[10:])
	}

	// Test boolToString
	if boolToString(true, "yes", "no") != "yes" {
		t.Error("Expected 'yes' for true")
	}
	if boolToString(false, "yes", "no") != "no" {
		t.Error("Expected 'no' for false")
	}

	// Test parseDuration
	d := parseDuration("1s", 0)
	if d != 1000000000 { // 1 second in nanoseconds
		t.Errorf("Expected 1s, got %v", d)
	}

	// Test invalid duration returns default
	d = parseDuration("invalid", 5*time.Second)
	if d != 5*time.Second {
		t.Errorf("Expected default 5s, got %v", d)
	}
}

// TestFailJudgment tests failJudgment helper
func TestFailJudgment(t *testing.T) {
	soul := &core.Soul{
		ID:   "test-soul",
		Name: "Test Soul",
	}

	err := &core.ConfigError{Field: "test", Message: "test error"}
	judgment := failJudgment(soul, err)

	if judgment.SoulID != soul.ID {
		t.Errorf("Expected SoulID %s, got %s", soul.ID, judgment.SoulID)
	}

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}

	if judgment.Message != err.Error() {
		t.Errorf("Expected message '%s', got '%s'", err.Error(), judgment.Message)
	}
}

// TestSuccessJudgment tests successJudgment helper
func TestSuccessJudgment(t *testing.T) {
	soul := &core.Soul{
		ID:   "test-soul",
		Name: "Test Soul",
	}

	judgment := successJudgment(soul, 100*time.Millisecond, "OK")

	if judgment.SoulID != soul.ID {
		t.Errorf("Expected SoulID %s, got %s", soul.ID, judgment.SoulID)
	}

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}

	if judgment.Duration != 100*time.Millisecond {
		t.Errorf("Expected duration 100ms, got %v", judgment.Duration)
	}
}

// TestDegradedJudgment tests degradedJudgment helper
func TestDegradedJudgment(t *testing.T) {
	soul := &core.Soul{
		ID:   "test-soul",
		Name: "Test Soul",
	}

	judgment := degradedJudgment(soul, 500*time.Millisecond, "Slow response")

	if judgment.SoulID != soul.ID {
		t.Errorf("Expected SoulID %s, got %s", soul.ID, judgment.SoulID)
	}

	if judgment.Status != core.SoulDegraded {
		t.Errorf("Expected status Degraded, got %s", judgment.Status)
	}
}

// TestConfigErrorHelper tests configError helper
func TestConfigErrorHelper(t *testing.T) {
	err := configError("field1", "error message")

	configErr, ok := err.(*core.ConfigError)
	if !ok {
		t.Fatal("Expected *core.ConfigError")
	}

	if configErr.Field != "field1" {
		t.Errorf("Expected field 'field1', got '%s'", configErr.Field)
	}

	if configErr.Message != "error message" {
		t.Errorf("Expected message 'error message', got '%s'", configErr.Message)
	}
}

// TestValidationErrorHelper tests validationError helper
func TestValidationErrorHelper(t *testing.T) {
	err := validationError("field2", "validation failed")

	valErr, ok := err.(*core.ValidationError)
	if !ok {
		t.Fatal("Expected *core.ValidationError")
	}

	if valErr.Field != "field2" {
		t.Errorf("Expected field 'field2', got '%s'", valErr.Field)
	}
}
