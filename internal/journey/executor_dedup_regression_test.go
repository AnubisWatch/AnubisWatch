package journey

import (
	"context"
	"encoding/json"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
	"reflect"
	"testing"
)

type auditJourneyChecker struct{}

func (*auditJourneyChecker) Type() core.CheckType      { return "audit-dedup" }
func (*auditJourneyChecker) Validate(*core.Soul) error { return nil }
func (*auditJourneyChecker) Judge(_ context.Context, s *core.Soul) (*core.Judgment, error) {
	body, _ := json.Marshal(map[string]string{"value": s.Target})
	return &core.Judgment{Status: core.SoulAlive, Details: &core.JudgmentDetails{ResponseBody: string(body)}}, nil
}
func init() { probe.RegisterChecker(&auditJourneyChecker{}) }
func auditJourneyFixture(t *testing.T, extract bool) (*Executor, *core.JourneyConfig, core.StorageConfig) {
	t.Helper()
	cfg := core.StorageConfig{Path: t.TempDir()}
	db, err := storage.NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	e := NewExecutor(db, newTestLogger())
	t.Cleanup(func() { e.db.Close() })
	step := core.JourneyStep{Name: "local", Type: "audit-dedup", Target: "stable"}
	if extract {
		step.Extract = map[string]core.ExtractionRule{"value": {From: "body", Path: "$.value"}}
	}
	j := &core.JourneyConfig{ID: "dedup", WorkspaceID: "default", Steps: []core.JourneyStep{step}}
	return e, j, cfg
}
func auditJourneyDedup(t *testing.T, extract bool) {
	t.Helper()
	e, j, _ := auditJourneyFixture(t, extract)
	if e.executeJourneyRun(context.Background(), j, true) == nil {
		t.Fatal("initial run missing")
	}
	before, err := e.db.PrefixScan("default/journey-runs/")
	if err != nil {
		t.Fatal(err)
	}
	second := e.executeJourneyRun(context.Background(), j, true)
	after, err := e.db.PrefixScan("default/journey-runs/")
	if err != nil {
		t.Fatal(err)
	}
	changed := !reflect.DeepEqual(before, after)
	t.Logf("extraction=%v EXPECTED: storage_changed=%v ACTUAL: storage_changed=%v", extract, !extract, changed)
	if changed == extract {
		t.Fatal("PROBLEM CONFIRMED")
	}
	if extract && second != nil {
		t.Fatal("duplicate was not skipped")
	}
}
func TestAuditJourneyDedup(t *testing.T) {
	t.Run("no-extraction-control", func(t *testing.T) { auditJourneyDedup(t, false) })
	t.Run("identical-extraction", func(t *testing.T) { auditJourneyDedup(t, true) })
	t.Run("manual-repeat", func(t *testing.T) {
		e, j, _ := auditJourneyFixture(t, true)
		e.executeJourneyRun(context.Background(), j, true)
		before, _ := e.db.PrefixScan("default/journey-runs/")
		if e.executeJourneyRun(context.Background(), j, false) == nil {
			t.Fatal("manual duplicate skipped")
		}
		after, _ := e.db.PrefixScan("default/journey-runs/")
		if reflect.DeepEqual(before, after) {
			t.Fatal("manual result not persisted")
		}
	})
	t.Run("changed-extraction", func(t *testing.T) {
		e, j, _ := auditJourneyFixture(t, true)
		e.executeJourneyRun(context.Background(), j, true)
		before, _ := e.db.PrefixScan("default/journey-runs/")
		j.Steps[0].Target = "changed"
		if e.executeJourneyRun(context.Background(), j, true) == nil {
			t.Fatal("changed result skipped")
		}
		after, _ := e.db.PrefixScan("default/journey-runs/")
		if reflect.DeepEqual(before, after) {
			t.Fatal("changed result not persisted")
		}
	})
	if !t.Failed() {
		t.Log("FIX VERIFIED")
	}
}

func auditJourneyFailedSave(t *testing.T, fail bool) {
	t.Helper()
	e, j, cfg := auditJourneyFixture(t, true)
	ctx := context.Background()
	if fail {
		if err := e.db.Close(); err != nil {
			t.Fatal(err)
		}
		c, cancel := context.WithCancel(ctx)
		cancel()
		ctx = c
	}
	e.executeJourneyRun(ctx, j, true)
	set := e.lastHash[j.ID] != ""
	t.Logf("save_failure=%v EXPECTED: hash_set=%v ACTUAL: hash_set=%v", fail, !fail, set)
	if set == fail {
		t.Fatal("PROBLEM CONFIRMED")
	}
	if fail {
		db, err := storage.NewEngine(cfg, newTestLogger())
		if err != nil {
			t.Fatal(err)
		}
		e.db = db
		if e.executeJourneyRun(context.Background(), j, true) == nil {
			t.Fatal("retry after failure incorrectly deduplicated")
		}
		if e.lastHash[j.ID] == "" {
			t.Fatal("successful retry did not advance hash")
		}
		before, _ := e.db.PrefixScan("default/journey-runs/")
		e.executeJourneyRun(context.Background(), j, true)
		after, _ := e.db.PrefixScan("default/journey-runs/")
		if !reflect.DeepEqual(before, after) {
			t.Fatal("retry result not deduplicated")
		}
	}

}
func TestAuditJourneyFailedSave(t *testing.T) {
	t.Run("successful-control", func(t *testing.T) { auditJourneyFailedSave(t, false) })
	t.Run("failed-save", func(t *testing.T) { auditJourneyFailedSave(t, true) })
	t.Run("keep-previous-hash", func(t *testing.T) {
		e, j, _ := auditJourneyFixture(t, true)
		e.executeJourneyRun(context.Background(), j, true)
		previous := e.lastHash[j.ID]
		j.Steps[0].Target = "changed"
		e.db.Close()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		e.executeJourneyRun(ctx, j, true)
		if e.lastHash[j.ID] != previous {
			t.Fatal("failed replacement advanced hash")
		}
	})
	t.Run("manual-failure", func(t *testing.T) {
		e, j, _ := auditJourneyFixture(t, true)
		e.db.Close()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		e.executeJourneyRun(ctx, j, false)
		if e.lastHash[j.ID] != "" {
			t.Fatal("failed manual run advanced hash")
		}
	})
	if !t.Failed() {
		t.Log("FIX VERIFIED")
	}
}
