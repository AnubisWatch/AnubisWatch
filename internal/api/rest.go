package api

import (
	"bytes"
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/crypto/bcrypt"
)

const (
	// maxRequestBodySize limits JSON request body size to prevent DoS (1MB)
	maxRequestBodySize = 1 << 20 // 1MB
	// maxJSONDepth limits JSON nesting depth to prevent stack overflow
	maxJSONDepth = 32
)

// sensitiveQueryParams lists URL query parameters whose values must never
// appear in logs, traces, or error messages (tokens, secrets, keys, etc.).
var sensitiveQueryParams = map[string]bool{
	"token":             true,
	"api_key":           true,
	"apikey":            true,
	"secret":            true,
	"password":          true,
	"passwd":            true,
	"key":               true,
	"auth":              true,
	"code":              true,
	"state":             true,
	"nonce":             true,
	"id_token":          true,
	"access_token":      true,
	"refresh_token":     true,
	"client_secret":     true,
	"error_description": true,
}

// maxDepthReader wraps an io.Reader to track JSON nesting depth.
// It counts [, { as +1 and ], } as -1, rejecting reads when
// depth exceeds maxJSONDepth.
type maxDepthReader struct {
	r        io.Reader
	depth    int
	inString bool
	escaped  bool
	err      error
}

func (m *maxDepthReader) Read(p []byte) (int, error) {
	if m.err != nil {
		return 0, m.err
	}
	n, err := m.r.Read(p)
	for i, b := range p[:n] {
		if m.inString {
			if m.escaped {
				m.escaped = false
				continue
			}
			switch b {
			case '\\':
				m.escaped = true
			case '"':
				m.inString = false
			}
			continue
		}

		switch b {
		case '"':
			m.inString = true
		case '[', '{':
			m.depth++
			if m.depth > maxJSONDepth {
				m.err = fmt.Errorf("JSON depth limit exceeded (max %d)", maxJSONDepth)
				return i, m.err
			}
		case ']', '}':
			m.depth--
		}
	}
	return n, err
}

type rateLimitClientState struct {
	count     int
	resetTime time.Time
}

// RESTServer implements the HTTP REST API
// The scribes record the judgments on papyrus scrolls
type RESTServer struct {
	mu         sync.Mutex // protects http field during Start/Stop race
	config     core.ServerConfig
	authConfig core.AuthConfig
	router     *Router
	http       *http.Server
	logger     *slog.Logger
	audit      *AuditLogger
	mcp        *MCPServer
	ws         *WebSocketServer
	// resetMail delivers password reset links; nil when mail is not configured.
	resetMail *resetMailer

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
	buildVersion     string
	buildCommit      string
	buildDate        string
	judgmentsTotal   uint64
	runtimeStats     func() RuntimeStats // see SetRuntimeStats; nil until wired
	verdictsFired    uint64
	verdictsResolved uint64
	// Settings (see settings.go): presentation overrides persisted through
	// settingsStore, plus read-only values reported from the loaded config.
	uiConfigMu    sync.RWMutex
	uiConfig      map[string]interface{}
	settingsStore SettingsStore
	storagePath   string
	retention     time.Duration
	// retentionPolicy bounds report periods and exports (reports.go).
	retentionPolicy *core.RetentionConfig
	// exports limits concurrent judgment exports (export_limit.go).
	exports exportLimiter

	// Rate limiter state is shared by every route wrapper on this server.
	rateLimitMu          sync.Mutex
	rateLimitIPClients   map[string]*rateLimitClientState
	rateLimitUserClients map[string]*rateLimitClientState
	rateLimitInitOnce    sync.Once
	rateLimitStopCh      chan struct{}
	// stopRateLimitOnce guards the close of rateLimitStopCh so Stop is
	// idempotent.
	stopRateLimitOnce sync.Once

	// Trusted proxies for X-Forwarded-For validation (nil/empty = trust none)
	trustedProxies []string

	proxyMatcherOnce sync.Once
	proxyMatcher     trustedProxyMatcher

	// sessions tracks live WebSocket/SSE streams by the session token that
	// opened them (shared with ws), so logout and password changes can close
	// them; sessionRevalidateInterval (default 30s) is how often an SSE stream
	// re-checks its token.
	sessions                  *liveSessionRegistry
	sessionRevalidateInterval time.Duration

	// branding is dashboard.branding, served by GET /api/v1/branding.
	branding core.DashboardBranding
	// apiKeys authenticates "anb_" bearer tokens and backs /api/v1/api-keys
	// (nil: API keys disabled). See apikeys.go.
	apiKeys APIKeyManager

	// heartbeat serves the push-monitor ping endpoints (nil: every token is
	// unknown). See heartbeat.go.
	heartbeat *heartbeatService
}

// trustedProxyMatcher parses server.trusted_proxies once.
func (s *RESTServer) trustedProxyMatcher() trustedProxyMatcher {
	s.proxyMatcherOnce.Do(func() { s.proxyMatcher = newTrustedProxyMatcher(s.trustedProxies) })
	return s.proxyMatcher
}

// realIP resolves the client IP, honouring X-Forwarded-For only as far as
// trusted proxies vouch for it (see resolveClientIP).
func (s *RESTServer) realIP(r *http.Request) string {
	return resolveClientIP(r, s.trustedProxyMatcher())
}

// SetBuildInfo publishes the same linker-injected identity reported by the CLI.
func (s *RESTServer) SetBuildInfo(version, commit, date string) {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	s.buildVersion, s.buildCommit, s.buildDate = version, commit, date
}

// Router handles HTTP routing
type JourneyExecutor interface {
	RunOnce(ctx context.Context, journey *core.JourneyConfig) (*core.JourneyRun, error)
	ListRuns(ctx context.Context, workspaceID, journeyID string, limit int) ([]*core.JourneyRun, error)
	GetRun(ctx context.Context, workspaceID, journeyID, runID string) (*core.JourneyRun, error)
}

// Router handles HTTP routing
type Router struct {
	routes map[string]map[string]Handler // path -> method -> handler
	// paramRoutes buckets parameterized patterns by segment count, each bucket
	// kept in most-specific-first order. Go map iteration is randomized, so
	// resolving parameterized routes by ranging over `routes` would pick a
	// non-deterministic winner whenever two patterns match the same path.
	paramRoutes map[int][]string
	middleware  []Middleware
	// fallbackMiddleware wraps requests no route matched (dashboard, status
	// pages, 404), which never reach the per-route middleware chain.
	fallbackMiddleware []Middleware
	dashboard          http.Handler
	statusPage         http.Handler
	allowedOrigins     []string // Allowed CORS origins from config
}

// moreSpecificRoute reports whether pattern a must be tried before b.
// A literal segment beats a `:param` at the earliest position where they
// differ (so /souls/stats wins over /souls/:id); ties break lexicographically
// to keep the order total and stable.
func moreSpecificRoute(a, b string) bool {
	aParts := strings.Split(a, "/")
	bParts := strings.Split(b, "/")
	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		aParam := strings.HasPrefix(aParts[i], ":")
		bParam := strings.HasPrefix(bParts[i], ":")
		if aParam != bParam {
			return !aParam
		}
	}
	if len(aParts) != len(bParts) {
		return len(aParts) < len(bParts)
	}
	return a < b
}

// Handler is an HTTP handler function
type Handler func(ctx *Context) error

// deprecatedAlias marks a route kept only for compatibility: responses carry
// `Deprecation: true` (RFC 9745) and a Link to the canonical route computed
// from the request path, so clients can find and switch to it.
func deprecatedAlias(successor func(path string) string, next Handler) Handler {
	return func(ctx *Context) error {
		h := ctx.Response.Header()
		h.Set("Deprecation", "true")
		h.Set("Link", "<"+successor(ctx.Request.URL.Path)+`>; rel="successor-version"`)
		return next(ctx)
	}
}

// alertsAliasSuccessor maps /api/v1/alerts/{channels,rules}... to the
// canonical /api/v1/{channels,rules}....
func alertsAliasSuccessor(path string) string {
	return strings.Replace(path, "/api/v1/alerts/", "/api/v1/", 1)
}

type hostAwareStatusPageHandler interface {
	http.Handler
	CanServeHost(host string) bool
}

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
	RequestID string // unique request identifier for log correlation
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

// maxListLimit is the largest page size list endpoints accept.
const maxListLimit = 100

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
	UpsertSoul(soul *core.Soul)
	RemoveSoul(soulID string)
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
	TestChannel(ctx context.Context, id string, workspace string) error
	DeleteChannel(id string) error
	DeleteChannelWithWorkspace(id string, workspace string) error
	DeleteRule(id string) error
	DeleteRuleWithWorkspace(id string, workspace string) error
	AcknowledgeIncident(incidentID, userID, workspace string) error
	ResolveIncident(incidentID, userID, workspace string) error
	ListActiveIncidents() []*core.Incident
	ListIncidents() []*core.Incident
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

type workspaceSwitcher interface {
	SwitchWorkspace(token, workspace string) (*User, error)
}

// clientAwareLogin is implemented by authenticators whose brute-force
// protection keys failed attempts by the caller's address, so a flood of
// wrong passwords from one address cannot lock the account out everywhere.
type clientAwareLogin interface {
	LoginFrom(email, password, clientIP string) (*User, string, error)
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
	// ApplyMutation routes an FSM-backed mutation through the cluster consensus
	// layer. A follower forwards it to the leader and returns once it applied
	// the entry itself; with no reachable leader it returns *core.RaftError
	// with ErrNoLeader, which handlers answer with 503. When clustering is
	// disabled it returns ErrNotLeader so the caller can write locally instead.
	ApplyMutation(cmd core.FSMCommand, timeout time.Duration) (uint64, uint64, error)
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
	// APIKeyID is set when the request authenticated with an API key rather
	// than an interactive session; ID is then "apikey:<APIKeyID>".
	APIKeyID string `json:"api_key_id,omitempty"`
	// MustChangePassword is set for an account created with an initial
	// password by an administrator; until it is changed, requireAuth admits
	// only the endpoints needed to change it.
	MustChangePassword bool `json:"must_change_password,omitempty"`
}

// IsAPIKey reports whether u authenticated with an API key.
func (u *User) IsAPIKey() bool { return u != nil && u.APIKeyID != "" }

// NewRESTServer creates a new REST server
func NewRESTServer(config core.ServerConfig, authConfig core.AuthConfig, store Storage, probe ProbeEngine, alert AlertManager, auth Authenticator, cluster ClusterManager, journey JourneyExecutor, dashboard http.Handler, statusPage http.Handler, mcp *MCPServer, logger *slog.Logger) *RESTServer {
	wsServer := NewWebSocketServer(logger, auth, config.AllowedOrigins) // Wire config origins to WebSocket
	wsServer.trustedProxies = config.TrustedProxies                     // spoof-resistant client IP for WS rate limits

	s := &RESTServer{
		config:     config,
		authConfig: authConfig,
		router: &Router{
			routes:         make(map[string]map[string]Handler),
			dashboard:      dashboard,
			statusPage:     statusPage,
			allowedOrigins: config.AllowedOrigins, // Store allowed origins in router for CORS preflight
		},
		logger:          logger.With("component", "rest_server"),
		audit:           &AuditLogger{logger: logger, backend: &structuredAuditBackend{logger: logger}},
		store:           store,
		probe:           probe,
		alert:           alert,
		auth:            auth,
		cluster:         cluster,
		journey:         journey,
		mcp:             mcp,
		ws:              wsServer,
		dashboard:       dashboard,
		statusPage:      statusPage,
		uiConfig:        make(map[string]interface{}),
		rateLimitStopCh: make(chan struct{}),
		trustedProxies:  config.TrustedProxies,
		sessions:        wsServer.sessions,
	}

	s.setupRoutes()
	wsServer.Start()
	return s
}

