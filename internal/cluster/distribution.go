package cluster

import (
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Distributor handles soul assignment across cluster nodes
// The Scales of Ma'at - weighing and distributing the load
type Distributor struct {
	mu       sync.RWMutex
	nodeID   string
	region   string
	strategy DistributionStrategy

	// Node load tracking
	nodeLoads map[string]*NodeLoad
	soulMap   map[string]string // soul ID -> node ID

	// Configuration
	rebalanceThreshold float64 // Load imbalance threshold
	rebalanceInterval  time.Duration
	failoverTimeout    time.Duration

	// Callbacks
	onAssignSoul   func(soulID string, nodeID string) error
	onUnassignSoul func(soulID string) error
	onRebalance    func(moves []SoulMove)

	logger *slog.Logger
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// DistributionStrategy determines how souls are assigned to nodes
type DistributionStrategy int

const (
	// StrategyRoundRobin assigns souls sequentially
	StrategyRoundRobin DistributionStrategy = iota
	// StrategyRegionAware prefers nodes in the same region
	StrategyRegionAware
	// StrategyLoadBased assigns to least loaded nodes
	StrategyLoadBased
	// StrategyHashBased uses consistent hashing
	StrategyHashBased
)

// NodeLoad tracks load information for a node
type NodeLoad struct {
	NodeID        string
	Region        string
	SoulCount     int
	CPUUsage      float64
	MemoryUsage   float64
	LastHeartbeat time.Time
	Healthy       bool
}

// SoulMove represents a soul reassignment
type SoulMove struct {
	SoulID   string
	FromNode string
	ToNode   string
	Reason   string
}

// NewDistributor creates a new distributor
func NewDistributor(nodeID, region string, strategy DistributionStrategy, logger *slog.Logger) *Distributor {
	return &Distributor{
		nodeID:             nodeID,
		region:             region,
		strategy:           strategy,
		nodeLoads:          make(map[string]*NodeLoad),
		soulMap:            make(map[string]string),
		rebalanceThreshold: 0.2, // 20% imbalance threshold
		rebalanceInterval:  5 * time.Minute,
		failoverTimeout:    30 * time.Second,
		logger:             logger.With("component", "distributor"),
		stopCh:             make(chan struct{}),
	}
}

// Start starts the distributor
func (d *Distributor) Start() {
	d.logger.Info("Starting distributor",
		"strategy", d.strategy.String(),
		"rebalance_interval", d.rebalanceInterval)

	d.wg.Add(1)
	go d.rebalanceLoop()
}

// Stop stops the distributor
func (d *Distributor) Stop() {
	close(d.stopCh)
	d.wg.Wait()
}

// RegisterNode registers a node in the cluster
func (d *Distributor) RegisterNode(nodeID, region string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.nodeLoads[nodeID] = &NodeLoad{
		NodeID:        nodeID,
		Region:        region,
		LastHeartbeat: time.Now(),
		Healthy:       true,
	}

	d.logger.Info("Node registered", "node_id", nodeID, "region", region)
}

// UnregisterNode removes a node and triggers failover
func (d *Distributor) UnregisterNode(nodeID string) {
	d.mu.Lock()

	node, exists := d.nodeLoads[nodeID]
	if !exists {
		d.mu.Unlock()
		return
	}

	node.Healthy = false

	// Copy soul IDs to reassign while holding the lock (copy-on-write pattern)
	var soulsToMove []string
	for soulID, assignedNode := range d.soulMap {
		if assignedNode == nodeID {
			soulsToMove = append(soulsToMove, soulID)
		}
	}

	delete(d.nodeLoads, nodeID)
	d.mu.Unlock()

	// Reassign souls outside the lock
	for _, soulID := range soulsToMove {
		if err := d.ReassignSoul(soulID); err != nil {
			d.logger.Error("Failed to reassign soul after node failure",
				"soul_id", soulID, "failed_node", nodeID, "error", err)
		}
	}

	d.logger.Info("Node unregistered", "node_id", nodeID, "souls_moved", len(soulsToMove))
}

// UpdateNodeLoad updates load information for a node
func (d *Distributor) UpdateNodeLoad(nodeID string, cpu, memory float64, soulCount int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if node, exists := d.nodeLoads[nodeID]; exists {
		node.CPUUsage = cpu
		node.MemoryUsage = memory
		node.SoulCount = soulCount
		node.LastHeartbeat = time.Now()
		node.Healthy = true
	}
}

// AssignSoul assigns a soul to a node
func (d *Distributor) AssignSoul(soul *core.Soul) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if already assigned
	if nodeID, exists := d.soulMap[soul.ID]; exists {
		return nodeID, nil
	}

	// Select node based on strategy
	selectedNode := d.selectNodeForSoul(soul)
	if selectedNode == "" {
		return "", fmt.Errorf("no healthy nodes available")
	}

	// Assign soul
	d.soulMap[soul.ID] = selectedNode
	if node, exists := d.nodeLoads[selectedNode]; exists {
		node.SoulCount++
	}

	d.logger.Debug("Soul assigned",
		"soul_id", soul.ID,
		"soul_name", soul.Name,
		"node_id", selectedNode)

	return selectedNode, nil
}

// UnassignSoul removes a soul assignment
func (d *Distributor) UnassignSoul(soulID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if nodeID, exists := d.soulMap[soulID]; exists {
		if node, nodeExists := d.nodeLoads[nodeID]; nodeExists {
			node.SoulCount--
		}
		delete(d.soulMap, soulID)
	}
}

// ReassignSoul moves a soul to a different node
func (d *Distributor) ReassignSoul(soulID string) error {
	d.mu.Lock()
	currentNode, exists := d.soulMap[soulID]
	if !exists {
		d.mu.Unlock()
		return fmt.Errorf("soul not assigned: %s", soulID)
	}
	d.mu.Unlock()

	// Unassign first
	d.UnassignSoul(soulID)

	// Note: Region-aware routing would require fetching the full soul object from storage.
	// For now, we create a minimal soul object. The region field won't be set,
	// so StrategyRegionAware will fall back to round-robin for this soul.
	soul := &core.Soul{ID: soulID}

	// Reassign
	newNode, err := d.AssignSoul(soul)
	if err != nil {
		// Try to restore old assignment
		d.mu.Lock()
		d.soulMap[soulID] = currentNode
		d.mu.Unlock()
		return err
	}

	d.logger.Info("Soul reassigned",
		"soul_id", soulID,
		"from_node", currentNode,
		"to_node", newNode)

	return nil
}

// GetNodeForSoul returns the node assigned to a soul
func (d *Distributor) GetNodeForSoul(soulID string) (string, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	nodeID, exists := d.soulMap[soulID]
	return nodeID, exists
}

// GetSoulsForNode returns all souls assigned to a node
func (d *Distributor) GetSoulsForNode(nodeID string) []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var souls []string
	for soulID, assignedNode := range d.soulMap {
		if assignedNode == nodeID {
			souls = append(souls, soulID)
		}
	}

	return souls
}

