package cluster

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// The distributor's staleness rule (LastHeartbeat vs failoverTimeout) is only safe if
// something refreshes the heartbeat of a node that is alive. Nothing in production
// used to: RegisterNode stamps a heartbeat once at startup, and UpdateNodeLoad and
// UnregisterNode have no callers, so after failoverTimeout every node looked stale
// and no soul could be assigned. The manager now drives heartbeats from Raft's own
// per-peer liveness.

func heartbeatRegressionPort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer l.Close()
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(l.Addr().(*net.TCPAddr).Port))
}

// heartbeatRegressionManager starts a clustered manager with a peer Raft never
// connects to, so the test can tell liveness-driven refreshes from blanket ones.
// failoverTimeout is set before Start: the heartbeat loop reads it from its own
// goroutine, so changing it afterwards raced with that loop.
func heartbeatRegressionManager(t *testing.T, failoverTimeout time.Duration) *Manager {
	t.Helper()

	db := newTestDB(t)
	t.Cleanup(func() { db.Close() })

	cfg := newTestRaftConfig()
	cfg.BindAddr = heartbeatRegressionPort(t)
	cfg.AdvertiseAddr = cfg.BindAddr
	cfg.Bootstrap = true
	cfg.Peers = []core.RaftPeer{{ID: "ghost-peer", Address: "127.0.0.1:1"}}

	manager, err := NewManager(core.NecropolisConfig{Enabled: true, Raft: cfg}, db, newTestLogger())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	manager.failoverTimeout = failoverTimeout
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = manager.Stop(context.Background()) })

	if manager.distributor == nil {
		t.Fatal("the manager did not create a distributor, so this test is not on the clustered path")
	}
	return manager
}

// TestLiveNodeStaysAssignablePastTheFailoverTimeout pins the producer: a node that is
// up keeps receiving souls even after the failover timeout has elapsed.
func TestLiveNodeStaysAssignablePastTheFailoverTimeout(t *testing.T) {
	// A short timeout so the test does not wait 30 seconds.
	manager := heartbeatRegressionManager(t, 30*time.Millisecond)
	time.Sleep(150 * time.Millisecond)

	soul := &core.Soul{ID: "soul-live", Name: "soul-live", Type: core.CheckHTTP,
		Target: "http://127.0.0.1:9/", Enabled: true}

	nodeID, err := manager.distributor.AssignSoul(soul)
	if err != nil || nodeID == "" {
		t.Fatalf("a live cluster node could not receive a soul after failoverTimeout (%v): (%q, %v)",
			manager.distributor.failoverTimeout, nodeID, err)
	}
}

// TestPeerRaftNeverReachedIsNotKeptAlive is the neighbouring control: the producer
// refreshes only what Raft reports as connected, so a peer that never answers still
// goes stale and its souls are failed over rather than being kept alive forever.
func TestPeerRaftNeverReachedIsNotKeptAlive(t *testing.T) {
	manager := heartbeatRegressionManager(t, 30*time.Millisecond)
	time.Sleep(150 * time.Millisecond)

	if node, ok := manager.distributor.nodeLoads["ghost-peer"]; ok && !manager.distributor.nodeStale(node) {
		t.Fatalf("a peer Raft never reached was refreshed as if it were alive (healthy=%v, last heartbeat %v ago)",
			node.Healthy, time.Since(node.LastHeartbeat))
	}
}
