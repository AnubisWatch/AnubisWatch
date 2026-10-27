package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Soul IDs are scoped by workspace in storage, so metrics enumeration must use
// the same composite identity instead of dropping a tenant's Soul globally.
func TestMetricsKeepSameSoulIDInDifferentWorkspaces(t *testing.T) {
	store := &workspaceAwareMetricsStorage{
		mockStorage: newMockStorage(),
		soulsByWorkspace: map[string][]*core.Soul{
			"default":  {{ID: "shared-id", Name: "Default API", Type: core.CheckHTTP, WorkspaceID: "default"}},
			"tenant-a": {{ID: "shared-id", Name: "Tenant API", Type: core.CheckHTTP, WorkspaceID: "tenant-a"}},
		},
	}
	if err := store.SaveWorkspaceNoCtx(&core.Workspace{ID: "tenant-a", Name: "Tenant A", Slug: "tenant-a"}); err != nil {
		t.Fatalf("save workspace: %v", err)
	}
	for _, workspace := range []string{"default", "tenant-a"} {
		souls, err := store.ListSoulsNoCtx(workspace, 0, 100)
		if err != nil || len(souls) != 1 {
			t.Fatalf("workspace %q returned %d Souls, err=%v", workspace, len(souls), err)
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
		t.Fatalf("workspace-scoped Souls sharing an ID were not both exported:\n%s", body)
	}
	for _, want := range []string{
		`soul="Default API",soul_id="shared-id",workspace="default"`,
		`soul="Tenant API",soul_id="shared-id",workspace="tenant-a"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing workspace-scoped Soul identity %q:\n%s", want, body)
		}
	}
}
