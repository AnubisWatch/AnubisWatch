package raft

// Real chaos tests for Raft consensus
// These tests verify cluster resilience under various failure scenarios

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// skipUnlessChaosEnv skips multi-node chaos tests unless explicitly enabled.
// These tests bind real localhost TCP ports and are sensitive to the network
// environment; they must be opted into rather than silently skipped so that
// CI jobs running them cannot pass as a no-op.
func skipUnlessChaosEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("ANUBIS_RUN_CHAOS_TESTS") == "" {
		t.Skip("Set ANUBIS_RUN_CHAOS_TESTS=1 to run multi-node chaos tests (requires open localhost networking)")
	}
}

// chaosStoreBundle holds persistent stores for a single node so a
// Start/Stop/Start restart cycle preserves durable state.
type chaosStoreBundle struct {
	logStore  *InMemoryLogStore
	snapStore *InMemorySnapshotStore
	stable    *InMemoryStableStore
	storage   *InMemoryStorage
	fsm       *StorageFSM
}

// createChaosTestCluster creates a multi-node cluster for chaos testing
func createChaosTestCluster(t *testing.T, nodeCount int) ([]*Node, func()) {
	nodes := make([]*Node, nodeCount)
	transports := make([]*TCPTransport, nodeCount)
	cleanups := make([]func(), nodeCount)

	for i := 0; i < nodeCount; i++ {
		cfg := core.RaftConfig{
			NodeID:           fmt.Sprintf("chaos-node-%d", i),
			BindAddr:         fmt.Sprintf("127.0.0.1:%d", 17000+i),
			AdvertiseAddr:    fmt.Sprintf("127.0.0.1:%d", 17000+i),
			Bootstrap:        i == 0, // First node bootstraps
			ElectionTimeout:  core.Duration{Duration: 200 * time.Millisecond},
			HeartbeatTimeout: core.Duration{Duration: 100 * time.Millisecond},
			CommitTimeout:    core.Duration{Duration: 50 * time.Millisecond},
			MaxAppendEntries: 64,
		}

		// Every node gets the full static membership (excluding itself) so the
		// bootstrap leader knows its followers and replicates heartbeats to them.
		peers := make([]core.RaftPeer, 0, nodeCount-1)
		for j := 0; j < nodeCount; j++ {
			if j == i {
				continue
			}
			peers = append(peers, core.RaftPeer{
				ID:      fmt.Sprintf("chaos-node-%d", j),
				Address: fmt.Sprintf("127.0.0.1:%d", 17000+j),
				Region:  "default",
				Role:    core.RoleVoter,
			})
		}
		cfg.Peers = peers

		storage := NewInMemoryLogStore()
		snapshot := NewInMemorySnapshotStore()
		fsm := NewStorageFSM(NewInMemoryStorage())

		node, err := NewNode(cfg, storage, snapshot, fsm, newTestRaftLogger())
		if err != nil {
			t.Fatalf("Failed to create chaos test node %d: %v", i, err)
		}

		// Create and set transport
		transport, err := NewTCPTransport(cfg.BindAddr, cfg.AdvertiseAddr, nil, newTestRaftLogger())
		if err != nil {
			t.Fatalf("Failed to create transport for node %d: %v", i, err)
		}
		transports[i] = transport
		node.SetTransport(transport)

		nodes[i] = node
		cleanups[i] = func(n *Node, tr *TCPTransport) func() {
			return func() {
				n.Stop()
				tr.Stop()
			}
		}(node, transport)
	}

	cleanup := func() {
		for _, c := range cleanups {
			c()
		}
	}

	return nodes, cleanup
}

// waitForLeader waits for a leader to be elected in the cluster
func waitForLeader(t *testing.T, nodes []*Node, timeout time.Duration) *Node {
	deadline := time.After(timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			t.Fatalf("Timeout waiting for leader election after %v", timeout)
			return nil
		case <-ticker.C:
			for _, node := range nodes {
				if node.IsLeader() {
					return node
				}
			}
		}
	}
}

