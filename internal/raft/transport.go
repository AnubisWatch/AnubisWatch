package raft

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

const (
	transportRPCDeadline = 5 * time.Second
	maxTransportPayload  = 16 << 20

	// methodForward carries a follower's leader-only request to the leader. It
	// waits for a commit, so it gets its own deadline and its own pooled
	// connection: sharing the Raft stream would hold vote and replication RPCs
	// to the same peer behind it.
	methodForward      = "Forward"
	forwardPoolSuffix  = "#forward"
	forwardRPCSlack    = 2 * time.Second
	maxForwardDeadline = 2 * time.Minute
)

// TCPTransport implements Transport over TCP with optional TLS
type TCPTransport struct {
	bindAddr      string
	advertiseAddr string
	tlsConfig     *tls.Config
	listener      net.Listener

	handlers  map[string]RPCHandler
	handlerMu sync.RWMutex

	// Connection pool with peer addresses
	connections map[string]net.Conn
	incoming    map[net.Conn]struct{} // Accepted connections, protected by connMu.
	peerAddrs   map[string]string     // peerID -> address mapping
	peerRPCMu   map[string]*sync.Mutex
	connMu      sync.Mutex

	// auth signs and verifies every frame when a cluster secret is set (see
	// transport_auth.go); nil leaves frames unsigned.
	auth atomic.Pointer[transportAuth]

	logger   *slog.Logger
	shutdown atomic.Bool
	doneCh   chan struct{}
	stopOnce sync.Once
}

// RPCHandler handles incoming RPCs
type RPCHandler func(cmd interface{}, respCh chan interface{})

// NewTCPTransport creates a new TCP transport
func NewTCPTransport(bindAddr, advertiseAddr string, tlsConfig *tls.Config, logger *slog.Logger) (*TCPTransport, error) {
	if advertiseAddr == "" {
		advertiseAddr = bindAddr
	}

	t := &TCPTransport{
		bindAddr:      bindAddr,
		advertiseAddr: advertiseAddr,
		tlsConfig:     tlsConfig,
		handlers:      make(map[string]RPCHandler),
		connections:   make(map[string]net.Conn),
		incoming:      make(map[net.Conn]struct{}),
		peerAddrs:     make(map[string]string),
		peerRPCMu:     make(map[string]*sync.Mutex),
		logger:        logger.With("component", "raft_transport"),
		doneCh:        make(chan struct{}),
	}

	return t, nil
}

// Start starts listening for incoming connections
func (t *TCPTransport) Start() error {
	var listener net.Listener
	var err error

	if t.tlsConfig != nil {
		listener, err = tls.Listen("tcp", t.bindAddr, t.tlsConfig)
	} else {
		var lc net.ListenConfig
		listener, err = lc.Listen(context.Background(), "tcp", t.bindAddr)
	}

	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	t.listener = listener

	t.logger.Info("TCP transport started",
		"bind_addr", t.bindAddr,
		"advertise_addr", t.advertiseAddr,
		"tls", t.tlsConfig != nil)

	// Accept connections
	go t.acceptLoop()

	return nil
}

// Stop stops the transport
func (t *TCPTransport) Stop() error {
	t.stopOnce.Do(func() {
		t.shutdown.Store(true)
		if t.listener != nil {
			t.listener.Close()
		}

		t.connMu.Lock()
		for _, conn := range t.connections {
			conn.Close()
		}
		for conn := range t.incoming {
			conn.Close()
		}
		t.connections = make(map[string]net.Conn)
		clear(t.incoming)
		t.connMu.Unlock()

		close(t.doneCh)
	})
	return nil
}

// SetAuth turns on frame authentication with the cluster secret: nodeID is
// stamped on outbound frames, and isMember (may be nil) decides which origins
// an inbound frame may come from. An empty secret turns authentication off.
// Call it before Start.
func (t *TCPTransport) SetAuth(nodeID, secret string, isMember func(origin, method string) bool) {
	if secret == "" {
		t.auth.Store(nil)
		return
	}
	t.auth.Store(newTransportAuth(nodeID, secret, isMember))
}

// Authenticated reports whether frames are signed and verified.
func (t *TCPTransport) Authenticated() bool { return t.auth.Load() != nil }

// LocalAddr returns the local address
func (t *TCPTransport) LocalAddr() string {
	return t.advertiseAddr
}

// RegisterHandler registers an RPC handler
func (t *TCPTransport) RegisterHandler(method string, handler RPCHandler) {
	t.handlerMu.Lock()
	t.handlers[method] = handler
	t.handlerMu.Unlock()
}

