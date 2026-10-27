package statuspage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/auth"
	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Handler serves public status pages
// The Public Temple where citizens view the health of services
//
//go:generate echo "The Judgment Never Sleeps"
type Handler struct {
	repository       Repository
	templateCache    map[string]*Template
	defaultTheme     core.StatusPageTheme
	sessionValidator SessionValidator
	// subs is set by EnableSubscriptions when outgoing mail is configured.
	subs *subscriptions
}

// SessionValidator reports whether a session token may view pages in a workspace.
// The status page is served outside the authenticated API, so the handler cannot
// validate sessions itself; cmd/anubis adapts the configured authenticator onto
// this. When none is configured the session path stays closed, as it always was.
type SessionValidator interface {
	ValidateStatusPageSession(token, workspaceID string) bool
}

// SetSessionValidator installs the session validator used for private pages.
func (h *Handler) SetSessionValidator(v SessionValidator) *Handler {
	h.sessionValidator = v
	return h
}

// Repository defines the storage interface for status pages
type Repository interface {
	GetStatusPageByDomain(domain string) (*core.StatusPage, error)
	GetStatusPageBySlug(slug string) (*core.StatusPage, error)
	GetSoul(id string) (*core.Soul, error)
	GetSoulJudgments(soulID string, limit int) ([]core.Judgment, error)
	GetIncidentsByPage(pageID string) ([]core.Incident, error)
	GetUptimeHistory(soulID string, days int) ([]core.UptimeDay, error)

	// Subscription management
	SaveSubscription(sub *core.StatusPageSubscription) error
	GetSubscriptionsByPage(pageID string) ([]*core.StatusPageSubscription, error)
	DeleteSubscription(subscriptionID string) error
}

// MaintenanceSource is optionally implemented by a Repository. When it is,
// souls inside an active maintenance window are shown as "embalmed" (under
// maintenance) instead of their latest judgment's status.
type MaintenanceSource interface {
	ListMaintenanceWindows() ([]*core.MaintenanceWindow, error)
}

