package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

var (
	errAccessDenied          = errors.New("access denied")
	errQueryResourceNotFound = errors.New("resource not found")
)

type journeyListItem struct {
	*core.JourneyConfig
	StepCount   int     `json:"step_count"`
	LastRun     string  `json:"last_run,omitempty"`
	LastStatus  string  `json:"last_status"`
	AvgDuration float64 `json:"avg_duration"`
	SuccessRate float64 `json:"success_rate"`
}

// handleListJourneys lists all journeys
func (s *RESTServer) handleListJourneys(ctx *Context) error {
	workspace := ctx.Workspace
	offset, limit := parsePagination(ctx.Request, 20, 100)

	journeys, err := s.store.ListJourneysNoCtx(workspace, offset, limit)
	if err != nil {
		s.logger.Error("failed to list journeys", "error", err, "workspace", workspace)
		return ctx.Error(http.StatusInternalServerError, "failed to retrieve journeys")
	}

	items := make([]journeyListItem, 0, len(journeys))
	for _, journey := range journeys {
		items = append(items, s.buildJourneyListItem(ctx.Request.Context(), workspace, journey))
	}

	return ctx.JSON(http.StatusOK, items)
}

// handleCreateJourney creates a new journey
func (s *RESTServer) handleCreateJourney(ctx *Context) error {
	var journey core.JourneyConfig
	if err := ctx.Bind(&journey); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid journey data")
	}
	if err := validateJourneyForAPI(&journey); err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}

	journey.ID = core.GenerateID()
	journey.WorkspaceID = contextWorkspace(ctx)
	journey.CreatedAt = time.Now()
	journey.UpdatedAt = time.Now()

	if err := s.store.SaveJourneyNoCtx(&journey); err != nil {
		return s.internalError(ctx, err, "failed to save journey")
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

	// IDOR protection: Check if journey belongs to user's workspace
	if !sameWorkspace(journey.WorkspaceID, contextWorkspace(ctx)) {
		return ctx.Error(http.StatusForbidden, "access denied")
	}

	return ctx.JSON(http.StatusOK, journey)
}

// handleUpdateJourney updates a journey
func (s *RESTServer) handleUpdateJourney(ctx *Context) error {
	id := ctx.Params["id"]

	// IDOR protection: Check if journey belongs to user's workspace
	existing, err := s.store.GetJourneyNoCtx(id)
	if err == nil && existing == nil {
		err = fmt.Errorf("journey not found")
	}
	if err != nil {
		return ctx.Error(http.StatusNotFound, "journey not found")
	}
	if !sameWorkspace(existing.WorkspaceID, contextWorkspace(ctx)) {
		return ctx.Error(http.StatusForbidden, "access denied")
	}

	var journey core.JourneyConfig
	if err := ctx.Bind(&journey); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid journey data")
	}
	if err := validateJourneyForAPI(&journey); err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}

	journey.ID = id
	journey.WorkspaceID = contextWorkspace(ctx)
	journey.CreatedAt = existing.CreatedAt
	journey.UpdatedAt = time.Now()

	if err := s.store.SaveJourneyNoCtx(&journey); err != nil {
		return s.internalError(ctx, err, "failed to save journey")
	}

	return ctx.JSON(http.StatusOK, journey)
}

// handleDeleteJourney deletes a journey
func (s *RESTServer) handleDeleteJourney(ctx *Context) error {
	id := ctx.Params["id"]

	// IDOR protection: Check if journey belongs to user's workspace
	journey, err := s.store.GetJourneyNoCtx(id)
	if err == nil && journey == nil {
		err = fmt.Errorf("journey not found")
	}
	if err != nil {
		return ctx.Error(http.StatusNotFound, "journey not found")
	}
	if !sameWorkspace(journey.WorkspaceID, contextWorkspace(ctx)) {
		return ctx.Error(http.StatusForbidden, "access denied")
	}

	if err := s.store.DeleteJourneyNoCtx(id); err != nil {
		return s.internalError(ctx, err, "failed to delete journey")
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

	// IDOR protection: Check if journey belongs to user's workspace
	if !sameWorkspace(journey.WorkspaceID, contextWorkspace(ctx)) {
		return ctx.Error(http.StatusForbidden, "access denied")
	}

	if s.journey == nil {
		return ctx.Error(http.StatusServiceUnavailable, "journey executor not available")
	}

	run, err := s.journey.RunOnce(ctx.Request.Context(), journey)
	if err != nil {
		return s.internalError(ctx, err, "failed to run journey")
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"journey_id": journey.ID,
		"run_id":     run.ID,
		"status":     run.Status,
		"duration":   run.Duration,
	})
}

