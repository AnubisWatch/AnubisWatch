package api

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/sla"
)

// SLA reports and judgment export (GET /api/v1/reports/sla, /reports/sla.csv
// and /souls/:id/judgments/export). See internal/sla for how the figures are
// computed and docs/API.md for the response contract.

const (
	// defaultReportPeriod is the period of a report without from.
	defaultReportPeriod = 30 * 24 * time.Hour
	// maxReportPeriod bounds one report: rollups make long periods cheap,
	// but not free.
	maxReportPeriod = 5 * 366 * 24 * time.Hour
	// Export row limits: the default, and the most one request may ask for.
	defaultExportLimit = 100_000
	maxExportLimit     = 1_000_000
)

// slaReportBudget bounds the time one SLA report may take.
var slaReportBudget = 60 * time.Second

// slaCollector is implemented by storage that tallies checks from its
// rollups (storage.CobaltDB.CollectSLA via the server adapter). Without it the
// handler counts raw judgments.
type slaCollector interface {
	CollectSLA(ctx context.Context, workspaceID, soulID string, start, end time.Time, excluded []core.TimeRange, rawSince, hourSince time.Time) (*sla.Tally, error)
}

// judgmentStreamer is implemented by storage that can stream a soul's
// judgments oldest first without loading the range at once.
type judgmentStreamer interface {
	StreamJudgments(ctx context.Context, workspaceID, soulID string, start, end time.Time, limit int, fn func(*core.Judgment) error) (int, bool, error)
}

// SetRetentionPolicy tells the report endpoints what retention keeps: SLA
// periods are clipped to the oldest data any resolution still holds, and
// exports to raw retention.
func (s *RESTServer) SetRetentionPolicy(policy core.RetentionConfig) {
	s.uiConfigMu.Lock()
	defer s.uiConfigMu.Unlock()
	p := policy
	s.retentionPolicy = &p
}

// reportHorizons returns, at now, when raw judgments and hourly rollups
// start, and the oldest instant any data is kept for (zero: unlimited).
func (s *RESTServer) reportHorizons(now time.Time) (rawSince, hourSince, oldest time.Time) {
	s.uiConfigMu.RLock()
	policy := s.retentionPolicy
	s.uiConfigMu.RUnlock()
	if policy == nil {
		return time.Time{}, time.Time{}, time.Time{}
	}
	if policy.Raw.Duration > 0 {
		rawSince = now.Add(-policy.Raw.Duration)
	}
	if policy.Hour.Duration > 0 {
		hourSince = now.Add(-policy.Hour.Duration)
	}
	day := strings.TrimSpace(policy.Day)
	if day == "" || day == "unlimited" {
		return rawSince, hourSince, time.Time{}
	}
	d, err := time.ParseDuration(day)
	if err != nil || d <= 0 {
		return rawSince, hourSince, time.Time{}
	}
	oldest = now.Add(-d)
	for _, t := range []time.Time{rawSince, hourSince} {
		if !t.IsZero() && t.Before(oldest) {
			oldest = t
		}
	}
	return rawSince, hourSince, oldest
}

// parseReportTime accepts RFC3339 or a UTC date (YYYY-MM-DD).
func parseReportTime(name, value string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", value); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("%s must be an RFC3339 timestamp or a YYYY-MM-DD date", name)
}

// parseReportRange reads from/to (default: the last 30 days), clips from to
// horizon (zero: none) and reports whether it did.
func parseReportRange(r *http.Request, now, horizon time.Time) (from, to time.Time, clipped bool, err error) {
	q := r.URL.Query()
	to = now
	if v := strings.TrimSpace(q.Get("to")); v != "" {
		if to, err = parseReportTime("to", v); err != nil {
			return
		}
	}
	from = to.Add(-defaultReportPeriod)
	if v := strings.TrimSpace(q.Get("from")); v != "" {
		if from, err = parseReportTime("from", v); err != nil {
			return
		}
	}
	if to.After(now) {
		to = now
	}
	if !from.Before(to) {
		err = fmt.Errorf("from must be before to (and not in the future)")
		return
	}
	if to.Sub(from) > maxReportPeriod {
		err = fmt.Errorf("the period may span at most %d days", int(maxReportPeriod/(24*time.Hour)))
		return
	}
	if !horizon.IsZero() && from.Before(horizon) {
		from, clipped = horizon, true
		if !from.Before(to) {
			err = fmt.Errorf("the period ends before the retention horizon %s", horizon.UTC().Format(time.RFC3339))
		}
	}
	return
}

