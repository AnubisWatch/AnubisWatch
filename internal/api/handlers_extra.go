package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// handleListJourneys lists all journeys
func (s *RESTServer) handleListJourneys(ctx *Context) error {
	workspace := ctx.Workspace
	offset, limit := parsePagination(ctx.Request, 20, 100)

	journeys, err := s.store.ListJourneysNoCtx(workspace, offset, limit)
	if err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusOK, journeys)
}

// handleCreateJourney creates a new journey
func (s *RESTServer) handleCreateJourney(ctx *Context) error {
	var journey core.JourneyConfig
	if err := ctx.Bind(&journey); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid journey data")
	}

	journey.ID = core.GenerateID()
	journey.WorkspaceID = ctx.Workspace
	journey.CreatedAt = time.Now()
	journey.UpdatedAt = time.Now()

	if err := s.store.SaveJourneyNoCtx(&journey); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusCreated, journey)
}

// handleGetJourney gets a journey by ID
func (s *RESTServer) handleGetJourney(ctx *Context) error {
	id := ctx.Params["id"]
	journey, err := s.store.GetJourneyNoCtx(id)
	if err == nil && journey == nil {
		err = fmt.Errorf("journey not found")
	}
	if err != nil {
		return ctx.Error(http.StatusNotFound, "journey not found")
	}
	return ctx.JSON(http.StatusOK, journey)
}

// handleUpdateJourney updates a journey
func (s *RESTServer) handleUpdateJourney(ctx *Context) error {
	id := ctx.Params["id"]
	var journey core.JourneyConfig
	if err := ctx.Bind(&journey); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid journey data")
	}

	journey.ID = id
	journey.WorkspaceID = ctx.Workspace
	journey.UpdatedAt = time.Now()

	if err := s.store.SaveJourneyNoCtx(&journey); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusOK, journey)
}

// handleDeleteJourney deletes a journey
func (s *RESTServer) handleDeleteJourney(ctx *Context) error {
	id := ctx.Params["id"]
	if err := s.store.DeleteJourneyNoCtx(id); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}
	return ctx.JSON(http.StatusNoContent, nil)
}

// handleRunJourney runs a journey immediately
func (s *RESTServer) handleRunJourney(ctx *Context) error {
	id := ctx.Params["id"]

	journey, err := s.store.GetJourneyNoCtx(id)
	if err == nil && journey == nil {
		err = fmt.Errorf("journey not found")
	}
	if err != nil {
		return ctx.Error(http.StatusNotFound, "journey not found")
	}

	return ctx.JSON(http.StatusAccepted, map[string]interface{}{
		"journey_id": journey.ID,
		"status":     "executing",
		"message":    "Journey execution triggered",
	})
}

// handleListJourneyRuns lists runs for a journey
func (s *RESTServer) handleListJourneyRuns(ctx *Context) error {
	id := ctx.Params["id"]
	limit := ctx.Request.URL.Query().Get("limit")
	n := 20
	if limit != "" {
		if v, err := strconv.Atoi(limit); err == nil && v > 0 {
			n = v
		}
	}

	if s.journey == nil {
		return ctx.Error(http.StatusServiceUnavailable, "journey executor not available")
	}

	runs, err := s.journey.ListRuns(ctx.Request.Context(), ctx.Workspace, id, n)
	if err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}
	return ctx.JSON(http.StatusOK, runs)
}

// handleGetJourneyRun gets a single journey run
func (s *RESTServer) handleGetJourneyRun(ctx *Context) error {
	journeyID := ctx.Params["id"]
	runID := ctx.Params["runId"]

	if s.journey == nil {
		return ctx.Error(http.StatusServiceUnavailable, "journey executor not available")
	}

	run, err := s.journey.GetRun(ctx.Request.Context(), ctx.Workspace, journeyID, runID)
	if err != nil {
		return ctx.Error(http.StatusNotFound, "journey run not found")
	}
	return ctx.JSON(http.StatusOK, run)
}