// activeMaintenance returns the maintenance windows active at now, or nil
// when the repository cannot list them.
func (h *Handler) activeMaintenance(now time.Time) []*core.MaintenanceWindow {
	source, ok := h.repository.(MaintenanceSource)
	if !ok {
		return nil
	}
	windows, err := source.ListMaintenanceWindows()
	if err != nil {
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

// soulPageStatus is the status a public page shows for soul: "embalmed"
// during maintenance, otherwise the latest judgment's status, or "unknown"
// before the first check. Stored judgments are never rewritten.
func soulPageStatus(soul *core.Soul, last core.Judgment, windows []*core.MaintenanceWindow, now time.Time) string {
	if core.SoulInMaintenance(windows, soul, now) {
		return string(core.SoulEmbalmed)
	}
	if last.ID == "" {
		return "unknown"
	}
	return string(last.Status)
}

// Template represents a cached status page template
type Template struct {
	HTML      string
	UpdatedAt time.Time
}

// NewHandler creates a new status page handler
func NewHandler(repo Repository) *Handler {
	return &Handler{
		repository:    repo,
		templateCache: make(map[string]*Template),
		defaultTheme:  core.GetDefaultTheme(),
	}
}

// CanServeHost reports whether the request host maps to a custom status page.
func (h *Handler) CanServeHost(host string) bool {
	domain := normalizeHost(host)
	if domain == "" {
		return false
	}
	_, err := h.repository.GetStatusPageByDomain(domain)
	return err == nil
}

// ServeHTTP handles status page requests
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Subscription endpoints share the /status/ prefix with page slugs.
	if h.serveSubscriptionRoute(w, r) {
		return
	}

	// Determine which status page to serve
	domain := normalizeHost(r.Host)

	// Try to find status page by custom domain first
	page, err := h.repository.GetStatusPageByDomain(domain)
	if err != nil {
		// Fall back to slug-based lookup
		slug := extractSlugFromPath(r.URL.Path)
		if slug == "" {
			http.NotFound(w, r)
			return
		}
		page, err = h.repository.GetStatusPageBySlug(slug)
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}

	// Password submissions use POST, then redirect to a URL without credentials.
	setPageCacheControl(w, page, 60)
	if page.Visibility == core.VisibilityProtected && r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form", http.StatusBadRequest)
			return
		}
		if h.verifyPagePassword(r.PostForm.Get("password"), page) {
			expiry := time.Now().Add(8 * time.Hour)
			// #nosec G124 -- Secure on TLS/proxied HTTPS; local HTTP requires a non-Secure cookie. HttpOnly and Strict always apply.
			http.SetCookie(w, &http.Cookie{Name: "anubis_page_pass", Value: pagePasswordTicket(page, expiry), Path: "/", Expires: expiry, MaxAge: 8 * 60 * 60, HttpOnly: true, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", SameSite: http.SameSiteStrictMode})
			q := r.URL.Query()
			q.Del("password")
			// Use the page's canonical origin-relative path. A request path
			// beginning // must never become a redirect to another authority.
			clean := url.URL{Path: "/status/" + page.Slug, RawQuery: q.Encode()}
			// #nosec G710 -- fixed /status/ prefix, no scheme/authority, encoded query.
			http.Redirect(w, r, clean.RequestURI(), http.StatusSeeOther)
			return
		}
		h.showPasswordForm(w, r, page)
		return
	}
	// Check visibility
	if !h.pageAccessAllowed(r, page) {
		if page.Visibility == core.VisibilityPrivate {
			h.requestAuthentication(w, r)
		} else {
			h.showPasswordForm(w, r, page)
		}
		return
	}

	// Build status page data
	data, err := buildStatusPageDataFn(h, page)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	data.SubscriptionsAvailable = h.SubscriptionsAvailable()

	// Serve appropriate format
	acceptHeader := r.Header.Get("Accept")
	if strings.Contains(acceptHeader, "application/json") || r.URL.Query().Get("format") == "json" {
		h.serveJSON(w, r, data)
		return
	}

	h.serveHTML(w, r, page, data)
}

func normalizeHost(host string) string {
	domain := strings.TrimSpace(strings.ToLower(host))
	if hostname, _, err := net.SplitHostPort(domain); err == nil {
		domain = hostname
	}
	return strings.Trim(domain, "[]")
}

// extractSlugFromPath extracts the slug from URL path
func extractSlugFromPath(path string) string {
	// Path format: /status/:slug or /status/:slug/*
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[0] != "status" {
		return ""
	}
	return parts[1]
}

// pageAccessAllowed reports whether the request may see this page's data under
// the page's visibility policy. Every endpoint that publishes a page must ask:
// ServeHTTP was the only one that did, so a private or password-protected page
// was refused at /status/<slug> and still served in full by the widget, badge and
// feed handlers, which load the same page by slug.
func (h *Handler) pageAccessAllowed(r *http.Request, page *core.StatusPage) bool {
	switch page.Visibility {
	case core.VisibilityPrivate:
		return h.isAuthenticated(r, page)
	case core.VisibilityProtected:
		return h.checkPasswordProtection(r, page)
	default:
		return true
	}
}

// isAuthenticated checks if request is authenticated for private pages
func (h *Handler) isAuthenticated(r *http.Request, page *core.StatusPage) bool {
	// auth_token is issued by REST/OIDC login. Keep the legacy cookie for
	// existing status-page clients, but never let it override a supplied login.
	for _, name := range []string{"auth_token", "anubis_session"} {
		if cookie, err := r.Cookie(name); err == nil && cookie.Value != "" {
			return h.validateSession(cookie.Value, page.WorkspaceID)
		}
	}

	// Check API key header
	apiKey := r.Header.Get("X-API-Key")
	if apiKey != "" {
		return h.validateAPIKey(apiKey, page.WorkspaceID)
	}

	return false
}

// validateSession validates a session token
func (h *Handler) validateSession(token string, workspaceID string) bool {
	// Without a configured validator there is nothing to validate against, so the
	// session path stays closed. A private page was therefore refused even for the
	// account that owns it, because nothing ever installed a validator.
	if h.sessionValidator == nil {
		return false
	}
	return h.sessionValidator.ValidateStatusPageSession(token, workspaceID)
}

// validateAPIKey accepts an API key ("anb_...") of the page's workspace. The
// session validator authenticates it like a bearer token; any other value in
// X-API-Key is refused, so a session token cannot be smuggled through it.
func (h *Handler) validateAPIKey(key string, workspaceID string) bool {
	if !core.IsAPIKeyToken(key) {
		return false
	}
	return h.validateSession(key, workspaceID)
}

// requestAuthentication sends authentication challenge
func (h *Handler) requestAuthentication(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(w, "Authentication required", http.StatusUnauthorized)
}

// checkPasswordProtection checks password protection
func (h *Handler) checkPasswordProtection(r *http.Request, page *core.StatusPage) bool {
	if page == nil || page.PasswordHash == "" {
		return false
	}
	cookie, err := r.Cookie("anubis_page_pass")
	if err != nil {
		return false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return false
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	payload := strings.Split(string(data), "|")
	if len(payload) != 2 || payload[0] != page.ID {
		return false
	}
	expires, err := strconv.ParseInt(payload[1], 10, 64)
	if err != nil || time.Now().Unix() >= expires {
		return false
	}
	expected := pagePasswordTicket(page, time.Unix(expires, 0))
	return hmac.Equal([]byte(cookie.Value), []byte(expected))
}
func pagePasswordTicket(page *core.StatusPage, expires time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(page.ID + "|" + strconv.FormatInt(expires.Unix(), 10)))
	mac := hmac.New(sha256.New, []byte(page.PasswordHash))
	mac.Write([]byte(payload))
	return payload + "." + hex.EncodeToString(mac.Sum(nil))
}

// verifyPagePassword verifies the page password
func (h *Handler) verifyPagePassword(password string, page *core.StatusPage) bool {
	// A page with no stored hash has no password to match, so nothing unlocks it.
	// The API refuses to save a password-protected page in that state, which used
	// to leave the page permanently unreachable.
	if page == nil || page.PasswordHash == "" {
		return false
	}
	return auth.VerifyPassword(page.PasswordHash, password)
}

// showPasswordForm renders password entry form
func (h *Handler) showPasswordForm(w http.ResponseWriter, r *http.Request, page *core.StatusPage) {
	theme := page.Theme
	if theme.PrimaryColor == "" {
		theme = h.defaultTheme
	}

	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Protected Status Page | %s</title>
    <style>
        :root {
            --primary: %s;
            --bg: %s;
            --text: %s;
            --accent: %s;
        }
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: %s;
            background: var(--bg);
            color: var(--text);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
        }
        .container {
            text-align: center;
            padding: 2rem;
        }
        .logo {
            font-size: 4rem;
            margin-bottom: 1rem;
        }
        h1 {
            color: var(--primary);
            margin-bottom: 1rem;
        }
        form {
            max-width: 300px;
            margin: 2rem auto;
        }
        input[type="password"] {
            width: 100%%;
            padding: 0.75rem;
            border: 1px solid var(--accent);
            border-radius: 0.5rem;
            background: rgba(255,255,255,0.05);
            color: var(--text);
            margin-bottom: 1rem;
        }
        button {
            width: 100%%;
            padding: 0.75rem;
            background: var(--primary);
            color: var(--bg);
            border: none;
            border-radius: 0.5rem;
            font-weight: 600;
            cursor: pointer;
        }
        .powered-by {
            margin-top: 2rem;
            font-size: 0.75rem;
            opacity: 0.6;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="logo">𓃥</div>
        <h1>%s</h1>
        <p>This status page is password protected</p>
        <form method="POST" action="">
            <input type="password" name="password" placeholder="Enter password" required autofocus>
            <button type="submit">Access Status Page</button>
        </form>
        <div class="powered-by">Powered by AnubisWatch</div>
    </div>
</body>
</html>`,
		html.EscapeString(page.Name),
		theme.PrimaryColor, theme.BackgroundColor, theme.TextColor, theme.AccentColor,
		theme.FontFamily,
		html.EscapeString(page.Name),
	)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Write failure past this point means the client is gone; not actionable.
	_, _ = w.Write([]byte(html))
}

// buildStatusPageDataFn can be overwritten by tests to inject build errors.
var buildStatusPageDataFn = func(h *Handler, page *core.StatusPage) (*core.StatusPageData, error) {
	return h.buildStatusPageData(page)
}

// buildStatusPageData builds the data for a status page
func (h *Handler) buildStatusPageData(page *core.StatusPage) (*core.StatusPageData, error) {
	// Get souls status
	souls := make([]core.SoulStatusInfo, 0, len(page.Souls))
	now := time.Now()
	windows := h.activeMaintenance(now)
	for _, soulID := range page.Souls {
		soul, err := h.repository.GetSoul(soulID)
		if err != nil {
			continue
		}

		// Get latest judgment
		judgments, err := h.repository.GetSoulJudgments(soulID, 1)
		var lastJudgment core.Judgment
		if err == nil && len(judgments) > 0 {
			lastJudgment = judgments[0]
		}

		// Determine status from last judgment; maintenance overrides it.
		status := soulPageStatus(soul, lastJudgment, windows, now)

		// Calculate uptime from 30-day history.
		// Only days that actually carry judgments may contribute: the repository
		// returns one entry per requested day, including days with no checks at
		// all (Status "unknown"). Counting those as 24 hours of downtime made a
		// healthy monitor that was created today report 100/30 = 3.33% uptime
		// on its public status page, and an idle monitor report 0%.
		uptimePercent := 100.0
		history, err := h.repository.GetUptimeHistory(soulID, 30)
		if err == nil && len(history) > 0 {
			totalHours := 0.0
			upHours := 0.0
			for _, day := range history {
				if day.Status == "unknown" {
					// No judgments recorded for this day: the monitor was not
					// (yet) reporting, which is not downtime.
					continue
				}
				totalHours += 24.0
				upHours += 24.0 * (day.Uptime / 100.0)
			}
			if totalHours > 0 {
				uptimePercent = (upHours / totalHours) * 100.0
			}
		}

		soulInfo := core.SoulStatusInfo{
			ID:            soul.ID,
			Name:          soul.Name,
			Status:        status,
			UptimePercent: uptimePercent,
			LastCheckedAt: lastJudgment.Timestamp,
			ResponseTime:  float64(lastJudgment.Duration.Milliseconds()),
		}
		souls = append(souls, soulInfo)
	}

	// Operator-posted incidents and maintenance announcements override the
	// component status they name (incidents.go).
	announcements := core.PublicAnnouncements(h.announcementsWithWindows(page), now)
	applyIncidentImpact(souls, page.Incidents, announcements, now)

	// Calculate overall status
	overallStatus := overallWithIncidents(core.CalculateOverallStatus(souls), page.Incidents)

	// Get groups with soul data
	var soulsByID map[string]core.SoulStatusInfo
	if len(page.Groups) > 0 {
		soulsByID = make(map[string]core.SoulStatusInfo, len(souls))
		for _, soul := range souls {
			// Keep the first match when an ID appears more than once.
			if _, exists := soulsByID[soul.ID]; !exists {
				soulsByID[soul.ID] = soul
			}
		}
	}
	groups := make([]core.GroupStatusInfo, 0, len(page.Groups))
	for _, group := range page.Groups {
		groupSouls := make([]core.SoulStatusInfo, 0)
		for _, soulID := range group.SoulIDs {
			if soul, exists := soulsByID[soulID]; exists {
				groupSouls = append(groupSouls, soul)
			}
		}
		groups = append(groups, core.GroupStatusInfo{
			ID:          group.ID,
			Name:        group.Name,
			Description: group.Description,
			Souls:       groupSouls,
		})
	}

	// Get active incidents from page
	statusIncidents := make([]core.StatusIncident, 0)
	for _, inc := range page.Incidents {
		if inc.Status == core.StatusOngoing || inc.Status == core.StatusInvestigating ||
			inc.Status == core.StatusIdentified || inc.Status == core.StatusMonitoring {
			statusIncidents = append(statusIncidents, inc)
		}
	}

	// Get uptime data
	uptimeData := h.buildUptimeData(page, souls)

	return &core.StatusPageData{
		Page: core.StatusPageInfo{
			ID:           page.ID,
			Name:         page.Name,
			Description:  page.Description,
			LogoURL:      page.LogoURL,
			CustomDomain: page.CustomDomain,
		},
		Status:    overallStatus,
		Souls:     souls,
		Groups:    groups,
		Incidents: statusIncidents,
		Uptime:    uptimeData,

		IncidentHistory: core.PublicIncidentHistory(page.Incidents, now),
		Maintenance:     announcements,
	}, nil
}

// buildUptimeData builds uptime data for the page
func (h *Handler) buildUptimeData(page *core.StatusPage, souls []core.SoulStatusInfo) core.UptimeData {
	days := make([]core.UptimeDay, 0, page.UptimeDays)

	for _, soul := range souls {
		history, err := h.repository.GetUptimeHistory(soul.ID, page.UptimeDays)
		if err != nil {
			continue
		}
		days = append(days, history...)
	}

	sort.Slice(days, func(i, j int) bool {
		return days[i].Date < days[j].Date
	})

	// Calculate overall uptime percentage
	// Days without any judgment (Status "unknown") are not downtime; the
	// per-soul figure in buildStatusPageData skips them the same way.
	var totalUptime float64
	judgedDays := 0
	for _, day := range days {
		if day.Status == "unknown" {
			continue
		}
		totalUptime += day.Uptime
		judgedDays++
	}
	overallPercent := 0.0
	if judgedDays > 0 {
		overallPercent = totalUptime / float64(judgedDays)
	}

	return core.UptimeData{
		Days:    days,
		Overall: overallPercent,
	}
}

// serveJSON serves status data as JSON
func (h *Handler) serveJSON(w http.ResponseWriter, r *http.Request, data *core.StatusPageData) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	// Client is gone on write failure; nothing actionable remains.
	_ = json.NewEncoder(w).Encode(data)
}

// serveHTML serves the rendered status page HTML
func (h *Handler) serveHTML(w http.ResponseWriter, r *http.Request, page *core.StatusPage, data *core.StatusPageData) {
	theme := page.Theme
	if theme.PrimaryColor == "" {
		theme = h.defaultTheme
	}

	// Generate HTML
	html := h.renderStatusPage(page, data, theme)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	setPageCacheControl(w, page, 60)
	// Write failure past this point means the client is gone; not actionable.
	_, _ = w.Write([]byte(html))
}

// setPageCacheControl keeps session/password protected output out of shared
// caches, including the JSON, badge, widget, feed and authentication responses.
func setPageCacheControl(w http.ResponseWriter, page *core.StatusPage, publicMaxAge int) {
	if page.Visibility == core.VisibilityPrivate || page.Visibility == core.VisibilityProtected {
		w.Header().Set("Cache-Control", "private, no-store")
		return
	}
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", publicMaxAge))
}

// renderStatusPage renders the status page HTML
func (h *Handler) renderStatusPage(page *core.StatusPage, data *core.StatusPageData, theme core.StatusPageTheme) string {
	// Build status indicator
	statusClass := "status-" + data.Status.Status

	// Build souls list
	soulsHTML := ""
	for _, soul := range data.Souls {
		soulClass := "soul-" + soul.Status
		soulsHTML += fmt.Sprintf(`
		<div class="soul %s">
			<div class="soul-name">%s</div>
			<div class="soul-status">
				<span class="status-indicator %s"></span>
				%s
			</div>
			<div class="soul-metrics">
				<span>%.2f ms</span>
				<span>%.2f%% uptime</span>
			</div>
		</div>`,
			soulClass, html.EscapeString(soul.Name), soulClass, html.EscapeString(soul.Status), soul.ResponseTime, soul.UptimePercent,
		)
	}

	// Build groups
	groupsHTML := ""
	for _, group := range data.Groups {
		groupSouls := ""
		for _, soul := range group.Souls {
			soulClass := "soul-" + soul.Status
			groupSouls += fmt.Sprintf(`
			<div class="soul %s">
				<span class="status-indicator %s"></span>
				%s
			</div>`,
				soulClass, soulClass, html.EscapeString(soul.Name),
			)
		}
		groupsHTML += fmt.Sprintf(`
		<div class="group">
			<h3>%s</h3>
			<p>%s</p>
			<div class="group-souls">%s</div>
		</div>`,
			html.EscapeString(group.Name), html.EscapeString(group.Description), groupSouls,
		)
	}

	// Build active incidents and pending maintenance (incidents.go)
	incidentsHTML := renderActiveSections(data)

	// Build uptime chart (simplified)
	uptimeHTML := fmt.Sprintf(`
	<div class="uptime-section">
		<h2>Uptime History</h2>
		<div class="uptime-overall">
			<span class="uptime-value">%.2f%%</span>
			<span class="uptime-label">Overall Uptime</span>
		</div>
		<div class="uptime-days">`, data.Uptime.Overall)

	for _, day := range data.Uptime.Days {
		dayClass := "day-" + day.Status
		uptimeHTML += fmt.Sprintf(`<div class="uptime-day %s" title="%s: %.1f%%"></div>`,
			dayClass, day.Date, day.Uptime)
	}
	uptimeHTML += "</div></div>"
	uptimeHTML += renderHistorySection(data)
	if data.SubscriptionsAvailable {
		uptimeHTML += subscribeFormHTML(page)
	}

	// Sanitize custom CSS: strip anything that could break out of the <style> block
	// or inject CSSexpression/behaviors/urls that could lead to script execution
	sanitizedCSS := sanitizeCSS(theme.CustomCSS)

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s | Status</title>
    <style>
        :root {
            --primary: %s;
            --bg: %s;
            --text: %s;
            --accent: %s;
            --card-bg: rgba(255,255,255,0.03);
            --border: rgba(255,255,255,0.1);
        }
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: %s;
            background: var(--bg);
            color: var(--text);
            line-height: 1.6;
        }
        %s
        .container {
            max-width: 1000px;
            margin: 0 auto;
            padding: 2rem;
        }
        header {
            text-align: center;
            padding: 3rem 0;
        }
        .logo {
            font-size: 4rem;
            margin-bottom: 1rem;
        }
        h1 {
            font-size: 2.5rem;
            color: var(--primary);
            margin-bottom: 0.5rem;
        }
        .description {
            color: rgba(255,255,255,0.6);
        }
        .status-bar {
            text-align: center;
            padding: 2rem;
            border-radius: 1rem;
            margin: 2rem 0;
            border: 1px solid var(--border);
        }
        .status-bar.operational { background: linear-gradient(135deg, rgba(34,197,94,0.1), rgba(34,197,94,0.05)); }
        .status-bar.degraded { background: linear-gradient(135deg, rgba(245,158,11,0.1), rgba(245,158,11,0.05)); }
        .status-bar.major_outage { background: linear-gradient(135deg, rgba(239,68,68,0.1), rgba(239,68,68,0.05)); }
        .status-indicator {
            width: 12px;
            height: 12px;
            border-radius: 50%%;
            display: inline-block;
            margin-right: 0.5rem;
        }
        .status-operational { background: #22c55e; }
        .status-degraded { background: #f59e0b; }
        .status-dead { background: #ef4444; }
        .status-bar h2 {
            font-size: 1.5rem;
            margin-bottom: 0.5rem;
        }
        .souls-grid {
            display: grid;
            gap: 1rem;
            margin: 2rem 0;
        }
        .soul {
            background: var(--card-bg);
            border: 1px solid var(--border);
            border-radius: 0.75rem;
            padding: 1.5rem;
            display: flex;
            align-items: center;
            justify-content: space-between;
        }
        .soul-name {
            font-weight: 600;
            font-size: 1.1rem;
        }
        .soul-metrics {
            display: flex;
            gap: 1rem;
            color: rgba(255,255,255,0.6);
            font-size: 0.875rem;
        }
        .groups-section {
            margin: 2rem 0;
        }
        .group {
            background: var(--card-bg);
            border: 1px solid var(--border);
            border-radius: 0.75rem;
            padding: 1.5rem;
            margin-bottom: 1rem;
        }
        .group h3 {
            color: var(--primary);
            margin-bottom: 0.5rem;
        }
        .group-souls {
            display: flex;
            gap: 1rem;
            margin-top: 1rem;
        }
        .incidents-section {
            margin: 2rem 0;
        }
        .incidents-section h2 {
            color: var(--primary);
            margin-bottom: 1rem;
        }
        .incident {
            background: var(--card-bg);
            border: 1px solid var(--border);
            border-radius: 0.75rem;
            padding: 1.5rem;
            margin-bottom: 1rem;
        }
        .incident.severity-critical { border-left: 4px solid #ef4444; }
        .incident.severity-high { border-left: 4px solid #f97316; }
        .incident.severity-medium { border-left: 4px solid #f59e0b; }
        .incident.severity-low { border-left: 4px solid #3b82f6; }
        .incident.impact-critical { border-left: 4px solid #ef4444; }
        .incident.impact-major { border-left: 4px solid #f97316; }
        .incident.impact-minor { border-left: 4px solid #f59e0b; }
        .incident.impact-none { border-left: 4px solid #3b82f6; }
        .active-incidents.impact-critical h2 { color: #ef4444; }
        .active-incidents.impact-major h2 { color: #f97316; }
        .active-incidents.impact-minor h2 { color: #f59e0b; }
        .impact-badge, .phase-badge { font-size: 0.75rem; padding: 0.1rem 0.5rem; border-radius: 999px; border: 1px solid var(--border); vertical-align: middle; }
        .incident-meta { display: flex; flex-wrap: wrap; gap: 1rem; font-size: 0.85rem; color: rgba(255,255,255,0.6); }
        .incident-affected { font-size: 0.85rem; margin-top: 0.5rem; }
        .incident-timeline { list-style: none; margin-top: 0.75rem; border-left: 2px solid var(--border); padding-left: 1rem; }
        .incident-timeline li { margin-bottom: 0.75rem; overflow-wrap: anywhere; }
        .incident-timeline time { font-size: 0.8rem; color: rgba(255,255,255,0.5); }
        .incident a, .announcement a { color: var(--accent); }
        .announcement { background: var(--card-bg); border: 1px solid var(--border); border-left: 4px solid #3b82f6; border-radius: 0.75rem; padding: 1.5rem; margin-bottom: 1rem; }
        .announcement.phase-completed { opacity: 0.7; border-left-color: #64748b; }
        .history-window, .history-empty { color: rgba(255,255,255,0.6); margin-bottom: 1rem; }
        .uptime-section {
            margin: 2rem 0;
            text-align: center;
        }
        .uptime-section h2 {
            color: var(--primary);
            margin-bottom: 1rem;
        }
        .uptime-overall {
            margin-bottom: 2rem;
        }
        .uptime-value {
            font-size: 3rem;
            font-weight: 700;
            color: var(--primary);
        }
        .uptime-label {
            display: block;
            color: rgba(255,255,255,0.6);
        }
        .uptime-days {
            display: flex;
            gap: 2px;
            justify-content: center;
            height: 40px;
        }
        .uptime-day {
            flex: 1;
            border-radius: 2px;
            min-width: 4px;
        }
        .day-operational { background: #22c55e; }
        .day-degraded { background: #f59e0b; }
        .day-dead { background: #ef4444; }
        .day-unknown { background: rgba(148, 163, 184, 0.35); }
        footer {
            text-align: center;
            padding: 2rem;
            border-top: 1px solid var(--border);
            color: rgba(255,255,255,0.4);
        }
        .powered-by {
            display: flex;
            align-items: center;
            justify-content: center;
            gap: 0.5rem;
        }
        .updated-at {
            margin-top: 1rem;
            font-size: 0.75rem;
        }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <div class="logo">𓃥</div>
            <h1>%s</h1>
            <p class="description">%s</p>
        </header>

        <div class="status-bar %s">
            <h2><span class="status-indicator %s"></span>%s</h2>
            <p>%s</p>
        </div>

        %s

        <div class="souls-grid">
            <h2 style="color: var(--primary); margin-bottom: 1rem;">Services</h2>
            %s
        </div>

        <div class="groups-section">
            %s
        </div>

        %s

        <footer>
            <div class="powered-by">
                <span>𓃥</span>
                <span>Powered by AnubisWatch</span>
            </div>
            <div class="updated-at">Last updated: %s</div>
        </footer>
    </div>
</body>
</html>`,
		html.EscapeString(page.Name),
		theme.PrimaryColor, theme.BackgroundColor, theme.TextColor, theme.AccentColor,
		theme.FontFamily,
		sanitizedCSS,
		html.EscapeString(page.Name), html.EscapeString(page.Description),
		data.Status.Status, statusClass, html.EscapeString(data.Status.Title), html.EscapeString(data.Status.Description),
		incidentsHTML,
		soulsHTML,
		groupsHTML,
		uptimeHTML,
		data.Status.UpdatedAt.Format(time.RFC3339),
	)
}

