package core

import (
	"crypto/tls"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// VerdictsConfig holds alert rules configuration
type VerdictsConfig struct {
	Rules      []AlertRule        `json:"rules" yaml:"rules"`
	Escalation []EscalationPolicy `json:"escalation,omitempty" yaml:"escalation,omitempty"`
}

// EscalationPolicy defines multi-stage escalation
type EscalationPolicy struct {
	Name   string            `json:"name" yaml:"name"`
	Stages []EscalationStage `json:"stages" yaml:"stages"`
}

// EscalationStage defines a single escalation stage
type EscalationStage struct {
	Wait      Duration `json:"wait" yaml:"wait"`
	Channels  []string `json:"channels" yaml:"channels"`
	Condition string   `json:"condition" yaml:"condition"` // not_acknowledged, not_resolved
}

// LoggingConfig defines logging settings
type LoggingConfig struct {
	Level  string `json:"level" yaml:"level"`   // debug, info, warn, error
	Format string `json:"format" yaml:"format"` // json, text
	Output string `json:"output" yaml:"output"` // stdout, file
	File   string `json:"file" yaml:"file"`     // log file path (if output=file)
	// MaxSizeMB rotates the log file once it reaches this size (default 100).
	MaxSizeMB int `json:"max_size_mb" yaml:"max_size_mb"`
	// MaxBackups is how many rotated files (anubis.log.1 …) are kept (default 5).
	MaxBackups int `json:"max_backups" yaml:"max_backups"`
}

// TelemetryConfig defines OpenTelemetry tracing settings
type TelemetryConfig struct {
	Enabled    bool    `json:"enabled" yaml:"enabled"`
	Endpoint   string  `json:"endpoint" yaml:"endpoint"`       // OTLP collector endpoint
	SampleRate float64 `json:"sample_rate" yaml:"sample_rate"` // Trace sampling rate (0.0-1.0)
}

// ServerConfig defines server settings
type ServerConfig struct {
	Host           string          `json:"host" yaml:"host"`
	Port           int             `json:"port" yaml:"port"`
	GRPCPort       int             `json:"grpc_port" yaml:"grpc_port"`
	TLS            TLSServerConfig `json:"tls" yaml:"tls"`
	AllowedOrigins []string        `json:"allowed_origins" yaml:"allowed_origins"`
	GRPCReflection bool            `json:"grpc_reflection" yaml:"grpc_reflection"` // Enable gRPC reflection (default: false for security)
	TrustedProxies []string        `json:"trusted_proxies" yaml:"trusted_proxies"` // IPs that can set X-Forwarded-For (empty = trust none)
	MetricsAuth    bool            `json:"metrics_auth" yaml:"metrics_auth"`       // Require auth on /metrics (default: false for Prometheus scraping; enable in multi-tenant/exposed setups)
	// PublicURL is the externally reachable base URL (e.g. https://status.example.com)
	// used to build absolute links in emails. Required when mail is configured.
	PublicURL string `json:"public_url,omitempty" yaml:"public_url,omitempty"`
}

// TLSServerConfig defines TLS settings
type TLSServerConfig struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Cert    string `json:"cert" yaml:"cert"`
	Key     string `json:"key" yaml:"key"`

	// MinVersion selects the minimum TLS version the HTTP and gRPC listeners
	// accept: 0 = unset (TLS 1.2), 3 = TLS 1.2, 4 = TLS 1.3. The legacy
	// selectors 1 (TLS 1.0) and 2 (TLS 1.1) are rejected by validation rather
	// than silently honoured — both protocols are deprecated and accepting
	// them here would be a downgrade from the server's floor.
	//
	// Resolve it with ResolveMinVersion; do not read the int directly.
	MinVersion int `json:"min_version" yaml:"min_version"`

	// PreferServer is accepted for backward compatibility and does nothing.
	// It used to map onto tls.Config.PreferServerCipherSuites, which Go has
	// ignored since 1.17 — cipher preference is chosen by the runtime now.
	//
	// Deprecated: has no effect; remove it from your config.
	PreferServer bool `json:"prefer_server,omitempty" yaml:"prefer_server,omitempty"`
}

