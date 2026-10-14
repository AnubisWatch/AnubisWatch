package cluster

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// The Distributor records LastHeartbeat (refreshed by RegisterNode and UpdateNodeLoad)
// and configures failoverTimeout, but for a long time nothing compared them: Healthy
// was cleared only by an explicit UnregisterNode, so a node that died silently kept
// receiving soul assignments and its existing souls were never failed over — the
// souls were then monitored by nobody, which is the product's whole job.
//
// These tests pin both halves: staleness is honoured when selecting a node, and the
// periodic tick fails a stale node's souls over.

func staleHeartbeatDistributor() *Distributor {
	return NewDistributor("distributor-1", "region-1", StrategyRoundRobin,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func staleHeartbeatSoul(id string) *core.Soul {
	return &core.Soul{ID: id, Name: id, Type: core.CheckHTTP, Target: "http://127.0.0.1:9/", Enabled: true}
}

func staleNode(id string) *NodeLoad {
	return &NodeLoad{
		NodeID: id, Region: "region-1", Healthy: true,
		LastHeartbeat: time.Now().Add(-time.Hour), // far past failoverTimeout
	}
}

func freshNode(id string) *NodeLoad {
	return &NodeLoad{NodeID: id, Region: "region-1", Healthy: true, LastHeartbeat: time.Now()}
}

func TestStaleNodeDoesNotReceiveNewAssignments(t *testing.T) {
	d := staleHeartbeatDistributor()
	d.nodeLoads["dead-node"] = staleNode("dead-node")

	nodeID, err := d.AssignSoul(staleHeartbeatSoul("soul-1"))
	if err == nil && nodeID == "dead-node" {
		t.Fatalf("a soul was assigned to a node whose last heartbeat is an hour old (failoverTimeout %v)", d.failoverTimeout)
	}
}

func TestRebalanceTickFailsOverAStaleNodesSouls(t *testing.T) {
	d := staleHeartbeatDistributor()
	d.nodeLoads["dead-node"] = staleNode("dead-node")
	d.nodeLoads["live-node"] = freshNode("live-node")
	d.soulMap["soul-1"] = "dead-node"

	d.checkAndRebalance()

	if node := d.soulMap["soul-1"]; node == "dead-node" {
		t.Fatal("a soul is still assigned to a node that stopped heartbeating, so its checks never run again")
	}
	if node := d.soulMap["soul-1"]; node != "live-node" {
		t.Fatalf("the soul was not moved to the healthy node (got %q)", node)
	}
	if d.nodeLoads["dead-node"].Healthy {
		t.Error("the node that stopped heartbeating is still marked healthy")
	}
}

// TestFreshNodesAreUnaffected is the neighbouring control: the staleness rule must
// not disturb a healthy cluster, or every rebalance tick would churn assignments.
func TestFreshNodesAreUnaffected(t *testing.T) {
	d := staleHeartbeatDistributor()
	d.nodeLoads["node-a"] = freshNode("node-a")
	d.nodeLoads["node-b"] = freshNode("node-b")

	nodeID, err := d.AssignSoul(staleHeartbeatSoul("soul-1"))
	if err != nil || nodeID == "" {
		t.Fatalf("a freshly heartbeating node must still receive souls (got %q, err %v)", nodeID, err)
	}

	before := d.soulMap["soul-1"]
	d.checkAndRebalance()
	if after := d.soulMap["soul-1"]; after != before {
		t.Errorf("a healthy cluster was disturbed by the failover sweep: %q -> %q", before, after)
	}
	if !d.nodeLoads["node-a"].Healthy || !d.nodeLoads["node-b"].Healthy {
		t.Error("the failover sweep marked a freshly heartbeating node unhealthy")
	}
}
