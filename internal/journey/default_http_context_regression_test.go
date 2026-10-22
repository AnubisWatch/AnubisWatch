package journey

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestJourneyDefaultHTTPConfigKeepsSharedContext(t *testing.T) {
	cases := []struct {
		name, feature                 string
		firstExplicit, secondExplicit bool
	}{
		{"default_extraction", "extract", false, false},
		{"explicit_extraction_control", "extract", true, false},
		{"default_assertion", "assert", false, false},
		{"explicit_assertion_control", "assert", true, false},
		{"default_cookie_steps", "cookie", false, false},
		{"default_cookie_login", "cookie", false, true},
		{"default_cookie_verify", "cookie", true, false},
		{"explicit_cookie_control", "cookie", true, true},
		{"default_status_policy_control", "bad_status", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					http.Error(w, "expected GET", 405)
					return
				}
				if r.URL.Path == "/verify" {
					cookie, err := r.Cookie("journey_session")
					if err != nil || cookie.Value != "session-value" {
						http.Error(w, "journey cookie missing", 401)
						return
					}
					_, _ = io.WriteString(w, "authorized")
					return
				}
				http.SetCookie(w, &http.Cookie{Name: "journey_session", Value: "session-value", Path: "/"})
				w.Header().Set("Content-Type", "application/json")
				if tc.feature == "bad_status" {
					w.WriteHeader(500)
				}
				_, _ = io.WriteString(w, `{"id":"resource-42"}`)
			}))
			defer srv.Close()
			config := func(explicit bool) *core.HTTPConfig {
				if !explicit {
					return nil
				}
				return &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}}
			}
			first := core.JourneyStep{Name: "login", Type: core.CheckHTTP, Target: srv.URL + "/login", HTTP: config(tc.firstExplicit), Timeout: core.Duration{Duration: time.Second}}
			if tc.feature == "extract" {
				first.Extract = map[string]core.ExtractionRule{"id": {From: "body", Path: "$.id"}, "session": {From: "cookie", Path: "journey_session"}}
			}
			if tc.feature == "assert" {
				first.Assertions = []core.Assertion{{Type: "json_path", Target: "$.id", Operator: "equals", Expected: "resource-42"}}
			}
			j := &core.JourneyConfig{ID: "default-http", WorkspaceID: "default", Steps: []core.JourneyStep{first}}
			if tc.feature == "cookie" {
				j.Steps = append(j.Steps, core.JourneyStep{Name: "verify", Type: core.CheckHTTP, Target: srv.URL + "/verify", HTTP: config(tc.secondExplicit), Timeout: core.Duration{Duration: time.Second}})
			}
			db := newTestDB(t)
			defer db.Close()
			e := NewExecutor(db, newTestLogger())
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			run, err := e.RunOnce(ctx, j)
			if err != nil {
				t.Fatal(err)
			}
			wantStatus := core.SoulAlive
			if tc.feature == "bad_status" {
				wantStatus = core.SoulDead
			}
			if run.Status != wantStatus {
				t.Errorf("FAIL: %s status=%s, want %s; steps=%+v", tc.feature, run.Status, wantStatus, run.Steps)
			}
			if tc.feature == "extract" {
				for name, want := range map[string]string{"id": "resource-42", "session": "session-value"} {
					if run.Variables[name] != want {
						t.Errorf("FAIL: captured %s=%q, want %q with optional HTTP config", name, run.Variables[name], want)
					}
				}
			}
			for i, step := range j.Steps {
				explicit := tc.firstExplicit
				if i == 1 {
					explicit = tc.secondExplicit
				}
				if !explicit && step.HTTP != nil {
					t.Error("caller configuration was mutated")
				}
				if step.HTTP != nil && step.HTTP.CookieJar != nil {
					t.Error("caller configuration retained execution cookie jar")
				}
			}
			records, err := e.ListRuns(ctx, "default", j.ID, 10)
			if err != nil || len(records) != 1 {
				t.Errorf("run persistence: count=%d err=%v", len(records), err)
			}
		})
	}
}
