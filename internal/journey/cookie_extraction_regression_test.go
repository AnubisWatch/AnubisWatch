package journey

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// A response header may repeat -- Set-Cookie does, once per cookie -- so every
// cookie a login response sets must be extractable by a journey step. The HTTP
// checker used to persist only the first value of each header, which silently
// gave later steps an empty variable.

func cookieExtractionExecutor(t *testing.T) *Executor {
	t.Helper()
	return NewExecutor(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func cookieExtractionStep(t *testing.T, serverURL string, extract map[string]core.ExtractionRule) core.JourneyStep {
	t.Helper()
	return core.JourneyStep{
		Name:    "login",
		Type:    core.CheckHTTP,
		Target:  serverURL,
		HTTP:    &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
		Extract: extract,
	}
}

func runCookieExtractionStep(t *testing.T, handler http.HandlerFunc, extract map[string]core.ExtractionRule) map[string]string {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}

	result := cookieExtractionExecutor(t).executeStep(
		context.Background(),
		&JourneyContext{Variables: map[string]string{}, CookieJar: jar},
		cookieExtractionStep(t, srv.URL, extract),
		0,
	)
	if result.Status != core.SoulAlive {
		t.Fatalf("step failed: status=%s message=%q", result.Status, result.Message)
	}
	return result.Extracted
}

// TestCookieExtractionFindsEveryCookieTheResponseSet pins the contract: each
// cookie in the response is extractable by name, whatever its order.
func TestCookieExtractionFindsEveryCookieTheResponseSet(t *testing.T) {
	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Set-Cookie", "session=session-value; Path=/; HttpOnly")
		w.Header().Add("Set-Cookie", "csrf=csrf-value; Path=/")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}

	extracted := runCookieExtractionStep(t, handler, map[string]core.ExtractionRule{
		"session": {From: "cookie", Path: "session"},
		"csrf":    {From: "cookie", Path: "csrf"},
	})

	if extracted["session"] != "session-value" {
		t.Errorf("session = %q, want session-value (extracted=%v)", extracted["session"], extracted)
	}
	if extracted["csrf"] != "csrf-value" {
		t.Errorf("csrf = %q, want csrf-value: a cookie the response set was not extractable (extracted=%v)",
			extracted["csrf"], extracted)
	}
}

// TestCookieExtractionSingleCookieAndHeadersStillWork is the neighbouring
// control: the single-cookie case and header extraction are unaffected.
func TestCookieExtractionSingleCookieAndHeadersStillWork(t *testing.T) {
	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Set-Cookie", "only=only-value; Path=/")
		w.Header().Set("X-Request-Id", "req-123")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}

	extracted := runCookieExtractionStep(t, handler, map[string]core.ExtractionRule{
		"only":    {From: "cookie", Path: "only"},
		"req_id":  {From: "header", Path: "X-Request-Id"},
		"absent":  {From: "cookie", Path: "not-set"},
		"body_ok": {From: "body", Path: "$.ok"},
	})

	for name, want := range map[string]string{
		"only":    "only-value",
		"req_id":  "req-123",
		"body_ok": "true",
	} {
		if got := extracted[name]; got != want {
			t.Errorf("%s = %q, want %q (extracted=%v)", name, got, want, extracted)
		}
	}
	if _, ok := extracted["absent"]; ok {
		t.Errorf("a cookie the response never set was extracted: %v", extracted)
	}
}