// maxSubscribeBodyBytes caps a subscription request: a page ID, an email, a
// webhook URL and a type fit in a few hundred bytes.
const maxSubscribeBodyBytes = 4096

// BadgeHandler serves embeddable status badge (SVG/PNG)
func (h *Handler) BadgeHandler(w http.ResponseWriter, r *http.Request) {
	// Extract page ID from path
	pageID := strings.TrimPrefix(r.URL.Path, "/badge/")
	if pageID == "" {
		http.Error(w, "Page ID required", http.StatusBadRequest)
		return
	}

	// Get page
	page, err := h.repository.GetStatusPageBySlug(pageID)
	if err != nil {
		http.Error(w, "Status page not found", http.StatusNotFound)
		return
	}

	setPageCacheControl(w, page, 60)
	if !h.pageAccessAllowed(r, page) {
		h.requestAuthentication(w, r)
		return
	}

	// Get souls status
	souls := make([]core.SoulStatusInfo, 0, len(page.Souls))
	now := time.Now()
	windows := h.activeMaintenance(now)
	for _, soulID := range page.Souls {
		soul, err := h.repository.GetSoul(soulID)
		if err != nil {
			continue
		}

		judgments, err := h.repository.GetSoulJudgments(soulID, 1)
		var lastJudgment core.Judgment
		if err == nil && len(judgments) > 0 {
			lastJudgment = judgments[0]
		}

		status := soulPageStatus(soul, lastJudgment, windows, now)

		souls = append(souls, core.SoulStatusInfo{
			ID:     soul.ID,
			Name:   soul.Name,
			Status: status,
		})
	}

	// Calculate overall status
	overallStatus := core.CalculateOverallStatus(souls)

	// Determine badge color
	color := statusColorFromStatus(overallStatus.Status)

	// Check format
	format := r.URL.Query().Get("format")
	if format == "json" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		// Client is gone on write failure; nothing actionable remains.
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": overallStatus.Status,
			"color":  color,
		})
		return
	}

	// Serve SVG badge
	w.Header().Set("Content-Type", "image/svg+xml")
	setPageCacheControl(w, page, 60)
	w.Header().Set("Access-Control-Allow-Origin", "*")

	badgeText := "AnubisWatch"
	statusText := strings.ToTitle(string(overallStatus.Status[0])) + overallStatus.Status[1:]

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="140" height="20">
  <linearGradient id="b" x2="0" y2="100%%">
    <stop offset="0" stop-color="#bbb" stop-opacity=".1"/>
    <stop offset="1" stop-opacity=".1"/>
  </linearGradient>
  <mask id="a">
    <rect width="140" height="20" rx="3" fill="#fff"/>
  </mask>
  <g mask="url(#a)">
    <rect width="60" height="20" fill="#555"/>
    <rect x="60" width="80" height="20" fill="#%s"/>
    <rect width="140" height="20" fill="url(#b)"/>
  </g>
  <g fill="#fff" text-anchor="middle" font-family="DejaVu Sans,Verdana,Geneva,sans-serif" font-size="11">
    <text x="30" y="15" fill="#010101" fill-opacity=".3">
      %s
    </text>
    <text x="30" y="14">
      %s
    </text>
    <text x="100" y="15" fill="#010101" fill-opacity=".3">
      %s
    </text>
    <text x="100" y="14">
      %s
    </text>
  </g>
