package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Test handleListSouls with status filter
func TestMCPServer_handleListSouls_WithStatusFilter(t *testing.T) {
	store := newMockStorage()
	store.souls["soul-1"] = &core.Soul{ID: "soul-1", Name: "Test Soul", Type: core.CheckHTTP, Enabled: true}
	store.souls["soul-2"] = &core.Soul{ID: "soul-2", Name: "Test Soul 2", Type: core.CheckHTTP, Enabled: true}
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	// Test with status filter
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_souls","arguments":{"status":"alive"}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	var resp MCPResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Logf("Got error: %s", resp.Error.Message)
	}
}

// Test handleListSouls with workspace filter
func TestMCPServer_handleListSouls_WithWorkspaceFilter(t *testing.T) {
	store := newMockStorage()
	store.souls["soul-1"] = &core.Soul{ID: "soul-1", Name: "Test Soul", Type: core.CheckHTTP, Enabled: true}
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	// Test with workspace filter
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_souls","arguments":{"workspace":"default"}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	var resp MCPResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Logf("Got error: %s", resp.Error.Message)
	}
}

// Test handleReadResource with valid soul ID
func TestMCPServer_handleReadResource_WithValidSoul(t *testing.T) {
	store := newMockStorage()
	store.souls["soul-1"] = &core.Soul{ID: "soul-1", Name: "Test Soul", Type: core.CheckHTTP, Enabled: true}
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	// Read soul resource
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"soul://soul-1"}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	var resp MCPResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Logf("Got error: %s", resp.Error.Message)
	}
}

// Test handleGetPrompt with analyze_soul
func TestMCPServer_handleGetPrompt_AnalyzeSoul(t *testing.T) {
	store := newMockStorage()
	store.souls["soul-1"] = &core.Soul{ID: "soul-1", Name: "Test Soul", Type: core.CheckHTTP, Enabled: true}
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	// Get analyze soul prompt
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"analyze_soul","arguments":{"soul_id":"soul-1"}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	var resp MCPResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Logf("Got error: %s", resp.Error.Message)
	}
}

// Test handleGetPrompt with incident_summary
func TestMCPServer_handleGetPrompt_IncidentSummary(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	// Get incident summary prompt
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"incident_summary","arguments":{}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	var resp MCPResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Logf("Got error: %s", resp.Error.Message)
	}
}

// Test handleCreateSoul with interval
func TestMCPServer_handleCreateSoul_WithInterval(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	// Create soul with interval
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_soul","arguments":{"name":"Test Soul","type":"http","target":"https://example.com","interval":"1m"}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	var resp MCPResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Logf("Got error: %s", resp.Error.Message)
	}
}

// Test handleCreateSoul without interval
func TestMCPServer_handleCreateSoul_WithoutInterval(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	// Create soul without interval
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_soul","arguments":{"name":"Test Soul 2","type":"http","target":"https://example2.com"}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	var resp MCPResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Logf("Got error: %s", resp.Error.Message)
	}
}

// Test handleGetPrompt with create_monitor_guide for website
func TestMCPServer_handleGetPrompt_CreateMonitorGuide_Website(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	// Get create monitor guide prompt for website
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"create_monitor_guide","arguments":{"type":"website"}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	var resp MCPResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Logf("Got error: %s", resp.Error.Message)
	}
}

// Test handleGetPrompt with create_monitor_guide for API
func TestMCPServer_handleGetPrompt_CreateMonitorGuide_API(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	// Get create monitor guide prompt for API
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"create_monitor_guide","arguments":{"type":"api"}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	var resp MCPResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Logf("Got error: %s", resp.Error.Message)
	}
}

// Test handleGetPrompt with create_monitor_guide for server
func TestMCPServer_handleGetPrompt_CreateMonitorGuide_Server(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	// Get create monitor guide prompt for server
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"create_monitor_guide","arguments":{"type":"server"}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	var resp MCPResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Logf("Got error: %s", resp.Error.Message)
	}
}