// waitForAllNodes waits for all nodes to see the leader
func waitForAllNodes(t *testing.T, nodes []*Node, timeout time.Duration) {
	deadline := time.After(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			t.Fatalf("Timeout waiting for all nodes to sync")
		case <-ticker.C:
			allHaveLeader := true
			for _, node := range nodes {
				if node.LeaderID() == "" {
					allHaveLeader = false
					break
				}
			}
			if allHaveLeader {
				return
			}
		}
	}
}

// waitForStableLeader waits until exactly one running node is leader and it
// stays leader across two consecutive polls, tolerating election churn.
func waitForStableLeader(t *testing.T, nodes []*Node, timeout time.Duration) *Node {
	t.Helper()
	deadline := time.After(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var previous *Node
	for {
		select {
		case <-deadline:
			t.Fatalf("Timeout waiting for stable leader after %v", timeout)
			return nil
		case <-ticker.C:
			var current *Node
			count := 0
			for _, node := range nodes {
				if node.running.Load() && node.IsLeader() {
					current = node
					count++
				}
			}
			if count == 1 && current == previous {
				return current
			}
			if count == 1 {
				previous = current
			} else {
				previous = nil
			}
		}
	}
}

// countLeaders returns the number of nodes that think they are leader
func countLeaders(nodes []*Node) int {
	count := 0
	for _, node := range nodes {
		if node.IsLeader() {
			count++
		}
	}
	return count
}

// countActiveNodes returns the number of running nodes
func countActiveNodes(nodes []*Node) int {
	count := 0
	for _, node := range nodes {
		if node.running.Load() {
			count++
		}
	}
	return count
}

// TestChaos_SingleNodeFailure_Real tests cluster survives single node failure
func TestChaos_SingleNodeFailure_Real(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping chaos test in short mode")
	}

	skipUnlessChaosEnv(t)

	nodes, cleanup := createChaosTestCluster(t, 5)
	defer cleanup()

	// Start all nodes
	for i, node := range nodes {
		if err := node.Start(); err != nil {
			t.Fatalf("Failed to start node %d: %v", i, err)
		}
	}

	// Wait for leader election
	leader := waitForLeader(t, nodes, 10*time.Second)
	t.Logf("Leader elected: %s", leader.nodeID)

	// Wait for all nodes to see the leader
	waitForAllNodes(t, nodes, 10*time.Second)

	// Find a non-leader node to kill
	killIndex := -1
	for i, node := range nodes {
		if !node.IsLeader() {
			killIndex = i
			break
		}
	}
	if killIndex == -1 {
		t.Fatal("Could not find non-leader node to kill")
	}

	t.Logf("Killing node: %s", nodes[killIndex].nodeID)
	nodes[killIndex].Stop()

	// The cluster may churn through an election after losing a member;
	// wait until it converges on exactly one leader rather than sampling once.
	newLeader := waitForStableLeader(t, nodes, 20*time.Second)
	t.Logf("Leader after kill: %s", newLeader.nodeID)

	t.Log("✅ Single node failure test passed")
}

// TestChaos_LeaderFailure_Real tests cluster survives leader failure
func TestChaos_LeaderFailure_Real(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping chaos test in short mode")
	}

	skipUnlessChaosEnv(t)

	nodes, cleanup := createChaosTestCluster(t, 5)
	defer cleanup()

	// Start all nodes
	for i, node := range nodes {
		if err := node.Start(); err != nil {
			t.Fatalf("Failed to start node %d: %v", i, err)
		}
	}

	// Wait for leader election
	oldLeader := waitForLeader(t, nodes, 5*time.Second)
	t.Logf("Initial leader: %s", oldLeader.nodeID)

	// Wait for stability
	waitForAllNodes(t, nodes, 3*time.Second)

	// Kill the leader
	t.Logf("Killing leader: %s", oldLeader.nodeID)
	oldLeader.Stop()

	// Wait for a new stable leader among the still-running nodes: the stopped
	// node keeps its last state, so it must be excluded from the scan.
	newLeader := waitForStableLeader(t, nodes, 20*time.Second)
	t.Logf("New leader elected: %s", newLeader.nodeID)

	if newLeader.nodeID == oldLeader.nodeID {
		t.Error("New leader should be different from old leader")
	}

	t.Log("✅ Leader failure test passed")
}

