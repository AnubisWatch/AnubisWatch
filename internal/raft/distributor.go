package raft

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Distributor handles assignment of souls (monitors) to nodes
// The Pharaoh decides which priest judges which soul
type Distributor struct {
	strategy core.DistributionStrategy
	nodeID   string
	region   string

	// State
	mu          sync.RWMutex
	plan        *core.DistributionPlan
	nodes       map[string]*core.NodeInfo
	souls       map[string]*core.Soul
	assignments map[string][]core.SoulAssignment // nodeID -> assignments
	revision    uint64

	// Callbacks
	onRebalance func(core.DistributionPlan)
}

// NewDistributor creates a new distributor
func NewDistributor(nodeID, region string, strategy core.DistributionStrategy) *Distributor {
	if strategy == "" {
		strategy = core.StrategyRoundRobin
	}

	return &Distributor{
		strategy: strategy,
		nodeID:   nodeID,
		region:   region,
		plan: &core.DistributionPlan{
			Version:     1,
			Timestamp:   time.Now().UTC(),
			Strategy:    strategy,
			Assignments: make([]core.SoulAssignment, 0),
			Revision:    1,
		},
		nodes:       make(map[string]*core.NodeInfo),
		souls:       make(map[string]*core.Soul),
		assignments: make(map[string][]core.SoulAssignment),
		revision:    1,
	}
}

// SetStrategy changes the distribution strategy
func (d *Distributor) SetStrategy(strategy core.DistributionStrategy) {
	d.mu.Lock()
	d.strategy = strategy
	d.mu.Unlock()
}

// AddNode adds a node to the distribution pool
func (d *Distributor) AddNode(node *core.NodeInfo) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.nodes[node.ID] = node
}

// RemoveNode removes a node from the distribution pool
func (d *Distributor) RemoveNode(nodeID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.nodes, nodeID)
	delete(d.assignments, nodeID)
}

// UpdateNode updates node information
func (d *Distributor) UpdateNode(node *core.NodeInfo) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if existing, ok := d.nodes[node.ID]; ok {
		// Preserve assigned souls count
		node.AssignedSouls = existing.AssignedSouls
	}
	d.nodes[node.ID] = node
}

// AddSoul adds a soul to be distributed
func (d *Distributor) AddSoul(soul *core.Soul) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.souls[soul.ID] = soul
}

// RemoveSoul removes a soul
func (d *Distributor) RemoveSoul(soulID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.souls, soulID)
}

// GetPlan returns the current distribution plan
func (d *Distributor) GetPlan() core.DistributionPlan {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return *d.plan
}

// Recompute recomputes the distribution plan
func (d *Distributor) Recompute() (core.DistributionPlan, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Filter healthy nodes
	healthyNodes := d.getHealthyNodes()
	if len(healthyNodes) == 0 {
		return core.DistributionPlan{}, fmt.Errorf("no healthy nodes available")
	}

	// Create new plan
	plan := core.DistributionPlan{
		Version:     d.plan.Version + 1,
		Timestamp:   time.Now().UTC(),
		Strategy:    d.strategy,
		Assignments: make([]core.SoulAssignment, 0),
		Revision:    d.revision + 1,
	}

	switch d.strategy {
	case core.StrategyRoundRobin:
		plan.Assignments = d.distributeRoundRobin(healthyNodes)
	case core.StrategyRegionAware:
		plan.Assignments = d.distributeRegionAware(healthyNodes)
	case core.StrategyRedundant:
		plan.Assignments = d.distributeRedundant(healthyNodes)
	case core.StrategyWeighted:
		plan.Assignments = d.distributeWeighted(healthyNodes)
	case core.StrategyLatencyOptimal:
		plan.Assignments = d.distributeLatencyOptimal(healthyNodes)
	default:
		plan.Assignments = d.distributeRoundRobin(healthyNodes)
	}

	d.plan = &plan
	d.revision = plan.Revision

	// Reset assignments map
	d.assignments = make(map[string][]core.SoulAssignment)
	for _, a := range plan.Assignments {
		d.assignments[a.NodeID] = append(d.assignments[a.NodeID], a)
	}

	if d.onRebalance != nil {
		go d.onRebalance(plan)
	}

	return plan, nil
}

// GetAssignment returns the assignment for a soul
func (d *Distributor) GetAssignment(soulID string) *core.SoulAssignment {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for _, assignment := range d.plan.Assignments {
		if assignment.SoulID == soulID {
			return &assignment
		}
	}
	return nil
}

// GetNodeAssignments returns all assignments for a node
func (d *Distributor) GetNodeAssignments(nodeID string) []core.SoulAssignment {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.assignments[nodeID]
}