// GetLoadDistribution returns current load distribution
func (d *Distributor) GetLoadDistribution() map[string]*NodeLoad {
	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make(map[string]*NodeLoad)
	for k, v := range d.nodeLoads {
		result[k] = v
	}

	return result
}

// selectNodeForSoul selects the best node for a soul based on strategy
func (d *Distributor) selectNodeForSoul(soul *core.Soul) string {
	var candidates []*NodeLoad

	for _, node := range d.nodeLoads {
		if node.Healthy {
			candidates = append(candidates, node)
		}
	}

	if len(candidates) == 0 {
		return ""
	}

	switch d.strategy {
	case StrategyRoundRobin:
		return d.selectRoundRobin(candidates)
	case StrategyRegionAware:
		return d.selectRegionAware(candidates, soul)
	case StrategyLoadBased:
		return d.selectLoadBased(candidates)
	case StrategyHashBased:
		return d.selectHashBased(candidates, soul.ID)
	default:
		return d.selectRoundRobin(candidates)
	}
}

// selectRoundRobin selects nodes in round-robin fashion
func (d *Distributor) selectRoundRobin(candidates []*NodeLoad) string {
	// Simple implementation: pick the node with least souls
	var selected *NodeLoad
	for _, node := range candidates {
		if selected == nil || node.SoulCount < selected.SoulCount {
			selected = node
		}
	}
	if selected != nil {
		return selected.NodeID
	}
	return ""
}

// selectRegionAware prefers nodes in the same region
func (d *Distributor) selectRegionAware(candidates []*NodeLoad, soul *core.Soul) string {
	// First, try same region
	var sameRegion []*NodeLoad
	for _, node := range candidates {
		if node.Region == d.region {
			sameRegion = append(sameRegion, node)
		}
	}

	// If same region nodes exist, pick least loaded
	if len(sameRegion) > 0 {
		return d.selectLoadBased(sameRegion)
	}

	// Otherwise pick from all candidates
	return d.selectLoadBased(candidates)
}

// selectLoadBased picks the least loaded node
func (d *Distributor) selectLoadBased(candidates []*NodeLoad) string {
	var selected *NodeLoad
	minLoad := float64(1<<63 - 1)

	for _, node := range candidates {
		// Calculate composite load score
		load := float64(node.SoulCount) + node.CPUUsage + node.MemoryUsage
		if load < minLoad {
			minLoad = load
			selected = node
		}
	}

	if selected != nil {
		return selected.NodeID
	}
	return ""
}

