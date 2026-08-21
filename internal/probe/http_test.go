package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestHTTPChecker_Judge_Basic(t *testing.T) {
	// Create test server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer ts.Close()

	checker := NewHTTPChecker()

	soul := &core.Soul{
		ID:      "test-http",
		Name:    "Test HTTP",
		Type:    core.CheckHTTP,
		Target:  ts.URL,
		Enabled: true,
		Weight:  core.Duration{Duration: 60 * time.Second},
		HTTP: &core.HTTPConfig{
			Method:      "GET",
			ValidStatus: []int{200},
		},
	}

	ctx := context.Background()
	judgment, err := checker.Judge(ctx, soul)

	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}

	if judgment.StatusCode != 200 {
		t.Errorf("Expected status code 200, got %d", judgment.StatusCode)
	}
}

func TestHTTPChecker_Judge_DoesNotPersistSensitiveResponseMaterial(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "session=super-secret")
		w.Header().Set("Authorization", "test-scheme response-value")
		w.Header().Set("X-Request-ID", "req-123")
		w.Write([]byte(`{"token":"body-secret"}`))
	}))
	defer ts.Close()

	judgment, err := NewHTTPChecker().Judge(context.Background(), &core.Soul{
		ID: "test-http", Type: core.CheckHTTP, Target: ts.URL,
		Timeout: core.Duration{Duration: time.Second},
		HTTP:    &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
	})
	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}
	if judgment.Details.ResponseBody != "" {
		t.Fatalf("response body persisted: %q", judgment.Details.ResponseBody)
	}
	if judgment.Details.ResponseHeaders["Set-Cookie"] != "" || judgment.Details.ResponseHeaders["Authorization"] != "" {
		t.Fatalf("sensitive headers persisted: %#v", judgment.Details.ResponseHeaders)
	}
	if judgment.Details.ResponseHeaders["X-Request-Id"] != "req-123" {
		t.Fatalf("safe response header missing: %#v", judgment.Details.ResponseHeaders)
	}
}

func TestHTTPChecker_Judge_CrossOriginRedirectDropsCredentials(t *testing.T) {
	var received http.Header
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/final?token=redirect-secret", http.StatusFound)
	}))
	defer origin.Close()

	judgment, err := NewHTTPChecker().Judge(context.Background(), &core.Soul{
		ID: "test-http", Type: core.CheckHTTP, Target: origin.URL,
		Timeout: core.Duration{Duration: time.Second},
		HTTP: &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}, FollowRedirects: true,
			Headers: map[string]string{"Authorization": "test-scheme request-value", "Cookie": "session=request-secret", "X-API-Key": "request-secret", "X-Trace": "keep-me"}},
	})
	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}
	for _, name := range []string{"Authorization", "Cookie", "X-API-Key"} {
		if received.Get(name) != "" {
			t.Fatalf("%s forwarded across origin: %#v", name, received)
		}
	}
	if received.Get("X-Trace") != "keep-me" {
		t.Fatalf("non-sensitive header was removed: %#v", received)
	}
	if len(judgment.Details.RedirectChain) != 1 || strings.Contains(judgment.Details.RedirectChain[0], "redirect-secret") {
		t.Fatalf("redirect chain leaked query credentials: %#v", judgment.Details.RedirectChain)
	}
}

