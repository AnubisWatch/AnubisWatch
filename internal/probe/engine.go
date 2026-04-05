package probe

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// EngineConfig configures probe engine behavior
type EngineConfig struct {
	MaxConcurrentChecks int // Maximum concurrent checks (default: 100)
	CircuitBreaker      CircuitBreakerConfig
	NodeID              string
	Region              string
}

// CircuitBreakerConfig configures circuit breaker behavior
type CircuitBreakerConfig struct {
	Enabled          bool          // Enable circuit breaker
	FailureThreshold int           // Failures before opening (default: 5)
	SuccessThreshold int           // Successes before closing (default: 3)
	Timeout          time.Duration // Time before attempting again (default: 30s)
}

// DefaultEngineConfig returns default engine configuration
func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		MaxConcurrentChecks: 100,
		CircuitBreaker: CircuitBreakerConfig{
			Enabled:          true,
			FailureThreshold: 5,
			SuccessThreshold: 3,
			Timeout:          30 * time.Second,
		},
	}
}

// Engine is the probe scheduling and execution engine.
// It manages the lifecycle of all soul checks on this Jackal.
type Engine struct {
	registry *CheckerRegistry
	store    Storage
	alerter  AlertDispatcher
	nodeID   string
	region   string
	config   EngineConfig

	souls  map[string]*soulRunner
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	logger *slog.Logger

	// Callbacks for Raft integration
	onJudgment func(*core.Judgment)

	// Concurrency limiting
	semaphore chan struct{}

	// Circuit breaker state
	circuitBreakers map[string]*circuitBreaker
	cbMu            sync.RWMutex

	// Stats
	checksRunning atomic.Int64
	totalChecks   atomic.Int64
	failedChecks  atomic.Int64
}

// Storage is the interface the probe engine uses to persist judgments
type Storage interface {
	SaveJudgment(ctx context.Context, j *core.Judgment) error
	GetSoul(ctx context.Context, workspaceID, soulID string) (*core.Soul, error)
	ListSouls(ctx context.Context, workspaceID string) ([]*core.Soul, error)
}

// AlertDispatcher is the interface for firing alerts
type AlertDispatcher interface {
	ProcessJudgment(soul *core.Soul, prevStatus core.SoulStatus, judgment *core.Judgment)
}

// EngineOptions configures the probe engine
type EngineOptions struct {
	Registry   *CheckerRegistry
	Store      Storage
	Alerter    AlertDispatcher
	NodeID     string
	Region     string
	Logger     *slog.Logger
	OnJudgment func(*core.Judgment)
	Config     EngineConfig
}

// circuitBreaker tracks failure/success counts for a soul
type circuitBreaker struct {
	mu              sync.RWMutex
	state           string // "closed", "open", "half-open"
	failures        int
	successes       int
	lastFailureTime time.Time
	lastStateChange time.Time
}

// soulRunner manages the ticker for a single soul
type soulRunner struct {
	soul       *core.Soul
	ticker     *time.Ticker
	cancel     context.CancelFunc
	lastStatus core.SoulStatus
}

// NewEngine creates a new probe engine
func NewEngine(opts EngineOptions) *Engine {
	ctx, cancel := context.WithCancel(context.Background())

	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	// Apply defaults
	if opts.Config.MaxConcurrentChecks <= 0 {
		opts.Config.MaxConcurrentChecks = 100
	}
	if !opts.Config.CircuitBreaker.Enabled {
		opts.Config.CircuitBreaker.FailureThreshold = 0 // Disabled
	}

	return &Engine{
		registry:        opts.Registry,
		store:           opts.Store,
		alerter:         opts.Alerter,
		nodeID:          opts.NodeID,
		region:          opts.Region,
		config:          opts.Config,
		souls:           make(map[string]*soulRunner),
		ctx:             ctx,
		cancel:          cancel,
		logger:          opts.Logger.With("component", "probe-engine"),
		onJudgment:      opts.OnJudgment,
		semaphore:       make(chan struct{}, opts.Config.MaxConcurrentChecks),
		circuitBreakers: make(map[string]*circuitBreaker),
	}
}

// AssignSouls sets the souls this Jackal is responsible for checking.
// Called by the Raft leader when distributing checks.
// Souls with region restrictions are filtered to only run on matching probes.
func (e *Engine) AssignSouls(souls []*core.Soul) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Determine which souls are new, removed, or updated
	newMap := make(map[string]*core.Soul, len(souls))
	for _, s := range souls {
		// Skip souls that have region restrictions and don't match this probe
		if len(s.Regions) > 0 && !e.regionMatches(s.Regions) {
			e.logger.Debug("soul skipped - region mismatch",
				"soul", s.Name,
				"id", s.ID,
				"soul_regions", s.Regions,
				"probe_region", e.region)
			continue
		}
		newMap[s.ID] = s
	}

	// Stop removed souls
	for id, runner := range e.souls {
		if _, exists := newMap[id]; !exists {
			runner.cancel()
			runner.ticker.Stop()
			delete(e.souls, id)
			e.logger.Info("soul unassigned", "soul", id)
		}
	}

	// Start new or updated souls
	for _, soul := range souls {
		// Skip souls that have region restrictions and don't match this probe
		if len(soul.Regions) > 0 && !e.regionMatches(soul.Regions) {
			continue
		}

		if existing, exists := e.souls[soul.ID]; exists {
			// Update soul config without restart if only config changed
			existing.soul = soul
			continue
		}
		e.startSoul(soul)
	}
}

