package region

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net"
	"sync"
	"time"
)

// Region represents a geographic/datacenter region
// The Lands of Egypt - divided into nomes (regions)
type Region struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Endpoint   string            `json:"endpoint"`
	Location   string            `json:"location"`
	Latitude   float64           `json:"latitude"`
	Longitude  float64           `json:"longitude"`
	Priority   int               `json:"priority"`
	Enabled    bool              `json:"enabled"`
	Metadata   map[string]string `json:"metadata"`
	LastHealth time.Time         `json:"last_health_check"`
	Healthy    bool              `json:"healthy"`
	Latency    time.Duration     `json:"latency"`
	NodeCount  int               `json:"node_count"`
	SoulCount  int               `json:"soul_count"`
}

// RegionInfo provides region information for routing decisions
type RegionInfo struct {
	RegionID  string
	Endpoint  string
	Latency   time.Duration
	Health    bool
	LoadScore float64
	Distance  float64 // Geographic distance from client
}

// Manager handles multi-region coordination
// The Nomarch - governor of the regions
type Manager struct {
	mu            sync.RWMutex
	localRegion   string
	regions       map[string]*Region
	nodesByRegion map[string][]string // region -> node IDs
	replication   *ReplicationManager
	router        *Router
	healthMonitor *HealthMonitor
	storage       Storage
	logger        *slog.Logger
	shutdownCh    chan struct{}
}

// Storage interface for region persistence
type Storage interface {
	SaveRegion(ctx context.Context, region *Region) error
	GetRegion(ctx context.Context, id string) (*Region, error)
	ListRegions(ctx context.Context) ([]*Region, error)
	DeleteRegion(ctx context.Context, id string) error
}

// ConflictStore provides entity-level storage for conflict detection
type ConflictStore interface {
	Storage
	GetEntityData(ctx context.Context, entityType, entityID string) (json.RawMessage, error)
	SaveEntityData(ctx context.Context, entityType, entityID string, data json.RawMessage) error
}

// Config contains region configuration
type Config struct {
	LocalRegion string
	Regions     []RegionConfig
	Replication ReplicationConfig
	Routing     RoutingConfig
	HealthCheck HealthConfig
}

// RegionConfig defines a region in config
type RegionConfig struct {
	ID        string
	Name      string
	Endpoint  string
	Location  string
	Latitude  float64
	Longitude float64
	Priority  int
	Enabled   bool
	Secret    string // For cross-region auth
}

// NewManager creates a new region manager
func NewManager(cfg Config, storage Storage, logger *slog.Logger) (*Manager, error) {
	m := &Manager{
		localRegion:   cfg.LocalRegion,
		regions:       make(map[string]*Region),
		nodesByRegion: make(map[string][]string),
		storage:       storage,
		logger:        logger.With("component", "region_manager"),
		shutdownCh:    make(chan struct{}),
	}

	// Initialize configured regions
	for _, rc := range cfg.Regions {
		region := &Region{
			ID:        rc.ID,
			Name:      rc.Name,
			Endpoint:  rc.Endpoint,
			Location:  rc.Location,
			Latitude:  rc.Latitude,
			Longitude: rc.Longitude,
			Priority:  rc.Priority,
			Enabled:   rc.Enabled,
			Healthy:   true,
			Metadata:  make(map[string]string),
		}
		m.regions[region.ID] = region
	}

	// Initialize replication manager
	m.replication = NewReplicationManager(cfg.Replication, m, logger)

	// Initialize router
	m.router = NewRouter(cfg.Routing, m, logger)

	// Initialize health monitor
	m.healthMonitor = NewHealthMonitor(cfg.HealthCheck, m, logger)

	return m, nil
}

// Start starts the region manager and all subsystems
func (m *Manager) Start(ctx context.Context) error {
	m.logger.Info("Starting region manager", "local_region", m.localRegion)

	// Load regions from storage
	if m.storage != nil {
		regions, err := m.storage.ListRegions(ctx)
		if err != nil {
			m.logger.Warn("Failed to load regions from storage", "error", err)
		} else {
			m.mu.Lock()
			for _, region := range regions {
				if _, exists := m.regions[region.ID]; !exists {
					m.regions[region.ID] = region
				}
			}
			m.mu.Unlock()
		}
	}

	// Start subsystems
	m.healthMonitor.Start(ctx)
	m.replication.Start(ctx)

	return nil
}

// Stop gracefully shuts down the region manager
func (m *Manager) Stop(ctx context.Context) error {
	m.logger.Info("Stopping region manager")

	close(m.shutdownCh)

	// Stop subsystems
	m.healthMonitor.Stop(ctx)
	m.replication.Stop(ctx)

	return nil
}