// setupRoutes configures API routes
func (s *RESTServer) setupRoutes() {
	// Middleware - order matters
	s.router.Use(s.requestIDMiddleware) // MUST be first: injects request ID before any logging
	s.router.Use(s.loggingMiddleware)
	s.router.Use(s.securityHeadersMiddleware) // Add security headers to all responses
	s.router.Use(s.corsMiddleware)
	s.router.Use(s.recoveryMiddleware)
	s.router.Use(s.validateJSONMiddleware)
	s.router.Use(s.validatePathParams) // Validate path parameters
	s.router.Use(s.rateLimitMiddleware)

	// Requests no API route matches (the embedded dashboard, status pages,
	// 404s) bypass the route chain above, but still need the browser
	// security headers. They are not rate limited: one dashboard load fetches
	// dozens of static assets.
	s.router.fallbackMiddleware = []Middleware{
		s.requestIDMiddleware,
		s.securityHeadersMiddleware,
		s.recoveryMiddleware,
	}

	// Health
	s.router.Handle("GET", "/health", s.handleHealth)
	s.router.Handle("GET", "/ready", s.handleReady)
	// /metrics is unauthenticated by default for Prometheus scraping. Because
	// it exposes monitor names/latencies across all workspaces, operators in
	// multi-tenant or internet-exposed deployments can set server.metrics_auth
	// to require a bearer token.
	if s.config.MetricsAuth {
		s.router.Handle("GET", "/metrics", s.requireAuth(s.handleMetrics))
	} else {
		s.router.Handle("GET", "/metrics", s.handleMetrics)
	}

	// OpenAPI / Swagger (no auth required)
	s.router.Handle("GET", "/api/openapi.json", s.handleOpenAPIJSON)
	s.router.Handle("GET", "/api/openapi.yaml", s.handleOpenAPIYAML)
	s.router.Handle("GET", "/api/docs", s.handleOpenAPIDocs)

	// Public Status Pages (no auth required)
	s.router.Handle("GET", "/status", s.requireAuth(s.handleStatusPage))
	s.router.Handle("GET", "/status.html", s.requireAuth(s.handleStatusPageHTML))
	s.router.Handle("GET", "/public/status", s.handlePublicStatus)

	// Auth
	s.router.Handle("GET", "/api/v1/auth/methods", s.handleAuthMethods)
	s.router.Handle("GET", "/api/v1/branding", s.handleBranding)
	s.router.Handle("POST", "/api/v1/auth/login", s.handleLogin)
	s.router.Handle("POST", "/api/v1/auth/logout", s.handleLogout)
	s.router.Handle("GET", "/api/v1/auth/me", s.requireAuth(s.handleMe))
	s.router.Handle("POST", "/api/v1/auth/workspace", s.requireAuth(s.handleSwitchWorkspace))
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
	s.router.Handle("GET", "/api/v1/souls/:id/uptime", s.requireAuth(s.handleSoulUptime))
	s.router.Handle("GET", "/api/v1/souls/:id/judgments/export", s.requireRole(s.handleExportJudgments, "judgments:read"))
	// SLA reports (reports.go)
	s.router.Handle("GET", "/api/v1/reports/sla", s.requireRole(s.handleSLAReport, "judgments:read"))
	s.router.Handle("GET", "/api/v1/reports/sla.csv", s.requireRole(s.handleSLAReportCSV, "judgments:read"))
	// Heartbeat (push) monitors (heartbeat.go): the ping URL reveal/rotate
	// routes, and the public pings, authenticated by their token alone.
	s.router.Handle("GET", "/api/v1/souls/:id/heartbeat", s.requireAuth(s.handleGetSoulHeartbeat))
	s.router.Handle("POST", "/api/v1/souls/:id/heartbeat/rotate", s.requireRole(s.handleRotateHeartbeatToken, "souls:*"))
	s.router.Handle("GET", "/api/v1/heartbeat/:token", s.handleHeartbeatPing(core.HeartbeatSuccess))
	s.router.Handle("POST", "/api/v1/heartbeat/:token", s.handleHeartbeatPing(core.HeartbeatSuccess))
	s.router.Handle("GET", "/api/v1/heartbeat/:token/start", s.handleHeartbeatPing(core.HeartbeatStart))
	s.router.Handle("POST", "/api/v1/heartbeat/:token/start", s.handleHeartbeatPing(core.HeartbeatStart))
	s.router.Handle("GET", "/api/v1/heartbeat/:token/fail", s.handleHeartbeatPing(core.HeartbeatFail))
	s.router.Handle("POST", "/api/v1/heartbeat/:token/fail", s.handleHeartbeatPing(core.HeartbeatFail))

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
	s.router.Handle("GET", "/api/v1/quota", s.requireAuth(s.handleGetQuota))

	// Stats
	s.router.Handle("GET", "/api/v1/stats", s.requireAuth(s.handleStats))
	s.router.Handle("GET", "/api/v1/stats/overview", s.requireAuth(s.handleStatsOverview))

	// Cluster (Raft)
	s.router.Handle("GET", "/api/v1/cluster/status", s.requireAuth(s.handleClusterStatus))
	s.router.Handle("GET", "/api/v1/cluster/peers", s.requireAuth(s.handleClusterPeers))
	s.router.Handle("POST", "/api/v1/cluster/nodes", s.requireRole(s.handleAddClusterNode, "members:*"))
	s.router.Handle("DELETE", "/api/v1/cluster/nodes/:id", s.requireRole(s.handleRemoveClusterNode, "members:*"))

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
	// Status page incidents and scheduled maintenance (statuspage_incidents.go)
	s.router.Handle("GET", "/api/v1/status-pages/:id/incidents", s.requireAuth(s.handleListStatusPageIncidents))
	s.router.Handle("POST", "/api/v1/status-pages/:id/incidents", s.requireRole(s.handleCreateStatusPageIncident, "settings:write"))
	s.router.Handle("PATCH", "/api/v1/status-pages/:id/incidents/:incidentId", s.requireRole(s.handlePatchStatusPageIncident, "settings:write"))
	s.router.Handle("DELETE", "/api/v1/status-pages/:id/incidents/:incidentId", s.requireRole(s.handleDeleteStatusPageIncident, "settings:write"))
	s.router.Handle("POST", "/api/v1/status-pages/:id/incidents/:incidentId/updates", s.requireRole(s.handlePostStatusPageIncidentUpdate, "settings:write"))
	s.router.Handle("POST", "/api/v1/status-pages/:id/incidents/:incidentId/resolve", s.requireRole(s.handleResolveStatusPageIncident, "settings:write"))
	s.router.Handle("GET", "/api/v1/status-pages/:id/announcements", s.requireAuth(s.handleListStatusPageAnnouncements))
	s.router.Handle("POST", "/api/v1/status-pages/:id/announcements", s.requireRole(s.handleCreateStatusPageAnnouncement, "settings:write"))
	s.router.Handle("PATCH", "/api/v1/status-pages/:id/announcements/:announcementId", s.requireRole(s.handlePatchStatusPageAnnouncement, "settings:write"))
	s.router.Handle("DELETE", "/api/v1/status-pages/:id/announcements/:announcementId", s.requireRole(s.handleDeleteStatusPageAnnouncement, "settings:write"))

	// MCP (Model Context Protocol)
	s.router.Handle("POST", "/api/v1/mcp", s.requireAuth(s.handleMCP))

	// API keys (automation credentials). Managed by interactive users only;
	// a key cannot list, create or revoke keys (see apikeys.go).
	s.router.Handle("GET", "/api/v1/api-keys", s.requireRole(s.handleListAPIKeys, "apikeys:*"))
	s.router.Handle("POST", "/api/v1/api-keys", s.requireRole(s.handleCreateAPIKey, "apikeys:*"))
	s.router.Handle("DELETE", "/api/v1/api-keys/:id", s.requireRole(s.handleRevokeAPIKey, "apikeys:*"))

	// Members and invitations (interactive users only; see members.go)
	s.router.Handle("GET", "/api/v1/members", s.requireRole(s.handleListMembers, "members:*"))
	s.router.Handle("POST", "/api/v1/members", s.requireRole(s.handleCreateMember, "members:*"))
	s.router.Handle("PATCH", "/api/v1/members/:id", s.requireRole(s.handleUpdateMember, "members:*"))
	s.router.Handle("DELETE", "/api/v1/members/:id", s.requireRole(s.handleRemoveMember, "members:*"))
	s.router.Handle("DELETE", "/api/v1/users/:id", s.requireRole(s.handleDeleteUser, "members:*"))
	s.router.Handle("GET", "/api/v1/invitations", s.requireRole(s.handleListInvitations, "members:*"))
	s.router.Handle("POST", "/api/v1/invitations", s.requireRole(s.handleCreateInvitation, "members:*"))
	s.router.Handle("DELETE", "/api/v1/invitations/:id", s.requireRole(s.handleRevokeInvitation, "members:*"))
	s.router.Handle("POST", "/api/v1/auth/accept-invite", s.handleAcceptInvitation)

	// Deprecated aliases: /alerts/channels|rules duplicate /channels and /rules,
	// /users/me duplicates /auth/me. They keep working but answer with
	// Deprecation and a Link to the canonical route; nothing in the dashboard
	// calls them.
	s.router.Handle("GET", "/api/v1/alerts/channels", deprecatedAlias(alertsAliasSuccessor, s.requireAuth(s.handleListChannels)))
	s.router.Handle("POST", "/api/v1/alerts/channels", deprecatedAlias(alertsAliasSuccessor, s.requireRole(s.handleCreateChannel, "channels:*")))
	s.router.Handle("GET", "/api/v1/alerts/channels/:id", deprecatedAlias(alertsAliasSuccessor, s.requireAuth(s.handleGetChannel)))
	s.router.Handle("PUT", "/api/v1/alerts/channels/:id", deprecatedAlias(alertsAliasSuccessor, s.requireRole(s.handleUpdateChannel, "channels:*")))
	s.router.Handle("DELETE", "/api/v1/alerts/channels/:id", deprecatedAlias(alertsAliasSuccessor, s.requireRole(s.handleDeleteChannel, "channels:*")))
	s.router.Handle("POST", "/api/v1/alerts/channels/:id/test", deprecatedAlias(alertsAliasSuccessor, s.requireRole(s.handleTestChannel, "channels:*")))

	s.router.Handle("GET", "/api/v1/alerts/rules", deprecatedAlias(alertsAliasSuccessor, s.requireAuth(s.handleListRules)))
	s.router.Handle("POST", "/api/v1/alerts/rules", deprecatedAlias(alertsAliasSuccessor, s.requireRole(s.handleCreateRule, "rules:*")))
	s.router.Handle("GET", "/api/v1/alerts/rules/:id", deprecatedAlias(alertsAliasSuccessor, s.requireAuth(s.handleGetRule)))
	s.router.Handle("PUT", "/api/v1/alerts/rules/:id", deprecatedAlias(alertsAliasSuccessor, s.requireRole(s.handleUpdateRule, "rules:*")))
	s.router.Handle("DELETE", "/api/v1/alerts/rules/:id", deprecatedAlias(alertsAliasSuccessor, s.requireRole(s.handleDeleteRule, "rules:*")))

	s.router.Handle("GET", "/api/v1/users/me", deprecatedAlias(func(string) string { return "/api/v1/auth/me" }, s.requireAuth(s.handleMe)))

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
	s.router.Handle("GET", "/api/v1/events", s.requireAuth(s.handleSSE))
}

// Handler returns the server's HTTP handler (routing plus the full
// middleware chain), for embedding the API in another server or test.
func (s *RESTServer) Handler() http.Handler {
	return s.router
}

// Start starts the REST server
func (s *RESTServer) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	if addr == ":0" {
		addr = ":8080"
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           s.router,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	s.mu.Lock()
	s.http = server
	s.mu.Unlock()

	s.logger.Info("REST server starting", "addr", addr)

	if s.config.TLS.Enabled {
		// Honour server.tls.min_version. It floors at TLS 1.2 (validation
		// rejects the TLS 1.0/1.1 selectors), so this can only ever raise the
		// bar relative to the version that used to be hardcoded here.
		//
		// PreferServerCipherSuites is deliberately not set: Go has ignored it
		// since 1.17, and setting it implied a control that does not exist.
		tlsConfig := &tls.Config{
			MinVersion: s.config.TLS.ResolveMinVersion(),
		}
		server.TLSConfig = tlsConfig
		return server.ListenAndServeTLS(s.config.TLS.Cert, s.config.TLS.Key)
	}
	return server.ListenAndServe()
}

// Stop stops the REST server. It is safe to call more than once: a second
// close of rateLimitStopCh would panic, and shutdown paths are exactly where
// a duplicate call is likely (signal handler racing a component failure).
func (s *RESTServer) Stop(ctx context.Context) error {
	// NewRESTServer starts the WebSocket broadcast loop, so RESTServer owns its
	// shutdown even when the HTTP listener was never started.
	if s.ws != nil {
		s.ws.Stop()
	}

	// Stop rate limiter cleanup goroutine
	s.stopRateLimitOnce.Do(func() {
		if s.rateLimitStopCh != nil {
			close(s.rateLimitStopCh)
		}
	})
	s.mu.Lock()
	http := s.http
	s.mu.Unlock()
	if http != nil {
		return http.Shutdown(ctx)
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
	checks := make(map[string]string)

	// Check storage - try a simple operation
	checks["storage"] = "ok"
	if s.store != nil {
		if pinger, ok := s.store.(interface{ Ping() error }); ok {
			if err := pinger.Ping(); err != nil {
				checks["storage"] = fmt.Sprintf("error: %v", err)
			}
		}
	}

	// A probe engine that stopped producing judgments while souls are
	// scheduled is the one failure a monitor cannot report about itself.
	if rs, ok := s.currentRuntimeStats(); ok {
		checks["scheduler"] = "ok"
		if problem := schedulerProblem(rs, time.Now()); problem != "" {
			checks["scheduler"] = problem
		}
	}

	// Check alert manager
	checks["alert_manager"] = "ok"
	if s.alert != nil {
		_ = s.alert.GetStats() // Just check that it responds
	}

	// Check cluster if clustered
	checks["cluster"] = "ok"
	if s.cluster != nil {
		status := s.cluster.GetStatus()
		if status != nil {
			if !status.IsClustered {
				checks["cluster"] = "standalone"
			} else if status.State != "leader" && status.State != "follower" {
				checks["cluster"] = fmt.Sprintf("unhealthy: %s", status.State)
			}
		}
	}

	// Readiness is a fail-closed admission signal. Any dependency marked as an
	// error or unhealthy must remove the pod from service endpoints.
	hasError := false
	for _, status := range checks {
		if strings.HasPrefix(status, "error") || strings.HasPrefix(status, "unhealthy") {
			hasError = true
			break
		}
	}

	statusCode := http.StatusOK
	readinessStatus := "ready"
	if hasError {
		statusCode = http.StatusServiceUnavailable
		readinessStatus = "not_ready"
	}

	return ctx.JSON(statusCode, map[string]interface{}{
		"status":    readinessStatus,
		"timestamp": time.Now().UTC(),
		"checks":    checks,
	})
}

func (s *RESTServer) handleOpenAPIJSON(ctx *Context) error {
	ctx.Response.Header().Set("Content-Type", "application/json")
	ctx.Response.Header().Set("Cache-Control", "public, max-age=3600")
	ctx.Response.WriteHeader(http.StatusOK)
	if _, err := ctx.Response.Write(openapiJSON); err != nil {
		s.logger.Warn("failed to write OpenAPI response", "err", err)
	}
	return nil
}

// handleOpenAPIYAML serves the spec in its authored form, which is what a
// client generator usually wants.
func (s *RESTServer) handleOpenAPIYAML(ctx *Context) error {
	ctx.Response.Header().Set("Content-Type", "application/yaml")
	ctx.Response.Header().Set("Cache-Control", "public, max-age=3600")
	ctx.Response.WriteHeader(http.StatusOK)
	if _, err := ctx.Response.Write(openapiYAML); err != nil {
		s.logger.Warn("failed to write OpenAPI response", "err", err)
	}
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
	// Override the default CSP (`default-src 'self'`) for this specific
	// endpoint — Swagger UI is loaded from jsdelivr CDN and needs an inline
	// initialization script. Scoped to this handler only; every other route
	// keeps the strict default from securityHeadersMiddleware.
	ctx.Response.Header().Set("Content-Security-Policy",
		"default-src 'self'; "+
			"script-src 'self' https://cdn.jsdelivr.net 'unsafe-inline'; "+
			"style-src 'self' https://cdn.jsdelivr.net 'unsafe-inline'; "+
			"img-src 'self' data:; "+
			"font-src 'self' data: https://cdn.jsdelivr.net; "+
			"connect-src 'self'")
	ctx.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
	ctx.Response.WriteHeader(http.StatusOK)
	if _, err := ctx.Response.Write([]byte(html)); err != nil {
		s.logger.Warn("failed to write status page HTML", "err", err)
	}
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

	var (
		user  *User
		token string
		err   error
	)
	if login, ok := s.auth.(clientAwareLogin); ok {
		user, token, err = login.LoginFrom(req.Email, req.Password, s.realIP(ctx.Request))
	} else {
		user, token, err = s.auth.Login(req.Email, req.Password)
	}
	if err != nil {
		return ctx.Error(http.StatusUnauthorized, "invalid credentials")
	}

	// SECURITY: Set httpOnly cookie for token storage (VULN-004 fix)
	// This prevents XSS attacks from accessing the token
	// #nosec G124 -- Secure is enabled for TLS/proxied HTTPS; local HTTP dev and E2E need non-Secure cookies.
	http.SetCookie(ctx.Response, &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   ctx.Request.TLS != nil || ctx.Request.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400 * 7, // 7 days
	})

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"user": user,
	})
}

