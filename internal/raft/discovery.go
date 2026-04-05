package raft

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Discovery handles peer discovery via gossip and mDNS
// The jackals howl to find each other in the Necropolis
type Discovery struct {
	config   core.RaftConfig
	nodeID   string
	bindAddr string
	region   string

	// mDNS
	mdnsServer *MDNSServer
	mdnsClient *MDNSClient

	// Gossip
	gossipInterval time.Duration
	gossipNodes    int
	knownPeers     map[string]*DiscoveredPeer
	peersMu        sync.RWMutex

	// Callbacks
	onPeerDiscovered func(core.RaftPeer)
	onPeerLost       func(string)

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	logger *slog.Logger
}

// DiscoveredPeer represents a peer discovered through gossip/mDNS
type DiscoveredPeer struct {
	ID           string
	Address      string
	Region       string
	Version      string
	Capabilities core.NodeCapabilities
	LastSeen     time.Time
	LastGossip   time.Time
	GossipCount  int
	IsStatic     bool // Configured vs discovered
}

// GossipMessage is sent between peers during gossip
type GossipMessage struct {
	Type      string           `json:"type"`
	NodeID    string           `json:"node_id"`
	Address   string           `json:"address"`
	Region    string           `json:"region"`
	Version   string           `json:"version"`
	Peers     []GossipPeerInfo `json:"peers"`
	Timestamp int64            `json:"timestamp"`
}

// GossipPeerInfo is information about a peer in gossip
type GossipPeerInfo struct {
	ID       string `json:"id"`
	Address  string `json:"address"`
	Region   string `json:"region"`
	LastSeen int64  `json:"last_seen"`
}

// MDNSServer handles mDNS service advertising
type MDNSServer struct {
	instance string
	service  string
	domain   string
	port     int
	ips      []net.IP
	txt      []string
	shutdown bool
	conn     *net.UDPConn
	connMu   sync.Mutex
}

// MDNSClient handles mDNS service discovery
type MDNSClient struct {
	service  string
	domain   string
	results  chan *MDNSService
	shutdown bool
	conn     *net.UDPConn
	connMu   sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
}

// MDNSService represents a discovered mDNS service
type MDNSService struct {
	Name string
	Host string
	Port int
	IPs  []net.IP
	TXT  []string
}

// NewDiscovery creates a new discovery service
func NewDiscovery(config core.RaftConfig, logger *slog.Logger) (*Discovery, error) {
	ctx, cancel := context.WithCancel(context.Background())

	d := &Discovery{
		config:         config,
		nodeID:         config.NodeID,
		bindAddr:       config.AdvertiseAddr,
		region:         "default",
		gossipInterval: 1 * time.Second,
		gossipNodes:    3,
		knownPeers:     make(map[string]*DiscoveredPeer),
		ctx:            ctx,
		cancel:         cancel,
		done:           make(chan struct{}),
		logger:         logger.With("component", "discovery"),
	}

	// Initialize mDNS
	d.mdnsServer = d.newMDNSServer()
	d.mdnsClient = d.newMDNSClient()

	// Add configured peers as static
	for _, peer := range config.Peers {
		d.knownPeers[peer.ID] = &DiscoveredPeer{
			ID:       peer.ID,
			Address:  peer.Address,
			Region:   peer.Region,
			LastSeen: time.Now(),
			IsStatic: true,
		}
	}

	return d, nil
}

// Start starts the discovery service
func (d *Discovery) Start() error {
	// Start mDNS server (advertise ourselves)
	if err := d.mdnsServer.Start(); err != nil {
		d.logger.Warn("mDNS server failed to start", "error", err)
	}

	// Start mDNS client (discover others)
	if err := d.mdnsClient.Start(); err != nil {
		d.logger.Warn("mDNS client failed to start", "error", err)
	}

	// Start gossip loop
	go d.gossipLoop()

	// Start mDNS discovery loop
	go d.mdnsDiscoveryLoop()

	d.logger.Info("Discovery service started",
		"gossip_interval", d.gossipInterval,
		"gossip_nodes", d.gossipNodes)

	return nil
}

// Stop stops the discovery service
func (d *Discovery) Stop() error {
	d.cancel()
	d.mdnsServer.Stop()
	d.mdnsClient.Stop()
	close(d.done)
	return nil
}

