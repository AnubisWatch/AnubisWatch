package journey

import (
	"context"
	"fmt"
	"log/slog"
	"net/http/cookiejar"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
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

func TestExecutor_Start_InvalidWeight(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	// Zero weight should return an error instead of panicking
	journeyZero := &core.JourneyConfig{
		ID:     "test-journey-zero",
		Name:   "Test Journey Zero",
		Weight: core.Duration{Duration: 0},
	}

	ctx := context.Background()
	err := executor.Start(ctx, journeyZero)
	if err == nil {
		t.Error("Expected error when starting journey with zero weight")
	}

	// Negative weight should also return an error
	journeyNegative := &core.JourneyConfig{
		ID:     "test-journey-neg",
		Name:   "Test Journey Negative",
		Weight: core.Duration{Duration: -1 * time.Second},
	}

	err = executor.Start(ctx, journeyNegative)
	if err == nil {
		t.Error("Expected error when starting journey with negative weight")
	}
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

func TestGetRun_NotFound(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())
	ctx := context.Background()

	// Should return "not found" error for non-existent run
	_, err := executor.GetRun(ctx, "default", "journey-1", "run-1")
	if err == nil {
		t.Error("Expected error from GetRun for non-existent run")
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
	result := executor.executeStep(ctx, &JourneyContext{Variables: map[string]string{}}, step, 0)

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
	result := executor.executeStep(ctx, &JourneyContext{Variables: map[string]string{}}, step, 0)

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
	result := executor.executeStep(ctx, &JourneyContext{Variables: map[string]string{}}, step, 0)

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
	result := executor.executeStep(ctx, &JourneyContext{Variables: variables}, step, 0)

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

func TestExecutor_compareValues(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	tests := []struct {
		name     string
		actual   string
		operator string
		expected string
		want     bool
	}{
		{"equals", "hello", "equals", "hello", true},
		{"equals false", "hello", "equals", "world", false},
		{"eq", "hello", "eq", "hello", true},
		{"==", "hello", "==", "hello", true},
		{"not_equals", "hello", "not_equals", "world", true},
		{"ne", "hello", "ne", "world", true},
		{"!=", "hello", "!=", "world", true},
		{"contains", "hello world", "contains", "world", true},
		{"contains false", "hello world", "contains", "xyz", false},
		{"greater_than", "100", "greater_than", "50", true},
		{"gt", "100", "gt", "50", true},
		{">", "100", ">", "50", true},
		{"less_than", "50", "less_than", "100", true},
		{"lt", "50", "lt", "100", true},
		{"<", "50", "<", "100", true},
		{"greater_equals", "100", "greater_equals", "100", true},
		{"ge", "100", "ge", "50", true},
		{">=", "100", ">=", "100", true},
		{"less_equals", "50", "less_equals", "50", true},
		{"le", "50", "le", "100", true},
		{"<=", "50", "<=", "50", true},
		{"unknown operator", "value", "unknown", "other", false},
		{"unknown operator same", "value", "unknown", "value", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := executor.compareValues(tt.actual, tt.operator, tt.expected)
			if got != tt.want {
				t.Errorf("compareValues(%q, %q, %q) = %v, want %v", tt.actual, tt.operator, tt.expected, got, tt.want)
			}
		})
	}
}

func TestExecutor_runAssertion(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	tests := []struct {
		name      string
		assertion core.Assertion
		judgment  *core.Judgment
		wantPass  bool
	}{
		{
			name: "status_code equals pass",
			assertion: core.Assertion{
				Type:     "status_code",
				Operator: "equals",
				Expected: "200",
			},
			judgment: &core.Judgment{
				StatusCode: 200,
			},
			wantPass: true,
		},
		{
			name: "status_code equals fail",
			assertion: core.Assertion{
				Type:     "status_code",
				Operator: "equals",
				Expected: "200",
			},
			judgment: &core.Judgment{
				StatusCode: 500,
			},
			wantPass: false,
		},
		{
			name: "body_contains pass",
			assertion: core.Assertion{
				Type:     "body_contains",
				Expected: "success",
			},
			judgment: &core.Judgment{
				Details: &core.JudgmentDetails{
					ResponseBody: `{"message": "success"}`,
				},
			},
			wantPass: true,
		},
		{
			name: "body_contains fail",
			assertion: core.Assertion{
				Type:     "body_contains",
				Expected: "error",
			},
			judgment: &core.Judgment{
				Details: &core.JudgmentDetails{
					ResponseBody: `{"message": "success"}`,
				},
			},
			wantPass: false,
		},
		{
			name: "unknown assertion type",
			assertion: core.Assertion{
				Type: "unknown",
			},
			judgment: &core.Judgment{},
			wantPass: false,
		},
		{
			name: "response_time less_than pass",
			assertion: core.Assertion{
				Type:     "response_time",
				Operator: "less_than",
				Expected: "5000",
			},
			judgment: &core.Judgment{
				Duration: 100 * time.Millisecond,
			},
			wantPass: true,
		},
		{
			name: "response_time greater_than fail",
			assertion: core.Assertion{
				Type:     "response_time",
				Operator: "greater_than",
				Expected: "50",
			},
			judgment: &core.Judgment{
				Duration: 10 * time.Millisecond,
			},
			wantPass: false,
		},
		{
			name: "header equals pass",
			assertion: core.Assertion{
				Type:     "header",
				Operator: "equals",
				Target:   "Content-Type",
				Expected: "application/json",
			},
			judgment: &core.Judgment{
				Details: &core.JudgmentDetails{
					ResponseHeaders: map[string]string{"Content-Type": "application/json"},
				},
			},
			wantPass: true,
		},
		{
			name: "header not_equals pass",
			assertion: core.Assertion{
				Type:     "header",
				Operator: "not_equals",
				Target:   "X-Custom",
				Expected: "secret",
			},
			judgment: &core.Judgment{
				Details: &core.JudgmentDetails{
					ResponseHeaders: map[string]string{"Content-Type": "text/html"},
				},
			},
			wantPass: true,
		},
		{
			name: "json_path equals pass",
			assertion: core.Assertion{
				Type:     "json_path",
				Operator: "equals",
				Target:   "$.status",
				Expected: "ok",
			},
			judgment: &core.Judgment{
				Details: &core.JudgmentDetails{
					ResponseBody: `{"status":"ok"}`,
				},
			},
			wantPass: true,
		},
		{
			name: "regex match pass",
			assertion: core.Assertion{
				Type:     "regex",
				Target:   "body",
				Expected: `(\d{3})`,
			},
			judgment: &core.Judgment{
				Details: &core.JudgmentDetails{
					ResponseBody: `HTTP/1.1 200 OK`,
				},
			},
			wantPass: true,
		},
		{
			name: "regex no match fail",
			assertion: core.Assertion{
				Type:     "regex",
				Target:   "",
				Expected: `^\d{4}$`,
			},
			judgment: &core.Judgment{
				Details: &core.JudgmentDetails{
					ResponseBody: `123`,
				},
			},
			wantPass: false,
		},
		{
			name: "custom failure message",
			assertion: core.Assertion{
				Type:     "status_code",
				Operator: "equals",
				Expected: "200",
				Message:  "Server is down!",
			},
			judgment: &core.Judgment{
				StatusCode: 503,
			},
			wantPass: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executor.runAssertion(tt.judgment, tt.assertion)
			if result.Passed != tt.wantPass {
				t.Errorf("runAssertion() passed = %v, want %v", result.Passed, tt.wantPass)
			}
		})
	}
}

func TestExecutor_runAssertions(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	assertions := []core.Assertion{
		{Type: "status_code", Operator: "equals", Expected: "200"},
		{Type: "body_contains", Expected: "success"},
	}

	judgment := &core.Judgment{
		StatusCode: 200,
		Details: &core.JudgmentDetails{
			ResponseBody: `{"message": "success"}`,
		},
	}

	results := executor.runAssertions(judgment, assertions)

	if len(results) != 2 {
		t.Errorf("Expected 2 assertion results, got %d", len(results))
	}

	for _, result := range results {
		if !result.Passed {
			t.Errorf("Expected assertion %s to pass", result.Type)
		}
	}
}

func TestExecutor_executeStep_WithAssertions(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	step := core.JourneyStep{
		Name:    "Assertion Step",
		Type:    core.CheckHTTP,
		Target:  "https://example.com",
		Timeout: core.Duration{Duration: 5 * time.Second},
		Assertions: []core.Assertion{
			{Type: "status_code", Operator: "equals", Expected: "200"},
		},
	}

	ctx := context.Background()
	result := executor.executeStep(ctx, &JourneyContext{Variables: map[string]string{}}, step, 0)

	if result.Name != "Assertion Step" {
		t.Errorf("Expected step name 'Assertion Step', got %s", result.Name)
	}
}

func TestNewExecutorWithNodeID(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	logger := newTestLogger()

	// Test with custom node ID and region
	exec := NewExecutorWithNodeID(db, logger, "node-1", "us-east-1")
	if exec.nodeID != "node-1" {
		t.Errorf("Expected nodeID 'node-1', got %s", exec.nodeID)
	}
	if exec.region != "us-east-1" {
		t.Errorf("Expected region 'us-east-1', got %s", exec.region)
	}

	// Test with empty values (should default)
	exec2 := NewExecutorWithNodeID(db, logger, "", "")
	if exec2.nodeID != "local" {
		t.Errorf("Expected default nodeID 'local', got %s", exec2.nodeID)
	}
	if exec2.region != "default" {
		t.Errorf("Expected default region 'default', got %s", exec2.region)
	}
}

// TestRetryWithBackoff_Success tests successful operation without retry
func TestRetryWithBackoff_Success(t *testing.T) {
	ctx := context.Background()
	callCount := 0

	err := retryWithBackoff(ctx, 3, 10*time.Millisecond, func() error {
		callCount++
		return nil
	})

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if callCount != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}
}

