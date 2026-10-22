package journey

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// An HTTP check with no valid_status configured must default to 200 only.
//
// The platform enforces that rule everywhere else: core.Soul.ApplyDefaults
// sets ValidStatus to []int{200} when it is empty, so a soul created through
// the REST, MCP or gRPC API treats an omitted valid_status as 200-only, and
// docs/CONFIGURATION.md documents the default as [200-299].
//
// The journey executor copied a user-supplied HTTP block verbatim, so a
// journey step that omitted valid_status reached the probe with an empty
// list. The probe only evaluates a status assertion when the list is
// non-empty, so ANY status code passed -- a 500 was recorded as a healthy
// step and a healthy journey.

func TestJourneyHTTPValidStatusDefaultsTo200(t *testing.T) {
	cases := []struct {
		name       string
		http       *core.HTTPConfig
		serverCode int
		want       core.SoulStatus
	}{
		{
			// The defect: valid_status omitted -> a 500 must not be healthy.
			name: "omitted_valid_status_rejects_500", http: &core.HTTPConfig{Method: "GET"}, serverCode: http.StatusInternalServerError,
			want: core.SoulDead,
		},
		{
			// The defect, mirrored on the response-code dimension: a 404 is
			// not a valid status either.
			name: "omitted_valid_status_rejects_404", http: &core.HTTPConfig{Method: "GET"}, serverCode: http.StatusNotFound,
			want: core.SoulDead,
		},
		{
			// Neighbouring control: an explicitly configured list is honoured.
			name: "explicit_valid_status_control", http: &core.HTTPConfig{Method: "GET", ValidStatus: []int{500}}, serverCode: http.StatusInternalServerError,
			want: core.SoulAlive,
		},
		{
			// Neighbouring control: the default path (no HTTP block at all)
			// already rejects a 500.
			name: "no_http_block_control", http: nil, serverCode: http.StatusInternalServerError,
			want: core.SoulDead,
		},
		{
			// Neighbouring control: a 200 stays healthy.
			name: "ok_response_control", http: &core.HTTPConfig{Method: "GET"}, serverCode: http.StatusOK,
			want: core.SoulAlive,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.serverCode)
			}))
			defer srv.Close()
			j := &core.JourneyConfig{ID: "valid-status", WorkspaceID: "default", Steps: []core.JourneyStep{{
				Name: "check", Type: core.CheckHTTP, Target: srv.URL, Timeout: core.Duration{Duration: 5 * time.Second},
				HTTP: tc.http,
			}}}
			db := newTestDB(t)
			defer db.Close()
			e := NewExecutor(db, newTestLogger())
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			run, err := e.RunOnce(ctx, j)
			if err != nil {
				t.Fatal(err)
			}
			if run.Steps[0].Status != tc.want {
				t.Errorf("FAIL: HTTP %d with valid_status=%v gave step status=%s, want %s (message=%q)",
					tc.serverCode, tc.http.ValidStatus, run.Steps[0].Status, tc.want, run.Steps[0].Message)
			}
			if run.Status != tc.want {
				t.Errorf("FAIL: HTTP %d with valid_status=%v gave journey status=%s, want %s",
					tc.serverCode, tc.http.ValidStatus, run.Status, tc.want)
			}
			records, err := e.ListRuns(ctx, "default", j.ID, 10)
			if err != nil || len(records) != 1 {
				t.Fatalf("persistence: count=%d err=%v", len(records), err)
			}
			if records[0].Status != tc.want {
				t.Errorf("FAIL: stored run status=%s, want %s", records[0].Status, tc.want)
			}
		})
	}
}
