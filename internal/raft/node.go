package raft

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

const (
	stableHardState   = "hard_state"
	snapshotChunkSize = 1 << 20
)

type hardState struct {
	CurrentTerm uint64 `json:"current_term"`
	VotedFor    string `json:"voted_for"`
	CommitIndex uint64 `json:"commit_index"`
	LastApplied uint64 `json:"last_applied"`
	LogBase     uint64 `json:"log_base"`
	LogBaseTerm uint64 `json:"log_base_term"`
}

type snapshotInstall struct {
	index uint64
	term  uint64
	data  bytes.Buffer
}

// newAtomicState creates an atomic.Value initialized with the given RaftState
func newAtomicState(s core.RaftState) atomic.Value {
	var v atomic.Value
	v.Store(s)
	return v
}

// Node represents a Raft consensus node
// The Pharaoh's throne in the Necropolis
type Node struct {
	// Configuration
	config        core.RaftConfig
	nodeID        string
	bindAddr      string
	advertiseAddr string
	region        string

	// State machine (state is atomic for lock-free reads in run()). Log entries
	// retain their absolute Raft indexes even after compaction; logBase is the
	// snapshot/compaction boundary immediately before log[0].
	mu          sync.RWMutex
	state       atomic.Value // stores core.RaftState
	currentTerm uint64
	votedFor    string
	log         []core.RaftLogEntry
	logBase     uint64
	logBaseTerm uint64
	commitIndex uint64
	lastApplied uint64

	// Volatile state for leaders (reset on election)
	nextIndex  map[string]uint64
	matchIndex map[string]uint64

	// Peers
	peers  map[string]*Peer
	peerMu sync.RWMutex

	// Membership configuration (for joint consensus)
	membership struct {
		mu           sync.RWMutex
		config       []string        // Current configuration (node IDs)
		oldConfig    []string        // Old configuration during joint consensus
		newConfig    []string        // New configuration during joint consensus
		jointState   bool            // True if in joint consensus
		pendingIndex uint64          // Log index of pending membership change
		changes      map[uint64]bool // Track applied membership change log indices
	}

	// Storage
	storage  LogStore
	stable   StableStore
	snapshot SnapshotStore
	fsm      FSM

	// Snapshot state
	snapshotThreshold  int
	lastSnapshotIndex  uint64
	snapshotInProgress atomic.Bool
	snapshotMu         sync.Mutex
	incomingSnapshot   *snapshotInstall

	// Networking
	transport Transport

	// Channels for internal communication
	applyCh    chan *applyFuture
	commitCh   chan uint64
	rpcCh      chan *rpcWrapper
	shutdownCh chan struct{}
	doneCh     chan struct{}
	// electionResetCh signals `run()` to reset the follower election
	// timer when a valid AppendEntries arrives. Buffered so the sender
	// never blocks — multiple resets before `run()` drains coalesce.
	electionResetCh chan struct{}

	// Timing
	electionTimeout  time.Duration
	heartbeatTimeout time.Duration
	commitTimeout    time.Duration

	// Leader tracking
	leaderID    string
	lastContact time.Time

	// Control
	running  atomic.Bool
	shutdown atomic.Bool

	// Logger
	logger *slog.Logger

	// Stats
	stats        core.ClusterStats
	applyWaiters sync.Map
}

// Peer represents a remote Raft node
type Peer struct {
	ID           string
	Address      string
	Region       string
	Role         core.RaftRole
	nextIndex    uint64
	matchIndex   uint64
	lastContact  time.Time
	heartbeatRTT time.Duration
}

// applyFuture represents a future result of applying a command
type applyFuture struct {
	command core.FSMCommand
	index   uint64
	term    uint64
	err     error
	done    chan struct{}
}

// rpcWrapper wraps an RPC with response channel
type rpcWrapper struct {
	cmd    interface{}
	respCh chan interface{}
}

// FSM is the finite state machine interface
type FSM interface {
	Apply(log *core.RaftLogEntry) interface{}
	Snapshot() (core.FSMCommand, error)
	Restore(snapshot []byte) error
}

// LogStore is the interface for log storage
type LogStore interface {
	FirstIndex() (uint64, error)
	LastIndex() (uint64, error)
	GetLog(index uint64, log *core.RaftLogEntry) error
	StoreLog(log *core.RaftLogEntry) error
	StoreLogs(logs []core.RaftLogEntry) error
	DeleteRange(minIdx, maxIdx uint64) error
}

// StableStore persists the Raft hard state that must survive a crash before a
// node may answer another election RPC.
type StableStore interface {
	SetUint64(key string, val uint64) error
	GetUint64(key string) (uint64, error)
	Set(key string, val []byte) error
	Get(key string) ([]byte, error)
}

// SnapshotStore is the interface for snapshot storage
type SnapshotStore interface {
	Create(version, index, term uint64, configuration []byte) (SnapshotSink, error)
	List() ([]SnapshotMeta, error)
	Open(id string) (SnapshotSource, error)
}

// SnapshotMeta holds metadata about a snapshot
type SnapshotMeta struct {
	ID      string
	Index   uint64
	Term    uint64
	Size    int64
	Version uint64
}

// SnapshotSink is where snapshots are written
type SnapshotSink interface {
	Write(p []byte) (n int, err error)
	Close() error
	ID() string
	Cancel() error
}

// SnapshotSource is where snapshots are read from
type SnapshotSource interface {
	Read(p []byte) (n int, err error)
	Close() error
}

// Transport handles network communication
type Transport interface {
	Start() error
	Stop() error
	SendAppendEntries(peerID string, req *core.AppendEntriesRequest) (*core.AppendEntriesResponse, error)
	SendRequestVote(peerID string, req *core.RequestVoteRequest) (*core.RequestVoteResponse, error)
	SendPreVote(peerID string, req *core.PreVoteRequest) (*core.PreVoteResponse, error)
	SendInstallSnapshot(peerID string, req *core.InstallSnapshotRequest) (*core.InstallSnapshotResponse, error)
	SendHeartbeat(peerID string, req *core.HeartbeatRequest) (*core.HeartbeatResponse, error)
	LocalAddr() string
}

// NewNode creates a node without a stable store. It remains available for
// embedders and tests; production uses NewNodeWithStableStore.
// MutationApplier is the public write-path contract: callers submit typed
// FSM commands through Raft consensus instead of writing to local storage.
type MutationApplier interface {
	ApplyCommand(cmd core.FSMCommand, timeout time.Duration) (uint64, uint64, interface{}, error)
}

func NewNode(config core.RaftConfig, storage LogStore, snapshot SnapshotStore, fsm FSM, logger *slog.Logger) (*Node, error) {
	return NewNodeWithStableStore(config, storage, nil, snapshot, fsm, logger)
}

// NewNodeWithStableStore creates a Raft node whose hard state is durable.
func NewNodeWithStableStore(config core.RaftConfig, storage LogStore, stable StableStore, snapshot SnapshotStore, fsm FSM, logger *slog.Logger) (*Node, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	n := &Node{
		config:            config,
		nodeID:            config.NodeID,
		bindAddr:          config.BindAddr,
		advertiseAddr:     config.AdvertiseAddr,
		region:            "default",
		state:             newAtomicState(core.StateFollower),
		currentTerm:       0,
		votedFor:          "",
		log:               []core.RaftLogEntry{{Index: 0, Term: 0}},
		logBase:           0,
		logBaseTerm:       0,
		commitIndex:       0,
		lastApplied:       0,
		nextIndex:         make(map[string]uint64),
		matchIndex:        make(map[string]uint64),
		peers:             make(map[string]*Peer),
		storage:           storage,
		stable:            stable,
		snapshot:          snapshot,
		fsm:               fsm,
		applyCh:           make(chan *applyFuture, 256),
		commitCh:          make(chan uint64, 16),
		rpcCh:             make(chan *rpcWrapper, 256),
		shutdownCh:        make(chan struct{}),
		doneCh:            make(chan struct{}),
		electionResetCh:   make(chan struct{}, 1),
		electionTimeout:   config.ElectionTimeout.Duration,
		heartbeatTimeout:  config.HeartbeatTimeout.Duration,
		commitTimeout:     config.CommitTimeout.Duration,
		leaderID:          "",
		lastContact:       time.Now(),
		logger:            logger.With("component", "raft", "node_id", config.NodeID),
		snapshotThreshold: config.SnapshotThreshold,
		lastSnapshotIndex: 0,
	}

	// Initialize peers from config
	for _, p := range config.Peers {
		if p.ID != config.NodeID {
			n.peers[p.ID] = &Peer{
				ID:      p.ID,
				Address: p.Address,
				Region:  p.Region,
				Role:    p.Role,
			}
		}
	}

	// Initialize membership configuration
	n.membership.config = []string{config.NodeID}
	for _, p := range config.Peers {
		n.membership.config = append(n.membership.config, p.ID)
	}
	n.membership.changes = make(map[uint64]bool)

	return n, nil
}

