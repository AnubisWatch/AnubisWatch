package storage

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/sla"
)

// slaFixture stores a soul's judgments every 13 minutes for the last 80
// hours: raw only before liveSince (50 hours ago), raw plus live rollups
// after it. Every fifth result is a failure.
func slaFixture(t *testing.T) (*CobaltDB, []*core.Judgment, time.Time) {
	t.Helper()
	db := newTestDB(t)
	t.Cleanup(func() { db.Close() })
	uptimeSoul(t, db)
	ts := NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger())
	now := time.Now()
	liveSince := now.Add(-50 * time.Hour)
	var all []*core.Judgment
	i := 0
	for at := now.Add(-80 * time.Hour); at.Before(now); at = at.Add(13 * time.Minute) {
		status := core.SoulAlive
		if i%5 == 0 {
			status = core.SoulDead
		}
		j := &core.Judgment{
			ID: fmt.Sprintf("j-%d", i), SoulID: "up", WorkspaceID: "default", Status: status,
			Duration: time.Duration(15+i%9*11) * time.Millisecond, Timestamp: at,
		}
		save := db.SaveJudgment
		if !at.Before(liveSince) {
			if i > 0 && all[len(all)-1].Timestamp.Before(liveSince) {
				setLiveSince(t, db, liveSince)
			}
			save = ts.SaveJudgment
		}
		if err := save(context.Background(), j); err != nil {
			t.Fatalf("save: %v", err)
		}
		all = append(all, j)
		i++
	}
	return db, all, now
}

// rawTally is the reference: every judgment of the period counted raw.
func rawTally(all []*core.Judgment, from, to time.Time, excluded []core.TimeRange) *sla.Tally {
	var tally sla.Tally
	tally.CountJudgments(sla.RawPieces(from, to, sla.Merge(excluded, from, to)), all)
	return &tally
}

func TestCollectSLAMatchesRawJudgmentsOnOverlappingRanges(t *testing.T) {
	db, all, now := slaFixture(t)
	hour := now.Truncate(time.Hour)
	cases := []struct {
		name     string
		from, to time.Time
		excluded []core.TimeRange
	}{
		{"live hours, ragged edges", now.Add(-30*time.Hour + 13*time.Minute), now.Add(-2*time.Hour - 4*time.Minute), nil},
		{"aligned live hours", hour.Add(-20 * time.Hour), hour.Add(-3 * time.Hour), nil},
		{"spans the start of live counting", now.Add(-70 * time.Hour), now, nil},
		{"overlaps the previous range", now.Add(-40 * time.Hour), now.Add(-10 * time.Hour), nil},
		{"with maintenance", now.Add(-40 * time.Hour), now, []core.TimeRange{
			{Start: hour.Add(-12*time.Hour - 20*time.Minute), End: hour.Add(-9 * time.Hour)},
			{Start: hour.Add(-30 * time.Hour), End: hour.Add(-29 * time.Hour)},
		}},
		{"sub-hour", now.Add(-5*time.Hour + 5*time.Minute), now.Add(-5*time.Hour + 50*time.Minute), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := db.CollectSLA(context.Background(), "default", "up", tc.from, tc.to, tc.excluded, SLAHorizons{})
			if err != nil {
				t.Fatalf("CollectSLA: %v", err)
			}
			want := rawTally(all, tc.from, tc.to, tc.excluded)
			if got.Checks != want.Checks || got.Up != want.Up {
				t.Fatalf("checks/up = %d/%d, raw %d/%d", got.Checks, got.Up, want.Checks, want.Up)
			}
			if math.Abs(got.DowntimeSeconds-want.DowntimeSeconds) > 1e-6 || math.Abs(got.LatencySumMs-want.LatencySumMs) > 1e-6 {
				t.Fatalf("downtime %v / latency %v, raw %v / %v", got.DowntimeSeconds, got.LatencySumMs, want.DowntimeSeconds, want.LatencySumMs)
			}
			if tc.to.Sub(tc.from) > 3*time.Hour && got.Sources.Hour == 0 {
				t.Fatalf("no hourly rollup read: %+v", got.Sources)
			}
		})
	}
}

