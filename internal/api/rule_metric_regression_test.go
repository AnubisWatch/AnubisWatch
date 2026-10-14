package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// The alert engine never fires a threshold or anomaly condition on a metric it
// does not measure, so the API must refuse such a rule instead of storing one
// that silently never alerts.

func ruleMetricServer() (*RESTServer, *mockAlertManager) {
	alert := &mockAlertManager{}
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:     core.ServerConfig{Host: "localhost", Port: 8080},
		authConfig: core.AuthConfig{Enabled: core.BoolPtr(true)},
		store:      newMockStorage(),
		router:     router,
		auth:       &mockAuthenticator{},
		alert:      alert,
		logger:     newTestLogger(),
		cluster:    &mockClusterManager{},
	}
	router.Handle("POST", "/api/v1/rules", server.requireAuth(server.handleCreateRule))
	router.Handle("PUT", "/api/v1/rules/:id", server.requireAuth(server.handleUpdateRule))
	return server, alert
}

func sendRule(t *testing.T, server *RESTServer, method, path string, rule core.AlertRule) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(rule)
	req := httptest.NewRequest(method, path, bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)
	return w
}

func metricRule(metric string) core.AlertRule {
	return core.AlertRule{
		Name: "cpu alert", Enabled: true, Scope: core.RuleScope{Type: "all"},
		Conditions: []core.AlertCondition{{Type: "threshold", Metric: metric, Operator: ">", Value: 90.0}},
		Channels:   []string{"channel-1"},
	}
}

func TestCreateRuleRejectsUnknownMetric(t *testing.T) {
	server, alert := ruleMetricServer()

	w := sendRule(t, server, "POST", "/api/v1/rules", metricRule("cpu"))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "unsupported metric") {
		t.Fatalf("create with metric cpu = %d %s, want 400 naming the unsupported metric", w.Code, w.Body.String())
	}
	if len(alert.rules) != 0 {
		t.Fatalf("rejected rule was registered: %d rules", len(alert.rules))
	}

	// latency_ms and tls_expiry_days are what the dashboard's rule form sends.
	for _, metric := range []string{"latency", "latency_ms", "tls_expiry_days"} {
		if w := sendRule(t, server, "POST", "/api/v1/rules", metricRule(metric)); w.Code != http.StatusCreated {
			t.Fatalf("create with metric %s = %d %s, want 201", metric, w.Code, w.Body.String())
		}
	}
}

func TestUpdateRuleRejectsUnknownMetric(t *testing.T) {
	server, alert := ruleMetricServer()
	existing := metricRule("latency")
	existing.ID = "rule-1"
	existing.WorkspaceID = "default"
	alert.rules = map[string]*core.AlertRule{"rule-1": &existing}

	w := sendRule(t, server, "PUT", "/api/v1/rules/rule-1", metricRule("memory"))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "unsupported metric") {
		t.Fatalf("update to metric memory = %d %s, want 400 naming the unsupported metric", w.Code, w.Body.String())
	}
	if got := alert.rules["rule-1"].Conditions[0].Metric; got != "latency" {
		t.Fatalf("stored metric after refused update = %q, want latency", got)
	}
}