// SetTransport sets the transport for the node and wires inbound RPCs
// (AppendEntries, RequestVote, PreVote, InstallSnapshot, Heartbeat) into the
// node's processing loop. Without this registration the transport would
// reject every incoming RPC with "unknown method" and followers could never
// hear from a leader or candidate.
func (n *Node) SetTransport(transport Transport) {
	n.transport = transport

	tt, ok := transport.(*TCPTransport)
	if !ok {
		return
	}
	handler := func(cmd interface{}, respCh chan interface{}) {
		select {
		case n.rpcCh <- &rpcWrapper{cmd: cmd, respCh: respCh}:
		case <-n.shutdownCh:
		}
	}
	for _, method := range []string{"AppendEntries", "RequestVote", "PreVote", "InstallSnapshot", "Heartbeat"} {
		tt.RegisterHandler(method, handler)
	}
}

// Start starts the Raft node
func (n *Node) Start() error {
	if n.running.Load() {
		return fmt.Errorf("node already running")
	}

	if err := n.restoreHardState(); err != nil {
		return fmt.Errorf("failed to restore Raft hard state: %w", err)
	}
	if err := n.restoreLatestSnapshot(); err != nil {
		return fmt.Errorf("failed to restore snapshot: %w", err)
	}
	if err := n.restoreLog(); err != nil {
		return fmt.Errorf("failed to restore log: %w", err)
	}
	if n.commitIndex > n.lastApplied {
		n.processCommitted(n.commitIndex)
		if n.lastApplied < n.commitIndex {
			return fmt.Errorf("failed to replay committed entries through index %d", n.commitIndex)
		}
	}

	// Register peer addresses with transport for connection pooling
	if tt, ok := n.transport.(*TCPTransport); ok {
		n.peerMu.RLock()
		for _, peer := range n.peers {
			tt.RegisterPeer(peer.ID, peer.Address)
		}
		n.peerMu.RUnlock()
	}

	// Start transport
	if n.transport != nil {
		if err := n.transport.Start(); err != nil {
			return fmt.Errorf("failed to start transport: %w", err)
		}
	}

	n.running.Store(true)

	// Start goroutines
	go n.run()
	go n.applyLoop()

	n.logger.Info("Raft node started",
		"node_id", n.nodeID,
		"bind_addr", n.bindAddr,
		"peers", len(n.peers))

	return nil
}

// Stop gracefully stops the Raft node
func (n *Node) Stop() error {
	if !n.running.Load() {
		return nil
	}

	n.shutdown.Store(true)
	close(n.shutdownCh)

	if n.transport != nil {
		if err := n.transport.Stop(); err != nil {
			n.logger.Warn("failed to stop transport", "err", err)
		}
	}

	// Wait for goroutines to finish
	select {
	case <-n.doneCh:
	case <-time.After(5 * time.Second):
		n.logger.Warn("Timeout waiting for node to stop")
	}

	n.running.Store(false)
	n.logger.Info("Raft node stopped")

	return nil
}

// State returns the current state
func (n *Node) State() core.RaftState {
	return n.state.Load().(core.RaftState)
}

// IsLeader returns true if this node is the leader
func (n *Node) IsLeader() bool {
	return n.state.Load().(core.RaftState) == core.StateLeader
}

// Leader returns the current leader ID
func (n *Node) Leader() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.leaderID
}

// Term returns the current term
func (n *Node) Term() uint64 {
	return atomic.LoadUint64(&n.currentTerm)
}

// LeaderID returns the current leader ID (public getter)
func (n *Node) LeaderID() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.leaderID
}

// CurrentTerm returns the current term (public getter)
func (n *Node) CurrentTerm() uint64 {
	return atomic.LoadUint64(&n.currentTerm)
}

// CommitIndex returns the index of the highest committed log entry
func (n *Node) CommitIndex() uint64 {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.commitIndex
}

// Peers returns a copy of the peers map
func (n *Node) Peers() map[string]*Peer {
	n.peerMu.RLock()
	defer n.peerMu.RUnlock()
	peersCopy := make(map[string]*Peer, len(n.peers))
	for k, v := range n.peers {
		peersCopy[k] = v
	}
	return peersCopy
}

// Done returns the shutdown channel
func (n *Node) Done() <-chan struct{} {
	return n.doneCh
}

// Shutdown initiates graceful shutdown (alias for Stop)
func (n *Node) Shutdown() {
	if err := n.Stop(); err != nil {
		n.logger.Warn("failed to stop Raft node", "err", err)
	}
}

// Apply applies a command to the FSM through Raft
func (n *Node) Apply(cmd core.FSMCommand, timeout time.Duration) (uint64, uint64, interface{}, error) {
	if n.shutdown.Load() {
		return 0, 0, nil, &core.RaftError{Code: core.ErrShutdown, Message: "node is shutting down"}
	}

	if !n.IsLeader() {
		return 0, 0, nil, &core.RaftError{
			Code:    core.ErrNotLeader,
			Message: "not leader",
			NodeID:  n.Leader(),
		}
	}

	future := &applyFuture{
		command: cmd,
		done:    make(chan struct{}),
	}

	// Send to apply channel
	select {
	case n.applyCh <- future:
	case <-time.After(timeout):
		return 0, 0, nil, &core.RaftError{Code: core.ErrTimeout, Message: "timeout submitting command"}
	}

	// Wait for result
	select {
	case <-future.done:
		return future.index, future.term, nil, future.err
	case <-time.After(timeout):
		return 0, 0, nil, &core.RaftError{Code: core.ErrTimeout, Message: "timeout waiting for apply"}
	}
}

// AddPeer adds a peer to the cluster using joint consensus
// This is safe for production use and prevents split-brain scenarios
func (n *Node) AddPeer(peer core.RaftPeer) error {
	n.mu.RLock()
	if n.state.Load().(core.RaftState) != core.StateLeader {
		n.mu.RUnlock()
		return &core.RaftError{Code: core.ErrNotLeader, Message: "only leader can add peers"}
	}
	n.mu.RUnlock()

	if peer.ID == n.nodeID {
		return fmt.Errorf("cannot add self as peer")
	}

	n.peerMu.RLock()
	if _, exists := n.peers[peer.ID]; exists {
		n.peerMu.RUnlock()
		return fmt.Errorf("peer %s already exists", peer.ID)
	}
	n.peerMu.RUnlock()

	// Get current configuration
	n.membership.mu.Lock()
	oldConfig := make([]string, len(n.membership.config))
	copy(oldConfig, n.membership.config)
	newConfig := append([]string(nil), n.membership.config...)
	newConfig = append(newConfig, peer.ID)
	n.membership.mu.Unlock()

	// Create membership change entry for joint consensus
	change := core.MembershipChange{
		Type:      core.MembershipAddPeer,
		Peer:      peer,
		OldConfig: oldConfig,
		NewConfig: newConfig,
		Phase:     "joint",
	}

	// Propose the membership change
	if err := n.proposeMembershipChange(change); err != nil {
		return fmt.Errorf("failed to propose membership change: %w", err)
	}

	// Register peer address with transport
	if tt, ok := n.transport.(*TCPTransport); ok {
		tt.RegisterPeer(peer.ID, peer.Address)
	}

	n.logger.Info("Peer added via joint consensus", "peer_id", peer.ID, "address", peer.Address)
	return nil
}

// RemovePeer removes a peer from the cluster using joint consensus
// This is safe for production use and prevents quorum loss
func (n *Node) RemovePeer(peerID string) error {
	n.mu.RLock()
	if n.state.Load().(core.RaftState) != core.StateLeader {
		n.mu.RUnlock()
		return &core.RaftError{Code: core.ErrNotLeader, Message: "only leader can remove peers"}
	}
	n.mu.RUnlock()

	if peerID == n.nodeID {
		return fmt.Errorf("cannot remove self")
	}

	n.peerMu.RLock()
	peer, exists := n.peers[peerID]
	if !exists {
		n.peerMu.RUnlock()
		return fmt.Errorf("peer %s not found", peerID)
	}
	n.peerMu.RUnlock()

	// Get current configuration
	n.membership.mu.Lock()
	oldConfig := make([]string, len(n.membership.config))
	copy(oldConfig, n.membership.config)
	newConfig := make([]string, 0, len(n.membership.config)-1)
	for _, id := range n.membership.config {
		if id != peerID {
			newConfig = append(newConfig, id)
		}
	}
	n.membership.mu.Unlock()

	// Create membership change entry for joint consensus
	change := core.MembershipChange{
		Type:      core.MembershipRemovePeer,
		Peer:      core.RaftPeer{ID: peer.ID, Address: peer.Address, Region: peer.Region, Role: peer.Role},
		OldConfig: oldConfig,
		NewConfig: newConfig,
		Phase:     "joint",
	}

	// Propose the membership change
	if err := n.proposeMembershipChange(change); err != nil {
		return fmt.Errorf("failed to propose membership change: %w", err)
	}

	// Unregister peer from transport
	if tt, ok := n.transport.(*TCPTransport); ok {
		tt.UnregisterPeer(peerID)
	}

	n.logger.Info("Peer removed via joint consensus", "peer_id", peerID)
	return nil
}

