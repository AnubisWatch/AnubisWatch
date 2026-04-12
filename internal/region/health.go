package region

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// HealthMonitor monitors the health of remote regions
// The Ibis - messenger that carries health status between regions
type HealthMonitor struct {
	mu      sync.RWMutex
	config  HealthConfig
	manager *Manager
	checks  map[string]*HealthCheck
	logger  *slog.Logger
	stopCh  chan struct{}
	client  *http.Client
}

// HealthConfig contains health monitoring settings
type HealthConfig struct {
	Enabled          bool
	Interval         time.Duration
	Timeout          time.Duration
	FailureThreshold int
	SuccessThreshold int
	Endpoints        map[string]string // region -> health endpoint
}

// HealthCheck tracks health status for a region
type HealthCheck struct {
	RegionID         string
	Endpoint         string
	ConsecutiveFails int
	ConsecutiveOK    int
	LastCheck        time.Time
	LastLatency      time.Duration
	mu               sync.RWMutex
}

// HealthStatus represents the health status of a region
type HealthStatus struct {
	RegionID     string        `json:"region_id"`
	Healthy      bool          `json:"healthy"`
	Latency      time.Duration `json:"latency"`
	LastCheck    time.Time     `json:"last_check"`
	FailCount    int           `json:"fail_count"`
	SuccessCount int           `json:"success_count"`
}

// NewHealthMonitor creates a new health monitor
func NewHealthMonitor(cfg HealthConfig, manager *Manager, logger *slog.Logger) *HealthMonitor {
	return &HealthMonitor{
		config:  cfg,
		manager: manager,
		checks:  make(map[string]*HealthCheck),
		logger:  logger.With("component", "region_health"),
		stopCh:  make(chan struct{}),
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// Start starts the health monitor
func (h *HealthMonitor) Start(ctx context.Context) {
	if !h.config.Enabled {
		h.logger.Info("Health monitoring is disabled")
		return
	}

	h.logger.Info("Starting health monitor", "interval", h.config.Interval)

	// Initialize health checks for existing regions
	regions := h.manager.ListRegions()
	for _, region := range regions {
		if region.ID == h.manager.GetLocalRegion() {
			continue // Don't check local region
		}

		h.AddRegion(region.ID, region.Endpoint)
	}

	// Start health check loop
	go h.healthCheckLoop(ctx)
}

// Stop stops the health monitor
func (h *HealthMonitor) Stop(ctx context.Context) {
	if !h.config.Enabled {
		return
	}

	h.logger.Info("Stopping health monitor")
	close(h.stopCh)
}

// AddRegion adds a region to health monitoring
func (h *HealthMonitor) AddRegion(regionID, endpoint string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Build health endpoint URL
	healthEndpoint := fmt.Sprintf("%s/api/v1/health", endpoint)
	if customEndpoint, exists := h.config.Endpoints[regionID]; exists {
		healthEndpoint = customEndpoint
	}

	h.checks[regionID] = &HealthCheck{
		RegionID: regionID,
		Endpoint: healthEndpoint,
	}

	h.logger.Debug("Added region to health monitoring",
		"region_id", regionID,
		"endpoint", healthEndpoint)
}

// RemoveRegion removes a region from health monitoring
func (h *HealthMonitor) RemoveRegion(regionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.checks, regionID)

	h.logger.Debug("Removed region from health monitoring", "region_id", regionID)
}

// GetStatus returns the health status of a region
func (h *HealthMonitor) GetStatus(regionID string) (*HealthStatus, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	check, exists := h.checks[regionID]
	if !exists {
		return nil, fmt.Errorf("region not found: %s", regionID)
	}

	check.mu.RLock()
	status := &HealthStatus{
		RegionID:     regionID,
		LastCheck:    check.LastCheck,
		Latency:      check.LastLatency,
		FailCount:    check.ConsecutiveFails,
		SuccessCount: check.ConsecutiveOK,
	}
	check.mu.RUnlock()

	// Determine health based on thresholds
	status.Healthy = check.ConsecutiveFails < h.config.FailureThreshold

	return status, nil
}

// GetAllStatuses returns health status for all monitored regions
func (h *HealthMonitor) GetAllStatuses() map[string]*HealthStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()

	statuses := make(map[string]*HealthStatus)
	for regionID, check := range h.checks {
		check.mu.RLock()
		status := &HealthStatus{
			RegionID:     regionID,
			LastCheck:    check.LastCheck,
			Latency:      check.LastLatency,
			FailCount:    check.ConsecutiveFails,
			SuccessCount: check.ConsecutiveOK,
			Healthy:      check.ConsecutiveFails < h.config.FailureThreshold,
		}
		check.mu.RUnlock()
		statuses[regionID] = status
	}

	return statuses
}

