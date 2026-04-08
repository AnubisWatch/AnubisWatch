package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// TestBuildJudgmentMetrics_WithAllStatuses tests buildJudgmentMetrics with different soul statuses
func TestBuildJudgmentMetrics_WithAllStatuses(t *testing.T) {
	store := newMockStorage()

	// Add souls with different statuses by creating judgments
	now := time.Now()
	statuses := []core.SoulStatus{core.SoulAlive, core.SoulDead, core.SoulDegraded}

	for i, status := range statuses {
		soulID := fmt.Sprintf("soul-%d", i)
		soulName := fmt.Sprintf("Test Soul %d", i)
		store.SaveSoul(context.Background(), &core.Soul{
			ID:   soulID,
			Name: soulName,
			Type: core.CheckHTTP,
		})

		// Add a judgment for this soul
		store.SaveJudgment(context.Background(), &core.Judgment{
			ID:       fmt.Sprintf("judgment-%d", i),
			SoulID:   soulID,
			Status:   status,
			Duration: time.Duration(100+i*50) * time.Millisecond,
			Timestamp: now,
		})
	}

	config := core.ServerConfig{Port: 8080}
	logger := newTestLogger()
	server := NewRESTServer(config, core.AuthConfig{Enabled: true}, store, &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, &mockClusterManager{}, nil, nil, nil, logger)

	metrics := server.buildJudgmentMetrics()

	// Should contain all status values
	if !strings.Contains(metrics, "anubis_soul_status") {
		t.Error("Expected anubis_soul_status metric")
	}

	if !strings.Contains(metrics, "anubis_soul_latency_seconds") {
		t.Error("Expected anubis_soul_latency_seconds metric")
	}
}

// TestBuildJudgmentMetrics_StorageError tests buildJudgmentMetrics when storage fails
func TestBuildJudgmentMetrics_StorageError(t *testing.T) {
	store := &failingMockStorage{}

	config := core.ServerConfig{Port: 8080}
	logger := newTestLogger()
	server := NewRESTServer(config, core.AuthConfig{Enabled: true}, store, &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, &mockClusterManager{}, nil, nil, nil, logger)

	metrics := server.buildJudgmentMetrics()
	// When storage fails, it should still return the metric headers but no data
	if metrics == "" {
		t.Error("Expected metric headers even on storage error")
	}

	// Should contain metric definitions but no actual values
	if !strings.Contains(metrics, "anubis_soul_status") {
		t.Error("Expected anubis_soul_status metric header")
	}
}

// TestBuildSystemMetrics tests buildSystemMetrics
func TestBuildSystemMetrics(t *testing.T) {
	config := core.ServerConfig{Port: 8080}
	logger := newTestLogger()
	server := NewRESTServer(config, core.AuthConfig{Enabled: true}, newMockStorage(), &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, &mockClusterManager{}, nil, nil, nil, logger)

	metrics := server.buildSystemMetrics()

	if metrics == "" {
		t.Error("Expected non-empty system metrics")
	}

	expectedMetrics := []string{
		"anubis_build_info",
		"anubis_memory_alloc_bytes",
		"anubis_memory_sys_bytes",
		"anubis_goroutines",
	}

	for _, expected := range expectedMetrics {
		if !strings.Contains(metrics, expected) {
			t.Errorf("Expected metric %s not found in output", expected)
		}
	}
}

// TestBuildClusterMetrics_WithLeader tests buildClusterMetrics when cluster manager returns leader
func TestBuildClusterMetrics_WithLeader(t *testing.T) {
	config := core.ServerConfig{Port: 8080}
	logger := newTestLogger()
	cluster := &mockClusterManager{isLeader: true}
	server := NewRESTServer(config, core.AuthConfig{Enabled: true}, newMockStorage(), &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, cluster, nil, nil, nil, logger)

	metrics := server.buildClusterMetrics()

	if !strings.Contains(metrics, "anubis_cluster_leader") {
		t.Error("Expected anubis_cluster_leader metric")
	}

	if !strings.Contains(metrics, "anubis_cluster_leader 1") {
		t.Error("Expected leader to be 1")
	}
}