// proposeMembershipChange proposes a membership change to the cluster
func (n *Node) proposeMembershipChange(change core.MembershipChange) error {
	data, _ := json.Marshal(change)

	entry := core.RaftLogEntry{Term: n.currentTerm, Type: core.LogMembershipChange, Data: data}
	n.mu.Lock()
	entry.Term = n.currentTerm
	if err := n.appendEntry(entry); err != nil {
		n.mu.Unlock()
		return fmt.Errorf("persist membership change: %w", err)
	}
	entry = n.log[len(n.log)-1]
	n.mu.Unlock()

	// Replicate to followers
	n.replicateLog()

	// Wait for the entry to be committed
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for membership change to commit")
		case <-ticker.C:
			n.mu.RLock()
			applied := n.lastApplied >= entry.Index
			n.mu.RUnlock()
			if applied {
				return nil
			}
		}
	}
}

// applyMembershipChange applies a committed membership change
func (n *Node) applyMembershipChange(change core.MembershipChange, index uint64) {
	n.membership.mu.Lock()
	defer n.membership.mu.Unlock()

	switch change.Phase {
	case "joint":
		// Enter joint consensus
		n.membership.oldConfig = change.OldConfig
		n.membership.newConfig = change.NewConfig
		n.membership.jointState = true
		n.membership.pendingIndex = index

		// Add the peer to our local map (for AddPeer)
		if change.Type == core.MembershipAddPeer {
			n.peerMu.Lock()
			n.peers[change.Peer.ID] = &Peer{
				ID:      change.Peer.ID,
				Address: change.Peer.Address,
				Region:  change.Peer.Region,
				Role:    change.Peer.Role,
			}
			n.peerMu.Unlock()
		}

		n.logger.Info("Entered joint consensus for membership change",
			"type", change.Type,
			"peer", change.Peer.ID,
			"old_config", change.OldConfig,
			"new_config", change.NewConfig)

		// Schedule transition to final configuration
		go n.transitionToFinalConfig(change, index)

	case "final":
		// Exit joint consensus, use new configuration
		n.membership.config = change.NewConfig
		n.membership.oldConfig = nil
		n.membership.newConfig = nil
		n.membership.jointState = false
		n.membership.pendingIndex = 0
		n.membership.changes[index] = true

		// Remove the peer from our local map (for RemovePeer)
		if change.Type == core.MembershipRemovePeer {
			n.peerMu.Lock()
			delete(n.peers, change.Peer.ID)
			n.peerMu.Unlock()
		}

		n.logger.Info("Membership change completed",
			"type", change.Type,
			"peer", change.Peer.ID,
			"config", change.NewConfig)
	}
}

// transitionToFinalConfig transitions from joint consensus to final configuration
func (n *Node) transitionToFinalConfig(change core.MembershipChange, jointIndex uint64) {
	// Wait a bit to ensure joint consensus entry is committed
	// Use select with shutdownCh to allow graceful cancellation
	select {
	case <-time.After(2 * time.Second):
	case <-n.shutdownCh:
		n.logger.Info("transitionToFinalConfig cancelled - node shutting down")
		return
	}

	n.mu.RLock()
	if n.state.Load().(core.RaftState) != core.StateLeader {
		n.mu.RUnlock()
		return
	}
	term := n.currentTerm
	n.mu.RUnlock()

	// Create final phase entry
	finalChange := change
	finalChange.Phase = "final"
	finalChange.OldConfig = nil

	data, _ := json.Marshal(finalChange)

	entry := core.RaftLogEntry{Term: term, Type: core.LogMembershipChange, Data: data}
	n.mu.Lock()
	entry.Term = n.currentTerm
	if err := n.appendEntry(entry); err != nil {
		n.logger.Error("Failed to persist final membership configuration", "error", err)
		n.mu.Unlock()
		return
	}
	entry = n.log[len(n.log)-1]
	n.mu.Unlock()

	n.replicateLog()

	n.logger.Info("Proposed final configuration", "peer", change.Peer.ID, "index", entry.Index)
}

// replicateLog triggers log replication to all peers
func (n *Node) replicateLog() {
	// Guard against nil transport in test scenarios
	if n.transport == nil {
		return
	}

	n.peerMu.RLock()
	peers := make([]*Peer, 0, len(n.peers))
	for _, p := range n.peers {
		peers = append(peers, p)
	}
	n.peerMu.RUnlock()

	for _, peer := range peers {
		go func(p *Peer) {
			req := &core.AppendEntriesRequest{
				Term:         n.currentTerm,
				LeaderID:     n.nodeID,
				PrevLogIndex: p.matchIndex,
				PrevLogTerm:  n.getLogTerm(p.matchIndex),
				Entries:      n.getEntriesAfter(p.nextIndex, n.config.MaxAppendEntries),
				LeaderCommit: n.commitIndex,
			}

			resp, err := n.transport.SendAppendEntries(p.ID, req)
			if err != nil {
				return
			}

			n.handleAppendEntriesResponse(p, req, resp)
		}(peer)
	}
}

// GetPeers returns the current peers
func (n *Node) GetPeers() []core.RaftPeer {
	n.peerMu.RLock()
	defer n.peerMu.RUnlock()

	peers := make([]core.RaftPeer, 0, len(n.peers))
	for _, p := range n.peers {
		peers = append(peers, core.RaftPeer{
			ID:      p.ID,
			Address: p.Address,
			Region:  p.Region,
			Role:    p.Role,
		})
	}
	return peers
}

// GetState returns the current cluster state
func (n *Node) GetState() core.ClusterState {
	n.mu.RLock()
	defer n.mu.RUnlock()
	n.peerMu.RLock()
	defer n.peerMu.RUnlock()

	peers := make([]core.RaftPeerInfo, 0, len(n.peers))
	for _, p := range n.peers {
		peers = append(peers, core.RaftPeerInfo{
			RaftPeer: core.RaftPeer{
				ID:      p.ID,
				Address: p.Address,
				Region:  p.Region,
				Role:    p.Role,
			},
			IsConnected:  time.Since(p.lastContact) < n.heartbeatTimeout*3,
			LastContact:  p.lastContact,
			NextIndex:    p.nextIndex,
			MatchIndex:   p.matchIndex,
			HeartbeatRTT: core.Duration{Duration: p.heartbeatRTT},
		})
	}

	return core.ClusterState{
		NodeID:       n.nodeID,
		State:        n.state.Load().(core.RaftState),
		Term:         n.currentTerm,
		LastLogIndex: n.lastLogIndexLocked(),
		LastLogTerm:  n.getLogTerm(n.lastLogIndexLocked()),
		CommitIndex:  n.commitIndex,
		LastApplied:  n.lastApplied,
		LeaderID:     n.leaderID,
		VotedFor:     n.votedFor,
		Peers:        peers,
		Stats:        n.stats,
		LastContact:  n.lastContact,
	}
}

// run is the main event loop
func (n *Node) run() {
	defer close(n.doneCh)

	electionTimer := n.newElectionTimer()
	commitTimer := time.NewTimer(n.commitTimeout)
	defer commitTimer.Stop()

	// Heartbeat ticker — the leader must broadcast AppendEntries at
	// roughly heartbeatTimeout/2 intervals so followers reset their
	// election timers before they start a competing election. Without
	// this ticker, sendHeartbeats fires exactly once inside becomeLeader
	// and never again: followers time out, become candidates, and the
	// cluster never stabilises.
	heartbeatInterval := n.heartbeatTimeout / 2
	if heartbeatInterval <= 0 {
		heartbeatInterval = 50 * time.Millisecond
	}
	heartbeatTicker := time.NewTicker(heartbeatInterval)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-n.shutdownCh:
			return

		case <-electionTimer.C:
			if n.state.Load().(core.RaftState) != core.StateLeader {
				n.startElection()
			}
			electionTimer = n.newElectionTimer()

		case <-commitTimer.C:
			if n.state.Load().(core.RaftState) == core.StateLeader {
				n.mu.Lock()
				n.checkCommit()
				n.mu.Unlock()
			}
			commitTimer.Reset(n.commitTimeout)

		case <-heartbeatTicker.C:
			if n.state.Load().(core.RaftState) == core.StateLeader {
				n.mu.RLock()
				commitIndex := n.commitIndex
				n.mu.RUnlock()
				n.sendHeartbeats(commitIndex)
			}

		case <-n.electionResetCh:
			// Follower heard from a live leader; defer the election
			// timer by the full randomized window so it doesn't race
			// against the next heartbeat.
			electionTimer.Stop()
			electionTimer = n.newElectionTimer()

		case rpc := <-n.rpcCh:
			n.handleRPC(rpc)

		case idx := <-n.commitCh:
			n.processCommitted(idx)
			n.maybeTakeSnapshot() // Check if snapshot needed after commit
		}

		// Send heartbeats if leader
		if n.state.Load().(core.RaftState) == core.StateLeader {
			n.mu.RLock()
			commitIndex := n.commitIndex
			n.mu.RUnlock()
			n.sendHeartbeats(commitIndex)
		}
	}
}

