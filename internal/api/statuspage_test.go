package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TestHandleStatusPage tests the handleStatusPage endpoint
func TestHandleStatusPage(t *testing.T) {
	store := newMockStorage()
	store.SaveSoul(nil, &core.Soul{
		ID:   "soul-1",
		Name: "Test Soul",
		Type: core.CheckHTTP,
	})

	server := &RESTServer{
		store:  store,
		router: &Router{routes: make(map[string]map[string]Handler)},
		logger: newTestLogger(),
	}

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("GET", "/status", nil),
		Response: rec,
	}

	err := server.handleStatusPage(ctx)
	if err != nil {
		t.Fatalf("handleStatusPage failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "System Status") {
		t.Error("Expected 'System Status' in response")
	}

	if !strings.Contains(body, "components") {
		t.Error("Expected 'components' in response")
	}
}

// TestHandleStatusPage_WithJudgments tests status page with judgment data
func TestHandleStatusPage_DoesNotExposeMonitorTarget(t *testing.T) {
	store := newMockStorage()
	store.SaveSoul(nil, &core.Soul{
		ID:     "soul-private",
		Name:   "Private Service",
		Type:   core.CheckHTTP,
		Target: "http://internal-api.service.local:8080/health",
	})
	server := &RESTServer{store: store, router: &Router{routes: make(map[string]map[string]Handler)}, logger: newTestLogger()}

	for _, tc := range []struct {
		name    string
		handler func(*Context) error
	}{
		{name: "json", handler: server.handleStatusPage},
		{name: "html", handler: server.handleStatusPageHTML},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			ctx := &Context{Request: httptest.NewRequest("GET", "/status", nil), Response: rec}
			if err := tc.handler(ctx); err != nil {
				t.Fatalf("status handler failed: %v", err)
			}
			if strings.Contains(rec.Body.String(), "internal-api.service.local") {
				t.Fatalf("public status exposed private target: %s", rec.Body.String())
			}
		})
	}
}

func TestHandleStatusPage_WithJudgments(t *testing.T) {
	store := newMockStorage()
	store.SaveSoul(nil, &core.Soul{
		ID:   "soul-1",
		Name: "Test Soul",
		Type: core.CheckHTTP,
	})

	now := time.Now()
	store.SaveJudgment(nil, &core.Judgment{
		ID:        "judgment-1",
		SoulID:    "soul-1",
		Status:    core.SoulAlive,
		Duration:  100 * time.Millisecond,
		Timestamp: now,
	})

	server := &RESTServer{
		store:  store,
		router: &Router{routes: make(map[string]map[string]Handler)},
		logger: newTestLogger(),
	}

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("GET", "/status", nil),
		Response: rec,
	}

	err := server.handleStatusPage(ctx)
	if err != nil {
		t.Fatalf("handleStatusPage failed: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "operational") {
		t.Error("Expected 'operational' status in response")
	}
}

// TestHandleStatusPage_StorageError tests handleStatusPage when storage fails
func TestHandleStatusPage_StorageError(t *testing.T) {
	store := &failingMockStorage{}

	server := &RESTServer{
		store:  store,
		router: &Router{routes: make(map[string]map[string]Handler)},
		logger: newTestLogger(),
	}

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("GET", "/status", nil),
		Response: rec,
	}

	err := server.handleStatusPage(ctx)
	// The handler returns an HTTP error response, not a Go error
	if err == nil {
		// Check that an error response was written
		if rec.Code == http.StatusOK {
			t.Error("Expected non-OK status when storage fails")
		}
	}
}

