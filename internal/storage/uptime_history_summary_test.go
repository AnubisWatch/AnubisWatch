package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// GetUptimeHistory scanned and decoded every raw judgment of a soul on each
// status page render (twice: 30 days and the page's uptime_days). Days that
// started after the soul's results began being counted into live day
// summaries are now read from those summaries; only older days use raw data.

func uptimeSoul(t *testing.T, db *CobaltDB) {
	t.Helper()
	if err := db.SaveSoul(context.Background(), &core.Soul{ID: "up", Name: "up", WorkspaceID: "default", Type: core.CheckHTTP}); err != nil {
		t.Fatalf("SaveSoul: %v", err)
	}
}

func utcDay(daysAgo int) time.Time {
	n := time.Now().UTC().AddDate(0, 0, -daysAgo)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

func saveVia(t *testing.T, save func(context.Context, *core.Judgment) error, at time.Time, status core.SoulStatus, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		j := &core.Judgment{
			ID: fmt.Sprintf("j-%d-%d", at.UnixNano(), i), SoulID: "up", WorkspaceID: "default",
			Status: status, Timestamp: at.Add(time.Duration(i) * time.Minute),
		}
		if err := save(context.Background(), j); err != nil {
			t.Fatalf("save: %v", err)
		}
	}
}

func historyByDate(t *testing.T, db *CobaltDB, days int) map[string]core.UptimeDay {
	t.Helper()
	history, err := db.GetUptimeHistory("up", days)
	if err != nil {
		t.Fatalf("GetUptimeHistory: %v", err)
	}
	if len(history) != days {
		t.Fatalf("history has %d days, want %d", len(history), days)
	}
	out := make(map[string]core.UptimeDay, len(history))
	for _, d := range history {
		out[d.Date] = d
	}
	return out
}

func setLiveSince(t *testing.T, db *CobaltDB, at time.Time) {
	t.Helper()
	if err := db.Put(liveSinceKey("default", "up"), []byte(at.UTC().Format(time.RFC3339Nano))); err != nil {
		t.Fatalf("Put live-since: %v", err)
	}
}

func TestUptimeHistoryReadsLiveDaySummariesNotRawJudgments(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	uptimeSoul(t, db)
	ts := NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger())

	saveVia(t, ts.SaveJudgment, utcDay(2).Add(10*time.Hour), core.SoulAlive, 3)
	saveVia(t, ts.SaveJudgment, utcDay(2).Add(12*time.Hour), core.SoulDead, 1)
	setLiveSince(t, db, utcDay(10)) // counting started well before those days

	// Remove every raw judgment: the history must come from the day summary.
	raw, err := db.PrefixScan("default/judgments/up/")
	if err != nil {
		t.Fatalf("PrefixScan: %v", err)
	}
	for key := range raw {
		if err := db.Delete(key); err != nil {
			t.Fatalf("Delete: %v", err)
		}
	}

	day := historyByDate(t, db, 7)[utcDay(2).Format("2006-01-02")]
	if day.Uptime != 75 || day.Status != "dead" {
		t.Fatalf("day with 3 up of 4 = uptime %.1f status %q; want 75 from the day summary", day.Uptime, day.Status)
	}
}

func TestUptimeHistoryDoesNotScanRawForLiveDays(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	uptimeSoul(t, db)
	setLiveSince(t, db, utcDay(10))

	// A raw judgment written without a day summary is not consulted for a day
	// covered by live counting: proof the raw history is not scanned.
	saveVia(t, db.SaveJudgment, utcDay(2).Add(time.Hour), core.SoulAlive, 1)

	if day := historyByDate(t, db, 7)[utcDay(2).Format("2006-01-02")]; day.Status != "unknown" {
		t.Fatalf("live day read raw judgments: status %q, want unknown", day.Status)
	}
}

func TestUptimeHistoryUsesRawJudgmentsBeforeLiveCounting(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	uptimeSoul(t, db)
	ts := NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger())

	// History written before the upgrade exists only as raw judgments.
	saveVia(t, db.SaveJudgment, utcDay(3).Add(time.Hour), core.SoulAlive, 1)
	saveVia(t, db.SaveJudgment, utcDay(3).Add(2*time.Hour), core.SoulDead, 1)
	// Live counting starts now, mid-day: today is only partly summarized.
	saveVia(t, db.SaveJudgment, time.Now().Add(-time.Minute), core.SoulDead, 1)
	saveVia(t, ts.SaveJudgment, time.Now(), core.SoulAlive, 1)

	history := historyByDate(t, db, 7)
	if d := history[utcDay(3).Format("2006-01-02")]; d.Uptime != 50 {
		t.Errorf("pre-upgrade day = %.1f%% (%s), want 50 from raw judgments", d.Uptime, d.Status)
	}
	// Within two minutes of UTC midnight the earlier check lands on yesterday.
	if time.Since(utcDay(0)) < 2*time.Minute {
		return
	}
	if d := history[utcDay(0).Format("2006-01-02")]; d.Uptime != 50 {
		t.Errorf("today, partly before live counting = %.1f%%, want 50 (both checks, from raw)", d.Uptime)
	}
}

func TestDaySummariesAreUTCDays(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	ts := NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger())

	// 01:00 at UTC+5 is 20:00 UTC the day before.
	day := utcDay(4)
	at := day.Add(20 * time.Hour).In(time.FixedZone("", 5*3600))
	saveVia(t, ts.SaveJudgment, at, core.SoulAlive, 1)

	if _, err := db.Get(fmt.Sprintf("default/ts/up/1day/%d", day.Unix())); err != nil {
		t.Fatalf("no day summary under UTC day %s for a check at %s: %v", day.Format("2006-01-02"), at, err)
	}
}

func TestCompactionLeavesLiveDaySummaryCountIntact(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	ts := NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger())

	day := utcDay(5)
	saveVia(t, ts.SaveJudgment, day.Add(3*time.Hour), core.SoulAlive, 3)
	for _, step := range [][2]TimeResolution{{Resolution1Min, Resolution5Min}, {Resolution5Min, Resolution1Hour}, {Resolution1Hour, Resolution1Day}} {
		if err := ts.compactToResolution(step[0], step[1], time.Hour); err != nil {
			t.Fatalf("compact %s->%s: %v", step[0], step[1], err)
		}
	}

	got, err := ts.QuerySummaries(context.Background(), "default", "up", Resolution1Day, day, day.Add(time.Second))
	if err != nil || len(got) != 1 || got[0].Count != 3 {
		t.Fatalf("day summary after compaction = %v (err %v); want one with count 3", got, err)
	}
}
