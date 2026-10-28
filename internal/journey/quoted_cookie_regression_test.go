package journey

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestJourneyCookieExtractionDecodesQuotedValues(t *testing.T) {
	cases := []struct {
		name, value, path, pattern, want string
		quoted                           bool
		raw                              string
	}{
		{"quoted_plain", "session-value", "session", "", "session-value", true, ""},
		{"space_autoquoted", "user value", "session", "", "user value", false, ""},
		{"comma_autoquoted", "a,b", "session", "", "a,b", false, ""},
		{"quoted_equals", "a=b=c", "session", "", "a=b=c", true, ""},
		{"quoted_regex", "prefix-resource-42", "session", `^prefix-(resource-[0-9]+)$`, "resource-42", true, ""},
		{"quoted_empty", "", "session", "", "", false, `session=""; Path=/`},
		{"unquoted_control", "session-value", "session", "", "session-value", false, ""},
		{"unquoted_equals_control", "a=b=c", "session", "", "a=b=c", false, ""},
		{"missing_control", "session-value", "missing", "", "", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/verify" {
					cookie, err := r.Cookie("session")
					if err != nil || cookie.Value != tc.value {
						http.Error(w, "native cookie jar fixture mismatch", 400)
						return
					}
					if r.Header.Get("X-Captured") != tc.want {
						http.Error(w, "extracted cookie does not match logical value", 422)
						return
					}
					w.WriteHeader(200)
					return
				}
				if tc.raw != "" {
					w.Header().Set("Set-Cookie", tc.raw)
				} else {
					http.SetCookie(w, &http.Cookie{Name: "session", Value: tc.value, Quoted: tc.quoted, Path: "/"})
				}
				w.WriteHeader(200)
			}))
			defer srv.Close()
			j := &core.JourneyConfig{ID: "quoted-cookie", WorkspaceID: "default", Steps: []core.JourneyStep{{
				Name: "set", Type: core.CheckHTTP, Target: srv.URL + "/set", Timeout: core.Duration{Duration: time.Second},
				Extract: map[string]core.ExtractionRule{"captured": {From: "cookie", Path: tc.path, Regex: tc.pattern}},
			}}}
			if tc.want != "" {
				j.Steps = append(j.Steps, core.JourneyStep{
					Name: "verify", Type: core.CheckHTTP, Target: srv.URL + "/verify", Timeout: core.Duration{Duration: time.Second},
					HTTP: &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}, Headers: map[string]string{"X-Captured": "${captured}"}},
				})
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
			if run.Steps[0].Status != core.SoulAlive {
				t.Fatal("fixture Set-Cookie response failed")
			}
			if got := run.Variables["captured"]; got != tc.want {
				t.Errorf("FAIL: extracted cookie=%q, want logical value %q", got, tc.want)
			}
			if tc.want == "" {
				if _, exists := run.Variables["captured"]; exists {
					t.Error("FAIL: empty or missing cookie created a nonempty variable")
				}
			}
			if run.Status != core.SoulAlive {
				t.Errorf("FAIL: reuse of extracted cookie failed: %+v", run.Steps)
			}
			records, err := e.ListRuns(ctx, "default", j.ID, 10)
			if err != nil || len(records) != 1 {
				t.Fatalf("persistence: count=%d err=%v", len(records), err)
			}
			if records[0].Variables["captured"] != tc.want {
				t.Error("FAIL: incorrect logical cookie value was persisted")
			}
		})
	}
}