// applyLoop applies committed entries to the FSM
func (n *Node) applyLoop() {
	for {
		select {
		case <-n.shutdownCh:
			return
		case future := <-n.applyCh:
			n.handleApply(future)
		}
	}
}

// startElection initiates a leader election with pre-vote
func (n *Node) startElection() {
	n.mu.Lock()

	// Check if single-node mode (no peers)
	n.peerMu.RLock()
	peers := make([]*Peer, 0, len(n.peers))
	for _, p := range n.peers {
		if p.Role != core.RoleNonVoter {
			peers = append(peers, p)
		}
	}
	n.peerMu.RUnlock()

	// Single-node mode: if no voting peers, immediately become leader
	// This allows a standalone node to function without requiring network consensus
	if len(peers) == 0 {
		n.logger.Info("Single-node mode: no peers, becoming leader immediately")
		n.state.Store(core.StateCandidate)
		n.currentTerm++
		n.votedFor = n.nodeID
		if err := n.persistHardStateLocked(); err != nil {
			n.logger.Error("Failed to persist single-node election state", "error", err)
			n.state.Store(core.StateFollower)
			n.mu.Unlock()
			return
		}
		n.leaderID = ""
		n.lastContact = time.Now()
		// Note: becomeLeader acquires its own lock, so we must unlock first
		n.mu.Unlock()
		n.becomeLeader()
		return
	}

	// Leader stickiness: if we've heard from a live leader within the base
	// election timeout, don't disrupt a healthy cluster just because our
	// local timer fired.
	if n.leaderID != "" && n.leaderID != n.nodeID && time.Since(n.lastContact) < n.electionTimeout {
		n.mu.Unlock()
		return
	}

	// Multi-node: proceed with full election process
	n.state.Store(core.StateCandidate)
	preVoteTerm := n.currentTerm + 1
	lastLogIndex := n.lastLogIndexLocked()
	lastLogTerm := n.getLogTerm(lastLogIndex)

	n.logger.Info("Starting pre-vote",
		"current_term", preVoteTerm,
		"last_log_index", lastLogIndex,
		"last_log_term", lastLogTerm)

	n.mu.Unlock()

	// Request pre-votes from all peers
	preVotes := n.requestPreVotes(preVoteTerm, lastLogIndex, lastLogTerm, peers)

	// Check if we should proceed with real election
	n.mu.Lock()

	if !preVotes {
		n.logger.Info("Pre-vote failed, not starting election")
		n.state.Store(core.StateFollower)
		n.mu.Unlock()
		return
	}

	// Pre-vote succeeded, start real election
	n.state.Store(core.StateCandidate)
	n.currentTerm++
	n.votedFor = n.nodeID
	if err := n.persistHardStateLocked(); err != nil {
		n.logger.Error("Failed to persist election state", "error", err)
		n.state.Store(core.StateFollower)
		n.mu.Unlock()
		return
	}
	n.leaderID = ""
	n.lastContact = time.Now()

	term := n.currentTerm
	lastLogIndex = n.lastLogIndexLocked()
	lastLogTerm = n.getLogTerm(lastLogIndex)

	n.logger.Info("Pre-vote succeeded, starting election",
		"term", term,
		"last_log_index", lastLogIndex,
		"last_log_term", lastLogTerm)

	// Release the lock before issuing vote RPCs: requestVotes blocks on the
	// network and becomeLeader acquires the lock itself.
	n.mu.Unlock()

	// Request votes from all peers
	votesGranted := n.requestVotes(term, lastLogIndex, lastLogTerm, peers)

	// Check if we won
	votesNeeded := int32((len(peers)+1)/2 + 1)
	if votesGranted >= votesNeeded {
		n.becomeLeader()
	} else {
		n.logger.Info("Election failed",
			"term", term,
			"votes", votesGranted,
			"needed", votesNeeded)
		n.state.Store(core.StateFollower)
	}
}

// requestPreVotes sends PreVote RPCs to all peers and returns true if majority would grant votes
func (n *Node) requestPreVotes(term, lastLogIndex, lastLogTerm uint64, peers []*Peer) bool {
	var preVotesGranted atomic.Int32
	preVotesGranted.Add(1) // Vote for self

	// Skip peer RPCs if transport is nil (test scenarios)
	if n.transport == nil {
		// With no transport, just count self-vote
		needed := len(peers)/2 + 1
		return int(preVotesGranted.Load()) >= needed
	}

	var wg sync.WaitGroup
	for _, peer := range peers {
		wg.Add(1)
		go func(p *Peer) {
			defer wg.Done()

			req := &core.PreVoteRequest{
				Term:         term,
				CandidateID:  n.nodeID,
				LastLogIndex: lastLogIndex,
				LastLogTerm:  lastLogTerm,
			}

			resp, err := n.transport.SendPreVote(p.ID, req)
			if err != nil {
				n.logger.Debug("PreVote failed",
					"peer", p.ID, "error", err)
				return
			}

			if resp.VoteGranted {
				preVotesGranted.Add(1)
			}

			// Update term if peer has higher term
			if resp.Term > term {
				n.mu.Lock()
				if resp.Term > n.currentTerm {
					oldTerm, oldVote := n.currentTerm, n.votedFor
					n.currentTerm, n.votedFor = resp.Term, ""
					if err := n.persistHardStateLocked(); err != nil {
						n.currentTerm, n.votedFor = oldTerm, oldVote
					} else {
						n.state.Store(core.StateFollower)
					}
				}
				n.mu.Unlock()
			}
		}(peer)
	}

	// Wait for pre-votes with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All responses received
	case <-time.After(n.electionTimeout / 2):
		// Timeout waiting for some pre-votes
		n.logger.Debug("PreVote timeout waiting for responses")
	}

	votes := preVotesGranted.Load()
	needed := int32((len(peers)+1)/2 + 1)

	n.logger.Debug("PreVote results",
		"votes", votes,
		"needed", needed,
		"total", len(peers)+1)

	return votes >= needed
}

// requestVotes sends RequestVote RPCs and returns the number of votes granted
func (n *Node) requestVotes(term, lastLogIndex, lastLogTerm uint64, peers []*Peer) int32 {
	var votesGranted atomic.Int32
	votesGranted.Add(1) // Vote for self

	// Skip peer RPCs if transport is nil (test scenarios)
	if n.transport == nil {
		return votesGranted.Load() // Return just self-vote
	}

	var wg sync.WaitGroup
	for _, peer := range peers {
		wg.Add(1)
		go func(p *Peer) {
			defer wg.Done()

			req := &core.RequestVoteRequest{
				Term:         term,
				CandidateID:  n.nodeID,
				LastLogIndex: lastLogIndex,
				LastLogTerm:  lastLogTerm,
				PreVoteTerm:  term, // Indicate we completed pre-vote
			}

			resp, err := n.transport.SendRequestVote(p.ID, req)
			if err != nil {
				n.logger.Debug("RequestVote failed",
					"peer", p.ID, "error", err)
				return
			}

			if resp.VoteGranted {
				votesGranted.Add(1)
			}

			// Update term if peer has higher term
			if resp.Term > term {
				n.mu.Lock()
				if resp.Term > n.currentTerm {
					oldTerm, oldVote := n.currentTerm, n.votedFor
					n.currentTerm, n.votedFor = resp.Term, ""
					if err := n.persistHardStateLocked(); err != nil {
						n.currentTerm, n.votedFor = oldTerm, oldVote
					} else {
						n.state.Store(core.StateFollower)
					}
				}
				n.mu.Unlock()
			}
		}(peer)
	}

	// Wait for votes with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All responses received
	case <-time.After(n.electionTimeout / 2):
		// Timeout waiting for some votes
		n.logger.Debug("RequestVote timeout waiting for responses")
	}

	return votesGranted.Load()
}

// becomeLeader transitions to leader state
// Caller must NOT hold n.mu - this function acquires it internally
func (n *Node) becomeLeader() {
	n.mu.Lock()

	n.logger.Info("Became leader", "term", n.currentTerm)

	n.state.Store(core.StateLeader)
	n.leaderID = n.nodeID
	n.stats.ElectionsWon++
	n.stats.LeaderChanges++

	// Initialize leader state
	lastLogIndex := n.lastLogIndexLocked()
	n.peerMu.RLock()
	for _, p := range n.peers {
		n.nextIndex[p.ID] = lastLogIndex + 1
		n.matchIndex[p.ID] = 0
	}
	n.peerMu.RUnlock()

	// Persist the leader no-op before it can be replicated or committed.
	if err := n.appendEntry(core.RaftLogEntry{Term: n.currentTerm, Type: core.LogNoOp}); err != nil {
		n.logger.Error("Failed to persist leader no-op", "error", err)
		n.state.Store(core.StateFollower)
		n.leaderID = ""
		n.mu.Unlock()
		return
	}

	commitIndex := n.commitIndex
	// Release before sending: sendHeartbeats takes n.mu.RLock itself.
	n.mu.Unlock()

	// Send immediate heartbeats
	n.sendHeartbeats(commitIndex)
}

