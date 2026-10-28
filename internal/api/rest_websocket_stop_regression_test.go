package api

import (
	"context"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestRESTStopStopsOwnedWebSocketServer(t *testing.T) {
	server := NewRESTServer(
		core.ServerConfig{Host: "127.0.0.1", Port: 8080},
		core.AuthConfig{Enabled: core.BoolPtr(true)},
		newMockStorage(), &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{},
		&mockClusterManager{}, nil, nil, nil, nil, newTestLogger(),
	)
	t.Cleanup(server.ws.Stop)

	select {
	case <-server.ws.ctx.Done():
		t.Fatal("WebSocket server was already stopped before REST shutdown")
	default:
	}

	if err := server.Stop(context.Background()); err != nil {
		t.Fatalf("REST Stop: %v", err)
	}

	select {
	case <-server.rateLimitStopCh:
		// Control: REST shutdown definitely ran.
	default:
		t.Fatal("control: REST Stop did not close the rate-limit stop channel")
	}

	select {
	case <-server.ws.ctx.Done():
		t.Log("PASS: REST Stop canceled the owned WebSocket server")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("FAIL: REST Stop returned while the owned WebSocket broadcast loop remained live")
	}
}
