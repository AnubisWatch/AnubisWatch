package sla

import (
	"math"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Latency holds latency percentiles over a period, in milliseconds, within
// RelativeError (1%) of the true value (log-linear histogram). Exact is
// false when any part came from rollups, whose per-bucket average stands in
// for every check of the bucket.
type Latency struct {
	AvgMs float64 `json:"avg_ms"`
	P50Ms float64 `json:"p50_ms"`
	P95Ms float64 `json:"p95_ms"`
	P99Ms float64 `json:"p99_ms"`
	Exact bool    `json:"exact"`
}

// ErrorBudget compares downtime with what an SLO target allows over the
// monitored (non-excluded) time.
type ErrorBudget struct {
	TargetPercent            float64 `json:"target_percent"`
	AllowedDowntimeSeconds   float64 `json:"allowed_downtime_seconds"`
	ConsumedDowntimeSeconds  float64 `json:"consumed_downtime_seconds"`
	RemainingDowntimeSeconds float64 `json:"remaining_downtime_seconds"`
	// RemainingPercent is the share of the budget left; negative once it is
	// overspent, null when the target allows no downtime at all.
	RemainingPercent *float64 `json:"remaining_percent"`
	Met              bool     `json:"met"`
}

// Report is the SLA of one soul, or of a workspace when SoulID is empty.
// Workspace durations are summed over souls (soul-seconds).
type Report struct {
	SoulID   string `json:"soul_id,omitempty"`
	SoulName string `json:"soul_name,omitempty"`
	SoulType string `json:"soul_type,omitempty"`

	PeriodSeconds    float64 `json:"period_seconds"`
	ExcludedSeconds  float64 `json:"excluded_seconds"`
	MonitoredSeconds float64 `json:"monitored_seconds"`
	// NoDataSeconds is monitored time without any recorded check.
	NoDataSeconds float64 `json:"no_data_seconds"`

	Checks int `json:"checks"`
	Failed int `json:"failed"`
	// AvailabilityPercent is null when there were no checks.
	AvailabilityPercent *float64 `json:"availability_percent"`
	DowntimeSeconds     float64  `json:"downtime_seconds"`

	Incidents   int      `json:"incidents"`
	MTTRSeconds *float64 `json:"mttr_seconds"`

	Latency     *Latency     `json:"latency"`
	ErrorBudget *ErrorBudget `json:"error_budget,omitempty"`
	Sources     Sources      `json:"sources"`
}

func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}

// Build turns a tally over a period of periodSeconds, of which
// excludedSeconds were maintenance, into a report. target is an SLO in
// percent (e.g. 99.9) or nil.
func Build(t *Tally, periodSeconds, excludedSeconds float64, target *float64) Report {
	if t == nil {
		t = &Tally{}
	}
	monitored := math.Max(periodSeconds-excludedSeconds, 0)
	r := Report{
		PeriodSeconds:    round(periodSeconds, 3),
		ExcludedSeconds:  round(excludedSeconds, 3),
		MonitoredSeconds: round(monitored, 3),
		NoDataSeconds:    round(math.Max(monitored-t.CoveredSeconds, 0), 3),
		Checks:           t.Checks,
		Failed:           t.Checks - t.Up,
		DowntimeSeconds:  round(t.DowntimeSeconds, 3),
		Sources:          t.Sources,
	}
	if t.Checks > 0 {
		a := round(float64(t.Up)/float64(t.Checks)*100, 4)
		r.AvailabilityPercent = &a
	}
	if p, ok := t.percentiles(0.50, 0.95, 0.99); ok {
		avg := 0.0
		if t.Checks > 0 {
			avg = t.LatencySumMs / float64(t.Checks)
		}
		r.Latency = &Latency{
			AvgMs: round(avg, 2), P50Ms: round(p[0], 2), P95Ms: round(p[1], 2), P99Ms: round(p[2], 2),
			Exact: !t.approximate,
		}
	}
	if target != nil {
		r.ErrorBudget = budget(*target, monitored, t.DowntimeSeconds)
	}
	return r
}

func budget(target, monitoredSeconds, downtimeSeconds float64) *ErrorBudget {
	allowed := monitoredSeconds * (100 - target) / 100
	b := &ErrorBudget{
		TargetPercent:            target,
		AllowedDowntimeSeconds:   round(allowed, 3),
		ConsumedDowntimeSeconds:  round(downtimeSeconds, 3),
		RemainingDowntimeSeconds: round(allowed-downtimeSeconds, 3),
		Met:                      downtimeSeconds <= allowed,
	}
	if allowed > 0 {
		pct := round((allowed-downtimeSeconds)/allowed*100, 2)
		b.RemainingPercent = &pct
	}
	return b
}

// ApplyBudget sets (or clears) the error budget of a report built earlier,
// e.g. a workspace total assembled from soul reports.
func ApplyBudget(r *Report, target *float64) {
	r.ErrorBudget = nil
	if target != nil {
		r.ErrorBudget = budget(*target, r.MonitoredSeconds, r.DowntimeSeconds)
	}
}

// IncidentStats counts the incidents that started in [from, to) and their
// mean time to resolve over those resolved. include selects the incidents
// that belong to the report (workspace and soul).
func IncidentStats(incidents []*core.Incident, from, to time.Time, include func(*core.Incident) bool) (int, *float64) {
	count, resolved := 0, 0
	var total time.Duration
	for _, inc := range incidents {
		if inc == nil || inc.StartedAt.Before(from) || !inc.StartedAt.Before(to) {
			continue
		}
		if include != nil && !include(inc) {
			continue
		}
		count++
		if inc.ResolvedAt != nil && !inc.ResolvedAt.Before(inc.StartedAt) {
			resolved++
			total += inc.ResolvedAt.Sub(inc.StartedAt)
		}
	}
	if resolved == 0 {
		return count, nil
	}
	mttr := round(total.Seconds()/float64(resolved), 3)
	return count, &mttr
}

// SetIncidents records incident figures on a report.
func (r *Report) SetIncidents(count int, mttrSeconds *float64) {
	r.Incidents = count
	r.MTTRSeconds = mttrSeconds
}
