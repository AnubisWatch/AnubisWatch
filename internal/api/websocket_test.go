package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/gorilla/websocket"
)

// TestWSClient_JoinRoom tests joining a room
func TestWSClient_JoinRoom(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)

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
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)

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
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)

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
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)

	// Should not panic
	server.removeClient("non-existent-client")
}

// TestWebSocketServer_Stop_NoClients tests stopping a server with no clients
func TestWebSocketServer_Stop_NoClients(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)

	server.Start()
	time.Sleep(10 * time.Millisecond)

	// Should not panic when stopping with no clients
	server.Stop()
}

// TestWebSocketServer_BroadcastIncident tests broadcasting an incident
func TestWebSocketServer_BroadcastIncident(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)
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
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)
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
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)

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
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)

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
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)

	// Should not panic
	server.SubscribeClient("non-existent", []string{"judgment"})
}

// TestWebSocketServer_BroadcastIncident_WithoutWorkspace tests broadcasting without workspace
func TestWebSocketServer_BroadcastIncident_WithoutWorkspace(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)
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

// TestWebSocketServer_removeClient_WithRooms tests removing a client that is in rooms
func TestWebSocketServer_removeClient_WithRooms(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)

	// Create a test server to get a real WebSocket connection
	httpServer := httptest.NewServer(http.HandlerFunc(server.HandleConnection))
	defer httpServer.Close()

	// Connect a WebSocket client
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "?workspace=test&user_id=user1"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Authorization": []string{"Bearer valid-token"}})
	if err != nil {
		t.Fatalf("Failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	// Give time for connection to establish
	time.Sleep(50 * time.Millisecond)

	// Get the client ID
	server.mu.RLock()
	var clientID string
	for id := range server.clients {
		clientID = id
		break
	}
	server.mu.RUnlock()

	if clientID == "" {
		t.Fatal("No client connected")
	}

	// Manually add client to additional rooms
	server.mu.Lock()
	if client, ok := server.clients[clientID]; ok {
		server.rooms["event:judgment"] = map[string]bool{clientID: true}
		client.Rooms["event:judgment"] = true
	}
	server.mu.Unlock()

	// Remove client - should clean up all rooms
	server.removeClient(clientID)

	// Give time for cleanup
	time.Sleep(50 * time.Millisecond)

	// Verify client is removed
	server.mu.RLock()
	if server.clients[clientID] != nil {
		t.Error("Client should be removed from server")
	}
	server.mu.RUnlock()
}

// TestWebSocketServer_broadcastToRoom_NonExistent tests broadcasting to non-existent room
func TestWebSocketServer_broadcastToRoom_NonExistent(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)

	msg := WSMessage{
		Type:      "test",
		Timestamp: time.Now().UTC(),
		Payload:   map[string]string{"key": "value"},
	}

	// Should not panic for non-existent room
	server.broadcastToRoom("non-existent-room", msg)
}

// TestWebSocketServer_broadcastToRoom_WithClients tests broadcasting to room with clients
func TestWebSocketServer_broadcastToRoom_WithClients(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)

	client := &WSClient{
		ID:        "test-client",
		Workspace: "default",
		Rooms:     make(map[string]bool),
		send:      make(chan []byte, 10),
		server:    server,
	}

	// Setup client and room
	server.mu.Lock()
	server.clients[client.ID] = client
	server.rooms["test-room"] = map[string]bool{client.ID: true}
	server.mu.Unlock()

	msg := WSMessage{
		Type:      "test",
		Timestamp: time.Now().UTC(),
		Payload:   map[string]string{"key": "value"},
	}

	// Broadcast to room
	server.broadcastToRoom("test-room", msg)

	// Verify message was sent
	select {
	case <-client.send:
		// Message received
	case <-time.After(100 * time.Millisecond):
		t.Error("Message should be sent to client")
	}
}

