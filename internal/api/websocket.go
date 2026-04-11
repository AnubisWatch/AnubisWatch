package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/gorilla/websocket"
)

// WebSocketServer handles real-time WebSocket connections
// The Oracle's live visions stream to the priests
type WebSocketServer struct {
	mu        sync.RWMutex
	clients   map[string]*WSClient
	rooms     map[string]map[string]bool // room -> clientIDs
	upgrader  websocket.Upgrader
	logger    *slog.Logger
	broadcast chan WSMessage
}

// WSClient represents a connected WebSocket client
type WSClient struct {
	ID        string
	Conn      *websocket.Conn
	Workspace string
	UserID    string
	Rooms     map[string]bool
	send      chan []byte
	server    *WebSocketServer
	mu        sync.RWMutex
}

// NewWebSocketServer creates a new WebSocket server
func NewWebSocketServer(logger *slog.Logger) *WebSocketServer {
	return &WebSocketServer{
		clients: make(map[string]*WSClient),
		rooms:   make(map[string]map[string]bool),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				// Allow all origins in development
				// In production, this should be restricted
				return true
			},
		},
		logger:    logger.With("component", "websocket"),
		broadcast: make(chan WSMessage, 256),
	}
}

// Start starts the WebSocket server
func (s *WebSocketServer) Start() {
	go s.broadcastLoop()
	s.logger.Info("WebSocket server started")
}

// Stop stops the WebSocket server
func (s *WebSocketServer) Stop() {
	s.mu.Lock()
	for _, client := range s.clients {
		if client.send != nil {
			close(client.send)
		}
		if client.Conn != nil {
			client.Conn.Close()
		}
	}
	s.clients = make(map[string]*WSClient)
	s.rooms = make(map[string]map[string]bool)
	s.mu.Unlock()
	if s.broadcast != nil {
		close(s.broadcast)
	}
	s.logger.Info("WebSocket server stopped")
}

// HandleConnection handles new WebSocket connections
func (s *WebSocketServer) HandleConnection(w http.ResponseWriter, r *http.Request) {
	// Get workspace from query params
	workspace := r.URL.Query().Get("workspace")
	if workspace == "" {
		workspace = "default"
	}

	// Get user ID from query params (in production, this should come from auth token)
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		userID = "anonymous"
	}

	// Upgrade HTTP to WebSocket
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("Failed to upgrade WebSocket", "error", err)
		return
	}

	// Create client
	client := &WSClient{
		ID:        generateClientID(),
		Conn:      conn,
		Workspace: workspace,
		UserID:    userID,
		Rooms:     make(map[string]bool),
		send:      make(chan []byte, 256),
		server:    s,
	}

	// Register client
	s.mu.Lock()
	s.clients[client.ID] = client
	s.mu.Unlock()

	// Subscribe to workspace room
	client.JoinRoom(fmt.Sprintf("workspace:%s", workspace))

	// Send welcome message
	welcome := WSMessage{
		Type:      "connected",
		Timestamp: time.Now().UTC(),
		Payload: map[string]interface{}{
			"client_id":   client.ID,
			"workspace":   workspace,
			"server_time": time.Now().UTC(),
		},
	}
	data, _ := json.Marshal(welcome)
	client.send <- data

	s.logger.Info("Client connected",
		"client_id", client.ID,
		"workspace", workspace,
		"remote_addr", r.RemoteAddr)

	// Start goroutines
	go client.writePump()
	go client.readPump()
}

// JoinRoom subscribes a client to a room
func (c *WSClient) JoinRoom(room string) {
	c.mu.Lock()
	c.Rooms[room] = true
	c.mu.Unlock()

	c.server.mu.Lock()
	if c.server.rooms[room] == nil {
		c.server.rooms[room] = make(map[string]bool)
	}
	c.server.rooms[room][c.ID] = true
	c.server.mu.Unlock()

	c.server.logger.Debug("Client joined room", "client_id", c.ID, "room", room)
}