// becomeFollower transitions to follower state
func (n *Node) becomeFollower(term uint64) error {
	wasLeader := n.state.Load().(core.RaftState) == core.StateLeader
	oldTerm, oldVote := n.currentTerm, n.votedFor
	n.currentTerm, n.votedFor = term, ""
	if err := n.persistHardStateLocked(); err != nil {
		n.currentTerm, n.votedFor = oldTerm, oldVote
		return err
	}
	n.state.Store(core.StateFollower)
	n.lastContact = time.Now()
	if wasLeader {
		n.logger.Info("Stepped down as leader", "term", term)
		n.stats.ElectionsLost++
	}
	return nil
}

func (n *Node) sendHeartbeats(commitIndex uint64) {
	// Guard against nil transport in test scenarios
	if n.transport == nil {
		return
	}

	n.peerMu.RLock()
	peers := make([]*Peer, 0, len(n.peers))
	for _, p := range n.peers {
		peers = append(peers, p)
	}
	n.peerMu.RUnlock()

	// Build every request under the read lock before spawning senders:
	// peer replication state (matchIndex/nextIndex), currentTerm, and the log
	// are all mutated by handleAppendEntriesResponse while holding n.mu.
	reqs := make([]*core.AppendEntriesRequest, len(peers))
	n.mu.RLock()
	for i, p := range peers {
		// PrevLogIndex must be the entry immediately before the first entry
		// sent (nextIndex-1), NOT matchIndex: after a leader change
		// nextIndex != matchIndex+1, and anchoring on matchIndex makes
		// followers splice entries at the wrong log positions.
		prevLogIndex := uint64(0)
		if p.nextIndex > 0 {
			prevLogIndex = p.nextIndex - 1
		}
		reqs[i] = &core.AppendEntriesRequest{
			Term:         n.currentTerm,
			LeaderID:     n.nodeID,
			PrevLogIndex: prevLogIndex,
			PrevLogTerm:  n.getLogTerm(prevLogIndex),
			Entries:      n.getEntriesAfter(p.nextIndex, n.config.MaxAppendEntries),
			LeaderCommit: commitIndex,
		}
	}
	n.mu.RUnlock()

	for i, peer := range peers {
		go func(p *Peer, req *core.AppendEntriesRequest) {
			if p.nextIndex <= n.logBase {
				n.sendSnapshot(p)
				return
			}
			resp, err := n.transport.SendAppendEntries(p.ID, req)
			if err != nil {
				return
			}

			n.handleAppendEntriesResponse(p, req, resp)
		}(peer, reqs[i])
	}
}

// handleRPC processes an incoming RPC
func (n *Node) handleRPC(rpc *rpcWrapper) {
	switch cmd := rpc.cmd.(type) {
	case *core.AppendEntriesRequest:
		resp := n.handleAppendEntries(cmd)
		rpc.respCh <- resp

	case *core.RequestVoteRequest:
		resp := n.handleRequestVote(cmd)
		rpc.respCh <- resp

	case *core.PreVoteRequest:
		resp := n.handlePreVote(cmd)
		rpc.respCh <- resp

	case *core.InstallSnapshotRequest:
		resp := n.handleInstallSnapshot(cmd)
		rpc.respCh <- resp

	case *core.HeartbeatRequest:
		resp := n.handleHeartbeat(cmd)
		rpc.respCh <- resp
	}
}

// handleAppendEntries processes AppendEntries RPC
func (n *Node) handleAppendEntries(req *core.AppendEntriesRequest) *core.AppendEntriesResponse {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Handle term mismatch
	if resp := n.validateTerm(req); resp != nil {
		return resp
	}

	// Valid heartbeat from leader
	n.lastContact = time.Now()
	n.leaderID = req.LeaderID
	// Reset the follower's election timer. Without this, the peer's
	// election timer fires at its scheduled instant regardless of how
	// recently we heard from the leader; combined with the time it
	// takes to acquire the leader-stickiness lock, that lets a
	// healthy leader be displaced by a stale-term campaign.
	select {
	case n.electionResetCh <- struct{}{}:
	default:
	}

	// Check log consistency with leader
	if resp := n.checkLogConsistency(req); resp != nil {
		return resp
	}

	// Persist and reconcile log entries before acknowledging the leader.
	if err := n.reconcileLogEntries(req); err != nil {
		n.logger.Error("Failed to persist AppendEntries", "error", err)
		return &core.AppendEntriesResponse{Term: n.currentTerm, Success: false}
	}

	// Update commit index
	n.updateCommitIndex(req)

	return &core.AppendEntriesResponse{
		Term:       n.currentTerm,
		Success:    true,
		MatchIndex: req.PrevLogIndex + uint64(len(req.Entries)),
	}
}

// validateTerm checks if the term is valid and updates state if needed
// Returns error response if term is stale, nil if valid
func (n *Node) validateTerm(req *core.AppendEntriesRequest) *core.AppendEntriesResponse {
	if req.Term < n.currentTerm {
		return &core.AppendEntriesResponse{Term: n.currentTerm, Success: false}
	}
	if req.Term > n.currentTerm {
		if err := n.becomeFollower(req.Term); err != nil {
			n.logger.Error("Failed to persist AppendEntries term", "error", err)
			return &core.AppendEntriesResponse{Term: n.currentTerm, Success: false}
		}
	}
	return nil
}

func (n *Node) checkLogConsistency(req *core.AppendEntriesRequest) *core.AppendEntriesResponse {
	if req.PrevLogIndex == n.logBase {
		if req.PrevLogTerm != n.logBaseTerm {
			return &core.AppendEntriesResponse{Term: n.currentTerm, Success: false, ConflictIndex: n.logBase + 1}
		}
		return nil
	}
	entry, ok := n.logEntry(req.PrevLogIndex)
	if !ok {
		return &core.AppendEntriesResponse{Term: n.currentTerm, Success: false, MatchIndex: n.lastLogIndexLocked(), ConflictIndex: n.logBase + 1}
	}
	if entry.Term != req.PrevLogTerm {
		conflictTerm, conflictIndex := entry.Term, req.PrevLogIndex
		for conflictIndex > n.logBase+1 && n.getLogTerm(conflictIndex-1) == conflictTerm {
			conflictIndex--
		}
		return &core.AppendEntriesResponse{Term: n.currentTerm, Success: false, ConflictTerm: conflictTerm, ConflictIndex: conflictIndex}
	}
	return nil
}

// reconcileLogEntries reconciles local log with entries from the leader
func (n *Node) reconcileLogEntries(req *core.AppendEntriesRequest) error {
	truncateAt := uint64(0)
	for i, incoming := range req.Entries {
		idx := req.PrevLogIndex + uint64(i) + 1
		if local, ok := n.logEntry(idx); ok && local.Term != incoming.Term {
			truncateAt = idx
			break
		}
	}
	if truncateAt > 0 {
		last := n.lastLogIndexLocked()
		if n.storage != nil && truncateAt <= last {
			if err := n.storage.DeleteRange(truncateAt, last); err != nil {
				return fmt.Errorf("delete conflicting log suffix: %w", err)
			}
		}
		pos, _ := n.logPosition(truncateAt)
		n.log = n.log[:pos]
	}
	last := n.lastLogIndexLocked()
	appended := make([]core.RaftLogEntry, 0)
	for i, incoming := range req.Entries {
		idx := req.PrevLogIndex + uint64(i) + 1
		if idx > last {
			incoming.Index = idx
			appended = append(appended, incoming)
		}
	}
	if len(appended) > 0 && n.storage != nil {
		if err := n.storage.StoreLogs(appended); err != nil {
			return fmt.Errorf("persist appended entries: %w", err)
		}
	}
	n.log = append(n.log, appended...)
	return nil
}

func (n *Node) updateCommitIndex(req *core.AppendEntriesRequest) {
	if req.LeaderCommit <= n.commitIndex {
		return
	}
	lastNewIndex := req.PrevLogIndex + uint64(len(req.Entries))
	newCommit := req.LeaderCommit
	if newCommit > lastNewIndex {
		newCommit = lastNewIndex
	}
	old := n.commitIndex
	n.commitIndex = newCommit
	if err := n.persistHardStateLocked(); err != nil {
		n.commitIndex = old
		n.logger.Error("Failed to persist follower commit index", "error", err)
		return
	}
	n.commitCh <- n.commitIndex
}

