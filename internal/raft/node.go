package raft

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

const (
	stableHardState = "hard_state"
	stablePeers     = "membership_peers"
	// stableRemoved marks a node removed from the cluster, so a restart does
	// not bring it back campaigning against the cluster it left.
	stableRemoved     = "membership_removed"
	snapshotChunkSize = 1 << 20
	// applyRetryInitialBackoff is the delay before the first retry of a
	// committed apply that failed. It doubles while the failure persists, up
	// to applyRetryMaxBackoff.
	applyRetryInitialBackoff = 100 * time.Millisecond
	// applyRetryMaxBackoff caps the retry delay, so a node keeps re-trying a
	// recovered store without spinning on a permanently broken one.
	applyRetryMaxBackoff = 5 * time.Second
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
	// appliedDirty is set while lastApplied is ahead of the persisted hard
	// state because writing it failed (see persistApplied).
	appliedDirty bool
	// tableScopeFloor is the applied index whose entries the FSM's per-table
	// tracking does not cover: those applied before this process started or
	// replaced by an installed snapshot (see tableReadIndex).
	tableScopeFloor uint64
	// restoring marks a snapshot restore in flight. The node must not
	// campaign while its state is being replaced.
	restoring bool

	// Volatile state for leaders (reset on election)
	nextIndex  map[string]uint64
	matchIndex map[string]uint64

	// Peers
	peers  map[string]*Peer
	peerMu sync.RWMutex

	// Membership configuration (single-server changes; see membership.go).
	// base is the latest committed configuration; latest folds every
	// membership entry in the log over it and is the one in effect.
	membership struct {
		mu          sync.RWMutex
		base        []core.RaftPeer // committed configuration, this node included
		latest      []core.RaftPeer // effective configuration, this node included
		latestIndex uint64          // index of the entry defining latest (0 = base)
	}
	// selfNonVoter is set when this node is not a voter of the effective
	// configuration (a non-voter, removed, or not added yet).
	selfNonVoter atomic.Bool
	// leaderNoopIndex is the index of the no-op this node appended when it
	// became leader, and leaderSince when that happened. Guarded by mu.
	leaderNoopIndex uint64
	leaderSince     time.Time
	// catchUpTarget is the leader commit index reported when contact with the
	// current leader was (re)established. Until lastApplied reaches it this
	// follower's state is stale and HasQuorumContact stays false. Guarded by mu.
	catchUpTarget uint64
	// selfAbsent is set while this node is not in its effective
	// configuration (removed, or not added yet).
	selfAbsent atomic.Bool

	// Storage
	storage  LogStore
	stable   StableStore
	snapshot SnapshotStore
	fsm      FSM
	// applyMu serializes committed-entry application with snapshot replacement
	// and capture. Do not hold mu while waiting for it: observers may read Node.
	applyMu sync.Mutex

	// Snapshot state
	snapshotThreshold  int
	lastSnapshotIndex  uint64
	snapshotInProgress atomic.Bool
	snapshotMu         sync.Mutex
	incomingSnapshot   *snapshotInstall

	// Networking
	transport Transport
	// Coalesce replication triggers while a peer's RPC is in flight. Key by
	// peer identity so an old member cannot reserve its replacement's slot.
	replicationInFlight sync.Map // *Peer -> struct{}

	// Channels for internal communication
	applyCh    chan *applyFuture
	commitCh   chan uint64
	rpcCh      chan *rpcWrapper
	shutdownCh chan struct{}
	doneCh     chan struct{}
	loops      sync.WaitGroup // protocol and application loops own doneCh together
	// applyRetryCh requests a re-attempt of a committed apply that failed.
	// Buffered (cap 1) so the apply path never blocks and repeated failures
	// coalesce into one pending retry. See applyRetryLoop.
	applyRetryCh chan struct{}
	// electionResetCh signals `run()` to reset the follower election
	// timer when a valid AppendEntries arrives. Buffered so the sender
	// never blocks — multiple resets before `run()` drains coalesce.
	electionResetCh chan struct{}

	// Timing
	electionTimeout  time.Duration
	heartbeatTimeout time.Duration
	commitTimeout    time.Duration
	// applyRetryBackoff is the delay before the next retry of a failed
	// committed apply. It grows while the failure persists and is reset once
	// a pass applies every committed entry. Guarded by applyRetryMu.
	applyRetryMu      sync.Mutex
	applyRetryBackoff time.Duration

	// Leader tracking
	leaderID    string
	lastContact time.Time

	// bg tracks replication goroutines so Stop can wait for them; bgClosed
	// (under bgMu) refuses new ones once Stop has begun.
	bgMu     sync.Mutex
	bgClosed bool
	bg       sync.WaitGroup

	// Control
	stopMu   sync.Mutex
	running  atomic.Bool
	shutdown atomic.Bool

	// Logger
	logger *slog.Logger

	// Stats
	stats        core.ClusterStats
	applyWaiters sync.Map

	// removed is set once a committed membership change removes this node: it
	// stops campaigning instead of disrupting the cluster it left.
	removed atomic.Bool
	// awaitingJoin is set while a node started with raft.join waits to be
	// added to a cluster (see join.go); it never campaigns meanwhile.
	awaitingJoin atomic.Bool
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
	// leaving marks a member of the committed configuration whose removal is
	// appended but not committed: it still receives the log but no longer
	// counts in any quorum.
	leaving bool
	// ackTerm/ackSentAt record the newest request of ackTerm that this peer
	// accepted (see markPeerAck). Guarded by peerMu.
	ackTerm   uint64
	ackSentAt time.Time
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
		applyRetryCh:      make(chan struct{}, 1),
		electionResetCh:   make(chan struct{}, 1),
		electionTimeout:   config.ElectionTimeout.Duration,
		heartbeatTimeout:  config.HeartbeatTimeout.Duration,
		commitTimeout:     config.CommitTimeout.Duration,
		applyRetryBackoff: applyRetryInitialBackoff,
		leaderID:          "",
		lastContact:       time.Now(),
		logger:            logger.With("component", "raft", "node_id", config.NodeID),
		snapshotThreshold: config.SnapshotThreshold,
		lastSnapshotIndex: 0,
	}

	// Initialize the configuration from the static peers. This node and
	// duplicate entries are collapsed, so neither counts twice in a quorum.
	n.membership.base = initialMembers(config, n.logger)
	n.refreshMembershipLocked()

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
	// With a cluster secret every RPC is signed and verified at the framing
	// layer, so no handler re-implements authentication.
	tt.SetAuth(n.nodeID, n.config.ClusterSecret, n.transportMember)
	handler := func(cmd interface{}, respCh chan interface{}) {
		select {
		case n.rpcCh <- &rpcWrapper{cmd: cmd, respCh: respCh}:
		case <-n.shutdownCh:
		}
	}
	for _, method := range []string{"AppendEntries", "RequestVote", "PreVote", "InstallSnapshot", "Heartbeat"} {
		tt.RegisterHandler(method, handler)
	}
	// Forward waits for a commit that the run loop produces, so it must not be
	// queued on the run loop: answer it from its own goroutine.
	tt.RegisterHandler(methodForward, func(cmd interface{}, respCh chan interface{}) {
		req, ok := cmd.(*core.ForwardRequest)
		if !ok {
			return
		}
		go func() { respCh <- n.handleForward(req) }()
	})
	// Join waits for a membership change to commit: its own goroutine too.
	tt.RegisterHandler(methodJoin, func(cmd interface{}, respCh chan interface{}) {
		req, ok := cmd.(*core.JoinRequest)
		if !ok {
			return
		}
		go func() { respCh <- n.handleJoin(req) }()
	})
	tt.RegisterHandler(methodIdentify, func(_ interface{}, respCh chan interface{}) {
		respCh <- n.handleIdentify()
	})
}

