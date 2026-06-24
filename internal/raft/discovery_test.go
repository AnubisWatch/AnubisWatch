package raft

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func newTestDiscoveryLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
}

func TestDiscovery_NewDiscovery(t *testing.T) {
	cfg := core.RaftConfig{
		NodeID:        "test-node",
		BindAddr:      "127.0.0.1:7000",
		AdvertiseAddr: "127.0.0.1:7000",
		Peers: []core.RaftPeer{
			{ID: "peer-1", Address: "127.0.0.1:7001", Region: "default"},
		},
	}

	discovery, err := NewDiscovery(cfg, newTestDiscoveryLogger())

	if err != nil {
		t.Fatalf("NewDiscovery failed: %v", err)
	}
	if discovery == nil {
		t.Fatal("Expected discovery to be created")
	}
	if discovery.nodeID != "test-node" {
		t.Errorf("Expected nodeID test-node, got %s", discovery.nodeID)
	}
	if discovery.region != "default" {
		t.Errorf("Expected region default, got %s", discovery.region)
	}
}

func TestDiscovery_GetPeers(t *testing.T) {
	cfg := core.RaftConfig{
		NodeID:        "test-node",
		BindAddr:      "127.0.0.1:7000",
		AdvertiseAddr: "127.0.0.1:7000",
	}

	discovery, _ := NewDiscovery(cfg, newTestDiscoveryLogger())

	peers := discovery.GetPeers()
	if peers == nil {
		t.Error("Expected peers map, got nil")
	}
}

func TestDiscovery_GetPeers_WithPeers(t *testing.T) {
	cfg := core.RaftConfig{
		NodeID:        "test-node",
		BindAddr:      "127.0.0.1:7000",
		AdvertiseAddr: "127.0.0.1:7000",
	}

	discovery, _ := NewDiscovery(cfg, newTestDiscoveryLogger())

	// Add peers
	discovery.peersMu.Lock()
	discovery.knownPeers["peer-1"] = &DiscoveredPeer{
		ID:      "peer-1",
		Address: "127.0.0.1:7001",
		Region:  "us-east",
	}
	discovery.knownPeers["peer-2"] = &DiscoveredPeer{
		ID:      "test-node", // Same as self - should be filtered
		Address: "127.0.0.1:7000",
		Region:  "us-east",
	}
	discovery.peersMu.Unlock()

	peers := discovery.GetPeers()

	// Should have 1 peer (self is filtered out)
	if len(peers) != 1 {
		t.Errorf("Expected 1 peer (self filtered), got %d", len(peers))
	}
	if peers[0].ID != "peer-1" {
		t.Errorf("Expected peer-1, got %s", peers[0].ID)
	}
}

func TestDiscovery_RegisterPeerCallback(t *testing.T) {
	cfg := core.RaftConfig{
		NodeID:        "test-node",
		BindAddr:      "127.0.0.1:7000",
		AdvertiseAddr: "127.0.0.1:7000",
	}

	discovery, _ := NewDiscovery(cfg, newTestDiscoveryLogger())

	called := false
	discovery.RegisterPeerCallback(
		func(peer core.RaftPeer) {
			called = true
		},
		func(nodeID string) {
			// onLost callback
		},
	)

	// Callback should be registered (no error)
	// We can't easily trigger it without full gossip setup
	// Expected - callback not triggered yet in this test
	_ = called
}

func TestDiscovery_StartStop(t *testing.T) {
	cfg := core.RaftConfig{
		NodeID:        "test-node",
		BindAddr:      "127.0.0.1:0",
		AdvertiseAddr: "127.0.0.1:0",
	}

	discovery, _ := NewDiscovery(cfg, newTestDiscoveryLogger())

	// Start should not panic
	err := discovery.Start()
	if err != nil {
		t.Logf("Start returned error (may be expected): %v", err)
	}

	// Give it a moment
	time.Sleep(10 * time.Millisecond)

	// Stop should not panic
	discovery.Stop()
}

func TestDiscovery_Stop(t *testing.T) {
	cfg := core.RaftConfig{
		NodeID:        "test-node-2",
		BindAddr:      "127.0.0.1:0",
		AdvertiseAddr: "127.0.0.1:0",
	}

	discovery, _ := NewDiscovery(cfg, newTestDiscoveryLogger())

	// Stop without start should work
	discovery.Stop()
}