// TestChaos_MultipleNodeFailures_Real tests cluster survives losing quorum
func TestChaos_MultipleNodeFailures_Real(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping chaos test in short mode")
	}

	skipUnlessChaosEnv(t)

	// 5 nodes, kill 2 (keep quorum)
	nodes, cleanup := createChaosTestCluster(t, 5)
	defer cleanup()

	// Start all nodes
	for i, node := range nodes {
		if err := node.Start(); err != nil {
			t.Fatalf("Failed to start node %d: %v", i, err)
		}
	}

	// Wait for leader
	waitForLeader(t, nodes, 5*time.Second)
	waitForAllNodes(t, nodes, 3*time.Second)

	// Kill 2 non-leader nodes
	killed := 0
	for _, node := range nodes {
		if !node.IsLeader() && killed < 2 {
			t.Logf("Killing node: %s", node.nodeID)
			node.Stop()
			killed++
		}
	}

	// Quorum (3 of 5) is maintained, so the cluster must converge back on a
	// single running leader — allow time for any re-election churn.
	leaderFound := waitForStableLeader(t, nodes, 20*time.Second)
	t.Logf("Leader still active: %s", leaderFound.nodeID)

	t.Log("✅ Multiple node failures test passed")
}

// TestChaos_LeaderElectionSpeed_Real measures leader election time
func TestChaos_LeaderElectionSpeed_Real(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping chaos test in short mode")
	}

	skipUnlessChaosEnv(t)

	nodes, cleanup := createChaosTestCluster(t, 3)
	defer cleanup()

	// Start all nodes; with full static membership a node needs quorum votes
	// to win, so no node can become leader before its peers are reachable.
	startTime := time.Now()
	for i, node := range nodes {
		if err := node.Start(); err != nil {
			t.Fatalf("Failed to start node %d: %v", i, err)
		}
	}

	// Wait for a leader to be elected and seen by all nodes
	waitForLeader(t, nodes, 5*time.Second)
	waitForAllNodes(t, nodes, 5*time.Second)
	elapsed := time.Since(startTime)

	t.Logf("Leader election completed in %v", elapsed)
	if elapsed > 5*time.Second {
		t.Errorf("Leader election took too long: %v", elapsed)
	}

	t.Log("✅ Leader election speed test passed")
}

// TestChaos_TermConsistency_Real ensures all nodes agree on term
func TestChaos_TermConsistency_Real(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping chaos test in short mode")
	}

	skipUnlessChaosEnv(t)

	nodes, cleanup := createChaosTestCluster(t, 3)
	defer cleanup()

	// Start all nodes
	for i, node := range nodes {
		if err := node.Start(); err != nil {
			t.Fatalf("Failed to start node %d: %v", i, err)
		}
	}

	// Wait for leader
	waitForLeader(t, nodes, 5*time.Second)
	waitForAllNodes(t, nodes, 3*time.Second)

	// Terms propagate with heartbeats, so poll until every running node
	// reports the same term instead of sampling a single instant.
	deadline := time.After(10 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var maxTerm uint64
	for {
		maxTerm = 0
		consistent := true
		for _, node := range nodes {
			term := node.CurrentTerm()
			if term > maxTerm {
				maxTerm = term
			}
		}
		for _, node := range nodes {
			if node.running.Load() && node.CurrentTerm() != maxTerm {
				consistent = false
				break
			}
		}
		if consistent {
			break
		}
		select {
		case <-deadline:
			for _, node := range nodes {
				t.Logf("Node %s at term %d", node.nodeID, node.CurrentTerm())
			}
			t.Fatal("Term inconsistency persisted for 10s")
		case <-ticker.C:
		}
	}

	t.Logf("All nodes at term %d", maxTerm)
	t.Log("✅ Term consistency test passed")
}