// GetLocalRegion returns the local region ID
func (m *Manager) GetLocalRegion() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.localRegion
}

// GetRegion returns a region by ID
func (m *Manager) GetRegion(id string) (*Region, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	region, exists := m.regions[id]
	if !exists {
		return nil, fmt.Errorf("region not found: %s", id)
	}

	// Return a copy
	return m.copyRegion(region), nil
}

// ListRegions returns all regions
func (m *Manager) ListRegions() []*Region {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Region, 0, len(m.regions))
	for _, region := range m.regions {
		result = append(result, m.copyRegion(region))
	}

	return result
}

// ListHealthyRegions returns all healthy regions
func (m *Manager) ListHealthyRegions() []*Region {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Region, 0)
	for _, region := range m.regions {
		if region.Healthy && region.Enabled {
			result = append(result, m.copyRegion(region))
		}
	}

	return result
}

// RegisterRegion registers a new region
func (m *Manager) RegisterRegion(ctx context.Context, region *Region) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if region.ID == "" {
		return fmt.Errorf("region ID is required")
	}

	region.LastHealth = time.Now()
	region.Healthy = true

	m.regions[region.ID] = region

	// Persist to storage
	if m.storage != nil {
		if err := m.storage.SaveRegion(ctx, region); err != nil {
			m.logger.Error("Failed to save region to storage", "error", err)
		}
	}

	m.logger.Info("Region registered", "region_id", region.ID, "name", region.Name)
	return nil
}

// UnregisterRegion removes a region
func (m *Manager) UnregisterRegion(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.regions[id]; !exists {
		return fmt.Errorf("region not found: %s", id)
	}

	delete(m.regions, id)
	delete(m.nodesByRegion, id)

	// Remove from storage
	if m.storage != nil {
		if err := m.storage.DeleteRegion(ctx, id); err != nil {
			m.logger.Error("Failed to delete region from storage", "error", err)
		}
	}

	m.logger.Info("Region unregistered", "region_id", id)
	return nil
}

// UpdateRegionHealth updates the health status of a region
func (m *Manager) UpdateRegionHealth(id string, healthy bool, latency time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	region, exists := m.regions[id]
	if !exists {
		return fmt.Errorf("region not found: %s", id)
	}

	wasHealthy := region.Healthy
	region.Healthy = healthy
	region.Latency = latency
	region.LastHealth = time.Now()

	// Log health transitions
	if wasHealthy && !healthy {
		m.logger.Warn("Region became unhealthy", "region_id", id, "latency", latency)
	} else if !wasHealthy && healthy {
		m.logger.Info("Region became healthy", "region_id", id, "latency", latency)
	}

	return nil
}

// RegisterNodeInRegion registers a node in a region
func (m *Manager) RegisterNodeInRegion(nodeID, regionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	region, exists := m.regions[regionID]
	if !exists {
		return fmt.Errorf("region not found: %s", regionID)
	}

	// Add node to region
	if m.nodesByRegion[regionID] == nil {
		m.nodesByRegion[regionID] = []string{}
	}

	// Check if already registered
	for _, id := range m.nodesByRegion[regionID] {
		if id == nodeID {
			return nil
		}
	}

	m.nodesByRegion[regionID] = append(m.nodesByRegion[regionID], nodeID)
	region.NodeCount = len(m.nodesByRegion[regionID])

	m.logger.Debug("Node registered in region", "node_id", nodeID, "region_id", regionID)
	return nil
}

// UnregisterNodeFromRegion removes a node from a region
func (m *Manager) UnregisterNodeFromRegion(nodeID, regionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	region, exists := m.regions[regionID]
	if !exists {
		return fmt.Errorf("region not found: %s", regionID)
	}

	nodes := m.nodesByRegion[regionID]
	newNodes := make([]string, 0, len(nodes))
	for _, id := range nodes {
		if id != nodeID {
			newNodes = append(newNodes, id)
		}
	}

	m.nodesByRegion[regionID] = newNodes
	region.NodeCount = len(newNodes)

	return nil
}

// GetNodesInRegion returns all nodes in a region
func (m *Manager) GetNodesInRegion(regionID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	nodes, exists := m.nodesByRegion[regionID]
	if !exists {
		return []string{}
	}

	// Return a copy
	result := make([]string, len(nodes))
	copy(result, nodes)
	return result
}

// GetRegionForNode returns the region a node belongs to
func (m *Manager) GetRegionForNode(nodeID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for regionID, nodes := range m.nodesByRegion {
		for _, id := range nodes {
			if id == nodeID {
				return regionID
			}
		}
	}

	return ""
}