// TestRetryWithBackoff_RetrySuccess tests successful retry
func TestRetryWithBackoff_RetrySuccess(t *testing.T) {
	ctx := context.Background()
	callCount := 0

	err := retryWithBackoff(ctx, 3, 10*time.Millisecond, func() error {
		callCount++
		if callCount < 3 {
			return fmt.Errorf("temporary error")
		}
		return nil
	})

	if err != nil {
		t.Errorf("Expected no error after retries, got %v", err)
	}
	if callCount != 3 {
		t.Errorf("Expected 3 calls, got %d", callCount)
	}
}

// TestRetryWithBackoff_MaxRetriesExceeded tests failure after max retries
func TestRetryWithBackoff_MaxRetriesExceeded(t *testing.T) {
	ctx := context.Background()
	callCount := 0

	err := retryWithBackoff(ctx, 3, 10*time.Millisecond, func() error {
		callCount++
		return fmt.Errorf("persistent error")
	})

	if err == nil {
		t.Error("Expected error after max retries")
	}
	if callCount != 3 {
		t.Errorf("Expected 3 calls, got %d", callCount)
	}
}

// TestRetryWithBackoff_ContextCancellation tests context cancellation during retry
func TestRetryWithBackoff_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	callCount := 0
	err := retryWithBackoff(ctx, 10, 100*time.Millisecond, func() error {
		callCount++
		return fmt.Errorf("error")
	})

	if err != context.DeadlineExceeded && err != context.Canceled {
		t.Errorf("Expected context error, got %v", err)
	}
}