func validateJourneyForAPI(journey *core.JourneyConfig) error {
	if journey.Name == "" {
		return fmt.Errorf("journey name is required")
	}
	if len(journey.Steps) == 0 {
		return fmt.Errorf("journey must have at least one step")
	}
	for i, step := range journey.Steps {
		if step.Name == "" {
			return fmt.Errorf("journey step %d name is required", i+1)
		}
		if step.Target == "" {
			return fmt.Errorf("journey step %d target is required", i+1)
		}
	}
	if journey.Weight.Duration <= 0 {
		journey.Weight.Duration = time.Minute
	}
	if journey.Timeout.Duration <= 0 {
		journey.Timeout.Duration = 30 * time.Second
	}
	for i := range journey.Steps {
		if journey.Steps[i].Type == "" {
			journey.Steps[i].Type = core.CheckHTTP
		}
		if journey.Steps[i].Timeout.Duration <= 0 {
			journey.Steps[i].Timeout.Duration = 10 * time.Second
		}
	}
	return nil
}

func (s *RESTServer) buildJourneyListItem(ctx context.Context, workspace string, journey *core.JourneyConfig) journeyListItem {
	item := journeyListItem{
		JourneyConfig: journey,
		StepCount:     len(journey.Steps),
		LastStatus:    "unknown",
		SuccessRate:   0,
	}

	if s.journey == nil {
		return item
	}

	runs, err := s.journey.ListRuns(ctx, workspace, journey.ID, 100)
	if err != nil || len(runs) == 0 {
		return item
	}

	item.LastStatus = journeyRunUIStatus(runs[0].Status)
	item.LastRun = time.UnixMilli(runs[0].StartedAt).UTC().Format(time.RFC3339)

	var totalDuration int64
	successes := 0
	counted := 0
	for _, run := range runs {
		if run == nil {
			continue
		}
		totalDuration += run.Duration
		if run.Status == core.SoulAlive {
			successes++
		}
		counted++
	}
	if counted > 0 {
		item.AvgDuration = float64(totalDuration) / float64(counted)
		item.SuccessRate = float64(successes) / float64(counted) * 100
	}

	return item
}

func journeyRunUIStatus(status core.SoulStatus) string {
	switch status {
	case core.SoulAlive:
		return "passed"
	case core.SoulDead:
		return "failed"
	default:
		return "pending"
	}
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
		return s.internalError(ctx, err, "failed to list journey runs")
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

	soul, err := s.store.GetSoulNoCtx(id)
	if err == nil && soul == nil {
		err = fmt.Errorf("soul not found")
	}
	if err != nil {
		return ctx.Error(http.StatusNotFound, "soul not found")
	}
	if !sameWorkspace(soul.WorkspaceID, contextWorkspace(ctx)) {
		return ctx.Error(http.StatusForbidden, "access denied")
	}

	limit := 50
	if raw := ctx.Request.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}

	judgments, err := s.store.ListJudgmentsNoCtx(id, time.Now().Add(-30*24*time.Hour), time.Now(), limit)
	if err != nil {
		return s.internalError(ctx, err, "failed to list soul logs")
	}

	logs := make([]map[string]interface{}, 0, len(judgments))
	for _, judgment := range judgments {
		level := "info"
		message := "Health check passed"
		if judgment.Status != core.SoulAlive {
			level = "error"
			message = "Health check failed"
			if judgment.Message != "" {
				message = judgment.Message
			}
		}
		logs = append(logs, map[string]interface{}{
			"timestamp":   judgment.Timestamp.Format(time.RFC3339),
			"level":       level,
			"message":     message,
			"soul_id":     id,
			"judgment_id": judgment.ID,
			"status":      judgment.Status,
			"duration":    judgment.Duration.String(),
		})
	}

	return ctx.JSON(http.StatusOK, logs)
}

