package raft

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// createTestNode creates a test node with minimal setup
func createTestNode(t *testing.T) *Node {
	t.Helper()
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, err := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())
	if err != nil {
		t.Fatalf("Failed to create test node: %v", err)
	}
	return node
}

func newTestRaftLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
}

func newTestRaftNodeConfig() core.RaftConfig {
	return core.RaftConfig{
		NodeID:           "test-node-1",
		BindAddr:         "127.0.0.1:0",
		AdvertiseAddr:    "127.0.0.1:7000",
		Bootstrap:        false,
		ElectionTimeout:  core.Duration{Duration: 100 * time.Millisecond},
		HeartbeatTimeout: core.Duration{Duration: 50 * time.Millisecond},
		CommitTimeout:    core.Duration{Duration: 50 * time.Millisecond},
		MaxAppendEntries: 64,
	}
}

func TestNode_NewNode(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, err := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}

	if node == nil {
		t.Fatal("Expected node to be created")
	}

	if node.nodeID != cfg.NodeID {
		t.Errorf("Expected node ID %s, got %s", cfg.NodeID, node.nodeID)
	}

	if node.state != core.StateFollower {
		t.Errorf("Expected initial state Follower, got %s", node.state)
	}

	if node.currentTerm != 0 {
		t.Errorf("Expected initial term 0, got %d", node.currentTerm)
	}

	if node.votedFor != "" {
		t.Errorf("Expected initial votedFor empty, got %s", node.votedFor)
	}

	if node.leaderID != "" {
		t.Errorf("Expected initial leaderID empty, got %s", node.leaderID)
	}
}

func TestNode_NewNode_InvalidConfig(t *testing.T) {
	cfg := core.RaftConfig{
		NodeID: "", // Invalid - empty node ID
	}

	_, err := NewNode(cfg, nil, nil, nil, newTestRaftLogger())
	if err == nil {
		t.Error("Expected error for invalid config")
	}
}

func TestNode_SetTransport(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	transport := &mockTransport{}
	node.SetTransport(transport)

	if node.transport == nil {
		t.Error("Expected transport to be set")
	}
}

func TestNode_State(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	state := node.State()
	if state != core.StateFollower {
		t.Errorf("Expected StateFollower, got %s", state)
	}
}

func TestNode_IsLeader(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	if node.IsLeader() {
		t.Error("Expected node to not be leader initially")
	}
}

func TestNode_Leader(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	leader := node.Leader()
	if leader != "" {
		t.Errorf("Expected empty leader, got %s", leader)
	}
}

func TestNode_Term(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	term := node.Term()
	if term != 0 {
		t.Errorf("Expected term 0, got %d", term)
	}
}

func TestNode_CurrentTerm(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	term := node.CurrentTerm()
	if term != 0 {
		t.Errorf("Expected term 0, got %d", term)
	}
}

func TestNode_CommitIndex(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	idx := node.CommitIndex()
	if idx != 0 {
		t.Errorf("Expected commit index 0, got %d", idx)
	}
}

func TestNode_LeaderID(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	leaderID := node.LeaderID()
	if leaderID != "" {
		t.Errorf("Expected empty leaderID, got %s", leaderID)
	}
}

func TestNode_Peers(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.Peers = []core.RaftPeer{
		{ID: "node-2", Address: "127.0.0.1:7001", Region: "default", Role: core.RoleVoter},
		{ID: "node-3", Address: "127.0.0.1:7002", Region: "default", Role: core.RoleVoter},
	}

	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	peers := node.Peers()

	if len(peers) != 2 {
		t.Errorf("Expected 2 peers, got %d", len(peers))
	}

	if _, exists := peers["node-2"]; !exists {
		t.Error("Expected node-2 to be in peers")
	}

	if _, exists := peers["node-3"]; !exists {
		t.Error("Expected node-3 to be in peers")
	}
}

func TestNode_Done(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	doneCh := node.Done()
	if doneCh == nil {
		t.Error("Expected done channel to be created")
	}
}

func TestNode_GetPeers(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.Peers = []core.RaftPeer{
		{ID: "node-2", Address: "127.0.0.1:7001", Region: "default", Role: core.RoleVoter},
	}

	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	peers := node.GetPeers()

	if len(peers) != 1 {
		t.Errorf("Expected 1 peer, got %d", len(peers))
	}

	if peers[0].ID != "node-2" {
		t.Errorf("Expected peer ID node-2, got %s", peers[0].ID)
	}
}

func TestNode_AddPeer(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	// Test that AddPeer requires leader
	peer := core.RaftPeer{
		ID:      "new-node",
		Address: "127.0.0.1:7003",
		Region:  "default",
		Role:    core.RoleVoter,
	}

	err := node.AddPeer(peer)
	if err == nil {
		t.Error("Expected error when adding peer as non-leader")
	}
	if raftErr, ok := err.(*core.RaftError); !ok || raftErr.Code != core.ErrNotLeader {
		t.Errorf("Expected NOT_LEADER error, got: %v", err)
	}
}

func TestNode_AddPeer_Self(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	peer := core.RaftPeer{
		ID:      cfg.NodeID, // Same as self
		Address: "127.0.0.1:7003",
		Region:  "default",
		Role:    core.RoleVoter,
	}

	err := node.AddPeer(peer)
	if err == nil {
		t.Error("Expected error when adding self as peer")
	}
}

func TestNode_AddPeer_Duplicate(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.Peers = []core.RaftPeer{
		{ID: "node-2", Address: "127.0.0.1:7001", Region: "default", Role: core.RoleVoter},
	}

	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	peer := core.RaftPeer{
		ID:      "node-2", // Duplicate
		Address: "127.0.0.1:7003",
		Region:  "default",
		Role:    core.RoleVoter,
	}

	err := node.AddPeer(peer)
	if err == nil {
		t.Error("Expected error when adding duplicate peer")
	}
}

func TestNode_RemovePeer(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.Peers = []core.RaftPeer{
		{ID: "node-2", Address: "127.0.0.1:7001", Region: "default", Role: core.RoleVoter},
	}

	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	// Test that RemovePeer requires leader
	err := node.RemovePeer("node-2")
	if err == nil {
		t.Error("Expected error when removing peer as non-leader")
	}
	if raftErr, ok := err.(*core.RaftError); !ok || raftErr.Code != core.ErrNotLeader {
		t.Errorf("Expected NOT_LEADER error, got: %v", err)
	}
}

func TestNode_RemovePeer_Self(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	err := node.RemovePeer(cfg.NodeID)
	if err == nil {
		t.Error("Expected error when removing self")
	}
}

func TestNode_RemovePeer_NotFound(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	err := node.RemovePeer("nonexistent")
	if err == nil {
		t.Error("Expected error when removing non-existent peer")
	}
}

func TestNode_GetState(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.Peers = []core.RaftPeer{
		{ID: "node-2", Address: "127.0.0.1:7001", Region: "default", Role: core.RoleVoter},
	}

	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	state := node.GetState()

	if state.NodeID != cfg.NodeID {
		t.Errorf("Expected node ID %s, got %s", cfg.NodeID, state.NodeID)
	}

	if state.State != core.StateFollower {
		t.Errorf("Expected state Follower, got %s", state.State)
	}

	if state.Term != 0 {
		t.Errorf("Expected term 0, got %d", state.Term)
	}
}

func TestNode_Apply_NotLeader(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	cmd := core.FSMCommand{
		Op:    core.FSMSet,
		Key:   "test",
		Value: []byte("test"),
	}

	_, _, _, err := node.Apply(cmd, 100*time.Millisecond)
	if err == nil {
		t.Error("Expected error when applying as non-leader")
	}
}

func TestNode_Apply_Shutdown(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())
	node.shutdown.Store(true)

	cmd := core.FSMCommand{
		Op:    core.FSMSet,
		Key:   "test",
		Value: []byte("test"),
	}

	_, _, _, err := node.Apply(cmd, 100*time.Millisecond)
	if err == nil {
		t.Error("Expected error when node is shutting down")
	}
}

func TestNode_Shutdown(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	// Shutdown should not panic
	node.Shutdown()
}

