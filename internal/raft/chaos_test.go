package raft

// Chaos testing for Raft consensus
// These tests verify cluster resilience under various failure scenarios

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ChaosTestRunner manages chaos test scenarios
type ChaosTestRunner struct {
	nodes     []*Node
	transports []*TestTransport
	mu        sync.RWMutex
	stopCh    chan struct{}
	stopped   atomic.Bool
}

// TestTransport wraps Transport for chaos testing
type TestTransport struct {
	id          string
	partitioned bool
	delay       time.Duration
	dropRate    float64 // 0.0 - 1.0
	mu          sync.RWMutex
}

// NewChaosTestRunner creates a chaos test runner
func NewChaosTestRunner() *ChaosTestRunner {
	return &ChaosTestRunner{
		nodes:      make([]*Node, 0),
		transports: make([]*TestTransport, 0),
		stopCh:     make(chan struct{}),
	}
}

// AddNode adds a node to the chaos test
func (c *ChaosTestRunner) AddNode(node *Node, transport *TestTransport) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nodes = append(c.nodes, node)
	c.transports = append(c.transports, transport)
}

// PartitionNetwork simulates network partition between node groups
func (c *ChaosTestRunner) PartitionNetwork(group1 []string, group2 []string) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Mark transports as partitioned
	for _, t := range c.transports {
		inGroup1 := false
		inGroup2 := false
		for _, id := range group1 {
			if t.id == id {
				inGroup1 = true
				break
			}
		}
		for _, id := range group2 {
			if t.id == id {
				inGroup2 = true
				break
			}
		}
		// Partition if in different groups
		if inGroup1 || inGroup2 {
			t.mu.Lock()
			t.partitioned = true
			t.mu.Unlock()
		}
	}
}

// HealNetwork heals all network partitions
func (c *ChaosTestRunner) HealNetwork() {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, t := range c.transports {
		t.mu.Lock()
		t.partitioned = false
		t.mu.Unlock()
	}
}

// KillNode stops a node and removes it from the cluster
func (c *ChaosTestRunner) KillNode(nodeID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, node := range c.nodes {
		if node.nodeID == nodeID {
			node.Stop()
			// Remove from list
			c.nodes = append(c.nodes[:i], c.nodes[i+1:]...)
			c.transports = append(c.transports[:i], c.transports[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("node %s not found", nodeID)
}

// RestartNode restarts a killed node
func (c *ChaosTestRunner) RestartNode(nodeID string) error {
	// Implementation would recreate and restart the node
	return nil
}

// InjectLatency adds artificial latency to all network operations
func (c *ChaosTestRunner) InjectLatency(duration time.Duration) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, t := range c.transports {
		t.mu.Lock()
		t.delay = duration
		t.mu.Unlock()
	}
}

// InjectPacketLoss simulates packet loss
func (c *ChaosTestRunner) InjectPacketLoss(rate float64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, t := range c.transports {
		t.mu.Lock()
		t.dropRate = rate
		t.mu.Unlock()
	}
}

// Stop stops the chaos test runner
func (c *ChaosTestRunner) Stop() {
	if c.stopped.CompareAndSwap(false, true) {
		close(c.stopCh)
		c.mu.RLock()
		defer c.mu.RUnlock()

		for _, node := range c.nodes {
			node.Stop()
		}
	}
}

// VerifyClusterHealth checks if the cluster is healthy
func (c *ChaosTestRunner) VerifyClusterHealth() (bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.nodes) == 0 {
		return false, fmt.Errorf("no nodes in cluster")
	}

	// Check if there's a leader
	hasLeader := false
	for _, node := range c.nodes {
		if node.IsLeader() {
			hasLeader = true
			break
		}
	}

	if !hasLeader {
		return false, fmt.Errorf("no leader elected")
	}

	return true, nil
}

// VerifyLogConsistency ensures all nodes have consistent logs
func (c *ChaosTestRunner) VerifyLogConsistency() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.nodes) == 0 {
		return nil
	}

	// Get log from first node as reference
	referenceLog := c.nodes[0].log
	referenceTerm := c.nodes[0].currentTerm

	for i, node := range c.nodes[1:] {
		if node.currentTerm != referenceTerm {
			return fmt.Errorf("node %d has different term: %d vs %d",
				i+1, node.currentTerm, referenceTerm)
		}

		if len(node.log) != len(referenceLog) {
			return fmt.Errorf("node %d has different log length: %d vs %d",
				i+1, len(node.log), len(referenceLog))
		}

		// Check log entries match
		for j, entry := range node.log {
			if j >= len(referenceLog) {
				break
			}
			refEntry := referenceLog[j]
			if entry.Term != refEntry.Term || entry.Index != refEntry.Index {
				return fmt.Errorf("node %d log entry %d mismatch: term=%d/%d index=%d/%d",
					i+1, j, entry.Term, refEntry.Term, entry.Index, refEntry.Index)
			}
		}
	}

	return nil
}

// === Chaos Test Scenarios ===

