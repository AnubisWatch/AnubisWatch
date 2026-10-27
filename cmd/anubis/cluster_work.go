package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/alert"
	"github.com/AnubisWatch/anubiswatch/internal/cluster"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/journey"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
	"github.com/AnubisWatch/anubiswatch/internal/raft"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

// judgmentTable tags replicated judgment commands. The FSM hands them to
// clusterWork.applyJudgment rather than writing their key, so every node also
// folds the judgment into its time-series summaries exactly as a local save
// would.
const judgmentTable = "judgments"

const (
	// reconcileDebounce coalesces a burst of soul changes into one pass.
	reconcileDebounce = 50 * time.Millisecond
	// reconcileInterval is the safety net: ownership is recomputed this often
	// even when no change was observed.
	reconcileInterval = 30 * time.Second
)

// clusterWork shares monitoring work across a Raft cluster. Each soul is owned
// by one live member (cluster.Manager.PlanSouls); every node runs only the
// souls it owns and recomputes that set whenever souls or membership change.
// Judgments and incidents are replicated through Raft in batches, so every
// node can show every soul's history, while alert rules are evaluated only on
// the owner -- the only node that produces the soul's judgments -- so each
// failing judgment is notified once. Channel, rule and incident changes made
// on another node are applied to this node's alert manager as they replicate.
type clusterWork struct {
	mgr     *cluster.Manager
	cfg     *core.Config
	store   *storage.CobaltDB
	ts      *storage.TimeSeriesStore
	batcher *cluster.Batcher
	logger  *slog.Logger

	// Set by BuildServerDependencies once they exist.
	engine   *probe.Engine
	alerts   *alert.Manager
	journeys *journey.Executor
	// onJudgment pushes judgments produced on other nodes to this node's live
	// clients; the owner already pushed its own.
	onJudgment func(*core.Judgment)

	trigger chan struct{}

	eventsMu sync.Mutex
	events   []clusterEvent
	eventsCh chan struct{}

	ownedMu sync.RWMutex
	owned   map[string]bool

	// journeyCtx parents the journey loops this node runs; cancelled on halt.
	journeyCtx    context.Context
	cancelJourney context.CancelFunc

	stopCh    chan struct{}
	wg        sync.WaitGroup
	startOnce sync.Once
	haltOnce  sync.Once
	stopOnce  sync.Once
}

// clusterEvent is one applied command, or a snapshot restore, waiting to be
// reflected in memory.
type clusterEvent struct {
	cmd     core.FSMCommand
	restore bool
}

func newClusterWork(mgr *cluster.Manager, cfg *core.Config, store *storage.CobaltDB, ts *storage.TimeSeriesStore, logger *slog.Logger) *clusterWork {
	w := &clusterWork{
		mgr:      mgr,
		cfg:      cfg,
		store:    store,
		ts:       ts,
		logger:   logger.With("component", "cluster_work"),
		trigger:  make(chan struct{}, 1),
		eventsCh: make(chan struct{}, 1),
		owned:    make(map[string]bool),
		stopCh:   make(chan struct{}),
	}
	w.journeyCtx, w.cancelJourney = context.WithCancel(context.Background())
	// A configured soul without an ID gets a stable one derived from its name,
	// so every node plans it under the same key. Done once, before any pass
	// reads cfg.Souls.
	for i := range cfg.Souls {
		if cfg.Souls[i].ID == "" {
			cfg.Souls[i].ID = configEntityID("config-soul-", cfg.Souls[i].Name)
		}
	}
	for i := range cfg.Journeys {
		if cfg.Journeys[i].ID == "" {
			cfg.Journeys[i].ID = configEntityID("config-journey-", cfg.Journeys[i].Name)
		}
	}
	w.batcher = cluster.NewBatcher(mgr.ApplyMutation, 0, logger)
	mgr.SetCommandHandler(judgmentTable, w.applyJudgment)
	mgr.SetApplyObserver(w.observe)
	mgr.SetRestoreObserver(func() { w.enqueue(clusterEvent{restore: true}) })
	mgr.SetMembershipObserver(w.Trigger)
	return w
}

// start begins reconciling ownership and replicating batches. Call it after the
// cluster manager started.
func (w *clusterWork) start() {
	w.startOnce.Do(func() {
		w.batcher.Start()
		w.wg.Add(2)
		go w.reconcileLoop()
		go w.eventLoop()
		w.Trigger()
	})
}

