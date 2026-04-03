package statuspage

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/acme"
	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Handler serves public status pages
// The Public Temple where citizens view the health of services
//go:generate echo "The Judgment Never Sleeps"
type Handler struct {
	repository     Repository
	acmeManager    *acme.Manager
	templateCache  map[string]*Template
	cacheMu        sync.RWMutex
	defaultTheme   core.StatusPageTheme
}

// Repository defines the storage interface for status pages
type Repository interface {
	GetStatusPageByDomain(domain string) (*core.StatusPage, error)
	GetStatusPageBySlug(slug string) (*core.StatusPage, error)
	GetSoul(id string) (*core.Soul, error)
	GetSoulJudgments(soulID string, limit int) ([]core.Judgment, error)
	GetIncidentsByPage(pageID string) ([]core.Incident, error)
	GetUptimeHistory(soulID string, days int) ([]core.UptimeDay, error)
}

// Template represents a cached status page template
type Template struct {
	HTML      string
	UpdatedAt time.Time
}

// NewHandler creates a new status page handler
func NewHandler(repo Repository, acmeMgr *acme.Manager) *Handler {
	return &Handler{
		repository:    repo,
		acmeManager:   acmeMgr,
		templateCache: make(map[string]*Template),
		defaultTheme:  core.GetDefaultTheme(),
	}
}

// ServeHTTP handles status page requests
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Determine which status page to serve
	domain := r.Host
	if colonIdx := strings.LastIndex(domain, ":"); colonIdx != -1 {
		domain = domain[:colonIdx]
	}

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

	// Check visibility
	if page.Visibility == core.VisibilityPrivate {
		// Require authentication
		if !h.isAuthenticated(r, page) {
			h.requestAuthentication(w, r)
			return
		}
	}

	if page.Visibility == core.VisibilityProtected {
		// Check password protection
		if !h.checkPasswordProtection(r, page) {
			h.showPasswordForm(w, r, page)
			return
		}
	}

	// Build status page data
	data, err := h.buildStatusPageData(page)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Serve appropriate format
	acceptHeader := r.Header.Get("Accept")
	if strings.Contains(acceptHeader, "application/json") || r.URL.Query().Get("format") == "json" {
		h.serveJSON(w, r, data)
		return
	}

	h.serveHTML(w, r, page, data)
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

