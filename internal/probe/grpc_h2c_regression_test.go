package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// The checker moved from the deprecated x/net http2.Transport to net/http's
// HTTP/2 support; plaintext targets must still speak h2c with prior knowledge.
func TestGRPCCheckerPlaintextH2C(t *testing.T) {
	var gotProto string
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotProto = r.Proto
		w.Header().Set("Grpc-Status", "0")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte{0, 0, 0, 0, 2, 8, 1}) // HealthCheckResponse.status = SERVING.
	}))
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	srv.Config.Protocols = &protocols
	srv.Start()
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	judgment, err := NewGRPCChecker().Judge(context.Background(), &core.Soul{
		ID: "h2c", Name: "h2c", Type: core.CheckGRPC, Target: u.Host,
		GRPC:    &core.GRPCConfig{TLS: false},
		Timeout: core.Duration{Duration: 2 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	if judgment.Status != core.SoulAlive {
		t.Fatalf("status = %s (%s), want alive", judgment.Status, judgment.Message)
	}
	if gotProto != "HTTP/2.0" {
		t.Fatalf("server saw %s, want HTTP/2.0 (h2c)", gotProto)
	}
}