// halt stops the ownership and event loops and the journeys this node runs,
// so no later pass reschedules work during shutdown. It is idempotent.
func (w *clusterWork) halt() {
	w.haltOnce.Do(func() {
		close(w.stopCh)
		w.wg.Wait()
		w.cancelJourney()
		if w.journeys != nil {
			w.journeys.Assign(w.journeyCtx, nil)
		}
	})
}

// stop halts the loops, then flushes queued replication (bounded by ctx).
// Call it while the cluster manager is still running.
func (w *clusterWork) stop(ctx context.Context) {
	w.stopOnce.Do(func() {
		w.halt()
		w.batcher.Stop(ctx)
	})
}

// Trigger schedules an ownership pass. It never blocks.
func (w *clusterWork) Trigger() {
	select {
	case w.trigger <- struct{}{}:
	default:
	}
}

func (w *clusterWork) reconcileLoop() {
	defer w.wg.Done()
	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
		case <-w.trigger:
			select {
			case <-time.After(reconcileDebounce):
			case <-w.stopCh:
				return
			}
			// Changes that arrived during the debounce are covered by this pass.
			select {
			case <-w.trigger:
			default:
			}
		}
		w.reconcile()
	}
}

// reconcile hands the probe engine exactly the enabled souls this node owns.
// AssignSouls keeps the schedule of a soul whose interval did not change, so
// repeated passes never double-schedule or restart a check.
func (w *clusterWork) reconcile() {
	if w.engine == nil || !w.mgr.Running() {
		return
	}
	souls := collectSouls(context.Background(), w.cfg, w.store, w.logger)
	plan := w.mgr.PlanSouls(souls)
	self := w.mgr.NodeID()

	owned := make(map[string]bool)
	mine := make([]*core.Soul, 0, len(souls))
	for _, soul := range souls {
		if plan[soul.ID] != self {
			continue
		}
		owned[soul.ID] = true
		if soul.Enabled {
			mine = append(mine, soul)
		}
	}
	w.ownedMu.Lock()
	changed := len(owned) != len(w.owned)
	var acquired []*core.Soul
	for _, soul := range mine {
		if !w.owned[soul.ID] {
			acquired = append(acquired, soul)
		}
	}
	for id := range owned {
		if !w.owned[id] {
			changed = true
			break
		}
	}
	w.owned = owned
	w.ownedMu.Unlock()
	// Prime alert state before the engine runs the souls taken over.
	w.seedAlertState(acquired)
	w.engine.AssignSouls(mine)
	if changed {
		w.logger.Info("soul ownership updated", "node", self, "owned", len(owned), "running", len(mine), "total", len(souls))
	}

	if w.journeys != nil {
		w.reconcileJourneys(self)
	}
}

// reconcileJourneys hands the journey executor exactly the journeys this node
// owns, planned like souls so each journey runs on one member.
func (w *clusterWork) reconcileJourneys(self string) {
	select {
	case <-w.stopCh:
		return
	default:
	}
	journeys := collectJourneys(context.Background(), w.cfg, w.store, w.logger)
	plan := w.mgr.PlanJourneys(journeys)
	mine := make([]*core.JourneyConfig, 0, len(journeys))
	for _, j := range journeys {
		if plan[j.ID] == self {
			mine = append(mine, j)
		}
	}
	w.journeys.Assign(w.journeyCtx, mine)
}

// replicateJourneyRun is the journey executor's run saver in cluster mode: the
// run is replicated in the next batch, so every node can list it.
func (w *clusterWork) replicateJourneyRun(_ context.Context, run *core.JourneyRun) error {
	return w.replicateAsync(journeyRunKey(run), run)
}

func journeyRunKey(run *core.JourneyRun) string {
	if run.ID == "" {
		run.ID = core.GenerateID()
	}
	workspaceID := run.WorkspaceID
	if workspaceID == "" {
		workspaceID = "default"
	}
	return fmt.Sprintf("%s/journey-runs/%s/%d/%s", workspaceID, run.JourneyID, run.StartedAt, run.ID)
}

// Manual calls acknowledge a committed, locally applied record. Scheduled
// runs keep the asynchronous batch path above.
func (w *clusterWork) replicateJourneyRunSync(ctx context.Context, run *core.JourneyRun) error {
	return saveJourneyRunSync(ctx, run, w.mgr.NodeID(), w.mgr)
}

