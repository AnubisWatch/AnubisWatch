package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestParseCLITime(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in      string
		want    time.Time
		wantErr bool
	}{
		{"2026-10-01T00:00:00Z", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), false},
		{"2026-10-01", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), false},
		{"30d", now.Add(-30 * 24 * time.Hour), false},
		{"12h", now.Add(-12 * time.Hour), false},
		{"90m", now.Add(-90 * time.Minute), false},
		{"-5d", time.Time{}, true},
		{"yesterday", time.Time{}, true},
	}
	for _, tc := range cases {
		got, err := parseCLITime(tc.in, now)
		if (err != nil) != tc.wantErr || (!tc.wantErr && !got.Equal(tc.want)) {
			t.Errorf("parseCLITime(%q) = %v, %v; want %v (err %v)", tc.in, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestReportAndExportCommandsRejectBadInput(t *testing.T) {
	cases := []struct {
		name string
		run  func() error
		want string
	}{
		{"export without soul", func() error { return runExportJudgments(nil, "http://x", "tok", &bytes.Buffer{}, &bytes.Buffer{}) }, "--soul is required"},
		{"export bad format", func() error {
			return runExportJudgments([]string{"--soul", "a", "--format", "xml"}, "http://x", "tok", &bytes.Buffer{}, &bytes.Buffer{})
		}, "csv or ndjson"},
		{"export not logged in", func() error {
			return runExportJudgments([]string{"--soul", "a"}, "http://x", "", &bytes.Buffer{}, &bytes.Buffer{})
		}, "not logged in"},
		{"export bad from", func() error {
			return runExportJudgments([]string{"--soul", "a", "--from", "soon"}, "http://x", "tok", &bytes.Buffer{}, &bytes.Buffer{})
		}, "--from"},
		{"report unknown", func() error { return runReportCommand([]string{"uptime"}, "http://x", "tok", &bytes.Buffer{}) }, "unknown report"},
		{"report bad format", func() error {
			return runReportCommand([]string{"sla", "--format", "xml"}, "http://x", "tok", &bytes.Buffer{})
		}, "--format"},
		{"report not logged in", func() error { return runReportCommand([]string{"sla"}, "http://x", "", &bytes.Buffer{}) }, "not logged in"},
	}
	for _, tc := range cases {
		if err := tc.run(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
	var help bytes.Buffer
	if err := runReportCommand(nil, "", "", &help); err != nil || !strings.Contains(help.String(), "anubis report sla") {
		t.Fatalf("report help: %v %q", err, help.String())
	}
}

// TestReportAndExportEndToEnd drives the real server and store through the
// CLI with an API key: the SLA report reads the soul's rollups, and the
// export streams its judgments, neutralising formula cells.
func TestReportAndExportEndToEnd(t *testing.T) {
	const email, password = "admin@example.com", "TestPass1234!"
	dataDir := filepath.Join(t.TempDir(), "data")
	port := freeLoopbackPort(t)
	srv := bootAuthServer(t, writeServerConfig(t, dataDir, port, email, password), port)
	defer srv.stop(t)
	admin, _ := loginAsAdmin(t, srv.baseURL, email, password)

	code, body := apiKeyRequest(t, admin, http.MethodPost, srv.baseURL+"/api/v1/api-keys", "", `{"name":"reports","role":"viewer"}`)
	if code != http.StatusCreated {
		t.Fatalf("create key: %d %s", code, body)
	}
	var key struct {
		Key string `json:"key"`
	}
	_ = json.Unmarshal([]byte(body), &key)

	ctx := context.Background()
	soul := &core.Soul{ID: "report-soul", Name: "Report Soul", Type: core.CheckHTTP, Target: "https://example.com", WorkspaceID: "default"}
	if err := srv.deps.Store.SaveSoul(ctx, soul); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-3 * time.Hour).Truncate(time.Minute)
	for i := 0; i < 40; i++ {
		status, msg := core.SoulAlive, "ok"
		if i < 4 {
			status, msg = core.SoulDead, "=HYPERLINK(\"http://evil\")"
		}
		j := &core.Judgment{ID: fmt.Sprintf("rj-%02d", i), SoulID: soul.ID, WorkspaceID: "default", Status: status, Message: msg,
			Duration: time.Duration(20+i) * time.Millisecond, Timestamp: start.Add(time.Duration(i) * time.Minute)}
		if err := srv.deps.TimeSeries.SaveJudgment(ctx, j); err != nil {
			t.Fatal(err)
		}
	}

	var out bytes.Buffer
	if err := runReportCommand([]string{"sla", "--from", "1d", "--format", "json", "--target", "99.9"}, srv.baseURL, key.Key, &out); err != nil {
		t.Fatalf("report json: %v", err)
	}
	var report struct {
		Souls []struct {
			SoulID      string `json:"soul_id"`
			Checks      int    `json:"checks"`
			Failed      int    `json:"failed"`
			ErrorBudget *struct {
				Met bool `json:"met"`
			} `json:"error_budget"`
		} `json:"souls"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode %q: %v", out.String(), err)
	}
	if len(report.Souls) != 1 || report.Souls[0].Checks != 40 || report.Souls[0].Failed != 4 || report.Souls[0].ErrorBudget == nil {
		t.Fatalf("report = %+v", report)
	}

	out.Reset()
	if err := runReportCommand([]string{"sla", "--from", "1d", "--soul", soul.ID}, srv.baseURL, key.Key, &out); err != nil {
		t.Fatalf("report table: %v", err)
	}
	if !strings.Contains(out.String(), "Report Soul") || !strings.Contains(out.String(), "90.000%") {
		t.Fatalf("table = %q", out.String())
	}

	out.Reset()
	if err := runReportCommand([]string{"sla", "--from", "1d", "--format", "csv"}, srv.baseURL, key.Key, &out); err != nil {
		t.Fatalf("report csv: %v", err)
	}
	if !strings.HasPrefix(out.String(), "soul_id,soul_name,") || !strings.Contains(out.String(), "(total)") {
		t.Fatalf("csv report = %q", out.String())
	}

	out.Reset()
	var notes bytes.Buffer
	if err := runExportJudgments([]string{"--soul", soul.ID, "--from", "1d"}, srv.baseURL, key.Key, &out, &notes); err != nil {
		t.Fatalf("export csv: %v", err)
	}
	rows, err := csv.NewReader(&out).ReadAll()
	if err != nil || len(rows) != 41 {
		t.Fatalf("csv: %d rows, %v", len(rows), err)
	}
	if rows[1][8] != `'=HYPERLINK("http://evil")` || rows[1][1] != "rj-00" {
		t.Fatalf("first row = %v", rows[1])
	}

	out.Reset()
	notes.Reset()
	if err := runExportJudgments([]string{"--soul", soul.ID, "--from", "1d", "--format", "ndjson", "--limit", "10"}, srv.baseURL, key.Key, &out, &notes); err != nil {
		t.Fatalf("export ndjson: %v", err)
	}
	if lines := strings.Count(out.String(), "\n"); lines != 10 || !strings.Contains(notes.String(), "stopped after 10 rows") {
		t.Fatalf("ndjson: %d lines, notes %q", lines, notes.String())
	}

	if err := runExportJudgments([]string{"--soul", "missing"}, srv.baseURL, key.Key, &out, &notes); err == nil || !strings.Contains(err.Error(), "soul not found") {
		t.Fatalf("missing soul: %v", err)
	}
	if err := runReportCommand([]string{"sla"}, srv.baseURL, "anb_bogus", &out); err == nil {
		t.Fatal("bogus token accepted")
	}
}
