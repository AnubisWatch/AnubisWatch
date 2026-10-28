package api

import (
	"context"
	"crypto/subtle"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Heartbeat (push) monitors: a cron job or worker calls
//
//	GET|POST /api/v1/heartbeat/:token        success ("I ran")
//	GET|POST /api/v1/heartbeat/:token/start  run started (measures duration)
//	GET|POST /api/v1/heartbeat/:token/fail   explicit failure, body = message
//
// The token is the only credential: no session, no API key. Tokens are
// looked up by their SHA-256 in an in-memory index rebuilt from the soul set,
// and the stored token is compared in constant time. Unknown tokens get a
// plain 404. Each ping is persisted by the HeartbeatRecorder (through Raft in
// cluster mode) and judged by the soul's owner; see probe.HeartbeatChecker.

const (
	heartbeatPingPrefix = "/api/v1/heartbeat/"
	// heartbeatTokenRateLimit is the per-token ping budget per window, on top
	// of the per-IP limit every route has.
	heartbeatTokenRateLimit  = 30
	heartbeatTokenRateWindow = time.Minute
	// heartbeatIndexMinAge bounds how often a lookup miss may rebuild the
	// token index, so a flood of unknown tokens costs one soul scan a second.
	heartbeatIndexMinAge = time.Second
	// heartbeatIndexMaxAge makes even a hit rebuild the index now and then,
	// so a deleted configured soul's token stops matching.
	heartbeatIndexMaxAge = 30 * time.Second
)

// HeartbeatRecorder persists pings and reads back a soul's ping state.
type HeartbeatRecorder interface {
	// RecordHeartbeat stores one ping for soul (replicated in cluster mode)
	// and wakes the soul's evaluator.
	RecordHeartbeat(ctx context.Context, soul *core.Soul, event core.HeartbeatEvent, message, source string) error
	// GetHeartbeatState returns the soul's ping state, or nil if none.
	GetHeartbeatState(ctx context.Context, workspaceID, soulID string) (*core.HeartbeatState, error)
}

// HeartbeatSoulLister returns every soul the server monitors (stored and
// configured) so ping tokens can be indexed.
type HeartbeatSoulLister func(ctx context.Context) ([]*core.Soul, error)

type heartbeatService struct {
	recorder HeartbeatRecorder
	souls    HeartbeatSoulLister
	now      func() time.Time

	mu      sync.Mutex
	index   map[string]*core.Soul // token hash -> soul snapshot
	builtAt time.Time
	limits  map[string]*heartbeatRate // token hash -> window
}

type heartbeatRate struct {
	count int
	reset time.Time
}

// SetHeartbeat enables the heartbeat ping endpoints. Without it they answer
// 404 for every token.
func (s *RESTServer) SetHeartbeat(recorder HeartbeatRecorder, souls HeartbeatSoulLister) {
	s.heartbeat = &heartbeatService{
		recorder: recorder,
		souls:    souls,
		now:      time.Now,
		index:    make(map[string]*core.Soul),
		limits:   make(map[string]*heartbeatRate),
	}
}

// isHeartbeatPingPath reports whether path is a public ping URL.
func isHeartbeatPingPath(path string) bool {
	return strings.HasPrefix(path, heartbeatPingPrefix)
}

// redactHeartbeatRequest returns r, or a shallow copy whose path hides the
// ping token, for logs, traces and audit records.
func redactHeartbeatRequest(r *http.Request) *http.Request {
	if r == nil || r.URL == nil || !isHeartbeatPingPath(r.URL.Path) {
		return r
	}
	rest := strings.TrimPrefix(r.URL.Path, heartbeatPingPrefix)
	suffix := ""
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		suffix = rest[i:]
	}
	clone := r.WithContext(r.Context())
	u := *r.URL
	u.Path = heartbeatPingPrefix + redactedSecretValue + suffix
	u.RawPath = ""
	clone.URL = &u
	return clone
}

// currentStoredSoul re-reads a soul from storage. known is false when the
// store has no record of it: a configured soul (never stored) keeps its
// indexed snapshot, while a stored soul that was deleted is reported gone.
func (s *RESTServer) currentStoredSoul(id string) (*core.Soul, bool) {
	soul, err := s.store.GetSoulNoCtx(id)
	if err == nil && soul != nil {
		return soul, true
	}
	return nil, false
}

func writeHeartbeatText(ctx *Context, status int, body string) error {
	h := ctx.Response.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	ctx.Response.WriteHeader(status)
	_, err := io.WriteString(ctx.Response, body+"\n")
	return err
}