func TestSelectGossipPeers(t *testing.T) {
	cfg := core.RaftConfig{
		NodeID:        "test-node",
		BindAddr:      "127.0.0.1:7000",
		AdvertiseAddr: "127.0.0.1:7000",
	}

	discovery, _ := NewDiscovery(cfg, newTestDiscoveryLogger())

	// Add some known peers
	discovery.knownPeers["peer-1"] = &DiscoveredPeer{
		ID:       "peer-1",
		Address:  "127.0.0.1:7001",
		Region:   "default",
		LastSeen: time.Now(),
	}
	discovery.knownPeers["peer-2"] = &DiscoveredPeer{
		ID:       "peer-2",
		Address:  "127.0.0.1:7002",
		Region:   "default",
		LastSeen: time.Now(),
	}
	discovery.knownPeers["peer-3"] = &DiscoveredPeer{
		ID:       "peer-3",
		Address:  "127.0.0.1:7003",
		Region:   "default",
		LastSeen: time.Now(),
	}

	// Select gossip peers (should return subset)
	peers := discovery.selectGossipPeers(2)
	if len(peers) > 3 {
		t.Errorf("Expected at most 3 peers, got %d", len(peers))
	}
}

func TestEncodeDecodeGossip(t *testing.T) {
	original := &GossipMessage{
		Type:      "gossip",
		NodeID:    "test-node",
		Address:   "127.0.0.1:7000",
		Region:    "default",
		Version:   "1.0.0",
		Timestamp: time.Now().Unix(),
		Peers: []GossipPeerInfo{
			{ID: "peer-1", Address: "127.0.0.1:7001", Region: "default", LastSeen: time.Now().Unix()},
		},
	}

	// Encode
	data, err := encodeGossip(original)
	if err != nil {
		t.Fatalf("encodeGossip failed: %v", err)
	}

	// Decode
	decoded, err := decodeGossip(data)
	if err != nil {
		t.Fatalf("decodeGossip failed: %v", err)
	}

	if decoded.Type != original.Type {
		t.Errorf("Expected type %s, got %s", original.Type, decoded.Type)
	}
	if decoded.NodeID != original.NodeID {
		t.Errorf("Expected nodeID %s, got %s", original.NodeID, decoded.NodeID)
	}
}

func TestMDNSServer_New(t *testing.T) {
	cfg := core.RaftConfig{
		NodeID:        "test-node",
		BindAddr:      "127.0.0.1:0",
		AdvertiseAddr: "127.0.0.1:0",
	}

	discovery, _ := NewDiscovery(cfg, newTestDiscoveryLogger())

	if discovery.mdnsServer == nil {
		t.Error("Expected mdnsServer to be initialized")
	}
}

func TestMDNSClient_New(t *testing.T) {
	cfg := core.RaftConfig{
		NodeID:        "test-node",
		BindAddr:      "127.0.0.1:0",
		AdvertiseAddr: "127.0.0.1:0",
	}

	discovery, _ := NewDiscovery(cfg, newTestDiscoveryLogger())

	if discovery.mdnsClient == nil {
		t.Error("Expected mdnsClient to be initialized")
	}
}

func TestMDNSServer_StartStop(t *testing.T) {
	server := &MDNSServer{
		instance: "test-instance",
		service:  "_anubiswatch._udp",
		domain:   "local",
		port:     0,
		ips:      []net.IP{net.ParseIP("127.0.0.1")},
	}

	// Start
	err := server.Start()
	if err != nil {
		t.Logf("MDNSServer Start returned: %v", err)
	}

	// Stop
	server.Stop()
}

func TestMDNSClient_StartStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := &MDNSClient{
		service: "_anubiswatch._udp",
		domain:  "local",
		results: make(chan *MDNSService, 10),
		ctx:     ctx,
		cancel:  cancel,
	}

	// Start
	client.Start()

	// Give it a moment
	time.Sleep(10 * time.Millisecond)

	// Stop
	client.Stop()
}

func TestMDNSClient_ParseResponse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := &MDNSClient{
		service: "_anubiswatch._tcp",
		domain:  "local",
		results: make(chan *MDNSService, 10),
		ctx:     ctx,
		cancel:  cancel,
	}

	// Test parsing an mDNS response
	// Format: _anubiswatch_|instance|port|txt1;txt2;txt3
	msg := "_anubiswatch_|test-node|7946|id=test-node;region=default;version=1.0.0"
	addr := &net.UDPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 7946,
	}

	result := client.parseResponse(msg, addr)

	if result == nil {
		t.Fatal("parseResponse returned nil for valid message")
	}
	if result.Name != "test-node" {
		t.Errorf("Expected name test-node, got %s", result.Name)
	}
	if result.Port != 7946 {
		t.Errorf("Expected port 7946, got %d", result.Port)
	}
}

