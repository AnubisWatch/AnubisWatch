// Package sla computes service-level figures (availability, downtime,
// incidents, MTTR, latency percentiles and error budget) for a soul or a
// workspace over an arbitrary period.
//
// The figures are built from "buckets": a span of monitored time together
// with the checks that ran in it. A bucket is either one UTC hour or day read
// from the time-series rollups, or a piece of an hour counted from raw
// judgments. The same formulas apply to both, so a whole hour gives the same
// result whichever source it is read from:
//
//   - availability = checks that found the soul alive / all checks
//   - downtime     = bucket length x failed checks / checks
//
// Maintenance windows that apply to a soul are excluded time: they are cut
// out of the period before anything is counted, judgments recorded inside
// them are ignored, and so are judgments whose status is embalmed.
package sla

import (
	"sort"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Source names where a bucket's figures came from.
const (
	SourceRaw  = "raw"
	SourceHour = "1hour"
	SourceDay  = "1day"
)

// Sources counts the buckets read from each source.
type Sources struct {
	Raw  int `json:"raw"`
	Hour int `json:"hour"`
	Day  int `json:"day"`
}

// Tally accumulates buckets. The zero value is ready to use.
type Tally struct {
	Checks          int
	Up              int
	LatencySumMs    float64
	DowntimeSeconds float64
	// CoveredSeconds is monitored time that had at least one check.
	CoveredSeconds float64
	Sources        Sources

	latencies histogram
	// approximate is set once a latency comes from a rollup, whose
	// per-bucket average stands in for its checks.
	approximate bool
}

// AddBucket counts checks that ran during a bucket of length span.
func (t *Tally) AddBucket(span time.Duration, checks, up int, latencySumMs float64, source string) {
	if checks <= 0 {
		return
	}
	if up > checks {
		up = checks
	}
	if up < 0 {
		up = 0
	}
	t.Checks += checks
	t.Up += up
	t.LatencySumMs += latencySumMs
	seconds := span.Seconds()
	t.CoveredSeconds += seconds
	t.DowntimeSeconds += seconds * float64(checks-up) / float64(checks)
	switch source {
	case SourceRaw:
		t.Sources.Raw++
	case SourceHour:
		t.Sources.Hour++
	case SourceDay:
		t.Sources.Day++
	}
}

// AddLatency records count checks with latency valueMs. exact is false for
// a rollup's average.
func (t *Tally) AddLatency(valueMs float64, count int, exact bool) {
	if count <= 0 {
		return
	}
	if !exact {
		t.approximate = true
	}
	t.latencies.add(valueMs, count)
}

// Merge adds o's buckets and latencies to t.
func (t *Tally) Merge(o *Tally) {
	if o == nil {
		return
	}
	t.Checks += o.Checks
	t.Up += o.Up
	t.LatencySumMs += o.LatencySumMs
	t.DowntimeSeconds += o.DowntimeSeconds
	t.CoveredSeconds += o.CoveredSeconds
	t.Sources.Raw += o.Sources.Raw
	t.Sources.Hour += o.Sources.Hour
	t.Sources.Day += o.Sources.Day
	t.approximate = t.approximate || o.approximate
	t.latencies.merge(&o.latencies)
}

// percentiles returns the nearest-rank p-th percentiles of the recorded
// latencies (within RelativeError), ok false when there are none.
func (t *Tally) percentiles(ps ...float64) ([]float64, bool) {
	return t.latencies.quantiles(ps...)
}

// CountJudgments adds raw judgments to t. pieces must be sorted,
// non-overlapping, and each within one UTC hour (see RawPieces); every piece
// becomes one bucket. Judgments outside every piece, nil ones and embalmed
// ones are ignored.
func (t *Tally) CountJudgments(pieces []core.TimeRange, judgments []*core.Judgment) {
	type acc struct {
		checks, up int
		latSum     float64
	}
	accs := make([]acc, len(pieces))
	for _, j := range judgments {
		if j == nil || j.Status == core.SoulEmbalmed {
			continue
		}
		i := sort.Search(len(pieces), func(i int) bool { return pieces[i].End.After(j.Timestamp) })
		if i == len(pieces) || j.Timestamp.Before(pieces[i].Start) {
			continue
		}
		ms := float64(j.Duration) / float64(time.Millisecond)
		accs[i].checks++
		if j.Status == core.SoulAlive {
			accs[i].up++
		}
		accs[i].latSum += ms
		t.AddLatency(ms, 1, true)
	}
	for i, a := range accs {
		t.AddBucket(pieces[i].End.Sub(pieces[i].Start), a.checks, a.up, a.latSum, SourceRaw)
	}
}

// Merge sorts ranges, clips them to [from, to) and joins any that overlap or
// touch. Empty ranges are dropped.
func Merge(ranges []core.TimeRange, from, to time.Time) []core.TimeRange {
	clipped := make([]core.TimeRange, 0, len(ranges))
	for _, r := range ranges {
		if r.Start.Before(from) {
			r.Start = from
		}
		if r.End.After(to) {
			r.End = to
		}
		if r.Start.Before(r.End) {
			clipped = append(clipped, r)
		}
	}
	sort.Slice(clipped, func(i, j int) bool { return clipped[i].Start.Before(clipped[j].Start) })
	out := clipped[:0]
	for _, r := range clipped {
		if n := len(out); n > 0 && !r.Start.After(out[n-1].End) {
			if r.End.After(out[n-1].End) {
				out[n-1].End = r.End
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

// Total is the summed length of merged ranges.
func Total(merged []core.TimeRange) time.Duration {
	var d time.Duration
	for _, r := range merged {
		d += r.End.Sub(r.Start)
	}
	return d
}

// Overlaps reports whether [a, b) intersects any of the merged ranges.
func Overlaps(merged []core.TimeRange, a, b time.Time) bool {
	i := sort.Search(len(merged), func(i int) bool { return merged[i].End.After(a) })
	return i < len(merged) && merged[i].Start.Before(b)
}

// Subtract returns [a, b) minus the merged ranges, in order.
func Subtract(a, b time.Time, merged []core.TimeRange) []core.TimeRange {
	var out []core.TimeRange
	cursor := a
	i := sort.Search(len(merged), func(i int) bool { return merged[i].End.After(a) })
	for ; i < len(merged) && merged[i].Start.Before(b); i++ {
		if merged[i].Start.After(cursor) {
			out = append(out, core.TimeRange{Start: cursor, End: merged[i].Start})
		}
		if merged[i].End.After(cursor) {
			cursor = merged[i].End
		}
	}
	if cursor.Before(b) {
		out = append(out, core.TimeRange{Start: cursor, End: b})
	}
	return out
}

// RawPieces splits [a, b) minus the merged exclusions at UTC hour
// boundaries, the bucket shape CountJudgments expects.
func RawPieces(a, b time.Time, merged []core.TimeRange) []core.TimeRange {
	var out []core.TimeRange
	for _, r := range Subtract(a, b, merged) {
		for s := r.Start; s.Before(r.End); {
			e := s.Truncate(time.Hour).Add(time.Hour)
			if e.After(r.End) {
				e = r.End
			}
			out = append(out, core.TimeRange{Start: s, End: e})
			s = e
		}
	}
	return out
}

// MaintenanceRanges returns the time inside [from, to) covered by the enabled
// maintenance windows that apply to soul, merged. Recurring windows repeat
// with the length of their first occurrence, as MaintenanceWindow.IsActive
// does. Windows are taken as currently defined: a window deleted or disabled
// since no longer excludes the time it covered.
func MaintenanceRanges(windows []*core.MaintenanceWindow, soul *core.Soul, from, to time.Time) []core.TimeRange {
	var ranges []core.TimeRange
	for _, w := range windows {
		if w == nil || !w.Enabled || w.EndTime.Before(w.StartTime) || !w.AppliesTo(soul) {
			continue
		}
		ranges = append(ranges, Occurrences(w, from, to)...)
	}
	return Merge(ranges, from, to)
}

// maxOccurrences bounds one window's expansion; a daily window over five
// years has under 2,000.
const maxOccurrences = 10_000

// Occurrences lists the intervals of w that intersect [from, to). The end of
// an occurrence is inclusive in IsActive; at nanosecond resolution the
// difference is immaterial, so intervals here are half-open.
func Occurrences(w *core.MaintenanceWindow, from, to time.Time) []core.TimeRange {
	length := w.EndTime.Sub(w.StartTime)
	if length <= 0 {
		return nil
	}
	var out []core.TimeRange
	add := func(start time.Time) {
		end := start.Add(length)
		if start.Before(to) && end.After(from) {
			out = append(out, core.TimeRange{Start: start, End: end})
		}
	}
	switch w.Recurring {
	case "daily", "weekly":
		step := 1
		if w.Recurring == "weekly" {
			step = 7
		}
		// Skip whole periods before the range; AddDate keeps the wall clock
		// across DST changes in the window's location.
		k := 0
		if gap := from.Sub(w.StartTime) - length; gap > 0 {
			k = int(gap/(time.Duration(step)*24*time.Hour)) - 1
			if k < 0 {
				k = 0
			}
		}
		for n := 0; n < maxOccurrences; n, k = n+1, k+1 {
			start := w.StartTime.AddDate(0, 0, k*step)
			if !start.Before(to) {
				break
			}
			add(start)
		}
	case "monthly":
		loc := w.StartTime.Location()
		year, month := w.StartTime.Year(), w.StartTime.Month()
		if f := from.Add(-length).In(loc); f.After(w.StartTime) {
			year, month = f.Year(), f.Month()-1
		}
		for n := 0; n < maxOccurrences; n++ {
			start := monthlyStart(w.StartTime, year, month)
			month++
			if start.Before(w.StartTime) {
				continue
			}
			if !start.Before(to) {
				break
			}
			add(start)
		}
	default:
		add(w.StartTime)
	}
	return out
}

// monthlyStart mirrors MaintenanceWindow.monthlyStart: the window's day of
// month, clamped to the month's last day.
func monthlyStart(first time.Time, year int, month time.Month) time.Time {
	loc := first.Location()
	norm := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	year, month = norm.Year(), norm.Month()
	day := first.Day()
	if last := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day(); day > last {
		day = last
	}
	h, m, s := first.Clock()
	return time.Date(year, month, day, h, m, s, first.Nanosecond(), loc)
}