func saveJourneyRunSync(ctx context.Context, run *core.JourneyRun, origin string, applier mutationApplier) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timeout := 30 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return context.DeadlineExceeded
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	key := journeyRunKey(run)
	data, err := json.Marshal(run)
	if err != nil {
		return err
	}
	if _, _, err := applier.ApplyMutation(core.FSMCommand{Op: core.FSMSet, Key: key, Value: data, Origin: origin}, timeout); err != nil {
		return fmt.Errorf("commit journey run: %w", err)
	}
	return nil
}

// seedAlertWindow bounds the judgment history read when a soul is taken over:
// seedAlertJudgments checks of it, at most a day and at least ten minutes.
const seedAlertJudgments = 200

// seedAlertState primes the alert manager for souls this node just took over
// from their replicated judgments, so failure streaks and status_for clocks
// continue where the previous owner left them. (Notification dedup state is
// replicated directly; see alertStorageAdapter.SaveDedupState.)
func (w *clusterWork) seedAlertState(souls []*core.Soul) {
	if w.alerts == nil {
		return
	}
	now := time.Now()
	for _, soul := range souls {
		window := time.Duration(seedAlertJudgments) * soul.Weight.Duration
		if window < 10*time.Minute {
			window = 10 * time.Minute
		}
		if window > 24*time.Hour {
			window = 24 * time.Hour
		}
		ctx := core.ContextWithWorkspaceID(context.Background(), workspaceOrDefault(soul.WorkspaceID))
		judgments, err := w.store.ListJudgments(ctx, soul.ID, now.Add(-window), now.Add(time.Minute), seedAlertJudgments)
		if err != nil {
			w.logger.Debug("failed to read judgments to seed alert state", "soul", soul.ID, "err", err)
			continue
		}
		w.alerts.SeedOwnedSoul(soul.ID, judgments)
	}
}

// Owns reports whether this node owned soulID at the last ownership pass.
func (w *clusterWork) Owns(soulID string) bool {
	w.ownedMu.RLock()
	defer w.ownedMu.RUnlock()
	return w.owned[soulID]
}

// judgmentStore is the storage surface the replicated-judgment handler uses:
// it reads a judgment to skip a duplicate and writes the replicated judgment.
// It is an interface so the handler's failure classification -- a storage
// failure is transient, a payload that cannot be decoded is not -- can be
// exercised without a real disk.
type judgmentStore interface {
	GetJudgmentByID(ctx context.Context, workspaceID, id string) (*core.Judgment, error)
	SaveJudgment(ctx context.Context, j *core.Judgment) error
}

// nodeJudgmentStore binds the replicated-judgment handler to the node's real
// storage. The judgment is always read from the main store, and written
// through the time-series store when one is configured, because that store
// also folds the judgment into the summaries.
type nodeJudgmentStore struct {
	store *storage.CobaltDB
	ts    *storage.TimeSeriesStore
}

func (s nodeJudgmentStore) GetJudgmentByID(ctx context.Context, workspaceID, id string) (*core.Judgment, error) {
	return s.store.GetJudgmentByID(ctx, workspaceID, id)
}

func (s nodeJudgmentStore) SaveJudgment(ctx context.Context, j *core.Judgment) error {
	if s.ts != nil {
		return s.ts.SaveJudgment(ctx, j)
	}
	return s.store.SaveJudgment(ctx, j)
}

// applyJudgment is the FSM handler for replicated judgments. It skips a
// judgment that is already stored, which keeps the summaries right when a
// forwarded batch is applied twice.
func (w *clusterWork) applyJudgment(cmd *core.FSMCommand) error {
	return applyReplicatedJudgment(cmd, nodeJudgmentStore{store: w.store, ts: w.ts})
}

// applyReplicatedJudgment applies one replicated judgment command.
//
// The failure it returns says whether a later attempt may still succeed. A
// storage failure is transient -- the disk may recover -- so it is marked
// retryable and the node retries the committed entry. A payload that cannot
// be decoded, or an op the handler does not support, can never be applied, so
// it stays a plain error: the element is skipped and the node keeps applying
// the rest of the log.
func applyReplicatedJudgment(cmd *core.FSMCommand, store judgmentStore) error {
	if cmd.Op != core.FSMSet {
		return fmt.Errorf("unsupported judgment op %d", cmd.Op)
	}
	var j core.Judgment
	if err := json.Unmarshal(cmd.Value, &j); err != nil {
		return fmt.Errorf("decode replicated judgment: %w", err)
	}
	ctx := context.Background()
	if _, err := store.GetJudgmentByID(ctx, j.WorkspaceID, j.ID); err == nil {
		return nil
	}
	if err := store.SaveJudgment(ctx, &j); err != nil {
		return raft.Retryable(err)
	}
	return nil
}