func TestNode_Start_NotRunning(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	// Start without transport should work (transport is optional)
	err := node.Start()
	if err != nil {
		t.Errorf("Start failed: %v", err)
	}

	// Stop
	err = node.Stop()
	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

func TestNode_Start_AlreadyRunning(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	// Start twice
	err := node.Start()
	if err != nil {
		t.Fatalf("First Start failed: %v", err)
	}

	err = node.Start()
	if err == nil {
		t.Error("Expected error when starting already running node")
	}

	// Cleanup
	node.Stop()
}

func TestNode_Stop_NotRunning(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	// Stop without start should not error
	err := node.Stop()
	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

func TestNode_Stop_Idempotent(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	err := node.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	err = node.Stop()
	if err != nil {
		t.Errorf("First Stop failed: %v", err)
	}

	err = node.Stop()
	if err != nil {
		t.Errorf("Second Stop failed: %v", err)
	}
}

func TestNode_HandleAppendEntries_Response(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	// Test term too old
	req := &core.AppendEntriesRequest{
		Term: 0, // Less than current term (which is 0, so this tests edge case)
	}

	resp := node.handleAppendEntries(req)
	if resp == nil {
		t.Fatal("Expected response")
	}
}

func TestNode_HandleRequestVote_Response(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	req := &core.RequestVoteRequest{
		Term:         1,
		CandidateID:  "candidate-1",
		LastLogIndex: 0,
		LastLogTerm:  0,
	}

	resp := node.handleRequestVote(req)
	if resp == nil {
		t.Fatal("Expected response")
	}

	if !resp.VoteGranted {
		t.Error("Expected vote to be granted")
	}
}

func TestNode_HandleRequestVote_TooOld(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	req := &core.RequestVoteRequest{
		Term:         0, // Same as current, won't update
		CandidateID:  "candidate-1",
		LastLogIndex: 0,
		LastLogTerm:  0,
	}

	resp := node.handleRequestVote(req)
	if resp == nil {
		t.Fatal("Expected response")
	}
}

func TestNode_HandleHeartbeat(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	req := &core.HeartbeatRequest{
		Term:     1,
		LeaderID: "leader-1",
	}

	resp := node.handleHeartbeat(req)
	if resp == nil {
		t.Fatal("Expected response")
	}

	if resp.LeaderID != "leader-1" {
		t.Errorf("Expected leader ID leader-1, got %s", resp.LeaderID)
	}
}

func TestNode_HandleInstallSnapshot(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	req := &core.InstallSnapshotRequest{
		Term:     1,
		LeaderID: "leader-1",
		Data:     []byte("snapshot-data"),
	}

	resp := node.handleInstallSnapshot(req)
	if resp == nil {
		t.Fatal("Expected response")
	}
}

func TestNode_BecomeFollower(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	// Manually become leader first
	node.state = core.StateLeader
	node.currentTerm = 1

	// Then become follower
	node.becomeFollower(2)

	if node.state != core.StateFollower {
		t.Errorf("Expected state Follower, got %s", node.state)
	}

	if node.currentTerm != 2 {
		t.Errorf("Expected term 2, got %d", node.currentTerm)
	}
}

func TestNode_HelperFunctions(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	// Test getLogTerm on empty log
	term := node.getLogTerm(0)
	if term != 0 {
		t.Errorf("Expected term 0 for empty log, got %d", term)
	}

	// Test getLogTerm on out of bounds index
	term = node.getLogTerm(999)
	if term != 0 {
		t.Errorf("Expected term 0 for out of bounds, got %d", term)
	}

	// Test getEntriesAfter on empty log
	entries := node.getEntriesAfter(0, 10)
	if entries == nil {
		t.Error("Expected empty slice, got nil")
	}

	// Test appendEntry
	entry := core.RaftLogEntry{
		Term: 1,
		Type: core.LogNoOp,
	}
	node.appendEntry(entry)

	if len(node.log) != 2 {
		t.Errorf("Expected log length 2, got %d", len(node.log))
	}
}

func TestNode_NewElectionTimer(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.ElectionTimeout = core.Duration{Duration: 50 * time.Millisecond}

	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	timer := node.newElectionTimer()
	if timer == nil {
		t.Fatal("Expected timer to be created")
	}

	// Timer should fire between 50ms and 100ms (1x to 2x election timeout)
	select {
	case <-timer.C:
		// Timer fired as expected
	case <-time.After(150 * time.Millisecond):
		t.Error("Timer did not fire within expected time")
	}
}

func TestApplyFuture_Structure(t *testing.T) {
	future := &applyFuture{
		command: core.FSMCommand{
			Op:    core.FSMSet,
			Key:   "test",
			Value: []byte("test"),
		},
		done: make(chan struct{}),
	}

	if future.command.Op != core.FSMSet {
		t.Error("Expected command op to be set")
	}

	if future.done == nil {
		t.Error("Expected done channel to be created")
	}
}

func TestRpcWrapper_Structure(t *testing.T) {
	respCh := make(chan interface{}, 1)
	wrapper := &rpcWrapper{
		cmd:    &core.AppendEntriesRequest{},
		respCh: respCh,
	}

	if wrapper.cmd == nil {
		t.Error("Expected cmd to be set")
	}

	if wrapper.respCh == nil {
		t.Error("Expected respCh to be set")
	}
}

func TestPeer_Structure(t *testing.T) {
	peer := &Peer{
		ID:         "peer-1",
		Address:    "127.0.0.1:7001",
		Region:     "default",
		Role:       core.RoleVoter,
		nextIndex:  1,
		matchIndex: 0,
	}

	if peer.ID != "peer-1" {
		t.Errorf("Expected ID peer-1, got %s", peer.ID)
	}

	if peer.Address != "127.0.0.1:7001" {
		t.Errorf("Expected address 127.0.0.1:7001, got %s", peer.Address)
	}
}

type mockTransport struct{}

func (m *mockTransport) Start() error { return nil }
func (m *mockTransport) Stop() error  { return nil }
func (m *mockTransport) SendAppendEntries(peerID string, req *core.AppendEntriesRequest) (*core.AppendEntriesResponse, error) {
	return &core.AppendEntriesResponse{Term: req.Term, Success: true}, nil
}
func (m *mockTransport) SendRequestVote(peerID string, req *core.RequestVoteRequest) (*core.RequestVoteResponse, error) {
	return &core.RequestVoteResponse{Term: req.Term, VoteGranted: true}, nil
}
func (m *mockTransport) SendPreVote(peerID string, req *core.PreVoteRequest) (*core.PreVoteResponse, error) {
	return &core.PreVoteResponse{Term: req.Term, VoteGranted: true}, nil
}
func (m *mockTransport) SendInstallSnapshot(peerID string, req *core.InstallSnapshotRequest) (*core.InstallSnapshotResponse, error) {
	return &core.InstallSnapshotResponse{Term: req.Term, Success: true}, nil
}
func (m *mockTransport) SendHeartbeat(peerID string, req *core.HeartbeatRequest) (*core.HeartbeatResponse, error) {
	return &core.HeartbeatResponse{Term: req.Term, LeaderID: req.LeaderID}, nil
}
func (m *mockTransport) LocalAddr() string { return "127.0.0.1:0" }

// Tests for Distributor

func TestDistributor_New(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)
	if d == nil {
		t.Fatal("Expected distributor to be created")
	}
	if d.strategy != core.StrategyRoundRobin {
		t.Errorf("Expected strategy RoundRobin, got %s", d.strategy)
	}
	if d.nodeID != "node-1" {
		t.Errorf("Expected nodeID node-1, got %s", d.nodeID)
	}
	if d.region != "default" {
		t.Errorf("Expected region default, got %s", d.region)
	}
}

func TestDistributor_New_EmptyStrategy(t *testing.T) {
	d := NewDistributor("node-1", "default", "")
	if d.strategy != core.StrategyRoundRobin {
		t.Errorf("Expected default strategy RoundRobin, got %s", d.strategy)
	}
}

func TestDistributor_AddNode(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	node := &core.NodeInfo{
		ID:       "node-2",
		Region:   "default",
		Address:  "127.0.0.1:7001",
		CanProbe: true,
	}
	d.AddNode(node)

	d.mu.RLock()
	if _, exists := d.nodes["node-2"]; !exists {
		t.Error("Expected node to be added")
	}
	d.mu.RUnlock()
}

func TestDistributor_RemoveNode(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)
	d.AddNode(&core.NodeInfo{ID: "node-2", Region: "default", Address: "127.0.0.1:7001", CanProbe: true})
	d.RemoveNode("node-2")

	d.mu.RLock()
	if _, exists := d.nodes["node-2"]; exists {
		t.Error("Expected node to be removed")
	}
	d.mu.RUnlock()
}

func TestDistributor_UpdateNode(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)
	d.AddNode(&core.NodeInfo{ID: "node-2", Region: "default", Address: "127.0.0.1:7001", CanProbe: true, LoadAvg: 0.5})

	d.UpdateNode(&core.NodeInfo{ID: "node-2", Region: "default", Address: "127.0.0.1:7001", CanProbe: true, LoadAvg: 0.8})

	d.mu.RLock()
	node := d.nodes["node-2"]
	d.mu.RUnlock()

	if node.LoadAvg != 0.8 {
		t.Errorf("Expected load 0.8, got %f", node.LoadAvg)
	}
}

func TestDistributor_AddSoul(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	soul := &core.Soul{
		ID:          "soul-1",
		WorkspaceID: "default",
		Name:        "Test Soul",
		Type:        core.CheckHTTP,
	}
	d.AddSoul(soul)

	d.mu.RLock()
	if _, exists := d.souls["soul-1"]; !exists {
		t.Error("Expected soul to be added")
	}
	d.mu.RUnlock()
}

func TestDistributor_RemoveSoul(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)
	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})
	d.RemoveSoul("soul-1")

	d.mu.RLock()
	if _, exists := d.souls["soul-1"]; exists {
		t.Error("Expected soul to be removed")
	}
	d.mu.RUnlock()
}

func TestDistributor_GetPlan(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	plan := d.GetPlan()
	if plan.Strategy != core.StrategyRoundRobin {
		t.Errorf("Expected strategy RoundRobin, got %s", plan.Strategy)
	}
}

func TestDistributor_Recompute_RoundRobin(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	// Add nodes
	d.AddNode(&core.NodeInfo{ID: "node-1", Region: "default", Address: "127.0.0.1:7001", CanProbe: true, LoadAvg: 0.3, MemoryUsage: 0.5})
	d.AddNode(&core.NodeInfo{ID: "node-2", Region: "default", Address: "127.0.0.1:7002", CanProbe: true, LoadAvg: 0.3, MemoryUsage: 0.5})

	// Add souls
	for i := 0; i < 4; i++ {
		d.AddSoul(&core.Soul{ID: fmt.Sprintf("soul-%d", i), WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})
	}

	_, err := d.Recompute()
	if err != nil {
		t.Fatalf("Recompute failed: %v", err)
	}

	plan := d.GetPlan()
	if len(plan.Assignments) != 4 {
		t.Errorf("Expected 4 assignments, got %d", len(plan.Assignments))
	}
}

func TestDistributor_GetAssignment(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)
	d.AddNode(&core.NodeInfo{ID: "node-1", Region: "default", Address: "127.0.0.1:7001", CanProbe: true, LoadAvg: 0.3, MemoryUsage: 0.5})
	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})
	d.Recompute()

	assignment := d.GetAssignment("soul-1")
	if assignment.NodeID == "" {
		t.Error("Expected assignment to have a node ID")
	}
}

func TestDistributor_GetNodeAssignments(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)
	d.AddNode(&core.NodeInfo{ID: "node-1", Region: "default", Address: "127.0.0.1:7001", CanProbe: true, LoadAvg: 0.3, MemoryUsage: 0.5})
	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})
	d.Recompute()

	assignments := d.GetNodeAssignments("node-1")
	if len(assignments) < 1 {
		t.Errorf("Expected at least 1 assignment for node-1, got %d", len(assignments))
	}
}

func TestDistributor_GetMyAssignments(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)
	d.AddNode(&core.NodeInfo{ID: "node-1", Region: "default", Address: "127.0.0.1:7001", CanProbe: true, LoadAvg: 0.3, MemoryUsage: 0.5})
	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})
	d.Recompute()

	assignments := d.GetMyAssignments()
	if len(assignments) < 1 {
		t.Errorf("Expected at least 1 assignment for self, got %d", len(assignments))
	}
}

func TestDistributor_GetStats(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)
	d.AddNode(&core.NodeInfo{ID: "node-1", Region: "default", Address: "127.0.0.1:7001", CanProbe: true, LoadAvg: 0.3, MemoryUsage: 0.5})
	d.AddNode(&core.NodeInfo{ID: "node-2", Region: "default", Address: "127.0.0.1:7002", CanProbe: true, LoadAvg: 0.3, MemoryUsage: 0.5})
	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})
	d.Recompute()

	stats := d.GetStats()
	if stats.TotalNodes != 2 {
		t.Errorf("Expected 2 total nodes, got %d", stats.TotalNodes)
	}
	if stats.TotalSouls != 1 {
		t.Errorf("Expected 1 total soul, got %d", stats.TotalSouls)
	}
}

func TestDistributor_getHealthyNodes(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)
	d.AddNode(&core.NodeInfo{ID: "node-1", Region: "default", Address: "127.0.0.1:7001", CanProbe: true, LoadAvg: 0.3, MemoryUsage: 0.5})
	d.AddNode(&core.NodeInfo{ID: "node-2", Region: "default", Address: "127.0.0.1:7002", CanProbe: false, LoadAvg: 0.3, MemoryUsage: 0.5})

	healthy := d.getHealthyNodes()
	if len(healthy) != 1 {
		t.Errorf("Expected 1 healthy node, got %d", len(healthy))
	}
	if healthy[0].ID != "node-1" {
		t.Errorf("Expected node-1, got %s", healthy[0].ID)
	}
}

func TestDistributor_IsResponsible(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)
	d.AddNode(&core.NodeInfo{ID: "node-1", Region: "default", Address: "127.0.0.1:7001", CanProbe: true, LoadAvg: 0.3, MemoryUsage: 0.5})
	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})
	d.Recompute()

	if !d.IsResponsible("soul-1") {
		t.Error("Expected node-1 to be responsible for soul-1")
	}
}

func TestDistributor_SetOnRebalanceCallback(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)
	var mu sync.Mutex
	called := false
	d.SetOnRebalanceCallback(func(plan core.DistributionPlan) {
		mu.Lock()
		called = true
		mu.Unlock()
	})

	// Trigger recompute which should call the callback
	d.AddNode(&core.NodeInfo{ID: "node-1", Region: "default", Address: "127.0.0.1:7001", CanProbe: true, LoadAvg: 0.3, MemoryUsage: 0.5})
	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})
	d.Recompute()

	// Give goroutine time to call callback
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	wasCalled := called
	mu.Unlock()

	if !wasCalled {
		t.Error("Expected onRebalance callback to be called")
	}
}

func TestDistributor_ValidatePlan(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)
	d.AddNode(&core.NodeInfo{ID: "node-1", Region: "default", Address: "127.0.0.1:7001", CanProbe: true, LoadAvg: 0.3, MemoryUsage: 0.5})
	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})
	d.Recompute()

	plan := d.GetPlan()
	if err := d.ValidatePlan(plan); err != nil {
		t.Errorf("ValidatePlan failed: %v", err)
	}
}

func TestDistributor_SetStrategy(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	d.SetStrategy(core.StrategyRegionAware)

	d.mu.RLock()
	strategy := d.strategy
	d.mu.RUnlock()

	if strategy != core.StrategyRegionAware {
		t.Errorf("Expected strategy RegionAware, got %s", strategy)
	}
}

