package raft

import (
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// A member without advertise_addr answers Join with its wildcard bind
// (0.0.0.0:7946). The joiner used to record that address and dial itself.
func TestInstallJoinedMembersReplacesWildcardResponderAddress(t *testing.T) {
	n := createTestNode(t)
	n.installJoinedMembers([]core.RaftPeer{
		{ID: "leader", Address: "0.0.0.0:7946", Role: core.RoleVoter},
		{ID: "other", Address: "10.0.0.3:7946", Role: core.RoleVoter},
		{ID: "unreachable", Address: "[::]:7946", Role: core.RoleVoter},
	}, "10.0.0.1:7946")

	n.peerMu.RLock()
	defer n.peerMu.RUnlock()
	if p := n.peers["leader"]; p == nil || p.Address != "10.0.0.1:7946" {
		t.Fatalf("responder recorded as %+v, want the address it was reached at", p)
	}
	if p := n.peers["other"]; p == nil || p.Address != "10.0.0.3:7946" {
		t.Fatalf("other member = %+v", p)
	}
	// A member with an undialable address is still a voter of the cluster and
	// must count in this node's quorums (leaving it out would let the joiner
	// win an election with fewer votes than a real majority); it is only
	// never dialed at that address (syncPeersLocked registers dialable
	// addresses only).
	if _, ok := n.peers["unreachable"]; !ok {
		t.Fatal("member with an undialable address left out of the configuration")
	}
}