// ResolveMinVersion maps the MinVersion selector onto a crypto/tls version
// constant. Unset and every accepted legacy value floor at TLS 1.2, so a
// config that omits the field gets the same policy the servers hardcoded
// before this was wired up.
func (c TLSServerConfig) ResolveMinVersion() uint16 {
	if c.MinVersion == 4 {
		return tls.VersionTLS13
	}
	return tls.VersionTLS12
}

// StorageConfig defines CobaltDB settings
type StorageConfig struct {
	Path       string           `json:"path" yaml:"path"`
	Encryption EncryptionConfig `json:"encryption" yaml:"encryption"`
	TimeSeries TimeSeriesConfig `json:"timeseries" yaml:"timeseries"`
	BTreeOrder int              `json:"btree_order" yaml:"btree_order"` // B+Tree order (default: 32)
	// RetentionDays is how long raw judgments are kept. `anubis init` has
	// always written it; it sets timeseries.retention.raw when that is unset.
	RetentionDays int `json:"retention_days,omitempty" yaml:"retention_days,omitempty"`
}

// EncryptionConfig defines at-rest encryption settings
type EncryptionConfig struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Key     string `json:"key" yaml:"key"`
}

// TimeSeriesConfig defines time-series storage settings
type TimeSeriesConfig struct {
	Compaction CompactionConfig `json:"compaction" yaml:"compaction"`
	Retention  RetentionConfig  `json:"retention" yaml:"retention"`
}

// CompactionConfig defines downsampling thresholds
type CompactionConfig struct {
	RawToMinute  Duration `json:"raw_to_minute" yaml:"raw_to_minute"`
	MinuteToFive Duration `json:"minute_to_five" yaml:"minute_to_five"`
	FiveToHour   Duration `json:"five_to_hour" yaml:"five_to_hour"`
	HourToDay    Duration `json:"hour_to_day" yaml:"hour_to_day"`
}

// RetentionConfig defines data retention periods
type RetentionConfig struct {
	Raw     Duration `json:"raw" yaml:"raw"`
	Minute  Duration `json:"minute" yaml:"minute"`
	FiveMin Duration `json:"five" yaml:"five"`
	Hour    Duration `json:"hour" yaml:"hour"`
	Day     string   `json:"day" yaml:"day"` // "unlimited" or duration
}

// NecropolisConfig defines cluster settings
type NecropolisConfig struct {
	Enabled       bool               `json:"enabled" yaml:"enabled"`
	NodeName      string             `json:"node_name" yaml:"node_name"`
	Region        string             `json:"region" yaml:"region"`
	Tags          map[string]string  `json:"tags" yaml:"tags"`
	BindAddr      string             `json:"bind_addr" yaml:"bind_addr"`
	AdvertiseAddr string             `json:"advertise_addr" yaml:"advertise_addr"`
	ClusterSecret string             `json:"cluster_secret" yaml:"cluster_secret"`
	SingleNode    bool               `json:"single_node" yaml:"single_node"` // Force single-node mode (no Raft consensus)
	Discovery     DiscoveryConfig    `json:"discovery" yaml:"discovery"`
	Raft          RaftConfig         `json:"raft" yaml:"raft"`
	Distribution  DistributionConfig `json:"distribution" yaml:"distribution"`
	Capabilities  CapabilitiesConfig `json:"capabilities" yaml:"capabilities"`
}

// DiscoveryConfig defines node discovery settings
type DiscoveryConfig struct {
	Mode  string   `json:"mode" yaml:"mode"` // mdns, gossip, manual
	Seeds []string `json:"seeds" yaml:"seeds"`
}

// gossips reports whether this node runs the UDP gossip/mDNS discovery layer.
// Mirrors the condition cluster.Manager uses to construct raft.Discovery:
// mode "manual" (or unset) means peers are configured statically instead.
func (n NecropolisConfig) gossips() bool {
	mode := strings.TrimSpace(n.Discovery.Mode)
	return mode != "" && mode != "manual"
}