// LeaveRoom unsubscribes a client from a room
func (c *WSClient) LeaveRoom(room string) {
	c.mu.Lock()
	delete(c.Rooms, room)
	c.mu.Unlock()

	c.server.mu.Lock()
	if c.server.rooms[room] != nil {
		delete(c.server.rooms[room], c.ID)
		if len(c.server.rooms[room]) == 0 {
			delete(c.server.rooms, room)
		}
	}
	c.server.mu.Unlock()

	c.server.logger.Debug("Client left room", "client_id", c.ID, "room", room)
}

// readPump reads messages from the WebSocket connection
func (c *WSClient) readPump() {
	defer func() {
		c.server.removeClient(c.ID)
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(512 * 1024) // 512KB max message size
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.server.logger.Error("WebSocket error", "client_id", c.ID, "error", err)
			}
			break
		}

		// Handle incoming message
		c.handleMessage(message)
	}
}

// handleMessage processes incoming client messages
func (c *WSClient) handleMessage(data []byte) {
	var msg ClientMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		c.send <- c.createErrorMessage("invalid_message", "Failed to parse message")
		return
	}

	switch msg.Type {
	case "subscribe":
		// Subscribe to events
		for _, event := range msg.Events {
			room := fmt.Sprintf("event:%s", event)
			c.JoinRoom(room)
		}
		c.send <- c.createSuccessMessage("subscribed", msg.Events)

	case "unsubscribe":
		// Unsubscribe from events
		for _, event := range msg.Events {
			room := fmt.Sprintf("event:%s", event)
			c.LeaveRoom(room)
		}
		c.send <- c.createSuccessMessage("unsubscribed", msg.Events)

	case "ping":
		// Respond with pong
		c.send <- c.createMessage("pong", map[string]interface{}{
			"timestamp": time.Now().UTC().Unix(),
		})

	case "join_workspace":
		// Switch workspace
		if msg.Workspace != "" {
			c.LeaveRoom(fmt.Sprintf("workspace:%s", c.Workspace))
			c.Workspace = msg.Workspace
			c.JoinRoom(fmt.Sprintf("workspace:%s", c.Workspace))
			c.send <- c.createSuccessMessage("workspace_changed", c.Workspace)
		}

	default:
		c.send <- c.createErrorMessage("unknown_type", fmt.Sprintf("Unknown message type: %s", msg.Type))
	}
}