// GetMyAssignments returns assignments for the local node
func (d *Distributor) GetMyAssignments() []core.SoulAssignment {
	return d.GetNodeAssignments(d.nodeID)
}

// GetStats returns distribution statistics
func (d *Distributor) GetStats() DistributionStats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	stats := DistributionStats{
		TotalSouls:       len(d.souls),
		TotalNodes:       len(d.nodes),
		HealthyNodes:     len(d.getHealthyNodes()),
		Strategy:         string(d.strategy),
		Revision:         d.revision,
		NodeDistribution: make(map[string]int),
	}

	for nodeID, assignments := range d.assignments {
		stats.NodeDistribution[nodeID] = len(assignments)
	}

	return stats
}

// getHealthyNodes returns nodes that are ready to accept souls
func (d *Distributor) getHealthyNodes() []*core.NodeInfo {
	healthy := make([]*core.NodeInfo, 0)
	for _, node := range d.nodes {
		if node.CanProbe && node.LoadAvg < 0.8 && node.MemoryUsage < 0.9 {
			healthy = append(healthy, node)
		}
	}
	return healthy
}

// distributeRoundRobin assigns souls in round-robin fashion
func (d *Distributor) distributeRoundRobin(nodes []*core.NodeInfo) []core.SoulAssignment {
	assignments := make([]core.SoulAssignment, 0)

	nodeIndex := 0
	for _, soul := range d.souls {
		if nodeIndex >= len(nodes) {
			nodeIndex = 0
		}
		node := nodes[nodeIndex]

		assignments = append(assignments, core.SoulAssignment{
			SoulID:   soul.ID,
			NodeID:   node.ID,
			Region:   node.Region,
			Priority: 1,
			IsBackup: false,
		})

		nodeIndex++
	}

	return assignments
}

// distributeRegionAware prefers same-region assignments
func (d *Distributor) distributeRegionAware(nodes []*core.NodeInfo) []core.SoulAssignment {
	// Group nodes by region
	byRegion := make(map[string][]*core.NodeInfo)
	for _, node := range nodes {
		byRegion[node.Region] = append(byRegion[node.Region], node)
	}

	assignments := make([]core.SoulAssignment, 0)

	for _, soul := range d.souls {
		// Find primary assignment (same region if possible)
		var primaryNode *core.NodeInfo
		if soulRegionNodes, ok := byRegion[soul.Region]; ok && len(soulRegionNodes) > 0 {
			// Pick least loaded node in same region
			primaryNode = d.pickLeastLoaded(soulRegionNodes)
		} else {
			// Fall back to any node
			primaryNode = d.pickLeastLoaded(nodes)
		}

		if primaryNode != nil {
			assignments = append(assignments, core.SoulAssignment{
				SoulID:   soul.ID,
				NodeID:   primaryNode.ID,
				Region:   primaryNode.Region,
				Priority: 1,
				IsBackup: false,
			})
		}
	}

	return assignments
}

// distributeRedundant assigns each soul to multiple nodes
func (d *Distributor) distributeRedundant(nodes []*core.NodeInfo) []core.SoulAssignment {
	if len(nodes) < 2 {
		return d.distributeRoundRobin(nodes)
	}

	// Sort nodes by capacity
	sorted := make([]*core.NodeInfo, len(nodes))
	copy(sorted, nodes)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].AssignedSouls < sorted[j].AssignedSouls
	})

	assignments := make([]core.SoulAssignment, 0)
	replicationFactor := 2 // Primary + 1 backup

	for _, soul := range d.souls {
		// Assign to replicationFactor nodes
		for i := 0; i < replicationFactor && i < len(sorted); i++ {
			assignments = append(assignments, core.SoulAssignment{
				SoulID:   soul.ID,
				NodeID:   sorted[i].ID,
				Region:   sorted[i].Region,
				Priority: i + 1,
				IsBackup: i > 0,
			})
		}
	}

	return assignments
}

// distributeWeighted assigns based on node capacity
func (d *Distributor) distributeWeighted(nodes []*core.NodeInfo) []core.SoulAssignment {
	// Calculate total capacity
	totalCapacity := 0
	for _, node := range nodes {
		capacity := node.MaxSouls - node.AssignedSouls
		if capacity < 0 {
			capacity = 0
		}
		totalCapacity += capacity
	}

	assignments := make([]core.SoulAssignment, 0)

	for _, soul := range d.souls {
		// Pick node with highest remaining capacity
		bestNode := d.pickBestWeighted(nodes, totalCapacity)
		if bestNode != nil {
			assignments = append(assignments, core.SoulAssignment{
				SoulID:   soul.ID,
				NodeID:   bestNode.ID,
				Region:   bestNode.Region,
				Priority: 1,
				IsBackup: false,
			})
			// Update capacity for next iteration
			bestNode.AssignedSouls++
			totalCapacity--
		}
	}

	return assignments
}