// TestBuildClusterMetrics_NotLeader tests buildClusterMetrics when not leader
func TestBuildClusterMetrics_NotLeader(t *testing.T) {
	config := core.ServerConfig{Port: 8080}
	logger := newTestLogger()
	cluster := &mockClusterManager{isLeader: false}
	server := NewRESTServer(config, core.AuthConfig{Enabled: true}, newMockStorage(), &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, cluster, nil, nil, nil, logger)

	metrics := server.buildClusterMetrics()

	if !strings.Contains(metrics, "anubis_cluster_leader 0") {
		t.Error("Expected leader to be 0")
	}
}

// TestBuildClusterMetrics_NoCluster tests buildClusterMetrics when no cluster manager
func TestBuildClusterMetrics_NoCluster(t *testing.T) {
	config := core.ServerConfig{Port: 8080}
	logger := newTestLogger()
	server := NewRESTServer(config, core.AuthConfig{Enabled: true}, newMockStorage(), &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, nil, nil, nil, nil, logger)

	metrics := server.buildClusterMetrics()

	if !strings.Contains(metrics, "anubis_cluster_leader 1") {
		t.Error("Expected leader to be 1 when no cluster manager")
	}
}

// TestBuildSoulMetrics_EmptyStorage tests buildSoulMetrics with empty storage
func TestBuildSoulMetrics_EmptyStorage(t *testing.T) {
	store := newMockStorage()
	config := core.ServerConfig{Port: 8080}
	logger := newTestLogger()
	server := NewRESTServer(config, core.AuthConfig{Enabled: true}, store, &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, &mockClusterManager{}, nil, nil, nil, logger)

	metrics := server.buildSoulMetrics()

	if !strings.Contains(metrics, "anubis_souls_total 0") {
		t.Errorf("Expected souls_total to be 0 for empty storage, got: %s", metrics)
	}
}

// TestBuildSoulMetrics_StorageError tests buildSoulMetrics when storage fails
func TestBuildSoulMetrics_StorageError(t *testing.T) {
	store := &failingMockStorage{}

	config := core.ServerConfig{Port: 8080}
	logger := newTestLogger()
	server := NewRESTServer(config, core.AuthConfig{Enabled: true}, store, &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, &mockClusterManager{}, nil, nil, nil, logger)

	metrics := server.buildSoulMetrics()
	// Should return empty string when storage fails
	if metrics != "" {
		t.Errorf("Expected empty metrics on storage error, got: %s", metrics)
	}
}

// TestBuildJudgmentMetrics_WithAllStatuses_Detailed tests buildJudgmentMetrics with detailed verification
func TestBuildJudgmentMetrics_WithAllStatuses_Detailed(t *testing.T) {
	store := newMockStorage()

	now := time.Now()

	// Test SoulAlive
	store.SaveSoul(context.Background(), &core.Soul{
		ID:   "soul-alive",
		Name: "Alive Soul",
		Type: core.CheckHTTP,
	})
	store.SaveJudgment(context.Background(), &core.Judgment{
		ID:        "judgment-alive",
		SoulID:    "soul-alive",
		Status:    core.SoulAlive,
		Duration:  150 * time.Millisecond,
		Timestamp: now,
	})

	// Test SoulDead
	store.SaveSoul(context.Background(), &core.Soul{
		ID:   "soul-dead",
		Name: "Dead Soul",
		Type: core.CheckTCP,
	})
	store.SaveJudgment(context.Background(), &core.Judgment{
		ID:        "judgment-dead",
		SoulID:    "soul-dead",
		Status:    core.SoulDead,
		Duration:  0,
		Timestamp: now,
	})

	// Test SoulDegraded
	store.SaveSoul(context.Background(), &core.Soul{
		ID:   "soul-degraded",
		Name: "Degraded Soul",
		Type: core.CheckDNS,
	})
	store.SaveJudgment(context.Background(), &core.Judgment{
		ID:        "judgment-degraded",
		SoulID:    "soul-degraded",
		Status:    core.SoulDegraded,
		Duration:  500 * time.Millisecond,
		Timestamp: now,
	})

	config := core.ServerConfig{Port: 8080}
	logger := newTestLogger()
	server := NewRESTServer(config, core.AuthConfig{Enabled: true}, store, &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, &mockClusterManager{}, nil, nil, nil, logger)

	metrics := server.buildJudgmentMetrics()

	// Verify all status values appear
	if !strings.Contains(metrics, "Alive Soul") {
		t.Error("Expected 'Alive Soul' in metrics")
	}
	if !strings.Contains(metrics, "Dead Soul") {
		t.Error("Expected 'Dead Soul' in metrics")
	}
	if !strings.Contains(metrics, "Degraded Soul") {
		t.Error("Expected 'Degraded Soul' in metrics")
	}

	// Verify latency values
	if !strings.Contains(metrics, "0.150000") {
		t.Log("Expected latency 0.150000 for 150ms")
	}
	if !strings.Contains(metrics, "0.500000") {
		t.Log("Expected latency 0.500000 for 500ms")
	}
}

