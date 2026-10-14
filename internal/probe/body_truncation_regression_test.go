package probe

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// When the check's own read limit stops it, the judgment must say so. Otherwise a
// healthy but large response whose asserted content lies past the limit is
// reported as a body-assertion failure, and the operator cannot tell a missing
// marker from the monitor's inability to read it. The gRPC check reports the same
// condition explicitly ("response exceeds %d bytes").

const bodyTruncationMarker = "MARKER-BEYOND-THE-READ-LIMIT"

func bodyTruncationJudgment(t *testing.T, body string) *core.Judgment {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	soul := &core.Soul{
		ID: "body-truncation-soul", Name: "body truncation", Target: srv.URL,
		HTTP: &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}, BodyContains: bodyTruncationMarker},
	}

	judgment, err := NewHTTPChecker().Judge(context.Background(), soul)
	if err != nil {
		t.Fatalf("judge failed: %v", err)
	}
	return judgment
}

func TestOversizedBodyIsReportedNotSilentlyTruncated(t *testing.T) {
	for _, tc := range []struct {
		name      string
		body      string
		wantTrunc bool
		wantAlive bool
	}{
		{
			// Exactly at the limit: the whole body is read, so the marker at its
			// end is evaluated normally and nothing is truncated.
			"body exactly at the read limit is not truncated",
			strings.Repeat("x", maxReadSize-len(bodyTruncationMarker)) + bodyTruncationMarker,
			false, true,
		},
		{
			"one byte past the read limit is truncated",
			strings.Repeat("x", maxReadSize+1) + bodyTruncationMarker,
			true, false,
		},
		{
			"far past the read limit is truncated",
			strings.Repeat("x", maxReadSize+4096) + bodyTruncationMarker,
			true, false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := bodyTruncationJudgment(t, tc.body)

			mentionsLimit := strings.Contains(j.Message, "1048576")
			if mentionsLimit != tc.wantTrunc {
				t.Errorf("message = %q (mentions the read limit: %v), want %v",
					j.Message, mentionsLimit, tc.wantTrunc)
			}
			if gotAlive := j.Status == core.SoulAlive; gotAlive != tc.wantAlive {
				t.Errorf("status = %s (alive: %v), want alive: %v (message=%q)", j.Status, gotAlive, tc.wantAlive, j.Message)
			}
		})
	}
}

// TestBodyAssertionsWithinTheLimitStillWork is the neighbouring control: normal
// bodies keep being evaluated, and their failures carry no size diagnostic.
func TestBodyAssertionsWithinTheLimitStillWork(t *testing.T) {
	passing := bodyTruncationJudgment(t, "prefix "+bodyTruncationMarker)
	if passing.Status != core.SoulAlive {
		t.Errorf("a body within the limit containing the marker: status = %s, want alive", passing.Status)
	}

	failing := bodyTruncationJudgment(t, "prefix without the marker")
	if failing.Status != core.SoulDead {
		t.Errorf("a body within the limit missing the marker: status = %s, want dead", failing.Status)
	}
	if strings.Contains(failing.Message, "1048576") {
		t.Errorf("a body that fit reported a read-limit problem: %q", failing.Message)
	}
}
