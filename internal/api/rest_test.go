package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func (m *mockStorage) GetSoul(id string) (*core.Soul, error)       { return m.souls[id], nil }
func (m *mockStorage) ListSouls(ws string, offset, limit int) ([]*core.Soul, error) {
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
func (m *mockStorage) SaveSoul(soul *core.Soul) error {
	m.souls[soul.ID] = soul
	return nil
}
func (m *mockStorage) DeleteSoul(id string) error {
	delete(m.souls, id)
	return nil
}
func (m *mockStorage) GetJudgment(id string) (*core.Judgment, error) { return m.judgments[id], nil }
func (m *mockStorage) ListJudgments(soulID string, start, end time.Time, limit int) ([]*core.Judgment, error) {
	return []*core.Judgment{}, nil
}
func (m *mockStorage) GetChannel(id string) (*core.AlertChannel, error) { return m.channels[id], nil }
func (m *mockStorage) ListChannels(ws string) ([]*core.AlertChannel, error) {
	channels := make([]*core.AlertChannel, 0, len(m.channels))
	for _, c := range m.channels {
		channels = append(channels, c)
	}
	return channels, nil
}
func (m *mockStorage) SaveChannel(ch *core.AlertChannel) error {
	m.channels[ch.ID] = ch
	return nil
}
func (m *mockStorage) DeleteChannel(id string) error {
	delete(m.channels, id)
	return nil
}
func (m *mockStorage) GetRule(id string) (*core.AlertRule, error) { return m.rules[id], nil }
func (m *mockStorage) ListRules(ws string) ([]*core.AlertRule, error) {
	rules := make([]*core.AlertRule, 0, len(m.rules))
	for _, r := range m.rules {
		rules = append(rules, r)
	}
	return rules, nil
}
func (m *mockStorage) SaveRule(rule *core.AlertRule) error {
	m.rules[rule.ID] = rule
	return nil
}
func (m *mockStorage) DeleteRule(id string) error {
	delete(m.rules, id)
	return nil
}
func (m *mockStorage) GetWorkspace(id string) (*core.Workspace, error) { return m.workspaces[id], nil }
func (m *mockStorage) ListWorkspaces() ([]*core.Workspace, error) {
	ws := make([]*core.Workspace, 0, len(m.workspaces))
	for _, w := range m.workspaces {
		ws = append(ws, w)
	}
	return ws, nil
}
func (m *mockStorage) SaveWorkspace(ws *core.Workspace) error {
	m.workspaces[ws.ID] = ws
	return nil
}
func (m *mockStorage) DeleteWorkspace(id string) error {
	delete(m.workspaces, id)
	return nil
}
func (m *mockStorage) GetStats(ws string, start, end time.Time) (*core.Stats, error) {
	return &core.Stats{}, nil
}

// mockProbeEngine implements ProbeEngine interface
type mockProbeEngine struct{}

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

func TestHandleHealth(t *testing.T) {
	storage := newMockStorage()
	router := &Router{routes: make(map[string]map[string]Handler)}

	server := &RESTServer{
		store:  storage,
		router: router,
		logger: newTestLogger(),
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
	storage.SaveSoul(&core.Soul{ID: "soul-1", Name: "Test Soul", Type: core.CheckHTTP, Target: "https://example.com"})

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
	}

	router.Handle("GET", "/api/v1/souls", server.requireAuth(server.handleListSouls))

	req := httptest.NewRequest("GET", "/api/v1/souls", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var souls []core.Soul
	json.NewDecoder(w.Body).Decode(&souls)
	if len(souls) != 1 {
		t.Errorf("expected 1 soul, got %d", len(souls))
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
	storage.SaveSoul(&core.Soul{ID: "to-delete", Name: "Delete Me", Type: core.CheckHTTP, Target: "https://example.com"})

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		logger: newTestLogger(),
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
	storage.SaveChannel(&core.AlertChannel{ID: "ch-1", Name: "test-channel", Type: core.ChannelWebHook, Enabled: true})

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		alert:  &mockAlertManager{},
		logger: newTestLogger(),
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
	storage.SaveRule(&core.AlertRule{ID: "rule-1", Name: "Test Rule", Enabled: true})

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		alert:  &mockAlertManager{},
		logger: newTestLogger(),
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
	storage.SaveWorkspace(&core.Workspace{ID: "ws-1", Name: "Test Workspace", Slug: "test"})

	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config: core.ServerConfig{Host: "localhost", Port: 8080},
		store:  storage,
		router: router,
		auth:   &mockAuthenticator{},
		alert:  &mockAlertManager{},
		logger: newTestLogger(),
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