// lookup resolves a ping token to its soul. It costs the same hash and
// constant-time comparison whether or not the token exists. current, when
// set, re-reads a stored soul so a token rotated or deleted on another node
// stops matching without waiting for the next index rebuild.
func (h *heartbeatService) lookup(ctx context.Context, token string, current func(id string) (*core.Soul, bool)) *core.Soul {
	hash := core.HeartbeatTokenHash(token)
	valid := core.ValidHeartbeatToken(token)

	h.mu.Lock()
	soul := h.index[hash]
	age := h.now().Sub(h.builtAt)
	if valid && (soul == nil && age >= heartbeatIndexMinAge || age >= heartbeatIndexMaxAge) {
		h.rebuildLocked(ctx)
		soul = h.index[hash]
	}
	h.mu.Unlock()

	if soul != nil && current != nil {
		if fresh, known := current(soul.ID); known {
			soul = fresh
		} else if !soul.CreatedAt.IsZero() {
			// Stored souls carry CreatedAt; configured ones never do.
			soul = nil
		}
	}

	stored := ""
	if soul != nil && soul.Heartbeat != nil {
		stored = core.HeartbeatTokenHash(soul.Heartbeat.Token)
	}
	match := subtle.ConstantTimeCompare([]byte(stored), []byte(hash)) == 1
	if !valid || soul == nil || !match || soul.Type != core.CheckHeartbeat {
		return nil
	}
	return soul
}

func (h *heartbeatService) rebuildLocked(ctx context.Context) {
	h.builtAt = h.now()
	if h.souls == nil {
		return
	}
	souls, err := h.souls(ctx)
	if err != nil {
		return
	}
	index := make(map[string]*core.Soul, len(h.index))
	for _, soul := range souls {
		if soul == nil || soul.Type != core.CheckHeartbeat || soul.Heartbeat == nil || soul.Heartbeat.Token == "" {
			continue
		}
		index[core.HeartbeatTokenHash(soul.Heartbeat.Token)] = soul
	}
	h.index = index
}

// invalidate forces the next lookup miss to rebuild the index and drops a
// rotated token at once.
func (h *heartbeatService) invalidate(oldToken string) {
	h.mu.Lock()
	if oldToken != "" {
		delete(h.index, core.HeartbeatTokenHash(oldToken))
	}
	h.builtAt = time.Time{}
	h.mu.Unlock()
}

// allow charges one ping to the token's budget.
func (h *heartbeatService) allow(token string) (bool, time.Duration) {
	key := core.HeartbeatTokenHash(token)
	now := h.now()
	h.mu.Lock()
	defer h.mu.Unlock()
	rate := h.limits[key]
	if rate == nil || !rate.reset.After(now) {
		if len(h.limits) > 10000 {
			for k, r := range h.limits {
				if !r.reset.After(now) {
					delete(h.limits, k)
				}
			}
		}
		rate = &heartbeatRate{reset: now.Add(heartbeatTokenRateWindow)}
		h.limits[key] = rate
	}
	if rate.count >= heartbeatTokenRateLimit {
		return false, rate.reset.Sub(now)
	}
	rate.count++
	return true, 0
}

// sanitizeHeartbeatMessage keeps a /fail message printable and short.
func sanitizeHeartbeatMessage(raw string) string {
	raw = strings.ToValidUTF8(raw, "")
	msg := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, raw)
	msg = strings.TrimSpace(msg)
	if len(msg) > core.MaxHeartbeatMessage {
		msg = strings.ToValidUTF8(msg[:core.MaxHeartbeatMessage], "")
	}
	return msg
}

func (s *RESTServer) handleHeartbeatPing(event core.HeartbeatEvent) Handler {
	return func(ctx *Context) error {
		token := ctx.Params["token"]
		if s.heartbeat == nil {
			return writeHeartbeatText(ctx, http.StatusNotFound, "not found")
		}
		soul := s.heartbeat.lookup(ctx.Request.Context(), token, s.currentStoredSoul)
		if soul == nil {
			return writeHeartbeatText(ctx, http.StatusNotFound, "not found")
		}
		if ok, retry := s.heartbeat.allow(token); !ok {
			ctx.Response.Header().Set("Retry-After", strconv.Itoa(max(1, int(retry/time.Second))))
			return writeHeartbeatText(ctx, http.StatusTooManyRequests, "rate limited")
		}

		message := ""
		if event == core.HeartbeatFail {
			if ctx.Request.Body != nil {
				body, _ := io.ReadAll(io.LimitReader(ctx.Request.Body, core.MaxHeartbeatMessage+1))
				message = string(body)
			}
			if message == "" {
				message = ctx.Request.URL.Query().Get("msg")
			}
			message = sanitizeHeartbeatMessage(message)
		}

		if err := s.heartbeat.recorder.RecordHeartbeat(ctx.Request.Context(), soul, event, message, s.realIP(ctx.Request)); err != nil {
			if _, unavailable := core.LeaderUnavailable(err); unavailable {
				ctx.Response.Header().Set("Retry-After", strconv.Itoa(leaderRetryAfterSeconds))
				return writeHeartbeatText(ctx, http.StatusServiceUnavailable, "unavailable")
			}
			s.logger.Error("failed to record heartbeat", "soul_id", soul.ID, "event", event, "error", err)
			return writeHeartbeatText(ctx, http.StatusInternalServerError, "error")
		}
		return writeHeartbeatText(ctx, http.StatusOK, "OK")
	}
}

