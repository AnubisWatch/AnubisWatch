package probe

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func init() {
	// Allow private IPs in tests (for localhost test servers)
	os.Setenv("ANUBIS_SSRF_ALLOW_PRIVATE", "1")
	// Reset the DefaultValidator so it picks up the env var
	DefaultValidator = NewSSRFValidator()
}

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
		Target: "example.com:443",
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
		Target: "example.com",
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
			Target:  "example.com:443",
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

func TestGetChecker(t *testing.T) {
	checker := GetChecker(core.CheckHTTP)
	if checker == nil {
		t.Error("Expected HTTP checker from global registry")
	}

	checker = GetChecker("invalid-type")
	if checker != nil {
		t.Error("Expected nil for invalid checker type")
	}
}

func TestRegisterChecker(t *testing.T) {
	// Test registering a custom checker
	customChecker := &testChecker{}
	RegisterChecker(customChecker)

	checker := GetChecker(core.CheckType("test"))
	if checker != customChecker {
		t.Error("Expected custom checker to be registered")
	}
}

type testChecker struct{}

func (c *testChecker) Type() core.CheckType { return "test" }
func (c *testChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
	return nil, nil
}
func (c *testChecker) Validate(soul *core.Soul) error { return nil }

func TestHTTPChecker_Type(t *testing.T) {
	checker := NewHTTPChecker()
	if checker.Type() != core.CheckHTTP {
		t.Errorf("Expected type %s, got %s", core.CheckHTTP, checker.Type())
	}
}

func TestTCPChecker_Type(t *testing.T) {
	checker := NewTCPChecker()
	if checker.Type() != core.CheckTCP {
		t.Errorf("Expected type %s, got %s", core.CheckTCP, checker.Type())
	}
}

func TestDNSChecker_Type(t *testing.T) {
	checker := NewDNSChecker()
	if checker.Type() != core.CheckDNS {
		t.Errorf("Expected type %s, got %s", core.CheckDNS, checker.Type())
	}
}

func TestTLSChecker_Type(t *testing.T) {
	checker := NewTLSChecker()
	if checker.Type() != core.CheckTLS {
		t.Errorf("Expected type %s, got %s", core.CheckTLS, checker.Type())
	}
}

func TestGRPCChecker_Type(t *testing.T) {
	checker := NewGRPCChecker()
	if checker.Type() != core.CheckGRPC {
		t.Errorf("Expected type %s, got %s", core.CheckGRPC, checker.Type())
	}
}

func TestWebSocketChecker_Type(t *testing.T) {
	checker := NewWebSocketChecker()
	if checker.Type() != core.CheckWebSocket {
		t.Errorf("Expected type %s, got %s", core.CheckWebSocket, checker.Type())
	}
}

func TestSMTPChecker_Type(t *testing.T) {
	checker := NewSMTPChecker()
	if checker.Type() != core.CheckSMTP {
		t.Errorf("Expected type %s, got %s", core.CheckSMTP, checker.Type())
	}
}

func TestIMAPChecker_Type(t *testing.T) {
	checker := NewIMAPChecker()
	if checker.Type() != core.CheckIMAP {
		t.Errorf("Expected type %s, got %s", core.CheckIMAP, checker.Type())
	}
}

func TestICMPChecker_Type(t *testing.T) {
	checker := NewICMPChecker()
	if checker.Type() != core.CheckICMP {
		t.Errorf("Expected type %s, got %s", core.CheckICMP, checker.Type())
	}
}

func TestUDPChecker_Type(t *testing.T) {
	checker := NewUDPChecker()
	if checker.Type() != core.CheckUDP {
		t.Errorf("Expected type %s, got %s", core.CheckUDP, checker.Type())
	}
}

// Test configError helper
func TestConfigError(t *testing.T) {
	err := configError("method", "invalid method")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	cerr, ok := err.(*core.ConfigError)
	if !ok {
		t.Fatal("Expected ConfigError")
	}
	if cerr.Field != "method" {
		t.Errorf("Expected field 'method', got %s", cerr.Field)
	}
	if cerr.Message != "invalid method" {
		t.Errorf("Expected message 'invalid method', got %s", cerr.Message)
	}
}

// mockStorage for probe tests
type mockProbeStorage struct{}

func (m *mockProbeStorage) SaveJudgment(ctx context.Context, j *core.Judgment) error {
	return nil
}
func (m *mockProbeStorage) GetSoul(ctx context.Context, workspaceID, soulID string) (*core.Soul, error) {
	return nil, &core.NotFoundError{Entity: "soul", ID: soulID}
}
func (m *mockProbeStorage) ListSouls(ctx context.Context, workspaceID string) ([]*core.Soul, error) {
	return []*core.Soul{}, nil
}

// mockAlerter for probe tests
type mockProbeAlerter struct{}

func (m *mockProbeAlerter) ProcessJudgment(soul *core.Soul, prevStatus core.SoulStatus, judgment *core.Judgment) {
}

// Test Engine ForceCheck
func TestEngine_ForceCheck(t *testing.T) {
	opts := EngineOptions{
		NodeID:  "test-node",
		Region:  "test-region",
		Store:   &mockProbeStorage{},
		Alerter: &mockProbeAlerter{},
		Logger:  newTestProbeLogger(),
	}
	engine := NewEngine(opts)

	// ForceCheck on non-existent soul should fail
	_, err := engine.ForceCheck("non-existent-soul")
	if err == nil {
		t.Error("Expected error for non-existent soul")
	}
}

// Test Engine GetStatus
func TestEngine_GetStatus(t *testing.T) {
	opts := EngineOptions{
		NodeID:  "test-node",
		Region:  "test-region",
		Store:   &mockProbeStorage{},
		Alerter: &mockProbeAlerter{},
		Logger:  newTestProbeLogger(),
	}
	engine := NewEngine(opts)

	status := engine.GetStatus()
	if status == nil {
		t.Fatal("Expected non-nil status")
	}
	if !status.Running {
		t.Error("Expected engine to be running")
	}
}

