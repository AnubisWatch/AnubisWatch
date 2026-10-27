package probe

import (
	"context"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestGRPCMetadataIsSent(t *testing.T) {
	publishValidatorForTest(t, permissiveSSRFValidator())
	got := make(chan string, 1)
	srv := newHTTP2TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Get("X-Probe-Token")
		w.Header().Set("Grpc-Status", "0")
		w.WriteHeader(200)
		_, _ = w.Write([]byte{0, 0, 0, 0, 2, 8, 1})
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	j, _ := NewGRPCChecker().Judge(context.Background(), &core.Soul{ID: "grpc-meta", Type: core.CheckGRPC, Target: u.Host, Timeout: core.Duration{Duration: 2 * time.Second}, GRPC: &core.GRPCConfig{TLS: true, InsecureSkipVerify: true, Metadata: map[string]string{"X-Probe-Token": "expected"}}})
	if j.Status != core.SoulAlive {
		t.Fatalf("control: health exchange failed: %s", j.Message)
	}
	if v := <-got; v != "expected" {
		t.Fatalf("FAIL: configured gRPC metadata missing: got %q", v)
	}
}
