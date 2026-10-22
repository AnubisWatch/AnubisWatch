package journey

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestJourneyInterpolationTreatsCapturedValuesLiterally(t *testing.T) {
	cases := []struct {
		name           string
		values         map[string]string
		template, want string
	}{
		{"cyclic_literal_data", map[string]string{"a": "${b}", "b": "${a}"}, "${a}|${b}", "${b}|${a}"},
		{"cyclic_repeated_tokens", map[string]string{"a": "${b}", "b": "${a}"}, "${a}|${b}|${a}", "${b}|${a}|${b}"},
		{"plain_control", map[string]string{"a": "alpha", "b": "beta"}, "${a}|${b}", "alpha|beta"},
		{"unknown_placeholder_control", map[string]string{"a": "alpha"}, "${a}|${missing}", "alpha|${missing}"},
		{"literal_characters_control", map[string]string{"a": "$1\\path", "b": "λ"}, "${a}|${b}", "$1\\path|λ"},
		{"empty_map_control", nil, "static ${missing}", "static ${missing}"},
		{"literal_names_control", map[string]string{"a+b": "alpha", "λ": "beta"}, "${a+b}|${λ}", "alpha|beta"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sent := make(chan string, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/values" {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(tc.values)
					return
				}
				if r.URL.Path != "/capture" || r.Method != http.MethodPost {
					http.Error(w, "invalid fixture request", 400)
					return
				}
				body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
				if err != nil {
					http.Error(w, "read failed", 400)
					return
				}
				sent <- string(body)
				if string(body) != tc.want {
					http.Error(w, "substituted values changed", 422)
					return
				}
				w.WriteHeader(200)
			}))
			defer srv.Close()
			extract := make(map[string]core.ExtractionRule)
			for name := range tc.values {
				extract[name] = core.ExtractionRule{From: "body", Path: "$." + name}
			}
			j := &core.JourneyConfig{ID: "literal-interpolation", WorkspaceID: "default", Steps: []core.JourneyStep{
				{Name: "capture", Type: core.CheckHTTP, Target: srv.URL + "/values", Timeout: core.Duration{Duration: time.Second}, Extract: extract},
				{Name: "send", Type: core.CheckHTTP, Target: srv.URL + "/capture", Timeout: core.Duration{Duration: time.Second}, HTTP: &core.HTTPConfig{Method: "POST", Body: tc.template, ValidStatus: []int{200}}},
			}}
			db := newTestDB(t)
			defer db.Close()
			e := NewExecutor(db, newTestLogger())
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			run, err := e.RunOnce(ctx, j)
			if err != nil {
				t.Fatal(err)
			}
			for name, want := range tc.values {
				if run.Variables[name] != want {
					t.Errorf("fixture captured value %s was changed", name)
				}
			}
			select {
			case got := <-sent:
				if got != tc.want {
					t.Errorf("FAIL: request body=%q, want literal substitutions %q", got, tc.want)
				}
			default:
				t.Fatal("fixture capture request was not reached")
			}
			if run.Status != core.SoulAlive {
				t.Errorf("FAIL: valid literal-data journey status=%s, want alive; steps=%+v", run.Status, run.Steps)
			}
			records, err := e.ListRuns(ctx, "default", j.ID, 10)
			if err != nil || len(records) != 1 {
				t.Fatalf("persistence: count=%d err=%v", len(records), err)
			}
			if records[0].Status != core.SoulAlive {
				t.Error("FAIL: corrupted request persisted as failed run")
			}
		})
	}
}
