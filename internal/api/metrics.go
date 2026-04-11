package api

import (
	"fmt"
	"math"
	"runtime"
	"sort"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// handleMetrics returns Prometheus-compatible metrics
func (s *RESTServer) handleMetrics(ctx *Context) error {
	// Sync verdict counters from alert manager stats
	if s.alert != nil {
		stats := s.alert.GetStats()
		s.metricsMu.Lock()
		s.verdictsFired = stats.SentAlerts + stats.FailedAlerts
		s.verdictsResolved = stats.ResolvedAlerts
		s.metricsMu.Unlock()
	}

	var output string

	output += s.buildSystemMetrics()
	output += s.buildSoulMetrics()
	output += s.buildJudgmentMetrics()
	output += s.buildClusterMetrics()
	output += s.buildAlertMetrics()
	output += s.buildLatencyMetrics()

	ctx.Response.Header().Set("Content-Type", "text/plain; version=0.0.4")
	ctx.Response.Write([]byte(output))
	return nil
}

// buildSystemMetrics returns system-level metrics
func (s *RESTServer) buildSystemMetrics() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	out := "# HELP anubis_build_info Build information\n"
	out += "# TYPE anubis_build_info gauge\n"
	out += "anubis_build_info{version=\"dev\"} 1\n"

	out += "# HELP anubis_memory_alloc_bytes Allocated memory in bytes\n"
	out += "# TYPE anubis_memory_alloc_bytes gauge\n"
	out += fmt.Sprintf("anubis_memory_alloc_bytes %d\n", m.Alloc)

	out += "# HELP anubis_memory_total_bytes Total memory allocated from OS\n"
	out += "# TYPE anubis_memory_total_bytes gauge\n"
	out += fmt.Sprintf("anubis_memory_total_bytes %d\n", m.TotalAlloc)

	out += "# HELP anubis_memory_sys_bytes System memory in bytes\n"
	out += "# TYPE anubis_memory_sys_bytes gauge\n"
	out += fmt.Sprintf("anubis_memory_sys_bytes %d\n", m.Sys)

	out += "# HELP anubis_goroutines Number of goroutines\n"
	out += "# TYPE anubis_goroutines gauge\n"
	out += fmt.Sprintf("anubis_goroutines %d\n", runtime.NumGoroutine())

	out += "# HELP anubis_gc_cycles_total Total number of GC cycles\n"
	out += "# TYPE anubis_gc_cycles_total counter\n"
	out += fmt.Sprintf("anubis_gc_cycles_total %d\n", m.NumGC)

	out += "# HELP anubis_judgments_total Total number of judgments executed since startup\n"
	out += "# TYPE anubis_judgments_total counter\n"
	s.metricsMu.RLock()
	out += fmt.Sprintf("anubis_judgments_total %d\n", s.judgmentsTotal)
	s.metricsMu.RUnlock()

	out += "# HELP anubis_verdicts_fired_total Total number of verdicts fired since startup\n"
	out += "# TYPE anubis_verdicts_fired_total counter\n"
	s.metricsMu.RLock()
	out += fmt.Sprintf("anubis_verdicts_fired_total %d\n", s.verdictsFired)
	s.metricsMu.RUnlock()

	out += "# HELP anubis_verdicts_resolved_total Total number of verdicts resolved since startup\n"
	out += "# TYPE anubis_verdicts_resolved_total counter\n"
	s.metricsMu.RLock()
	out += fmt.Sprintf("anubis_verdicts_resolved_total %d\n", s.verdictsResolved)
	s.metricsMu.RUnlock()

	// Verdicts by severity
	if s.alert != nil {
		stats := s.alert.GetStats()
		if len(stats.VerdictsBySeverity) > 0 {
			out += "# HELP anubis_verdicts_total Total number of verdicts by severity\n"
			out += "# TYPE anubis_verdicts_total counter\n"
			for severity, count := range stats.VerdictsBySeverity {
				out += fmt.Sprintf("anubis_verdicts_total{severity=\"%s\"} %d\n", severity, count)
			}
		}
	}

	return out
}

