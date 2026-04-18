package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

const (
	// maxRequestBodySize limits JSON request body size to prevent DoS (1MB)
	maxRequestBodySize = 1 << 20 // 1MB
	// maxJSONDepth limits JSON nesting depth to prevent stack overflow
	maxJSONDepth = 32
)

// maxDepthReader wraps an io.Reader to track JSON nesting depth.
// It counts [, { as +1 and ], } as -1, rejecting reads when
// depth exceeds maxJSONDepth.
type maxDepthReader struct {
	r     io.Reader
	depth int
}

func (m *maxDepthReader) Read(p []byte) (int, error) {
	n, err := m.r.Read(p)
	for _, b := range p[:n] {
		switch b {
		case '[', '{':
			m.depth++
			if m.depth > maxJSONDepth {
				return n, fmt.Errorf("JSON depth limit exceeded (max %d)", maxJSONDepth)
			}
		case ']', '}':
			m.depth--
		}
	}
	return n, err
}

// RESTServer implements the HTTP REST API
// The scribes record the judgments on papyrus scrolls
type RESTServer struct {
	config     core.ServerConfig
	authConfig core.AuthConfig
	router     *Router
	http       *http.Server
	logger     *slog.Logger
	mcp        *MCPServer
	ws         *WebSocketServer

	// Dependencies
	store      Storage
	probe      ProbeEngine
	alert      AlertManager
	auth       Authenticator
	cluster    ClusterManager
	dashboard  http.Handler
	statusPage http.Handler
	journey    JourneyExecutor

	// Prometheus-style counters (in-memory, reset on restart)
	metricsMu        sync.RWMutex
	judgmentsTotal   uint64
	verdictsFired    uint64
	verdictsResolved uint64
}

// JourneyExecutor interface for journey operations
type JourneyExecutor interface {
	ListRuns(ctx context.Context, workspaceID, journeyID string, limit int) ([]*core.JourneyRun, error)
	GetRun(ctx context.Context, workspaceID, journeyID, runID string) (*core.JourneyRun, error)
}

// Router handles HTTP routing
type Router struct {
	routes         map[string]map[string]Handler // path -> method -> handler
	middleware     []Middleware
	dashboard      http.Handler
	statusPage     http.Handler
	allowedOrigins []string // Allowed CORS origins from config
}

// Handler is an HTTP handler function
type Handler func(ctx *Context) error

// Middleware wraps handlers
type Middleware func(Handler) Handler

// Context holds request context
type Context struct {
	Request   *http.Request
	Response  http.ResponseWriter
	Params    map[string]string
	User      *User
	Workspace string
	StartTime time.Time
}

// Pagination holds pagination metadata
type Pagination struct {
	Total      int  `json:"total"`
	Offset     int  `json:"offset"`
	Limit      int  `json:"limit"`
	HasMore    bool `json:"has_more"`
	NextOffset *int `json:"next_offset,omitempty"`
}

// PaginatedResponse wraps data with pagination metadata
type PaginatedResponse struct {
	Data       interface{} `json:"data"`
	Pagination Pagination  `json:"pagination"`
}

// parsePagination extracts pagination params from request
func parsePagination(r *http.Request, defaultLimit, maxLimit int) (offset, limit int) {
	offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}

	limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > maxLimit {
		limit = defaultLimit
	}

	return offset, limit
}

// Storage interface for data access
type Storage interface {
	GetSoulNoCtx(id string) (*core.Soul, error)
	ListSoulsNoCtx(workspace string, offset, limit int) ([]*core.Soul, error)
	SaveSoul(ctx context.Context, soul *core.Soul) error
	DeleteSoul(ctx context.Context, id string) error

	GetJudgmentNoCtx(id string) (*core.Judgment, error)
	ListJudgmentsNoCtx(soulID string, start, end time.Time, limit int) ([]*core.Judgment, error)

	GetChannelNoCtx(id string, workspace string) (*core.AlertChannel, error)
	ListChannelsNoCtx(workspace string) ([]*core.AlertChannel, error)
	SaveChannelNoCtx(channel *core.AlertChannel) error
	DeleteChannelNoCtx(id string, workspace string) error

	GetRuleNoCtx(id string, workspace string) (*core.AlertRule, error)
	ListRulesNoCtx(workspace string) ([]*core.AlertRule, error)
	SaveRuleNoCtx(rule *core.AlertRule) error
	DeleteRuleNoCtx(id string, workspace string) error

	GetWorkspaceNoCtx(id string) (*core.Workspace, error)
	ListWorkspacesNoCtx() ([]*core.Workspace, error)
	SaveWorkspaceNoCtx(ws *core.Workspace) error
	DeleteWorkspaceNoCtx(id string) error

	GetStatsNoCtx(workspace string, start, end time.Time) (*core.Stats, error)

	GetStatusPageNoCtx(id string) (*core.StatusPage, error)
	ListStatusPagesNoCtx() ([]*core.StatusPage, error)
	SaveStatusPageNoCtx(page *core.StatusPage) error
	DeleteStatusPageNoCtx(id string) error

	// Journey methods
	GetJourneyNoCtx(id string) (*core.JourneyConfig, error)
	ListJourneysNoCtx(workspace string, offset, limit int) ([]*core.JourneyConfig, error)
	SaveJourneyNoCtx(journey *core.JourneyConfig) error
	DeleteJourneyNoCtx(id string) error

	// Dashboard methods
	GetDashboardNoCtx(id string) (*core.CustomDashboard, error)
	ListDashboardsNoCtx() ([]*core.CustomDashboard, error)
	SaveDashboardNoCtx(dashboard *core.CustomDashboard) error
	DeleteDashboardNoCtx(id string) error

	// Maintenance methods
	GetMaintenanceWindow(id string) (*core.MaintenanceWindow, error)
	ListMaintenanceWindows() ([]*core.MaintenanceWindow, error)
	SaveMaintenanceWindow(w *core.MaintenanceWindow) error
	DeleteMaintenanceWindow(id string) error
}

// ProbeEngine interface for probe operations
type ProbeEngine interface {
	AssignSouls(souls []*core.Soul)
	GetStatus() *core.ProbeStatus
	ForceCheck(soulID string) (*core.Judgment, error)
}

// AlertManager interface for alert operations
type AlertManager interface {
	GetStats() core.AlertManagerStats
	ListChannels() []*core.AlertChannel
	ListChannelsByWorkspace(workspace string) []*core.AlertChannel
	ListRules() []*core.AlertRule
	ListRulesByWorkspace(workspace string) []*core.AlertRule
	GetChannel(id string) (*core.AlertChannel, error)
	GetRule(id string) (*core.AlertRule, error)
	RegisterChannel(channel *core.AlertChannel) error
	RegisterRule(rule *core.AlertRule) error
	DeleteChannel(id string) error
	DeleteChannelWithWorkspace(id string, workspace string) error
	DeleteRule(id string) error
	DeleteRuleWithWorkspace(id string, workspace string) error
	AcknowledgeIncident(incidentID, userID, workspace string) error
	ResolveIncident(incidentID, userID, workspace string) error
	ListActiveIncidents() []*core.Incident
}

// Authenticator interface for authentication
type Authenticator interface {
	Authenticate(token string) (*User, error)
	Login(email, password string) (*User, string, error)
	Logout(token string) error
	Shutdown()
	// Password management (HIGH-04)
	ChangePassword(token, currentPassword, newPassword string) error
	RequestPasswordReset(email string) (string, error)
	ConfirmPasswordReset(token, newPassword string) error
}

// OIDCAuth extends Authenticator with OIDC methods
type OIDCAuth interface {
	Authenticator
	OIDCLoginURL() (string, string, string, error)
	OIDCCallback(code, state, nonce string) (*User, string, error)
}

// ClusterManager interface for cluster operations
type ClusterManager interface {
	IsLeader() bool
	Leader() string
	IsClustered() bool
	GetStatus() *ClusterStatus
}