// RaftConfig defines Raft consensus settings
type RaftConfig struct {
	NodeID            string         `json:"node_id" yaml:"node_id"`
	BindAddr          string         `json:"bind_addr" yaml:"bind_addr"`
	AdvertiseAddr     string         `json:"advertise_addr" yaml:"advertise_addr"`
	Region            string         `json:"region" yaml:"region"`
	Bootstrap         bool           `json:"bootstrap" yaml:"bootstrap"`
	ElectionTimeout   Duration       `json:"election_timeout" yaml:"election_timeout"`
	HeartbeatTimeout  Duration       `json:"heartbeat_timeout" yaml:"heartbeat_timeout"`
	CommitTimeout     Duration       `json:"commit_timeout" yaml:"commit_timeout"`
	SnapshotInterval  Duration       `json:"snapshot_interval" yaml:"snapshot_interval"`
	SnapshotThreshold int            `json:"snapshot_threshold" yaml:"snapshot_threshold"`
	MaxAppendEntries  int            `json:"max_append_entries" yaml:"max_append_entries"`
	TrailingLogs      int            `json:"trailing_logs" yaml:"trailing_logs"`
	Peers             []RaftPeer     `json:"peers" yaml:"peers"`
	TLS               *TLSPeerConfig `json:"tls" yaml:"tls"`
	Role              RaftRole       `json:"role" yaml:"role"`

	// ClusterSecret is the K9 shared HMAC key. Mirror of
	// NecropolisConfig.ClusterSecret; populated by the cluster
	// manager when it builds the Raft config. Empty means
	// authentication is disabled.
	ClusterSecret string `json:"cluster_secret" yaml:"cluster_secret"`

	// Join lists Raft addresses of existing members. A node that has no
	// peers yet, is not bootstrapping and has join addresses does not elect
	// itself: it asks those members to add it (see raft.Node joinLoop) and
	// learns the real member IDs from the answer.
	Join []string `json:"join,omitempty" yaml:"join,omitempty"`
}

// TLSPeerConfig holds TLS configuration for peer-to-peer communication
type TLSPeerConfig struct {
	CertFile          string `json:"cert_file" yaml:"cert_file"`
	KeyFile           string `json:"key_file" yaml:"key_file"`
	CAFile            string `json:"ca_file" yaml:"ca_file"`
	VerifyPeers       bool   `json:"verify_peers" yaml:"verify_peers"`
	RequireClientCert bool   `json:"require_client_cert" yaml:"require_client_cert"`
}

// RaftRole represents additional cluster roles
type RaftRole string

const (
	RoleVoter    RaftRole = "voter"    // Full voting member
	RoleNonVoter RaftRole = "nonvoter" // Observer, no voting rights
	RoleSpare    RaftRole = "spare"    // Standby, can be promoted
)

// RaftPeer represents a peer node in the cluster
type RaftPeer struct {
	ID       string   `json:"id" yaml:"id"`
	Address  string   `json:"address" yaml:"address"`
	Region   string   `json:"region" yaml:"region"`
	Role     RaftRole `json:"role" yaml:"role"`
	NonVoter bool     `json:"non_voter" yaml:"non_voter"`
}

// DistributionConfig defines check distribution settings
type DistributionConfig struct {
	Strategy          string   `json:"strategy" yaml:"strategy"` // hash (default), round-robin, region-aware, load-based
	Redundancy        int      `json:"redundancy" yaml:"redundancy"`
	RebalanceInterval Duration `json:"rebalance_interval" yaml:"rebalance_interval"`
}

// CapabilitiesConfig defines probe capabilities
type CapabilitiesConfig struct {
	ICMP            bool `json:"icmp" yaml:"icmp"`
	IPv6            bool `json:"ipv6" yaml:"ipv6"`
	DNS             bool `json:"dns" yaml:"dns"`
	InternalNetwork bool `json:"internal_network" yaml:"internal_network"`
}

// TenantsConfig defines multi-tenancy settings
type TenantsConfig struct {
	Enabled       bool        `json:"enabled" yaml:"enabled"`
	Isolation     string      `json:"isolation" yaml:"isolation"` // strict, shared
	DefaultQuotas QuotaConfig `json:"default_quotas" yaml:"default_quotas"`
}

