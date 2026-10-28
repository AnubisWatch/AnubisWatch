package sla

import (
	"math"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

var base = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func at(d time.Duration) time.Time { return base.Add(d) }

func rng(a, b time.Duration) core.TimeRange { return core.TimeRange{Start: at(a), End: at(b)} }

func judgment(offset time.Duration, status core.SoulStatus, latency time.Duration) *core.Judgment {
	return &core.Judgment{Timestamp: at(offset), Status: status, Duration: latency}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// everyMinute returns one judgment per minute in [from, to), dead where dead
// says so.
func everyMinute(from, to time.Duration, dead func(i int) bool) []*core.Judgment {
	var out []*core.Judgment
	for i, d := 0, from; d < to; i, d = i+1, d+time.Minute {
		status := core.SoulAlive
		if dead != nil && dead(i) {
			status = core.SoulDead
		}
		out = append(out, judgment(d, status, time.Duration(10+i%10)*time.Millisecond))
	}
	return out
}

func TestReportMath(t *testing.T) {
	ptr := func(v float64) *float64 { return &v }
	cases := []struct {
		name        string
		from, to    time.Duration
		excluded    []core.TimeRange
		judgments   []*core.Judgment
		target      *float64
		checks      int
		failed      int
		avail       *float64
		downtime    float64
		excludedSec float64
		noData      float64
		budgetLeft  *float64
		met         bool
	}{
		{
			name: "empty period has no availability and no downtime",
			from: 0, to: 2 * time.Hour, target: ptr(99.9),
			noData: 7200, budgetLeft: ptr(100), met: true,
		},
		{
			name: "all up",
			from: 0, to: time.Hour, judgments: everyMinute(0, time.Hour, nil),
			checks: 60, avail: ptr(100),
		},
		{
			name: "six of sixty failed in one hour is six minutes down",
			from: 0, to: time.Hour, judgments: everyMinute(0, time.Hour, func(i int) bool { return i < 6 }),
			target: ptr(99), checks: 60, failed: 6, avail: ptr(90), downtime: 360,
			budgetLeft: ptr(-900), met: false,
		},
		{
			name: "maintenance cuts out time and the checks inside it",
			from: 0, to: 2 * time.Hour,
			excluded: []core.TimeRange{rng(30*time.Minute, 90*time.Minute)},
			// Everything inside maintenance failed; none of it counts.
			judgments: everyMinute(0, 2*time.Hour, func(i int) bool { return i >= 30 && i < 90 }),
			target:    ptr(99.9), checks: 60, avail: ptr(100), excludedSec: 3600,
			budgetLeft: ptr(100), met: true,
		},
		{
			name: "embalmed results are ignored",
			from: 0, to: time.Hour,
			judgments: append(everyMinute(0, 30*time.Minute, nil),
				judgment(40*time.Minute, core.SoulEmbalmed, time.Millisecond)),
			checks: 30, avail: ptr(100),
		},
		{
			name: "partial range counts only its own part of an hour",
			from: 15 * time.Minute, to: 45 * time.Minute,
			judgments: everyMinute(0, time.Hour, func(i int) bool { return i%2 == 0 }),
			checks:    30, failed: 15, avail: ptr(50), downtime: 900,
		},
		{
			name: "half the period without checks is no data, not downtime",
			from: 0, to: 2 * time.Hour, judgments: everyMinute(0, time.Hour, nil),
			checks: 60, avail: ptr(100), noData: 3600,
		},
		{
			name: "judgments outside the period are ignored",
			from: time.Hour, to: 2 * time.Hour, judgments: everyMinute(0, 3*time.Hour, func(i int) bool { return i < 60 || i >= 120 }),
			checks: 60, avail: ptr(100),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			from, to := at(tc.from), at(tc.to)
			excl := Merge(tc.excluded, from, to)
			var tally Tally
			tally.CountJudgments(RawPieces(from, to, excl), tc.judgments)
			r := Build(&tally, to.Sub(from).Seconds(), Total(excl).Seconds(), tc.target)

			if r.Checks != tc.checks || r.Failed != tc.failed {
				t.Fatalf("checks/failed = %d/%d, want %d/%d", r.Checks, r.Failed, tc.checks, tc.failed)
			}
			switch {
			case tc.avail == nil && r.AvailabilityPercent != nil:
				t.Fatalf("availability = %v, want null", *r.AvailabilityPercent)
			case tc.avail != nil && (r.AvailabilityPercent == nil || !near(*r.AvailabilityPercent, *tc.avail)):
				t.Fatalf("availability = %v, want %v", r.AvailabilityPercent, *tc.avail)
			}
			if !near(r.DowntimeSeconds, tc.downtime) {
				t.Fatalf("downtime = %v, want %v", r.DowntimeSeconds, tc.downtime)
			}
			if !near(r.ExcludedSeconds, tc.excludedSec) || !near(r.MonitoredSeconds, to.Sub(from).Seconds()-tc.excludedSec) {
				t.Fatalf("excluded/monitored = %v/%v", r.ExcludedSeconds, r.MonitoredSeconds)
			}
			if !near(r.NoDataSeconds, tc.noData) {
				t.Fatalf("no data = %v, want %v", r.NoDataSeconds, tc.noData)
			}
			if tc.target == nil {
				if r.ErrorBudget != nil {
					t.Fatal("error budget without a target")
				}
				return
			}
			b := r.ErrorBudget
			if b == nil || b.Met != tc.met || (tc.budgetLeft == nil) != (b.RemainingPercent == nil) ||
				(tc.budgetLeft != nil && !near(*b.RemainingPercent, *tc.budgetLeft)) {
				t.Fatalf("budget = %+v, want remaining %v met %v", b, tc.budgetLeft, tc.met)
			}
		})
	}
}

func TestErrorBudgetFigures(t *testing.T) {
	target := 99.9
	// 30 days with 10 minutes of downtime: 43.2 minutes allowed.
	r := Build(&Tally{Checks: 100, Up: 99, DowntimeSeconds: 600}, 30*24*3600, 0, &target)
	b := r.ErrorBudget
	if !near(b.AllowedDowntimeSeconds, 2592) || !near(b.RemainingDowntimeSeconds, 1992) || !b.Met {
		t.Fatalf("budget = %+v", b)
	}
	if !near(*b.RemainingPercent, 76.85) {
		t.Fatalf("remaining percent = %v", *b.RemainingPercent)
	}
	hundred := 100.0
	if b := Build(&Tally{}, 3600, 0, &hundred).ErrorBudget; b.RemainingPercent != nil || !b.Met {
		t.Fatalf("100%% target budget = %+v", b)
	}
	ApplyBudget(&r, nil)
	if r.ErrorBudget != nil {
		t.Fatal("ApplyBudget(nil) kept the budget")
	}
}

func TestLatencyPercentiles(t *testing.T) {
	var tally Tally
	var js []*core.Judgment
	for i := 1; i <= 100; i++ {
		js = append(js, judgment(time.Duration(i)*time.Second, core.SoulAlive, time.Duration(i)*time.Millisecond))
	}
	tally.CountJudgments(RawPieces(at(0), at(time.Hour), nil), js)
	l := Build(&tally, 3600, 0, nil).Latency
	within := func(got, want float64) bool { return math.Abs(got-want) <= want*RelativeError }
	if l == nil || !within(l.P50Ms, 50) || !within(l.P95Ms, 95) || !within(l.P99Ms, 99) || !near(l.AvgMs, 50.5) || !l.Exact {
		t.Fatalf("latency = %+v", l)
	}
	// A rollup average makes the figures approximate.
	tally.AddLatency(500, 10, false)
	if l := Build(&tally, 3600, 0, nil).Latency; l.Exact || !within(l.P99Ms, 500) {
		t.Fatalf("with rollup: %+v", l)
	}
	if Build(&Tally{}, 3600, 0, nil).Latency != nil {
		t.Fatal("latency without samples")
	}
}

// Regression (review proof): latency folding once went quadratic when more
// than 100k distinct values existed, so a long report pinned a CPU. The
// histogram keeps adds O(1) and merges O(buckets) whatever the spread.
func TestLatencyHistogramStaysCheapAndAccurate(t *testing.T) {
	var tally Tally
	start := time.Now()
	for i := 0; i < 300_000; i++ {
		tally.AddLatency(float64(i)/10, 1, true) // 0 .. 30 s, all distinct
	}
	var other Tally
	other.AddLatency(1e9, 5, true) // beyond the last bucket: clamped, not lost
	for i := 0; i < 2000; i++ {
		tally.Merge(&other)
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("300k adds and 2000 merges took %s", el)
	}
	if len(tally.latencies.counts) != histBuckets || tally.latencies.total != 310_000 {
		t.Fatalf("histogram holds %d buckets / %d samples", len(tally.latencies.counts), tally.latencies.total)
	}
	p, _ := tally.percentiles(0.5, 0.9)
	for k, want := range []float64{15500, 27900} {
		if math.Abs(p[k]-want) > want*RelativeError {
			t.Fatalf("p[%d] = %v, want %v ±1%%", k, p[k], want)
		}
	}
	if v := histValue(histIndex(0)); v != 0 {
		t.Fatalf("zero latency reported as %v", v)
	}
}

func TestMergeAndSubtract(t *testing.T) {
	merged := Merge([]core.TimeRange{
		rng(5*time.Hour, 6*time.Hour), rng(time.Hour, 2*time.Hour), rng(90*time.Minute, 3*time.Hour),
		rng(-time.Hour, 30*time.Minute), rng(3*time.Hour, 3*time.Hour), rng(7*time.Hour, 9*time.Hour),
	}, at(0), at(8*time.Hour))
	want := []core.TimeRange{rng(0, 30*time.Minute), rng(time.Hour, 3*time.Hour), rng(5*time.Hour, 6*time.Hour), rng(7*time.Hour, 8*time.Hour)}
	if len(merged) != len(want) {
		t.Fatalf("merged = %v", merged)
	}
	for i := range want {
		if merged[i] != want[i] {
			t.Fatalf("merged[%d] = %v, want %v", i, merged[i], want[i])
		}
	}
	if Total(merged) != 4*time.Hour+30*time.Minute {
		t.Fatalf("total = %v", Total(merged))
	}
	rest := Subtract(at(0), at(8*time.Hour), merged)
	if Total(rest) != 3*time.Hour+30*time.Minute || len(rest) != 3 {
		t.Fatalf("subtract = %v", rest)
	}
	if !Overlaps(merged, at(150*time.Minute), at(4*time.Hour)) || Overlaps(merged, at(3*time.Hour), at(5*time.Hour)) {
		t.Fatal("Overlaps wrong at the edges")
	}
	for _, p := range RawPieces(at(10*time.Minute), at(4*time.Hour+5*time.Minute), merged) {
		if p.Start.Truncate(time.Hour) != p.End.Add(-time.Nanosecond).Truncate(time.Hour) {
			t.Fatalf("piece %v crosses an hour", p)
		}
	}
}

func TestMaintenanceOccurrences(t *testing.T) {
	soul := &core.Soul{ID: "s1", WorkspaceID: "default"}
	window := func(recurring string, start time.Time, length time.Duration) *core.MaintenanceWindow {
		return &core.MaintenanceWindow{ID: "w", Enabled: true, WorkspaceID: "default", Recurring: recurring,
			StartTime: start, EndTime: start.Add(length)}
	}
	cases := []struct {
		name     string
		w        *core.MaintenanceWindow
		from, to time.Time
		count    int
		total    time.Duration
	}{
		{"one-off inside", window("", at(2*time.Hour), time.Hour), at(0), at(24 * time.Hour), 1, time.Hour},
		{"one-off clipped", window("", at(-time.Hour), 2*time.Hour), at(0), at(24 * time.Hour), 1, time.Hour},
		{"daily over a week", window("daily", at(time.Hour), 30*time.Minute), at(0), at(7 * 24 * time.Hour), 7, 7 * 30 * time.Minute},
		{"daily started long ago", window("daily", at(-400*24*time.Hour+time.Hour), 30*time.Minute), at(0), at(2 * 24 * time.Hour), 2, time.Hour},
		{"daily not before its first occurrence", window("daily", at(3*24*time.Hour), time.Hour), at(0), at(5 * 24 * time.Hour), 2, 2 * time.Hour},
		{"weekly over 30 days", window("weekly", at(0), time.Hour), at(0), at(30 * 24 * time.Hour), 5, 5 * time.Hour},
		{"monthly over a year", window("monthly", time.Date(2026, 1, 31, 22, 0, 0, 0, time.UTC), time.Hour),
			time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), 12, 12 * time.Hour},
		{"disabled", func() *core.MaintenanceWindow { w := window("", at(0), time.Hour); w.Enabled = false; return w }(), at(0), at(time.Hour), 0, 0},
		{"other workspace", func() *core.MaintenanceWindow { w := window("", at(0), time.Hour); w.WorkspaceID = "ws2"; return w }(), at(0), at(time.Hour), 0, 0},
		{"other soul", func() *core.MaintenanceWindow {
			w := window("", at(0), time.Hour)
			w.SoulIDs = []string{"s2"}
			return w
		}(), at(0), at(time.Hour), 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MaintenanceRanges([]*core.MaintenanceWindow{tc.w}, soul, tc.from, tc.to)
			if len(got) != tc.count || Total(got) != tc.total {
				t.Fatalf("got %d ranges totalling %v, want %d / %v: %v", len(got), Total(got), tc.count, tc.total, got)
			}
			// Every occurrence must be one IsActive agrees with.
			for _, r := range got {
				mid := r.Start.Add(r.End.Sub(r.Start) / 2)
				if !tc.w.IsActive(mid) {
					t.Fatalf("IsActive(%v) = false inside occurrence %v", mid, r)
				}
			}
		})
	}
}