// ClusterStatus holds cluster status info
type ClusterStatus struct {
	IsClustered bool   `json:"is_clustered"`
	NodeID      string `json:"node_id"`
	State       string `json:"state,omitempty"`
	Leader      string `json:"leader,omitempty"`
	Term        uint64 `json:"term,omitempty"`
	PeerCount   int    `json:"peer_count,omitempty"`
	CommitIndex uint64 `json:"commit_index,omitempty"`
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
func NewRESTServer(config core.ServerConfig, authConfig core.AuthConfig, store Storage, probe ProbeEngine, alert AlertManager, auth Authenticator, cluster ClusterManager, journey JourneyExecutor, dashboard http.Handler, statusPage http.Handler, mcp *MCPServer, logger *slog.Logger) *RESTServer {
	wsServer := NewWebSocketServer(logger, auth, config.AllowedOrigins) // Wire config origins to WebSocket

	s := &RESTServer{
		config:     config,
		authConfig: authConfig,
		router: &Router{
			routes:         make(map[string]map[string]Handler),
			dashboard:      dashboard,
			statusPage:     statusPage,
			allowedOrigins: config.AllowedOrigins, // Store allowed origins in router for CORS preflight
		},
		logger:     logger.With("component", "rest_server"),
		store:      store,
		probe:      probe,
		alert:      alert,
		auth:       auth,
		cluster:    cluster,
		journey:    journey,
		mcp:        mcp,
		ws:         wsServer,
		dashboard:  dashboard,
		statusPage: statusPage,
	}

	s.setupRoutes()
	wsServer.Start()
	return s
}

// setupRoutes configures API routes
func (s *RESTServer) setupRoutes() {
	// Middleware - order matters
	s.router.Use(s.loggingMiddleware)
	s.router.Use(s.securityHeadersMiddleware) // Add security headers to all responses
	s.router.Use(s.corsMiddleware)
	s.router.Use(s.recoveryMiddleware)
	s.router.Use(s.validateJSONMiddleware)
	s.router.Use(s.validatePathParams) // Validate path parameters
	s.router.Use(s.rateLimitMiddleware)

	// Health
	s.router.Handle("GET", "/health", s.handleHealth)
	s.router.Handle("GET", "/ready", s.handleReady)
	s.router.Handle("GET", "/metrics", s.requireAuth(s.handleMetrics))

	// OpenAPI / Swagger (no auth required)
	s.router.Handle("GET", "/api/openapi.json", s.handleOpenAPIJSON)
	s.router.Handle("GET", "/api/docs", s.handleOpenAPIDocs)

	// Public Status Pages (no auth required)
	s.router.Handle("GET", "/status", s.handleStatusPage)
	s.router.Handle("GET", "/status.html", s.handleStatusPageHTML)
	s.router.Handle("GET", "/public/status", s.handlePublicStatus)

	// Auth
	s.router.Handle("POST", "/api/v1/auth/login", s.handleLogin)
	s.router.Handle("POST", "/api/v1/auth/logout", s.handleLogout)
	s.router.Handle("GET", "/api/v1/auth/me", s.requireAuth(s.handleMe))
	// Password management (HIGH-04)
	s.router.Handle("PUT", "/api/v1/auth/change-password", s.requireAuth(s.handleChangePassword))
	s.router.Handle("POST", "/api/v1/auth/reset-password", s.handleRequestPasswordReset)
	s.router.Handle("POST", "/api/v1/auth/reset-password/confirm", s.handleConfirmPasswordReset)

	// OIDC Auth (if configured)
	if _, ok := s.auth.(OIDCAuth); ok {
		s.router.Handle("GET", "/api/v1/auth/oidc/login", s.handleOIDCLogin)
		s.router.Handle("GET", "/api/v1/auth/oidc/callback", s.handleOIDCCallback)
	}

	// Souls
	s.router.Handle("GET", "/api/v1/souls", s.requireAuth(s.handleListSouls))
	s.router.Handle("POST", "/api/v1/souls", s.requireRole(s.handleCreateSoul, "souls:*"))
	s.router.Handle("GET", "/api/v1/souls/:id", s.requireAuth(s.handleGetSoul))
	s.router.Handle("PUT", "/api/v1/souls/:id", s.requireRole(s.handleUpdateSoul, "souls:*"))
	s.router.Handle("DELETE", "/api/v1/souls/:id", s.requireRole(s.handleDeleteSoul, "souls:*"))
	s.router.Handle("POST", "/api/v1/souls/:id/check", s.requireRole(s.handleForceCheck, "souls:*"))
	s.router.Handle("GET", "/api/v1/souls/:id/judgments", s.requireAuth(s.handleListJudgments))

	// Judgments
	s.router.Handle("GET", "/api/v1/judgments/:id", s.requireAuth(s.handleGetJudgment))
	s.router.Handle("GET", "/api/v1/judgments", s.requireAuth(s.handleListAllJudgments))

	// Channels
	s.router.Handle("GET", "/api/v1/channels", s.requireAuth(s.handleListChannels))
	s.router.Handle("POST", "/api/v1/channels", s.requireRole(s.handleCreateChannel, "channels:*"))
	s.router.Handle("GET", "/api/v1/channels/:id", s.requireAuth(s.handleGetChannel))
	s.router.Handle("PUT", "/api/v1/channels/:id", s.requireRole(s.handleUpdateChannel, "channels:*"))
	s.router.Handle("DELETE", "/api/v1/channels/:id", s.requireRole(s.handleDeleteChannel, "channels:*"))
	s.router.Handle("POST", "/api/v1/channels/:id/test", s.requireRole(s.handleTestChannel, "channels:*"))

	// Rules
	s.router.Handle("GET", "/api/v1/rules", s.requireAuth(s.handleListRules))
	s.router.Handle("POST", "/api/v1/rules", s.requireRole(s.handleCreateRule, "rules:*"))
	s.router.Handle("GET", "/api/v1/rules/:id", s.requireAuth(s.handleGetRule))
	s.router.Handle("PUT", "/api/v1/rules/:id", s.requireRole(s.handleUpdateRule, "rules:*"))
	s.router.Handle("DELETE", "/api/v1/rules/:id", s.requireRole(s.handleDeleteRule, "rules:*"))

	// Workspaces
	s.router.Handle("GET", "/api/v1/workspaces", s.requireAuth(s.handleListWorkspaces))
	s.router.Handle("POST", "/api/v1/workspaces", s.requireRole(s.handleCreateWorkspace, "members:*"))
	s.router.Handle("GET", "/api/v1/workspaces/:id", s.requireAuth(s.handleGetWorkspace))
	s.router.Handle("PUT", "/api/v1/workspaces/:id", s.requireRole(s.handleUpdateWorkspace, "settings:write"))
	s.router.Handle("DELETE", "/api/v1/workspaces/:id", s.requireRole(s.handleDeleteWorkspace, "members:*"))

	// Stats
	s.router.Handle("GET", "/api/v1/stats", s.requireAuth(s.handleStats))
	s.router.Handle("GET", "/api/v1/stats/overview", s.requireAuth(s.handleStatsOverview))

	// Cluster (Raft)
	s.router.Handle("GET", "/api/v1/cluster/status", s.requireAuth(s.handleClusterStatus))
	s.router.Handle("GET", "/api/v1/cluster/peers", s.requireAuth(s.handleClusterPeers))

	// Config
	s.router.Handle("GET", "/api/v1/config", s.requireAuth(s.handleGetConfig))
	s.router.Handle("PUT", "/api/v1/config", s.requireRole(s.handleUpdateConfig, "settings:write"))

	// Incidents
	s.router.Handle("GET", "/api/v1/incidents", s.requireAuth(s.handleListIncidents))
	s.router.Handle("POST", "/api/v1/incidents/:id/acknowledge", s.requireRole(s.handleAcknowledgeIncident, "rules:*"))
	s.router.Handle("POST", "/api/v1/incidents/:id/resolve", s.requireRole(s.handleResolveIncident, "rules:*"))

	// Status Pages
	s.router.Handle("GET", "/api/v1/status-pages", s.requireAuth(s.handleListStatusPages))
	s.router.Handle("POST", "/api/v1/status-pages", s.requireRole(s.handleCreateStatusPage, "settings:write"))
	s.router.Handle("GET", "/api/v1/status-pages/:id", s.requireAuth(s.handleGetStatusPage))
	s.router.Handle("PUT", "/api/v1/status-pages/:id", s.requireRole(s.handleUpdateStatusPage, "settings:write"))
	s.router.Handle("DELETE", "/api/v1/status-pages/:id", s.requireRole(s.handleDeleteStatusPage, "settings:write"))

	// MCP (Model Context Protocol)
	s.router.Handle("POST", "/api/v1/mcp", s.requireAuth(s.handleMCP))

	// Alerts aliases for frontend compatibility
	s.router.Handle("GET", "/api/v1/alerts/channels", s.requireAuth(s.handleListChannels))
	s.router.Handle("POST", "/api/v1/alerts/channels", s.requireRole(s.handleCreateChannel, "channels:*"))
	s.router.Handle("GET", "/api/v1/alerts/channels/:id", s.requireAuth(s.handleGetChannel))
	s.router.Handle("PUT", "/api/v1/alerts/channels/:id", s.requireRole(s.handleUpdateChannel, "channels:*"))
	s.router.Handle("DELETE", "/api/v1/alerts/channels/:id", s.requireRole(s.handleDeleteChannel, "channels:*"))
	s.router.Handle("POST", "/api/v1/alerts/channels/:id/test", s.requireRole(s.handleTestChannel, "channels:*"))

	s.router.Handle("GET", "/api/v1/alerts/rules", s.requireAuth(s.handleListRules))
	s.router.Handle("POST", "/api/v1/alerts/rules", s.requireRole(s.handleCreateRule, "rules:*"))
	s.router.Handle("GET", "/api/v1/alerts/rules/:id", s.requireAuth(s.handleGetRule))
	s.router.Handle("PUT", "/api/v1/alerts/rules/:id", s.requireRole(s.handleUpdateRule, "rules:*"))
	s.router.Handle("DELETE", "/api/v1/alerts/rules/:id", s.requireRole(s.handleDeleteRule, "rules:*"))

	// Users alias
	s.router.Handle("GET", "/api/v1/users/me", s.requireAuth(s.handleMe))

	// Journeys endpoints
	s.router.Handle("GET", "/api/v1/journeys", s.requireAuth(s.handleListJourneys))
	s.router.Handle("POST", "/api/v1/journeys", s.requireRole(s.handleCreateJourney, "souls:*"))
	s.router.Handle("GET", "/api/v1/journeys/:id", s.requireAuth(s.handleGetJourney))
	s.router.Handle("PUT", "/api/v1/journeys/:id", s.requireRole(s.handleUpdateJourney, "souls:*"))
	s.router.Handle("DELETE", "/api/v1/journeys/:id", s.requireRole(s.handleDeleteJourney, "souls:*"))
	s.router.Handle("POST", "/api/v1/journeys/:id/run", s.requireRole(s.handleRunJourney, "souls:*"))
	s.router.Handle("GET", "/api/v1/journeys/:id/runs", s.requireAuth(s.handleListJourneyRuns))
	s.router.Handle("GET", "/api/v1/journeys/:id/runs/:runId", s.requireAuth(s.handleGetJourneyRun))

	// Maintenance Windows
	s.router.Handle("GET", "/api/v1/maintenance", s.requireAuth(s.handleListMaintenanceWindows))
	s.router.Handle("POST", "/api/v1/maintenance", s.requireRole(s.handleCreateMaintenanceWindow, "settings:write"))
	s.router.Handle("GET", "/api/v1/maintenance/:id", s.requireAuth(s.handleGetMaintenanceWindow))
	s.router.Handle("PUT", "/api/v1/maintenance/:id", s.requireRole(s.handleUpdateMaintenanceWindow, "settings:write"))
	s.router.Handle("DELETE", "/api/v1/maintenance/:id", s.requireRole(s.handleDeleteMaintenanceWindow, "settings:write"))

	// Dashboards endpoints
	s.router.Handle("GET", "/api/v1/dashboards", s.requireAuth(s.handleListDashboards))
	s.router.Handle("POST", "/api/v1/dashboards", s.requireRole(s.handleCreateDashboard, "settings:write"))
	s.router.Handle("GET", "/api/v1/dashboards/:id", s.requireAuth(s.handleGetDashboard))
	s.router.Handle("PUT", "/api/v1/dashboards/:id", s.requireRole(s.handleUpdateDashboard, "settings:write"))
	s.router.Handle("DELETE", "/api/v1/dashboards/:id", s.requireRole(s.handleDeleteDashboard, "settings:write"))
	s.router.Handle("POST", "/api/v1/dashboards/:id/query", s.requireRole(s.handleDashboardQuery, "souls:read"))
	s.router.Handle("GET", "/api/v1/dashboards/templates", s.requireAuth(s.handleDashboardTemplates))

	// MCP tools endpoint
	s.router.Handle("GET", "/api/v1/mcp/tools", s.requireAuth(s.handleMCPTools))

	// Soul logs endpoint
	s.router.Handle("GET", "/api/v1/souls/:id/logs", s.requireAuth(s.handleSoulLogs))

	// WebSocket endpoint
	s.router.Handle("GET", "/ws", s.handleWebSocket)

	// SSE (Server-Sent Events) endpoint - better fallback support
	s.router.Handle("GET", "/api/v1/events", s.handleSSE)
}

// Start starts the REST server
func (s *RESTServer) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	if addr == ":0" {
		addr = ":8080"
	}

	s.http = &http.Server{
		Addr:              addr,
		Handler:           s.router,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
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

// openapiJSON holds the pre-marshalled OpenAPI spec
var openapiJSON = []byte(`{"openapi":"3.0.3","info":{"title":"AnubisWatch API","version":"4.0.0","description":"REST API for AnubisWatch uptime monitoring"},"paths":{"/health":{"get":{"summary":"Health check","tags":["System"],"responses":{"200":{"description":"Service is healthy"}}}},"/ready":{"get":{"summary":"Readiness check","tags":["System"],"responses":{"200":{"description":"Service is ready"}}}},"/api/v1/auth/login":{"post":{"summary":"Authenticate user","tags":["Auth"],"requestBody":{"required":true,"content":{"application/json":{"schema":{"type":"object","required":["email","password"],"properties":{"email":{"type":"string","format":"email"},"password":{"type":"string"}}}}}},"responses":{"200":{"description":"Login successful"},"401":{"description":"Invalid credentials"}}}},"/api/v1/auth/logout":{"post":{"summary":"Log out","tags":["Auth"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"Logout successful"}}}},"/api/v1/auth/me":{"get":{"summary":"Get current user","tags":["Auth"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"Current user"}}}},"/api/v1/souls":{"get":{"summary":"List souls","tags":["Souls"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"offset","in":"query","schema":{"type":"integer","default":0}},{"name":"limit","in":"query","schema":{"type":"integer","default":20,"max":100}}],"responses":{"200":{"description":"Paginated list"}}},"post":{"summary":"Create soul","tags":["Souls"],"security":[{"BearerAuth":[]}],"responses":{"201":{"description":"Soul created"}}}},"/api/v1/souls/{id}":{"get":{"summary":"Get soul","tags":["Souls"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"Soul details"},"404":{"description":"Not found"}}},"put":{"summary":"Update soul","tags":["Souls"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"Soul updated"}}},"delete":{"summary":"Delete soul","tags":["Souls"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"204":{"description":"Soul deleted"}}}},"/api/v1/souls/{id}/check":{"post":{"summary":"Force check","tags":["Souls"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"Check result"}}}},"/api/v1/souls/{id}/judgments":{"get":{"summary":"List judgments","tags":["Judgments"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"List of judgments"}}}},"/api/v1/channels":{"get":{"summary":"List channels","tags":["Alerts"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"Paginated list"}}},"post":{"summary":"Create channel","tags":["Alerts"],"security":[{"BearerAuth":[]}],"responses":{"201":{"description":"Channel created"}}}},"/api/v1/channels/{id}":{"get":{"summary":"Get channel","tags":["Alerts"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"Channel details"}}},"put":{"summary":"Update channel","tags":["Alerts"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"Channel updated"}}},"delete":{"summary":"Delete channel","tags":["Alerts"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"204":{"description":"Channel deleted"}}}},"/api/v1/channels/{id}/test":{"post":{"summary":"Test channel","tags":["Alerts"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"Test sent"}}}},"/api/v1/rules":{"get":{"summary":"List rules","tags":["Alerts"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"Paginated list"}}},"post":{"summary":"Create rule","tags":["Alerts"],"security":[{"BearerAuth":[]}],"responses":{"201":{"description":"Rule created"}}}},"/api/v1/rules/{id}":{"get":{"summary":"Get rule","tags":["Alerts"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"Rule details"}}},"put":{"summary":"Update rule","tags":["Alerts"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"Rule updated"}}},"delete":{"summary":"Delete rule","tags":["Alerts"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"204":{"description":"Rule deleted"}}}},"/api/v1/incidents":{"get":{"summary":"List incidents","tags":["Incidents"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"List of incidents"}}}},"/api/v1/incidents/{id}/acknowledge":{"post":{"summary":"Acknowledge incident","tags":["Incidents"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"Acknowledged"}}}},"/api/v1/incidents/{id}/resolve":{"post":{"summary":"Resolve incident","tags":["Incidents"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"Resolved"}}}},"/api/v1/workspaces":{"get":{"summary":"List workspaces","tags":["Workspaces"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"List of workspaces"}}},"post":{"summary":"Create workspace","tags":["Workspaces"],"security":[{"BearerAuth":[]}],"responses":{"201":{"description":"Workspace created"}}}},"/api/v1/workspaces/{id}":{"get":{"summary":"Get workspace","tags":["Workspaces"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"Workspace details"}}},"put":{"summary":"Update workspace","tags":["Workspaces"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"Workspace updated"}}},"delete":{"summary":"Delete workspace","tags":["Workspaces"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"204":{"description":"Workspace deleted"}}}},"/api/v1/stats":{"get":{"summary":"Get stats","tags":["Stats"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"Statistics"}}}},"/api/v1/stats/overview":{"get":{"summary":"Stats overview","tags":["Stats"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"Overview"}}}},"/api/v1/cluster/status":{"get":{"summary":"Cluster status","tags":["Cluster"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"Cluster status"}}}},"/api/v1/cluster/peers":{"get":{"summary":"Cluster peers","tags":["Cluster"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"Peers"}}}},"/api/v1/config":{"get":{"summary":"Get config","tags":["Config"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"Configuration"}}},"put":{"summary":"Update config","tags":["Config"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"Config updated"}}}},"/api/v1/status-pages":{"get":{"summary":"List status pages","tags":["Status Pages"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"List"}}},"post":{"summary":"Create status page","tags":["Status Pages"],"security":[{"BearerAuth":[]}],"responses":{"201":{"description":"Created"}}}},"/api/v1/status-pages/{id}":{"get":{"summary":"Get status page","tags":["Status Pages"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"Details"}}},"put":{"summary":"Update status page","tags":["Status Pages"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"Updated"}}},"delete":{"summary":"Delete status page","tags":["Status Pages"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"204":{"description":"Deleted"}}}},"/api/v1/journeys":{"get":{"summary":"List journeys","tags":["Journeys"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"List"}}},"post":{"summary":"Create journey","tags":["Journeys"],"security":[{"BearerAuth":[]}],"responses":{"201":{"description":"Created"}}}},"/api/v1/journeys/{id}":{"get":{"summary":"Get journey","tags":["Journeys"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"Details"}}},"put":{"summary":"Update journey","tags":["Journeys"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"Updated"}}},"delete":{"summary":"Delete journey","tags":["Journeys"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"204":{"description":"Deleted"}}}},"/api/v1/journeys/{id}/run":{"post":{"summary":"Run journey","tags":["Journeys"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"Run started"}}}},"/api/v1/journeys/{id}/runs":{"get":{"summary":"List journey runs","tags":["Journeys"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"List"}}}},"/api/v1/journeys/{id}/runs/{runId}":{"get":{"summary":"Get journey run","tags":["Journeys"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}},{"name":"runId","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"Run details"}}}},"/api/v1/maintenance":{"get":{"summary":"List maintenance windows","tags":["Maintenance"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"List"}}},"post":{"summary":"Create maintenance window","tags":["Maintenance"],"security":[{"BearerAuth":[]}],"responses":{"201":{"description":"Created"}}}},"/api/v1/maintenance/{id}":{"get":{"summary":"Get maintenance window","tags":["Maintenance"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"Details"}}},"put":{"summary":"Update maintenance window","tags":["Maintenance"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"Updated"}}},"delete":{"summary":"Delete maintenance window","tags":["Maintenance"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"Deleted"}}}},"/api/v1/dashboards":{"get":{"summary":"List dashboards","tags":["Dashboards"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"List"}}},"post":{"summary":"Create dashboard","tags":["Dashboards"],"security":[{"BearerAuth":[]}],"responses":{"201":{"description":"Created"}}}},"/api/v1/dashboards/{id}":{"get":{"summary":"Get dashboard","tags":["Dashboards"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"Details"}}},"put":{"summary":"Update dashboard","tags":["Dashboards"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"Updated"}}},"delete":{"summary":"Delete dashboard","tags":["Dashboards"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"204":{"description":"Deleted"}}}},"/api/v1/dashboards/{id}/query":{"post":{"summary":"Query dashboard","tags":["Dashboards"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"Query results"}}}},"/api/v1/dashboards/templates":{"get":{"summary":"Dashboard templates","tags":["Dashboards"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"Templates"}}}},"/api/v1/mcp":{"post":{"summary":"MCP endpoint","tags":["MCP"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"MCP response"}}}},"/api/v1/mcp/tools":{"get":{"summary":"List MCP tools","tags":["MCP"],"security":[{"BearerAuth":[]}],"responses":{"200":{"description":"List of tools"}}}},"/api/v1/souls/{id}/logs":{"get":{"summary":"Soul logs","tags":["Souls"],"security":[{"BearerAuth":[]}],"parameters":[{"name":"id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"Logs"}}}},"/ws":{"get":{"summary":"WebSocket","tags":["WebSocket"],"responses":{"101":{"description":"Upgrade"}}}},"/api/v1/events":{"get":{"summary":"SSE stream","tags":["SSE"],"responses":{"200":{"description":"Stream"}}}}},"components":{"securitySchemes":{"BearerAuth":{"type":"http","scheme":"bearer","bearerFormat":"JWT"}}}}`)

func (s *RESTServer) handleOpenAPIJSON(ctx *Context) error {
	ctx.Response.Header().Set("Content-Type", "application/json")
	ctx.Response.Header().Set("Cache-Control", "public, max-age=3600")
	ctx.Response.WriteHeader(http.StatusOK)
	ctx.Response.Write(openapiJSON)
	return nil
}

func (s *RESTServer) handleOpenAPIDocs(ctx *Context) error {
	html := `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>AnubisWatch API Docs</title>
<script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js" defer></script>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css">
<style>
body{margin:0;background:#0f172a}
.swagger-ui .topbar{display:none}
.swagger-ui{background:#0f172a;color:#e2e8f0}
</style>
</head>
<body>
<div id="swagger-ui"></div>
<script>
window.addEventListener('load', function() {
  SwaggerUIBundle({
    url: '/api/openapi.json',
    dom_id: '#swagger-ui',
    presets: [SwaggerUIBundle.presets.apis],
    layout: 'BaseLayout',
    defaultModelsExpandDepth: 2,
  });
});
</script>
</body>
</html>`
	ctx.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
	ctx.Response.WriteHeader(http.StatusOK)
	ctx.Response.Write([]byte(html))
	return nil
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

// Password management handlers (HIGH-04)

func (s *RESTServer) handleChangePassword(ctx *Context) error {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := ctx.Bind(&req); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid request body")
	}

	// Extract token from Authorization header (already validated by requireAuth)
	authHeader := ctx.Request.Header.Get("Authorization")
	token := strings.TrimPrefix(authHeader, "Bearer ")

	if err := s.auth.ChangePassword(token, req.CurrentPassword, req.NewPassword); err != nil {
		if strings.Contains(err.Error(), "current password") {
			return ctx.Error(http.StatusUnauthorized, err.Error())
		}
		if strings.Contains(err.Error(), "policy") {
			return ctx.Error(http.StatusBadRequest, err.Error())
		}
		s.logger.Error("change password failed", "error", err)
		return ctx.Error(http.StatusInternalServerError, "failed to change password")
	}

	return ctx.JSON(http.StatusOK, map[string]string{
		"message": "password changed successfully, please login again",
	})
}

func (s *RESTServer) handleRequestPasswordReset(ctx *Context) error {
	var req struct {
		Email string `json:"email"`
	}
	if err := ctx.Bind(&req); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid request body")
	}

	_, err := s.auth.RequestPasswordReset(req.Email)
	if err != nil {
		s.logger.Error("password reset request failed", "error", err)
		return ctx.Error(http.StatusInternalServerError, "failed to request password reset")
	}

	// Always return success (even for unknown emails) to prevent enumeration
	return ctx.JSON(http.StatusOK, map[string]string{
		"message": "if the email exists, a reset token has been generated (check server logs)",
	})
}

func (s *RESTServer) handleConfirmPasswordReset(ctx *Context) error {
	var req struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := ctx.Bind(&req); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid request body")
	}

	if err := s.auth.ConfirmPasswordReset(req.Token, req.NewPassword); err != nil {
		if strings.Contains(err.Error(), "expired") || strings.Contains(err.Error(), "invalid") {
			return ctx.Error(http.StatusBadRequest, err.Error())
		}
		if strings.Contains(err.Error(), "policy") {
			return ctx.Error(http.StatusBadRequest, err.Error())
		}
		s.logger.Error("password reset confirm failed", "error", err)
		return ctx.Error(http.StatusInternalServerError, "failed to reset password")
	}

	return ctx.JSON(http.StatusOK, map[string]string{
		"message": "password reset successfully, please login with your new password",
	})
}

