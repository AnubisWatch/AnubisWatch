package raft

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestDistributor_Recompute_NoHealthyNodes(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	// Add a node but mark it unhealthy
	d.AddNode(&core.NodeInfo{
		ID:       "node-1",
		Region:   "us-east",
		MaxSouls: 100,
		CanProbe: false, // Unhealthy
	})

	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})

	_, err := d.Recompute()
	if err == nil {
		t.Error("Expected error when no healthy nodes")
	}
}

func TestDistributor_Recompute_RegionAware(t *testing.T) {
	d := NewDistributor("node-1", "us-east", core.StrategyRegionAware)

	// Add healthy nodes in different regions
	d.AddNode(&core.NodeInfo{
		ID:          "node-1",
		Region:      "us-east",
		MaxSouls:    100,
		CanProbe:    true,
		LoadAvg:     0.5,
		MemoryUsage: 0.5,
	})
	d.AddNode(&core.NodeInfo{
		ID:          "node-2",
		Region:      "us-west",
		MaxSouls:    100,
		CanProbe:    true,
		LoadAvg:     0.5,
		MemoryUsage: 0.5,
	})

	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP, Region: "us-east"})

	plan, err := d.Recompute()
	if err != nil {
		t.Fatalf("Recompute failed: %v", err)
	}

	if len(plan.Assignments) != 1 {
		t.Errorf("Expected 1 assignment, got %d", len(plan.Assignments))
	}
}

func TestDistributor_Recompute_Redundant(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRedundant)

	// Add healthy nodes
	d.AddNode(&core.NodeInfo{
		ID:          "node-1",
		Region:      "us-east",
		MaxSouls:    100,
		CanProbe:    true,
		LoadAvg:     0.5,
		MemoryUsage: 0.5,
	})
	d.AddNode(&core.NodeInfo{
		ID:          "node-2",
		Region:      "us-west",
		MaxSouls:    100,
		CanProbe:    true,
		LoadAvg:     0.5,
		MemoryUsage: 0.5,
	})

	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})

	plan, err := d.Recompute()
	if err != nil {
		t.Fatalf("Recompute failed: %v", err)
	}

	// Redundant strategy should assign to multiple nodes
	if len(plan.Assignments) < 1 {
		t.Errorf("Expected at least 1 assignment, got %d", len(plan.Assignments))
	}
}

func TestDistributor_Recompute_Weighted(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyWeighted)

	// Add healthy nodes with different capacities
	d.AddNode(&core.NodeInfo{
		ID:          "node-1",
		Region:      "us-east",
		MaxSouls:    100,
		CanProbe:    true,
		LoadAvg:     0.5,
		MemoryUsage: 0.5,
	})
	d.AddNode(&core.NodeInfo{
		ID:          "node-2",
		Region:      "us-west",
		MaxSouls:    50,
		CanProbe:    true,
		LoadAvg:     0.5,
		MemoryUsage: 0.5,
	})

	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})
	d.AddSoul(&core.Soul{ID: "soul-2", WorkspaceID: "default", Name: "Test2", Type: core.CheckHTTP})

	plan, err := d.Recompute()
	if err != nil {
		t.Fatalf("Recompute failed: %v", err)
	}

	if len(plan.Assignments) != 2 {
		t.Errorf("Expected 2 assignments, got %d", len(plan.Assignments))
	}
}

func TestDistributor_Recompute_UnknownStrategy(t *testing.T) {
	d := NewDistributor("node-1", "default", core.DistributionStrategy("unknown"))

	// Add healthy node
	d.AddNode(&core.NodeInfo{
		ID:          "node-1",
		Region:      "us-east",
		MaxSouls:    100,
		CanProbe:    true,
		LoadAvg:     0.5,
		MemoryUsage: 0.5,
	})

	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})

	// Unknown strategy should fall back to round robin
	plan, err := d.Recompute()
	if err != nil {
		t.Fatalf("Recompute failed: %v", err)
	}

	if len(plan.Assignments) != 1 {
		t.Errorf("Expected 1 assignment, got %d", len(plan.Assignments))
	}
}

