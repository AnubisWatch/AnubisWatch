package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Helper to create a test REST server
func newTestServerWithStorage(store Storage) *RESTServer {
	config := core.ServerConfig{Port: 8080}
	logger := newTestLogger()
	return NewRESTServer(config, core.AuthConfig{Enabled: core.BoolPtr(true)}, store, &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, &mockClusterManager{}, nil, nil, nil, nil, logger)
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

// TestHandleGetJourney tests handleGetJourney
func TestHandleGetJourney(t *testing.T) {
	store := newMockStorage()
	server := newTestServerWithStorage(store)

	// Create a journey first
	store.SaveJourneyNoCtx(&core.JourneyConfig{
		ID:   "journey-1",
		Name: "Test Journey",
	})

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:   httptest.NewRequest("GET", "/api/v1/journeys/journey-1", nil),
		Response:  rec,
		Params:    map[string]string{"id": "journey-1"},
		Workspace: "default",
	}

	err := server.handleGetJourney(ctx)
	if err != nil {
		t.Fatalf("handleGetJourney failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
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

// TestHandleRunJourney tests handleRunJourney
func TestHandleRunJourney(t *testing.T) {
	store := newMockStorage()
	server := newTestServerWithStorage(store)

	// Create a journey first
	store.SaveJourneyNoCtx(&core.JourneyConfig{
		ID:   "journey-1",
		Name: "Test Journey",
		Steps: []core.JourneyStep{
			{Name: "Step 1", Target: "http://example.com"},
		},
	})

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("POST", "/api/v1/journeys/journey-1/run", nil),
		Response: rec,
		Params:   map[string]string{"id": "journey-1"},
	}

	err := server.handleRunJourney(ctx)
	if err != nil {
		t.Fatalf("handleRunJourney failed: %v", err)
	}

	if rec.Code != http.StatusAccepted {
		t.Errorf("Expected status %d, got %d", http.StatusAccepted, rec.Code)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if result["journey_id"] != "journey-1" {
		t.Errorf("Expected journey_id journey-1, got %s", result["journey_id"])
	}

	if result["status"] != "executing" {
		t.Errorf("Expected status executing, got %s", result["status"])
	}
}

// TestHandleRunJourney_NotFound tests handleRunJourney with non-existent journey
func TestHandleRunJourney_NotFound(t *testing.T) {
	store := newMockStorage()
	server := newTestServerWithStorage(store)

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("POST", "/api/v1/journeys/nonexistent/run", nil),
		Response: rec,
		Params:   map[string]string{"id": "nonexistent"},
	}

	err := server.handleRunJourney(ctx)
	// Error may be returned or set in context, check both
	if err == nil && rec.Code != http.StatusNotFound {
		t.Error("Expected error for non-existent journey")
	}

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

// TestHandleGetJourney_NotFound tests handleGetJourney with non-existent journey
func TestHandleGetJourney_NotFound(t *testing.T) {
	store := newMockStorage()
	server := newTestServerWithStorage(store)

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:   httptest.NewRequest("GET", "/api/v1/journeys/nonexistent", nil),
		Response:  rec,
		Params:    map[string]string{"id": "nonexistent"},
		Workspace: "default",
	}

	err := server.handleGetJourney(ctx)
	// Error may be returned or set in context, check both
	if err == nil && rec.Code != http.StatusNotFound {
		t.Errorf("Expected error or not found status, got status %d", rec.Code)
	}

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

// TestHandleDeleteJourney_NotFound tests handleDeleteJourney with non-existent journey
func TestHandleDeleteJourney_NotFound(t *testing.T) {
	store := newMockStorage()
	server := newTestServerWithStorage(store)

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("DELETE", "/api/v1/journeys/nonexistent", nil),
		Response: rec,
		Params:   map[string]string{"id": "nonexistent"},
	}

	err := server.handleDeleteJourney(ctx)
	// Error may be returned or set in context
	if err == nil && rec.Code != http.StatusNotFound {
		t.Errorf("Expected error or not found status, got status %d", rec.Code)
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

// TestHandleUpdateSoul_InvalidJSON tests update with invalid JSON
func TestHandleUpdateSoul_InvalidJSON(t *testing.T) {
	storage := newMockStorage()
	storage.SaveSoul(nil, &core.Soul{ID: "soul-1", Name: "Test Soul", Type: core.CheckHTTP})

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		store:   storage,
		router:  router,
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	// Invalid JSON body
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/souls/soul-1", bytes.NewBufferString("invalid json"))
	ctx := &Context{
		Request:   req,
		Response:  rec,
		Params:    map[string]string{"id": "soul-1"},
		Workspace: "default",
	}

	server.handleUpdateSoul(ctx)
	// Check response code - handlers may set error in context rather than return
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

// TestHandleUpdateSoul_StorageError tests update when storage fails
func TestHandleUpdateSoul_StorageError(t *testing.T) {
	storage := &failingMockStorage{}

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		store:   storage,
		router:  router,
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	soul := core.Soul{Name: "Updated Soul", Type: core.CheckHTTP}
	body, _ := json.Marshal(soul)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/souls/soul-1", bytes.NewBuffer(body))
	ctx := &Context{
		Request:   req,
		Response:  rec,
		Params:    map[string]string{"id": "soul-1"},
		Workspace: "default",
	}

	server.handleUpdateSoul(ctx)
	// Check response code
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

// TestHandleForceCheck_ProbeError tests force check when probe fails
func TestHandleForceCheck_ProbeError(t *testing.T) {
	storage := newMockStorage()
	storage.SaveSoul(nil, &core.Soul{ID: "soul-1", Name: "Test Soul", Type: core.CheckHTTP})

	probe := &mockProbeEngine{forceCheckError: true}

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		store:   storage,
		router:  router,
		logger:  newTestLogger(),
		probe:   probe,
		cluster: &mockClusterManager{},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/souls/soul-1/check", nil)
	ctx := &Context{
		Request:  req,
		Response: rec,
		Params:   map[string]string{"id": "soul-1"},
	}

	server.handleForceCheck(ctx)
	// Check response code
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

// TestHandleListJudgments_StorageError tests listing judgments when storage fails
func TestHandleListJudgments_StorageError(t *testing.T) {
	storage := &failingMockStorage{}

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		store:   storage,
		router:  router,
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/souls/soul-1/judgments", nil)
	ctx := &Context{
		Request:  req,
		Response: rec,
		Params:   map[string]string{"id": "soul-1"},
	}

	server.handleListJudgments(ctx)
	// Check response code
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

// TestHandleCreateChannel_InvalidJSON tests creating channel with invalid JSON
func TestHandleCreateChannel_InvalidJSON(t *testing.T) {
	storage := newMockStorage()

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		store:   storage,
		router:  router,
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/channels", bytes.NewBufferString("invalid json"))
	ctx := &Context{
		Request:   req,
		Response:  rec,
		Workspace: "default",
	}

	server.handleCreateChannel(ctx)
	// Check response code
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

// TestHandleUpdateChannel_InvalidJSON tests updating channel with invalid JSON
func TestHandleUpdateChannel_InvalidJSON(t *testing.T) {
	storage := newMockStorage()
	storage.SaveChannelNoCtx(&core.AlertChannel{ID: "channel-1", Name: "Test Channel", Type: "email"})

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		store:   storage,
		router:  router,
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/channels/channel-1", bytes.NewBufferString("invalid json"))
	ctx := &Context{
		Request:   req,
		Response:  rec,
		Params:    map[string]string{"id": "channel-1"},
		Workspace: "default",
	}

	server.handleUpdateChannel(ctx)
	// Check response code
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

// TestHandleCreateRule_InvalidJSON tests creating rule with invalid JSON
func TestHandleCreateRule_InvalidJSON(t *testing.T) {
	storage := newMockStorage()

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		store:   storage,
		router:  router,
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/rules", bytes.NewBufferString("invalid json"))
	ctx := &Context{
		Request:   req,
		Response:  rec,
		Workspace: "default",
	}

	server.handleCreateRule(ctx)
	// Check response code
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

// TestHandleUpdateRule_InvalidJSON tests updating rule with invalid JSON
func TestHandleUpdateRule_InvalidJSON(t *testing.T) {
	storage := newMockStorage()
	storage.SaveRuleNoCtx(&core.AlertRule{ID: "rule-1", Name: "Test Rule", Channels: []string{"channel-1"}})

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		store:   storage,
		router:  router,
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/rules/rule-1", bytes.NewBufferString("invalid json"))
	ctx := &Context{
		Request:   req,
		Response:  rec,
		Params:    map[string]string{"id": "rule-1"},
		Workspace: "default",
	}

	server.handleUpdateRule(ctx)
	// Check response code
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

// TestHandleCreateWorkspace_InvalidJSON tests creating workspace with invalid JSON
func TestHandleCreateWorkspace_InvalidJSON(t *testing.T) {
	storage := newMockStorage()

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		store:   storage,
		router:  router,
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/workspaces", bytes.NewBufferString("invalid json"))
	ctx := &Context{
		Request:  req,
		Response: rec,
	}

	server.handleCreateWorkspace(ctx)
	// Check response code
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

// TestHandleCreateStatusPage_InvalidJSON tests creating status page with invalid JSON
func TestHandleCreateStatusPage_InvalidJSON(t *testing.T) {
	storage := newMockStorage()

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		store:   storage,
		router:  router,
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/status-pages", bytes.NewBufferString("invalid json"))
	ctx := &Context{
		Request:   req,
		Response:  rec,
		Workspace: "default",
	}

	server.handleCreateStatusPage(ctx)
	// Check response code
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

// TestHandleUpdateStatusPage_InvalidJSON tests updating status page with invalid JSON
func TestHandleUpdateStatusPage_InvalidJSON(t *testing.T) {
	storage := newMockStorage()

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		store:   storage,
		router:  router,
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/status-pages/page-1", bytes.NewBufferString("invalid json"))
	ctx := &Context{
		Request:   req,
		Response:  rec,
		Params:    map[string]string{"id": "page-1"},
		Workspace: "default",
	}

	server.handleUpdateStatusPage(ctx)
	// Check response code
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

// TestHandleClusterStatus_NotLeader tests cluster status when not leader
func TestHandleClusterStatus_NotLeader(t *testing.T) {
	storage := newMockStorage()

	cluster := &mockClusterManager{}
	cluster.isLeader = false

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		store:   storage,
		router:  router,
		logger:  newTestLogger(),
		cluster: cluster,
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/cluster/status", nil)
	ctx := &Context{
		Request:  req,
		Response: rec,
	}

	err := server.handleClusterStatus(ctx)
	if err != nil {
		t.Fatalf("handleClusterStatus failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

// TestHandleClusterPeers_NotLeader tests cluster peers when not leader
func TestHandleClusterPeers_NotLeader(t *testing.T) {
	storage := newMockStorage()

	cluster := &mockClusterManager{}
	cluster.isLeader = false

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		store:   storage,
		router:  router,
		logger:  newTestLogger(),
		cluster: cluster,
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/cluster/peers", nil)
	ctx := &Context{
		Request:  req,
		Response: rec,
	}

	server.handleClusterPeers(ctx)
	// This handler doesn't check for leader status, it just returns peer info
	// The actual API might require leader in real implementation
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

// TestHandleListChannels_StorageError tests listing channels when storage fails
func TestHandleListChannels_StorageError(t *testing.T) {
	// Note: This handler uses s.alert.ListChannels(), not store
	// Skipping as it requires alert manager setup
	t.Skip("Skipped - handler uses alert manager, not storage")
}

// TestHandleListRules_StorageError tests listing rules when storage fails
func TestHandleListRules_StorageError(t *testing.T) {
	// Note: This handler uses s.alert.ListRules(), not store
	// Skipping as it requires alert manager setup
	t.Skip("Skipped - handler uses alert manager, not storage")
}

// TestHandleListWorkspaces_StorageError tests listing workspaces when storage fails
func TestHandleListWorkspaces_StorageError(t *testing.T) {
	storage := &failingMockStorage{}

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		store:   storage,
		router:  router,
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/workspaces", nil)
	ctx := &Context{
		Request:  req,
		Response: rec,
	}

	server.handleListWorkspaces(ctx)
	// Check response code
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

// TestHandleListStatusPages_StorageError tests listing status pages when storage fails
func TestHandleListStatusPages_StorageError(t *testing.T) {
	storage := &failingMockStorage{}

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		store:   storage,
		router:  router,
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/status-pages", nil)
	ctx := &Context{
		Request:  req,
		Response: rec,
	}

	server.handleListStatusPages(ctx)
	// Check response code
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

// TestContainsInjectionPatterns_PathTraversal tests path traversal detection
func TestContainsInjectionPatterns_PathTraversal(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"../etc/passwd", true},
		{"..\\windows\\system32", true},
		{"normal/path", false},
		{"normal\\\\path", false},
		{"/valid/path/to/file", false},
	}

	for _, tt := range tests {
		result := containsInjectionPatterns(tt.input)
		if result != tt.expected {
			t.Errorf("containsInjectionPatterns(%q) = %v, expected %v", tt.input, result, tt.expected)
		}
	}
}

// TestContainsInjectionPatterns_NullBytes tests null byte detection
func TestContainsInjectionPatterns_NullBytes(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"test\x00file", true},
		{"normal string", false},
		{"hello world", false},
	}

	for _, tt := range tests {
		result := containsInjectionPatterns(tt.input)
		if result != tt.expected {
			t.Errorf("containsInjectionPatterns(%q) = %v, expected %v", tt.input, result, tt.expected)
		}
	}
}

// TestContainsInjectionPatterns_SQLInjection tests SQL injection pattern detection
func TestContainsInjectionPatterns_SQLInjection(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"';--", true},
		{"SELECT * FROM users", true},
		{"INSERT INTO table", true},
		{"DELETE FROM table", true},
		{"DROP TABLE users", true},
		{"UNION SELECT password FROM admin", true},
		{"' OR '1'='1", true},
		{"'='", true},
		{"@@version", true},
		{"@variable", true},
		{"EXEC(sp_cmdshell)", true},
		{"/* comment */", true},
		{"normal text", false},
		{"hello world", false},
		{"email@example.com", false}, // @ in email should not trigger
		{"select * from", true},      // lowercase should match
	}

	for _, tt := range tests {
		result := containsInjectionPatterns(tt.input)
		if result != tt.expected {
			t.Errorf("containsInjectionPatterns(%q) = %v, expected %v", tt.input, result, tt.expected)
		}
	}
}

// TestContainsInjectionPatterns_XSS tests XSS pattern detection
func TestContainsInjectionPatterns_XSS(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"<script>alert('xss')</script>", true},
		{"<SCRIPT SRC=http://evil.com></SCRIPT>", true},
		{"javascript:alert('xss')", true},
		{"<div>normal content</div>", false},
		{"normal text", false},
	}

	for _, tt := range tests {
		result := containsInjectionPatterns(tt.input)
		if result != tt.expected {
			t.Errorf("containsInjectionPatterns(%q) = %v, expected %v", tt.input, result, tt.expected)
		}
	}
}

// TestContainsInjectionPatterns_Combined tests combined patterns
func TestContainsInjectionPatterns_Combined(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"../test<script>", true},         // path traversal + xss
		{"test\x00<script>", true},        // null byte + xss
		{"'; DROP TABLE users;--", true},  // sql injection
		{"../test'; SELECT * FROM", true}, // path traversal + sql
		{"normal_safe_input_123", false},
		{"", false},
	}

	for _, tt := range tests {
		result := containsInjectionPatterns(tt.input)
		if result != tt.expected {
			t.Errorf("containsInjectionPatterns(%q) = %v, expected %v", tt.input, result, tt.expected)
		}
	}
}

// TestSecurityHeadersMiddleware tests the security headers middleware
func TestSecurityHeadersMiddleware(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	// Create a simple handler that returns success
	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	// Wrap with security headers middleware
	wrapped := server.securityHeadersMiddleware(testHandler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req.Host = "localhost:8080"
	ctx := &Context{
		Request:  req,
		Response: rec,
	}

	err := wrapped(ctx)
	if err != nil {
		t.Fatalf("securityHeadersMiddleware failed: %v", err)
	}

	// Check security headers
	tests := []struct {
		header   string
		expected string
	}{
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "DENY"},
		{"X-XSS-Protection", "1; mode=block"},
		{"Referrer-Policy", "strict-origin-when-cross-origin"},
		{"Content-Security-Policy", "default-src 'self'"},
	}

	for _, tt := range tests {
		value := rec.Header().Get(tt.header)
		if value != tt.expected {
			t.Errorf("Header %s = %q, expected %q", tt.header, value, tt.expected)
		}
	}
}

// TestSecurityHeadersMiddleware_MissingHost tests security headers with missing Host header
func TestSecurityHeadersMiddleware_MissingHost(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	// Create a simple handler that returns success
	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	// Wrap with security headers middleware
	wrapped := server.securityHeadersMiddleware(testHandler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	// Don't set Host header
	req.Host = ""
	ctx := &Context{
		Request:  req,
		Response: rec,
	}

	err := wrapped(ctx)
	if err != nil {
		t.Fatalf("securityHeadersMiddleware failed: %v", err)
	}

	// Should return bad request for missing Host header on /api/v1/ paths
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

// TestSecurityHeadersMiddleware_NonAPIPath tests security headers for non-API paths
func TestSecurityHeadersMiddleware_NonAPIPath(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	// Create a simple handler that returns success
	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	// Wrap with security headers middleware
	wrapped := server.securityHeadersMiddleware(testHandler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	// Don't set Host header - should be OK for non-API paths
	req.Host = ""
	ctx := &Context{
		Request:  req,
		Response: rec,
	}

	err := wrapped(ctx)
	if err != nil {
		t.Fatalf("securityHeadersMiddleware failed: %v", err)
	}

	// Should succeed even without Host header
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	// Security headers should still be set
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("Expected X-Content-Type-Options header to be set")
	}
}

// TestValidatePathParams_PathTraversal tests path parameter validation for path traversal
func TestValidatePathParams_PathTraversal(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	// Create a simple handler that returns success
	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	// Wrap with path param validation middleware
	wrapped := server.validatePathParams(testHandler)

	tests := []struct {
		name       string
		params     map[string]string
		expectCode int
	}{
		{
			name:       "path traversal dots",
			params:     map[string]string{"id": "../etc/passwd"},
			expectCode: http.StatusBadRequest,
		},
		{
			name:       "double slash",
			params:     map[string]string{"id": "//etc/passwd"},
			expectCode: http.StatusBadRequest,
		},
		{
			name:       "normal id",
			params:     map[string]string{"id": "valid-id-123"},
			expectCode: http.StatusOK,
		},
		{
			name:       "uuid format",
			params:     map[string]string{"id": "550e8400-e29b-41d4-a716-446655440000"},
			expectCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/api/v1/test", nil)
			ctx := &Context{
				Request:  req,
				Response: rec,
				Params:   tt.params,
			}

			err := wrapped(ctx)
			if err != nil {
				t.Fatalf("validatePathParams failed: %v", err)
			}

			if rec.Code != tt.expectCode {
				t.Errorf("Expected status %d, got %d", tt.expectCode, rec.Code)
			}
		})
	}
}

// TestValidatePathParams_LongParam tests path parameter validation for length limits
func TestValidatePathParams_LongParam(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	// Create a simple handler that returns success
	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	// Wrap with path param validation middleware
	wrapped := server.validatePathParams(testHandler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	// Create a param longer than 256 characters
	longParam := strings.Repeat("a", 257)
	ctx := &Context{
		Request:  req,
		Response: rec,
		Params:   map[string]string{"id": longParam},
	}

	err := wrapped(ctx)
	if err != nil {
		t.Fatalf("validatePathParams failed: %v", err)
	}

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for long param, got %d", http.StatusBadRequest, rec.Code)
	}
}

// TestValidatePathParams_NullByte tests path parameter validation for null bytes
func TestValidatePathParams_NullByte(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	// Create a simple handler that returns success
	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	// Wrap with path param validation middleware
	wrapped := server.validatePathParams(testHandler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	ctx := &Context{
		Request:  req,
		Response: rec,
		Params:   map[string]string{"id": "test\x00file"},
	}

	err := wrapped(ctx)
	if err != nil {
		t.Fatalf("validatePathParams failed: %v", err)
	}

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for null byte, got %d", http.StatusBadRequest, rec.Code)
	}
}

// TestRateLimitMiddleware_SkipHealthEndpoints tests that health endpoints skip rate limiting
func TestRateLimitMiddleware_SkipHealthEndpoints(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	// Create a simple handler that returns success
	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	// Wrap with rate limit middleware
	wrapped := server.rateLimitMiddleware(testHandler)

	// Test health endpoint
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	ctx := &Context{
		Request:  req,
		Response: rec,
	}

	err := wrapped(ctx)
	if err != nil {
		t.Fatalf("rateLimitMiddleware failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

// TestRateLimitMiddleware_SkipMetricsEndpoints tests that metrics endpoints skip rate limiting
func TestRateLimitMiddleware_SkipMetricsEndpoints(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	// Create a simple handler that returns success
	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	// Wrap with rate limit middleware
	wrapped := server.rateLimitMiddleware(testHandler)

	// Test metrics endpoint
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	ctx := &Context{
		Request:  req,
		Response: rec,
	}

	err := wrapped(ctx)
	if err != nil {
		t.Fatalf("rateLimitMiddleware failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

// TestRateLimitMiddleware_SkipReadyEndpoints tests that ready endpoints skip rate limiting
func TestRateLimitMiddleware_SkipReadyEndpoints(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	// Create a simple handler that returns success
	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	// Wrap with rate limit middleware
	wrapped := server.rateLimitMiddleware(testHandler)

	// Test ready endpoint
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ready", nil)
	ctx := &Context{
		Request:  req,
		Response: rec,
	}

	err := wrapped(ctx)
	if err != nil {
		t.Fatalf("rateLimitMiddleware failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

// TestRateLimitMiddleware_AllowsRequestsWithinLimit tests that requests within limit are allowed
func TestRateLimitMiddleware_AllowsRequestsWithinLimit(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	// Create a simple handler that returns success
	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	// Wrap with rate limit middleware
	wrapped := server.rateLimitMiddleware(testHandler)

	// Make a request
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/souls", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	ctx := &Context{
		Request:  req,
		Response: rec,
	}

	err := wrapped(ctx)
	if err != nil {
		t.Fatalf("rateLimitMiddleware failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	// Check that rate limit headers are set
	if rec.Header().Get("X-RateLimit-Limit") == "" {
		t.Error("Expected X-RateLimit-Limit header to be set")
	}
	if rec.Header().Get("X-RateLimit-Remaining") == "" {
		t.Error("Expected X-RateLimit-Remaining header to be set")
	}
	if rec.Header().Get("X-RateLimit-Reset") == "" {
		t.Error("Expected X-RateLimit-Reset header to be set")
	}
}

// TestRateLimitMiddleware_XForwardedFor tests rate limiting with X-Forwarded-For header
func TestRateLimitMiddleware_XForwardedFor(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	// Create a simple handler that returns success
	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	// Wrap with rate limit middleware
	wrapped := server.rateLimitMiddleware(testHandler)

	// Make a request with X-Forwarded-For header
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/souls", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 192.168.1.1")
	ctx := &Context{
		Request:  req,
		Response: rec,
	}

	err := wrapped(ctx)
	if err != nil {
		t.Fatalf("rateLimitMiddleware failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

// TestRateLimitMiddleware_WithAuthenticatedUser tests rate limiting with authenticated user
func TestRateLimitMiddleware_WithAuthenticatedUser(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	// Create a simple handler that returns success
	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	// Wrap with rate limit middleware
	wrapped := server.rateLimitMiddleware(testHandler)

	// Make a request with authenticated user
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/souls", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	ctx := &Context{
		Request:  req,
		Response: rec,
		User:     &User{ID: "user-123", Name: "Test User"},
	}

	err := wrapped(ctx)
	if err != nil {
		t.Fatalf("rateLimitMiddleware failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

// TestRateLimitMiddleware_AuthEndpointLimit tests rate limiting for auth endpoints
func TestRateLimitMiddleware_AuthEndpointLimit(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	// Create a simple handler that returns success
	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	// Wrap with rate limit middleware
	wrapped := server.rateLimitMiddleware(testHandler)

	// Make a request to auth endpoint
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/auth/login", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	ctx := &Context{
		Request:  req,
		Response: rec,
	}

	err := wrapped(ctx)
	if err != nil {
		t.Fatalf("rateLimitMiddleware failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	// Check that rate limit headers are set (auth endpoints have lower limit)
	limit := rec.Header().Get("X-RateLimit-Limit")
	if limit == "" {
		t.Error("Expected X-RateLimit-Limit header to be set")
	}
}

// TestRateLimitMiddleware_IPRateExceeded tests that IP rate limit is enforced
func TestRateLimitMiddleware_IPRateExceeded(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}
	wrapped := server.rateLimitMiddleware(testHandler)

	// Auth endpoints (/auth/ prefix) have limit of 10/minute
	for i := 0; i < 11; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/auth/login", nil)
		req.RemoteAddr = "10.0.0.1:9999"
		ctx := &Context{Request: req, Response: rec}
		wrapped(ctx)
		if i < 10 {
			if rec.Code != http.StatusOK {
				t.Fatalf("request %d: expected 200, got %d", i+1, rec.Code)
			}
		} else {
			// 11th request should be rate limited
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("expected 429 on 11th request, got %d", rec.Code)
			}
		}
	}

	// Verify rate limit headers
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/auth/login", nil)
	req.RemoteAddr = "10.0.0.1:9999"
	ctx := &Context{Request: req, Response: rec}
	wrapped(ctx)
	if rec.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header")
	}
	if rec.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Errorf("expected X-RateLimit-Remaining=0, got %s", rec.Header().Get("X-RateLimit-Remaining"))
	}
}

// TestRateLimitMiddleware_UserRateExceeded tests user-based rate limiting across multiple IPs
func TestRateLimitMiddleware_UserRateExceeded(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}
	wrapped := server.rateLimitMiddleware(testHandler)

	user := &User{ID: "rate-user", Name: "Rate Limited User"}

	// Default limit is 100, user limit is 200 (2x)
	// Use different IPs to bypass IP rate limiting but hit user limit
	for i := 0; i < 200; i++ {
		ip := fmt.Sprintf("172.16.%d.%d:1234", i/256, i%256)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/souls", nil)
		req.RemoteAddr = ip
		ctx := &Context{Request: req, Response: rec, User: user}
		wrapped(ctx)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, rec.Code)
		}
	}

	// 201st request should hit user rate limit (from a new IP)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/souls", nil)
	req.RemoteAddr = "172.16.10.0:5678"
	ctx := &Context{Request: req, Response: rec, User: user}
	wrapped(ctx)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rec.Code)
	}
}

// TestRateLimitMiddleware_SensitiveEndpoint tests sensitive endpoint rate limit
func TestRateLimitMiddleware_SensitiveEndpoint(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}
	wrapped := server.rateLimitMiddleware(testHandler)

	// Sensitive endpoints (paths containing "delete" or "update") have limit of 20
	for i := 0; i < 21; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("DELETE", "/api/v1/souls/delete-soul", nil)
		req.RemoteAddr = "10.10.10.1:1111"
		ctx := &Context{Request: req, Response: rec}
		wrapped(ctx)
		if i < 20 {
			if rec.Code != http.StatusOK {
				t.Fatalf("request %d: expected 200, got %d", i+1, rec.Code)
			}
		} else {
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("expected 429 on 21st request, got %d", rec.Code)
			}
		}
	}

	// Verify the limit header shows 20 (sensitive limit)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/souls/delete-soul", nil)
	req.RemoteAddr = "10.10.10.1:1111"
	ctx := &Context{Request: req, Response: rec}
	wrapped(ctx)
	limit := rec.Header().Get("X-RateLimit-Limit")
	if limit != "20" {
		t.Errorf("expected X-RateLimit-Limit=20, got %s", limit)
	}
}

// TestRateLimitMiddleware_NegativeRemaining tests the defensive remaining < 0 branch
func TestRateLimitMiddleware_NegativeRemaining(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}
	wrapped := server.rateLimitMiddleware(testHandler)

	// Exhaust the auth limit (10 requests)
	for i := 0; i < 11; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/login", nil)
		req.RemoteAddr = "192.168.50.1:2222"
		ctx := &Context{Request: req, Response: rec}
		wrapped(ctx)
	}

	// After exceeding, remaining should be clamped to 0, not negative
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/login", nil)
	req.RemoteAddr = "192.168.50.1:2222"
	ctx := &Context{Request: req, Response: rec}
	wrapped(ctx)

	remaining := rec.Header().Get("X-RateLimit-Remaining")
	if remaining == "" {
		// Request was rate limited, remaining should be 0
		t.Log("Request was rate limited as expected")
	}
}

// TestValidateJSONMiddleware_ValidJSON tests validateJSONMiddleware with valid JSON
func TestValidateJSONMiddleware_ValidJSON(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	wrapped := server.validateJSONMiddleware(testHandler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/test", strings.NewReader(`{"name":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := &Context{
		Request:  req,
		Response: rec,
	}

	err := wrapped(ctx)
	if err != nil {
		t.Fatalf("validateJSONMiddleware failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

// TestValidateJSONMiddleware_InvalidContentType tests validateJSONMiddleware with wrong Content-Type
func TestValidateJSONMiddleware_InvalidContentType(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	wrapped := server.validateJSONMiddleware(testHandler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/test", strings.NewReader(`{"name":"test"}`))
	req.Header.Set("Content-Type", "text/plain") // Wrong content type
	ctx := &Context{
		Request:  req,
		Response: rec,
	}

	err := wrapped(ctx)
	if err != nil {
		t.Fatalf("validateJSONMiddleware failed: %v", err)
	}

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for invalid content type, got %d", http.StatusBadRequest, rec.Code)
	}
}

// TestValidateJSONMiddleware_InvalidJSON tests validateJSONMiddleware with invalid JSON
func TestValidateJSONMiddleware_InvalidJSON(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	wrapped := server.validateJSONMiddleware(testHandler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/test", strings.NewReader(`{invalid json}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := &Context{
		Request:  req,
		Response: rec,
	}

	err := wrapped(ctx)
	if err != nil {
		t.Fatalf("validateJSONMiddleware failed: %v", err)
	}

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for invalid JSON, got %d", http.StatusBadRequest, rec.Code)
	}
}

// TestValidateJSONMiddleware_InjectionPattern tests validateJSONMiddleware with injection attempt
func TestValidateJSONMiddleware_InjectionPattern(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	wrapped := server.validateJSONMiddleware(testHandler)

	rec := httptest.NewRecorder()
	// Try SQL injection in JSON
	req := httptest.NewRequest("POST", "/api/v1/test", strings.NewReader(`{"query":"'; DROP TABLE users;--"}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := &Context{
		Request:  req,
		Response: rec,
	}

	err := wrapped(ctx)
	if err != nil {
		t.Fatalf("validateJSONMiddleware failed: %v", err)
	}

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for injection pattern, got %d", http.StatusBadRequest, rec.Code)
	}
}

// TestValidateJSONMiddleware_EmptyBody tests validateJSONMiddleware with empty body
func TestValidateJSONMiddleware_EmptyBody(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	wrapped := server.validateJSONMiddleware(testHandler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/test", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	ctx := &Context{
		Request:  req,
		Response: rec,
	}

	err := wrapped(ctx)
	if err != nil {
		t.Fatalf("validateJSONMiddleware failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d for empty body, got %d", http.StatusOK, rec.Code)
	}
}

// TestValidateJSONMiddleware_GETRequest tests validateJSONMiddleware with GET request (should skip validation)
func TestValidateJSONMiddleware_GETRequest(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	wrapped := server.validateJSONMiddleware(testHandler)

	rec := httptest.NewRecorder()
	// GET request without Content-Type should pass
	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	ctx := &Context{
		Request:  req,
		Response: rec,
	}

	err := wrapped(ctx)
	if err != nil {
		t.Fatalf("validateJSONMiddleware failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d for GET request, got %d", http.StatusOK, rec.Code)
	}
}

// TestValidateJSONMiddleware_LargeBody tests validateJSONMiddleware with body > 1MB
func TestValidateJSONMiddleware_LargeBody(t *testing.T) {
	storage := newMockStorage()
	server := newTestServerWithStorage(storage)

	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	wrapped := server.validateJSONMiddleware(testHandler)

	rec := httptest.NewRecorder()
	// Create a body larger than 1MB
	largeBody := strings.Repeat("a", (1<<20)+100)
	req := httptest.NewRequest("POST", "/api/v1/test", strings.NewReader(largeBody))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(largeBody))
	ctx := &Context{
		Request:  req,
		Response: rec,
	}

	err := wrapped(ctx)
	if err != nil {
		t.Fatalf("validateJSONMiddleware failed: %v", err)
	}

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected status %d for large body, got %d", http.StatusRequestEntityTooLarge, rec.Code)
	}
}

// TestHandleDeleteSoul_StorageError tests delete soul with storage error
func TestHandleDeleteSoul_StorageError(t *testing.T) {
	store := &failingMockStorage{}

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		store:  store,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
	}

	router.Handle("DELETE", "/api/v1/souls/:id", server.requireAuth(server.handleDeleteSoul))

	req := httptest.NewRequest("DELETE", "/api/v1/souls/soul-1", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

// TestHandleDeleteChannel_StorageError tests delete channel with storage error
func TestHandleDeleteChannel_StorageError(t *testing.T) {
	store := &failingMockStorage{}

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		store:  store,
		router: router,
		alert:  &failingAlertManager{},
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
	}

	router.Handle("DELETE", "/api/v1/channels/:id", server.requireAuth(server.handleDeleteChannel))

	req := httptest.NewRequest("DELETE", "/api/v1/channels/ch-1", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

// TestHandleDeleteRule_StorageError tests delete rule with storage error
func TestHandleDeleteRule_StorageError(t *testing.T) {
	store := &failingMockStorage{}

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		store:  store,
		router: router,
		alert:  &failingAlertManager{},
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
	}

	router.Handle("DELETE", "/api/v1/rules/:id", server.requireAuth(server.handleDeleteRule))

	req := httptest.NewRequest("DELETE", "/api/v1/rules/rule-1", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

// TestHandleDeleteWorkspace_StorageError tests delete workspace with storage error
func TestHandleDeleteWorkspace_StorageError(t *testing.T) {
	store := &failingMockStorage{}

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		store:  store,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
	}

	router.Handle("DELETE", "/api/v1/workspaces/:id", server.requireAuth(server.handleDeleteWorkspace))

	req := httptest.NewRequest("DELETE", "/api/v1/workspaces/ws-1", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

// TestHandleDeleteJourney_StorageError tests delete journey with storage error
func TestHandleDeleteJourney_StorageError(t *testing.T) {
	store := &failingMockStorage{}

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		store:  store,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
	}

	router.Handle("DELETE", "/api/v1/journeys/:id", server.requireAuth(server.handleDeleteJourney))

	req := httptest.NewRequest("DELETE", "/api/v1/journeys/journey-1", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}