func TestHTTPChecker_Judge_StatusMismatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	checker := NewHTTPChecker()

	soul := &core.Soul{
		ID:     "test-http",
		Name:   "Test HTTP",
		Type:   core.CheckHTTP,
		Target: ts.URL,
		HTTP: &core.HTTPConfig{
			Method:      "GET",
			ValidStatus: []int{200}, // Expect 200, will get 500
		},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestHTTPChecker_Judge_BodyContains(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello, World!"))
	}))
	defer ts.Close()

	checker := NewHTTPChecker()

	soul := &core.Soul{
		ID:     "test-http",
		Name:   "Test HTTP",
		Type:   core.CheckHTTP,
		Target: ts.URL,
		HTTP: &core.HTTPConfig{
			Method:       "GET",
			ValidStatus:  []int{200},
			BodyContains: "World",
		},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestHTTPChecker_Judge_BodyContainsMismatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello, World!"))
	}))
	defer ts.Close()

	checker := NewHTTPChecker()

	soul := &core.Soul{
		ID:     "test-http",
		Name:   "Test HTTP",
		Type:   core.CheckHTTP,
		Target: ts.URL,
		HTTP: &core.HTTPConfig{
			Method:       "GET",
			ValidStatus:  []int{200},
			BodyContains: "Goodbye", // Not present
		},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestHTTPChecker_Judge_BodyRegex(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("User ID: 12345"))
	}))
	defer ts.Close()

	checker := NewHTTPChecker()

	soul := &core.Soul{
		ID:     "test-http",
		Name:   "Test HTTP",
		Type:   core.CheckHTTP,
		Target: ts.URL,
		HTTP: &core.HTTPConfig{
			Method:      "GET",
			ValidStatus: []int{200},
			BodyRegex:   "User ID: \\d+",
		},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestHTTPChecker_Judge_JSONPath(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name": "John", "age": 30}`))
	}))
	defer ts.Close()

	checker := NewHTTPChecker()

	soul := &core.Soul{
		ID:     "test-http",
		Name:   "Test HTTP",
		Type:   core.CheckHTTP,
		Target: ts.URL,
		HTTP: &core.HTTPConfig{
			Method:      "GET",
			ValidStatus: []int{200},
			JSONPath: map[string]string{
				"$.name": "John",
			},
		},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestHTTPChecker_Judge_JSONPathMismatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name": "John", "age": 30}`))
	}))
	defer ts.Close()

	checker := NewHTTPChecker()

	soul := &core.Soul{
		ID:     "test-http",
		Name:   "Test HTTP",
		Type:   core.CheckHTTP,
		Target: ts.URL,
		HTTP: &core.HTTPConfig{
			Method:      "GET",
			ValidStatus: []int{200},
			JSONPath: map[string]string{
				"$.name": "Jane", // Wrong value
			},
		},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestHTTPChecker_Judge_ResponseHeader(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Header", "test-value")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	checker := NewHTTPChecker()

	soul := &core.Soul{
		ID:     "test-http",
		Name:   "Test HTTP",
		Type:   core.CheckHTTP,
		Target: ts.URL,
		HTTP: &core.HTTPConfig{
			Method:      "GET",
			ValidStatus: []int{200},
			ResponseHeaders: map[string]string{
				"X-Custom-Header": "test-value",
			},
		},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestHTTPChecker_Judge_Feather(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	checker := NewHTTPChecker()

	soul := &core.Soul{
		ID:     "test-http",
		Name:   "Test HTTP",
		Type:   core.CheckHTTP,
		Target: ts.URL,
		HTTP: &core.HTTPConfig{
			Method:      "GET",
			ValidStatus: []int{200},
			Feather:     core.Duration{Duration: 500 * time.Millisecond}, // Generous budget
		},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestHTTPChecker_Judge_FeatherExceeded(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	checker := NewHTTPChecker()

	soul := &core.Soul{
		ID:     "test-http",
		Name:   "Test HTTP",
		Type:   core.CheckHTTP,
		Target: ts.URL,
		HTTP: &core.HTTPConfig{
			Method:      "GET",
			ValidStatus: []int{200},
			Feather:     core.Duration{Duration: 10 * time.Millisecond}, // Tight budget
		},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should be degraded, not dead
	if judgment.Status != core.SoulDegraded {
		t.Errorf("Expected status Degraded, got %s", judgment.Status)
	}
}

func TestHTTPChecker_Judge_NoFollowRedirects(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/redirected", http.StatusMovedPermanently)
	}))
	defer ts.Close()

	checker := NewHTTPChecker()

	soul := &core.Soul{
		ID:     "test-http",
		Name:   "Test HTTP",
		Type:   core.CheckHTTP,
		Target: ts.URL,
		HTTP: &core.HTTPConfig{
			Method:          "GET",
			ValidStatus:     []int{301}, // Expect redirect
			FollowRedirects: false,
		},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestHTTPChecker_Judge_MaxRedirects(t *testing.T) {
	redirectCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectCount++
		if redirectCount < 5 {
			http.Redirect(w, r, "/redirect", http.StatusFound)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer ts.Close()

	checker := NewHTTPChecker()

	soul := &core.Soul{
		ID:     "test-http",
		Name:   "Test HTTP",
		Type:   core.CheckHTTP,
		Target: ts.URL,
		HTTP: &core.HTTPConfig{
			Method:       "GET",
			ValidStatus:  []int{200},
			MaxRedirects: 2, // Will exceed
		},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should fail due to too many redirects
	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestHTTPChecker_Judge_ConnectError(t *testing.T) {
	checker := NewHTTPChecker()

	soul := &core.Soul{
		ID:     "test-http",
		Name:   "Test HTTP",
		Type:   core.CheckHTTP,
		Target: "http://localhost:1", // Invalid port
		HTTP: &core.HTTPConfig{
			Method:      "GET",
			ValidStatus: []int{200},
		},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestHTTPChecker_Judge_Timeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer ts.Close()

	checker := NewHTTPChecker()

	soul := &core.Soul{
		ID:      "test-http",
		Name:    "Test HTTP",
		Type:    core.CheckHTTP,
		Target:  ts.URL,
		Timeout: core.Duration{Duration: 100 * time.Millisecond}, // Short timeout
		HTTP: &core.HTTPConfig{
			Method:      "GET",
			ValidStatus: []int{200},
		},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestHTTPChecker_Judge_CustomHeaders(t *testing.T) {
	var receivedHeaders http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	checker := NewHTTPChecker()

	soul := &core.Soul{
		ID:     "test-http",
		Name:   "Test HTTP",
		Type:   core.CheckHTTP,
		Target: ts.URL,
		HTTP: &core.HTTPConfig{
			Method:      "GET",
			ValidStatus: []int{200},
			Headers: map[string]string{
				"X-Custom-Header": "custom-value",
			},
		},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}

	if receivedHeaders.Get("X-Custom-Header") != "custom-value" {
		t.Error("Expected custom header to be sent")
	}
}

func TestHTTPChecker_Judge_PostRequest(t *testing.T) {
	var receivedBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		receivedBody = string(buf[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	checker := NewHTTPChecker()

	soul := &core.Soul{
		ID:     "test-http",
		Name:   "Test HTTP",
		Type:   core.CheckHTTP,
		Target: ts.URL,
		HTTP: &core.HTTPConfig{
			Method:      "POST",
			ValidStatus: []int{200},
			Body:        `{"key": "value"}`,
		},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}

	if receivedBody != `{"key": "value"}` {
		t.Errorf("Expected body to be sent, got %s", receivedBody)
	}
}

func TestHTTPChecker_Validate_MissingTarget(t *testing.T) {
	checker := NewHTTPChecker()

	soul := &core.Soul{
		ID:   "test-http",
		Name: "Test HTTP",
		Type: core.CheckHTTP,
	}

	err := checker.Validate(soul)
	if err == nil {
		t.Error("Expected validation error for missing target")
	}
}

func TestHTTPChecker_Validate_InvalidPrefix(t *testing.T) {
	checker := NewHTTPChecker()

	soul := &core.Soul{
		ID:     "test-http",
		Name:   "Test HTTP",
		Type:   core.CheckHTTP,
		Target: "ftp://example.com", // Invalid prefix
	}

	err := checker.Validate(soul)
	if err == nil {
		t.Error("Expected validation error for invalid URL prefix")
	}
}

func TestHTTPChecker_Validate_Valid(t *testing.T) {
	checker := NewHTTPChecker()

	soul := &core.Soul{
		ID:     "test-http",
		Name:   "Test HTTP",
		Type:   core.CheckHTTP,
		Target: "https://example.com",
	}

	err := checker.Validate(soul)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestHTTPChecker_Validate_InsecureSkipVerify(t *testing.T) {
	checker := NewHTTPChecker()

	soul := &core.Soul{
		ID:     "test-http",
		Name:   "Test HTTP",
		Type:   core.CheckHTTP,
		Target: "https://example.com",
		HTTP:   &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}, InsecureSkipVerify: true},
	}

	// Should pass validation (just logs a warning)
	err := checker.Validate(soul)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestHTTPChecker_GetTransport_CacheHit(t *testing.T) {
	checker := NewHTTPChecker()
	cfg := &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}}

	// First call creates transport
	t1 := checker.getTransport(cfg, 10*time.Second)
	if t1 == nil {
		t.Fatal("Expected non-nil transport")
	}

	// Second call should return cached transport (cache hit path)
	t2 := checker.getTransport(cfg, 10*time.Second)
	if t2 == nil {
		t.Fatal("Expected non-nil transport from cache")
	}

	// Should be the same instance (cache hit)
	if t1 != t2 {
		t.Error("Expected cached transport to be the same instance")
	}
}

func TestHTTPChecker_GetTransport_DifferentConfigs(t *testing.T) {
	checker := NewHTTPChecker()
	cfg1 := &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}, InsecureSkipVerify: false}
	cfg2 := &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}, InsecureSkipVerify: true}

	t1 := checker.getTransport(cfg1, 10*time.Second)
	t2 := checker.getTransport(cfg2, 10*time.Second)

	// Different configs should create different transports
	if t1 == t2 {
		t.Error("Expected different transports for different configs")
	}
}

func Test_extractTLSInfo(t *testing.T) {
	// Test with nil TLS state
	info := extractTLSInfo(nil)
	if info != nil {
		t.Errorf("Expected nil for nil state, got %v", info)
	}
}

// Test extractJSONPath with invalid JSON
func TestExtractJSONPath_InvalidJSON(t *testing.T) {
	invalidJSON := []byte(`{invalid json}`)
	result := extractJSONPath(invalidJSON, "name")
	if result != "" {
		t.Errorf("Expected empty string for invalid JSON, got %s", result)
	}
}

// Test extractJSONPath with array access (should return empty as not supported)
func TestExtractJSONPath_ArrayNotSupported(t *testing.T) {
	jsonData := []byte(`{"items": [1, 2, 3]}`)
	result := extractJSONPath(jsonData, "items.0")
	if result != "" {
		t.Logf("Array access returned: %s", result)
	}
}

// TestExtractJSONPath_AllTypes tests all type conversion branches
func TestExtractJSONPath_AllTypes(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		path     string
		expected string
	}{
		{"string", `{"name": "hello"}`, "name", "hello"},
		{"int", `{"count": 42}`, "count", "42"},
		{"float", `{"price": 3.14}`, "price", "3.14"},
		{"bool_true", `{"active": true}`, "active", "true"},
		{"bool_false", `{"active": false}`, "active", "false"},
		{"null", `{"value": null}`, "value", "null"},
		{"object", `{"nested": {"a": 1}}`, "nested", `{"a":1}`},
		{"missing_key", `{"name": "test"}`, "missing", ""},
		{"empty_path", `{"name": "test"}`, "", ""},
		{"dollar_prefix", `{"name": "test"}`, "$.name", "test"},
		{"deep_nested", `{"a": {"b": {"c": "deep"}}}`, "a.b.c", "deep"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJSONPath([]byte(tt.json), tt.path)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// TestValidateJSONSchema_AllBranches tests all validation branches
func TestValidateJSONSchema_AllBranches(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		schema   string
		strict   bool
		expected bool
	}{
		{
			name:     "invalid_schema_json",
			data:     `{"a": 1}`,
			schema:   `{invalid}`,
			expected: false,
		},
		{
			name:     "invalid_data_json",
			data:     `{invalid}`,
			schema:   `{"type": "object"}`,
			expected: false,
		},
		{
			name:     "type_match",
			data:     `{"name": "test"}`,
			schema:   `{"type": "object"}`,
			expected: true,
		},
		{
			name:     "type_mismatch",
			data:     `"just a string"`,
			schema:   `{"type": "object"}`,
			expected: false,
		},
		{
			name:     "required_fields_pass",
			data:     `{"name": "test", "email": "a@b.com"}`,
			schema:   `{"required": ["name", "email"]}`,
			expected: true,
		},
		{
			name:     "required_fields_fail",
			data:     `{"name": "test"}`,
			schema:   `{"required": ["name", "email"]}`,
			expected: false,
		},
		{
			name:     "properties_validation_pass",
			data:     `{"age": 25}`,
			schema:   `{"properties": {"age": {"type": "number"}}}`,
			expected: true,
		},
		{
			name:     "properties_validation_fail",
			data:     `{"age": "not a number"}`,
			schema:   `{"properties": {"age": {"type": "number"}}}`,
			expected: false,
		},
		{
			name:     "enum_pass",
			data:     `"active"`,
			schema:   `{"enum": ["active", "inactive"]}`,
			expected: true,
		},
		{
			name:     "enum_fail",
			data:     `"unknown"`,
			schema:   `{"enum": ["active", "inactive"]}`,
			expected: false,
		},
		{
			name:     "strict_additional_props_fail",
			data:     `{"name": "test", "extra": true}`,
			schema:   `{"properties": {"name": {"type": "string"}}}`,
			strict:   true,
			expected: false,
		},
		{
			name:     "strict_pass",
			data:     `{"name": "test"}`,
			schema:   `{"properties": {"name": {"type": "string"}}}`,
			strict:   true,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validateJSONSchema([]byte(tt.data), tt.schema, tt.strict)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// TestMatchesType_AllTypes tests all type matching branches
func TestMatchesType_AllTypes(t *testing.T) {
	tests := []struct {
		name     string
		data     interface{}
		typeName string
		expected bool
	}{
		{"object", map[string]interface{}{"a": 1}, "object", true},
		{"not_object", "string", "object", false},
		{"array", []interface{}{1, 2}, "array", true},
		{"not_array", "string", "array", false},
		{"string", "hello", "string", true},
		{"not_string", 42, "string", false},
		{"number", float64(3.14), "number", true},
		{"not_number", "string", "number", false},
		{"integer_whole", float64(42), "integer", true},
		{"integer_fractional", float64(3.14), "integer", false},
		{"not_integer", "string", "integer", false},
		{"boolean", true, "boolean", true},
		{"not_boolean", "string", "boolean", false},
		{"null", nil, "null", true},
		{"not_null", "string", "null", false},
		{"unknown_type", "anything", "custom", true}, // default case returns true
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesType(tt.data, tt.typeName)
			if result != tt.expected {
				t.Errorf("matchesType(%T, %q) = %v, want %v", tt.data, tt.typeName, result, tt.expected)
			}
		})
	}
}

func TestTransportCacheStats_NoHits(t *testing.T) {
	checker := NewHTTPChecker()
	hits, misses, ratio := checker.TransportCacheStats()
	if hits != 0 || misses != 0 || ratio != 0 {
		t.Errorf("Expected zero stats, got hits=%d, misses=%d, ratio=%f", hits, misses, ratio)
	}
}

func TestTransportCacheStats_WithHits(t *testing.T) {
	checker := NewHTTPChecker()
	checker.cacheHits.Store(3)
	checker.cacheMisses.Store(7)

	hits, misses, ratio := checker.TransportCacheStats()
	if hits != 3 {
		t.Errorf("Expected 3 hits, got %d", hits)
	}
	if misses != 7 {
		t.Errorf("Expected 7 misses, got %d", misses)
	}
	if ratio != 0.3 {
		t.Errorf("Expected ratio 0.3, got %f", ratio)
	}
}

func TestTransportCacheStats_OnlyHits(t *testing.T) {
	checker := NewHTTPChecker()
	checker.cacheHits.Store(5)
	checker.cacheMisses.Store(0)

	hits, misses, ratio := checker.TransportCacheStats()
	if hits != 5 {
		t.Errorf("Expected 5 hits, got %d", hits)
	}
	if misses != 0 {
		t.Errorf("Expected 0 misses, got %d", misses)
	}
	if ratio != 1.0 {
		t.Errorf("Expected ratio 1.0, got %f", ratio)
	}
}

func TestTransportCacheStats_OnlyMisses(t *testing.T) {
	checker := NewHTTPChecker()
	checker.cacheHits.Store(0)
	checker.cacheMisses.Store(5)

	hits, misses, ratio := checker.TransportCacheStats()
	if hits != 0 {
		t.Errorf("Expected 0 hits, got %d", hits)
	}
	if misses != 5 {
		t.Errorf("Expected 5 misses, got %d", misses)
	}
	if ratio != 0 {
		t.Errorf("Expected ratio 0.0, got %f", ratio)
	}
}
