package probe

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// ErrNoJudgment is returned by a checker when an evaluation produced nothing
// to record. The engine skips the judgment (no storage write, no alert
// evaluation) but still counts the evaluation as engine progress. Only the
// heartbeat checker returns it: a push monitor is judged when a ping arrives
// or when its deadline passes, not on every evaluation tick.
var ErrNoJudgment = errors.New("probe: no judgment due")

// HeartbeatStateSource gives the heartbeat checker the pings a soul received
// and the soul's latest judgment. In cluster mode both are read from
// Raft-replicated storage, so whichever node owns the soul sees pings that
// landed on any node.
type HeartbeatStateSource interface {
	// GetHeartbeatState returns the soul's ping state, or nil when it never
	// received a ping.
	GetHeartbeatState(ctx context.Context, workspaceID, soulID string) (*core.HeartbeatState, error)
	// LatestJudgment returns the soul's newest stored judgment, or nil.
	LatestJudgment(ctx context.Context, workspaceID, soulID string) (*core.Judgment, error)
}

// HeartbeatChecker judges push (heartbeat) souls. It never dials anything:
// it compares the replicated ping state with the soul's deadline.
//
//   - A ping newer than the last one judged becomes one judgment stamped with
//     the ping time: alive for a success ping (duration = time since /start),
//     dead for a /fail ping.
//   - With no new ping, nothing is recorded until LastPingAt + interval +
//     grace has passed; then one dead judgment, and another every interval
//     while no ping arrives, so uptime and alert rules see the outage.
//
// Progress (which ping was judged, when the last missed-deadline judgment was
// recorded) is kept in memory per soul and seeded from the latest stored
// judgment the first time a soul is evaluated, so a restart or an ownership
// change neither re-judges an old ping nor repeats a dead judgment early.
type HeartbeatChecker struct {
	source HeartbeatStateSource
	now    func() time.Time

	mu       sync.Mutex
	progress map[string]*heartbeatProgress
	// memory holds ping state when no source is configured (tests and the
	// global default registry).
	memory map[string]*core.HeartbeatState
}

type heartbeatProgress struct {
	judgedPing time.Time // LastPingAt of the newest ping turned into a judgment
	lastDead   time.Time // when the last missed-deadline judgment was recorded
	firstSeen  time.Time // reference for a soul that never pinged and has no CreatedAt
}

// NewHeartbeatChecker creates a heartbeat checker. A nil source keeps ping
// state in memory (see RecordPing).
func NewHeartbeatChecker(source HeartbeatStateSource) *HeartbeatChecker {
	return &HeartbeatChecker{
		source:   source,
		now:      time.Now,
		progress: make(map[string]*heartbeatProgress),
		memory:   make(map[string]*core.HeartbeatState),
	}
}

// SetClock replaces the checker's clock (tests).
func (c *HeartbeatChecker) SetClock(now func() time.Time) { c.now = now }

// Type implements Checker.
func (c *HeartbeatChecker) Type() core.CheckType { return core.CheckHeartbeat }

// Validate implements Checker.
func (c *HeartbeatChecker) Validate(soul *core.Soul) error {
	if soul.Heartbeat == nil {
		return configError("heartbeat", "heartbeat configuration is required")
	}
	return nil
}

// RecordPing folds a ping into the in-memory state. It is only used when the
// checker has no source.
func (c *HeartbeatChecker) RecordPing(soulID string, event core.HeartbeatEvent, at time.Time, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.memory[soulID]
	if st == nil {
		st = &core.HeartbeatState{SoulID: soulID}
		c.memory[soulID] = st
	}
	st.Apply(event, at, message, "", "")
}

// Forget drops the progress kept for a soul that is no longer scheduled
// here, so a later takeover re-seeds it from storage.
func (c *HeartbeatChecker) Forget(soulID string) {
	c.mu.Lock()
	delete(c.progress, soulID)
	c.mu.Unlock()
}

func (c *HeartbeatChecker) state(ctx context.Context, soul *core.Soul) (*core.HeartbeatState, error) {
	if c.source == nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		if st := c.memory[soul.ID]; st != nil {
			cp := *st
			return &cp, nil
		}
		return nil, nil
	}
	return c.source.GetHeartbeatState(ctx, soul.WorkspaceID, soul.ID)
}

// Judge implements Checker.
func (c *HeartbeatChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
	st, err := c.state(ctx, soul)
	if err != nil {
		return nil, fmt.Errorf("read heartbeat state: %w", err)
	}
	var lastPing time.Time
	if st != nil {
		lastPing = st.LastPingAt
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now().UTC()

	p := c.progress[soul.ID]
	if p == nil {
		p = &heartbeatProgress{firstSeen: now}
		if c.source != nil {
			if latest, err := c.source.LatestJudgment(ctx, soul.WorkspaceID, soul.ID); err == nil && latest != nil {
				if !lastPing.IsZero() && !latest.Timestamp.Before(lastPing) {
					p.judgedPing = lastPing
				}
				if latest.Status == core.SoulDead && latest.Timestamp.After(lastPing) {
					p.lastDead = latest.Timestamp
				}
			}
		}
		c.progress[soul.ID] = p
	}

	// A new ping: judge it, stamped with the time it arrived.
	if !lastPing.IsZero() && lastPing.After(p.judgedPing) {
		p.judgedPing = lastPing
		p.lastDead = time.Time{}
		j := &core.Judgment{
			ID:        core.GenerateID(),
			SoulID:    soul.ID,
			Timestamp: lastPing.UTC(),
			Duration:  st.LastDuration,
			Status:    core.SoulAlive,
			Message:   "heartbeat ping received",
			Details:   &core.JudgmentDetails{},
		}
		if st.LastEvent == core.HeartbeatFail {
			j.Status = core.SoulDead
			j.Message = "job reported failure"
			if st.LastMessage != "" {
				j.Message += ": " + st.LastMessage
			}
		}
		return j, nil
	}

	interval := soul.HeartbeatInterval()
	grace := soul.HeartbeatGrace()
	ref := lastPing
	if ref.IsZero() {
		ref = soul.CreatedAt
		if ref.IsZero() || ref.After(now) {
			ref = p.firstSeen
		}
	}
	if now.Before(ref.Add(interval + grace)) {
		return nil, ErrNoJudgment
	}
	// Overdue: one dead judgment per interval until a ping arrives.
	if !p.lastDead.IsZero() && p.lastDead.After(ref) && now.Sub(p.lastDead) < interval {
		return nil, ErrNoJudgment
	}
	p.lastDead = now
	msg := fmt.Sprintf("no heartbeat ping received (expected every %s, grace %s)", interval, grace)
	if !lastPing.IsZero() {
		msg = fmt.Sprintf("no heartbeat ping since %s (expected every %s, grace %s)", lastPing.UTC().Format(time.RFC3339), interval, grace)
	}
	return &core.Judgment{
		ID:        core.GenerateID(),
		SoulID:    soul.ID,
		Timestamp: now,
		Status:    core.SoulDead,
		Message:   msg,
		Details:   &core.JudgmentDetails{},
	}, nil
}

// heartbeatEvalInterval is how often the engine evaluates a heartbeat soul's
// deadline: a quarter of the shorter of interval and grace, between 1s and
// 30s. Evaluations that find nothing to record cost one storage read.
func heartbeatEvalInterval(s *core.Soul) time.Duration {
	window := min(s.HeartbeatInterval(), s.HeartbeatGrace()) / 4
	return min(max(window, time.Second), 30*time.Second)
}

// soulForgetter is implemented by checkers that keep per-soul state.
type soulForgetter interface {
	Forget(soulID string)
}