func (s *RESTServer) handleLogout(ctx *Context) error {
	token := requestAuthToken(ctx.Request)

	if err := s.auth.Logout(token); err != nil {
		return ctx.Error(http.StatusInternalServerError, "logout failed")
	}
	// Live streams authenticated with this token only at their handshake;
	// close them now rather than at their next revalidation.
	s.sessions.revokeToken(token)

	// SECURITY: Clear the httpOnly cookie (VULN-004 fix)
	// #nosec G124 -- Matches the login cookie Secure policy so local HTTP logout can clear it.
	http.SetCookie(ctx.Response, &http.Cookie{
		Name:     "auth_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   ctx.Request.TLS != nil || ctx.Request.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})

	return ctx.JSON(http.StatusOK, map[string]interface{}{"message": "logged out"})
}

func (s *RESTServer) handleMe(ctx *Context) error {
	return ctx.JSON(http.StatusOK, ctx.User)
}

func (s *RESTServer) handleSwitchWorkspace(ctx *Context) error {
	if done, err := rejectAPIKey(ctx); done {
		return err
	}
	var req struct {
		Workspace string `json:"workspace"`
	}
	if err := ctx.Bind(&req); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid request body")
	}

	workspaceID := strings.TrimSpace(req.Workspace)
	if workspaceID == "" {
		return ctx.Error(http.StatusBadRequest, "workspace is required")
	}

	ws, err := s.store.GetWorkspaceNoCtx(workspaceID)
	if err != nil {
		if isNotFoundError(err) {
			return ctx.Error(http.StatusNotFound, "workspace not found")
		}
		return s.internalError(ctx, err, "failed to fetch workspace")
	}
	if ws == nil {
		return ctx.Error(http.StatusNotFound, "workspace not found")
	}
	if !canAccessWorkspace(ctx, ws) && !s.isWorkspaceMember(ctx, ws.ID) {
		return ctx.Error(http.StatusForbidden, "access denied")
	}

	switcher, ok := s.auth.(workspaceSwitcher)
	if !ok {
		return ctx.Error(http.StatusNotImplemented, "workspace switching is not supported by this authenticator")
	}

	user, err := switcher.SwitchWorkspace(requestAuthToken(ctx.Request), ws.ID)
	if err != nil {
		return s.internalError(ctx, err, "failed to switch workspace")
	}
	ctx.User = user
	ctx.Workspace = user.Workspace
	return ctx.JSON(http.StatusOK, user)
}

// Password management handlers (HIGH-04)

func (s *RESTServer) handleChangePassword(ctx *Context) error {
	if done, err := rejectAPIKey(ctx); done {
		return err
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := ctx.Bind(&req); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid request body")
	}

	// Use the same header-or-cookie extraction as requireAuth so HttpOnly
	// cookie sessions can change their password without a bearer header.
	token := requestAuthToken(ctx.Request)

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
	// A password change ends the user's sessions; end their live streams too.
	s.revokeInvalidLiveSessions()

	return ctx.JSON(http.StatusOK, map[string]string{
		"message": "password changed successfully, please login again",
	})
}

// handleAuthMethods tells the (unauthenticated) login page which sign-in
// methods exist. It exposes only booleans: no issuer, URL or user data.
//
//   - oidc: the authenticator supports the OIDC redirect flow, i.e. the
//     /api/v1/auth/oidc/login route is registered.
//   - ldap: auth.type is ldap; the password form authenticates against LDAP.
//   - local: a local account can sign in with a password. True for local and
//     OIDC (OIDC keeps the local admin login); false for LDAP, which falls
//     back to local accounts only while the LDAP server is unreachable.
func (s *RESTServer) handleAuthMethods(ctx *Context) error {
	_, oidc := s.auth.(OIDCAuth)
	ldap := s.authConfig.Type == "ldap"
	return ctx.JSON(http.StatusOK, map[string]bool{
		"local": !ldap,
		"oidc":  oidc,
		"ldap":  ldap,
		// password_reset: "Forgot password?" can email a reset link to the
		// local account (outgoing mail is configured).
		"password_reset": s.resetMail != nil && !ldap,
	})
}

func (s *RESTServer) handleRequestPasswordReset(ctx *Context) error {
	var req struct {
		Email string `json:"email"`
	}
	if err := ctx.Bind(&req); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid request body")
	}

	if s.resetMail != nil {
		return s.requestPasswordResetByEmail(ctx, req.Email)
	}

	// Without outgoing mail nothing can deliver a reset token, so none is
	// issued: each one would only grow the (in a cluster, replicated)
	// authentication state for an unauthenticated caller.
	return ctx.JSON(http.StatusOK, map[string]string{
		"message": "this server cannot email reset links; ask an administrator to reset your password (`anubis admin reset-password` for the configured admin)",
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
	s.revokeInvalidLiveSessions()

	return ctx.JSON(http.StatusOK, map[string]string{
		"message": "password reset successfully, please login with your new password",
	})
}

// OIDC handlers

// oidcFlowCookieMaxAge bounds the oidc_state/oidc_nonce cookies to the
// authenticator's 10-minute login-state lifetime.
const oidcFlowCookieMaxAge = 600

// oidcLoginFailedPath is where a browser lands when SSO fails; the login
// page shows a generic error for the sso_error parameter.
const oidcLoginFailedPath = "/login?sso_error=1"

// setOIDCFlowCookie sets (or, with an empty value, clears) one of the
// short-lived cookies binding an OIDC login to the browser that started it.
//
// They are SameSite=Lax, not Strict: the IdP returns the browser to
// /oidc/callback with a cross-site top-level GET, and a Strict cookie is
// withheld from exactly that request, so the callback never saw them. Lax
// still withholds them from cross-site subrequests and POSTs. They are Secure
// whenever the client is on HTTPS -- TLS here, or X-Forwarded-Proto from a
// trusted proxy -- and plain only for local HTTP development.
func (s *RESTServer) setOIDCFlowCookie(ctx *Context, name, value string) {
	maxAge := oidcFlowCookieMaxAge
	if value == "" {
		maxAge = -1
	}
	// #nosec G124 -- Secure follows the client's scheme (requestIsHTTPS); plain HTTP is local development only.
	http.SetCookie(ctx.Response, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/api/v1/auth/oidc/",
		HttpOnly: true,
		Secure:   requestIsHTTPS(ctx.Request, s.trustedProxyMatcher()),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
}

// oidcCallbackWantsBrowserRedirect reports whether the callback was reached
// by a browser navigation (Accept names text/html), which must end on a
// page, rather than by an API client expecting JSON.
func oidcCallbackWantsBrowserRedirect(r *http.Request) bool {
	accept := strings.ToLower(r.Header.Get("Accept"))
	return strings.Contains(accept, "text/html") || strings.Contains(accept, "application/xhtml+xml")
}

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

	// The nonce and state cookies bind the callback to the browser that
	// started this flow (CSRF / login-fixation protection). The PKCE verifier
	// stays server-side with the login state.
	s.setOIDCFlowCookie(ctx, "oidc_nonce", nonce)
	s.setOIDCFlowCookie(ctx, "oidc_state", state)
	ctx.Response.Header().Set("Cache-Control", "no-store")

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

	browser := oidcCallbackWantsBrowserRedirect(ctx.Request)
	ctx.Response.Header().Set("Cache-Control", "no-store")
	// fail answers an API client with JSON and sends a browser back to the
	// login page; the reason is logged, never reflected.
	fail := func(status int, message string) error {
		if browser {
			http.Redirect(ctx.Response, ctx.Request, oidcLoginFailedPath, http.StatusSeeOther)
			return nil
		}
		return ctx.Error(status, message)
	}

	// The flow cookies are single-use whatever the outcome.
	s.setOIDCFlowCookie(ctx, "oidc_nonce", "")
	s.setOIDCFlowCookie(ctx, "oidc_state", "")

	// Do not reflect provider-controlled query values. Besides being untrusted,
	// error descriptions can contain user identifiers or token fragments.
	if ctx.Request.URL.Query().Get("error") != "" {
		return fail(http.StatusBadRequest, "OIDC provider rejected authentication (access_denied)")
	}

	code := ctx.Request.URL.Query().Get("code")
	state := ctx.Request.URL.Query().Get("state")
	if code == "" || state == "" {
		return fail(http.StatusBadRequest, "missing code or state")
	}

	// The callback must come from the browser that started the flow: it
	// carries the state it was sent to the IdP with, and the nonce.
	nonceCookie, err := ctx.Request.Cookie("oidc_nonce")
	if err != nil || nonceCookie.Value == "" {
		s.logger.Error("OIDC callback missing nonce cookie")
		return fail(http.StatusBadRequest, "missing nonce cookie: possible CSRF attack")
	}
	stateCookie, err := ctx.Request.Cookie("oidc_state")
	if err != nil || subtle.ConstantTimeCompare([]byte(stateCookie.Value), []byte(state)) != 1 {
		s.logger.Error("OIDC callback state does not match the state cookie")
		return fail(http.StatusBadRequest, "state mismatch: possible CSRF attack")
	}

	user, token, err := oidcAuth.OIDCCallback(code, state, nonceCookie.Value)
	if err != nil {
		s.logger.Error("OIDC callback failed", "error", err)
		return fail(http.StatusUnauthorized, "OIDC authentication failed")
	}

	// Use the same HttpOnly session cookie policy as local login. Keeping the
	// token out of JSON prevents browser JavaScript from reading it. Strict is
	// fine here even though this response answers a cross-site navigation:
	// the dashboard's own same-origin requests carry it.
	// #nosec G124 -- Secure is enabled for TLS/proxied HTTPS; local HTTP dev and E2E need non-Secure cookies.
	http.SetCookie(ctx.Response, &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   ctx.Request.TLS != nil || ctx.Request.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400 * 7,
	})

	if browser {
		// The dashboard detects the session through /api/v1/auth/me.
		http.Redirect(ctx.Response, ctx.Request, "/", http.StatusSeeOther)
		return nil
	}
	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"user": user,
	})
}

// Soul handlers

// soulMonitorStatus maps a judgment status to the soul status the REST API
// reports. alive/dead keep their historical healthy/unhealthy names;
// degraded and embalmed pass through so callers can tell them apart from
// a soul that has never been checked.
func soulMonitorStatus(status core.SoulStatus) string {
	switch status {
	case core.SoulAlive:
		return "healthy"
	case core.SoulDead:
		return "unhealthy"
	case core.SoulDegraded:
		return string(core.SoulDegraded)
	case core.SoulEmbalmed:
		return string(core.SoulEmbalmed)
	default:
		return "unknown"
	}
}

// activeMaintenanceWindows returns the maintenance windows active at now.
// List endpoints fetch them once per request and test each soul against the
// result with core.SoulInMaintenance.
func (s *RESTServer) activeMaintenanceWindows(now time.Time) []*core.MaintenanceWindow {
	if s.store == nil {
		return nil
	}
	windows, err := s.store.ListMaintenanceWindows()
	if err != nil {
		s.logger.Warn("failed to list maintenance windows", "error", err)
		return nil
	}
	active := make([]*core.MaintenanceWindow, 0, len(windows))
	for _, window := range windows {
		if window != nil && window.IsActive(now) {
			active = append(active, window)
		}
	}
	return active
}

func (s *RESTServer) soulWithLatestJudgment(soul *core.Soul) soulMonitorDTO {
	now := time.Now()
	return s.soulWithLatestJudgmentAt(soul, s.activeMaintenanceWindows(now), now)
}

// soulWithLatestJudgmentAt builds the monitor view of soul. A soul inside an
// active maintenance window is reported as embalmed whatever its latest
// judgment says; the stored judgments are left untouched.
func (s *RESTServer) soulWithLatestJudgmentAt(soul *core.Soul, windows []*core.MaintenanceWindow, now time.Time) soulMonitorDTO {
	response := redactedSoulMonitorResponse(soul, "unknown", nil, 0)

	if soul == nil {
		return response
	}

	judgments, err := s.store.ListJudgmentsNoCtx(soul.ID, time.Time{}, now.Add(time.Hour), 1)
	if err == nil && len(judgments) > 0 && judgments[0] != nil {
		latest := judgments[0]
		response.Status = soulMonitorStatus(latest.Status)
		response.LastCheck = &latest.Timestamp
		response.Latency = latest.Duration.Milliseconds()
		response.Flapping = judgmentFlapping(latest)
	}
	if core.SoulInMaintenance(windows, soul, now) {
		response.Status = string(core.SoulEmbalmed)
	}
	return response
}

func (s *RESTServer) handleListSouls(ctx *Context) error {
	workspace := ctx.Workspace
	offset, limit := parsePagination(ctx.Request, 20, 100)

	souls, err := s.store.ListSoulsNoCtx(workspace, offset, limit+1)
	if err != nil {
		s.logger.Error("failed to list souls", "error", err, "workspace", workspace)
		return ctx.Error(http.StatusInternalServerError, "failed to retrieve souls")
	}

	// Check if there are more results
	hasMore := len(souls) > limit
	if hasMore {
		souls = souls[:limit]
	}
	nextOffset := offset + limit
	now := time.Now()
	windows := s.activeMaintenanceWindows(now)
	data := make([]soulMonitorDTO, 0, len(souls))
	for _, soul := range souls {
		data = append(data, s.soulWithLatestJudgmentAt(soul, windows, now))
	}

	response := PaginatedResponse{
		Data: data,
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
	var raw json.RawMessage
	if err := ctx.Bind(&raw); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid soul data")
	}
	var soul core.Soul
	if err := json.Unmarshal(raw, &soul); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid soul data")
	}
	// A new soul is enabled unless the request says otherwise; a missing
	// field used to create a soul that was never scheduled.
	var presence struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.Unmarshal(raw, &presence); err == nil && presence.Enabled == nil {
		soul.Enabled = true
	}

	soul.WorkspaceID = ctx.Workspace
	soul.ID = core.GenerateID()
	soul.CreatedAt = time.Now()
	soul.UpdatedAt = time.Now()
	if soul.Heartbeat != nil {
		// Ping tokens are always generated server-side.
		soul.Heartbeat.Token = ""
	}
	soul.ApplyDefaults()

	if err := soul.Validate(); err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}

	if err := s.store.SaveSoul(ctx.Request.Context(), &soul); err != nil {
		return s.internalError(ctx, err, "internal server error")
	}

	s.syncProbeSoul(&soul)

	return ctx.JSON(http.StatusCreated, s.soulWithLatestJudgment(&soul))
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

	return ctx.JSON(http.StatusOK, s.soulWithLatestJudgment(soul))
}

func (s *RESTServer) handleUpdateSoul(ctx *Context) error {
	id := ctx.Params["id"]
	var raw json.RawMessage
	if err := ctx.Bind(&raw); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid soul data")
	}
	var soul core.Soul
	if err := json.Unmarshal(raw, &soul); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid soul data")
	}
	var presence struct {
		Enabled *bool `json:"enabled"`
	}
	_ = json.Unmarshal(raw, &presence)

	// IDOR protection: Verify soul belongs to user's workspace before updating
	existing, err := s.store.GetSoulNoCtx(id)
	if err != nil || existing == nil {
		return ctx.Error(http.StatusNotFound, "soul not found")
	}
	// Like create, a body without "enabled" leaves it as it was instead of
	// silently disabling the soul.
	if presence.Enabled == nil {
		soul.Enabled = existing.Enabled
	}
	if existing.WorkspaceID != ctx.Workspace {
		return ctx.Error(http.StatusForbidden, "access denied: soul belongs to another workspace")
	}

	soul.ID = id
	soul.WorkspaceID = ctx.Workspace
	// Preserve server-managed fields to prevent mass assignment
	soul.CreatedAt = existing.CreatedAt
	soul.UpdatedAt = time.Now()
	mergeSoulSecrets(existing, &soul)
	soul.ApplyDefaults()

	if err := soul.Validate(); err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}

	if err := s.store.SaveSoul(ctx.Request.Context(), &soul); err != nil {
		if refused, werr := quotaExceeded(ctx, err); refused {
			return werr
		}
		s.logger.Error("failed to update soul", "error", err, "soul_id", id)
		if handled, werr := s.leaderUnavailable(ctx, err); handled {
			return werr
		}
		return ctx.Error(http.StatusInternalServerError, "failed to update soul")
	}

	s.syncProbeSoul(&soul)

	return ctx.JSON(http.StatusOK, s.soulWithLatestJudgment(&soul))
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
		if handled, werr := s.leaderUnavailable(ctx, err); handled {
			return werr
		}
		s.logger.Error("failed to delete soul", "error", err, "soul_id", id)
		return ctx.Error(http.StatusInternalServerError, "failed to delete soul")
	}

	if s.probe != nil {
		s.probe.RemoveSoul(id)
	}

	return ctx.JSON(http.StatusNoContent, nil)
}