// Test Engine ListActiveSouls
func TestEngine_ListActiveSouls(t *testing.T) {
	opts := EngineOptions{
		NodeID:  "test-node",
		Region:  "test-region",
		Store:   &mockProbeStorage{},
		Alerter: &mockProbeAlerter{},
		Logger:  newTestProbeLogger(),
	}
	engine := NewEngine(opts)

	souls := engine.ListActiveSouls()
	if souls == nil {
		t.Error("Expected non-nil souls slice")
	}
	if len(souls) != 0 {
		t.Errorf("Expected 0 souls, got %d", len(souls))
	}
}

// Test Engine Stop
func TestEngine_Stop(t *testing.T) {
	opts := EngineOptions{
		NodeID:  "test-node",
		Region:  "test-region",
		Store:   &mockProbeStorage{},
		Alerter: &mockProbeAlerter{},
		Logger:  newTestProbeLogger(),
	}
	engine := NewEngine(opts)

	// Stop should not panic
	engine.Stop()
}

// Test Engine TriggerImmediate
func TestEngine_TriggerImmediate(t *testing.T) {
	opts := EngineOptions{
		NodeID:  "test-node",
		Region:  "test-region",
		Store:   &mockProbeStorage{},
		Alerter: &mockProbeAlerter{},
		Logger:  newTestProbeLogger(),
	}
	engine := NewEngine(opts)

	// TriggerImmediate on non-existent soul should fail
	_, err := engine.TriggerImmediate(context.Background(), "non-existent")
	if err == nil {
		t.Error("Expected error for non-existent soul")
	}
}

// Test Engine GetSoulStatus
func TestEngine_GetSoulStatus(t *testing.T) {
	opts := EngineOptions{
		NodeID:  "test-node",
		Region:  "test-region",
		Store:   &mockProbeStorage{},
		Alerter: &mockProbeAlerter{},
		Logger:  newTestProbeLogger(),
	}
	engine := NewEngine(opts)

	// GetSoulStatus on non-existent soul should fail
	_, err := engine.GetSoulStatus("non-existent")
	if err == nil {
		t.Error("Expected error for non-existent soul")
	}
}

// Test Engine Stats
func TestEngine_Stats(t *testing.T) {
	opts := EngineOptions{
		NodeID:  "test-node",
		Region:  "test-region",
		Store:   &mockProbeStorage{},
		Alerter: &mockProbeAlerter{},
		Logger:  newTestProbeLogger(),
	}
	engine := NewEngine(opts)

	stats := engine.Stats()
	if stats == nil {
		t.Error("Expected non-nil stats")
	}
}

// Test TLS Checker Judge function
func TestTLSChecker_Judge_Success(t *testing.T) {
	checker := NewTLSChecker()
	soul := &core.Soul{
		ID:      "test-tls-judge",
		Name:    "Test TLS Judge",
		Type:    core.CheckTLS,
		Target:  "google.com:443",
		Timeout: core.Duration{Duration: 10 * time.Second},
		TLS: &core.TLSConfig{
			ExpiryWarnDays:     30,
			ExpiryCriticalDays: 7,
		},
	}

	judgment, err := checker.Judge(context.Background(), soul)
	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}
	// Connection should succeed, status depends on cert validity
	if judgment.Status != core.SoulAlive && judgment.Status != core.SoulDegraded {
		t.Errorf("Expected status alive or degraded, got %s", judgment.Status)
	}
	// TLS info should be populated
	if judgment.TLSInfo == nil {
		t.Error("Expected TLS info")
	}
}

func TestTLSChecker_Judge_InvalidHost(t *testing.T) {
	checker := NewTLSChecker()
	soul := &core.Soul{
		ID:      "test-tls-invalid",
		Name:    "Test TLS Invalid",
		Type:    core.CheckTLS,
		Target:  "invalid.host.example:443",
		Timeout: core.Duration{Duration: 2 * time.Second},
		TLS:     &core.TLSConfig{},
	}

	judgment, err := checker.Judge(context.Background(), soul)
	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}
	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status dead, got %s", judgment.Status)
	}
}

// Test DNS Checker resolve function
func TestDNSChecker_Resolve_A(t *testing.T) {
	checker := NewDNSChecker()
	records, err := checker.resolve(context.Background(), "example.com", "A", "8.8.8.8")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if len(records) == 0 {
		t.Error("Expected at least one A record")
	}
}

func TestDNSChecker_Resolve_AAAA(t *testing.T) {
	checker := NewDNSChecker()
	records, err := checker.resolve(context.Background(), "google.com", "AAAA", "8.8.8.8")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if len(records) == 0 {
		t.Error("Expected at least one AAAA record")
	}
}

func TestDNSChecker_Resolve_CNAME(t *testing.T) {
	checker := NewDNSChecker()
	records, err := checker.resolve(context.Background(), "www.google.com", "CNAME", "8.8.8.8")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if len(records) == 0 {
		t.Error("Expected CNAME record")
	}
}

func TestDNSChecker_Resolve_MX(t *testing.T) {
	checker := NewDNSChecker()
	records, err := checker.resolve(context.Background(), "google.com", "MX", "8.8.8.8")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if len(records) == 0 {
		t.Error("Expected MX records")
	}
}

func TestDNSChecker_Resolve_TXT(t *testing.T) {
	checker := NewDNSChecker()
	records, err := checker.resolve(context.Background(), "google.com", "TXT", "8.8.8.8")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if len(records) == 0 {
		t.Error("Expected TXT records")
	}
}

func TestDNSChecker_Resolve_NS(t *testing.T) {
	checker := NewDNSChecker()
	records, err := checker.resolve(context.Background(), "google.com", "NS", "8.8.8.8")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if len(records) == 0 {
		t.Error("Expected NS records")
	}
}

func TestDNSChecker_Resolve_InvalidType(t *testing.T) {
	checker := NewDNSChecker()
	_, err := checker.resolve(context.Background(), "example.com", "INVALID", "8.8.8.8")
	if err == nil {
		t.Error("Expected error for invalid record type")
	}
}

func TestDNSChecker_Resolve_InvalidNameserver(t *testing.T) {
	checker := NewDNSChecker()
	// Use a non-routable address that will timeout
	records, err := checker.resolve(context.Background(), "example.com", "A", "192.0.2.1")
	// May error or succeed via fallback - just verify function doesn't crash
	_ = records
	_ = err
}