// replicateJudgment queues a judgment this node produced for replication.
func (w *clusterWork) replicateJudgment(j *core.Judgment) error {
	if j.ID == "" {
		j.ID = core.GenerateID()
	}
	if j.WorkspaceID == "" {
		j.WorkspaceID = "default"
	}
	data, err := json.Marshal(j)
	if err != nil {
		return err
	}
	w.batcher.Enqueue(core.FSMCommand{
		Op:     core.FSMSet,
		Table:  judgmentTable,
		Key:    fmt.Sprintf("%s/judgments/%s/%d/%s", j.WorkspaceID, j.SoulID, j.Timestamp.UnixNano(), j.ID),
		Value:  data,
		Origin: w.mgr.NodeID(),
	})
	return nil
}

// replicateAsync queues a keyed write for the next batch.
func (w *clusterWork) replicateAsync(key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	w.batcher.Enqueue(core.FSMCommand{Op: core.FSMSet, Key: key, Value: data, Origin: w.mgr.NodeID()})
	return nil
}

// observe is the apply observer. It runs on the Raft apply path, so it only
// queues the command; eventLoop does the work in order.
func (w *clusterWork) observe(cmd core.FSMCommand) {
	w.enqueue(clusterEvent{cmd: cmd})
}

func (w *clusterWork) enqueue(ev clusterEvent) {
	w.eventsMu.Lock()
	w.events = append(w.events, ev)
	w.eventsMu.Unlock()
	select {
	case w.eventsCh <- struct{}{}:
	default:
	}
}

func (w *clusterWork) eventLoop() {
	defer w.wg.Done()
	for {
		select {
		case <-w.stopCh:
			return
		case <-w.eventsCh:
		}
		w.eventsMu.Lock()
		events := w.events
		w.events = nil
		w.eventsMu.Unlock()
		for _, ev := range events {
			w.handleEvent(ev)
		}
	}
}

// handleEvent reflects one applied command in memory. Commands this node
// proposed are skipped where the proposing path already updated memory (alert
// state, live judgment pushes); soul changes always trigger an ownership pass,
// which is idempotent.
func (w *clusterWork) handleEvent(ev clusterEvent) {
	if ev.restore {
		if w.alerts != nil {
			w.alerts.Resync()
		}
		w.Trigger()
		return
	}
	cmd := ev.cmd
	local := cmd.Origin != "" && cmd.Origin == w.mgr.NodeID()

	if cmd.Table == judgmentTable {
		if !local && w.onJudgment != nil {
			var j core.Judgment
			if json.Unmarshal(cmd.Value, &j) == nil {
				w.onJudgment(&j)
			}
		}
		return
	}

	parts := strings.SplitN(cmd.Key, "/", 4)
	if len(parts) < 3 {
		return
	}
	switch {
	case parts[1] == "souls", parts[1] == "journeys":
		w.Trigger()
	case parts[1] == "alerts" && len(parts) == 4 && w.alerts != nil && !local:
		w.applyAlertRecord(parts[2], parts[3], cmd)
	case parts[1] == "heartbeats" && cmd.Op == core.FSMSet && w.engine != nil:
		// A ping landed on some node; only the owner runs the soul, so only
		// it judges the ping (EvaluateSoul is a no-op elsewhere).
		w.engine.EvaluateSoul(parts[2])
	}
}

func (w *clusterWork) applyAlertRecord(kind, id string, cmd core.FSMCommand) {
	set := cmd.Op == core.FSMSet
	switch kind {
	case "channels":
		if !set {
			w.alerts.UnloadChannel(id)
			return
		}
		var ch core.AlertChannel
		if json.Unmarshal(cmd.Value, &ch) == nil {
			w.alerts.LoadChannel(&ch)
		}
	case "rules":
		if !set {
			w.alerts.UnloadRule(id)
			return
		}
		var rule core.AlertRule
		if json.Unmarshal(cmd.Value, &rule) == nil {
			w.alerts.LoadRule(&rule)
		}
	case "dedup":
		if !set {
			w.alerts.UnloadDedupState(id)
			return
		}
		var entry core.AlertHistoryEntry
		if json.Unmarshal(cmd.Value, &entry) == nil {
			w.alerts.LoadDedupState(&entry)
		}
	case "incidents":
		if !set {
			return
		}
		var incident core.Incident
		if json.Unmarshal(cmd.Value, &incident) == nil {
			w.alerts.LoadIncident(&incident)
		}
	}
}

