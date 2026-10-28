package statuspage

import (
	"math"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// The repository returns one entry per requested day, with Status "unknown"
// and Uptime 0 for days that carry no judgments. The per-soul figure already
// skips those days; the page-level Overall must too, otherwise a monitor
// created today shows a near-zero overall uptime on the public page.
func TestOverallUptimeIgnoresDaysWithoutJudgments(t *testing.T) {
	history := map[string][]core.UptimeDay{
		"new": {
			{Date: "2026-09-01", Status: "unknown", Uptime: 0},
			{Date: "2026-09-02", Status: "unknown", Uptime: 0},
			{Date: "2026-09-03", Status: "operational", Uptime: 100},
		},
		"old": {
			{Date: "2026-09-01", Status: "operational", Uptime: 100},
			{Date: "2026-09-02", Status: "dead", Uptime: 50},
			{Date: "2026-09-03", Status: "operational", Uptime: 100},
		},
		"idle": {
			{Date: "2026-09-01", Status: "unknown", Uptime: 0},
		},
	}
	build := func(ids ...string) core.UptimeData {
		h := NewHandler(&auditTimelineRepo{mockRepository: &mockRepository{}, history: history})
		souls := make([]core.SoulStatusInfo, 0, len(ids))
		for _, id := range ids {
			souls = append(souls, core.SoulStatusInfo{ID: id})
		}
		return h.buildUptimeData(&core.StatusPage{UptimeDays: 3}, souls)
	}
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

	// Control: a soul whose days all carry judgments averages normally.
	if got := build("old"); !near(got.Overall, 250.0/3) {
		t.Fatalf("control: Overall = %v, want %v", got.Overall, 250.0/3)
	}
	// Only the judged day counts for a new monitor; the timeline keeps all days.
	got := build("new")
	if !near(got.Overall, 100) {
		t.Errorf("new monitor: Overall = %v, want 100 (unknown days are not downtime)", got.Overall)
	}
	if len(got.Days) != 3 {
		t.Errorf("timeline days = %d, want all 3 kept", len(got.Days))
	}
	// Mixed souls: 100, 100, 50, 100 over the four judged days.
	if got := build("new", "old"); !near(got.Overall, 87.5) {
		t.Errorf("mixed: Overall = %v, want 87.5", got.Overall)
	}
	// Nothing judged at all: no data, not a measured 0%.
	if got := build("idle"); got.Overall != 0 {
		t.Errorf("idle: Overall = %v, want 0 with no judged days", got.Overall)
	}
}
