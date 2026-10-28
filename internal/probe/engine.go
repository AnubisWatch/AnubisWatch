package probe

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
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

	// StartJitter bounds the deterministic offset applied to the first check
	// of souls started together by a bulk AssignSouls (process start, cluster
	// rebalance). Each soul waits hash(soulID) mod min(interval, StartJitter)
	// before its first check, so a restart does not fire every soul at once.
	// Zero disables the offset. Single-soul starts (API create via UpsertSoul,
	// an interval change, a takeover of one soul) are never delayed.
	StartJitter time.Duration
	NodeID      string
	Region      string

	// AllowInsecureSkipVerify is the K7 master switch. When false (the
	// default), every probe checker refuses to honour per-soul
	// InsecureSkipVerify=true and fails the check with a clear error.
	// Set via the --insecure-skip-verify CLI flag, the
	// ANUBIS_ALLOW_INSECURE_TLS env var, or
	// security.allow_insecure_skip_verify in config. Production must
	// keep this false; enabling it disables TLS verification for any
	// check that requests it, which is a MITM vector.
	AllowInsecureSkipVerify bool

	// FlapDetection is the global flap-detection policy (probe.flap_detection);
	// a soul's own flap_detection block overrides it.
	FlapDetection core.FlapDetectionConfig
}

// CircuitBreakerConfig configures circuit breaker behavior.
//
// The breaker tracks per-soul failure streaks and open/half-open/closed state,
// but it never suppresses a scheduled check: an uptime monitor that skipped
// probes while a target was down left gaps in history, under-weighted the
// outage in up/checks uptime, and noticed recovery up to Timeout late. Every
// tick therefore produces a real judgment; the state is kept for diagnostics.
type CircuitBreakerConfig struct {
	Enabled          bool          // Enable circuit breaker
	FailureThreshold int           // Failures before opening (default: 5)
	SuccessThreshold int           // Successes before closing (default: 3)
	Timeout          time.Duration // Time before attempting again (default: 30s)
}

// DefaultStartJitter is the production cap on the first-check offset of a
// bulk-assigned soul. Short intervals use their own interval as the window.
const DefaultStartJitter = 30 * time.Second

// DefaultEngineConfig returns default engine configuration
func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		MaxConcurrentChecks: 100,
		StartJitter:         DefaultStartJitter,
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

	// lastJudgmentAt is the wall-clock UnixNano at which the most recent
	// judgment (scheduled or manual) was completed and published; 0 = never.
	lastJudgmentAt atomic.Int64
	// startedAt is when the engine was built; readiness measures staleness
	// from here until the first judgment lands.
	startedAt time.Time

	// now and retryWait are the clock used by confirmation retries and flap
	// detection; tests replace them to stay deterministic.
	now       func() time.Time
	retryWait func(ctx context.Context, d time.Duration) error
}

// EngineLiveness is a cheap snapshot for readiness probes and metrics.
type EngineLiveness struct {
	// LastJudgmentAt is when the engine last completed a judgment (zero if
	// it has not completed one since start).
	LastJudgmentAt time.Time
	// ScheduledSouls is the number of souls with an active runner.
	ScheduledSouls int
	// ChecksRunning is the number of checks currently in flight.
	ChecksRunning int64
	// LongestInterval is the longest check interval among scheduled souls.
	LongestInterval time.Duration
	// StartedAt is when the engine was created.
	StartedAt time.Time
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

// soulRunner manages the ticker for a single soul.
//
// The soul config and lastStatus are accessed both by the runner's own
// ticker goroutine and by REST-triggered calls (AssignSouls, TriggerImmediate,
// GetSoulStatus). mu guards those two mutable fields; ticker/cancel are set
// once at construction and only touched by the owning engine under e.mu.
type soulRunner struct {
	mu         sync.Mutex
	soul       *core.Soul
	interval   time.Duration
	ticker     *time.Ticker
	cancel     context.CancelFunc
	lastStatus core.SoulStatus
	// startDelay is the offset before the first check; set once at start.
	startDelay time.Duration
	// kick requests an out-of-schedule evaluation (heartbeat pings); it is
	// buffered so a burst of pings coalesces into one evaluation.
	kick chan struct{}
	// Flap detection state, guarded by mu: the times of the most recent
	// status transitions inside the window, and whether the soul flaps.
	flapTransitions []time.Time
	flapping        bool
	// flapBaseline is the status before flapping started; flapSteady counts
	// consecutive judgments without a status change.
	flapBaseline core.SoulStatus
	flapSteady   int
}

func (r *soulRunner) getSoul() *core.Soul {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.soul
}

func (r *soulRunner) setSoul(s *core.Soul) {
	r.mu.Lock()
	r.soul = s
	r.mu.Unlock()
}

// minSoulInterval guards against intervals that would turn a runner into a hot
// loop. Records written before the API rejected sub-second intervals (the
// dashboard once sent seconds that were decoded as nanoseconds) fall back to
// the default instead of hammering the target and the store.
var minSoulInterval = core.MinSoulInterval

const defaultSoulInterval = 60 * time.Second

func soulInterval(s *core.Soul) time.Duration {
	if s.Type == core.CheckHeartbeat {
		// A heartbeat soul is evaluated against its deadline, not probed.
		return heartbeatEvalInterval(s)
	}
	if s.Weight.Duration >= minSoulInterval {
		return s.Weight.Duration
	}
	return defaultSoulInterval
}

// soulTimeout returns the per-check timeout, treating unset or implausibly
// small values (see minSoulInterval) as the default.
func soulTimeout(s *core.Soul) time.Duration {
	if s.Timeout.Duration >= core.MinSoulTimeout {
		return s.Timeout.Duration
	}
	return 10 * time.Second
}

func (r *soulRunner) getStatus() core.SoulStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastStatus
}