// TestChaos_SplitVote_Real tests split vote scenario
func TestChaos_SplitVote_Real(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping chaos test in short mode")
	}

	// Start with 2 nodes (even number, can cause split votes)
	nodes, cleanup := createChaosTestCluster(t, 2)
	defer cleanup()

	// Start both nodes simultaneously
	var wg sync.WaitGroup
	for i, node := range nodes {
		wg.Add(1)
		go func(idx int, n *Node) {
			defer wg.Done()
			if err := n.Start(); err != nil {
				t.Errorf("Failed to start node %d: %v", idx, err)
			}
		}(i, node)
	}
	wg.Wait()

	// Wait for leader (may take longer with 2 nodes)
	leader := waitForLeader(t, nodes, 10*time.Second)
	t.Logf("Leader elected despite split vote risk: %s", leader.nodeID)

	// Verify only one leader
	leaderCount := countLeaders(nodes)
	if leaderCount != 1 {
		t.Errorf("Expected 1 leader, got %d", leaderCount)
	}

	t.Log("✅ Split vote test passed")
}

// createPersistentChaosCluster creates nodes whose store bundles survive
// Stop/Start cycles. The caller controls lifetimes of the returned bundles.
// This test requires a properly configured network environment.
func createPersistentChaosCluster(t *testing.T, nodeCount int) ([]*Node, []*chaosStoreBundle, func()) {
	t.Helper()
	skipUnlessChaosEnv(t)
	nodes := make([]*Node, nodeCount)
	bundles := make([]*chaosStoreBundle, nodeCount)
	transports := make([]*TCPTransport, nodeCount)
	cleanups := make([]func(), nodeCount)

	for i := 0; i < nodeCount; i++ {
		cfg := core.RaftConfig{
			NodeID:           fmt.Sprintf("chaos-node-%d", i),
			BindAddr:         fmt.Sprintf("127.0.0.1:%d", 17100+i),
			AdvertiseAddr:    fmt.Sprintf("127.0.0.1:%d", 17100+i),
			Bootstrap:        i == 0,
			ElectionTimeout:  core.Duration{Duration: 200 * time.Millisecond},
			HeartbeatTimeout: core.Duration{Duration: 100 * time.Millisecond},
			CommitTimeout:    core.Duration{Duration: 50 * time.Millisecond},
			MaxAppendEntries: 64,
		}
		peers := make([]core.RaftPeer, 0, nodeCount-1)
		for j := 0; j < nodeCount; j++ {
			if j == i {
				continue
			}
			peers = append(peers, core.RaftPeer{
				ID:      fmt.Sprintf("chaos-node-%d", j),
				Address: fmt.Sprintf("127.0.0.1:%d", 17100+j),
				Region:  "default", Role: core.RoleVoter,
			})
		}
		cfg.Peers = peers

		b := &chaosStoreBundle{
			logStore:  NewInMemoryLogStore(),
			snapStore: NewInMemorySnapshotStore(),
			stable:    NewInMemoryStableStore(),
			storage:   NewInMemoryStorage(),
		}
		b.fsm = NewStorageFSM(b.storage)

		transport, err := NewTCPTransport(cfg.BindAddr, cfg.AdvertiseAddr, nil, newTestRaftLogger())
		if err != nil {
			t.Fatalf("transport %d: %v", i, err)
		}
		transports[i] = transport

		bundles[i] = b
		cleanups[i] = func(n *Node, tr *TCPTransport) func() {
			return func() { n.Stop(); tr.Stop() }
		}(nodes[i], transport)
	}

	// Second pass: create Node instances (needs all bundle pointers first)
	for i := 0; i < nodeCount; i++ {
		node, err := NewNodeWithStableStore(
			core.RaftConfig{
				NodeID:           fmt.Sprintf("chaos-node-%d", i),
				BindAddr:         fmt.Sprintf("127.0.0.1:%d", 17100+i),
				AdvertiseAddr:    fmt.Sprintf("127.0.0.1:%d", 17100+i),
				Bootstrap:        i == 0,
				ElectionTimeout:  core.Duration{Duration: 200 * time.Millisecond},
				HeartbeatTimeout: core.Duration{Duration: 100 * time.Millisecond},
				CommitTimeout:    core.Duration{Duration: 50 * time.Millisecond},
				MaxAppendEntries: 64,
				Peers: func() []core.RaftPeer {
					pp := make([]core.RaftPeer, 0, nodeCount-1)
					for j := 0; j < nodeCount; j++ {
						if j == i {
							continue
						}
						pp = append(pp, core.RaftPeer{
							ID:      fmt.Sprintf("chaos-node-%d", j),
							Address: fmt.Sprintf("127.0.0.1:%d", 17100+j),
							Region:  "default", Role: core.RoleVoter,
						})
					}
					return pp
				}(),
			},
			bundles[i].logStore, bundles[i].stable, bundles[i].snapStore, bundles[i].fsm,
			newTestRaftLogger(),
		)
		if err != nil {
			t.Fatalf("NewNodeWithStableStore %d: %v", i, err)
		}
		node.SetTransport(transports[i])
		nodes[i] = node
		cleanups[i] = func(n *Node, tr *TCPTransport) func() {
			return func() { n.Stop(); tr.Stop() }
		}(node, transports[i])
	}

	cleanup := func() {
		for _, c := range cleanups {
			c()
		}
	}
	return nodes, bundles, cleanup
}

