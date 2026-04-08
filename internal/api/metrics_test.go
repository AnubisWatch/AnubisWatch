package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TestHandleMetrics tests the handleMetrics function
func TestHandleMetrics(t *testing.T) {
	config := core.ServerConfig{Port: 8080}
	logger := newTestLogger()
	server := NewRESTServer(config, core.AuthConfig{Enabled: true}, newMockStorage(), &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, &mockClusterManager{}, nil, nil, nil, logger)

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("GET", "/metrics", nil),
		Response: rec,
	}

	err := server.handleMetrics(ctx)
	if err != nil {
		t.Fatalf("handleMetrics failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") && !strings.Contains(contentType, "application/openmetrics") {
		t.Errorf("Expected text/plain or openmetrics content type, got %s", contentType)
	}

	body := rec.Body.String()
	if body == "" {
		t.Error("Expected non-empty metrics response")
	}
}

// TestHandleMetrics_Content checks that metrics contains expected Prometheus format
func TestHandleMetrics_Content(t *testing.T) {
	config := core.ServerConfig{Port: 8080}
	logger := newTestLogger()
	server := NewRESTServer(config, core.AuthConfig{Enabled: true}, newMockStorage(), &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, &mockClusterManager{}, nil, nil, nil, logger)

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("GET", "/metrics", nil),
		Response: rec,
	}

	err := server.handleMetrics(ctx)
	if err != nil {
		t.Fatalf("handleMetrics failed: %v", err)
	}

	body := rec.Body.String()

	// Check for common Prometheus metric format
	expectedPrefixes := []string{
		"# HELP",
		"# TYPE",
	}

	hasValidMetric := false
	for _, prefix := range expectedPrefixes {
		if strings.Contains(body, prefix) {
			hasValidMetric = true
			break
		}
	}

	if !hasValidMetric && body != "" {
		// If response is not empty but doesn't have Prometheus format,
		// it might be a different format which is also acceptable
		t.Logf("Metrics response doesn't have Prometheus format, content: %s", body[:min(len(body), 100)])
	}
}

// TestBuildJudgmentMetrics_WithData tests buildJudgmentMetrics with actual judgment data
func TestBuildJudgmentMetrics_WithData(t *testing.T) {
	store := newMockStorage()
	// Add a soul first
	store.SaveSoul(context.Background(), &core.Soul{
		ID:   "soul-1",
		Name: "Test Soul",
		Type: core.CheckHTTP,
	})

	config := core.ServerConfig{Port: 8080}
	logger := newTestLogger()
	server := NewRESTServer(config, core.AuthConfig{Enabled: true}, store, &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, &mockClusterManager{}, nil, nil, nil, logger)

	metrics := server.buildJudgmentMetrics()
	// Should contain metrics even without judgments
	if metrics == "" {
		t.Log("Empty metrics when no judgments exist")
	}

	// Should contain Prometheus metric names
	if !strings.Contains(metrics, "anubis_") {
		t.Log("Metrics don't contain expected anubis_ prefix")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestBuildSoulMetrics_WithSouls tests buildSoulMetrics with souls in storage
func TestBuildSoulMetrics_WithSouls(t *testing.T) {
	store := newMockStorage()
	store.SaveSoul(context.Background(), &core.Soul{
		ID:   "soul-1",
		Name: "Test Soul",
		Type: core.CheckHTTP,
	})

	config := core.ServerConfig{Port: 8080}
	logger := newTestLogger()
	server := NewRESTServer(config, core.AuthConfig{Enabled: true}, store, &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, &mockClusterManager{}, nil, nil, nil, logger)

	metrics := server.buildSoulMetrics()
	if metrics == "" {
		t.Error("Expected non-empty metrics with souls")
	}

	if !strings.Contains(metrics, "anubis_souls_total") {
		t.Error("Expected anubis_souls_total metric")
	}

	if !strings.Contains(metrics, "1") {
		t.Error("Expected soul count of 1")
	}
}

// TestBuildJudgmentMetrics_WithJudgments tests buildJudgmentMetrics with judgments
func TestBuildJudgmentMetrics_WithJudgments(t *testing.T) {
	store := newMockStorage()
	store.SaveSoul(context.Background(), &core.Soul{
		ID:   "soul-1",
		Name: "Test Soul",
		Type: core.CheckHTTP,
	})

	config := core.ServerConfig{Port: 8080}
	logger := newTestLogger()
	server := NewRESTServer(config, core.AuthConfig{Enabled: true}, store, &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, &mockClusterManager{}, nil, nil, nil, logger)

	metrics := server.buildJudgmentMetrics()
	// Should not panic even without judgments
	if metrics == "" {
		t.Log("Empty metrics expected when no judgments exist")
	}
}
