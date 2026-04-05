package raft

import (
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestDistributor_Recompute_NoHealthyNodes(t *testing.T) {
	d := NewDistributor("node-1", "default", core.StrategyRoundRobin)

	// Add a node but mark it unhealthy
	d.AddNode(&core.NodeInfo{
		ID:        "node-1",
		Region:    "us-east",
		MaxSouls:  100,
		CanProbe:  false, // Unhealthy
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
		ID:        "node-1",
		Region:    "us-east",
		MaxSouls:  100,
		CanProbe:  true,
		LoadAvg:   0.5,
		MemoryUsage: 0.5,
	})
	d.AddNode(&core.NodeInfo{
		ID:        "node-2",
		Region:    "us-west",
		MaxSouls:  100,
		CanProbe:  true,
		LoadAvg:   0.5,
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
		ID:        "node-1",
		Region:    "us-east",
		MaxSouls:  100,
		CanProbe:  true,
		LoadAvg:   0.5,
		MemoryUsage: 0.5,
	})
	d.AddNode(&core.NodeInfo{
		ID:        "node-2",
		Region:    "us-west",
		MaxSouls:  100,
		CanProbe:  true,
		LoadAvg:   0.5,
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
		ID:        "node-1",
		Region:    "us-east",
		MaxSouls:  100,
		CanProbe:  true,
		LoadAvg:   0.5,
		MemoryUsage: 0.5,
	})
	d.AddNode(&core.NodeInfo{
		ID:        "node-2",
		Region:    "us-west",
		MaxSouls:  50,
		CanProbe:  true,
		LoadAvg:   0.5,
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
		ID:        "node-1",
		Region:    "us-east",
		MaxSouls:  100,
		CanProbe:  true,
		LoadAvg:   0.5,
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
		ID:        "node-1",
		Region:    "us-east",
		MaxSouls:  100,
		CanProbe:  true,
		LoadAvg:   0.5,
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
