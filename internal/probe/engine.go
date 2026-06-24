package probe

import (
	"context"
	"fmt"
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

	// AllowInsecureSkipVerify is the K7 master switch. When false (the
	// default), every probe checker refuses to honour per-soul
	// InsecureSkipVerify=true and fails the check with a clear error.
	// Set via the --insecure-skip-verify CLI flag, the
	// ANUBIS_ALLOW_INSECURE_TLS env var, or
	// security.allow_insecure_skip_verify in config. Production must
	// keep this false; enabling it disables TLS verification for any
	// check that requests it, which is a MITM vector.
	AllowInsecureSkipVerify bool
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

	// Callbacks for Raft integration. Stored as an atomic.Pointer
	// so the per-check read in judgeSoul/TriggerImmediate doesn't
	// need a mutex round-trip — the callback is set once at startup
	// and read on every check, which is the natural fit for atomics.
	onJudgment atomic.Pointer[func(*core.Judgment)]

	// Concurrency limiting
	semaphore chan struct{}

	// Circuit breaker state — guarded by e.mu (not a separate lock)
	circuitBreakers map[string]*circuitBreaker

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

	e := &Engine{
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
		semaphore:       make(chan struct{}, opts.Config.MaxConcurrentChecks),
		circuitBreakers: make(map[string]*circuitBreaker),
	}

	// Seed the judgment callback atomically so the very first check
	// (which can fire before SetOnJudgment is called) sees the value
	// the engine was constructed with.
	if opts.OnJudgment != nil {
		fn := opts.OnJudgment
		e.onJudgment.Store(&fn)
	}
	return e
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
			// Clean up circuit breaker for unassigned soul. e.mu
			// (held above) also guards e.circuitBreakers — keep the
			// two maps' lifecycles in sync.
			delete(e.circuitBreakers, id)
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

	// K7: apply the master security gate before dispatching the check.
	// If the soul requests InsecureSkipVerify but the process is not
	// started with --insecure-skip-verify (or ANUBIS_ALLOW_INSECURE_TLS),
	// we override the per-soul flag to false so the underlying checker
	// does the secure thing. We also log a warning so the operator
	// notices the misconfiguration. The original (insecure) intent is
	// preserved in the audit event for forensic review.
	soul = e.applySecurityGate(soul)

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

	// Persist with retry
	if e.store != nil {
		if err := retryWithBackoff(ctx, 3, 100*time.Millisecond, func() error {
			return e.store.SaveJudgment(ctx, judgment)
		}); err != nil {
			e.logger.Error("failed to save judgment after retries", "err", err, "soul", soul.Name)
			// Continue processing - don't fail the check due to storage issues
		}
	}

	// Notify Raft (for distributed aggregation). Atomic load — no
	// mutex on the per-check hot path.
	if onJudgment := e.onJudgment.Load(); onJudgment != nil {
		(*onJudgment)(judgment)
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
		if e.store == nil {
			return nil, &core.NotFoundError{Entity: "soul", ID: soulID}
		}

		soul, err := e.store.GetSoul(ctx, "", soulID)
		if err != nil {
			return nil, err
		}
		if soul == nil {
			return nil, &core.NotFoundError{Entity: "soul", ID: soulID}
		}

		runner = &soulRunner{
			soul:       soul,
			lastStatus: core.SoulUnknown,
		}
	}

	checker, ok := e.registry.Get(runner.soul.Type)
	if !ok {
		return nil, &core.ConfigError{Field: "type", Message: "unknown type " + string(runner.soul.Type)}
	}

	if err := checker.Validate(runner.soul); err != nil {
		return nil, err
	}

	timeout := runner.soul.Timeout.Duration
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// K7: apply the same master security gate here so manually-triggered
	// checks honour the global InsecureSkipVerify policy.
	checkSoul := e.applySecurityGate(runner.soul)

	judgment, err := checker.Judge(checkCtx, checkSoul)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if checkCtx.Err() != nil {
			return nil, checkCtx.Err()
		}
		judgment = failJudgment(runner.soul, err)
		e.recordFailure(runner.soul.ID)
	} else if judgment.Status == core.SoulDead {
		e.recordFailure(runner.soul.ID)
	} else {
		e.recordSuccess(runner.soul.ID)
	}

	judgment.JackalID = e.nodeID
	judgment.Region = e.region
	judgment.WorkspaceID = runner.soul.WorkspaceID
	if judgment.ID == "" {
		judgment.ID = core.GenerateID()
	}
	if judgment.SoulID == "" {
		judgment.SoulID = runner.soul.ID
	}
	if judgment.Timestamp.IsZero() {
		judgment.Timestamp = time.Now().UTC()
	}

	if e.store != nil {
		if err := retryWithBackoff(ctx, 3, 100*time.Millisecond, func() error {
			return e.store.SaveJudgment(ctx, judgment)
		}); err != nil {
			e.logger.Error("failed to save immediate judgment after retries", "err", err, "soul", runner.soul.Name)
			return nil, err
		}
	}

	if onJudgment := e.onJudgment.Load(); onJudgment != nil {
		(*onJudgment)(judgment)
	}

	if e.alerter != nil {
		prevStatus := runner.lastStatus
		runner.lastStatus = judgment.Status
		e.alerter.ProcessJudgment(runner.soul, prevStatus, judgment)
	}

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

