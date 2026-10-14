package probe

import (
	"context"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"sync/atomic"
	"testing"
	"time"
)

type auditGateChecker struct{ entered, release chan struct{} }

func (c *auditGateChecker) Type() core.CheckType      { return "audit-gate" }
func (c *auditGateChecker) Validate(*core.Soul) error { return nil }
func (c *auditGateChecker) Judge(context.Context, *core.Soul) (*core.Judgment, error) {
	close(c.entered)
	<-c.release
	return &core.Judgment{Status: core.SoulAlive}, nil
}
func auditProbeCase(t *testing.T, action string) {
	t.Helper()
	c := &auditGateChecker{make(chan struct{}), make(chan struct{})}
	registry := NewCheckerRegistry()
	registry.Register(c)
	var published atomic.Int64
	e := NewEngine(EngineOptions{Registry: registry, Logger: newTestProbeLogger(), OnJudgment: func(*core.Judgment) { published.Add(1) }})
	soul := &core.Soul{ID: "gated", Type: c.Type(), Weight: core.Duration{Duration: time.Hour}}
	ctx, cancel := context.WithCancel(e.ctx)
	r := &soulRunner{soul: soul, interval: time.Hour, ticker: time.NewTicker(time.Hour), cancel: cancel}
	defer r.ticker.Stop()
	defer cancel()
	defer e.Stop()
	e.souls[soul.ID] = r
	done := make(chan struct{})
	e.wg.Add(1)
	go func() { defer e.wg.Done(); defer close(done); e.judgeSoul(ctx, r) }()
	<-c.entered
	var stopped chan struct{}
	switch action {
	case "remove":
		e.RemoveSoul(soul.ID)
	case "update":
		updated := *soul
		updated.Target = "new"
		e.UpsertSoul(&updated)
	case "stop":
		stopped = make(chan struct{})
		go func() { e.Stop(); close(stopped) }()
		<-ctx.Done()
	}
	close(c.release)
	<-done
	if stopped != nil {
		<-stopped
	}
	want := int64(0)
	if action == "control" {
		want = 1
	}
	t.Logf("%s EXPECTED: publications=%d ACTUAL: publications=%d", action, want, published.Load())
	if published.Load() != want {
		t.Fatal("PROBLEM CONFIRMED")
	}
}
func TestAuditProbeStale(t *testing.T) {
	for _, action := range []string{"control", "remove", "update", "stop"} {
		t.Run(action, func(t *testing.T) { auditProbeCase(t, action) })
	}
	if !t.Failed() {
		t.Log("FIX VERIFIED")
	}
}
