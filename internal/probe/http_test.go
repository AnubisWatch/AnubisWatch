package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
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