// RegisterPeerCallback sets the callback for discovered peers
func (d *Discovery) RegisterPeerCallback(onDiscovered func(core.RaftPeer), onLost func(string)) {
	d.onPeerDiscovered = onDiscovered
	d.onPeerLost = onLost
}

// GetPeers returns all known peers
func (d *Discovery) GetPeers() []DiscoveredPeer {
	d.peersMu.RLock()
	defer d.peersMu.RUnlock()

	peers := make([]DiscoveredPeer, 0, len(d.knownPeers))
	for _, p := range d.knownPeers {
		if p.ID != d.nodeID {
			peers = append(peers, *p)
		}
	}
	return peers
}

// gossipLoop periodically gossips with random peers
func (d *Discovery) gossipLoop() {
	ticker := time.NewTicker(d.gossipInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.doGossip()
		}
	}
}

// doGossip sends gossip messages to random peers
func (d *Discovery) doGossip() {
	peers := d.selectGossipPeers(d.gossipNodes)
	if len(peers) == 0 {
		return
	}

	// Build gossip message
	msg := GossipMessage{
		Type:      "gossip",
		NodeID:    d.nodeID,
		Address:   d.bindAddr,
		Region:    d.region,
		Version:   "0.1.0",
		Timestamp: time.Now().Unix(),
	}

	// Include known peers (except the ones we're gossiping to)
	d.peersMu.RLock()
	for id, peer := range d.knownPeers {
		if peer.ID != d.nodeID {
			msg.Peers = append(msg.Peers, GossipPeerInfo{
				ID:       id,
				Address:  peer.Address,
				Region:   peer.Region,
				LastSeen: peer.LastSeen.Unix(),
			})
		}
	}
	d.peersMu.RUnlock()

	// Send to selected peers
	for _, peer := range peers {
		d.sendGossip(peer, msg)
	}
}

// selectGossipPeers selects random peers for gossip
func (d *Discovery) selectGossipPeers(n int) []*DiscoveredPeer {
	d.peersMu.RLock()
	defer d.peersMu.RUnlock()

	// Collect all non-static peers
	candidates := make([]*DiscoveredPeer, 0)
	for _, p := range d.knownPeers {
		if !p.IsStatic && p.ID != d.nodeID {
			candidates = append(candidates, p)
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	// Shuffle and select
	rand.Shuffle(len(candidates), func(i, j int) {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	})

	if len(candidates) > n {
		candidates = candidates[:n]
	}

	return candidates
}

// sendGossip sends a gossip message to a peer
func (d *Discovery) sendGossip(peer *DiscoveredPeer, msg GossipMessage) {
	// In a real implementation, this would use the transport layer
	// For now, just update last gossip time
	peer.LastGossip = time.Now()
	peer.GossipCount++
}

// handleGossip processes an incoming gossip message
func (d *Discovery) handleGossip(msg *GossipMessage) {
	d.logger.Debug("Received gossip",
		"from", msg.NodeID,
		"peers", len(msg.Peers))

	// Update sender info
	d.peersMu.Lock()
	if peer, ok := d.knownPeers[msg.NodeID]; ok {
		peer.LastSeen = time.Now()
	} else {
		// New peer discovered
		newPeer := &DiscoveredPeer{
			ID:       msg.NodeID,
			Address:  msg.Address,
			Region:   msg.Region,
			Version:  msg.Version,
			LastSeen: time.Now(),
			IsStatic: false,
		}
		d.knownPeers[msg.NodeID] = newPeer

		d.logger.Info("New peer discovered via gossip",
			"peer_id", msg.NodeID,
			"address", msg.Address)

		if d.onPeerDiscovered != nil {
			go d.onPeerDiscovered(core.RaftPeer{
				ID:      msg.NodeID,
				Address: msg.Address,
				Region:  msg.Region,
			})
		}
	}
	d.peersMu.Unlock()

	// Merge peer info from gossip message
	for _, p := range msg.Peers {
		d.peersMu.Lock()
		if peer, ok := d.knownPeers[p.ID]; ok {
			// Update if newer
			if p.LastSeen > peer.LastSeen.Unix() {
				peer.LastSeen = time.Unix(p.LastSeen, 0)
				peer.Address = p.Address
				peer.Region = p.Region
			}
		} else if p.ID != d.nodeID {
			// New peer
			d.knownPeers[p.ID] = &DiscoveredPeer{
				ID:       p.ID,
				Address:  p.Address,
				Region:   p.Region,
				LastSeen: time.Unix(p.LastSeen, 0),
				IsStatic: false,
			}

			d.logger.Info("New peer learned via gossip",
				"peer_id", p.ID,
				"address", p.Address)
		}
		d.peersMu.Unlock()
	}
}

// mdnsDiscoveryLoop periodically queries mDNS for peers
func (d *Discovery) mdnsDiscoveryLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.queryMDNS()
		}
	}
}