func (n *Node) handleRequestVote(req *core.RequestVoteRequest) *core.RequestVoteResponse {
	n.mu.Lock()
	defer n.mu.Unlock()
	if req.Term < n.currentTerm {
		return &core.RequestVoteResponse{Term: n.currentTerm, VoteGranted: false, Reason: "term too old"}
	}
	if req.Term > n.currentTerm {
		if err := n.becomeFollower(req.Term); err != nil {
			return &core.RequestVoteResponse{Term: n.currentTerm, VoteGranted: false, Reason: "failed to persist term"}
		}
	}
	canVote := n.votedFor == "" || n.votedFor == req.CandidateID
	logIsCurrent := n.isLogMoreUpToDate(req.LastLogTerm, req.LastLogIndex)
	if canVote && logIsCurrent {
		oldVote := n.votedFor
		n.votedFor = req.CandidateID
		if err := n.persistHardStateLocked(); err != nil {
			n.votedFor = oldVote
			return &core.RequestVoteResponse{Term: n.currentTerm, VoteGranted: false, Reason: "failed to persist vote"}
		}
		n.lastContact = time.Now()
		return &core.RequestVoteResponse{Term: n.currentTerm, VoteGranted: true}
	}
	reason := "already voted"
	if !logIsCurrent {
		reason = "log not current"
	}
	return &core.RequestVoteResponse{Term: n.currentTerm, VoteGranted: false, Reason: reason}
}

func (n *Node) isLogMoreUpToDate(candidateLastLogTerm, candidateLastLogIndex uint64) bool {
	lastLogIndex := n.lastLogIndexLocked()
	lastLogTerm := n.getLogTerm(lastLogIndex)

	return candidateLastLogTerm > lastLogTerm ||
		(candidateLastLogTerm == lastLogTerm && candidateLastLogIndex >= lastLogIndex)
}

// handlePreVote processes PreVote RPC
func (n *Node) handlePreVote(req *core.PreVoteRequest) *core.PreVoteResponse {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.logger.Debug("Received PreVote request",
		"candidate", req.CandidateID,
		"term", req.Term,
		"last_log_index", req.LastLogIndex,
		"last_log_term", req.LastLogTerm)

	// In pre-vote, we don't update our term yet
	// We only check if we would grant a vote

	// Check if the candidate's term is at least as current as ours
	if req.Term < n.currentTerm {
		return &core.PreVoteResponse{
			Term:        n.currentTerm,
			VoteGranted: false,
			Reason:      "term too old",
		}
	}

	// Leader stickiness (Raft §4.2.3): deny pre-votes while we believe a
	// live leader exists. Without this, any node whose election timer fires
	// early can depose a healthy leader and cause perpetual churn.
	isLeader := n.state.Load().(core.RaftState) == core.StateLeader
	hasFreshLeader := n.leaderID != "" && n.leaderID != req.CandidateID &&
		time.Since(n.lastContact) < n.electionTimeout
	if isLeader || hasFreshLeader {
		return &core.PreVoteResponse{
			Term:        n.currentTerm,
			VoteGranted: false,
			Reason:      "have live leader",
		}
	}

	// Check if candidate's log is at least as up-to-date as ours
	logIsCurrent := n.isLogMoreUpToDate(req.LastLogTerm, req.LastLogIndex)

	if !logIsCurrent {
		n.logger.Debug("PreVote denied: log not current",
			"candidate_log_term", req.LastLogTerm,
			"candidate_log_index", req.LastLogIndex)

		return &core.PreVoteResponse{
			Term:        n.currentTerm,
			VoteGranted: false,
			Reason:      "log not current",
		}
	}

	// We would grant a vote - candidate has current log
	n.logger.Debug("PreVote granted",
		"candidate", req.CandidateID,
		"term", req.Term)

	return &core.PreVoteResponse{
		Term:        n.currentTerm,
		VoteGranted: true,
	}
}

// handleInstallSnapshot processes InstallSnapshot RPC
func (n *Node) handleInstallSnapshot(req *core.InstallSnapshotRequest) *core.InstallSnapshotResponse {
	n.mu.Lock()
	if req.Term < n.currentTerm {
		term := n.currentTerm
		n.mu.Unlock()
		return &core.InstallSnapshotResponse{Term: term, Success: false}
	}
	if req.Term > n.currentTerm {
		if err := n.becomeFollower(req.Term); err != nil {
			term := n.currentTerm
			n.mu.Unlock()
			return &core.InstallSnapshotResponse{Term: term, Success: false}
		}
	}
	n.lastContact, n.leaderID = time.Now(), req.LeaderID
	term := n.currentTerm
	n.mu.Unlock()

	n.snapshotMu.Lock()
	defer n.snapshotMu.Unlock()
	if req.Offset == 0 {
		n.incomingSnapshot = &snapshotInstall{index: req.LastIncludedIndex, term: req.LastIncludedTerm}
	}
	install := n.incomingSnapshot
	if install == nil || install.index != req.LastIncludedIndex || install.term != req.LastIncludedTerm || uint64(install.data.Len()) != req.Offset {
		return &core.InstallSnapshotResponse{Term: term, Success: false}
	}
	if _, err := install.data.Write(req.Data); err != nil {
		return &core.InstallSnapshotResponse{Term: term, Success: false}
	}
	if !req.Done {
		return &core.InstallSnapshotResponse{Term: term, Success: true}
	}
	payload := append([]byte(nil), install.data.Bytes()...)
	if err := n.fsm.Restore(payload); err != nil {
		n.incomingSnapshot = nil
		return &core.InstallSnapshotResponse{Term: term, Success: false}
	}
	if n.snapshot != nil {
		sink, err := n.snapshot.Create(1, install.index, install.term, nil)
		if err != nil {
			n.incomingSnapshot = nil
			return &core.InstallSnapshotResponse{Term: term, Success: false}
		}
		if _, err = sink.Write(payload); err != nil {
			_ = sink.Cancel()
			n.incomingSnapshot = nil
			return &core.InstallSnapshotResponse{Term: term, Success: false}
		}
		if err = sink.Close(); err != nil {
			n.incomingSnapshot = nil
			return &core.InstallSnapshotResponse{Term: term, Success: false}
		}
	}
	n.mu.Lock()
	old := hardState{CurrentTerm: n.currentTerm, VotedFor: n.votedFor, CommitIndex: n.commitIndex, LastApplied: n.lastApplied, LogBase: n.logBase, LogBaseTerm: n.logBaseTerm}
	n.logBase, n.logBaseTerm = install.index, install.term
	n.commitIndex, n.lastApplied, n.lastSnapshotIndex = install.index, install.index, install.index
	n.log = []core.RaftLogEntry{{Index: install.index, Term: install.term}}
	if err := n.persistHardStateLocked(); err != nil {
		n.currentTerm, n.votedFor, n.commitIndex, n.lastApplied, n.logBase, n.logBaseTerm = old.CurrentTerm, old.VotedFor, old.CommitIndex, old.LastApplied, old.LogBase, old.LogBaseTerm
		n.mu.Unlock()
		n.incomingSnapshot = nil
		return &core.InstallSnapshotResponse{Term: term, Success: false}
	}
	n.mu.Unlock()
	if n.storage != nil {
		if first, _ := n.storage.FirstIndex(); first > 0 && first <= install.index {
			_ = n.storage.DeleteRange(first, install.index)
		}
	}
	n.incomingSnapshot = nil
	return &core.InstallSnapshotResponse{Term: term, Success: true}
}

// handleHeartbeat processes heartbeat RPC
func (n *Node) handleHeartbeat(req *core.HeartbeatRequest) *core.HeartbeatResponse {
	n.mu.Lock()
	defer n.mu.Unlock()
	if req.Term >= n.currentTerm {
		if req.Term > n.currentTerm {
			if err := n.becomeFollower(req.Term); err != nil {
				n.logger.Error("Failed to persist heartbeat term", "error", err)
				return &core.HeartbeatResponse{NodeID: n.nodeID, Term: n.currentTerm, LeaderID: n.leaderID, Timestamp: time.Now().UnixMilli()}
			}
		}
		n.lastContact = time.Now()
		n.leaderID = req.LeaderID
		if n.state.Load().(core.RaftState) != core.StateFollower {
			n.state.Store(core.StateFollower)
		}
	}
	n.stats.HeartbeatCount++
	return &core.HeartbeatResponse{NodeID: n.nodeID, Term: n.currentTerm, IsLeader: n.state.Load().(core.RaftState) == core.StateLeader, LeaderID: n.leaderID, Timestamp: time.Now().UnixMilli()}
}