</svg>`, color, badgeText, badgeText, statusText, statusText)

	// Write failure past this point means the client is gone; not actionable.
	_, _ = w.Write([]byte(svg))
}

// WidgetHandler serves an embeddable HTML/JS status badge widget
func (h *Handler) WidgetHandler(w http.ResponseWriter, r *http.Request) {
	pageID := r.URL.Query().Get("page")
	if pageID == "" {
		http.Error(w, "Page ID required", http.StatusBadRequest)
		return
	}

	// Get page
	page, err := h.repository.GetStatusPageBySlug(pageID)
	if err != nil {
		http.Error(w, "Status page not found", http.StatusNotFound)
		return
	}

	setPageCacheControl(w, page, 60)
	if !h.pageAccessAllowed(r, page) {
		h.requestAuthentication(w, r)
		return
	}

	// Get souls status
	souls := make([]core.SoulStatusInfo, 0, len(page.Souls))
	now := time.Now()
	windows := h.activeMaintenance(now)
	for _, soulID := range page.Souls {
		soul, err := h.repository.GetSoul(soulID)
		if err != nil {
			continue
		}

		judgments, err := h.repository.GetSoulJudgments(soulID, 1)
		var lastJudgment core.Judgment
		if err == nil && len(judgments) > 0 {
			lastJudgment = judgments[0]
		}

		status := soulPageStatus(soul, lastJudgment, windows, now)

		souls = append(souls, core.SoulStatusInfo{
			ID:     soul.ID,
			Name:   soul.Name,
			Status: status,
		})
	}

	overallStatus := core.CalculateOverallStatus(souls)

	statusColor := "#" + statusColorFromStatus(overallStatus.Status)

	style := r.URL.Query().Get("style")
	showDetails := style == "detailed"

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	setPageCacheControl(w, page, 60)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")

	if showDetails {
		soulRows := ""
		for _, soul := range souls {
			var dotColor string
			switch soul.Status {
			case "alive", "operational":
				dotColor = "#22c55e"
			case "degraded":
				dotColor = "#f59e0b"
			case "embalmed":
				dotColor = "#3b82f6"
			case "dead", "down":
				dotColor = "#ef4444"
			default:
				dotColor = "#6b7280"
			}
			soulRows += fmt.Sprintf(`<tr>
				<td style="padding:4px 8px;font-size:12px;">%s</td>
				<td style="padding:4px 8px;"><span style="display:inline-block;width:8px;height:8px;border-radius:50%%;background:%s;"></span></td>
			</tr>`, html.EscapeString(soul.Name), dotColor)
		}

		html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;font-size:13px;background:transparent}
.widget{border:1px solid #e5e7eb;border-radius:8px;overflow:hidden;background:#fff;min-width:200px}
.header{display:flex;align-items:center;gap:6px;padding:8px 12px;background:#f9fafb;border-bottom:1px solid #e5e7eb}
.dot{width:10px;height:10px;border-radius:50%%;background:%s;flex-shrink:0}
.title{font-weight:600;color:#111827;flex:1}
.status{font-weight:500;color:%s;font-size:12px}
table{width:100%%;border-collapse:collapse}
tbody tr:hover{background:#f3f4f6}
.footer{padding:4px 8px;text-align:right;font-size:10px;color:#9ca3af;border-top:1px solid #e5e7eb}
.footer a{color:#6366f1;text-decoration:none}
</style>
</head>
<body>
<div class="widget">
  <div class="header">
    <span class="dot"></span>
    <span class="title">%s</span>
    <span class="status">%s</span>
  </div>
  <table>
    <tbody>%s</tbody>
  </table>
  <div class="footer">Powered by <a href="https://anubiswatch.com" target="_blank">AnubisWatch</a></div>
</div>
</body>
</html>`, statusColor, statusColor, html.EscapeString(page.Name), strings.ToTitle(string(overallStatus.Status[0]))+overallStatus.Status[1:], soulRows)

		// Client is gone on write failure; nothing actionable remains.
		_, _ = w.Write([]byte(html))
	} else {
		// Compact badge style
		html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:transparent}