// SendAppendEntries sends an AppendEntries RPC
func (t *TCPTransport) SendAppendEntries(peerID string, req *core.AppendEntriesRequest) (*core.AppendEntriesResponse, error) {
	resp, err := t.sendRPC(peerID, "AppendEntries", req)
	if err != nil {
		return nil, err
	}
	return resp.(*core.AppendEntriesResponse), nil
}

// SendRequestVote sends a RequestVote RPC
func (t *TCPTransport) SendRequestVote(peerID string, req *core.RequestVoteRequest) (*core.RequestVoteResponse, error) {
	resp, err := t.sendRPC(peerID, "RequestVote", req)
	if err != nil {
		return nil, err
	}
	return resp.(*core.RequestVoteResponse), nil
}

// SendPreVote sends a PreVote RPC
func (t *TCPTransport) SendPreVote(peerID string, req *core.PreVoteRequest) (*core.PreVoteResponse, error) {
	resp, err := t.sendRPC(peerID, "PreVote", req)
	if err != nil {
		return nil, err
	}
	return resp.(*core.PreVoteResponse), nil
}

// SendInstallSnapshot sends an InstallSnapshot RPC
func (t *TCPTransport) SendInstallSnapshot(peerID string, req *core.InstallSnapshotRequest) (*core.InstallSnapshotResponse, error) {
	resp, err := t.sendRPC(peerID, "InstallSnapshot", req)
	if err != nil {
		return nil, err
	}
	return resp.(*core.InstallSnapshotResponse), nil
}

// SendHeartbeat sends a heartbeat RPC
func (t *TCPTransport) SendHeartbeat(peerID string, req *core.HeartbeatRequest) (*core.HeartbeatResponse, error) {
	resp, err := t.sendRPC(peerID, "Heartbeat", req)
	if err != nil {
		return nil, err
	}
	return resp.(*core.HeartbeatResponse), nil
}

// SendForward sends a Forward RPC to a peer, waiting up to deadline for the
// answer. The exchange uses a connection separate from the Raft RPC stream.
func (t *TCPTransport) SendForward(peerID string, req *core.ForwardRequest, deadline time.Duration) (*core.ForwardResponse, error) {
	if deadline <= 0 {
		deadline = transportRPCDeadline
	}
	if deadline > maxForwardDeadline {
		deadline = maxForwardDeadline
	}
	resp, err := t.sendRPCOn(peerID, peerID+forwardPoolSuffix, methodForward, req, deadline)
	if err != nil {
		return nil, err
	}
	return resp.(*core.ForwardResponse), nil
}

// SendJoin asks the member listening on addr to add this node (see join.go).
func (t *TCPTransport) SendJoin(addr string, req *core.JoinRequest, deadline time.Duration) (*core.JoinResponse, error) {
	key := t.registerAddr(addr)
	resp, err := t.sendRPCOn(key, key, methodJoin, req, deadline)
	if err != nil {
		return nil, err
	}
	return resp.(*core.JoinResponse), nil
}

// SendIdentify asks the node listening on addr for its node ID.
func (t *TCPTransport) SendIdentify(addr string, deadline time.Duration) (*core.IdentifyResponse, error) {
	if deadline <= 0 {
		deadline = transportRPCDeadline
	}
	key := t.registerAddr(addr)
	resp, err := t.sendRPCOn(key, key, methodIdentify, &core.IdentifyRequest{}, deadline)
	if err != nil {
		return nil, err
	}
	return resp.(*core.IdentifyResponse), nil
}

// registerAddr names a pool entry for a node known only by address.
func (t *TCPTransport) registerAddr(addr string) string {
	key := joinPoolPrefix + addr
	t.RegisterPeer(key, addr)
	return key
}

// HasPeer reports whether the transport knows an address for peerID.
func (t *TCPTransport) HasPeer(peerID string) bool {
	t.connMu.Lock()
	defer t.connMu.Unlock()
	_, ok := t.peerAddrs[peerID]
	return ok
}

// acceptLoop accepts incoming connections
func (t *TCPTransport) acceptLoop() {
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			if t.shutdown.Load() {
				return
			}
			t.logger.Error("Accept error", "error", err)
			continue
		}

		t.connMu.Lock()
		if t.shutdown.Load() {
			t.connMu.Unlock()
			conn.Close()
			return
		}
		t.incoming[conn] = struct{}{}
		t.connMu.Unlock()

		go t.handleConnection(conn)
	}
}