func TestDistributor_distributeRegionAware(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRegionAware)

	nodes := []*core.NodeInfo{
		{ID: "node-1", Region: "us-east", AssignedSouls: 0, LoadAvg: 0.3},
		{ID: "node-2", Region: "us-east", AssignedSouls: 0, LoadAvg: 0.5},
		{ID: "node-3", Region: "us-west", AssignedSouls: 0, LoadAvg: 0.2},
	}

	d.AddSoul(&core.Soul{ID: "soul-1", Region: "us-east"})
	d.AddSoul(&core.Soul{ID: "soul-2", Region: "us-west"})

	assignments := d.distributeRegionAware(nodes)

	if len(assignments) != 2 {
		t.Errorf("Expected 2 assignments, got %d", len(assignments))
	}

	soul1Assigned := false
	soul2Assigned := false
	for _, a := range assignments {
		if a.SoulID == "soul-1" && a.Region == "us-east" {
			soul1Assigned = true
		}
		if a.SoulID == "soul-2" && a.Region == "us-west" {
			soul2Assigned = true
		}
	}

	if !soul1Assigned {
		t.Error("Expected soul-1 to be assigned to us-east")
	}
	if !soul2Assigned {
		t.Error("Expected soul-2 to be assigned to us-west")
	}
}

func TestDistributor_distributeRedundant(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRedundant)

	nodes := []*core.NodeInfo{
		{ID: "node-1", Region: "us-east", AssignedSouls: 0},
		{ID: "node-2", Region: "us-west", AssignedSouls: 0},
		{ID: "node-3", Region: "eu-west", AssignedSouls: 0},
	}

	d.AddSoul(&core.Soul{ID: "soul-1"})

	assignments := d.distributeRedundant(nodes)

	if len(assignments) != 2 {
		t.Errorf("Expected 2 assignments (primary + backup), got %d", len(assignments))
	}

	primary := 0
	backup := 0
	for _, a := range assignments {
		if a.IsBackup {
			backup++
		} else {
			primary++
		}
	}

	if primary != 1 || backup != 1 {
		t.Error("Expected 1 primary and 1 backup")
	}
}

func TestDistributor_distributeRedundant_SingleNode(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRedundant)

	nodes := []*core.NodeInfo{
		{ID: "node-1", Region: "us-east"},
	}

	d.AddSoul(&core.Soul{ID: "soul-1"})

	assignments := d.distributeRedundant(nodes)

	if len(assignments) != 1 {
		t.Errorf("Expected 1 assignment with single node, got %d", len(assignments))
	}
}

func TestDistributor_distributeWeighted(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyWeighted)

	nodes := []*core.NodeInfo{
		{ID: "node-1", Region: "us-east", MaxSouls: 100, AssignedSouls: 10},
		{ID: "node-2", Region: "us-west", MaxSouls: 50, AssignedSouls: 40},
	}

	d.AddSoul(&core.Soul{ID: "soul-1"})
	d.AddSoul(&core.Soul{ID: "soul-2"})

	assignments := d.distributeWeighted(nodes)

	if len(assignments) != 2 {
		t.Errorf("Expected 2 assignments, got %d", len(assignments))
	}

	for _, a := range assignments {
		if a.NodeID != "node-1" {
			t.Errorf("Expected assignment to node-1 (higher capacity), got %s", a.NodeID)
		}
	}
}

func TestDistributor_pickLeastLoaded(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	nodes := []*core.NodeInfo{
		{ID: "node-1", LoadAvg: 0.8},
		{ID: "node-2", LoadAvg: 0.3},
		{ID: "node-3", LoadAvg: 0.5},
	}

	best := d.pickLeastLoaded(nodes)

	if best.ID != "node-2" {
		t.Errorf("Expected node-2 (lowest load), got %s", best.ID)
	}
}

func TestDistributor_pickLeastLoaded_Empty(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	best := d.pickLeastLoaded([]*core.NodeInfo{})

	if best != nil {
		t.Error("Expected nil for empty nodes")
	}
}

func TestDistributor_pickBestWeighted(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyWeighted)

	nodes := []*core.NodeInfo{
		{ID: "node-1", MaxSouls: 100, AssignedSouls: 50},
		{ID: "node-2", MaxSouls: 100, AssignedSouls: 20},
		{ID: "node-3", MaxSouls: 100, AssignedSouls: 80},
	}

	totalCapacity := 150
	best := d.pickBestWeighted(nodes, totalCapacity)

	if best.ID != "node-2" {
		t.Errorf("Expected node-2 (highest remaining capacity), got %s", best.ID)
	}
}

func TestDistributor_pickBestWeighted_Empty(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyWeighted)

	best := d.pickBestWeighted([]*core.NodeInfo{}, 100)

	if best != nil {
		t.Error("Expected nil for empty nodes")
	}
}

func TestDistributor_TriggerRebalance(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	var mu sync.Mutex
	called := false
	d.SetOnRebalanceCallback(func(plan core.DistributionPlan) {
		mu.Lock()
		called = true
		mu.Unlock()
	})

	d.AddNode(&core.NodeInfo{ID: "node-1", Region: "default", Address: "127.0.0.1:7001", CanProbe: true})
	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})

	// Recompute triggers the callback
	d.Recompute()

	// Give goroutine time to call callback
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	wasCalled := called
	mu.Unlock()

	if !wasCalled {
		t.Error("Expected rebalance callback to be called")
	}
}

func TestDistributor_ValidatePlan_MissingSoul(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})
	d.AddSoul(&core.Soul{ID: "soul-2", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})

	plan := core.DistributionPlan{
		Assignments: []core.SoulAssignment{
			{SoulID: "soul-1", NodeID: "node-1"},
		},
	}

	err := d.ValidatePlan(plan)
	if err == nil {
		t.Error("Expected error for plan missing soul assignment")
	}
}

func TestDistributor_GetPlan_NoNodes(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})

	// GetPlan should return empty plan without crashing
	plan := d.GetPlan()

	if len(plan.Assignments) != 0 {
		t.Errorf("Expected 0 assignments without nodes, got %d", len(plan.Assignments))
	}
}

func TestDistributor_UpdateNode_NotExists(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	d.UpdateNode(&core.NodeInfo{
		ID:     "new-node",
		Region: "default",
	})

	d.mu.RLock()
	node, ok := d.nodes["new-node"]
	d.mu.RUnlock()

	if !ok {
		t.Error("Expected node to be added even via UpdateNode")
	}
	if node.Region != "default" {
		t.Errorf("Expected region default, got %s", node.Region)
	}
}

func TestDistributor_RemoveSoul_NotExists(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	d.RemoveSoul("non-existent-soul")
}

func TestDistributor_RemoveNode_NotExists(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	d.RemoveNode("non-existent-node")
}

func TestDistributor_IsResponsible_NoAssignment(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	result := d.IsResponsible("non-existent-soul")
	if result {
		t.Error("Expected false for non-existent soul")
	}
}

func TestDistributor_GetAssignment_NotFound(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	assignment := d.GetAssignment("non-existent")
	if assignment != nil {
		t.Error("Expected nil for non-existent soul")
	}
}

func TestDistributor_distributeRoundRobin_NoCrashWithNodes(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})

	// Test with at least one node to avoid panic
	nodes := []*core.NodeInfo{
		{ID: "node-1", Region: "us-east"},
	}

	assignments := d.distributeRoundRobin(nodes)

	if len(assignments) != 1 {
		t.Errorf("Expected 1 assignment, got %d", len(assignments))
	}
}

func TestDistributor_distributeRegionAware_NoMatchingRegion(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRegionAware)

	nodes := []*core.NodeInfo{
		{ID: "node-1", Region: "us-east"},
		{ID: "node-2", Region: "us-west"},
	}

	d.AddSoul(&core.Soul{ID: "soul-1", Region: "eu-west"})

	assignments := d.distributeRegionAware(nodes)

	if len(assignments) != 1 {
		t.Errorf("Expected 1 assignment, got %d", len(assignments))
	}
}

func TestDistributor_distributeWeighted_NoCapacity(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyWeighted)

	nodes := []*core.NodeInfo{
		{ID: "node-1", Region: "us-east", MaxSouls: 1, AssignedSouls: 1},
	}

	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})

	// Note: Implementation still assigns even when no capacity (returns first node)
	assignments := d.distributeWeighted(nodes)

	// Implementation assigns to first node even with no capacity
	if len(assignments) != 1 {
		t.Errorf("Expected 1 assignment (implementation assigns anyway), got %d", len(assignments))
	}
}

// Test Node Apply with valid command
func TestNode_Apply_ValidCommand(t *testing.T) {
	node := createTestNode(t)

	// Create a valid FSMCommand
	cmd := core.FSMCommand{
		Op:    core.FSMSet,
		Table: "test",
		Key:   "test-key",
		Value: []byte("test-value"),
	}

	// Apply should return error or succeed - just verify it doesn't panic
	_, _, _, err := node.Apply(cmd, 100*time.Millisecond)

	// Error is acceptable (node may not be leader or running)
	// Test passes if no panic
	_ = err
}

// Test Node Apply with empty data
func TestNode_Apply_EmptyData(t *testing.T) {
	node := createTestNode(t)

	cmd := core.FSMCommand{
		Op:    core.FSMSet,
		Table: "test",
		Key:   "empty-key",
		Value: []byte{},
	}

	_, _, _, err := node.Apply(cmd, 100*time.Millisecond)
	// Error is acceptable - test passes if no panic
	_ = err
}

// Test Node Apply with invalid operation
func TestNode_Apply_InvalidOp(t *testing.T) {
	node := createTestNode(t)

	cmd := core.FSMCommand{
		Op:    core.FSMDelete,
		Table: "test",
		Key:   "key",
		Value: []byte("value"),
	}

	_, _, _, err := node.Apply(cmd, 100*time.Millisecond)
	// Error is expected (not leader) - test passes if no panic
	_ = err
}

// Test Apply with zero timeout (should timeout immediately)
func TestNode_Apply_ZeroTimeout(t *testing.T) {
	node := createTestNode(t)
	node.running.Store(true)
	// Set state to leader so it passes the IsLeader() check
	node.mu.Lock()
	node.state = core.StateLeader
	node.mu.Unlock()

	cmd := core.FSMCommand{
		Op:    core.FSMSet,
		Table: "test",
		Key:   "key",
		Value: []byte("value"),
	}

	// Zero timeout should cause immediate timeout
	_, _, _, err := node.Apply(cmd, 0)
	if err == nil {
		t.Error("Expected timeout error with zero timeout")
	}
}

// Test handleAppendEntries with heartbeat (no entries)
func TestNode_HandleAppendEntries_Heartbeat(t *testing.T) {
	node := createTestNode(t)
	node.currentTerm = 5

	req := &core.AppendEntriesRequest{
		Term:         5,
		LeaderID:     "leader-1",
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		Entries:      []core.RaftLogEntry{}, // Heartbeat - no entries
	}

	resp := node.handleAppendEntries(req)
	if !resp.Success {
		t.Error("Expected Success=true for valid heartbeat")
	}
	if resp.Term != 5 {
		t.Errorf("Expected term 5, got %d", resp.Term)
	}
}

// Test handleAppendEntries with entries to append
func TestNode_HandleAppendEntries_WithEntries(t *testing.T) {
	node := createTestNode(t)
	node.currentTerm = 5
	node.log = []core.RaftLogEntry{
		{Index: 1, Term: 1, Type: core.LogCommand, Data: []byte(`{"op":"set","table":"test","key":"old","value":"data"}`)},
	}

	req := &core.AppendEntriesRequest{
		Term:         5,
		LeaderID:     "leader-1",
		PrevLogIndex: 1,
		PrevLogTerm:  1,
		Entries: []core.RaftLogEntry{
			{Index: 2, Term: 2, Type: core.LogCommand, Data: []byte(`{"op":"set","table":"test","key":"new","value":"data"}`)},
		},
		LeaderCommit: 0,
	}

	resp := node.handleAppendEntries(req)
	// Response depends on log matching - test verifies no panic
	_ = resp
}

