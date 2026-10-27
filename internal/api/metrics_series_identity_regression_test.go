package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Per-Soul Prometheus metrics need resource identity labels: names are mutable
// and two workspaces may independently use the same name.
func TestPrometheusSoulSeriesAreUniqueAcrossWorkspaces(t *testing.T) {
	store := &workspaceAwareMetricsStorage{
		mockStorage: newMockStorage(),
		soulsByWorkspace: map[string][]*core.Soul{
			"default":  {{ID: "soul-default", Name: "API", Type: core.CheckHTTP, WorkspaceID: "default"}},
			"tenant-a": {{ID: "soul-tenant", Name: "API", Type: core.CheckHTTP, WorkspaceID: "tenant-a"}},
		},
	}
	if err := store.SaveWorkspaceNoCtx(&core.Workspace{ID: "tenant-a", Name: "Tenant A", Slug: "tenant-a"}); err != nil {
		t.Fatalf("save workspace: %v", err)
	}
	now := time.Now()
	for _, judgment := range []*core.Judgment{
		{ID: "j-default", SoulID: "soul-default", Status: core.SoulAlive, Duration: 100 * time.Millisecond, Timestamp: now},
		{ID: "j-tenant", SoulID: "soul-tenant", Status: core.SoulDead, Duration: 200 * time.Millisecond, Timestamp: now},
	} {
		if err := store.SaveJudgment(context.Background(), judgment); err != nil {
			t.Fatalf("save judgment: %v", err)
		}
	}

	server := NewRESTServer(core.ServerConfig{Port: 8080}, core.AuthConfig{Enabled: core.BoolPtr(true)}, store, &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, &mockClusterManager{}, nil, nil, nil, nil, newTestLogger())
	recorder := httptest.NewRecorder()
	ctx := &Context{Request: httptest.NewRequest("GET", "/metrics", nil), Response: recorder}
	if err := server.handleMetrics(ctx); err != nil {
		t.Fatalf("handleMetrics: %v", err)
	}
	body := recorder.Body.String()

	if !strings.Contains(body, "anubis_souls_total 2\n") {
		t.Fatalf("both Souls were not exported:\n%s", body)
	}
	for _, want := range []string{
		`soul="API",soul_id="soul-default",workspace="default"`,
		`soul="API",soul_id="soul-tenant",workspace="tenant-a"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing identity labels %q:\n%s", want, body)
		}
	}

	for _, prefix := range []string{
		"anubis_soul_status{",
		"anubis_soul_latency_seconds{",
		"anubis_soul_uptime_ratio{",
	} {
		seen := make(map[string]struct{})
		count := 0
		for _, line := range strings.Split(body, "\n") {
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			count++
			identity, _, ok := strings.Cut(line, " ")
			if !ok {
				t.Fatalf("malformed metric line %q", line)
			}
			if _, duplicate := seen[identity]; duplicate {
				t.Fatalf("duplicate Prometheus series identity %q", identity)
			}
			seen[identity] = struct{}{}
		}
		if count != 2 {
			t.Fatalf("metric %s count = %d, want 2\n%s", prefix, count, body)
		}
	}
}