// handleConnection handles an incoming connection
func (t *TCPTransport) handleConnection(conn net.Conn) {
	defer func() {
		conn.Close()
		t.connMu.Lock()
		delete(t.incoming, conn)
		t.connMu.Unlock()
	}()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	for {
		if t.shutdown.Load() {
			return
		}
		// Read RPC type
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				t.logger.Debug("Read error", "error", err)
			}
			return
		}

		method := strings.TrimSpace(line)
		if method == "" {
			continue
		}

		auth := t.auth.Load()
		var frame *frameAuth
		line, err = reader.ReadString('\n')
		if err != nil {
			t.logger.Debug("Read error", "error", err)
			return
		}
		if auth != nil {
			// Authenticated framing: the auth line precedes the length.
			if frame, err = parseAuthLine(strings.TrimSpace(line)); err == nil {
				err = auth.precheck(method, frame, time.Now())
			}
			if err != nil {
				t.logger.Warn("Rejected Raft RPC", "method", method, "remote", remoteAddr(conn), "error", err)
				return
			}
			if line, err = reader.ReadString('\n'); err != nil {
				t.logger.Debug("Read error", "error", err)
				return
			}
		} else if strings.HasPrefix(line, transportAuthPrefix) {
			t.logger.Warn("Rejected signed Raft RPC: this node has no cluster_secret; set the same necropolis.cluster_secret on every node",
				"method", method, "remote", remoteAddr(conn))
			return
		}

		lengthStr := strings.TrimSpace(line)
		var length int
		n, err := fmt.Sscanf(lengthStr, "%d", &length)
		if err != nil || n != 1 {
			t.logger.Debug("Invalid length", "line", lengthStr)
			return
		}

		// Prevent OOM: cap payload size at 16MB.
		if length <= 0 || length > maxTransportPayload {
			t.logger.Debug("Invalid payload length", "length", length)
			return
		}

		// Read payload
		payload := make([]byte, length)
		_, err = io.ReadFull(reader, payload)
		if err != nil {
			t.logger.Debug("Read payload error", "error", err)
			return
		}

		if auth != nil {
			if err := auth.verify(method, frame, payload, time.Now()); err != nil {
				t.logger.Warn("Rejected Raft RPC", "method", method, "remote", remoteAddr(conn), "error", err)
				return
			}
		}

		// Handle RPC
		resp, err := t.handleRPC(method, payload)
		if err != nil {
			t.logger.Debug("RPC error", "method", method, "error", err)
			return
		}

		// Send response
		respData, err := json.Marshal(resp)
		if err != nil {
			t.logger.Debug("Marshal error", "error", err)
			return
		}

		if _, err := fmt.Fprintf(writer, "%d\n", len(respData)); err != nil {
			t.logger.Debug("Write response header error", "error", err)
			return
		}
		if auth != nil {
			if _, err := fmt.Fprintf(writer, "%s\n", auth.responseMAC(frame, respData)); err != nil {
				t.logger.Debug("Write response header error", "error", err)
				return
			}
		}
		if _, err := writer.Write(respData); err != nil {
			t.logger.Debug("Write response payload error", "error", err)
			return
		}
		if err := writer.Flush(); err != nil {
			t.logger.Debug("Flush response error", "error", err)
			return
		}
	}
}

func remoteAddr(conn net.Conn) string {
	if addr := conn.RemoteAddr(); addr != nil {
		return addr.String()
	}
	return ""
}

// handleRPC dispatches to the appropriate handler
func (t *TCPTransport) handleRPC(method string, payload []byte) (interface{}, error) {
	if t.shutdown.Load() {
		return nil, fmt.Errorf("transport stopped")
	}

	t.handlerMu.RLock()
	handler, ok := t.handlers[method]
	t.handlerMu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown method: %s", method)
	}

	var cmd interface{}
	switch method {
	case "AppendEntries":
		cmd = &core.AppendEntriesRequest{}
	case "RequestVote":
		cmd = &core.RequestVoteRequest{}
	case "PreVote":
		cmd = &core.PreVoteRequest{}
	case "InstallSnapshot":
		cmd = &core.InstallSnapshotRequest{}
	case "Heartbeat":
		cmd = &core.HeartbeatRequest{}
	case methodForward:
		cmd = &core.ForwardRequest{}
	case methodJoin:
		cmd = &core.JoinRequest{}
	case methodIdentify:
		cmd = &core.IdentifyRequest{}
	default:
		return nil, fmt.Errorf("unknown method: %s", method)
	}

	if err := json.Unmarshal(payload, cmd); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	handlerTimeout := 5 * time.Second
	if fwd, ok := cmd.(*core.ForwardRequest); ok {
		// The leader waits for the forwarded command to commit; give it the
		// sender's own budget (capped) rather than the Raft RPC timeout.
		handlerTimeout = time.Duration(fwd.TimeoutMs)*time.Millisecond + forwardRPCSlack
		if handlerTimeout > maxForwardDeadline {
			handlerTimeout = maxForwardDeadline
		}
	}
	if method == methodJoin {
		handlerTimeout = joinChangeTimeout + forwardRPCSlack
	}

	respCh := make(chan interface{}, 1)
	handler(cmd, respCh)

	select {
	case resp := <-respCh:
		return resp, nil
	case <-time.After(handlerTimeout):
		return nil, fmt.Errorf("handler timeout")
	}
}