// buildSoulMetrics returns soul-related metrics
func (s *RESTServer) buildSoulMetrics() string {
	souls, err := s.store.ListSoulsNoCtx("default", 0, 10000)
	if err != nil {
		return "# anubis_souls_total 0\n"
	}

	out := "# HELP anubis_souls_total Total number of souls\n"
	out += "# TYPE anubis_souls_total gauge\n"
	out += fmt.Sprintf("anubis_souls_total %d\n", len(souls))

	// Status distribution (derived from latest judgments)
	out += "# HELP anubis_soul_status_count Number of souls by status\n"
	out += "# TYPE anubis_soul_status_count gauge\n"
	statusCounts := map[string]int{
		"alive":    0,
		"dead":     0,
		"degraded": 0,
		"unknown":  0,
		"embalmed": 0,
	}

	// Per-soul status and latency
	out += "# HELP anubis_soul_status Current soul status (0=dead, 1=alive, 2=degraded, 3=unknown, 4=embalmed)\n"
	out += "# TYPE anubis_soul_status gauge\n"
	out += "# HELP anubis_soul_latency_seconds Last check latency in seconds\n"
	out += "# TYPE anubis_soul_latency_seconds gauge\n"
	out += "# HELP anubis_soul_uptime_ratio Uptime ratio of the soul (0.0 to 1.0)\n"
	out += "# TYPE anubis_soul_uptime_ratio gauge\n"

	for _, soul := range souls {
		// Get recent judgments for latency, uptime, and status
		judgments, err := s.store.ListJudgmentsNoCtx(soul.ID, time.Now().Add(-24*time.Hour), time.Now(), 1000)
		if err != nil || len(judgments) == 0 {
			statusCounts["unknown"]++
			statusValue := 3 // unknown
			out += fmt.Sprintf("anubis_soul_status{soul=\"%s\"} %d\n", soul.Name, statusValue)
			out += fmt.Sprintf("anubis_soul_uptime_ratio{soul=\"%s\"} -1\n", soul.Name)
			continue
		}

		latest := judgments[0]

		// Status metric
		statusValue := statusNumeric(latest.Status)
		statusCounts[string(latest.Status)]++
		out += fmt.Sprintf("anubis_soul_status{soul=\"%s\"} %d\n", soul.Name, statusValue)

		// Latency metric
		latencySeconds := float64(latest.Duration) / float64(time.Second)
		out += fmt.Sprintf("anubis_soul_latency_seconds{soul=\"%s\"} %f\n", soul.Name, latencySeconds)

		// Uptime ratio: percentage of alive/degraded judgments in recent history
		uptimeRatio := computeUptimeRatio(judgments)
		out += fmt.Sprintf("anubis_soul_uptime_ratio{soul=\"%s\"} %.4f\n", soul.Name, uptimeRatio)
	}

	for status, count := range statusCounts {
		out += fmt.Sprintf("anubis_soul_status_count{status=\"%s\"} %d\n", status, count)
	}

	return out
}

// buildJudgmentMetrics returns judgment/health check metrics
func (s *RESTServer) buildJudgmentMetrics() string {
	souls, _ := s.store.ListSoulsNoCtx("default", 0, 10000)

	totalChecks := 0
	failedChecks := 0
	for _, soul := range souls {
		judgments, err := s.store.ListJudgmentsNoCtx(soul.ID, time.Now().Add(-24*time.Hour), time.Now(), 100)
		if err != nil {
			continue
		}
		totalChecks += len(judgments)
		for _, j := range judgments {
			if j.Status == core.SoulDead {
				failedChecks++
			}
		}
	}

	out := "# HELP anubis_judgments_in_24h Number of judgments in the last 24 hours\n"
	out += "# TYPE anubis_judgments_in_24h gauge\n"
	out += fmt.Sprintf("anubis_judgments_in_24h %d\n", totalChecks)

	out += "# HELP anubis_judgments_failed_in_24h Number of failed judgments in the last 24 hours\n"
	out += "# TYPE anubis_judgments_failed_in_24h gauge\n"
	out += fmt.Sprintf("anubis_judgments_failed_in_24h %d\n", failedChecks)

	return out
}

