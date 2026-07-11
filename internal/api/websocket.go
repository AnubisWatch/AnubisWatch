package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/coder/websocket"
)

// connectionLimiter tracks rate limit state for an IP
type connectionLimiter struct {
	connections  int       // current concurrent connections
	lastConnect  time.Time // last connection attempt
	connectCount int       // connections in current window
	windowReset  time.Time // when to reset the window
	messageCount int       // messages in current window
	messageReset time.Time // when to reset message window
}

// WebSocketServer handles real-time WebSocket connections
// The Oracle's live visions stream to the priests
type WebSocketServer struct {
	mu             sync.RWMutex
	clients        map[string]*WSClient
	rooms          map[string]map[string]bool // room -> clientIDs
	logger         *slog.Logger
	broadcast      chan WSMessage
	authenticator  Authenticator // Added for token validation - uses Authenticator from rest.go
	allowedOrigins []string      // Allowed origins for WebSocket connections (CSRF protection)
	trustedProxies []string      // proxies whose X-Forwarded-For may be trusted for client IP

	// Rate limiting
	ipLimits         map[string]*connectionLimiter // IP -> limiter state
	maxConnsPerIP    int                           // maximum concurrent connections per IP
	maxConnsPerUser  int                           // maximum concurrent connections per user
	connRateLimit    int                           // max connection attempts per window per IP
	rateLimitWindow  time.Duration                 // rate limit window duration
	messageRateLimit int                           // max messages per window per client
	messageWindow    time.Duration                 // message rate limit window

	// Shutdown coordination
	ctx    context.Context    // cancelled on Stop() to signal broadcastLoop exit
	cancel context.CancelFunc // cancels ctx
	stopWg sync.WaitGroup
}

// WSClient represents a connected WebSocket client
type WSClient struct {
	ID        string
	Conn      *websocket.Conn
	Workspace string
	UserID    string
	IP        string // client IP for rate limiting
	Rooms     map[string]bool
	send      chan []byte
	sendOnce  sync.Once // protect against double-close on send channel
	server    *WebSocketServer
	mu        sync.RWMutex
	ctx       context.Context    // client connection context (cancelled on disconnect)
	cancel    context.CancelFunc // cancel function for the connection context
}

