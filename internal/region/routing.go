package region

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Router handles region-aware request routing
// The Aaru - the field of reeds where the path between regions is guided
type Router struct {
	mu        sync.RWMutex
	config    RoutingConfig
	manager   *Manager
	routes    map[string]*Route
	latencies map[string]time.Duration // region -> latency
	logger    *slog.Logger
}

// RoutingConfig contains routing settings
type RoutingConfig struct {
	Enabled         bool
	DefaultRegion   string
	LatencyBased    bool
	GeoBased        bool
	HealthBased     bool
	StickySessions  bool
	FailoverTimeout time.Duration
	MaxHops         int
}

// Route represents a routing rule
type Route struct {
	ID        string
	Path      string
	Method    string
	Regions   []string
	Priority  int
	Active    bool
	CreatedAt time.Time
}

// RoutingDecision represents a routing decision
type RoutingDecision struct {
	TargetRegion string
	TargetNode   string
	Endpoint     string
	Latency      time.Duration
	Reason       string
	ViaGateway   string
}

// NewRouter creates a new region router
func NewRouter(cfg RoutingConfig, manager *Manager, logger *slog.Logger) *Router {
	return &Router{
		config:    cfg,
		manager:   manager,
		routes:    make(map[string]*Route),
		latencies: make(map[string]time.Duration),
		logger:    logger.With("component", "region_router"),
	}
}

// RouteRequest routes an incoming request to the best region
func (r *Router) RouteRequest(req *http.Request) (*RoutingDecision, error) {
	if !r.config.Enabled {
		return &RoutingDecision{
			TargetRegion: r.manager.GetLocalRegion(),
			Reason:       "routing_disabled",
		}, nil
	}

	// Extract client information
	clientIP := extractClientIP(req)
	clientLat, clientLon := r.getClientLocation(clientIP)

	// Get healthy regions
	regions := r.manager.ListHealthyRegions()
	if len(regions) == 0 {
		return nil, fmt.Errorf("no healthy regions available")
	}

	// If local region is healthy, prefer it
	localRegion := r.manager.GetLocalRegion()
	for _, region := range regions {
		if region.ID == localRegion {
			return &RoutingDecision{
				TargetRegion: region.ID,
				Endpoint:     region.Endpoint,
				Latency:      region.Latency,
				Reason:       "local_region",
			}, nil
		}
	}

	// Select best region based on routing strategy
	var selected *Region

	switch {
	case r.config.GeoBased:
		selected = r.selectByGeography(regions, clientLat, clientLon)
	case r.config.LatencyBased:
		selected = r.selectByLatency(regions)
	case r.config.HealthBased:
		selected = r.selectByHealth(regions)
	default:
		selected = r.selectByLoad(regions)
	}

	if selected == nil {
		return nil, fmt.Errorf("could not select a target region")
	}

	return &RoutingDecision{
		TargetRegion: selected.ID,
		Endpoint:     selected.Endpoint,
		Latency:      selected.Latency,
		Reason:       "optimal_selection",
	}, nil
}

// RouteForSoul routes requests for a specific soul
func (r *Router) RouteForSoul(soulID string) (*RoutingDecision, error) {
	// Find which region has the soul
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Get all regions and check which one has the soul
	regions := r.manager.ListHealthyRegions()

	// For now, route to the local region
	// In a full implementation, we'd track soul location
	localRegion := r.manager.GetLocalRegion()

	for _, region := range regions {
		if region.ID == localRegion {
			return &RoutingDecision{
				TargetRegion: region.ID,
				Endpoint:     region.Endpoint,
				Reason:       "soul_locality",
			}, nil
		}
	}

	// Fallback to first available region
	if len(regions) > 0 {
		return &RoutingDecision{
			TargetRegion: regions[0].ID,
			Endpoint:     regions[0].Endpoint,
			Reason:       "fallback",
		}, nil
	}

	return nil, fmt.Errorf("no regions available")
}

// AddRoute adds a routing rule
func (r *Router) AddRoute(route *Route) error {
	if route.ID == "" {
		route.ID = generateRouteID()
	}

	route.CreatedAt = time.Now()
	route.Active = true

	r.mu.Lock()
	defer r.mu.Unlock()

	r.routes[route.ID] = route

	r.logger.Info("Route added",
		"route_id", route.ID,
		"path", route.Path,
		"method", route.Method)

	return nil
}

// RemoveRoute removes a routing rule
func (r *Router) RemoveRoute(routeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.routes[routeID]; !exists {
		return fmt.Errorf("route not found: %s", routeID)
	}

	delete(r.routes, routeID)

	r.logger.Info("Route removed", "route_id", routeID)
	return nil
}