// SelectBestRegion selects the best region for a client based on latency and load
func (m *Manager) SelectBestRegion(clientLat, clientLon float64) (*Region, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var bestRegion *Region
	bestScore := float64(1<<63 - 1)

	for _, region := range m.regions {
		if !region.Healthy || !region.Enabled {
			continue
		}

		// Calculate score (lower is better)
		// Weight: latency (40%), distance (30%), load (30%)
		latencyScore := float64(region.Latency.Milliseconds()) * 0.4
		distance := haversineDistance(clientLat, clientLon, region.Latitude, region.Longitude)
		distanceScore := distance * 0.3
		loadScore := float64(region.SoulCount) * 0.3

		score := latencyScore + distanceScore + loadScore

		// Apply priority bonus (higher priority = lower score)
		score -= float64(region.Priority) * 10

		if score < bestScore {
			bestScore = score
			bestRegion = m.copyRegion(region)
		}
	}

	if bestRegion == nil {
		return nil, fmt.Errorf("no healthy regions available")
	}

	return bestRegion, nil
}

// GetReplicationManager returns the replication manager
func (m *Manager) GetReplicationManager() *ReplicationManager {
	return m.replication
}

// GetRouter returns the region router
func (m *Manager) GetRouter() *Router {
	return m.router
}

// GetHealthMonitor returns the health monitor
func (m *Manager) GetHealthMonitor() *HealthMonitor {
	return m.healthMonitor
}

// copyRegion creates a deep copy of a region
func (m *Manager) copyRegion(region *Region) *Region {
	copy := &Region{
		ID:         region.ID,
		Name:       region.Name,
		Endpoint:   region.Endpoint,
		Location:   region.Location,
		Latitude:   region.Latitude,
		Longitude:  region.Longitude,
		Priority:   region.Priority,
		Enabled:    region.Enabled,
		LastHealth: region.LastHealth,
		Healthy:    region.Healthy,
		Latency:    region.Latency,
		NodeCount:  region.NodeCount,
		SoulCount:  region.SoulCount,
		Metadata:   make(map[string]string),
	}

	for k, v := range region.Metadata {
		copy.Metadata[k] = v
	}

	return copy
}

// haversineDistance calculates the distance between two coordinates in km
func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371 // Earth radius in km

	lat1Rad := lat1 * (3.14159265359 / 180)
	lat2Rad := lat2 * (3.14159265359 / 180)
	deltaLat := (lat2 - lat1) * (3.14159265359 / 180)
	deltaLon := (lon2 - lon1) * (3.14159265359 / 180)

	a := sin(deltaLat/2)*sin(deltaLat/2) +
		cos(lat1Rad)*cos(lat2Rad)*
			sin(deltaLon/2)*sin(deltaLon/2)
	c := 2 * atan2(sqrt(a), sqrt(1-a))

	return R * c
}

// Helper functions for haversine calculation
func sin(x float64) float64      { return math.Sin(x) }
func cos(x float64) float64      { return math.Cos(x) }
func sqrt(x float64) float64     { return math.Sqrt(x) }
func atan2(y, x float64) float64 { return math.Atan2(y, x) }

// GetRegionStatus returns the status of all regions
func (m *Manager) GetRegionStatus() map[string]RegionStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := make(map[string]RegionStatus)
	for id, region := range m.regions {
		status[id] = RegionStatus{
			ID:         id,
			Name:       region.Name,
			Healthy:    region.Healthy,
			Enabled:    region.Enabled,
			Latency:    region.Latency,
			NodeCount:  region.NodeCount,
			SoulCount:  region.SoulCount,
			LastHealth: region.LastHealth,
			Endpoint:   region.Endpoint,
		}
	}

	return status
}

// RegionStatus provides a summary of region health
type RegionStatus struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Healthy    bool          `json:"healthy"`
	Enabled    bool          `json:"enabled"`
	Latency    time.Duration `json:"latency"`
	NodeCount  int           `json:"node_count"`
	SoulCount  int           `json:"soul_count"`
	LastHealth time.Time     `json:"last_health_check"`
	Endpoint   string        `json:"endpoint"`
}

// ResolveRegionEndpoint resolves a region endpoint to an IP address
func ResolveRegionEndpoint(endpoint string) (string, error) {
	// Parse the endpoint
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		// Try as just a host
		host = endpoint
		port = "7946"
	}

	// Resolve the hostname
	addrs, err := net.LookupHost(host)
	if err != nil {
		return endpoint, err // Return original if resolution fails
	}

	if len(addrs) == 0 {
		return endpoint, fmt.Errorf("no addresses found for %s", host)
	}

	return net.JoinHostPort(addrs[0], port), nil
}