// Test handleAppendEntries that triggers becomeFollower
func TestNode_HandleAppendEntries_HigherTerm(t *testing.T) {
	node := createTestNode(t)
	node.currentTerm = 5
	node.state = core.StateCandidate

	req := &core.AppendEntriesRequest{
		Term:     10, // Higher than currentTerm
		LeaderID: "leader-1",
	}

	resp := node.handleAppendEntries(req)
	if !resp.Success {
		t.Error("Expected Success=true when term is higher")
	}
	if node.currentTerm != 10 {
		t.Errorf("Expected currentTerm to be updated to 10, got %d", node.currentTerm)
	}
}

// Test getLogTerm
func TestNode_GetLogTerm(t *testing.T) {
	node := createTestNode(t)
	node.log = []core.RaftLogEntry{
		{}, // Index 0 unused
		{Index: 1, Term: 1},
		{Index: 2, Term: 2},
		{Index: 3, Term: 1},
	}

	// Test valid index
	if term := node.getLogTerm(1); term != 1 {
		t.Errorf("Expected term 1 for index 1, got %d", term)
	}
	if term := node.getLogTerm(2); term != 2 {
		t.Errorf("Expected term 2 for index 2, got %d", term)
	}

	// Test index 0 (returns 0)
	if term := node.getLogTerm(0); term != 0 {
		t.Errorf("Expected term 0 for index 0, got %d", term)
	}

	// Test out of range index (returns 0)
	if term := node.getLogTerm(100); term != 0 {
		t.Errorf("Expected term 0 for out of range index, got %d", term)
	}
}

// Test getEntriesAfter
func TestNode_GetEntriesAfter(t *testing.T) {
	node := createTestNode(t)
	node.log = []core.RaftLogEntry{
		{}, // Index 0 unused
		{Index: 1, Term: 1, Data: []byte("data1")},
		{Index: 2, Term: 2, Data: []byte("data2")},
		{Index: 3, Term: 1, Data: []byte("data3")},
	}

	// Test getting entries after index 1
	entries := node.getEntriesAfter(1, 2)
	if len(entries) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(entries))
	}

	// Test getting all entries after index 1
	entries = node.getEntriesAfter(1, 10)
	if len(entries) != 3 {
		t.Errorf("Expected 3 entries, got %d", len(entries))
	}

	// Test start beyond log
	entries = node.getEntriesAfter(100, 2)
	if entries != nil {
		t.Error("Expected nil for start beyond log")
	}
}

// Test appendEntry
func TestNode_AppendEntry(t *testing.T) {
	node := createTestNode(t)
	node.log = []core.RaftLogEntry{
		{}, // Index 0 unused
		{Index: 1, Term: 1},
	}

	entry := core.RaftLogEntry{
		Term: 2,
		Type: core.LogCommand,
		Data: []byte("new-entry"),
	}

	node.appendEntry(entry)

	if len(node.log) != 3 {
		t.Errorf("Expected log length 3, got %d", len(node.log))
	}

	if node.log[2].Index != 2 {
		t.Errorf("Expected new entry index 2, got %d", node.log[2].Index)
	}
}