// isAuthenticated checks if request is authenticated for private pages
func (h *Handler) isAuthenticated(r *http.Request, page *core.StatusPage) bool {
	// Check session cookie or API key
	sessionCookie, err := r.Cookie("anubis_session")
	if err == nil && sessionCookie.Value != "" {
		// Validate session (implementation depends on auth system)
		return h.validateSession(sessionCookie.Value, page.WorkspaceID)
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
	// Implementation would validate against session store
	// Placeholder - actual implementation would check Feather or session cache
	return false
}

// validateAPIKey validates an API key
func (h *Handler) validateAPIKey(key string, workspaceID string) bool {
	// Implementation would validate against API key store
	return false
}

// requestAuthentication sends authentication challenge
func (h *Handler) requestAuthentication(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(w, "Authentication required", http.StatusUnauthorized)
}

// checkPasswordProtection checks password protection
func (h *Handler) checkPasswordProtection(r *http.Request, page *core.StatusPage) bool {
	// Check password cookie
	passCookie, err := r.Cookie("anubis_page_pass")
	if err == nil && passCookie.Value != "" {
		return h.verifyPagePassword(passCookie.Value, page)
	}

	// Check form submission
	password := r.URL.Query().Get("password")
	if password != "" {
		return h.verifyPagePassword(password, page)
	}

	return false
}

// verifyPagePassword verifies the page password
func (h *Handler) verifyPagePassword(password string, page *core.StatusPage) bool {
	// Hash and compare against stored hash
	// Placeholder - actual implementation would check hashed password
	return false
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
        <form method="GET" action="">
            <input type="password" name="password" placeholder="Enter password" required autofocus>
            <button type="submit">Access Status Page</button>
        </form>
        <div class="powered-by">Powered by AnubisWatch</div>
    </div>
</body>
</html>`,
		page.Name,
		theme.PrimaryColor, theme.BackgroundColor, theme.TextColor, theme.AccentColor,
		theme.FontFamily,
		page.Name,
	)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// buildStatusPageData builds the data for a status page
func (h *Handler) buildStatusPageData(page *core.StatusPage) (*core.StatusPageData, error) {
	// Get souls status
	souls := make([]core.SoulStatusInfo, 0, len(page.Souls))
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

		// Determine status from last judgment
		status := "unknown"
		if lastJudgment.ID != "" {
			status = string(lastJudgment.Status)
		}

		soulInfo := core.SoulStatusInfo{
			ID:            soul.ID,
			Name:          soul.Name,
			Status:        status,
			UptimePercent: 100.0, // TODO: Calculate from history
			LastCheckedAt: lastJudgment.Timestamp,
			ResponseTime:  float64(lastJudgment.Duration.Milliseconds()),
		}
		souls = append(souls, soulInfo)
	}

	// Calculate overall status
	overallStatus := core.CalculateOverallStatus(souls)

	// Get groups with soul data
	groups := make([]core.GroupStatusInfo, 0, len(page.Groups))
	for _, group := range page.Groups {
		groupSouls := make([]core.SoulStatusInfo, 0)
		for _, soulID := range group.SoulIDs {
			for _, soul := range souls {
				if soul.ID == soulID {
					groupSouls = append(groupSouls, soul)
					break
				}
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
	}, nil
}

// convertIncidentUpdates converts internal updates to status updates
func convertIncidentUpdates(updates []core.IncidentUpdate) []core.IncidentUpdate {
	result := make([]core.IncidentUpdate, len(updates))
	for i, u := range updates {
		result[i] = core.IncidentUpdate{
			ID:        u.ID,
			Message:   u.Message,
			Status:    u.Status,
			UpdatedAt: u.UpdatedAt,
		}
	}
	return result
}

// buildUptimeData builds uptime data for the page
func (h *Handler) buildUptimeData(page *core.StatusPage, souls []core.SoulStatusInfo) core.UptimeData {
	days := make([]core.UptimeDay, 0, page.UptimeDays)

	for _, soul := range souls {
		history, err := h.repository.GetUptimeHistory(soul.ID, page.UptimeDays)
		if err != nil {
			continue
		}
		for _, day := range history {
			days = append(days, day)
		}
	}

	// Calculate overall uptime percentage
	var totalUptime float64
	for _, day := range days {
		totalUptime += day.Uptime
	}
	overallPercent := 0.0
	if len(days) > 0 {
		overallPercent = totalUptime / float64(len(days))
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
	json.NewEncoder(w).Encode(data)
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
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.Write([]byte(html))
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
			soulClass, soul.Name, soulClass, soul.Status, soul.ResponseTime, soul.UptimePercent,
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
				soulClass, soulClass, soul.Name,
			)
		}
		groupsHTML += fmt.Sprintf(`
		<div class="group">
			<h3>%s</h3>
			<p>%s</p>
			<div class="group-souls">%s</div>
		</div>`,
			group.Name, group.Description, groupSouls,
		)
	}

	// Build incidents
	incidentsHTML := ""
	if len(data.Incidents) > 0 {
		incidentsHTML = `<div class="incidents-section"><h2>Active Incidents</h2>`
		for _, inc := range data.Incidents {
			incidentsHTML += fmt.Sprintf(`
			<div class="incident severity-%s">
				<h3>%s</h3>
				<p>%s</p>
				<div class="incident-meta">
					<span>Started: %s</span>
					<span>Status: %s</span>
				</div>
			</div>`,
				inc.Severity, inc.Title, inc.Description,
				inc.StartedAt.Format(time.RFC3339), inc.Status,
			)
		}
		incidentsHTML += "</div>"
	}

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
		page.Name,
		theme.PrimaryColor, theme.BackgroundColor, theme.TextColor, theme.AccentColor,
		theme.FontFamily,
		theme.CustomCSS,
		page.Name, page.Description,
		data.Status.Status, statusClass, data.Status.Title, data.Status.Description,
		incidentsHTML,
		soulsHTML,
		groupsHTML,
		incidentsHTML,
		uptimeHTML,
		data.Status.UpdatedAt.Format(time.RFC3339),
	)
}

// SubscribeHandler handles subscription requests
func (h *Handler) SubscribeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request
	var req struct {
		PageID     string `json:"page_id"`
		Email      string `json:"email"`
		WebhookURL string `json:"webhook_url,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Validate
	if req.PageID == "" || (req.Email == "" && req.WebhookURL == "") {
		http.Error(w, "Page ID and email or webhook required", http.StatusBadRequest)
		return
	}

	// Create subscription
	sub := &core.StatusPageSubscription{
		ID:           core.GenerateID(),
		PageID:       req.PageID,
		Email:        req.Email,
		WebhookURL:   req.WebhookURL,
		SubscribedAt: time.Now().UTC(),
	}

	// Store subscription (implementation would save to repository)
	// ...

	// Return success
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"message":       "Subscription created",
		"subscription_id": sub.ID,
	})
}

// Router returns the status page router
func (h *Handler) Router() http.Handler {
	mux := http.NewServeMux()

	// Main status page handler
	mux.HandleFunc("/status/", func(w http.ResponseWriter, r *http.Request) {
		// Check if this is a subscription request
		if r.URL.Path == "/status/subscribe" {
			h.SubscribeHandler(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})

	// ACME challenge handler
	mux.Handle("/.well-known/acme-challenge/", h.acmeManager.ChallengeHandler())

	return mux
}