func TestCollectSLADoesNotScanRawForWholeLiveHours(t *testing.T) {
	db, all, now := slaFixture(t)
	from, to := now.Truncate(time.Hour).Add(-20*time.Hour), now.Truncate(time.Hour).Add(-2*time.Hour)
	want := rawTally(all, from, to, nil)
	// Purge the raw judgments of the period: the rollups must still answer.
	for _, j := range all {
		if !j.Timestamp.Before(from) && j.Timestamp.Before(to) {
			if err := db.DeletePrefix(fmt.Sprintf("default/judgments/up/%d", j.Timestamp.UnixNano())); err != nil {
				t.Fatal(err)
			}
		}
	}
	got, err := db.CollectSLA(context.Background(), "default", "up", from, to, nil, SLAHorizons{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Checks != want.Checks || got.Up != want.Up || got.Sources.Raw != 0 || got.Sources.Hour != 18 {
		t.Fatalf("got %d/%d sources %+v, want %d/%d from 18 hours", got.Checks, got.Up, got.Sources, want.Checks, want.Up)
	}
}

func TestCollectSLAUsesDailyRollupsPastHourlyRetention(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	uptimeSoul(t, db)
	ts := NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger())
	day := utcDay(5)
	setLiveSince(t, db, day.Add(-time.Hour))
	for i := 0; i < 3*24*6; i++ { // every 10 minutes for three days
		status := core.SoulAlive
		if i%6 == 0 {
			status = core.SoulDead
		}
		j := &core.Judgment{ID: fmt.Sprintf("d-%d", i), SoulID: "up", WorkspaceID: "default", Status: status,
			Duration: 20 * time.Millisecond, Timestamp: day.Add(time.Duration(i) * 10 * time.Minute)}
		if err := ts.SaveJudgment(context.Background(), j); err != nil {
			t.Fatal(err)
		}
	}
	// Hourly rollups older than "now" are gone, so whole days come from daily ones.
	got, err := db.CollectSLA(context.Background(), "default", "up", day, day.Add(3*24*time.Hour), nil, SLAHorizons{HourSince: day.Add(4 * 24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Checks != 432 || got.Up != 360 || got.Sources.Day != 3 || got.Sources.Hour != 0 || got.Sources.Raw != 0 {
		t.Fatalf("got %d/%d from %+v", got.Checks, got.Up, got.Sources)
	}
	// Uniform checks: the daily estimate equals the hourly one.
	if math.Abs(got.DowntimeSeconds-3*24*3600/6.0) > 1e-6 {
		t.Fatalf("downtime = %v", got.DowntimeSeconds)
	}
}

func TestCollectSLATreatsPurgedRawAsNoData(t *testing.T) {
	db, all, now := slaFixture(t)
	from, to := now.Add(-79*time.Hour), now.Add(-60*time.Hour) // before live counting: raw only
	rawSince := now.Add(-70 * time.Hour)
	got, err := db.CollectSLA(context.Background(), "default", "up", from, to, nil, SLAHorizons{RawSince: rawSince})
	if err != nil {
		t.Fatal(err)
	}
	want := rawTally(all, rawSince, to, nil)
	if got.Checks != want.Checks || got.Checks == 0 {
		t.Fatalf("checks = %d, want %d (only after the raw horizon)", got.Checks, want.Checks)
	}
	if empty, _ := db.CollectSLA(context.Background(), "default", "up", to, from, nil, SLAHorizons{}); empty.Checks != 0 {
		t.Fatal("inverted range counted checks")
	}
}

func TestStreamJudgmentsIsOrderedBoundedAndWorkspaceScoped(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	start := time.Now().Add(-5 * time.Hour).Truncate(time.Second)
	for i := 0; i < 50; i++ {
		for _, ws := range []string{"default", "ws2"} {
			j := &core.Judgment{ID: fmt.Sprintf("%s-%02d", ws, i), SoulID: "same-id", WorkspaceID: ws,
				Status: core.SoulAlive, Timestamp: start.Add(time.Duration(i) * 6 * time.Minute)}
			if err := db.SaveJudgment(context.Background(), j); err != nil {
				t.Fatal(err)
			}
		}
	}
	collect := func(from, to time.Time, limit int) ([]*core.Judgment, bool) {
		var got []*core.Judgment
		n, truncated, err := db.StreamJudgments(context.Background(), "default", "same-id", from, to, limit, func(j *core.Judgment) error {
			got = append(got, j)
			return nil
		})
		if err != nil || n != len(got) {
			t.Fatalf("stream: n=%d len=%d err=%v", n, len(got), err)
		}
		return got, truncated
	}

	cases := []struct {
		name      string
		from, to  time.Time
		limit     int
		want      int
		truncated bool
	}{
		{"everything", start, start.Add(6 * time.Hour), 0, 50, false},
		{"half-open range", start.Add(6 * time.Minute), start.Add(60 * time.Minute), 0, 9, false},
		{"limited", start, start.Add(6 * time.Hour), 7, 7, true},
		{"limit equal to rows", start, start.Add(6 * time.Hour), 50, 50, false},
		{"empty", start.Add(-2 * time.Hour), start, 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, truncated := collect(tc.from, tc.to, tc.limit)
			if len(got) != tc.want || truncated != tc.truncated {
				t.Fatalf("got %d rows truncated=%v, want %d/%v", len(got), truncated, tc.want, tc.truncated)
			}
			for i, j := range got {
				if j.WorkspaceID != "default" {
					t.Fatalf("row %d from workspace %q", i, j.WorkspaceID)
				}
				if i > 0 && !got[i-1].Timestamp.Before(j.Timestamp) {
					t.Fatalf("rows out of order at %d", i)
				}
			}
		})
	}

	stop := errors.New("stop")
	n, _, err := db.StreamJudgments(context.Background(), "default", "same-id", start, start.Add(6*time.Hour), 0, func(*core.Judgment) error {
		return stop
	})
	if !errors.Is(err, stop) || n != 0 {
		t.Fatalf("callback error: n=%d err=%v", n, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := db.StreamJudgments(ctx, "default", "same-id", start, start.Add(time.Hour), 0, func(*core.Judgment) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled stream: %v", err)
	}
}

// Regression (review proof): the day hourly retention cuts through used
// neither its daily rollup (not whole) nor the purged hourly or raw data, so
// up to a day of checks, failures included, vanished as "no data".
func TestCollectSLAKeepsTheDayHourlyRetentionCutsThrough(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	uptimeSoul(t, db)
	ts := NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger())
	day := utcDay(5)
	setLiveSince(t, db, day.Add(-time.Hour))
	var all []*core.Judgment
	for i := 0; i < 3*24*6; i++ {
		status := core.SoulAlive
		if i%6 == 0 || (i >= 150 && i < 170) { // a burst of failures in the purged hours of day 2
			status = core.SoulDead
		}
		j := &core.Judgment{ID: fmt.Sprintf("d-%d", i), SoulID: "up", WorkspaceID: "default", Status: status,
			Duration: 20 * time.Millisecond, Timestamp: day.Add(time.Duration(i) * 10 * time.Minute)}
		if err := ts.SaveJudgment(context.Background(), j); err != nil {
			t.Fatal(err)
		}
		all = append(all, j)
	}
	want := rawTally(all, day, day.Add(3*24*time.Hour), nil)
	for _, cut := range []time.Duration{36 * time.Hour, 24*time.Hour + 30*time.Minute, 47 * time.Hour} {
		t.Run(cut.String(), func(t *testing.T) {
			hourSince := day.Add(cut)
			got, err := db.CollectSLA(context.Background(), "default", "up", day, day.Add(3*24*time.Hour), nil,
				SLAHorizons{HourSince: hourSince, RawSince: hourSince.Add(24 * time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			if got.CoveredSeconds < 3*24*3600-1 || got.Checks != want.Checks || got.Up != want.Up {
				t.Fatalf("covered %.0fs, %d/%d checks up; want the whole period and %d/%d (sources %+v)",
					got.CoveredSeconds, got.Up, got.Checks, want.Up, want.Checks, got.Sources)
			}
			if got.Sources.Day != 2 {
				t.Fatalf("sources = %+v, want day 1 and the remainder of day 2 from daily rollups", got.Sources)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := db.CollectSLA(ctx, "default", "up", day, day.Add(3*24*time.Hour), nil, SLAHorizons{}); err == nil {
		t.Fatal("cancelled collection did not stop")
	}
}