func parseTarget(r *http.Request) (*float64, error) {
	v := strings.TrimSpace(r.URL.Query().Get("target"))
	if v == "" {
		return nil, nil
	}
	t, err := strconv.ParseFloat(v, 64)
	if err != nil || !(t > 0 && t <= 100) {
		return nil, fmt.Errorf("target must be a percentage greater than 0 and at most 100, e.g. 99.9")
	}
	return &t, nil
}

// SLAResponse is the body of GET /api/v1/reports/sla.
type SLAResponse struct {
	From             time.Time    `json:"from"`
	To               time.Time    `json:"to"`
	GeneratedAt      time.Time    `json:"generated_at"`
	TargetPercent    *float64     `json:"target_percent"`
	RetentionHorizon *time.Time   `json:"retention_horizon"`
	Clipped          bool         `json:"clipped"`
	Summary          sla.Report   `json:"summary"`
	Souls            []sla.Report `json:"souls"`
}

// buildSLAReport computes the SLA response for the request's workspace, or
// for one soul of it with soul_id. The returned status is the HTTP status of
// a failure.
func (s *RESTServer) buildSLAReport(ctx *Context) (*SLAResponse, int, error) {
	// The report stops when the client goes away or its time budget runs out.
	rctx, cancel := context.WithTimeout(ctx.Request.Context(), slaReportBudget)
	defer cancel()
	now := time.Now().UTC()
	rawSince, hourSince, oldest := s.reportHorizons(now)
	from, to, clipped, err := parseReportRange(ctx.Request, now, oldest)
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	target, err := parseTarget(ctx.Request)
	if err != nil {
		return nil, http.StatusBadRequest, err
	}

	var souls []*core.Soul
	soulFilter := strings.TrimSpace(ctx.Request.URL.Query().Get("soul_id"))
	if soulFilter != "" {
		soul, err := s.store.GetSoulNoCtx(soulFilter)
		// A soul of another workspace answers as missing: its ID says nothing.
		if err != nil || soul == nil || defaultWorkspaceID(soul.WorkspaceID) != defaultWorkspaceID(ctx.Workspace) {
			return nil, http.StatusNotFound, fmt.Errorf("soul not found")
		}
		souls = []*core.Soul{soul}
	} else {
		all, err := s.store.ListSoulsNoCtx(ctx.Workspace, 0, 0)
		if err != nil {
			return nil, http.StatusInternalServerError, err
		}
		for _, soul := range all {
			if soul != nil && defaultWorkspaceID(soul.WorkspaceID) == defaultWorkspaceID(ctx.Workspace) {
				souls = append(souls, soul)
			}
		}
		sort.Slice(souls, func(i, j int) bool {
			if souls[i].Name != souls[j].Name {
				return souls[i].Name < souls[j].Name
			}
			return souls[i].ID < souls[j].ID
		})
	}

	windows, err := s.store.ListMaintenanceWindows()
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	var incidents []*core.Incident
	if s.alert != nil {
		incidents = s.alert.ListIncidents()
	}
	inWorkspace := func(inc *core.Incident) bool {
		return defaultWorkspaceID(inc.WorkspaceID) == defaultWorkspaceID(ctx.Workspace)
	}

	resp := &SLAResponse{
		From: from, To: to, GeneratedAt: now, TargetPercent: target, Clipped: clipped,
		Souls: make([]sla.Report, 0, len(souls)),
	}
	if !oldest.IsZero() {
		h := oldest
		resp.RetentionHorizon = &h
	}

	total := &sla.Tally{}
	var totalPeriod, totalExcluded float64
	period := to.Sub(from).Seconds()
	for _, soul := range souls {
		excluded := sla.MaintenanceRanges(windows, soul, from, to)
		tally, err := s.collectSLA(rctx, soul, from, to, excluded, rawSince, hourSince)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return nil, http.StatusServiceUnavailable, fmt.Errorf("the SLA report exceeded its %s time budget; pick a shorter period or one soul", slaReportBudget)
			}
			return nil, http.StatusInternalServerError, err
		}
		excludedSeconds := sla.Total(excluded).Seconds()
		report := sla.Build(tally, period, excludedSeconds, target)
		report.SoulID, report.SoulName, report.SoulType = soul.ID, soul.Name, string(soul.Type)
		soulID := soul.ID
		report.SetIncidents(sla.IncidentStats(incidents, from, to, func(inc *core.Incident) bool {
			return inWorkspace(inc) && inc.SoulID == soulID
		}))
		resp.Souls = append(resp.Souls, report)
		total.Merge(tally)
		totalPeriod += period
		totalExcluded += excludedSeconds
	}

	resp.Summary = sla.Build(total, totalPeriod, totalExcluded, target)
	if soulFilter != "" {
		resp.Summary.SetIncidents(resp.Souls[0].Incidents, resp.Souls[0].MTTRSeconds)
	} else {
		resp.Summary.SetIncidents(sla.IncidentStats(incidents, from, to, inWorkspace))
	}
	return resp, http.StatusOK, nil
}

