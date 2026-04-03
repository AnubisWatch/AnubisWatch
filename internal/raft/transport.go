package raft

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TCPTransport implements Transport over TCP with optional TLS
type TCPTransport struct {
	bindAddr     string
	advertiseAddr string
	tlsConfig    *tls.Config
	listener     net.Listener

	handlers     map[string]RPCHandler
	handlerMu    sync.RWMutex

	connections  map[string]net.Conn
	connMu       sync.Mutex

	logger       *slog.Logger
	shutdown     bool
	doneCh       chan struct{}
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
		listener, err = net.Listen("tcp", t.bindAddr)
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
	t.shutdown = true
	if t.listener != nil {
		t.listener.Close()
	}

	t.connMu.Lock()
	for _, conn := range t.connections {
		conn.Close()
	}
	t.connections = make(map[string]net.Conn)
	t.connMu.Unlock()

	close(t.doneCh)
	return nil
}

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

// acceptLoop accepts incoming connections
func (t *TCPTransport) acceptLoop() {
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			if t.shutdown {
				return
			}
			t.logger.Error("Accept error", "error", err)
			continue
		}

		go t.handleConnection(conn)
	}
}

// handleConnection handles an incoming connection
func (t *TCPTransport) handleConnection(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	for {
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

		// Read payload length
		line, err = reader.ReadString('\n')
		if err != nil {
			t.logger.Debug("Read error", "error", err)
			return
		}

		lengthStr := strings.TrimSpace(line)
		var length int
		n, err := fmt.Sscanf(lengthStr, "%d", &length)
		if err != nil || n != 1 {
			t.logger.Debug("Invalid length", "line", lengthStr)
			return
		}

		// Read payload
		payload := make([]byte, length)
		_, err = io.ReadFull(reader, payload)
		if err != nil {
			t.logger.Debug("Read payload error", "error", err)
			return
		}

		// Handle RPC
		resp, err := t.handleRPC(method, payload)
		if err != nil {
			t.logger.Debug("RPC error", "method", method, "error", err)
			continue
		}

		// Send response
		respData, err := json.Marshal(resp)
		if err != nil {
			t.logger.Debug("Marshal error", "error", err)
			continue
		}

		fmt.Fprintf(writer, "%d\n%s\n", len(respData), respData)
		writer.Flush()
	}
}

// handleRPC dispatches to the appropriate handler
func (t *TCPTransport) handleRPC(method string, payload []byte) (interface{}, error) {
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
	case "InstallSnapshot":
		cmd = &core.InstallSnapshotRequest{}
	case "Heartbeat":
		cmd = &core.HeartbeatRequest{}
	default:
		return nil, fmt.Errorf("unknown method: %s", method)
	}

	if err := json.Unmarshal(payload, cmd); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	respCh := make(chan interface{}, 1)
	handler(cmd, respCh)

	select {
	case resp := <-respCh:
		return resp, nil
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("handler timeout")
	}
}

// sendRPC sends an RPC to a peer
func (t *TCPTransport) sendRPC(peerID string, method string, req interface{}) (interface{}, error) {
	// Get connection from pool or create new
	conn, err := t.getConnection(peerID)
	if err != nil {
		return nil, err
	}

	defer t.releaseConnection(peerID, conn)

	// Send request
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal error: %w", err)
	}

	writer := bufio.NewWriter(conn)
	fmt.Fprintf(writer, "%s\n%d\n%s\n", method, len(reqData), reqData)
	if err := writer.Flush(); err != nil {
		t.removeConnection(peerID)
		return nil, fmt.Errorf("write error: %w", err)
	}

	// Read response
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.removeConnection(peerID)
		return nil, fmt.Errorf("read error: %w", err)
	}

	lengthStr := strings.TrimSpace(line)
	var length int
	if _, err := fmt.Sscanf(lengthStr, "%d", &length); err != nil {
		return nil, fmt.Errorf("invalid response length: %w", err)
	}

	respData := make([]byte, length)
	_, err = io.ReadFull(reader, respData)
	if err != nil {
		t.removeConnection(peerID)
		return nil, fmt.Errorf("read payload error: %w", err)
	}

	// Parse response based on method
	var resp interface{}
	switch method {
	case "AppendEntries":
		r := &core.AppendEntriesResponse{}
		if err := json.Unmarshal(respData, r); err != nil {
			return nil, err
		}
		resp = r
	case "RequestVote":
		r := &core.RequestVoteResponse{}
		if err := json.Unmarshal(respData, r); err != nil {
			return nil, err
		}
		resp = r
	case "InstallSnapshot":
		r := &core.InstallSnapshotResponse{}
		if err := json.Unmarshal(respData, r); err != nil {
			return nil, err
		}
		resp = r
	case "Heartbeat":
		r := &core.HeartbeatResponse{}
		if err := json.Unmarshal(respData, r); err != nil {
			return nil, err
		}
		resp = r
	}

	return resp, nil
}

// getConnection gets or creates a connection to a peer
func (t *TCPTransport) getConnection(peerID string) (net.Conn, error) {
	t.connMu.Lock()
	defer t.connMu.Unlock()

	if conn, ok := t.connections[peerID]; ok {
		return conn, nil
	}

	// Need to create new connection - we need the peer's address
	// For now, use a placeholder - the actual address should come from peer config
	return nil, fmt.Errorf("connection not found for peer %s", peerID)
}

// releaseConnection returns a connection to the pool
func (t *TCPTransport) releaseConnection(peerID string, conn net.Conn) {
	// Keep connection open for reuse
}

// removeConnection removes a connection from the pool
func (t *TCPTransport) removeConnection(peerID string) {
	t.connMu.Lock()
	if conn, ok := t.connections[peerID]; ok {
		conn.Close()
		delete(t.connections, peerID)
	}
	t.connMu.Unlock()
}

// AddPeerConnection adds a connection to a peer
func (t *TCPTransport) AddPeerConnection(peerID string, address string) error {
	var conn net.Conn
	var err error

	if t.tlsConfig != nil {
		conn, err = tls.Dial("tcp", address, t.tlsConfig)
	} else {
		conn, err = net.Dial("tcp", address)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", address, err)
	}

	t.connMu.Lock()
	t.connections[peerID] = conn
	t.connMu.Unlock()

	return nil
}
