package api

import (
	"fmt"
	"runtime"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// handleMetrics returns Prometheus-compatible metrics
func (s *RESTServer) handleMetrics(ctx *Context) error {
	var output string

	// Build metrics output
	output += s.buildSystemMetrics()
	output += s.buildSoulMetrics()
	output += s.buildJudgmentMetrics()
	output += s.buildClusterMetrics()

	ctx.Response.Header().Set("Content-Type", "text/plain; version=0.0.4")
	ctx.Response.Write([]byte(output))
	return nil
}

// buildSystemMetrics returns system-level metrics
func (s *RESTServer) buildSystemMetrics() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	metrics := "# HELP anubis_build_info Build information\n"
	metrics += "# TYPE anubis_build_info gauge\n"
	metrics += fmt.Sprintf("anubis_build_info{version=\"dev\"} 1\n")

	metrics += "# HELP anubis_memory_alloc_bytes Allocated memory in bytes\n"
	metrics += "# TYPE anubis_memory_alloc_bytes gauge\n"
	metrics += fmt.Sprintf("anubis_memory_alloc_bytes %d\n", m.Alloc)

	metrics += "# HELP anubis_memory_sys_bytes System memory in bytes\n"
	metrics += "# TYPE anubis_memory_sys_bytes gauge\n"
	metrics += fmt.Sprintf("anubis_memory_sys_bytes %d\n", m.Sys)

	metrics += "# HELP anubis_goroutines Number of goroutines\n"
	metrics += "# TYPE anubis_goroutines gauge\n"
	metrics += fmt.Sprintf("anubis_goroutines %d\n", runtime.NumGoroutine())

	return metrics
}

// buildSoulMetrics returns soul-related metrics
func (s *RESTServer) buildSoulMetrics() string {
	souls, err := s.store.ListSoulsNoCtx("default", 0, 10000)
	if err != nil {
		return ""
	}

	metrics := "# HELP anubis_souls_total Total number of souls\n"
	metrics += "# TYPE anubis_souls_total gauge\n"
	metrics += fmt.Sprintf("anubis_souls_total %d\n", len(souls))

	return metrics
}

// buildJudgmentMetrics returns judgment/health check metrics
func (s *RESTServer) buildJudgmentMetrics() string {
	// Get latest judgments for each soul
	souls, _ := s.store.ListSoulsNoCtx("default", 0, 10000)

	metrics := "# HELP anubis_soul_status Current soul status (0=dead, 1=alive, 2=degraded, 3=unknown)\n"
	metrics += "# TYPE anubis_soul_status gauge\n"

	metrics += "# HELP anubis_soul_latency_seconds Last check latency in seconds\n"
	metrics += "# TYPE anubis_soul_latency_seconds gauge\n"

	for _, soul := range souls {
		judgments, err := s.store.ListJudgmentsNoCtx(soul.ID, time.Now().Add(-24*time.Hour), time.Now(), 1)
		if err != nil || len(judgments) == 0 {
			continue
		}

		latest := judgments[0]

		// Status metric
		statusValue := 3 // unknown
		switch latest.Status {
		case core.SoulAlive:
			statusValue = 1
		case core.SoulDead:
			statusValue = 0
		case core.SoulDegraded:
			statusValue = 2
		}

		metrics += fmt.Sprintf("anubis_soul_status{soul=\"%s\"} %d\n", soul.Name, statusValue)

		// Latency metric
		latencySeconds := float64(latest.Duration) / float64(time.Second)
		metrics += fmt.Sprintf("anubis_soul_latency_seconds{soul=\"%s\"} %f\n", soul.Name, latencySeconds)
	}

	return metrics
}

// buildClusterMetrics returns cluster metrics
func (s *RESTServer) buildClusterMetrics() string {
	metrics := "# HELP anubis_cluster_leader Is this node the leader (1=yes, 0=no)\n"
	metrics += "# TYPE anubis_cluster_leader gauge\n"

	if s.cluster != nil {
		isLeader := 0
		if s.cluster.IsLeader() {
			isLeader = 1
		}
		metrics += fmt.Sprintf("anubis_cluster_leader %d\n", isLeader)
	} else {
		metrics += "anubis_cluster_leader 1\n"
	}

	return metrics
}