// TestHandlePublicStatus tests the handlePublicStatus endpoint
func TestHandlePublicStatus(t *testing.T) {
	store := newMockStorage()
	store.SaveSoul(nil, &core.Soul{
		ID:   "soul-1",
		Name: "Test Soul",
		Type: core.CheckHTTP,
	})

	now := time.Now()
	store.SaveJudgment(nil, &core.Judgment{
		ID:        "judgment-1",
		SoulID:    "soul-1",
		Status:    core.SoulAlive,
		Duration:  100 * time.Millisecond,
		Timestamp: now,
	})

	server := &RESTServer{
		store:  store,
		router: &Router{routes: make(map[string]map[string]Handler)},
		logger: newTestLogger(),
	}

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("GET", "/public-status", nil),
		Response: rec,
	}

	err := server.handlePublicStatus(ctx)
	if err != nil {
		t.Fatalf("handlePublicStatus failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "status") {
		t.Error("Expected 'status' field in response")
	}

	if !strings.Contains(body, "operational") {
		t.Error("Expected 'operational' in response")
	}
}

// TestHandlePublicStatus_EmptyStore tests public status with no souls
func TestHandlePublicStatus_EmptyStore(t *testing.T) {
	store := newMockStorage()

	server := &RESTServer{
		store:  store,
		router: &Router{routes: make(map[string]map[string]Handler)},
		logger: newTestLogger(),
	}

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("GET", "/public-status", nil),
		Response: rec,
	}

	err := server.handlePublicStatus(ctx)
	if err != nil {
		t.Fatalf("handlePublicStatus failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "operational") {
		t.Error("Expected 'operational' status for empty store")
	}
}

// TestHandlePublicStatus_WithOutage tests public status with some outages
func TestHandlePublicStatus_WithOutage(t *testing.T) {
	store := newMockStorage()

	// Add healthy soul
	store.SaveSoul(nil, &core.Soul{
		ID:   "soul-1",
		Name: "Healthy Soul",
		Type: core.CheckHTTP,
	})

	// Add failing soul
	store.SaveSoul(nil, &core.Soul{
		ID:   "soul-2",
		Name: "Failing Soul",
		Type: core.CheckHTTP,
	})

	now := time.Now()
	store.SaveJudgment(nil, &core.Judgment{
		ID:        "judgment-1",
		SoulID:    "soul-1",
		Status:    core.SoulAlive,
		Duration:  100 * time.Millisecond,
		Timestamp: now.Add(-1 * time.Minute), // Use past time to ensure it's within range
	})
	store.SaveJudgment(nil, &core.Judgment{
		ID:        "judgment-2",
		SoulID:    "soul-2",
		Status:    core.SoulDead,
		Duration:  0,
		Timestamp: now.Add(-1 * time.Minute), // Use past time to ensure it's within range
	})

	server := &RESTServer{
		store:  store,
		router: &Router{routes: make(map[string]map[string]Handler)},
		logger: newTestLogger(),
	}

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("GET", "/public-status", nil),
		Response: rec,
	}

	err := server.handlePublicStatus(ctx)
	if err != nil {
		t.Fatalf("handlePublicStatus failed: %v", err)
	}

	body := rec.Body.String()
	t.Logf("Response body: %s", body)
	if !strings.Contains(body, "major_outage") {
		t.Error("Expected 'major_outage' status when souls are dead")
	}
}

// TestHandlePublicStatus_WithDegraded tests public status with degraded souls
func TestHandlePublicStatus_WithDegraded(t *testing.T) {
	store := newMockStorage()
	store.SaveSoul(nil, &core.Soul{
		ID:   "soul-1",
		Name: "Test Soul",
		Type: core.CheckHTTP,
	})

	now := time.Now()
	store.SaveJudgment(nil, &core.Judgment{
		ID:        "judgment-1",
		SoulID:    "soul-1",
		Status:    core.SoulDegraded,
		Duration:  500 * time.Millisecond,
		Timestamp: now,
	})

	server := &RESTServer{
		store:  store,
		router: &Router{routes: make(map[string]map[string]Handler)},
		logger: newTestLogger(),
	}

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("GET", "/public-status", nil),
		Response: rec,
	}

	err := server.handlePublicStatus(ctx)
	if err != nil {
		t.Fatalf("handlePublicStatus failed: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "degraded") {
		t.Error("Expected 'degraded' status when souls are degraded")
	}
}