func defaultWorkspaceID(ws string) string {
	if ws == "" {
		return "default"
	}
	return ws
}

// collectSLA tallies one soul, from rollups when the store supports it.
func (s *RESTServer) collectSLA(ctx context.Context, soul *core.Soul, from, to time.Time, excluded []core.TimeRange, rawSince, hourSince time.Time) (*sla.Tally, error) {
	if c, ok := s.store.(slaCollector); ok {
		return c.CollectSLA(ctx, soul.WorkspaceID, soul.ID, from, to, excluded, rawSince, hourSince)
	}
	tally := &sla.Tally{}
	start := from
	if !rawSince.IsZero() && start.Before(rawSince) {
		start = rawSince
	}
	for a := start; a.Before(to); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		b := a.Add(24 * time.Hour)
		if b.After(to) {
			b = to
		}
		// ListJudgmentsNoCtx excludes its start bound; pieces include theirs.
		judgments, err := s.store.ListJudgmentsNoCtx(soul.ID, a.Add(-time.Nanosecond), b, 0)
		if err != nil {
			return nil, err
		}
		tally.CountJudgments(sla.RawPieces(a, b, excluded), judgments)
		a = b
	}
	return tally, nil
}

func (s *RESTServer) handleSLAReport(ctx *Context) error {
	resp, status, err := s.buildSLAReport(ctx)
	if err != nil {
		if status == http.StatusInternalServerError {
			return s.internalError(ctx, err, "failed to compute SLA report")
		}
		return ctx.Error(status, err.Error())
	}
	return ctx.JSON(http.StatusOK, resp)
}