func TestIncidentStats(t *testing.T) {
	resolved := func(d time.Duration) *time.Time { r := at(d); return &r }
	incidents := []*core.Incident{
		{SoulID: "a", WorkspaceID: "default", StartedAt: at(time.Hour), ResolvedAt: resolved(time.Hour + 10*time.Minute)},
		{SoulID: "a", WorkspaceID: "default", StartedAt: at(2 * time.Hour), ResolvedAt: resolved(2*time.Hour + 30*time.Minute)},
		{SoulID: "a", WorkspaceID: "default", StartedAt: at(3 * time.Hour)},                                // open
		{SoulID: "a", WorkspaceID: "default", StartedAt: at(-time.Hour), ResolvedAt: resolved(time.Hour)},  // before the period
		{SoulID: "b", WorkspaceID: "other", StartedAt: at(time.Hour), ResolvedAt: resolved(5 * time.Hour)}, // other workspace
		nil,
	}
	inDefault := func(i *core.Incident) bool { return i.WorkspaceID == "default" }
	n, mttr := IncidentStats(incidents, at(0), at(24*time.Hour), inDefault)
	if n != 3 || mttr == nil || *mttr != 1200 {
		t.Fatalf("got %d incidents, mttr %v; want 3 and 1200s", n, mttr)
	}
	if n, mttr := IncidentStats(incidents, at(10*time.Hour), at(11*time.Hour), inDefault); n != 0 || mttr != nil {
		t.Fatalf("empty period: %d, %v", n, mttr)
	}
}

func TestMergeTallies(t *testing.T) {
	var a, b, total Tally
	a.CountJudgments(RawPieces(at(0), at(time.Hour), nil), everyMinute(0, time.Hour, func(i int) bool { return i < 6 }))
	b.CountJudgments(RawPieces(at(0), at(time.Hour), nil), everyMinute(0, time.Hour, nil))
	total.Merge(&a)
	total.Merge(&b)
	total.Merge(nil)
	r := Build(&total, 7200, 0, nil)
	if r.Checks != 120 || r.Failed != 6 || *r.AvailabilityPercent != 95 || r.DowntimeSeconds != 360 || r.Sources.Raw != 2 {
		t.Fatalf("merged = %+v", r)
	}
}
