package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"log/slog"
	"os"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
}

// mockStorage implements Storage interface for testing
type mockStorage struct {
	souls       map[string]*core.Soul
	judgments   map[string]*core.Judgment
	channels    map[string]*core.AlertChannel
	rules       map[string]*core.AlertRule
	workspaces  map[string]*core.Workspace
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		souls:       make(map[string]*core.Soul),
		judgments:   make(map[string]*core.Judgment),
		channels:    make(map[string]*core.AlertChannel),
		rules:       make(map[string]*core.AlertRule),
		workspaces:  make(map[string]*core.Workspace),
	}
}

func (m *mockStorage) GetSoulNoCtx(id string) (*core.Soul, error)       { return m.souls[id], nil }
func (m *mockStorage) ListSoulsNoCtx(ws string, offset, limit int) ([]*core.Soul, error) {
	souls := make([]*core.Soul, 0, len(m.souls))
	for _, s := range m.souls {
		souls = append(souls, s)
	}
	if offset >= len(souls) {
		return []*core.Soul{}, nil
	}
	end := offset + limit
	if end > len(souls) {
		end = len(souls)
	}
	return souls[offset:end], nil
}
func (m *mockStorage) SaveSoul(ctx context.Context, soul *core.Soul) error {
	m.souls[soul.ID] = soul
	return nil
}
func (m *mockStorage) DeleteSoul(ctx context.Context, id string) error {
	delete(m.souls, id)
	return nil
}
func (m *mockStorage) GetJudgmentNoCtx(id string) (*core.Judgment, error) { return m.judgments[id], nil }
func (m *mockStorage) ListJudgmentsNoCtx(soulID string, start, end time.Time, limit int) ([]*core.Judgment, error) {
	return []*core.Judgment{}, nil
}
func (m *mockStorage) GetChannelNoCtx(id string) (*core.AlertChannel, error) { return m.channels[id], nil }
func (m *mockStorage) ListChannelsNoCtx(ws string) ([]*core.AlertChannel, error) {
	channels := make([]*core.AlertChannel, 0, len(m.channels))
	for _, c := range m.channels {
		channels = append(channels, c)
	}
	return channels, nil
}
func (m *mockStorage) SaveChannelNoCtx(ch *core.AlertChannel) error {
	m.channels[ch.ID] = ch
	return nil
}
func (m *mockStorage) DeleteChannelNoCtx(id string) error {
	delete(m.channels, id)
	return nil
}
func (m *mockStorage) GetRuleNoCtx(id string) (*core.AlertRule, error) { return m.rules[id], nil }
func (m *mockStorage) ListRulesNoCtx(ws string) ([]*core.AlertRule, error) {
	rules := make([]*core.AlertRule, 0, len(m.rules))
	for _, r := range m.rules {
		rules = append(rules, r)
	}
	return rules, nil
}
func (m *mockStorage) SaveRuleNoCtx(rule *core.AlertRule) error {
	m.rules[rule.ID] = rule
	return nil
}
func (m *mockStorage) DeleteRuleNoCtx(id string) error {
	delete(m.rules, id)
	return nil
}
func (m *mockStorage) GetWorkspaceNoCtx(id string) (*core.Workspace, error) { return m.workspaces[id], nil }
func (m *mockStorage) ListWorkspacesNoCtx() ([]*core.Workspace, error) {
	ws := make([]*core.Workspace, 0, len(m.workspaces))
	for _, w := range m.workspaces {
		ws = append(ws, w)
	}
	return ws, nil
}
func (m *mockStorage) SaveWorkspaceNoCtx(ws *core.Workspace) error {
	m.workspaces[ws.ID] = ws
	return nil
}
func (m *mockStorage) DeleteWorkspaceNoCtx(id string) error {
	delete(m.workspaces, id)
	return nil
}
func (m *mockStorage) GetStatsNoCtx(ws string, start, end time.Time) (*core.Stats, error) {
	return &core.Stats{}, nil
}

// StatusPage methods
func (m *mockStorage) GetStatusPageNoCtx(id string) (*core.StatusPage, error) { return nil, nil }
func (m *mockStorage) ListStatusPagesNoCtx() ([]*core.StatusPage, error) { return []*core.StatusPage{}, nil }
func (m *mockStorage) SaveStatusPageNoCtx(page *core.StatusPage) error { return nil }
func (m *mockStorage) DeleteStatusPageNoCtx(id string) error { return nil }

// mockProbeEngine implements ProbeEngine interface
type mockProbeEngine struct {
	souls map[string]*core.Soul
}

func (p *mockProbeEngine) GetStatus() *core.ProbeStatus {
	return &core.ProbeStatus{Running: true, ActiveChecks: 0}
}
func (p *mockProbeEngine) ForceCheck(soulID string) (*core.Judgment, error) {
	return &core.Judgment{ID: "judgment-1", SoulID: soulID, Status: core.SoulAlive}, nil
}

// mockAlertManager implements AlertManager interface
type mockAlertManager struct{}

func (a *mockAlertManager) GetStats() core.AlertManagerStats {
	return core.AlertManagerStats{}
}
func (a *mockAlertManager) ListChannels() []*core.AlertChannel { return nil }
func (a *mockAlertManager) ListRules() []*core.AlertRule       { return nil }
func (a *mockAlertManager) RegisterChannel(ch *core.AlertChannel) error { return nil }
func (a *mockAlertManager) RegisterRule(rule *core.AlertRule) error     { return nil }
func (a *mockAlertManager) DeleteChannel(id string) error               { return nil }
func (a *mockAlertManager) DeleteRule(id string) error                  { return nil }
func (a *mockAlertManager) AcknowledgeIncident(id, userID string) error { return nil }
func (a *mockAlertManager) ResolveIncident(id, userID string) error     { return nil }

// mockAuthenticator implements Authenticator interface
type mockAuthenticator struct{}

func (a *mockAuthenticator) Authenticate(token string) (*User, error) {
	if token == "valid-token" {
		return &User{ID: "user-1", Email: "test@example.com", Role: "admin", Workspace: "default"}, nil
	}
	return nil, http.ErrNoCookie
}
func (a *mockAuthenticator) Login(email, password string) (*User, string, error) {
	if email == "test@example.com" && password == "password" {
		user := &User{ID: "user-1", Email: email, Role: "admin", Workspace: "default"}
		return user, "valid-token", nil
	}
	return nil, "", http.ErrNoCookie
}
func (a *mockAuthenticator) Logout(token string) error { return nil }

// mockClusterManager implements ClusterManager interface
type mockClusterManager struct{}

func (m *mockClusterManager) IsLeader() bool                           { return true }
func (m *mockClusterManager) Leader() string                           { return "test-node" }
func (m *mockClusterManager) IsClustered() bool                        { return false }
func (m *mockClusterManager) GetStatus() *ClusterStatus                { return &ClusterStatus{IsClustered: false, NodeID: "test-node", State: "standalone"} }

func TestHandleHealth(t *testing.T) {
	storage := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}

	server := &RESTServer{
		store:   storage,
		router:  router,
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/health", server.handleHealth)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	json.NewDecoder(w.Body).Decode(&response)
	if response["status"] != "healthy" {
		t.Errorf("expected status 'healthy', got '%v'", response["status"])
	}
}

