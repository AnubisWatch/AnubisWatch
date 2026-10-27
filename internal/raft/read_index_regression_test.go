package raft

import (
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// HA authentication made every API request a Raft write (a barrier key) just
// to get a linearizable read; on a lagging follower each one waited out its
// timeout and the node's whole API queued behind them. A follower now asks
// the leader for its read index: no log entry, and once the call returns the
// follower has applied every write acknowledged before it.
func TestFollowerReadBarrierSeesPriorWritesWithoutAppending(t *testing.T) {
	nodes := startForwardCluster(t, 3, nil)
	leader, follower := waitForForwardLeader(t, nodes)

	cmd := core.FSMCommand{Op: core.FSMSet, Key: "default/souls/read-index", Value: []byte(`{"id":"read-index"}`)}
	if _, _, _, err := leader.node.Apply(cmd, 5*time.Second); err != nil {
		t.Fatalf("leader write: %v", err)
	}
	lastIndex := func() uint64 {
		leader.node.mu.RLock()
		defer leader.node.mu.RUnlock()
		return leader.node.lastLogIndexLocked()
	}
	before := lastIndex()

	for i := 0; i < 20; i++ {
		index, err := follower.node.ReadBarrierOrForward(5 * time.Second)
		if err != nil {
			t.Fatalf("follower read barrier: %v", err)
		}
		if index < before {
			t.Fatalf("read index %d is below the acknowledged write at %d", index, before)
		}
	}
	if got, err := follower.store.Get(cmd.Key); err != nil || string(got) != string(cmd.Value) {
		t.Fatalf("after the barrier the follower cannot read the prior write: %q, %v", got, err)
	}
	if after := lastIndex(); after != before {
		t.Fatalf("read barriers appended log entries: last index %d -> %d", before, after)
	}

	// The leader serves its own barrier the same way.
	if _, err := leader.node.ReadBarrierOrForward(5 * time.Second); err != nil {
		t.Fatalf("leader read barrier: %v", err)
	}
}