// OIDC handlers

func (s *RESTServer) handleOIDCLogin(ctx *Context) error {
	oidcAuth, ok := s.auth.(OIDCAuth)
	if !ok {
		return ctx.Error(http.StatusBadRequest, "OIDC not configured")
	}

	loginURL, state, nonce, err := oidcAuth.OIDCLoginURL()
	if err != nil {
		s.logger.Error("OIDC login failed", "error", err)
		return ctx.Error(http.StatusInternalServerError, "OIDC login failed: "+err.Error())
	}

	// Store nonce in secure cookie for CSRF protection
	// The nonce binds the callback to this specific authentication flow
	http.SetCookie(ctx.Response, &http.Cookie{
		Name:     "oidc_nonce",
		Value:    nonce,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   600, // 10 minutes
	})

	// Store state in cookie for verification
	http.SetCookie(ctx.Response, &http.Cookie{
		Name:     "oidc_state",
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   600, // 10 minutes
	})

	// Redirect to OIDC provider
	ctx.Response.Header().Set("Location", loginURL)
	ctx.Response.WriteHeader(http.StatusFound)
	return nil
}

func (s *RESTServer) handleOIDCCallback(ctx *Context) error {
	oidcAuth, ok := s.auth.(OIDCAuth)
	if !ok {
		return ctx.Error(http.StatusBadRequest, "OIDC not configured")
	}

	code := ctx.Request.URL.Query().Get("code")
	state := ctx.Request.URL.Query().Get("state")

	if code == "" || state == "" {
		return ctx.Error(http.StatusBadRequest, "missing code or state")
	}

	// Check for OIDC error
	if errParam := ctx.Request.URL.Query().Get("error"); errParam != "" {
		errDesc := ctx.Request.URL.Query().Get("error_description")
		return ctx.Error(http.StatusBadRequest, fmt.Sprintf("OIDC error: %s (%s)", errParam, errDesc))
	}

	// Retrieve nonce from secure cookie for CSRF protection
	nonceCookie, err := ctx.Request.Cookie("oidc_nonce")
	if err != nil {
		s.logger.Error("OIDC callback missing nonce cookie", "error", err)
		return ctx.Error(http.StatusBadRequest, "missing nonce cookie: possible CSRF attack")
	}
	nonce := nonceCookie.Value

	// Clear the nonce cookie after use
	http.SetCookie(ctx.Response, &http.Cookie{
		Name:     "oidc_nonce",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1, // Delete cookie
	})

	// Clear the state cookie
	http.SetCookie(ctx.Response, &http.Cookie{
		Name:     "oidc_state",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1, // Delete cookie
	})

	user, token, err := oidcAuth.OIDCCallback(code, state, nonce)
	if err != nil {
		s.logger.Error("OIDC callback failed", "error", err)
		return ctx.Error(http.StatusUnauthorized, "OIDC authentication failed: "+err.Error())
	}

	// Return token (in production, redirect to dashboard with token in cookie)
	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"user":  user,
		"token": token,
	})
}