// buildClusterMetrics returns cluster metrics
func (s *RESTServer) buildClusterMetrics() string {
	out := "# HELP anubis_cluster_leader Is this node the leader (1=yes, 0=no)\n"
	out += "# TYPE anubis_cluster_leader gauge\n"

	if s.cluster != nil {
		isLeader := 0
		if s.cluster.IsLeader() {
			isLeader = 1
		}
		out += fmt.Sprintf("anubis_cluster_leader %d\n", isLeader)

		status := s.cluster.GetStatus()
		if status != nil {
			out += "# HELP anubis_cluster_nodes Number of nodes in the cluster\n"
			out += "# TYPE anubis_cluster_nodes gauge\n"
			out += fmt.Sprintf("anubis_cluster_nodes %d\n", status.PeerCount+1)

			out += "# HELP anubis_raft_term Current Raft term number\n"
			out += "# TYPE anubis_raft_term gauge\n"
			out += fmt.Sprintf("anubis_raft_term %d\n", status.Term)

			out += "# HELP anubis_raft_commit_index Current Raft log commit index\n"
			out += "# TYPE anubis_raft_commit_index gauge\n"
			out += fmt.Sprintf("anubis_raft_commit_index %d\n", status.CommitIndex)

			out += "# HELP anubis_cluster_is_clustered Whether this node is in cluster mode\n"
			out += "# TYPE anubis_cluster_is_clusterd gauge\n"
			clustered := 0
			if status.IsClustered {
				clustered = 1
			}
			out += fmt.Sprintf("anubis_cluster_is_clustered %d\n", clustered)
		}
	} else {
		out += "anubis_cluster_leader 1\n"
		out += "anubis_cluster_nodes 1\n"
		out += "anubis_raft_term 0\n"
		out += "anubis_raft_commit_index 0\n"
		out += "anubis_cluster_is_clustered 0\n"
	}

	return out
}

// buildAlertMetrics returns alert manager metrics
func (s *RESTServer) buildAlertMetrics() string {
	if s.alert == nil {
		return ""
	}

	stats := s.alert.GetStats()

	out := "# HELP anubis_alerts_total Total number of alerts processed\n"
	out += "# TYPE anubis_alerts_total counter\n"
	out += fmt.Sprintf("anubis_alerts_total %d\n", stats.TotalAlerts)

	out += "# HELP anubis_alerts_sent_total Total number of alerts successfully sent\n"
	out += "# TYPE anubis_alerts_sent_total counter\n"
	out += fmt.Sprintf("anubis_alerts_sent_total %d\n", stats.SentAlerts)

	out += "# HELP anubis_alerts_failed_total Total number of failed alert dispatches\n"
	out += "# TYPE anubis_alerts_failed_total counter\n"
	out += fmt.Sprintf("anubis_alerts_failed_total %d\n", stats.FailedAlerts)

	out += "# HELP anubis_alerts_resolved_total Total number of resolved alerts\n"
	out += "# TYPE anubis_alerts_resolved_total counter\n"
	out += fmt.Sprintf("anubis_alerts_resolved_total %d\n", stats.ResolvedAlerts)

	out += "# HELP anubis_alerts_acknowledged_total Total number of acknowledged alerts\n"
	out += "# TYPE anubis_alerts_acknowledged_total counter\n"
	out += fmt.Sprintf("anubis_alerts_acknowledged_total %d\n", stats.AcknowledgedAlerts)

	out += "# HELP anubis_alerts_rate_limited_total Total number of rate-limited alerts\n"
	out += "# TYPE anubis_alerts_rate_limited_total counter\n"
	out += fmt.Sprintf("anubis_alerts_rate_limited_total %d\n", stats.RateLimitedAlerts)

	out += "# HELP anubis_alerts_filtered_total Total number of filtered alerts\n"
	out += "# TYPE anubis_alerts_filtered_total counter\n"
	out += fmt.Sprintf("anubis_alerts_filtered_total %d\n", stats.FilteredAlerts)

	out += "# HELP anubis_active_incidents Number of currently active incidents\n"
	out += "# TYPE anubis_active_incidents gauge\n"
	out += fmt.Sprintf("anubis_active_incidents %d\n", stats.ActiveIncidents)

	return out
}

