package core

import (
	"time"
)

// Workspace represents a multi-tenant workspace
// Each workspace is like a separate necropolis
type Workspace struct {
	ID          string            `json:"id" yaml:"id"`
	Name        string            `json:"name" yaml:"name"`
	Slug        string            `json:"slug" yaml:"slug"` // URL-friendly identifier
	Description string            `json:"description" yaml:"description"`
	OwnerID     string            `json:"owner_id" yaml:"owner_id"`
	Settings    WorkspaceSettings `json:"settings" yaml:"settings"`
	Quotas      QuotaConfig       `json:"quotas" yaml:"quotas"`
	Features    FeatureFlags      `json:"features" yaml:"features"`
	Status      WorkspaceStatus   `json:"status" yaml:"status"`
	CreatedAt   time.Time         `json:"created_at" yaml:"-"`
	UpdatedAt   time.Time         `json:"updated_at" yaml:"-"`
	DeletedAt   *time.Time        `json:"deleted_at,omitempty" yaml:"-"`
}

// WorkspaceSettings contains workspace-specific settings
type WorkspaceSettings struct {
	Timezone        string            `json:"timezone" yaml:"timezone"`
	DateFormat      string            `json:"date_format" yaml:"date_format"`
	StatusPageTheme string            `json:"status_page_theme" yaml:"status_page_theme"`
	Branding        WorkspaceBranding `json:"branding" yaml:"branding"`
	Notifications   NotificationPrefs `json:"notifications" yaml:"notifications"`
}

// WorkspaceBranding contains custom branding settings
type WorkspaceBranding struct {
	LogoURL       string `json:"logo_url" yaml:"logo_url"`
	FaviconURL    string `json:"favicon_url" yaml:"favicon_url"`
	PrimaryColor  string `json:"primary_color" yaml:"primary_color"`
	AccentColor   string `json:"accent_color" yaml:"accent_color"`
	CustomCSS     string `json:"custom_css" yaml:"custom_css"`
	HidePoweredBy bool   `json:"hide_powered_by" yaml:"hide_powered_by"`
}

// NotificationPrefs contains notification preferences
type NotificationPrefs struct {
	DigestEnabled    bool     `json:"digest_enabled" yaml:"digest_enabled"`
	DigestFrequency  string   `json:"digest_frequency" yaml:"digest_frequency"` // hourly, daily, weekly
	AlertThreshold   int      `json:"alert_threshold" yaml:"alert_threshold"`   // min severity level
	ExcludedChannels []string `json:"excluded_channels" yaml:"excluded_channels"`
}

// FeatureFlags controls feature availability
type FeatureFlags struct {
	StatusPage     bool `json:"status_page" yaml:"status_page"`
	ACME           bool `json:"acme" yaml:"acme"`
	MCP            bool `json:"mcp" yaml:"mcp"`
	AdvancedAlerts bool `json:"advanced_alerts" yaml:"advanced_alerts"`
	SSO            bool `json:"sso" yaml:"sso"`
}

// WorkspaceStatus represents workspace lifecycle state
type WorkspaceStatus string

const (
	WorkspaceActive    WorkspaceStatus = "active"
	WorkspaceSuspended WorkspaceStatus = "suspended"
	WorkspaceTrial     WorkspaceStatus = "trial"
	WorkspaceDeleted   WorkspaceStatus = "deleted"
)

// Member represents a workspace member
type Member struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspace_id"`
	UserID      string     `json:"user_id"`
	Role        MemberRole `json:"role"`
	JoinedAt    time.Time  `json:"joined_at"`
	LastActive  time.Time  `json:"last_active"`
}

// MemberRole defines permission levels
type MemberRole string

const (
	RoleOwner  MemberRole = "owner"  // Full access
	RoleAdmin  MemberRole = "admin"  // Manage souls, channels, members
	RoleEditor MemberRole = "editor" // Manage souls only
	RoleViewer MemberRole = "viewer" // Read-only access
	RoleAPI    MemberRole = "api"    // API-only access
)

// Can checks if role has permission
func (r MemberRole) Can(permission string) bool {
	perms := map[MemberRole][]string{
		RoleOwner:  {"*"},
		RoleAdmin:  {"souls:*", "channels:*", "rules:*", "members:*", "settings:read", "settings:write"},
		RoleEditor: {"souls:*", "channels:read", "rules:read"},
		RoleViewer: {"souls:read", "judgments:read", "channels:read", "rules:read"},
		RoleAPI:    {"souls:*", "judgments:read", "api:*"},
	}

	rolePerms, ok := perms[r]
	if !ok {
		return false
	}

	for _, p := range rolePerms {
		if p == "*" || p == permission {
			return true
		}
	}
	return false
}

// WorkspaceStats contains workspace statistics
type WorkspaceStats struct {
	WorkspaceID      string    `json:"workspace_id"`
	TotalSouls       int       `json:"total_souls"`
	HealthySouls     int       `json:"healthy_souls"`
	DegradedSouls    int       `json:"degraded_souls"`
	DeadSouls        int       `json:"dead_souls"`
	TotalJudgments   int64     `json:"total_judgments"`
	FailedJudgments  int64     `json:"failed_judgments"`
	TotalIncidents   int       `json:"total_incidents"`
	ActiveIncidents  int       `json:"active_incidents"`
	AvgUptimePercent float64   `json:"avg_uptime_percent"`
	AvgLatency       float64   `json:"avg_latency_ms"`
	PeriodStart      time.Time `json:"period_start"`
	PeriodEnd        time.Time `json:"period_end"`
}

// NamespaceKey returns the storage key prefix for a workspace
func (w *Workspace) NamespaceKey(key string) string {
	if w == nil || w.ID == "" {
		return key
	}
	return w.ID + "/" + key
}

// ValidateSlug validates workspace slug
func ValidateSlug(slug string) error {
	if slug == "" {
		return &ValidationError{Field: "slug", Message: "slug is required"}
	}
	if len(slug) < 3 || len(slug) > 63 {
		return &ValidationError{Field: "slug", Message: "slug must be 3-63 characters"}
	}
	// Check valid characters (lowercase, numbers, hyphens)
	for _, c := range slug {
		isLower := c >= 'a' && c <= 'z'
		isDigit := c >= '0' && c <= '9'
		isHyphen := c == '-'
		if !isLower && !isDigit && !isHyphen {
			return &ValidationError{Field: "slug", Message: "slug must contain only lowercase letters, numbers, and hyphens"}
		}
	}
	return nil
}

// Reserved slugs that cannot be used
var ReservedSlugs = []string{
	"admin", "api", "auth", "www", "app", "dashboard",
	"status", "health", "metrics", "mcp", "grpc", "ws",
}

// IsReservedSlug checks if slug is reserved
func IsReservedSlug(slug string) bool {
	for _, r := range ReservedSlugs {
		if r == slug {
			return true
		}
	}
	return false
}
