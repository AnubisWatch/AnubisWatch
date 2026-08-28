package api

import (
	"context"
	"log/slog"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Stop used to close rateLimitStopCh unconditionally, so a second call panicked
// with "close of closed channel". Shutdown paths are where a duplicate call is
// most likely, which is the worst place for a panic.
func TestRESTServerStopIsIdempotent(t *testing.T) {
	s := &RESTServer{
		config:          core.ServerConfig{Port: 8443},
		logger:          slog.Default(),
		rateLimitStopCh: make(chan struct{}),
	}

	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	// Would panic before stopRateLimitOnce guarded the close.
	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}

	select {
	case <-s.rateLimitStopCh:
	default:
		t.Fatal("rateLimitStopCh was not closed")
	}
}