// SetOnJudgment sets the callback function for judgment events.
// Safe to call concurrently with a check in flight — readers use
// atomic.Load. Passing nil clears the callback.
func (e *Engine) SetOnJudgment(fn func(*core.Judgment)) {
	if fn == nil {
		e.onJudgment.Store(nil)
		return
	}
	e.onJudgment.Store(&fn)
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

// applySecurityGate enforces the K7 master switch. The input soul is
// shallow-copied before any per-soul InsecureSkipVerify field is
// mutated, so callers retain their original struct untouched. The
// function returns the same Soul value by default; only when a per-soul
// InsecureSkipVerify flag is true while the global allow is false does
// it clone and zero the field, logging a warning so the operator
// notices a misconfigured soul running under a strict process.
//
// The function does NOT block the check entirely — K7's design is
// "fail secure", which means the check still runs, just with TLS
// verification enabled. If you want to fail-loud instead, set
// Soul.HTTP.InsecureSkipVerify to false and add an explicit deny
// rule in the dispatch layer.
func (e *Engine) applySecurityGate(soul *core.Soul) *core.Soul {
	if e.config.AllowInsecureSkipVerify {
		return soul
	}

	// Detect on the original struct whether any per-soul flag is set.
	// We must read the *original* (caller-owned) pointer here; once
	// we copy below, the "did the caller want insecure?" signal is
	// gone. This is the only place we look at the unfiltered values.
	wasInsecure := (soul.HTTP != nil && soul.HTTP.InsecureSkipVerify) ||
		(soul.SMTP != nil && soul.SMTP.InsecureSkipVerify) ||
		(soul.IMAP != nil && soul.IMAP.InsecureSkipVerify) ||
		(soul.GRPC != nil && soul.GRPC.InsecureSkipVerify) ||
		(soul.WebSocket != nil && soul.WebSocket.InsecureSkipVerify)
	if !wasInsecure {
		return soul
	}

	// Build a private copy. Each per-soul config that had
	// InsecureSkipVerify=true gets a fresh struct allocated so we
	// never mutate the caller's shared state. The original fields
	// are copied across; only the boolean is forced to false.
	c := *soul
	if c.HTTP != nil && c.HTTP.InsecureSkipVerify {
		original := *c.HTTP
		c.HTTP = &original
		c.HTTP.InsecureSkipVerify = false
	}
	if c.SMTP != nil && c.SMTP.InsecureSkipVerify {
		original := *c.SMTP
		c.SMTP = &original
		c.SMTP.InsecureSkipVerify = false
	}
	if c.IMAP != nil && c.IMAP.InsecureSkipVerify {
		original := *c.IMAP
		c.IMAP = &original
		c.IMAP.InsecureSkipVerify = false
	}
	if c.GRPC != nil && c.GRPC.InsecureSkipVerify {
		original := *c.GRPC
		c.GRPC = &original
		c.GRPC.InsecureSkipVerify = false
	}
	if c.WebSocket != nil && c.WebSocket.InsecureSkipVerify {
		original := *c.WebSocket
		c.WebSocket = &original
		c.WebSocket.InsecureSkipVerify = false
	}

	e.logger.Warn("K7: soul requested InsecureSkipVerify but process has it disabled; running with TLS verification",
		"event", "k7_insecure_skip_overridden",
		"soul", c.Name,
		"soul_id", c.ID,
		"override_flag", "--insecure-skip-verify",
		"env_var", "ANUBIS_ALLOW_INSECURE_TLS",
	)

	return &c
}

// getCircuitBreaker returns or creates a circuit breaker for a soul.
// e.mu guards the map (not a separate cbMu) so a concurrent
// AssignSouls/Stop can safely delete an entry while a check is in flight.
func (e *Engine) getCircuitBreaker(soulID string) *circuitBreaker {
	e.mu.RLock()
	cb, exists := e.circuitBreakers[soulID]
	e.mu.RUnlock()

	if exists {
		return cb
	}

	// Create new circuit breaker
	e.mu.Lock()
	defer e.mu.Unlock()

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
	state := cb.state
	lastChange := cb.lastStateChange
	cb.mu.RUnlock()

	if state == "closed" {
		return false
	}

	if state == "open" {
		// Check if timeout has elapsed to transition to half-open
		if time.Since(lastChange) >= cfg.Timeout {
			// Try to transition to half-open
			cb.mu.Lock()
			// Double-check state after acquiring lock
			if cb.state == "open" {
				cb.state = "half-open"
				cb.successes = 0
				cb.lastStateChange = time.Now()
			}
			cb.mu.Unlock()
			return false
		}
		return true
	}

	// half-open: allow checks but monitor results
	return false
}

// retryWithBackoff retries an operation with exponential backoff
func retryWithBackoff(ctx context.Context, maxRetries int, initialDelay time.Duration, op func() error) error {
	var err error
	delay := initialDelay

	for i := 0; i < maxRetries; i++ {
		err = op()
		if err == nil {
			return nil
		}

		// Don't retry on context cancellation
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Wait before retrying, but respect context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			delay *= 2 // Exponential backoff
		}
	}

	return fmt.Errorf("failed after %d retries: %w", maxRetries, err)
}
