package probe

import (
	"context"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"sync/atomic"
	"testing"
	"time"
)

type auditStorageCompletionGate struct {
	mockProbeStorage
	entered, release chan struct{}
}

func (s *auditStorageCompletionGate) SaveJudgment(context.Context, *core.Judgment) error {
	close(s.entered)
	<-s.release
	return nil
}
func TestAuditProbeStorageCompletion(t *testing.T) {
	for _, action := range []string{"control", "remove", "update", "stop"} {
		t.Run(action, func(t *testing.T) {
			store := &auditStorageCompletionGate{entered: make(chan struct{}), release: make(chan struct{})}
			checker := &countingChecker{}
			registry := NewCheckerRegistry()
			registry.Register(checker)
			var published atomic.Int64
			e := NewEngine(EngineOptions{Registry: registry, Store: store, Logger: newTestProbeLogger(), OnJudgment: func(*core.Judgment) { published.Add(1) }})
			defer e.Stop()
			soul := &core.Soul{ID: "storage-gate", Type: checker.Type(), Weight: core.Duration{Duration: time.Hour}}
			ctx, cancel := context.WithCancel(e.ctx)
			defer cancel()
			runner := &soulRunner{soul: soul, interval: time.Hour, ticker: time.NewTicker(time.Hour), cancel: cancel}
			defer runner.ticker.Stop()
			e.souls[soul.ID] = runner
			done := make(chan struct{})
			e.wg.Add(1)
			go func() { defer e.wg.Done(); defer close(done); e.judgeSoul(ctx, runner) }()
			<-store.entered
			var stopped chan struct{}
			switch action {
			case "remove":
				e.RemoveSoul(soul.ID)
			case "update":
				updated := *soul
				updated.Target = "updated"
				e.UpsertSoul(&updated)
			case "stop":
				stopped = make(chan struct{})
				go func() { e.Stop(); close(stopped) }()
				<-ctx.Done()
			}
			close(store.release)
			<-done
			if stopped != nil {
				<-stopped
			}
			want := int64(0)
			if action == "control" {
				want = 1
			}
			t.Logf("EXPECTED: publications=%d ACTUAL: publications=%d", want, published.Load())
			if published.Load() != want {
				t.Error("regression mismatch")
			}
		})
	}
}