// handleAppendEntriesResponse processes AppendEntries response.
// Multiple replication RPCs to a peer may be in flight at once, so responses
// must only advance progress or reject the probe that is still current.
func (n *Node) sendSnapshot(peer *Peer) {
	if n.snapshot == nil || n.transport == nil {
		return
	}
	metas, err := n.snapshot.List()
	if err != nil || len(metas) == 0 {
		return
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Index > metas[j].Index })
	meta := metas[0]
	source, err := n.snapshot.Open(meta.ID)
	if err != nil {
		return
	}
	data, err := io.ReadAll(source)
	_ = source.Close()
	if err != nil {
		return
	}
	n.mu.RLock()
	term, leaderID := n.currentTerm, n.nodeID
	n.mu.RUnlock()
	for offset := 0; offset <= len(data); offset += snapshotChunkSize {
		end := offset + snapshotChunkSize
		if end > len(data) {
			end = len(data)
		}
		done := end == len(data)
		resp, err := n.transport.SendInstallSnapshot(peer.ID, &core.InstallSnapshotRequest{Term: term, LeaderID: leaderID, LastIncludedIndex: meta.Index, LastIncludedTerm: meta.Term, Offset: uint64(offset), Data: data[offset:end], Done: done})
		if err != nil || !resp.Success {
			return
		}
		if done {
			break
		}
	}
	n.mu.Lock()
	if n.state.Load().(core.RaftState) == core.StateLeader && term == n.currentTerm {
		n.matchIndex[peer.ID] = meta.Index
		n.nextIndex[peer.ID] = meta.Index + 1
		peer.matchIndex = meta.Index
		peer.nextIndex = meta.Index + 1
	}
	n.mu.Unlock()
}

func (n *Node) handleAppendEntriesResponse(peer *Peer, req *core.AppendEntriesRequest, resp *core.AppendEntriesResponse) {
	if peer == nil || req == nil || resp == nil {
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	if n.state.Load().(core.RaftState) != core.StateLeader {
		return
	}

	if resp.Term > n.currentTerm {
		if err := n.becomeFollower(resp.Term); err != nil {
			n.logger.Error("Failed to persist higher term", "error", err)
		}
		return
	}

	// A response from an earlier term belongs to an earlier leadership epoch.
	// It must not mutate the replication progress initialized for this term.
	if req.Term != n.currentTerm || resp.Term != n.currentTerm {
		return
	}

	currentMatch := n.matchIndex[peer.ID]
	currentNext := n.nextIndex[peer.ID]

	if resp.Success {
		matched := req.PrevLogIndex + uint64(len(req.Entries))
		if matched <= currentMatch {
			return
		}

		// Successful responses are monotonic. In particular, a delayed success
		// for an older, shorter request cannot move either cursor backwards.
		n.matchIndex[peer.ID] = matched
		if next := matched + 1; next > currentNext {
			n.nextIndex[peer.ID] = next
		}
		peer.matchIndex = n.matchIndex[peer.ID]
		peer.nextIndex = n.nextIndex[peer.ID]
		n.checkCommit()
		return
	}

	// A rejection is useful only for the probe at current nextIndex. A newer
	// response may already have advanced or backed off this peer. Also, never
	// back up across an index the peer has already acknowledged successfully.
	if currentNext == 0 || req.PrevLogIndex != currentNext-1 || req.PrevLogIndex <= currentMatch {
		return
	}

	// Decrement nextIndex and retry.
	next := req.PrevLogIndex
	if resp.ConflictTerm > 0 {
		// Optimization: skip to after conflict term.
		next = resp.ConflictIndex
	}
	if next > 1 {
		next--
	}

	// Rejections may only back off progress, and matchIndex is a floor: a
	// follower cannot reject an index it already confirmed in this term.
	minNext := currentMatch + 1
	if next < minNext {
		next = minNext
	}
	if next >= currentNext {
		return
	}

	n.nextIndex[peer.ID] = next
	peer.nextIndex = next
}

// checkCommit updates commitIndex if majority has replicated
// Caller must hold n.mu.Lock()
func (n *Node) checkCommit() {
	if n.state.Load().(core.RaftState) != core.StateLeader {
		return
	}
	for index := n.lastLogIndexLocked(); index > n.commitIndex; index-- {
		entry, ok := n.logEntry(index)
		if !ok || entry.Term != n.currentTerm {
			continue
		}
		committed := n.checkStandardCommit(index)
		if entry.Type == core.LogMembershipChange {
			committed = n.checkJointConsensusCommit(index)
		}
		if !committed {
			continue
		}
		old := n.commitIndex
		n.commitIndex = index
		if err := n.persistHardStateLocked(); err != nil {
			n.commitIndex = old
			n.logger.Error("Failed to persist leader commit index", "error", err)
			return
		}
		n.commitCh <- index
		return
	}
}

// checkStandardCommit checks if an entry is committed under standard rules
func (n *Node) checkStandardCommit(index uint64) bool {
	count := 1 // Leader has it

	n.membership.mu.RLock()
	config := n.membership.config
	n.membership.mu.RUnlock()

	n.peerMu.RLock()
	for _, nodeID := range config {
		if nodeID == n.nodeID {
			continue
		}
		if p, ok := n.peers[nodeID]; ok && n.matchIndex[p.ID] >= index {
			count++
		}
	}
	n.peerMu.RUnlock()

	return count > len(config)/2
}

// checkJointConsensusCommit checks if an entry is committed under joint consensus
// During joint consensus, an entry needs majority in BOTH old and new configs
func (n *Node) checkJointConsensusCommit(index uint64) bool {
	n.membership.mu.RLock()
	oldConfig := n.membership.oldConfig
	newConfig := n.membership.newConfig
	jointState := n.membership.jointState
	n.membership.mu.RUnlock()

	// If not in joint consensus, use standard commit rules
	if !jointState {
		return n.checkStandardCommit(index)
	}

	// Check old configuration
	oldCount := 0
	if containsString(oldConfig, n.nodeID) {
		oldCount = 1 // Leader is in old config
	}

	n.peerMu.RLock()
	for _, nodeID := range oldConfig {
		if nodeID == n.nodeID {
			continue
		}
		if p, ok := n.peers[nodeID]; ok && n.matchIndex[p.ID] >= index {
			oldCount++
		}
	}

	// Check new configuration
	newCount := 0
	if containsString(newConfig, n.nodeID) {
		newCount = 1 // Leader is in new config
	}

	for _, nodeID := range newConfig {
		if nodeID == n.nodeID {
			continue
		}
		if p, ok := n.peers[nodeID]; ok && n.matchIndex[p.ID] >= index {
			newCount++
		}
	}
	n.peerMu.RUnlock()

	// Both configurations must have majority
	return oldCount > len(oldConfig)/2 && newCount > len(newConfig)/2
}

// containsString checks if a string is in a slice
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// processCommitted applies committed entries to FSM.
// lastApplied is advanced only after the entry has been applied successfully.
func (n *Node) processCommitted(commitIndex uint64) {
	for {
		n.mu.RLock()
		if n.lastApplied >= commitIndex {
			n.mu.RUnlock()
			return
		}
		nextIndex := n.lastApplied + 1
		entry, ok := n.logEntry(nextIndex)
		n.mu.RUnlock()
		if !ok {
			n.logger.Error("Committed log entry unavailable", "index", nextIndex, "base", n.logBase)
			return
		}
		var applyErr error
		if entry.Type == core.LogMembershipChange {
			var change core.MembershipChange
			if err := json.Unmarshal(entry.Data, &change); err != nil {
				applyErr = fmt.Errorf("decode membership change at index %d: %w", entry.Index, err)
			} else {
				n.applyMembershipChange(change, entry.Index)
			}
		} else if result := n.fsm.Apply(&entry); result != nil {
			if err, ok := result.(error); ok {
				applyErr = err
			} else {
				applyErr = fmt.Errorf("FSM apply returned unexpected result at index %d: %v", entry.Index, result)
			}
		}
		if applyErr != nil {
			n.logger.Error("Failed to apply committed entry", "index", entry.Index, "term", entry.Term, "error", applyErr)
			n.notifyApply(entry.Index, entry.Term, applyErr)
			return
		}
		n.mu.Lock()
		old := n.lastApplied
		n.lastApplied = nextIndex
		if err := n.persistHardStateLocked(); err != nil {
			n.lastApplied = old
			n.mu.Unlock()
			n.notifyApply(entry.Index, entry.Term, err)
			return
		}
		n.mu.Unlock()
		n.notifyApply(entry.Index, entry.Term, nil)
	}
}

// handleApply appends a command to the log
func (n *Node) handleApply(future *applyFuture) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.state.Load().(core.RaftState) != core.StateLeader {
		future.err = &core.RaftError{Code: core.ErrNotLeader, Message: "not leader", NodeID: n.leaderID}
		close(future.done)
		return
	}
	cmdData, err := json.Marshal(future.command)
	if err != nil {
		future.err = fmt.Errorf("failed to serialize command: %w", err)
		close(future.done)
		return
	}
	entry := core.RaftLogEntry{Term: n.currentTerm, Type: core.LogCommand, Data: cmdData}
	if err := n.appendEntry(entry); err != nil {
		future.err = fmt.Errorf("persist command: %w", err)
		close(future.done)
		return
	}
	entry = n.log[len(n.log)-1]
	future.index, future.term = entry.Index, entry.Term
	n.applyWaiters.Store(entry.Index, future)
}