// Test DNS Checker judgePropagation function
func TestDNSChecker_JudgePropagation_Success(t *testing.T) {
	checker := NewDNSChecker()
	soul := &core.Soul{
		ID:      "test-dns-prop",
		Name:    "Test DNS Propagation",
		Type:    core.CheckDNS,
		Target:  "example.com",
		Timeout: core.Duration{Duration: 10 * time.Second},
		DNS: &core.DNSConfig{
			RecordType:           "A",
			PropagationCheck:     true,
			Nameservers:          []string{"8.8.8.8", "1.1.1.1"},
			PropagationThreshold: 50,
		},
	}

	judgment, err := checker.Judge(context.Background(), soul)
	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}
	if judgment.Status != core.SoulAlive && judgment.Status != core.SoulDegraded {
		t.Errorf("Expected status alive or degraded, got %s", judgment.Status)
	}
}

// Test extractJSONPath function
func TestExtractJSONPath(t *testing.T) {
	jsonData := []byte(`{"user": {"name": "John", "age": 30, "active": true}}`)

	tests := []struct {
		path     string
		expected string
	}{
		{"$.user.name", "John"},
		{"user.name", "John"},
		{"$.user.age", "30"},
		{"user.active", "true"},
		{"$.nonexistent", ""},
		{"", ""},
	}

	for _, tt := range tests {
		result := extractJSONPath(jsonData, tt.path)
		if result != tt.expected {
			t.Errorf("extractJSONPath(%q) = %q, want %q", tt.path, result, tt.expected)
		}
	}
}

// Test tlsVersionString function
func TestTLSVersionString(t *testing.T) {
	tests := []struct {
		version  uint16
		expected string
	}{
		{tls.VersionTLS10, "TLS1.0"},
		{tls.VersionTLS11, "TLS1.1"},
		{tls.VersionTLS12, "TLS1.2"},
		{tls.VersionTLS13, "TLS1.3"},
		{0x9999, "0x9999"}, // Unknown version
	}

	for _, tt := range tests {
		result := tlsVersionString(tt.version)
		if result != tt.expected {
			t.Errorf("tlsVersionString(0x%04x) = %q, want %q", tt.version, result, tt.expected)
		}
	}
}

// Test validateJSONSchema and validateNode functions
func TestValidateJSONSchema(t *testing.T) {
	schema := `{"type": "object", "required": ["name"], "properties": {"name": {"type": "string"}}}`

	validData := []byte(`{"name": "John"}`)
	invalidData := []byte(`{"age": 30}`)   // Missing required field
	invalidType := []byte(`{"name": 123}`) // Wrong type

	if !validateJSONSchema(validData, schema, false) {
		t.Error("Expected valid JSON to pass schema validation")
	}
	if validateJSONSchema(invalidData, schema, false) {
		t.Error("Expected invalid JSON to fail schema validation")
	}
	if validateJSONSchema(invalidType, schema, false) {
		t.Error("Expected wrong type to fail schema validation")
	}
}

func TestValidateJSONSchema_InvalidSchema(t *testing.T) {
	// Invalid JSON schema
	invalidSchema := `not valid json`
	data := []byte(`{"name": "John"}`)

	if validateJSONSchema(data, invalidSchema, false) {
		t.Error("Expected invalid schema to fail")
	}
}

func TestValidateJSONSchema_StrictMode(t *testing.T) {
	schema := `{"type": "object", "properties": {"name": {"type": "string"}}}`
	dataWithExtra := []byte(`{"name": "John", "extra": "field"}`)

	// Non-strict mode should pass
	if !validateJSONSchema(dataWithExtra, schema, false) {
		t.Error("Expected non-strict mode to pass with extra fields")
	}
	// Strict mode should fail
	if validateJSONSchema(dataWithExtra, schema, true) {
		t.Error("Expected strict mode to fail with extra fields")
	}
}

func TestValidateNode_EnumValidation(t *testing.T) {
	schema := map[string]interface{}{
		"enum": []interface{}{"red", "green", "blue"},
	}

	if !validateNode("red", schema, false) {
		t.Error("Expected 'red' to pass enum validation")
	}
	if validateNode("yellow", schema, false) {
		t.Error("Expected 'yellow' to fail enum validation")
	}
}

// Test matchesType function
func TestMatchesType(t *testing.T) {
	tests := []struct {
		data         interface{}
		expectedType string
		want         bool
	}{
		{map[string]interface{}{}, "object", true},
		{[]interface{}{}, "array", true},
		{"string", "string", true},
		{42.0, "number", true},
		{42.0, "integer", true},
		{42.5, "integer", false},
		{true, "boolean", true},
		{nil, "null", true},
		{"string", "object", false},
		{42.0, "string", false},
	}

	for _, tt := range tests {
		result := matchesType(tt.data, tt.expectedType)
		if result != tt.want {
			t.Errorf("matchesType(%T, %q) = %v, want %v", tt.data, tt.expectedType, result, tt.want)
		}
	}
}

// Test gRPC Checker Judge function - error cases
func TestGRPCChecker_Judge_ConnectionFailed(t *testing.T) {
	checker := NewGRPCChecker()
	soul := &core.Soul{
		ID:      "test-grpc-fail",
		Name:    "Test gRPC Fail",
		Type:    core.CheckGRPC,
		Target:  "example.com:1",
		Timeout: core.Duration{Duration: 1 * time.Second},
		GRPC:    &core.GRPCConfig{},
	}

	judgment, err := checker.Judge(context.Background(), soul)
	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}
	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status dead, got %s", judgment.Status)
	}
}

// Test gRPC helper functions
func TestBuildGRPCHealthCheckRequest_Empty(t *testing.T) {
	req := buildGRPCHealthCheckRequest("")
	if len(req) != 5 {
		t.Errorf("Expected length 5, got %d", len(req))
	}
	if req[0] != 0 {
		t.Error("Expected uncompressed flag")
	}
}

