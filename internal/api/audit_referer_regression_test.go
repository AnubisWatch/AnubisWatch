package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// auditDetailsFor drives the real audit path (LogRequest -> buffer -> backend)
// and returns the recorded event's parsed details. Parsing rather than matching
// the raw JSON keeps the assertions about values, not about json.Marshal's
// escaping of '&'.
func auditDetailsFor(t *testing.T, requestURL, referer string) map[string]any {
	t.Helper()

	backend := &mockAuditBackend{}
	al := NewAuditLogger(slog.New(slog.NewTextHandler(io.Discard, nil)), backend)

	r := httptest.NewRequest(http.MethodGet, requestURL, nil)
	if referer != "" {
		r.Header.Set("Referer", referer)
	}

	al.LogRequest(r, "user-1", http.StatusOK, 5*time.Millisecond)
	al.Stop() // flushes the buffer through the real shutdown path

	events := backend.GetEvents()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 audit event, got %d", len(events))
	}

	var details map[string]any
	if err := json.Unmarshal(events[0].Details, &details); err != nil {
		t.Fatalf("could not parse audit details %q: %v", string(events[0].Details), err)
	}
	return details
}

func detailString(t *testing.T, details map[string]any, key string) string {
	t.Helper()
	v, ok := details[key].(string)
	if !ok {
		t.Fatalf("audit detail %q is not a string: %#v", key, details[key])
	}
	return v
}

// TestAuditDetailsMaskSensitiveRefererParams pins that an audit record does not
// store the unmasked value of a sensitive URL parameter just because it arrived
// in the Referer header rather than in the request's own query. The set of
// sensitive parameters is the one the record's own sanitizer defines, and it
// includes the OIDC callback parameters (code, state, id_token, access_token).
func TestAuditDetailsMaskSensitiveRefererParams(t *testing.T) {
	const (
		querySecret  = "QUERYSECRETVALUE"
		codeSecret   = "CODESECRETVALUE"
		stateSecret  = "STATESECRETVALUE"
		refererToken = "REFERERTOKENVALUE"
	)

	details := auditDetailsFor(t,
		"/api/v1/souls?token="+querySecret,
		"https://anubis.example/api/v1/auth/oidc/callback?code="+codeSecret+"&state="+stateSecret+"&token="+refererToken,
	)

	// CONTROL: the request's own query keeps being masked.
	query := detailString(t, details, "query")
	if strings.Contains(query, querySecret) || query != "token=***" {
		t.Fatalf("the request's own query is no longer masked: %q", query)
	}

	referer := detailString(t, details, "referer")
	for _, secret := range []string{codeSecret, stateSecret, refererToken} {
		if strings.Contains(referer, secret) {
			t.Errorf("the Referer's sensitive parameter value %s was recorded unmasked: %q", secret, referer)
		}
	}

	// The Referer must still be useful: masking values is not the same as
	// discarding the URL.
	if !strings.HasPrefix(referer, "https://anubis.example/api/v1/auth/oidc/callback?") {
		t.Errorf("the Referer was discarded rather than masked: %q", referer)
	}
	if !strings.Contains(referer, "code=***") || !strings.Contains(referer, "state=***") {
		t.Errorf("expected the Referer's sensitive parameters masked in place: %q", referer)
	}
}

// TestAuditDetailsKeepOrdinaryRefererParams is the neighbouring control: a
// Referer without sensitive parameters, or without a query at all, is recorded
// unchanged, so the masking must not alter ordinary values or drop the field.
func TestAuditDetailsKeepOrdinaryRefererParams(t *testing.T) {
	for _, tc := range []struct{ name, referer, want string }{
		{"no query", "https://anubis.example/souls", "https://anubis.example/souls"},
		{"ordinary query", "https://anubis.example/souls?page=2&sort=name", "https://anubis.example/souls?page=2&sort=name"},
		{"no referer", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := detailString(t, auditDetailsFor(t, "/api/v1/souls", tc.referer), "referer")
			if got != tc.want {
				t.Errorf("referer = %q, want %q", got, tc.want)
			}
		})
	}
}