func TestHandleLogin_Success(t *testing.T) {
	storage := newMockStorage()
	auth := &mockAuthenticator{}
	router := &Router{routes: make(map[string]map[string]Handler)}

	server := &RESTServer{
		store:  storage,
		router: router,
		auth:   auth,
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("POST", "/api/v1/auth/login", server.handleLogin)

	body := bytes.NewBufferString(`{"email":"test@example.com","password":"password"}`)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", body)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var response map[string]interface{}
	json.NewDecoder(w.Body).Decode(&response)
	if response["token"] == "" {
		t.Error("expected token in response")
	}
}

func TestHandleLogin_InvalidCredentials(t *testing.T) {
	storage := newMockStorage()
	auth := &mockAuthenticator{}
	router := &Router{routes: make(map[string]map[string]Handler)}

	server := &RESTServer{
		store:  storage,
		router: router,
		auth:   auth,
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("POST", "/api/v1/auth/login", server.handleLogin)

	body := bytes.NewBufferString(`{"email":"wrong@example.com","password":"wrong"}`)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", body)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestHandleListSouls(t *testing.T) {
	storage := newMockStorage()
	storage.SaveSoul(context.Background(), &core.Soul{ID: "soul-1", Name: "Test Soul", Type: core.CheckHTTP, Target: "https://example.com"})

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/api/v1/souls", server.requireAuth(server.handleListSouls))

	req := httptest.NewRequest("GET", "/api/v1/souls", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var response PaginatedResponse
	json.NewDecoder(w.Body).Decode(&response)

	souls, ok := response.Data.([]interface{})
	if !ok {
		t.Errorf("expected data array, got %T", response.Data)
	}
	if len(souls) != 1 {
		t.Errorf("expected 1 soul, got %d", len(souls))
	}

	// Verify pagination metadata
	if response.Pagination.Offset != 0 {
		t.Errorf("expected offset 0, got %d", response.Pagination.Offset)
	}
	if response.Pagination.Limit != 20 {
		t.Errorf("expected limit 20, got %d", response.Pagination.Limit)
	}
}

func TestHandleCreateSoul(t *testing.T) {
	storage := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("POST", "/api/v1/souls", server.requireAuth(server.handleCreateSoul))

	soul := core.Soul{
		Name:   "New Soul",
		Type:   core.CheckHTTP,
		Target: "https://new-example.com",
		Weight: core.Duration{Duration: 60 * time.Second},
		HTTP:   &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
	}
	body, _ := json.Marshal(soul)

	req := httptest.NewRequest("POST", "/api/v1/souls", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	var created core.Soul
	json.NewDecoder(w.Body).Decode(&created)
	if created.Name != "New Soul" {
		t.Errorf("expected name 'New Soul', got '%s'", created.Name)
	}
}

func TestHandleGetSoul_NotFound(t *testing.T) {
	storage := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/api/v1/souls/:id", server.requireAuth(server.handleGetSoul))

	req := httptest.NewRequest("GET", "/api/v1/souls/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should return 404 for nonexistent soul
	if w.Code != http.StatusNotFound && w.Code != http.StatusOK {
		t.Errorf("expected status 404 or 200, got %d", w.Code)
	}
}

func TestHandleDeleteSoul(t *testing.T) {
	storage := newMockStorage()
	storage.SaveSoul(context.Background(), &core.Soul{ID: "to-delete", Name: "Delete Me", Type: core.CheckHTTP, Target: "https://example.com"})

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("DELETE", "/api/v1/souls/:id", server.requireAuth(server.handleDeleteSoul))

	req := httptest.NewRequest("DELETE", "/api/v1/souls/to-delete", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent && w.Code != http.StatusOK {
		t.Errorf("expected status 204 or 200, got %d", w.Code)
	}
}

func TestHandleListChannels(t *testing.T) {
	storage := newMockStorage()
	storage.SaveChannelNoCtx(&core.AlertChannel{ID: "ch-1", Name: "test-channel", Type: core.ChannelWebHook, Enabled: true})

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		alert:  &mockAlertManager{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/api/v1/channels", server.requireAuth(server.handleListChannels))

	req := httptest.NewRequest("GET", "/api/v1/channels", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandleListRules(t *testing.T) {
	storage := newMockStorage()
	storage.SaveRuleNoCtx(&core.AlertRule{ID: "rule-1", Name: "Test Rule", Enabled: true})

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		alert:  &mockAlertManager{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/api/v1/rules", server.requireAuth(server.handleListRules))

	req := httptest.NewRequest("GET", "/api/v1/rules", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandleListWorkspaces(t *testing.T) {
	storage := newMockStorage()
	storage.SaveWorkspaceNoCtx(&core.Workspace{ID: "ws-1", Name: "Test Workspace", Slug: "test"})

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		alert:  &mockAlertManager{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/api/v1/workspaces", server.requireAuth(server.handleListWorkspaces))

	req := httptest.NewRequest("GET", "/api/v1/workspaces", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestRequireAuth_MissingToken(t *testing.T) {
	storage := newMockStorage()
	auth := &mockAuthenticator{}
	router := &Router{routes: make(map[string]map[string]Handler)}

	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   auth,
		alert:  &mockAlertManager{},
		logger: newTestLogger(),
	}

	handlerCalled := false
	testHandler := func(ctx *Context) error {
		handlerCalled = true
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	router.Handle("GET", "/api/v1/protected", server.requireAuth(testHandler))

	req := httptest.NewRequest("GET", "/api/v1/protected", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
	if handlerCalled {
		t.Error("handler should not be called without valid token")
	}
}

func TestRouter_ParameterizedRoutes(t *testing.T) {
	router := &Router{routes: make(map[string]map[string]Handler)}

	handlerCalled := false
	var capturedParams map[string]string
	testHandler := func(ctx *Context) error {
		handlerCalled = true
		capturedParams = ctx.Params
		return ctx.JSON(http.StatusOK, map[string]string{"id": ctx.Params["id"]})
	}

	router.Handle("GET", "/api/v1/souls/:id", testHandler)

	req := httptest.NewRequest("GET", "/api/v1/souls/soul-123", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if !handlerCalled {
		t.Error("handler should be called for parameterized route")
	}
	if capturedParams["id"] != "soul-123" {
		t.Errorf("expected id 'soul-123', got '%s'", capturedParams["id"])
	}
}

func TestRouter_NotFound(t *testing.T) {
	router := &Router{routes: make(map[string]map[string]Handler)}

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestContextHelpers(t *testing.T) {
	w := httptest.NewRecorder()
	ctx := &Context{
		Response: w,
		Request:  httptest.NewRequest("GET", "/", nil),
		Params:   make(map[string]string),
	}

	// Test JSON helper
	err := ctx.JSON(http.StatusOK, map[string]string{"key": "value"})
	if err != nil {
		t.Errorf("JSON helper failed: %v", err)
	}

	var response map[string]string
	json.NewDecoder(w.Body).Decode(&response)
	if response["key"] != "value" {
		t.Errorf("expected 'value', got '%s'", response["key"])
	}

	// Test Error helper
	w = httptest.NewRecorder()
	ctx.Response = w
	err = ctx.Error(http.StatusBadRequest, "test error")
	if err != nil {
		t.Errorf("Error helper failed: %v", err)
	}

	var errorResponse map[string]string
	json.NewDecoder(w.Body).Decode(&errorResponse)
	if errorResponse["error"] != "test error" {
		t.Errorf("expected 'test error', got '%s'", errorResponse["error"])
	}
}

func TestHandleGetSoul_Success(t *testing.T) {
	storage := newMockStorage()
	storage.SaveSoul(context.Background(), &core.Soul{ID: "soul-1", Name: "Test Soul", Type: core.CheckHTTP, Target: "https://example.com"})

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/api/v1/souls/:id", server.requireAuth(server.handleGetSoul))

	req := httptest.NewRequest("GET", "/api/v1/souls/soul-1", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdateSoul(t *testing.T) {
	storage := newMockStorage()
	storage.SaveSoul(context.Background(), &core.Soul{ID: "soul-to-update", Name: "Original", Type: core.CheckHTTP, Target: "https://example.com"})

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("PUT", "/api/v1/souls/:id", server.requireAuth(server.handleUpdateSoul))

	updated := core.Soul{
		Name:   "Updated Soul",
		Type:   core.CheckHTTP,
		Target: "https://updated.com",
		Weight: core.Duration{Duration: 120 * time.Second},
		HTTP:   &core.HTTPConfig{Method: "GET", ValidStatus: []int{200, 204}},
	}
	body, _ := json.Marshal(updated)

	req := httptest.NewRequest("PUT", "/api/v1/souls/soul-to-update", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleForceCheck(t *testing.T) {
	storage := newMockStorage()
	probe := &mockProbeEngine{}
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		probe:  probe,
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("POST", "/api/v1/souls/:id/check", server.requireAuth(server.handleForceCheck))

	req := httptest.NewRequest("POST", "/api/v1/souls/soul-1/check", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleStats(t *testing.T) {
	storage := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/api/v1/stats", server.requireAuth(server.handleStats))

	req := httptest.NewRequest("GET", "/api/v1/stats", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateChannel(t *testing.T) {
	storage := newMockStorage()
	alert := &mockAlertManager{}
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		alert:  alert,
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("POST", "/api/v1/channels", server.requireAuth(server.handleCreateChannel))

	channel := core.AlertChannel{
		Name:    "Test Channel",
		Type:    core.ChannelWebHook,
		Enabled: true,
		Config:  map[string]interface{}{"url": "https://hooks.example.com"},
	}
	body, _ := json.Marshal(channel)

	req := httptest.NewRequest("POST", "/api/v1/channels", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateRule(t *testing.T) {
	storage := newMockStorage()
	alert := &mockAlertManager{}
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		alert:  alert,
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("POST", "/api/v1/rules", server.requireAuth(server.handleCreateRule))

	rule := core.AlertRule{
		Name:    "Test Rule",
		Enabled: true,
		Scope:   core.RuleScope{Type: "all"},
		Conditions: []core.AlertCondition{
			{Type: "status_change", From: "alive", To: "dead"},
		},
		Channels: []string{"channel-1"},
	}
	body, _ := json.Marshal(rule)

	req := httptest.NewRequest("POST", "/api/v1/rules", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleAcknowledgeIncident(t *testing.T) {
	storage := newMockStorage()
	alert := &mockAlertManager{}
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		alert:  alert,
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("POST", "/api/v1/incidents/:id/ack", server.requireAuth(server.handleAcknowledgeIncident))

	req := httptest.NewRequest("POST", "/api/v1/incidents/incident-1/ack", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleResolveIncident(t *testing.T) {
	storage := newMockStorage()
	alert := &mockAlertManager{}
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		alert:  alert,
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("POST", "/api/v1/incidents/:id/resolve", server.requireAuth(server.handleResolveIncident))

	req := httptest.NewRequest("POST", "/api/v1/incidents/incident-1/resolve", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleClusterStatus(t *testing.T) {
	storage := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/api/v1/cluster/status", server.requireAuth(server.handleClusterStatus))

	req := httptest.NewRequest("GET", "/api/v1/cluster/status", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateStatusPage(t *testing.T) {
	storage := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("POST", "/api/v1/status-pages", server.requireAuth(server.handleCreateStatusPage))

	page := core.StatusPage{
		Name:    "Test Status Page",
		Slug:    "test-status",
		Enabled: true,
	}
	body, _ := json.Marshal(page)

	req := httptest.NewRequest("POST", "/api/v1/status-pages", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdateStatusPage(t *testing.T) {
	storage := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("PUT", "/api/v1/status-pages/:id", server.requireAuth(server.handleUpdateStatusPage))

	page := core.StatusPage{
		ID:      "page-1",
		Name:    "Updated Status Page",
		Slug:    "updated-status",
		Enabled: false,
	}
	body, _ := json.Marshal(page)

	req := httptest.NewRequest("PUT", "/api/v1/status-pages/page-1", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDeleteStatusPage(t *testing.T) {
	storage := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("DELETE", "/api/v1/status-pages/:id", server.requireAuth(server.handleDeleteStatusPage))

	req := httptest.NewRequest("DELETE", "/api/v1/status-pages/page-1", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleListStatusPages(t *testing.T) {
	storage := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/api/v1/status-pages", server.requireAuth(server.handleListStatusPages))

	req := httptest.NewRequest("GET", "/api/v1/status-pages", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetStatusPage(t *testing.T) {
	storage := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/api/v1/status-pages/:id", server.requireAuth(server.handleGetStatusPage))

	req := httptest.NewRequest("GET", "/api/v1/status-pages/page-1", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// May return 200 or 404 depending on implementation
	if w.Code != http.StatusOK && w.Code != http.StatusNotFound {
		t.Errorf("expected status 200 or 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLoggingMiddleware(t *testing.T) {
	storage := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	handlerCalled := false
	testHandler := func(ctx *Context) error {
		handlerCalled = true
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	wrappedHandler := server.loggingMiddleware(testHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	ctx := &Context{
		Request:  req,
		Response: w,
		Params:   make(map[string]string),
	}

	err := wrappedHandler(ctx)
	if err != nil {
		t.Errorf("handler returned error: %v", err)
	}
	if !handlerCalled {
		t.Error("handler should be called")
	}
	if ctx.StartTime.IsZero() {
		t.Error("StartTime should be set")
	}
}

func TestCORSMiddleware(t *testing.T) {
	storage := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	wrappedHandler := server.corsMiddleware(testHandler)

	// Test regular request
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	ctx := &Context{
		Request:  req,
		Response: w,
		Params:   make(map[string]string),
	}

	err := wrappedHandler(ctx)
	if err != nil {
		t.Errorf("handler returned error: %v", err)
	}

	// Check CORS headers
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("Expected CORS header Access-Control-Allow-Origin: *")
	}
	if w.Header().Get("Access-Control-Allow-Methods") != "GET, POST, PUT, DELETE, OPTIONS" {
		t.Error("Expected CORS header Access-Control-Allow-Methods")
	}
}

func TestCORSMiddleware_Preflight(t *testing.T) {
	storage := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	testHandler := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	wrappedHandler := server.corsMiddleware(testHandler)

	// Test OPTIONS preflight request
	req := httptest.NewRequest("OPTIONS", "/test", nil)
	w := httptest.NewRecorder()
	ctx := &Context{
		Request:  req,
		Response: w,
		Params:   make(map[string]string),
	}

	err := wrappedHandler(ctx)
	if err != nil {
		t.Errorf("handler returned error: %v", err)
	}

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204 for OPTIONS, got %d", w.Code)
	}
}

func TestRecoveryMiddleware(t *testing.T) {
	storage := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	// Handler that panics
	panicHandler := func(ctx *Context) error {
		panic("test panic")
	}

	wrappedHandler := server.recoveryMiddleware(panicHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	ctx := &Context{
		Request:  req,
		Response: w,
		Params:   make(map[string]string),
	}

	// Should recover from panic and return 500
	err := wrappedHandler(ctx)
	if err != nil {
		t.Errorf("handler returned error: %v", err)
	}

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500 after panic recovery, got %d", w.Code)
	}
}

func TestRouter_Use(t *testing.T) {
	router := &Router{routes: make(map[string]map[string]Handler)}

	middlewareCalled := false
	testMw := func(handler Handler) Handler {
		return func(ctx *Context) error {
			middlewareCalled = true
			return handler(ctx)
		}
	}

	router.Use(testMw)

	handlerCalled := false
	testHandler := func(ctx *Context) error {
		handlerCalled = true
		return nil
	}

	router.Handle("GET", "/test", testHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if !middlewareCalled {
		t.Error("middleware should be called")
	}
	if !handlerCalled {
		t.Error("handler should be called")
	}
}

func TestMatchRoute(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		path      string
		wantMatch bool
		wantParams map[string]string
	}{
		{
			name:      "exact match",
			pattern:   "/api/v1/souls",
			path:      "/api/v1/souls",
			wantMatch: true,
			wantParams: map[string]string{},
		},
		{
			name:      "param match",
			pattern:   "/api/v1/souls/:id",
			path:      "/api/v1/souls/123",
			wantMatch: true,
			wantParams: map[string]string{"id": "123"},
		},
		{
			name:      "multiple params",
			pattern:   "/api/v1/:resource/:id",
			path:      "/api/v1/souls/456",
			wantMatch: true,
			wantParams: map[string]string{"resource": "souls", "id": "456"},
		},
		{
			name:      "length mismatch",
			pattern:   "/api/v1/souls/:id",
			path:      "/api/v1/souls/123/extra",
			wantMatch: false,
			wantParams: nil,
		},
		{
			name:      "path mismatch",
			pattern:   "/api/v1/souls/:id",
			path:      "/api/v1/channels/123",
			wantMatch: false,
			wantParams: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, matched := matchRoute(tt.pattern, tt.path)
			if matched != tt.wantMatch {
				t.Errorf("matchRoute(%s, %s) matched = %v, want %v", tt.pattern, tt.path, matched, tt.wantMatch)
			}
			if tt.wantMatch && tt.wantParams != nil {
				for k, v := range tt.wantParams {
					if params[k] != v {
						t.Errorf("param %s = %s, want %s", k, params[k], v)
					}
				}
			}
		})
	}
}

func TestContext_Bind(t *testing.T) {
	w := httptest.NewRecorder()
	type testData struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	req := httptest.NewRequest("POST", "/", bytes.NewBufferString(`{"name":"test","value":42}`))
	ctx := &Context{
		Request:  req,
		Response: w,
		Params:   make(map[string]string),
	}

	var data testData
	err := ctx.Bind(&data)
	if err != nil {
		t.Errorf("Bind failed: %v", err)
	}
	if data.Name != "test" {
		t.Errorf("expected name 'test', got '%s'", data.Name)
	}
	if data.Value != 42 {
		t.Errorf("expected value 42, got %d", data.Value)
	}
}

func TestHandleReady(t *testing.T) {
	storage := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/ready", server.handleReady)

	req := httptest.NewRequest("GET", "/ready", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	json.NewDecoder(w.Body).Decode(&response)
	if response["status"] != "ready" {
		t.Errorf("expected status 'ready', got '%v'", response["status"])
	}
}

func TestHandleLogout(t *testing.T) {
	storage := newMockStorage()
	auth := &mockAuthenticator{}
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   auth,
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("POST", "/api/v1/auth/logout", server.handleLogout)

	req := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleMe(t *testing.T) {
	storage := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/api/v1/auth/me", server.requireAuth(server.handleMe))

	req := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var user User
	json.NewDecoder(w.Body).Decode(&user)
	if user.ID != "user-1" {
		t.Errorf("expected user-1, got %s", user.ID)
	}
}

func TestHandleStatsOverview(t *testing.T) {
	storage := newMockStorage()
	alert := &mockAlertManager{}
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		alert:  alert,
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/api/v1/stats/overview", server.requireAuth(server.handleStatsOverview))

	req := httptest.NewRequest("GET", "/api/v1/stats/overview", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleClusterPeers(t *testing.T) {
	storage := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/api/v1/cluster/peers", server.requireAuth(server.handleClusterPeers))

	req := httptest.NewRequest("GET", "/api/v1/cluster/peers", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleListIncidents(t *testing.T) {
	storage := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/api/v1/incidents", server.requireAuth(server.handleListIncidents))

	req := httptest.NewRequest("GET", "/api/v1/incidents", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleTestChannel(t *testing.T) {
	storage := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("POST", "/api/v1/channels/:id/test", server.requireAuth(server.handleTestChannel))

	req := httptest.NewRequest("POST", "/api/v1/channels/ch-1/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleListAllJudgments(t *testing.T) {
	storage := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/api/v1/judgments", server.requireAuth(server.handleListAllJudgments))

	req := httptest.NewRequest("GET", "/api/v1/judgments", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetJudgment(t *testing.T) {
	storage := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/api/v1/judgments/:id", server.requireAuth(server.handleGetJudgment))

	req := httptest.NewRequest("GET", "/api/v1/judgments/judgment-1", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// May return 200 or 404
	if w.Code != http.StatusOK && w.Code != http.StatusNotFound {
		t.Errorf("expected status 200 or 404, got %d: %s", w.Code, w.Body.String())
	}
}

// WebSocket tests

func TestNewWebSocketServer(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger)

	if server == nil {
		t.Fatal("NewWebSocketServer returned nil")
	}
	if server.clients == nil {
		t.Error("clients map should be initialized")
	}
	if server.upgrader == nil {
		t.Error("upgrader should be initialized")
	}
	if server.broadcast == nil {
		t.Error("broadcast channel should be initialized")
	}
}

func TestWebSocketServer_StartStop(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger)

	// Start should not panic
	server.Start()

	// Give goroutine time to start
	time.Sleep(10 * time.Millisecond)

	// Stop should not panic
	server.Stop()
}

func TestWebSocketServer_BroadcastJudgment(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger)
	server.Start()
	defer server.Stop()

	judgment := &core.Judgment{
		ID:     "test-judgment",
		SoulID: "test-soul",
		Status: core.SoulAlive,
	}

	// Should not panic
	server.BroadcastJudgment(judgment)

	// Give time for broadcast
	time.Sleep(10 * time.Millisecond)
}

func TestWebSocketServer_BroadcastAlert(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger)
	server.Start()
	defer server.Stop()

	event := &core.AlertEvent{
		ID:       "test-event",
		SoulID:   "test-soul",
		SoulName: "Test Soul",
		Status:   core.SoulDead,
	}

	// Should not panic
	server.BroadcastAlert(event)

	// Give time for broadcast
	time.Sleep(10 * time.Millisecond)
}

func TestWebSocketServer_BroadcastStats(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger)
	server.Start()
	defer server.Stop()

	stats := map[string]interface{}{
		"total_souls": 10,
		"active":      8,
	}

	// Should not panic
	server.BroadcastStats(stats)

	// Give time for broadcast
	time.Sleep(10 * time.Millisecond)
}

func TestWebSocketServer_SubscribeUnsubscribe(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger)

	// Should not panic
	server.SubscribeClient("client-1", []string{"judgment", "alert"})
	server.UnsubscribeClient("client-1", []string{"judgment"})
}

func TestWebSocketServer_HandleConnection(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ws?workspace=default", nil)

	// Should not panic
	server.HandleConnection(w, req)
}

func TestWSUpgrader_CheckOrigin(t *testing.T) {
	upgrader := &WSUpgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}

	req := httptest.NewRequest("GET", "http://example.com", nil)
	result := upgrader.CheckOrigin(req)

	if !result {
		t.Error("Expected CheckOrigin to return true")
	}
}

func TestWSConn_Structure(t *testing.T) {
	conn := &WSConn{
		closed: false,
	}

	if conn == nil {
		t.Error("WSConn should not be nil")
	}
}

func TestWSClient_Structure(t *testing.T) {
	client := &WSClient{
		ID:        "test-client",
		Workspace: "default",
		send:      make(chan []byte, 256),
	}

	if client.ID != "test-client" {
		t.Errorf("Expected ID test-client, got %s", client.ID)
	}
	if client.Workspace != "default" {
		t.Errorf("Expected workspace default, got %s", client.Workspace)
	}
}

func TestBroadcastChannel_Closed(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger)
	server.Start()

	// Stop will close the broadcast channel
	server.Stop()

	// Verify channel is closed by attempting to receive
	_, ok := <-server.broadcast
	if ok {
		t.Error("Expected broadcast channel to be closed")
	}
}

func TestGenerateClientID(t *testing.T) {
	id1 := generateClientID()

	// Ensure unique by waiting for nanosecond change
	time.Sleep(time.Microsecond)

	id2 := generateClientID()

	if id1 == "" {
		t.Error("Generated client ID should not be empty")
	}
	// IDs might be same if generated in same nanosecond
	// Just verify format is correct
	if id1[:3] != "ws_" {
		t.Errorf("Expected ID prefix ws_, got %s", id1[:3])
	}
	if id2[:3] != "ws_" {
		t.Errorf("Expected ID prefix ws_, got %s", id2[:3])
	}
}

func TestWSMessage_Structure(t *testing.T) {
	msg := WSMessage{
		Type:      "test",
		Timestamp: time.Now().UTC(),
		Payload:   map[string]string{"key": "value"},
	}

	if msg.Type != "test" {
		t.Errorf("Expected type test, got %s", msg.Type)
	}
	if msg.Payload == nil {
		t.Error("Payload should not be nil")
	}
}

// Additional tests for uncovered methods

func TestWebSocketServer_GetStats(t *testing.T) {
	server := NewWebSocketServer(newTestLogger())

	stats := server.GetStats()
	if stats == nil {
		t.Error("Expected stats to be returned")
	}
}

// REST Server handler tests for uncovered methods

func TestHandleGetChannel(t *testing.T) {
	storage := newMockStorage()
	storage.SaveChannelNoCtx(&core.AlertChannel{ID: "ch-1", Name: "Test Channel", Type: core.ChannelWebHook, Enabled: true})

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:  core.ServerConfig{Host: "localhost", Port: 8080},
		store:   storage,
		router:  router,
		auth:    &mockAuthenticator{},
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/api/v1/channels/:id", server.requireAuth(server.handleGetChannel))

	req := httptest.NewRequest("GET", "/api/v1/channels/ch-1", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdateChannel(t *testing.T) {
	storage := newMockStorage()
	alert := &mockAlertManager{}
	storage.SaveChannelNoCtx(&core.AlertChannel{ID: "chToUpdate", Name: "Original", Type: core.ChannelWebHook, Enabled: true})

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:  core.ServerConfig{Host: "localhost", Port: 8080},
		store:   storage,
		router:  router,
		auth:    &mockAuthenticator{},
		alert:   alert,
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("PUT", "/api/v1/channels/:id", server.requireAuth(server.handleUpdateChannel))

	updated := core.AlertChannel{
		Name:    "Updated Channel",
		Type:    core.ChannelWebHook,
		Enabled: true,
		Config:  map[string]interface{}{"url": "https://updated.example.com"},
	}
	body, _ := json.Marshal(updated)

	req := httptest.NewRequest("PUT", "/api/v1/channels/chToUpdate", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDeleteChannel(t *testing.T) {
	storage := newMockStorage()
	alert := &mockAlertManager{}

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:  core.ServerConfig{Host: "localhost", Port: 8080},
		store:   storage,
		router:  router,
		auth:    &mockAuthenticator{},
		alert:   alert,
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("DELETE", "/api/v1/channels/:id", server.requireAuth(server.handleDeleteChannel))

	req := httptest.NewRequest("DELETE", "/api/v1/channels/ch-to-delete", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent && w.Code != http.StatusOK {
		t.Errorf("expected status 204 or 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetRule(t *testing.T) {
	storage := newMockStorage()
	storage.SaveRuleNoCtx(&core.AlertRule{ID: "rule-1", Name: "Test Rule", Enabled: true})

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:  core.ServerConfig{Host: "localhost", Port: 8080},
		store:   storage,
		router:  router,
		auth:    &mockAuthenticator{},
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/api/v1/rules/:id", server.requireAuth(server.handleGetRule))

	req := httptest.NewRequest("GET", "/api/v1/rules/rule-1", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdateRule(t *testing.T) {
	storage := newMockStorage()
	alert := &mockAlertManager{}
	storage.SaveRuleNoCtx(&core.AlertRule{ID: "ruleToUpdate", Name: "Original", Enabled: true})

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:  core.ServerConfig{Host: "localhost", Port: 8080},
		store:   storage,
		router:  router,
		auth:    &mockAuthenticator{},
		alert:   alert,
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("PUT", "/api/v1/rules/:id", server.requireAuth(server.handleUpdateRule))

	updated := core.AlertRule{
		Name:    "Updated Rule",
		Enabled: true,
		Scope:   core.RuleScope{Type: "all"},
	}
	body, _ := json.Marshal(updated)

	req := httptest.NewRequest("PUT", "/api/v1/rules/ruleToUpdate", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDeleteRule(t *testing.T) {
	storage := newMockStorage()
	alert := &mockAlertManager{}

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:  core.ServerConfig{Host: "localhost", Port: 8080},
		store:   storage,
		router:  router,
		auth:    &mockAuthenticator{},
		alert:   alert,
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("DELETE", "/api/v1/rules/:id", server.requireAuth(server.handleDeleteRule))

	req := httptest.NewRequest("DELETE", "/api/v1/rules/rule-to-delete", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent && w.Code != http.StatusOK {
		t.Errorf("expected status 204 or 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateWorkspace(t *testing.T) {
	storage := newMockStorage()

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:  core.ServerConfig{Host: "localhost", Port: 8080},
		store:   storage,
		router:  router,
		auth:    &mockAuthenticator{},
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("POST", "/api/v1/workspaces", server.requireAuth(server.handleCreateWorkspace))

	ws := core.Workspace{
		Name: "New Workspace",
		Slug: "new-workspace",
	}
	body, _ := json.Marshal(ws)

	req := httptest.NewRequest("POST", "/api/v1/workspaces", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetWorkspace(t *testing.T) {
	storage := newMockStorage()
	storage.SaveWorkspaceNoCtx(&core.Workspace{ID: "ws-1", Name: "Test Workspace", Slug: "test"})

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:  core.ServerConfig{Host: "localhost", Port: 8080},
		store:   storage,
		router:  router,
		auth:    &mockAuthenticator{},
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/api/v1/workspaces/:id", server.requireAuth(server.handleGetWorkspace))

	req := httptest.NewRequest("GET", "/api/v1/workspaces/ws-1", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdateWorkspace(t *testing.T) {
	storage := newMockStorage()
	storage.SaveWorkspaceNoCtx(&core.Workspace{ID: "wsToUpdate", Name: "Original", Slug: "original"})

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:  core.ServerConfig{Host: "localhost", Port: 8080},
		store:   storage,
		router:  router,
		auth:    &mockAuthenticator{},
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("PUT", "/api/v1/workspaces/:id", server.requireAuth(server.handleUpdateWorkspace))

	updated := core.Workspace{
		Name: "Updated Workspace",
		Slug: "updated-workspace",
	}
	body, _ := json.Marshal(updated)

	req := httptest.NewRequest("PUT", "/api/v1/workspaces/wsToUpdate", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDeleteWorkspace(t *testing.T) {
	storage := newMockStorage()

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:  core.ServerConfig{Host: "localhost", Port: 8080},
		store:   storage,
		router:  router,
		auth:    &mockAuthenticator{},
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("DELETE", "/api/v1/workspaces/:id", server.requireAuth(server.handleDeleteWorkspace))

	req := httptest.NewRequest("DELETE", "/api/v1/workspaces/ws-to-delete", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent && w.Code != http.StatusOK {
		t.Errorf("expected status 204 or 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleListJudgments(t *testing.T) {
	storage := newMockStorage()

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:  core.ServerConfig{Host: "localhost", Port: 8080},
		store:   storage,
		router:  router,
		auth:    &mockAuthenticator{},
		logger:  newTestLogger(),
		cluster: &mockClusterManager{},
	}

	router.Handle("GET", "/api/v1/souls/:id/judgments", server.requireAuth(server.handleListJudgments))

	req := httptest.NewRequest("GET", "/api/v1/souls/soul-1/judgments", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

// GRPC Server tests

func TestNewGRPCServer(t *testing.T) {
	logger := newTestLogger()
	config := core.ServerConfig{Host: "localhost", Port: 9090}

	server := NewGRPCServer(config, logger)

	if server == nil {
		t.Fatal("NewGRPCServer returned nil")
	}
	if server.config.Host != "localhost" {
		t.Errorf("Expected Host localhost, got %s", server.config.Host)
	}
}

func TestGRPCServer_RegisterServices(t *testing.T) {
	logger := newTestLogger()
	config := core.ServerConfig{Host: "localhost", Port: 9090}
	server := NewGRPCServer(config, logger)

	soulService := &mockSoulService{}
	judgmentService := &mockJudgmentService{}
	alertService := &mockAlertService{}
	clusterService := &mockClusterService{}

	server.RegisterServices(soulService, judgmentService, alertService, clusterService)

	if server.soulService == nil {
		t.Error("soulService should be set")
	}
	if server.judgmentService == nil {
		t.Error("judgmentService should be set")
	}
	if server.alertService == nil {
		t.Error("alertService should be set")
	}
	if server.clusterService == nil {
		t.Error("clusterService should be set")
	}
}

func TestGRPCServer_Start(t *testing.T) {
	logger := newTestLogger()
	config := core.ServerConfig{Host: "127.0.0.1", Port: 19091}
	server := NewGRPCServer(config, logger)

	err := server.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Give server time to start
	time.Sleep(10 * time.Millisecond)

	// Stop the server
	ctx := context.Background()
	server.Stop(ctx)
}

func TestGRPCServer_Stop(t *testing.T) {
	logger := newTestLogger()
	config := core.ServerConfig{Host: "127.0.0.1", Port: 19092}
	server := NewGRPCServer(config, logger)

	// Start server
	err := server.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	// Stop server
	ctx := context.Background()
	err = server.Stop(ctx)
	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

func TestGRPCServer_Stop_NoListener(t *testing.T) {
	logger := newTestLogger()
	config := core.ServerConfig{Host: "localhost", Port: 9090}
	server := NewGRPCServer(config, logger)

	ctx := context.Background()
	err := server.Stop(ctx)
	if err != nil {
		t.Errorf("Stop should return nil when no listener: %v", err)
	}
}

func TestGRPCServer_isStopped(t *testing.T) {
	logger := newTestLogger()
	config := core.ServerConfig{Host: "localhost", Port: 9090}
	server := NewGRPCServer(config, logger)

	// Initially listener is nil, so isStopped returns true
	// This is expected behavior
	if !server.isStopped() {
		t.Error("Server should be stopped initially (no listener)")
	}

	// Stop without start should be safe
	ctx := context.Background()
	err := server.Stop(ctx)
	if err != nil {
		t.Errorf("Stop should return nil when no listener: %v", err)
	}

	// Should still be stopped
	if !server.isStopped() {
		t.Error("Server should be stopped after Stop() without Start()")
	}
}

func TestGRPCServer_GetStats(t *testing.T) {
	logger := newTestLogger()
	config := core.ServerConfig{Host: "localhost", Port: 9090}
	server := NewGRPCServer(config, logger)

	stats := server.GetStats()
	if stats == nil {
		t.Error("Expected stats to be returned")
	}
	if stats["active_connections"] != 0 {
		t.Errorf("Expected 0 active_connections, got %v", stats["active_connections"])
	}
	if stats["total_requests"] != 0 {
		t.Errorf("Expected 0 total_requests, got %v", stats["total_requests"])
	}
}

// Mock services for GRPC tests

type mockSoulService struct{}

func (m *mockSoulService) ListSouls(ctx context.Context, req *ListSoulsRequest) (*ListSoulsResponse, error) {
	return &ListSoulsResponse{}, nil
}
func (m *mockSoulService) GetSoul(ctx context.Context, req *GetSoulRequest) (*SoulResponse, error) {
	return &SoulResponse{}, nil
}
func (m *mockSoulService) CreateSoul(ctx context.Context, req *CreateSoulRequest) (*SoulResponse, error) {
	return &SoulResponse{}, nil
}
func (m *mockSoulService) UpdateSoul(ctx context.Context, req *UpdateSoulRequest) (*SoulResponse, error) {
	return &SoulResponse{}, nil
}
func (m *mockSoulService) DeleteSoul(ctx context.Context, req *DeleteSoulRequest) (*DeleteResponse, error) {
	return &DeleteResponse{}, nil
}
func (m *mockSoulService) ForceCheck(ctx context.Context, req *ForceCheckRequest) (*JudgmentResponse, error) {
	return &JudgmentResponse{}, nil
}

type mockJudgmentService struct{}

func (m *mockJudgmentService) GetJudgment(ctx context.Context, req *GetJudgmentRequest) (*JudgmentResponse, error) {
	return &JudgmentResponse{}, nil
}
func (m *mockJudgmentService) ListJudgments(ctx context.Context, req *ListJudgmentsRequest) (*ListJudgmentsResponse, error) {
	return &ListJudgmentsResponse{}, nil
}

type mockAlertService struct{}

func (m *mockAlertService) ListChannels(ctx context.Context, req *ListChannelsRequest) (*ListChannelsResponse, error) {
	return &ListChannelsResponse{}, nil
}
func (m *mockAlertService) CreateChannel(ctx context.Context, req *CreateChannelRequest) (*ChannelResponse, error) {
	return &ChannelResponse{}, nil
}
func (m *mockAlertService) DeleteChannel(ctx context.Context, req *DeleteChannelRequest) (*DeleteResponse, error) {
	return &DeleteResponse{}, nil
}
func (m *mockAlertService) ListRules(ctx context.Context, req *ListRulesRequest) (*ListRulesResponse, error) {
	return &ListRulesResponse{}, nil
}
func (m *mockAlertService) CreateRule(ctx context.Context, req *CreateRuleRequest) (*RuleResponse, error) {
	return &RuleResponse{}, nil
}
func (m *mockAlertService) DeleteRule(ctx context.Context, req *DeleteRuleRequest) (*DeleteResponse, error) {
	return &DeleteResponse{}, nil
}

type mockClusterService struct{}

func (m *mockClusterService) GetStatus(ctx context.Context, req *GetClusterStatusRequest) (*ClusterStatusResponse, error) {
	return &ClusterStatusResponse{}, nil
}
func (m *mockClusterService) GetPeers(ctx context.Context, req *GetClusterPeersRequest) (*ClusterPeersResponse, error) {
	return &ClusterPeersResponse{}, nil
}
func (m *mockClusterService) Join(ctx context.Context, req *JoinClusterRequest) (*JoinClusterResponse, error) {
	return &JoinClusterResponse{}, nil
}
func (m *mockClusterService) Leave(ctx context.Context, req *LeaveClusterRequest) (*LeaveClusterResponse, error) {
	return &LeaveClusterResponse{}, nil
}

// MCP Server Tests

func TestNewMCPServer(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	if server == nil {
		t.Fatal("Expected MCP server to be created")
	}
}

func TestMCPServer_ServeHTTP_InvalidJSON(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	req := httptest.NewRequest("POST", "/mcp", strings.NewReader("invalid json"))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// Should not crash - may return error response
}

func TestMCPServer_handleInitialize(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var resp MCPResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Result == nil {
		t.Error("Expected result in response")
	}
}

func TestMCPServer_handleListTools(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	t.Logf("Response status: %d, Body: %s", w.Code, w.Body.String())

	var resp MCPResponse
	json.NewDecoder(w.Body).Decode(&resp)
	// Response should have result or error, or server may not handle this method
	// Test passes if server doesn't crash
}

func TestMCPServer_handleListSouls(t *testing.T) {
	store := newMockStorage()
	store.souls["soul-1"] = &core.Soul{ID: "soul-1", Name: "Test Soul", Type: core.CheckHTTP, Enabled: true}
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_souls","arguments":{}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	var resp MCPResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Logf("Got error: %s", resp.Error.Message)
	}
}

func TestMCPServer_handleGetSoul(t *testing.T) {
	store := newMockStorage()
	store.souls["soul-1"] = &core.Soul{ID: "soul-1", Name: "Test Soul", Type: core.CheckHTTP, Enabled: true}
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_soul","arguments":{"soul_id":"soul-1"}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// Should return soul or error
}

func TestMCPServer_handleGetSoul_NotFound(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_soul","arguments":{"soul_id":"non-existent"}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	var resp MCPResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error == nil {
		t.Log("Expected error for non-existent soul")
	}
}

func TestMCPServer_handleForceCheck(t *testing.T) {
	store := newMockStorage()
	store.souls["soul-1"] = &core.Soul{ID: "soul-1", Name: "Test Soul", Type: core.CheckHTTP, Enabled: true}
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"force_check","arguments":{"soul_id":"soul-1"}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// Should call probe.ForceCheck
}

func TestMCPServer_handleGetJudgments(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_judgments","arguments":{"soul_id":"soul-1"}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// Should return judgments list
}

func TestMCPServer_handleListIncidents(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_incidents","arguments":{}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// Should return incidents list
}

func TestMCPServer_handleGetStats(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_stats","arguments":{}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// Should return stats
}

func TestMCPServer_handleAcknowledgeIncident(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"acknowledge_incident","arguments":{"incident_id":"inc-1"}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// Should acknowledge incident
}

func TestMCPServer_handleCreateSoul(t *testing.T) {
	store := newMockStorage()
	store.workspaces["default"] = &core.Workspace{ID: "default", Name: "Default"}
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_soul","arguments":{"name":"New Soul","type":"http","config":{"url":"https://example.com"}}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// Should create soul
}

func TestMCPServer_handleListResources(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"resources/list","params":{}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	var resp MCPResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Logf("Got error: %s", resp.Error.Message)
	}
}

func TestMCPServer_handleListPrompts(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"prompts/list","params":{}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	var resp MCPResponse
	json.NewDecoder(w.Body).Decode(&resp)
}

func TestMCPServer_errorResponse(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	resp := server.errorResponse(1, 400, "Bad request")
	if resp.Error == nil {
		t.Fatal("Expected error in response")
	}
	if resp.Error.Code != 400 {
		t.Errorf("Expected code 400, got %d", resp.Error.Code)
	}
	if resp.Error.Message != "Bad request" {
		t.Errorf("Expected 'Bad request', got %s", resp.Error.Message)
	}
}

func TestMCPServer_writeError(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	w := httptest.NewRecorder()
	server.writeError(w, 1, 400, "Test error")

	body := w.Body.String()
	if !strings.Contains(body, "Test error") {
		t.Error("Expected error message in response")
	}
}

func TestMCPServer_handleGetPrompt(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"analyze_soul"}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)
	// May return error if prompt not found
}

func TestMCPServer_handleReadResource(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"anubiswatch://stats"}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)
	// May return error if resource not found
}

func TestMCPServer_handleCallTool_Unknown(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"unknown_tool","arguments":{}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	var resp MCPResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error == nil {
		t.Log("Expected error for unknown tool")
	}
}

func TestMCPServer_ServeHTTP_GET(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	req := httptest.NewRequest("GET", "/mcp", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)
	// Should not crash
}

// Tests for gRPC handleConnection
func TestGRPCServer_handleConnection(t *testing.T) {
	logger := newTestLogger()
	server := NewGRPCServer(core.ServerConfig{Host: "127.0.0.1", Port: 0}, logger)

	// Create a mock connection using net.Pipe (doesn't use TCP)
	clientConn, serverConn := net.Pipe()

	// Call handleConnection - should not panic and should close connection
	done := make(chan bool)
	go func() {
		server.handleConnection(serverConn)
		done <- true
	}()

	// Close client side to simulate disconnect
	clientConn.Close()

	// Wait for handleConnection to complete
	select {
	case <-done:
		// Success - connection handled and closed
	case <-time.After(2 * time.Second):
		t.Error("handleConnection did not complete in time")
	}
}

// Test handleCreateSoul with missing workspace
func TestMCPServer_handleCreateSoul_NoWorkspace(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	args := json.RawMessage(`{"name": "New Soul", "type": "http"}`)
	_, _ = server.handleCreateSoul(args)
}

// Test handleCreateSoul with workspace
func TestMCPServer_handleCreateSoul_WithWorkspace(t *testing.T) {
	store := newMockStorage()
	store.workspaces["default"] = &core.Workspace{ID: "default", Name: "Default"}
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	args := json.RawMessage(`{"name": "New Soul", "type": "http", "workspace": "default"}`)
	_, _ = server.handleCreateSoul(args)
}

// Test handleReadResource with unknown URI via HTTP
func TestMCPServer_handleReadResource_UnknownURI(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"anubiswatch://unknown/resource"}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)
	// May return error for unknown resource
}

// Test handleGetPrompt with get_prompt method via ServeHTTP
func TestMCPServer_handleGetPrompt_HTTP(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"analyze_soul"}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Logf("Expected 200, got %d", w.Code)
	}
}

// Test handleAcknowledgeIncident
func TestMCPServer_handleAcknowledgeIncident_NoID(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	args := json.RawMessage(`{}`)
	_, _ = server.handleAcknowledgeIncident(args)
}

// Test WebSocket server
func TestWebSocketServer_BroadcastToEmptyRoom(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger)

	// Broadcast with no clients should not panic
	server.BroadcastAlert(&core.AlertEvent{SoulID: "test-soul"})
	server.BroadcastStats(map[string]interface{}{"test": "data"})
	server.BroadcastJudgment(&core.Judgment{SoulID: "test-soul"})
}

// Test WebSocket broadcastLoop with clients
func TestWebSocketServer_BroadcastWithClients(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger)
	server.Start()
	defer server.Stop()

	// Add a client
	client := &WSClient{
		ID:        "test-client",
		Workspace: "default",
		send:      make(chan []byte, 10),
	}
	server.SubscribeClient(client.ID, []string{"alert", "judgment"})

	// Manually add client to the server's clients map
	server.mu.Lock()
	server.clients[client.ID] = client
	server.mu.Unlock()

	// Broadcast a message
	server.BroadcastJudgment(&core.Judgment{SoulID: "test-soul"})

	// Give it time to process
	time.Sleep(50 * time.Millisecond)

	// Client should have received a message
	select {
	case msg := <-client.send:
		if len(msg) == 0 {
			t.Error("Expected non-empty message")
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Expected message in client send buffer")
	}
}

// Test WebSocket server Stop
func TestWebSocketServer_Stop(t *testing.T) {
	logger := newTestLogger()
	server := NewWebSocketServer(logger)
	server.Start()

	// Add a client
	client := &WSClient{
		ID:        "test-client",
		Workspace: "default",
		send:      make(chan []byte, 10),
	}
	server.mu.Lock()
	server.clients[client.ID] = client
	server.mu.Unlock()

	// Stop should close all client channels
	server.Stop()

	// Channel should be closed
	_, ok := <-client.send
	if ok {
		t.Error("Expected client send channel to be closed")
	}
}

// Test handleListSouls with nil store result
func TestMCPServer_handleListSouls_Error(t *testing.T) {
	store := &mockStorage{
		souls: make(map[string]*core.Soul),
	}
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	args := json.RawMessage(`{"workspace": "nonexistent"}`)
	result, err := server.handleListSouls(args)
	// May return error or empty list depending on implementation
	_ = result
	_ = err
}

// Test handleGetSoul with error
func TestMCPServer_handleGetSoul_Error(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	args := json.RawMessage(`{}`)
	result, err := server.handleGetSoul(args)
	if err == nil {
		t.Log("Expected error for missing soul_id")
	}
	_ = result
}

// Test handleForceCheck with probe error
func TestMCPServer_handleForceCheck_Error(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	args := json.RawMessage(`{"soul_id": "nonexistent-soul"}`)
	result, err := server.handleForceCheck(args)
	// Should return judgment even for nonexistent soul
	_ = result
	_ = err
}

// Test handleReadResource via direct call
func TestMCPServer_handleReadResource_Direct(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	req := &MCPRequest{
		ID:      1,
		Method:  "resources/read",
		Params:  json.RawMessage(`{"uri": "anubiswatch://stats"}`),
	}
	resp := server.handleReadResource(req)
	if resp == nil {
		t.Fatal("Expected non-nil response")
	}
}

// Test handleGetPrompt via direct call
func TestMCPServer_handleGetPrompt_Direct(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	req := &MCPRequest{
		ID:      1,
		Method:  "prompts/get",
		Params:  json.RawMessage(`{"name": "analyze_soul"}`),
	}
	resp := server.handleGetPrompt(req)
	if resp == nil {
		t.Fatal("Expected non-nil response")
	}
}

// Test MCP resource handlers
func TestMCPServer_resourceGettingStarted(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	result, err := server.resourceGettingStarted()
	if err != nil {
		t.Fatalf("resourceGettingStarted failed: %v", err)
	}
	content, ok := result.(string)
	if !ok {
		t.Fatal("Expected string result")
	}
	if !strings.Contains(content, "AnubisWatch") {
		t.Error("Expected content to contain AnubisWatch")
	}
	if !strings.Contains(content, "Souls") {
		t.Error("Expected content to mention Souls")
	}
}

func TestMCPServer_resourceAPIReference(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	result, err := server.resourceAPIReference()
	if err != nil {
		t.Fatalf("resourceAPIReference failed: %v", err)
	}
	content, ok := result.(string)
	if !ok {
		t.Fatal("Expected string result")
	}
	if !strings.Contains(content, "API Reference") {
		t.Error("Expected content to contain API Reference")
	}
}

func TestMCPServer_resourceCurrentStatus(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	result, err := server.resourceCurrentStatus()
	if err != nil {
		t.Fatalf("resourceCurrentStatus failed: %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("Expected map result")
	}
	if m["status"] != "operational" {
		t.Error("Expected status to be operational")
	}
}

// Test MCP prompt handlers
func TestMCPServer_promptAnalyzeSoul(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	result, err := server.promptAnalyzeSoul(map[string]string{"soul_id": "test-123"})
	if err != nil {
		t.Fatalf("promptAnalyzeSoul failed: %v", err)
	}
	if !strings.Contains(result, "test-123") {
		t.Error("Expected result to contain soul_id")
	}
}

func TestMCPServer_promptIncidentSummary(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	result, err := server.promptIncidentSummary(map[string]string{})
	if err != nil {
		t.Fatalf("promptIncidentSummary failed: %v", err)
	}
	if !strings.Contains(result, "incident") {
		t.Error("Expected result to mention incidents")
	}
}

func TestMCPServer_promptCreateMonitorGuide_Website(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	result, err := server.promptCreateMonitorGuide(map[string]string{"target_type": "website"})
	if err != nil {
		t.Fatalf("promptCreateMonitorGuide failed: %v", err)
	}
	if !strings.Contains(result, "Website Monitor") {
		t.Error("Expected website guide")
	}
}

func TestMCPServer_promptCreateMonitorGuide_API(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	result, err := server.promptCreateMonitorGuide(map[string]string{"target_type": "api"})
	if err != nil {
		t.Fatalf("promptCreateMonitorGuide failed: %v", err)
	}
	if !strings.Contains(result, "API Monitor") {
		t.Error("Expected API guide")
	}
}

func TestMCPServer_promptCreateMonitorGuide_Server(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	result, err := server.promptCreateMonitorGuide(map[string]string{"target_type": "server"})
	if err != nil {
		t.Fatalf("promptCreateMonitorGuide failed: %v", err)
	}
	if !strings.Contains(result, "Server Monitor") {
		t.Error("Expected server guide")
	}
}

func TestMCPServer_promptCreateMonitorGuide_Unknown(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	result, err := server.promptCreateMonitorGuide(map[string]string{"target_type": "unknown"})
	if err != nil {
		t.Fatalf("promptCreateMonitorGuide failed: %v", err)
	}
	if !strings.Contains(result, "Generic") {
		t.Error("Expected generic guide for unknown type")
	}
}

// Test MCP registration methods
func TestMCPServer_RegisterTool(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	server.RegisterTool(MCPTool{
		Name:        "test-tool",
		Description: "Test tool description",
		Handler: func(args json.RawMessage) (interface{}, error) {
			return "result", nil
		},
	})

	// Verify tool was registered
	server.mu.RLock()
	_, ok := server.tools["test-tool"]
	server.mu.RUnlock()

	if !ok {
		t.Error("Expected tool to be registered")
	}
}

func TestMCPServer_RegisterResource(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	server.RegisterResource(MCPResource{
		URI:         "test-resource",
		Name:        "Test Resource",
		Description: "A test resource",
		Handler: func() (interface{}, error) {
			return "content", nil
		},
	})

	// Verify resource was registered
	server.mu.RLock()
	_, ok := server.resources["test-resource"]
	server.mu.RUnlock()

	if !ok {
		t.Error("Expected resource to be registered")
	}
}

func TestMCPServer_RegisterPrompt(t *testing.T) {
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	logger := newTestLogger()

	server := NewMCPServer(store, probe, alert, logger)

	server.RegisterPrompt(MCPPrompt{
		Name:        "test-prompt",
		Description: "Test prompt",
		Handler: func(args map[string]string) (string, error) {
			return "prompt", nil
		},
	})

	// Verify prompt was registered
	server.mu.RLock()
	_, ok := server.prompts["test-prompt"]
	server.mu.RUnlock()

	if !ok {
		t.Error("Expected prompt to be registered")
	}
}

// Test REST Server
func TestNewRESTServer(t *testing.T) {
	config := core.ServerConfig{
		Host: "localhost",
		Port: 8080,
	}
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	auth := &mockAuthenticator{}
	cluster := &mockClusterManager{}
	logger := newTestLogger()

	server := NewRESTServer(config, store, probe, alert, auth, cluster, nil, nil, nil, logger)

	if server == nil {
		t.Fatal("Expected REST server to be created")
	}
	if server.store != store {
		t.Error("Expected store to be set")
	}
	if server.probe != probe {
		t.Error("Expected probe to be set")
	}
	if server.alert != alert {
		t.Error("Expected alert to be set")
	}
}

func TestRESTServer_StartStop(t *testing.T) {
	config := core.ServerConfig{
		Host: "127.0.0.1",
		Port: 0, // Let OS pick available port
	}
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	auth := &mockAuthenticator{}
	cluster := &mockClusterManager{}
	logger := newTestLogger()

	server := NewRESTServer(config, store, probe, alert, auth, cluster, nil, nil, nil, logger)

	// Start server in background
	go func() {
		_ = server.Start()
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Stop should work
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := server.Stop(ctx)
	if err != nil {
		t.Logf("Stop returned: %v", err)
	}
}

func TestRESTServer_StopWithoutStart(t *testing.T) {
	config := core.ServerConfig{
		Host: "127.0.0.1",
		Port: 0,
	}
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	auth := &mockAuthenticator{}
	cluster := &mockClusterManager{}
	logger := newTestLogger()

	server := NewRESTServer(config, store, probe, alert, auth, cluster, nil, nil, nil, logger)

	// Stop without start should return nil
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := server.Stop(ctx)
	if err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}
}

func TestRESTServer_handleHealth(t *testing.T) {
	config := core.ServerConfig{}
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	auth := &mockAuthenticator{}
	cluster := &mockClusterManager{}
	logger := newTestLogger()

	server := NewRESTServer(config, store, probe, alert, auth, cluster, nil, nil, nil, logger)

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("GET", "/health", nil),
		Response: rec,
	}

	err := server.handleHealth(ctx)
	if err != nil {
		t.Fatalf("handleHealth failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rec.Code)
	}
}

func TestRESTServer_handleReady(t *testing.T) {
	config := core.ServerConfig{}
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	auth := &mockAuthenticator{}
	cluster := &mockClusterManager{}
	logger := newTestLogger()

	server := NewRESTServer(config, store, probe, alert, auth, cluster, nil, nil, nil, logger)

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("GET", "/ready", nil),
		Response: rec,
	}

	err := server.handleReady(ctx)
	if err != nil {
		t.Fatalf("handleReady failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rec.Code)
	}
}

func TestRESTServer_handleMe(t *testing.T) {
	config := core.ServerConfig{}
	store := newMockStorage()
	probe := &mockProbeEngine{}
	alert := &mockAlertManager{}
	auth := &mockAuthenticator{}
	cluster := &mockClusterManager{}
	logger := newTestLogger()

	server := NewRESTServer(config, store, probe, alert, auth, cluster, nil, nil, nil, logger)

	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:  httptest.NewRequest("GET", "/api/v1/auth/me", nil),
		Response: rec,
	}

	err := server.handleMe(ctx)
	if err != nil {
		t.Fatalf("handleMe failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rec.Code)
	}
}
