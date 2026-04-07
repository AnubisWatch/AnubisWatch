package raft

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Node represents a Raft consensus node
// The Pharaoh's throne in the Necropolis
type Node struct {
	// Configuration
	config        core.RaftConfig
	nodeID        string
	bindAddr      string
	advertiseAddr string
	region        string

	// State machine (protected by mu)
	mu          sync.RWMutex
	state       core.RaftState
	currentTerm uint64
	preVoteTerm uint64 // Term for pre-vote (separate from currentTerm)
	votedFor    string
	preVotedFor string // Candidate we pre-voted for
	log         []core.RaftLogEntry
	commitIndex uint64
	lastApplied uint64

	// Volatile state for leaders (reset on election)
	nextIndex  map[string]uint64
	matchIndex map[string]uint64

	// Peers
	peers  map[string]*Peer
	peerMu sync.RWMutex

	// Storage
	storage  LogStore
	snapshot SnapshotStore
	fsm      FSM

	// Snapshot state
	snapshotThreshold  int
	lastSnapshotIndex  uint64
	snapshotInProgress atomic.Bool

	// Networking
	transport Transport

	// Channels for internal communication
	applyCh    chan *applyFuture
	commitCh   chan uint64
	rpcCh      chan *rpcWrapper
	shutdownCh chan struct{}
	doneCh     chan struct{}

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
	stats core.ClusterStats
}