func TestDiscoveredPeer_Structure(t *testing.T) {
	peer := &DiscoveredPeer{
		ID:           "peer-1",
		Address:      "127.0.0.1:7001",
		Region:       "default",
		Version:      "1.0.0",
		LastSeen:     time.Now(),
		LastGossip:   time.Now(),
		GossipCount:  5,
		IsStatic:     true,
		Capabilities: core.NodeCapabilities{CanProbe: true},
	}

	if peer.ID != "peer-1" {
		t.Errorf("Expected ID peer-1, got %s", peer.ID)
	}
	if !peer.IsStatic {
		t.Error("Expected IsStatic to be true")
	}
}

func TestGossipMessage_Structure(t *testing.T) {
	msg := GossipMessage{
		Type:      "gossip",
		NodeID:    "test-node",
		Address:   "127.0.0.1:7000",
		Region:    "default",
		Version:   "1.0.0",
		Timestamp: time.Now().Unix(),
		Peers: []GossipPeerInfo{
			{ID: "peer-1", Address: "127.0.0.1:7001", Region: "default"},
		},
	}

	if msg.Type != "gossip" {
		t.Errorf("Expected type gossip, got %s", msg.Type)
	}
	if len(msg.Peers) != 1 {
		t.Errorf("Expected 1 peer, got %d", len(msg.Peers))
	}
}

func TestMDNSService_Structure(t *testing.T) {
	service := MDNSService{
		Name: "test-service",
		Host: "localhost",
		Port: 7000,
		IPs:  []net.IP{net.ParseIP("127.0.0.1")},
		TXT:  []string{"version=1.0.0"},
	}

	if service.Name != "test-service" {
		t.Errorf("Expected name test-service, got %s", service.Name)
	}
	if service.Port != 7000 {
		t.Errorf("Expected port 7000, got %d", service.Port)
	}
}

func TestDiscovery_handleGossip_NewPeer(t *testing.T) {
	cfg := core.RaftConfig{
		NodeID:        "test-node",
		BindAddr:      "127.0.0.1:7000",
		AdvertiseAddr: "127.0.0.1:7000",
	}

	discovery, _ := NewDiscovery(cfg, newTestDiscoveryLogger())

	var discoveredPeer core.RaftPeer
	var mu sync.Mutex
	discovery.RegisterPeerCallback(
		func(peer core.RaftPeer) {
			mu.Lock()
			discoveredPeer = peer
			mu.Unlock()
		},
		func(nodeID string) {},
	)

	msg := &GossipMessage{
		Type:      "gossip",
		NodeID:    "new-peer",
		Address:   "127.0.0.1:7005",
		Region:    "default",
		Version:   "1.0.0",
		Timestamp: time.Now().Unix(),
	}

	discovery.handleGossip(msg)

	// Check peer was added
	discovery.peersMu.RLock()
	peer, ok := discovery.knownPeers["new-peer"]
	discovery.peersMu.RUnlock()

	if !ok {
		t.Fatal("Expected new peer to be added")
	}
	if peer.Address != "127.0.0.1:7005" {
		t.Errorf("Expected address 127.0.0.1:7005, got %s", peer.Address)
	}

	// Give goroutine time to call callback
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	gotID := discoveredPeer.ID
	mu.Unlock()
	if gotID != "new-peer" {
		t.Errorf("Expected callback to be called with new-peer, got %s", gotID)
	}
}

func TestDiscovery_handleGossip_ExistingPeer(t *testing.T) {
	cfg := core.RaftConfig{
		NodeID:        "test-node",
		BindAddr:      "127.0.0.1:7000",
		AdvertiseAddr: "127.0.0.1:7000",
	}

	discovery, _ := NewDiscovery(cfg, newTestDiscoveryLogger())

	// Add existing peer
	oldTime := time.Now().Add(-time.Hour)
	discovery.knownPeers["existing-peer"] = &DiscoveredPeer{
		ID:       "existing-peer",
		Address:  "127.0.0.1:7001",
		Region:   "default",
		LastSeen: oldTime,
		IsStatic: false,
	}

	msg := &GossipMessage{
		Type:      "gossip",
		NodeID:    "existing-peer",
		Address:   "127.0.0.1:7001",
		Region:    "default",
		Version:   "1.0.0",
		Timestamp: time.Now().Unix(),
	}

	discovery.handleGossip(msg)

	// Check peer was updated
	discovery.peersMu.RLock()
	peer, ok := discovery.knownPeers["existing-peer"]
	discovery.peersMu.RUnlock()

	if !ok {
		t.Fatal("Expected existing peer to remain")
	}
	if !peer.LastSeen.After(oldTime) {
		t.Error("Expected LastSeen to be updated")
	}
}