// sendRPC sends an RPC to a peer. A peer's complete request/response exchange
// is serialized because pooled TCP connections are ordered byte streams.
func (t *TCPTransport) sendRPC(peerID string, method string, req interface{}) (interface{}, error) {
	return t.sendRPCOn(peerID, peerID, method, req, transportRPCDeadline)
}

// sendRPCOn is sendRPC over the pooled connection named poolKey (dialled to
// peerID's address) with an explicit deadline for the whole exchange.
func (t *TCPTransport) sendRPCOn(peerID, poolKey string, method string, req interface{}, deadline time.Duration) (interface{}, error) {
	rpcMu := t.peerLock(poolKey)
	rpcMu.Lock()
	defer rpcMu.Unlock()

	conn, err := t.getPooledConnection(poolKey, peerID)
	if err != nil {
		return nil, err
	}
	// From here on the pool entry is poolKey; shadow peerID so every
	// connection-management call below addresses that entry.
	peerID = poolKey
	if err := conn.SetDeadline(time.Now().Add(deadline)); err != nil {
		t.removeConnectionIfMatch(peerID, conn)
		return nil, fmt.Errorf("set RPC deadline: %w", err)
	}
	defer t.releaseConnection(peerID, conn)

	// Send request
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal error: %w", err)
	}

	// Frames are numbered here, after the peer's stream lock is held, so
	// frames to one peer are written in sequence order.
	auth := t.auth.Load()
	authLine := ""
	var frame *frameAuth
	if auth != nil {
		authLine, frame = auth.sign(method, reqData)
	}

	writer := bufio.NewWriter(conn)
	if _, err := fmt.Fprintf(writer, "%s\n%s%d\n", method, authLine, len(reqData)); err != nil {
		t.removeConnectionIfMatch(peerID, conn)
		return nil, fmt.Errorf("write header error: %w", err)
	}
	if _, err := writer.Write(reqData); err != nil {
		t.removeConnectionIfMatch(peerID, conn)
		return nil, fmt.Errorf("write payload error: %w", err)
	}
	if err := writer.Flush(); err != nil {
		// Write failed - remove from pool (which also closes the connection).
		t.removeConnectionIfMatch(peerID, conn)
		return nil, fmt.Errorf("write error: %w", err)
	}

	// Read response
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.removeConnectionIfMatch(peerID, conn)
		return nil, fmt.Errorf("read error: %w", err)
	}

	lengthStr := strings.TrimSpace(line)
	var length int
	if _, err := fmt.Sscanf(lengthStr, "%d", &length); err != nil {
		t.removeConnectionIfMatch(peerID, conn)
		return nil, fmt.Errorf("invalid response length: %w", err)
	}
	if length <= 0 || length > maxTransportPayload {
		t.removeConnectionIfMatch(peerID, conn)
		return nil, fmt.Errorf("invalid response length: %d", length)
	}

	var respMAC string
	if auth != nil {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.removeConnectionIfMatch(peerID, conn)
			return nil, fmt.Errorf("read response signature: %w", err)
		}
		respMAC = strings.TrimSpace(line)
	}

	respData := make([]byte, length)
	_, err = io.ReadFull(reader, respData)
	if err != nil {
		t.removeConnectionIfMatch(peerID, conn)
		return nil, fmt.Errorf("read payload error: %w", err)
	}
	if auth != nil && !hmac.Equal([]byte(auth.responseMAC(frame, respData)), []byte(respMAC)) {
		t.removeConnectionIfMatch(peerID, conn)
		return nil, fmt.Errorf("%w: response signature mismatch", errTransportAuth)
	}

	// Parse response based on method
	var resp interface{}
	switch method {
	case "AppendEntries":
		resp = &core.AppendEntriesResponse{}
	case "RequestVote":
		resp = &core.RequestVoteResponse{}
	case "PreVote":
		resp = &core.PreVoteResponse{}
	case "InstallSnapshot":
		resp = &core.InstallSnapshotResponse{}
	case "Heartbeat":
		resp = &core.HeartbeatResponse{}
	case methodForward:
		resp = &core.ForwardResponse{}
	case methodJoin:
		resp = &core.JoinResponse{}
	case methodIdentify:
		resp = &core.IdentifyResponse{}
	default:
		t.removeConnectionIfMatch(peerID, conn)
		return nil, fmt.Errorf("unknown method: %s", method)
	}
	if err := json.Unmarshal(respData, resp); err != nil {
		t.removeConnectionIfMatch(peerID, conn)
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return resp, nil
}