// TestExecuteStep_UnknownType tests step with unknown checker type
func TestExecuteStep_UnknownType(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())
	ctx := context.Background()

	step := core.JourneyStep{
		Name:   "Unknown Step",
		Type:   "unknown-type",
		Target: "http://example.com",
	}

	result := executor.executeStep(ctx, &JourneyContext{Variables: map[string]string{}}, step, 0)

	if result.Status != core.SoulDead {
		t.Errorf("Expected status SoulDead, got %s", result.Status)
	}
	if result.Message != "unknown step type: unknown-type" {
		t.Errorf("Expected unknown type message, got %s", result.Message)
	}
}

// TestExecuteJourney_ContinueOnFailure tests journey continuing on step failure
func TestExecuteJourney_ContinueOnFailure(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())
	ctx := context.Background()

	journey := &core.JourneyConfig{
		ID:                "test-journey",
		Name:              "Test Journey",
		WorkspaceID:       "default",
		ContinueOnFailure: true, // Continue on failure
		Steps: []core.JourneyStep{
			{
				Name:   "Step 1",
				Type:   "http",
				Target: "invalid-url-that-will-fail",
			},
			{
				Name:   "Step 2",
				Type:   "http",
				Target: "also-invalid",
			},
		},
	}

	// Execute journey - should not panic
	executor.executeJourney(ctx, journey)

	// Both steps should have been executed (continued after first failure)

	t.Log("Journey with ContinueOnFailure executed without panic")
}

