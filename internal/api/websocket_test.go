package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TestWSClient_JoinRoom tests joining a room
func TestWSClient_JoinRoom(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger)

	client := &WSClient{
		ID:        "test-client",
		Workspace: "default",
		Rooms:     make(map[string]bool),
		server:    server,
	}

	server.mu.Lock()
	server.clients[client.ID] = client
	server.mu.Unlock()

	// Join a room
	client.JoinRoom("workspace:default")

	// Verify client is in room
	client.mu.RLock()
	if !client.Rooms["workspace:default"] {
		t.Error("Client should be in workspace:default room")
	}
	client.mu.RUnlock()

	// Verify room has client
	server.mu.RLock()
	if server.rooms["workspace:default"] == nil {
		t.Error("Room should exist")
	} else if !server.rooms["workspace:default"][client.ID] {
		t.Error("Room should contain client")
	}
	server.mu.RUnlock()
}

// TestWSClient_LeaveRoom tests leaving a room
func TestWSClient_LeaveRoom(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger)

	client := &WSClient{
		ID:        "test-client",
		Workspace: "default",
		Rooms:     make(map[string]bool),
		server:    server,
	}

	server.mu.Lock()
	server.clients[client.ID] = client
	server.mu.Unlock()

	// Join then leave
	client.JoinRoom("workspace:default")
	client.LeaveRoom("workspace:default")

	// Verify client is not in room
	client.mu.RLock()
	if client.Rooms["workspace:default"] {
		t.Error("Client should not be in workspace:default room")
	}
	client.mu.RUnlock()

	// Verify room doesn't have client
	server.mu.RLock()
	if server.rooms["workspace:default"] != nil && server.rooms["workspace:default"][client.ID] {
		t.Error("Room should not contain client")
	}
	server.mu.RUnlock()
}

// TestWSClient_LeaveRoom_LastClient tests leaving a room as the last client
func TestWSClient_LeaveRoom_LastClient(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger)

	client := &WSClient{
		ID:        "test-client",
		Workspace: "default",
		Rooms:     make(map[string]bool),
		server:    server,
	}

	server.mu.Lock()
	server.clients[client.ID] = client
	server.mu.Unlock()

	// Join and leave as only client
	client.JoinRoom("test-room")
	client.LeaveRoom("test-room")

	// Room should be cleaned up
	server.mu.RLock()
	if server.rooms["test-room"] != nil {
		t.Error("Room should be deleted when last client leaves")
	}
	server.mu.RUnlock()
}

// TestWSClient_createMessage tests message creation
func TestWSClient_createMessage(t *testing.T) {
	client := &WSClient{
		ID: "test-client",
	}

	payload := map[string]string{"key": "value"}
	data := client.createMessage("test", payload)

	if len(data) == 0 {
		t.Error("createMessage should return non-empty data")
	}

	// Verify it's valid JSON
	str := string(data)
	if !contains(str, "test") {
		t.Error("Message should contain type 'test'")
	}
}

// TestWSClient_createSuccessMessage tests success message creation
func TestWSClient_createSuccessMessage(t *testing.T) {
	client := &WSClient{
		ID: "test-client",
	}

	data := client.createSuccessMessage("subscribe", []string{"event1", "event2"})

	if len(data) == 0 {
		t.Error("createSuccessMessage should return non-empty data")
	}

	str := string(data)
	if !contains(str, "success") {
		t.Error("Success message should contain 'success'")
	}
	if !contains(str, "subscribe") {
		t.Error("Success message should contain action")
	}
}

// TestWSClient_createErrorMessage tests error message creation
func TestWSClient_createErrorMessage(t *testing.T) {
	client := &WSClient{
		ID: "test-client",
	}

	data := client.createErrorMessage("invalid_event", "Unknown event type")

	if len(data) == 0 {
		t.Error("createErrorMessage should return non-empty data")
	}

	str := string(data)
	if !contains(str, "error") {
		t.Error("Error message should contain 'error'")
	}
	if !contains(str, "invalid_event") {
		t.Error("Error message should contain error code")
	}
}

// TestWebSocketServer_removeClient_NonExistent tests removing a non-existent client
func TestWebSocketServer_removeClient_NonExistent(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger)

	// Should not panic
	server.removeClient("non-existent-client")
}

// TestWebSocketServer_Stop_NoClients tests stopping a server with no clients
func TestWebSocketServer_Stop_NoClients(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger)

	server.Start()
	time.Sleep(10 * time.Millisecond)

	// Should not panic when stopping with no clients
	server.Stop()
}