// TestWebSocketServer_broadcastToRoom_FullBuffer tests broadcasting when client buffer is full
func TestWebSocketServer_broadcastToRoom_FullBuffer(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)

	// Create a test server to get a real WebSocket connection
	httpServer := httptest.NewServer(http.HandlerFunc(server.HandleConnection))
	defer httpServer.Close()

	// Connect a WebSocket client
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "?workspace=test&user_id=user1"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Authorization": []string{"Bearer valid-token"}})
	if err != nil {
		t.Fatalf("Failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	// Give time for connection to establish
	time.Sleep(50 * time.Millisecond)

	// Get the client ID
	server.mu.RLock()
	var clientID string
	for id := range server.clients {
		clientID = id
		break
	}
	server.mu.RUnlock()

	if clientID == "" {
		t.Fatal("No client connected")
	}

	// Fill the client's send buffer without reading
	server.mu.RLock()
	client := server.clients[clientID]
	server.mu.RUnlock()

	if client != nil {
		// Fill buffer
		for i := 0; i < 256; i++ {
			select {
			case client.send <- []byte("fill"):
			default:
				// Buffer full
			}
		}
	}

	msg := WSMessage{
		Type:      "test",
		Timestamp: time.Now().UTC(),
		Payload:   map[string]string{"key": "value"},
	}

	// Broadcast to room - should attempt to remove client if buffer full
	room := fmt.Sprintf("workspace:%s", "test")
	server.broadcastToRoom(room, msg)

	// Give time for processing
	time.Sleep(50 * time.Millisecond)
}

// TestWebSocketServer_broadcastToRoom_NilClient tests broadcasting when client is nil
func TestWebSocketServer_broadcastToRoom_NilClient(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)

	// Setup room with non-existent client ID
	server.mu.Lock()
	server.rooms["test-room"] = map[string]bool{"ghost-client": true}
	server.mu.Unlock()

	msg := WSMessage{
		Type:      "test",
		Timestamp: time.Now().UTC(),
		Payload:   map[string]string{"key": "value"},
	}

	// Should not panic when client doesn't exist
	server.broadcastToRoom("test-room", msg)
}

// TestWebSocketServer_BroadcastAlert_NoWorkspace tests broadcasting alert without workspace
func TestWebSocketServer_BroadcastAlert_NoWorkspace(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)
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

	// Broadcast an alert without workspace
	event := &core.AlertEvent{
		ID:      "alert-1",
		SoulID:  "soul-1",
		Message: "Test alert",
		// No WorkspaceID
	}

	server.BroadcastAlert(event)

	// Give broadcast time to process
	time.Sleep(50 * time.Millisecond)
}

// TestWebSocketServer_BroadcastStats_NoWorkspace tests broadcasting stats without workspace
func TestWebSocketServer_BroadcastStats_NoWorkspace(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)
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

	// Broadcast stats without workspace
	stats := map[string]int{"total": 10}
	server.BroadcastStats("", stats)

	// Give broadcast time to process
	time.Sleep(50 * time.Millisecond)
}

// TestWebSocketServer_GetStats_WithClients tests getting server stats with clients
func TestWebSocketServer_GetStats_WithClients(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)

	// Create test clients
	for i := 0; i < 3; i++ {
		client := &WSClient{
			ID:        fmt.Sprintf("client-%d", i),
			Workspace: "default",
			Rooms:     make(map[string]bool),
			send:      make(chan []byte, 10),
			server:    server,
		}
		server.mu.Lock()
		server.clients[client.ID] = client
		server.mu.Unlock()
	}

	stats := server.GetStats()

	if stats["connected_clients"] != 3 {
		t.Errorf("Expected 3 clients, got %v", stats["connected_clients"])
	}
}

// TestWebSocketServer_Stop_WithClients tests stopping server with clients
func TestWebSocketServer_Stop_WithClients(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)
	server.Start()

	// Create test clients with send channels
	client1 := &WSClient{
		ID:        "client-1",
		Workspace: "default",
		Rooms:     make(map[string]bool),
		send:      make(chan []byte, 10),
		server:    server,
	}
	client2 := &WSClient{
		ID:        "client-2",
		Workspace: "default",
		Rooms:     make(map[string]bool),
		send:      make(chan []byte, 10),
		server:    server,
	}

	server.mu.Lock()
	server.clients[client1.ID] = client1
	server.clients[client2.ID] = client2
	server.mu.Unlock()

	// Stop should clean up all clients
	server.Stop()

	// Verify all clients removed
	server.mu.RLock()
	if len(server.clients) != 0 {
		t.Errorf("Expected 0 clients after stop, got %d", len(server.clients))
	}
	server.mu.RUnlock()
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

