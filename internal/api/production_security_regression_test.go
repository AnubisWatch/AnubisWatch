package api

import (
	"context"
	"fmt"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityRegressionJourneySecrets(t *testing.T) {
	store := newMockStorage()
	store.journeys["journeysecret"] = &core.JourneyConfig{ID: "journeysecret", WorkspaceID: "default", Name: "Login", Variables: map[string]string{"password": "proof-credential"}, Steps: []core.JourneyStep{{Name: "auth", Type: core.CheckHTTP, Target: "https://example.com", HTTP: &core.HTTPConfig{Headers: map[string]string{"Authorization": "Bearer proof-token"}}}}}
	store.souls["soulsecret"] = &core.Soul{ID: "soulsecret", WorkspaceID: "default", Name: "Control", Type: core.CheckHTTP, Target: "https://example.com", HTTP: &core.HTTPConfig{Headers: map[string]string{"Authorization": "Bearer proof-token"}}}
	srv := NewRESTServer(core.ServerConfig{}, core.AuthConfig{Enabled: core.BoolPtr(true)}, store, nil, nil, &mockAuthenticatorWithViewerRole{}, nil, nil, nil, nil, nil, newTestLogger())
	defer srv.Stop(context.Background())
	get := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("Authorization", "Bearer valid-token")
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, r)
		return w
	}
	control := get("/api/v1/souls/soulsecret")
	if control.Code != 200 || strings.Contains(control.Body.String(), "proof-token") {
		t.Fatalf("INVALID CONTROL: %d %s", control.Code, control.Body.String())
	}
	actual := get("/api/v1/journeys/journeysecret")
	fmt.Printf("EXPECTED: viewer cannot read credentials; ACTUAL: status=%d body=%s\n", actual.Code, actual.Body.String())
	if actual.Code == 200 && (strings.Contains(actual.Body.String(), "proof-token") || strings.Contains(actual.Body.String(), "proof-credential")) {
		t.Fatal("PROBLEM CONFIRMED")
	}
	fmt.Println("PROBLEM NOT REPRODUCED")
}
func TestSecurityRegressionPrivateStatus(t *testing.T) {
	store := newMockStorage()
	store.souls["privatesoul"] = &core.Soul{ID: "privatesoul", Name: "private-payroll-database", WorkspaceID: "default", Enabled: true}
	srv := NewRESTServer(core.ServerConfig{}, core.AuthConfig{Enabled: core.BoolPtr(true)}, store, nil, nil, &mockAuthenticatorWithViewerRole{}, nil, nil, nil, nil, nil, newTestLogger())
	defer srv.Stop(context.Background())
	get := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, r)
		return w
	}
	control := get("/api/v1/souls")
	if control.Code != 401 {
		t.Fatalf("INVALID CONTROL: %d", control.Code)
	}
	actual := get("/status")
	fmt.Printf("EXPECTED: unconfigured public status does not reveal private monitors; ACTUAL: status=%d body=%s\n", actual.Code, actual.Body.String())
	if actual.Code == 200 && strings.Contains(actual.Body.String(), "private-payroll-database") {
		t.Fatal("PROBLEM CONFIRMED")
	}
	fmt.Println("PROBLEM NOT REPRODUCED")
}
func TestSecurityRegressionMetricsTenant(t *testing.T) {
	base := newMockStorage()
	base.workspaces["tenant-b"] = &core.Workspace{ID: "tenant-b", Name: "Private tenant"}
	private := &core.Soul{ID: "tenantbsoul", WorkspaceID: "tenant-b", Name: "private-tenant-b-payroll"}
	base.souls[private.ID] = private
	store := &workspaceAwareMetricsStorage{mockStorage: base, soulsByWorkspace: map[string][]*core.Soul{"tenant-b": {private}}}
	srv := NewRESTServer(core.ServerConfig{MetricsAuth: true}, core.AuthConfig{Enabled: core.BoolPtr(true)}, store, nil, nil, &mockAuthenticatorWithViewerRole{}, nil, nil, nil, nil, nil, newTestLogger())
	defer srv.Stop(context.Background())
	get := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("Authorization", "Bearer valid-token")
		w := httptest.NewRecorder()
		srv.router.ServeHTTP(w, r)
		return w
	}
	control := get("/api/v1/souls/tenantbsoul")
	if control.Code != 403 {
		t.Fatalf("INVALID CONTROL: %d %s", control.Code, control.Body.String())
	}
	actual := get("/metrics")
	fmt.Printf("EXPECTED: default workspace viewer denied tenant-b monitor data; ACTUAL: status=%d private_monitor_present=%v\n", actual.Code, strings.Contains(actual.Body.String(), "private-tenant-b-payroll"))
	if actual.Code == 200 && strings.Contains(actual.Body.String(), "private-tenant-b-payroll") {
		t.Fatal("PROBLEM CONFIRMED")
	}
	fmt.Println("PROBLEM NOT REPRODUCED")
}
