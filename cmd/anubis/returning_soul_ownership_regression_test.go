package main

import (
	"context"
	"github.com/AnubisWatch/anubiswatch/internal/alert"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
	"io"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"
)

func TestReturningSoulOwnershipRebuildsFailureStreak(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		db, e := storage.NewEngine(core.StorageConfig{Path: t.TempDir()}, logger)
		if e != nil {
			t.Fatal(e)
		}
		defer db.Close()
		now := time.Now()
		j := func(at time.Time, s core.SoulStatus) *core.Judgment {
			return &core.Judgment{ID: core.GenerateID(), WorkspaceID: "default", SoulID: "s", Timestamp: at, Status: s}
		}
		old := []*core.Judgment{j(now.Add(-4*time.Minute), core.SoulDead), j(now.Add(-3*time.Minute), core.SoulDead)}
		for _, v := range []*core.Judgment{j(now.Add(-10*time.Second), core.SoulAlive), j(now.Add(-5*time.Second), core.SoulDead)} {
			if e := db.SaveJudgment(context.Background(), v); e != nil {
				t.Fatal(e)
			}
		}
		soul := &core.Soul{ID: "s", WorkspaceID: "default", Weight: core.Duration{Duration: time.Second}}
		for _, returning := range []bool{false, true} {
			m := alert.NewManager(nil, logger)
			m.LoadRule(&core.AlertRule{ID: "r", WorkspaceID: "default", Enabled: true, Scope: core.RuleScope{Type: "all"}, Conditions: []core.AlertCondition{{Type: "consecutive_failures", Threshold: 3}}})
			if returning {
				m.SeedFromJudgments("s", old)
			}
			w := &clusterWork{store: db, alerts: m, logger: logger}
			w.seedAlertState([]*core.Soul{soul})
			m.ProcessJudgment(soul, core.SoulDead, j(now, core.SoulDead))
			count := len(m.ListActiveIncidents())
			t.Logf("EXPECTED: returning=%v no incident before3 actual consecutive failures ACTUAL: incidents=%d", returning, count)
			if !returning && count != 0 {
				t.Fatal("INVALID CONTROL")
			}
			if returning && count != 0 {
				t.Fatal("PROBLEM CONFIRMED")
			}
		}
		t.Log("PROBLEM NOT REPRODUCED")
	})
}