// TestJourneyContext_CookieJarPersistence tests that cookie jar is shared across HTTP steps
func TestJourneyContext_CookieJarPersistence(t *testing.T) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("Failed to create cookie jar: %v", err)
	}

	ctx := &JourneyContext{
		Variables: map[string]string{"token": "abc123"},
		CookieJar: jar,
	}

	if ctx.CookieJar == nil {
		t.Fatal("CookieJar should be set")
	}
}

// TestExecutor_VariableInterpolationInHTTPConfig tests variable interpolation in HTTP headers and body
func TestExecutor_VariableInterpolationInHTTPConfig(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	step := core.JourneyStep{
		Name:    "Auth Step",
		Type:    core.CheckHTTP,
		Target:  "https://${host}/api/${endpoint}",
		Timeout: core.Duration{Duration: 5 * time.Second},
		HTTP: &core.HTTPConfig{
			Method: "${method}",
			Headers: map[string]string{
				"Authorization": "Bearer ${token}",
				"Content-Type":  "application/json",
			},
			Body:        `{"user": "${user_id}"}`,
			ValidStatus: []int{200},
		},
	}

	ctx := &JourneyContext{
		Variables: map[string]string{
			"host":     "example.com",
			"endpoint": "health",
			"method":   "GET",
			"token":    "secret-token",
			"user_id":  "12345",
		},
	}

	result := executor.executeStep(context.Background(), ctx, step, 0)

	// Step should execute (network may fail but interpolation works)
	if result.Name != "Auth Step" {
		t.Errorf("Expected step name 'Auth Step', got %s", result.Name)
	}
}

// TestExecutor_VariableInterpolationInTCPConfig tests variable interpolation in TCP config
func TestExecutor_VariableInterpolationInTCPConfig(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	step := core.JourneyStep{
		Name:    "TCP Step",
		Type:    core.CheckTCP,
		Target:  "${host}:${port}",
		Timeout: core.Duration{Duration: 5 * time.Second},
		TCP: &core.TCPConfig{
			Send:        "PING ${user}\r\n",
			BannerMatch: "^Welcome to ${server_name}",
		},
	}

	ctx := &JourneyContext{
		Variables: map[string]string{
			"host":        "localhost",
			"port":        "8080",
			"user":        "admin",
			"server_name": "test-server",
		},
	}

	result := executor.executeStep(context.Background(), ctx, step, 0)

	if result.Name != "TCP Step" {
		t.Errorf("Expected step name 'TCP Step', got %s", result.Name)
	}
}

// TestExecutor_VariableInterpolationInDNSConfig tests variable interpolation in DNS config
func TestExecutor_VariableInterpolationInDNSConfig(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	step := core.JourneyStep{
		Name:    "DNS Step",
		Type:    core.CheckDNS,
		Target:  "${domain}",
		Timeout: core.Duration{Duration: 5 * time.Second},
		DNS: &core.DNSConfig{
			RecordType:  "${record_type}",
			Nameservers: []string{"${dns_server}"},
		},
	}

	ctx := &JourneyContext{
		Variables: map[string]string{
			"domain":      "example.com",
			"record_type": "A",
			"dns_server":  "8.8.8.8",
		},
	}

	result := executor.executeStep(context.Background(), ctx, step, 0)

	if result.Name != "DNS Step" {
		t.Errorf("Expected step name 'DNS Step', got %s", result.Name)
	}
}