// TestWebSocketServer_BroadcastIncident tests broadcasting an incident
func TestWebSocketServer_BroadcastIncident(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger)
	server.Start()
	defer server.Stop()

	// Create a test client
	client := &WSClient{
		ID:        "test-client",
		Workspace: "default",
		Rooms:     make(map[string]bool),
		send:      make(chan []byte, 10),
		server:    server,
	}

	server.mu.Lock()
	server.clients[client.ID] = client
	server.mu.Unlock()

	// Broadcast an incident
	incident := &core.Incident{
		ID:          "incident-1",
		SoulID:      "soul-1",
		Status:      core.IncidentOpen,
		Severity:    core.SeverityCritical,
		WorkspaceID: "default",
	}

	server.BroadcastIncident(incident)

	// Give broadcast time to process
	time.Sleep(50 * time.Millisecond)
}

// TestWebSocketServer_BroadcastSoulUpdate tests broadcasting a soul update
func TestWebSocketServer_BroadcastSoulUpdate(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger)
	server.Start()
	defer server.Stop()

	// Create a test client
	client := &WSClient{
		ID:        "test-client",
		Workspace: "default",
		Rooms:     make(map[string]bool),
		send:      make(chan []byte, 10),
		server:    server,
	}

	server.mu.Lock()
	server.clients[client.ID] = client
	server.mu.Unlock()

	// Broadcast a soul update
	soul := &core.Soul{
		ID:          "soul-1",
		Name:        "Test Soul",
		WorkspaceID: "default",
	}

	server.BroadcastSoulUpdate(soul)

	// Give broadcast time to process
	time.Sleep(50 * time.Millisecond)
}

// TestIsWebSocketRequest tests the WebSocket request detection
func TestIsWebSocketRequest(t *testing.T) {
	// Test with WebSocket upgrade header
	req1, _ := http.NewRequest("GET", "/ws", nil)
	req1.Header.Set("Upgrade", "websocket")
	if !IsWebSocketRequest(req1) {
		t.Error("Expected WebSocket request to be detected")
	}

	// Test without WebSocket header
	req2, _ := http.NewRequest("GET", "/ws", nil)
	if IsWebSocketRequest(req2) {
		t.Error("Expected non-WebSocket request")
	}

	// Test with different upgrade value
	req3, _ := http.NewRequest("GET", "/ws", nil)
	req3.Header.Set("Upgrade", "h2c")
	if IsWebSocketRequest(req3) {
		t.Error("Expected non-WebSocket request for h2c upgrade")
	}
}

// TestWebSocketServer_SubscribeClient tests subscribing a client to events
func TestWebSocketServer_SubscribeClient(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger)

	client := &WSClient{
		ID:     "test-client",
		Rooms:  make(map[string]bool),
		send:   make(chan []byte, 10),
		server: server,
	}

	server.mu.Lock()
	server.clients[client.ID] = client
	server.mu.Unlock()

	// Subscribe to events
	server.SubscribeClient(client.ID, []string{"judgment", "alert"})

	// Verify client is in event rooms
	client.mu.RLock()
	if !client.Rooms["event:judgment"] {
		t.Error("Client should be in event:judgment room")
	}
	if !client.Rooms["event:alert"] {
		t.Error("Client should be in event:alert room")
	}
	client.mu.RUnlock()
}

// TestWebSocketServer_UnsubscribeClient tests unsubscribing a client
func TestWebSocketServer_UnsubscribeClient(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger)

	client := &WSClient{
		ID:     "test-client",
		Rooms:  make(map[string]bool),
		send:   make(chan []byte, 10),
		server: server,
	}

	server.mu.Lock()
	server.clients[client.ID] = client
	server.mu.Unlock()

	// Subscribe first
	server.SubscribeClient(client.ID, []string{"judgment"})

	// Then unsubscribe
	server.UnsubscribeClient(client.ID, []string{"judgment"})

	// Verify client is not in room
	client.mu.RLock()
	if client.Rooms["event:judgment"] {
		t.Error("Client should not be in event:judgment room after unsubscribe")
	}
	client.mu.RUnlock()
}

// TestWebSocketServer_SubscribeNonExistentClient tests subscribing a non-existent client
func TestWebSocketServer_SubscribeNonExistentClient(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger)

	// Should not panic
	server.SubscribeClient("non-existent", []string{"judgment"})
}

// TestWebSocketServer_BroadcastIncident_WithoutWorkspace tests broadcasting without workspace
func TestWebSocketServer_BroadcastIncident_WithoutWorkspace(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger)
	server.Start()
	defer server.Stop()

	// Broadcast an incident without workspace
	incident := &core.Incident{
		ID:       "incident-1",
		SoulID:   "soul-1",
		Status:   core.IncidentOpen,
		Severity: core.SeverityCritical,
		// No WorkspaceID
	}

	server.BroadcastIncident(incident)

	// Give broadcast time to process
	time.Sleep(50 * time.Millisecond)
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