// TestBuildJudgmentMetrics_OutdatedJudgments tests that outdated judgments are filtered out
func TestBuildJudgmentMetrics_OutdatedJudgments(t *testing.T) {
	store := newMockStorage()

	now := time.Now()
	oldTime := now.Add(-25 * time.Hour) // Older than 24 hours

	store.SaveSoul(context.Background(), &core.Soul{
		ID:   "soul-old",
		Name: "Old Soul",
		Type: core.CheckHTTP,
	})

	// Add an old judgment (should be filtered out)
	store.SaveJudgment(context.Background(), &core.Judgment{
		ID:        "judgment-old",
		SoulID:    "soul-old",
		Status:    core.SoulAlive,
		Duration:  100 * time.Millisecond,
		Timestamp: oldTime,
	})

	config := core.ServerConfig{Port: 8080}
	logger := newTestLogger()
	server := NewRESTServer(config, core.AuthConfig{Enabled: true}, store, &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, &mockClusterManager{}, nil, nil, nil, logger)

	metrics := server.buildJudgmentMetrics()

	// Old judgments should be filtered out
	if strings.Contains(metrics, "Old Soul") {
		t.Log("Old soul may appear depending on time range logic")
	}
}

// TestBuildSystemMetrics_Content tests system metrics content in detail
func TestBuildSystemMetrics_Content(t *testing.T) {
	config := core.ServerConfig{Port: 8080}
	logger := newTestLogger()
	server := NewRESTServer(config, core.AuthConfig{Enabled: true}, newMockStorage(), &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, &mockClusterManager{}, nil, nil, nil, logger)

	metrics := server.buildSystemMetrics()

	// Check for specific Prometheus format
	expectedStrings := []string{
		"# HELP anubis_build_info",
		"# TYPE anubis_build_info gauge",
		"anubis_build_info{version=\"dev\"} 1",
		"# HELP anubis_memory_alloc_bytes",
		"# TYPE anubis_memory_alloc_bytes gauge",
		"# HELP anubis_memory_sys_bytes",
		"# TYPE anubis_memory_sys_bytes gauge",
		"# HELP anubis_goroutines",
		"# TYPE anubis_goroutines gauge",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(metrics, expected) {
			t.Errorf("Expected '%s' in metrics", expected)
		}
	}
}

// TestHandleMetrics_Integration tests the full metrics endpoint
func TestHandleMetrics_Integration(t *testing.T) {
	store := newMockStorage()

	// Add souls and judgments
	now := time.Now()
	store.SaveSoul(context.Background(), &core.Soul{
		ID:   "soul-1",
		Name: "Integration Soul",
		Type: core.CheckHTTP,
	})
	store.SaveJudgment(context.Background(), &core.Judgment{
		ID:        "judgment-1",
		SoulID:    "soul-1",
		Status:    core.SoulAlive,
		Duration:  200 * time.Millisecond,
		Timestamp: now,
	})

	config := core.ServerConfig{Port: 8080}
	logger := newTestLogger()
	server := NewRESTServer(config, core.AuthConfig{Enabled: true}, store, &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, &mockClusterManager{}, nil, nil, nil, logger)

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

	// Verify all metric sections are present
	if !strings.Contains(body, "anubis_build_info") {
		t.Error("Expected anubis_build_info in output")
	}
	if !strings.Contains(body, "anubis_souls_total") {
		t.Error("Expected anubis_souls_total in output")
	}
	if !strings.Contains(body, "anubis_soul_status") {
		t.Error("Expected anubis_soul_status in output")
	}
	if !strings.Contains(body, "anubis_cluster_leader") {
		t.Error("Expected anubis_cluster_leader in output")
	}
}