// Dashboard handlers

func (s *RESTServer) handleListDashboards(ctx *Context) error {
	dashboards, err := s.store.ListDashboardsNoCtx()
	if err != nil {
		return s.internalError(ctx, err, "failed to list dashboards")
	}
	workspace := contextWorkspace(ctx)
	filtered := make([]*core.CustomDashboard, 0, len(dashboards))
	for _, dashboard := range dashboards {
		if dashboard == nil {
			continue
		}
		if sameWorkspace(dashboard.WorkspaceID, workspace) {
			filtered = append(filtered, dashboard)
		}
	}
	return ctx.JSON(http.StatusOK, filtered)
}

func (s *RESTServer) handleCreateDashboard(ctx *Context) error {
	var dashboard core.CustomDashboard
	if err := ctx.Bind(&dashboard); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid dashboard data")
	}

	dashboard.ID = core.GenerateID()
	dashboard.WorkspaceID = contextWorkspace(ctx)
	dashboard.CreatedAt = time.Now()
	dashboard.UpdatedAt = time.Now()

	if err := s.store.SaveDashboardNoCtx(&dashboard); err != nil {
		return s.internalError(ctx, err, "failed to save dashboard")
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

	// IDOR protection: Check if dashboard belongs to user's workspace
	if !sameWorkspace(dashboard.WorkspaceID, contextWorkspace(ctx)) {
		return ctx.Error(http.StatusForbidden, "access denied")
	}

	return ctx.JSON(http.StatusOK, dashboard)
}

func (s *RESTServer) handleUpdateDashboard(ctx *Context) error {
	id := ctx.Params["id"]

	// IDOR protection: Check if dashboard belongs to user's workspace
	existing, err := s.store.GetDashboardNoCtx(id)
	if err == nil && existing == nil {
		err = fmt.Errorf("dashboard not found")
	}
	if err != nil {
		return ctx.Error(http.StatusNotFound, "dashboard not found")
	}
	if !sameWorkspace(existing.WorkspaceID, contextWorkspace(ctx)) {
		return ctx.Error(http.StatusForbidden, "access denied")
	}

	var dashboard core.CustomDashboard
	if err := ctx.Bind(&dashboard); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid dashboard data")
	}

	dashboard.ID = id
	dashboard.WorkspaceID = contextWorkspace(ctx)
	dashboard.CreatedAt = existing.CreatedAt
	dashboard.UpdatedAt = time.Now()

	if err := s.store.SaveDashboardNoCtx(&dashboard); err != nil {
		return s.internalError(ctx, err, "failed to save dashboard")
	}

	return ctx.JSON(http.StatusOK, dashboard)
}

func (s *RESTServer) handleDeleteDashboard(ctx *Context) error {
	id := ctx.Params["id"]

	// IDOR protection: Check if dashboard belongs to user's workspace
	dashboard, err := s.store.GetDashboardNoCtx(id)
	if err == nil && dashboard == nil {
		err = fmt.Errorf("dashboard not found")
	}
	if err != nil {
		return ctx.Error(http.StatusNotFound, "dashboard not found")
	}
	if !sameWorkspace(dashboard.WorkspaceID, contextWorkspace(ctx)) {
		return ctx.Error(http.StatusForbidden, "access denied")
	}

	if err := s.store.DeleteDashboardNoCtx(id); err != nil {
		return s.internalError(ctx, err, "failed to delete dashboard")
	}
	return ctx.JSON(http.StatusNoContent, nil)
}