// Test WSClient.handleMessage with invalid JSON
func TestWSClient_handleMessage_InvalidJSON(t *testing.T) {
	client := &WSClient{
		ID:    "test-client",
		Rooms: make(map[string]bool),
		send:  make(chan []byte, 10),
	}

	client.handleMessage([]byte(`{invalid json}`))

	// Should send error message to send channel
	select {
	case msg := <-client.send:
		if !strings.Contains(string(msg), "invalid_message") {
			t.Errorf("Expected invalid_message error, got %s", string(msg))
		}
	default:
		t.Error("Expected error message in send channel")
	}
}

// Test WSClient.handleMessage with ping
func TestWSClient_handleMessage_Ping(t *testing.T) {
	client := &WSClient{
		ID:    "test-client",
		Rooms: make(map[string]bool),
		send:  make(chan []byte, 10),
	}

	client.handleMessage([]byte(`{"type":"ping"}`))

	select {
	case msg := <-client.send:
		if !strings.Contains(string(msg), "pong") {
			t.Errorf("Expected pong, got %s", string(msg))
		}
	default:
		t.Error("Expected pong message in send channel")
	}
}

// Test WSClient.handleMessage with unknown type
func TestWSClient_handleMessage_UnknownType(t *testing.T) {
	client := &WSClient{
		ID:    "test-client",
		Rooms: make(map[string]bool),
		send:  make(chan []byte, 10),
	}

	client.handleMessage([]byte(`{"type":"unknown_type_xyz"}`))

	select {
	case msg := <-client.send:
		if !strings.Contains(string(msg), "unknown_type") {
			t.Errorf("Expected unknown_type error, got %s", string(msg))
		}
	default:
		t.Error("Expected error message in send channel")
	}
}

// Test WSClient.handleMessage with subscribe
func TestWSClient_handleMessage_Subscribe(t *testing.T) {
	server := &WebSocketServer{
		clients: make(map[string]*WSClient),
		rooms:   make(map[string]map[string]bool),
		logger:  newTestLogger(),
	}
	client := &WSClient{
		ID:     "test-client",
		Rooms:  make(map[string]bool),
		send:   make(chan []byte, 10),
		server: server,
	}

	client.handleMessage([]byte(`{"type":"subscribe","events":["judgment","alert"]}`))

	// Should have sent success message
	select {
	case msg := <-client.send:
		if !strings.Contains(string(msg), "subscribed") {
			t.Errorf("Expected subscribed, got %s", string(msg))
		}
	default:
		t.Error("Expected success message in send channel")
	}

	// Should have joined rooms
	if !client.Rooms["event:judgment"] {
		t.Error("Expected client to be in event:judgment room")
	}
	if !client.Rooms["event:alert"] {
		t.Error("Expected client to be in event:alert room")
	}
}

// Test WSClient.handleMessage with unsubscribe
func TestWSClient_handleMessage_Unsubscribe(t *testing.T) {
	server := &WebSocketServer{
		clients: make(map[string]*WSClient),
		rooms:   make(map[string]map[string]bool),
		logger:  newTestLogger(),
	}
	client := &WSClient{
		ID:     "test-client",
		Rooms:  map[string]bool{"event:judgment": true},
		send:   make(chan []byte, 10),
		server: server,
	}

	client.handleMessage([]byte(`{"type":"unsubscribe","events":["judgment"]}`))

	// Should have sent success message
	select {
	case msg := <-client.send:
		if !strings.Contains(string(msg), "unsubscribed") {
			t.Errorf("Expected unsubscribed, got %s", string(msg))
		}
	default:
		t.Error("Expected success message in send channel")
	}

	// Should have left room
	if client.Rooms["event:judgment"] {
		t.Error("Expected client to have left event:judgment room")
	}
}