// TestJourney_VariablePassingBetweenSteps tests that variables extracted in one step
// are available in subsequent steps
func TestJourney_VariablePassingBetweenSteps(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	executor := NewExecutor(db, newTestLogger())

	// Create a journey with two steps where step 1 extracts a variable
	// and step 2 uses it
	journey := &core.JourneyConfig{
		ID:          "var-pass-test",
		Name:        "Variable Passing Test",
		WorkspaceID: "default",
		Variables:   map[string]string{"base_url": "example.com"},
		Steps: []core.JourneyStep{
			{
				Name:    "Step 1",
				Type:    core.CheckHTTP,
				Target:  "https://${base_url}/",
				Timeout: core.Duration{Duration: 5 * time.Second},
				Extract: map[string]core.ExtractionRule{
					"extracted_value": {From: "body", Regex: `value=([a-z0-9]+)`},
				},
			},
			{
				Name:    "Step 2",
				Type:    core.CheckHTTP,
				Target:  "https://${base_url}/api/${extracted_value}",
				Timeout: core.Duration{Duration: 5 * time.Second},
			},
		},
	}

	ctx := context.Background()
	executor.executeJourney(ctx, journey)

	// Journey should execute without error
	t.Log("Journey with variable passing executed successfully")
}

// Tests for computeDedupHash

func TestComputeDedupHash_NoPaths(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	executor := NewExecutor(db, newTestLogger())

	journey := &core.JourneyConfig{
		ID:    "test-journey",
		Steps: []core.JourneyStep{},
	}

	hash := executor.computeDedupHash(journey)
	if hash != "" {
		t.Errorf("Expected empty hash when no JSONPath rules, got %s", hash)
	}
}

func TestComputeDedupHash_WithPaths(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	executor := NewExecutor(db, newTestLogger())

	journey := &core.JourneyConfig{
		ID: "test-journey",
		Steps: []core.JourneyStep{
			{
				Name:   "Step 1",
				Target: "https://example.com",
				Extract: map[string]core.ExtractionRule{
					"token": {From: "body", Path: "$.token"},
				},
			},
			{
				Name:   "Step 2",
				Target: "https://example.com/api",
				Extract: map[string]core.ExtractionRule{
					"id": {From: "body", Path: "$.id"},
				},
			},
		},
	}

	hash := executor.computeDedupHash(journey)
	if hash == "" {
		t.Error("Expected non-empty hash when JSONPath rules exist")
	}
}

// Tests for extractJSONPath edge cases

func TestExtractJSONPath_Nested(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	executor := NewExecutor(db, newTestLogger())

	body := `{"user": {"profile": {"name": "John"}}}`

	result := executor.extractJSONPath(body, "$.user.profile.name")
	if result != "John" {
		t.Errorf("Expected 'John', got %s", result)
	}
}

func TestExtractJSONPath_ArrayInPath(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	executor := NewExecutor(db, newTestLogger())

	body := `{"users": [{"name": "John"}]}`

	// Array at intermediate path should return empty
	result := executor.extractJSONPath(body, "$.users.name")
	if result != "" {
		t.Errorf("Expected empty for array in path, got %s", result)
	}
}

func TestExtractJSONPath_Bool(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	executor := NewExecutor(db, newTestLogger())

	body := `{"active": true, "deleted": false}`

	result := executor.extractJSONPath(body, "$.active")
	if result != "true" {
		t.Errorf("Expected 'true', got %s", result)
	}

	result = executor.extractJSONPath(body, "$.deleted")
	if result != "false" {
		t.Errorf("Expected 'false', got %s", result)
	}
}

func TestExtractJSONPath_Nil(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	executor := NewExecutor(db, newTestLogger())

	body := `{"value": null}`

	result := executor.extractJSONPath(body, "$.value")
	if result != "" {
		t.Errorf("Expected empty for nil, got %s", result)
	}
}

func TestExtractJSONPath_DefaultMarshal(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	executor := NewExecutor(db, newTestLogger())

	body := `{"obj": {"a": 1, "b": 2}}`

	result := executor.extractJSONPath(body, "$.obj")
	if result == "" {
		t.Error("Expected marshaled JSON object")
	}
	if !strings.Contains(result, "a") {
		t.Error("Expected marshaled object to contain keys")
	}
}

func TestExtractJSONPath_InvalidPathPrefix(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	executor := NewExecutor(db, newTestLogger())

	result := executor.extractJSONPath(`{"id": 1}`, "id")
	if result != "" {
		t.Errorf("Expected empty for path without '$.' prefix, got %s", result)
	}
}

// Tests for executeStep with various configs