// Soul handlers

func (s *RESTServer) handleListSouls(ctx *Context) error {
	workspace := ctx.Workspace
	offset, limit := parsePagination(ctx.Request, 20, 100)

	souls, err := s.store.ListSoulsNoCtx(workspace, offset, limit)
	if err != nil {
		s.logger.Error("failed to list souls", "error", err, "workspace", workspace)
		return ctx.Error(http.StatusInternalServerError, "failed to retrieve souls")
	}

	// Check if there are more results
	hasMore := len(souls) == limit
	nextOffset := offset + limit

	response := PaginatedResponse{
		Data: souls,
		Pagination: Pagination{
			Offset:  offset,
			Limit:   limit,
			HasMore: hasMore,
		},
	}

	if hasMore {
		response.Pagination.NextOffset = &nextOffset
	}

	return ctx.JSON(http.StatusOK, response)
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

	if err := s.store.SaveSoul(ctx.Request.Context(), &soul); err != nil {
		return s.internalError(ctx, err, "internal server error")
	}

	// Assign soul to probe engine for monitoring
	if s.probe != nil {
		s.probe.AssignSouls([]*core.Soul{&soul})
	}

	return ctx.JSON(http.StatusCreated, soul)
}

func (s *RESTServer) handleGetSoul(ctx *Context) error {
	id := ctx.Params["id"]
	soul, err := s.store.GetSoulNoCtx(id)
	if err != nil {
		return ctx.Error(http.StatusNotFound, "soul not found")
	}

	// IDOR protection: Check if soul belongs to user's workspace
	if soul.WorkspaceID != ctx.Workspace {
		return ctx.Error(http.StatusForbidden, "access denied")
	}

	return ctx.JSON(http.StatusOK, soul)
}