// buildLatencyMetrics returns latency percentile metrics
func (s *RESTServer) buildLatencyMetrics() string {
	souls, err := s.store.ListSoulsNoCtx("default", 0, 10000)
	if err != nil || len(souls) == 0 {
		return ""
	}

	// Collect all recent latencies
	var latencies []float64
	for _, soul := range souls {
		judgments, err := s.store.ListJudgmentsNoCtx(soul.ID, time.Now().Add(-24*time.Hour), time.Now(), 100)
		if err != nil {
			continue
		}
		for _, j := range judgments {
			latencies = append(latencies, float64(j.Duration)/float64(time.Second))
		}
	}

	if len(latencies) == 0 {
		return ""
	}

	sort.Float64s(latencies)

	p50 := percentile(latencies, 50)
	p95 := percentile(latencies, 95)
	p99 := percentile(latencies, 99)
	avg := average(latencies)

	out := "# HELP anubis_latency_p50_seconds 50th percentile check latency\n"
	out += "# TYPE anubis_latency_p50_seconds gauge\n"
	out += fmt.Sprintf("anubis_latency_p50_seconds %f\n", p50)

	out += "# HELP anubis_latency_p95_seconds 95th percentile check latency\n"
	out += "# TYPE anubis_latency_p95_seconds gauge\n"
	out += fmt.Sprintf("anubis_latency_p95_seconds %f\n", p95)

	out += "# HELP anubis_latency_p99_seconds 99th percentile check latency\n"
	out += "# TYPE anubis_latency_p99_seconds gauge\n"
	out += fmt.Sprintf("anubis_latency_p99_seconds %f\n", p99)

	out += "# HELP anubis_latency_avg_seconds Average check latency\n"
	out += "# TYPE anubis_latency_avg_seconds gauge\n"
	out += fmt.Sprintf("anubis_latency_avg_seconds %f\n", avg)

	out += "# HELP anubis_latency_samples_total Number of latency samples used\n"
	out += "# TYPE anubis_latency_samples_total gauge\n"
	out += fmt.Sprintf("anubis_latency_samples_total %d\n", len(latencies))

	return out
}

// statusNumeric converts a soul status string to a numeric value
func statusNumeric(status core.SoulStatus) int {
	switch status {
	case core.SoulAlive:
		return 1
	case core.SoulDead:
		return 0
	case core.SoulDegraded:
		return 2
	case core.SoulEmbalmed:
		return 4
	default:
		return 3
	}
}

// computeUptimeRatio computes the ratio of healthy (alive/degraded) judgments
func computeUptimeRatio(judgments []*core.Judgment) float64 {
	if len(judgments) == 0 {
		return -1
	}

	healthy := 0
	for _, j := range judgments {
		if j.Status == core.SoulAlive || j.Status == core.SoulDegraded {
			healthy++
		}
	}

	return float64(healthy) / float64(len(judgments))
}

// percentile returns the p-th percentile value from a sorted slice
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}

	rank := p / 100.0 * float64(len(sorted)-1)
	lower := int(math.Floor(rank))
	upper := int(math.Ceil(rank))
	if lower == upper {
		return sorted[lower]
	}

	frac := rank - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}

// average returns the mean of a float64 slice
func average(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}
