package core

import (
	"time"
)

// StatusPage represents a public status page
// The public temple where citizens view the health of services
type StatusPage struct {
	ID            string            `json:"id" yaml:"id"`
	WorkspaceID   string            `json:"workspace_id" yaml:"workspace_id"`
	Name          string            `json:"name" yaml:"name"`
	Slug          string            `json:"slug" yaml:"slug"`
	Description   string            `json:"description" yaml:"description"`
	LogoURL       string            `json:"logo_url" yaml:"logo_url"`
	FaviconURL    string            `json:"favicon_url" yaml:"favicon_url"`
	CustomDomain  string            `json:"custom_domain" yaml:"custom_domain"`
	Theme         StatusPageTheme   `json:"theme" yaml:"theme"`
	Visibility    PageVisibility    `json:"visibility" yaml:"visibility"`
	Enabled       bool              `json:"enabled" yaml:"enabled"`
	Souls         []string          `json:"souls" yaml:"souls"` // Soul IDs to display
	Groups        []StatusGroup     `json:"groups" yaml:"groups"`
	Incidents     []StatusIncident  `json:"incidents" yaml:"incidents"`
	UptimeDays    int               `json:"uptime_days" yaml:"uptime_days"` // Days to show
	CreatedAt     time.Time         `json:"created_at" yaml:"-"`
	UpdatedAt     time.Time         `json:"updated_at" yaml:"-"`
}

// StatusPageTheme contains theme configuration
type StatusPageTheme struct {
	PrimaryColor    string `json:"primary_color" yaml:"primary_color"`
	BackgroundColor string `json:"background_color" yaml:"background_color"`
	TextColor       string `json:"text_color" yaml:"text_color"`
	AccentColor     string `json:"accent_color" yaml:"accent_color"`
	FontFamily      string `json:"font_family" yaml:"font_family"`
	CustomCSS       string `json:"custom_css" yaml:"custom_css"`
}

// PageVisibility controls who can see the page
type PageVisibility string

const (
	VisibilityPublic    PageVisibility = "public"    // Anyone can see
	VisibilityPrivate   PageVisibility = "private"   // Authentication required
	VisibilityProtected PageVisibility = "protected" // Password protected
)

// StatusGroup groups souls by category
type StatusGroup struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	SoulIDs     []string `json:"soul_ids"`
	Order       int      `json:"order"`
}

// IncidentStatus represents incident status for status pages and alert incidents
type IncidentStatus string

const (
	IncidentOpen       IncidentStatus = "open"
	IncidentAcked      IncidentStatus = "acknowledged"
	IncidentResolved   IncidentStatus = "resolved"
	// Legacy aliases for status page compatibility
	StatusOngoing       IncidentStatus = "ongoing"
	StatusInvestigating IncidentStatus = "investigating"
	StatusIdentified    IncidentStatus = "identified"
	StatusMonitoring    IncidentStatus = "monitoring"
	StatusResolved      IncidentStatus = "resolved"
)

// StatusIncident is an incident displayed on the status page
type StatusIncident struct {
	ID          string             `json:"id"`
	Title       string             `json:"title"`
	Description string             `json:"description"`
	Status      IncidentStatus     `json:"status"`
	Severity    Severity           `json:"severity"`
	AffectedSouls []string         `json:"affected_souls"`
	StartedAt   time.Time          `json:"started_at"`
	ResolvedAt  *time.Time         `json:"resolved_at,omitempty"`
	Updates     []IncidentUpdate   `json:"updates"`
}

// IncidentUpdate is a status update for an incident
type IncidentUpdate struct {
	ID        string    `json:"id"`
	Message   string    `json:"message"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

// StatusPageData is the data exposed via status page API
type StatusPageData struct {
	Page      StatusPageInfo     `json:"page"`
	Status    OverallStatus      `json:"status"`
	Souls     []SoulStatusInfo   `json:"souls"`
	Groups    []GroupStatusInfo  `json:"groups"`
	Incidents []StatusIncident   `json:"incidents"`
	Uptime    UptimeData         `json:"uptime"`
}

// StatusPageInfo basic page info
type StatusPageInfo struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	LogoURL      string `json:"logo_url"`
	CustomDomain string `json:"custom_domain,omitempty"`
}

// OverallStatus represents the overall system status
type OverallStatus struct {
	Status      string    `json:"status"` // operational, degraded, major_outage
	Title       string    `json:"title"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SoulStatusInfo is soul info for status page
type SoulStatusInfo struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Status         string    `json:"status"`
	UptimePercent  float64   `json:"uptime_percent"`
	LastCheckedAt  time.Time `json:"last_checked_at"`
	ResponseTime   float64   `json:"response_time_ms"`
}

// GroupStatusInfo is group info for status page
type GroupStatusInfo struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Souls       []SoulStatusInfo `json:"souls"`
}

// UptimeData contains historical uptime
type UptimeData struct {
	Days        []UptimeDay `json:"days"`
	Overall     float64     `json:"overall_percent"`
}

// UptimeDay is a single day's uptime
type UptimeDay struct {
	Date   string  `json:"date"` // YYYY-MM-DD
	Status string  `json:"status"`
	Uptime float64 `json:"uptime_percent"`
}

// CalculateOverallStatus determines overall status from souls
func CalculateOverallStatus(souls []SoulStatusInfo) OverallStatus {
	dead := 0
	degraded := 0

	for _, soul := range souls {
		switch soul.Status {
		case "dead":
			dead++
		case "degraded":
			degraded++
		}
	}

	if dead > 0 {
		return OverallStatus{
			Status:      "major_outage",
			Title:       "Major Outage",
			Description: "Some systems are experiencing issues",
			UpdatedAt:   time.Now().UTC(),
		}
	}

	if degraded > 0 {
		return OverallStatus{
			Status:      "degraded",
			Title:       "Degraded Performance",
			Description: "Some systems are running slower than usual",
			UpdatedAt:   time.Now().UTC(),
		}
	}

	return OverallStatus{
		Status:      "operational",
		Title:       "All Systems Operational",
		Description: "Everything is running smoothly",
		UpdatedAt:   time.Now().UTC(),
	}
}

// GetDefaultTheme returns the default Egyptian theme
func GetDefaultTheme() StatusPageTheme {
	return StatusPageTheme{
		PrimaryColor:    "#fbbf24", // Amber/Gold
		BackgroundColor: "#0f172a", // Slate 900
		TextColor:       "#f8fafc", // Slate 50
		AccentColor:     "#14b8a6", // Teal
		FontFamily:      "system-ui, -apple-system, sans-serif",
	}
}

// StatusPageSubscription represents a subscription to updates
type StatusPageSubscription struct {
	ID          string    `json:"id"`
	PageID      string    `json:"page_id"`
	Email       string    `json:"email"`
	WebhookURL  string    `json:"webhook_url"`
	SubscribedAt time.Time `json:"subscribed_at"`
}