func TestDistributor_Recompute_WithCallback(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	var mu sync.Mutex
	callbackCalled := false
	d.SetOnRebalanceCallback(func(plan core.DistributionPlan) {
		mu.Lock()
		callbackCalled = true
		mu.Unlock()
	})

	// Add healthy node
	d.AddNode(&core.NodeInfo{
		ID:          "node-1",
		Region:      "us-east",
		MaxSouls:    100,
		CanProbe:    true,
		LoadAvg:     0.5,
		MemoryUsage: 0.5,
	})

	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})

	_, err := d.Recompute()
	if err != nil {
		t.Fatalf("Recompute failed: %v", err)
	}

	// Give goroutine time to call callback
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	wasCalled := callbackCalled
	mu.Unlock()

	if !wasCalled {
		t.Error("Expected callback to be called")
	}
}

func TestDistributor_Recompute_LatencyOptimal(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyLatencyOptimal)

	// Add healthy nodes with different loads
	d.AddNode(&core.NodeInfo{
		ID:          "node-1",
		Region:      "us-east",
		MaxSouls:    100,
		CanProbe:    true,
		LoadAvg:     0.1, // Low load - should be preferred
		MemoryUsage: 0.3,
	})
	d.AddNode(&core.NodeInfo{
		ID:          "node-2",
		Region:      "us-west",
		MaxSouls:    100,
		CanProbe:    true,
		LoadAvg:     0.9, // High load - should be avoided
		MemoryUsage: 0.8,
	})

	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})

	plan, err := d.Recompute()
	if err != nil {
		t.Fatalf("Recompute failed: %v", err)
	}

	if len(plan.Assignments) != 1 {
		t.Fatalf("Expected 1 assignment, got %d", len(plan.Assignments))
	}

	// Should prefer node-1 (lower load = better latency score)
	if plan.Assignments[0].NodeID != "node-1" {
		t.Errorf("Expected assignment to node-1 (lower load), got %s", plan.Assignments[0].NodeID)
	}
}

func TestDistributor_Recompute_RoundRobin_Distribution(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	// Add 2 healthy nodes
	for i := 1; i <= 2; i++ {
		d.AddNode(&core.NodeInfo{
			ID:          fmt.Sprintf("node-%d", i),
			Region:      "us-east",
			MaxSouls:    100,
			CanProbe:    true,
			LoadAvg:     0.5,
			MemoryUsage: 0.5,
		})
	}

	// Add 4 souls - should be evenly distributed
	for i := 1; i <= 4; i++ {
		d.AddSoul(&core.Soul{ID: fmt.Sprintf("soul-%d", i), WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})
	}

	plan, err := d.Recompute()
	if err != nil {
		t.Fatalf("Recompute failed: %v", err)
	}

	if len(plan.Assignments) != 4 {
		t.Errorf("Expected 4 assignments, got %d", len(plan.Assignments))
	}

	// Count per node
	counts := make(map[string]int)
	for _, a := range plan.Assignments {
		counts[a.NodeID]++
	}

	// Round robin should give 2 each
	for nodeID, count := range counts {
		if count != 2 {
			t.Errorf("Expected 2 assignments for %s, got %d", nodeID, count)
		}
	}
}

func TestDistributor_Recompute_Redundant_MultipleAssignments(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRedundant)

	// Add 3 healthy nodes
	for i := 1; i <= 3; i++ {
		d.AddNode(&core.NodeInfo{
			ID:          fmt.Sprintf("node-%d", i),
			Region:      "us-east",
			MaxSouls:    100,
			CanProbe:    true,
			LoadAvg:     0.5,
			MemoryUsage: 0.5,
		})
	}

	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})

	plan, err := d.Recompute()
	if err != nil {
		t.Fatalf("Recompute failed: %v", err)
	}

	// Redundant should assign to 2 nodes (replication factor)
	if len(plan.Assignments) != 2 {
		t.Errorf("Expected 2 assignments (redundant), got %d", len(plan.Assignments))
	}

	// Should have one primary and one backup
	hasPrimary := false
	hasBackup := false
	for _, a := range plan.Assignments {
		if !a.IsBackup && a.Priority == 1 {
			hasPrimary = true
		}
		if a.IsBackup && a.Priority == 2 {
			hasBackup = true
		}
	}
	if !hasPrimary || !hasBackup {
		t.Error("Expected one primary and one backup assignment")
	}
}