// GetRoutes returns all routing rules
func (r *Router) GetRoutes() []*Route {
	r.mu.RLock()
	defer r.mu.RUnlock()

	routes := make([]*Route, 0, len(r.routes))
	for _, route := range r.routes {
		routes = append(routes, route)
	}

	return routes
}

// UpdateLatency updates the measured latency to a region
func (r *Router) UpdateLatency(regionID string, latency time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.latencies[regionID] = latency
}

// GetLatency returns the measured latency to a region
func (r *Router) GetLatency(regionID string) time.Duration {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if latency, exists := r.latencies[regionID]; exists {
		return latency
	}

	return 0
}

// selectByGeography selects the closest region by geography
func (r *Router) selectByGeography(regions []*Region, clientLat, clientLon float64) *Region {
	if clientLat == 0 && clientLon == 0 {
		return r.selectByLoad(regions)
	}

	var best *Region
	bestDistance := float64(1<<63 - 1)

	for _, region := range regions {
		distance := haversineDistance(clientLat, clientLon, region.Latitude, region.Longitude)
		if distance < bestDistance {
			bestDistance = distance
			best = region
		}
	}

	return best
}

// selectByLatency selects the region with lowest latency
func (r *Router) selectByLatency(regions []*Region) *Region {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var best *Region
	bestLatency := time.Duration(1<<63 - 1)

	for _, region := range regions {
		latency := region.Latency
		if measured, exists := r.latencies[region.ID]; exists && measured > 0 {
			latency = measured
		}

		if latency < bestLatency {
			bestLatency = latency
			best = region
		}
	}

	if best == nil && len(regions) > 0 {
		return regions[0]
	}

	return best
}

// selectByHealth selects the healthiest region
func (r *Router) selectByHealth(regions []*Region) *Region {
	var best *Region
	bestScore := -1.0

	for _, region := range regions {
		score := float64(region.NodeCount) * 100.0
		if region.Healthy {
			score += 1000.0
		}
		score -= float64(region.Latency.Milliseconds())

		if score > bestScore {
			bestScore = score
			best = region
		}
	}

	return best
}

// selectByLoad selects the least loaded region
func (r *Router) selectByLoad(regions []*Region) *Region {
	var best *Region
	bestLoad := int(^uint(0) >> 1) // Max int

	for _, region := range regions {
		load := region.SoulCount
		if load < bestLoad {
			bestLoad = load
			best = region
		}
	}

	return best
}

// getClientLocation returns the geographic location of a client
func (r *Router) getClientLocation(clientIP string) (float64, float64) {
	// In a real implementation, this would use a GeoIP database
	// For now, return 0,0 (unknown)
	return 0, 0
}

// extractClientIP extracts the client IP from a request
func extractClientIP(req *http.Request) string {
	// Check X-Forwarded-For header
	xff := req.Header.Get("X-Forwarded-For")
	if xff != "" {
		// Use the first IP in the chain
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	xri := req.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}

// generateRouteID generates a unique route ID
func generateRouteID() string {
	return fmt.Sprintf("route-%d-%s", time.Now().UnixNano(), generateRandomString(6))
}

// CreateProxyRequest creates a proxy request to another region
func (r *Router) CreateProxyRequest(req *http.Request, decision *RoutingDecision) (*http.Request, error) {
	// Create new URL pointing to target region
	targetURL := *req.URL
	targetURL.Host = decision.Endpoint
	targetURL.Scheme = "https"

	// Create new request
	proxyReq, err := http.NewRequest(req.Method, targetURL.String(), req.Body)
	if err != nil {
		return nil, err
	}

	// Copy headers
	for key, values := range req.Header {
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	// Add routing headers
	proxyReq.Header.Set("X-Forwarded-Region", r.manager.GetLocalRegion())
	proxyReq.Header.Set("X-Target-Region", decision.TargetRegion)

	return proxyReq, nil
}

// IsLocalRegion checks if a request should be handled locally
func (r *Router) IsLocalRegion(decision *RoutingDecision) bool {
	return decision.TargetRegion == r.manager.GetLocalRegion()
}

// GetRoutingMetrics returns routing metrics
func (r *Router) GetRoutingMetrics() RoutingMetrics {
	r.mu.RLock()
	defer r.mu.RUnlock()

	metrics := RoutingMetrics{
		RegionLatencies: make(map[string]time.Duration),
		RouteCount:      len(r.routes),
	}

	for region, latency := range r.latencies {
		metrics.RegionLatencies[region] = latency
	}

	return metrics
}

// RoutingMetrics contains routing performance metrics
type RoutingMetrics struct {
	RegionLatencies map[string]time.Duration `json:"region_latencies"`
	RouteCount      int                      `json:"route_count"`
	TotalRequests   int64                    `json:"total_requests"`
	FailedRequests  int64                    `json:"failed_requests"`
}