.badge{display:inline-flex;align-items:center;gap:6px;padding:4px 12px;border-radius:20px;background:%s;color:#fff;font-size:12px;font-weight:500;cursor:pointer;text-decoration:none;transition:opacity .2s}
.badge:hover{opacity:.85}
.dot{width:8px;height:8px;border-radius:50%%;background:#fff;animation:pulse 2s infinite}
@keyframes pulse{0%%,100%%{opacity:1}50%%{opacity:.4}}
</style>
</head>
<body>
<a class="badge" href="/status/%s" target="_blank">
  <span class="dot"></span>
  %s: %s
</a>
</body>
</html>`, statusColor, html.EscapeString(pageID), html.EscapeString(page.Name), strings.ToTitle(string(overallStatus.Status[0]))+overallStatus.Status[1:])

		// Client is gone on write failure; nothing actionable remains.
		_, _ = w.Write([]byte(html))
	}
}

// RSSFeedHandler serves RSS feed for status page updates
func (h *Handler) RSSFeedHandler(w http.ResponseWriter, r *http.Request) {
	pageID := strings.TrimPrefix(r.URL.Path, "/status/")
	pageID = strings.TrimSuffix(pageID, "/feed.xml")

	page, err := h.repository.GetStatusPageBySlug(pageID)
	if err != nil {
		http.Error(w, "Status page not found", http.StatusNotFound)
		return
	}

	setPageCacheControl(w, page, 300)
	if !h.pageAccessAllowed(r, page) {
		h.requestAuthentication(w, r)
		return
	}

	// Get incidents for feed
	incidents, err := h.repository.GetIncidentsByPage(page.ID)
	if err != nil {
		incidents = []core.Incident{}
	}

	feedBaseURL := "https://" + page.CustomDomain
	if page.CustomDomain == "" {
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		feedBaseURL = fmt.Sprintf("%s://%s/status/%s", scheme, r.Host, pageID)
	}

	// Build RSS feed
	rss := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>%s - Status Updates</title>
    <description>Status updates for %s</description>
    <link>%s</link>
    <lastBuildDate>%s</lastBuildDate>
    <generator>AnubisWatch</generator>
`, html.EscapeString(page.Name), html.EscapeString(page.Name), html.EscapeString(feedBaseURL), time.Now().UTC().Format(time.RFC1123Z))

	for _, inc := range incidents {
		// Get soul name for context
		soulName := inc.SoulID
		if soul, err := h.repository.GetSoul(inc.SoulID); err == nil && soul != nil {
			soulName = soul.Name
		}

		rss += fmt.Sprintf(`    <item>
      <title>%s - %s</title>
      <description>Incident on %s: %s</description>
      <pubDate>%s</pubDate>
      <link>%s/incidents/%s</link>
      <guid isPermaLink="false">%s</guid>
    </item>
`, html.EscapeString(soulName), html.EscapeString(string(inc.Status)), html.EscapeString(soulName), html.EscapeString(string(inc.Status)),
			inc.StartedAt.Format(time.RFC1123Z),
			html.EscapeString(feedBaseURL), html.EscapeString(inc.ID), html.EscapeString(inc.ID))
	}

	// The page's own incidents and scheduled maintenance (incidents.go).
	rss += rssPageItems(page, h.announcementsWithWindows(page), feedBaseURL, time.Now())

	rss += `  </channel>
</rss>`

	w.Header().Set("Content-Type", "application/rss+xml")
	setPageCacheControl(w, page, 300)
	// Client is gone on write failure; nothing actionable remains.
	_, _ = w.Write([]byte(rss))
}

