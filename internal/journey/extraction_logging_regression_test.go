package journey

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestJourneyExtractionLogsOmitResponseValues(t *testing.T) {
	// Known synthetic fixtures only: no real credentials are used by this test.
	values := map[string]string{
		"body_result":   "synthetic-response-body-fixture-19",
		"header_result": "synthetic-response-header-fixture-27",
		"cookie_result": "synthetic-response-cookie-fixture-38",
	}
	for _, format := range []string{"json", "text"} {
		for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo} {
			t.Run(format+"/"+level.String(), func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("Authorization", values["header_result"])
					http.SetCookie(w, &http.Cookie{Name: "session", Value: values["cookie_result"], Path: "/"})
					_ = json.NewEncoder(w).Encode(map[string]string{"value": values["body_result"]})
				}))
				defer srv.Close()
				var output bytes.Buffer
				opts := &slog.HandlerOptions{Level: level}
				var handler slog.Handler = slog.NewJSONHandler(&output, opts)
				if format == "text" {
					handler = slog.NewTextHandler(&output, opts)
				}
				logger := slog.New(handler)
				logger.Info("logging-control")
				db := newTestDB(t)
				defer db.Close()
				e := NewExecutor(db, logger)
				j := &core.JourneyConfig{ID: "private-extraction", WorkspaceID: "default", Steps: []core.JourneyStep{{
					Name: "extract", Type: core.CheckHTTP, Target: srv.URL, Timeout: core.Duration{Duration: time.Second},
					Extract: map[string]core.ExtractionRule{
						"body_result":   {From: "body", Path: "$.value"},
						"header_result": {From: "header", Path: "Authorization"},
						"cookie_result": {From: "cookie", Path: "session"},
					},
				}}}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				run, err := e.RunOnce(ctx, j)
				if err != nil {
					t.Fatal(err)
				}
				if run.Status != core.SoulAlive {
					t.Fatal("fixture HTTP step failed")
				}
				records, err := e.ListRuns(ctx, "default", j.ID, 10)
				if err != nil || len(records) != 1 {
					t.Fatalf("persistence: count=%d err=%v", len(records), err)
				}
				logText := output.String()
				if !strings.Contains(logText, "logging-control") {
					t.Fatal("fixture logger is not active")
				}
				for name, want := range values {
					if run.Variables[name] != want || records[0].Variables[name] != want {
						t.Errorf("extraction/persistence for %s was changed", name)
					}
					if strings.Contains(logText, want) {
						t.Errorf("FAIL: %s log exposed the extracted response value for %s", format, name)
					}
					if level == slog.LevelDebug && !strings.Contains(logText, name) {
						t.Errorf("diagnostic variable name %s is missing", name)
					}
				}
				if level == slog.LevelDebug && !strings.Contains(logText, "extracted variable") {
					t.Error("extraction diagnostic event is missing")
				}
				if level == slog.LevelInfo && strings.Contains(logText, "extracted variable") {
					t.Error("debug event escaped the info-level filter")
				}
			})
		}
	}
}