// QuotaConfig defines per-workspace resource limits. They are enforced only
// when tenants.enabled is set; 0 means unlimited.
type QuotaConfig struct {
	// MaxSouls, MaxJourneys and MaxAlertChannels cap how many of each a
	// workspace may hold. Only creates are refused; existing resources stay
	// readable and editable after a limit is lowered.
	MaxSouls         int `json:"max_souls" yaml:"max_souls"`
	MaxJourneys      int `json:"max_journeys" yaml:"max_journeys"`
	MaxAlertChannels int `json:"max_alert_channels" yaml:"max_alert_channels"`
	// MaxTeamMembers caps a workspace's members plus pending invitations.
	// Inviting or adding a member past it is refused; existing members stay.
	MaxTeamMembers int `json:"max_team_members,omitempty" yaml:"max_team_members,omitempty"`
	// RetentionDays caps every time-series retention tier.
	RetentionDays int `json:"retention_days" yaml:"retention_days"`
	// CheckIntervalMin is the shortest check interval a soul or journey may
	// be created with, or changed to.
	CheckIntervalMin Duration `json:"check_interval_min" yaml:"check_interval_min"`
}

// DashboardConfig defines dashboard settings
type DashboardConfig struct {
	Enabled  bool              `json:"enabled" yaml:"enabled"`
	Branding DashboardBranding `json:"branding" yaml:"branding"`
}

// DashboardBranding defines dashboard customization. It is public (the login
// page shows it), so it holds nothing secret.
type DashboardBranding struct {
	Title string `json:"title" yaml:"title"`
	// Logo is an https:// URL or a same-origin path ("/..."); the dashboard's
	// CSP allows both as images.
	Logo  string `json:"logo" yaml:"logo"`
	Theme string `json:"theme" yaml:"theme"` // auto, dark, light
}

// MaxBrandingTitleRunes bounds dashboard.branding.title.
const MaxBrandingTitleRunes = 64

func (b DashboardBranding) validate() error {
	if n := utf8.RuneCountInString(b.Title); n > MaxBrandingTitleRunes {
		return fmt.Errorf("dashboard.branding.title is %d characters; at most %d", n, MaxBrandingTitleRunes)
	}
	for _, r := range b.Title {
		if unicode.IsControl(r) {
			return fmt.Errorf("dashboard.branding.title must not contain control characters")
		}
	}
	switch b.Theme {
	case "", "auto", "dark", "light":
	default:
		return fmt.Errorf("dashboard.branding.theme must be auto, dark or light, not %q", b.Theme)
	}
	if b.Logo == "" {
		return nil
	}
	if len(b.Logo) > 2048 {
		return fmt.Errorf("dashboard.branding.logo is longer than 2048 bytes")
	}
	if strings.HasPrefix(b.Logo, "/") && !strings.HasPrefix(b.Logo, "//") {
		return nil
	}
	u, err := url.Parse(b.Logo)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return fmt.Errorf("dashboard.branding.logo must be an https:// URL or a path starting with /")
	}
	return nil
}

// AuthConfig defines authentication settings
type AuthConfig struct {
	Enabled *bool     `json:"enabled" yaml:"enabled"` // auth enabled/disabled (nil = not explicitly set)
	Type    string    `json:"type" yaml:"type"`       // local, oidc, ldap
	Local   LocalAuth `json:"local" yaml:"local"`
	OIDC    OIDCAuth  `json:"oidc" yaml:"oidc"`
	LDAP    LDAPAuth  `json:"ldap" yaml:"ldap"`
}

// IsEnabled returns the resolved auth enabled state. If Enabled is nil,
// it returns false (disabled by default).
func (c AuthConfig) IsEnabled() bool {
	return c.Enabled != nil && *c.Enabled
}

// LocalAuth defines local authentication
type LocalAuth struct {
	AdminEmail    string `json:"admin_email" yaml:"admin_email"`
	AdminPassword string `json:"admin_password" yaml:"admin_password"`
}