// swapStatus stores the new status and returns the previous one.
func (r *soulRunner) swapStatus(s core.SoulStatus) core.SoulStatus {
	r.mu.Lock()
	prev := r.lastStatus
	r.lastStatus = s
	r.mu.Unlock()
	return prev
}

// NewEngine creates a new probe engine
func NewEngine(opts EngineOptions) *Engine {
	ctx, cancel := context.WithCancel(context.Background())

	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	// Apply defaults
	if opts.Config.MaxConcurrentChecks <= 0 {
		opts.Config.MaxConcurrentChecks = core.DefaultMaxConcurrentChecks
	}
	if opts.Config.StartJitter < 0 {
		opts.Config.StartJitter = 0
	}
	if !opts.Config.CircuitBreaker.Enabled {
		opts.Config.CircuitBreaker.FailureThreshold = 0 // Disabled
	}

	e := &Engine{
		startedAt:       time.Now(),
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
		now:             time.Now,
		retryWait:       sleepContext,
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
		if s == nil {
			continue
		}
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
			e.forgetSoul(id)
			e.logger.Info("soul unassigned", "soul", id)
		}
	}

	// Spread first checks only when this call starts several runners at once
	// (process start, rebalance). A lone new runner — a soul created through
	// the API in cluster mode, an interval change — is checked immediately.
	starting := 0
	for id, soul := range newMap {
		if existing, exists := e.souls[id]; !exists || existing.interval != soulInterval(soul) {
			starting++
		}
	}
	jitter := starting > 1

	// Start new or updated souls
	for _, soul := range souls {
		if soul == nil {
			continue
		}
		// Skip souls that have region restrictions and don't match this probe
		if len(soul.Regions) > 0 && !e.regionMatches(soul.Regions) {
			continue
		}

		if existing, exists := e.souls[soul.ID]; exists {
			if existing.interval == soulInterval(soul) {
				// Configuration-only updates do not disturb the schedule.
				existing.setSoul(soul)
				continue
			}

			// A ticker's period is fixed. Replace the whole runner when Weight
			// changes so the new cadence takes effect immediately and cleanly.
			existing.cancel()
			existing.ticker.Stop()
			delete(e.souls, soul.ID)
		}
		var delay time.Duration
		if jitter {
			delay = soulStartOffset(soul.ID, soulInterval(soul), e.config.StartJitter)
		}
		e.startSoul(soul, delay)
	}
}

// soulStartOffset returns a deterministic first-check offset in
// [0, min(interval, maxJitter)) derived from an FNV-1a hash of the soul ID, so
// the same soul lands on the same slot across restarts and tests.
func soulStartOffset(soulID string, interval, maxJitter time.Duration) time.Duration {
	window := interval
	if maxJitter < window {
		window = maxJitter
	}
	if window <= 0 {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(soulID))
	return time.Duration(h.Sum64() % uint64(window))
}

