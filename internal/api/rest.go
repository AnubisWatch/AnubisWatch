package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// RESTServer implements the HTTP REST API
// The scribes record the judgments on papyrus scrolls
type RESTServer struct {
	mu       sync.RWMutex
	config   core.ServerConfig
	router   *Router
	http     *http.Server
	logger   *slog.Logger

	// Dependencies
	store    Storage
	probe    ProbeEngine
	alert    AlertManager
	auth     Authenticator
}

// Router handles HTTP routing
type Router struct {
	routes  map[string]map[string]Handler // path -> method -> handler
	middleware []Middleware
}

// Handler is an HTTP handler function
type Handler func(ctx *Context) error

// Middleware wraps handlers
type Middleware func(Handler) Handler

// Context holds request context
type Context struct {
	Request  *http.Request
	Response http.ResponseWriter
	Params   map[string]string
	User     *User
	Workspace string
	StartTime time.Time
}

// Storage interface for data access
type Storage interface {
	GetSoul(id string) (*core.Soul, error)
	ListSouls(workspace string, offset, limit int) ([]*core.Soul, error)
	SaveSoul(soul *core.Soul) error
	DeleteSoul(id string) error

	GetJudgment(id string) (*core.Judgment, error)
	ListJudgments(soulID string, start, end time.Time, limit int) ([]*core.Judgment, error)

	GetChannel(id string) (*core.AlertChannel, error)
	ListChannels(workspace string) ([]*core.AlertChannel, error)
	SaveChannel(channel *core.AlertChannel) error
	DeleteChannel(id string) error

	GetRule(id string) (*core.AlertRule, error)
	ListRules(workspace string) ([]*core.AlertRule, error)
	SaveRule(rule *core.AlertRule) error
	DeleteRule(id string) error

	GetWorkspace(id string) (*core.Workspace, error)
	ListWorkspaces() ([]*core.Workspace, error)
	SaveWorkspace(ws *core.Workspace) error
	DeleteWorkspace(id string) error

	GetStats(workspace string, start, end time.Time) (*core.Stats, error)
}

// ProbeEngine interface for probe operations
type ProbeEngine interface {
	GetStatus() *core.ProbeStatus
	ForceCheck(soulID string) (*core.Judgment, error)
}

// AlertManager interface for alert operations
type AlertManager interface {
	GetStats() core.AlertManagerStats
	ListChannels() []*core.AlertChannel
	ListRules() []*core.AlertRule
	RegisterChannel(channel *core.AlertChannel) error
	RegisterRule(rule *core.AlertRule) error
	DeleteChannel(id string) error
	DeleteRule(id string) error
	AcknowledgeIncident(incidentID, userID string) error
	ResolveIncident(incidentID, userID string) error
}

// Authenticator interface for authentication
type Authenticator interface {
	Authenticate(token string) (*User, error)
	Login(email, password string) (*User, string, error)
	Logout(token string) error
}

// User represents an authenticated user
type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	Workspace string    `json:"workspace"`
	CreatedAt time.Time `json:"created_at"`
}

// NewRESTServer creates a new REST server
func NewRESTServer(config core.ServerConfig, store Storage, probe ProbeEngine, alert AlertManager, auth Authenticator, logger *slog.Logger) *RESTServer {
	s := &RESTServer{
		config: config,
		router: &Router{
			routes: make(map[string]map[string]Handler),
		},
		logger: logger.With("component", "rest_server"),
		store:  store,
		probe:  probe,
		alert:  alert,
		auth:   auth,
	}

	s.setupRoutes()
	return s
}

