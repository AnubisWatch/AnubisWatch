package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/sla"
)

// SLAHorizons tell CollectSLA which data retention has already purged.
// A zero time means nothing has been purged at that resolution.
type SLAHorizons struct {
	// RawSince: raw judgments before it are gone.
	RawSince time.Time
	// HourSince: hourly rollups before it are gone; whole UTC days before it
	// are read from daily rollups instead.
	HourSince time.Time
}

// rawBatch bounds one raw scan: CollectSLA decodes at most a day of a soul's
// raw judgments at a time.
const rawBatch = 24 * time.Hour

// CollectSLA tallies a soul's checks in [start, end), leaving out the merged
// excluded ranges (maintenance).
//
// Whole UTC hours that are not excluded and were counted live into hourly
// rollups (see liveSinceKey) are read from those rollups, and whole UTC days
// whose hourly rollups retention has purged from daily rollups, so a long
// period never scans raw judgments. Raw judgments are read only for hours cut
// by the period's edges or by an exclusion, and for time before live
// counting began; once raw retention has purged them that time counts as no
// data.
//
// The day hourly retention cuts through is read as its daily rollup minus
// the surviving hourly rollups (the purged hours as one bucket), so no time
// is lost at the boundary. ctx cancels a long collection between buckets.
func (db *CobaltDB) CollectSLA(ctx context.Context, workspaceID, soulID string, start, end time.Time, excluded []core.TimeRange, horizons SLAHorizons) (*sla.Tally, error) {
	workspaceID = defaultWorkspace(workspaceID)
	tally := &sla.Tally{}
	if !start.Before(end) {
		return tally, nil
	}
	excluded = sla.Merge(excluded, start, end)

	var liveHour, liveDay time.Time
	if raw, err := db.Get(liveSinceKey(workspaceID, soulID)); err == nil {
		if t, err := time.Parse(time.RFC3339Nano, string(raw)); err == nil {
			liveHour = ceilTo(t, Resolution1Hour)
			liveDay = ceilTo(t, Resolution1Day)
		}
	}
	hourSince := ceilTo(horizons.HourSince, Resolution1Hour)

	var hourly, daily map[int64]*JudgmentSummary
	var err error
	if !liveHour.IsZero() {
		from := maxTime(start, liveHour, hourSince)
		if from.Before(end) {
			if hourly, err = db.summariesByBucket(workspaceID, soulID, Resolution1Hour, from, end); err != nil {
				return nil, err
			}
		}
		if !horizons.HourSince.IsZero() && maxTime(start, liveDay).Before(hourSince) {
			if daily, err = db.summariesByBucket(workspaceID, soulID, Resolution1Day, maxTime(start, liveDay), hourSince); err != nil {
				return nil, err
			}
		}
	}

	addSummary := func(s *JudgmentSummary, span time.Duration, source string) {
		if s == nil || s.Count == 0 {
			return
		}
		tally.AddBucket(span, s.Count, s.SuccessCount, s.AvgLatency*float64(s.Count), source)
		tally.AddLatency(s.AvgLatency, s.Count, false)
	}

	// Raw stretches still to read, in order.
	var rawRanges []core.TimeRange
	addRaw := func(a, b time.Time) {
		if n := len(rawRanges); n > 0 && rawRanges[n-1].End.Equal(a) {
			rawRanges[n-1].End = b
			return
		}
		rawRanges = append(rawRanges, core.TimeRange{Start: a, End: b})
	}

	for n, cursor := 0, start; cursor.Before(end); n++ {
		if n%512 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if !liveHour.IsZero() {
			// A whole UTC day older than hourly retention, read from its daily rollup.
			day := truncateToResolution(cursor, Resolution1Day)
			dayEnd := day.Add(24 * time.Hour)
			whole := day.Equal(cursor) && !day.Before(liveDay) && !dayEnd.After(end) && !sla.Overlaps(excluded, day, dayEnd)
			if whole && !dayEnd.After(hourSince) {
				addSummary(daily[day.Unix()], 24*time.Hour, sla.SourceDay)
				cursor = dayEnd
				continue
			}
			// The day hourly retention cuts through: its purged hours are
			// the daily rollup minus the hourly rollups that survive.
			if whole && day.Before(hourSince) {
				addSummary(dailyRemainder(daily[day.Unix()], hourly, hourSince, dayEnd), hourSince.Sub(day), sla.SourceDay)
				cursor = hourSince
				continue
			}
		}
		hour := cursor.Truncate(time.Hour)
		next := hour.Add(time.Hour)
		if !liveHour.IsZero() && hour.Equal(cursor) && !hour.Before(liveHour) && !hour.Before(hourSince) &&
			!next.After(end) && !sla.Overlaps(excluded, hour, next) {
			addSummary(hourly[hour.Unix()], time.Hour, sla.SourceHour)
			cursor = next
			continue
		}
		if next.After(end) {
			next = end
		}
		addRaw(cursor, next)
		cursor = next
	}

	for _, r := range rawRanges {
		a := r.Start
		if !horizons.RawSince.IsZero() && a.Before(horizons.RawSince) {
			a = horizons.RawSince // purged: no data
		}
		for a.Before(r.End) {
			b := a.Add(rawBatch)
			if b.After(r.End) {
				b = r.End
			}
			pieces := sla.RawPieces(a, b, excluded)
			if len(pieces) > 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				judgments, err := db.QueryJudgments(ctx, workspaceID, soulID, a, b, 0)
				if err != nil {
					return nil, err
				}
				tally.CountJudgments(pieces, judgments)
			}
			a = b
		}
	}
	return tally, nil
}