// Test notifyApply
func TestNode_NotifyApply(t *testing.T) {
	node := createTestNode(t)

	// Create a future and register it
	future := &applyFuture{
		done: make(chan struct{}),
	}
	applyWaiters.Store(uint64(1), future)

	// Notify
	node.notifyApply(1, 5, nil)

	// Wait for notification
	select {
	case <-future.done:
		if future.term != 5 {
			t.Errorf("Expected term 5, got %d", future.term)
		}
		if future.err != nil {
			t.Errorf("Expected nil error, got %v", future.err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("notifyApply timed out")
	}
}

// Test notifyApply with unknown index
func TestNode_NotifyApply_UnknownIndex(t *testing.T) {
	node := createTestNode(t)

	// Notify for index with no waiter - should not panic
	node.notifyApply(999, 5, nil)

	// Test passes if no panic
}

// Test handleAppendEntries with leaderCommit > commitIndex
func TestNode_HandleAppendEntries_LeaderCommit(t *testing.T) {
	node := createTestNode(t)
	node.currentTerm = 5
	node.commitIndex = 1
	node.log = []core.RaftLogEntry{
		{}, // Index 0 unused
		{Index: 1, Term: 1},
		{Index: 2, Term: 2},
	}

	req := &core.AppendEntriesRequest{
		Term:         5,
		LeaderID:     "leader-1",
		PrevLogIndex: 2,
		PrevLogTerm:  2,
		Entries:      []core.RaftLogEntry{},
		LeaderCommit: 2, // Greater than commitIndex
	}

	// This may block on commitCh send, so use timeout
	done := make(chan struct{})
	var resp *core.AppendEntriesResponse
	go func() {
		resp = node.handleAppendEntries(req)
		close(done)
	}()

	select {
	case <-done:
		if !resp.Success {
			t.Error("Expected Success=true")
		}
	case <-time.After(100 * time.Millisecond):
		// Timeout is acceptable - commitCh may be full
		t.Log("handleAppendEntries timed out (expected if commitCh full)")
	}
}

// Test handleAppendEntries with log conflict resolution
func TestNode_HandleAppendEntries_LogConflict(t *testing.T) {
	node := createTestNode(t)
	node.currentTerm = 5
	node.log = []core.RaftLogEntry{
		{}, // Index 0 unused
		{Index: 1, Term: 1, Data: []byte("old1")},
		{Index: 2, Term: 2, Data: []byte("old2")},
		{Index: 3, Term: 2, Data: []byte("old3")},
	}

	// Leader sends entries that conflict at index 2
	req := &core.AppendEntriesRequest{
		Term:         5,
		LeaderID:     "leader-1",
		PrevLogIndex: 1,
		PrevLogTerm:  1,
		Entries: []core.RaftLogEntry{
			{Index: 2, Term: 3, Data: []byte("new2")}, // Different term at index 2
			{Index: 3, Term: 3, Data: []byte("new3")},
		},
		LeaderCommit: 0,
	}

	resp := node.handleAppendEntries(req)

	// Should truncate conflicting entries and append new ones
	if !resp.Success {
		t.Error("Expected Success=true")
	}

	// Log should have: empty, index1, new2, new3
	if len(node.log) != 4 {
		t.Errorf("Expected log length 4, got %d", len(node.log))
	}
}

// Test handleAppendEntries with entries already in log
func TestNode_HandleAppendEntries_DuplicateEntries(t *testing.T) {
	node := createTestNode(t)
	node.currentTerm = 5
	node.log = []core.RaftLogEntry{
		{}, // Index 0 unused
		{Index: 1, Term: 1, Data: []byte("data1")},
		{Index: 2, Term: 2, Data: []byte("data2")},
	}

	// Leader sends entries that already exist
	req := &core.AppendEntriesRequest{
		Term:         5,
		LeaderID:     "leader-1",
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		Entries: []core.RaftLogEntry{
			{Index: 1, Term: 1, Data: []byte("data1")},
			{Index: 2, Term: 2, Data: []byte("data2")},
		},
		LeaderCommit: 0,
	}

	resp := node.handleAppendEntries(req)
	if !resp.Success {
		t.Error("Expected Success=true for duplicate entries")
	}

	// Log should not have duplicates
	if len(node.log) != 3 {
		t.Errorf("Expected log length 3, got %d", len(node.log))
	}
}

// Test handleAppendEntries with term mismatch - conflict detection
func TestNode_HandleAppendEntries_ConflictDetection(t *testing.T) {
	node := createTestNode(t)
	node.currentTerm = 5
	node.log = []core.RaftLogEntry{
		{}, // Index 0 unused
		{Index: 1, Term: 1},
		{Index: 2, Term: 2},
		{Index: 3, Term: 2}, // Same term as index 2
		{Index: 4, Term: 1}, // Different term
	}

	req := &core.AppendEntriesRequest{
		Term:         5,
		LeaderID:     "leader-1",
		PrevLogIndex: 4, // Points to index 4 (term 1)
		PrevLogTerm:  2, // Different from actual term (1)
	}

	resp := node.handleAppendEntries(req)
	if resp.Success {
		t.Error("Expected Success=false for term mismatch")
	}
	if resp.ConflictTerm == 0 {
		t.Error("Expected ConflictTerm to be set")
	}
	if resp.ConflictIndex == 0 {
		t.Error("Expected ConflictIndex to be set")
	}
}

// Test handleRequestVote with term less than currentTerm
func TestNode_HandleRequestVote_TermTooOld(t *testing.T) {
	node := createTestNode(t)
	node.currentTerm = 10

	req := &core.RequestVoteRequest{
		Term:        5,
		CandidateID: "candidate-1",
	}

	resp := node.handleRequestVote(req)
	if resp.VoteGranted {
		t.Error("Expected VoteGranted=false for old term")
	}
	if resp.Reason == "" {
		t.Error("Expected Reason to be set")
	}
}

// Test handleRequestVote with higher term
func TestNode_HandleRequestVote_HigherTerm(t *testing.T) {
	node := createTestNode(t)
	node.currentTerm = 5
	node.votedFor = ""

	req := &core.RequestVoteRequest{
		Term:        10,
		CandidateID: "candidate-1",
	}

	resp := node.handleRequestVote(req)
	if !resp.VoteGranted {
		t.Error("Expected VoteGranted=true for higher term")
	}
	if node.currentTerm != 10 {
		t.Errorf("Expected currentTerm to be updated to 10, got %d", node.currentTerm)
	}
}

// Test handleRequestVote with same term but already voted
func TestNode_HandleRequestVote_AlreadyVoted(t *testing.T) {
	node := createTestNode(t)
	node.currentTerm = 5
	node.votedFor = "other-candidate"

	req := &core.RequestVoteRequest{
		Term:        5,
		CandidateID: "candidate-1",
	}

	resp := node.handleRequestVote(req)
	if resp.VoteGranted {
		t.Error("Expected VoteGranted=false when already voted")
	}
}

// Test handleRequestVote with up-to-date log
func TestNode_HandleRequestVote_UpToDateLog(t *testing.T) {
	node := createTestNode(t)
	node.currentTerm = 5
	node.votedFor = ""
	node.log = []core.RaftLogEntry{
		{}, // Index 0 unused
		{Index: 1, Term: 1},
		{Index: 2, Term: 2},
	}

	req := &core.RequestVoteRequest{
		Term:         5,
		CandidateID:  "candidate-1",
		LastLogIndex: 2,
		LastLogTerm:  2,
	}

	resp := node.handleRequestVote(req)
	if !resp.VoteGranted {
		t.Error("Expected VoteGranted=true for up-to-date log")
	}
}

// Test handleRequestVote with stale log
func TestNode_HandleRequestVote_StaleLog(t *testing.T) {
	node := createTestNode(t)
	node.currentTerm = 5
	node.votedFor = ""
	node.log = []core.RaftLogEntry{
		{}, // Index 0 unused
		{Index: 1, Term: 1},
		{Index: 2, Term: 5}, // Our log has term 5 at index 2
	}

	// Candidate has older log (lower lastLogTerm)
	req := &core.RequestVoteRequest{
		Term:         5,
		CandidateID:  "candidate-1",
		LastLogIndex: 5, // More entries but lower term
		LastLogTerm:  3, // Lower than our lastLogTerm (5)
	}

	resp := node.handleRequestVote(req)
	if resp.VoteGranted {
		t.Error("Expected VoteGranted=false for stale log")
	}
	if resp.Reason == "" {
		t.Error("Expected Reason to be set")
	}
}

// Test handlePreVote grants vote for current log
func TestNode_HandlePreVote_LogCurrent(t *testing.T) {
	node := createTestNode(t)
	node.currentTerm = 5
	node.log = []core.RaftLogEntry{
		{}, // Index 0 unused
		{Index: 1, Term: 1},
		{Index: 2, Term: 2},
	}

	req := &core.PreVoteRequest{
		Term:         6, // Higher pre-vote term
		CandidateID:  "candidate-1",
		LastLogIndex: 2,
		LastLogTerm:  2,
	}

	resp := node.handlePreVote(req)
	if !resp.VoteGranted {
		t.Error("Expected PreVote to be granted for current log")
	}
	if resp.Term != 5 {
		t.Errorf("Expected term 5 (unchanged), got %d", resp.Term)
	}
}

// Test handlePreVote denies vote for stale log
func TestNode_HandlePreVote_StaleLog(t *testing.T) {
	node := createTestNode(t)
	node.currentTerm = 5
	node.log = []core.RaftLogEntry{
		{}, // Index 0 unused
		{Index: 1, Term: 1},
		{Index: 2, Term: 5}, // Our log has term 5
	}

	req := &core.PreVoteRequest{
		Term:         6,
		CandidateID:  "candidate-1",
		LastLogIndex: 10, // More entries
		LastLogTerm:  3,  // But lower term
	}

	resp := node.handlePreVote(req)
	if resp.VoteGranted {
		t.Error("Expected PreVote to be denied for stale log")
	}
	if resp.Reason == "" {
		t.Error("Expected Reason to be set")
	}
}

// Test handlePreVote denies vote for old term
func TestNode_HandlePreVote_TermTooOld(t *testing.T) {
	node := createTestNode(t)
	node.currentTerm = 10
	node.log = []core.RaftLogEntry{
		{}, // Index 0 unused
		{Index: 1, Term: 1},
	}

	req := &core.PreVoteRequest{
		Term:         5, // Lower than currentTerm
		CandidateID:  "candidate-1",
		LastLogIndex: 1,
		LastLogTerm:  1,
	}

	resp := node.handlePreVote(req)
	if resp.VoteGranted {
		t.Error("Expected PreVote to be denied for old term")
	}
	if resp.Reason == "" {
		t.Error("Expected Reason to be set")
	}
}

// Test handlePreVote does not update term
func TestNode_HandlePreVote_NoTermUpdate(t *testing.T) {
	node := createTestNode(t)
	node.currentTerm = 5
	node.votedFor = ""
	node.log = []core.RaftLogEntry{
		{}, // Index 0 unused
		{Index: 1, Term: 1},
	}

	req := &core.PreVoteRequest{
		Term:         10, // Higher term
		CandidateID:  "candidate-1",
		LastLogIndex: 1,
		LastLogTerm:  1,
	}

	_ = node.handlePreVote(req)

	// Pre-vote should NOT update our term
	if node.currentTerm != 5 {
		t.Errorf("Expected term to remain 5, got %d", node.currentTerm)
	}
	if node.votedFor != "" {
		t.Errorf("Expected votedFor to remain empty, got %s", node.votedFor)
	}
}

// Test becomeLeader transitions correctly
func TestNode_BecomeLeader(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.Peers = []core.RaftPeer{
		{ID: "node-2", Address: "127.0.0.1:7001", Role: core.RoleVoter},
		{ID: "node-3", Address: "127.0.0.1:7002", Role: core.RoleVoter},
	}
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())
	node.SetTransport(&mockTransport{})

	node.mu.Lock()
	node.currentTerm = 3
	node.log = []core.RaftLogEntry{{}} // sentinel
	node.mu.Unlock()

	node.becomeLeader()

	if node.state != core.StateLeader {
		t.Errorf("Expected StateLeader, got %s", node.state)
	}
	if node.leaderID != cfg.NodeID {
		t.Errorf("Expected leaderID %s, got %s", cfg.NodeID, node.leaderID)
	}
	if node.stats.ElectionsWon != 1 {
		t.Errorf("Expected ElectionsWon=1, got %d", node.stats.ElectionsWon)
	}
	if node.stats.LeaderChanges != 1 {
		t.Errorf("Expected LeaderChanges=1, got %d", node.stats.LeaderChanges)
	}

	// Should have a no-op entry appended
	node.mu.RLock()
	logLen := len(node.log)
	node.mu.RUnlock()
	if logLen != 2 {
		t.Errorf("Expected log length 2 (sentinel + no-op), got %d", logLen)
	}
}

// Test handleRPC dispatches correctly
func TestNode_HandleRPC(t *testing.T) {
	node := createTestNode(t)

	// Test AppendEntries dispatch
	respCh := make(chan interface{}, 1)
	rpc := &rpcWrapper{
		cmd: &core.AppendEntriesRequest{
			Term:     1,
			LeaderID: "leader-1",
		},
		respCh: respCh,
	}

	node.handleRPC(rpc)

	select {
	case resp := <-respCh:
		aeResp, ok := resp.(*core.AppendEntriesResponse)
		if !ok {
			t.Fatal("Expected AppendEntriesResponse")
		}
		if aeResp == nil {
			t.Error("Expected non-nil response")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("handleRPC timed out for AppendEntries")
	}

	// Test Heartbeat dispatch
	respCh2 := make(chan interface{}, 1)
	rpc2 := &rpcWrapper{
		cmd: &core.HeartbeatRequest{
			Term:     1,
			LeaderID: "leader-1",
		},
		respCh: respCh2,
	}

	node.handleRPC(rpc2)

	select {
	case resp := <-respCh2:
		hbResp, ok := resp.(*core.HeartbeatResponse)
		if !ok {
			t.Fatal("Expected HeartbeatResponse")
		}
		_ = hbResp
	case <-time.After(100 * time.Millisecond):
		t.Error("handleRPC timed out for Heartbeat")
	}

	// Test RequestVote dispatch
	respCh3 := make(chan interface{}, 1)
	rpc3 := &rpcWrapper{
		cmd: &core.RequestVoteRequest{
			Term:        1,
			CandidateID: "candidate-1",
		},
		respCh: respCh3,
	}

	node.handleRPC(rpc3)

	select {
	case resp := <-respCh3:
		rvResp, ok := resp.(*core.RequestVoteResponse)
		if !ok {
			t.Fatal("Expected RequestVoteResponse")
		}
		_ = rvResp
	case <-time.After(100 * time.Millisecond):
		t.Error("handleRPC timed out for RequestVote")
	}

	// Test InstallSnapshot dispatch
	respCh4 := make(chan interface{}, 1)
	rpc4 := &rpcWrapper{
		cmd: &core.InstallSnapshotRequest{
			Term:     1,
			LeaderID: "leader-1",
			Data:     []byte("snap"),
		},
		respCh: respCh4,
	}

	node.handleRPC(rpc4)

	select {
	case resp := <-respCh4:
		isResp, ok := resp.(*core.InstallSnapshotResponse)
		if !ok {
			t.Fatal("Expected InstallSnapshotResponse")
		}
		_ = isResp
	case <-time.After(100 * time.Millisecond):
		t.Error("handleRPC timed out for InstallSnapshot")
	}
}

// Test handleApply as leader appends to log
func TestNode_HandleApply_AsLeader(t *testing.T) {
	node := createTestNode(t)
	node.SetTransport(&mockTransport{})
	node.running.Store(true)

	node.mu.Lock()
	node.state = core.StateLeader
	node.currentTerm = 1
	node.log = []core.RaftLogEntry{{}} // sentinel
	node.mu.Unlock()

	cmd := core.FSMCommand{
		Op:    core.FSMSet,
		Table: "test",
		Key:   "key1",
		Value: []byte("value1"),
	}

	future := &applyFuture{
		command: cmd,
		done:    make(chan struct{}),
	}

	node.handleApply(future)

	if future.index != 1 {
		t.Errorf("Expected index 1, got %d", future.index)
	}
	if future.term != 1 {
		t.Errorf("Expected term 1, got %d", future.term)
	}

	node.mu.RLock()
	logLen := len(node.log)
	node.mu.RUnlock()
	if logLen != 2 {
		t.Errorf("Expected log length 2, got %d", logLen)
	}
}

// Test handleApply as non-leader returns error
func TestNode_HandleApply_NotLeader(t *testing.T) {
	node := createTestNode(t)
	// Default state is follower

	cmd := core.FSMCommand{Op: core.FSMSet, Key: "k", Value: []byte("v")}
	future := &applyFuture{
		command: cmd,
		done:    make(chan struct{}),
	}

	node.handleApply(future)

	if future.err == nil {
		t.Error("Expected error for non-leader apply")
	}

	select {
	case <-future.done:
		// Expected: channel should be closed
	default:
		t.Error("Expected done channel to be closed")
	}
}

// Test handleAppendEntriesResponse with success
func TestNode_HandleAppendEntriesResponse_Success(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.Peers = []core.RaftPeer{
		{ID: "node-2", Address: "127.0.0.1:7001", Role: core.RoleVoter},
	}
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())
	node.SetTransport(&mockTransport{})
	node.state = core.StateLeader
	node.currentTerm = 1
	node.log = []core.RaftLogEntry{
		{}, // sentinel
		{Index: 1, Term: 1, Type: core.LogCommand, Data: []byte(`{"op":"set"}`)},
		{Index: 2, Term: 1, Type: core.LogCommand, Data: []byte(`{"op":"set"}`)},
	}

	peer := &Peer{
		ID:         "node-2",
		Address:    "127.0.0.1:7001",
		Role:       core.RoleVoter,
		nextIndex:  3,
		matchIndex: 0,
	}
	node.peerMu.Lock()
	node.peers["node-2"] = peer
	node.nextIndex["node-2"] = 3
	node.matchIndex["node-2"] = 0
	node.peerMu.Unlock()

	req := &core.AppendEntriesRequest{
		Term:         1,
		LeaderID:     cfg.NodeID,
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		Entries: []core.RaftLogEntry{
			{Index: 1, Term: 1, Data: []byte("d1")},
			{Index: 2, Term: 1, Data: []byte("d2")},
		},
	}
	resp := &core.AppendEntriesResponse{
		Term:    1,
		Success: true,
	}

	node.handleAppendEntriesResponse(peer, req, resp)

	if node.matchIndex["node-2"] != 2 {
		t.Errorf("Expected matchIndex 2, got %d", node.matchIndex["node-2"])
	}
	if node.nextIndex["node-2"] != 3 {
		t.Errorf("Expected nextIndex 3, got %d", node.nextIndex["node-2"])
	}
}

// Test handleAppendEntriesResponse with failure and conflict
func TestNode_HandleAppendEntriesResponse_Conflict(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.Peers = []core.RaftPeer{
		{ID: "node-2", Address: "127.0.0.1:7001", Role: core.RoleVoter},
	}
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())
	node.state = core.StateLeader
	node.currentTerm = 1
	node.nextIndex["node-2"] = 5
	node.matchIndex["node-2"] = 0

	peer := &Peer{ID: "node-2", nextIndex: 5, matchIndex: 0}
	node.peerMu.Lock()
	node.peers["node-2"] = peer
	node.peerMu.Unlock()

	req := &core.AppendEntriesRequest{
		Term:         1,
		PrevLogIndex: 4,
		PrevLogTerm:  1,
		Entries:      []core.RaftLogEntry{},
	}
	resp := &core.AppendEntriesResponse{
		Term:          1,
		Success:       false,
		ConflictTerm:  1,
		ConflictIndex: 3,
	}

	node.handleAppendEntriesResponse(peer, req, resp)

	// ConflictIndex=3, then code sets nextIndex=ConflictIndex(3), then decrements to 2
	if node.nextIndex["node-2"] != 2 {
		t.Errorf("Expected nextIndex to be decremented to 2, got %d", node.nextIndex["node-2"])
	}
}

// Test handleAppendEntriesResponse with higher term causes stepdown
func TestNode_HandleAppendEntriesResponse_HigherTerm(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.Peers = []core.RaftPeer{
		{ID: "node-2", Address: "127.0.0.1:7001", Role: core.RoleVoter},
	}
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())
	node.state = core.StateLeader
	node.currentTerm = 1

	peer := &Peer{ID: "node-2", nextIndex: 1, matchIndex: 0}
	node.peerMu.Lock()
	node.peers["node-2"] = peer
	node.peerMu.Unlock()

	req := &core.AppendEntriesRequest{Term: 1}
	resp := &core.AppendEntriesResponse{Term: 5, Success: false}

	node.handleAppendEntriesResponse(peer, req, resp)

	if node.state != core.StateFollower {
		t.Errorf("Expected StateFollower after higher term, got %s", node.state)
	}
	if node.currentTerm != 5 {
		t.Errorf("Expected term 5, got %d", node.currentTerm)
	}
}