// getPooledConnection returns the pooled connection named poolKey, dialling
// peerID's registered address when there is none. The Raft RPC stream uses
// the peer ID as its key; Forward RPCs use their own (see SendForward).
func (t *TCPTransport) getPooledConnection(poolKey, peerID string) (net.Conn, error) {
	t.connMu.Lock()
	defer t.connMu.Unlock()

	// Check existing connection
	if conn, ok := t.connections[poolKey]; ok {
		// Connection exists, assume it's healthy
		// The actual RPC will reveal if it's stale
		return conn, nil
	}

	// Get peer address
	addr, ok := t.peerAddrs[peerID]
	if !ok {
		return nil, fmt.Errorf("unknown peer %s", peerID)
	}

	// Create new connection with timeout
	const connectionTimeout = 5 * time.Second
	var conn net.Conn
	var err error
	ctx := context.Background()
	if t.tlsConfig != nil {
		dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: connectionTimeout}, Config: t.tlsConfig}
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	} else {
		var nd net.Dialer
		nd.Timeout = connectionTimeout
		conn, err = nd.DialContext(ctx, "tcp", addr)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s (%s): %w", peerID, addr, err)
	}

	t.connections[poolKey] = conn
	t.logger.Debug("Created connection to peer", "peer_id", peerID, "address", addr)

	return conn, nil
}

// peerLock returns the mutex that serializes one peer's connection stream.
func (t *TCPTransport) peerLock(peerID string) *sync.Mutex {
	t.connMu.Lock()
	defer t.connMu.Unlock()
	if mu, ok := t.peerRPCMu[peerID]; ok {
		return mu
	}
	mu := &sync.Mutex{}
	t.peerRPCMu[peerID] = mu
	return mu
}

// releaseConnection returns a connection to the pool (keeps it open for reuse).
func (t *TCPTransport) releaseConnection(peerID string, conn net.Conn) {
	if err := conn.SetDeadline(time.Time{}); err != nil {
		t.removeConnectionIfMatch(peerID, conn)
	}
}

// removeConnectionIfMatch avoids a failed old RPC closing a replacement connection.
func (t *TCPTransport) removeConnectionIfMatch(peerID string, failed net.Conn) {
	t.connMu.Lock()
	defer t.connMu.Unlock()
	if conn, ok := t.connections[peerID]; ok && conn == failed {
		conn.Close()
		delete(t.connections, peerID)
		t.logger.Debug("Removed connection to peer", "peer_id", peerID)
	}
}

// AddPeerConnection adds or updates a connection to a peer
func (t *TCPTransport) AddPeerConnection(peerID string, address string) error {
	// Store peer address for future reconnection
	t.connMu.Lock()
	t.peerAddrs[peerID] = address
	existingConn, hasExisting := t.connections[peerID]
	delete(t.connections, peerID)
	t.connMu.Unlock()

	// Close existing connection if any (will be recreated)
	if hasExisting {
		existingConn.Close()
	}

	// Create new connection
	var conn net.Conn
	var err error
	ctx := context.Background()

	if t.tlsConfig != nil {
		dialer := &tls.Dialer{Config: t.tlsConfig}
		conn, err = dialer.DialContext(ctx, "tcp", address)
	} else {
		var nd net.Dialer
		conn, err = nd.DialContext(ctx, "tcp", address)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", address, err)
	}

	t.connMu.Lock()
	t.connections[peerID] = conn
	t.connMu.Unlock()

	t.logger.Debug("Added peer connection", "peer_id", peerID, "address", address)
	return nil
}

// RegisterPeer registers a peer address for future connections
func (t *TCPTransport) RegisterPeer(peerID, address string) {
	t.connMu.Lock()
	t.peerAddrs[peerID] = address
	t.connMu.Unlock()
}

// UnregisterPeer removes a peer and closes its connection
func (t *TCPTransport) UnregisterPeer(peerID string) {
	t.connMu.Lock()
	defer t.connMu.Unlock()

	for _, key := range []string{peerID, peerID + forwardPoolSuffix} {
		if conn, ok := t.connections[key]; ok {
			conn.Close()
			delete(t.connections, key)
		}
	}
	delete(t.peerAddrs, peerID)
}