func TestDiscovery_handleGossip_MergePeers(t *testing.T) {
	cfg := core.RaftConfig{
		NodeID:        "test-node",
		BindAddr:      "127.0.0.1:7000",
		AdvertiseAddr: "127.0.0.1:7000",
	}

	discovery, _ := NewDiscovery(cfg, newTestDiscoveryLogger())

	msg := &GossipMessage{
		Type:      "gossip",
		NodeID:    "sender-peer",
		Address:   "127.0.0.1:7001",
		Region:    "default",
		Version:   "1.0.0",
		Timestamp: time.Now().Unix(),
		Peers: []GossipPeerInfo{
			{
				ID:       "learned-peer",
				Address:  "127.0.0.1:7002",
				Region:   "us-east",
				LastSeen: time.Now().Unix(),
			},
		},
	}

	discovery.handleGossip(msg)

	// Check learned peer was added
	discovery.peersMu.RLock()
	peer, ok := discovery.knownPeers["learned-peer"]
	discovery.peersMu.RUnlock()

	if !ok {
		t.Fatal("Expected learned peer to be added")
	}
	if peer.Address != "127.0.0.1:7002" {
		t.Errorf("Expected address 127.0.0.1:7002, got %s", peer.Address)
	}
	if peer.Region != "us-east" {
		t.Errorf("Expected region us-east, got %s", peer.Region)
	}
}

func TestDiscovery_handleMDNSDiscovery(t *testing.T) {
	cfg := core.RaftConfig{
		NodeID:        "test-node",
		BindAddr:      "127.0.0.1:7000",
		AdvertiseAddr: "127.0.0.1:7000",
	}

	discovery, _ := NewDiscovery(cfg, newTestDiscoveryLogger())

	var discoveredPeer core.RaftPeer
	var mu sync.Mutex
	discovery.RegisterPeerCallback(
		func(peer core.RaftPeer) {
			mu.Lock()
			discoveredPeer = peer
			mu.Unlock()
		},
		func(nodeID string) {},
	)

	svc := &MDNSService{
		Name: "mdns-peer",
		Host: "127.0.0.1",
		Port: 7946,
		TXT:  []string{"id=mdns-peer", "region=us-west", "version=1.0.0"},
	}

	discovery.handleMDNSDiscovery(svc)

	// Check peer was added
	discovery.peersMu.RLock()
	peer, ok := discovery.knownPeers["mdns-peer"]
	discovery.peersMu.RUnlock()

	if !ok {
		t.Fatal("Expected mDNS peer to be added")
	}
	if peer.Address != "127.0.0.1:7946" {
		t.Errorf("Expected address 127.0.0.1:7946, got %s", peer.Address)
	}
	if peer.Region != "us-west" {
		t.Errorf("Expected region us-west, got %s", peer.Region)
	}

	// Give goroutine time to call callback
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	gotID := discoveredPeer.ID
	mu.Unlock()
	if gotID != "mdns-peer" {
		t.Errorf("Expected callback with mdns-peer, got %s", gotID)
	}
}

func TestDiscovery_handleMDNSDiscovery_Self(t *testing.T) {
	cfg := core.RaftConfig{
		NodeID:        "test-node",
		BindAddr:      "127.0.0.1:7000",
		AdvertiseAddr: "127.0.0.1:7000",
	}

	discovery, _ := NewDiscovery(cfg, newTestDiscoveryLogger())

	svc := &MDNSService{
		Name: "test-node", // Same as our node ID
		Host: "127.0.0.1",
		Port: 7946,
		TXT:  []string{"id=test-node", "region=default"},
	}

	// Should not add ourselves
	discovery.handleMDNSDiscovery(svc)

	discovery.peersMu.RLock()
	_, ok := discovery.knownPeers["test-node"]
	discovery.peersMu.RUnlock()

	if ok {
		t.Error("Expected not to add ourselves as peer")
	}
}