// csvSafe neutralises spreadsheet formula injection: a text cell starting
// with = + - @ (or a tab or carriage return, which some spreadsheets strip
// before evaluating) is prefixed with a single quote. Apply it to every cell
// whose text comes from users or monitored targets, never to numbers.
func csvSafe(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

var unsafeFilenameChars = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// attachment sets a Content-Disposition naming a download; name is reduced
// to a safe charset so it cannot break out of the header.
func attachment(w http.ResponseWriter, name string) {
	name = strings.Trim(unsafeFilenameChars.ReplaceAllString(name, "_"), "_.")
	if name == "" {
		name = "export"
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
}

func formatOptional(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

func formatFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

var slaCSVHeader = []string{
	"soul_id", "soul_name", "soul_type", "from", "to", "availability_percent", "checks", "failed",
	"downtime_seconds", "excluded_seconds", "monitored_seconds", "no_data_seconds", "incidents", "mttr_seconds",
	"latency_avg_ms", "latency_p50_ms", "latency_p95_ms", "latency_p99_ms", "latency_exact",
	"target_percent", "error_budget_remaining_percent", "error_budget_remaining_seconds", "slo_met",
}

func slaCSVRow(r sla.Report, from, to time.Time) []string {
	row := []string{
		csvSafe(r.SoulID), csvSafe(r.SoulName), csvSafe(r.SoulType),
		from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339),
		formatOptional(r.AvailabilityPercent), strconv.Itoa(r.Checks), strconv.Itoa(r.Failed),
		formatFloat(r.DowntimeSeconds), formatFloat(r.ExcludedSeconds), formatFloat(r.MonitoredSeconds),
		formatFloat(r.NoDataSeconds), strconv.Itoa(r.Incidents), formatOptional(r.MTTRSeconds),
	}
	if l := r.Latency; l != nil {
		row = append(row, formatFloat(l.AvgMs), formatFloat(l.P50Ms), formatFloat(l.P95Ms), formatFloat(l.P99Ms), strconv.FormatBool(l.Exact))
	} else {
		row = append(row, "", "", "", "", "")
	}
	if b := r.ErrorBudget; b != nil {
		row = append(row, formatFloat(b.TargetPercent), formatOptional(b.RemainingPercent), formatFloat(b.RemainingDowntimeSeconds), strconv.FormatBool(b.Met))
	} else {
		row = append(row, "", "", "", "")
	}
	return row
}

// handleSLAReportCSV serves the SLA report as CSV: one row per soul, then a
// workspace total row whose soul_id is empty and soul_name "(total)".
func (s *RESTServer) handleSLAReportCSV(ctx *Context) error {
	resp, status, err := s.buildSLAReport(ctx)
	if err != nil {
		if status == http.StatusInternalServerError {
			return s.internalError(ctx, err, "failed to compute SLA report")
		}
		return ctx.Error(status, err.Error())
	}
	w := ctx.Response
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	attachment(w, fmt.Sprintf("anubis-sla-%s-%s.csv", resp.From.UTC().Format("20060102"), resp.To.UTC().Format("20060102")))
	w.WriteHeader(http.StatusOK)
	cw := csv.NewWriter(w)
	_ = cw.Write(slaCSVHeader)
	for _, r := range resp.Souls {
		_ = cw.Write(slaCSVRow(r, resp.From, resp.To))
	}
	total := resp.Summary
	total.SoulID, total.SoulName, total.SoulType = "", "(total)", ""
	_ = cw.Write(slaCSVRow(total, resp.From, resp.To))
	cw.Flush()
	return cw.Error()
}

// exportRecord is one exported judgment. Protocol details (response bodies,
// headers, TLS data) are left out: an export is a check history, not a dump.
type exportRecord struct {
	Timestamp  time.Time `json:"timestamp"`
	ID         string    `json:"id"`
	SoulID     string    `json:"soul_id"`
	Status     string    `json:"status"`
	DurationMs float64   `json:"duration_ms"`
	StatusCode int       `json:"status_code"`
	Region     string    `json:"region,omitempty"`
	JackalID   string    `json:"jackal_id,omitempty"`
	Message    string    `json:"message,omitempty"`
}

var exportCSVHeader = []string{"timestamp", "id", "soul_id", "status", "duration_ms", "status_code", "region", "jackal_id", "message"}

func toExportRecord(j *core.Judgment) exportRecord {
	return exportRecord{
		Timestamp: j.Timestamp.UTC(), ID: j.ID, SoulID: j.SoulID, Status: string(j.Status),
		DurationMs: float64(j.Duration) / float64(time.Millisecond), StatusCode: j.StatusCode,
		Region: j.Region, JackalID: j.JackalID, Message: j.Message,
	}
}

func (r exportRecord) csvRow() []string {
	return []string{
		r.Timestamp.Format(time.RFC3339Nano), csvSafe(r.ID), csvSafe(r.SoulID), csvSafe(r.Status),
		formatFloat(r.DurationMs), strconv.Itoa(r.StatusCode), csvSafe(r.Region), csvSafe(r.JackalID), csvSafe(r.Message),
	}
}

// handleExportJudgments streams a soul's judgments in [from, to), oldest
// first, as CSV or NDJSON. The period is clipped to raw retention, and at most
// limit rows are sent; the X-Export-Truncated trailer says whether more
// remained (continue from the last timestamp).
func (s *RESTServer) handleExportJudgments(ctx *Context) error {
	soulID := ctx.Params["id"]
	soul, err := s.store.GetSoulNoCtx(soulID)
	if err != nil || soul == nil || defaultWorkspaceID(soul.WorkspaceID) != defaultWorkspaceID(ctx.Workspace) {
		return ctx.Error(http.StatusNotFound, "soul not found")
	}
	q := ctx.Request.URL.Query()
	format := strings.ToLower(strings.TrimSpace(q.Get("format")))
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "ndjson" {
		return ctx.Error(http.StatusBadRequest, "format must be csv or ndjson")
	}
	limit := defaultExportLimit
	if v := strings.TrimSpace(q.Get("limit")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxExportLimit {
			return ctx.Error(http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", maxExportLimit))
		}
		limit = n
	}
	now := time.Now().UTC()
	rawSince, _, _ := s.reportHorizons(now)
	from, to, clipped, err := parseReportRange(ctx.Request, now, rawSince)
	if err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}
	user := "anonymous"
	if ctx.User != nil && ctx.User.ID != "" {
		user = ctx.User.ID
	}
	if !s.exports.acquire(user) {
		ctx.Response.Header().Set("Retry-After", "30")
		return ctx.Error(http.StatusTooManyRequests, fmt.Sprintf("too many exports running (at most %d per user, %d per server); retry when one finishes", maxExportsPerUser, maxExportsTotal))
	}
	defer s.exports.release(user)
	sctx, cancel := context.WithTimeout(ctx.Request.Context(), exportMaxDuration)
	defer cancel()

	w := ctx.Response
	h := w.Header()
	if format == "csv" {
		h.Set("Content-Type", "text/csv; charset=utf-8")
	} else {
		h.Set("Content-Type", "application/x-ndjson")
	}
	attachment(w, fmt.Sprintf("anubis-judgments-%s-%s-%s.%s", soul.ID, from.Format("20060102T150405Z"), to.Format("20060102T150405Z"), format))
	h.Set("X-Export-From", from.Format(time.RFC3339Nano))
	h.Set("X-Export-To", to.Format(time.RFC3339Nano))
	h.Set("X-Export-Clipped", strconv.FormatBool(clipped))
	h.Set("Trailer", "X-Export-Truncated, X-Export-Rows")
	w.WriteHeader(http.StatusOK)

	// Every buffered write must reach the client within exportIdleTimeout.
	out := newIdleDeadlineWriter(w, exportIdleTimeout)
	buf := bufio.NewWriterSize(out, exportBufferSize)
	var write func(exportRecord) error
	var flush func() error
	if format == "csv" {
		cw := csv.NewWriter(buf)
		_ = cw.Write(exportCSVHeader)
		write = func(r exportRecord) error { return cw.Write(r.csvRow()) }
		flush = func() error { cw.Flush(); return errors.Join(cw.Error(), buf.Flush()) }
	} else {
		enc := json.NewEncoder(buf)
		write = func(r exportRecord) error { return enc.Encode(r) }
		flush = buf.Flush
	}

	rows := 0
	emit := func(j *core.Judgment) error {
		if j == nil {
			return nil
		}
		if err := write(toExportRecord(j)); err != nil {
			return err
		}
		rows++
		if rows%1000 == 0 {
			if err := flush(); err != nil {
				return err
			}
			out.Flush()
		}
		return nil
	}
	truncated, err := s.streamJudgments(sctx, soul, from, to, limit, emit)
	if ferr := flush(); err == nil {
		err = ferr
	}
	h.Set("X-Export-Truncated", strconv.FormatBool(truncated))
	h.Set("X-Export-Rows", strconv.Itoa(rows))
	if err != nil && err != io.EOF {
		// Headers are gone; the body just ends early.
		s.logger.Warn("judgment export ended early", "soul_id", soul.ID, "rows", rows, "error", err)
	}
	return nil
}

// streamJudgments passes the soul's judgments in [from, to) oldest first to
// fn, at most limit of them, and reports whether more remained.
func (s *RESTServer) streamJudgments(ctx context.Context, soul *core.Soul, from, to time.Time, limit int, fn func(*core.Judgment) error) (bool, error) {
	if st, ok := s.store.(judgmentStreamer); ok {
		_, truncated, err := st.StreamJudgments(ctx, soul.WorkspaceID, soul.ID, from, to, limit, fn)
		return truncated, err
	}
	sent := 0
	for a := from; a.Before(to); {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		b := a.Truncate(time.Hour).Add(time.Hour)
		if b.After(to) {
			b = to
		}
		judgments, err := s.store.ListJudgmentsNoCtx(soul.ID, a.Add(-time.Nanosecond), b, 0)
		if err != nil {
			return false, err
		}
		for i := len(judgments) - 1; i >= 0; i-- { // newest first -> oldest first
			j := judgments[i]
			if j == nil || j.Timestamp.Before(a) || !j.Timestamp.Before(b) {
				continue
			}
			if sent >= limit {
				return true, nil
			}
			if err := fn(j); err != nil {
				return false, err
			}
			sent++
		}
		a = b
	}
	return false, nil
}