// Test handleReadResource with unknown resource URI
func TestMCPServer_handleReadResource_UnknownResource(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	// Read unknown resource
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"unknown://resource"}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	var resp MCPResponse
	json.NewDecoder(w.Body).Decode(&resp)
	// Should return error for unknown resource
	if resp.Error == nil {
		t.Error("Expected error for unknown resource")
	}
}

// Test handleReadResource with soul ID parameter
func TestMCPServer_handleReadResource_SoulWithID(t *testing.T) {
	store := newMockStorage()
	store.souls["soul-1"] = &core.Soul{ID: "soul-1", Name: "Test Soul", Type: core.CheckHTTP, Enabled: true}
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	// Read soul resource with ID
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"soul://soul-1"}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	var resp MCPResponse
	json.NewDecoder(w.Body).Decode(&resp)
	// Should not error
	if resp.Error != nil {
		t.Logf("Got error: %s", resp.Error.Message)
	}
}

// Test handleGetPrompt with missing prompt name
func TestMCPServer_handleGetPrompt_MissingName(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	// Get prompt without name
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"arguments":{}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	var resp MCPResponse
	json.NewDecoder(w.Body).Decode(&resp)
	// Should return error for missing name
	if resp.Error == nil {
		t.Error("Expected error for missing prompt name")
	}
}

// Test handleGetPrompt with unknown prompt
func TestMCPServer_handleGetPrompt_UnknownPrompt(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	// Get unknown prompt
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"unknown_prompt","arguments":{}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	var resp MCPResponse
	json.NewDecoder(w.Body).Decode(&resp)
	// Should return error for unknown prompt
	if resp.Error == nil {
		t.Error("Expected error for unknown prompt")
	}
}

// Test handleReadResource with error from handler
func TestMCPServer_handleReadResource_HandlerError(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	// Register a resource that returns error
	server.RegisterResource(MCPResource{
		URI:         "test://error",
		Name:        "Error Resource",
		Description: "A resource that returns error",
		Handler: func() (interface{}, error) {
			return nil, fmt.Errorf("resource handler error")
		},
	})

	req := &MCPRequest{
		ID:     1,
		Method: "resources/read",
		Params: json.RawMessage(`{"uri": "test://error"}`),
	}
	resp := server.handleReadResource(req)
	if resp == nil {
		t.Fatal("Expected non-nil response")
	}
	if resp.Error == nil {
		t.Error("Expected error response")
	}
}

// Test handleGetPrompt with handler error
func TestMCPServer_handleGetPrompt_HandlerError(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	// Register a prompt that returns error
	server.RegisterPrompt(MCPPrompt{
		Name:        "error_prompt",
		Description: "A prompt that returns error",
		Handler: func(args map[string]string) (string, error) {
			return "", fmt.Errorf("prompt handler error")
		},
	})

	req := &MCPRequest{
		ID:     1,
		Method: "prompts/get",
		Params: json.RawMessage(`{"name": "error_prompt", "arguments": {}}`),
	}
	resp := server.handleGetPrompt(req)
	if resp == nil {
		t.Fatal("Expected non-nil response")
	}
	if resp.Error == nil {
		t.Error("Expected error response")
	}
}

// Test handleGetSoul with valid soul
func TestMCPServer_handleGetSoul_Valid(t *testing.T) {
	store := newMockStorage()
	store.souls["soul-1"] = &core.Soul{ID: "soul-1", Name: "Test Soul", Type: core.CheckHTTP, Enabled: true}
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	result, err := server.handleGetSoul(context.Background(), json.RawMessage(`{"soul_id":"soul-1"}`))
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Error("Expected non-nil result")
	}
}

// Test handleGetSoul with missing soul_id
func TestMCPServer_handleGetSoul_MissingID(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	result, err := server.handleGetSoul(context.Background(), json.RawMessage(`{}`))
	// Should return nil result for empty soul_id - storage will handle it
	if err != nil {
		t.Logf("Got error (may be expected): %v", err)
	}
	_ = result
}