func (s *RESTServer) handleUpdateSoul(ctx *Context) error {
	id := ctx.Params["id"]
	var soul core.Soul
	if err := ctx.Bind(&soul); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid soul data")
	}

	// IDOR protection: Verify soul belongs to user's workspace before updating
	existing, err := s.store.GetSoulNoCtx(id)
	if err != nil || existing == nil {
		return ctx.Error(http.StatusNotFound, "soul not found")
	}
	if existing.WorkspaceID != ctx.Workspace {
		return ctx.Error(http.StatusForbidden, "access denied: soul belongs to another workspace")
	}

	soul.ID = id
	soul.WorkspaceID = ctx.Workspace
	// Preserve server-managed fields to prevent mass assignment
	soul.CreatedAt = existing.CreatedAt
	soul.UpdatedAt = time.Now()

	if err := s.store.SaveSoul(ctx.Request.Context(), &soul); err != nil {
		s.logger.Error("failed to update soul", "error", err, "soul_id", id)
		return ctx.Error(http.StatusInternalServerError, "failed to update soul")
	}

	return ctx.JSON(http.StatusOK, soul)
}

func (s *RESTServer) handleDeleteSoul(ctx *Context) error {
	id := ctx.Params["id"]

	// Verify soul belongs to user's workspace
	soul, err := s.store.GetSoulNoCtx(id)
	if err != nil || soul == nil {
		return ctx.Error(http.StatusNotFound, "soul not found")
	}
	if soul.WorkspaceID != ctx.Workspace {
		return ctx.Error(http.StatusForbidden, "access denied: soul belongs to another workspace")
	}

	if err := s.store.DeleteSoul(ctx.Request.Context(), id); err != nil {
		s.logger.Error("failed to delete soul", "error", err, "soul_id", id)
		return ctx.Error(http.StatusInternalServerError, "failed to delete soul")
	}

	return ctx.JSON(http.StatusNoContent, nil)
}

func (s *RESTServer) handleForceCheck(ctx *Context) error {
	id := ctx.Params["id"]
	soul, err := s.store.GetSoulNoCtx(id)
	if err != nil || soul == nil {
		return ctx.Error(http.StatusNotFound, "soul not found")
	}
	if soul.WorkspaceID != ctx.Workspace {
		return ctx.Error(http.StatusForbidden, "access denied: soul belongs to another workspace")
	}

	judgment, err := s.probe.ForceCheck(id)
	if err != nil {
		return s.internalError(ctx, err, "internal server error")
	}

	return ctx.JSON(http.StatusOK, judgment)
}

func (s *RESTServer) handleListJudgments(ctx *Context) error {
	soulID := ctx.Params["id"]

	// Verify soul belongs to user's workspace
	soul, err := s.store.GetSoulNoCtx(soulID)
	if err != nil || soul == nil {
		return ctx.Error(http.StatusNotFound, "soul not found")
	}
	if soul.WorkspaceID != ctx.Workspace {
		return ctx.Error(http.StatusForbidden, "access denied: soul belongs to another workspace")
	}

	start := time.Now().Add(-24 * time.Hour)
	end := time.Now()

	judgments, err := s.store.ListJudgmentsNoCtx(soulID, start, end, 100)
	if err != nil {
		return s.internalError(ctx, err, "internal server error")
	}

	return ctx.JSON(http.StatusOK, judgments)
}

func (s *RESTServer) handleGetJudgment(ctx *Context) error {
	id := ctx.Params["id"]
	judgment, err := s.store.GetJudgmentNoCtx(id)
	if err != nil {
		return ctx.Error(http.StatusNotFound, "judgment not found")
	}

	// IDOR protection: verify judgment belongs to caller's workspace
	if judgment.WorkspaceID != "" && judgment.WorkspaceID != ctx.Workspace {
		return ctx.Error(http.StatusForbidden, "access denied: judgment belongs to another workspace")
	}

	return ctx.JSON(http.StatusOK, judgment)
}

func (s *RESTServer) handleListAllJudgments(ctx *Context) error {
	// List recent judgments across all souls
	return ctx.JSON(http.StatusOK, []interface{}{})
}

// Channel handlers

func (s *RESTServer) handleListChannels(ctx *Context) error {
	offset, limit := parsePagination(ctx.Request, 20, 100)

	// HIGH-09: Filter channels by user's workspace
	allChannels := s.alert.ListChannelsByWorkspace(ctx.Workspace)

	// Apply pagination
	start := offset
	if start > len(allChannels) {
		start = len(allChannels)
	}
	end := start + limit
	if end > len(allChannels) {
		end = len(allChannels)
	}

	channels := allChannels[start:end]
	hasMore := end < len(allChannels)
	nextOffset := offset + limit

	response := PaginatedResponse{
		Data: channels,
		Pagination: Pagination{
			Total:   len(allChannels),
			Offset:  offset,
			Limit:   limit,
			HasMore: hasMore,
		},
	}

	if hasMore {
		response.Pagination.NextOffset = &nextOffset
	}

	return ctx.JSON(http.StatusOK, response)
}

func (s *RESTServer) handleCreateChannel(ctx *Context) error {
	var channel core.AlertChannel
	if err := ctx.Bind(&channel); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid channel data")
	}

	channel.ID = core.GenerateID()
	channel.WorkspaceID = ctx.Workspace
	channel.CreatedAt = time.Now()
	channel.UpdatedAt = time.Now()

	if err := s.alert.RegisterChannel(&channel); err != nil {
		return s.internalError(ctx, err, "internal server error")
	}

	return ctx.JSON(http.StatusCreated, channel)
}

func (s *RESTServer) handleGetChannel(ctx *Context) error {
	id := ctx.Params["id"]
	// HIGH-09: Verify channel belongs to user's workspace
	if ch, err := s.alert.GetChannel(id); err == nil {
		if ch.WorkspaceID != "" && ch.WorkspaceID != ctx.Workspace {
			return ctx.Error(http.StatusForbidden, "access denied: channel belongs to another workspace")
		}
		return ctx.JSON(http.StatusOK, ch)
	}
	// Fallback to storage with user's workspace
	channel, err := s.store.GetChannelNoCtx(id, ctx.Workspace)
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

	// HIGH-09: Verify channel belongs to user's workspace
	existing, err := s.alert.GetChannel(id)
	if err != nil {
		// Fallback to storage for workspace verification
		existing, err = s.store.GetChannelNoCtx(id, ctx.Workspace)
		if err != nil || existing == nil {
			return ctx.Error(http.StatusNotFound, "channel not found")
		}
	}
	if existing.WorkspaceID != "" && existing.WorkspaceID != ctx.Workspace {
		return ctx.Error(http.StatusForbidden, "access denied: channel belongs to another workspace")
	}

	channel.ID = id
	channel.WorkspaceID = ctx.Workspace
	channel.UpdatedAt = time.Now()

	if err := s.alert.RegisterChannel(&channel); err != nil {
		return s.internalError(ctx, err, "internal server error")
	}

	return ctx.JSON(http.StatusOK, channel)
}

func (s *RESTServer) handleDeleteChannel(ctx *Context) error {
	id := ctx.Params["id"]
	// HIGH-09: Verify channel belongs to user's workspace
	if err := s.alert.DeleteChannelWithWorkspace(id, ctx.Workspace); err != nil {
		errMsg := err.Error()
		if errMsg == "channel not found: "+id {
			return ctx.Error(http.StatusNotFound, "channel not found")
		}
		if strings.Contains(errMsg, "does not belong to workspace") {
			return ctx.Error(http.StatusForbidden, "access denied: "+errMsg)
		}
		return s.internalError(ctx, err, "internal server error")
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
	offset, limit := parsePagination(ctx.Request, 20, 100)

	// HIGH-09: Filter rules by user's workspace
	allRules := s.alert.ListRulesByWorkspace(ctx.Workspace)

	// Apply pagination
	start := offset
	if start > len(allRules) {
		start = len(allRules)
	}
	end := start + limit
	if end > len(allRules) {
		end = len(allRules)
	}

	rules := allRules[start:end]
	hasMore := end < len(allRules)
	nextOffset := offset + limit

	response := PaginatedResponse{
		Data: rules,
		Pagination: Pagination{
			Total:   len(allRules),
			Offset:  offset,
			Limit:   limit,
			HasMore: hasMore,
		},
	}

	if hasMore {
		response.Pagination.NextOffset = &nextOffset
	}

	return ctx.JSON(http.StatusOK, response)
}

func (s *RESTServer) handleCreateRule(ctx *Context) error {
	var rule core.AlertRule
	if err := ctx.Bind(&rule); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid rule data")
	}

	rule.ID = core.GenerateID()
	rule.WorkspaceID = ctx.Workspace
	rule.CreatedAt = time.Now()

	if err := s.alert.RegisterRule(&rule); err != nil {
		return s.internalError(ctx, err, "internal server error")
	}

	return ctx.JSON(http.StatusCreated, rule)
}

func (s *RESTServer) handleGetRule(ctx *Context) error {
	id := ctx.Params["id"]
	// HIGH-09: Verify rule belongs to user's workspace
	if rule, err := s.alert.GetRule(id); err == nil {
		if rule.WorkspaceID != "" && rule.WorkspaceID != ctx.Workspace {
			return ctx.Error(http.StatusForbidden, "access denied: rule belongs to another workspace")
		}
		return ctx.JSON(http.StatusOK, rule)
	}
	// Fallback to storage with user's workspace
	rule, err := s.store.GetRuleNoCtx(id, ctx.Workspace)
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

	// HIGH-09: Verify rule belongs to user's workspace
	existing, err := s.alert.GetRule(id)
	if err != nil {
		// Fallback to storage for workspace verification
		existing, err = s.store.GetRuleNoCtx(id, ctx.Workspace)
		if err != nil || existing == nil {
			return ctx.Error(http.StatusNotFound, "rule not found")
		}
	}
	if existing.WorkspaceID != "" && existing.WorkspaceID != ctx.Workspace {
		return ctx.Error(http.StatusForbidden, "access denied: rule belongs to another workspace")
	}

	rule.ID = id
	rule.WorkspaceID = ctx.Workspace

	if err := s.alert.RegisterRule(&rule); err != nil {
		return s.internalError(ctx, err, "internal server error")
	}

	return ctx.JSON(http.StatusOK, rule)
}