// TestChaos_RestartRecovery_Real verifies that hard state and committed entries
// survive a full cluster restart (Stop → recreate Node → Start with the same
// persistent stores).
func TestChaos_RestartRecovery_Real(t *testing.T) {
	skipUnlessChaosEnv(t)

	nodes, bundles, cleanup := createPersistentChaosCluster(t, 3)
	defer cleanup()

	for _, n := range nodes {
		if err := n.Start(); err != nil {
			t.Fatalf("start: %v", err)
		}
	}
	leader := waitForStableLeader(t, nodes, 10*time.Second)
	waitForAllNodes(t, nodes, 5*time.Second)

	preTerm := leader.CurrentTerm()
	preCommit := leader.CommitIndex()
	preLogIndex := leader.lastLogIndexLocked()
	t.Logf("Pre-restart: term=%d commit=%d logIndex=%d", preTerm, preCommit, preLogIndex)

	// Stop all nodes and wait for ports to be released.
	for _, n := range nodes {
		n.Stop()
	}
	time.Sleep(500 * time.Millisecond)

	// Create fresh Node instances with the SAME persistent stores.
	newNodes := make([]*Node, 3)
	newTransports := make([]*TCPTransport, 3)
	for i := 0; i < 3; i++ {
		cfg := core.RaftConfig{
			NodeID:           fmt.Sprintf("chaos-node-%d", i),
			BindAddr:         fmt.Sprintf("127.0.0.1:%d", 17100+i),
			AdvertiseAddr:    fmt.Sprintf("127.0.0.1:%d", 17100+i),
			Bootstrap:        i == 0,
			ElectionTimeout:  core.Duration{Duration: 200 * time.Millisecond},
			HeartbeatTimeout: core.Duration{Duration: 100 * time.Millisecond},
			CommitTimeout:    core.Duration{Duration: 50 * time.Millisecond},
			MaxAppendEntries: 64,
			Peers: func() []core.RaftPeer {
				pp := make([]core.RaftPeer, 0, 2)
				for j := 0; j < 3; j++ {
					if j == i {
						continue
					}
					pp = append(pp, core.RaftPeer{
						ID:      fmt.Sprintf("chaos-node-%d", j),
						Address: fmt.Sprintf("127.0.0.1:%d", 17100+j),
						Region:  "default", Role: core.RoleVoter,
					})
				}
				return pp
			}(),
		}
		node, err := NewNodeWithStableStore(cfg, bundles[i].logStore, bundles[i].stable, bundles[i].snapStore, bundles[i].fsm, newTestRaftLogger())
		if err != nil {
			t.Fatalf("recreate node %d: %v", i, err)
		}
		tr, err := NewTCPTransport(cfg.BindAddr, cfg.AdvertiseAddr, nil, newTestRaftLogger())
		if err != nil {
			t.Fatalf("transport %d: %v", i, err)
		}
		node.SetTransport(tr)
		newNodes[i] = node
		newTransports[i] = tr
	}
	defer func() {
		for _, n := range newNodes {
			n.Stop()
		}
		for _, tr := range newTransports {
			tr.Stop()
		}
	}()

	// Start the recreated nodes.
	for _, n := range newNodes {
		if err := n.Start(); err != nil {
			t.Fatalf("restart: %v", err)
		}
	}
	_ = waitForStableLeader(t, newNodes, 15*time.Second)
	t.Logf("Stable leader elected after restart")

	// Verify hard state survived on the first node that was originally the
	// bootstrap node. Term, commit index, and log length must not regress.
	restored := newNodes[0]
	// Give heartbeats a moment to propagate so leaderID is populated.
	time.Sleep(500 * time.Millisecond)

	// Log the state of every post-restart node for diagnostics.
	for i, n := range newNodes {
		t.Logf("Node %d: term=%d commit=%d logIndex=%d leaderID=%q running=%v",
			i, n.CurrentTerm(), n.CommitIndex(), n.lastLogIndexLocked(),
			n.LeaderID(), n.running.Load())
	}
	t.Logf("Pre-restart: term=%d commit=%d logIndex=%d", preTerm, preCommit, preLogIndex)
	t.Logf("Post-restart node-0: term=%d commit=%d logIndex=%d leaderID=%q",
		restored.CurrentTerm(), restored.CommitIndex(), restored.lastLogIndexLocked(),
		restored.LeaderID())

	if restored.CurrentTerm() < preTerm {
		t.Errorf("term regressed: was %d, now %d", preTerm, restored.CurrentTerm())
	}
	if restored.CommitIndex() < preCommit {
		t.Errorf("commit index regressed: was %d, now %d", preCommit, restored.CommitIndex())
	}
	if restored.lastLogIndexLocked() < preLogIndex {
		t.Errorf("log index regressed: was %d, now %d", preLogIndex, restored.lastLogIndexLocked())
	}
	t.Log("✅ Restart recovery test passed")
}

// TestChaos_LeaderFailover_Real verifies that killing the leader produces a
// new leader among the surviving nodes within a reasonable timeout.
func TestChaos_LeaderFailover_Real(t *testing.T) {
	skipUnlessChaosEnv(t)

	nodes, _, cleanup := createPersistentChaosCluster(t, 3)
	defer cleanup()

	for _, n := range nodes {
		if err := n.Start(); err != nil {
			t.Fatalf("start: %v", err)
		}
	}
	originalLeader := waitForStableLeader(t, nodes, 10*time.Second)
	t.Logf("Original leader: %s", originalLeader.nodeID)

	// Stop the leader.
	originalLeader.Stop()
	t.Logf("Leader %s stopped", originalLeader.nodeID)

	// A new leader must be elected by the two remaining nodes.
	newLeader := waitForStableLeader(t, nodes, 10*time.Second)
	if newLeader == nil || newLeader.nodeID == originalLeader.nodeID {
		t.Fatal("a different node must become leader after the original is stopped")
	}
	t.Logf("New leader elected: %s", newLeader.nodeID)
	t.Log("✅ Leader failover test passed")
}
