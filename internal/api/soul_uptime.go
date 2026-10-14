package api

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// UptimeWindow summarizes a soul's checks over one period. UptimePercent is
// nil when there were no checks: "no data" must not read as 100% or 0%.
type UptimeWindow struct {
	Checks        int      `json:"checks"`
	Up            int      `json:"up"`
	Failures      int      `json:"failures"`
	UptimePercent *float64 `json:"uptime_percent"`
	AvgLatencyMs  float64  `json:"avg_latency_ms"`
}

// UptimePoint is one bucket of a soul's checks, starting at Start.
type UptimePoint struct {
	Start time.Time `json:"start"`
	UptimeWindow
}

// UptimeSeries splits a range into equal buckets, oldest first.
type UptimeSeries struct {
	Range       string        `json:"range"`
	StepSeconds int           `json:"step_seconds"`
	Points      []UptimePoint `json:"points"`
}

// SoulUptimeSummary is the response of GET /api/v1/souls/:id/uptime.
type SoulUptimeSummary struct {
	SoulID  string                  `json:"soul_id"`
	Windows map[string]UptimeWindow `json:"windows"` // "24h", "7d", "30d"
	Series  UptimeSeries            `json:"series"`
}

// uptimeSeriesLayout returns the first bucket start, the bucket length and
// the bucket count for a range, aligned to zone: 5-minute buckets for 1h,
// hours for 24h, calendar days for 7d and 30d. zone is a fixed offset, so
// every bucket of a range has the same length.
func uptimeSeriesLayout(rangeName string, now time.Time, zone *time.Location) (time.Time, time.Duration, int, bool) {
	l := now.In(zone)
	switch rangeName {
	case "1h":
		last := time.Date(l.Year(), l.Month(), l.Day(), l.Hour(), l.Minute()/5*5, 0, 0, zone)
		return last.Add(-11 * 5 * time.Minute), 5 * time.Minute, 12, true
	case "24h":
		last := time.Date(l.Year(), l.Month(), l.Day(), l.Hour(), 0, 0, 0, zone)
		return last.Add(-23 * time.Hour), time.Hour, 24, true
	case "7d", "30d":
		n := 7
		if rangeName == "30d" {
			n = 30
		}
		today := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, zone)
		return today.AddDate(0, 0, -(n - 1)), 24 * time.Hour, n, true
	}
	return time.Time{}, 0, 0, false
}

type uptimeTally struct {
	checks, up   int
	latencySumMs float64
}

func (t *uptimeTally) add(j *core.Judgment) {
	t.checks++
	// Only alive counts as up, matching the status page's uptime history.
	if j.Status == core.SoulAlive {
		t.up++
	}
	t.latencySumMs += float64(j.Duration) / float64(time.Millisecond)
}

func (t uptimeTally) window() UptimeWindow {
	w := UptimeWindow{Checks: t.checks, Up: t.up, Failures: t.checks - t.up}
	if t.checks > 0 {
		pct := math.Round(float64(t.up)/float64(t.checks)*10000) / 100
		w.UptimePercent = &pct
		w.AvgLatencyMs = math.Round(t.latencySumMs/float64(t.checks)*10) / 10
	}
	return w
}