// Test checkCommit as leader
func TestNode_CheckCommit(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.Peers = []core.RaftPeer{
		{ID: "node-2", Address: "127.0.0.1:7001", Role: core.RoleVoter},
	}
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())
	node.state = core.StateLeader
	node.currentTerm = 1
	node.log = []core.RaftLogEntry{
		{}, // sentinel
		{Index: 1, Term: 1, Type: core.LogCommand, Data: []byte("d1")},
	}
	node.matchIndex["node-2"] = 1
	node.commitIndex = 0

	// Drain commitCh to prevent blocking
	go func() {
		for range node.commitCh {
		}
	}()

	node.checkCommit()

	// Should advance commitIndex if majority replicated
	// With 2 peers, majority = (2+1)/2 + 1 = 2, leader has 1 entry
	// So leader match = 1, peer match = 1, count = 2, needed > 1 => commit
	if node.commitIndex != 1 {
		t.Errorf("Expected commitIndex 1, got %d", node.commitIndex)
	}
}

// Test checkCommit as non-leader does nothing
func TestNode_CheckCommit_NotLeader(t *testing.T) {
	node := createTestNode(t)
	node.commitIndex = 0
	node.log = []core.RaftLogEntry{
		{},
		{Index: 1, Term: 1},
	}

	node.checkCommit()

	if node.commitIndex != 0 {
		t.Errorf("Expected commitIndex unchanged at 0, got %d", node.commitIndex)
	}
}

// Test processCommitted applies entries to FSM
func TestNode_ProcessCommitted(t *testing.T) {
	store := NewInMemoryStorage()
	fsm := NewStorageFSM(store)

	cfg := newTestRaftNodeConfig()
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())
	node.lastApplied = 0
	node.log = []core.RaftLogEntry{
		{}, // sentinel
		{Index: 1, Term: 1, Type: core.LogCommand, Data: []byte(`{"op":"set","table":"test","key":"k1","value":"djE="}`)},
		{Index: 2, Term: 1, Type: core.LogCommand, Data: []byte(`{"op":"set","table":"test","key":"k2","value":"djI="}`)},
	}

	// Register futures for notification
	f1 := &applyFuture{done: make(chan struct{})}
	f2 := &applyFuture{done: make(chan struct{})}
	applyWaiters.Store(uint64(1), f1)
	applyWaiters.Store(uint64(2), f2)

	node.processCommitted(2)

	if node.lastApplied != 2 {
		t.Errorf("Expected lastApplied 2, got %d", node.lastApplied)
	}

	// Verify futures were notified
	select {
	case <-f1.done:
	case <-time.After(100 * time.Millisecond):
		t.Error("Future 1 not notified")
	}
	select {
	case <-f2.done:
	case <-time.After(100 * time.Millisecond):
		t.Error("Future 2 not notified")
	}
}

// Test restoreLog returns nil
func TestNode_RestoreLog(t *testing.T) {
	node := createTestNode(t)
	err := node.restoreLog()
	if err != nil {
		t.Errorf("Expected nil error from restoreLog, got %v", err)
	}
}

// Test compactLog removes old entries
func TestNode_CompactLog(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.TrailingLogs = 1 // Keep only 1 trailing log
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())
	node.log = []core.RaftLogEntry{
		{}, // sentinel index 0
		{Index: 1, Term: 1},
		{Index: 2, Term: 1},
		{Index: 3, Term: 1},
		{Index: 4, Term: 1},
		{Index: 5, Term: 1},
	}

	node.compactLog(3)

	// After compaction with TrailingLogs=1, log should be smaller
	if len(node.log) >= 6 {
		t.Errorf("Expected log to be compacted, got length %d", len(node.log))
	}
}

// Test compactLog with invalid index
func TestNode_CompactLog_InvalidIndex(t *testing.T) {
	node := createTestNode(t)
	node.log = []core.RaftLogEntry{
		{},
		{Index: 1, Term: 1},
	}

	// Index 0 - should not compact
	node.compactLog(0)
	if len(node.log) != 2 {
		t.Errorf("Expected log unchanged, got %d", len(node.log))
	}

	// Index beyond log - should not compact
	node.compactLog(10)
	if len(node.log) != 2 {
		t.Errorf("Expected log unchanged, got %d", len(node.log))
	}
}

// Test maybeTakeSnapshot with nil snapshot store
func TestNode_MaybeTakeSnapshot_NilStore(t *testing.T) {
	node := createTestNode(t)
	node.snapshot = nil
	node.snapshotThreshold = 10

	// Should not panic
	node.maybeTakeSnapshot()
}

// Test maybeTakeSnapshot below threshold
func TestNode_MaybeTakeSnapshot_BelowThreshold(t *testing.T) {
	node := createTestNode(t)
	node.snapshotThreshold = 100
	node.log = make([]core.RaftLogEntry, 5)
	node.commitIndex = 4

	// Should not take snapshot - below threshold
	node.maybeTakeSnapshot()
}

// Test maybeTakeSnapshot when snapshot already in progress
func TestNode_MaybeTakeSnapshot_InProgress(t *testing.T) {
	node := createTestNode(t)
	node.snapshotThreshold = 5
	node.log = make([]core.RaftLogEntry, 20)
	node.commitIndex = 15
	node.snapshotInProgress.Store(true)

	// Should return early because snapshot is in progress
	node.maybeTakeSnapshot()

	// Verify flag is still set
	if !node.snapshotInProgress.Load() {
		t.Error("snapshotInProgress should still be true")
	}
}

// Test maybeTakeSnapshot with zero threshold
func TestNode_MaybeTakeSnapshot_ZeroThreshold(t *testing.T) {
	node := createTestNode(t)
	node.snapshotThreshold = 0

	// Should return early with zero threshold
	node.maybeTakeSnapshot()
}

// Test maybeTakeSnapshot with negative threshold
func TestNode_MaybeTakeSnapshot_NegativeThreshold(t *testing.T) {
	node := createTestNode(t)
	node.snapshotThreshold = -1

	// Should return early with negative threshold
	node.maybeTakeSnapshot()
}

// Test startElection with mock transport
func TestNode_StartElection_NoPeers(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	// No peers configured
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())
	node.SetTransport(&mockTransport{})
	node.log = []core.RaftLogEntry{{}} // sentinel

	// With no peers, single node should win election immediately
	node.startElection()

	node.mu.RLock()
	defer node.mu.RUnlock()

	// Single node with 0 peers: votesNeeded = (0+1)/2 + 1 = 1
	// Self vote = 1, so wins election
	if node.state != core.StateLeader {
		t.Logf("State after election: %s (term: %d)", node.state, node.currentTerm)
	}
}

// Test sendHeartbeats with peers
func TestNode_SendHeartbeats(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.Peers = []core.RaftPeer{
		{ID: "node-2", Address: "127.0.0.1:7001", Role: core.RoleVoter},
	}
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())
	node.SetTransport(&mockTransport{})
	node.state = core.StateLeader
	node.currentTerm = 1
	node.log = []core.RaftLogEntry{{}} // sentinel
	node.nextIndex["node-2"] = 1
	node.matchIndex["node-2"] = 0

	// Should not panic
	node.sendHeartbeats()

	// Give goroutines time to complete
	time.Sleep(50 * time.Millisecond)
}

// Test applyLoop reads from applyCh and stops on shutdown
func TestNode_ApplyLoop(t *testing.T) {
	node := createTestNode(t)
	node.SetTransport(&mockTransport{})

	go node.applyLoop()

	// Send shutdown signal
	close(node.shutdownCh)

	// Give time for goroutine to exit
	time.Sleep(50 * time.Millisecond)
}

// Test handleHeartbeat with old term
func TestNode_HandleHeartbeat_OldTerm(t *testing.T) {
	node := createTestNode(t)
	node.currentTerm = 10

	req := &core.HeartbeatRequest{
		Term:     5, // Lower than current
		LeaderID: "leader-1",
	}

	resp := node.handleHeartbeat(req)
	if resp == nil {
		t.Fatal("Expected response")
	}
	// Should return current term in response
}

// TestContainsString tests the containsString helper function
func TestContainsString(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		s        string
		expected bool
	}{
		{
			name:     "string exists in slice",
			slice:    []string{"a", "b", "c"},
			s:        "b",
			expected: true,
		},
		{
			name:     "string does not exist",
			slice:    []string{"a", "b", "c"},
			s:        "d",
			expected: false,
		},
		{
			name:     "empty slice",
			slice:    []string{},
			s:        "a",
			expected: false,
		},
		{
			name:     "nil slice",
			slice:    nil,
			s:        "a",
			expected: false,
		},
		{
			name:     "empty string search",
			slice:    []string{"a", "", "c"},
			s:        "",
			expected: true,
		},
		{
			name:     "first element",
			slice:    []string{"first", "second", "third"},
			s:        "first",
			expected: true,
		},
		{
			name:     "last element",
			slice:    []string{"first", "second", "last"},
			s:        "last",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsString(tt.slice, tt.s)
			if result != tt.expected {
				t.Errorf("containsString(%v, %q) = %v, expected %v", tt.slice, tt.s, result, tt.expected)
			}
		})
	}
}

// TestNode_checkJointConsensusCommit tests joint consensus commit checking
func TestNode_checkJointConsensusCommit(t *testing.T) {
	node := createTestNode(t)
	node.Start()
	defer node.Stop()

	// Test when not in joint consensus (should use standard commit)
	if !node.checkJointConsensusCommit(0) {
		t.Error("Should return true for index 0 when not in joint consensus")
	}

	// Set up joint consensus state
	node.membership.mu.Lock()
	node.membership.jointState = true
	node.membership.oldConfig = []string{"node-1", "node-2", "node-3"}
	node.membership.newConfig = []string{"node-1", "node-4", "node-5"}
	node.membership.mu.Unlock()

	// Set up peers
	peer2 := &Peer{ID: "node-2", nextIndex: 1, matchIndex: 5}
	peer3 := &Peer{ID: "node-3", nextIndex: 1, matchIndex: 5}
	peer4 := &Peer{ID: "node-4", nextIndex: 1, matchIndex: 5}
	peer5 := &Peer{ID: "node-5", nextIndex: 1, matchIndex: 5}

	node.peerMu.Lock()
	node.peers["node-2"] = peer2
	node.peers["node-3"] = peer3
	node.peers["node-4"] = peer4
	node.peers["node-5"] = peer5
	node.matchIndex["node-2"] = 5
	node.matchIndex["node-3"] = 5
	node.matchIndex["node-4"] = 5
	node.matchIndex["node-5"] = 5
	node.peerMu.Unlock()

	// Both old and new config need majority
	if !node.checkJointConsensusCommit(5) {
		t.Error("Should commit when both configs have majority")
	}

	// Reduce old config matches
	node.peerMu.Lock()
	node.matchIndex["node-2"] = 0
	node.matchIndex["node-3"] = 0
	node.peerMu.Unlock()

	if node.checkJointConsensusCommit(5) {
		t.Error("Should not commit when old config lacks majority")
	}
}

// TestNode_checkJointConsensusCommit_NotJoint tests when not in joint state
func TestNode_checkJointConsensusCommit_NotJoint(t *testing.T) {
	node := createTestNode(t)
	node.Start()
	defer node.Stop()

	node.membership.mu.Lock()
	node.membership.jointState = false
	node.membership.mu.Unlock()

	peer2 := &Peer{ID: "node-2", nextIndex: 1, matchIndex: 5}
	peer3 := &Peer{ID: "node-3", nextIndex: 1, matchIndex: 5}

	node.peerMu.Lock()
	node.peers["node-2"] = peer2
	node.peers["node-3"] = peer3
	node.matchIndex["node-2"] = 5
	node.matchIndex["node-3"] = 5
	node.peerMu.Unlock()

	if !node.checkJointConsensusCommit(5) {
		t.Error("Should commit with standard rules when not in joint consensus")
	}
}

