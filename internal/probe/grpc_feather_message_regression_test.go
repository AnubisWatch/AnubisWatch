package probe

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func grpcFeatherJudgment(t *testing.T, grpcStatus string, delay time.Duration) *core.Judgment {
	t.Helper()
	srv := newHTTP2TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.Header().Set("Grpc-Status", grpcStatus)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte{0, 0, 0, 0, 2, 8, 1})
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	j, err := NewGRPCChecker().Judge(context.Background(), &core.Soul{
		ID: "grpc-feather", Type: core.CheckGRPC, Target: u.Host,
		GRPC:    &core.GRPCConfig{TLS: true, InsecureSkipVerify: true, Feather: core.Duration{Duration: 10 * time.Millisecond}},
		Timeout: core.Duration{Duration: 2 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	return j
}

// A slow response must not overwrite the diagnosis of a failed health check:
// the judgment stays dead and its message keeps the real failure cause.
func TestGRPCFeatherDoesNotMaskFailureMessage(t *testing.T) {
	publishValidatorForTest(t, permissiveSSRFValidator())

	control := grpcFeatherJudgment(t, "0", 60*time.Millisecond)
	if control.Status != core.SoulDegraded || !strings.Contains(control.Message, "feather") {
		t.Fatalf("control: status=%s msg=%q, want degraded feather message", control.Status, control.Message)
	}

	failed := grpcFeatherJudgment(t, "14", 60*time.Millisecond)
	if failed.Status != core.SoulDead {
		t.Fatalf("status=%s, want dead", failed.Status)
	}
	if !strings.Contains(failed.Message, "grpc-status=14") {
		t.Fatalf("failure cause lost: message=%q", failed.Message)
	}
}