// Test handleGetSoul with invalid JSON
func TestMCPServer_handleGetSoul_InvalidJSON(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	_, err := server.handleGetSoul(context.Background(), json.RawMessage(`{not valid json}`))
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// Test handleForceCheck with valid soul
func TestMCPServer_handleForceCheck_Valid(t *testing.T) {
	store := newMockStorage()
	store.souls["soul-1"] = &core.Soul{ID: "soul-1", Name: "Test Soul", Type: core.CheckHTTP, Enabled: true}
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	result, err := server.handleForceCheck(context.Background(), json.RawMessage(`{"soul_id":"soul-1"}`))
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Error("Expected non-nil result")
	}
}

// Test handleForceCheck with invalid JSON
func TestMCPServer_handleForceCheck_InvalidJSON(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	_, err := server.handleForceCheck(context.Background(), json.RawMessage(`{not valid json}`))
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// Test handleAcknowledgeIncident with valid ID
func TestMCPServer_handleAcknowledgeIncident_Valid(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	result, err := server.handleAcknowledgeIncident(context.Background(), json.RawMessage(`{"incident_id":"inc-1"}`))
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	// Result may be nil on success
	_ = result
}

// Test handleAcknowledgeIncident with invalid JSON
func TestMCPServer_handleAcknowledgeIncident_InvalidJSON(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	_, err := server.handleAcknowledgeIncident(context.Background(), json.RawMessage(`{not valid json}`))
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// Test handleCreateSoul with invalid JSON
func TestMCPServer_handleCreateSoul_InvalidJSON(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	_, err := server.handleCreateSoul(context.Background(), json.RawMessage(`{not valid json}`))
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// Test handleGetStats with workspace filter
func TestMCPServer_handleGetStats_WithWorkspace(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	result, err := server.handleGetStats(context.Background(), json.RawMessage(`{"workspace":"default"}`))
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Error("Expected non-nil result")
	}
}

// Test handleListIncidents returns empty list
func TestMCPServer_handleListIncidents_Empty(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	result, err := server.handleListIncidents(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Error("Expected non-nil result")
	}
}

// Test handleCallTool with unknown tool
func TestMCPServer_handleCallTool_UnknownTool(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	req := &MCPRequest{
		ID:     1,
		Method: "tools/call",
		Params: json.RawMessage(`{"name":"unknown_tool","arguments":{}}`),
	}
	resp := server.handleCallTool(context.Background(), req)
	if resp == nil {
		t.Fatal("Expected non-nil response")
	}
	if resp.Error == nil {
		t.Error("Expected error for unknown tool")
	}
}

// Test RESTServer.handleMCP with nil MCP server
func TestRESTServer_handleMCP_NilMCP(t *testing.T) {
	store := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:  core.ServerConfig{Host: "localhost", Port: 8080},
		store:   store,
		router:  router,
		auth:    &mockAuthenticator{},
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/api/v1/mcp", server.requireAuth(server.handleMCP))

	req := httptest.NewRequest("GET", "/api/v1/mcp", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503, got %d", w.Code)
	}
}

// Test RESTServer.handleMCP without auth
func TestRESTServer_handleMCP_NoAuth(t *testing.T) {
	store := newMockStorage()
	mcp := NewMCPServer(store, &mockProbeEngine{}, &mockAlertManager{}, newTestLogger())
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:  core.ServerConfig{Host: "localhost", Port: 8080},
		store:   store,
		router:  router,
		auth:    &mockAuthenticator{},
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
		mcp:     mcp,
	}

	router.Handle("GET", "/api/v1/mcp", server.requireAuth(server.handleMCP))

	req := httptest.NewRequest("GET", "/api/v1/mcp", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should require authentication (401 or 405 depending on router behavior)
	if w.Code != http.StatusUnauthorized && w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 401 or 405, got %d", w.Code)
	}
}

// Test RESTServer.handleClusterStatus with nil cluster
func TestRESTServer_handleClusterStatus_NilCluster(t *testing.T) {
	store := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  store,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		// No cluster set (nil)
	}

	router.Handle("GET", "/api/v1/cluster/status", server.requireAuth(server.handleClusterStatus))

	req := httptest.NewRequest("GET", "/api/v1/cluster/status", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
}

// Test RESTServer.handleClusterPeers with nil cluster
func TestRESTServer_handleClusterPeers_NilCluster(t *testing.T) {
	store := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  store,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		// No cluster set (nil)
	}

	router.Handle("GET", "/api/v1/cluster/peers", server.requireAuth(server.handleClusterPeers))

	req := httptest.NewRequest("GET", "/api/v1/cluster/peers", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
}

// Test RESTServer.handleClusterPeers with cluster
func TestRESTServer_handleClusterPeers_WithCluster(t *testing.T) {
	store := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:  core.ServerConfig{Host: "localhost", Port: 8080},
		store:   store,
		router:  router,
		auth:    &mockAuthenticator{},
		logger:  newTestLogger(),
		cluster: &mockClusterManager{isLeader: true},
	}

	router.Handle("GET", "/api/v1/cluster/peers", server.requireAuth(server.handleClusterPeers))

	req := httptest.NewRequest("GET", "/api/v1/cluster/peers", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
}

// Test RESTServer.handleStatusPage with souls and judgments
func TestRESTServer_handleStatusPage_WithData(t *testing.T) {
	store := newMockStorage()
	store.souls["soul-1"] = &core.Soul{ID: "soul-1", Name: "Test Soul", Type: core.CheckHTTP, Enabled: true, Target: "https://example.com"}
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:  core.ServerConfig{Host: "localhost", Port: 8080},
		store:   store,
		router:  router,
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/status", server.handleStatusPage)

	req := httptest.NewRequest("GET", "/status", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
}

// Test RESTServer.handlePublicStatus with empty store
func TestRESTServer_handlePublicStatus_Empty(t *testing.T) {
	store := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:  core.ServerConfig{Host: "localhost", Port: 8080},
		store:   store,
		router:  router,
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/api/v1/public/status", server.handlePublicStatus)

	req := httptest.NewRequest("GET", "/api/v1/public/status", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
}

// Test RESTServer.handleStatusPageHTML with data
func TestRESTServer_handleStatusPageHTML_WithData(t *testing.T) {
	store := newMockStorage()
	store.souls["soul-1"] = &core.Soul{ID: "soul-1", Name: "Test Soul", Type: core.CheckHTTP, Enabled: true, Target: "https://example.com"}
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:  core.ServerConfig{Host: "localhost", Port: 8080},
		store:   store,
		router:  router,
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/status.html", server.handleStatusPageHTML)

	req := httptest.NewRequest("GET", "/status.html", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("Expected text/html content type, got %s", w.Header().Get("Content-Type"))
	}
}

// Test RESTServer.handleWebSocket with nil WebSocket server
func TestRESTServer_handleWebSocket_NilWS(t *testing.T) {
	store := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:  core.ServerConfig{Host: "localhost", Port: 8080},
		store:   store,
		router:  router,
		auth:    &mockAuthenticator{},
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
		// ws is nil
	}

	router.Handle("GET", "/api/v1/ws", server.requireAuth(server.handleWebSocket))

	req := httptest.NewRequest("GET", "/api/v1/ws", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503, got %d", w.Code)
	}
}

// Test RESTServer.Start with zero port fallback
func TestRESTServer_Start_ZeroPort(t *testing.T) {
	store := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:  core.ServerConfig{Host: "", Port: 0},
		store:   store,
		router:  router,
		auth:    &mockAuthenticator{},
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	// Start server in background
	go func() {
		_ = server.Start()
	}()

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Stop it
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = server.Stop(ctx)
}

// Test RESTServer.Stop with no HTTP server
func TestRESTServer_Stop_NoServer(t *testing.T) {
	server := &RESTServer{}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := server.Stop(ctx)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}
