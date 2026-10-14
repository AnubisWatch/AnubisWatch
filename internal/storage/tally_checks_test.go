package storage

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TallyChecks counts a soul's checks per range from live hourly summaries,
// reading raw judgments only for partial hours and for time before live
// counting. The soul uptime endpoint used to decode a month of raw judgments
// on every request instead.

func tallyFixture(t *testing.T) (*CobaltDB, []*core.Judgment, time.Time) {
	t.Helper()
	db := newTestDB(t)
	t.Cleanup(func() { db.Close() })
	uptimeSoul(t, db)
	ts := NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger())
	now := time.Now()
	liveSince := now.Add(-40 * time.Hour)

	var all []*core.Judgment
	save := func(save func(context.Context, *core.Judgment) error, at time.Time, i int) {
		status := core.SoulAlive
		if i%4 == 0 {
			status = core.SoulDead
		}
		j := &core.Judgment{
			ID: fmt.Sprintf("j-%d", at.UnixNano()), SoulID: "up", WorkspaceID: "default",
			Status: status, Duration: time.Duration(20+i%7*13) * time.Millisecond, Timestamp: at,
		}
		if err := save(context.Background(), j); err != nil {
			t.Fatalf("save: %v", err)
		}
		all = append(all, j)
	}
	// Before live counting: raw judgments only.
	for i, at := 0, now.Add(-72*time.Hour); at.Before(liveSince); i, at = i+1, at.Add(37*time.Minute) {
		save(db.SaveJudgment, at, i)
	}
	setLiveSince(t, db, liveSince)
	// Live: raw judgments plus 1-minute, hourly and daily summaries.
	for i, at := 0, liveSince.Add(time.Minute); at.Before(now); i, at = i+1, at.Add(7*time.Minute) {
		save(ts.SaveJudgment, at, i)
	}
	return db, all, now
}

func bruteForce(all []*core.Judgment, r core.TimeRange) core.CheckTally {
	var c core.CheckTally
	for _, j := range all {
		if !j.Timestamp.Before(r.Start) && j.Timestamp.Before(r.End) {
			c.Checks++
			if j.Status == core.SoulAlive {
				c.Up++
			}
			c.LatencySumMs += float64(j.Duration) / float64(time.Millisecond)
		}
	}
	return c
}

func TestTallyChecksMatchesRawJudgments(t *testing.T) {
	db, all, now := tallyFixture(t)
	hour := now.Truncate(time.Hour)
	end := now.Add(time.Nanosecond)

	ranges := []core.TimeRange{
		{Start: now.Add(-24 * time.Hour), End: end},                                                    // live, unaligned edges
		{Start: now.Add(-60 * time.Hour), End: end},                                                    // crosses the start of live counting
		{Start: now.Add(-70 * time.Hour), End: now.Add(-50 * time.Hour)},                               // entirely before live counting
		{Start: hour.Add(-10 * time.Hour), End: hour.Add(-4 * time.Hour)},                              // whole hours only
		{Start: hour.Add(-9*time.Hour + 30*time.Minute), End: hour.Add(-2*time.Hour + 30*time.Minute)}, // half-hour zone
		{Start: hour.Add(-3 * time.Hour), End: hour.Add(-3 * time.Hour)},                               // empty
	}
	got, err := db.TallyChecks("up", ranges)
	if err != nil {
		t.Fatalf("TallyChecks: %v", err)
	}
	for i, r := range ranges {
		want := bruteForce(all, r)
		g := got[i]
		if g.Checks != want.Checks || g.Up != want.Up || math.Abs(g.LatencySumMs-want.LatencySumMs) > 1e-6*math.Max(1, want.LatencySumMs) {
			t.Errorf("range %d [%s, %s): got %+v, want %+v", i, r.Start.Format(time.Kitchen), r.End.Format(time.Kitchen), g, want)
		}
	}
	if got[1].Checks == 0 || got[2].Checks == 0 {
		t.Fatal("fixture produced no checks in the ranges that cross or precede live counting")
	}
}

func TestTallyChecksReadsHourlySummariesForLiveHours(t *testing.T) {
	db, all, now := tallyFixture(t)
	hour := now.Truncate(time.Hour)
	r := core.TimeRange{Start: hour.Add(-20 * time.Hour), End: hour.Add(-2 * time.Hour)}
	want := bruteForce(all, r)

	// Remove every raw judgment: whole live hours must still be counted.
	raw, err := db.PrefixScan("default/judgments/up/")
	if err != nil {
		t.Fatalf("PrefixScan: %v", err)
	}
	for key := range raw {
		if err := db.Delete(key); err != nil {
			t.Fatalf("Delete: %v", err)
		}
	}

	got, err := db.TallyChecks("up", []core.TimeRange{r})
	if err != nil {
		t.Fatalf("TallyChecks: %v", err)
	}
	if got[0].Checks != want.Checks || got[0].Up != want.Up || want.Checks == 0 {
		t.Fatalf("whole live hours with raw judgments deleted = %+v, want %+v from hourly summaries", got[0], want)
	}
}

// ListJudgments reads only the requested window for a soul; the bounds stay
// exclusive, and a zero start still means "from the beginning".
func TestListJudgmentsWindowBounds(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	uptimeSoul(t, db)
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	for i := 0; i < 5; i++ {
		saveVia(t, db.SaveJudgment, base.Add(time.Duration(i)*10*time.Minute), core.SoulAlive, 1)
	}
	ctx := core.ContextWithWorkspaceID(context.Background(), "default")

	got, err := db.ListJudgments(ctx, "up", base.Add(10*time.Minute), base.Add(30*time.Minute), 0)
	if err != nil || len(got) != 1 || !got[0].Timestamp.Equal(base.Add(20*time.Minute)) {
		t.Fatalf("window (10m, 30m) = %d judgments (err %v), want only the one at 20m", len(got), err)
	}
	all, err := db.ListJudgments(ctx, "up", time.Time{}, time.Now(), 0)
	if err != nil || len(all) != 5 {
		t.Fatalf("zero start = %d judgments (err %v), want all 5", len(all), err)
	}
}