// regionMatches returns true if the probe's region is in the list of regions
func (e *Engine) regionMatches(regions []string) bool {
	if len(regions) == 0 {
		return true
	}
	for _, r := range regions {
		if r == e.region {
			return true
		}
	}
	return false
}

// startSoul starts a goroutine for checking a soul
func (e *Engine) startSoul(soul *core.Soul) {
	ctx, cancel := context.WithCancel(e.ctx)

	interval := soul.Weight.Duration
	if interval == 0 {
		interval = 60 * time.Second // default 60s
	}

	runner := &soulRunner{
		soul:   soul,
		ticker: time.NewTicker(interval),
		cancel: cancel,
	}
	e.souls[soul.ID] = runner

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer runner.ticker.Stop()

		// Immediate first check
		e.judgeSoul(ctx, runner)

		for {
			select {
			case <-ctx.Done():
				return
			case <-runner.ticker.C:
				e.judgeSoul(ctx, runner)
			}
		}
	}()

	e.logger.Info("soul assigned",
		"soul", soul.Name,
		"id", soul.ID,
		"type", soul.Type,
		"interval", interval,
	)
}

// judgeSoul executes a single check with concurrency limiting and circuit breaker
func (e *Engine) judgeSoul(ctx context.Context, runner *soulRunner) {
	soul := runner.soul

	// Check circuit breaker first
	if cb := e.getCircuitBreaker(soul.ID); cb != nil && cb.isOpen(e.Config().CircuitBreaker) {
		e.logger.Debug("circuit breaker open, skipping check",
			"soul", soul.Name, "id", soul.ID)
		return
	}

	// Acquire semaphore (concurrency limiting)
	select {
	case e.semaphore <- struct{}{}:
		// Acquired
	case <-ctx.Done():
		return
	}
	defer func() { <-e.semaphore }()

	// Increment stats
	e.checksRunning.Add(1)
	e.totalChecks.Add(1)
	defer e.checksRunning.Add(-1)

	checker, ok := e.registry.Get(soul.Type)
	if !ok {
		e.logger.Error("unknown checker type", "type", soul.Type, "soul", soul.Name)
		return
	}

	// Validate config
	if err := checker.Validate(soul); err != nil {
		e.logger.Error("invalid soul config", "soul", soul.Name, "err", err)
		e.recordFailure(soul.ID)
		return
	}

	// Create timeout context
	timeout := soul.Timeout.Duration
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Execute the check
	judgment, err := checker.Judge(checkCtx, soul)
	if err != nil {
		judgment = failJudgment(soul, err)
		e.recordFailure(soul.ID)
	} else if judgment.Status == core.SoulDead {
		e.recordFailure(soul.ID)
	} else {
		e.recordSuccess(soul.ID)
	}

	// Enrich judgment with node info
	judgment.JackalID = e.nodeID
	judgment.Region = e.region
	if judgment.ID == "" {
		judgment.ID = core.GenerateID()
	}

	// Persist
	if e.store != nil {
		if err := e.store.SaveJudgment(ctx, judgment); err != nil {
			e.logger.Error("failed to save judgment", "err", err, "soul", soul.Name)
		}
	}

	// Notify Raft (for distributed aggregation)
	if e.onJudgment != nil {
		e.onJudgment(judgment)
	}

	// Evaluate alert rules
	if e.alerter != nil {
		prevStatus := runner.lastStatus
		runner.lastStatus = judgment.Status
		e.alerter.ProcessJudgment(soul, prevStatus, judgment)
	}

	e.logger.Debug("judgment complete",
		"soul", soul.Name,
		"status", judgment.Status,
		"duration", judgment.Duration,
	)
}

// TriggerImmediate forces an immediate check of a specific soul
func (e *Engine) TriggerImmediate(ctx context.Context, soulID string) (*core.Judgment, error) {
	e.mu.RLock()
	runner, ok := e.souls[soulID]
	e.mu.RUnlock()

	if !ok {
		return nil, &core.NotFoundError{Entity: "soul", ID: soulID}
	}

	checker, ok := e.registry.Get(runner.soul.Type)
	if !ok {
		return nil, &core.ConfigError{Field: "type", Message: "unknown type " + string(runner.soul.Type)}
	}

	judgment, err := checker.Judge(ctx, runner.soul)
	if err != nil {
		return nil, err
	}

	judgment.JackalID = e.nodeID
	judgment.Region = e.region
	return judgment, nil
}