func (s *RESTServer) syncProbeSoul(soul *core.Soul) {
	if s.probe == nil || soul == nil {
		return
	}
	if soul.Enabled {
		s.probe.UpsertSoul(soul)
		return
	}
	s.probe.RemoveSoul(soul.ID)
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

	if s.probe == nil {
		return ctx.Error(http.StatusServiceUnavailable, "probe engine unavailable")
	}
	if soul.Type == core.CheckHeartbeat {
		return ctx.Error(http.StatusConflict, "heartbeat souls are judged by their pings and deadline; there is nothing to check")
	}

	judgment, err := s.probe.ForceCheck(id)
	if err != nil {
		return s.internalError(ctx, err, "internal server error")
	}

	return ctx.JSON(http.StatusOK, redactedJudgmentFromCore(judgment))
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

	// Storage returns newest first. A cursor identifies the last item from the
	// previous page; the next page therefore contains strictly older items.
	cursor := ctx.Request.URL.Query().Get("cursor")
	offset, limit := parsePagination(ctx.Request, 20, 100)
	start := time.Now().Add(-24 * time.Hour)
	end := time.Now()
	fetchLimit := offset + limit + 1 // one look-ahead item determines has_more

	var cursorJudgment *core.Judgment
	if cursor != "" {
		offset = 0 // cursor pagination takes precedence over legacy offset
		cursorJudgment, err = s.store.GetJudgmentNoCtx(cursor)
		if err != nil || cursorJudgment == nil || cursorJudgment.SoulID != soulID ||
			(cursorJudgment.WorkspaceID != "" && cursorJudgment.WorkspaceID != ctx.Workspace) {
			return ctx.Error(http.StatusBadRequest, "invalid judgment cursor")
		}
		// Include the cursor itself, then remove it deterministically below. This
		// preserves same-timestamp judgments by using ID as the tie-breaker. The
		// storage API has no native cursor, so fetch the bounded 24-hour window;
		// truncating before the tie-break would skip same-timestamp entries.
		end = cursorJudgment.Timestamp.Add(time.Nanosecond)
		fetchLimit = 0
	}

	judgments, err := s.store.ListJudgmentsNoCtx(soulID, start, end, fetchLimit)
	if err != nil {
		return s.internalError(ctx, err, "internal server error")
	}
	nonNil := judgments[:0]
	for _, judgment := range judgments {
		if judgment != nil {
			nonNil = append(nonNil, judgment)
		}
	}
	judgments = nonNil
	sort.Slice(judgments, func(i, j int) bool {
		if judgments[i].Timestamp.Equal(judgments[j].Timestamp) {
			return judgments[i].ID > judgments[j].ID
		}
		return judgments[i].Timestamp.After(judgments[j].Timestamp)
	})

	if cursorJudgment != nil {
		filtered := make([]*core.Judgment, 0, len(judgments))
		for _, judgment := range judgments {
			if judgment == nil {
				continue
			}
			if judgment.Timestamp.Before(cursorJudgment.Timestamp) ||
				(judgment.Timestamp.Equal(cursorJudgment.Timestamp) && judgment.ID < cursor) {
				filtered = append(filtered, judgment)
			}
		}
		judgments = filtered
	}

	if offset > len(judgments) {
		offset = len(judgments)
	}
	judgments = judgments[offset:]
	hasMore := len(judgments) > limit
	if hasMore {
		judgments = judgments[:limit]
	}

	nextCursor := ""
	if hasMore && len(judgments) > 0 {
		nextCursor = judgments[len(judgments)-1].ID
	}
	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"data":        namedJudgmentsFromCore(judgments, map[string]string{soul.ID: soul.Name}),
		"total":       len(judgments), // page count; global count requires storage support
		"has_more":    hasMore,
		"next_cursor": nextCursor,
	})
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

	return ctx.JSON(http.StatusOK, redactedJudgmentFromCore(judgment))
}

func (s *RESTServer) handleListAllJudgments(ctx *Context) error {
	// Get all souls in the user's workspace
	souls, err := s.store.ListSoulsNoCtx(ctx.Workspace, 0, 1000)
	if err != nil {
		return s.internalError(ctx, err, "failed to list souls")
	}

	start := time.Now().Add(-24 * time.Hour) // Last 24 hours
	end := time.Now()

	// Collect judgments from each soul concurrently (limit to 100 per soul)
	type judgmentResult struct {
		judgments []*core.Judgment
	}
	results := make([]judgmentResult, len(souls))
	var wg sync.WaitGroup

	for i, soul := range souls {
		if soul == nil {
			continue
		}
		wg.Add(1)
		go func(idx int, soulID string) {
			defer wg.Done()
			judgments, err := s.store.ListJudgmentsNoCtx(soulID, start, end, 100)
			if err != nil {
				results[idx] = judgmentResult{}
				return
			}
			results[idx] = judgmentResult{judgments: judgments}
		}(i, soul.ID)
	}
	wg.Wait()

	// Aggregate results
	var allJudgments []*core.Judgment
	for _, result := range results {
		allJudgments = append(allJudgments, result.judgments...)
	}

	// Sort by timestamp descending
	sort.Slice(allJudgments, func(i, j int) bool {
		return allJudgments[i].Timestamp.After(allJudgments[j].Timestamp)
	})

	// Limit to 500 total judgments
	if len(allJudgments) > 500 {
		allJudgments = allJudgments[:500]
	}

	return ctx.JSON(http.StatusOK, namedJudgmentsFromCore(allJudgments, soulNames(souls)))
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

	redacted := make([]*redactedChannelDTO, 0, len(channels))
	for _, channel := range channels {
		redacted = append(redacted, redactedChannelDTOFromCore(channel))
	}

	response := PaginatedResponse{
		Data: redacted,
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
	enabledSet, err := ctx.bindCreate(&channel)
	if err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid channel data")
	}
	if !enabledSet {
		channel.Enabled = true
	}

	channel.ID = core.GenerateID()
	channel.WorkspaceID = ctx.Workspace
	channel.CreatedAt = time.Now()
	channel.UpdatedAt = time.Now()

	if err := s.alert.RegisterChannel(&channel); err != nil {
		if handled, werr := s.leaderUnavailable(ctx, err); handled {
			return werr
		}
		if refused, werr := quotaExceeded(ctx, err); refused {
			return werr
		}
		return ctx.Error(http.StatusBadRequest, err.Error())
	}

	return ctx.JSON(http.StatusCreated, redactedChannelDTOFromCore(&channel))
}

func (s *RESTServer) handleGetChannel(ctx *Context) error {
	id := ctx.Params["id"]
	// HIGH-09: Verify channel belongs to user's workspace
	if ch, err := s.alert.GetChannel(id); err == nil {
		if ch.WorkspaceID != "" && ch.WorkspaceID != ctx.Workspace {
			return ctx.Error(http.StatusForbidden, "access denied: channel belongs to another workspace")
		}
		return ctx.JSON(http.StatusOK, redactedChannelDTOFromCore(ch))
	}
	// Fallback to storage with user's workspace
	channel, err := s.store.GetChannelNoCtx(id, ctx.Workspace)
	if err != nil {
		return ctx.Error(http.StatusNotFound, "channel not found")
	}

	return ctx.JSON(http.StatusOK, redactedChannelDTOFromCore(channel))
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
	channel.CreatedAt = existing.CreatedAt
	channel.UpdatedAt = time.Now()
	if err := mergeChannelSecrets(existing, &channel); err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}

	if err := s.alert.RegisterChannel(&channel); err != nil {
		if handled, werr := s.leaderUnavailable(ctx, err); handled {
			return werr
		}
		if refused, werr := quotaExceeded(ctx, err); refused {
			return werr
		}
		return ctx.Error(http.StatusBadRequest, err.Error())
	}

	return ctx.JSON(http.StatusOK, redactedChannelDTOFromCore(&channel))
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
	if err := s.alert.TestChannel(ctx.Request.Context(), id, contextWorkspace(ctx)); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return ctx.Error(http.StatusNotFound, "channel not found")
		}
		if strings.Contains(err.Error(), "does not belong to workspace") {
			return ctx.Error(http.StatusForbidden, "access denied")
		}
		return s.internalError(ctx, err, "failed to send test notification")
	}
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
	enabledSet, err := ctx.bindCreate(&rule)
	if err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid rule data")
	}
	if !enabledSet {
		rule.Enabled = true
	}

	rule.ID = core.GenerateID()
	rule.WorkspaceID = ctx.Workspace
	rule.CreatedAt = time.Now()

	if err := rule.Validate(); err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}
	if err := rule.ValidateMetrics(); err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}

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
	rule.CreatedAt = existing.CreatedAt

	if err := rule.Validate(); err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}
	if err := rule.ValidateMetrics(); err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}

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

	workspace := contextWorkspace(ctx)
	filtered := make([]*core.Workspace, 0, len(workspaces))
	for _, ws := range workspaces {
		if ws == nil {
			continue
		}
		if sameWorkspace(ws.ID, workspace) || canAccessWorkspace(ctx, ws) || s.isWorkspaceMember(ctx, ws.ID) {
			filtered = append(filtered, ws)
		}
	}

	return ctx.JSON(http.StatusOK, filtered)
}

func (s *RESTServer) handleCreateWorkspace(ctx *Context) error {
	var ws core.Workspace
	if err := ctx.Bind(&ws); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid workspace data")
	}

	workspace := contextWorkspace(ctx)
	existing, err := s.store.GetWorkspaceNoCtx(workspace)
	if err == nil && existing != nil {
		return ctx.Error(http.StatusConflict, "workspace already exists")
	}
	if err != nil && !isNotFoundError(err) {
		return s.internalError(ctx, err, "failed to fetch workspace")
	}

	ws.Name = strings.TrimSpace(ws.Name)
	if ws.Name == "" {
		return ctx.Error(http.StatusBadRequest, "workspace name is required")
	}

	ws.Slug = strings.TrimSpace(strings.ToLower(ws.Slug))
	if ws.Slug == "" {
		ws.Slug = slugFromLabel(ws.Name)
	}
	if err := core.ValidateSlug(ws.Slug); err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}
	if core.IsReservedSlug(ws.Slug) {
		return ctx.Error(http.StatusBadRequest, "workspace slug is reserved")
	}

	now := time.Now()
	ws.ID = workspace
	ws.Description = strings.TrimSpace(ws.Description)
	ws.OwnerID = ""
	if ctx.User != nil {
		ws.OwnerID = ctx.User.ID
	}
	ws.Quotas = s.workspaceQuotaLimits(workspace)
	ws.Features = defaultWorkspaceFeatures()
	ws.Status = core.WorkspaceActive
	ws.CreatedAt = now
	ws.UpdatedAt = now
	ws.DeletedAt = nil

	if err := s.store.SaveWorkspaceNoCtx(&ws); err != nil {
		return s.internalError(ctx, err, "internal server error")
	}

	return ctx.JSON(http.StatusCreated, ws)
}

func (s *RESTServer) handleGetWorkspace(ctx *Context) error {
	id := ctx.Params["id"]
	ws, err := s.store.GetWorkspaceNoCtx(id)
	if err != nil {
		if !isNotFoundError(err) {
			return s.internalError(ctx, err, "failed to fetch workspace")
		}
		return ctx.Error(http.StatusNotFound, "workspace not found")
	}
	if !sameWorkspace(ws.ID, contextWorkspace(ctx)) {
		return ctx.Error(http.StatusForbidden, "access denied")
	}

	return ctx.JSON(http.StatusOK, ws)
}

func (s *RESTServer) handleUpdateWorkspace(ctx *Context) error {
	id := ctx.Params["id"]

	// Fetch existing workspace for IDOR check
	existing, err := s.store.GetWorkspaceNoCtx(id)
	if err != nil {
		if isNotFoundError(err) {
			return ctx.Error(http.StatusNotFound, "workspace not found")
		}
		return s.internalError(ctx, err, "failed to fetch workspace")
	}
	if existing == nil {
		return ctx.Error(http.StatusNotFound, "workspace not found")
	}
	if existing.ID != id || !sameWorkspace(existing.ID, contextWorkspace(ctx)) {
		return ctx.Error(http.StatusForbidden, "access denied")
	}

	// Bind only the mutable fields; protected fields from existing are preserved
	var ws core.Workspace
	if err := ctx.Bind(&ws); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid workspace data")
	}
	ws.Name = strings.TrimSpace(ws.Name)
	if ws.Name == "" {
		return ctx.Error(http.StatusBadRequest, "workspace name is required")
	}

	// Mass assignment protection: preserve all sensitive fields from existing.
	// Only Name, Description, and Settings can be updated by the client.
	ws.ID = id
	ws.Slug = existing.Slug         // immutable
	ws.OwnerID = existing.OwnerID   // immutable - prevents privilege escalation
	ws.Quotas = existing.Quotas     // immutable - prevent quota bypass
	ws.Features = existing.Features // immutable - prevent feature escalation
	ws.Status = existing.Status     // immutable - prevent status manipulation
	ws.CreatedAt = existing.CreatedAt
	ws.DeletedAt = existing.DeletedAt // server-managed
	ws.UpdatedAt = time.Now()

	if err := s.store.SaveWorkspaceNoCtx(&ws); err != nil {
		return s.internalError(ctx, err, "internal server error")
	}

	return ctx.JSON(http.StatusOK, ws)
}