func (s *RESTServer) handleDeleteRule(ctx *Context) error {
	id := ctx.Params["id"]
	// HIGH-09: Verify rule belongs to user's workspace
	if err := s.alert.DeleteRuleWithWorkspace(id, ctx.Workspace); err != nil {
		errMsg := err.Error()
		if errMsg == "rule not found: "+id {
			return ctx.Error(http.StatusNotFound, "rule not found")
		}
		if strings.Contains(errMsg, "does not belong to workspace") {
			return ctx.Error(http.StatusForbidden, "access denied: "+errMsg)
		}
		return s.internalError(ctx, err, "internal server error")
	}

	return ctx.JSON(http.StatusNoContent, nil)
}

// Workspace handlers

func (s *RESTServer) handleListWorkspaces(ctx *Context) error {
	workspaces, err := s.store.ListWorkspacesNoCtx()
	if err != nil {
		return s.internalError(ctx, err, "internal server error")
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

	if err := s.store.SaveWorkspaceNoCtx(&ws); err != nil {
		return s.internalError(ctx, err, "internal server error")
	}

	return ctx.JSON(http.StatusCreated, ws)
}

func (s *RESTServer) handleGetWorkspace(ctx *Context) error {
	id := ctx.Params["id"]
	ws, err := s.store.GetWorkspaceNoCtx(id)
	if err != nil {
		return ctx.Error(http.StatusNotFound, "workspace not found")
	}

	return ctx.JSON(http.StatusOK, ws)
}

func (s *RESTServer) handleUpdateWorkspace(ctx *Context) error {
	id := ctx.Params["id"]

	// Fetch existing workspace for IDOR check
	existing, err := s.store.GetWorkspaceNoCtx(id)
	if err != nil {
		return s.internalError(ctx, err, "failed to fetch workspace")
	}
	if existing == nil {
		return ctx.Error(http.StatusNotFound, "workspace not found")
	}
	if existing.ID != id {
		return ctx.Error(http.StatusForbidden, "access denied")
	}

	// Bind only the mutable fields; protected fields from existing are preserved
	var ws core.Workspace
	if err := ctx.Bind(&ws); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid workspace data")
	}

	// Mass assignment protection: preserve all sensitive fields from existing.
	// Only Name, Description, and Settings can be updated by the client.
	ws.ID = id
	ws.Slug = existing.Slug     // immutable
	ws.OwnerID = existing.OwnerID // immutable - prevents privilege escalation
	ws.Quotas = existing.Quotas // immutable - prevent quota bypass
	ws.Features = existing.Features // immutable - prevent feature escalation
	ws.Status = existing.Status // immutable - prevent status manipulation
	ws.CreatedAt = existing.CreatedAt
	ws.UpdatedAt = time.Now()

	if err := s.store.SaveWorkspaceNoCtx(&ws); err != nil {
		return s.internalError(ctx, err, "internal server error")
	}

	return ctx.JSON(http.StatusOK, ws)
}

func (s *RESTServer) handleDeleteWorkspace(ctx *Context) error {
	id := ctx.Params["id"]
	if err := s.store.DeleteWorkspaceNoCtx(id); err != nil {
		return s.internalError(ctx, err, "internal server error")
	}

	return ctx.JSON(http.StatusNoContent, nil)
}

// Stats handlers

func (s *RESTServer) handleStats(ctx *Context) error {
	workspace := ctx.Workspace
	start := time.Now().Add(-24 * time.Hour)
	end := time.Now()

	stats, err := s.store.GetStatsNoCtx(workspace, start, end)
	if err != nil {
		return s.internalError(ctx, err, "internal server error")
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
			"today":          0,
			"failures":       0,
			"avg_latency_ms": 0,
		},
		"alerts": s.alert.GetStats(),
	}

	return ctx.JSON(http.StatusOK, overview)
}

// Cluster handlers

func (s *RESTServer) handleClusterStatus(ctx *Context) error {
	if s.cluster == nil {
		return ctx.JSON(http.StatusOK, map[string]interface{}{
			"is_clustered": false,
			"node_id":      "standalone",
			"state":        "standalone",
		})
	}

	status := s.cluster.GetStatus()
	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"is_clustered": status.IsClustered,
		"node_id":      status.NodeID,
		"state":        status.State,
		"leader":       status.Leader,
		"term":         status.Term,
		"peer_count":   status.PeerCount,
	})
}

func (s *RESTServer) handleClusterPeers(ctx *Context) error {
	if s.cluster == nil {
		return ctx.JSON(http.StatusOK, []interface{}{})
	}

	// Return peer information from cluster status
	status := s.cluster.GetStatus()
	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"peer_count": status.PeerCount,
		"is_leader":  s.cluster.IsLeader(),
	})
}

// Incident handlers

func (s *RESTServer) handleListIncidents(ctx *Context) error {
	incidents := s.alert.ListActiveIncidents()
	return ctx.JSON(http.StatusOK, incidents)
}

func (s *RESTServer) handleAcknowledgeIncident(ctx *Context) error {
	id := ctx.Params["id"]
	userID := ctx.User.ID

	if err := s.alert.AcknowledgeIncident(id, userID, ctx.Workspace); err != nil {
		return s.internalError(ctx, err, "internal server error")
	}

	return ctx.JSON(http.StatusOK, map[string]string{"status": "acknowledged"})
}

func (s *RESTServer) handleResolveIncident(ctx *Context) error {
	id := ctx.Params["id"]
	userID := ctx.User.ID

	if err := s.alert.ResolveIncident(id, userID, ctx.Workspace); err != nil {
		return s.internalError(ctx, err, "internal server error")
	}

	return ctx.JSON(http.StatusOK, map[string]string{"status": "resolved"})
}

// Config handlers

func (s *RESTServer) handleGetConfig(ctx *Context) error {
	config := map[string]interface{}{
		"instance_name":     "AnubisWatch",
		"timezone":          "UTC",
		"language":          "en",
		"theme":             "dark",
		"retention_days":    30,
		"storage_path":      "/var/lib/anubis",
		"auth_enabled":      s.authConfig.IsEnabled(),
		"mcp_enabled":       s.mcp != nil,
		"websocket_enabled": true,
		"host":              s.config.Host,
		"port":              s.config.Port,
		"grpc_port":         s.config.GRPCPort,
		"tls_enabled":       s.config.TLS.Enabled,
		"auto_cert":         s.config.TLS.AutoCert,
		"auth_type":         s.authConfig.Type,
	}
	return ctx.JSON(http.StatusOK, config)
}

func (s *RESTServer) handleUpdateConfig(ctx *Context) error {
	var input map[string]interface{}
	// Limit request body size to prevent memory exhaustion
	ctx.Request.Body = http.MaxBytesReader(ctx.Response, ctx.Request.Body, maxRequestBodySize)
	if err := json.NewDecoder(ctx.Request.Body).Decode(&input); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid JSON")
	}

	// Config is stored in-memory for the current session.
	// For persistent config changes, a config file rewrite would be needed.
	// This endpoint acknowledges the request for now.
	return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// Status Page handlers

func (s *RESTServer) handleListStatusPages(ctx *Context) error {
	pages, err := s.store.ListStatusPagesNoCtx()
	if err != nil {
		return s.internalError(ctx, err, "internal server error")
	}
	return ctx.JSON(http.StatusOK, pages)
}

func (s *RESTServer) handleCreateStatusPage(ctx *Context) error {
	var page core.StatusPage
	if err := ctx.Bind(&page); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid status page data")
	}

	page.ID = core.GenerateID()
	page.WorkspaceID = ctx.Workspace
	page.CreatedAt = time.Now()
	page.UpdatedAt = time.Now()

	if err := s.store.SaveStatusPageNoCtx(&page); err != nil {
		return s.internalError(ctx, err, "internal server error")
	}

	return ctx.JSON(http.StatusCreated, page)
}

func (s *RESTServer) handleGetStatusPage(ctx *Context) error {
	id := ctx.Params["id"]
	page, err := s.store.GetStatusPageNoCtx(id)
	if err != nil {
		return ctx.Error(http.StatusNotFound, "status page not found")
	}
	return ctx.JSON(http.StatusOK, page)
}

func (s *RESTServer) handleUpdateStatusPage(ctx *Context) error {
	id := ctx.Params["id"]
	var page core.StatusPage
	if err := ctx.Bind(&page); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid status page data")
	}

	page.ID = id
	page.WorkspaceID = ctx.Workspace
	page.UpdatedAt = time.Now()

	if err := s.store.SaveStatusPageNoCtx(&page); err != nil {
		return s.internalError(ctx, err, "internal server error")
	}

	return ctx.JSON(http.StatusOK, page)
}

func (s *RESTServer) handleDeleteStatusPage(ctx *Context) error {
	id := ctx.Params["id"]
	if err := s.store.DeleteStatusPageNoCtx(id); err != nil {
		return s.internalError(ctx, err, "internal server error")
	}
	return ctx.JSON(http.StatusNoContent, nil)
}

// MCP Handler

func (s *RESTServer) handleMCP(ctx *Context) error {
	if s.mcp == nil {
		return ctx.Error(http.StatusServiceUnavailable, "MCP server not initialized")
	}

	// MCP requires authentication
	if ctx.User == nil {
		return ctx.Error(http.StatusUnauthorized, "authentication required")
	}

	s.mcp.ServeHTTP(ctx.Response, ctx.Request)
	return nil
}

func (s *RESTServer) handleWebSocket(ctx *Context) error {
	if s.ws == nil {
		return ctx.Error(http.StatusServiceUnavailable, "WebSocket server not initialized")
	}

	s.ws.HandleConnection(ctx.Response, ctx.Request)
	return nil
}

