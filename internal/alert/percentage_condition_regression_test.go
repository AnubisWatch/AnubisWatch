package alert

// Regression test: the "percentage" condition type is accepted by rule
// validation (core validConditionTypes) and documented in
// docs/CONFIGURATION.md, so it must actually evaluate. It had no case in
// checkConditions or evaluateCondition, making such a rule permanently
// silent. Contract: metric + operator + threshold + window (docs example:
// failure_rate > 10 over 5m).

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestPercentageConditionFires(t *testing.T) {
	m := NewManager(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Now()

	pctRule := func(metric string) *core.AlertRule {
		return &core.AlertRule{
			ID:   "rule-" + metric,
			Name: metric,
			Conditions: []core.AlertCondition{{
				Type:      "percentage",
				Metric:    metric,
				Operator:  ">",
				Threshold: 10,
				Window:    core.Duration{Duration: 5 * time.Minute},
			}},
		}
	}

	// 4 alive + 1 dead within the window = 20% failures > 10% threshold.
	rule := pctRule("failure_rate")
	for i := 0; i < 4; i++ {
		m.noteRateJudgment(&core.Judgment{
			ID: core.GenerateID(), SoulID: "soul-1", WorkspaceID: "default",
			Status: core.SoulAlive, Timestamp: now.Add(-4*time.Minute + time.Duration(i)*30*time.Second),
		}, []*core.AlertRule{rule})
	}
	judgment := &core.Judgment{
		ID: core.GenerateID(), SoulID: "soul-1", WorkspaceID: "default",
		Status: core.SoulDead, Timestamp: now,
	}
	m.noteRateJudgment(judgment, []*core.AlertRule{rule})

	prev := core.SoulAlive
	if !m.checkConditions(rule, prev, judgment) {
		t.Fatalf("percentage condition (failure_rate > 10%% with 20%% failures) did not fire")
	}

	// uptime_percent is the complement: 80% uptime is not < 10%.
	if m.checkConditions(pctRule("uptime_percent"), prev, judgment) {
		t.Fatalf("uptime_percent 80%% wrongly breached a >10%% threshold")
	}

	// packet_loss_percent comes from the judgment's own measurement.
	lossRule := &core.AlertRule{
		ID:   "rule-loss",
		Name: "loss",
		Conditions: []core.AlertCondition{{
			Type:      "percentage",
			Metric:    "packet_loss_percent",
			Operator:  ">",
			Threshold: 5,
		}},
	}
	lossy := &core.Judgment{
		ID: core.GenerateID(), SoulID: "soul-2", WorkspaceID: "default",
		Status: core.SoulDegraded, Timestamp: now,
		Details: &core.JudgmentDetails{PacketLoss: 50},
	}
	if !m.checkConditions(lossRule, prev, lossy) {
		t.Fatalf("packet_loss_percent 50%% did not breach a >5%% threshold")
	}

	// Control: an unmeasured metric never fires.
	if m.checkConditions(pctRule("cpu_percent"), prev, judgment) {
		t.Fatalf("unmeasured metric fired")
	}
}