// TestHandlePublicStatus_StorageError tests public status when storage fails
func TestHandlePublicStatus_StorageError(t *testing.T) {
	store := &failingMockStorage{}

	server := &RESTServer{
		store:  store,
		router: &Router{routes: make(map[string]map[string]Handler)},
		logger: newTestLogger(),
	}

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("GET", "/public-status", nil),
		Response: rec,
	}

	err := server.handlePublicStatus(ctx)
	if err != nil {
		t.Fatalf("handlePublicStatus should not error on storage failure: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "unknown") {
		t.Error("Expected 'unknown' status when storage fails")
	}
}

// TestHandleStatusPageHTML tests the handleStatusPageHTML endpoint
func TestHandleStatusPageHTML(t *testing.T) {
	store := newMockStorage()
	store.SaveSoul(nil, &core.Soul{
		ID:   "soul-1",
		Name: "Test Soul",
		Type: core.CheckHTTP,
	})

	server := &RESTServer{
		store:  store,
		router: &Router{routes: make(map[string]map[string]Handler)},
		logger: newTestLogger(),
	}

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("GET", "/status-page", nil),
		Response: rec,
	}

	err := server.handleStatusPageHTML(ctx)
	if err != nil {
		t.Fatalf("handleStatusPageHTML failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("Expected text/html content type, got %s", contentType)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("Expected HTML DOCTYPE in response")
	}

	if !strings.Contains(body, "System Status") {
		t.Error("Expected 'System Status' in HTML response")
	}
}

// TestHandleStatusPageHTML_EmptyStore tests HTML status page with no souls
func TestHandleStatusPageHTML_EmptyStore(t *testing.T) {
	store := newMockStorage()

	server := &RESTServer{
		store:  store,
		router: &Router{routes: make(map[string]map[string]Handler)},
		logger: newTestLogger(),
	}

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("GET", "/status-page", nil),
		Response: rec,
	}

	err := server.handleStatusPageHTML(ctx)
	if err != nil {
		t.Fatalf("handleStatusPageHTML failed: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "All Systems Operational") {
		t.Error("Expected 'All Systems Operational' for empty store")
	}
}

// TestHandleStatusPageHTML_WithVariousStatuses tests HTML page with different soul statuses
func TestHandleStatusPageHTML_WithVariousStatuses(t *testing.T) {
	store := newMockStorage()

	// Add multiple souls with different statuses
	store.SaveSoul(nil, &core.Soul{
		ID:   "soul-1",
		Name: "Healthy Soul",
		Type: core.CheckHTTP,
	})
	store.SaveSoul(nil, &core.Soul{
		ID:   "soul-2",
		Name: "Degraded Soul",
		Type: core.CheckTCP,
	})
	store.SaveSoul(nil, &core.Soul{
		ID:   "soul-3",
		Name: "Failing Soul",
		Type: core.CheckDNS,
	})

	now := time.Now()
	store.SaveJudgment(nil, &core.Judgment{
		ID:        "judgment-1",
		SoulID:    "soul-1",
		Status:    core.SoulAlive,
		Timestamp: now,
	})
	store.SaveJudgment(nil, &core.Judgment{
		ID:        "judgment-2",
		SoulID:    "soul-2",
		Status:    core.SoulDegraded,
		Timestamp: now,
	})
	store.SaveJudgment(nil, &core.Judgment{
		ID:        "judgment-3",
		SoulID:    "soul-3",
		Status:    core.SoulDead,
		Timestamp: now,
	})

	server := &RESTServer{
		store:  store,
		router: &Router{routes: make(map[string]map[string]Handler)},
		logger: newTestLogger(),
	}

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("GET", "/status-page", nil),
		Response: rec,
	}

	err := server.handleStatusPageHTML(ctx)
	if err != nil {
		t.Fatalf("handleStatusPageHTML failed: %v", err)
	}

	body := rec.Body.String()

	// Should show degraded status (not major outage since only 1 of 3 is dead)
	if !strings.Contains(body, "Degraded Performance") && !strings.Contains(body, "Major System Outage") {
		t.Error("Expected appropriate status class in HTML")
	}

	// Should contain component entries
	if !strings.Contains(body, "Healthy Soul") {
		t.Error("Expected 'Healthy Soul' in HTML")
	}

	if !strings.Contains(body, "Degraded Soul") {
		t.Error("Expected 'Degraded Soul' in HTML")
	}

	if !strings.Contains(body, "Failing Soul") {
		t.Error("Expected 'Failing Soul' in HTML")
	}
}

