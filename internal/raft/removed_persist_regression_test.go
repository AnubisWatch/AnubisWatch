package raft

import (
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// A removed node used to forget its removal on restart (the flag lived only in
// memory) and campaign against the cluster it left; and being added back did
// not clear the flag, so a re-added node never campaigned.
func TestRemovedFlagPersistsAndClearsOnReAdd(t *testing.T) {
	stable := NewInMemoryStableStore()
	newNode := func() *Node {
		n, err := NewNodeWithStableStore(newTestRaftNodeConfig(), NewInMemoryLogStore(), stable,
			NewInMemorySnapshotStore(), NewStorageFSM(NewInMemoryStorage()), newTestRaftLogger())
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	n := newNode()
	n.applyMembershipChange(core.MembershipChange{Type: core.MembershipRemovePeer, Phase: "final", Peer: core.RaftPeer{ID: n.nodeID}}, 5)
	if !n.removed.Load() {
		t.Fatal("removal not applied")
	}

	restarted := newNode()
	restarted.restoreRemoved()
	if !restarted.removed.Load() {
		t.Fatal("removal forgotten after restart")
	}

	restarted.applyMembershipChange(core.MembershipChange{Type: core.MembershipAddPeer, Phase: "joint", Peer: core.RaftPeer{ID: restarted.nodeID}}, 9)
	if restarted.removed.Load() {
		t.Fatal("re-added node still marked removed")
	}
	again := newNode()
	again.restoreRemoved()
	if again.removed.Load() {
		t.Fatal("re-add not persisted")
	}
}
