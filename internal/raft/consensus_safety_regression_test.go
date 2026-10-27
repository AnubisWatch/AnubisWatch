package raft

import (
	"errors"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// A leader appends command A at index 1 in term 5; a new leader (term 6)
// overwrites index 1 with command B and commits it. The proposal of A was
// discarded, so its future must fail -- it used to report success because
// futures were resolved by index alone.
func TestStaleProposalOverwrittenByNewLeaderFails(t *testing.T) {
	n := newFollowerTestNode(t, NewStorageFSM(NewInMemoryStorage()))
	n.peers["p2"] = &Peer{ID: "p2"}
	n.peers["p3"] = &Peer{ID: "p3"}
	n.mu.Lock()
	n.currentTerm = 5
	n.state.Store(core.StateLeader)
	n.mu.Unlock()
	fut := &applyFuture{command: core.FSMCommand{Op: core.FSMSet, Table: "t", Key: "A", Value: []byte("1")}, done: make(chan struct{})}
	n.handleApply(fut)
	if fut.index != 1 {
		t.Fatalf("index=%d", fut.index)
	}
	data := []byte(`{"op":0,"table":"t","key":"B","value":"Mg=="}`)
	n.handleAppendEntries(&core.AppendEntriesRequest{Term: 6, LeaderID: "p2", LeaderCommit: 1,
		Entries: []core.RaftLogEntry{{Index: 1, Term: 6, Type: core.LogCommand, Data: data}}})
	n.processCommitted(1)
	select {
	case <-fut.done:
	case <-time.After(2 * time.Second):
		t.Fatal("future never resolved")
	}
	if !isRaftCode(fut.err, core.ErrLeadershipLost) {
		t.Fatalf("discarded proposal must fail with ErrLeadershipLost, got %v", fut.err)
	}
}

// Even without an observed step-down or truncation (e.g. a future registered
// at an index whose entry is later applied with another term), the term check
// at apply time keeps the future from succeeding.
func TestNotifyApplyRejectsTermMismatch(t *testing.T) {
	n := createTestNode(t)
	fut := &applyFuture{index: 7, term: 3, done: make(chan struct{})}
	n.applyWaiters.Store(uint64(7), fut)
	n.notifyApply(7, 4, nil)
	<-fut.done
	if !isRaftCode(fut.err, core.ErrLeadershipLost) {
		t.Fatalf("want ErrLeadershipLost, got %v", fut.err)
	}

	ok := &applyFuture{index: 8, term: 4, done: make(chan struct{})}
	n.applyWaiters.Store(uint64(8), ok)
	n.notifyApply(8, 4, nil)
	<-ok.done
	if ok.err != nil {
		t.Fatalf("matching term must succeed, got %v", ok.err)
	}
}

// Stepping down fails every pending proposal: whether it commits is now up to
// the next leader, so it may not be reported as applied.
func TestStepDownFailsPendingProposals(t *testing.T) {
	n := createTestNode(t)
	n.mu.Lock()
	n.currentTerm = 2
	n.state.Store(core.StateLeader)
	n.mu.Unlock()
	fut := &applyFuture{command: core.FSMCommand{Op: core.FSMSet, Key: "k", Value: []byte("v")}, done: make(chan struct{})}
	n.handleApply(fut)
	n.mu.Lock()
	if err := n.becomeFollower(3); err != nil {
		t.Fatal(err)
	}
	n.mu.Unlock()
	select {
	case <-fut.done:
	case <-time.After(time.Second):
		t.Fatal("pending proposal not failed on step-down")
	}
	var raftErr *core.RaftError
	if !errors.As(fut.err, &raftErr) || raftErr.Code != core.ErrLeadershipLost {
		t.Fatalf("want ErrLeadershipLost, got %v", fut.err)
	}
	if _, ok := core.LeaderUnavailable(fut.err); !ok {
		t.Fatal("ErrLeadershipLost must be classified as leader-unavailable for API callers")
	}
}

// Voters A(self),B,C; non-voters N1,N2. An entry acked only by A+N1+N2 must
// not commit: B+C are a voter majority that can elect a leader without it.
func TestNonVoterAcksDoNotCommit(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.Peers = []core.RaftPeer{
		{ID: "B", Address: "127.0.0.1:7001", Role: core.RoleVoter},
		{ID: "C", Address: "127.0.0.1:7002", Role: core.RoleVoter},
		{ID: "N1", Address: "127.0.0.1:7003", Role: core.RoleNonVoter},
		{ID: "N2", Address: "127.0.0.1:7004", Role: core.RoleNonVoter},
	}
	n, err := NewNode(cfg, NewInMemoryLogStore(), NewInMemorySnapshotStore(), NewStorageFSM(NewInMemoryStorage()), newTestRaftLogger())
	if err != nil {
		t.Fatal(err)
	}
	n.matchIndex["N1"] = 1
	n.matchIndex["N2"] = 1
	if n.checkStandardCommit(1) {
		t.Fatal("entry committed by leader + 2 non-voters; voters B,C lack it")
	}
	n.matchIndex["B"] = 1
	if !n.checkStandardCommit(1) {
		t.Fatal("leader + voter B is a majority of 3 voters")
	}
}

// Listing this node (or a peer twice) in raft.peers used to count it twice in
// the configuration, so a 3-node cluster needed 3 of 4 "members" to commit.
func TestSelfAndDuplicatePeersAreDeduplicated(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.Peers = []core.RaftPeer{
		{ID: cfg.NodeID, Address: "127.0.0.1:7000"},
		{ID: "p2", Address: "127.0.0.1:7001"},
		{ID: "p3", Address: "127.0.0.1:7002"},
		{ID: "p3", Address: "127.0.0.1:7002"},
	}
	n, err := NewNode(cfg, NewInMemoryLogStore(), NewInMemorySnapshotStore(), NewStorageFSM(NewInMemoryStorage()), newTestRaftLogger())
	if err != nil {
		t.Fatal(err)
	}
	members, _ := n.Configuration()
	if len(members) != 3 {
		t.Fatalf("configuration = %v, want 3 distinct members", configurationIDs(members))
	}
	n.matchIndex["p2"] = 1
	if !n.checkStandardCommit(1) {
		t.Fatal("leader + p2 is a majority of 3 voters")
	}
}