// TestHandleStatusPage_WithDeadSouls tests major_outage status
func TestHandleStatusPage_WithDeadSouls(t *testing.T) {
	store := newMockStorage()
	store.SaveSoul(nil, &core.Soul{
		ID:   "soul-dead",
		Name: "Dead Soul",
		Type: core.CheckHTTP,
	})

	// Use a timestamp slightly in the past to ensure it falls within the handler's time window
	judgmentTime := time.Now().Add(-1 * time.Second)
	store.SaveJudgment(nil, &core.Judgment{
		ID:        "judgment-dead",
		SoulID:    "soul-dead",
		Status:    core.SoulDead,
		Duration:  500 * time.Millisecond,
		Timestamp: judgmentTime,
	})

	server := &RESTServer{
		store:  store,
		router: &Router{routes: make(map[string]map[string]Handler)},
		logger: newTestLogger(),
	}

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("GET", "/status", nil),
		Response: rec,
	}

	err := server.handleStatusPage(ctx)
	if err != nil {
		t.Fatalf("handleStatusPage failed: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "major_outage") {
		t.Errorf("Expected 'major_outage' in response, got: %s", body)
	}
}

// TestHandleStatusPage_MixedStatuses tests degraded + operational
func TestHandleStatusPage_MixedStatuses(t *testing.T) {
	store := newMockStorage()

	store.SaveSoul(nil, &core.Soul{ID: "soul-ok", Name: "OK Soul", Type: core.CheckHTTP})
	store.SaveSoul(nil, &core.Soul{ID: "soul-degraded", Name: "Degraded Soul", Type: core.CheckTCP})

	judgmentTime := time.Now().Add(-1 * time.Second)
	store.SaveJudgment(nil, &core.Judgment{ID: "j-ok", SoulID: "soul-ok", Status: core.SoulAlive, Timestamp: judgmentTime})
	store.SaveJudgment(nil, &core.Judgment{ID: "j-degraded", SoulID: "soul-degraded", Status: core.SoulDegraded, Timestamp: judgmentTime})

	server := &RESTServer{
		store:  store,
		router: &Router{routes: make(map[string]map[string]Handler)},
		logger: newTestLogger(),
	}

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("GET", "/status", nil),
		Response: rec,
	}

	err := server.handleStatusPage(ctx)
	if err != nil {
		t.Fatalf("handleStatusPage failed: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "degraded_performance") {
		t.Errorf("Expected 'degraded_performance' in response, got: %s", body)
	}
}

// TestHandleStatusPageHTML_StorageError tests HTML handler when store fails
func TestHandleStatusPageHTML_StorageError(t *testing.T) {
	store := &failingMockStorage{}

	server := &RESTServer{
		store:  store,
		router: &Router{routes: make(map[string]map[string]Handler)},
		logger: newTestLogger(),
	}

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("GET", "/status-page", nil),
		Response: rec,
	}

	err := server.handleStatusPageHTML(ctx)
	if err != nil {
		t.Fatalf("handleStatusPageHTML should not error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}