func (s *RESTServer) handleDeleteWorkspace(ctx *Context) error {
	id := ctx.Params["id"]
	existing, err := s.store.GetWorkspaceNoCtx(id)
	if err != nil {
		if isNotFoundError(err) {
			return ctx.Error(http.StatusNotFound, "workspace not found")
		}
		return s.internalError(ctx, err, "failed to fetch workspace")
	}
	if existing == nil {
		return ctx.Error(http.StatusNotFound, "workspace not found")
	}
	if !sameWorkspace(existing.ID, contextWorkspace(ctx)) {
		return ctx.Error(http.StatusForbidden, "access denied")
	}
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
	workspace := contextWorkspace(ctx)
	start := time.Now().Add(-24 * time.Hour)
	end := time.Now()

	souls, err := s.store.ListSoulsNoCtx(workspace, 0, 1000)
	if err != nil {
		return s.internalError(ctx, err, "failed to list souls for stats")
	}

	healthy := 0
	degraded := 0
	dead := 0
	unknown := 0
	embalmed := 0
	today := 0
	failures := 0
	var totalLatency time.Duration
	windows := s.activeMaintenanceWindows(end)

	for _, soul := range souls {
		if soul == nil {
			continue
		}
		judgments, err := s.store.ListJudgmentsNoCtx(soul.ID, start, end, 1000)
		if err != nil {
			judgments = nil
		}
		// A soul with no judgment in the window has not been checked (or not
		// recently): that is unknown, not dead. Maintenance overrides both.
		switch {
		case core.SoulInMaintenance(windows, soul, end):
			embalmed++
		case len(judgments) == 0 || judgments[0] == nil:
			unknown++
		case judgments[0].Status == core.SoulAlive:
			healthy++
		case judgments[0].Status == core.SoulDegraded:
			degraded++
		case judgments[0].Status == core.SoulDead:
			dead++
		default:
			unknown++
		}
		for _, judgment := range judgments {
			if judgment == nil {
				continue
			}
			today++
			totalLatency += judgment.Duration
			if judgment.Status != core.SoulAlive {
				failures++
			}
		}
	}

	avgLatencyMs := 0.0
	if today > 0 {
		avgLatencyMs = float64(totalLatency) / float64(today) / float64(time.Millisecond)
	}

	overview := map[string]interface{}{
		"souls": map[string]int{
			"total":    len(souls),
			"healthy":  healthy,
			"degraded": degraded,
			"dead":     dead,
			"unknown":  unknown,
			"embalmed": embalmed,
		},
		"judgments": map[string]interface{}{
			"today":          today,
			"failures":       failures,
			"avg_latency_ms": avgLatencyMs,
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

// ClusterPeer is one entry of GET /api/v1/cluster/peers. LastContact is an
// RFC3339 timestamp, or "" when this node has never heard from the peer.
type ClusterPeer struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Address     string `json:"address"`
	State       string `json:"state"`
	LastContact string `json:"last_contact"`
	IsLeader    bool   `json:"is_leader"`
	Region      string `json:"region"`
}

// clusterPeerLister is implemented by cluster managers that can enumerate
// members. It is optional so ClusterManager stays small for its other users.
type clusterPeerLister interface {
	ClusterPeers() []ClusterPeer
}

// handleClusterPeers always returns a JSON array of ClusterPeer, this node
// first. A standalone node reports itself as the leader.
func (s *RESTServer) handleClusterPeers(ctx *Context) error {
	if lister, ok := s.cluster.(clusterPeerLister); ok {
		if peers := lister.ClusterPeers(); len(peers) > 0 {
			return ctx.JSON(http.StatusOK, peers)
		}
	}

	self := ClusterPeer{
		ID:          "standalone",
		State:       "leader",
		LastContact: time.Now().UTC().Format(time.RFC3339),
		IsLeader:    true,
	}
	if s.cluster != nil && s.cluster.IsClustered() {
		if status := s.cluster.GetStatus(); status != nil {
			if status.NodeID != "" {
				self.ID = status.NodeID
			}
			self.State = status.State
		}
		self.IsLeader = s.cluster.IsLeader()
	}
	self.Name = self.ID
	return ctx.JSON(http.StatusOK, []ClusterPeer{self})
}

// ClusterMembership is implemented by cluster managers that can change Raft
// membership at runtime. AddNode and RemoveNode may be called on any node; a
// follower forwards the change to the leader.
type ClusterMembership interface {
	AddNode(id, address, region string) error
	RemoveNode(id string) error
}

// ClusterAddressMembership is implemented by a ClusterMembership that can add
// a node known only by its Raft address, learning its node ID from it.
type ClusterAddressMembership interface {
	AddNodeByAddress(address, region string) (nodeID, learnedRegion string, err error)
}

// Membership errors a ClusterMembership reports, mapped onto HTTP statuses.
var (
	ErrClusterDisabled   = errors.New("clustering is not enabled on this node")
	ErrClusterNodeExists = errors.New("node is already a cluster member")
	ErrClusterNodeAbsent = errors.New("node is not a cluster member")
	ErrClusterIsLeader   = errors.New("the current leader cannot remove itself")
	// ErrClusterNodeUnreachable: the node to add by address did not answer.
	ErrClusterNodeUnreachable = errors.New("the node could not be reached to learn its node ID")
)

// clusterNodeRequest is the body of POST /api/v1/cluster/nodes.
type clusterNodeRequest struct {
	NodeID  string `json:"node_id"`
	Address string `json:"address"`
	Region  string `json:"region"`
}

func (s *RESTServer) clusterMembership() ClusterMembership {
	if s.cluster == nil || !s.cluster.IsClustered() {
		return nil
	}
	membership, _ := s.cluster.(ClusterMembership)
	return membership
}

// membershipError maps a membership change failure onto a response.
func (s *RESTServer) membershipError(ctx *Context, err error) error {
	switch {
	case errors.Is(err, ErrClusterDisabled):
		return ctx.Error(http.StatusConflict, "clustering is not enabled on this node")
	case errors.Is(err, ErrClusterNodeExists):
		return ctx.Error(http.StatusConflict, "node is already a cluster member")
	case errors.Is(err, ErrClusterNodeAbsent):
		return ctx.Error(http.StatusNotFound, "node is not a cluster member")
	case errors.Is(err, ErrClusterIsLeader):
		return ctx.Error(http.StatusConflict, "the current leader cannot remove itself; retry after leadership moves")
	case errors.Is(err, ErrClusterNodeUnreachable):
		return ctx.Error(http.StatusBadGateway, err.Error())
	}
	if handled, werr := s.leaderUnavailable(ctx, err); handled {
		return werr
	}
	s.logger.Error("cluster membership change failed", "error", err, "path", ctx.Request.URL.Path)
	return ctx.Error(http.StatusInternalServerError, "cluster membership change failed")
}

// handleAddClusterNode adds a voting member through a replicated Raft
// configuration change (one change at a time).
func (s *RESTServer) handleAddClusterNode(ctx *Context) error {
	membership := s.clusterMembership()
	if membership == nil {
		return ctx.Error(http.StatusConflict, "clustering is not enabled on this node")
	}
	var req clusterNodeRequest
	if err := ctx.Bind(&req); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid JSON")
	}
	req.NodeID = strings.TrimSpace(req.NodeID)
	req.Address = strings.TrimSpace(req.Address)
	req.Region = strings.TrimSpace(req.Region)
	host, port, err := net.SplitHostPort(req.Address)
	if err != nil || host == "" || port == "" {
		return ctx.Error(http.StatusBadRequest, "address must be host:port")
	}
	if req.NodeID == "" {
		// Add by address: the node itself reports its ID.
		byAddress, ok := membership.(ClusterAddressMembership)
		if !ok {
			return ctx.Error(http.StatusBadRequest, "node_id is required")
		}
		nodeID, region, err := byAddress.AddNodeByAddress(req.Address, req.Region)
		if err != nil {
			return s.membershipError(ctx, err)
		}
		req.NodeID, req.Region = nodeID, region
	} else {
		if len(req.NodeID) > 128 || strings.ContainsAny(req.NodeID, "/ \t\n") {
			return ctx.Error(http.StatusBadRequest, "node_id must not contain '/' or whitespace")
		}
		if err := membership.AddNode(req.NodeID, req.Address, req.Region); err != nil {
			return s.membershipError(ctx, err)
		}
	}
	return ctx.JSON(http.StatusCreated, map[string]string{
		"node_id": req.NodeID,
		"address": req.Address,
		"region":  req.Region,
	})
}

// handleRemoveClusterNode removes a member through a replicated Raft
// configuration change.
func (s *RESTServer) handleRemoveClusterNode(ctx *Context) error {
	membership := s.clusterMembership()
	if membership == nil {
		return ctx.Error(http.StatusConflict, "clustering is not enabled on this node")
	}
	id := ctx.Params["id"]
	if err := membership.RemoveNode(id); err != nil {
		return s.membershipError(ctx, err)
	}
	return ctx.JSON(http.StatusNoContent, nil)
}

// Incident handlers

func (s *RESTServer) handleListIncidents(ctx *Context) error {
	// Default to listing every incident so the UI can show recovery history
	// alongside currently-open incidents. `?status=active` keeps the
	// previous behaviour for clients that only want open/ack'd incidents.
	statusFilter := ctx.Request.URL.Query().Get("status")
	var all []*core.Incident
	switch statusFilter {
	case "active", "open", "acknowledged":
		all = s.alert.ListActiveIncidents()
	default:
		all = s.alert.ListIncidents()
	}
	incidents := make([]*core.Incident, 0, len(all))
	for _, incident := range all {
		if incident == nil {
			continue
		}
		if statusFilter != "" && statusFilter != "all" && statusFilter != "active" && string(incident.Status) != statusFilter {
			continue
		}
		if incident.WorkspaceID == "" || incident.WorkspaceID == ctx.Workspace {
			incidents = append(incidents, incident)
		}
	}
	return ctx.JSON(http.StatusOK, incidents)
}

func (s *RESTServer) handleAcknowledgeIncident(ctx *Context) error {
	id := ctx.Params["id"]
	userID := ctx.User.ID

	if err := s.alert.AcknowledgeIncident(id, userID, ctx.Workspace); err != nil {
		if isNotFoundError(err) {
			return ctx.Error(http.StatusNotFound, "incident not found")
		}
		return s.internalError(ctx, err, "internal server error")
	}

	return ctx.JSON(http.StatusOK, map[string]string{"status": "acknowledged"})
}

func (s *RESTServer) handleResolveIncident(ctx *Context) error {
	id := ctx.Params["id"]
	userID := ctx.User.ID

	if err := s.alert.ResolveIncident(id, userID, ctx.Workspace); err != nil {
		if isNotFoundError(err) {
			return ctx.Error(http.StatusNotFound, "incident not found")
		}
		return s.internalError(ctx, err, "internal server error")
	}

	return ctx.JSON(http.StatusOK, map[string]string{"status": "resolved"})
}

// Config handlers live in settings.go.

// Status Page handlers

func contextWorkspace(ctx *Context) string {
	if ctx.Workspace == "" {
		return "default"
	}
	return ctx.Workspace
}

func requestAuthToken(r *http.Request) string {
	token := r.Header.Get("Authorization")
	token = strings.TrimPrefix(token, "Bearer ")
	if token == "" {
		if cookie, err := r.Cookie("auth_token"); err == nil {
			token = cookie.Value
		}
	}
	return token
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	var notFound *core.NotFoundError
	if errors.As(err, &notFound) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}

func canAccessWorkspace(ctx *Context, ws *core.Workspace) bool {
	if ws == nil {
		return false
	}
	if sameWorkspace(ws.ID, contextWorkspace(ctx)) {
		return true
	}
	return ctx.User != nil && ws.OwnerID != "" && ws.OwnerID == ctx.User.ID
}

func sameWorkspace(resourceWorkspace, requestWorkspace string) bool {
	if resourceWorkspace == "" {
		resourceWorkspace = "default"
	}
	if requestWorkspace == "" {
		requestWorkspace = "default"
	}
	return resourceWorkspace == requestWorkspace
}

func slugFromLabel(label string) string {
	label = strings.ToLower(strings.TrimSpace(label))
	var b strings.Builder
	lastHyphen := false
	for _, r := range label {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if valid {
			b.WriteRune(r)
			lastHyphen = false
			continue
		}
		if !lastHyphen && b.Len() > 0 {
			b.WriteByte('-')
			lastHyphen = true
		}
	}

	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "workspace"
	}
	if len(slug) > 63 {
		slug = strings.Trim(slug[:63], "-")
	}
	if len(slug) < 3 {
		slug += "-ws"
	}
	return slug
}

func defaultWorkspaceFeatures() core.FeatureFlags {
	return core.FeatureFlags{
		StatusPage:     true,
		MCP:            true,
		AdvancedAlerts: true,
	}
}

// quotaUsageReporter is implemented by the server's storage adapter, which
// also enforces the quotas, so the reported limits are the enforced ones.
type quotaUsageReporter interface {
	QuotaUsage(workspace string) (*core.WorkspaceQuotaUsage, error)
}

// quotaUsage reports the workspace's usage against its quotas. A store that
// does not enforce quotas reports nothing as limited.
func (s *RESTServer) quotaUsage(workspace string) (*core.WorkspaceQuotaUsage, error) {
	if reporter, ok := s.store.(quotaUsageReporter); ok {
		return reporter.QuotaUsage(workspace)
	}
	return &core.WorkspaceQuotaUsage{Workspace: workspace}, nil
}

// workspaceQuotaLimits is the quota record stamped on a new workspace: the
// limits currently enforced (all 0, unlimited, when tenants are disabled).
func (s *RESTServer) workspaceQuotaLimits(workspace string) core.QuotaConfig {
	usage, err := s.quotaUsage(workspace)
	if err != nil || usage == nil {
		return core.QuotaConfig{}
	}
	return core.QuotaConfig{
		MaxSouls:         usage.Souls.Limit,
		MaxJourneys:      usage.Journeys.Limit,
		MaxAlertChannels: usage.AlertChannels.Limit,
		MaxTeamMembers:   usage.TeamMembers.Limit,
		RetentionDays:    usage.RetentionDays,
		CheckIntervalMin: usage.CheckIntervalMin,
	}
}

// handleGetQuota reports the caller's workspace usage against its quotas.
func (s *RESTServer) handleGetQuota(ctx *Context) error {
	usage, err := s.quotaUsage(contextWorkspace(ctx))
	if err != nil {
		return s.internalError(ctx, err, "failed to compute quota usage")
	}
	if dir := s.userDirectory(); dir != nil {
		if n, err := dir.CountMembers(contextWorkspace(ctx)); err == nil {
			usage.TeamMembers.Used = n
		}
	}
	return ctx.JSON(http.StatusOK, usage)
}

// statusPageRequest carries an optional plaintext password alongside the page. The
// stored hash is never accepted from a client: it is derived from this field or
// preserved from the stored page.
type statusPageRequest struct {
	core.StatusPage
	Password string `json:"password"`
}

// jsonHasTopLevelKey reports whether the JSON object in raw has key as a
// top-level field, matched case-insensitively the way encoding/json binds
// fields. A substring search over the body cannot answer this: it matched any
// value equal to the key (a description of "password"), and missed keys the
// decoder still binds ("Password", escaped spellings), so a request could
// clear a stored password it never mentioned, or keep one it replaced.
func jsonHasTopLevelKey(raw []byte, key string) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return false
	}
	for k := range fields {
		if strings.EqualFold(k, key) {
			return true
		}
	}
	return false
}

// statusPageKeepOmittedFields restores the stored value of every optional field
// the request body did not mention.
//
// A PUT body is a partial update: the StatusPageCreate schema in openapi.yaml
// requires only "name" and "slug", and documents the same "omit to keep the
// stored password" rule for "password" that "enabled" and "visibility" already
// follow. Binding the body into a fresh core.StatusPage cannot tell an omitted
// field from an explicit zero value, so every omitted field was overwritten
// with its zero value: the description, logo, favicon, custom domain and
// uptime window were cleared, the theme was reset to the default, and the
// soul list, groups and incidents were dropped.
//
// A field the body does mention stays authoritative, so an explicit "" or []
// still clears it, exactly as an empty password clears the password.
func statusPageKeepOmittedFields(raw []byte, page *core.StatusPage, existing *core.StatusPage) {
	if !jsonHasTopLevelKey(raw, "description") {
		page.Description = existing.Description
	}
	if !jsonHasTopLevelKey(raw, "logo_url") {
		page.LogoURL = existing.LogoURL
	}
	if !jsonHasTopLevelKey(raw, "favicon_url") {
		page.FaviconURL = existing.FaviconURL
	}
	if !jsonHasTopLevelKey(raw, "custom_domain") {
		page.CustomDomain = existing.CustomDomain
	}
	if !jsonHasTopLevelKey(raw, "theme") {
		page.Theme = existing.Theme
	}
	if !jsonHasTopLevelKey(raw, "uptime_days") {
		page.UptimeDays = existing.UptimeDays
	}
	if !jsonHasTopLevelKey(raw, "souls") {
		page.Souls = existing.Souls
	}
	if !jsonHasTopLevelKey(raw, "groups") {
		page.Groups = existing.Groups
	}
	// Incidents and announcements are read-only here: they are written only
	// through /status-pages/:id/incidents and /announcements, which validate
	// them and keep the page within its size budget. A GET-then-PUT client
	// echoes a stale copy, so the stored value always wins.
	page.Incidents = existing.Incidents
	page.Announcements = existing.Announcements
}

// applyStatusPagePassword sets, clears or preserves the page password. A body that
// carries "password" is authoritative: an empty value clears the password, a
// non-empty one replaces it, and an absent field keeps previousHash. Preserving is
// the only sane semantics for an update, because a client can never read the hash
// back and therefore cannot echo it.
func applyStatusPagePassword(page *core.StatusPage, raw []byte, password, previousHash string) error {
	// PasswordSet is derived for responses only; never persist a client's claim.
	page.PasswordSet = false

	if !jsonHasTopLevelKey(raw, "password") {
		page.PasswordHash = previousHash
		return nil
	}
	if strings.TrimSpace(password) == "" {
		page.PasswordHash = ""
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to store the page password: %w", err)
	}
	page.PasswordHash = string(hash)
	return nil
}