// dailyRemainder is the part of a daily rollup not covered by the hourly
// rollups of [from, dayEnd), or nil when nothing is left.
func dailyRemainder(day *JudgmentSummary, hourly map[int64]*JudgmentSummary, from, dayEnd time.Time) *JudgmentSummary {
	if day == nil {
		return nil
	}
	rest := JudgmentSummary{Count: day.Count, SuccessCount: day.SuccessCount}
	latencySum := day.AvgLatency * float64(day.Count)
	for h := from; h.Before(dayEnd); h = h.Add(time.Hour) {
		if s := hourly[h.Unix()]; s != nil {
			rest.Count -= s.Count
			rest.SuccessCount -= s.SuccessCount
			latencySum -= s.AvgLatency * float64(s.Count)
		}
	}
	if rest.Count <= 0 {
		return nil
	}
	if rest.SuccessCount < 0 {
		rest.SuccessCount = 0
	}
	rest.AvgLatency = math.Max(latencySum/float64(rest.Count), 0)
	return &rest
}

// summariesByBucket reads a soul's rollups at res with bucket start in
// [from, to), keyed by bucket Unix time.
func (db *CobaltDB) summariesByBucket(workspaceID, soulID string, res TimeResolution, from, to time.Time) (map[int64]*JudgmentSummary, error) {
	prefix := fmt.Sprintf("%s/ts/%s/%s/", workspaceID, soulID, res)
	startKey := fmt.Sprintf("%s%d", prefix, truncateToResolution(from, res).Unix())
	endKey := fmt.Sprintf("%s%d", prefix, to.Unix())
	var results map[string][]byte
	var err error
	if len(startKey) == len(endKey) {
		results, err = db.RangeScan(startKey, endKey)
	} else {
		results, err = db.PrefixScan(prefix) // decimal widths differ: lexical order breaks
	}
	if err != nil {
		return nil, err
	}
	out := make(map[int64]*JudgmentSummary, len(results))
	for key, data := range results {
		unix, err := strconv.ParseInt(key[strings.LastIndex(key, "/")+1:], 10, 64)
		if err != nil || unix < from.Unix() || unix >= to.Unix() {
			continue
		}
		var s JudgmentSummary
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		out[unix] = &s
	}
	return out, nil
}

// ceilTo rounds t up to the next bucket boundary at res (t itself when it is
// one); the zero time stays zero.
func ceilTo(t time.Time, res TimeResolution) time.Time {
	if t.IsZero() {
		return t
	}
	b := truncateToResolution(t, res)
	if b.Before(t) {
		b = b.Add(resolutionSpan(res))
	}
	return b
}

func maxTime(ts ...time.Time) time.Time {
	var m time.Time
	for _, t := range ts {
		if t.After(m) {
			m = t
		}
	}
	return m
}

// exportChunk is the slice of time StreamJudgments reads at once, so an
// export holds at most one chunk of a soul's judgments in memory.
const exportChunk = time.Hour

// StreamJudgments calls fn with a soul's judgments in [start, end), oldest
// first, reading the raw store one hour at a time. It stops after limit
// judgments when limit > 0, or at the first error from fn, and returns how
// many it passed on and whether it stopped at the limit.
func (db *CobaltDB) StreamJudgments(ctx context.Context, workspaceID, soulID string, start, end time.Time, limit int, fn func(*core.Judgment) error) (int, bool, error) {
	workspaceID = defaultWorkspace(workspaceID)
	sent := 0
	for a := start; a.Before(end); {
		if err := ctx.Err(); err != nil {
			return sent, false, err
		}
		b := a.Truncate(exportChunk).Add(exportChunk)
		if b.After(end) {
			b = end
		}
		judgments, err := db.QueryJudgments(ctx, workspaceID, soulID, a, b, 0)
		if err != nil {
			return sent, false, err
		}
		// QueryJudgments returns newest first.
		sort.SliceStable(judgments, func(i, j int) bool {
			if judgments[i].Timestamp.Equal(judgments[j].Timestamp) {
				return judgments[i].ID < judgments[j].ID
			}
			return judgments[i].Timestamp.Before(judgments[j].Timestamp)
		})
		for _, j := range judgments {
			if limit > 0 && sent >= limit {
				return sent, true, nil
			}
			if err := fn(j); err != nil {
				return sent, false, err
			}
			sent++
		}
		a = b
	}
	return sent, false, nil
}