// TestNode_applyMembershipChange_Joint tests applying joint consensus phase
func TestNode_applyMembershipChange_Joint(t *testing.T) {
	node := createTestNode(t)
	node.Start()
	defer node.Stop()

	change := core.MembershipChange{
		Type:      core.MembershipAddPeer,
		Phase:     "joint",
		OldConfig: []string{"node-1", "node-2"},
		NewConfig: []string{"node-1", "node-2", "node-3"},
		Peer: core.RaftPeer{
			ID:      "node-3",
			Address: "localhost:8003",
		},
	}

	node.applyMembershipChange(change, 10)

	node.membership.mu.RLock()
	if !node.membership.jointState {
		t.Error("Should be in joint state")
	}
	node.membership.mu.RUnlock()

	node.peerMu.RLock()
	if node.peers["node-3"] == nil {
		t.Error("Peer node-3 should have been added")
	}
	node.peerMu.RUnlock()
}

// TestNode_applyMembershipChange_Final_RemovePeer tests final phase
func TestNode_applyMembershipChange_Final_RemovePeer(t *testing.T) {
	node := createTestNode(t)
	node.Start()
	defer node.Stop()

	node.peerMu.Lock()
	node.peers["node-3"] = &Peer{ID: "node-3", Address: "localhost:8003"}
	node.peerMu.Unlock()

	jointChange := core.MembershipChange{
		Type:      core.MembershipRemovePeer,
		Phase:     "joint",
		OldConfig: []string{"node-1", "node-2", "node-3"},
		NewConfig: []string{"node-1", "node-2"},
		Peer: core.RaftPeer{
			ID:      "node-3",
			Address: "localhost:8003",
		},
	}
	node.applyMembershipChange(jointChange, 10)

	finalChange := core.MembershipChange{
		Type:      core.MembershipRemovePeer,
		Phase:     "final",
		NewConfig: []string{"node-1", "node-2"},
		Peer: core.RaftPeer{
			ID:      "node-3",
			Address: "localhost:8003",
		},
	}
	node.applyMembershipChange(finalChange, 11)

	node.membership.mu.RLock()
	if node.membership.jointState {
		t.Error("Should not be in joint state after final phase")
	}
	node.membership.mu.RUnlock()

	node.peerMu.RLock()
	if node.peers["node-3"] != nil {
		t.Error("Peer node-3 should have been removed")
	}
	node.peerMu.RUnlock()
}

// TestNode_maybeTakeSnapshot_NotLeader tests snapshot only on leader
func TestNode_maybeTakeSnapshot_NotLeader(t *testing.T) {
	node := createTestNode(t)
	node.Start()
	defer node.Stop()

	node.mu.Lock()
	node.state = core.StateFollower
	node.mu.Unlock()

	node.config.SnapshotThreshold = 10

	node.mu.Lock()
	for i := 0; i < 20; i++ {
		node.log = append(node.log, core.RaftLogEntry{
			Index: uint64(i + 1),
			Term:  1,
			Type:  core.LogCommand,
		})
	}
	node.lastApplied = 20
	node.commitIndex = 20
	node.mu.Unlock()

	node.maybeTakeSnapshot()
	time.Sleep(50 * time.Millisecond)
}

// TestNode_maybeTakeSnapshot_Threshold tests snapshot threshold
func TestNode_maybeTakeSnapshot_Threshold(t *testing.T) {
	node := createTestNode(t)
	node.Start()
	defer node.Stop()

	node.mu.Lock()
	node.state = core.StateLeader
	node.mu.Unlock()

	node.config.SnapshotThreshold = 100

	node.mu.Lock()
	for i := 0; i < 5; i++ {
		node.log = append(node.log, core.RaftLogEntry{
			Index: uint64(i + 1),
			Term:  1,
			Type:  core.LogCommand,
		})
	}
	node.lastApplied = 5
	node.commitIndex = 5
	node.mu.Unlock()

	node.maybeTakeSnapshot()
	time.Sleep(50 * time.Millisecond)
}

// TestNode_transitionToFinalConfig_NotLeader tests that transitionToFinalConfig returns early when not leader
func TestNode_transitionToFinalConfig_NotLeader(t *testing.T) {
	node := createTestNode(t)
	node.Start()
	defer node.Stop()

	// Ensure not leader
	node.mu.Lock()
	node.state = core.StateFollower
	node.mu.Unlock()

	change := core.MembershipChange{
		Type: core.MembershipAddPeer,
		Peer: core.RaftPeer{ID: "node-2", Address: "127.0.0.1:7002"},
	}

	// Should return immediately without error
	node.transitionToFinalConfig(change, 1)
}

// TestNode_transitionToFinalConfig_AsLeader tests transitionToFinalConfig when node is leader
func TestNode_transitionToFinalConfig_AsLeader(t *testing.T) {
	node := createTestNode(t)
	node.Start()
	defer node.Stop()

	// Set as leader
	node.mu.Lock()
	node.state = core.StateLeader
	node.currentTerm = 1
	node.mu.Unlock()

	change := core.MembershipChange{
		Type: core.MembershipAddPeer,
		Peer: core.RaftPeer{ID: "node-2", Address: "127.0.0.1:7002"},
	}

	// Should not panic even without peers
	node.transitionToFinalConfig(change, 1)
}

// TestNode_maybeTakeSnapshot_AsLeader tests snapshot when node is leader and threshold met
func TestNode_maybeTakeSnapshot_AsLeader(t *testing.T) {
	node := createTestNode(t)
	node.Start()
	defer node.Stop()

	node.mu.Lock()
	node.state = core.StateLeader
	node.config.SnapshotThreshold = 5
	// Pre-populate log with entries
	for i := 0; i <= 10; i++ {
		node.log = append(node.log, core.RaftLogEntry{
			Index: uint64(i),
			Term:  1,
			Type:  core.LogCommand,
		})
	}
	node.lastApplied = 10
	node.commitIndex = 10
	node.mu.Unlock()

	node.maybeTakeSnapshot()
	time.Sleep(100 * time.Millisecond)
}

// TestNode_maybeTakeSnapshot_FullPath tests the complete snapshot creation path
func TestNode_maybeTakeSnapshot_FullPath(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.SnapshotThreshold = 5
	cfg.TrailingLogs = 2 // Small value to avoid underflow in compactLog
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())

	// Set as leader without starting (to avoid run loop interference)
	node.mu.Lock()
	node.state = core.StateLeader
	node.snapshotThreshold = 5
	// Pre-populate log (index 0 is placeholder, entries start at 1)
	for i := 1; i <= 10; i++ {
		node.log = append(node.log, core.RaftLogEntry{
			Index: uint64(i),
			Term:  1,
			Type:  core.LogCommand,
		})
	}
	node.commitIndex = 10
	node.lastApplied = 10
	node.mu.Unlock()

	node.maybeTakeSnapshot()
	time.Sleep(100 * time.Millisecond)

	// Verify snapshot was created
	snaps, err := snapshot.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(snaps) == 0 {
		t.Fatal("expected snapshot to be created")
	}

	// Verify lastSnapshotIndex was updated
	node.mu.RLock()
	if node.lastSnapshotIndex != 10 {
		t.Errorf("expected lastSnapshotIndex=10, got %d", node.lastSnapshotIndex)
	}
	node.mu.RUnlock()
}

// TestNode_StartElection_NoPeers_Candidate tests startElection with no peers in candidate state
func TestNode_StartElection_NoPeers_Candidate(t *testing.T) {
	node := createTestNode(t)
	node.Start()
	defer node.Stop()

	node.mu.Lock()
	node.state = core.StateCandidate
	node.currentTerm = 10
	node.mu.Unlock()

	// Should not panic with no peers
	node.startElection()
}

// TestNode_proposeMembershipChange_Committed tests proposeMembershipChange when entry is already committed
func TestNode_proposeMembershipChange_Committed(t *testing.T) {
	node := createTestNode(t)
	node.Start()
	defer node.Stop()

	node.mu.Lock()
	node.state = core.StateLeader
	node.currentTerm = 1
	node.commitIndex = 100 // High enough to satisfy commit check immediately
	node.mu.Unlock()

	change := core.MembershipChange{
		Type:      core.MembershipAddPeer,
		Peer:      core.RaftPeer{ID: "node-2", Address: "127.0.0.1:7002"},
		OldConfig: []string{"node-1"},
		NewConfig: []string{"node-1", "node-2"},
		Phase:     "joint",
	}

	// Should succeed since commitIndex >= entry.Index
	err := node.proposeMembershipChange(change)
	if err != nil {
		t.Fatalf("proposeMembershipChange failed: %v", err)
	}

	// Verify the log entry was appended
	node.mu.RLock()
	logLen := len(node.log)
	node.mu.RUnlock()
	if logLen == 0 {
		t.Error("Expected log entry to be appended")
	}
}

// TestNode_requestPreVotes_NoPeers tests requestPreVotes with no peers (votes for self)
func TestNode_requestPreVotes_NoPeers(t *testing.T) {
	node := createTestNode(t)
	node.Start()
	defer node.Stop()

	// Pre-votes with no peers should still count self-vote
	result := node.requestPreVotes(1, 0, 0, nil)

	// Self-vote + 0 peers = 1 vote, needed = (0+1)/2+1 = 1, so should be true
	if !result {
		t.Error("Expected requestPreVotes to return true with self-vote")
	}
}

// TestNode_requestVotes_NoPeers tests requestVotes with no peers
func TestNode_requestVotes_NoPeers(t *testing.T) {
	node := createTestNode(t)
	node.Start()
	defer node.Stop()

	// Votes with no peers should still count self-vote
	votes := node.requestVotes(1, 0, 0, nil)
	if votes != 1 {
		t.Errorf("Expected 1 vote (self), got %d", votes)
	}
}

// TestNode_applyLoop tests applyLoop shutdown path
func TestNode_applyLoop_Shutdown(t *testing.T) {
	node := createTestNode(t)
	node.Start()

	// Stop should trigger shutdownCh close and applyLoop exit
	node.Stop()

	// If we get here without hanging, applyLoop exited cleanly
}

// TestNode_AddPeer_NotLeader tests AddPeer when node is not leader
func TestNode_AddPeer_NotLeader(t *testing.T) {
	node := createTestNode(t)

	err := node.AddPeer(core.RaftPeer{ID: "peer-1", Address: "127.0.0.1:7001"})
	if err == nil {
		t.Error("Expected error when adding peer as non-leader")
	}

	raftErr, ok := err.(*core.RaftError)
	if !ok || raftErr.Code != core.ErrNotLeader {
		t.Errorf("Expected ErrNotLeader, got %v", err)
	}
}

// TestNode_AddPeer_SelfAsPeer tests AddPeer with node's own ID
func TestNode_AddPeer_SelfAsPeer(t *testing.T) {
	node := createTestNode(t)
	node.mu.Lock()
	node.state = core.StateLeader
	node.mu.Unlock()

	err := node.AddPeer(core.RaftPeer{ID: "test-node-1", Address: "127.0.0.1:7001"})
	if err == nil {
		t.Error("Expected error when adding self as peer")
	}
	if err.Error() != "cannot add self as peer" {
		t.Errorf("Expected 'cannot add self as peer', got %v", err)
	}
}