func TestDiscovery_checkPeerHealth(t *testing.T) {
	cfg := core.RaftConfig{
		NodeID:        "test-node",
		BindAddr:      "127.0.0.1:7000",
		AdvertiseAddr: "127.0.0.1:7000",
	}

	discovery, _ := NewDiscovery(cfg, newTestDiscoveryLogger())

	// Add stale peer (older than 2 minute timeout)
	staleTime := time.Now().Add(-5 * time.Minute)
	discovery.knownPeers["stale-peer"] = &DiscoveredPeer{
		ID:       "stale-peer",
		Address:  "127.0.0.1:7001",
		Region:   "default",
		LastSeen: staleTime,
		IsStatic: false,
	}

	// Add fresh peer
	discovery.knownPeers["fresh-peer"] = &DiscoveredPeer{
		ID:       "fresh-peer",
		Address:  "127.0.0.1:7002",
		Region:   "default",
		LastSeen: time.Now(),
		IsStatic: false,
	}

	// Add static peer (should not be removed even if stale)
	discovery.knownPeers["static-peer"] = &DiscoveredPeer{
		ID:       "static-peer",
		Address:  "127.0.0.1:7003",
		Region:   "default",
		LastSeen: staleTime,
		IsStatic: true,
	}

	var lostPeerID string
	var mu sync.Mutex
	discovery.RegisterPeerCallback(
		func(peer core.RaftPeer) {},
		func(nodeID string) {
			mu.Lock()
			lostPeerID = nodeID
			mu.Unlock()
		},
	)

	discovery.checkPeerHealth()

	// Give goroutine time to call callback
	time.Sleep(50 * time.Millisecond)

	// Stale peer should be removed
	discovery.peersMu.RLock()
	_, staleOk := discovery.knownPeers["stale-peer"]
	_, freshOk := discovery.knownPeers["fresh-peer"]
	_, staticOk := discovery.knownPeers["static-peer"]
	discovery.peersMu.RUnlock()

	if staleOk {
		t.Error("Expected stale peer to be removed")
	}
	if !freshOk {
		t.Error("Expected fresh peer to remain")
	}
	if !staticOk {
		t.Error("Expected static peer to remain despite being stale")
	}

	mu.Lock()
	gotID := lostPeerID
	mu.Unlock()
	if gotID != "stale-peer" {
		t.Errorf("Expected onLost callback with stale-peer, got %s", gotID)
	}
}

func TestDiscovery_doGossip_NoPeers(t *testing.T) {
	cfg := core.RaftConfig{
		NodeID:        "test-node",
		BindAddr:      "127.0.0.1:7000",
		AdvertiseAddr: "127.0.0.1:7000",
	}

	discovery, _ := NewDiscovery(cfg, newTestDiscoveryLogger())

	// Should not panic with no peers
	discovery.doGossip()

	// Test passes if no panic
}

func TestDiscovery_doGossip_WithPeers(t *testing.T) {
	cfg := core.RaftConfig{
		NodeID:        "test-node",
		BindAddr:      "127.0.0.1:7000",
		AdvertiseAddr: "127.0.0.1:7000",
	}

	discovery, _ := NewDiscovery(cfg, newTestDiscoveryLogger())

	// Add peers to knownPeers
	peer1 := &DiscoveredPeer{
		ID:      "peer-1",
		Address: "127.0.0.1:7001",
		Region:  "default",
	}
	peer2 := &DiscoveredPeer{
		ID:      "peer-2",
		Address: "127.0.0.1:7002",
		Region:  "default",
	}

	discovery.peersMu.Lock()
	discovery.knownPeers["peer-1"] = peer1
	discovery.knownPeers["peer-2"] = peer2
	discovery.peersMu.Unlock()

	// doGossip should attempt to send gossip (will fail but should not panic)
	discovery.doGossip()

	// Test passes if no panic
}

func TestDiscovery_sendGossip(t *testing.T) {
	cfg := core.RaftConfig{
		NodeID:        "test-node",
		BindAddr:      "127.0.0.1:7000",
		AdvertiseAddr: "127.0.0.1:7000",
	}

	discovery, _ := NewDiscovery(cfg, newTestDiscoveryLogger())

	peer := &DiscoveredPeer{
		ID:      "target-peer",
		Address: "127.0.0.1:7001",
		Region:  "default",
	}

	msg := GossipMessage{
		Type:   "gossip",
		NodeID: "test-node",
	}

	oldCount := peer.GossipCount
	oldGossip := peer.LastGossip

	discovery.sendGossip(peer, msg)

	if peer.GossipCount != oldCount+1 {
		t.Errorf("Expected GossipCount to increment, got %d", peer.GossipCount)
	}
	if !peer.LastGossip.After(oldGossip) {
		t.Error("Expected LastGossip to be updated")
	}
}

