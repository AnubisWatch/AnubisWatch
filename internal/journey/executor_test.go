package journey

import (
	"context"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
	"log/slog"
	"os"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
}

func newTestDB(t *testing.T) *storage.CobaltDB {
	dir := t.TempDir()
	cfg := core.StorageConfig{Path: dir}
	db, err := storage.NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	return db
}

func TestExecutor_NewExecutor(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	// Verify executor is created with initialized fields
	_ = executor.running
	if executor.db == nil {
		t.Error("Expected db to be set")
	}
}

func TestExecutor_StartStop(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	journey := &core.JourneyConfig{
		ID:          "test-journey",
		WorkspaceID: "default",
		Name:        "Test Journey",
		Enabled:     true,
		Weight:      core.Duration{Duration: 1 * time.Hour},
		Steps:       []core.JourneyStep{},
	}

	ctx := context.Background()

	// Start journey
	err := executor.Start(ctx, journey)
	if err != nil {
		t.Errorf("Start failed: %v", err)
	}

	// Verify journey is running
	if _, exists := executor.running[journey.ID]; !exists {
		t.Error("Expected journey to be in running map")
	}

	// Stop journey
	executor.Stop(journey.ID)

	// Verify journey is stopped
	if _, exists := executor.running[journey.ID]; exists {
		t.Error("Expected journey to be removed from running map")
	}
}

func TestExecutor_StartAlreadyRunning(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	journey := &core.JourneyConfig{
		ID:          "test-journey",
		WorkspaceID: "default",
		Name:        "Test Journey",
		Enabled:     true,
		Weight:      core.Duration{Duration: 1 * time.Hour},
	}

	ctx := context.Background()

	// Start journey first time
	err := executor.Start(ctx, journey)
	if err != nil {
		t.Errorf("First Start failed: %v", err)
	}

	// Try to start again
	err = executor.Start(ctx, journey)
	if err == nil {
		t.Error("Expected error when starting already running journey")
	}

	// Clean up
	executor.Stop(journey.ID)
}

func TestExecutor_StopAll(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	journeys := []*core.JourneyConfig{
		{ID: "journey-1", WorkspaceID: "default", Name: "Journey 1", Enabled: true, Weight: core.Duration{Duration: 1 * time.Hour}},
		{ID: "journey-2", WorkspaceID: "default", Name: "Journey 2", Enabled: true, Weight: core.Duration{Duration: 1 * time.Hour}},
		{ID: "journey-3", WorkspaceID: "default", Name: "Journey 3", Enabled: true, Weight: core.Duration{Duration: 1 * time.Hour}},
	}

	ctx := context.Background()

	// Start all journeys
	for _, j := range journeys {
		executor.Start(ctx, j)
	}

	// Stop all
	executor.StopAll()

	// Verify all are stopped
	if len(executor.running) != 0 {
		t.Errorf("Expected 0 running journeys, got %d", len(executor.running))
	}
}

func TestExecutor_StopNonExistent(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	// Should not panic
	executor.Stop("non-existent")

	// Should still have empty running map
	if len(executor.running) != 0 {
		t.Errorf("Expected 0 running journeys, got %d", len(executor.running))
	}
}

