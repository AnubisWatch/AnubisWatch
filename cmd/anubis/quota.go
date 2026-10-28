package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

// quotaGuard enforces tenants.default_quotas. REST, gRPC and MCP write souls
// and journeys through restStorageAdapter, and alert channels (from every API
// and from the config file) through the alert manager's alertStorageAdapter;
// both adapters call this guard, so no surface can skip it.
//
// Only creates are counted: an update to a resource that already exists is
// never refused for a count quota, so data left over a lowered limit stays
// editable. check_interval_min refuses a create below the floor, and an update
// only when it changes the interval to a value below it.
//
// Counts come from this node's copy of storage, which in a cluster is the
// Raft-replicated state. mu serialises check-and-write on this node, so
// concurrent creates through one node cannot overshoot; two nodes creating at
// the same instant can each pass before seeing the other's commit, overshooting
// by at most one per concurrent node. That race is accepted rather than routing
// every create through a quota-aware FSM command.
type quotaGuard struct {
	store   *storage.CobaltDB
	enabled bool
	limits  core.QuotaConfig
	mu      sync.Mutex
}

func newQuotaGuard(store *storage.CobaltDB, tenants core.TenantsConfig) *quotaGuard {
	return &quotaGuard{store: store, enabled: tenants.Enabled, limits: tenants.DefaultQuotas}
}

func (g *quotaGuard) active() bool {
	return g != nil && g.enabled && g.store != nil
}

// noUnlock is what an admit call returns when it took no lock.
func noUnlock() {}

// admitSoul checks a soul write. The caller must call the returned function
// once the write has finished.
func (g *quotaGuard) admitSoul(soul *core.Soul) (func(), error) {
	if !g.active() || soul == nil {
		return noUnlock, nil
	}
	ws := workspaceOrDefault(soul.WorkspaceID)
	existing, _ := g.store.GetSoulNoCtx(soul.ID)
	if existing != nil && workspaceOrDefault(existing.WorkspaceID) != ws {
		existing = nil
	}
	if err := g.checkInterval(ws, soul.Weight.Duration, existing != nil, func() time.Duration { return existing.Weight.Duration }); err != nil {
		return noUnlock, err
	}
	if existing != nil || g.limits.MaxSouls <= 0 {
		return noUnlock, nil
	}
	g.mu.Lock()
	souls, err := g.store.ListSouls(context.Background(), ws, 0, 0)
	if err != nil {
		g.mu.Unlock()
		return noUnlock, err
	}
	if err := core.CheckQuotaCount(ws, core.QuotaMaxSouls, g.limits.MaxSouls, len(souls)); err != nil {
		g.mu.Unlock()
		return noUnlock, err
	}
	return g.mu.Unlock, nil
}

// admitJourney checks a journey write, like admitSoul.
func (g *quotaGuard) admitJourney(j *core.JourneyConfig) (func(), error) {
	if !g.active() || j == nil {
		return noUnlock, nil
	}
	ws := workspaceOrDefault(j.WorkspaceID)
	existing, _ := g.store.GetJourney(context.Background(), ws, j.ID)
	if err := g.checkInterval(ws, j.Weight.Duration, existing != nil, func() time.Duration { return existing.Weight.Duration }); err != nil {
		return noUnlock, err
	}
	if existing != nil || g.limits.MaxJourneys <= 0 {
		return noUnlock, nil
	}
	g.mu.Lock()
	journeys, err := g.store.ListJourneys(context.Background(), ws)
	if err != nil {
		g.mu.Unlock()
		return noUnlock, err
	}
	if err := core.CheckQuotaCount(ws, core.QuotaMaxJourneys, g.limits.MaxJourneys, len(journeys)); err != nil {
		g.mu.Unlock()
		return noUnlock, err
	}
	return g.mu.Unlock, nil
}

// admitChannel checks an alert channel write, like admitSoul.
func (g *quotaGuard) admitChannel(ch *core.AlertChannel) (func(), error) {
	if !g.active() || ch == nil || g.limits.MaxAlertChannels <= 0 {
		return noUnlock, nil
	}
	ws := workspaceOrDefault(ch.WorkspaceID)
	if existing, err := g.store.GetAlertChannel(ch.ID, ws); err == nil && existing != nil {
		return noUnlock, nil
	}
	g.mu.Lock()
	channels, err := g.store.ListAlertChannels(ws)
	if err != nil {
		g.mu.Unlock()
		return noUnlock, err
	}
	if err := core.CheckQuotaCount(ws, core.QuotaMaxAlertChannels, g.limits.MaxAlertChannels, len(channels)); err != nil {
		g.mu.Unlock()
		return noUnlock, err
	}
	return g.mu.Unlock, nil
}

// admitMember checks adding one member (or invitation) to a workspace that
// already has current of them. The auth directory calls it under its own
// lock, which serialises member changes, so no lock is taken here.
func (g *quotaGuard) admitMember(workspace string, current int) error {
	if !g.active() || g.limits.MaxTeamMembers <= 0 {
		return nil
	}
	return core.CheckQuotaCount(workspaceOrDefault(workspace), core.QuotaMaxTeamMembers, g.limits.MaxTeamMembers, current)
}

// checkInterval applies check_interval_min. An update that keeps its current
// interval passes even when that interval is below the floor.
func (g *quotaGuard) checkInterval(ws string, interval time.Duration, update bool, current func() time.Duration) error {
	floor := g.limits.CheckIntervalMin.Duration
	if floor <= 0 || (update && current() == interval) {
		return nil
	}
	return core.CheckQuotaInterval(ws, floor, interval)
}

// usage reports the workspace's counts against its limits. With tenants off
// every limit is reported as 0 (unlimited).
func (g *quotaGuard) usage(workspace string) (*core.WorkspaceQuotaUsage, error) {
	ws := workspaceOrDefault(workspace)
	u := &core.WorkspaceQuotaUsage{Workspace: ws}
	if g == nil || g.store == nil {
		return u, nil
	}
	ctx := context.Background()
	souls, err := g.store.ListSouls(ctx, ws, 0, 0)
	if err != nil {
		return nil, err
	}
	journeys, err := g.store.ListJourneys(ctx, ws)
	if err != nil {
		return nil, err
	}
	channels, err := g.store.ListAlertChannels(ws)
	if err != nil {
		return nil, err
	}
	u.Souls.Used, u.Journeys.Used, u.AlertChannels.Used = len(souls), len(journeys), len(channels)
	if g.enabled {
		u.Enforced = true
		u.Souls.Limit = max(g.limits.MaxSouls, 0)
		u.Journeys.Limit = max(g.limits.MaxJourneys, 0)
		u.AlertChannels.Limit = max(g.limits.MaxAlertChannels, 0)
		u.TeamMembers.Limit = max(g.limits.MaxTeamMembers, 0)
		u.CheckIntervalMin = core.Duration{Duration: max(g.limits.CheckIntervalMin.Duration, 0)}
		u.RetentionDays = max(g.limits.RetentionDays, 0)
	}
	return u, nil
}

// applyQuotaRetention caps the time-series retention at
// tenants.default_quotas.retention_days. Retention is purged node-wide, so the
// quota caps every workspace's history at once.
func applyQuotaRetention(cfg *core.Config, logger *slog.Logger) {
	if cfg == nil || !cfg.Tenants.Enabled {
		return
	}
	days := cfg.Tenants.DefaultQuotas.RetentionDays
	if core.CapRetention(&cfg.Storage.TimeSeries.Retention, days) && logger != nil {
		logger.Info("time-series retention capped by tenants.default_quotas.retention_days", "days", days)
	}
}