// UpsertSoul adds or updates one assignment without treating it as a complete
// global snapshot. API mutations use this path so changing one workspace cannot
// unassign runners belonging to other workspaces.
func (e *Engine) UpsertSoul(soul *core.Soul) {
	if soul == nil {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if len(soul.Regions) > 0 && !e.regionMatches(soul.Regions) {
		if existing, ok := e.souls[soul.ID]; ok {
			existing.cancel()
			existing.ticker.Stop()
			delete(e.souls, soul.ID)
			delete(e.circuitBreakers, soul.ID)
			e.forgetSoul(soul.ID)
		}
		return
	}

	if existing, ok := e.souls[soul.ID]; ok {
		if existing.interval == soulInterval(soul) {
			existing.setSoul(soul)
			return
		}
		existing.cancel()
		existing.ticker.Stop()
		delete(e.souls, soul.ID)
	}
	e.startSoul(soul, 0)
}

// RemoveSoul removes exactly one assignment and leaves every other workspace's
// runners untouched.
func (e *Engine) RemoveSoul(soulID string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if runner, ok := e.souls[soulID]; ok {
		runner.cancel()
		runner.ticker.Stop()
		delete(e.souls, soulID)
	}
	delete(e.circuitBreakers, soulID)
	e.forgetSoul(soulID)
}

// forgetSoul drops per-soul checker state (heartbeat progress) for a soul
// this engine no longer runs.
func (e *Engine) forgetSoul(soulID string) {
	if e.registry == nil {
		return
	}
	if c, ok := e.registry.Get(core.CheckHeartbeat); ok {
		if f, ok := c.(soulForgetter); ok {
			f.Forget(soulID)
		}
	}
}

// EvaluateSoul asks the runner of an assigned soul to evaluate it now, out of
// schedule. It never blocks and reports whether the soul is assigned here.
// Heartbeat pings use it so a ping is judged as soon as it is stored.
func (e *Engine) EvaluateSoul(soulID string) bool {
	e.mu.RLock()
	runner, ok := e.souls[soulID]
	e.mu.RUnlock()
	if !ok || runner.kick == nil {
		return false
	}
	select {
	case runner.kick <- struct{}{}:
	default:
	}
	return true
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

// startSoul starts a goroutine for checking a soul. The first check runs
// after startDelay (0 = immediately); later checks follow every interval
// from that first check.
func (e *Engine) startSoul(soul *core.Soul, startDelay time.Duration) {
	ctx, cancel := context.WithCancel(e.ctx)

	interval := soulInterval(soul)

	runner := &soulRunner{
		soul:       soul,
		interval:   interval,
		ticker:     time.NewTicker(interval),
		cancel:     cancel,
		startDelay: startDelay,
		kick:       make(chan struct{}, 1),
	}
	e.souls[soul.ID] = runner

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer runner.ticker.Stop()

		// Carry flap state over from the last stored judgment (a previous
		// runner, owner or process) before the first check.
		e.restoreRunnerFlap(ctx, runner)

		if startDelay > 0 {
			timer := time.NewTimer(startDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			// Re-phase the ticker on the offset slot.
			runner.ticker.Reset(interval)
		}

		// First check
		e.judgeSoul(ctx, runner)

		for {
			select {
			case <-ctx.Done():
				return
			case <-runner.ticker.C:
				e.judgeSoul(ctx, runner)
			case <-runner.kick:
				e.judgeSoul(ctx, runner)
			}
		}
	}()

	e.logger.Info("soul assigned",
		"soul", soul.Name,
		"id", soul.ID,
		"type", soul.Type,
		"interval", interval,
		"start_delay", startDelay,
	)
}

// judgeSoul executes a single check with concurrency limiting and circuit breaker
func (e *Engine) judgeSoul(ctx context.Context, runner *soulRunner) {
	if ctx.Err() != nil {
		return
	}
	soul := runner.getSoul()
	assignedSoul := soul

	// The circuit breaker deliberately does not gate scheduled checks (see
	// CircuitBreakerConfig): every tick must yield a real judgment so outages
	// leave no gaps and recovery is seen on the next interval.

	// Acquire semaphore (concurrency limiting). The slot is released while a
	// confirmation retry waits, so a slow-failing soul does not hold capacity
	// it is not using; release is idempotent for the deferred call.
	select {
	case e.semaphore <- struct{}{}:
		// Acquired
	case <-ctx.Done():
		return
	}
	held := true
	release := func() {
		if held {
			held = false
			e.checksRunning.Add(-1)
			<-e.semaphore
		}
	}
	defer release()

	// Increment stats
	e.checksRunning.Add(1)
	e.totalChecks.Add(1)

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

	// A soul that fails validation (e.g. a target blocked by SSRF protection)
	// is recorded as a dead judgment rather than skipped, so the operator sees
	// why it never turns alive and alert rules can fire.
	var judgment *core.Judgment
	var err error
	attempts := 0
	if err = checker.Validate(soul); err != nil {
		e.logger.Warn("soul failed validation", "soul", soul.Name, "err", err)
	} else {
		// Confirmation retries: a failed attempt is re-run up to
		// soul.Retries times, retry_interval apart, while the retry still
		// starts before the next scheduled check. Only the final outcome
		// is recorded below.
		retries := soul.EffectiveRetries()
		deadline := e.now().Add(runner.interval)
		for {
			attempts++
			judgment, err = runAttempt(ctx, checker, soul)
			if attempts > retries || !attemptFailed(judgment, err) || ctx.Err() != nil {
				break
			}
			wait := soul.EffectiveRetryInterval()
			if !e.now().Add(wait).Before(deadline) {
				break
			}
			release()
			if e.retryWait(ctx, wait) != nil {
				return
			}
			select {
			case e.semaphore <- struct{}{}:
				held = true
				e.checksRunning.Add(1)
			case <-ctx.Done():
				return
			}
		}
	}
	// The assignment may have been canceled or updated while the check ran.
	if ctx.Err() != nil || runner.getSoul() != assignedSoul {
		return
	}
	if errors.Is(err, ErrNoJudgment) {
		// Nothing due (a heartbeat soul inside its deadline): the
		// evaluation itself is engine progress, but nothing is recorded.
		e.markJudgment()
		return
	}
	if err != nil {
		judgment = failJudgment(soul, err)
		e.recordFailure(soul.ID)
	} else if judgment.Status == core.SoulDead {
		e.recordFailure(soul.ID)
	} else {
		e.recordSuccess(soul.ID)
	}

	// Enrich the result before any persistence or publication. WorkspaceID is
	// a tenant-boundary invariant: background checks run on the engine context,
	// which intentionally has no request workspace and would otherwise fall back
	// to "default" in storage and WebSocket routing.
	judgment.JackalID = e.nodeID
	judgment.Region = e.region
	judgment.WorkspaceID = soul.WorkspaceID
	if judgment.SoulID == "" {
		judgment.SoulID = soul.ID
	}
	if judgment.Timestamp.IsZero() {
		judgment.Timestamp = time.Now().UTC()
	}
	if judgment.ID == "" {
		judgment.ID = core.GenerateID()
	}
	if soul.EffectiveRetries() > 0 && attempts > 0 {
		judgmentDetails(judgment).Attempts = attempts
	}
	e.applyFlapState(runner, soul, judgment)

	// Persist with retry
	if e.store != nil {
		if err := retryWithBackoff(ctx, 3, 100*time.Millisecond, func() error {
			return e.store.SaveJudgment(ctx, judgment)
		}); err != nil {
			e.logger.Error("failed to save judgment after retries", "err", err, "soul", soul.Name)
			// Continue processing - don't fail the check due to storage issues
		}
	}

	if ctx.Err() != nil || runner.getSoul() != assignedSoul {
		return
	}

	// Notify Raft (for distributed aggregation). Atomic load — no
	// mutex on the per-check hot path.
	if onJudgment := e.onJudgment.Load(); onJudgment != nil {
		(*onJudgment)(judgment)
	}

	// Evaluate alert rules
	prevStatus := runner.swapStatus(judgment.Status)
	if e.alerter != nil {
		e.alerter.ProcessJudgment(soul, prevStatus, judgment)
	}
	e.markJudgment()

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

	soul := runner.getSoul()

	checker, ok := e.registry.Get(soul.Type)
	if !ok {
		return nil, &core.ConfigError{Field: "type", Message: "unknown type " + string(soul.Type)}
	}

	// Validation failures (e.g. a target blocked by SSRF policy) are reported
	// as a dead judgment, matching scheduled checks, instead of an opaque error.
	validateErr := checker.Validate(soul)

	checkCtx, cancel := context.WithTimeout(ctx, soulTimeout(soul))
	defer cancel()

	// The concurrency limit applies to every check, including manual ones: the REST
	// force-check endpoint, the MCP force_check tool and the gRPC ForceCheck RPC all
	// arrive here. Without this, manual checks were unbounded by MaxConcurrentChecks and
	// were not counted in the reported ChecksRunning. Acquire with the check's own
	// timeout so a saturated engine returns an error instead of blocking the caller
	// forever.
	select {
	case e.semaphore <- struct{}{}:
	case <-checkCtx.Done():
		return nil, fmt.Errorf("concurrency limit reached (%d concurrent checks): %w", cap(e.semaphore), checkCtx.Err())
	}
	defer func() { <-e.semaphore }()

	e.checksRunning.Add(1)
	defer e.checksRunning.Add(-1)

	// K7: apply the same master security gate here so manually-triggered
	// checks honour the global InsecureSkipVerify policy.
	checkSoul := e.applySecurityGate(soul)

	var judgment *core.Judgment
	err := validateErr
	if err == nil {
		judgment, err = checker.Judge(checkCtx, checkSoul)
	}
	if errors.Is(err, ErrNoJudgment) {
		return nil, err
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if checkCtx.Err() != nil {
			return nil, checkCtx.Err()
		}
		judgment = failJudgment(soul, err)
		e.recordFailure(soul.ID)
	} else if judgment.Status == core.SoulDead {
		e.recordFailure(soul.ID)
	} else {
		e.recordSuccess(soul.ID)
	}

	judgment.JackalID = e.nodeID
	judgment.Region = e.region
	judgment.WorkspaceID = soul.WorkspaceID
	if judgment.ID == "" {
		judgment.ID = core.GenerateID()
	}
	if judgment.SoulID == "" {
		judgment.SoulID = soul.ID
	}
	if judgment.Timestamp.IsZero() {
		judgment.Timestamp = time.Now().UTC()
	}
	// A manual check is a single attempt (no confirmation retries) but
	// still counts toward flap detection.
	e.applyFlapState(runner, soul, judgment)

	if e.store != nil {
		if err := retryWithBackoff(ctx, 3, 100*time.Millisecond, func() error {
			return e.store.SaveJudgment(ctx, judgment)
		}); err != nil {
			e.logger.Error("failed to save immediate judgment after retries", "err", err, "soul", soul.Name)
			return nil, err
		}
	}

	if onJudgment := e.onJudgment.Load(); onJudgment != nil {
		(*onJudgment)(judgment)
	}

	prevStatus := runner.swapStatus(judgment.Status)
	if e.alerter != nil {
		e.alerter.ProcessJudgment(soul, prevStatus, judgment)
	}
	e.markJudgment()

	return judgment, nil
}

// markJudgment records that a judgment was completed now. Monotonic: a
// slower concurrent check finishing later never moves the time backwards.
func (e *Engine) markJudgment() {
	now := time.Now().UnixNano()
	for {
		prev := e.lastJudgmentAt.Load()
		if now <= prev || e.lastJudgmentAt.CompareAndSwap(prev, now) {
			return
		}
	}
}

// Liveness reports when the engine last completed a judgment and how many
// souls it is scheduling. A readiness check can treat "souls scheduled but no
// judgment for several intervals" as a stalled engine.
func (e *Engine) Liveness() EngineLiveness {
	e.mu.RLock()
	scheduled := len(e.souls)
	var longest time.Duration
	for _, r := range e.souls {
		r.mu.Lock()
		if r.interval > longest {
			longest = r.interval
		}
		r.mu.Unlock()
	}
	e.mu.RUnlock()

	var last time.Time
	if ns := e.lastJudgmentAt.Load(); ns != 0 {
		last = time.Unix(0, ns).UTC()
	}
	return EngineLiveness{
		LastJudgmentAt:  last,
		ScheduledSouls:  scheduled,
		ChecksRunning:   e.checksRunning.Load(),
		LongestInterval: longest,
		StartedAt:       e.startedAt,
	}
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

	// Return a copy of the last known status; the field is mutated
	// concurrently by the runner's check goroutine.
	status := runner.getStatus()
	return &status, nil
}

// ListActiveSouls returns all currently assigned souls
func (e *Engine) ListActiveSouls() []*core.Soul {
	e.mu.RLock()
	defer e.mu.RUnlock()

	souls := make([]*core.Soul, 0, len(e.souls))
	for _, runner := range e.souls {
		souls = append(souls, runner.getSoul())
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
	return ApplySecurityPolicy(soul, e.config.AllowInsecureSkipVerify, e.logger)
}

// ApplySecurityPolicy applies the process TLS policy without mutating the caller's
// Soul. Both scheduled probes and journey steps must use this execution boundary.
func ApplySecurityPolicy(soul *core.Soul, allowInsecure bool, logger *slog.Logger) *core.Soul {
	if soul == nil || allowInsecure {
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

	if logger != nil {
		logger.Warn("soul requested InsecureSkipVerify but process has it disabled; running with TLS verification",
			"event", "k7_insecure_skip_overridden",
			"soul", c.Name,
			"soul_id", c.ID,
			"override_flag", "--insecure-skip-verify",
			"env_var", "ANUBIS_ALLOW_INSECURE_TLS",
		)
	}

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
		cb.failures = 0
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

	// A failed recovery probe reopens immediately; closed circuits use the threshold.
	cfg := e.Config().CircuitBreaker
	if cb.state == "half-open" || (cb.state == "closed" && cb.failures >= cfg.FailureThreshold) {
		cb.state = "open"
		cb.lastStateChange = time.Now()
	}
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
		if i == maxRetries-1 {
			break
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