// Test WSClient.handleMessage with join_workspace
func TestWSClient_handleMessage_JoinWorkspace(t *testing.T) {
	server := &WebSocketServer{
		clients: make(map[string]*WSClient),
		rooms:   make(map[string]map[string]bool),
		logger:  newTestLogger(),
	}
	client := &WSClient{
		ID:        "test-client",
		Workspace: "default",
		Rooms:     make(map[string]bool),
		send:      make(chan []byte, 10),
		server:    server,
	}

	client.handleMessage([]byte(`{"type":"join_workspace","workspace":"new-ws"}`))

	select {
	case msg := <-client.send:
		if !strings.Contains(string(msg), "forbidden") {
			t.Errorf("Expected forbidden error, got %s", string(msg))
		}
	default:
		t.Error("Expected error message in send channel")
	}

	// Workspace should remain unchanged
	if client.Workspace != "default" {
		t.Errorf("Expected workspace default, got %s", client.Workspace)
	}
}

// Test WSClient.handleMessage with join_workspace empty
func TestWSClient_handleMessage_JoinWorkspace_Empty(t *testing.T) {
	client := &WSClient{
		ID:        "test-client",
		Workspace: "default",
		Rooms:     make(map[string]bool),
		send:      make(chan []byte, 10),
	}

	client.handleMessage([]byte(`{"type":"join_workspace","workspace":""}`))

	// Should reject workspace switching with forbidden error
	select {
	case msg := <-client.send:
		if !strings.Contains(string(msg), "forbidden") {
			t.Errorf("Expected forbidden error, got %s", string(msg))
		}
	default:
		t.Error("Expected error message in send channel")
	}
}

// TestWebSocketServer_BroadcastClusterEvent tests cluster event broadcasting
func TestWebSocketServer_BroadcastClusterEvent(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)

	// Create a client subscribed to cluster events
	client := &WSClient{
		ID:        "test-client-cluster",
		Workspace: "default",
		UserID:    "test-user",
		Rooms:     make(map[string]bool),
		send:      make(chan []byte, 256),
		server:    server,
	}
	server.mu.Lock()
	server.clients[client.ID] = client
	server.mu.Unlock()

	// Subscribe to cluster events
	client.JoinRoom("event:cluster")

	// Broadcast a cluster event
	server.BroadcastClusterEvent("jackal.joined", map[string]interface{}{
		"node_id": "node-1",
		"region":  "us-east",
	})

	// Client should receive the message
	select {
	case data := <-client.send:
		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("Failed to parse message: %v", err)
		}
		if msg.Type != "cluster_event" {
			t.Errorf("Expected type 'cluster_event', got '%s'", msg.Type)
		}
		payload, ok := msg.Payload.(map[string]interface{})
		if !ok {
			t.Fatal("Expected payload to be map")
		}
		if payload["event"] != "jackal.joined" {
			t.Errorf("Expected event 'jackal.joined', got '%v'", payload["event"])
		}
	case <-time.After(time.Second):
		t.Fatal("Did not receive cluster event within timeout")
	}
}

func TestWebSocketServer_BroadcastJackalJoined(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)

	client := &WSClient{
		ID:     "test-join",
		Rooms:  make(map[string]bool),
		send:   make(chan []byte, 256),
		server: server,
	}
	server.mu.Lock()
	server.clients[client.ID] = client
	server.mu.Unlock()
	client.JoinRoom("event:cluster")

	server.BroadcastJackalJoined("jackal-1", "eu-west")

	select {
	case data := <-client.send:
		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("Failed to parse message: %v", err)
		}
		if msg.Type != "cluster_event" {
			t.Errorf("Expected 'cluster_event', got '%s'", msg.Type)
		}
		payload := msg.Payload.(map[string]interface{})
		if payload["event"] != "jackal.joined" {
			t.Errorf("Expected event 'jackal.joined', got '%v'", payload["event"])
		}
	case <-time.After(time.Second):
		t.Fatal("Did not receive jackal.joined event")
	}
}

func TestWebSocketServer_BroadcastJackalLeft(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)

	client := &WSClient{
		ID:     "test-leave",
		Rooms:  make(map[string]bool),
		send:   make(chan []byte, 256),
		server: server,
	}
	server.mu.Lock()
	server.clients[client.ID] = client
	server.mu.Unlock()
	client.JoinRoom("event:cluster")

	server.BroadcastJackalLeft("jackal-2", "timeout")

	select {
	case data := <-client.send:
		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("Failed to parse message: %v", err)
		}
		payload := msg.Payload.(map[string]interface{})
		if payload["event"] != "jackal.left" {
			t.Errorf("Expected event 'jackal.left', got '%v'", payload["event"])
		}
	case <-time.After(time.Second):
		t.Fatal("Did not receive jackal.left event")
	}
}