// transportMember decides which authenticated origins may send RPCs to this
// node. It accepts every origin that passed authentication (cluster secret /
// mTLS): Raft requires a server to accept RPCs from servers outside its own
// configuration, because its configuration may lag -- a node that has not yet
// received the entry adding D must still accept AppendEntries from D once D
// is the elected leader, or it can never catch up. Disruption by a removed
// node is prevented elsewhere: it stops campaigning (removed), and pre-vote
// with leader stickiness denies votes while a live leader exists. Forward
// requests are still restricted to members (verifyForward).
func (n *Node) transportMember(origin, method string) bool {
	return origin != ""
}

// Start starts the Raft node
func (n *Node) Start() error {
	if n.running.Load() {
		return fmt.Errorf("node already running")
	}

	if err := n.restoreHardState(); err != nil {
		return fmt.Errorf("failed to restore Raft hard state: %w", err)
	}
	if err := n.restorePeers(); err != nil {
		return fmt.Errorf("failed to restore Raft membership: %w", err)
	}
	n.restoreRemoved()
	if err := n.restoreLatestSnapshot(); err != nil {
		return fmt.Errorf("failed to restore snapshot: %w", err)
	}
	if err := n.restoreLog(); err != nil {
		return fmt.Errorf("failed to restore log: %w", err)
	}
	// Membership entries in the log are in effect whether or not they have
	// committed: a pending change survives the restart.
	n.mu.Lock()
	if n.logHasMembershipLocked() {
		n.refreshMembershipLocked()
	}
	n.tableScopeFloor = n.lastApplied
	n.mu.Unlock()
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
	n.loops.Add(3)
	go func() { defer n.loops.Done(); n.run() }()
	go func() { defer n.loops.Done(); n.applyLoop() }()
	go func() { defer n.loops.Done(); n.applyRetryLoop() }()
	go func() { n.loops.Wait(); close(n.doneCh) }()

	n.peerMu.RLock()
	peerCount := len(n.peers)
	n.peerMu.RUnlock()
	n.logger.Info("Raft node started",
		"node_id", n.nodeID,
		"bind_addr", n.bindAddr,
		"peers", peerCount)
	n.startJoining()

	return nil
}