// redactedStatusPage returns a copy safe to serialise: the password hash never
// leaves the server, and PasswordSet tells the caller whether one is configured.
// The stored page is not modified.
func redactedStatusPage(page *core.StatusPage) *core.StatusPage {
	if page == nil {
		return nil
	}
	copied := *page
	copied.PasswordSet = page.PasswordHash != ""
	copied.PasswordHash = ""
	return &copied
}

// statusPageSubscriberCounter is optionally implemented by Storage: it returns
// the number of confirmed email subscribers per status page ID.
type statusPageSubscriberCounter interface {
	ConfirmedSubscriberCounts() (map[string]int, error)
}

// statusPageResponse is a redacted status page plus its confirmed subscriber
// count. The count is derived on read and never stored with the page; no
// subscriber address is exposed.
type statusPageResponse struct {
	*core.StatusPage
	SubscriberCount int `json:"subscriber_count"`
}

// subscriberCounts returns the confirmed subscriber count per page ID, or nil
// when the store cannot count (the count then reads 0). Subscriptions are
// keyed by page ID and callers only pass pages of the caller's workspace, so
// the counts are workspace-scoped through the page.
func (s *RESTServer) subscriberCounts() map[string]int {
	counter, ok := s.store.(statusPageSubscriberCounter)
	if !ok {
		return nil
	}
	counts, err := counter.ConfirmedSubscriberCounts()
	if err != nil {
		s.logger.Warn("failed to count status page subscribers", "err", err)
		return nil
	}
	return counts
}

func (s *RESTServer) statusPageResponse(page *core.StatusPage, counts map[string]int) statusPageResponse {
	return statusPageResponse{StatusPage: redactedStatusPage(page), SubscriberCount: counts[page.ID]}
}

func normalizeStatusPageForSave(page *core.StatusPage) error {
	page.Name = strings.TrimSpace(page.Name)
	page.Slug = strings.TrimSpace(strings.ToLower(page.Slug))
	page.Description = strings.TrimSpace(page.Description)
	page.CustomDomain = strings.TrimSpace(strings.ToLower(page.CustomDomain))

	if page.Name == "" {
		return fmt.Errorf("status page name is required")
	}
	if page.Slug == "" {
		return fmt.Errorf("status page slug is required")
	}
	if len(page.Slug) > 100 || !isValidStatusPageSlug(page.Slug) {
		return fmt.Errorf("status page slug must contain only lowercase letters, numbers, and hyphens")
	}
	if page.Slug == "subscribe" || page.Slug == "subscriptions" {
		// /status/subscribe and /status/subscriptions/... are the email
		// subscription endpoints.
		return fmt.Errorf("status page slug %q is reserved", page.Slug)
	}
	if page.Visibility == "" {
		page.Visibility = core.VisibilityPublic
	}
	if page.Visibility == core.VisibilityProtected && page.PasswordHash == "" {
		return fmt.Errorf("a password-protected status page requires a password")
	}
	if page.UptimeDays <= 0 {
		page.UptimeDays = 90
	}
	if page.Theme.PrimaryColor == "" &&
		page.Theme.BackgroundColor == "" &&
		page.Theme.TextColor == "" &&
		page.Theme.AccentColor == "" &&
		page.Theme.FontFamily == "" {
		page.Theme = core.GetDefaultTheme()
	}
	return nil
}

// validateStatusPageSouls checks that every soul ID referenced in the page
// belongs to the page's workspace. Cross-tenant soul disclosure is prevented
// both at write time (here) and at read time (buildStatusPageData in the
// statuspage handler), so an attacker cannot publish another tenant's soul
// status on their own public status page.
func validateStatusPageSouls(page *core.StatusPage, store Storage) error {
	// Collect all unique soul IDs from the page and its groups.
	seen := make(map[string]bool)
	for _, id := range page.Souls {
		if id != "" && !seen[id] {
			seen[id] = true
		}
	}
	for _, g := range page.Groups {
		for _, id := range g.SoulIDs {
			if id != "" && !seen[id] {
				seen[id] = true
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	for id := range seen {
		soul, err := store.GetSoulNoCtx(id)
		if err != nil {
			return fmt.Errorf("soul %s: %w", id, err)
		}
		if !sameWorkspace(soul.WorkspaceID, page.WorkspaceID) {
			return fmt.Errorf("soul %s does not belong to workspace %s", id, page.WorkspaceID)
		}
	}
	return nil
}

func isValidStatusPageSlug(slug string) bool {
	if strings.HasPrefix(slug, "-") || strings.HasSuffix(slug, "-") {
		return false
	}
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func (s *RESTServer) handleListStatusPages(ctx *Context) error {
	pages, err := s.store.ListStatusPagesNoCtx()
	if err != nil {
		return s.internalError(ctx, err, "internal server error")
	}
	workspace := contextWorkspace(ctx)
	filtered := make([]*core.StatusPage, 0, len(pages))
	for _, page := range pages {
		if sameWorkspace(page.WorkspaceID, workspace) {
			filtered = append(filtered, page)
		}
	}
	counts := map[string]int(nil)
	if len(filtered) > 0 {
		counts = s.subscriberCounts()
	}
	out := make([]statusPageResponse, 0, len(filtered))
	for _, page := range filtered {
		out = append(out, s.statusPageResponse(page, counts))
	}
	return ctx.JSON(http.StatusOK, out)
}

// statusPageSlugInUse reports whether slug is already claimed by another
// published status page.
//
// The public /status/{slug} URL carries no workspace and Storage resolves a
// slug on its own, so the slug namespace is global: two published pages
// sharing one slug make that URL ambiguous, and the lookup answers with
// whichever page it reaches first. A password-protected page's URL can then
// serve another page's content without asking for the password.
//
// Only published pages occupy the URL -- a disabled page is not reachable
// there -- so a page that will not be published cannot shadow one that is.
// A page that will be published may not take a slug another published page
// already holds; a page that is being updated is excluded, because it
// re-sends its own slug.
func statusPageSlugInUse(pages []*core.StatusPage, slug, ownID string, published bool) bool {
	if !published {
		return false
	}
	for _, page := range pages {
		if page == nil || page.ID == ownID {
			continue
		}
		if page.Enabled && page.Slug == slug {
			return true
		}
	}
	return false
}

func (s *RESTServer) handleCreateStatusPage(ctx *Context) error {
	// On create, default Enabled=true unless the caller explicitly opts
	// out. Storage's GetStatusPageBySlug filters out !Enabled pages,
	// so a status page created without enabled silently 404s on the
	// public /status/{slug} URL — a confusing UX. We can't disambiguate
	// "field omitted" from "explicit false" with a bool, so use a peek
	// at the raw JSON body before bind.
	raw, _ := io.ReadAll(ctx.Request.Body)
	ctx.Request.Body = io.NopCloser(bytes.NewReader(raw))
	enabledExplicit := jsonHasTopLevelKey(raw, "enabled")

	var req statusPageRequest
	if err := ctx.Bind(&req); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid status page data")
	}
	page := req.StatusPage
	if !enabledExplicit {
		page.Enabled = true
	}
	// A new page starts without incidents or announcements; they are posted
	// through their own validated endpoints (statuspage_incidents.go).
	page.Incidents = nil
	page.Announcements = nil
	if err := applyStatusPagePassword(&page, raw, req.Password, ""); err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}
	if err := normalizeStatusPageForSave(&page); err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}

	// A slug names one public URL, so it may name one status page. The
	// namespace is global because the public /status/{slug} URL carries no
	// workspace and Storage resolves a slug on its own.
	pages, err := s.store.ListStatusPagesNoCtx()
	if err != nil {
		return s.internalError(ctx, err, "internal server error")
	}
	if statusPageSlugInUse(pages, page.Slug, "", page.Enabled) {
		return ctx.Error(http.StatusConflict, "status page slug already in use")
	}

	page.ID = core.GenerateID()
	page.WorkspaceID = contextWorkspace(ctx)
	page.CreatedAt = time.Now()
	page.UpdatedAt = time.Now()

	if err := validateStatusPageSouls(&page, s.store); err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}

	if err := s.store.SaveStatusPageNoCtx(&page); err != nil {
		return s.internalError(ctx, err, "internal server error")
	}

	// A new page has no subscribers yet.
	return ctx.JSON(http.StatusCreated, s.statusPageResponse(&page, nil))
}

func (s *RESTServer) handleGetStatusPage(ctx *Context) error {
	id := ctx.Params["id"]
	page, err := s.store.GetStatusPageNoCtx(id)
	if err != nil {
		return ctx.Error(http.StatusNotFound, "status page not found")
	}
	if !sameWorkspace(page.WorkspaceID, contextWorkspace(ctx)) {
		return ctx.Error(http.StatusForbidden, "access denied")
	}
	return ctx.JSON(http.StatusOK, s.statusPageResponse(page, s.subscriberCounts()))
}

func (s *RESTServer) handleUpdateStatusPage(ctx *Context) error {
	id := ctx.Params["id"]
	// Serialise with the incident and announcement endpoints, which
	// read-modify-write the same stored page.
	unlock := lockStatusPage(id)
	defer unlock()

	raw, _ := io.ReadAll(ctx.Request.Body)
	ctx.Request.Body = io.NopCloser(bytes.NewReader(raw))
	enabledExplicit := jsonHasTopLevelKey(raw, "enabled")
	visibilityExplicit := jsonHasTopLevelKey(raw, "visibility")

	var req statusPageRequest
	if err := ctx.Bind(&req); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid status page data")
	}
	page := req.StatusPage

	existing, err := s.store.GetStatusPageNoCtx(id)
	if err != nil {
		return ctx.Error(http.StatusNotFound, "status page not found")
	}
	if !sameWorkspace(existing.WorkspaceID, contextWorkspace(ctx)) {
		return ctx.Error(http.StatusForbidden, "access denied")
	}
	// An update that omits "enabled" must keep the stored value: binding the zero
	// value would disable the page, and Storage's GetStatusPageBySlug filters out
	// !Enabled pages, so the public /status/{slug} URL would silently start 404ing.
	// A bool cannot tell "field omitted" from "explicit false", hence the peek --
	// the same rule the create handler applies.
	if !enabledExplicit {
		page.Enabled = existing.Enabled
	}
	// The same rule applies to "visibility": binding the zero value would let
	// normalizeStatusPageForSave default the page to public, so an update that
	// omits the field would silently publish a protected page's contents at
	// the public /status/{slug} URL without its password.
	if !visibilityExplicit {
		page.Visibility = existing.Visibility
	}
	// The same rule applies to the remaining optional fields: a PUT body is a
	// partial update, so a field it does not mention must keep its stored
	// value instead of being overwritten with the zero value binding produced.
	statusPageKeepOmittedFields(raw, &page, existing)
	if err := applyStatusPagePassword(&page, raw, req.Password, existing.PasswordHash); err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}
	if err := normalizeStatusPageForSave(&page); err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}

	// A slug names one public URL, so it may name one status page. The
	// namespace is global because the public /status/{slug} URL carries no
	// workspace and Storage resolves a slug on its own. This page is excluded
	// by ID: an update re-sends its own slug, and that is not a collision.
	pages, err := s.store.ListStatusPagesNoCtx()
	if err != nil {
		return s.internalError(ctx, err, "internal server error")
	}
	if statusPageSlugInUse(pages, page.Slug, id, page.Enabled) {
		return ctx.Error(http.StatusConflict, "status page slug already in use")
	}

	page.ID = id
	page.WorkspaceID = contextWorkspace(ctx)
	page.CreatedAt = existing.CreatedAt
	page.UpdatedAt = time.Now()

	if err := validateStatusPageSouls(&page, s.store); err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}

	if err := s.store.SaveStatusPageNoCtx(&page); err != nil {
		return s.internalError(ctx, err, "internal server error")
	}

	return ctx.JSON(http.StatusOK, s.statusPageResponse(&page, s.subscriberCounts()))
}

func (s *RESTServer) handleDeleteStatusPage(ctx *Context) error {
	id := ctx.Params["id"]
	page, err := s.store.GetStatusPageNoCtx(id)
	if err != nil {
		return ctx.Error(http.StatusNotFound, "status page not found")
	}
	if !sameWorkspace(page.WorkspaceID, contextWorkspace(ctx)) {
		return ctx.Error(http.StatusForbidden, "access denied")
	}
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

	// Propagate both the workspace and the caller's role so MCP tool handlers
	// can enforce RBAC — the MCP endpoint mixes read tools with mutating ones
	// (create_soul, acknowledge_incident), and without the role a read-only
	// user could invoke the mutating tools and bypass the REST RBAC model.
	reqCtx := core.ContextWithWorkspaceID(ctx.Request.Context(), contextWorkspace(ctx))
	reqCtx = core.ContextWithUserRole(reqCtx, ctx.User.Role)
	req := ctx.Request.WithContext(reqCtx)
	s.mcp.ServeHTTP(ctx.Response, req)
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

	// The stream authenticated once, in requireAuth. End it when its session
	// is revoked (logout, password change) or stops authenticating.
	streamCtx, endStream := context.WithCancel(ctx.Request.Context())
	defer endStream()
	if token := requestAuthToken(ctx.Request); s.authConfig.IsEnabled() && token != "" && ctx.User != nil {
		unregister := s.sessions.add(token, ctx.User.ID, endStream)
		defer unregister()
		watchSession(streamCtx, s.tokenAuth(), token, s.liveSessionRevalidateInterval(), endStream)
	}

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
		case <-streamCtx.Done():
			return nil
		}
	}
}

// liveSessionRevalidateInterval is how often an SSE stream re-checks its
// session token.
func (s *RESTServer) liveSessionRevalidateInterval() time.Duration {
	if s.sessionRevalidateInterval > 0 {
		return s.sessionRevalidateInterval
	}
	return defaultSessionRevalidateInterval
}

// revokeInvalidLiveSessions closes every live WebSocket/SSE stream whose
// session token no longer authenticates. Password changes and resets end
// sessions in bulk, so the affected tokens are found by re-checking them.
func (s *RESTServer) revokeInvalidLiveSessions() {
	if s.auth == nil {
		return
	}
	s.sessions.revoke(func(st liveStream) bool {
		_, err := s.tokenAuth().Authenticate(st.token)
		return err != nil
	})
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

		// SECURITY: Support both Authorization header and httpOnly cookie (VULN-004 fix)
		// Check Authorization header first, then fall back to cookie, then query param.
		token := ctx.Request.Header.Get("Authorization")
		token = strings.TrimPrefix(token, "Bearer ")

		// If no Authorization header, check for httpOnly cookie
		if token == "" {
			if cookie, err := ctx.Request.Cookie("auth_token"); err == nil {
				token = cookie.Value
			}
		}

		// NOTE: Bearer tokens must never be accepted via query parameters.
		// Query strings leak through browser history, Referer headers,
		// reverse-proxy access logs, and shared links. EventSource (which
		// cannot set custom headers) is instead authenticated via the
		// httpOnly "auth_token" cookie checked above.

		if token == "" {
			return ctx.Error(http.StatusUnauthorized, "missing authorization token")
		}

		user, err := s.tokenAuth().Authenticate(token)
		if err != nil {
			return ctx.Error(http.StatusUnauthorized, "invalid token")
		}

		ctx.User = user
		ctx.Workspace = user.Workspace
		// Per-user quotas need the identity, which rateLimitMiddleware
		// (running before this) does not have yet.
		if s.userRateLimited(ctx) {
			return ctx.Error(http.StatusTooManyRequests, "Rate limit exceeded for this account")
		}
		if user.MustChangePassword && !passwordChangeAllowed(ctx.Request) {
			return ctx.JSON(http.StatusForbidden, map[string]string{
				"error": "password change required: set a new password with PUT /api/v1/auth/change-password",
				"code":  "password_change_required",
			})
		}
		return handler(ctx)
	}
}