// Helper functions

func (n *Node) lastLogIndexLocked() uint64 {
	if len(n.log) == 0 {
		return n.logBase
	}
	return n.log[len(n.log)-1].Index
}

func (n *Node) logPosition(index uint64) (int, bool) {
	if index < n.logBase {
		return 0, false
	}
	pos := index - n.logBase
	if pos >= uint64(len(n.log)) {
		return 0, false
	}
	return int(pos), true
}

func (n *Node) logEntry(index uint64) (core.RaftLogEntry, bool) {
	pos, ok := n.logPosition(index)
	if !ok {
		return core.RaftLogEntry{}, false
	}
	return n.log[pos], true
}

func (n *Node) getLogTerm(index uint64) uint64 {
	if index == n.logBase {
		return n.logBaseTerm
	}
	entry, ok := n.logEntry(index)
	if !ok {
		return 0
	}
	return entry.Term
}

func (n *Node) getEntriesAfter(start uint64, maxCount int) []core.RaftLogEntry {
	if maxCount <= 0 {
		return []core.RaftLogEntry{}
	}
	if start <= n.logBase {
		start = n.logBase + 1
	}
	pos, ok := n.logPosition(start)
	if !ok {
		if len(n.log) <= 1 {
			return []core.RaftLogEntry{}
		}
		return nil
	}
	end := pos + maxCount
	if end > len(n.log) {
		end = len(n.log)
	}
	out := make([]core.RaftLogEntry, end-pos)
	copy(out, n.log[pos:end])
	return out
}

func (n *Node) appendEntry(entry core.RaftLogEntry) error {
	entry.Index = n.lastLogIndexLocked() + 1
	if n.storage != nil {
		if err := n.storage.StoreLog(&entry); err != nil {
			return err
		}
	}
	n.log = append(n.log, entry)
	return nil
}

func (n *Node) persistHardStateLocked() error {
	if n.stable == nil {
		return nil
	}
	data, err := json.Marshal(hardState{CurrentTerm: n.currentTerm, VotedFor: n.votedFor, CommitIndex: n.commitIndex, LastApplied: n.lastApplied, LogBase: n.logBase, LogBaseTerm: n.logBaseTerm})
	if err != nil {
		return err
	}
	return n.stable.Set(stableHardState, data)
}

func (n *Node) restoreHardState() error {
	if n.stable == nil {
		return nil
	}
	data, err := n.stable.Get(stableHardState)
	if err != nil {
		var notFound *core.NotFoundError
		if errors.As(err, &notFound) {
			return nil
		}
		return err
	}
	var state hardState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}
	n.currentTerm, n.votedFor = state.CurrentTerm, state.VotedFor
	n.commitIndex, n.lastApplied = state.CommitIndex, state.LastApplied
	n.logBase, n.logBaseTerm = state.LogBase, state.LogBaseTerm
	return nil
}

func (n *Node) newElectionTimer() *time.Timer {
	// Randomize timeout between 1x and 2x election timeout. G404
	// suppress: this is for thundering-herd avoidance, not for
	// security — math/rand/v2 is thread-safe, lock-free, and the
	// best source of monotonic-clock-aligned jitter.
	d := n.electionTimeout + time.Duration(rand.Int64N(int64(n.electionTimeout))) // #nosec G404 -- see comment
	return time.NewTimer(d)
}

func (n *Node) restoreLatestSnapshot() error {
	if n.snapshot == nil {
		return nil
	}
	metas, err := n.snapshot.List()
	if err != nil {
		var notFound *core.NotFoundError
		if errors.As(err, &notFound) {
			return nil
		}
		return err
	}
	if len(metas) == 0 {
		return nil
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Index > metas[j].Index })
	meta := metas[0]
	if meta.Index < n.logBase {
		return nil
	}
	source, err := n.snapshot.Open(meta.ID)
	if err != nil {
		return err
	}
	defer source.Close()
	payload, err := io.ReadAll(source)
	if err != nil {
		return err
	}
	if err := n.fsm.Restore(payload); err != nil {
		return err
	}
	n.logBase, n.logBaseTerm = meta.Index, meta.Term
	n.lastSnapshotIndex = meta.Index
	if n.commitIndex < meta.Index {
		n.commitIndex = meta.Index
	}
	if n.lastApplied < meta.Index {
		n.lastApplied = meta.Index
	}
	n.log = []core.RaftLogEntry{{Index: meta.Index, Term: meta.Term}}
	return nil
}

func (n *Node) restoreLog() error {
	if n.storage == nil {
		return fmt.Errorf("log store not available")
	}
	firstIdx, err := n.storage.FirstIndex()
	if err != nil {
		return fmt.Errorf("failed to get first log index: %w", err)
	}
	lastIdx, err := n.storage.LastIndex()
	if err != nil {
		return fmt.Errorf("failed to get last log index: %w", err)
	}
	if lastIdx < firstIdx || firstIdx == 0 {
		return nil
	}
	if firstIdx > n.logBase+1 {
		return fmt.Errorf("persistent log starts at %d after base %d", firstIdx, n.logBase)
	}
	n.log = []core.RaftLogEntry{{Index: n.logBase, Term: n.logBaseTerm}}
	start := firstIdx
	if start <= n.logBase {
		start = n.logBase + 1
	}
	for i := start; i <= lastIdx; i++ {
		var entry core.RaftLogEntry
		if err := n.storage.GetLog(i, &entry); err != nil {
			return fmt.Errorf("failed to restore log entry %d: %w", i, err)
		}
		if entry.Index != i {
			return fmt.Errorf("log entry %d restored with index %d", i, entry.Index)
		}
		n.log = append(n.log, entry)
	}
	return nil
}

func (n *Node) notifyApply(index, term uint64, err error) {
	if future, ok := n.applyWaiters.Load(index); ok {
		f := future.(*applyFuture)
		f.term, f.err = term, err
		close(f.done)
		n.applyWaiters.Delete(index)
	}
}

// maybeTakeSnapshot checks if a snapshot should be taken and creates one
func (n *Node) maybeTakeSnapshot() {
	if n.snapshot == nil || n.snapshotThreshold <= 0 || n.snapshotInProgress.Load() {
		return
	}
	n.mu.RLock()
	logSize := int(n.lastLogIndexLocked() - n.logBase)
	snapIndex := n.commitIndex
	snapTerm := n.getLogTerm(snapIndex)
	n.mu.RUnlock()
	if logSize < n.snapshotThreshold || snapIndex <= n.logBase {
		return
	}
	if !n.snapshotInProgress.CompareAndSwap(false, true) {
		return
	}
	defer n.snapshotInProgress.Store(false)
	command, err := n.fsm.Snapshot()
	if err != nil {
		n.logger.Error("Failed to snapshot FSM", "error", err)
		return
	}
	sink, err := n.snapshot.Create(1, snapIndex, snapTerm, nil)
	if err != nil {
		n.logger.Error("Failed to create snapshot sink", "error", err)
		return
	}
	if _, err := sink.Write(command.Value); err != nil {
		_ = sink.Cancel()
		n.logger.Error("Failed to write snapshot", "error", err)
		return
	}
	if err := sink.Close(); err != nil {
		n.logger.Error("Failed to close snapshot", "error", err)
		return
	}
	n.mu.Lock()
	n.lastSnapshotIndex = snapIndex
	n.mu.Unlock()
	n.compactLog(snapIndex)
}

// compactLog removes log entries that are included in the snapshot
func (n *Node) compactLog(snapshotIndex uint64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if snapshotIndex <= n.logBase || snapshotIndex > n.lastLogIndexLocked() {
		return
	}
	snapshotTerm := n.getLogTerm(snapshotIndex)
	trailing := uint64(n.config.TrailingLogs)
	retainFrom := snapshotIndex + 1
	if trailing > 0 && snapshotIndex > trailing {
		retainFrom = snapshotIndex - trailing + 1
	} else if trailing > 0 {
		retainFrom = n.logBase + 1
	}
	retained := []core.RaftLogEntry{{Index: snapshotIndex, Term: snapshotTerm}}
	for index := retainFrom; index <= n.lastLogIndexLocked(); index++ {
		if entry, ok := n.logEntry(index); ok && index > snapshotIndex {
			retained = append(retained, entry)
		}
	}
	oldBase := n.logBase
	n.logBase, n.logBaseTerm, n.log = snapshotIndex, snapshotTerm, retained
	if err := n.persistHardStateLocked(); err != nil {
		n.logger.Error("Failed to persist compacted log base", "error", err)
		return
	}
	if n.storage != nil && oldBase+1 <= snapshotIndex {
		if err := n.storage.DeleteRange(oldBase+1, snapshotIndex); err != nil {
			n.logger.Error("Failed to compact persistent log", "error", err)
		}
	}
}