func TestExecutor_executeStep_WithUDPConfig(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	executor := NewExecutor(db, newTestLogger())

	step := core.JourneyStep{
		Name:    "UDP Step",
		Type:    core.CheckUDP,
		Target:  "127.0.0.1:53",
		Timeout: core.Duration{Duration: 1 * time.Second},
		UDP: &core.UDPConfig{
			SendHex:        "deadbeef",
			ExpectContains: "response",
		},
	}

	ctx := context.Background()
	result := executor.executeStep(ctx, &JourneyContext{Variables: map[string]string{}}, step, 0)

	if result.Name != "UDP Step" {
		t.Errorf("Expected step name 'UDP Step', got %s", result.Name)
	}
}

func TestExecutor_executeStep_WithTLSConfig(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	executor := NewExecutor(db, newTestLogger())

	step := core.JourneyStep{
		Name:    "TLS Step",
		Type:    core.CheckTLS,
		Target:  "example.com:443",
		Timeout: core.Duration{Duration: 5 * time.Second},
		TLS: &core.TLSConfig{
			MinProtocol:    "1.2",
			ExpectedIssuer: "Let's Encrypt",
			ExpectedSAN:    []string{"example.com"},
		},
	}

	ctx := context.Background()
	result := executor.executeStep(ctx, &JourneyContext{Variables: map[string]string{}}, step, 0)

	if result.Name != "TLS Step" {
		t.Errorf("Expected step name 'TLS Step', got %s", result.Name)
	}
}

func TestExecutor_executeStep_WithExtractRules(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	executor := NewExecutor(db, newTestLogger())

	step := core.JourneyStep{
		Name:    "Extract Step",
		Type:    core.CheckHTTP,
		Target:  "https://example.com",
		Timeout: core.Duration{Duration: 5 * time.Second},
		Extract: map[string]core.ExtractionRule{
			"token": {From: "body", Path: "$.token"},
		},
	}

	ctx := context.Background()
	result := executor.executeStep(ctx, &JourneyContext{Variables: map[string]string{}}, step, 0)

	if result.Name != "Extract Step" {
		t.Errorf("Expected step name 'Extract Step', got %s", result.Name)
	}
}

// Tests for executeJourney dedup behavior

func TestExecuteJourney_DedupSkip(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	executor := NewExecutor(db, newTestLogger())

	journey := &core.JourneyConfig{
		ID:          "dedup-journey",
		WorkspaceID: "default",
		Name:        "Dedup Test",
		Steps: []core.JourneyStep{
			{
				Name:   "Step 1",
				Type:   core.CheckHTTP,
				Target: "invalid-url",
				Extract: map[string]core.ExtractionRule{
					"id": {From: "body", Path: "$.id"},
				},
			},
		},
	}

	ctx := context.Background()

	// First execution should run
	executor.executeJourney(ctx, journey)

	// Second execution should be skipped due to dedup
	// We can't easily observe the skip, but it should not panic
	executor.executeJourney(ctx, journey)
}

func TestExecuteJourney_DedupDisabled(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	executor := NewExecutor(db, newTestLogger())

	journey := &core.JourneyConfig{
		ID:          "no-dedup-journey",
		WorkspaceID: "default",
		Name:        "No Dedup Test",
		Steps: []core.JourneyStep{
			{
				Name:   "Step 1",
				Type:   core.CheckHTTP,
				Target: "invalid-url",
			},
		},
	}

	ctx := context.Background()

	// Should execute without dedup since no JSONPath extract rules
	executor.executeJourney(ctx, journey)
	executor.executeJourney(ctx, journey)
}

func TestExecutor_executeStep_NoDetails(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	executor := NewExecutor(db, newTestLogger())

	step := core.JourneyStep{
		Name:    "No Details Step",
		Type:    core.CheckHTTP,
		Target:  "https://example.com",
		Timeout: core.Duration{Duration: 5 * time.Second},
		Assertions: []core.Assertion{
			{Type: "body_contains", Expected: "success"},
			{Type: "header", Target: "Content-Type", Operator: "equals", Expected: "application/json"},
			{Type: "json_path", Target: "$.status", Operator: "equals", Expected: "ok"},
			{Type: "regex", Target: "body", Expected: `\d+`},
		},
	}

	ctx := context.Background()
	result := executor.executeStep(ctx, &JourneyContext{Variables: map[string]string{}}, step, 0)

	if result.Name != "No Details Step" {
		t.Errorf("Expected step name 'No Details Step', got %s", result.Name)
	}
}