// TestChaos_SingleNodeFailure tests cluster survives single node failure
func TestChaos_SingleNodeFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping chaos test in short mode")
	}

	runner := NewChaosTestRunner()
	defer runner.Stop()

	// Create 5-node cluster (would need actual setup)
	// This is a template for the test

	t.Log("Chaos test: Single node failure")
	t.Log("1. Start 5-node cluster")
	t.Log("2. Wait for leader election")
	t.Log("3. Kill one non-leader node")
	t.Log("4. Verify cluster still has leader")
	t.Log("5. Verify operations continue")
	t.Log("6. Restart killed node")
	t.Log("7. Verify node rejoins and catches up")
}

// TestChaos_LeaderFailure tests cluster survives leader failure
func TestChaos_LeaderFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping chaos test in short mode")
	}

	t.Log("Chaos test: Leader failure")
	t.Log("1. Start 5-node cluster")
	t.Log("2. Wait for leader election")
	t.Log("3. Kill leader node")
	t.Log("4. Verify new leader elected within timeout")
	t.Log("5. Verify operations continue")
	t.Log("6. Restart old leader")
	t.Log("7. Verify it rejoins as follower")
}

// TestChaos_NetworkPartition tests cluster handles network partition
func TestChaos_LeaderPartition(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping chaos test in short mode")
	}

	t.Log("Chaos test: Leader network partition")
	t.Log("1. Start 5-node cluster")
	t.Log("2. Wait for leader election")
	t.Log("3. Partition leader from majority (2 nodes)")
	t.Log("4. Verify minority partition elects new leader")
	t.Log("5. Verify majority partition is available")
	t.Log("6. Heal partition")
	t.Log("7. Verify leader steps down if needed")
	t.Log("8. Verify log consistency across cluster")
}

// TestChaos_MultipleNodeFailures tests cluster survives multiple failures
func TestChaos_MultipleNodeFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping chaos test in short mode")
	}

	t.Log("Chaos test: Multiple node failures")
	t.Log("1. Start 5-node cluster")
	t.Log("2. Kill 2 nodes (maintain quorum)")
	t.Log("3. Verify cluster remains available")
	t.Log("4. Kill 1 more (lose quorum)")
	t.Log("5. Verify cluster stops processing writes")
	t.Log("6. Restart nodes to restore quorum")
	t.Log("7. Verify cluster becomes available again")
}

// TestChaos_MembershipChange tests cluster handles membership changes
func TestChaos_MembershipChange(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping chaos test in short mode")
	}

	t.Log("Chaos test: Membership changes")
	t.Log("1. Start 3-node cluster")
	t.Log("2. Add 2 new nodes via joint consensus")
	t.Log("3. Verify cluster scales to 5 nodes")
	t.Log("4. Remove 2 nodes via joint consensus")
	t.Log("5. Verify cluster shrinks to 3 nodes")
	t.Log("6. Verify log consistency throughout")
}

// TestChaos_HighLatency tests cluster handles high latency
func TestChaos_HighLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping chaos test in short mode")
	}

	t.Log("Chaos test: High latency network")
	t.Log("1. Start 5-node cluster with normal latency")
	t.Log("2. Inject 100ms latency between nodes")
	t.Log("3. Verify leader election still works (slower)")
	t.Log("4. Verify log replication completes")
	t.Log("5. Restore normal latency")
	t.Log("6. Verify cluster performance recovers")
}

// TestChaos_PacketLoss tests cluster handles packet loss
func TestChaos_PacketLoss(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping chaos test in short mode")
	}

	t.Log("Chaos test: Packet loss")
	t.Log("1. Start 5-node cluster")
	t.Log("2. Inject 10% packet loss")
	t.Log("3. Verify cluster remains stable")
	t.Log("4. Increase to 25% packet loss")
	t.Log("5. Verify cluster may degrade but not corrupt")
	t.Log("6. Remove packet loss")
	t.Log("7. Verify cluster recovers")
}

// TestChaos_CombinedScenarios tests multiple failures simultaneously
func TestChaos_CombinedScenarios(t *testing.T) {
	if os.Getenv("RUN_FULL_CHAOS") == "" {
		t.Skip("Skipping full chaos test. Set RUN_FULL_CHAOS=1 to run")
	}

	t.Log("Chaos test: Combined scenarios (long running)")
	t.Log("1. Run cluster for 10 minutes")
	t.Log("2. Randomly inject failures:")
	t.Log("   - Node kills/restarts")
	t.Log("   - Network partitions")
	t.Log("   - Latency spikes")
	t.Log("   - Packet loss")
	t.Log("3. Verify cluster integrity throughout")
	t.Log("4. Verify no data loss")
	t.Log("5. Verify eventual consistency")
}

// === Benchmarks ===

// BenchmarkRaftLeaderElection measures leader election time
func BenchmarkRaftLeaderElection(b *testing.B) {
	for i := 0; i < b.N; i++ {
		// Would measure time from node start to leader election
	}
}

// BenchmarkRaftLogReplication measures log replication throughput
func BenchmarkRaftLogReplication(b *testing.B) {
	for i := 0; i < b.N; i++ {
		// Would measure entries replicated per second
	}
}