// selectHashBased uses consistent hashing
func (d *Distributor) selectHashBased(candidates []*NodeLoad, key string) string {
	if len(candidates) == 0 {
		return ""
	}

	// Simple hash-based selection
	// For production, use a proper consistent hashing implementation
	hash := 0
	for i := 0; i < len(key); i++ {
		hash = 31*hash + int(key[i])
	}

	index := int(math.Abs(float64(hash))) % len(candidates)
	return candidates[index].NodeID
}

// rebalanceLoop periodically checks and rebalances load
func (d *Distributor) rebalanceLoop() {
	defer d.wg.Done()
	ticker := time.NewTicker(d.rebalanceInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.checkAndRebalance()
		case <-d.stopCh:
			return
		}
	}
}

// checkAndRebalance checks load distribution and rebalances if needed
func (d *Distributor) checkAndRebalance() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.nodeLoads) < 2 {
		return
	}

	// Calculate average load
	var totalLoad float64
	nodeCount := 0
	for _, node := range d.nodeLoads {
		if node.Healthy {
			totalLoad += float64(node.SoulCount)
			nodeCount++
		}
	}

	if nodeCount == 0 {
		return
	}

	avgLoad := totalLoad / float64(nodeCount)

	// Find overloaded and underloaded nodes
	var overloaded, underloaded []*NodeLoad
	for _, node := range d.nodeLoads {
		if !node.Healthy {
			continue
		}

		load := float64(node.SoulCount)
		deviation := (load - avgLoad) / avgLoad

		if deviation > d.rebalanceThreshold {
			overloaded = append(overloaded, node)
		} else if deviation < -d.rebalanceThreshold {
			underloaded = append(underloaded, node)
		}
	}

	if len(overloaded) == 0 || len(underloaded) == 0 {
		return
	}

	// Sort by load
	sort.Slice(overloaded, func(i, j int) bool {
		return overloaded[i].SoulCount > overloaded[j].SoulCount
	})
	sort.Slice(underloaded, func(i, j int) bool {
		return underloaded[i].SoulCount < underloaded[j].SoulCount
	})

	// Calculate moves needed
	var moves []SoulMove
	for _, over := range overloaded {
		for _, under := range underloaded {
			if over.SoulCount <= int(avgLoad) {
				break
			}
			if under.SoulCount >= int(avgLoad) {
				continue
			}

			// Find souls to move
			for soulID, nodeID := range d.soulMap {
				if nodeID == over.NodeID {
					moves = append(moves, SoulMove{
						SoulID:   soulID,
						FromNode: over.NodeID,
						ToNode:   under.NodeID,
						Reason:   "rebalance",
					})

					over.SoulCount--
					under.SoulCount++

					if over.SoulCount <= int(avgLoad) {
						break
					}
				}
			}
		}
	}

	if len(moves) > 0 {
		d.logger.Info("Rebalancing cluster",
			"moves", len(moves),
			"overloaded", len(overloaded),
			"underloaded", len(underloaded))

		// Execute moves - use separate lock to avoid blocking other readers
		// while moves are being executed
		d.mu.Unlock()
		for _, move := range moves {
			if err := d.executeMove(move); err != nil {
				d.logger.Error("Failed to execute move",
					"soul_id", move.SoulID,
					"error", err)
			}
		}

		// Capture soul map state for callback before re-acquiring lock
		// This ensures callback sees consistent state
		soulMapCopy := make(map[string]string, len(d.soulMap))
		for k, v := range d.soulMap {
			soulMapCopy[k] = v
		}
		d.mu.Lock()

		// Log rebalance completion before calling callback
		d.logger.Info("Rebalance moves completed",
			"moves_executed", len(moves))

		if d.onRebalance != nil {
			// Pass a consistent copy of the soul map state to callback
			d.onRebalance(moves)
		}
	}
}

// executeMove executes a soul move
func (d *Distributor) executeMove(move SoulMove) error {
	d.mu.Lock()
	d.soulMap[move.SoulID] = move.ToNode
	d.mu.Unlock()

	d.logger.Debug("Soul moved",
		"soul_id", move.SoulID,
		"from", move.FromNode,
		"to", move.ToNode,
		"reason", move.Reason)

	return nil
}

// SetCallbacks sets the distribution callbacks
func (d *Distributor) SetCallbacks(
	onAssign func(soulID string, nodeID string) error,
	onUnassign func(soulID string) error,
	onRebalance func(moves []SoulMove),
) {
	d.onAssignSoul = onAssign
	d.onUnassignSoul = onUnassign
	d.onRebalance = onRebalance
}

// String returns the string representation of a strategy
func (s DistributionStrategy) String() string {
	switch s {
	case StrategyRoundRobin:
		return "round_robin"
	case StrategyRegionAware:
		return "region_aware"
	case StrategyLoadBased:
		return "load_based"
	case StrategyHashBased:
		return "hash_based"
	default:
		return "unknown"
	}
}