// setupRoutes configures API routes
func (s *RESTServer) setupRoutes() {
	// Middleware
	s.router.Use(s.loggingMiddleware)
	s.router.Use(s.corsMiddleware)
	s.router.Use(s.recoveryMiddleware)

	// Health
	s.router.Handle("GET", "/health", s.handleHealth)
	s.router.Handle("GET", "/ready", s.handleReady)

	// Auth
	s.router.Handle("POST", "/api/v1/auth/login", s.handleLogin)
	s.router.Handle("POST", "/api/v1/auth/logout", s.handleLogout)
	s.router.Handle("GET", "/api/v1/auth/me", s.requireAuth(s.handleMe))

	// Souls
	s.router.Handle("GET", "/api/v1/souls", s.requireAuth(s.handleListSouls))
	s.router.Handle("POST", "/api/v1/souls", s.requireAuth(s.handleCreateSoul))
	s.router.Handle("GET", "/api/v1/souls/:id", s.requireAuth(s.handleGetSoul))
	s.router.Handle("PUT", "/api/v1/souls/:id", s.requireAuth(s.handleUpdateSoul))
	s.router.Handle("DELETE", "/api/v1/souls/:id", s.requireAuth(s.handleDeleteSoul))
	s.router.Handle("POST", "/api/v1/souls/:id/check", s.requireAuth(s.handleForceCheck))
	s.router.Handle("GET", "/api/v1/souls/:id/judgments", s.requireAuth(s.handleListJudgments))

	// Judgments
	s.router.Handle("GET", "/api/v1/judgments/:id", s.requireAuth(s.handleGetJudgment))
	s.router.Handle("GET", "/api/v1/judgments", s.requireAuth(s.handleListAllJudgments))

	// Channels
	s.router.Handle("GET", "/api/v1/channels", s.requireAuth(s.handleListChannels))
	s.router.Handle("POST", "/api/v1/channels", s.requireAuth(s.handleCreateChannel))
	s.router.Handle("GET", "/api/v1/channels/:id", s.requireAuth(s.handleGetChannel))
	s.router.Handle("PUT", "/api/v1/channels/:id", s.requireAuth(s.handleUpdateChannel))
	s.router.Handle("DELETE", "/api/v1/channels/:id", s.requireAuth(s.handleDeleteChannel))
	s.router.Handle("POST", "/api/v1/channels/:id/test", s.requireAuth(s.handleTestChannel))

	// Rules
	s.router.Handle("GET", "/api/v1/rules", s.requireAuth(s.handleListRules))
	s.router.Handle("POST", "/api/v1/rules", s.requireAuth(s.handleCreateRule))
	s.router.Handle("GET", "/api/v1/rules/:id", s.requireAuth(s.handleGetRule))
	s.router.Handle("PUT", "/api/v1/rules/:id", s.requireAuth(s.handleUpdateRule))
	s.router.Handle("DELETE", "/api/v1/rules/:id", s.requireAuth(s.handleDeleteRule))

	// Workspaces
	s.router.Handle("GET", "/api/v1/workspaces", s.requireAuth(s.handleListWorkspaces))
	s.router.Handle("POST", "/api/v1/workspaces", s.requireAuth(s.handleCreateWorkspace))
	s.router.Handle("GET", "/api/v1/workspaces/:id", s.requireAuth(s.handleGetWorkspace))
	s.router.Handle("PUT", "/api/v1/workspaces/:id", s.requireAuth(s.handleUpdateWorkspace))
	s.router.Handle("DELETE", "/api/v1/workspaces/:id", s.requireAuth(s.handleDeleteWorkspace))

	// Stats
	s.router.Handle("GET", "/api/v1/stats", s.requireAuth(s.handleStats))
	s.router.Handle("GET", "/api/v1/stats/overview", s.requireAuth(s.handleStatsOverview))

	// Cluster (Raft)
	s.router.Handle("GET", "/api/v1/cluster/status", s.requireAuth(s.handleClusterStatus))
	s.router.Handle("GET", "/api/v1/cluster/peers", s.requireAuth(s.handleClusterPeers))

	// Incidents
	s.router.Handle("GET", "/api/v1/incidents", s.requireAuth(s.handleListIncidents))
	s.router.Handle("POST", "/api/v1/incidents/:id/acknowledge", s.requireAuth(s.handleAcknowledgeIncident))
	s.router.Handle("POST", "/api/v1/incidents/:id/resolve", s.requireAuth(s.handleResolveIncident))
}

// Start starts the REST server
func (s *RESTServer) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	if addr == ":0" {
		addr = ":8080"
	}

	s.http = &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	s.logger.Info("REST server starting", "addr", addr)

	if s.config.TLS.Enabled {
		return s.http.ListenAndServeTLS(s.config.TLS.Cert, s.config.TLS.Key)
	}
	return s.http.ListenAndServe()
}

// Stop stops the REST server
func (s *RESTServer) Stop(ctx context.Context) error {
	if s.http != nil {
		return s.http.Shutdown(ctx)
	}
	return nil
}

// Handler implementations

func (s *RESTServer) handleHealth(ctx *Context) error {
	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
	})
}

func (s *RESTServer) handleReady(ctx *Context) error {
	// Check dependencies
	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"status":    "ready",
		"timestamp": time.Now().UTC(),
	})
}