// collectSouls returns the configured souls plus every soul stored in any
// workspace, de-duplicated by ID -- the set the probe engine is responsible
// for.
func collectSouls(ctx context.Context, cfg *core.Config, store *storage.CobaltDB, logger *slog.Logger) []*core.Soul {
	souls := make([]*core.Soul, 0, len(cfg.Souls))
	seen := make(map[string]struct{}, len(cfg.Souls))
	for i := range cfg.Souls {
		soul := &cfg.Souls[i]
		if _, dup := seen[soul.ID]; dup {
			continue
		}
		seen[soul.ID] = struct{}{}
		souls = append(souls, soul)
	}

	workspaceIDs := allWorkspaceIDs(ctx, store, logger, "probe assignment")
	for _, workspaceID := range workspaceIDs {
		stored, err := store.ListSouls(ctx, workspaceID, 0, 0)
		if err != nil {
			logger.Warn("failed to load stored souls for probe assignment", "workspace", workspaceID, "err", err)
			continue
		}
		for _, soul := range stored {
			if soul == nil || soul.ID == "" {
				continue
			}
			if _, dup := seen[soul.ID]; dup {
				continue
			}
			seen[soul.ID] = struct{}{}
			souls = append(souls, soul)
		}
	}
	return souls
}

// collectJourneys returns the configured journeys plus every journey stored in
// any workspace, de-duplicated by ID.
func collectJourneys(ctx context.Context, cfg *core.Config, store *storage.CobaltDB, logger *slog.Logger) []*core.JourneyConfig {
	journeys := make([]*core.JourneyConfig, 0, len(cfg.Journeys))
	seen := make(map[string]struct{}, len(cfg.Journeys))
	for i := range cfg.Journeys {
		j := cfg.Journeys[i]
		if j.ID == "" {
			continue
		}
		if _, dup := seen[j.ID]; dup {
			continue
		}
		seen[j.ID] = struct{}{}
		if j.WorkspaceID == "" {
			j.WorkspaceID = "default"
		}
		journeys = append(journeys, &j)
	}

	workspaceIDs := allWorkspaceIDs(ctx, store, logger, "journey assignment")
	for _, workspaceID := range workspaceIDs {
		stored, err := store.ListJourneys(ctx, workspaceID)
		if err != nil {
			logger.Warn("failed to load stored journeys for assignment", "workspace", workspaceID, "err", err)
			continue
		}
		for _, j := range stored {
			if j == nil || j.ID == "" {
				continue
			}
			if _, dup := seen[j.ID]; dup {
				continue
			}
			seen[j.ID] = struct{}{}
			journeys = append(journeys, j)
		}
	}
	return journeys
}

// clusterProbe is the probe engine as the API surfaces (REST, MCP, gRPC) see
// it in cluster mode. Their writes have already replicated by the time they
// call UpsertSoul or RemoveSoul, so rather than scheduling the soul here --
// which would run it on whichever node took the request -- it asks for an
// ownership pass, which schedules it on its owner only.
type clusterProbe struct {
	engine *probe.Engine
	work   *clusterWork
}

func (p *clusterProbe) UpsertSoul(*core.Soul) { p.work.Trigger() }

func (p *clusterProbe) RemoveSoul(soulID string) {
	p.engine.RemoveSoul(soulID)
	p.work.Trigger()
}

func (p *clusterProbe) GetStatus() *core.ProbeStatus { return p.engine.GetStatus() }

func (p *clusterProbe) ForceCheck(soulID string) (*core.Judgment, error) {
	return p.engine.ForceCheck(soulID)
}

// allWorkspaceIDs lists "default" plus every stored workspace, without
// duplicates. The default workspace is implicit and never stored as a record.
// A listing error is logged (when a logger is given) and the default
// workspace is still returned.
func allWorkspaceIDs(ctx context.Context, store *storage.CobaltDB, logger *slog.Logger, purpose string) []string {
	ids := []string{"default"}
	workspaces, err := store.ListWorkspaces(ctx)
	if err != nil {
		if logger != nil {
			logger.Warn("failed to load workspaces for "+purpose, "err", err)
		}
		return ids
	}
	for _, workspace := range workspaces {
		if workspace != nil && workspace.ID != "" && workspace.ID != "default" {
			ids = append(ids, workspace.ID)
		}
	}
	return ids
}