// handleSoulUptime returns a soul's uptime over the last 24 hours, 7 days and
// 30 days, plus a series of equal buckets over the requested range (1h, 24h,
// 7d or 30d; default 7d). The dashboard used to derive these from the
// judgments endpoint, which returns at most 100 results of the last 24 hours,
// so its 7- and 30-day figures covered at most a day.
//
// Buckets are aligned to the caller's time zone, given as tz_offset_minutes
// (east of UTC, e.g. 180 for UTC+3); it defaults to UTC.
func (s *RESTServer) handleSoulUptime(ctx *Context) error {
	soulID := ctx.Params["id"]
	soul, err := s.store.GetSoulNoCtx(soulID)
	if err != nil || soul == nil {
		return ctx.Error(http.StatusNotFound, "soul not found")
	}
	if soul.WorkspaceID != ctx.Workspace {
		return ctx.Error(http.StatusForbidden, "access denied: soul belongs to another workspace")
	}

	offsetMinutes := 0
	if raw := ctx.Request.URL.Query().Get("tz_offset_minutes"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < -14*60 || n > 14*60 {
			return ctx.Error(http.StatusBadRequest, "tz_offset_minutes must be an integer between -840 and 840")
		}
		offsetMinutes = n
	}
	zone := time.FixedZone("", offsetMinutes*60)

	rangeName := ctx.Request.URL.Query().Get("range")
	if rangeName == "" {
		rangeName = "7d"
	}
	now := time.Now()
	first, step, buckets, ok := uptimeSeriesLayout(rangeName, now, zone)
	if !ok {
		return ctx.Error(http.StatusBadRequest, "range must be one of 1h, 24h, 7d, 30d")
	}

	// One range per window, then one per series bucket. End is just after
	// now so a check stamped this instant still counts.
	end := now.Add(time.Nanosecond)
	ranges := []core.TimeRange{
		{Start: now.Add(-24 * time.Hour), End: end},
		{Start: now.Add(-7 * 24 * time.Hour), End: end},
		{Start: now.Add(-30 * 24 * time.Hour), End: end},
	}
	for i := 0; i < buckets; i++ {
		ranges = append(ranges, core.TimeRange{
			Start: first.Add(time.Duration(i) * step),
			End:   first.Add(time.Duration(i+1) * step),
		})
	}
	tallies, err := s.tallyChecks(soulID, ranges)
	if err != nil {
		return s.internalError(ctx, err, "failed to load judgments")
	}
	last24h, last7d, last30d, series := tallies[0], tallies[1], tallies[2], tallies[3:]

	summary := SoulUptimeSummary{
		SoulID: soulID,
		Windows: map[string]UptimeWindow{
			"24h": last24h.window(),
			"7d":  last7d.window(),
			"30d": last30d.window(),
		},
		Series: UptimeSeries{
			Range:       rangeName,
			StepSeconds: int(step / time.Second),
			Points:      make([]UptimePoint, buckets),
		},
	}
	for i := range series {
		summary.Series.Points[i] = UptimePoint{
			Start:        first.Add(time.Duration(i) * step),
			UptimeWindow: series[i].window(),
		}
	}
	return ctx.JSON(http.StatusOK, summary)
}

// checkTallier is implemented by storage that can count checks from its
// hourly summaries (storage.CobaltDB.TallyChecks), reading raw judgments
// only for partial hours. Without it the handler counts raw judgments.
type checkTallier interface {
	TallyChecks(soulID string, ranges []core.TimeRange) ([]core.CheckTally, error)
}

// tallyChecks counts the soul's checks in each range. Reading 30 days of raw
// judgments decoded every check of that month on each dashboard request
// (about 86,000 for a soul checked every 30 seconds), so summaries are used
// when the store has them.
func (s *RESTServer) tallyChecks(soulID string, ranges []core.TimeRange) ([]uptimeTally, error) {
	out := make([]uptimeTally, len(ranges))
	if t, ok := s.store.(checkTallier); ok {
		counted, err := t.TallyChecks(soulID, ranges)
		if err != nil {
			return nil, err
		}
		for i, c := range counted {
			out[i] = uptimeTally{checks: c.Checks, up: c.Up, latencySumMs: c.LatencySumMs}
		}
		return out, nil
	}

	from, to := ranges[0].Start, ranges[0].End
	for _, r := range ranges[1:] {
		if r.Start.Before(from) {
			from = r.Start
		}
		if r.End.After(to) {
			to = r.End
		}
	}
	// ListJudgmentsNoCtx excludes its start bound; ranges include theirs.
	judgments, err := s.store.ListJudgmentsNoCtx(soulID, from.Add(-time.Nanosecond), to, 0)
	if err != nil {
		return nil, err
	}
	for _, j := range judgments {
		if j == nil {
			continue
		}
		for i, r := range ranges {
			if !j.Timestamp.Before(r.Start) && j.Timestamp.Before(r.End) {
				out[i].add(j)
			}
		}
	}
	return out, nil
}