// NewWebSocketServer creates a new WebSocket server
func NewWebSocketServer(logger *slog.Logger, authenticator Authenticator, allowedOrigins []string) *WebSocketServer {
	if len(allowedOrigins) == 0 {
		// SECURITY: No default origins in production - require explicit configuration
		// This prevents unintended cross-origin connections
		logger.Warn("WebSocket allowedOrigins is empty - no origins will be allowed. Configure allowed_origins in server config.")
		allowedOrigins = []string{}
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &WebSocketServer{
		clients:          make(map[string]*WSClient),
		rooms:            make(map[string]map[string]bool),
		logger:           logger.With("component", "websocket"),
		broadcast:        make(chan WSMessage, 256),
		authenticator:    authenticator,
		allowedOrigins:   allowedOrigins,
		ipLimits:         make(map[string]*connectionLimiter),
		maxConnsPerIP:    10,          // max 10 concurrent connections per IP
		maxConnsPerUser:  5,           // max 5 concurrent connections per user
		connRateLimit:    60,          // max 60 connection attempts per minute per IP
		rateLimitWindow:  time.Minute, // 1 minute window
		messageRateLimit: 60,          // max 60 messages per minute per client (VULN-005 fix)
		messageWindow:    time.Minute, // 1 minute window for message rate limiting
		ctx:              ctx,
		cancel:           cancel,
	}
}

// Start starts the WebSocket server
func (s *WebSocketServer) Start() {
	s.stopWg.Add(1)
	go s.broadcastLoop(s.ctx)
	s.logger.Info("WebSocket server started")
}

// Stop stops the WebSocket server
func (s *WebSocketServer) Stop() {
	// Cancel the server context first — this signals broadcastLoop to exit
	// via its <-ctx.Done() select branch. Then close the broadcast channel
	// as a secondary signal (the drain loop handles remaining messages).
	s.cancel()

	// Close broadcast channel to signal broadcastLoop. Closing while holding
	// no lock is safe: broadcastLoop reads the channel without holding the
	// mutex; safeSend's defer/recover guards against sends racing close.
	if s.broadcast != nil {
		close(s.broadcast)
	}

	// Wait for broadcastLoop to exit after context cancellation and channel
	// close. TheWg.Wait ensures the loop has finished before we close
	// client channels.
	s.stopWg.Wait()

	s.mu.Lock()
	for _, client := range s.clients {
		if client.send != nil {
			client.sendOnce.Do(func() { close(client.send) })
		}
		if client.cancel != nil {
			client.cancel()
		}
		if client.Conn != nil {
			client.Conn.CloseNow()
		}
	}
	s.clients = make(map[string]*WSClient)
	s.rooms = make(map[string]map[string]bool)
	s.mu.Unlock()
	s.logger.Info("WebSocket server stopped")
}

// realIP returns the client IP used for rate/connection limiting. It honours
// X-Forwarded-For only when the connection originates from a configured trusted
// proxy; otherwise it uses RemoteAddr. This prevents an attacker from spoofing
// X-Forwarded-For to bypass the per-IP DoS limits (matching the REST path).
func (s *WebSocketServer) realIP(r *http.Request) string {
	if len(s.trustedProxies) > 0 {
		if remoteHost, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			for _, proxy := range s.trustedProxies {
				if remoteHost == proxy {
					if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
						return strings.TrimSpace(strings.Split(fwd, ",")[0])
					}
					break
				}
			}
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// HandleConnection handles new WebSocket connections
func (s *WebSocketServer) HandleConnection(w http.ResponseWriter, r *http.Request) {
	// Get client IP for rate limiting (spoof-resistant: trusts X-Forwarded-For
	// only from configured proxies).
	clientIP := s.realIP(r)
	// Strip port from IP address if present (e.g., "192.168.1.1:1234" -> "192.168.1.1")
	// This prevents rate limit bypass via ephemeral port changes
	if host, _, err := net.SplitHostPort(clientIP); err == nil {
		clientIP = host
	}

	// SECURITY: Atomically check rate-limit and reserve a connection slot
	// under the write lock, closing the TOCTOU window between check and
	// increment. The slot is released by removeClient via decrementConnectionCount.
	if !s.reserveConnection(clientIP) {
		s.logger.Warn("WebSocket connection rejected: rate limit or connection limit exceeded",
			"remote_addr", clientIP)
		http.Error(w, "Too Many Requests: rate limit or connection limit exceeded", http.StatusTooManyRequests)
		return
	}

	// Extract token from Authorization header or httpOnly auth cookie.
	// Browser WebSocket clients cannot set Authorization headers, so the dashboard
	// relies on the same secure cookie used by REST API requests.
	// SECURITY: Reject query parameter tokens to prevent token leakage in access logs,
	// browser history, and Referer headers. (HIGH-03 fix)
	if r.URL.Query().Get("token") != "" {
		s.logger.Warn("WebSocket connection rejected: token via query parameter is not allowed",
			"remote_addr", r.RemoteAddr)
		http.Error(w, "Unauthorized: token must be provided via Authorization header or auth cookie, not query parameter", http.StatusUnauthorized)
		return
	}

	authHeader := r.Header.Get("Authorization")
	token := ""
	if strings.HasPrefix(authHeader, "Bearer ") {
		token = strings.TrimPrefix(authHeader, "Bearer ")
	} else if cookie, err := r.Cookie("auth_token"); err == nil {
		token = cookie.Value
	}

	// Validate token
	if token == "" {
		s.logger.Warn("WebSocket connection rejected: empty token", "remote_addr", r.RemoteAddr)
		http.Error(w, "Unauthorized: missing token", http.StatusUnauthorized)
		return
	}

	// Authenticate the token
	user, err := s.authenticator.Authenticate(token)
	if err != nil {
		s.logger.Warn("WebSocket connection rejected: invalid token",
			"remote_addr", r.RemoteAddr,
			"error", err)
		http.Error(w, "Unauthorized: invalid token", http.StatusUnauthorized)
		return
	}

	// Use authenticated user's workspace only — never trust client-supplied
	// workspace query parameter (was a tenant isolation bypass).
	workspace := user.Workspace
	if workspace == "" {
		workspace = "default"
	}

	// Upgrade HTTP to WebSocket with origin checking
	opts := &websocket.AcceptOptions{
		OriginPatterns: s.allowedOrigins,
	}
	conn, err := websocket.Accept(w, r, opts)
	if err != nil {
		s.logger.Error("Failed to upgrade WebSocket", "error", err)
		return
	}

	// Create a cancellable context for this connection
	ctx, cancel := context.WithCancel(context.Background())

	// Create client with authenticated user info
	client := &WSClient{
		ID:        generateClientID(),
		Conn:      conn,
		Workspace: workspace,
		UserID:    user.ID,
		IP:        clientIP, // store IP for rate limiting cleanup
		Rooms:     make(map[string]bool),
		send:      make(chan []byte, 256),
		server:    s,
		ctx:       ctx,
		cancel:    cancel,
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
			"user_id":     user.ID,
			"server_time": time.Now().UTC(),
		},
	}
	data, _ := json.Marshal(welcome)
	client.send <- data

	s.logger.Info("Client connected",
		"client_id", client.ID,
		"user_id", user.ID,
		"workspace", workspace,
		"remote_addr", r.RemoteAddr)

	// Start goroutines — writePump receives the same cancellable context
	// so it exits when the connection is closed (via client.cancel in Stop()).
	go client.writePump(ctx)
	go client.readPump(ctx)
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
func (c *WSClient) readPump(ctx context.Context) {
	defer func() {
		c.server.removeClient(c.ID)
		c.Conn.CloseNow()
	}()

	c.Conn.SetReadLimit(512 * 1024) // 512KB max message size

	for {
		readCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		msgType, data, err := c.Conn.Read(readCtx)
		cancel()
		if err != nil {
			closeStatus := websocket.CloseStatus(err)
			if closeStatus != websocket.StatusNormalClosure &&
				closeStatus != websocket.StatusGoingAway &&
				closeStatus != -1 {
				c.server.logger.Error("WebSocket error", "client_id", c.ID, "error", err)
			}
			break
		}

		// Only handle text/binary messages
		if msgType == websocket.MessageText {
			c.handleMessage(data)
		}
	}
}

// handleMessage processes incoming client messages
func (c *WSClient) handleMessage(data []byte) {
	// SECURITY: Check message rate limit (VULN-005 fix)
	// Prevent DoS via message flooding - per-client limit (not per-IP)
	// This prevents NAT/proxy users from interfering with each other
	// Skip if server is nil (test mode) or rate limiting is not configured
	if c.server != nil && c.server.messageRateLimit > 0 && !c.server.checkMessageRateLimit(c.ID) {
		c.server.logger.Warn("WebSocket message rejected: rate limit exceeded",
			"client_id", c.ID,
			"ip", c.IP)
		c.send <- c.createErrorMessage("rate_limited", "Too many messages - rate limit exceeded")
		return
	}

	var msg ClientMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		c.send <- c.createErrorMessage("invalid_message", "Failed to parse message")
		return
	}

	switch msg.Type {
	case "subscribe":
		// Subscribe to events
		for _, event := range msg.Events {
			c.JoinRoom(workspaceEventRoom(c.Workspace, event))
		}
		c.send <- c.createSuccessMessage("subscribed", msg.Events)

	case "unsubscribe":
		// Unsubscribe from events
		for _, event := range msg.Events {
			c.LeaveRoom(workspaceEventRoom(c.Workspace, event))
		}
		c.send <- c.createSuccessMessage("unsubscribed", msg.Events)

	case "ping":
		// Respond with pong
		c.send <- c.createMessage("pong", map[string]interface{}{
			"timestamp": time.Now().UTC().Unix(),
		})

	case "join_workspace":
		// Reject workspace switching - users are bound to their authenticated workspace
		c.send <- c.createErrorMessage("forbidden", "workspace switching not supported")

	default:
		c.send <- c.createErrorMessage("unknown_type", fmt.Sprintf("Unknown message type: %s", msg.Type))
	}
}

// writePump writes messages to the WebSocket connection.
// The ctx parameter is the client connection context — it is cancelled by Stop()
// via client.cancel, ensuring the pump exits when the connection closes.
func (c *WSClient) writePump(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.CloseNow()
	}()

	for {
		select {
		case <-ctx.Done():
			// Connection context cancelled — client is disconnecting
			return
		case message, ok := <-c.send:
			if !ok {
				// Channel closed
				c.Conn.Close(websocket.StatusNormalClosure, "")
				return
			}

			writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Conn.Write(writeCtx, websocket.MessageText, message)
			cancel()
			if err != nil {
				return
			}

		case <-ticker.C:
			// coder/websocket handles ping automatically via ping/pong callbacks
			// Send a manual ping for liveness check
			pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			if err := c.Conn.Ping(pingCtx); err != nil {
				cancel()
				return
			}
			cancel()
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

	// Get IP before releasing lock
	clientIP := client.IP

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
	// Clean up the per-client message rate limiter entry created by checkMessageRateLimit
	delete(s.ipLimits, clientID)
	s.mu.Unlock()

	// Decrement connection count for rate limiting (outside of lock to avoid deadlock)
	s.decrementConnectionCount(clientIP)

	if client.cancel != nil {
		client.cancel()
	}
	// Use sync.Once to prevent double-close if Stop() already closed it
	client.sendOnce.Do(func() { close(client.send) })
	client.Conn.CloseNow()

	s.logger.Info("Client disconnected", "client_id", clientID)
}

// broadcastLoop broadcasts messages to all clients.
// The ctx is cancelled by Stop(), ensuring the loop exits when the server shuts down.
func (s *WebSocketServer) broadcastLoop(ctx context.Context) {
	defer s.stopWg.Done()
	for {
		select {
		case <-ctx.Done():
			// Server context cancelled — drain remaining broadcast messages
			// before exiting so in-flight messages aren't dropped.
			for msg := range s.broadcast {
				s.broadcastMessage(msg)
			}
			return
		case msg, ok := <-s.broadcast:
			if !ok {
				// Channel closed externally (Stop called)
				return
			}
			s.broadcastMessage(msg)
		}
	}
}

// broadcastMessage sends a message to all connected clients.
// Called by broadcastLoop; not safe to call directly with the server lock held.
func (s *WebSocketServer) broadcastMessage(msg WSMessage) {
	data, _ := json.Marshal(msg)

	s.mu.RLock()
	clients := make([]*WSClient, 0, len(s.clients))
	for _, client := range s.clients {
		clients = append(clients, client)
	}
	s.mu.RUnlock()

	for _, client := range clients {
		// safeSend uses the client context so that sends to a disconnecting
		// client return immediately instead of hanging.
		if err := safeSend(client.ctx, client.send, data); err != nil {
			s.removeClient(client.ID)
		}
	}
}

// safeSend sends data to a client send channel.
// It checks the client context first (cancellation), then attempts a
// non-blocking send. If the channel is full it returns immediately;
// if the channel is closed the select will unblock and return an error.
// G118 fix: the ctx parameter makes the sender's goroutine cancellable.
// A nil ctx is treated as a no-op (context never done) — safe for test clients.
func safeSend(ctx context.Context, ch chan []byte, data []byte) error {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	// Uses a non-blocking send. The channel close is guarded by
	// client.sendOnce.Do(close) in Stop() — the panic from sending
	// on a closed channel can still occur in a race window, so a
	// narrow defer/recover is kept as a safety net.
	defer func() { recover() }()
	select {
	case ch <- data:
		return nil
	default:
		return fmt.Errorf("send buffer full")
	}
}

// BroadcastToWorkspace broadcasts a message to all clients in a workspace
func (s *WebSocketServer) BroadcastToWorkspace(workspace string, msg WSMessage) {
	room := fmt.Sprintf("workspace:%s", workspace)
	s.broadcastToRoom(room, msg)
}

func workspaceEventRoom(workspace, event string) string {
	if workspace == "" {
		return fmt.Sprintf("event:%s", event)
	}
	return fmt.Sprintf("workspace:%s:event:%s", workspace, event)
}

// broadcastToRoom sends a message to a specific room.
// Delegates to broadcastMessage after resolving the room's client set.
func (s *WebSocketServer) broadcastToRoom(room string, msg WSMessage) {
	s.mu.RLock()
	clients, exists := s.rooms[room]
	if !exists {
		s.mu.RUnlock()
		return
	}

	// Snapshot client IDs while holding the read lock. Iterating the map
	// after releasing the lock races with JoinRoom/LeaveRoom/removeClient
	// which mutate s.rooms under the write lock, causing a fatal concurrent
	// map read/write panic.
	clientIDs := make([]string, 0, len(clients))
	for id := range clients {
		clientIDs = append(clientIDs, id)
	}
	s.mu.RUnlock()

	data, _ := json.Marshal(msg)

	for _, clientID := range clientIDs {
		s.mu.RLock()
		client, ok := s.clients[clientID]
		s.mu.RUnlock()

		if ok {
			// safeSend uses client.ctx so sends to a disconnecting client
			// fail immediately instead of blocking the broadcast loop.
			if err := safeSend(client.ctx, client.send, data); err != nil {
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

	s.broadcastTenantEvent(judgment.WorkspaceID, "judgment", msg)
}

// BroadcastAlert broadcasts an alert to connected clients
func (s *WebSocketServer) BroadcastAlert(event *core.AlertEvent) {
	msg := WSMessage{
		Type:      "alert",
		Timestamp: time.Now().UTC(),
		Payload:   event,
	}

	s.broadcastTenantEvent(event.WorkspaceID, "alert", msg)
}

// BroadcastStats broadcasts stats update to connected clients
func (s *WebSocketServer) BroadcastStats(workspace string, stats interface{}) {
	msg := WSMessage{
		Type:      "stats",
		Timestamp: time.Now().UTC(),
		Payload:   stats,
	}

	if workspace != "" {
		s.broadcastToRoom(workspaceEventRoom(workspace, "stats"), msg)
		return
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

	s.broadcastTenantEvent(incident.WorkspaceID, "incident", msg)
}

// BroadcastSoulUpdate broadcasts a soul update to connected clients
func (s *WebSocketServer) BroadcastSoulUpdate(soul *core.Soul) {
	msg := WSMessage{
		Type:      "soul_update",
		Timestamp: time.Now().UTC(),
		Payload:   soul,
	}

	s.broadcastTenantEvent(soul.WorkspaceID, "soul", msg)
}

func (s *WebSocketServer) broadcastTenantEvent(workspace, event string, msg WSMessage) {
	// Never broadcast globally — even if workspace is empty, scope to the
	// "default" room only. The previous global broadcast was a cross-tenant leak.
	if workspace == "" {
		workspace = "default"
	}
	s.broadcastToRoom(workspaceEventRoom(workspace, event), msg)
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
		client.JoinRoom(workspaceEventRoom(client.Workspace, event))
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
		client.LeaveRoom(workspaceEventRoom(client.Workspace, event))
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

// checkMessageRateLimit checks if a client has exceeded the message rate limit
// Uses clientID (not IP) to prevent NAT users from sharing limits
// Returns true if message is allowed, false if rate limited
func (s *WebSocketServer) checkMessageRateLimit(clientID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Handle test cases where ipLimits may not be initialized
	if s.ipLimits == nil {
		s.ipLimits = make(map[string]*connectionLimiter)
	}

	now := time.Now()
	limiter, exists := s.ipLimits[clientID]
	if !exists {
		// No existing client entry - create one
		s.ipLimits[clientID] = &connectionLimiter{
			messageReset: now.Add(s.messageWindow),
		}
		limiter = s.ipLimits[clientID]
	}

	// Reset message window if expired
	if now.After(limiter.messageReset) {
		limiter.messageCount = 0
		limiter.messageReset = now.Add(s.messageWindow)
	}

	// Check message rate limit
	if limiter.messageCount >= s.messageRateLimit {
		return false
	}

	limiter.messageCount++
	return true
}
func generateClientID() string {
	return fmt.Sprintf("ws_%d", time.Now().UnixNano())
}

// checkRateLimit checks if the IP has exceeded the connection rate limit
// Returns true if connection is allowed, false if rate limited
func (s *WebSocketServer) checkRateLimit(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	limiter, exists := s.ipLimits[ip]
	if !exists {
		// First connection from this IP
		s.ipLimits[ip] = &connectionLimiter{
			lastConnect:  now,
			connectCount: 1,
			windowReset:  now.Add(s.rateLimitWindow),
			messageReset: now.Add(s.messageWindow), // Initialize message window
		}
		return true
	}

	// Reset window if expired
	if now.After(limiter.windowReset) {
		limiter.connectCount = 0
		limiter.windowReset = now.Add(s.rateLimitWindow)
	}

	// Check rate limit
	if limiter.connectCount >= s.connRateLimit {
		return false
	}

	limiter.connectCount++
	limiter.lastConnect = now
	return true
}

// reserveConnection atomically checks and reserves a connection slot for an IP
// under the write lock, closing the TOCTOU window between check and increment.
// Returns true if the slot was reserved; the caller must eventually call
// decrementConnectionCount to release it (via removeClient).
func (s *WebSocketServer) reserveConnection(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ipLimits == nil {
		s.ipLimits = make(map[string]*connectionLimiter)
	}

	limiter, exists := s.ipLimits[ip]
	if !exists {
		limiter = &connectionLimiter{
			lastConnect:  time.Now(),
			connectCount: 1,
			windowReset:  time.Now().Add(s.rateLimitWindow),
			messageReset: time.Now().Add(s.messageWindow),
		}
		s.ipLimits[ip] = limiter
		return true
	}

	// Reset rate-limit window if expired
	if time.Now().After(limiter.windowReset) {
		limiter.connectCount = 0
		limiter.windowReset = time.Now().Add(s.rateLimitWindow)
	}

	// Check rate limit
	if limiter.connectCount >= s.connRateLimit {
		return false
	}

	// Check concurrent connection limit
	if limiter.connections >= s.maxConnsPerIP {
		return false
	}

	limiter.connectCount++
	limiter.connections++
	limiter.lastConnect = time.Now()
	return true
}

// decrementConnectionCount decrements the connection counter for an IP
func (s *WebSocketServer) decrementConnectionCount(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	limiter, exists := s.ipLimits[ip]
	if !exists {
		return
	}

	if limiter.connections > 0 {
		limiter.connections--
	}

	// Clean up if no more connections
	if limiter.connections == 0 && time.Since(limiter.lastConnect) > s.rateLimitWindow {
		delete(s.ipLimits, ip)
	}
}
