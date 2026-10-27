package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

const reportUsage = `Usage: anubis report sla [options]

Show the SLA of every soul in the workspace (or of one) over a period:
availability, failed checks, downtime, incidents, MTTR, latency percentiles
and, with --target, the error budget. Maintenance windows are excluded time.

Options:
  --from T       Start: RFC3339, YYYY-MM-DD, or an age such as 30d / 12h (default 30d)
  --to T         End (default now)
  --target P     SLO target in percent, e.g. 99.9
  --soul ID      Only this soul
  --format F     table (default), json or csv

Uses the server from the config file and the token from "anubis login" or
ANUBIS_API_TOKEN.
`

const exportJudgmentsUsage = `Usage: anubis export judgments --soul ID [options]

Stream a soul's check results, oldest first, from the server.

Options:
  --soul ID      Soul to export (required)
  --from T       Start: RFC3339, YYYY-MM-DD, or an age such as 7d / 12h (default 30d)
  --to T         End (default now)
  --format F     csv (default) or ndjson
  --limit N      Maximum rows (server default 100000, max 1000000)
  --output FILE  Write to FILE instead of stdout
`

func reportCommand() {
	if err := runReportCommand(os.Args[2:], getAPIURL(), getAPIToken(), os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func exportJudgmentsCommand(args []string) {
	if err := runExportJudgments(args, getAPIURL(), getAPIToken(), os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// parseCLITime accepts RFC3339, a YYYY-MM-DD date (UTC) or an age before now
// such as 90d, 12h or 30m.
func parseCLITime(value string, now time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", value); err == nil {
		return t, nil
	}
	if n, err := strconv.Atoi(strings.TrimSuffix(value, "d")); err == nil && strings.HasSuffix(value, "d") && n >= 0 {
		return now.Add(-time.Duration(n) * 24 * time.Hour), nil
	}
	if d, err := time.ParseDuration(value); err == nil && d >= 0 {
		return now.Add(-d), nil
	}
	return time.Time{}, fmt.Errorf("invalid time %q: use RFC3339, YYYY-MM-DD or an age like 30d", value)
}

// rangeParams turns --from/--to into query parameters.
func rangeParams(q url.Values, from, to string) error {
	now := time.Now().UTC()
	for name, v := range map[string]string{"from": from, "to": to} {
		if strings.TrimSpace(v) == "" {
			continue
		}
		t, err := parseCLITime(v, now)
		if err != nil {
			return fmt.Errorf("--%s: %w", name, err)
		}
		q.Set(name, t.UTC().Format(time.RFC3339))
	}
	return nil
}

// apiStream issues an authenticated GET and returns the response when the
// status is 200, or the server's error message.
func apiStream(ctx context.Context, target, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusOK {
		return resp, nil
	}
	defer resp.Body.Close()
	var body struct {
		Error string `json:"error"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if json.Unmarshal(raw, &body) == nil && body.Error != "" {
		return nil, fmt.Errorf("server: %s (HTTP %d)", body.Error, resp.StatusCode)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errors.New("not authenticated; run `anubis login` or set ANUBIS_API_TOKEN")
	}
	return nil, fmt.Errorf("server returned HTTP %d", resp.StatusCode)
}

func runExportJudgments(args []string, apiURL, token string, out, errOut io.Writer) error {
	if len(args) > 0 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		fmt.Fprint(out, exportJudgmentsUsage)
		return nil
	}
	fs := flag.NewFlagSet("export judgments", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	soul := fs.String("soul", "", "soul ID")
	from := fs.String("from", "", "start")
	to := fs.String("to", "", "end")
	format := fs.String("format", "csv", "csv or ndjson")
	limit := fs.Int("limit", 0, "maximum rows")
	output := fs.String("output", "", "output file")
	fs.StringVar(output, "o", "", "output file")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w\n\n%s", err, exportJudgmentsUsage)
	}
	if strings.TrimSpace(*soul) == "" {
		return errors.New("--soul is required (see `anubis export judgments --help`)")
	}
	if *format != "csv" && *format != "ndjson" {
		return errors.New("--format must be csv or ndjson")
	}
	if token == "" {
		return errors.New("not logged in; run `anubis login` first or set ANUBIS_API_TOKEN")
	}
	q := url.Values{"format": {*format}}
	if err := rangeParams(q, *from, *to); err != nil {
		return err
	}
	if *limit > 0 {
		q.Set("limit", strconv.Itoa(*limit))
	}
	target := apiURL + "/api/v1/souls/" + url.PathEscape(*soul) + "/judgments/export?" + q.Encode()
	resp, err := apiStream(context.Background(), target, token)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	dst := out
	if *output != "" {
		f, err := os.OpenFile(*output, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		dst = f
	}
	if _, err := io.Copy(dst, resp.Body); err != nil {
		return fmt.Errorf("export interrupted: %w", err)
	}
	if resp.Header.Get("X-Export-Clipped") == "true" {
		fmt.Fprintf(errOut, "Note: the period was clipped to raw-judgment retention; it starts at %s.\n", resp.Header.Get("X-Export-From"))
	}
	if resp.Trailer.Get("X-Export-Truncated") == "true" {
		fmt.Fprintf(errOut, "Note: stopped after %s rows; continue with --from set to the last timestamp, or raise --limit.\n", resp.Trailer.Get("X-Export-Rows"))
	}
	return nil
}

// slaReportView is the part of the SLA response the CLI prints.
type slaReportView struct {
	SoulID              string   `json:"soul_id"`
	SoulName            string   `json:"soul_name"`
	Checks              int      `json:"checks"`
	Failed              int      `json:"failed"`
	AvailabilityPercent *float64 `json:"availability_percent"`
	DowntimeSeconds     float64  `json:"downtime_seconds"`
	Incidents           int      `json:"incidents"`
	MTTRSeconds         *float64 `json:"mttr_seconds"`
	Latency             *struct {
		P95Ms float64 `json:"p95_ms"`
	} `json:"latency"`
	ErrorBudget *struct {
		RemainingPercent *float64 `json:"remaining_percent"`
		Met              bool     `json:"met"`
	} `json:"error_budget"`
}

func runReportCommand(args []string, apiURL, token string, out io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(out, reportUsage)
		return nil
	}
	if args[0] != "sla" {
		return fmt.Errorf("unknown report %q (sla)", args[0])
	}
	fs := flag.NewFlagSet("report sla", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	from := fs.String("from", "", "start")
	to := fs.String("to", "", "end")
	target := fs.String("target", "", "SLO target percent")
	soul := fs.String("soul", "", "soul ID")
	format := fs.String("format", "table", "table, json or csv")
	if err := fs.Parse(args[1:]); err != nil {
		return fmt.Errorf("%w\n\n%s", err, reportUsage)
	}
	if *format != "table" && *format != "json" && *format != "csv" {
		return errors.New("--format must be table, json or csv")
	}
	if token == "" {
		return errors.New("not logged in; run `anubis login` first or set ANUBIS_API_TOKEN")
	}
	q := url.Values{}
	if err := rangeParams(q, *from, *to); err != nil {
		return err
	}
	if *target != "" {
		q.Set("target", *target)
	}
	if *soul != "" {
		q.Set("soul_id", *soul)
	}
	path := "/api/v1/reports/sla"
	if *format == "csv" {
		path += ".csv"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	resp, err := apiStream(ctx, apiURL+path+"?"+q.Encode(), token)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if *format != "table" {
		_, err := io.Copy(out, resp.Body)
		return err
	}

	var report struct {
		From          time.Time       `json:"from"`
		To            time.Time       `json:"to"`
		TargetPercent *float64        `json:"target_percent"`
		Clipped       bool            `json:"clipped"`
		Summary       slaReportView   `json:"summary"`
		Souls         []slaReportView `json:"souls"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return fmt.Errorf("decode report: %w", err)
	}
	fmt.Fprintf(out, "SLA %s → %s", report.From.Local().Format("2006-01-02 15:04"), report.To.Local().Format("2006-01-02 15:04"))
	if report.TargetPercent != nil {
		fmt.Fprintf(out, "  (target %s%%)", strconv.FormatFloat(*report.TargetPercent, 'f', -1, 64))
	}
	fmt.Fprintln(out)
	if report.Clipped {
		fmt.Fprintln(out, "Note: the period was clipped to what retention keeps.")
	}
	fmt.Fprintln(out)
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "SOUL\tAVAILABILITY\tCHECKS\tFAILED\tDOWNTIME\tINCIDENTS\tMTTR\tP95\tBUDGET LEFT")
	row := func(name string, r slaReportView) {
		avail, p95, mttr, budget := "no data", "-", "-", "-"
		if r.AvailabilityPercent != nil {
			avail = strconv.FormatFloat(*r.AvailabilityPercent, 'f', 3, 64) + "%"
		}
		if r.Latency != nil {
			p95 = strconv.FormatFloat(r.Latency.P95Ms, 'f', 0, 64) + "ms"
		}
		if r.MTTRSeconds != nil {
			mttr = (time.Duration(*r.MTTRSeconds) * time.Second).String()
		}
		if r.ErrorBudget != nil && r.ErrorBudget.RemainingPercent != nil {
			budget = strconv.FormatFloat(*r.ErrorBudget.RemainingPercent, 'f', 1, 64) + "%"
			if !r.ErrorBudget.Met {
				budget += " (missed)"
			}
		}
		downtime := (time.Duration(r.DowntimeSeconds) * time.Second).String()
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%s\t%d\t%s\t%s\t%s\n", name, avail, r.Checks, r.Failed, downtime, r.Incidents, mttr, p95, budget)
	}
	for _, s := range report.Souls {
		name := s.SoulName
		if name == "" {
			name = s.SoulID
		}
		row(truncate(name, 32), s)
	}
	if len(report.Souls) != 1 {
		row("TOTAL", report.Summary)
	}
	return tw.Flush()
}
