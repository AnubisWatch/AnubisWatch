package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

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