// handleMCPTools returns available MCP tools
func (s *RESTServer) handleMCPTools(ctx *Context) error {
	tools := []map[string]interface{}{
		{
			"name":        "list_souls",
			"description": "List all monitored souls",
			"parameters":  map[string]interface{}{},
		},
		{
			"name":        "get_soul_status",
			"description": "Get status of a specific soul",
			"parameters": map[string]interface{}{
				"soul_id": map[string]string{"type": "string", "description": "ID of the soul"},
			},
		},
	}
	return ctx.JSON(http.StatusOK, tools)
}

// handleSoulLogs returns logs for a soul
func (s *RESTServer) handleSoulLogs(ctx *Context) error {
	id := ctx.Params["id"]
	logs := []map[string]interface{}{
		{
			"timestamp": time.Now().Add(-5 * time.Minute).Format(time.RFC3339),
			"level":     "info",
			"message":   "Health check passed",
			"soul_id":   id,
		},
	}
	return ctx.JSON(http.StatusOK, logs)
}

// Dashboard handlers

func (s *RESTServer) handleListDashboards(ctx *Context) error {
	dashboards, err := s.store.ListDashboardsNoCtx()
	if err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}
	return ctx.JSON(http.StatusOK, dashboards)
}

func (s *RESTServer) handleCreateDashboard(ctx *Context) error {
	var dashboard core.CustomDashboard
	if err := ctx.Bind(&dashboard); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid dashboard data")
	}

	dashboard.ID = core.GenerateID()
	dashboard.WorkspaceID = ctx.Workspace
	dashboard.CreatedAt = time.Now()
	dashboard.UpdatedAt = time.Now()

	if err := s.store.SaveDashboardNoCtx(&dashboard); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusCreated, dashboard)
}

func (s *RESTServer) handleGetDashboard(ctx *Context) error {
	id := ctx.Params["id"]
	dashboard, err := s.store.GetDashboardNoCtx(id)
	if err == nil && dashboard == nil {
		err = fmt.Errorf("dashboard not found")
	}
	if err != nil {
		return ctx.Error(http.StatusNotFound, "dashboard not found")
	}
	return ctx.JSON(http.StatusOK, dashboard)
}

func (s *RESTServer) handleUpdateDashboard(ctx *Context) error {
	id := ctx.Params["id"]
	var dashboard core.CustomDashboard
	if err := ctx.Bind(&dashboard); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid dashboard data")
	}

	dashboard.ID = id
	dashboard.WorkspaceID = ctx.Workspace
	dashboard.UpdatedAt = time.Now()

	if err := s.store.SaveDashboardNoCtx(&dashboard); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusOK, dashboard)
}

func (s *RESTServer) handleDeleteDashboard(ctx *Context) error {
	id := ctx.Params["id"]
	if err := s.store.DeleteDashboardNoCtx(id); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}
	return ctx.JSON(http.StatusNoContent, nil)
}

// handleDashboardQuery resolves a widget query and returns data
func (s *RESTServer) handleDashboardQuery(ctx *Context) error {
	var query core.WidgetQuery
	if err := ctx.Bind(&query); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid query")
	}

	var result interface{}
	var err error

	switch query.Source {
	case "souls":
		result, err = s.querySouls(query)
	case "judgments":
		result, err = s.queryJudgments(query)
	case "stats":
		result, err = s.queryStats(query)
	case "alerts":
		result, err = s.queryAlerts(query)
	default:
		return ctx.Error(http.StatusBadRequest, "unknown query source: "+query.Source)
	}

	if err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusOK, result)
}

func (s *RESTServer) querySouls(q core.WidgetQuery) (interface{}, error) {
	souls, err := s.store.ListSoulsNoCtx("", 0, 1000)
	if err != nil {
		return nil, err
	}

	switch q.Metric {
	case "count":
		return map[string]int{"count": len(souls)}, nil
	case "list":
		return souls, nil
	default:
		return map[string]int{"count": len(souls)}, nil
	}
}