// healthCheckLoop runs periodic health checks
func (h *HealthMonitor) healthCheckLoop(ctx context.Context) {
	ticker := time.NewTicker(h.config.Interval)
	defer ticker.Stop()

	// Do initial checks
	h.runHealthChecks(ctx)

	for {
		select {
		case <-ticker.C:
			h.runHealthChecks(ctx)
		case <-h.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// runHealthChecks checks health of all regions
func (h *HealthMonitor) runHealthChecks(ctx context.Context) {
	h.mu.RLock()
	checks := make(map[string]*HealthCheck)
	for id, check := range h.checks {
		checks[id] = check
	}
	h.mu.RUnlock()

	for regionID, check := range checks {
		go h.checkHealth(ctx, regionID, check)
	}
}

// checkHealth performs a health check for a region
func (h *HealthMonitor) checkHealth(ctx context.Context, regionID string, check *HealthCheck) {
	start := time.Now()

	// Perform health check
	req, err := http.NewRequestWithContext(ctx, "GET", check.Endpoint, nil)
	if err != nil {
		h.updateHealthStatus(check, false, 0)
		return
	}

	resp, err := h.client.Do(req)
	latency := time.Since(start)

	if err != nil {
		h.updateHealthStatus(check, false, latency)
		h.logger.Debug("Health check failed",
			"region_id", regionID,
			"error", err)
		return
	}
	defer resp.Body.Close()

	healthy := resp.StatusCode == http.StatusOK
	h.updateHealthStatus(check, healthy, latency)

	// Update region health in manager
	h.manager.UpdateRegionHealth(regionID, healthy, latency)

	if healthy {
		h.logger.Debug("Health check succeeded",
			"region_id", regionID,
			"latency", latency)
	} else {
		h.logger.Debug("Health check failed",
			"region_id", regionID,
			"status_code", resp.StatusCode)
	}
}

// updateHealthStatus updates the health status of a check
func (h *HealthMonitor) updateHealthStatus(check *HealthCheck, healthy bool, latency time.Duration) {
	check.mu.Lock()
	defer check.mu.Unlock()

	check.LastCheck = time.Now()
	check.LastLatency = latency

	if healthy {
		check.ConsecutiveOK++
		check.ConsecutiveFails = 0
	} else {
		check.ConsecutiveFails++
		check.ConsecutiveOK = 0
	}
}

// IsHealthy returns true if a region is healthy
func (h *HealthMonitor) IsHealthy(regionID string) bool {
	h.mu.RLock()
	check, exists := h.checks[regionID]
	h.mu.RUnlock()

	if !exists {
		return false
	}

	check.mu.RLock()
	healthy := check.ConsecutiveFails < h.config.FailureThreshold
	check.mu.RUnlock()

	return healthy
}

// GetHealthyRegions returns all healthy regions
func (h *HealthMonitor) GetHealthyRegions() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var healthy []string
	for regionID, check := range h.checks {
		check.mu.RLock()
		if check.ConsecutiveFails < h.config.FailureThreshold {
			healthy = append(healthy, regionID)
		}
		check.mu.RUnlock()
	}

	return healthy
}

// GetUnhealthyRegions returns all unhealthy regions
func (h *HealthMonitor) GetUnhealthyRegions() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var unhealthy []string
	for regionID, check := range h.checks {
		check.mu.RLock()
		if check.ConsecutiveFails >= h.config.FailureThreshold {
			unhealthy = append(unhealthy, regionID)
		}
		check.mu.RUnlock()
	}

	return unhealthy
}

// WaitForHealthy waits for a region to become healthy
func (h *HealthMonitor) WaitForHealthy(ctx context.Context, regionID string) error {
	ticker := time.NewTicker(h.config.Interval)
	defer ticker.Stop()

	for {
		if h.IsHealthy(regionID) {
			return nil
		}

		select {
		case <-ticker.C:
			continue
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// ForceCheck forces an immediate health check for a region
func (h *HealthMonitor) ForceCheck(ctx context.Context, regionID string) (*HealthStatus, error) {
	h.mu.RLock()
	check, exists := h.checks[regionID]
	h.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("region not found: %s", regionID)
	}

	h.checkHealth(ctx, regionID, check)
	return h.GetStatus(regionID)
}

// HealthSummary provides a summary of region health
type HealthSummary struct {
	TotalRegions     int       `json:"total_regions"`
	HealthyRegions   int       `json:"healthy_regions"`
	UnhealthyRegions int       `json:"unhealthy_regions"`
	UnknownRegions   int       `json:"unknown_regions"`
	LastUpdate       time.Time `json:"last_update"`
}

// GetSummary returns a health summary
func (h *HealthMonitor) GetSummary() *HealthSummary {
	h.mu.RLock()
	defer h.mu.RUnlock()

	summary := &HealthSummary{
		TotalRegions: len(h.checks),
		LastUpdate:   time.Now(),
	}

	for _, check := range h.checks {
		check.mu.RLock()
		healthy := check.ConsecutiveFails < h.config.FailureThreshold
		checked := !check.LastCheck.IsZero()
		check.mu.RUnlock()

		if !checked {
			summary.UnknownRegions++
		} else if healthy {
			summary.HealthyRegions++
		} else {
			summary.UnhealthyRegions++
		}
	}

	return summary
}
