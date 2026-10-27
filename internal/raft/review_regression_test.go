package raft

import (
	"fmt"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// directAppendTransport delivers AppendEntries straight to one follower node.
type directAppendTransport struct {
	*mockTransport
	follower *Node
}

func (tr *directAppendTransport) SendAppendEntries(_ string, req *core.AppendEntriesRequest) (*core.AppendEntriesResponse, error) {
	return tr.follower.handleAppendEntries(req), nil
}

func waitReplicationIdle(t *testing.T, n *Node) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		busy := false
		n.replicationInFlight.Range(func(_, _ any) bool { busy = true; return false })
		if !busy {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("replication never went idle")
}

func newDirectLeaderFollower(t *testing.T, entries int) (*Node, *Node, *Peer) {
	t.Helper()
	leader, follower := createTestNode(t), createTestNode(t)
	leader.nodeID, follower.nodeID = "leader", "follower"
	leader.currentTerm, follower.currentTerm = 2, 2
	leader.state.Store(core.StateLeader)
	for i := 0; i < entries; i++ {
		if err := leader.appendEntry(core.RaftLogEntry{Term: 1, Type: core.LogNoOp, Data: []byte(fmt.Sprintf("e%d", i+1))}); err != nil {
			t.Fatal(err)
		}
	}
	next := leader.lastLogIndexLocked() + 1
	p := &Peer{ID: "follower", Role: core.RoleVoter, nextIndex: next}
	leader.peers["follower"] = p
	leader.nextIndex["follower"] = next
	leader.matchIndex["follower"] = 0
	leader.transport = &directAppendTransport{mockTransport: &mockTransport{}, follower: follower}
	return leader, follower, p
}

// Regression (liveness): a follower whose log is shorter than the leader's
// (a new member, a node back from a long outage) is probed back one-two
// indexes per round: the rejection's MatchIndex/ConflictIndex hint is ignored
// when ConflictTerm is 0. Catching up N entries takes ~N/2 heartbeat rounds.
func TestLaggingFollowerCatchesUpWithConflictHints(t *testing.T) {
	const entries = 1000
	leader, _, p := newDirectLeaderFollower(t, entries)
	rounds := 0
	for ; rounds < 5000; rounds++ {
		leader.mu.RLock()
		match := p.matchIndex
		leader.mu.RUnlock()
		if match >= entries {
			break
		}
		leader.sendHeartbeats(0)
		waitReplicationIdle(t, leader)
	}
	// With the follower's hint (it reports its last index) the leader would
	// jump straight to index 1 and need ~entries/MaxAppendEntries (16) rounds.
	if rounds > 40 {
		t.Fatalf("follower needed %d replication rounds to catch up %d entries (MaxAppendEntries=%d); "+
			"at the default 150ms heartbeat interval a 8k-entry log takes ~10 min, longer than the 30s membership-change timeout",
			rounds, entries, leader.config.MaxAppendEntries)
	}
}

// Regression (liveness, thesis §4.1): with a cluster secret a node refuses every
// RPC from an origin outside its own configuration. A node that has not yet
// received the entry adding D therefore refuses D's AppendEntries -- even
// after D became the legitimate leader -- and can never learn the entry.
func TestTransportAcceptsLeaderOutsideLocalConfig(t *testing.T) {
	cfg := newTestRaftNodeConfig()
	cfg.NodeID = "C"
	cfg.Peers = []core.RaftPeer{{ID: "A", Address: "127.0.0.1:1"}, {ID: "B", Address: "127.0.0.1:2"}}
	c, err := NewNode(cfg, NewInMemoryLogStore(), NewInMemorySnapshotStore(), NewStorageFSM(NewInMemoryStorage()), newTestRaftLogger())
	if err != nil {
		t.Fatal(err)
	}
	// D was added by {A,B,D} (config {A,B,C,D}, majority 3) and later won an
	// election with A,B,D. C lagged and never saw the config entry.
	if !c.transportMember("D", "AppendEntries") {
		t.Fatal("C refuses AppendEntries from leader D because D is not in C's (stale) configuration; C can never catch up")
	}
}

// Regression (fail-closed gap): HasQuorumContact turns true on the first
// AppendEntries from the leader, even when this node has applied almost
// nothing of what the leader has committed. A node returning from a
// partition then plans soul ownership from its stale replicated live-member
// record until it catches up, overlapping the owners the majority assigned.
func TestQuorumContactRequiresCatchUp(t *testing.T) {
	n := createTestNode(t)
	n.currentTerm = 2
	resp := n.handleAppendEntries(&core.AppendEntriesRequest{Term: 2, LeaderID: "L", PrevLogIndex: 0, PrevLogTerm: 0,
		Entries: []core.RaftLogEntry{{Term: 2, Type: core.LogNoOp}}, LeaderCommit: 100000})
	if !resp.Success {
		t.Fatal("append rejected")
	}
	if n.HasQuorumContact() {
		n.mu.RLock()
		applied, commit := n.lastApplied, n.commitIndex
		n.mu.RUnlock()
		t.Fatalf("HasQuorumContact=true while applied=%d commit=%d and the leader has committed 100000", applied, commit)
	}
	// Once applied reaches what the leader had committed at contact, the
	// node is current.
	n2 := createTestNode(t)
	n2.currentTerm = 2
	n2.handleAppendEntries(&core.AppendEntriesRequest{Term: 2, LeaderID: "L",
		Entries: []core.RaftLogEntry{{Term: 2, Type: core.LogNoOp}}, LeaderCommit: 1})
	n2.processCommitted(1)
	if !n2.HasQuorumContact() {
		t.Fatal("caught-up follower in contact with its leader reports no quorum contact")
	}
	// A node that sees itself absent from its latest configuration (its
	// removal appended) relinquishes at once.
	e := membershipLogEntry(t, 2, 2, "L")
	n2.handleAppendEntries(&core.AppendEntriesRequest{Term: 2, LeaderID: "L", PrevLogIndex: 1, PrevLogTerm: 2,
		Entries: []core.RaftLogEntry{e}, LeaderCommit: 1})
	if n2.HasQuorumContact() {
		t.Fatal("node absent from its latest configuration still reports quorum contact")
	}
}

// Regression (latent log corruption): replicateLog (used by every membership
// change) lacks sendHeartbeats' snapshot check. For a peer whose nextIndex is
// at or below the leader's compaction base it sends PrevLogIndex=nextIndex-1
// with PrevLogTerm=0 while getEntriesAfter clamps the entries to start at
// logBase+1. An empty follower with nextIndex=1 accepts it and stores the
// leader's entry logBase+1 at index 1.
func TestReplicateLogSendsSnapshotBelowLogBase(t *testing.T) {
	leader, follower, p := newDirectLeaderFollower(t, 0)
	leader.logBase, leader.logBaseTerm = 5, 1
	leader.log = []core.RaftLogEntry{{Index: 5, Term: 1}, {Index: 6, Term: 2, Type: core.LogNoOp, Data: []byte("six")}, {Index: 7, Term: 2, Type: core.LogNoOp, Data: []byte("seven")}}
	p.nextIndex, leader.nextIndex["follower"] = 1, 1
	leader.replicateLog()
	waitReplicationIdle(t, leader)
	follower.mu.RLock()
	e, ok := follower.logEntry(1)
	follower.mu.RUnlock()
	if ok && string(e.Data) == "six" {
		t.Fatalf("follower stored leader entry 6 (%q) at index 1: log indexes shifted", e.Data)
	}
}