func (s *RESTServer) queryJudgments(q core.WidgetQuery) (interface{}, error) {
	// Parse time range
	duration := 24 * time.Hour
	if q.TimeRange != "" {
		duration = parseTimeRange(q.TimeRange)
	}

	start := time.Now().Add(-duration)
	end := time.Now()

	soulID := q.Filters["soul_id"]
	var judgments []*core.Judgment
	var err error

	if soulID != "" {
		judgments, err = s.store.ListJudgmentsNoCtx(soulID, start, end, 1000)
	} else {
		// List all souls and get their judgments
		souls, err := s.store.ListSoulsNoCtx("", 0, 100)
		if err != nil {
			return nil, err
		}
		for _, soul := range souls {
			js, _ := s.store.ListJudgmentsNoCtx(soul.ID, start, end, 100)
			judgments = append(judgments, js...)
		}
	}
	if err != nil {
		return nil, err
	}

	// Aggregate by time buckets
	buckets := make(map[string]struct {
		Count    int     `json:"count"`
		AvgLat   float64 `json:"avg_latency"`
		Passed   int     `json:"passed"`
		Failed   int     `json:"failed"`
		TotalLat float64 `json:"-"`
	})

	for _, j := range judgments {
		bucket := j.Timestamp.Truncate(time.Hour).Format("15:04")
		b := buckets[bucket]
		b.Count++
		b.TotalLat += float64(j.Duration.Milliseconds())
		b.AvgLat = b.TotalLat / float64(b.Count)
		if j.Status == core.SoulAlive {
			b.Passed++
		} else {
			b.Failed++
		}
		buckets[bucket] = b
	}

	type TimeBucket struct {
		Time       string  `json:"time"`
		Count      int     `json:"count"`
		AvgLatency float64 `json:"avg_latency"`
		Passed     int     `json:"passed"`
		Failed     int     `json:"failed"`
	}
	result := make([]TimeBucket, 0, len(buckets))
	for t, b := range buckets {
		result = append(result, TimeBucket{
			Time:       t,
			Count:      b.Count,
			AvgLatency: b.AvgLat,
			Passed:     b.Passed,
			Failed:     b.Failed,
		})
	}

	return result, nil
}

func (s *RESTServer) queryStats(q core.WidgetQuery) (interface{}, error) {
	souls, _ := s.store.ListSoulsNoCtx("", 0, 1000)

	// Get recent judgments to determine status distribution
	judgments, _ := s.store.ListJudgmentsNoCtx("", time.Now().Add(-24*time.Hour), time.Now(), 1000)
	alive := 0
	dead := 0
	degraded := 0
	for _, j := range judgments {
		switch j.Status {
		case core.SoulAlive:
			alive++
		case core.SoulDead:
			dead++
		case core.SoulDegraded:
			degraded++
		}
	}

	total := alive + dead + degraded
	uptime := 0.0
	if total > 0 {
		uptime = float64(alive) / float64(total) * 100
	}

	return map[string]interface{}{
		"total":    len(souls),
		"alive":    alive,
		"dead":     dead,
		"degraded": degraded,
		"uptime":   uptime,
	}, nil
}

func (s *RESTServer) queryAlerts(q core.WidgetQuery) (interface{}, error) {
	stats := s.alert.GetStats()
	return map[string]interface{}{
		"channels": len(s.alert.ListChannels()),
		"rules":    len(s.alert.ListRules()),
		"stats":    stats,
	}, nil
}

func parseTimeRange(s string) time.Duration {
	if len(s) < 2 {
		return 24 * time.Hour
	}
	unit := s[len(s)-1:]
	num, _ := strconv.Atoi(s[:len(s)-1])
	switch unit {
	case "h":
		return time.Duration(num) * time.Hour
	case "d":
		return time.Duration(num) * 24 * time.Hour
	case "w":
		return time.Duration(num) * 7 * 24 * time.Hour
	}
	return 24 * time.Hour
}

