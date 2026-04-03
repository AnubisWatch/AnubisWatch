package probe

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Engine is the probe scheduling and execution engine.
// It manages the lifecycle of all soul checks on this Jackal.
type Engine struct {
	registry  *CheckerRegistry
	store     Storage
	alerter   AlertDispatcher
	nodeID    string
	region    string

	souls  map[string]*soulRunner
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	logger *slog.Logger

	// Callbacks for Raft integration
	onJudgment func(*core.Judgment)
}

// Storage is the interface the probe engine uses to persist judgments
type Storage interface {
	SaveJudgment(ctx context.Context, j *core.Judgment) error
	GetSoul(ctx context.Context, workspaceID, soulID string) (*core.Soul, error)
	ListSouls(ctx context.Context, workspaceID string) ([]*core.Soul, error)
}

// AlertDispatcher is the interface for firing alerts
type AlertDispatcher interface {
	Evaluate(ctx context.Context, soul *core.Soul, judgment *core.Judgment) error
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
}

// soulRunner manages the ticker for a single soul
type soulRunner struct {
	soul   *core.Soul
	ticker *time.Ticker
	cancel context.CancelFunc
}

// NewEngine creates a new probe engine
func NewEngine(opts EngineOptions) *Engine {
	ctx, cancel := context.WithCancel(context.Background())

	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	return &Engine{
		registry:   opts.Registry,
		store:      opts.Store,
		alerter:    opts.Alerter,
		nodeID:     opts.NodeID,
		region:     opts.Region,
		souls:      make(map[string]*soulRunner),
		ctx:        ctx,
		cancel:     cancel,
		logger:     opts.Logger.With("component", "probe-engine"),
		onJudgment: opts.OnJudgment,
	}
}

// AssignSouls sets the souls this Jackal is responsible for checking.
// Called by the Raft leader when distributing checks.
func (e *Engine) AssignSouls(souls []*core.Soul) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Determine which souls are new, removed, or updated
	newMap := make(map[string]*core.Soul, len(souls))
	for _, s := range souls {
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
		if existing, exists := e.souls[soul.ID]; exists {
			// Update soul config without restart if only config changed
			existing.soul = soul
			continue
		}
		e.startSoul(soul)
	}
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
		e.judgeSoul(ctx, soul)

		for {
			select {
			case <-ctx.Done():
				return
			case <-runner.ticker.C:
				e.judgeSoul(ctx, runner.soul)
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

// judgeSoul executes a single check
func (e *Engine) judgeSoul(ctx context.Context, soul *core.Soul) {
	checker, ok := e.registry.Get(soul.Type)
	if !ok {
		e.logger.Error("unknown checker type", "type", soul.Type, "soul", soul.Name)
		return
	}

	// Validate config
	if err := checker.Validate(soul); err != nil {
		e.logger.Error("invalid soul config", "soul", soul.Name, "err", err)
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
		if err := e.alerter.Evaluate(ctx, soul, judgment); err != nil {
			e.logger.Error("alert evaluation failed", "err", err, "soul", soul.Name)
		}
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

// GetSoulStatus returns the current status of a soul
func (e *Engine) GetSoulStatus(soulID string) (*core.SoulStatus, error) {
	e.mu.RLock()
	_, ok := e.souls[soulID]
	e.mu.RUnlock()

	if !ok {
		return nil, &core.NotFoundError{Entity: "soul", ID: soulID}
	}

	// TODO: Return actual status from latest judgment
	return nil, nil
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
		"active_souls": len(e.souls),
		"node_id":      e.nodeID,
		"region":       e.region,
	}
}
