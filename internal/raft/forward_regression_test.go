package raft

import (
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// A follower used to refuse every write with ErrNotLeader, which the API layer
// surfaced as HTTP 500. Writes are now forwarded to the leader over the Raft
// transport (ApplyOrForward), and the follower answers only once it applied the
// entry itself. These tests run real TCPTransports on loopback.

type forwardTestNode struct {
	node      *Node
	transport *TCPTransport
	store     *InMemoryStorage
}

func reserveLoopbackAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer l.Close()
	return l.Addr().String()
}

// startForwardCluster starts n nodes over TCP; secrets[i] is node i's cluster
// secret (nil means none for every node).
func startForwardCluster(t *testing.T, n int, secrets []string) []*forwardTestNode {
	t.Helper()
	addrs := make([]string, n)
	for i := range addrs {
		addrs[i] = reserveLoopbackAddr(t)
	}
	nodes := make([]*forwardTestNode, n)
	for i := range nodes {
		cfg := core.RaftConfig{
			NodeID:           fmt.Sprintf("fwd-%d", i),
			BindAddr:         addrs[i],
			AdvertiseAddr:    addrs[i],
			Bootstrap:        i == 0,
			ElectionTimeout:  core.Duration{Duration: 200 * time.Millisecond},
			HeartbeatTimeout: core.Duration{Duration: 100 * time.Millisecond},
			CommitTimeout:    core.Duration{Duration: 20 * time.Millisecond},
			MaxAppendEntries: 64,
		}
		if secrets != nil {
			cfg.ClusterSecret = secrets[i]
		}
		for j := range addrs {
			if j != i {
				cfg.Peers = append(cfg.Peers, core.RaftPeer{ID: fmt.Sprintf("fwd-%d", j), Address: addrs[j], Role: core.RoleVoter})
			}
		}
		store := NewInMemoryStorage()
		node, err := NewNodeWithStableStore(cfg, NewInMemoryLogStore(), NewInMemoryStableStore(), NewInMemorySnapshotStore(), NewStorageFSM(store), newTestRaftLogger())
		if err != nil {
			t.Fatalf("NewNode %d: %v", i, err)
		}
		transport, err := NewTCPTransport(cfg.BindAddr, cfg.AdvertiseAddr, nil, newTestRaftLogger())
		if err != nil {
			t.Fatalf("transport %d: %v", i, err)
		}
		node.SetTransport(transport)
		if err := node.Start(); err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		nodes[i] = &forwardTestNode{node: node, transport: transport, store: store}
	}
	t.Cleanup(func() {
		for _, n := range nodes {
			_ = n.node.Stop()
		}
	})
	return nodes
}