// writePump writes messages to the WebSocket connection
func (c *WSClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				// Channel closed
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			c.Conn.WriteMessage(websocket.TextMessage, message)

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// createMessage creates a WebSocket message
func (c *WSClient) createMessage(msgType string, payload interface{}) []byte {
	msg := WSMessage{
		Type:      msgType,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
	data, _ := json.Marshal(msg)
	return data
}

// createSuccessMessage creates a success message
func (c *WSClient) createSuccessMessage(action string, data interface{}) []byte {
	return c.createMessage("success", map[string]interface{}{
		"action": action,
		"data":   data,
	})
}

// createErrorMessage creates an error message
func (c *WSClient) createErrorMessage(code, message string) []byte {
	return c.createMessage("error", map[string]interface{}{
		"code":    code,
		"message": message,
	})
}

// removeClient removes a client
func (s *WebSocketServer) removeClient(clientID string) {
	s.mu.Lock()
	client, exists := s.clients[clientID]
	if !exists {
		s.mu.Unlock()
		return
	}

	// Remove from rooms
	for room := range client.Rooms {
		if s.rooms[room] != nil {
			delete(s.rooms[room], clientID)
			if len(s.rooms[room]) == 0 {
				delete(s.rooms, room)
			}
		}
	}

	delete(s.clients, clientID)
	s.mu.Unlock()

	close(client.send)
	client.Conn.Close()

	s.logger.Info("Client disconnected", "client_id", clientID)
}

// broadcastLoop broadcasts messages to all clients
func (s *WebSocketServer) broadcastLoop() {
	for msg := range s.broadcast {
		data, err := json.Marshal(msg)
		if err != nil {
			s.logger.Error("Failed to marshal message", "error", err)
			continue
		}

		s.mu.RLock()
		clients := make([]*WSClient, 0, len(s.clients))
		for _, client := range s.clients {
			clients = append(clients, client)
		}
		s.mu.RUnlock()

		for _, client := range clients {
			select {
			case client.send <- data:
			default:
				// Client send buffer full, close connection
				s.removeClient(client.ID)
			}
		}
	}
}

// BroadcastToWorkspace broadcasts a message to all clients in a workspace
func (s *WebSocketServer) BroadcastToWorkspace(workspace string, msg WSMessage) {
	room := fmt.Sprintf("workspace:%s", workspace)
	s.broadcastToRoom(room, msg)
}

// BroadcastToRoom broadcasts a message to a specific room
func (s *WebSocketServer) broadcastToRoom(room string, msg WSMessage) {
	s.mu.RLock()
	clients, exists := s.rooms[room]
	s.mu.RUnlock()

	if !exists {
		return
	}

	data, err := json.Marshal(msg)
	if err != nil {
		s.logger.Error("Failed to marshal message", "error", err)
		return
	}

	for clientID := range clients {
		s.mu.RLock()
		client, ok := s.clients[clientID]
		s.mu.RUnlock()

		if ok {
			select {
			case client.send <- data:
			default:
				s.removeClient(client.ID)
			}
		}
	}
}

// BroadcastJudgment broadcasts a judgment to connected clients
func (s *WebSocketServer) BroadcastJudgment(judgment *core.Judgment) {
	msg := WSMessage{
		Type:      "judgment",
		Timestamp: time.Now().UTC(),
		Payload:   judgment,
	}

	// Broadcast to workspace room
	s.BroadcastToWorkspace(judgment.WorkspaceID, msg)

	// Also broadcast to event room
	s.broadcastToRoom("event:judgment", msg)

	// Add to general broadcast
	s.broadcast <- msg
}

// BroadcastAlert broadcasts an alert to connected clients
func (s *WebSocketServer) BroadcastAlert(event *core.AlertEvent) {
	msg := WSMessage{
		Type:      "alert",
		Timestamp: time.Now().UTC(),
		Payload:   event,
	}

	// Broadcast to workspace room
	if event.WorkspaceID != "" {
		s.BroadcastToWorkspace(event.WorkspaceID, msg)
	}

	// Also broadcast to event room
	s.broadcastToRoom("event:alert", msg)

	// Add to general broadcast
	s.broadcast <- msg
}

// BroadcastStats broadcasts stats update to connected clients
func (s *WebSocketServer) BroadcastStats(workspace string, stats interface{}) {
	msg := WSMessage{
		Type:      "stats",
		Timestamp: time.Now().UTC(),
		Payload:   stats,
	}

	if workspace != "" {
		s.BroadcastToWorkspace(workspace, msg)
	}

	s.broadcastToRoom("event:stats", msg)
	s.broadcast <- msg
}

// BroadcastIncident broadcasts an incident update to connected clients
func (s *WebSocketServer) BroadcastIncident(incident *core.Incident) {
	msg := WSMessage{
		Type:      "incident",
		Timestamp: time.Now().UTC(),
		Payload:   incident,
	}

	if incident.WorkspaceID != "" {
		s.BroadcastToWorkspace(incident.WorkspaceID, msg)
	}

	s.broadcastToRoom("event:incident", msg)
	s.broadcast <- msg
}

// BroadcastSoulUpdate broadcasts a soul update to connected clients
func (s *WebSocketServer) BroadcastSoulUpdate(soul *core.Soul) {
	msg := WSMessage{
		Type:      "soul_update",
		Timestamp: time.Now().UTC(),
		Payload:   soul,
	}

	s.BroadcastToWorkspace(soul.WorkspaceID, msg)
	s.broadcastToRoom("event:soul", msg)
	s.broadcast <- msg
}

// BroadcastClusterEvent broadcasts a cluster lifecycle event (jackal join/leave,
// raft leader change, etc.) to connected clients.
func (s *WebSocketServer) BroadcastClusterEvent(event string, payload interface{}) {
	msg := WSMessage{
		Type:      "cluster_event",
		Timestamp: time.Now().UTC(),
		Payload: map[string]interface{}{
			"event":   event,
			"payload": payload,
		},
	}

	s.broadcastToRoom("event:cluster", msg)
	s.broadcast <- msg
}

// BroadcastJackalJoined broadcasts that a jackal node joined the cluster
func (s *WebSocketServer) BroadcastJackalJoined(nodeID, region string) {
	s.BroadcastClusterEvent("jackal.joined", map[string]interface{}{
		"node_id": nodeID,
		"region":  region,
	})
}

// BroadcastJackalLeft broadcasts that a jackal node left the cluster
func (s *WebSocketServer) BroadcastJackalLeft(nodeID, reason string) {
	s.BroadcastClusterEvent("jackal.left", map[string]interface{}{
		"node_id": nodeID,
		"reason":  reason,
	})
}

// BroadcastRaftLeaderChange broadcasts a Raft leader change event
func (s *WebSocketServer) BroadcastRaftLeaderChange(leaderID string, term uint64) {
	s.BroadcastClusterEvent("raft.leader_change", map[string]interface{}{
		"leader_id": leaderID,
		"term":      term,
	})
}

// SubscribeClient subscribes a client to specific events
func (s *WebSocketServer) SubscribeClient(clientID string, events []string) {
	s.mu.RLock()
	client, exists := s.clients[clientID]
	s.mu.RUnlock()

	if !exists {
		return
	}

	for _, event := range events {
		room := fmt.Sprintf("event:%s", event)
		client.JoinRoom(room)
	}

	s.logger.Debug("Client subscribed", "client_id", clientID, "events", events)
}

// UnsubscribeClient unsubscribes a client
func (s *WebSocketServer) UnsubscribeClient(clientID string, events []string) {
	s.mu.RLock()
	client, exists := s.clients[clientID]
	s.mu.RUnlock()

	if !exists {
		return
	}

	for _, event := range events {
		room := fmt.Sprintf("event:%s", event)
		client.LeaveRoom(room)
	}

	s.logger.Debug("Client unsubscribed", "client_id", clientID, "events", events)
}

// GetStats returns WebSocket server statistics
func (s *WebSocketServer) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Count clients per workspace
	workspaceCounts := make(map[string]int)
	for _, client := range s.clients {
		workspaceCounts[client.Workspace]++
	}

	return map[string]interface{}{
		"connected_clients": len(s.clients),
		"active_rooms":      len(s.rooms),
		"workspace_counts":  workspaceCounts,
		"broadcast_queue":   len(s.broadcast),
	}
}

// GetClientCount returns the number of connected clients
func (s *WebSocketServer) GetClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// IsWebSocketRequest checks if the request is a WebSocket upgrade
func IsWebSocketRequest(r *http.Request) bool {
	return r.Header.Get("Upgrade") == "websocket"
}

// WSMessage is a WebSocket message sent from server to client
type WSMessage struct {
	Type      string      `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Payload   interface{} `json:"payload"`
}

// ClientMessage is a WebSocket message sent from client to server
type ClientMessage struct {
	Type      string   `json:"type"`
	Events    []string `json:"events,omitempty"`
	Workspace string   `json:"workspace,omitempty"`
}

func generateClientID() string {
	return fmt.Sprintf("ws_%d", time.Now().UnixNano())
}