// OIDCAuth defines OIDC settings
type OIDCAuth struct {
	Issuer       string `json:"issuer" yaml:"issuer"`
	ClientID     string `json:"client_id" yaml:"client_id"`
	ClientSecret string `json:"client_secret" yaml:"client_secret"`
	RedirectURL  string `json:"redirect_url" yaml:"redirect_url"`
	// AllowUnverifiedEmail accepts identities whose provider omits the
	// email_verified claim. Accounts are matched by email, so by default
	// only an explicitly verified address may sign in.
	AllowUnverifiedEmail bool `json:"allow_unverified_email,omitempty" yaml:"allow_unverified_email,omitempty"`
}

// LDAPAuth defines LDAP settings
type LDAPAuth struct {
	URL          string `json:"url" yaml:"url"`
	BindDN       string `json:"bind_dn" yaml:"bind_dn"`
	BindPassword string `json:"bind_password" yaml:"bind_password"`
	BaseDN       string `json:"base_dn" yaml:"base_dn"`
	UserFilter   string `json:"user_filter" yaml:"user_filter"`
}

// Validate validates the Raft configuration
func (c *RaftConfig) Validate() error {
	if c.NodeID == "" {
		return &ValidationError{Field: "node_id", Message: "node ID is required"}
	}
	if c.BindAddr == "" {
		return &ValidationError{Field: "bind_addr", Message: "bind address is required"}
	}
	if c.AdvertiseAddr == "" {
		c.AdvertiseAddr = c.BindAddr
	}
	if c.ElectionTimeout.Duration == 0 {
		c.ElectionTimeout.Duration = time.Second
	} else if c.ElectionTimeout.Duration < 0 {
		return &ValidationError{Field: "election_timeout", Message: "election timeout must be positive"}
	}
	if c.HeartbeatTimeout.Duration == 0 {
		c.HeartbeatTimeout.Duration = 300 * time.Millisecond
	} else if c.HeartbeatTimeout.Duration < 0 {
		return &ValidationError{Field: "heartbeat_timeout", Message: "heartbeat timeout must be positive"}
	}
	// Match the effective ticker interval in raft.Node.run, including its
	// fallback when halving a positive duration rounds down to zero.
	heartbeatInterval := c.HeartbeatTimeout.Duration / 2
	if heartbeatInterval == 0 {
		heartbeatInterval = 50 * time.Millisecond
	}
	if heartbeatInterval >= c.ElectionTimeout.Duration {
		return &ValidationError{Field: "heartbeat_timeout", Message: "heartbeat interval must be shorter than election timeout"}
	}
	if c.CommitTimeout.Duration == 0 {
		c.CommitTimeout.Duration = 50 * time.Millisecond
	} else if c.CommitTimeout.Duration < 0 {
		return &ValidationError{Field: "commit_timeout", Message: "commit timeout must be positive"}
	}
	if c.MaxAppendEntries == 0 {
		c.MaxAppendEntries = 64
	} else if c.MaxAppendEntries < 0 {
		return &ValidationError{Field: "max_append_entries", Message: "max append entries must be positive"}
	}
	if c.SnapshotThreshold < 0 {
		return &ValidationError{Field: "snapshot_threshold", Message: "snapshot threshold must not be negative"}
	}
	if c.TrailingLogs < 0 {
		return &ValidationError{Field: "trailing_logs", Message: "trailing logs must not be negative"}
	}
	return nil
}

// validate validates the server configuration
func (c ServerConfig) validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return &ConfigError{Field: "server.port", Message: "port must be between 1 and 65535"}
	}
	if c.GRPCPort < 0 || c.GRPCPort > 65535 {
		return &ConfigError{Field: "server.grpc_port", Message: "grpc_port must be 0 (disabled) or between 1 and 65535"}
	}
	if c.TLS.Enabled {
		if c.TLS.Cert == "" || c.TLS.Key == "" {
			return &ConfigError{Field: "server.tls", Message: "TLS is enabled but cert/key were not provided"}
		}
	}
	// Validate the selector whether or not TLS is enabled: a config that asks
	// for TLS 1.0 should be rejected at load time, not quietly upgraded the
	// day someone flips tls.enabled to true.
	switch c.TLS.MinVersion {
	case 0, 3, 4:
	case 1, 2:
		return &ConfigError{
			Field:   "server.tls.min_version",
			Message: "TLS 1.0 and 1.1 are not supported; use 3 (TLS 1.2) or 4 (TLS 1.3)",
		}
	default:
		return &ConfigError{
			Field:   "server.tls.min_version",
			Message: fmt.Sprintf("unknown value %d; use 0 (default, TLS 1.2), 3 (TLS 1.2), or 4 (TLS 1.3)", c.TLS.MinVersion),
		}
	}
	return nil
}