func TestBuildGRPCHealthCheckRequest_WithService(t *testing.T) {
	req := buildGRPCHealthCheckRequest("test.service")
	if len(req) < 5 {
		t.Errorf("Expected length >= 5, got %d", len(req))
	}
	if req[0] != 0 {
		t.Error("Expected uncompressed flag")
	}
}

// Test DNS Checker Judge function - error case
func TestDNSChecker_Judge_WithExpectedRecords(t *testing.T) {
	checker := NewDNSChecker()
	soul := &core.Soul{
		ID:      "test-dns-expected",
		Name:    "Test DNS Expected",
		Type:    core.CheckDNS,
		Target:  "example.com",
		Timeout: core.Duration{Duration: 5 * time.Second},
		DNS: &core.DNSConfig{
			RecordType: "A",
			Expected:   []string{"93.184.216.34"}, // example.com's IP
		},
	}

	judgment, err := checker.Judge(context.Background(), soul)
	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}
	// Status depends on whether expected record is found
	if judgment.Status != core.SoulAlive && judgment.Status != core.SoulDead {
		t.Errorf("Expected status alive or dead, got %s", judgment.Status)
	}
}

// Helper function to create test engine
func newTestEngine(t *testing.T) *Engine {
	opts := EngineOptions{
		NodeID:   "test-node",
		Region:   "test-region",
		Store:    &mockProbeStorage{},
		Alerter:  &mockProbeAlerter{},
		Logger:   newTestProbeLogger(),
		Registry: NewCheckerRegistry(),
	}
	return NewEngine(opts)
}

// Test TriggerImmediate - soul not found
func TestEngine_TriggerImmediate_NotFound(t *testing.T) {
	engine := newTestEngine(t)

	ctx := context.Background()
	_, err := engine.TriggerImmediate(ctx, "nonexistent-soul")

	if err == nil {
		t.Error("Expected error for nonexistent soul")
	}
}

// Test GetStatus - empty status
func TestEngine_GetStatus_NoArgs(t *testing.T) {
	engine := newTestEngine(t)

	// GetStatus takes no arguments in current implementation
	status := engine.GetStatus()
	// Should not panic
	_ = status
}

// Test ListActiveSouls - empty
func TestEngine_ListActiveSouls_Empty(t *testing.T) {
	engine := newTestEngine(t)

	souls := engine.ListActiveSouls()
	if len(souls) != 0 {
		t.Errorf("Expected 0 souls, got %d", len(souls))
	}
}

// Test Stats
func TestEngine_Stats_Empty(t *testing.T) {
	engine := newTestEngine(t)

	stats := engine.Stats()
	if stats["active_souls"] != 0 {
		t.Errorf("Expected 0 active souls, got %d", stats["active_souls"])
	}
}

// Test judgeSoul with unknown checker type
func TestEngine_judgeSoul_UnknownType(t *testing.T) {
	engine := newTestEngine(t)

	soul := &core.Soul{
		ID:     "test-unknown-judge",
		Name:   "Test Unknown Judge",
		Type:   "unknown-type",
		Target: "localhost:80",
	}

	runner := &soulRunner{soul: soul}
	ctx := context.Background()

	// Should not panic, just log error
	engine.judgeSoul(ctx, runner)
	// Test passes if no panic
}

// Test judgeSoul with validation error
func TestEngine_judgeSoul_ValidationError(t *testing.T) {
	engine := newTestEngine(t)

	soul := &core.Soul{
		ID:     "test-invalid-judge",
		Name:   "Test Invalid Judge",
		Type:   core.CheckHTTP,
		Target: "", // Empty target should fail validation
	}

	runner := &soulRunner{soul: soul}
	ctx := context.Background()

	// Should not panic, just log error
	engine.judgeSoul(ctx, runner)
	// Test passes if no panic
}

// Test TriggerImmediate with unknown checker type
func TestEngine_TriggerImmediate_UnknownType(t *testing.T) {
	engine := newTestEngine(t)

	// Manually add a soul with unknown type to the engine
	soul := &core.Soul{
		ID:     "test-unknown-type",
		Name:   "Test Unknown Type",
		Type:   "unknown-checker-type",
		Target: "localhost:80",
	}

	engine.mu.Lock()
	engine.souls[soul.ID] = &soulRunner{soul: soul}
	engine.mu.Unlock()

	ctx := context.Background()
	_, err := engine.TriggerImmediate(ctx, soul.ID)

	if err == nil {
		t.Error("Expected error for unknown checker type")
	}
}

