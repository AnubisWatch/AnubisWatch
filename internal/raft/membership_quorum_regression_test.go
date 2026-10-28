package raft

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func membershipLogEntry(t *testing.T, index, term uint64, members ...string) core.RaftLogEntry {
	t.Helper()
	e := membershipEntry{MembershipChange: core.MembershipChange{Type: core.MembershipAddPeer, Phase: membershipPhaseSingle}}
	for _, id := range members {
		e.Members = append(e.Members, core.RaftPeer{ID: id, Address: "127.0.0.1:1", Role: core.RoleVoter})
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return core.RaftLogEntry{Index: index, Term: term, Type: core.LogMembershipChange, Data: data}
}

// A configuration takes effect when appended (not when applied), and a
// truncated configuration entry takes its configuration with it.
func TestMembershipEffectiveOnAppendAndRevertedOnTruncation(t *testing.T) {
	n := newFollowerTestNode(t, NewStorageFSM(NewInMemoryStorage()))
	self := n.nodeID
	entry := membershipLogEntry(t, 1, 2, self, "p9")
	n.handleAppendEntries(&core.AppendEntriesRequest{Term: 2, LeaderID: "p9", Entries: []core.RaftLogEntry{entry}})
	if _, ok := n.Peers()["p9"]; !ok {
		t.Fatal("appended (uncommitted) configuration not in effect")
	}
	if _, idx := n.Configuration(); idx != 1 {
		t.Fatalf("configuration index = %d, want 1", idx)
	}
	// A newer leader overwrites index 1 with a plain command.
	n.handleAppendEntries(&core.AppendEntriesRequest{Term: 3, LeaderID: "other",
		Entries: []core.RaftLogEntry{{Index: 1, Term: 3, Type: core.LogNoOp}}})
	if _, ok := n.Peers()["p9"]; ok {
		t.Fatal("truncated configuration entry still in effect")
	}
	if _, idx := n.Configuration(); idx != 0 {
		t.Fatalf("configuration index = %d after truncation, want 0", idx)
	}
}

// Only one membership change may be in flight: a second one is refused while
// the latest configuration entry is uncommitted.
func TestConcurrentMembershipChangeRefused(t *testing.T) {
	n := createTestNode(t)
	n.mu.Lock()
	n.currentTerm = 1
	n.state.Store(core.StateLeader)
	if err := n.appendEntry(membershipLogEntry(t, 0, 1, n.nodeID, "p2")); err != nil {
		t.Fatal(err)
	}
	n.refreshMembershipLocked()
	n.mu.Unlock()

	err := n.proposeMembershipChange(core.MembershipChange{Type: core.MembershipAddPeer, Peer: core.RaftPeer{ID: "p3", Address: "127.0.0.1:3"}})
	if !isRaftCode(err, ErrCodeMembershipChangePending) {
		t.Fatalf("want %s, got %v", ErrCodeMembershipChangePending, err)
	}
}

// A pending (uncommitted) configuration survives a restart: it is read back
// from the log instead of a side variable.
func TestPendingMembershipSurvivesRestart(t *testing.T) {
	logs, stable := NewInMemoryLogStore(), NewInMemoryStableStore()
	cfg := newTestRaftNodeConfig()
	cfg.ElectionTimeout = core.Duration{Duration: time.Hour}
	if err := logs.StoreLog(func() *core.RaftLogEntry { e := membershipLogEntry(t, 1, 1, cfg.NodeID, "p7", "p8"); return &e }()); err != nil {
		t.Fatal(err)
	}
	n, err := NewNodeWithStableStore(cfg, logs, stable, NewInMemorySnapshotStore(), NewStorageFSM(NewInMemoryStorage()), newTestRaftLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Start(); err != nil {
		t.Fatal(err)
	}
	defer n.Stop()
	members, idx := n.Configuration()
	if len(members) != 3 || idx != 1 {
		t.Fatalf("restarted configuration = %v at %d, want 3 members from index 1", configurationIDs(members), idx)
	}
}

// A leader whose term no voter majority acknowledged within an election
// timeout steps down and fails its pending proposals.
func TestCheckQuorumStepsDownIsolatedLeader(t *testing.T) {
	n := createTestNode(t)
	n.peers["p2"] = &Peer{ID: "p2", Role: core.RoleVoter}
	n.peers["p3"] = &Peer{ID: "p3", Role: core.RoleVoter}
	n.mu.Lock()
	n.currentTerm = 4
	n.state.Store(core.StateLeader)
	n.leaderSince = time.Now().Add(-time.Hour)
	n.mu.Unlock()

	// One voter acknowledged recently: leader + p2 is a majority.
	n.markPeerAck("p2", 4, time.Now())
	n.checkQuorum()
	if !n.IsLeader() || !n.HasQuorumContact() {
		t.Fatal("leader with a majority must stay leader")
	}

	fut := &applyFuture{command: core.FSMCommand{Op: core.FSMSet, Key: "k"}, done: make(chan struct{})}
	n.handleApply(fut)
	// Acks from an older term never count.
	n.markPeerAck("p2", 3, time.Now().Add(time.Second))
	n.peers["p2"].ackSentAt = time.Now().Add(-time.Hour)
	n.checkQuorum()
	if n.IsLeader() {
		t.Fatal("leader without majority contact did not step down")
	}
	if n.HasQuorumContact() {
		t.Fatal("stepped-down node without a leader reports quorum contact")
	}
	<-fut.done
	if !isRaftCode(fut.err, core.ErrLeadershipLost) {
		t.Fatalf("pending proposal: want ErrLeadershipLost, got %v", fut.err)
	}
}

// compactLog dropped the entries TrailingLogs should keep.
func TestCompactLogKeepsTrailingLogs(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.TrailingLogs = 3
	logs := NewInMemoryLogStore()
	n, err := NewNode(cfg, logs, NewInMemorySnapshotStore(), NewStorageFSM(NewInMemoryStorage()), newTestRaftLogger())
	if err != nil {
		t.Fatal(err)
	}
	n.mu.Lock()
	for i := 1; i <= 10; i++ {
		if err := n.appendEntry(core.RaftLogEntry{Term: 1, Type: core.LogNoOp}); err != nil {
			t.Fatal(err)
		}
	}
	n.mu.Unlock()
	n.compactLog(8)
	if n.logBase != 5 {
		t.Fatalf("logBase = %d, want 5 (entries 6..8 kept as trailing logs)", n.logBase)
	}
	for i := uint64(6); i <= 10; i++ {
		if _, ok := n.logEntry(i); !ok {
			t.Fatalf("entry %d dropped", i)
		}
	}
	var e core.RaftLogEntry
	if err := logs.GetLog(6, &e); err != nil || e.Index != 6 {
		t.Fatalf("persistent trailing entry 6 deleted: %+v, %v", e, err)
	}
	if err := logs.GetLog(5, &e); err == nil && e.Index == 5 {
		t.Fatal("persistent entry 5 (below the new base) was not compacted")
	}
}

// A real leader cut off from both followers steps down (check-quorum) and
// reports no quorum contact, instead of acting as leader indefinitely.
func TestPartitionedLeaderStepsDown(t *testing.T) {
	if testing.Short() {
		t.Skip("multi-node test")
	}
	nodes := startForwardCluster(t, 3, nil)
	leader, _ := waitForForwardLeader(t, nodes)
	if err := leader.node.VerifyLeader(2 * time.Second); err != nil {
		t.Fatalf("VerifyLeader on a healthy leader: %v", err)
	}
	if _, err := leader.node.ReadBarrier(2 * time.Second); err != nil {
		t.Fatalf("ReadBarrier on a healthy leader: %v", err)
	}
	for _, n := range nodes {
		if n != leader {
			_ = n.node.Stop()
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for leader.node.IsLeader() {
		if time.Now().After(deadline) {
			t.Fatal("isolated leader never stepped down")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if leader.node.HasQuorumContact() {
		t.Fatal("isolated node reports quorum contact")
	}
}