// waitForForwardLeader waits until one node leads and every running node
// follows it.
func waitForForwardLeader(t *testing.T, nodes []*forwardTestNode) (*forwardTestNode, *forwardTestNode) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var leader *forwardTestNode
		for _, n := range nodes {
			if n.node.running.Load() && n.node.IsLeader() {
				leader = n
			}
		}
		agreed := leader != nil
		for _, n := range nodes {
			if agreed && n.node.running.Load() && n.node.Leader() != leader.node.nodeID {
				agreed = false
			}
		}
		if agreed {
			for _, n := range nodes {
				if n != leader && n.node.running.Load() {
					return leader, n
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no stable leader")
	return nil, nil
}

func TestFollowerWriteIsForwardedAndReadableLocally(t *testing.T) {
	nodes := startForwardCluster(t, 3, nil)
	_, follower := waitForForwardLeader(t, nodes)

	cmd := core.FSMCommand{Op: core.FSMSet, Key: "default/souls/forwarded", Value: []byte(`{"id":"forwarded"}`)}
	index, term, err := follower.node.ApplyOrForward(cmd, 5*time.Second)
	if err != nil {
		t.Fatalf("a follower's write was refused: %v", err)
	}
	if index == 0 || term == 0 {
		t.Fatalf("forwarded write returned no position (index %d, term %d)", index, term)
	}
	// Read-your-write: the follower returned only after applying the entry.
	if got, err := follower.store.Get(cmd.Key); err != nil || string(got) != string(cmd.Value) {
		t.Fatalf("the follower cannot read its own forwarded write: %q, %v", got, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for _, n := range nodes {
		for {
			if _, err := n.store.Get(cmd.Key); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("node %s never applied the forwarded write", n.node.nodeID)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestForwardedWriteSignedWithTheClusterSecret(t *testing.T) {
	nodes := startForwardCluster(t, 3, []string{"shared-secret", "shared-secret", "shared-secret"})
	_, follower := waitForForwardLeader(t, nodes)

	cmd := core.FSMCommand{Op: core.FSMSet, Key: "default/souls/signed", Value: []byte(`{}`)}
	if _, _, err := follower.node.ApplyOrForward(cmd, 5*time.Second); err != nil {
		t.Fatalf("a correctly signed forward was refused: %v", err)
	}
}

func TestForwardWithTheWrongSecretIsRejected(t *testing.T) {
	nodes := startForwardCluster(t, 3, []string{"leader-secret", "leader-secret", "leader-secret"})
	leader, follower := waitForForwardLeader(t, nodes)

	// The follower signs with a different key, as an attacker without the
	// secret would.
	follower.node.config.ClusterSecret = "wrong-secret"
	cmd := core.FSMCommand{Op: core.FSMSet, Key: "default/souls/forged", Value: []byte(`{}`)}
	_, _, err := follower.node.ApplyOrForward(cmd, 3*time.Second)
	if !isRaftCode(err, errCodeForwardRejected) {
		t.Fatalf("a forward signed with the wrong secret was not rejected: %v", err)
	}
	if _, getErr := leader.store.Get(cmd.Key); getErr == nil {
		t.Fatal("the leader applied a forward signed with the wrong secret")
	}
}

func TestVerifyForwardRejectsUnsignedReplayedAndStale(t *testing.T) {
	n := createTestNode(t)
	n.config.ClusterSecret = "secret"
	n.peers["peer"] = &Peer{ID: "peer"}
	secret := []byte("secret")
	signed := func(mutate func(*core.ForwardRequest)) *core.ForwardRequest {
		req := &core.ForwardRequest{Kind: core.ForwardApply, Origin: "peer", Command: &core.FSMCommand{Key: "k"},
			TimeoutMs: 1000, Timestamp: time.Now().UnixNano(), Nonce: newForwardNonce()}
		if mutate != nil {
			mutate(req)
		}
		req.MAC = forwardMAC(secret, req)
		return req
	}

	tests := []struct {
		name    string
		req     *core.ForwardRequest
		wantErr bool
	}{
		{"valid", signed(nil), false},
		{"unsigned", &core.ForwardRequest{Kind: core.ForwardApply, Origin: "peer", Nonce: "n"}, true},
		{"stale", signed(func(r *core.ForwardRequest) { r.Timestamp = time.Now().Add(-2 * forwardMaxSkew).UnixNano() }), true},
		{"not a member", signed(func(r *core.ForwardRequest) { r.Origin = "stranger" }), true},
		{"tampered", func() *core.ForwardRequest {
			r := signed(nil)
			r.Command = &core.FSMCommand{Key: "other"}
			return r
		}(), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := n.verifyForward(tt.req)
			if (err != nil) != tt.wantErr {
				t.Fatalf("verifyForward error = %v, want error %v", err, tt.wantErr)
			}
		})
	}

	replayed := signed(nil)
	if err := n.verifyForward(replayed); err != nil {
		t.Fatalf("first use rejected: %v", err)
	}
	if err := n.verifyForward(replayed); err == nil {
		t.Fatal("a replayed forward request was accepted")
	}
}

func TestFollowerWriteWithoutLeaderFailsWithNoLeader(t *testing.T) {
	nodes := startForwardCluster(t, 3, nil)
	leader, follower := waitForForwardLeader(t, nodes)

	// Stop the leader and the other follower: the survivor has no majority and
	// cannot elect anyone.
	for _, n := range nodes {
		if n != follower {
			_ = n.node.Stop()
		}
	}
	_ = leader

	start := time.Now()
	_, _, err := follower.node.ApplyOrForward(core.FSMCommand{Op: core.FSMSet, Key: "k", Value: []byte("v")}, 30*time.Second)
	var raftErr *core.RaftError
	if !errors.As(err, &raftErr) || raftErr.Code != core.ErrNoLeader {
		t.Fatalf("a write with no leader returned %v, want a RaftError with %s", err, core.ErrNoLeader)
	}
	// Bounded by the no-leader budget (5 election timeouts), not the 30s timeout.
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("the write waited %v for a leader; the no-leader budget is %v", elapsed, follower.node.noLeaderWait(30*time.Second))
	}
}

func TestFollowerLearnsLeaderAddressFromAppendEntries(t *testing.T) {
	nodes := startForwardCluster(t, 3, nil)
	leader, follower := waitForForwardLeader(t, nodes)

	// Forget the leader's address, as when the leader is not a configured peer
	// under its real ID; the next AppendEntries must teach it again.
	follower.transport.UnregisterPeer(leader.node.nodeID)
	deadline := time.Now().Add(3 * time.Second)
	for !follower.transport.HasPeer(leader.node.nodeID) {
		if time.Now().After(deadline) {
			t.Fatal("the follower never learned the leader's address from AppendEntries")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestMembershipChangeIsForwardedFromAFollower(t *testing.T) {
	nodes := startForwardCluster(t, 3, nil)
	leader, follower := waitForForwardLeader(t, nodes)

	newPeer := core.RaftPeer{ID: "fwd-new", Address: reserveLoopbackAddr(t), Role: core.RoleNonVoter}
	if err := follower.node.AddPeerOrForward(newPeer, 40*time.Second); err != nil {
		t.Fatalf("a follower could not add a peer through the leader: %v", err)
	}
	// The change replicates: every node, the follower included, knows the
	// peer and can dial it.
	deadline := time.Now().Add(5 * time.Second)
	for _, n := range nodes {
		for {
			_, known := n.node.Peers()[newPeer.ID]
			if known && n.transport.HasPeer(newPeer.ID) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("node %s never learned the added peer", n.node.nodeID)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	if err := follower.node.RemovePeerOrForward(newPeer.ID, 40*time.Second); err != nil {
		t.Fatalf("a follower could not remove a peer through the leader: %v", err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		if _, known := leader.node.Peers()[newPeer.ID]; !known {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the removed peer is still a member on the leader")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRuntimeMembershipSurvivesRestart(t *testing.T) {
	stable := NewInMemoryStableStore()
	cfg := newTestRaftNodeConfig()
	n, err := NewNodeWithStableStore(cfg, NewInMemoryLogStore(), stable, NewInMemorySnapshotStore(), NewStorageFSM(NewInMemoryStorage()), newTestRaftLogger())
	if err != nil {
		t.Fatal(err)
	}
	n.applyMembershipChange(core.MembershipChange{Type: core.MembershipAddPeer, Peer: core.RaftPeer{ID: "added", Address: "127.0.0.1:1"}, Phase: "joint"}, 1)
	n.applyMembershipChange(core.MembershipChange{Type: core.MembershipAddPeer, Peer: core.RaftPeer{ID: "added", Address: "127.0.0.1:1"}, Phase: "final"}, 2)

	restarted, err := NewNodeWithStableStore(cfg, NewInMemoryLogStore(), stable, NewInMemorySnapshotStore(), NewStorageFSM(NewInMemoryStorage()), newTestRaftLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.restorePeers(); err != nil {
		t.Fatal(err)
	}
	if _, ok := restarted.Peers()["added"]; !ok {
		t.Fatalf("a peer added at runtime was lost across a restart: %v", restarted.Peers())
	}
}
