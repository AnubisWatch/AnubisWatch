package api

import (
	"strings"
	"testing"
	"time"
)

func TestSchedulerProblem(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		rs   RuntimeStats
		bad  bool
	}{
		{"nothing scheduled", RuntimeStats{StartedAt: now.Add(-time.Hour)}, false},
		{"recent judgment", RuntimeStats{ScheduledSouls: 3, LongestInterval: time.Minute, LastJudgmentAt: now.Add(-90 * time.Second)}, false},
		{"stalled scheduler", RuntimeStats{ScheduledSouls: 3, LongestInterval: time.Minute, LastJudgmentAt: now.Add(-4 * time.Minute)}, true},
		// An hourly monitor is not stale after ten quiet minutes.
		{"long interval", RuntimeStats{ScheduledSouls: 1, LongestInterval: time.Hour, LastJudgmentAt: now.Add(-10 * time.Minute)}, false},
		{"fresh start, no judgment yet", RuntimeStats{ScheduledSouls: 2, LongestInterval: time.Minute, StartedAt: now.Add(-30 * time.Second)}, false},
		{"never judged since start", RuntimeStats{ScheduledSouls: 2, LongestInterval: time.Minute, StartedAt: now.Add(-10 * time.Minute)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := schedulerProblem(tt.rs, now)
			if (got != "") != tt.bad {
				t.Fatalf("schedulerProblem = %q, want problem=%v", got, tt.bad)
			}
			if tt.bad && !strings.HasPrefix(got, "unhealthy") {
				t.Fatalf("problem %q must start with unhealthy so readiness fails closed", got)
			}
		})
	}
}

func TestRuntimeMetrics(t *testing.T) {
	s := &RESTServer{}
	if got := s.buildRuntimeMetrics(); got != "" {
		t.Fatalf("unwired server emitted runtime metrics: %q", got)
	}
	last := time.Unix(1_800_000_000, 0)
	s.SetRuntimeStats(func() RuntimeStats {
		return RuntimeStats{ScheduledSouls: 4, ChecksRunning: 2, LastJudgmentAt: last, ReplicationDropped: 7}
	})
	got := s.buildRuntimeMetrics()
	for _, want := range []string{
		"anubis_scheduled_souls 4\n",
		"anubis_checks_in_flight 2\n",
		"anubis_last_judgment_timestamp_seconds 1800000000\n",
		"anubis_replication_dropped_total 7\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("metrics missing %q:\n%s", want, got)
		}
	}
	s.SetRuntimeStats(func() RuntimeStats { return RuntimeStats{ReplicationDropped: -1} })
	if got := s.buildRuntimeMetrics(); strings.Contains(got, "replication_dropped") {
		t.Errorf("standalone server reported replication drops:\n%s", got)
	}
}