// ForceCheck triggers an immediate check (REST API compatible)
func (e *Engine) ForceCheck(soulID string) (*core.Judgment, error) {
	ctx := context.Background()
	return e.TriggerImmediate(ctx, soulID)
}

// GetStatus returns probe engine status (REST API compatible)
func (e *Engine) GetStatus() *core.ProbeStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return &core.ProbeStatus{
		Running:       true,
		ActiveChecks:  len(e.souls),
		ChecksRunning: int(e.checksRunning.Load()),
		FailedChecks:  e.failedChecks.Load(),
		TotalChecks:   e.totalChecks.Load(),
		NodeID:        e.nodeID,
		Region:        e.region,
	}
}

// GetSoulStatus returns the current status of a soul
func (e *Engine) GetSoulStatus(soulID string) (*core.SoulStatus, error) {
	e.mu.RLock()
	runner, ok := e.souls[soulID]
	e.mu.RUnlock()

	if !ok {
		return nil, &core.NotFoundError{Entity: "soul", ID: soulID}
	}

	// Return the last known status from the runner
	return &runner.lastStatus, nil
}

// ListActiveSouls returns all currently assigned souls
func (e *Engine) ListActiveSouls() []*core.Soul {
	e.mu.RLock()
	defer e.mu.RUnlock()

	souls := make([]*core.Soul, 0, len(e.souls))
	for _, runner := range e.souls {
		souls = append(souls, runner.soul)
	}
	return souls
}

// Stop gracefully shuts down the probe engine
func (e *Engine) Stop() {
	e.logger.Info("stopping probe engine")
	e.cancel()
	e.wg.Wait()
	e.logger.Info("probe engine stopped")
}

// Stats returns engine statistics
func (e *Engine) Stats() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return map[string]interface{}{
		"active_souls":    len(e.souls),
		"node_id":         e.nodeID,
		"region":          e.region,
		"checks_running":  e.checksRunning.Load(),
		"total_checks":    e.totalChecks.Load(),
		"failed_checks":   e.failedChecks.Load(),
		"semaphore_usage": len(e.semaphore),
		"semaphore_max":   cap(e.semaphore),
	}
}

// Config returns the engine configuration
func (e *Engine) Config() EngineConfig {
	return e.config
}

// getCircuitBreaker returns or creates a circuit breaker for a soul
func (e *Engine) getCircuitBreaker(soulID string) *circuitBreaker {
	e.cbMu.RLock()
	cb, exists := e.circuitBreakers[soulID]
	e.cbMu.RUnlock()

	if exists {
		return cb
	}

	// Create new circuit breaker
	e.cbMu.Lock()
	defer e.cbMu.Unlock()

	// Double-check after acquiring write lock
	if cb, exists = e.circuitBreakers[soulID]; exists {
		return cb
	}

	cb = &circuitBreaker{
		state:           "closed",
		failures:        0,
		successes:       0,
		lastStateChange: time.Now(),
	}
	e.circuitBreakers[soulID] = cb
	return cb
}

// recordSuccess records a successful check for circuit breaker
func (e *Engine) recordSuccess(soulID string) {
	cb := e.getCircuitBreaker(soulID)
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == "closed" {
		return // Already healthy
	}

	cb.successes++
	cb.failures = 0

	// Transition from half-open to closed
	cfg := e.Config().CircuitBreaker
	if cb.state == "half-open" && cb.successes >= cfg.SuccessThreshold {
		cb.state = "closed"
		cb.lastStateChange = time.Now()
	}
}

// recordFailure records a failed check for circuit breaker
func (e *Engine) recordFailure(soulID string) {
	e.failedChecks.Add(1)

	cb := e.getCircuitBreaker(soulID)
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.successes = 0
	cb.lastFailureTime = time.Now()

	// Transition from closed to open
	cfg := e.Config().CircuitBreaker
	if cb.state == "closed" && cb.failures >= cfg.FailureThreshold {
		cb.state = "open"
		cb.lastStateChange = time.Now()
	}
}

// isOpen returns true if the circuit breaker should block checks
func (cb *circuitBreaker) isOpen(cfg CircuitBreakerConfig) bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if cb.state == "closed" {
		return false
	}

	if cb.state == "open" {
		// Check if timeout has elapsed to transition to half-open
		if time.Since(cb.lastStateChange) >= cfg.Timeout {
			cb.mu.RUnlock()
			cb.mu.Lock()
			if cb.state == "open" {
				cb.state = "half-open"
				cb.successes = 0
				cb.lastStateChange = time.Now()
			}
			cb.mu.Unlock()
			cb.mu.RLock()
			return false
		}
		return true
	}

	// half-open: allow checks but monitor results
	return false
}
