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

func TestJourneyCookieExtractionHonorsRegex(t *testing.T) {
	cases := []struct {
		name, from, path, value, pattern, want string
	}{
		{"cookie_capture", "cookie", "source", "prefix-resource-42", `^prefix-(resource-[0-9]+)$`, "resource-42"},
		{"cookie_nonmatch", "cookie", "source", "prefix-resource-42", `^other-(.*)$`, ""},
		{"cookie_invalid_regex", "cookie", "source", "prefix-resource-42", `[`, ""},
		{"cookie_no_capture_group", "cookie", "source", "prefix-resource-42", `resource-[0-9]+`, ""},
		{"cookie_equals_in_value", "cookie", "source", "prefix-a=b=c", `^prefix-(.*)$`, "a=b=c"},
		{"raw_cookie_control", "cookie", "source", "prefix-resource-42", "", "prefix-resource-42"},
		{"missing_cookie_control", "cookie", "missing", "prefix-resource-42", `^prefix-(.*)$`, ""},
		{"header_capture_control", "header", "X-Source", "prefix-resource-42", `^prefix-(resource-[0-9]+)$`, "resource-42"},
		{"body_capture_control", "body", "", "prefix-resource-42", `^prefix-(resource-[0-9]+)$`, "resource-42"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.SetCookie(w, &http.Cookie{Name: "source", Value: tc.value, Path: "/"})
				w.Header().Set("X-Source", tc.value)
				_, _ = io.WriteString(w, tc.value)
			}))
			defer srv.Close()
			j := &core.JourneyConfig{ID: "cookie-regex", WorkspaceID: "default", Steps: []core.JourneyStep{{
				Name: "capture", Type: core.CheckHTTP, Target: srv.URL, Timeout: core.Duration{Duration: time.Second},
				HTTP:    &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
				Extract: map[string]core.ExtractionRule{"captured": {From: tc.from, Path: tc.path, Regex: tc.pattern}},
			}}}
			db := newTestDB(t)
			defer db.Close()
			e := NewExecutor(db, newTestLogger())
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			run, err := e.RunOnce(ctx, j)
			if err != nil {
				t.Fatal(err)
			}
			if run.Status != core.SoulAlive {
				t.Fatalf("fixture request failed: %+v", run.Steps)
			}
			if got := run.Variables["captured"]; got != tc.want {
				t.Errorf("FAIL: %s extraction regex=%q captured=%q, want %q", tc.from, tc.pattern, got, tc.want)
			}
			if tc.want == "" {
				if _, exists := run.Variables["captured"]; exists {
					t.Error("FAIL: rejected extraction created a captured variable")
				}
			}
			records, err := e.ListRuns(ctx, "default", j.ID, 10)
			if err != nil || len(records) != 1 {
				t.Fatalf("persistence: count=%d err=%v", len(records), err)
			}
			if got := records[0].Variables["captured"]; got != tc.want {
				t.Errorf("FAIL: persisted captured=%q, want %q", got, tc.want)
			}
		})
	}
}