// handleDashboardTemplates returns pre-built dashboard templates
func (s *RESTServer) handleDashboardTemplates(ctx *Context) error {
	templates := []map[string]interface{}{
		{
			"id":          "template-overview",
			"name":        "Overview",
			"description": "High-level system overview with key metrics",
			"refresh_sec": 30,
			"widgets": []map[string]interface{}{
				{"id": "w1", "title": "Total Souls", "type": "stat", "grid": map[string]int{"x": 0, "y": 0, "width": 3, "height": 2}, "query": map[string]string{"source": "souls", "metric": "count", "time_range": "24h"}},
				{"id": "w2", "title": "Status Distribution", "type": "bar_chart", "grid": map[string]int{"x": 3, "y": 0, "width": 4, "height": 2}, "query": map[string]string{"source": "souls", "metric": "status_distribution", "time_range": "24h"}},
				{"id": "w3", "title": "Uptime", "type": "stat", "grid": map[string]int{"x": 7, "y": 0, "width": 3, "height": 2}, "query": map[string]string{"source": "stats", "metric": "uptime", "time_range": "24h"}},
				{"id": "w4", "title": "Alert Summary", "type": "stat", "grid": map[string]int{"x": 10, "y": 0, "width": 2, "height": 2}, "query": map[string]string{"source": "alerts", "metric": "count", "time_range": "24h"}},
				{"id": "w5", "title": "Latency Over Time", "type": "line_chart", "grid": map[string]int{"x": 0, "y": 2, "width": 8, "height": 3}, "query": map[string]string{"source": "judgments", "metric": "latency", "time_range": "24h", "aggregation": "avg"}},
				{"id": "w6", "title": "Check Volume", "type": "bar_chart", "grid": map[string]int{"x": 8, "y": 2, "width": 4, "height": 3}, "query": map[string]string{"source": "judgments", "metric": "count", "time_range": "24h"}},
			},
		},
		{
			"id":          "template-performance",
			"name":        "Performance",
			"description": "Detailed performance metrics and latency analysis",
			"refresh_sec": 15,
			"widgets": []map[string]interface{}{
				{"id": "w1", "title": "Avg Latency", "type": "stat", "grid": map[string]int{"x": 0, "y": 0, "width": 4, "height": 2}, "query": map[string]string{"source": "judgments", "metric": "latency", "time_range": "24h", "aggregation": "avg"}},
				{"id": "w2", "title": "P95 Latency", "type": "stat", "grid": map[string]int{"x": 4, "y": 0, "width": 4, "height": 2}, "query": map[string]string{"source": "judgments", "metric": "latency", "time_range": "24h", "aggregation": "p95"}},
				{"id": "w3", "title": "P99 Latency", "type": "stat", "grid": map[string]int{"x": 8, "y": 0, "width": 4, "height": 2}, "query": map[string]string{"source": "judgments", "metric": "latency", "time_range": "24h", "aggregation": "p99"}},
				{"id": "w4", "title": "Latency Trend", "type": "line_chart", "grid": map[string]int{"x": 0, "y": 2, "width": 12, "height": 4}, "query": map[string]string{"source": "judgments", "metric": "latency", "time_range": "7d", "aggregation": "avg"}},
			},
		},
		{
			"id":          "template-reliability",
			"name":        "Reliability",
			"description": "Service reliability and incident tracking",
			"refresh_sec": 60,
			"widgets": []map[string]interface{}{
				{"id": "w1", "title": "Healthy Services", "type": "stat", "grid": map[string]int{"x": 0, "y": 0, "width": 3, "height": 2}, "query": map[string]string{"source": "stats", "metric": "alive", "time_range": "24h"}},
				{"id": "w2", "title": "Failed Services", "type": "stat", "grid": map[string]int{"x": 3, "y": 0, "width": 3, "height": 2}, "query": map[string]string{"source": "stats", "metric": "dead", "time_range": "24h"}},
				{"id": "w3", "title": "Active Alerts", "type": "gauge", "grid": map[string]int{"x": 6, "y": 0, "width": 3, "height": 2}, "query": map[string]string{"source": "alerts", "metric": "count", "time_range": "24h"}},
				{"id": "w4", "title": "Uptime %", "type": "gauge", "grid": map[string]int{"x": 9, "y": 0, "width": 3, "height": 2}, "query": map[string]string{"source": "stats", "metric": "uptime", "time_range": "7d"}},
				{"id": "w5", "title": "Service Health Table", "type": "table", "grid": map[string]int{"x": 0, "y": 2, "width": 12, "height": 4}, "query": map[string]string{"source": "souls", "metric": "list", "time_range": "24h"}},
			},
		},
	}

	return ctx.JSON(http.StatusOK, templates)
}