// passwordChangeAllowed lists what an account that must change its initial
// password may still do: read who it is and change the password.
func passwordChangeAllowed(r *http.Request) bool {
	switch r.Method + " " + r.URL.Path {
	case "GET /api/v1/auth/me", "GET /api/v1/users/me", "PUT /api/v1/auth/change-password", "GET /api/v1/auth/methods":
		return true
	}
	return false
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
		// Strip sensitive query params before logging/tracing; heartbeat
		// ping URLs carry their secret in the path.
		logReq := redactHeartbeatRequest(ctx.Request)
		sanitizedURL := sanitizeURL(logReq.URL.String())

		// Extract trace context from incoming headers
		propagator := otel.GetTextMapPropagator()
		parentCtx := propagator.Extract(ctx.Request.Context(), propagation.HeaderCarrier(ctx.Request.Header))

		// Start span for this request
		spanName := fmt.Sprintf("%s %s", ctx.Request.Method, logReq.URL.Path)
		ctxSpan, span := otel.Tracer("anubiswatch/http").Start(parentCtx, spanName,
			trace.WithAttributes(
				attribute.String("http.method", ctx.Request.Method),
				attribute.String("http.url", sanitizedURL),
				attribute.String("http.route", logReq.URL.Path),
				attribute.String("http.request_id", ctx.RequestID),
			),
		)
		defer span.End()

		// Replace context with span context
		ctx.Request = ctx.Request.WithContext(ctxSpan)

		ctx.StartTime = time.Now()
		recorder := &responseRecorder{ResponseWriter: ctx.Response, statusCode: http.StatusOK}
		ctx.Response = recorder
		err := handler(ctx)
		duration := time.Since(ctx.StartTime)

		// Record response status
		if err != nil {
			span.SetAttributes(attribute.String("error", err.Error()))
		}
		span.SetAttributes(
			attribute.Int("http.status_code", recorder.statusCode),
			attribute.Int64("http.duration_ms", duration.Milliseconds()),
		)

		userID := ""
		if ctx.User != nil {
			userID = ctx.User.ID
		}
		if s.audit != nil {
			s.audit.LogRequest(redactHeartbeatRequest(ctx.Request), userID, recorder.statusCode, duration, ctx.RequestID)
		}

		s.logger.Info("HTTP request",
			"method", ctx.Request.Method,
			"path", logReq.URL.Path,
			"query", sanitizeQueryString(ctx.Request.URL.RawQuery), // sanitized — masks tokens
			"duration", duration,
			"error", err,
			"request_id", ctx.RequestID,
			"trace_id", span.SpanContext().TraceID().String())

		return err
	}
}

// sanitizeURL returns a copy of urlStr with the values of sensitive query
// parameters replaced by "***" so that tokens, secrets and credentials never
// appear in logs, traces or error messages. Only known parameter names from
// sensitiveQueryParams are masked; all other parameters are left intact.
func sanitizeURL(urlStr string) string {
	idx := strings.IndexByte(urlStr, '?')
	if idx < 0 {
		return urlStr
	}
	base := urlStr[:idx]
	rawQuery := urlStr[idx+1:]
	sanitized := sanitizeQueryString(rawQuery)
	if sanitized == rawQuery {
		return urlStr
	}
	return base + "?" + sanitized
}

// sanitizeQueryString returns a copy of rawQuery with the values of sensitive
// query parameters replaced by "***". It accepts a raw query string (the part
// after "?") and returns the sanitized form.
func sanitizeQueryString(rawQuery string) string {
	if rawQuery == "" {
		return rawQuery
	}

	var b strings.Builder
	b.Grow(len(rawQuery))

	first := true
	for rawQuery != "" {
		var pair string
		ampIdx := strings.IndexByte(rawQuery, '&')
		if ampIdx < 0 {
			pair = rawQuery
			rawQuery = ""
		} else {
			pair = rawQuery[:ampIdx]
			rawQuery = rawQuery[ampIdx+1:]
		}

		if !first {
			b.WriteByte('&')
		}
		first = false

		eqIdx := strings.IndexByte(pair, '=')
		if eqIdx < 0 {
			// No value — parameter without "=", pass through
			b.WriteString(pair)
			continue
		}
		key := pair[:eqIdx]
		val := pair[eqIdx+1:]
		b.WriteString(key)
		b.WriteByte('=')
		decodedKey, err := url.QueryUnescape(key)
		if err == nil && sensitiveQueryParams[strings.ToLower(decodedKey)] && val != "" {
			b.WriteString("***")
		} else {
			b.WriteString(val)
		}
	}

	return b.String()
}

// requestIDMiddleware generates a unique request ID for every API request.
// It reads an existing X-Request-ID header (for client-side correlation) or
// generates a new ULID. The ID is set on the response, attached to the
// request context, and surfaced in structured logs and OpenTelemetry spans.
// Placing this middleware first means every downstream middleware and handler
// can rely on ctx.RequestID being set.
func (s *RESTServer) requestIDMiddleware(handler Handler) Handler {
	return func(ctx *Context) error {
		id := ctx.Request.Header.Get("X-Request-ID")
		if id == "" {
			id = core.GenerateID()
		}
		ctx.RequestID = id
		ctx.Response.Header().Set("X-Request-ID", id)
		return handler(ctx)
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

		ctx.Response.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		ctx.Response.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		ctx.Response.Header().Set("Access-Control-Max-Age", "86400") // 24 hours

		if ctx.Request.Method == "OPTIONS" {
			ctx.Response.WriteHeader(http.StatusNoContent)
			return nil
		}

		return handler(ctx)
	}
}

// getAllowedOrigins returns the list of allowed CORS origins.
// Production deployments should always set server.allowed_origins explicitly
// or via ANUBIS_CORS_ORIGINS. The localhost fallback is safe only for
// single-node or local development environments.
func (s *RESTServer) getAllowedOrigins() []string {
	return resolveAllowedOrigins(s.config.AllowedOrigins)
}

// resolveAllowedOrigins is the single definition of the CORS origin policy:
// explicit configuration first, then the ANUBIS_CORS_ORIGINS environment
// variable, then the localhost development defaults.
//
// Both the CORS middleware and the router's preflight handler go through here,
// because when each applied the policy on its own they disagreed: with nothing
// configured, a preflight from a default development origin was answered without
// Access-Control-Allow-Origin while the same origin was allowed on the actual
// request, so the browser blocked exactly the requests the server permitted.
func resolveAllowedOrigins(configured []string) []string {
	// Priority 1: Config file settings (MED-18)
	if len(configured) > 0 {
		return configured
	}
	// Priority 2: Environment variable
	if allowed := os.Getenv("ANUBIS_CORS_ORIGINS"); allowed != "" {
		origins := strings.Split(allowed, ",")
		for i := range origins {
			origins[i] = strings.TrimSpace(origins[i])
		}
		return origins
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
				if err := ctx.Error(http.StatusInternalServerError, "internal server error"); err != nil {
					s.logger.Warn("failed to write panic response", "err", err)
				}
			}
		}()
		return handler(ctx)
	}
}