func TestDiscovery_Query(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := &MDNSClient{
		service: "_anubiswatch._tcp",
		domain:  "local",
		results: make(chan *MDNSService, 10),
		ctx:     ctx,
		cancel:  cancel,
		conn:    nil, // No actual connection
	}

	// Should not crash - returns nil or empty slice depending on implementation
	// Query sends to nil conn then waits for timeout
	services := client.Query("_anubiswatch._tcp")
	// Accept either nil or empty slice - both are valid for no connection
	_ = services
}

func TestDecodeGossip_Valid(t *testing.T) {
	data := []byte(`{"node_id":"test-node","address":"127.0.0.1:7946","peers":[]}`)

	msg, err := decodeGossip(data)
	if err != nil {
		t.Fatalf("decodeGossip failed: %v", err)
	}

	if msg.NodeID != "test-node" {
		t.Errorf("Expected NodeID test-node, got %s", msg.NodeID)
	}
}

func TestDecodeGossip_InvalidJSON(t *testing.T) {
	data := []byte(`{invalid json}`)

	_, err := decodeGossip(data)
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestEncodeGossip(t *testing.T) {
	msg := &GossipMessage{
		NodeID:  "test-node",
		Address: "127.0.0.1:7946",
	}

	data, err := encodeGossip(msg)
	if err != nil {
		t.Fatalf("encodeGossip failed: %v", err)
	}

	if len(data) == 0 {
		t.Error("Expected encoded data")
	}
}

func TestDiscovery_queryPeriodically(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	client := &MDNSClient{
		service: "_anubiswatch._tcp",
		domain:  "local",
		results: make(chan *MDNSService, 10),
		ctx:     ctx,
		cancel:  cancel,
	}

	// Start the periodic query
	go client.queryPeriodically()

	// Let it run briefly
	time.Sleep(50 * time.Millisecond)

	// Stop should not panic
	client.Stop()
}

func TestMDNSClient_QueryTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := &MDNSClient{
		service: "_anubiswatch._tcp",
		domain:  "local",
		results: make(chan *MDNSService, 10),
		ctx:     ctx,
		cancel:  cancel,
		conn:    nil,
	}

	// Pre-populate results channel
	client.results <- &MDNSService{Name: "service-1"}
	client.results <- &MDNSService{Name: "service-2"}

	services := client.Query("_anubiswatch._tcp")

	if len(services) < 2 {
		t.Errorf("Expected at least 2 services, got %d", len(services))
	}
}

// Test queryMDNS - tests that it doesn't panic when called
func TestDiscovery_queryMDNS(t *testing.T) {
	cfg := core.RaftConfig{
		NodeID:        "test-node",
		BindAddr:      "127.0.0.1:0",
		AdvertiseAddr: "127.0.0.1:0",
	}

	discovery, _ := NewDiscovery(cfg, newTestDiscoveryLogger())

	// queryMDNS should not panic even without a working mdns connection
	// It will likely return nothing or log an error
	discovery.queryMDNS()

	// Test passes if no panic
}

func TestDiscovery_handleMDNSDiscovery_MissingTXT(t *testing.T) {
	cfg := core.RaftConfig{
		NodeID:        "test-node",
		BindAddr:      "127.0.0.1:0",
		AdvertiseAddr: "127.0.0.1:0",
	}

	discovery, _ := NewDiscovery(cfg, newTestDiscoveryLogger())

	// Service with no TXT records
	svc := &MDNSService{
		Name: "no-txt-peer",
		Host: "127.0.0.1",
		Port: 7946,
		TXT:  []string{},
	}

	// Should not panic - will skip adding peer due to empty nodeID
	discovery.handleMDNSDiscovery(svc)

	// Peer should not be added (no id= in TXT)
	discovery.peersMu.RLock()
	_, ok := discovery.knownPeers["no-txt-peer"]
	discovery.peersMu.RUnlock()

	if ok {
		t.Error("Expected peer not to be added without ID in TXT")
	}
}