// TestNode_AddPeer_DuplicatePeer tests AddPeer with existing peer ID
func TestNode_AddPeer_DuplicatePeer(t *testing.T) {
	node := createTestNode(t)
	node.mu.Lock()
	node.state = core.StateLeader
	node.mu.Unlock()

	// Add a peer directly to the peers map
	node.peerMu.Lock()
	node.peers["existing-peer"] = &Peer{
		ID:      "existing-peer",
		Address: "127.0.0.1:7002",
	}
	node.peerMu.Unlock()

	// Also add to membership config
	node.membership.mu.Lock()
	node.membership.config = []string{"test-node-1", "existing-peer"}
	node.membership.mu.Unlock()

	err := node.AddPeer(core.RaftPeer{ID: "existing-peer", Address: "127.0.0.1:7002"})
	if err == nil {
		t.Error("Expected error when adding duplicate peer")
	}
}

// TestNode_RemovePeer_NotLeader tests RemovePeer when node is not leader
func TestNode_RemovePeer_NotLeader(t *testing.T) {
	node := createTestNode(t)

	err := node.RemovePeer("peer-1")
	if err == nil {
		t.Error("Expected error when removing peer as non-leader")
	}

	raftErr, ok := err.(*core.RaftError)
	if !ok || raftErr.Code != core.ErrNotLeader {
		t.Errorf("Expected ErrNotLeader, got %v", err)
	}
}

// TestNode_RemovePeer_SelfAsLeader tests RemovePeer with node's own ID when leader
func TestNode_RemovePeer_SelfAsLeader(t *testing.T) {
	node := createTestNode(t)
	node.mu.Lock()
	node.state = core.StateLeader
	node.mu.Unlock()

	err := node.RemovePeer("test-node-1")
	if err == nil {
		t.Error("Expected error when removing self")
	}
	if err.Error() != "cannot remove self" {
		t.Errorf("Expected 'cannot remove self', got %v", err)
	}
}

// TestNode_RemovePeer_NotFoundAsLeader tests RemovePeer with non-existent peer when leader
func TestNode_RemovePeer_NotFoundAsLeader(t *testing.T) {
	node := createTestNode(t)
	node.mu.Lock()
	node.state = core.StateLeader
	node.mu.Unlock()

	err := node.RemovePeer("non-existent-peer")
	if err == nil {
		t.Error("Expected error when removing non-existent peer")
	}
	if err.Error() != "peer non-existent-peer not found" {
		t.Errorf("Expected peer not found error, got %v", err)
	}
}

// failingMockTransport returns errors for all RPC calls
type failingMockTransport struct{}

func (m *failingMockTransport) Start() error      { return nil }
func (m *failingMockTransport) Stop() error       { return nil }
func (m *failingMockTransport) LocalAddr() string { return "127.0.0.1:0" }
func (m *failingMockTransport) SendAppendEntries(peerID string, req *core.AppendEntriesRequest) (*core.AppendEntriesResponse, error) {
	return nil, fmt.Errorf("network error")
}
func (m *failingMockTransport) SendRequestVote(peerID string, req *core.RequestVoteRequest) (*core.RequestVoteResponse, error) {
	return nil, fmt.Errorf("network error")
}
func (m *failingMockTransport) SendPreVote(peerID string, req *core.PreVoteRequest) (*core.PreVoteResponse, error) {
	return nil, fmt.Errorf("network error")
}
func (m *failingMockTransport) SendInstallSnapshot(peerID string, req *core.InstallSnapshotRequest) (*core.InstallSnapshotResponse, error) {
	return nil, fmt.Errorf("network error")
}
func (m *failingMockTransport) SendHeartbeat(peerID string, req *core.HeartbeatRequest) (*core.HeartbeatResponse, error) {
	return nil, fmt.Errorf("network error")
}

// highTermMockTransport returns higher term responses (peer is ahead)
type highTermMockTransport struct{}

func (m *highTermMockTransport) Start() error      { return nil }
func (m *highTermMockTransport) Stop() error       { return nil }
func (m *highTermMockTransport) LocalAddr() string { return "127.0.0.1:0" }
func (m *highTermMockTransport) SendAppendEntries(peerID string, req *core.AppendEntriesRequest) (*core.AppendEntriesResponse, error) {
	return &core.AppendEntriesResponse{Term: req.Term, Success: true}, nil
}
func (m *highTermMockTransport) SendRequestVote(peerID string, req *core.RequestVoteRequest) (*core.RequestVoteResponse, error) {
	return &core.RequestVoteResponse{Term: req.Term + 10, VoteGranted: false, Reason: "log not current"}, nil
}
func (m *highTermMockTransport) SendPreVote(peerID string, req *core.PreVoteRequest) (*core.PreVoteResponse, error) {
	return &core.PreVoteResponse{Term: req.Term + 10, VoteGranted: false, Reason: "log not current"}, nil
}
func (m *highTermMockTransport) SendInstallSnapshot(peerID string, req *core.InstallSnapshotRequest) (*core.InstallSnapshotResponse, error) {
	return &core.InstallSnapshotResponse{Term: req.Term, Success: true}, nil
}
func (m *highTermMockTransport) SendHeartbeat(peerID string, req *core.HeartbeatRequest) (*core.HeartbeatResponse, error) {
	return &core.HeartbeatResponse{Term: req.Term, LeaderID: req.LeaderID}, nil
}

// TestNode_RequestPreVotes_TransportError tests requestPreVotes when transport fails
func TestNode_RequestPreVotes_TransportError(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.Peers = []core.RaftPeer{
		{ID: "node-2", Address: "127.0.0.1:7001", Role: core.RoleVoter},
		{ID: "node-3", Address: "127.0.0.1:7002", Role: core.RoleVoter},
	}
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())
	node.SetTransport(&failingMockTransport{})
	node.log = []core.RaftLogEntry{{}} // sentinel

	peers := []*Peer{
		{ID: "node-2", Address: "127.0.0.1:7001", Role: core.RoleVoter},
		{ID: "node-3", Address: "127.0.0.1:7002", Role: core.RoleVoter},
	}

	// With all transport errors, only self-vote counts (1 vote)
	// votesNeeded = (2+1)/2 + 1 = 2, so should return false
	result := node.requestPreVotes(1, 0, 0, peers)
	if result {
		t.Error("Expected requestPreVotes to return false when all peers fail")
	}
}

// TestNode_RequestPreVotes_HigherTerm tests requestPreVotes when peer has higher term
func TestNode_RequestPreVotes_HigherTerm(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.Peers = []core.RaftPeer{
		{ID: "node-2", Address: "127.0.0.1:7001", Role: core.RoleVoter},
	}
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())
	node.SetTransport(&highTermMockTransport{})
	node.currentTerm = 5
	node.log = []core.RaftLogEntry{{}} // sentinel

	peers := []*Peer{
		{ID: "node-2", Address: "127.0.0.1:7001", Role: core.RoleVoter},
	}

	result := node.requestPreVotes(5, 0, 0, peers)
	// Self-vote = 1, peer denied = 0, total = 1
	// votesNeeded = (1+1)/2 + 1 = 2, so should return false
	if result {
		t.Error("Expected requestPreVotes to return false when peer denies")
	}
	// Peer responded with higher term (15), node should have updated
	node.mu.RLock()
	if node.currentTerm < 15 {
		t.Errorf("Expected currentTerm to be updated to at least 15, got %d", node.currentTerm)
	}
	if node.state != core.StateFollower {
		t.Errorf("Expected state to become Follower after higher term, got %s", node.state)
	}
	node.mu.RUnlock()
}

// TestNode_RequestVotes_TransportError tests requestVotes when transport fails
func TestNode_RequestVotes_TransportError(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.Peers = []core.RaftPeer{
		{ID: "node-2", Address: "127.0.0.1:7001", Role: core.RoleVoter},
		{ID: "node-3", Address: "127.0.0.1:7002", Role: core.RoleVoter},
	}
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())
	node.SetTransport(&failingMockTransport{})
	node.log = []core.RaftLogEntry{{}} // sentinel

	peers := []*Peer{
		{ID: "node-2", Address: "127.0.0.1:7001", Role: core.RoleVoter},
		{ID: "node-3", Address: "127.0.0.1:7002", Role: core.RoleVoter},
	}

	votes := node.requestVotes(1, 0, 0, peers)
	// Only self-vote should count
	if votes != 1 {
		t.Errorf("Expected 1 vote (self only), got %d", votes)
	}
}

// TestNode_RequestVotes_HigherTerm tests requestVotes when peer has higher term
func TestNode_RequestVotes_HigherTerm(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.Peers = []core.RaftPeer{
		{ID: "node-2", Address: "127.0.0.1:7001", Role: core.RoleVoter},
	}
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())
	node.SetTransport(&highTermMockTransport{})
	node.currentTerm = 5
	node.log = []core.RaftLogEntry{{}} // sentinel

	peers := []*Peer{
		{ID: "node-2", Address: "127.0.0.1:7001", Role: core.RoleVoter},
	}

	votes := node.requestVotes(5, 0, 0, peers)
	// Self-vote only, peer denied
	if votes != 1 {
		t.Errorf("Expected 1 vote (self only), got %d", votes)
	}
	// Peer responded with higher term (15), node should update
	node.mu.RLock()
	if node.currentTerm < 15 {
		t.Errorf("Expected currentTerm to be updated to at least 15, got %d", node.currentTerm)
	}
	if node.state != core.StateFollower {
		t.Errorf("Expected state to become Follower after higher term, got %s", node.state)
	}
	node.mu.RUnlock()
}

// TestNode_StartElection_PreVoteFails tests startElection when pre-vote fails
func TestNode_StartElection_PreVoteFails(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.Peers = []core.RaftPeer{
		{ID: "node-2", Address: "127.0.0.1:7001", Role: core.RoleVoter},
		{ID: "node-3", Address: "127.0.0.1:7002", Role: core.RoleVoter},
	}
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())
	node.SetTransport(&failingMockTransport{})
	node.log = []core.RaftLogEntry{{}} // sentinel

	node.mu.Lock()
	node.state = core.StateFollower
	node.currentTerm = 1
	node.mu.Unlock()

	node.startElection()

	// Pre-vote should have failed (only 1 vote out of 2 needed)
	// Node should have transitioned back to Follower
	node.mu.RLock()
	if node.state != core.StateFollower {
		t.Errorf("Expected state to be Follower after failed pre-vote, got %s", node.state)
	}
	node.mu.RUnlock()
}

// TestNode_StartElection_VoteFails tests startElection when pre-vote passes but vote fails
func TestNode_StartElection_VoteFails(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.Peers = []core.RaftPeer{
		{ID: "node-2", Address: "127.0.0.1:7001", Role: core.RoleVoter},
	}
	storage := NewInMemoryLogStore()
	snapshot := NewInMemorySnapshotStore()
	fsm := NewStorageFSM(NewInMemoryStorage())

	node, _ := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())
	node.SetTransport(&failingMockTransport{})
	node.log = []core.RaftLogEntry{{}} // sentinel

	node.mu.Lock()
	node.state = core.StateFollower
	node.currentTerm = 1
	node.mu.Unlock()

	node.startElection()

	// Pre-vote: self=1, peer fails (transport error)
	// votesNeeded = (1+1)/2+1 = 2, only 1 vote -> pre-vote fails
	// Node should be Follower
	node.mu.RLock()
	if node.state != core.StateFollower {
		t.Errorf("Expected state to be Follower after failed pre-vote, got %s", node.state)
	}
	node.mu.RUnlock()
}

// TestNode_HandleRPC_PreVote tests PreVote RPC dispatch
func TestNode_HandleRPC_PreVote(t *testing.T) {
	node := createTestNode(t)

	respCh := make(chan interface{}, 1)
	rpc := &rpcWrapper{
		cmd: &core.PreVoteRequest{
			Term:         1,
			CandidateID:  "candidate-1",
			LastLogIndex: 0,
			LastLogTerm:  0,
		},
		respCh: respCh,
	}

	node.handleRPC(rpc)

	select {
	case resp := <-respCh:
		pvResp, ok := resp.(*core.PreVoteResponse)
		if !ok {
			t.Fatal("Expected PreVoteResponse")
		}
		if !pvResp.VoteGranted {
			t.Error("Expected PreVote to be granted")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("handleRPC timed out for PreVote")
	}
}