// Router returns the status page router
func (h *Handler) Router() http.Handler {
	mux := http.NewServeMux()

	// Main status page handler
	mux.HandleFunc("/status/", func(w http.ResponseWriter, r *http.Request) {
		// Subscription endpoints
		if h.serveSubscriptionRoute(w, r) {
			return
		}
		// Check if this is an RSS feed request
		if strings.HasSuffix(r.URL.Path, "/feed.xml") {
			h.RSSFeedHandler(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})

	// Badge endpoint (embeddable status badge)
	mux.HandleFunc("/badge/", h.BadgeHandler)

	// Widget endpoint (embeddable HTML status widget)
	mux.HandleFunc("/widget", h.WidgetHandler)

	return mux
}

// statusColorFromStatus returns the hex color for a given overall status string.
func statusColorFromStatus(status string) string {
	switch status {
	case "operational":
		return "22c55e"
	case "degraded":
		return "f59e0b"
	case "down", "major_outage":
		return "ef4444"
	default:
		return "6b7280"
	}
}

// sanitizeCSS removes dangerous patterns from user-supplied CSS that could be
// used for XSS attacks within a <style> block, including:
// - Breaking out of the style block (</style>)
// - Expression() / behaviors (IE)
// - javascript: / data: URLs in background/image properties
// - @import with absolute URLs (could bypass CSP)
// It returns a safe subset of CSS or empty string if the input is unsafe.
func sanitizeCSS(css string) string {
	if css == "" {
		return ""
	}
	// Block closing style tag (case-insensitive via bytes, no regex needed)
	if strings.Contains(strings.ToLower(css), "</style") {
		return ""
	}
	// Block expression(), behavior(), -moz-binding (IE/old Firefox XSS vectors)
	dangerous := []string{
		"expression(", "behavior(", "-moz-binding",
		"javascript:", "data:", "vbscript:",
		"url(\"javascript:", "url('javascript:", "url(javascript:",
		"url(\"data:", "url('data:", "url(data:",
		"@import", // prevents bypassing CSP via absolute URL
	}
	lower := strings.ToLower(css)
	for _, pattern := range dangerous {
		if strings.Contains(lower, pattern) {
			return ""
		}
	}
	return css
}