// queryMDNS queries mDNS for AnubisWatch services
func (d *Discovery) queryMDNS() {
	services := d.mdnsClient.Query("_anubiswatch._tcp")
	for _, svc := range services {
		d.handleMDNSDiscovery(svc)
	}
}

// handleMDNSDiscovery processes a discovered mDNS service
func (d *Discovery) handleMDNSDiscovery(svc *MDNSService) {
	// Parse TXT records for node info
	var nodeID, region, version string
	for _, txt := range svc.TXT {
		if strings.HasPrefix(txt, "id=") {
			nodeID = strings.TrimPrefix(txt, "id=")
		} else if strings.HasPrefix(txt, "region=") {
			region = strings.TrimPrefix(txt, "region=")
		} else if strings.HasPrefix(txt, "version=") {
			version = strings.TrimPrefix(txt, "version=")
		}
	}

	if nodeID == "" || nodeID == d.nodeID {
		return
	}

	address := fmt.Sprintf("%s:%d", svc.Host, svc.Port)

	d.peersMu.Lock()
	if peer, ok := d.knownPeers[nodeID]; ok {
		peer.LastSeen = time.Now()
		peer.Address = address
	} else {
		d.knownPeers[nodeID] = &DiscoveredPeer{
			ID:       nodeID,
			Address:  address,
			Region:   region,
			Version:  version,
			LastSeen: time.Now(),
			IsStatic: false,
		}

		d.logger.Info("New peer discovered via mDNS",
			"peer_id", nodeID,
			"address", address)

		if d.onPeerDiscovered != nil {
			go d.onPeerDiscovered(core.RaftPeer{
				ID:      nodeID,
				Address: address,
				Region:  region,
			})
		}
	}
	d.peersMu.Unlock()
}

// checkPeerHealth marks stale peers as lost
func (d *Discovery) checkPeerHealth() {
	timeout := 2 * time.Minute
	now := time.Now()

	d.peersMu.Lock()
	for id, peer := range d.knownPeers {
		if peer.IsStatic {
			continue
		}
		if now.Sub(peer.LastSeen) > timeout {
			delete(d.knownPeers, id)
			d.logger.Info("Peer lost", "peer_id", id)

			if d.onPeerLost != nil {
				go d.onPeerLost(id)
			}
		}
	}
	d.peersMu.Unlock()
}

// MDNSServer implementation using UDP broadcast

func (d *Discovery) newMDNSServer() *MDNSServer {
	// Get all local IPs
	ips, _ := net.InterfaceAddrs()
	localIPs := make([]net.IP, 0)
	for _, addr := range ips {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				localIPs = append(localIPs, ipNet.IP)
			}
		}
	}

	return &MDNSServer{
		instance: d.nodeID,
		service:  "_anubiswatch._tcp",
		domain:   "local",
		port:     7946, // Default Raft port
		ips:      localIPs,
		txt: []string{
			fmt.Sprintf("id=%s", d.nodeID),
			fmt.Sprintf("region=%s", d.region),
			"version=0.1.0",
		},
	}
}

func (s *MDNSServer) Start() error {
	// Bind to UDP port for broadcast
	addr, err := net.ResolveUDPAddr("udp4", ":7947")
	if err != nil {
		return err
	}

	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return err
	}

	s.connMu.Lock()
	s.conn = conn
	s.connMu.Unlock()
	s.shutdown = false

	// Start listening
	go s.listenAndServe()

	// Broadcast our presence
	go s.broadcastPresence()

	return nil
}