func TestWebSocketServer_BroadcastRaftLeaderChange(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger, &mockAuthenticator{}, nil)

	client := &WSClient{
		ID:     "test-leader",
		Rooms:  make(map[string]bool),
		send:   make(chan []byte, 256),
		server: server,
	}
	server.mu.Lock()
	server.clients[client.ID] = client
	server.mu.Unlock()
	client.JoinRoom("event:cluster")

	server.BroadcastRaftLeaderChange("jackal-3", 42)

	select {
	case data := <-client.send:
		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("Failed to parse message: %v", err)
		}
		payload := msg.Payload.(map[string]interface{})
		if payload["event"] != "raft.leader_change" {
			t.Errorf("Expected 'raft.leader_change', got '%v'", payload["event"])
		}
		eventPayload := payload["payload"].(map[string]interface{})
		if eventPayload["leader_id"] != "jackal-3" {
			t.Errorf("Expected leader_id 'jackal-3', got '%v'", eventPayload["leader_id"])
		}
		if eventPayload["term"].(float64) != 42 {
			t.Errorf("Expected term 42, got '%v'", eventPayload["term"])
		}
	case <-time.After(time.Second):
		t.Fatal("Did not receive raft.leader_change event")
	}
}

func TestClientMessage_SubscribeType(t *testing.T) {
	client := &WSClient{
		ID:    "test-sub",
		Rooms: make(map[string]bool),
		send:  make(chan []byte, 256),
		server: &WebSocketServer{
			clients: make(map[string]*WSClient),
			rooms:   make(map[string]map[string]bool),
		},
	}
	client.server.clients[client.ID] = client
	client.server.logger = newTestLogger()

	client.handleMessage([]byte(`{"type":"subscribe","events":["judgment","alert","cluster_event"]}`))

	select {
	case data := <-client.send:
		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}
		if msg.Type != "success" {
			t.Errorf("Expected 'success', got '%s'", msg.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("Did not receive subscribe response")
	}
}

func TestClientMessage_UnsubscribeType(t *testing.T) {
	client := &WSClient{
		ID:    "test-unsub",
		Rooms: make(map[string]bool),
		send:  make(chan []byte, 256),
		server: &WebSocketServer{
			clients: make(map[string]*WSClient),
			rooms:   make(map[string]map[string]bool),
		},
	}
	client.server.clients[client.ID] = client
	client.server.logger = newTestLogger()

	client.handleMessage([]byte(`{"type":"unsubscribe","events":["judgment"]}`))

	select {
	case data := <-client.send:
		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}
		if msg.Type != "success" {
			t.Errorf("Expected 'success', got '%s'", msg.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("Did not receive unsubscribe response")
	}
}

func TestClientMessage_Ping(t *testing.T) {
	client := &WSClient{
		ID:    "test-ping",
		Rooms: make(map[string]bool),
		send:  make(chan []byte, 10),
		server: &WebSocketServer{
			clients: make(map[string]*WSClient),
			rooms:   make(map[string]map[string]bool),
		},
	}
	client.server.logger = newTestLogger()

	client.handleMessage([]byte(`{"type":"ping"}`))

	select {
	case data := <-client.send:
		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}
		if msg.Type != "pong" {
			t.Errorf("Expected 'pong', got '%s'", msg.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("Did not receive pong response")
	}
}

func TestClientMessage_UnknownType(t *testing.T) {
	client := &WSClient{
		ID:    "test-unknown",
		Rooms: make(map[string]bool),
		send:  make(chan []byte, 10),
		server: &WebSocketServer{
			clients: make(map[string]*WSClient),
			rooms:   make(map[string]map[string]bool),
		},
	}
	client.server.logger = newTestLogger()

	client.handleMessage([]byte(`{"type":"foobar"}`))

	select {
	case data := <-client.send:
		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}
		if msg.Type != "error" {
			t.Errorf("Expected 'error', got '%s'", msg.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("Did not receive error response")
	}
}