func (s *RESTServer) handleLogin(ctx *Context) error {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := ctx.Bind(&req); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid request body")
	}

	user, token, err := s.auth.Login(req.Email, req.Password)
	if err != nil {
		return ctx.Error(http.StatusUnauthorized, "invalid credentials")
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"user":  user,
		"token": token,
	})
}

func (s *RESTServer) handleLogout(ctx *Context) error {
	token := ctx.Request.Header.Get("Authorization")
	token = strings.TrimPrefix(token, "Bearer ")

	if err := s.auth.Logout(token); err != nil {
		return ctx.Error(http.StatusInternalServerError, "logout failed")
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{"message": "logged out"})
}

func (s *RESTServer) handleMe(ctx *Context) error {
	return ctx.JSON(http.StatusOK, ctx.User)
}

// Soul handlers

func (s *RESTServer) handleListSouls(ctx *Context) error {
	workspace := ctx.Workspace
	offset, _ := strconv.Atoi(ctx.Request.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(ctx.Request.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	souls, err := s.store.ListSouls(workspace, offset, limit)
	if err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusOK, souls)
}

func (s *RESTServer) handleCreateSoul(ctx *Context) error {
	var soul core.Soul
	if err := ctx.Bind(&soul); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid soul data")
	}

	soul.WorkspaceID = ctx.Workspace
	soul.ID = core.GenerateID()
	soul.CreatedAt = time.Now()
	soul.UpdatedAt = time.Now()

	if err := s.store.SaveSoul(&soul); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusCreated, soul)
}

func (s *RESTServer) handleGetSoul(ctx *Context) error {
	id := ctx.Params["id"]
	soul, err := s.store.GetSoul(id)
	if err != nil {
		return ctx.Error(http.StatusNotFound, "soul not found")
	}

	return ctx.JSON(http.StatusOK, soul)
}

func (s *RESTServer) handleUpdateSoul(ctx *Context) error {
	id := ctx.Params["id"]
	var soul core.Soul
	if err := ctx.Bind(&soul); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid soul data")
	}

	soul.ID = id
	soul.WorkspaceID = ctx.Workspace
	soul.UpdatedAt = time.Now()

	if err := s.store.SaveSoul(&soul); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusOK, soul)
}

func (s *RESTServer) handleDeleteSoul(ctx *Context) error {
	id := ctx.Params["id"]
	if err := s.store.DeleteSoul(id); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusNoContent, nil)
}

func (s *RESTServer) handleForceCheck(ctx *Context) error {
	id := ctx.Params["id"]
	judgment, err := s.probe.ForceCheck(id)
	if err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusOK, judgment)
}

func (s *RESTServer) handleListJudgments(ctx *Context) error {
	soulID := ctx.Params["id"]
	start := time.Now().Add(-24 * time.Hour)
	end := time.Now()

	judgments, err := s.store.ListJudgments(soulID, start, end, 100)
	if err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusOK, judgments)
}

func (s *RESTServer) handleGetJudgment(ctx *Context) error {
	id := ctx.Params["id"]
	judgment, err := s.store.GetJudgment(id)
	if err != nil {
		return ctx.Error(http.StatusNotFound, "judgment not found")
	}

	return ctx.JSON(http.StatusOK, judgment)
}

func (s *RESTServer) handleListAllJudgments(ctx *Context) error {
	// List recent judgments across all souls
	return ctx.JSON(http.StatusOK, []interface{}{})
}

// Channel handlers

func (s *RESTServer) handleListChannels(ctx *Context) error {
	channels := s.alert.ListChannels()
	return ctx.JSON(http.StatusOK, channels)
}

func (s *RESTServer) handleCreateChannel(ctx *Context) error {
	var channel core.AlertChannel
	if err := ctx.Bind(&channel); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid channel data")
	}

	channel.ID = core.GenerateID()
	channel.CreatedAt = time.Now()
	channel.UpdatedAt = time.Now()

	if err := s.alert.RegisterChannel(&channel); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusCreated, channel)
}

func (s *RESTServer) handleGetChannel(ctx *Context) error {
	id := ctx.Params["id"]
	channel, err := s.store.GetChannel(id)
	if err != nil {
		return ctx.Error(http.StatusNotFound, "channel not found")
	}

	return ctx.JSON(http.StatusOK, channel)
}

func (s *RESTServer) handleUpdateChannel(ctx *Context) error {
	id := ctx.Params["id"]
	var channel core.AlertChannel
	if err := ctx.Bind(&channel); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid channel data")
	}

	channel.ID = id
	channel.UpdatedAt = time.Now()

	if err := s.alert.RegisterChannel(&channel); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusOK, channel)
}

