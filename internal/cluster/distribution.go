package cluster

import (
	"fmt"
	"hash/fnv"
	"log/slog"
	"sort"
	"strings"
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

	logger      *slog.Logger
	stopCh      chan struct{}
	wg          sync.WaitGroup
	lifecycleMu sync.Mutex
	started     bool
	stopped     bool
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

// Start starts the distributor. Repeated calls and calls after Stop are no-ops.
func (d *Distributor) Start() {
	d.lifecycleMu.Lock()
	if d.started || d.stopped {
		d.lifecycleMu.Unlock()
		return
	}
	d.started = true
	d.wg.Add(1)
	d.lifecycleMu.Unlock()

	d.mu.RLock()
	strategy := d.strategy
	interval := d.rebalanceInterval
	d.mu.RUnlock()
	d.logger.Info("Starting distributor",
		"strategy", strategy.String(),
		"rebalance_interval", interval)
	go d.rebalanceLoop(interval)
}

// Stop stops the distributor. It is safe before Start and on repeated calls.
func (d *Distributor) Stop() {
	d.lifecycleMu.Lock()
	if !d.stopped {
		d.stopped = true
		close(d.stopCh)
	}
	d.lifecycleMu.Unlock()
	d.wg.Wait()
}

// RegisterNode registers a node in the cluster
func (d *Distributor) RegisterNode(nodeID, region string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	node, exists := d.nodeLoads[nodeID]
	if !exists {
		node = &NodeLoad{NodeID: nodeID}
		d.nodeLoads[nodeID] = node
	}
	node.Region = region
	node.LastHeartbeat = time.Now()
	node.Healthy = true

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

// Heartbeat records that a node is alive without touching its load figures. The
// manager drives this from Raft's per-peer liveness, so a node that is up keeps
// being assigned souls while one that stops being reachable goes stale after
// failoverTimeout and is failed over.
func (d *Distributor) Heartbeat(nodeID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	node, exists := d.nodeLoads[nodeID]
	if !exists {
		return
	}
	node.LastHeartbeat = time.Now()
	node.Healthy = true
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
		if node, exists := d.nodeLoads[currentNode]; exists {
			node.SoulCount++
		}
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

	result := make(map[string]*NodeLoad, len(d.nodeLoads))
	for k, v := range d.nodeLoads {
		load := *v
		result[k] = &load
	}

	return result
}

// nodeStale reports whether a node has stopped heartbeating for longer than the
// configured failover timeout. LastHeartbeat is refreshed by RegisterNode and
// UpdateNodeLoad and read nowhere else, so without this comparison a node that dies
// silently keeps its Healthy flag forever.
func (d *Distributor) nodeStale(node *NodeLoad) bool {
	// A zero LastHeartbeat means the node was never seen through RegisterNode or
	// UpdateNodeLoad (both always set it), so there is no staleness to judge. A node
	// that registered and then stopped heartbeating has a non-zero timestamp and is
	// detected here.
	if node.LastHeartbeat.IsZero() {
		return false
	}
	return d.failoverTimeout > 0 && time.Since(node.LastHeartbeat) > d.failoverTimeout
}

// failoverStaleNodes marks nodes past the failover timeout unhealthy and reassigns
// the souls they were holding, mirroring what UnregisterNode does for an explicit
// departure. Without it those souls stay mapped to a node that will never check
// them again, which is exactly what failoverTimeout exists to trigger.
func (d *Distributor) failoverStaleNodes() {
	d.mu.Lock()
	var staleNodes []string
	for nodeID, node := range d.nodeLoads {
		if !node.Healthy || !d.nodeStale(node) {
			continue
		}
		node.Healthy = false
		staleNodes = append(staleNodes, nodeID)
	}

	var soulsToMove []string
	if len(staleNodes) > 0 {
		stale := make(map[string]bool, len(staleNodes))
		for _, nodeID := range staleNodes {
			stale[nodeID] = true
		}
		for soulID, nodeID := range d.soulMap {
			if stale[nodeID] {
				soulsToMove = append(soulsToMove, soulID)
			}
		}
	}
	d.mu.Unlock()

	for _, nodeID := range staleNodes {
		d.logger.Warn("Node missed its heartbeat and was marked unhealthy", "node_id", nodeID)
	}
	for _, soulID := range soulsToMove {
		if err := d.ReassignSoul(soulID); err != nil {
			d.logger.Error("Failed to reassign soul from a node that stopped heartbeating",
				"soul_id", soulID, "error", err)
		}
	}
}

// selectNodeForSoul selects the best node for a soul based on strategy
func (d *Distributor) selectNodeForSoul(soul *core.Soul) string {
	var candidates []*NodeLoad

	for _, node := range d.nodeLoads {
		if node.Healthy && !d.nodeStale(node) {
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

// selectHashBased uses rendezvous (highest-random-weight) hashing: each node
// scores hash(key, node) and the highest score wins. The owner depends only on
// the key and the candidate set, never on its order, and when a node joins or
// leaves only the keys that node wins or held change owner -- a modulo hash
// would reshuffle nearly every key.
func (d *Distributor) selectHashBased(candidates []*NodeLoad, key string) string {
	return rendezvousOwner(candidates, key)
}

func rendezvousScore(key, nodeID string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(key))
	h.Write([]byte{0})
	h.Write([]byte(nodeID))
	return h.Sum64()
}

func rendezvousOwner(candidates []*NodeLoad, key string) string {
	best, bestScore := "", uint64(0)
	for _, node := range candidates {
		score := rendezvousScore(key, node.NodeID)
		if best == "" || score > bestScore || (score == bestScore && node.NodeID < best) {
			best, bestScore = node.NodeID, score
		}
	}
	return best
}

// rendezvousRank orders candidates by descending score for key. Its first
// element is rendezvousOwner's answer, so a redundant plan's primary is the
// owner a single-owner plan would pick.
func rendezvousRank(candidates []*NodeLoad, key string) []*NodeLoad {
	ranked := append([]*NodeLoad(nil), candidates...)
	sort.Slice(ranked, func(i, j int) bool {
		si, sj := rendezvousScore(key, ranked[i].NodeID), rendezvousScore(key, ranked[j].NodeID)
		if si != sj {
			return si > sj
		}
		return ranked[i].NodeID < ranked[j].NodeID
	})
	return ranked
}

// ParseStrategy maps a necropolis.distribution.strategy value onto a strategy.
// The empty string and unrecognised values (including the documented
// "latency-optimized" and "redundant", which have no implementation) fall back
// to hash-based ownership; ok reports whether the value was recognised.
func ParseStrategy(name string) (strategy DistributionStrategy, ok bool) {
	switch strings.NewReplacer("_", "-", " ", "-").Replace(strings.ToLower(strings.TrimSpace(name))) {
	case "", "hash", "hash-based", "consistent-hash", "consistent-hashing", "rendezvous":
		return StrategyHashBased, true
	case "round-robin", "roundrobin":
		return StrategyRoundRobin, true
	case "region-aware", "region":
		return StrategyRegionAware, true
	case "load-based", "least-loaded":
		return StrategyLoadBased, true
	default:
		return StrategyHashBased, false
	}
}

// Rebuild recomputes the owner of every soul over members and replaces the
// assignment table with the result, so GetNodeForSoul and GetSoulsForNode
// answer from it. Unlike AssignSoul it is deterministic: the outcome depends
// only on the soul set, the member set and the strategy -- never on call order,
// map iteration or this node's own region -- so every node computes the same
// table from the same replicated inputs without coordinating.
func (d *Distributor) Rebuild(souls []*core.Soul, members []NodeLoad) map[string]string {
	d.mu.RLock()
	strategy := d.strategy
	d.mu.RUnlock()

	plan := PlanOwnership(strategy, souls, members)

	d.mu.Lock()
	defer d.mu.Unlock()
	d.soulMap = make(map[string]string, len(plan))
	counts := make(map[string]int, len(members))
	for soulID, nodeID := range plan {
		d.soulMap[soulID] = nodeID
		counts[nodeID]++
	}
	for nodeID, node := range d.nodeLoads {
		node.SoulCount = counts[nodeID]
	}
	return plan
}

// PlanOwnership assigns each soul to one member under strategy and returns
// soul ID -> node ID. Souls are visited in ID order and ties go to the lowest
// node ID, so the plan is a pure function of its inputs. A soul restricted to
// regions (Soul.Regions) is only given to a member serving one of them, and is
// left out of the plan when none does -- a standalone probe in the wrong
// region skips such a soul the same way.
//
// Round-robin and load-based keep counts exactly even but may move other souls
// when one is added or removed; hash-based moves only the souls whose owner
// joined or left; region-aware prefers members in the soul's Region and
// balances by count within that choice.
func PlanOwnership(strategy DistributionStrategy, souls []*core.Soul, members []NodeLoad) map[string]string {
	sets := PlanOwnerSets(strategy, souls, members, 1)
	plan := make(map[string]string, len(sets))
	for soulID, owners := range sets {
		plan[soulID] = owners[0]
	}
	return plan
}

// PlanOwnerSets is PlanOwnership with redundancy: each soul gets up to
// redundancy distinct members, primary first. The primary is exactly what
// PlanOwnership returns, so redundancy 1 (or less) is the single-owner plan and
// raising it never moves a primary. The other members follow the strategy:
// hash takes the next highest rendezvous scores (so a membership change only
// touches the sets that involved the changed member); the other strategies take
// the least-loaded remaining members by count of secondary assignments, with
// region-aware preferring the soul's Region first. A soul with fewer eligible
// members than redundancy gets all of them, so redundancy degrades instead of
// leaving a soul unprobed. Like PlanOwnership it is a pure function of its
// inputs.
func PlanOwnerSets(strategy DistributionStrategy, souls []*core.Soul, members []NodeLoad, redundancy int) map[string][]string {
	nodes := make([]*NodeLoad, 0, len(members))
	seen := make(map[string]bool, len(members))
	for i := range members {
		if members[i].NodeID == "" || seen[members[i].NodeID] {
			continue
		}
		seen[members[i].NodeID] = true
		member := members[i]
		nodes = append(nodes, &member)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].NodeID < nodes[j].NodeID })

	ordered := make([]*core.Soul, 0, len(souls))
	for _, soul := range souls {
		if soul != nil && soul.ID != "" {
			ordered = append(ordered, soul)
		}
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })

	counts := make(map[string]int, len(nodes))
	leastLoaded := func(candidates []*NodeLoad) string {
		best := ""
		for _, node := range candidates {
			if best == "" || counts[node.NodeID] < counts[best] {
				best = node.NodeID
			}
		}
		return best
	}

	secondaryCounts := make(map[string]int, len(nodes))
	plan := make(map[string][]string, len(ordered))
	for _, soul := range ordered {
		candidates := nodes
		if len(soul.Regions) > 0 {
			candidates = make([]*NodeLoad, 0, len(nodes))
			for _, node := range nodes {
				for _, region := range soul.Regions {
					if node.Region == region {
						candidates = append(candidates, node)
						break
					}
				}
			}
		}
		if len(candidates) == 0 {
			continue
		}

		var owner string
		switch strategy {
		case StrategyHashBased:
			owner = rendezvousOwner(candidates, soul.ID)
		case StrategyRegionAware:
			preferred := candidates
			if soul.Region != "" {
				var same []*NodeLoad
				for _, node := range candidates {
					if node.Region == soul.Region {
						same = append(same, node)
					}
				}
				if len(same) > 0 {
					preferred = same
				}
			}
			owner = leastLoaded(preferred)
		default:
			owner = leastLoaded(candidates)
		}
		owners := []string{owner}
		counts[owner]++

		if redundancy > 1 && len(candidates) > 1 {
			rest := make([]*NodeLoad, 0, len(candidates)-1)
			for _, node := range candidates {
				if node.NodeID != owner {
					rest = append(rest, node)
				}
			}
			for _, node := range pickSecondaries(strategy, soul, rest, secondaryCounts, redundancy-1) {
				owners = append(owners, node)
				secondaryCounts[node]++
			}
		}
		plan[soul.ID] = owners
	}
	return plan
}

// pickSecondaries chooses up to want members from rest for soul's backup
// probes. rest is in node-ID order, which makes every tie deterministic.
func pickSecondaries(strategy DistributionStrategy, soul *core.Soul, rest []*NodeLoad, counts map[string]int, want int) []string {
	var ranked []*NodeLoad
	if strategy == StrategyHashBased {
		ranked = rendezvousRank(rest, soul.ID)
	} else {
		ranked = append([]*NodeLoad(nil), rest...)
		sameRegion := func(n *NodeLoad) bool {
			return strategy == StrategyRegionAware && soul.Region != "" && n.Region == soul.Region
		}
		sort.SliceStable(ranked, func(i, j int) bool {
			if si, sj := sameRegion(ranked[i]), sameRegion(ranked[j]); si != sj {
				return si
			}
			return counts[ranked[i].NodeID] < counts[ranked[j].NodeID]
		})
	}
	if len(ranked) > want {
		ranked = ranked[:want]
	}
	out := make([]string, len(ranked))
	for i, n := range ranked {
		out[i] = n.NodeID
	}
	return out
}

// rebalanceLoop periodically checks and rebalances load.
func (d *Distributor) rebalanceLoop(interval time.Duration) {
	defer d.wg.Done()
	ticker := time.NewTicker(interval)
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
	// Fail over nodes that stopped heartbeating first: their souls must move even
	// when the load itself is balanced, and even in a single-node cluster.
	d.failoverStaleNodes()

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
	planned := make(map[string]bool)
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
				if nodeID == over.NodeID && !planned[soulID] {
					moves = append(moves, SoulMove{
						SoulID:   soulID,
						FromNode: over.NodeID,
						ToNode:   under.NodeID,
						Reason:   "rebalance",
					})

					over.SoulCount--
					under.SoulCount++
					planned[soulID] = true

					if over.SoulCount <= int(avgLoad) || under.SoulCount >= int(avgLoad) {
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

		d.mu.Lock()
		callback := d.onRebalance

		// Log rebalance completion before calling callback.
		d.logger.Info("Rebalance moves completed",
			"moves_executed", len(moves))

		if callback != nil {
			d.mu.Unlock()
			callback(moves)
			d.mu.Lock()
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
	d.mu.Lock()
	d.onAssignSoul = onAssign
	d.onUnassignSoul = onUnassign
	d.onRebalance = onRebalance
	d.mu.Unlock()
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