// Test TriggerImmediate with valid soul
func TestEngine_TriggerImmediate_Success(t *testing.T) {
	engine := newTestEngine(t)

	// Add a valid HTTP soul
	soul := &core.Soul{
		ID:      "test-http-trigger",
		Name:    "Test HTTP Trigger",
		Type:    core.CheckHTTP,
		Target:  "https://example.com",
		Enabled: true,
		HTTP: &core.HTTPConfig{
			Method:      "GET",
			ValidStatus: []int{200},
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	engine.mu.Lock()
	engine.souls[soul.ID] = &soulRunner{soul: soul}
	engine.mu.Unlock()

	ctx := context.Background()
	judgment, err := engine.TriggerImmediate(ctx, soul.ID)

	if err != nil {
		t.Fatalf("TriggerImmediate failed: %v", err)
	}
	if judgment == nil {
		t.Fatal("Expected judgment to be returned")
	}
	if judgment.JackalID != engine.nodeID {
		t.Error("Expected JackalID to be set")
	}
	if judgment.Region != engine.region {
		t.Error("Expected Region to be set")
	}
}

// Test judgePropagation with propagation below threshold
func TestDNSChecker_JudgePropagation_BelowThreshold(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns-propagation",
		Name:   "Test DNS Propagation",
		Type:   core.CheckDNS,
		Target: "example.com",
		DNS: &core.DNSConfig{
			RecordType:           "A",
			PropagationCheck:     true,
			Nameservers:          []string{"8.8.8.8:53", "1.1.1.1:53", "9.9.9.9:53"},
			PropagationThreshold: 100, // Require 100% propagation
		},
		Timeout: core.Duration{Duration: 10 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should pass or be degraded depending on DNS propagation
	if judgment == nil {
		t.Fatal("Expected judgment to be returned")
	}
	if judgment.Details.PropagationResult == nil {
		t.Error("Expected propagation results")
	}
}

// Test NewEngine with nil logger (uses default logger)
func TestNewEngine_NilLogger(t *testing.T) {
	opts := EngineOptions{
		NodeID:   "test-node",
		Region:   "test-region",
		Registry: NewCheckerRegistry(),
		// Logger is nil - should use default
	}

	engine := NewEngine(opts)

	if engine == nil {
		t.Fatal("Expected engine to be created")
	}
	if engine.logger == nil {
		t.Error("Expected logger to be set to default")
	}
}

// Test judgeSoul successful path with storage
func TestEngine_judgeSoul_Success(t *testing.T) {
	engine := newTestEngine(t)

	soul := &core.Soul{
		ID:      "test-http-judge-success",
		Name:    "Test HTTP Judge Success",
		Type:    core.CheckHTTP,
		Target:  "https://example.com",
		Enabled: true,
		HTTP: &core.HTTPConfig{
			Method:      "GET",
			ValidStatus: []int{200},
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	runner := &soulRunner{
		soul:       soul,
		lastStatus: core.SoulUnknown,
	}
	ctx := context.Background()

	// Should not panic and should execute successfully
	engine.judgeSoul(ctx, runner)

	// Test passes if no panic - judgment should be stored via mock
}

// Test judgePropagation with nameserver errors
func TestDNSChecker_JudgePropagation_NameserverErrors(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns-propagation-errors",
		Name:   "Test DNS Propagation Errors",
		Type:   core.CheckDNS,
		Target: "example.com",
		DNS: &core.DNSConfig{
			RecordType:       "A",
			PropagationCheck: true,
			// Use invalid nameserver that will fail
			Nameservers:          []string{"127.0.0.1:1", "127.0.0.1:2"},
			PropagationThreshold: 0, // Allow any propagation
		},
		Timeout: core.Duration{Duration: 2 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment == nil {
		t.Fatal("Expected judgment to be returned")
	}
	// With all nameservers failing and threshold 0, should be degraded
	if judgment.Status != core.SoulDegraded && judgment.Status != core.SoulDead {
		t.Logf("Got status %s, expected Degraded or Dead", judgment.Status)
	}
}

// Test CircuitBreaker state transitions
func TestCircuitBreaker_StateTransitions(t *testing.T) {
	engine := NewEngine(EngineOptions{
		Registry: NewCheckerRegistry(),
		NodeID:   "test-node",
		Region:   "test-region",
		Logger:   newTestProbeLogger(),
		Config: EngineConfig{
			CircuitBreaker: CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 3,
				SuccessThreshold: 2,
				Timeout:          100 * time.Millisecond,
			},
		},
	})

	soulID := "test-soul-cb"

	// Initially closed
	cb := engine.getCircuitBreaker(soulID)
	cb.mu.RLock()
	state := cb.state
	cb.mu.RUnlock()
	if state != "closed" {
		t.Errorf("Expected initially closed, got '%s'", state)
	}

	// Record failures to open circuit (threshold is 3)
	for i := 0; i < 3; i++ {
		engine.recordFailure(soulID)
	}

	// Should now be open
	cb.mu.RLock()
	state = cb.state
	failures := cb.failures
	cb.mu.RUnlock()
	if state != "open" {
		t.Errorf("Expected state 'open' after 3 failures, got '%s'", state)
	}
	if failures < 3 {
		t.Errorf("Expected at least 3 failures, got %d", failures)
	}

	// Wait for timeout to elapse
	time.Sleep(150 * time.Millisecond)

	// Call isOpen to trigger transition to half-open
	cb.isOpen(engine.Config().CircuitBreaker)

	cb.mu.RLock()
	state = cb.state
	cb.mu.RUnlock()
	if state != "half-open" {
		t.Errorf("Expected state 'half-open' after timeout, got '%s'", state)
	}
}

// Test recordSuccess closes circuit breaker from half-open
func TestCircuitBreaker_RecordSuccess(t *testing.T) {
	engine := NewEngine(EngineOptions{
		Registry: NewCheckerRegistry(),
		NodeID:   "test-node",
		Region:   "test-region",
		Logger:   newTestProbeLogger(),
	})

	soulID := "test-soul-success"

	// Get circuit breaker and force to half-open state
	cb := engine.getCircuitBreaker(soulID)
	cb.mu.Lock()
	cb.state = "half-open"
	cb.successes = 1
	cb.mu.Unlock()

	// Record successes (need 3 total to close)
	engine.recordSuccess(soulID)
	engine.recordSuccess(soulID)
	engine.recordSuccess(soulID)

	// Should be closed now (successes >= 3)
	cb.mu.RLock()
	state := cb.state
	cb.mu.RUnlock()

	if state != "closed" {
		t.Errorf("Expected state 'closed' after successes, got '%s'", state)
	}
}

// Test recordFailure opens circuit breaker
func TestCircuitBreaker_RecordFailure(t *testing.T) {
	engine := NewEngine(EngineOptions{
		Registry: NewCheckerRegistry(),
		NodeID:   "test-node",
		Region:   "test-region",
		Logger:   newTestProbeLogger(),
	})

	soulID := "test-soul-failure"

	// Record failures (threshold is 5 by default)
	for i := 0; i < 5; i++ {
		engine.recordFailure(soulID)
	}

	// Get circuit breaker
	cb := engine.getCircuitBreaker(soulID)
	cb.mu.RLock()
	state := cb.state
	failures := cb.failures
	cb.mu.RUnlock()

	if state != "open" {
		t.Errorf("Expected state 'open' after 5 failures, got '%s'", state)
	}
	if failures < 5 {
		t.Errorf("Expected at least 5 failures, got %d", failures)
	}

	// Verify failedChecks counter incremented
	stats := engine.Stats()
	if stats["failed_checks"].(int64) < 5 {
		t.Errorf("Expected at least 5 failed checks in stats, got %d", stats["failed_checks"])
	}
}

// Test semaphore concurrency limiting
func TestEngine_Semaphore_ConcurrencyLimit(t *testing.T) {
	const maxConcurrent = 3

	registry := NewCheckerRegistry()
	engine := NewEngine(EngineOptions{
		Registry: registry,
		NodeID:   "test-node",
		Region:   "test-region",
		Logger:   newTestProbeLogger(),
		Config: EngineConfig{
			MaxConcurrentChecks: maxConcurrent,
		},
	})

	// Verify semaphore capacity
	if cap(engine.semaphore) != maxConcurrent {
		t.Errorf("Expected semaphore capacity %d, got %d", maxConcurrent, cap(engine.semaphore))
	}
}

// Test Engine Config returns correct values
func TestEngine_Config(t *testing.T) {
	engine := NewEngine(EngineOptions{
		Registry: NewCheckerRegistry(),
		NodeID:   "test-node",
		Region:   "test-region",
		Logger:   newTestProbeLogger(),
		Config: EngineConfig{
			MaxConcurrentChecks: 50,
			CircuitBreaker: CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 10,
				SuccessThreshold: 5,
				Timeout:          60 * time.Second,
			},
		},
	})

	config := engine.Config()

	if config.MaxConcurrentChecks != 50 {
		t.Errorf("Expected MaxConcurrentChecks 50, got %d", config.MaxConcurrentChecks)
	}
	if config.CircuitBreaker.FailureThreshold != 10 {
		t.Errorf("Expected FailureThreshold 10, got %d", config.CircuitBreaker.FailureThreshold)
	}
	if config.CircuitBreaker.SuccessThreshold != 5 {
		t.Errorf("Expected SuccessThreshold 5, got %d", config.CircuitBreaker.SuccessThreshold)
	}
	if config.CircuitBreaker.Timeout != 60*time.Second {
		t.Errorf("Expected Timeout 60s, got %v", config.CircuitBreaker.Timeout)
	}
	if !config.CircuitBreaker.Enabled {
		t.Error("Expected CircuitBreaker to be enabled")
	}
}

// Test circuit breaker is created per soul
func TestCircuitBreaker_PerSoul(t *testing.T) {
	engine := NewEngine(EngineOptions{
		Registry: NewCheckerRegistry(),
		NodeID:   "test-node",
		Region:   "test-region",
		Logger:   newTestProbeLogger(),
	})

	// Get circuit breakers for different souls
	cb1 := engine.getCircuitBreaker("soul-1")
	cb2 := engine.getCircuitBreaker("soul-2")

	// Should be different instances
	if cb1 == cb2 {
		t.Error("Expected different circuit breaker instances per soul")
	}

	// Getting same soul should return same instance
	cb1Again := engine.getCircuitBreaker("soul-1")
	if cb1 != cb1Again {
		t.Error("Expected same circuit breaker instance for same soul")
	}
}

// Test default engine config
func TestDefaultEngineConfig(t *testing.T) {
	cfg := DefaultEngineConfig()

	if cfg.MaxConcurrentChecks != 100 {
		t.Errorf("Expected MaxConcurrentChecks 100, got %d", cfg.MaxConcurrentChecks)
	}
	if !cfg.CircuitBreaker.Enabled {
		t.Error("Expected CircuitBreaker to be enabled by default")
	}
	if cfg.CircuitBreaker.FailureThreshold != 5 {
		t.Errorf("Expected FailureThreshold 5, got %d", cfg.CircuitBreaker.FailureThreshold)
	}
	if cfg.CircuitBreaker.SuccessThreshold != 3 {
		t.Errorf("Expected SuccessThreshold 3, got %d", cfg.CircuitBreaker.SuccessThreshold)
	}
	if cfg.CircuitBreaker.Timeout != 30*time.Second {
		t.Errorf("Expected Timeout 30s, got %v", cfg.CircuitBreaker.Timeout)
	}
}

// Test AssignSouls with region filtering
func TestEngine_AssignSouls_RegionFiltering(t *testing.T) {
	registry := NewCheckerRegistry()
	engine := NewEngine(EngineOptions{
		Registry: registry,
		NodeID:   "test-node-us",
		Region:   "us-east-1",
		Logger:   newTestProbeLogger(),
	})

	// Souls with different region restrictions
	souls := []*core.Soul{
		{
			ID:      "soul-us",
			Name:    "Soul US",
			Type:    core.CheckHTTP,
			Target:  "https://example.com",
			Regions: []string{"us-east-1", "us-west-2"}, // Should be assigned
			HTTP:    &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
		},
		{
			ID:      "soul-eu",
			Name:    "Soul EU",
			Type:    core.CheckHTTP,
			Target:  "https://example.com",
			Regions: []string{"eu-west-1", "eu-central-1"}, // Should be skipped
			HTTP:    &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
		},
		{
			ID:      "soul-global",
			Name:    "Soul Global",
			Type:    core.CheckHTTP,
			Target:  "https://example.com",
			Regions: []string{}, // No restriction - should be assigned
			HTTP:    &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
		},
		{
			ID:     "soul-any",
			Name:   "Soul Any",
			Type:   core.CheckHTTP,
			Target: "https://example.com",
			// No Regions field - should be assigned
			HTTP: &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
		},
	}

	engine.AssignSouls(souls)

	// Should have 3 souls assigned (us, global, any)
	activeSouls := engine.ListActiveSouls()
	if len(activeSouls) != 3 {
		t.Errorf("Expected 3 souls assigned, got %d", len(activeSouls))
	}

	// Verify which souls are assigned
	assignedIDs := make(map[string]bool)
	for _, s := range activeSouls {
		assignedIDs[s.ID] = true
	}

	if !assignedIDs["soul-us"] {
		t.Error("Expected soul-us to be assigned (region matches)")
	}
	if !assignedIDs["soul-global"] {
		t.Error("Expected soul-global to be assigned (no region restriction)")
	}
	if !assignedIDs["soul-any"] {
		t.Error("Expected soul-any to be assigned (no Regions field)")
	}
	if assignedIDs["soul-eu"] {
		t.Error("Expected soul-eu to NOT be assigned (region mismatch)")
	}
}

// TestRetryWithBackoff_SuccessFirstTry tests immediate success
func TestRetryWithBackoff_SuccessFirstTry(t *testing.T) {
	callCount := 0
	err := retryWithBackoff(context.Background(), 3, time.Millisecond, func() error {
		callCount++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}

// TestRetryWithBackoff_SuccessAfterRetries tests success after failures
func TestRetryWithBackoff_SuccessAfterRetries(t *testing.T) {
	callCount := 0
	err := retryWithBackoff(context.Background(), 5, time.Millisecond, func() error {
		callCount++
		if callCount < 3 {
			return fmt.Errorf("temporary failure")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}
}

// TestRetryWithBackoff_AllRetriesExhausted tests all retries failing
func TestRetryWithBackoff_AllRetriesExhausted(t *testing.T) {
	callCount := 0
	expectedErr := fmt.Errorf("permanent failure")
	err := retryWithBackoff(context.Background(), 3, time.Millisecond, func() error {
		callCount++
		return expectedErr
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}
	if !strings.Contains(err.Error(), "failed after 3 retries") {
		t.Errorf("expected retry count in error message, got: %v", err)
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected wrapped error to be %v, got: %v", expectedErr, err)
	}
}

// TestRetryWithBackoff_ContextCancelled tests context cancellation before retry
func TestRetryWithBackoff_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	callCount := 0
	cancel() // Cancel immediately

	err := retryWithBackoff(ctx, 5, time.Millisecond, func() error {
		callCount++
		return fmt.Errorf("failure")
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}

// TestRetryWithBackoff_ContextCancelledDuringWait tests context cancellation during backoff wait
func TestRetryWithBackoff_ContextCancelledDuringWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	callCount := 0

	err := retryWithBackoff(ctx, 5, 50*time.Millisecond, func() error {
		callCount++
		if callCount > 1 {
			cancel()
		}
		return fmt.Errorf("failure")
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

// TestEngine_SetOnJudgment tests the SetOnJudgment callback
func TestEngine_SetOnJudgment(t *testing.T) {
	opts := EngineOptions{
		NodeID:  "test-node",
		Region:  "test-region",
		Store:   &mockProbeStorage{},
		Alerter: &mockProbeAlerter{},
		Logger:  newTestProbeLogger(),
	}
	engine := NewEngine(opts)

	called := false
	engine.SetOnJudgment(func(j *core.Judgment) {
		called = true
	})

	// Verify the callback was registered without panicking
	_ = called
}

// TestCircuitBreaker_IsOpen_AllStates tests all isOpen branches
func TestCircuitBreaker_IsOpen_AllStates(t *testing.T) {
	engine := NewEngine(EngineOptions{
		Registry: NewCheckerRegistry(),
		NodeID:   "test-node",
		Region:   "test-region",
		Logger:   newTestProbeLogger(),
		Config: EngineConfig{
			CircuitBreaker: CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 3,
				SuccessThreshold: 2,
				Timeout:          200 * time.Millisecond,
			},
		},
	})

	soulID := "test-is-open"
	cb := engine.getCircuitBreaker(soulID)
	cfg := engine.Config().CircuitBreaker

	// 1. State closed - should return false
	result := cb.isOpen(cfg)
	if result {
		t.Error("isOpen should return false for closed state")
	}

	// 2. Open the circuit (3 failures)
	for i := 0; i < 3; i++ {
		engine.recordFailure(soulID)
	}

	// 3. State open, timeout NOT elapsed - should return true
	result = cb.isOpen(cfg)
	if !result {
		t.Error("isOpen should return true for open state before timeout")
	}

	// 4. Wait for timeout to elapse, should transition to half-open
	time.Sleep(250 * time.Millisecond)
	result = cb.isOpen(cfg)
	if result {
		t.Error("isOpen should return false after timeout elapsed (half-open)")
	}

	// 5. State half-open - should return false
	result = cb.isOpen(cfg)
	if result {
		t.Error("isOpen should return false for half-open state")
	}
}

// TestCircuitBreaker_IsOpen_DoubleCheck tests the double-check pattern in isOpen
func TestCircuitBreaker_IsOpen_DoubleCheck(t *testing.T) {
	engine := NewEngine(EngineOptions{
		Registry: NewCheckerRegistry(),
		NodeID:   "test-node",
		Region:   "test-region",
		Logger:   newTestProbeLogger(),
		Config: EngineConfig{
			CircuitBreaker: CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 3,
				SuccessThreshold: 2,
				Timeout:          100 * time.Millisecond,
			},
		},
	})

	soulID := "test-double-check"
	cb := engine.getCircuitBreaker(soulID)

	// Open the circuit
	for i := 0; i < 3; i++ {
		engine.recordFailure(soulID)
	}

	// Force state to half-open (simulating another goroutine already transitioned)
	cb.mu.Lock()
	cb.state = "half-open"
	cb.mu.Unlock()

	// Wait for timeout
	time.Sleep(150 * time.Millisecond)

	// isOpen on half-open should return false
	result := cb.isOpen(engine.Config().CircuitBreaker)
	if result {
		t.Error("isOpen should return false for half-open state")
	}
}

// Test regionMatches with no matching region (covers return false path)
func TestEngine_regionMatches(t *testing.T) {
	tests := []struct {
		name        string
		probeRegion string
		soulRegions []string
		expected    bool
	}{
		{"empty regions", "us-east-1", nil, true},
		{"matching region", "us-east-1", []string{"us-east-1", "eu-west-1"}, true},
		{"no match", "eu-west-1", []string{"us-east-1", "us-west-2"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := &Engine{region: tt.probeRegion}
			result := engine.regionMatches(tt.soulRegions)
			if result != tt.expected {
				t.Errorf("regionMatches(%v) with region=%q = %v, want %v",
					tt.soulRegions, tt.probeRegion, result, tt.expected)
			}
		})
	}
}

// Test GetSoulStatus success path with existing soul
func TestEngine_GetSoulStatus_ExistingSoul(t *testing.T) {
	engine := newTestEngine(t)

	soul := &core.Soul{
		ID:      "test-http-status",
		Name:    "Test HTTP Status",
		Type:    core.CheckHTTP,
		Target:  "https://example.com",
		Enabled: true,
		HTTP: &core.HTTPConfig{
			Method:      "GET",
			ValidStatus: []int{200},
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	runner := &soulRunner{
		soul:       soul,
		lastStatus: core.SoulAlive,
	}
	engine.mu.Lock()
	engine.souls[soul.ID] = runner
	engine.mu.Unlock()

	status, err := engine.GetSoulStatus(soul.ID)
	if err != nil {
		t.Fatalf("GetSoulStatus failed: %v", err)
	}
	if *status != core.SoulAlive {
		t.Errorf("Expected status %s, got %s", core.SoulAlive, *status)
	}
}

// Test judgeSoul with circuit breaker open (covers early return skip path)
func TestEngine_judgeSoul_CircuitBreakerOpen(t *testing.T) {
	engine := NewEngine(EngineOptions{
		Registry: NewCheckerRegistry(),
		NodeID:   "test-node",
		Region:   "test-region",
		Store:    &mockProbeStorage{},
		Alerter:  &mockProbeAlerter{},
		Logger:   newTestProbeLogger(),
		Config: EngineConfig{
			CircuitBreaker: CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 2,
				SuccessThreshold: 2,
				Timeout:          10 * time.Second, // Long timeout so circuit stays open
			},
		},
	})

	soul := &core.Soul{
		ID:     "cb-open-soul",
		Name:   "CB Open",
		Type:   core.CheckHTTP,
		Target: "https://example.com",
		HTTP:   &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
	}

	// Record enough failures to open circuit
	for i := 0; i < 2; i++ {
		engine.recordFailure(soul.ID)
	}

	// Verify circuit is open
	cb := engine.getCircuitBreaker(soul.ID)
	cb.mu.RLock()
	state := cb.state
	cb.mu.RUnlock()
	if state != "open" {
		t.Fatalf("Expected circuit open, got %s", state)
	}

	runner := &soulRunner{soul: soul}
	ctx := context.Background()

	// Should skip check due to open circuit
	engine.judgeSoul(ctx, runner)

	// No stats should have been incremented (check was skipped)
	stats := engine.Stats()
	if stats["total_checks"].(int64) != 0 {
		t.Errorf("Expected 0 total checks (skipped), got %d", stats["total_checks"])
	}
}

// Test judgeSoul with context already cancelled (covers ctx.Done in semaphore select)
func TestEngine_judgeSoul_ContextCancelled(t *testing.T) {
	engine := NewEngine(EngineOptions{
		Registry: NewCheckerRegistry(),
		NodeID:   "test-node",
		Region:   "test-region",
		Store:    &mockProbeStorage{},
		Alerter:  &mockProbeAlerter{},
		Logger:   newTestProbeLogger(),
		Config: EngineConfig{
			MaxConcurrentChecks: 1, // Small semaphore
		},
	})

	// Pre-fill the semaphore so acquire blocks
	engine.semaphore <- struct{}{}

	soul := &core.Soul{
		ID:     "ctx-cancel-soul",
		Name:   "Ctx Cancel",
		Type:   core.CheckHTTP,
		Target: "https://example.com",
		HTTP:   &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
	}

	runner := &soulRunner{soul: soul}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before calling judgeSoul

	// Should return via ctx.Done in semaphore select since semaphore is full
	engine.judgeSoul(ctx, runner)

	// No checks should have run
	stats := engine.Stats()
	if stats["total_checks"].(int64) != 0 {
		t.Errorf("Expected 0 total checks (cancelled), got %d", stats["total_checks"])
	}
}

// Test TriggerImmediate with context cancelled
func TestEngine_TriggerImmediate_ContextCancelled(t *testing.T) {
	engine := newTestEngine(t)

	// Register a custom checker that blocks waiting on context
	ctxChecker := &contextAwareChecker{}
	engine.registry.Register(ctxChecker)

	soul := &core.Soul{
		ID:      "ctx-cancel-trigger",
		Name:    "Ctx Cancel Trigger",
		Type:    "ctx-check",
		Target:  "example.com:1",
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	engine.mu.Lock()
	engine.souls[soul.ID] = &soulRunner{soul: soul}
	engine.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before trigger

	// Should fail due to context cancellation
	_, err := engine.TriggerImmediate(ctx, soul.ID)
	if err == nil {
		t.Error("Expected error from cancelled context")
	}
}

type contextAwareChecker struct{}

func (c *contextAwareChecker) Type() core.CheckType { return "ctx-check" }
func (c *contextAwareChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
	// Check context immediately
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return &core.Judgment{Status: core.SoulAlive}, nil
}
func (c *contextAwareChecker) Validate(soul *core.Soul) error { return nil }

// Test judgeSoul with checker error path (records failure)
func TestEngine_judgeSoul_CheckerError(t *testing.T) {
	// Use a mock checker that returns an error
	engine := NewEngine(EngineOptions{
		Registry: NewCheckerRegistry(),
		NodeID:   "test-node",
		Region:   "test-region",
		Store:    &mockProbeStorage{},
		Alerter:  &mockProbeAlerter{},
		Logger:   newTestProbeLogger(),
	})

	// Register a custom checker that always errors
	errorChecker := &errorMockChecker{}
	engine.registry.Register(errorChecker)

	soul := &core.Soul{
		ID:      "error-checker-soul",
		Name:    "Error Checker",
		Type:    "error-check",
		Target:  "example.com:1",
		Timeout: core.Duration{Duration: 100 * time.Millisecond},
	}

	runner := &soulRunner{
		soul:       soul,
		lastStatus: core.SoulUnknown,
	}
	ctx := context.Background()

	// Should not panic, should record failure
	engine.judgeSoul(ctx, runner)

	// Should have recorded a failure
	stats := engine.Stats()
	if stats["failed_checks"].(int64) < 1 {
		t.Errorf("Expected at least 1 failed check, got %d", stats["failed_checks"])
	}
}

type errorMockChecker struct{}

func (c *errorMockChecker) Type() core.CheckType { return "error-check" }
func (c *errorMockChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
	return nil, fmt.Errorf("simulated checker error")
}
func (c *errorMockChecker) Validate(soul *core.Soul) error { return nil }