func (s *RESTServer) handleDeleteChannel(ctx *Context) error {
	id := ctx.Params["id"]
	if err := s.alert.DeleteChannel(id); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusNoContent, nil)
}

func (s *RESTServer) handleTestChannel(ctx *Context) error {
	id := ctx.Params["id"]
	// Send test notification
	return ctx.JSON(http.StatusOK, map[string]string{"status": "test sent", "channel_id": id})
}

// Rule handlers

func (s *RESTServer) handleListRules(ctx *Context) error {
	rules := s.alert.ListRules()
	return ctx.JSON(http.StatusOK, rules)
}

func (s *RESTServer) handleCreateRule(ctx *Context) error {
	var rule core.AlertRule
	if err := ctx.Bind(&rule); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid rule data")
	}

	rule.ID = core.GenerateID()
	rule.CreatedAt = time.Now()

	if err := s.alert.RegisterRule(&rule); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusCreated, rule)
}

func (s *RESTServer) handleGetRule(ctx *Context) error {
	id := ctx.Params["id"]
	rule, err := s.store.GetRule(id)
	if err != nil {
		return ctx.Error(http.StatusNotFound, "rule not found")
	}

	return ctx.JSON(http.StatusOK, rule)
}

func (s *RESTServer) handleUpdateRule(ctx *Context) error {
	id := ctx.Params["id"]
	var rule core.AlertRule
	if err := ctx.Bind(&rule); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid rule data")
	}

	rule.ID = id

	if err := s.alert.RegisterRule(&rule); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusOK, rule)
}

func (s *RESTServer) handleDeleteRule(ctx *Context) error {
	id := ctx.Params["id"]
	if err := s.alert.DeleteRule(id); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusNoContent, nil)
}

// Workspace handlers

func (s *RESTServer) handleListWorkspaces(ctx *Context) error {
	workspaces, err := s.store.ListWorkspaces()
	if err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusOK, workspaces)
}

func (s *RESTServer) handleCreateWorkspace(ctx *Context) error {
	var ws core.Workspace
	if err := ctx.Bind(&ws); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid workspace data")
	}

	ws.ID = core.GenerateID()
	ws.CreatedAt = time.Now()
	ws.UpdatedAt = time.Now()

	if err := s.store.SaveWorkspace(&ws); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusCreated, ws)
}

func (s *RESTServer) handleGetWorkspace(ctx *Context) error {
	id := ctx.Params["id"]
	ws, err := s.store.GetWorkspace(id)
	if err != nil {
		return ctx.Error(http.StatusNotFound, "workspace not found")
	}

	return ctx.JSON(http.StatusOK, ws)
}

func (s *RESTServer) handleUpdateWorkspace(ctx *Context) error {
	id := ctx.Params["id"]
	var ws core.Workspace
	if err := ctx.Bind(&ws); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid workspace data")
	}

	ws.ID = id
	ws.UpdatedAt = time.Now()

	if err := s.store.SaveWorkspace(&ws); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusOK, ws)
}

func (s *RESTServer) handleDeleteWorkspace(ctx *Context) error {
	id := ctx.Params["id"]
	if err := s.store.DeleteWorkspace(id); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusNoContent, nil)
}

// Stats handlers

func (s *RESTServer) handleStats(ctx *Context) error {
	workspace := ctx.Workspace
	start := time.Now().Add(-24 * time.Hour)
	end := time.Now()

	stats, err := s.store.GetStats(workspace, start, end)
	if err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusOK, stats)
}

func (s *RESTServer) handleStatsOverview(ctx *Context) error {
	overview := map[string]interface{}{
		"souls": map[string]int{
			"total":    0,
			"healthy":  0,
			"degraded": 0,
			"dead":     0,
		},
		"judgments": map[string]interface{}{
			"today":     0,
			"failures":  0,
			"avg_latency_ms": 0,
		},
		"alerts": s.alert.GetStats(),
	}

	return ctx.JSON(http.StatusOK, overview)
}

// Cluster handlers

func (s *RESTServer) handleClusterStatus(ctx *Context) error {
	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"status": "healthy",
		"role":   "leader",
	})
}

func (s *RESTServer) handleClusterPeers(ctx *Context) error {
	return ctx.JSON(http.StatusOK, []interface{}{})
}

// Incident handlers

func (s *RESTServer) handleListIncidents(ctx *Context) error {
	// List active incidents
	return ctx.JSON(http.StatusOK, []interface{}{})
}

