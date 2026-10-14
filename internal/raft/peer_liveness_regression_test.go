package raft

import (
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Peer.lastContact is what ClusterState.Peers[].IsConnected is computed from, and it
// used to be written nowhere: every assignment in node.go was n.lastContact, the
// node's own contact with its leader. IsConnected was therefore permanently false, so
// the cluster manager's heartbeat producer refreshed only itself, live peers were
// failed over after failoverTimeout, and the cluster status API reported every peer as
// disconnected.
//
// These tests pin both directions of the liveness rule: a peer that answers our
// replication is connected to us, and a peer that contacts us (the leader) is
// connected to us.

func livenessLeaderWithPeer(t *testing.T) *Node {
	t.Helper()
	node := createTestNode(t)

	node.SetTransport(&mockTransport{}) // answers AppendEntries with Success
	node.state.Store(core.StateLeader)
	node.peerMu.Lock()
	node.peers["peer-1"] = &Peer{ID: "peer-1", Address: "127.0.0.1:1"}
	node.peerMu.Unlock()
	node.mu.Lock()
	node.matchIndex["peer-1"] = 0
	node.nextIndex["peer-1"] = 1
	node.mu.Unlock()
	return node
}

func peerIsConnected(node *Node, peerID string) bool {
	for _, p := range node.GetState().Peers {
		if p.ID == peerID {
			return p.IsConnected
		}
	}
	return false
}

// TestPeerAnsweringReplicationIsConnected pins the leader's view, through the real
// replication path rather than by calling the response handler directly.
func TestPeerAnsweringReplicationIsConnected(t *testing.T) {
	node := livenessLeaderWithPeer(t)
	node.replicateLog()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if peerIsConnected(node, "peer-1") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("a peer that answered AppendEntries is still reported as disconnected")
}

// TestPeerContactingUsIsConnected pins the other direction, which is what a follower
// needs in order to see its leader as alive.
func TestPeerContactingUsIsConnected(t *testing.T) {
	t.Run("heartbeat", func(t *testing.T) {
		node := createTestNode(t)
		node.peerMu.Lock()
		node.peers["leader-1"] = &Peer{ID: "leader-1", Address: "127.0.0.1:2"}
		node.peerMu.Unlock()

		node.handleHeartbeat(&core.HeartbeatRequest{Term: node.CurrentTerm(), LeaderID: "leader-1"})

		if !peerIsConnected(node, "leader-1") {
			t.Fatal("a follower did not record the leader that just sent it a heartbeat")
		}
	})

	t.Run("append entries", func(t *testing.T) {
		node := createTestNode(t)
		node.peerMu.Lock()
		node.peers["leader-1"] = &Peer{ID: "leader-1", Address: "127.0.0.1:2"}
		node.peerMu.Unlock()

		node.handleAppendEntries(&core.AppendEntriesRequest{
			Term: node.CurrentTerm(), LeaderID: "leader-1",
		})

		if !peerIsConnected(node, "leader-1") {
			t.Fatal("a follower did not record the leader that just replicated to it")
		}
	})
}

// TestUnknownSenderIsNotTracked is the control: contact from something that is not a
// known peer must not invent a peer entry.
func TestUnknownSenderIsNotTracked(t *testing.T) {
	node := createTestNode(t)
	before := len(node.GetState().Peers)

	node.handleHeartbeat(&core.HeartbeatRequest{Term: node.CurrentTerm(), LeaderID: "stranger"})

	if after := len(node.GetState().Peers); after != before {
		t.Fatalf("contact from an unknown sender created a peer entry (%d -> %d)", before, after)
	}
}