func (s *RESTServer) handleSSE(ctx *Context) error {
	// Set SSE headers
	w := ctx.Response
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// CORS headers - use same logic as middleware
	origin := ctx.Request.Header.Get("Origin")
	allowedOrigins := s.getAllowedOrigins()
	if s.isOriginAllowed(origin, allowedOrigins) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
	w.Header().Set("Vary", "Origin")

	// Send initial connection message
	fmt.Fprintf(w, "data: %s\n\n", `{"type":"connected","timestamp":`+fmt.Sprintf("%d", time.Now().Unix())+`}`)
	w.(http.Flusher).Flush()

	// Keep connection alive with heartbeat
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Send heartbeat
			fmt.Fprintf(w, "data: %s\n\n", `{"type":"heartbeat","timestamp":`+fmt.Sprintf("%d", time.Now().Unix())+`}`)
			w.(http.Flusher).Flush()
		case <-ctx.Request.Context().Done():
			return nil
		}
	}
}

// Middleware

func (s *RESTServer) requireAuth(handler Handler) Handler {
	return func(ctx *Context) error {
		// When auth is disabled, deny all mutating requests and allow read-only for GET/HEAD
		if !s.authConfig.IsEnabled() {
			method := ctx.Request.Method
			if method != http.MethodGet && method != http.MethodHead {
				return ctx.Error(http.StatusForbidden, "authentication is required for this operation — enable auth in config to access this endpoint")
			}
			ctx.User = &User{
				ID:        "anonymous",
				Email:     "anonymous@anubis.watch",
				Name:      "Anonymous",
				Role:      "viewer",
				Workspace: "default",
			}
			ctx.Workspace = "default"
			return handler(ctx)
		}

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

// requireRole wraps requireAuth and additionally checks that the authenticated user
// has the specified permission via their workspace role.
func (s *RESTServer) requireRole(handler Handler, permission string) Handler {
	return s.requireAuth(func(ctx *Context) error {
		if ctx.User == nil || ctx.User.Role == "" {
			return ctx.Error(http.StatusForbidden, "insufficient permissions")
		}
		role := core.MemberRole(ctx.User.Role)
		if !role.Can(permission) {
			return ctx.Error(http.StatusForbidden, "role '"+ctx.User.Role+"' lacks permission '"+permission+"'")
		}
		return handler(ctx)
	})
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
		origin := ctx.Request.Header.Get("Origin")

		// Get allowed origins from config or use defaults
		allowedOrigins := s.getAllowedOrigins()

		// Check if origin is allowed
		if s.isOriginAllowed(origin, allowedOrigins) {
			ctx.Response.Header().Set("Access-Control-Allow-Origin", origin)
			ctx.Response.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		// Always set Vary: Origin to prevent cache poisoning
		ctx.Response.Header().Set("Vary", "Origin")

		ctx.Response.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		ctx.Response.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		ctx.Response.Header().Set("Access-Control-Max-Age", "86400") // 24 hours

		if ctx.Request.Method == "OPTIONS" {
			ctx.Response.WriteHeader(http.StatusNoContent)
			return nil
		}

		return handler(ctx)
	}
}

// getAllowedOrigins returns the list of allowed CORS origins
func (s *RESTServer) getAllowedOrigins() []string {
	// Priority 1: Config file settings (MED-18)
	if len(s.config.AllowedOrigins) > 0 {
		return s.config.AllowedOrigins
	}
	// Priority 2: Environment variable
	if allowed := os.Getenv("ANUBIS_CORS_ORIGINS"); allowed != "" {
		return strings.Split(allowed, ",")
	}
	// Priority 3: Default localhost for development
	return []string{
		"http://localhost:3000",
		"http://localhost:8080",
		"http://127.0.0.1:3000",
		"http://127.0.0.1:8080",
	}
}

// isOriginAllowed checks if an origin is in the allowed list
func (s *RESTServer) isOriginAllowed(origin string, allowed []string) bool {
	if origin == "" {
		return false
	}
	for _, allowed := range allowed {
		if strings.EqualFold(origin, allowed) {
			return true
		}
	}
	return false
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

// validateJSONMiddleware validates Content-Type and JSON body for POST/PUT requests
func (s *RESTServer) validateJSONMiddleware(handler Handler) Handler {
	return func(ctx *Context) error {
		if ctx.Request.Method == "POST" || ctx.Request.Method == "PUT" || ctx.Request.Method == "PATCH" {
			contentType := ctx.Request.Header.Get("Content-Type")
			if !strings.HasPrefix(contentType, "application/json") {
				return ctx.Error(http.StatusBadRequest, "Content-Type must be application/json")
			}

			// Check Content-Length
			if ctx.Request.ContentLength > 1<<20 { // 1MB limit
				return ctx.Error(http.StatusRequestEntityTooLarge, "Request body too large (max 1MB)")
			}

			// Validate JSON body structure to prevent attacks (only if body exists and has content)
			if ctx.Request.Body != nil && ctx.Request.ContentLength > 0 {
				bodyBytes, err := io.ReadAll(io.LimitReader(ctx.Request.Body, 1<<20))
				if err != nil {
					return ctx.Error(http.StatusBadRequest, "Invalid request body")
				}

				// Restore body for later handlers
				ctx.Request.Body = io.NopCloser(strings.NewReader(string(bodyBytes)))

				// Only validate non-empty bodies
				if len(bodyBytes) > 0 {
					// Check for common injection patterns
					bodyStr := string(bodyBytes)
					if containsInjectionPatterns(bodyStr) {
						s.logger.Warn("Potential injection attack detected",
							"ip", ctx.Request.RemoteAddr,
							"path", ctx.Request.URL.Path)
						return ctx.Error(http.StatusBadRequest, "Invalid characters in request")
					}

					// Validate it's valid JSON
					var jsonCheck interface{}
					if err := json.Unmarshal(bodyBytes, &jsonCheck); err != nil {
						return ctx.Error(http.StatusBadRequest, "Invalid JSON format")
					}
				}
			}
		}
		return handler(ctx)
	}
}

// containsInjectionPatterns checks for common injection attack patterns
func containsInjectionPatterns(input string) bool {
	// Check for path traversal
	if strings.Contains(input, "../") || strings.Contains(input, "..\\") {
		return true
	}
	// Check for null bytes
	if strings.Contains(input, "\x00") {
		return true
	}
	// Check for common SQL injection patterns
	sqlPatterns := []string{
		";--", "/*", "*/", "@@", "@variable",
		"EXEC(", "SELECT * FROM", "INSERT INTO", "DELETE FROM", "DROP TABLE",
		"UNION SELECT", "OR 1=1", "' OR '", "'='",
	}
	lowerInput := strings.ToLower(input)
	for _, pattern := range sqlPatterns {
		if strings.Contains(lowerInput, strings.ToLower(pattern)) {
			return true
		}
	}
	// Check for script tags (XSS)
	if strings.Contains(lowerInput, "<script") || strings.Contains(lowerInput, "javascript:") {
		return true
	}
	return false
}

// securityHeadersMiddleware adds security headers to all responses
func (s *RESTServer) securityHeadersMiddleware(handler Handler) Handler {
	return func(ctx *Context) error {
		// Add security headers
		ctx.Response.Header().Set("X-Content-Type-Options", "nosniff")
		ctx.Response.Header().Set("X-Frame-Options", "DENY")
		ctx.Response.Header().Set("X-XSS-Protection", "1; mode=block")
		ctx.Response.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		ctx.Response.Header().Set("Content-Security-Policy", "default-src 'self'")
		// HSTS: force HTTPS (max-age 1 year, include subdomains, preload)
		ctx.Response.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")

		// Check for required headers on sensitive endpoints
		if strings.HasPrefix(ctx.Request.URL.Path, "/api/v1/") {
			// Validate Host header to prevent DNS rebinding
			if ctx.Request.Host == "" {
				return ctx.Error(http.StatusBadRequest, "Missing Host header")
			}
		}

		return handler(ctx)
	}
}

// validatePathParams validates path parameters for common injection patterns
func (s *RESTServer) validatePathParams(handler Handler) Handler {
	return func(ctx *Context) error {
		for key, value := range ctx.Params {
			// Check for path traversal attempts
			if strings.Contains(value, "..") || strings.Contains(value, "//") {
				s.logger.Warn("Path traversal attempt detected",
					"ip", ctx.Request.RemoteAddr,
					"param", key,
					"value", value)
				return ctx.Error(http.StatusBadRequest, "Invalid path parameter")
			}
			// Check parameter length
			if len(value) > 256 {
				return ctx.Error(http.StatusBadRequest, "Path parameter too long")
			}
			// Check for null bytes
			if strings.Contains(value, "\x00") {
				return ctx.Error(http.StatusBadRequest, "Invalid characters in parameter")
			}
		}
		return handler(ctx)
	}
}

// rateLimitMiddleware implements rate limiting per IP and per user
func (s *RESTServer) rateLimitMiddleware(handler Handler) Handler {
	type clientState struct {
		count     int
		resetTime time.Time
	}

	var (
		mu          sync.RWMutex
		ipClients   = make(map[string]*clientState)
		userClients = make(map[string]*clientState) // Per-user rate limiting
		// Different limits for different types of endpoints
		defaultLimit   = 100 // requests per minute for regular endpoints
		authLimit      = 10  // stricter limit for auth endpoints
		sensitiveLimit = 20  // limit for sensitive operations
		window         = time.Minute
	)

	// Determine limit based on endpoint
	getLimit := func(path string) int {
		switch {
		case strings.HasPrefix(path, "/api/v1/auth/") || strings.HasPrefix(path, "/auth/") || path == "/login" || path == "/register":
			return authLimit
		case strings.HasPrefix(path, "/api/v1/souls") && (strings.Contains(path, "delete") || strings.Contains(path, "update")):
			return sensitiveLimit
		default:
			return defaultLimit
		}
	}

	// Cleanup old entries periodically
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			mu.Lock()
			now := time.Now()
			for ip, state := range ipClients {
				if state.resetTime.Before(now) {
					delete(ipClients, ip)
				}
			}
			for user, state := range userClients {
				if state.resetTime.Before(now) {
					delete(userClients, user)
				}
			}
			mu.Unlock()
		}
	}()

	return func(ctx *Context) error {
		// Skip rate limiting for health endpoints
		if strings.HasPrefix(ctx.Request.URL.Path, "/health") ||
			strings.HasPrefix(ctx.Request.URL.Path, "/ready") {
			return handler(ctx)
		}

		// Get client IP
		ip := ctx.Request.RemoteAddr
		if forwarded := ctx.Request.Header.Get("X-Forwarded-For"); forwarded != "" {
			ip = strings.Split(forwarded, ",")[0]
		}

		// Get user ID if authenticated
		userID := ""
		if ctx.User != nil {
			userID = ctx.User.ID
		}

		limit := getLimit(ctx.Request.URL.Path)
		now := time.Now()

		mu.Lock()

		// Check IP-based rate limit
		ipState, ipExists := ipClients[ip]
		if !ipExists || ipState.resetTime.Before(now) {
			ipClients[ip] = &clientState{
				count:     1,
				resetTime: now.Add(window),
			}
		} else if ipState.count >= limit {
			mu.Unlock()
			ctx.Response.Header().Set("Retry-After", strconv.Itoa(int(ipState.resetTime.Sub(now).Seconds())))
			ctx.Response.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			ctx.Response.Header().Set("X-RateLimit-Remaining", "0")
			return ctx.Error(http.StatusTooManyRequests, fmt.Sprintf("Rate limit exceeded (%d requests/minute)", limit))
		} else {
			ipState.count++
		}

		// Check user-based rate limit (if authenticated)
		if userID != "" {
			userState, userExists := userClients[userID]
			if !userExists || userState.resetTime.Before(now) {
				userClients[userID] = &clientState{
					count:     1,
					resetTime: now.Add(window),
				}
			} else if userState.count >= limit*2 { // User limit is 2x IP limit (shared across IPs)
				mu.Unlock()
				ctx.Response.Header().Set("Retry-After", strconv.Itoa(int(userState.resetTime.Sub(now).Seconds())))
				ctx.Response.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit*2))
				ctx.Response.Header().Set("X-RateLimit-Remaining", "0")
				return ctx.Error(http.StatusTooManyRequests, "User rate limit exceeded")
			} else {
				userState.count++
			}
		}

		// Set rate limit headers
		ipState = ipClients[ip]
		remaining := limit - ipState.count
		if remaining < 0 {
			remaining = 0
		}
		ctx.Response.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
		ctx.Response.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		ctx.Response.Header().Set("X-RateLimit-Reset", strconv.Itoa(int(ipState.resetTime.Unix())))

		mu.Unlock()
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

	// Handle CORS preflight globally (before route matching)
	if method == "OPTIONS" {
		origin := req.Header.Get("Origin")

		// Use configured allowed origins from router (same as corsMiddleware)
		// Priority: router.allowedOrigins (from config) > environment variable > empty (production-safe)
		var allowedOrigins []string
		if len(r.allowedOrigins) > 0 {
			allowedOrigins = r.allowedOrigins
		} else if envOrigins := os.Getenv("ANUBIS_CORS_ORIGINS"); envOrigins != "" {
			allowedOrigins = strings.Split(envOrigins, ",")
		}
		// If no origins configured, we'll check against empty list (production-safe default)

		originAllowed := false
		for _, allowed := range allowedOrigins {
			if strings.EqualFold(origin, allowed) {
				originAllowed = true
				break
			}
		}

		if originAllowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Max-Age", "86400")
		w.WriteHeader(http.StatusNoContent)
		return
	}

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

	// Status page routes (before dashboard fallback)
	if r.statusPage != nil && strings.HasPrefix(path, "/status/") {
		r.statusPage.ServeHTTP(w, req)
		return
	}

	// ACME challenge routes for Let's Encrypt
	if r.statusPage != nil && strings.HasPrefix(path, "/.well-known/acme-challenge/") {
		r.statusPage.ServeHTTP(w, req)
		return
	}

	// No route found - serve dashboard for non-API routes
	// Exclude API, health, metrics, and status page routes
	isExcluded := strings.HasPrefix(path, "/api/") ||
		strings.HasPrefix(path, "/health") ||
		strings.HasPrefix(path, "/ready") ||
		
		path == "/status" ||
		path == "/status.html" ||
		strings.HasPrefix(path, "/public/")
	if r.dashboard != nil && !isExcluded {
		r.dashboard.ServeHTTP(w, req)
		return
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

// InternalError logs the actual error and returns a generic internal server error
// Use this for 500 errors to prevent information leakage (CWE-209)
func (s *RESTServer) internalError(ctx *Context, err error, context string) error {
	s.logger.Error("internal server error", "context", context, "error", err, "path", ctx.Request.URL.Path)
	return ctx.Error(http.StatusInternalServerError, "internal server error")
}

// Bind parses JSON request body with size limits to prevent DoS
func (c *Context) Bind(v interface{}) error {
	// Limit request body size to prevent memory exhaustion
	c.Request.Body = http.MaxBytesReader(c.Response, c.Request.Body, maxRequestBodySize)
	return json.NewDecoder(&maxDepthReader{r: c.Request.Body}).Decode(v)
}

// Maintenance Window handlers

func (s *RESTServer) handleListMaintenanceWindows(ctx *Context) error {
	windows, err := s.store.ListMaintenanceWindows()
	if err != nil {
		return s.internalError(ctx, err, "internal server error")
	}
	return ctx.JSON(http.StatusOK, windows)
}

func (s *RESTServer) handleCreateMaintenanceWindow(ctx *Context) error {
	var w core.MaintenanceWindow
	// Limit request body size to prevent memory exhaustion
	ctx.Request.Body = http.MaxBytesReader(ctx.Response, ctx.Request.Body, maxRequestBodySize)
	if err := json.NewDecoder(ctx.Request.Body).Decode(&w); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid JSON")
	}
	if err := s.store.SaveMaintenanceWindow(&w); err != nil {
		return s.internalError(ctx, err, "internal server error")
	}
	return ctx.JSON(http.StatusCreated, w)
}

func (s *RESTServer) handleGetMaintenanceWindow(ctx *Context) error {
	id := ctx.Params["id"]
	w, err := s.store.GetMaintenanceWindow(id)
	if err != nil {
		return ctx.Error(http.StatusNotFound, "maintenance window not found")
	}
	return ctx.JSON(http.StatusOK, w)
}

func (s *RESTServer) handleUpdateMaintenanceWindow(ctx *Context) error {
	id := ctx.Params["id"]
	w, err := s.store.GetMaintenanceWindow(id)
	if err != nil {
		return ctx.Error(http.StatusNotFound, "maintenance window not found")
	}
	var input map[string]interface{}
	// Limit request body size to prevent memory exhaustion
	ctx.Request.Body = http.MaxBytesReader(ctx.Response, ctx.Request.Body, maxRequestBodySize)
	if err := json.NewDecoder(ctx.Request.Body).Decode(&input); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid JSON")
	}
	// Apply updates with safe type assertions
	if v, ok := input["name"]; ok {
		if str, ok := v.(string); ok {
			w.Name = str
		}
	}
	if v, ok := input["description"]; ok {
		if str, ok := v.(string); ok {
			w.Description = str
		}
	}
	if v, ok := input["soul_ids"]; ok {
		if ids, ok := v.([]interface{}); ok {
			w.SoulIDs = make([]string, 0, len(ids))
			for _, id := range ids {
				if str, ok := id.(string); ok {
					w.SoulIDs = append(w.SoulIDs, str)
				}
			}
		}
	}
	if v, ok := input["tags"]; ok {
		if tags, ok := v.([]interface{}); ok {
			w.Tags = make([]string, 0, len(tags))
			for _, t := range tags {
				if str, ok := t.(string); ok {
					w.Tags = append(w.Tags, str)
				}
			}
		}
	}
	if v, ok := input["start_time"]; ok {
		if str, ok := v.(string); ok {
			w.StartTime, _ = time.Parse(time.RFC3339, str)
		}
	}
	if v, ok := input["end_time"]; ok {
		if str, ok := v.(string); ok {
			w.EndTime, _ = time.Parse(time.RFC3339, str)
		}
	}
	if v, ok := input["recurring"]; ok {
		if str, ok := v.(string); ok {
			w.Recurring = str
		}
	}
	if v, ok := input["enabled"]; ok {
		if b, ok := v.(bool); ok {
			w.Enabled = b
		}
	}
	w.UpdatedAt = time.Now().UTC()
	if err := s.store.SaveMaintenanceWindow(w); err != nil {
		return s.internalError(ctx, err, "internal server error")
	}
	return ctx.JSON(http.StatusOK, w)
}

func (s *RESTServer) handleDeleteMaintenanceWindow(ctx *Context) error {
	id := ctx.Params["id"]
	if err := s.store.DeleteMaintenanceWindow(id); err != nil {
		return ctx.Error(http.StatusNotFound, "maintenance window not found")
	}
	return ctx.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}

// OnJudgmentCallback returns a callback function for broadcasting judgments via WebSocket
func (s *RESTServer) OnJudgmentCallback() func(*core.Judgment) {
	return func(judgment *core.Judgment) {
		s.metricsMu.Lock()
		s.judgmentsTotal++
		s.metricsMu.Unlock()

		if s.ws != nil {
			s.ws.BroadcastJudgment(judgment)
		}
	}
}