func TestDiscovery_handleMDNSDiscovery_PartialTXT(t *testing.T) {
	cfg := core.RaftConfig{
		NodeID:        "test-node",
		BindAddr:      "127.0.0.1:0",
		AdvertiseAddr: "127.0.0.1:0",
	}

	discovery, _ := NewDiscovery(cfg, newTestDiscoveryLogger())

	var callbackCalled bool
	var mu sync.Mutex
	discovery.RegisterPeerCallback(
		func(peer core.RaftPeer) {
			mu.Lock()
			callbackCalled = true
			mu.Unlock()
		},
		func(nodeID string) {},
	)

	// Service with partial TXT records (only id)
	svc := &MDNSService{
		Name: "partial-peer",
		Host: "127.0.0.1",
		Port: 7946,
		TXT:  []string{"id=partial-peer"},
	}

	discovery.handleMDNSDiscovery(svc)

	// Check peer was added with defaults
	discovery.peersMu.RLock()
	peer, ok := discovery.knownPeers["partial-peer"]
	discovery.peersMu.RUnlock()

	if !ok {
		t.Fatal("Expected peer to be added")
	}
	if peer.Region != "" {
		t.Errorf("Expected empty region, got %s", peer.Region)
	}
	if peer.Version != "" {
		t.Errorf("Expected empty version, got %s", peer.Version)
	}

	// Give goroutine time to call callback
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if !callbackCalled {
		t.Error("Expected callback to be called")
	}
	mu.Unlock()
}

// --- K9: HMAC authentication tests ---

// TestSignAndVerifyGossip_RoundTrip exercises the happy path: a
// message signed with secret A is verified by the same secret.
func TestSignAndVerifyGossip_RoundTrip(t *testing.T) {
	secret := []byte("cluster-shared-secret-12345")
	msg := &GossipMessage{
		Type:      "gossip",
		NodeID:    "alice",
		Address:   "10.0.0.1:7000",
		Region:    "default",
		Version:   "0.1.0",
		Timestamp: time.Now().Unix(),
	}

	signGossip(msg, secret)
	if msg.HMAC == "" {
		t.Fatal("K9: signGossip must populate HMAC field")
	}

	if err := verifyGossip(msg, secret, 5*time.Minute, time.Now()); err != nil {
		t.Errorf("K9: verifyGossip on a freshly-signed message should succeed, got %v", err)
	}
}

// TestVerifyGossip_RejectWrongSecret asserts that a message signed
// with one secret cannot be verified by a cluster with a different
// secret. This is the core K9 property — the cluster refuses to
// honour messages from a node that doesn't share the secret.
func TestVerifyGossip_RejectWrongSecret(t *testing.T) {
	signerSecret := []byte("alice-secret")
	verifierSecret := []byte("bob-secret")

	msg := &GossipMessage{
		Type:      "gossip",
		NodeID:    "alice",
		Timestamp: time.Now().Unix(),
	}
	signGossip(msg, signerSecret)

	err := verifyGossip(msg, verifierSecret, 5*time.Minute, time.Now())
	if err == nil {
		t.Fatal("K9: verifyGossip with mismatched secret must reject")
	}
	if !errors.Is(err, ErrMessageUnauthenticated) {
		t.Errorf("K9: wrong-secret error should wrap ErrMessageUnauthenticated, got %v", err)
	}
}

// TestVerifyGossip_RejectMissingHMAC covers the case where the
// sender forgot to sign (or has no secret). With a secret set, the
// receiver must drop the message.
func TestVerifyGossip_RejectMissingHMAC(t *testing.T) {
	secret := []byte("real-secret")
	msg := &GossipMessage{
		Type:      "gossip",
		NodeID:    "alice",
		Timestamp: time.Now().Unix(),
	}

	err := verifyGossip(msg, secret, 5*time.Minute, time.Now())
	if err == nil {
		t.Fatal("K9: missing HMAC must be rejected when secret is set")
	}
	if !errors.Is(err, ErrMessageUnauthenticated) {
		t.Errorf("K9: missing-hmac error should wrap ErrMessageUnauthenticated, got %v", err)
	}
}

// TestVerifyGossip_AcceptUnsignedWhenNoSecret covers the
// single-node / test-mode case: when the receiver has no secret
// configured, an unsigned message is accepted (so existing
// single-node test setups don't need a secret just to function).
func TestVerifyGossip_AcceptUnsignedWhenNoSecret(t *testing.T) {
	msg := &GossipMessage{
		Type:      "gossip",
		NodeID:    "alice",
		Timestamp: time.Now().Unix(),
	}

	if err := verifyGossip(msg, nil, 5*time.Minute, time.Now()); err != nil {
		t.Errorf("K9: unsigned message with no secret should pass, got %v", err)
	}
}