func TestDistributor_LatencyScore(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyLatencyOptimal)

	tests := []struct {
		name  string
		node  *core.NodeInfo
		score float64
	}{
		{
			name:  "low load, low memory",
			node:  &core.NodeInfo{LoadAvg: 0.1, MemoryUsage: 0.2},
			score: 0.7*0.1 + 0.3*0.2, // 0.13
		},
		{
			name:  "high load, high memory",
			node:  &core.NodeInfo{LoadAvg: 0.9, MemoryUsage: 0.9},
			score: 0.7*0.9 + 0.3*0.9, // 0.9
		},
		{
			name:  "capped load",
			node:  &core.NodeInfo{LoadAvg: 2.0, MemoryUsage: 0.5},
			score: 0.7*1.0 + 0.3*0.5, // 0.85
		},
	}

	for _, tc := range tests {
		score := d.latencyScore(tc.node)
		if score != tc.score {
			t.Errorf("%s: expected score %.4f, got %.4f", tc.name, tc.score, score)
		}
	}
}

func TestDistributor_SetStrategy_ChangesStrategy(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	d.SetStrategy(core.StrategyLatencyOptimal)

	if d.strategy != core.StrategyLatencyOptimal {
		t.Errorf("Expected strategy to be %s, got %s", core.StrategyLatencyOptimal, d.strategy)
	}
}

func TestDistributor_GetNodeAssignments_NodeSpecific(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	d.AddNode(&core.NodeInfo{
		ID:          "node-1",
		Region:      "us-east",
		MaxSouls:    100,
		CanProbe:    true,
		LoadAvg:     0.5,
		MemoryUsage: 0.5,
	})
	d.AddNode(&core.NodeInfo{
		ID:          "node-2",
		Region:      "us-west",
		MaxSouls:    100,
		CanProbe:    true,
		LoadAvg:     0.5,
		MemoryUsage: 0.5,
	})

	for i := 1; i <= 4; i++ {
		d.AddSoul(&core.Soul{ID: fmt.Sprintf("soul-%d", i), WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})
	}

	_, err := d.Recompute()
	if err != nil {
		t.Fatalf("Recompute failed: %v", err)
	}

	assignments := d.GetNodeAssignments("node-1")
	if len(assignments) == 0 {
		t.Error("Expected assignments for local node")
	}

	node2Assignments := d.GetNodeAssignments("node-2")
	if len(node2Assignments) == 0 {
		t.Error("Expected assignments for node-2")
	}
}

func TestDistributor_IsResponsible_Specific(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	d.AddNode(&core.NodeInfo{
		ID:          "node-1",
		Region:      "us-east",
		MaxSouls:    100,
		CanProbe:    true,
		LoadAvg:     0.5,
		MemoryUsage: 0.5,
	})

	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})

	_, err := d.Recompute()
	if err != nil {
		t.Fatalf("Recompute failed: %v", err)
	}

	if !d.IsResponsible("soul-1") {
		t.Error("Expected node-1 to be responsible for soul-1")
	}

	if d.IsResponsible("nonexistent") {
		t.Error("Expected node-1 to NOT be responsible for nonexistent soul")
	}
}

func TestDistributor_ValidatePlan_Specific(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	d.AddNode(&core.NodeInfo{
		ID:          "node-1",
		Region:      "us-east",
		MaxSouls:    100,
		CanProbe:    true,
		LoadAvg:     0.5,
		MemoryUsage: 0.5,
	})

	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})

	plan, err := d.Recompute()
	if err != nil {
		t.Fatalf("Recompute failed: %v", err)
	}

	err = d.ValidatePlan(plan)
	if err != nil {
		t.Errorf("Expected valid plan, got error: %v", err)
	}
}

func TestDistributor_GetStats_Specific(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	d.AddNode(&core.NodeInfo{
		ID:          "node-1",
		Region:      "us-east",
		MaxSouls:    100,
		CanProbe:    true,
		LoadAvg:     0.5,
		MemoryUsage: 0.5,
	})

	d.AddSoul(&core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "Test", Type: core.CheckHTTP})

	_, err := d.Recompute()
	if err != nil {
		t.Fatalf("Recompute failed: %v", err)
	}

	stats := d.GetStats()

	if stats.TotalSouls != 1 {
		t.Errorf("Expected 1 soul, got %d", stats.TotalSouls)
	}
	if stats.Strategy != string(core.StrategyRoundRobin) {
		t.Errorf("Expected strategy %s, got %s", core.StrategyRoundRobin, stats.Strategy)
	}
	if stats.NodeDistribution["node-1"] != 1 {
		t.Errorf("Expected 1 assignment for node-1, got %d", stats.NodeDistribution["node-1"])
	}
}