// MinEncryptionKeyBytes is the shortest storage encryption key accepted.
// `anubis init` has always generated 16-character random keys, so 16 is the
// floor that keeps those deployments starting; RecommendedEncryptionKeyBytes
// (a 64-character hex string such as `openssl rand -hex 32`) is advised.
const (
	MinEncryptionKeyBytes         = 16
	RecommendedEncryptionKeyBytes = 32
)

// AllowWeakEncryptionKeyEnv lets an operator start once with a key shorter
// than MinEncryptionKeyBytes so the data can be backed up and restored into a
// store with a strong key. It is a migration escape hatch, not a setting.
const AllowWeakEncryptionKeyEnv = "ANUBIS_ALLOW_WEAK_ENCRYPTION_KEY"

// ValidateEncryptionKey enforces the storage encryption key rule shared by
// config validation and the storage engine. The key string is used verbatim
// as HKDF input (a hex string is NOT decoded), so every key accepted before
// this check existed derives exactly the same AES keys as before.
func ValidateEncryptionKey(key string) error {
	if key == "" {
		return fmt.Errorf("encryption key is empty")
	}
	if len(key) < MinEncryptionKeyBytes && os.Getenv(AllowWeakEncryptionKeyEnv) != "1" {
		return fmt.Errorf("encryption key is %d bytes; at least %d are required (%d+ recommended, e.g. a 64-character hex string from `openssl rand -hex 32`); to open an existing store once for migration set %s=1",
			len(key), MinEncryptionKeyBytes, RecommendedEncryptionKeyBytes, AllowWeakEncryptionKeyEnv)
	}
	return nil
}

// validate validates the storage configuration
func (c StorageConfig) validate() error {
	if c.Path == "" {
		return &ConfigError{Field: "storage.path", Message: "storage path is required"}
	}
	if c.Encryption.Enabled {
		key := strings.TrimSpace(c.Encryption.Key)
		if key == "" {
			return &ConfigError{Field: "storage.encryption.key", Message: "encryption key is required when encryption is enabled"}
		}
		lowerKey := strings.ToLower(key)
		for _, marker := range []string{"replace-with", "change_me", "changeme"} {
			if strings.Contains(lowerKey, marker) {
				return &ConfigError{Field: "storage.encryption.key", Message: "placeholder encryption keys are not allowed"}
			}
		}
		if err := ValidateEncryptionKey(c.Encryption.Key); err != nil {
			return &ConfigError{Field: "storage.encryption.key", Message: err.Error()}
		}
	}
	if c.BTreeOrder < 3 && c.BTreeOrder != 0 {
		return &ConfigError{Field: "storage.btree_order", Message: "btree_order must be at least 3"}
	}
	return nil
}