func (s *MDNSServer) listenAndServe() {
	buf := make([]byte, 1024)
	for !s.shutdown {
		s.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, addr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				continue
			}
			return
		}

		// Parse incoming discovery message
		msg := string(buf[:n])
		if strings.Contains(msg, "_anubiswatch") {
			// Respond with our info
			response := fmt.Sprintf("%s|%d|%s", s.instance, s.port, strings.Join(s.txt, ";"))
			s.conn.WriteToUDP([]byte(response), addr)
		}
	}
}

func (s *MDNSServer) broadcastPresence() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	msg := fmt.Sprintf("_anubiswatch_|%s|%d|%s", s.instance, s.port, strings.Join(s.txt, ";"))

	for !s.shutdown {
		// Broadcast to local network
		broadcastAddr := &net.UDPAddr{
			IP:   net.IPv4(255, 255, 255, 255),
			Port: 7947,
		}

		s.connMu.Lock()
		conn := s.conn
		s.connMu.Unlock()

		if conn != nil {
			conn.WriteToUDP([]byte(msg), broadcastAddr)
		}

		<-ticker.C
	}
}

func (s *MDNSServer) Stop() {
	s.shutdown = true
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if s.conn != nil {
		s.conn.Close()
	}
}

// MDNSClient implementation using UDP broadcast

func (d *Discovery) newMDNSClient() *MDNSClient {
	ctx, cancel := context.WithCancel(context.Background())
	return &MDNSClient{
		service: "_anubiswatch._tcp",
		domain:  "local",
		results: make(chan *MDNSService, 10),
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (c *MDNSClient) Start() error {
	// Bind to UDP port
	addr, err := net.ResolveUDPAddr("udp4", ":7947")
	if err != nil {
		return err
	}

	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return err
	}

	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()
	c.shutdown = false

	// Start listening for responses
	go c.listenForResponses()

	// Start querying
	go c.queryPeriodically()

	return nil
}

func (c *MDNSClient) listenForResponses() {
	buf := make([]byte, 1024)
	for !c.shutdown {
		c.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, addr, err := c.conn.ReadFromUDP(buf)
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				continue
			}
			return
		}

		// Parse response
		msg := string(buf[:n])
		if svc := c.parseResponse(msg, addr); svc != nil {
			select {
			case c.results <- svc:
			default:
				// Channel full, skip
			}
		}
	}
}

func (c *MDNSClient) parseResponse(msg string, addr *net.UDPAddr) *MDNSService {
	// Format: _anubiswatch_|instance|port|txt1;txt2;txt3
	parts := strings.Split(msg, "|")
	if len(parts) < 4 || !strings.Contains(parts[0], "_anubiswatch") {
		return nil
	}

	svc := &MDNSService{
		Name: parts[1],
		Host: addr.IP.String(),
		Port: 0,
		TXT:  strings.Split(parts[3], ";"),
	}

	// Parse port
	fmt.Sscanf(parts[2], "%d", &svc.Port)
	if svc.Port == 0 {
		svc.Port = 7946
	}

	return svc
}

func (c *MDNSClient) queryPeriodically() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for !c.shutdown {
		c.Query("_anubiswatch._tcp")
		<-ticker.C
	}
}

func (c *MDNSClient) Stop() {
	c.shutdown = true
	c.cancel()
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.conn != nil {
		c.conn.Close()
	}
}

func (c *MDNSClient) Query(service string) []*MDNSService {
	// Send broadcast query
	queryMsg := "_anubiswatch_"

	broadcastAddr := &net.UDPAddr{
		IP:   net.IPv4(255, 255, 255, 255),
		Port: 7947,
	}

	c.connMu.Lock()
	conn := c.conn
	c.connMu.Unlock()

	if conn != nil {
		conn.WriteToUDP([]byte(queryMsg), broadcastAddr)
	}

	// Collect responses
	var services []*MDNSService
	timeout := time.After(2 * time.Second)

	for {
		select {
		case svc := <-c.results:
			services = append(services, svc)
		case <-timeout:
			return services
		}
	}
}

// JSON helpers

func encodeGossip(msg *GossipMessage) ([]byte, error) {
	return json.Marshal(msg)
}

func decodeGossip(data []byte) (*GossipMessage, error) {
	var msg GossipMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}