// heartbeatInfo is the reveal view of a heartbeat soul: its ping URLs (token
// included only for callers allowed to manage souls) and its ping state.
type heartbeatInfo struct {
	SoulID   string               `json:"soul_id"`
	Token    string               `json:"token,omitempty"`
	PingURL  string               `json:"ping_url,omitempty"`
	StartURL string               `json:"start_url,omitempty"`
	FailURL  string               `json:"fail_url,omitempty"`
	Interval core.Duration        `json:"interval"`
	Grace    core.Duration        `json:"grace"`
	State    *core.HeartbeatState `json:"state"`
}

func (s *RESTServer) heartbeatBaseURL(r *http.Request) string {
	if base := s.config.PublicBaseURL(); base != "" {
		return base
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *RESTServer) heartbeatSoulForRequest(ctx *Context) (*core.Soul, error) {
	soul, err := s.store.GetSoulNoCtx(ctx.Params["id"])
	if err != nil || soul == nil {
		return nil, ctx.Error(http.StatusNotFound, "soul not found")
	}
	if soul.WorkspaceID != ctx.Workspace {
		return nil, ctx.Error(http.StatusForbidden, "access denied: soul belongs to another workspace")
	}
	if soul.Type != core.CheckHeartbeat {
		return nil, ctx.Error(http.StatusBadRequest, "soul is not a heartbeat monitor")
	}
	return soul, nil
}

func (s *RESTServer) heartbeatView(ctx *Context, soul *core.Soul, reveal bool) heartbeatInfo {
	info := heartbeatInfo{
		SoulID:   soul.ID,
		Interval: core.Duration{Duration: soul.HeartbeatInterval()},
		Grace:    core.Duration{Duration: soul.HeartbeatGrace()},
	}
	if reveal && soul.Heartbeat != nil && soul.Heartbeat.Token != "" {
		info.Token = soul.Heartbeat.Token
		info.PingURL = s.heartbeatBaseURL(ctx.Request) + heartbeatPingPrefix + soul.Heartbeat.Token
		info.StartURL = info.PingURL + "/start"
		info.FailURL = info.PingURL + "/fail"
	}
	if s.heartbeat != nil && s.heartbeat.recorder != nil {
		if st, err := s.heartbeat.recorder.GetHeartbeatState(ctx.Request.Context(), soul.WorkspaceID, soul.ID); err == nil {
			info.State = st
		}
	}
	return info
}

func canManageSouls(ctx *Context) bool {
	return ctx.User != nil && core.MemberRole(ctx.User.Role).Can("souls:*")
}

// handleGetSoulHeartbeat returns the ping URLs and the last ping. The token
// is revealed only to roles that may edit souls.
func (s *RESTServer) handleGetSoulHeartbeat(ctx *Context) error {
	soul, err := s.heartbeatSoulForRequest(ctx)
	if soul == nil {
		return err
	}
	return ctx.JSON(http.StatusOK, s.heartbeatView(ctx, soul, canManageSouls(ctx)))
}

// handleRotateHeartbeatToken replaces the ping token; the old URL stops
// working immediately on this node and at the next index rebuild elsewhere
// (every node re-checks the stored token on each ping).
func (s *RESTServer) handleRotateHeartbeatToken(ctx *Context) error {
	soul, err := s.heartbeatSoulForRequest(ctx)
	if soul == nil {
		return err
	}
	updated := *soul
	cfg := core.HeartbeatConfig{}
	if soul.Heartbeat != nil {
		cfg = *soul.Heartbeat
	}
	oldToken := cfg.Token
	cfg.Token = ""
	updated.Heartbeat = &cfg
	if err := updated.EnsureHeartbeatToken(); err != nil {
		return s.internalError(ctx, err, "failed to generate token")
	}
	updated.UpdatedAt = time.Now()
	if err := s.store.SaveSoul(ctx.Request.Context(), &updated); err != nil {
		if handled, werr := s.leaderUnavailable(ctx, err); handled {
			return werr
		}
		return s.internalError(ctx, err, "failed to rotate token")
	}
	if s.heartbeat != nil {
		s.heartbeat.invalidate(oldToken)
	}
	s.syncProbeSoul(&updated)
	return ctx.JSON(http.StatusOK, s.heartbeatView(ctx, &updated, true))
}