// validate validates the auth configuration
func (c AuthConfig) validate() error {
	validTypes := map[string]bool{"local": true, "oidc": true, "ldap": true}
	if c.Type == "" {
		return &ConfigError{Field: "auth.type", Message: "auth type is required"}
	}
	if !validTypes[c.Type] {
		return &ConfigError{Field: "auth.type", Message: fmt.Sprintf("invalid auth type: %s", c.Type)}
	}

	switch c.Type {
	case "oidc":
		if c.OIDC.Issuer == "" {
			return &ConfigError{Field: "auth.oidc.issuer", Message: "OIDC issuer is required"}
		}
		if c.OIDC.ClientID == "" {
			return &ConfigError{Field: "auth.oidc.client_id", Message: "OIDC client ID is required"}
		}
		if c.OIDC.ClientSecret == "" {
			return &ConfigError{Field: "auth.oidc.client_secret", Message: "OIDC client secret is required"}
		}
	case "ldap":
		if c.LDAP.URL == "" {
			return &ConfigError{Field: "auth.ldap.url", Message: "LDAP URL is required"}
		}
		if c.LDAP.BaseDN == "" {
			return &ConfigError{Field: "auth.ldap.base_dn", Message: "LDAP base DN is required"}
		}
	}
	if c.Type == "local" && c.IsEnabled() {
		if c.Local.AdminEmail == "" {
			return &ConfigError{Field: "auth.local.admin_email", Message: "admin email is required when local auth is enabled"}
		}
		if c.Local.AdminPassword == "" {
			return &ConfigError{Field: "auth.local.admin_password", Message: "admin password is required when local auth is enabled"}
		}
	}
	if c.Type == "local" && c.Local.AdminPassword != "" && c.Local.AdminEmail == "" {
		return &ConfigError{Field: "auth.local.admin_email", Message: "admin email is required when admin password is configured"}
	}
	if c.Local.AdminPassword != "" && !isBcryptHash(c.Local.AdminPassword) {
		if err := validateAdminPassword(c.Local.AdminPassword); err != nil {
			return &ConfigError{Field: "auth.local.admin_password", Message: err.Error()}
		}
	}
	return nil
}

func isBcryptHash(password string) bool {
	return strings.HasPrefix(password, "$2a$") ||
		strings.HasPrefix(password, "$2b$") ||
		strings.HasPrefix(password, "$2y$")
}

func validateAdminPassword(password string) error {
	normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(password))
	defaults := map[string]bool{
		"admin":    true,
		"password": true,
		"changeme": true,
	}
	if defaults[normalized] {
		return fmt.Errorf("password must not be a default or placeholder value")
	}

	if len(password) < 12 {
		return fmt.Errorf("password must be at least 12 characters")
	}

	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, r := range password {
		switch {
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= '0' && r <= '9':
			hasDigit = true
		default:
			hasSpecial = true
		}
	}

	classes := 0
	if hasUpper {
		classes++
	}
	if hasLower {
		classes++
	}
	if hasDigit {
		classes++
	}
	if hasSpecial {
		classes++
	}
	if classes < 3 {
		return fmt.Errorf("password must contain at least 3 of: uppercase, lowercase, digits, special characters")
	}
	return nil
}

// validate validates the journey configuration
func (c JourneyConfig) validate(index int) error {
	if c.Name == "" {
		return &ConfigError{Field: fmt.Sprintf("journeys[%d].name", index), Message: "name is required"}
	}
	if len(c.Steps) == 0 {
		return &ConfigError{Field: fmt.Sprintf("journeys[%d].steps", index), Message: "at least one step is required"}
	}
	for i, step := range c.Steps {
		if step.Name == "" {
			return &ConfigError{Field: fmt.Sprintf("journeys[%d].steps[%d].name", index, i), Message: "step name is required"}
		}
		if step.Target == "" {
			return &ConfigError{Field: fmt.Sprintf("journeys[%d].steps[%d].target", index, i), Message: "step target is required"}
		}
	}
	return nil
}

// validate validates the logging configuration
func (c LoggingConfig) validate() error {
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[c.Level] {
		return &ConfigError{Field: "logging.level", Message: fmt.Sprintf("invalid log level: %s", c.Level)}
	}
	validFormats := map[string]bool{"json": true, "text": true}
	if !validFormats[c.Format] {
		return &ConfigError{Field: "logging.format", Message: fmt.Sprintf("invalid log format: %s", c.Format)}
	}
	// file and both default logging.file to <storage.path>/logs/anubis.log.
	validOutputs := map[string]bool{"stdout": true, "stderr": true, "file": true, "both": true}
	if !validOutputs[c.Output] {
		return &ConfigError{Field: "logging.output", Message: fmt.Sprintf("invalid log output: %s (want stdout, stderr, file or both)", c.Output)}
	}
	return nil
}