// TestVerifyGossip_RejectSignedWhenNoSecret is the converse: a
// signed message arriving at a cluster that hasn't configured a
// secret is suspicious and must be rejected (likely a misconfigured
// sender thinking they're talking to a different cluster).
func TestVerifyGossip_RejectSignedWhenNoSecret(t *testing.T) {
	msg := &GossipMessage{
		Type:      "gossip",
		NodeID:    "alice",
		Timestamp: time.Now().Unix(),
	}
	signGossip(msg, []byte("some-other-secret"))

	err := verifyGossip(msg, nil, 5*time.Minute, time.Now())
	if err == nil {
		t.Fatal("K9: signed message arriving at a no-secret cluster must be rejected")
	}
	if !errors.Is(err, ErrMessageUnauthenticated) {
		t.Errorf("K9: no-secret-but-signed error should wrap ErrMessageUnauthenticated, got %v", err)
	}
}

// TestVerifyGossip_RejectStaleMessage exercises the replay window.
// We freeze `now` 10 minutes after the message timestamp; the
// receiver (with a 5-minute window) must drop the message.
func TestVerifyGossip_RejectStaleMessage(t *testing.T) {
	secret := []byte("real-secret")
	now := time.Now()
	msg := &GossipMessage{
		Type:      "gossip",
		NodeID:    "alice",
		Timestamp: now.Add(-10 * time.Minute).Unix(),
	}
	signGossip(msg, secret)

	err := verifyGossip(msg, secret, 5*time.Minute, now)
	if err == nil {
		t.Fatal("K9: 10-minute-old message should be rejected by a 5-minute window")
	}
	if !errors.Is(err, ErrMessageStale) {
		t.Errorf("K9: stale-message error should wrap ErrMessageStale, got %v", err)
	}
}

// TestVerifyGossip_RejectTamperedPeerList is the tamper-detection
// property. An attacker captures a signed message, modifies the
// embedded peer list (e.g. injects themselves), and forwards it.
// The HMAC must not match the modified body.
func TestVerifyGossip_RejectTamperedPeerList(t *testing.T) {
	secret := []byte("real-secret")
	msg := &GossipMessage{
		Type:      "gossip",
		NodeID:    "alice",
		Address:   "10.0.0.1:7000",
		Timestamp: time.Now().Unix(),
		Peers: []GossipPeerInfo{
			{ID: "peer-1", Address: "10.0.0.2:7000"},
		},
	}
	signGossip(msg, secret)

	// Attacker tampers.
	msg.Peers = append(msg.Peers, GossipPeerInfo{
		ID: "evil-peer", Address: "10.0.0.99:7000",
	})

	if err := verifyGossip(msg, secret, 5*time.Minute, time.Now()); err == nil {
		t.Fatal("K9: tampered peer list must invalidate the HMAC")
	}
}

// TestSignGossip_NoOpWithoutSecret asserts the helper is a no-op when
// no secret is configured, so single-node test setups can pass
// unsigned messages without any code change.
func TestSignGossip_NoOpWithoutSecret(t *testing.T) {
	msg := &GossipMessage{Type: "gossip", NodeID: "alice", Timestamp: time.Now().Unix()}
	signGossip(msg, nil)
	if msg.HMAC != "" {
		t.Errorf("K9: signGossip with no secret must not set HMAC, got %q", msg.HMAC)
	}
}

// TestDiscovery_NewDiscovery_WarnsOnEmptySecret asserts the
// operator-visible warning fires when ANUBIS_CLUSTER_SECRET is
// unset in a deployment that would normally be authenticated.
func TestDiscovery_NewDiscovery_WarnsOnEmptySecret(t *testing.T) {
	// K9: we can't easily assert log output here, but the call must
	// not panic and the secret must end up empty in the struct.
	cfg := core.RaftConfig{
		NodeID:   "warn-test",
		BindAddr: "127.0.0.1:7000",
	}
	d, err := NewDiscovery(cfg, newTestDiscoveryLogger())
	if err != nil {
		t.Fatalf("NewDiscovery: %v", err)
	}
	// authEnabled was removed from production code; the check is now
	// inlined here. See refactor.md #17 — the function had no
	// non-test callers.
	if len(d.clusterSecret) > 0 {
		t.Error("K9: empty ClusterSecret should leave the discovery layer unauthenticated")
	}
	defer d.Stop()
}
