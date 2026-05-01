package statuspage

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/acme"
	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Handler serves public status pages
// The Public Temple where citizens view the health of services
//
//go:generate echo "The Judgment Never Sleeps"
type Handler struct {
	repository    Repository
	acmeManager   *acme.Manager
	templateCache map[string]*Template
	defaultTheme  core.StatusPageTheme
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
		html.EscapeString(page.Name),
		theme.PrimaryColor, theme.BackgroundColor, theme.TextColor, theme.AccentColor,
		theme.FontFamily,
		html.EscapeString(page.Name),
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

		// Calculate uptime from 30-day history
		uptimePercent := 100.0
		history, err := h.repository.GetUptimeHistory(soulID, 30)
		if err == nil && len(history) > 0 {
			totalHours := 0.0
			upHours := 0.0
			for _, day := range history {
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
				html.EscapeString(string(inc.Severity)), html.EscapeString(inc.Title), html.EscapeString(inc.Description),
				inc.StartedAt.Format(time.RFC3339), html.EscapeString(string(inc.Status)),
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
		html.EscapeString(page.Name),
		theme.PrimaryColor, theme.BackgroundColor, theme.TextColor, theme.AccentColor,
		theme.FontFamily,
		sanitizedCSS,
		html.EscapeString(page.Name), html.EscapeString(page.Description),
		data.Status.Status, statusClass, html.EscapeString(data.Status.Title), html.EscapeString(data.Status.Description),
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
		Type       string `json:"type"` // email, webhook, rss
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Validate
	if req.PageID == "" {
		http.Error(w, "Page ID required", http.StatusBadRequest)
		return
	}

	if req.Type == "" {
		req.Type = "email"
	}

	if req.Type == "email" && req.Email == "" {
		http.Error(w, "Email required for email subscriptions", http.StatusBadRequest)
		return
	}

	if req.Type == "webhook" && req.WebhookURL == "" {
		http.Error(w, "Webhook URL required for webhook subscriptions", http.StatusBadRequest)
		return
	}

	// Create subscription
	sub := &core.StatusPageSubscription{
		ID:           core.GenerateID(),
		PageID:       req.PageID,
		Email:        req.Email,
		WebhookURL:   req.WebhookURL,
		Type:         req.Type,
		SubscribedAt: time.Now().UTC(),
		Confirmed:    false, // Require email confirmation
	}

	// Store subscription
	if h.repository != nil {
		if err := h.repository.SaveSubscription(sub); err != nil {
			http.Error(w, "Failed to save subscription", http.StatusInternalServerError)
			return
		}
	}

	// Return success
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":               true,
		"message":               "Subscription created. Please confirm your subscription.",
		"subscription_id":       sub.ID,
		"confirmation_required": req.Type == "email",
	})
}

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

	// Get souls status
	souls := make([]core.SoulStatusInfo, 0, len(page.Souls))
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

		status := "unknown"
		if lastJudgment.ID != "" {
			status = string(lastJudgment.Status)
		}

		souls = append(souls, core.SoulStatusInfo{
			ID:     soul.ID,
			Name:   soul.Name,
			Status: status,
		})
	}

	// Calculate overall status
	overallStatus := core.CalculateOverallStatus(souls)

	// Determine badge color
	var color string
	switch overallStatus.Status {
	case "operational":
		color = "22c55e" // Green
	case "degraded":
		color = "f59e0b" // Amber
	case "down", "major_outage":
		color = "ef4444" // Red
	default:
		color = "6b7280" // Gray
	}

	// Check format
	format := r.URL.Query().Get("format")
	if format == "json" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": overallStatus.Status,
			"color":  color,
		})
		return
	}

	// Serve SVG badge
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=60")
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

	w.Write([]byte(svg))
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

	// Get souls status
	souls := make([]core.SoulStatusInfo, 0, len(page.Souls))
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

		status := "unknown"
		if lastJudgment.ID != "" {
			status = string(lastJudgment.Status)
		}

		souls = append(souls, core.SoulStatusInfo{
			ID:     soul.ID,
			Name:   soul.Name,
			Status: status,
		})
	}

	overallStatus := core.CalculateOverallStatus(souls)

	var statusColor string
	switch overallStatus.Status {
	case "operational":
		statusColor = "#22c55e"
	case "degraded":
		statusColor = "#f59e0b"
	case "down", "major_outage":
		statusColor = "#ef4444"
	default:
		statusColor = "#6b7280"
	}

	style := r.URL.Query().Get("style")
	showDetails := style == "detailed"

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")

	if showDetails {
		soulRows := ""
		for _, soul := range souls {
			dotColor := statusColor
			switch soul.Status {
			case "alive", "operational":
				dotColor = "#22c55e"
			case "degraded":
				dotColor = "#f59e0b"
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

		w.Write([]byte(html))
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

		w.Write([]byte(html))
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

	// Get incidents for feed
	incidents, err := h.repository.GetIncidentsByPage(page.ID)
	if err != nil {
		incidents = []core.Incident{}
	}

	// Build RSS feed
	rss := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>%s - Status Updates</title>
    <description>Status updates for %s</description>
    <link>https://%s</link>
    <lastBuildDate>%s</lastBuildDate>
    <generator>AnubisWatch</generator>
`, html.EscapeString(page.Name), html.EscapeString(page.Name), html.EscapeString(page.CustomDomain), time.Now().UTC().Format(time.RFC1123Z))

	for _, inc := range incidents {
		// Get soul name for context
		soulName := inc.SoulID
		if soul, err := h.repository.GetSoul(inc.SoulID); err == nil && soul != nil {
			soulName = soul.Name
		}

		rss += fmt.Sprintf(`    <item>
      <title>%s - %s</title>
      <description><![CDATA[Incident on %s: %s]]></description>
      <pubDate>%s</pubDate>
      <link>https://%s/incidents/%s</link>
      <guid>%s</guid>
    </item>
`, html.EscapeString(soulName), html.EscapeString(string(inc.Status)), html.EscapeString(soulName), html.EscapeString(string(inc.Status)),
			inc.StartedAt.Format(time.RFC1123Z),
			html.EscapeString(page.CustomDomain), html.EscapeString(inc.ID), html.EscapeString(inc.ID))
	}

	rss += `  </channel>
</rss>`

	w.Header().Set("Content-Type", "application/rss+xml")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Write([]byte(rss))
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

	// ACME challenge handler for Let's Encrypt
	if h.acmeManager != nil {
		mux.Handle("/.well-known/acme-challenge/", h.acmeManager.ChallengeHandler())
	}

	return mux
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
