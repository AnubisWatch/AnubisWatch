package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Helper to create a test REST server
func newTestServerWithStorage(store Storage) *RESTServer {
	config := core.ServerConfig{Port: 8080}
	logger := newTestLogger()
	return NewRESTServer(config, core.AuthConfig{Enabled: true}, store, &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, &mockClusterManager{}, nil, nil, nil, logger)
}

// TestHandleListJourneys tests handleListJourneys
func TestHandleListJourneys(t *testing.T) {
	store := newMockStorage()
	server := newTestServerWithStorage(store)

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:   httptest.NewRequest("GET", "/api/v1/journeys", nil),
		Response:  rec,
		Workspace: "default",
	}

	err := server.handleListJourneys(ctx)
	if err != nil {
		t.Fatalf("handleListJourneys failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

// TestHandleCreateJourney tests handleCreateJourney
func TestHandleCreateJourney(t *testing.T) {
	store := newMockStorage()
	server := newTestServerWithStorage(store)

	journey := core.JourneyConfig{
		Name: "Test Journey",
		Steps: []core.JourneyStep{
			{Name: "Step 1", Target: "http://example.com"},
		},
	}
	body, _ := json.Marshal(journey)

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:   httptest.NewRequest("POST", "/api/v1/journeys", bytes.NewReader(body)),
		Response:  rec,
		Workspace: "default",
	}

	err := server.handleCreateJourney(ctx)
	if err != nil {
		t.Fatalf("handleCreateJourney failed: %v", err)
	}

	if rec.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d", http.StatusCreated, rec.Code)
	}

	var result core.JourneyConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if result.Name != journey.Name {
		t.Errorf("Expected name %s, got %s", journey.Name, result.Name)
	}

	if result.ID == "" {
		t.Error("Expected journey ID to be generated")
	}
}

// TestHandleCreateJourney_InvalidData tests handleCreateJourney with invalid data
func TestHandleCreateJourney_InvalidData(t *testing.T) {
	store := newMockStorage()
	server := newTestServerWithStorage(store)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/journeys", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	ctx := &Context{
		Request:   req,
		Response:  rec,
		Workspace: "default",
	}

	err := server.handleCreateJourney(ctx)
	// Error may be returned or set in context, check both
	if err == nil && rec.Code != http.StatusBadRequest {
		t.Errorf("Expected error or bad request status, got status %d", rec.Code)
	}
}

// TestHandleUpdateJourney tests handleUpdateJourney
func TestHandleUpdateJourney(t *testing.T) {
	store := newMockStorage()
	server := newTestServerWithStorage(store)

	updated := core.JourneyConfig{
		Name: "Updated Name",
		Steps: []core.JourneyStep{
			{Name: "Updated Step", Target: "http://updated.com"},
		},
	}
	body, _ := json.Marshal(updated)

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:   httptest.NewRequest("PUT", "/api/v1/journeys/journey-1", bytes.NewReader(body)),
		Response:  rec,
		Params:    map[string]string{"id": "journey-1"},
		Workspace: "default",
	}

	err := server.handleUpdateJourney(ctx)
	if err != nil {
		t.Fatalf("handleUpdateJourney failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

// TestHandleUpdateJourney_InvalidData tests handleUpdateJourney with invalid data
func TestHandleUpdateJourney_InvalidData(t *testing.T) {
	store := newMockStorage()
	server := newTestServerWithStorage(store)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/journeys/journey-1", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	ctx := &Context{
		Request:  req,
		Response: rec,
		Params:   map[string]string{"id": "journey-1"},
	}

	err := server.handleUpdateJourney(ctx)
	if err == nil && rec.Code != http.StatusBadRequest {
		t.Errorf("Expected error or bad request status, got status %d", rec.Code)
	}
}

// TestHandleDeleteJourney tests handleDeleteJourney
func TestHandleDeleteJourney(t *testing.T) {
	store := newMockStorage()
	server := newTestServerWithStorage(store)

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("DELETE", "/api/v1/journeys/journey-1", nil),
		Response: rec,
		Params:   map[string]string{"id": "journey-1"},
	}

	// Just verify it doesn't panic
	_ = server.handleDeleteJourney(ctx)
}

// TestHandleMCPTools tests handleMCPTools
func TestHandleMCPTools(t *testing.T) {
	store := newMockStorage()
	server := newTestServerWithStorage(store)

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("GET", "/api/v1/mcp/tools", nil),
		Response: rec,
	}

	err := server.handleMCPTools(ctx)
	if err != nil {
		t.Fatalf("handleMCPTools failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var result []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if len(result) == 0 {
		t.Error("Expected at least one tool")
	}
}

// TestHandleSoulLogs tests handleSoulLogs
func TestHandleSoulLogs(t *testing.T) {
	store := newMockStorage()
	server := newTestServerWithStorage(store)

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("GET", "/api/v1/souls/soul-1/logs", nil),
		Response: rec,
		Params:   map[string]string{"id": "soul-1"},
	}

	err := server.handleSoulLogs(ctx)
	if err != nil {
		t.Fatalf("handleSoulLogs failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var result []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if len(result) == 0 {
		t.Error("Expected at least one log entry")
	}
}