// handleDashboardQuery resolves a widget query and returns data
func (s *RESTServer) handleDashboardQuery(ctx *Context) error {
	id := ctx.Params["id"]
	if id != "" {
		dashboard, err := s.store.GetDashboardNoCtx(id)
		if err != nil || dashboard == nil {
			return ctx.Error(http.StatusNotFound, "dashboard not found")
		}
		if !sameWorkspace(dashboard.WorkspaceID, contextWorkspace(ctx)) {
			return ctx.Error(http.StatusForbidden, "access denied")
		}
	}

	var query core.WidgetQuery
	if err := ctx.Bind(&query); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid query")
	}

	var result interface{}
	var err error

	switch query.Source {
	case "souls":
		result, err = s.querySouls(query, ctx.Workspace)
	case "judgments":
		result, err = s.queryJudgments(query, ctx.Workspace)
	case "stats":
		result, err = s.queryStats(query, ctx.Workspace)
	case "alerts":
		result, err = s.queryAlerts(query, ctx.Workspace)
	default:
		return ctx.Error(http.StatusBadRequest, "unknown query source: "+query.Source)
	}

	if err != nil {
		if errors.Is(err, errAccessDenied) {
			return ctx.Error(http.StatusForbidden, "access denied")
		}
		if errors.Is(err, errQueryResourceNotFound) {
			return ctx.Error(http.StatusNotFound, "resource not found")
		}
		return s.internalError(ctx, err, "dashboard query failed")
	}

	return ctx.JSON(http.StatusOK, result)
}

func (s *RESTServer) querySouls(q core.WidgetQuery, workspace string) (interface{}, error) {
	souls, err := s.store.ListSoulsNoCtx(workspace, 0, 1000)
	if err != nil {
		return nil, err
	}

	switch q.Metric {
	case "count":
		return map[string]int{"count": len(souls)}, nil
	case "list":
		return souls, nil
	case "status_distribution":
		distribution := map[string]int{
			"healthy":   0,
			"unhealthy": 0,
			"unknown":   0,
		}
		for _, soul := range souls {
			status := s.soulWithLatestJudgment(soul).Status
			if _, ok := distribution[status]; !ok {
				status = "unknown"
			}
			distribution[status]++
		}
		return distribution, nil
	default:
		return map[string]int{"count": len(souls)}, nil
	}
}

func (s *RESTServer) queryJudgments(q core.WidgetQuery, workspace string) (interface{}, error) {
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
		soul, err := s.store.GetSoulNoCtx(soulID)
		if err != nil || soul == nil {
			return nil, errQueryResourceNotFound
		}
		if !sameWorkspace(soul.WorkspaceID, workspace) {
			return nil, errAccessDenied
		}
		judgments, err = s.store.ListJudgmentsNoCtx(soulID, start, end, 1000)
	} else {
		// List all souls and get their judgments
		souls, err := s.store.ListSoulsNoCtx(workspace, 0, 100)
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

func (s *RESTServer) queryStats(q core.WidgetQuery, workspace string) (interface{}, error) {
	souls, _ := s.store.ListSoulsNoCtx(workspace, 0, 1000)

	// Get recent judgments for all souls to determine status distribution
	var judgments []*core.Judgment
	for _, soul := range souls {
		js, _ := s.store.ListJudgmentsNoCtx(soul.ID, time.Now().Add(-24*time.Hour), time.Now(), 100)
		judgments = append(judgments, js...)
	}

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

func (s *RESTServer) queryAlerts(q core.WidgetQuery, workspace string) (interface{}, error) {
	stats := s.alert.GetStats()
	activeIncidents := 0
	for _, incident := range s.alert.ListActiveIncidents() {
		if incident != nil && sameWorkspace(incident.WorkspaceID, workspace) {
			activeIncidents++
		}
	}
	if q.Metric == "count" {
		return map[string]int{"count": activeIncidents}, nil
	}
	return map[string]interface{}{
		"channels": len(s.alert.ListChannelsByWorkspace(workspace)),
		"rules":    len(s.alert.ListRulesByWorkspace(workspace)),
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