// validateJSONMiddleware validates Content-Type and JSON body for POST/PUT requests
func (s *RESTServer) validateJSONMiddleware(handler Handler) Handler {
	return func(ctx *Context) error {
		// Heartbeat pings come from cron jobs and scripts with arbitrary
		// (or no) Content-Type; the handler reads at most 1KB itself.
		if isHeartbeatPingPath(ctx.Request.URL.Path) {
			return handler(ctx)
		}
		if ctx.Request.Method == "POST" || ctx.Request.Method == "PUT" || ctx.Request.Method == "PATCH" {
			// Check Content-Length for pre-negotiated-size check
			if ctx.Request.ContentLength > maxRequestBodySize {
				return ctx.Error(http.StatusRequestEntityTooLarge, "Request body too large (max 1MB)")
			}

			// Whether a body is present is decided from the bytes that arrive,
			// not from the declared length. ContentLength is -1 when the length
			// is unknown, which net/http reports for HTTP/2 requests that omit
			// content-length (HTTP/2 has no chunked encoding). Treating unknown
			// as "no body" skipped every check below for such requests; treating
			// it as "has a body" rejected bodyless writes (logout, force-check,
			// acknowledge), which must not need a Content-Type header. Reading
			// settles it: only a request that carries bytes is held to the policy.
			var bodyBytes []byte
			mayHaveBody := ctx.Request.ContentLength != 0 || ctx.Request.TransferEncoding != nil
			if mayHaveBody && ctx.Request.Body != nil && ctx.Request.Body != http.NoBody {
				// Read maxRequestBodySize+1 to distinguish "exactly at limit"
				// from "oversized" — io.LimitReader truncates silently.
				limited := io.LimitReader(ctx.Request.Body, maxRequestBodySize+1)
				var err error
				bodyBytes, err = io.ReadAll(limited)
				if err != nil {
					return ctx.Error(http.StatusBadRequest, "Invalid request body")
				}
				if len(bodyBytes) > maxRequestBodySize {
					return ctx.Error(http.StatusRequestEntityTooLarge, "Request body too large (max 1MB)")
				}

				// Restore body for later handlers
				ctx.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			}

			if len(bodyBytes) > 0 {
				contentType := ctx.Request.Header.Get("Content-Type")
				if !strings.HasPrefix(contentType, "application/json") {
					return ctx.Error(http.StatusBadRequest, "Content-Type must be application/json")
				}

				// Check for common injection patterns
				if containsInjectionPatterns(string(bodyBytes)) {
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

// defaultContentSecurityPolicy covers the API, the embedded dashboard and the
// public status pages. Scripts load only from this origin and never inline:
// the dashboard's index.html references its Vite bundles by URL and status
// pages ship no script at all. Inline styles stay allowed because status
// pages embed their (sanitized) theme CSS in a <style> block and use style
// attributes.
const defaultContentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; connect-src 'self' ws: wss:; font-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'"

// securityHeadersMiddleware adds security headers to all responses
func (s *RESTServer) securityHeadersMiddleware(handler Handler) Handler {
	return func(ctx *Context) error {
		// Add security headers
		ctx.Response.Header().Set("X-Content-Type-Options", "nosniff")
		ctx.Response.Header().Set("X-Frame-Options", "DENY")
		ctx.Response.Header().Set("X-XSS-Protection", "1; mode=block")
		ctx.Response.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		ctx.Response.Header().Set("Content-Security-Policy", defaultContentSecurityPolicy)
		// HSTS: force HTTPS — only set when TLS is actually enabled
		if s.config.TLS.Enabled {
			ctx.Response.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		}

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

// Rate limits, per minute. Authenticated users additionally get twice these
// budgets across all of their addresses (userRateLimited).
const (
	rateLimitDefault   = 100
	rateLimitAuth      = 10
	rateLimitSensitive = 20
	rateLimitWindow    = time.Minute
)

// rateLimitClass returns the extra, smaller budget a request draws from on
// top of the shared one. Credential operations have their own budget;
// ordinary dashboard reads must not consume it. The bucket names are fixed
// classes, never caller-controlled URL paths.
func rateLimitClass(r *http.Request) (string, int) {
	path := r.URL.Path
	authPath := strings.HasPrefix(path, "/api/v1/auth/") || strings.HasPrefix(path, "/auth/") || path == "/login" || path == "/register"
	if authPath && (r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions || strings.HasSuffix(path, "/oidc/login") || strings.HasSuffix(path, "/oidc/callback")) {
		return "auth", rateLimitAuth
	}
	if strings.HasPrefix(path, "/api/v1/souls") && (r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete || strings.Contains(path, "delete") || strings.Contains(path, "update")) {
		return "sensitive", rateLimitSensitive
	}
	return "", rateLimitDefault
}

type rateLimitQuota struct {
	state *rateLimitClientState
	limit int
}

// takeRateLimitQuotas charges one request to every quota named by keys, or
// to none of them if any is exhausted, in which case it returns that quota.
// On success it returns the quota with the least headroom left. Caller holds
// s.rateLimitMu.
func takeRateLimitQuotas(clients map[string]*rateLimitClientState, now time.Time, keys []string, limits []int) (rateLimitQuota, bool) {
	quotas := make([]rateLimitQuota, 0, len(keys))
	for i, key := range keys {
		state := clients[key]
		if state == nil || !state.resetTime.After(now) {
			state = &rateLimitClientState{resetTime: now.Add(rateLimitWindow)}
			clients[key] = state
		}
		quotas = append(quotas, rateLimitQuota{state, limits[i]})
	}
	for _, q := range quotas {
		if q.state.count >= q.limit {
			return q, false
		}
	}
	selected := quotas[0]
	for _, q := range quotas {
		q.state.count++
		if q.limit-q.state.count < selected.limit-selected.state.count {
			selected = q
		}
	}
	return selected, true
}

func setRateLimitHeaders(ctx *Context, q rateLimitQuota, now time.Time, ok bool) {
	h := ctx.Response.Header()
	h.Set("X-RateLimit-Limit", strconv.Itoa(q.limit))
	if !ok {
		h.Set("Retry-After", strconv.Itoa(max(1, int(q.state.resetTime.Sub(now)/time.Second))))
		h.Set("X-RateLimit-Remaining", "0")
		return
	}
	h.Set("X-RateLimit-Remaining", strconv.Itoa(q.limit-q.state.count))
	h.Set("X-RateLimit-Reset", strconv.FormatInt(q.state.resetTime.Unix(), 10))
}

// rateLimitMiddleware implements rate limiting per client IP. It runs before
// authentication, so per-user quotas are applied by requireAuth
// (userRateLimited) once the user is known; each request is charged to each
// quota exactly once.
func (s *RESTServer) rateLimitMiddleware(handler Handler) Handler {
	s.rateLimitInitOnce.Do(func() {
		if s.rateLimitStopCh == nil {
			s.rateLimitStopCh = make(chan struct{})
		}
		s.rateLimitMu.Lock()
		s.rateLimitIPClients = make(map[string]*rateLimitClientState)
		s.rateLimitUserClients = make(map[string]*rateLimitClientState)
		s.rateLimitMu.Unlock()

		go func() {
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-s.rateLimitStopCh:
					return
				case <-ticker.C:
					s.rateLimitMu.Lock()
					now := time.Now()
					for ip, state := range s.rateLimitIPClients {
						if state.resetTime.Before(now) {
							delete(s.rateLimitIPClients, ip)
						}
					}
					for user, state := range s.rateLimitUserClients {
						if state.resetTime.Before(now) {
							delete(s.rateLimitUserClients, user)
						}
					}
					s.rateLimitMu.Unlock()
				}
			}
		}()
	})

	return func(ctx *Context) error {
		if strings.HasPrefix(ctx.Request.URL.Path, "/health") || strings.HasPrefix(ctx.Request.URL.Path, "/ready") {
			return handler(ctx)
		}

		ip := s.realIP(ctx.Request)
		class, classLimit := rateLimitClass(ctx.Request)
		keys, limits := []string{ip}, []int{rateLimitDefault}
		if class != "" {
			keys, limits = append(keys, ip+"\x00"+class), append(limits, classLimit)
		}
		now := time.Now()
		s.rateLimitMu.Lock()
		q, ok := takeRateLimitQuotas(s.rateLimitIPClients, now, keys, limits)
		setRateLimitHeaders(ctx, q, now, ok)
		s.rateLimitMu.Unlock()
		if !ok {
			return ctx.Error(http.StatusTooManyRequests, fmt.Sprintf("Rate limit exceeded (%d requests/minute)", q.limit))
		}
		return handler(ctx)
	}
}

// userRateLimited charges an authenticated request to its user's quotas
// and reports whether one of them is exhausted. Those quotas bound a user
// spreading requests over many addresses. Routes not behind
// rateLimitMiddleware, and anonymous access with auth disabled, are not
// charged.
func (s *RESTServer) userRateLimited(ctx *Context) bool {
	if ctx.User == nil || ctx.User.ID == "" || !s.authConfig.IsEnabled() {
		return false
	}
	if strings.HasPrefix(ctx.Request.URL.Path, "/health") || strings.HasPrefix(ctx.Request.URL.Path, "/ready") {
		return false
	}
	class, classLimit := rateLimitClass(ctx.Request)
	keys, limits := []string{ctx.User.ID}, []int{rateLimitDefault * 2}
	if class != "" {
		keys, limits = append(keys, ctx.User.ID+"\x00"+class), append(limits, classLimit*2)
	}
	now := time.Now()
	s.rateLimitMu.Lock()
	defer s.rateLimitMu.Unlock()
	if s.rateLimitUserClients == nil {
		return false
	}
	q, ok := takeRateLimitQuotas(s.rateLimitUserClients, now, keys, limits)
	if !ok {
		setRateLimitHeaders(ctx, q, now, false)
		return true
	}
	// Report the user quota when it is tighter than the IP quota already
	// announced by rateLimitMiddleware.
	if remaining, err := strconv.Atoi(ctx.Response.Header().Get("X-RateLimit-Remaining")); err != nil || q.limit-q.state.count < remaining {
		setRateLimitHeaders(ctx, q, now, true)
	}
	return false
}

// Router methods

func (r *Router) Handle(method, path string, handler Handler) {
	if r.routes[path] == nil {
		r.routes[path] = make(map[string]Handler)

		if strings.Contains(path, ":") {
			if r.paramRoutes == nil {
				r.paramRoutes = make(map[int][]string)
			}
			// matchRoute only matches equal segment counts, so bucketing by
			// count keeps the per-request scan to the plausible candidates.
			n := len(strings.Split(path, "/"))
			bucket := append(r.paramRoutes[n], path)
			sort.Slice(bucket, func(i, j int) bool {
				return moreSpecificRoute(bucket[i], bucket[j])
			})
			r.paramRoutes[n] = bucket
		}
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

		// Resolve through the shared policy, so a preflight reaches the same
		// decision as corsMiddleware for the same origin.
		allowedOrigins := resolveAllowedOrigins(r.allowedOrigins)

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
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
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
			if err := handler(ctx); err != nil {
				slog.Error("handler error", "path", path, "method", method, "err", err)
			}
			return
		}
	}

	// Try parameterized routes, most specific first
	for _, routePath := range r.paramRoutes[len(strings.Split(path, "/"))] {
		if params, ok := matchRoute(routePath, path); ok {
			if handler, ok := r.routes[routePath][method]; ok {
				ctx := &Context{
					Request:  req,
					Response: w,
					Params:   params,
				}
				if err := handler(ctx); err != nil {
					slog.Error("handler error", "path", path, "method", method, "err", err)
				}
				return
			}
		}
	}

	// Status page routes (before dashboard fallback)
	if r.statusPage != nil && strings.HasPrefix(path, "/status/") {
		r.serveFallback(w, req, statusPageFallback(r.statusPage))
		return
	}

	if handler, ok := r.statusPage.(hostAwareStatusPageHandler); ok && handler.CanServeHost(req.Host) {
		r.serveFallback(w, req, statusPageFallback(handler))
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
		r.serveFallback(w, req, func(ctx *Context) error {
			r.dashboard.ServeHTTP(ctx.Response, ctx.Request)
			return nil
		})
		return
	}

	// No route found
	r.serveFallback(w, req, func(ctx *Context) error {
		ctx.Response.WriteHeader(http.StatusNotFound)
		if err := json.NewEncoder(ctx.Response).Encode(map[string]string{"error": "not found"}); err != nil {
			slog.Warn("failed to encode 404 response", "err", err)
		}
		return nil
	})
}

// serveFallback runs handler, wrapped in the router's fallback middleware,
// for a request no route matched.
func (r *Router) serveFallback(w http.ResponseWriter, req *http.Request, handler Handler) {
	for i := len(r.fallbackMiddleware) - 1; i >= 0; i-- {
		handler = r.fallbackMiddleware[i](handler)
	}
	ctx := &Context{Request: req, Response: w, Params: make(map[string]string)}
	if err := handler(ctx); err != nil {
		slog.Error("handler error", "path", req.URL.Path, "method", req.Method, "err", err)
	}
}

// statusPageFallback serves a public status page. Status pages may be framed
// by the same origin (as the embeddable widget always allowed) but not by
// other sites; everything else keeps the default security headers.
func statusPageFallback(page http.Handler) Handler {
	return func(ctx *Context) error {
		ctx.Response.Header().Set("X-Frame-Options", "SAMEORIGIN")
		page.ServeHTTP(ctx.Response, ctx.Request)
		return nil
	}
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
	if status == http.StatusNoContent {
		c.Response.WriteHeader(status)
		return nil
	}
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
	if handled, werr := s.leaderUnavailable(ctx, err); handled {
		return werr
	}
	if refused, werr := quotaExceeded(ctx, err); refused {
		return werr
	}
	s.logger.Error("internal server error", "context", context, "error", err, "path", ctx.Request.URL.Path)
	return ctx.Error(http.StatusInternalServerError, "internal server error")
}

// quotaExceeded answers 409 Conflict when err is a workspace quota refusal,
// naming the quota and its limit so clients can say which one was hit. It
// reports false (writing nothing) for any other error.
func quotaExceeded(ctx *Context, err error) (bool, error) {
	qe, ok := core.AsQuotaExceeded(err)
	if !ok {
		return false, nil
	}
	body := map[string]interface{}{
		"error":     qe.Error(),
		"code":      "quota_exceeded",
		"quota":     qe.Quota,
		"workspace": qe.Workspace,
	}
	if qe.Quota == core.QuotaCheckIntervalMin {
		body["limit"] = qe.Floor.String()
	} else {
		body["limit"] = qe.Limit
		body["current"] = qe.Current
	}
	return true, ctx.JSON(http.StatusConflict, body)
}

// leaderRetryAfterSeconds is the Retry-After sent with a 503 for a write the
// cluster could not commit for want of a leader: about one election.
const leaderRetryAfterSeconds = 2

// leaderUnavailable answers 503 with Retry-After when err says the write could
// not reach a Raft leader -- an election is in progress or this node is cut off
// from the majority. Such a write was not applied and is safe to retry, unlike
// a 500. handled reports whether it wrote that response; for any other error
// it writes nothing and returns false. (It used to return ctx.JSON's result,
// which is nil on success, so callers fell through and appended a second
// "internal server error" body to the 503.)
func (s *RESTServer) leaderUnavailable(ctx *Context, err error) (bool, error) {
	raftErr, unavailable := core.LeaderUnavailable(err)
	if !unavailable {
		return false, nil
	}
	s.logger.Warn("write refused: no cluster leader", "error", err, "path", ctx.Request.URL.Path)
	ctx.Response.Header().Set("Retry-After", strconv.Itoa(leaderRetryAfterSeconds))
	body := map[string]string{"error": "cluster leader unavailable; retry shortly"}
	if raftErr.NodeID != "" {
		body["leader"] = raftErr.NodeID
	}
	return true, ctx.JSON(http.StatusServiceUnavailable, body)
}

// Bind parses JSON request body with size limits to prevent DoS
func (c *Context) Bind(v interface{}) error {
	// Limit request body size to prevent memory exhaustion
	c.Request.Body = http.MaxBytesReader(c.Response, c.Request.Body, maxRequestBodySize)
	return json.NewDecoder(&maxDepthReader{r: c.Request.Body}).Decode(v)
}

// bindCreate binds a create request into v and reports whether the body set
// "enabled". New channels, rules, journeys and maintenance windows are enabled
// unless the request says otherwise; a missing field used to store them
// disabled, so they silently did nothing.
func (c *Context) bindCreate(v interface{}) (enabledSet bool, err error) {
	var raw json.RawMessage
	if err := c.Bind(&raw); err != nil {
		return false, err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return false, err
	}
	var presence struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.Unmarshal(raw, &presence); err != nil {
		return false, err
	}
	return presence.Enabled != nil, nil
}

// Maintenance Window handlers

func (s *RESTServer) handleListMaintenanceWindows(ctx *Context) error {
	windows, err := s.store.ListMaintenanceWindows()
	if err != nil {
		return s.internalError(ctx, err, "internal server error")
	}
	workspace := contextWorkspace(ctx)
	filtered := make([]*core.MaintenanceWindow, 0, len(windows))
	for _, window := range windows {
		if sameWorkspace(window.WorkspaceID, workspace) {
			filtered = append(filtered, window)
		}
	}
	return ctx.JSON(http.StatusOK, filtered)
}

func (s *RESTServer) handleCreateMaintenanceWindow(ctx *Context) error {
	var w core.MaintenanceWindow
	enabledSet, err := ctx.bindCreate(&w)
	if err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid JSON")
	}
	if !enabledSet {
		w.Enabled = true
	}
	if err := normalizeMaintenanceWindowForSave(&w); err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}
	w.ID = core.GenerateID()
	w.WorkspaceID = contextWorkspace(ctx)
	now := time.Now().UTC()
	w.CreatedAt = now
	w.UpdatedAt = now
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
	if !sameWorkspace(w.WorkspaceID, contextWorkspace(ctx)) {
		return ctx.Error(http.StatusForbidden, "access denied")
	}
	return ctx.JSON(http.StatusOK, w)
}

func (s *RESTServer) handleUpdateMaintenanceWindow(ctx *Context) error {
	id := ctx.Params["id"]
	w, err := s.store.GetMaintenanceWindow(id)
	if err != nil {
		return ctx.Error(http.StatusNotFound, "maintenance window not found")
	}
	if !sameWorkspace(w.WorkspaceID, contextWorkspace(ctx)) {
		return ctx.Error(http.StatusForbidden, "access denied")
	}
	var input map[string]json.RawMessage
	if err := ctx.Bind(&input); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid JSON")
	}
	updated := *w
	updated.SoulIDs = append([]string(nil), w.SoulIDs...)
	updated.Tags = append([]string(nil), w.Tags...)
	if err := applyMaintenanceWindowPatch(&updated, input); err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}
	if err := normalizeMaintenanceWindowForSave(&updated); err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}
	updated.ID = id
	updated.WorkspaceID = contextWorkspace(ctx)
	updated.CreatedAt = w.CreatedAt
	updated.UpdatedAt = time.Now().UTC()
	if err := s.store.SaveMaintenanceWindow(&updated); err != nil {
		return s.internalError(ctx, err, "internal server error")
	}
	return ctx.JSON(http.StatusOK, updated)
}

func normalizeMaintenanceWindowForSave(w *core.MaintenanceWindow) error {
	w.Name = strings.TrimSpace(w.Name)
	w.Description = strings.TrimSpace(w.Description)
	w.Recurring = strings.TrimSpace(strings.ToLower(w.Recurring))
	if w.Recurring == "none" {
		w.Recurring = ""
	}
	w.SoulIDs = cleanStringList(w.SoulIDs)
	w.Tags = cleanStringList(w.Tags)

	if w.Name == "" {
		return fmt.Errorf("maintenance window name is required")
	}
	if w.StartTime.IsZero() {
		return fmt.Errorf("maintenance window start_time is required")
	}
	if w.EndTime.IsZero() {
		return fmt.Errorf("maintenance window end_time is required")
	}
	if !w.EndTime.After(w.StartTime) {
		return fmt.Errorf("maintenance window end_time must be after start_time")
	}
	switch w.Recurring {
	case "", "daily", "weekly", "monthly":
		return nil
	default:
		return fmt.Errorf("maintenance window recurring must be one of daily, weekly, or monthly")
	}
}

func applyMaintenanceWindowPatch(w *core.MaintenanceWindow, input map[string]json.RawMessage) error {
	if err := applyStringPatch(input, "name", &w.Name); err != nil {
		return err
	}
	if err := applyStringPatch(input, "description", &w.Description); err != nil {
		return err
	}
	if err := applyStringSlicePatch(input, "soul_ids", &w.SoulIDs); err != nil {
		return err
	}
	if err := applyStringSlicePatch(input, "tags", &w.Tags); err != nil {
		return err
	}
	if err := applyTimePatch(input, "start_time", &w.StartTime); err != nil {
		return err
	}
	if err := applyTimePatch(input, "end_time", &w.EndTime); err != nil {
		return err
	}
	if err := applyStringPatch(input, "recurring", &w.Recurring); err != nil {
		return err
	}
	if raw, ok := input["enabled"]; ok {
		var enabled bool
		if err := json.Unmarshal(raw, &enabled); err != nil {
			return fmt.Errorf("maintenance window enabled must be a boolean")
		}
		w.Enabled = enabled
	}
	return nil
}

func applyStringPatch(input map[string]json.RawMessage, field string, target *string) error {
	raw, ok := input[field]
	if !ok {
		return nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("maintenance window %s must be a string", field)
	}
	*target = value
	return nil
}

func applyStringSlicePatch(input map[string]json.RawMessage, field string, target *[]string) error {
	raw, ok := input[field]
	if !ok {
		return nil
	}
	var value []string
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("maintenance window %s must be an array of strings", field)
	}
	*target = value
	return nil
}

func applyTimePatch(input map[string]json.RawMessage, field string, target *time.Time) error {
	raw, ok := input[field]
	if !ok {
		return nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("maintenance window %s must be an RFC3339 timestamp string", field)
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return fmt.Errorf("maintenance window %s must be an RFC3339 timestamp string", field)
	}
	*target = parsed
	return nil
}

func cleanStringList(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return cleaned
}

func (s *RESTServer) handleDeleteMaintenanceWindow(ctx *Context) error {
	id := ctx.Params["id"]
	w, err := s.store.GetMaintenanceWindow(id)
	if err != nil {
		return ctx.Error(http.StatusNotFound, "maintenance window not found")
	}
	if !sameWorkspace(w.WorkspaceID, contextWorkspace(ctx)) {
		return ctx.Error(http.StatusForbidden, "access denied")
	}
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

		if s.ws == nil || judgment == nil {
			return
		}
		// Redact like REST responses (raw HTTP bodies and headers stay
		// server-side) and name the soul so the live feed can label it.
		payload := judgmentDTO{Judgment: redactedJudgmentFromCore(judgment)}
		if s.store != nil {
			if soul, err := s.store.GetSoulNoCtx(judgment.SoulID); err == nil && soul != nil {
				payload.SoulName = soul.Name
			}
		}
		s.ws.broadcastTenantEvent(judgment.WorkspaceID, "judgment", WSMessage{
			Type:      "judgment",
			Timestamp: time.Now().UTC(),
			Payload:   payload,
		})
	}
}
