package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

// Heartbeat (push) monitors in the server.
//
// A ping can land on any node. The receiving node folds it into the soul's
// HeartbeatState record ({workspace}/heartbeats/{soul}) and writes that
// record through the same path as every other API write: a Raft mutation in
// cluster mode, a local Put standalone. Only the soul's owner judges it: its
// probe engine runs the heartbeat checker, which reads the replicated state
// and records an alive/dead judgment through the normal judgment pipeline
// (storage/replication, live push, Ma'at). The owner is woken as soon as the
// record applies -- standalone by the recorder, in a cluster by the apply
// observer (clusterWork.handleEvent), which every node runs but only the
// owner has a runner for. Deadline checks also run only on the owner, so a
// missed ping yields one dead judgment cluster-wide.

// heartbeatSource reads ping state and the latest judgment from the local
// store, which Raft keeps current on every node.
type heartbeatSource struct {
	store *storage.CobaltDB
}

func (h heartbeatSource) GetHeartbeatState(_ context.Context, workspaceID, soulID string) (*core.HeartbeatState, error) {
	return loadHeartbeatState(h.store, workspaceID, soulID)
}

func (h heartbeatSource) LatestJudgment(ctx context.Context, workspaceID, soulID string) (*core.Judgment, error) {
	j, err := h.store.GetLatestJudgment(ctx, workspaceID, soulID)
	var nf *core.NotFoundError
	if errors.As(err, &nf) {
		return nil, nil
	}
	return j, err
}

func loadHeartbeatState(store *storage.CobaltDB, workspaceID, soulID string) (*core.HeartbeatState, error) {
	data, err := store.Get(core.HeartbeatStateKey(workspaceID, soulID))
	if err != nil {
		var nf *core.NotFoundError
		if errors.As(err, &nf) {
			return nil, nil
		}
		return nil, err
	}
	var st core.HeartbeatState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("decode heartbeat state: %w", err)
	}
	return &st, nil
}

// heartbeatRecorder implements api.HeartbeatRecorder.
type heartbeatRecorder struct {
	store  *storage.CobaltDB
	rest   *restStorageAdapter // replicates the record in cluster mode
	nodeID string
	// wake asks the local engine to judge the soul now (standalone only; in
	// a cluster the apply observer wakes the owner).
	wake func(soulID string)
	now  func() time.Time

	// mu serialises this node's read-modify-write of ping records.
	mu sync.Mutex
}

func (r *heartbeatRecorder) GetHeartbeatState(_ context.Context, workspaceID, soulID string) (*core.HeartbeatState, error) {
	return loadHeartbeatState(r.store, workspaceID, soulID)
}

func (r *heartbeatRecorder) RecordHeartbeat(_ context.Context, soul *core.Soul, event core.HeartbeatEvent, message, source string) error {
	now := time.Now
	if r.now != nil {
		now = r.now
	}
	r.mu.Lock()
	st, err := loadHeartbeatState(r.store, soul.WorkspaceID, soul.ID)
	if err != nil {
		r.mu.Unlock()
		return err
	}
	if st == nil {
		st = &core.HeartbeatState{SoulID: soul.ID, WorkspaceID: soul.WorkspaceID}
	}
	st.Apply(event, now().UTC(), message, source, r.nodeID)
	data, err := json.Marshal(st)
	if err != nil {
		r.mu.Unlock()
		return err
	}
	key := core.HeartbeatStateKey(soul.WorkspaceID, soul.ID)
	if r.rest != nil && r.rest.replicator != nil {
		err = r.rest.applyMutation(core.FSMCommand{Op: core.FSMSet, Key: key, Value: data})
	} else {
		err = r.store.Put(key, data)
	}
	r.mu.Unlock()
	if err != nil {
		return err
	}
	if r.wake != nil && event != core.HeartbeatStart {
		r.wake(soul.ID)
	}
	return nil
}

// heartbeatSoulLister lists every monitored soul (configured and stored) for
// the ping-token index.
func heartbeatSoulLister(cfg *core.Config, store *storage.CobaltDB, logger *slog.Logger) func(context.Context) ([]*core.Soul, error) {
	return func(ctx context.Context) ([]*core.Soul, error) {
		return collectSouls(ctx, cfg, store, logger), nil
	}
}

// assignConfigHeartbeatIDs gives configured heartbeat souls without an ID the
// same stable ID cluster mode derives from the name, so their ping state has
// a key on every node and across restarts.
func assignConfigHeartbeatIDs(cfg *core.Config) {
	for i := range cfg.Souls {
		if cfg.Souls[i].Type == core.CheckHeartbeat && cfg.Souls[i].ID == "" {
			cfg.Souls[i].ID = configEntityID("config-soul-", cfg.Souls[i].Name)
		}
	}
}
