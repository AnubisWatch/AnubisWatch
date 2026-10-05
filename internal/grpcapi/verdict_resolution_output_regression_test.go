package grpcapi

import (
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
	"time"
)

func TestVerdictOutputPreservesResolutionTime(t *testing.T) {
	if eventToVerdict(nil) != nil {
		t.Fatal("nil")
	}
	zero := time.Time{}
	instant := time.Date(2026, 10, 5, 1, 2, 3, 123456789, time.FixedZone("fixture", 7200))
	for _, at := range []*time.Time{nil, &zero, &instant} {
		p := eventToVerdict(&core.AlertEvent{Resolved: true, ResolvedAt: at})
		if at == nil || at.IsZero() {
			if p.ResolvedAt != nil {
				t.Fatal("absent resolution")
			}
		} else if p.ResolvedAt == nil || !p.ResolvedAt.AsTime().Equal(*at) {
			t.Fatal("resolution instant")
		}
	}
	for _, tc := range []struct {
		event core.AlertEvent
		want  string
	}{{core.AlertEvent{}, "firing"}, {core.AlertEvent{Acknowledged: true}, "acknowledged"}, {core.AlertEvent{Acknowledged: true, Resolved: true}, "resolved"}, {core.AlertEvent{Status: "RESOLVED"}, "resolved"}, {core.AlertEvent{Status: "ACKNOWLEDGED"}, "acknowledged"}} {
		if eventToVerdict(&tc.event).Status != tc.want {
			t.Fatal("status precedence")
		}
	}
}