// Peer represents a remote Raft node
type Peer struct {
	ID           string
	Address      string
	Region       string
	Role         core.RaftRole
	conn         net.Conn
	tlsConn      *tls.Conn
	nextIndex    uint64
	matchIndex   uint64
	lastContact  time.Time
	heartbeatRTT time.Duration
	inflight     atomic.Uint64
	mu           sync.RWMutex
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
	DeleteRange(min, max uint64) error
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

// NewNode creates a new Raft node
func NewNode(config core.RaftConfig, storage LogStore, snapshot SnapshotStore, fsm FSM, logger *slog.Logger) (*Node, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	n := &Node{
		config:            config,
		nodeID:            config.NodeID,
		bindAddr:          config.BindAddr,
		advertiseAddr:     config.AdvertiseAddr,
		region:            "default",
		state:             core.StateFollower,
		currentTerm:       0,
		votedFor:          "",
		log:               make([]core.RaftLogEntry, 1), // Index 0 is unused
		commitIndex:       0,
		lastApplied:       0,
		nextIndex:         make(map[string]uint64),
		matchIndex:        make(map[string]uint64),
		peers:             make(map[string]*Peer),
		storage:           storage,
		snapshot:          snapshot,
		fsm:               fsm,
		applyCh:           make(chan *applyFuture, 256),
		commitCh:          make(chan uint64, 16),
		rpcCh:             make(chan *rpcWrapper, 256),
		shutdownCh:        make(chan struct{}),
		doneCh:            make(chan struct{}),
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

	return n, nil
}

// SetTransport sets the transport for the node
func (n *Node) SetTransport(transport Transport) {
	n.transport = transport
}

// Start starts the Raft node
func (n *Node) Start() error {
	if n.running.Load() {
		return fmt.Errorf("node already running")
	}

	// Restore from storage
	if err := n.restoreLog(); err != nil {
		return fmt.Errorf("failed to restore log: %w", err)
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
		n.transport.Stop()
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
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.state
}

// IsLeader returns true if this node is the leader
func (n *Node) IsLeader() bool {
	return n.State() == core.StateLeader
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

// Peers returns a copy of the peers map
func (n *Node) Peers() map[string]*Peer {
	n.peerMu.RLock()
	defer n.peerMu.RUnlock()
	copy := make(map[string]*Peer, len(n.peers))
	for k, v := range n.peers {
		copy[k] = v
	}
	return copy
}

// Done returns the shutdown channel
func (n *Node) Done() <-chan struct{} {
	return n.doneCh
}

// Shutdown initiates graceful shutdown (alias for Stop)
func (n *Node) Shutdown() {
	n.Stop()
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

// AddPeer adds a peer to the cluster
func (n *Node) AddPeer(peer core.RaftPeer) error {
	n.peerMu.Lock()
	defer n.peerMu.Unlock()

	if peer.ID == n.nodeID {
		return fmt.Errorf("cannot add self as peer")
	}

	if _, exists := n.peers[peer.ID]; exists {
		return fmt.Errorf("peer %s already exists", peer.ID)
	}

	n.peers[peer.ID] = &Peer{
		ID:      peer.ID,
		Address: peer.Address,
		Region:  peer.Region,
		Role:    peer.Role,
	}

	// Register peer address with transport for connection pooling
	if tt, ok := n.transport.(*TCPTransport); ok {
		tt.RegisterPeer(peer.ID, peer.Address)
	}

	n.logger.Info("Peer added", "peer_id", peer.ID, "address", peer.Address)
	return nil
}

// RemovePeer removes a peer from the cluster
func (n *Node) RemovePeer(peerID string) error {
	n.peerMu.Lock()
	defer n.peerMu.Unlock()

	if peerID == n.nodeID {
		return fmt.Errorf("cannot remove self")
	}

	if _, exists := n.peers[peerID]; !exists {
		return fmt.Errorf("peer %s not found", peerID)
	}

	delete(n.peers, peerID)

	// Unregister peer from transport
	if tt, ok := n.transport.(*TCPTransport); ok {
		tt.UnregisterPeer(peerID)
	}

	n.logger.Info("Peer removed", "peer_id", peerID)
	return nil
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
		State:        n.state,
		Term:         n.currentTerm,
		LastLogIndex: uint64(len(n.log) - 1),
		LastLogTerm:  n.getLogTerm(uint64(len(n.log) - 1)),
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

	for {
		select {
		case <-n.shutdownCh:
			return

		case <-electionTimer.C:
			if n.state != core.StateLeader {
				n.startElection()
			}
			electionTimer = n.newElectionTimer()

		case <-commitTimer.C:
			if n.state == core.StateLeader {
				n.checkCommit()
			}
			commitTimer.Reset(n.commitTimeout)

		case rpc := <-n.rpcCh:
			n.handleRPC(rpc)

		case idx := <-n.commitCh:
			n.processCommitted(idx)
			n.maybeTakeSnapshot() // Check if snapshot needed after commit
		}

		// Send heartbeats if leader
		if n.state == core.StateLeader {
			n.sendHeartbeats()
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

	// Pre-vote phase: check if we would win an election
	n.state = core.StateCandidate
	preVoteTerm := n.currentTerm + 1
	lastLogIndex := uint64(len(n.log) - 1)
	lastLogTerm := n.getLogTerm(lastLogIndex)

	n.logger.Info("Starting pre-vote",
		"current_term", preVoteTerm,
		"last_log_index", lastLogIndex,
		"last_log_term", lastLogTerm)

	n.peerMu.RLock()
	peers := make([]*Peer, 0, len(n.peers))
	for _, p := range n.peers {
		if p.Role != core.RoleNonVoter {
			peers = append(peers, p)
		}
	}
	n.peerMu.RUnlock()
	n.mu.Unlock()

	// Request pre-votes from all peers
	preVotes := n.requestPreVotes(preVoteTerm, lastLogIndex, lastLogTerm, peers)

	// Check if we should proceed with real election
	n.mu.Lock()
	defer n.mu.Unlock()

	if !preVotes {
		n.logger.Info("Pre-vote failed, not starting election")
		n.state = core.StateFollower
		return
	}

	// Pre-vote succeeded, start real election
	n.state = core.StateCandidate
	n.currentTerm++
	n.votedFor = n.nodeID
	n.leaderID = ""
	n.lastContact = time.Now()

	term := n.currentTerm
	lastLogIndex = uint64(len(n.log) - 1)
	lastLogTerm = n.getLogTerm(lastLogIndex)

	n.logger.Info("Pre-vote succeeded, starting election",
		"term", term,
		"last_log_index", lastLogIndex,
		"last_log_term", lastLogTerm)

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
		n.state = core.StateFollower
	}
}

// requestPreVotes sends PreVote RPCs to all peers and returns true if majority would grant votes
func (n *Node) requestPreVotes(term, lastLogIndex, lastLogTerm uint64, peers []*Peer) bool {
	var preVotesGranted atomic.Int32
	preVotesGranted.Add(1) // Vote for self

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
					n.currentTerm = resp.Term
					n.state = core.StateFollower
					n.votedFor = ""
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
					n.currentTerm = resp.Term
					n.state = core.StateFollower
					n.votedFor = ""
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
func (n *Node) becomeLeader() {
	n.logger.Info("Became leader", "term", n.currentTerm)

	n.state = core.StateLeader
	n.leaderID = n.nodeID
	n.stats.ElectionsWon++
	n.stats.LeaderChanges++

	// Initialize leader state
	lastLogIndex := uint64(len(n.log) - 1)
	n.peerMu.RLock()
	for _, p := range n.peers {
		n.nextIndex[p.ID] = lastLogIndex + 1
		n.matchIndex[p.ID] = 0
	}
	n.peerMu.RUnlock()

	// Append no-op entry
	n.appendEntry(core.RaftLogEntry{
		Term: n.currentTerm,
		Type: core.LogNoOp,
	})

	// Send immediate heartbeats
	n.sendHeartbeats()
}

// becomeFollower transitions to follower state
func (n *Node) becomeFollower(term uint64) {
	wasLeader := n.state == core.StateLeader
	n.state = core.StateFollower
	n.currentTerm = term
	n.votedFor = ""
	n.lastContact = time.Now()

	if wasLeader {
		n.logger.Info("Stepped down as leader", "term", term)
		n.stats.ElectionsLost++
	}
}

// sendHeartbeats sends heartbeats to all peers
func (n *Node) sendHeartbeats() {
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

	// Reply false if term < currentTerm
	if req.Term < n.currentTerm {
		return &core.AppendEntriesResponse{
			Term:    n.currentTerm,
			Success: false,
		}
	}

	// If term > currentTerm, become follower
	if req.Term > n.currentTerm {
		n.becomeFollower(req.Term)
	}

	// Valid heartbeat from leader
	n.lastContact = time.Now()
	n.leaderID = req.LeaderID

	// Reply false if log doesn't contain an entry at prevLogIndex
	// whose term matches prevLogTerm
	if req.PrevLogIndex > 0 {
		if req.PrevLogIndex >= uint64(len(n.log)) {
			return &core.AppendEntriesResponse{
				Term:       n.currentTerm,
				Success:    false,
				MatchIndex: uint64(len(n.log) - 1),
			}
		}
		if n.log[req.PrevLogIndex].Term != req.PrevLogTerm {
			// Find conflict index
			conflictTerm := n.log[req.PrevLogIndex].Term
			conflictIndex := req.PrevLogIndex
			for conflictIndex > 0 && n.log[conflictIndex].Term == conflictTerm {
				conflictIndex--
			}
			return &core.AppendEntriesResponse{
				Term:          n.currentTerm,
				Success:       false,
				ConflictTerm:  conflictTerm,
				ConflictIndex: conflictIndex + 1,
			}
		}
	}

	// If an existing entry conflicts with a new one, delete the existing entry
	// and all that follow it
	for i, entry := range req.Entries {
		idx := req.PrevLogIndex + uint64(i) + 1
		if idx < uint64(len(n.log)) {
			if n.log[idx].Term != entry.Term {
				n.log = n.log[:idx]
				break
			}
		} else {
			break
		}
	}

	// Append any new entries not already in the log
	for i, entry := range req.Entries {
		idx := req.PrevLogIndex + uint64(i) + 1
		if idx >= uint64(len(n.log)) {
			n.log = append(n.log, entry)
		}
	}

	// If leaderCommit > commitIndex, set commitIndex = min(leaderCommit, index of last new entry)
	if req.LeaderCommit > n.commitIndex {
		lastNewIndex := req.PrevLogIndex + uint64(len(req.Entries))
		if req.LeaderCommit < lastNewIndex {
			n.commitIndex = req.LeaderCommit
		} else {
			n.commitIndex = lastNewIndex
		}
		n.commitCh <- n.commitIndex
	}

	return &core.AppendEntriesResponse{
		Term:       n.currentTerm,
		Success:    true,
		MatchIndex: req.PrevLogIndex + uint64(len(req.Entries)),
	}
}

// handleRequestVote processes RequestVote RPC
func (n *Node) handleRequestVote(req *core.RequestVoteRequest) *core.RequestVoteResponse {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Reply false if term < currentTerm
	if req.Term < n.currentTerm {
		return &core.RequestVoteResponse{
			Term:        n.currentTerm,
			VoteGranted: false,
			Reason:      "term too old",
		}
	}

	// If term > currentTerm, update term and become follower
	if req.Term > n.currentTerm {
		n.currentTerm = req.Term
		n.state = core.StateFollower
		n.votedFor = ""
	}

	// If votedFor is null or candidateId, and candidate's log is at
	// least as up-to-date as receiver's log, grant vote
	canVote := n.votedFor == "" || n.votedFor == req.CandidateID
	lastLogIndex := uint64(len(n.log) - 1)
	lastLogTerm := n.getLogTerm(lastLogIndex)

	logIsCurrent := req.LastLogTerm > lastLogTerm ||
		(req.LastLogTerm == lastLogTerm && req.LastLogIndex >= lastLogIndex)

	if canVote && logIsCurrent {
		n.votedFor = req.CandidateID
		n.lastContact = time.Now()

		n.logger.Debug("Voted for candidate",
			"candidate", req.CandidateID,
			"term", req.Term)

		return &core.RequestVoteResponse{
			Term:        n.currentTerm,
			VoteGranted: true,
		}
	}

	reason := "already voted"
	if !logIsCurrent {
		reason = "log not current"
	}

	return &core.RequestVoteResponse{
		Term:        n.currentTerm,
		VoteGranted: false,
		Reason:      reason,
	}
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

	// Check if candidate's log is at least as up-to-date as ours
	lastLogIndex := uint64(len(n.log) - 1)
	lastLogTerm := n.getLogTerm(lastLogIndex)

	logIsCurrent := req.LastLogTerm > lastLogTerm ||
		(req.LastLogTerm == lastLogTerm && req.LastLogIndex >= lastLogIndex)

	if !logIsCurrent {
		n.logger.Debug("PreVote denied: log not current",
			"candidate_log_term", req.LastLogTerm,
			"candidate_log_index", req.LastLogIndex,
			"my_log_term", lastLogTerm,
			"my_log_index", lastLogIndex)

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
	defer n.mu.Unlock()

	if req.Term < n.currentTerm {
		return &core.InstallSnapshotResponse{
			Term:    n.currentTerm,
			Success: false,
		}
	}

	if req.Term > n.currentTerm {
		n.becomeFollower(req.Term)
	}

	n.lastContact = time.Now()
	n.leaderID = req.LeaderID

	// Snapshot restore: Reset log and update state based on snapshot metadata
	// For now just acknowledge

	return &core.InstallSnapshotResponse{
		Term:    n.currentTerm,
		Success: true,
	}
}

// handleHeartbeat processes heartbeat RPC
func (n *Node) handleHeartbeat(req *core.HeartbeatRequest) *core.HeartbeatResponse {
	n.mu.Lock()
	defer n.mu.Unlock()

	if req.Term >= n.currentTerm {
		if req.Term > n.currentTerm {
			n.currentTerm = req.Term
		}
		n.lastContact = time.Now()
		n.leaderID = req.LeaderID

		if n.state != core.StateFollower {
			n.becomeFollower(req.Term)
		}
	}

	n.stats.HeartbeatCount++

	return &core.HeartbeatResponse{
		NodeID:    n.nodeID,
		Term:      n.currentTerm,
		IsLeader:  n.state == core.StateLeader,
		LeaderID:  n.leaderID,
		Timestamp: time.Now().UnixMilli(),
	}
}

// handleAppendEntriesResponse processes AppendEntries response
func (n *Node) handleAppendEntriesResponse(peer *Peer, req *core.AppendEntriesRequest, resp *core.AppendEntriesResponse) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.state != core.StateLeader {
		return
	}

	if resp.Term > n.currentTerm {
		n.becomeFollower(resp.Term)
		return
	}

	if resp.Success {
		if len(req.Entries) > 0 {
			n.matchIndex[peer.ID] = req.PrevLogIndex + uint64(len(req.Entries))
			n.nextIndex[peer.ID] = n.matchIndex[peer.ID] + 1
			peer.matchIndex = n.matchIndex[peer.ID]
			peer.nextIndex = n.nextIndex[peer.ID]
			n.checkCommit()
		}
	} else {
		// Decrement nextIndex and retry
		if resp.ConflictTerm > 0 {
			// Optimization: skip to after conflict term
			n.nextIndex[peer.ID] = resp.ConflictIndex
		} else {
			n.nextIndex[peer.ID] = req.PrevLogIndex
		}
		if n.nextIndex[peer.ID] > 1 {
			n.nextIndex[peer.ID]--
		}
		peer.nextIndex = n.nextIndex[peer.ID]
	}
}

// checkCommit updates commitIndex if majority has replicated
func (n *Node) checkCommit() {
	if n.state != core.StateLeader {
		return
	}

	lastLogIndex := uint64(len(n.log) - 1)

	// Find the highest index that majority has replicated
	for N := lastLogIndex; N > n.commitIndex; N-- {
		if n.log[N].Term != n.currentTerm {
			continue
		}

		count := 1 // Leader has it
		n.peerMu.RLock()
		for _, p := range n.peers {
			if n.matchIndex[p.ID] >= N {
				count++
			}
		}
		n.peerMu.RUnlock()

		if count > (len(n.peers)+1)/2 {
			n.commitIndex = N
			n.commitCh <- n.commitIndex
			break
		}
	}
}

// processCommitted applies committed entries to FSM
func (n *Node) processCommitted(commitIndex uint64) {
	for {
		n.mu.Lock()
		if n.lastApplied >= commitIndex {
			n.mu.Unlock()
			break
		}
		n.lastApplied++
		entry := n.log[n.lastApplied]
		n.mu.Unlock()

		// Apply to FSM
		n.fsm.Apply(&entry)

		// Notify waiters
		n.notifyApply(entry.Index, entry.Term, nil)
	}
}

// handleApply appends a command to the log
func (n *Node) handleApply(future *applyFuture) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.state != core.StateLeader {
		future.err = &core.RaftError{Code: core.ErrNotLeader, Message: "not leader"}
		close(future.done)
		return
	}

	// Serialize command
	cmdData, err := json.Marshal(future.command)
	if err != nil {
		future.err = fmt.Errorf("failed to serialize command: %w", err)
		close(future.done)
		return
	}

	entry := core.RaftLogEntry{
		Index: uint64(len(n.log)),
		Term:  n.currentTerm,
		Type:  core.LogCommand,
		Data:  cmdData,
	}

	n.log = append(n.log, entry)
	future.index = entry.Index
	future.term = entry.Term

	// Future will be signaled when entry is committed
	applyWaiters.Store(entry.Index, future)
}

// Helper functions

func (n *Node) getLogTerm(index uint64) uint64 {
	if index == 0 || index >= uint64(len(n.log)) {
		return 0
	}
	return n.log[index].Term
}

func (n *Node) getEntriesAfter(start uint64, max int) []core.RaftLogEntry {
	if start >= uint64(len(n.log)) {
		return nil
	}
	end := start + uint64(max)
	if end > uint64(len(n.log)) {
		end = uint64(len(n.log))
	}
	return n.log[start:end]
}

func (n *Node) appendEntry(entry core.RaftLogEntry) {
	entry.Index = uint64(len(n.log))
	n.log = append(n.log, entry)
}

func (n *Node) newElectionTimer() *time.Timer {
	// Randomize timeout between 1x and 2x election timeout
	d := n.electionTimeout + time.Duration(rand.Int63n(int64(n.electionTimeout)))
	return time.NewTimer(d)
}

func (n *Node) restoreLog() error {
	// Restore log entries from persistent storage (LogStore interface)
	return nil
}

func (n *Node) notifyApply(index, term uint64, err error) {
	if future, ok := applyWaiters.Load(index); ok {
		f := future.(*applyFuture)
		f.term = term
		f.err = err
		close(f.done)
		applyWaiters.Delete(index)
	}
}

// maybeTakeSnapshot checks if a snapshot should be taken and creates one
func (n *Node) maybeTakeSnapshot() {
	if n.snapshot == nil || n.snapshotThreshold <= 0 {
		return
	}

	// Check if snapshot is in progress
	if n.snapshotInProgress.Load() {
		return
	}

	n.mu.RLock()
	logSize := len(n.log) - 1 // Exclude index 0
	commitIndex := n.commitIndex
	n.mu.RUnlock()

	// Check if log exceeds threshold
	if logSize < n.snapshotThreshold {
		return
	}

	// Try to claim snapshot creation
	if !n.snapshotInProgress.CompareAndSwap(false, true) {
		return // Another goroutine took it
	}
	defer n.snapshotInProgress.Store(false)

	n.logger.Info("Taking snapshot", "log_size", logSize, "commit_index", commitIndex)

	// Create snapshot
	snapIndex := commitIndex
	snapTerm := n.getLogTerm(snapIndex)

	// Get current configuration
	configData, _ := json.Marshal(n.peers)

	sink, err := n.snapshot.Create(1, snapIndex, snapTerm, configData)
	if err != nil {
		n.logger.Error("Failed to create snapshot sink", "err", err)
		return
	}
	defer sink.Close()

	// Write log entries up to commitIndex to snapshot
	n.mu.RLock()
	var entries []byte
	// Safe conversion: snapIndex is bounded by log length which is int
	snapIdx := int(snapIndex)
	if snapIdx > len(n.log) {
		snapIdx = len(n.log)
	}
	for i := 1; i <= snapIdx && i < len(n.log); i++ {
		entryData, _ := json.Marshal(n.log[i])
		entries = append(entries, entryData...)
		entries = append(entries, '\n')
	}
	n.mu.RUnlock()

	if len(entries) > 0 {
		if _, err := sink.Write(entries); err != nil {
			n.logger.Error("Failed to write snapshot", "err", err)
			sink.Cancel()
			return
		}
	}

	n.logger.Info("Snapshot created", "index", snapIndex, "term", snapTerm)

	// Update last snapshot index
	n.mu.Lock()
	n.lastSnapshotIndex = snapIndex
	n.mu.Unlock()

	// Compact log - remove entries before snapshot
	n.compactLog(snapIndex)
}

// compactLog removes log entries that are included in the snapshot
func (n *Node) compactLog(snapshotIndex uint64) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if snapshotIndex <= 0 || snapshotIndex >= uint64(len(n.log)) {
		return
	}

	// Keep trailing logs as configured
	trailingLogs := n.config.TrailingLogs
	if trailingLogs <= 0 {
		trailingLogs = 1024 // Default
	}

	// Calculate new start - keep some trailing logs
	newStart := snapshotIndex - uint64(trailingLogs)
	if newStart < 1 {
		newStart = 1
	}

	// Create new log with retained entries
	newLog := make([]core.RaftLogEntry, newStart)
	copy(newLog, n.log[:newStart])

	// Append entries from snapshotIndex onwards
	for i := snapshotIndex; i < uint64(len(n.log)); i++ {
		newLog = append(newLog, n.log[i])
	}

	oldLen := len(n.log)
	n.log = newLog

	n.logger.Info("Log compacted",
		"old_size", oldLen,
		"new_size", len(n.log),
		"removed", oldLen-len(n.log))
}

// applyWaiters stores futures waiting for commit
type applyWaitersMap struct {
	sync.Map
}

var applyWaiters applyWaitersMap