func TestInterpolateVariables(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	tests := []struct {
		name      string
		input     string
		variables map[string]string
		expected  string
	}{
		{
			name:      "single variable",
			input:     "https://api.example.com/${token}",
			variables: map[string]string{"token": "abc123"},
			expected:  "https://api.example.com/abc123",
		},
		{
			name:      "multiple variables",
			input:     "https://${host}/${endpoint}",
			variables: map[string]string{"host": "api.example.com", "endpoint": "users"},
			expected:  "https://api.example.com/users",
		},
		{
			name:      "no variables",
			input:     "https://example.com",
			variables: map[string]string{},
			expected:  "https://example.com",
		},
		{
			name:      "unknown variable",
			input:     "https://example.com/${unknown}",
			variables: map[string]string{"known": "value"},
			expected:  "https://example.com/${unknown}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executor.interpolateVariables(tt.input, tt.variables)
			if result != tt.expected {
				t.Errorf("interpolateVariables() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestExtractRegex(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	tests := []struct {
		name     string
		input    string
		pattern  string
		expected string
	}{
		{
			name:     "simple match",
			input:    "Hello World",
			pattern:  "Hello (\\w+)",
			expected: "World",
		},
		{
			name:     "no match",
			input:    "Hello World",
			pattern:  "Goodbye (\\w+)",
			expected: "",
		},
		{
			name:     "invalid regex",
			input:    "Hello World",
			pattern:  "[invalid",
			expected: "",
		},
		{
			name:     "full match no groups returns first group only",
			input:    "12345",
			pattern:  "\\d+",
			expected: "", // No capturing groups, so returns empty
		},
		{
			name:     "match with capturing group",
			input:    "Hello World",
			pattern:  "(Hello)",
			expected: "Hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executor.extractRegex(tt.input, tt.pattern)
			if result != tt.expected {
				t.Errorf("extractRegex() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestExtractFromBody(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	body := `{"name": "John", "age": 30, "city": "New York"}`

	tests := []struct {
		name     string
		body     string
		rule     core.ExtractionRule
		expected string
	}{
		{
			name: "json path extraction",
			body: body,
			rule: core.ExtractionRule{
				From: "body",
				Path: "$.name",
			},
			expected: "John",
		},
		{
			name: "regex extraction",
			body: body,
			rule: core.ExtractionRule{
				From:  "body",
				Regex: `"name":\s*"([^"]+)"`,
			},
			expected: "John",
		},
		{
			name: "no match",
			body: body,
			rule: core.ExtractionRule{
				From: "body",
				Path: "$.nonexistent",
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executor.extractFromBody(tt.body, tt.rule)
			if result != tt.expected {
				t.Errorf("extractFromBody() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestExtractFromHeader(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	headers := map[string]string{
		"Content-Type": "application/json",
		"X-Request-ID": "abc123",
		"X-API-Key":    "secret-key",
	}

	tests := []struct {
		name     string
		headers  map[string]string
		rule     core.ExtractionRule
		expected string
	}{
		{
			name:    "direct header",
			headers: headers,
			rule: core.ExtractionRule{
				From: "header",
				Path: "X-Request-ID",
			},
			expected: "abc123",
		},
		{
			name:    "header with regex",
			headers: headers,
			rule: core.ExtractionRule{
				From:  "header",
				Path:  "X-API-Key",
				Regex: "secret-(\\w+)",
			},
			expected: "key",
		},
		{
			name:    "missing header",
			headers: headers,
			rule: core.ExtractionRule{
				From: "header",
				Path: "X-Nonexistent",
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executor.extractFromHeader(tt.headers, tt.rule)
			if result != tt.expected {
				t.Errorf("extractFromHeader() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestExtractFromCookie(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	tests := []struct {
		name     string
		headers  map[string]string
		rule     core.ExtractionRule
		expected string
	}{
		{
			name: "extract cookie",
			headers: map[string]string{
				"Set-Cookie": "session=abc123; Path=/; HttpOnly",
			},
			rule: core.ExtractionRule{
				From: "cookie",
				Path: "session",
			},
			expected: "abc123",
		},
		{
			name: "missing cookie",
			headers: map[string]string{
				"Set-Cookie": "session=abc123; Path=/",
			},
			rule: core.ExtractionRule{
				From: "cookie",
				Path: "nonexistent",
			},
			expected: "",
		},
		{
			name:    "no set-cookie header",
			headers: map[string]string{},
			rule: core.ExtractionRule{
				From: "cookie",
				Path: "session",
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executor.extractFromCookie(tt.headers, tt.rule)
			if result != tt.expected {
				t.Errorf("extractFromCookie() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestGetChecker(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	// Test that we can get checkers for known types
	checkTypes := []core.CheckType{
		core.CheckHTTP,
		core.CheckTCP,
		core.CheckDNS,
		core.CheckTLS,
	}

	for _, ct := range checkTypes {
		checker := executor.getChecker(ct)
		if checker == nil {
			t.Errorf("getChecker(%s) returned nil", ct)
		}
	}

	// Unknown type should return nil
	checker := executor.getChecker("unknown-type")
	if checker != nil {
		t.Error("getChecker(unknown-type) should return nil")
	}
}

func TestListRuns(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())
	ctx := context.Background()

	// Should not panic - returns from storage
	runs, err := executor.ListRuns(ctx, "default", "journey-1", 10)
	if err != nil {
		t.Errorf("ListRuns failed: %v", err)
	}
	if runs == nil {
		t.Error("Expected runs slice, got nil")
	}
}

func TestGetRun_NotImplemented(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())
	ctx := context.Background()

	// Should return "not implemented" error
	_, err := executor.GetRun(ctx, "default", "run-1")
	if err == nil {
		t.Error("Expected error from GetRun")
	}
}

func TestExecutor_executeStep_Success(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	step := core.JourneyStep{
		Name:    "Test Step",
		Type:    core.CheckHTTP,
		Target:  "https://example.com",
		Timeout: core.Duration{Duration: 5 * time.Second},
		HTTP: &core.HTTPConfig{
			Method:      "GET",
			ValidStatus: []int{200},
		},
	}

	ctx := context.Background()
	result := executor.executeStep(ctx, step, map[string]string{}, 0)

	// Step should execute (may fail due to network, but function runs)
	if result.Name != "Test Step" {
		t.Errorf("Expected step name 'Test Step', got %s", result.Name)
	}
	if result.StepIndex != 0 {
		t.Errorf("Expected step index 0, got %d", result.StepIndex)
	}
}

func TestExecutor_executeStep_UnknownType(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	step := core.JourneyStep{
		Name:   "Unknown Step",
		Type:   "unknown-type",
		Target: "https://example.com",
	}

	ctx := context.Background()
	result := executor.executeStep(ctx, step, map[string]string{}, 0)

	if result.Status != core.SoulDead {
		t.Errorf("Expected status dead for unknown type, got %s", result.Status)
	}
	if result.Message == "" {
		t.Error("Expected error message for unknown type")
	}
}

func TestExecutor_executeStep_ValidationFailed(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	step := core.JourneyStep{
		Name:   "Invalid Step",
		Type:   core.CheckHTTP,
		Target: "", // Empty target should fail validation
		HTTP:   &core.HTTPConfig{},
	}

	ctx := context.Background()
	result := executor.executeStep(ctx, step, map[string]string{}, 0)

	if result.Status != core.SoulDead {
		t.Errorf("Expected status dead for validation failure, got %s", result.Status)
	}
	if result.Message == "" {
		t.Error("Expected error message for validation failure")
	}
}

func TestExecutor_executeStep_WithVariableInterpolation(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	step := core.JourneyStep{
		Name:    "Variable Step",
		Type:    core.CheckHTTP,
		Target:  "https://${host}/${path}",
		Timeout: core.Duration{Duration: 5 * time.Second},
		HTTP: &core.HTTPConfig{
			Method:      "GET",
			ValidStatus: []int{200},
		},
	}

	variables := map[string]string{
		"host": "example.com",
		"path": "api",
	}

	ctx := context.Background()
	result := executor.executeStep(ctx, step, variables, 0)

	// Step should execute with interpolated variables
	if result.Name != "Variable Step" {
		t.Errorf("Expected step name 'Variable Step', got %s", result.Name)
	}
}

func TestExecutor_extractVariables(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	judgment := &core.Judgment{
		Status:  core.SoulAlive,
		Message: "OK",
		Details: &core.JudgmentDetails{
			ResponseBody:    `{"token": "abc123", "user": {"name": "John"}}`,
			ResponseHeaders: map[string]string{"X-Request-ID": "req-456"},
		},
	}

	rules := map[string]core.ExtractionRule{
		"token": {
			From: "body",
			Path: "$.token",
		},
		"requestId": {
			From: "header",
			Path: "X-Request-ID",
		},
	}

	extracted := executor.extractVariables(judgment, rules)

	if extracted["token"] != "abc123" {
		t.Errorf("Expected token 'abc123', got %s", extracted["token"])
	}
	if extracted["requestId"] != "req-456" {
		t.Errorf("Expected requestId 'req-456', got %s", extracted["requestId"])
	}
}

func TestExecutor_extractVariables_EmptyRules(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	judgment := &core.Judgment{
		Status:  core.SoulAlive,
		Details: &core.JudgmentDetails{},
	}

	extracted := executor.extractVariables(judgment, map[string]core.ExtractionRule{})

	if len(extracted) != 0 {
		t.Errorf("Expected 0 extracted variables, got %d", len(extracted))
	}
}

func TestExecutor_extractVariables_CookieExtraction(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	judgment := &core.Judgment{
		Status: core.SoulAlive,
		Details: &core.JudgmentDetails{
			ResponseHeaders: map[string]string{
				"Set-Cookie": "session=xyz789; Path=/; HttpOnly",
			},
		},
	}

	rules := map[string]core.ExtractionRule{
		"session": {
			From: "cookie",
			Path: "session",
		},
	}

	extracted := executor.extractVariables(judgment, rules)

	if extracted["session"] != "xyz789" {
		t.Errorf("Expected session 'xyz789', got %s", extracted["session"])
	}
}