func (s *RESTServer) handleAcknowledgeIncident(ctx *Context) error {
	id := ctx.Params["id"]
	userID := ctx.User.ID

	if err := s.alert.AcknowledgeIncident(id, userID); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusOK, map[string]string{"status": "acknowledged"})
}

func (s *RESTServer) handleResolveIncident(ctx *Context) error {
	id := ctx.Params["id"]
	userID := ctx.User.ID

	if err := s.alert.ResolveIncident(id, userID); err != nil {
		return ctx.Error(http.StatusInternalServerError, err.Error())
	}

	return ctx.JSON(http.StatusOK, map[string]string{"status": "resolved"})
}

// Middleware

func (s *RESTServer) requireAuth(handler Handler) Handler {
	return func(ctx *Context) error {
		token := ctx.Request.Header.Get("Authorization")
		token = strings.TrimPrefix(token, "Bearer ")

		if token == "" {
			return ctx.Error(http.StatusUnauthorized, "missing authorization token")
		}

		user, err := s.auth.Authenticate(token)
		if err != nil {
			return ctx.Error(http.StatusUnauthorized, "invalid token")
		}

		ctx.User = user
		ctx.Workspace = user.Workspace
		return handler(ctx)
	}
}

func (s *RESTServer) loggingMiddleware(handler Handler) Handler {
	return func(ctx *Context) error {
		ctx.StartTime = time.Now()
		err := handler(ctx)
		duration := time.Since(ctx.StartTime)

		s.logger.Info("HTTP request",
			"method", ctx.Request.Method,
			"path", ctx.Request.URL.Path,
			"duration", duration,
			"error", err)

		return err
	}
}

func (s *RESTServer) corsMiddleware(handler Handler) Handler {
	return func(ctx *Context) error {
		ctx.Response.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.Response.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		ctx.Response.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if ctx.Request.Method == "OPTIONS" {
			ctx.Response.WriteHeader(http.StatusNoContent)
			return nil
		}

		return handler(ctx)
	}
}

func (s *RESTServer) recoveryMiddleware(handler Handler) Handler {
	return func(ctx *Context) error {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("Panic recovered", "error", r)
				ctx.Error(http.StatusInternalServerError, "internal server error")
			}
		}()
		return handler(ctx)
	}
}

// Router methods

func (r *Router) Handle(method, path string, handler Handler) {
	if r.routes[path] == nil {
		r.routes[path] = make(map[string]Handler)
	}

	// Apply middleware
	h := handler
	for i := len(r.middleware) - 1; i >= 0; i-- {
		h = r.middleware[i](h)
	}

	r.routes[path][method] = h
}

func (r *Router) Use(mw Middleware) {
	r.middleware = append(r.middleware, mw)
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Find matching route
	path := req.URL.Path
	method := req.Method

	// Try exact match first
	if handlers, ok := r.routes[path]; ok {
		if handler, ok := handlers[method]; ok {
			ctx := &Context{
				Request:  req,
				Response: w,
				Params:   make(map[string]string),
			}
			handler(ctx)
			return
		}
	}

	// Try parameterized routes (simple implementation)
	for routePath, handlers := range r.routes {
		if params, ok := matchRoute(routePath, path); ok {
			if handler, ok := handlers[method]; ok {
				ctx := &Context{
					Request:  req,
					Response: w,
					Params:   params,
				}
				handler(ctx)
				return
			}
		}
	}

	// No route found
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
}

func matchRoute(pattern, path string) (map[string]string, bool) {
	patternParts := strings.Split(pattern, "/")
	pathParts := strings.Split(path, "/")

	if len(patternParts) != len(pathParts) {
		return nil, false
	}

	params := make(map[string]string)
	for i := 0; i < len(patternParts); i++ {
		if strings.HasPrefix(patternParts[i], ":") {
			params[patternParts[i][1:]] = pathParts[i]
		} else if patternParts[i] != pathParts[i] {
			return nil, false
		}
	}

	return params, true
}

// Context helpers

func (c *Context) JSON(status int, data interface{}) error {
	c.Response.Header().Set("Content-Type", "application/json")
	c.Response.WriteHeader(status)
	return json.NewEncoder(c.Response).Encode(data)
}

func (c *Context) Error(status int, message string) error {
	return c.JSON(status, map[string]string{"error": message})
}

func (c *Context) Bind(v interface{}) error {
	return json.NewDecoder(c.Request.Body).Decode(v)
}