// Stop gracefully stops the Raft node
func (n *Node) Stop() error {
	n.stopMu.Lock()
	defer n.stopMu.Unlock()

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

	// Wait for goroutines to finish: the protocol loops, then the
	// replication RPCs still in flight (the stopped transport fails them).
	n.bgMu.Lock()
	n.bgClosed = true
	n.bgMu.Unlock()
	bgDone := make(chan struct{})
	go func() { n.bg.Wait(); close(bgDone) }()
	deadline := time.Now().Add(5 * time.Second)
	select {
	case <-n.doneCh:
	case <-time.After(time.Until(deadline)):
		n.logger.Warn("Timeout waiting for node to stop")
	}
	select {
	case <-bgDone:
	case <-time.After(time.Until(deadline)):
		n.logger.Warn("Timeout waiting for replication RPCs to stop")
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

// replicateLog triggers log replication to all peers. It is the heartbeat
// path: one code path builds every AppendEntries, including the switch to
// InstallSnapshot for a peer whose next entry is already compacted away.
func (n *Node) replicateLog() {
	n.mu.RLock()
	commitIndex := n.commitIndex
	n.mu.RUnlock()
	n.sendHeartbeats(commitIndex)
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
			// A peer is connected if it answered our replication, or -- for the current
			// leader -- if this node itself heard from it: n.lastContact records that,
			// and a follower has no other evidence about its leader.
			IsConnected: time.Since(p.lastContact) < n.heartbeatTimeout*3 ||
				(p.ID == n.leaderID && time.Since(n.lastContact) < n.heartbeatTimeout*3),
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
			// A node that is replacing its state from a snapshot must not
			// campaign: the restore keeps it from answering the leader, and
			// a campaign here would disrupt a healthy cluster.
			if n.state.Load().(core.RaftState) != core.StateLeader && !n.isRestoring() {
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
				n.checkQuorum()
			}
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

// applyLoop serializes proposal admission and committed application. Slow FSM
// work must not prevent the protocol loop from answering heartbeats or votes.
func (n *Node) applyLoop() {
	for {
		select {
		case <-n.shutdownCh:
			return
		case future := <-n.applyCh:
			n.handleApply(future)
		case idx := <-n.commitCh:
			n.processCommitted(idx)
			n.maybeTakeSnapshot()
		}
	}
}

// isRestoring reports whether a snapshot restore is in flight. The node must
// not campaign while its state is being replaced.
func (n *Node) isRestoring() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.restoring
}

// startElection initiates a leader election with pre-vote
func (n *Node) startElection() {
	if n.removed.Load() || n.awaitingJoin.Load() {
		return
	}
	n.mu.Lock()

	// A node that is replacing its state from a snapshot must not campaign:
	// the restore keeps it from answering the leader, and a campaign here
	// would disrupt a healthy cluster.
	if n.restoring {
		n.mu.Unlock()
		return
	}

	// Only voters of the effective configuration campaign, and only their
	// votes count.
	if n.selfNonVoter.Load() {
		n.mu.Unlock()
		return
	}
	peers := n.votingPeers()

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
		// Promotion rechecks the election epoch under its own lock.
		term := n.currentTerm
		n.mu.Unlock()
		n.becomeLeader(term)
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

	// Check if we should proceed with real election. Pre-votes for the old
	// epoch cannot start a campaign after a response has advanced our term.
	n.mu.Lock()
	if n.currentTerm != preVoteTerm-1 || n.state.Load().(core.RaftState) != core.StateCandidate {
		n.mu.Unlock()
		return
	}

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
		n.becomeLeader(term)
	} else {
		n.mu.Lock()
		if n.currentTerm == term && n.state.Load().(core.RaftState) == core.StateCandidate {
			n.logger.Info("Election failed",
				"term", term,
				"votes", votesGranted,
				"needed", votesNeeded)
			n.state.Store(core.StateFollower)
		}
		n.mu.Unlock()
	}
}

// requestPreVotes sends PreVote RPCs to all peers and returns true if majority would grant votes
func (n *Node) requestPreVotes(term, lastLogIndex, lastLogTerm uint64, peers []*Peer) bool {
	var preVotesGranted atomic.Int32
	preVotesGranted.Add(1) // Vote for self

	// Skip peer RPCs if transport is nil (test scenarios)
	if n.transport == nil {
		// Only the self-vote can be counted, and the quorum is still a majority of the
		// voting members INCLUDING the candidate -- the same rule startElection uses for
		// the real election, and the one this function's main path below uses.
		// len(peers)/2 + 1 omitted the self-vote, which for an odd peer count sits below
		// a majority: a node that could reach no peer passed its own pre-vote, defeating
		// the protection the pre-vote exists to provide.
		needed := (len(peers)+1)/2 + 1
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
					// The normal step-down path: persist the term, clear the
					// leader, and fail any proposals if this node led.
					if err := n.becomeFollower(resp.Term); err != nil {
						n.logger.Error("Failed to persist higher term from a vote response", "error", err)
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
					// The normal step-down path: persist the term, clear the
					// leader, and fail any proposals if this node led.
					if err := n.becomeFollower(resp.Term); err != nil {
						n.logger.Error("Failed to persist higher term from a vote response", "error", err)
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
func (n *Node) becomeLeader(expectedTerm uint64) {
	n.mu.Lock()
	// Votes belong to one campaign. A higher-term response may have stepped us
	// down while other granted votes were in flight; those votes cannot elect
	// this node in the newly observed term.
	if n.currentTerm != expectedTerm || n.state.Load().(core.RaftState) != core.StateCandidate {
		n.mu.Unlock()
		return
	}

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
		p.nextIndex = lastLogIndex + 1
		p.matchIndex = 0
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
	// A membership change may be proposed only once this entry commits, and
	// check-quorum gives the new leader one election timeout to be heard.
	n.leaderNoopIndex = n.lastLogIndexLocked()
	n.leaderSince = time.Now()

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
	if term > oldTerm {
		// The leader of the new term is not known yet; callers that learn it
		// (AppendEntries, InstallSnapshot, Heartbeat) set it afterwards.
		n.leaderID = ""
	}
	if wasLeader {
		n.logger.Info("Stepped down as leader", "term", term)
		n.stats.ElectionsLost++
		// Proposals of the old term may still commit under the next leader, or
		// be overwritten: their outcome is unknown here, so none may succeed.
		n.failPendingFutures(0, "leadership lost before the entry committed")
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
	needsSnapshot := make([]bool, len(peers))
	n.mu.RLock()
	// A caller's earlier leader check can race a higher-term stepdown. Capture
	// the term and suffix only while this node still owns leadership; otherwise
	// an old leader could send its stale log stamped with the new leader's term.
	if n.state.Load().(core.RaftState) != core.StateLeader {
		n.mu.RUnlock()
		return
	}
	for i, p := range peers {
		if _, busy := n.replicationInFlight.LoadOrStore(p, struct{}{}); busy {
			continue
		}
		// Only entries actually compacted away need a snapshot; with no
		// compaction (logBase 0) everything from index 1 is in the log.
		needsSnapshot[i] = n.logBase > 0 && p.nextIndex <= n.logBase
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
			LeaderAddr:   n.advertiseAddr,
		}
	}
	n.mu.RUnlock()

	for i, peer := range peers {
		if reqs[i] == nil {
			continue
		}
		p, req, snapshot := peer, reqs[i], needsSnapshot[i]
		started := n.goBackground(func() {
			defer n.replicationInFlight.Delete(p)
			if snapshot {
				n.sendSnapshot(p)
				return
			}
			sentAt := time.Now()
			resp, err := n.transport.SendAppendEntries(p.ID, req)
			if err != nil {
				return
			}
			n.markPeerContacted(p.ID)
			if resp != nil && resp.Term == req.Term {
				n.markPeerAck(p.ID, req.Term, sentAt)
			}

			n.handleAppendEntriesResponse(p, req, resp)
		})
		if !started {
			n.replicationInFlight.Delete(p)
		}
	}
}

// goBackground runs fn on a goroutine that Stop waits for. It refuses (and
// reports false) once Stop has begun.
func (n *Node) goBackground(fn func()) bool {
	n.bgMu.Lock()
	defer n.bgMu.Unlock()
	if n.bgClosed {
		return false
	}
	n.bg.Add(1)
	go func() {
		defer n.bg.Done()
		fn()
	}()
	return true
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

	// Valid heartbeat from leader. Contact that is new (another leader, or
	// after a silence) starts a catch-up: this node's applied state is stale
	// until it reaches the commit index the leader reports now.
	if n.leaderID != req.LeaderID || time.Since(n.lastContact) >= n.electionTimeout {
		n.catchUpTarget = req.LeaderCommit
	}
	n.lastContact = time.Now()
	n.leaderID = req.LeaderID
	// Learn where the leader lives when it is not a configured peer (a node
	// added at runtime, or a join address configured under another ID), so
	// writes can be forwarded to it. A known address is never overwritten.
	if req.LeaderAddr != "" && req.LeaderID != n.nodeID && dialableAddr(req.LeaderAddr) {
		if tt, ok := n.transport.(*TCPTransport); ok && !tt.HasPeer(req.LeaderID) {
			tt.RegisterPeer(req.LeaderID, req.LeaderAddr)
		}
	}
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
		// Too short: the leader should resume right after our last entry.
		// Below our base (already in our snapshot): right after the base.
		last := n.lastLogIndexLocked()
		conflict := n.logBase + 1
		if req.PrevLogIndex > last {
			conflict = last + 1
		}
		return &core.AppendEntriesResponse{Term: n.currentTerm, Success: false, MatchIndex: last, ConflictIndex: conflict}
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
		// Proposals at the truncated indexes were overwritten by another
		// leader's entries: they can never commit.
		n.failPendingFutures(truncateAt, "entry overwritten by a newer leader")
	}
	last := n.lastLogIndexLocked()
	appended := make([]core.RaftLogEntry, 0)
	membershipChanged := truncateAt > 0
	for i, incoming := range req.Entries {
		idx := req.PrevLogIndex + uint64(i) + 1
		if idx > last {
			incoming.Index = idx
			appended = append(appended, incoming)
			if incoming.Type == core.LogMembershipChange {
				membershipChanged = true
			}
		}
	}
	if len(appended) > 0 && n.storage != nil {
		if err := n.storage.StoreLogs(appended); err != nil {
			if truncateAt > 0 {
				n.refreshMembershipLocked()
			}
			return fmt.Errorf("persist appended entries: %w", err)
		}
	}
	n.log = append(n.log, appended...)
	if membershipChanged {
		// A configuration is in effect as soon as it is in the log, and a
		// truncated one is no longer.
		n.refreshMembershipLocked()
	}
	return nil
}

// notifyCommit wakes the run loop after commitIndex advances. It must never
// block: both call sites hold n.mu, and the run loop that would drain commitCh
// is the very goroutine executing these calls (it dispatches handleRPC), so a
// blocking send on a full buffer wedges the node holding the node mutex and
// nothing can drain it again. Dropping an intermediate value is safe because
// processCommitted applies every entry up to the index it is given, so only the
// newest pending index carries information.
func (n *Node) notifyCommit(index uint64) {
	select {
	case n.commitCh <- index:
		return
	default:
	}
	// Buffer full: discard the stale pending signal and keep the newest index.
	select {
	case <-n.commitCh:
	default:
	}
	select {
	case n.commitCh <- index:
	default:
	}
}

// updateCommitIndex advances the follower's commit index from the leader's

func (n *Node) updateCommitIndex(req *core.AppendEntriesRequest) {
	if req.LeaderCommit <= n.commitIndex {
		return
	}
	lastNewIndex := req.PrevLogIndex + uint64(len(req.Entries))
	newCommit := req.LeaderCommit
	if newCommit > lastNewIndex {
		newCommit = lastNewIndex
	}
	if newCommit <= n.commitIndex {
		return
	}
	old := n.commitIndex
	n.commitIndex = newCommit
	if err := n.persistHardStateLocked(); err != nil {
		n.commitIndex = old
		n.logger.Error("Failed to persist follower commit index", "error", err)
		return
	}
	n.notifyCommit(n.commitIndex)
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
	n.applyMu.Lock()
	defer n.applyMu.Unlock()
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
	// A delayed/repeated snapshot must never replace newer committed state.
	// Acknowledge it without restoring; the leader can continue replication.
	if req.LastIncludedIndex <= n.commitIndex || req.LastIncludedIndex <= n.lastApplied {
		boundary := max(n.commitIndex, n.lastApplied)
		n.mu.Unlock()
		n.snapshotMu.Lock()
		if n.incomingSnapshot != nil && n.incomingSnapshot.index <= boundary {
			n.incomingSnapshot = nil
		}
		n.snapshotMu.Unlock()
		return &core.InstallSnapshotResponse{Term: term, Success: true}
	}
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
	if _, ok := n.fsm.(*StorageFSM); ok {
		// Validate the image without replacing live state before publishing it.
		var image map[string][]byte
		if err := json.Unmarshal(payload, &image); err != nil {
			n.incomingSnapshot = nil
			return &core.InstallSnapshotResponse{Term: term, Success: false}
		}
	}
	// Persist the replacement before changing application state. If writing
	// its chunks/manifest fails, the current state must remain usable.
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
	// Publication can block on storage while another RPC advances the term.
	// Recheck at the destructive transition before touching the FSM.
	n.mu.Lock()
	if req.Term != n.currentTerm || install.index <= n.commitIndex || install.index <= n.lastApplied {
		response := &core.InstallSnapshotResponse{Term: n.currentTerm, Success: req.Term == n.currentTerm}
		n.mu.Unlock()
		n.incomingSnapshot = nil
		return response
	}
	// The restore replaces the whole keyspace and can take a long time. It
	// must not run under n.mu: the same lock guards the heartbeat path,
	// AppendEntries and the election timer, so holding it across a large
	// restore would make a healthy leader look dead and could start a
	// competing election over a snapshot that is still being installed.
	// Publish the transition, drop the lock for the I/O, and take it again
	// to move the boundary.
	n.restoring = true
	n.mu.Unlock()

	var restoreErr error
	if fsm, ok := n.fsm.(*StorageFSM); ok {
		// Observers may read the node. Delay notification until mu is released.
		restoreErr = fsm.restore(payload)
		if restoreErr == nil {
			defer fsm.notifyRestored()
		}
	} else {
		restoreErr = n.fsm.Restore(payload)
	}

	n.mu.Lock()
	n.restoring = false
	if restoreErr != nil {
		n.mu.Unlock()
		n.incomingSnapshot = nil
		return &core.InstallSnapshotResponse{Term: n.currentTerm, Success: false}
	}
	if fsm, ok := n.fsm.(interface{ SetLastApplied(uint64) }); ok {
		fsm.SetLastApplied(install.index)
	}
	if install.index > n.tableScopeFloor {
		n.tableScopeFloor = install.index
	}
	// The restore ran without the node lock, so entries may have arrived
	// and the commit index may have advanced while it was in flight. The
	// snapshot supersedes only the entries at or below its boundary: keep
	// everything above it, and never move an index backwards.
	kept := make([]core.RaftLogEntry, 0, len(n.log))
	for _, e := range n.log {
		if e.Index > install.index {
			kept = append(kept, e)
		}
	}
	n.log = append([]core.RaftLogEntry{{Index: install.index, Term: install.term}}, kept...)
	n.logBase, n.logBaseTerm = install.index, install.term
	var installedConfig []core.RaftPeer
	if len(req.Configuration) > 0 {
		installedConfig, _ = normalizeMembers(req.Configuration)
		n.setMembershipBaseLocked(installedConfig)
	} else {
		n.refreshMembershipLocked()
	}
	if n.commitIndex < install.index {
		n.commitIndex = install.index
	}
	if n.lastApplied < install.index {
		n.lastApplied = install.index
	}
	n.lastSnapshotIndex = install.index
	// The snapshot came from the leader, so the leader is alive: a restore
	// that took longer than the election timeout must not look like a dead
	// leader and trigger a competing election.
	n.lastContact = time.Now()
	if err := n.persistHardStateLocked(); err != nil {
		// The durable snapshot is already authoritative and the FSM was
		// replaced. Rolling only indexes back would mismatch the live state.
		// Recovery can rebuild this same boundary from the snapshot metadata.
		n.mu.Unlock()
		n.incomingSnapshot = nil
		return &core.InstallSnapshotResponse{Term: n.currentTerm, Success: false}
	}
	// The request may belong to a term this node has already left. The state
	// was replaced either way, so the boundary is published and the stale
	// term is reported back to the sender.
	accepted := req.Term == n.currentTerm
	term = n.currentTerm
	n.mu.Unlock()
	if installedConfig != nil {
		n.persistMembers(installedConfig)
	}
	// Give the follower a fresh election window: the restore kept it from
	// answering the leader for as long as it ran.
	select {
	case n.electionResetCh <- struct{}{}:
	default:
	}
	if n.storage != nil {
		if first, _ := n.storage.FirstIndex(); first > 0 && first <= install.index {
			_ = n.storage.DeleteRange(first, install.index)
		}
	}
	n.incomingSnapshot = nil
	return &core.InstallSnapshotResponse{Term: term, Success: accepted}
}

// markPeerContacted records that a peer has just answered us. Peer.lastContact is
// what ClusterState.Peers[].IsConnected is computed from; without this write it stays
// at the zero time, so every peer is reported as disconnected forever and consumers
// that rely on the field (the cluster manager's heartbeat producer, the cluster status
// API) treat a live peer as dead.
//
// Call it from paths that do not already hold n.mu: sendHeartbeats takes peerMu and
// then n.mu, so taking peerMu while holding n.mu would invert that order.
func (n *Node) markPeerContacted(peerID string) {
	if peerID == "" || peerID == n.nodeID {
		return
	}
	n.peerMu.Lock()
	defer n.peerMu.Unlock()
	if peer, ok := n.peers[peerID]; ok {
		peer.lastContact = time.Now()
	}
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
		if n.state.Load().(core.RaftState) == core.StateLeader {
			n.stepDownLocked()
			n.leaderID = req.LeaderID
		}
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
	// The membership entries the snapshot covers are not in the log any more:
	// send the committed configuration along with it.
	configuration := n.membershipBase()
	for offset := 0; offset <= len(data); offset += snapshotChunkSize {
		end := offset + snapshotChunkSize
		if end > len(data) {
			end = len(data)
		}
		done := end == len(data)
		req := &core.InstallSnapshotRequest{Term: term, LeaderID: leaderID, LastIncludedIndex: meta.Index, LastIncludedTerm: meta.Term, Offset: uint64(offset), Data: data[offset:end], Done: done}
		if done {
			req.Configuration = configuration
		}
		sentAt := time.Now()
		resp, err := n.transport.SendInstallSnapshot(peer.ID, req)
		if err != nil || resp == nil {
			return
		}
		if resp.Term == term {
			n.markPeerAck(peer.ID, term, sentAt)
		}
		if !resp.Success {
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

	// Fast backup (Raft §5.3): use the follower's hint instead of probing
	// back one index per round. With a conflicting term, resume after this
	// leader's last entry of that term, or at the first index the follower
	// holds of it; with a log that is too short, at the follower's end.
	next := req.PrevLogIndex
	switch {
	case resp.ConflictTerm > 0:
		next = resp.ConflictIndex
		for i := len(n.log) - 1; i > 0; i-- {
			if n.log[i].Term == resp.ConflictTerm {
				next = n.log[i].Index + 1
				break
			}
			if n.log[i].Term < resp.ConflictTerm {
				break
			}
		}
	case resp.ConflictIndex > 0:
		next = resp.ConflictIndex
	}
	if next < 1 {
		next = 1
	}
	// A next index at or below the compaction base is served by a snapshot
	// on the next round (sendHeartbeats).

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
		if !n.checkStandardCommit(index) {
			continue
		}
		old := n.commitIndex
		n.commitIndex = index
		if err := n.persistHardStateLocked(); err != nil {
			n.commitIndex = old
			n.logger.Error("Failed to persist leader commit index", "error", err)
			return
		}
		n.notifyCommit(index)
		return
	}
}

// checkStandardCommit reports whether index is stored on a majority of the
// voters of the effective configuration. Non-voters and members whose removal
// is pending never count. Caller holds n.mu.
func (n *Node) checkStandardCommit(index uint64) bool {
	votes, voters := n.countVoters(true, func(p *Peer) bool { return n.matchIndex[p.ID] >= index })
	return voters > 0 && votes > voters/2
}

// appliedPersistEvery bounds how many applied entries may share one
// hard-state write.
const appliedPersistEvery = 256

// processCommitted applies committed entries to FSM.
// lastApplied is advanced only after the entry has been applied successfully.
//
// The applied index is persisted once per pass (and every
// appliedPersistEvery entries) rather than after each entry: that write is an
// fsync of its own, and paying it per entry halved how fast a follower could
// catch up. The FSM's own writes are durable when Apply returns, so a crash
// can only leave the persisted index behind the store, and the entries after
// it are applied again on restart. That replay is safe: plain sets and
// deletes converge, command handlers must be idempotent, and the one
// compare-and-set (shared authentication) is guarded by a revision that only
// grows, so a replayed change is a no-op.
func (n *Node) processCommitted(commitIndex uint64) {
	n.applyMu.Lock()
	defer n.applyMu.Unlock()
	unpersisted := 0
	for {
		n.mu.RLock()
		if n.lastApplied >= commitIndex {
			dirty := n.appliedDirty
			n.mu.RUnlock()
			if (unpersisted > 0 || dirty) && !n.persistApplied() {
				return
			}
			// Nothing left to apply: the pass succeeded, so the next
			// failure starts from the initial backoff again.
			n.resetApplyRetryBackoff()
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
			if change, err := decodeMembershipEntry(entry.Data); err != nil {
				applyErr = fmt.Errorf("decode membership change at index %d: %w", entry.Index, err)
			} else {
				n.applyMembershipEntry(change, entry.Index)
			}
		} else if result := n.fsm.Apply(&entry); result != nil {
			if err, ok := result.(error); ok {
				applyErr = err
			} else {
				applyErr = fmt.Errorf("FSM apply returned unexpected result at index %d: %v", entry.Index, result)
			}
		}
		if applyErr != nil {
			if !isRetryableApplyError(applyErr) {
				// A deterministic failure can never succeed on a later
				// attempt: an undecodable payload, a rejected command, an
				// unknown op. Retrying it would wedge this node at this
				// index forever and stall snapshotting and compaction with
				// it, so the entry is skipped and the node keeps applying
				// the rest of the log.
				n.logger.Error("Skipping committed entry that cannot be applied",
					"index", entry.Index, "term", entry.Term, "error", applyErr)
				if !n.skipCommittedEntry(nextIndex) {
					return
				}
				n.notifyApply(entry.Index, entry.Term, applyErr)
				continue
			}
			n.logger.Error("Failed to apply committed entry", "index", entry.Index, "term", entry.Term, "error", applyErr)
			n.notifyApply(entry.Index, entry.Term, applyErr)
			// lastApplied stays behind the failed entry, so the commit
			// index does not advance either and notifyCommit will not fire
			// again: the leader keeps heartbeating the same LeaderCommit
			// value. Without an explicit retry this node would stay wedged
			// at this index (and snapshotting, which describes only the
			// applied state, would stall with it).
			n.scheduleApplyRetry()
			return
		}
		n.mu.Lock()
		n.lastApplied = nextIndex
		n.mu.Unlock()
		n.notifyApply(entry.Index, entry.Term, nil)
		if unpersisted++; unpersisted >= appliedPersistEvery {
			if !n.persistApplied() {
				return
			}
			unpersisted = 0
		}
	}
}

// persistApplied writes the hard state after entries were applied. On
// failure the in-memory applied index stays where it is -- those entries are
// in the store -- and the write is retried by the next pass. Caller holds
// n.applyMu.
func (n *Node) persistApplied() bool {
	n.mu.Lock()
	err := n.persistHardStateLocked()
	n.appliedDirty = err != nil
	n.mu.Unlock()
	if err != nil {
		n.logger.Error("Failed to persist the applied index", "error", err)
		n.scheduleApplyRetry()
		return false
	}
	return true
}

// skipCommittedEntry consumes a committed entry that can never be applied.
//
// The applied index moves past the entry so the node keeps applying the log
// instead of wedging at an index it can never pass. The FSM index is moved
// with it, so both agree on which entries are consumed.
//
// It reports false when the applied index could not be persisted: the caller
// must stop the pass, because the entry is still unapplied.
func (n *Node) skipCommittedEntry(nextIndex uint64) bool {
	n.mu.Lock()
	old := n.lastApplied
	n.lastApplied = nextIndex
	if err := n.persistHardStateLocked(); err != nil {
		n.lastApplied = old
		n.mu.Unlock()
		// The applied index could not be persisted: the pass must be
		// repeated once storage recovers.
		n.scheduleApplyRetry()
		return false
	}
	n.mu.Unlock()
	if fsm, ok := n.fsm.(interface{ SetLastApplied(uint64) }); ok {
		fsm.SetLastApplied(nextIndex)
	}
	return true
}

// scheduleApplyRetry asks the retry loop to re-attempt the apply of the
// committed entries that failed. It never blocks: the apply path runs on the
// run loop, and the request channel is buffered, so repeated failures
// coalesce into a single pending retry.
func (n *Node) scheduleApplyRetry() {
	select {
	case n.applyRetryCh <- struct{}{}:
	default:
	}
}

// resetApplyRetryBackoff restores the initial retry delay after a pass that
// applied every committed entry.
func (n *Node) resetApplyRetryBackoff() {
	n.applyRetryMu.Lock()
	n.applyRetryBackoff = applyRetryInitialBackoff
	n.applyRetryMu.Unlock()
}

// nextApplyRetryDelay returns the delay before the next retry and grows it,
// so a persistent storage failure backs off instead of spinning.
func (n *Node) nextApplyRetryDelay() time.Duration {
	n.applyRetryMu.Lock()
	defer n.applyRetryMu.Unlock()
	delay := n.applyRetryBackoff
	if delay <= 0 {
		delay = applyRetryInitialBackoff
	}
	next := delay * 2
	if next > applyRetryMaxBackoff {
		next = applyRetryMaxBackoff
	}
	n.applyRetryBackoff = next
	return delay
}

// applyRetryLoop re-signals the commit path after a failed apply.
//
// processCommitted returns without advancing lastApplied when the FSM or the
// hard-state write fails. The commit index does not move either, so the
// leader's next heartbeat carries the same LeaderCommit value and
// updateCommitIndex returns early without notifying the run loop. This loop
// re-signals the commit channel instead, which lets lastApplied advance as
// soon as storage recovers.
func (n *Node) applyRetryLoop() {
	for {
		select {
		case <-n.shutdownCh:
			return
		case <-n.applyRetryCh:
		}

		delay := n.nextApplyRetryDelay()
		timer := time.NewTimer(delay)
		select {
		case <-n.shutdownCh:
			timer.Stop()
			return
		case <-timer.C:
		}

		n.mu.RLock()
		index := n.commitIndex
		n.mu.RUnlock()
		if index == 0 {
			continue
		}
		// processCommitted applies every entry up to the index it gets, so
		// the newest committed index is enough to resume the failed apply.
		n.notifyCommit(index)
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

// dialableAddr reports whether addr names a host another node can dial: a
// leader that advertises its wildcard bind address ("0.0.0.0:7946") must not
// be recorded under it.
func dialableAddr(addr string) bool {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host == "" || port == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		return false
	}
	return true
}

// setRemoved records whether this node is out of the cluster, durably when a
// stable store is configured.
func (n *Node) setRemoved(removed bool) {
	n.removed.Store(removed)
	if n.stable == nil {
		return
	}
	value := []byte("0")
	if removed {
		value = []byte("1")
	}
	if err := n.stable.Set(stableRemoved, value); err != nil {
		n.logger.Error("Failed to persist removal state", "error", err)
	}
}

// restoreRemoved reloads the removal flag on start.
func (n *Node) restoreRemoved() {
	if n.stable == nil {
		return
	}
	if data, err := n.stable.Get(stableRemoved); err == nil && string(data) == "1" {
		n.removed.Store(true)
		n.logger.Warn("This node was removed from the cluster; it will not campaign until it is added back")
	}
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
	// A durable FSM that already applied past the snapshot holds its state
	// and more: lastApplied is persisted only after an entry is applied.
	// Rewriting the whole store from the snapshot on every restart took
	// minutes for a real dataset and kept the node from rejoining.
	if d, ok := n.fsm.(interface{ StateIsDurable() bool }); ok && d.StateIsDurable() && n.lastApplied >= meta.Index {
		n.lastSnapshotIndex = meta.Index
		if fsm, ok := n.fsm.(interface{ SetLastApplied(uint64) }); ok {
			fsm.SetLastApplied(n.lastApplied)
		}
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
	// Restore replaces the application state with the snapshot image, so any
	// later committed entries must be replayed even if hard state says applied.
	n.lastApplied = meta.Index
	if fsm, ok := n.fsm.(interface{ SetLastApplied(uint64) }); ok {
		fsm.SetLastApplied(meta.Index)
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

// notifyApply resolves the proposal waiting at index. A proposal succeeds
// only if the entry applied there is the one it appended: same index and the
// term it was proposed in. Another term at that index means a newer leader
// overwrote the proposal, which therefore never took effect.
func (n *Node) notifyApply(index, term uint64, err error) {
	v, ok := n.applyWaiters.LoadAndDelete(index)
	if !ok {
		return
	}
	f := v.(*applyFuture)
	if f.term != 0 && f.term != term {
		err = &core.RaftError{Code: core.ErrLeadershipLost,
			Message: fmt.Sprintf("entry %d proposed in term %d was replaced by a term %d entry", index, f.term, term)}
	} else {
		f.term = term
	}
	f.err = err
	close(f.done)
}

// maybeTakeSnapshot checks if a snapshot should be taken and creates one
func (n *Node) maybeTakeSnapshot() {
	n.applyMu.Lock()
	defer n.applyMu.Unlock()
	if n.snapshot == nil || n.snapshotThreshold <= 0 || n.snapshotInProgress.Load() {
		return
	}
	n.mu.RLock()
	// Entries kept as trailing logs are already covered by the last snapshot:
	// measure the log from whichever boundary is newer.
	covered := max(n.logBase, n.lastSnapshotIndex)
	logSize := int(n.lastLogIndexLocked() - covered)
	// Committed entries may still be awaiting an application retry after I/O
	// failure. A snapshot may describe only the state actually applied.
	snapIndex := n.lastApplied
	snapTerm := n.getLogTerm(snapIndex)
	n.mu.RUnlock()
	if logSize < n.snapshotThreshold || snapIndex <= covered {
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

// compactLog removes log entries that are included in the snapshot. The
// in-memory log and the persisted base change under n.mu; the stored entries
// below the new base are deleted after it is released, because nothing reads
// them any more and deleting thousands of keys must not stop heartbeats and
// AppendEntries (it did, for close to a minute, in the HA soak).
func (n *Node) compactLog(snapshotIndex uint64) {
	oldBase, newBase, ok := n.compactLogLocked(snapshotIndex)
	if !ok || n.storage == nil || oldBase+1 > newBase {
		return
	}
	if err := n.storage.DeleteRange(oldBase+1, newBase); err != nil {
		n.logger.Error("Failed to compact persistent log", "error", err)
	}
}

// compactLogLocked moves the log base and reports the stored range that the
// caller must delete.
func (n *Node) compactLogLocked(snapshotIndex uint64) (uint64, uint64, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if snapshotIndex <= n.logBase || snapshotIndex > n.lastLogIndexLocked() {
		return 0, 0, false
	}
	// Keep the TrailingLogs entries up to and including the snapshot index,
	// so a follower slightly behind can still catch up from the log instead
	// of needing the whole snapshot. The new base is the entry just before
	// the oldest retained one.
	newBase := snapshotIndex
	if trailing := uint64(n.config.TrailingLogs); trailing > 0 {
		if snapshotIndex > trailing {
			newBase = snapshotIndex - trailing
		} else {
			newBase = 0
		}
	}
	if newBase <= n.logBase {
		return 0, 0, false
	}
	newBaseTerm := n.getLogTerm(newBase)
	pos, ok := n.logPosition(newBase)
	if !ok {
		return 0, 0, false
	}
	retained := make([]core.RaftLogEntry, 0, len(n.log)-pos)
	retained = append(retained, core.RaftLogEntry{Index: newBase, Term: newBaseTerm})
	retained = append(retained, n.log[pos+1:]...)
	oldBase := n.logBase
	n.logBase, n.logBaseTerm, n.log = newBase, newBaseTerm, retained
	if err := n.persistHardStateLocked(); err != nil {
		n.logger.Error("Failed to persist compacted log base", "error", err)
		return 0, 0, false
	}
	return oldBase, newBase, true
}