// distributeLatencyOptimal assigns probes to nodes closest to targets based on
// historical latency measurements. Nodes with lower average latency for a target
// get priority.
func (d *Distributor) distributeLatencyOptimal(nodes []*core.NodeInfo) []core.SoulAssignment {
	if len(nodes) == 0 {
		return nil
	}

	assignments := make([]core.SoulAssignment, 0)

	for _, soul := range d.souls {
		// Score nodes by a combination of load and estimated latency
		// In a real implementation, this would use historical RTT data per soul-node pair.
		// Here we use load average as a proxy (lower load typically = lower latency).
		bestNode := nodes[0]
		bestScore := d.latencyScore(nodes[0])

		for _, node := range nodes[1:] {
			score := d.latencyScore(node)
			if score < bestScore {
				bestNode = node
				bestScore = score
			}
		}

		if bestNode != nil {
			assignments = append(assignments, core.SoulAssignment{
				SoulID:   soul.ID,
				NodeID:   bestNode.ID,
				Region:   bestNode.Region,
				Priority: 1,
				IsBackup: false,
			})
		}
	}

	return assignments
}

// latencyScore computes a score where lower is better.
// Combines load average (70%) and memory pressure (30%) as proxies for responsiveness.
func (d *Distributor) latencyScore(node *core.NodeInfo) float64 {
	load := node.LoadAvg
	if load > 1.0 {
		load = 1.0
	}
	memPressure := node.MemoryUsage
	if memPressure > 1.0 {
		memPressure = 1.0
	}
	return 0.7*load + 0.3*memPressure
}

// pickLeastLoaded selects the least loaded node
func (d *Distributor) pickLeastLoaded(nodes []*core.NodeInfo) *core.NodeInfo {
	if len(nodes) == 0 {
		return nil
	}

	best := nodes[0]
	for _, node := range nodes {
		if node.LoadAvg < best.LoadAvg {
			best = node
		}
	}
	return best
}

// pickBestWeighted selects node based on weighted capacity
func (d *Distributor) pickBestWeighted(nodes []*core.NodeInfo, totalCapacity int) *core.NodeInfo {
	if len(nodes) == 0 {
		return nil
	}

	best := nodes[0]
	bestScore := float64(best.MaxSouls-best.AssignedSouls) / float64(totalCapacity)

	for _, node := range nodes {
		capacity := node.MaxSouls - node.AssignedSouls
		if capacity < 0 {
			continue
		}
		score := float64(capacity) / float64(totalCapacity)
		if score > bestScore {
			best = node
			bestScore = score
		}
	}

	return best
}

// IsResponsible checks if this node is responsible for a soul
func (d *Distributor) IsResponsible(soulID string) bool {
	assignment := d.GetAssignment(soulID)
	if assignment == nil {
		return false
	}
	return assignment.NodeID == d.nodeID
}

// DistributionStats holds distribution statistics
type DistributionStats struct {
	TotalSouls       int            `json:"total_souls"`
	TotalNodes       int            `json:"total_nodes"`
	HealthyNodes     int            `json:"healthy_nodes"`
	Strategy         string         `json:"strategy"`
	Revision         uint64         `json:"revision"`
	NodeDistribution map[string]int `json:"node_distribution"`
}

// SetOnRebalanceCallback sets the callback for rebalance events
func (d *Distributor) SetOnRebalanceCallback(cb func(core.DistributionPlan)) {
	d.onRebalance = cb
}

// ValidatePlan validates a distribution plan
func (d *Distributor) ValidatePlan(plan core.DistributionPlan) error {
	// Check that all souls are assigned
	d.mu.RLock()
	totalSouls := len(d.souls)
	d.mu.RUnlock()

	assignedSouls := make(map[string]bool)
	for _, a := range plan.Assignments {
		assignedSouls[a.SoulID] = true
	}

	if len(assignedSouls) != totalSouls {
		return fmt.Errorf("plan does not assign all souls: %d assigned, %d total",
			len(assignedSouls), totalSouls)
	}

	// Check that all assigned nodes exist
	for _, a := range plan.Assignments {
		d.mu.RLock()
		_, exists := d.nodes[a.NodeID]
		d.mu.RUnlock()
		if !exists {
			return fmt.Errorf("plan assigns to unknown node: %s", a.NodeID)
		}
	}

	return nil
}
