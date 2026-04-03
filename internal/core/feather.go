package core

// FeatherConfig defines a performance budget (Feather of Ma'at)
type FeatherConfig struct {
	Name   string        `json:"name" yaml:"name"`
	Scope  string        `json:"scope" yaml:"scope"` // "tag:xxx", "soul:xxx", "type:xxx"
	Rules  FeatherRules  `json:"rules" yaml:"rules"`
	Window Duration      `json:"window" yaml:"window"`           // evaluation window
	ViolationThreshold int           `json:"violation_threshold" yaml:"violation_threshold"` // consecutive violations
}

// FeatherRules defines latency thresholds
type FeatherRules struct {
	P50  Duration `json:"p50" yaml:"p50"`
	P95  Duration `json:"p95" yaml:"p95"`
	P99  Duration `json:"p99" yaml:"p99"`
	Max  Duration `json:"max" yaml:"max"`
}

// VerdictsConfig holds alert rules configuration
type VerdictsConfig struct {
	Rules      []AlertRule      `json:"rules" yaml:"rules"`
	Escalation []EscalationPolicy `json:"escalation,omitempty" yaml:"escalation,omitempty"`
}

// EscalationPolicy defines multi-stage escalation
type EscalationPolicy struct {
	Name   string           `json:"name" yaml:"name"`
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
}

// ServerConfig defines server settings
type ServerConfig struct {
	Host string          `json:"host" yaml:"host"`
	Port int             `json:"port" yaml:"port"`
	TLS  TLSServerConfig `json:"tls" yaml:"tls"`
}

// TLSServerConfig defines TLS settings
type TLSServerConfig struct {
	Enabled     bool     `json:"enabled" yaml:"enabled"`
	Cert        string   `json:"cert" yaml:"cert"`
	Key         string   `json:"key" yaml:"key"`
	AutoCert    bool     `json:"auto_cert" yaml:"auto_cert"`
	ACMEEmail   string   `json:"acme_email" yaml:"acme_email"`
	ACMEDomains []string `json:"acme_domains" yaml:"acme_domains"`
}

// StorageConfig defines CobaltDB settings
type StorageConfig struct {
	Path       string           `json:"path" yaml:"path"`
	Encryption EncryptionConfig `json:"encryption" yaml:"encryption"`
	TimeSeries TimeSeriesConfig `json:"timeseries" yaml:"timeseries"`
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
	Enabled       bool              `json:"enabled" yaml:"enabled"`
	NodeName      string            `json:"node_name" yaml:"node_name"`
	Region        string            `json:"region" yaml:"region"`
	Tags          map[string]string `json:"tags" yaml:"tags"`
	BindAddr      string            `json:"bind_addr" yaml:"bind_addr"`
	AdvertiseAddr string            `json:"advertise_addr" yaml:"advertise_addr"`
	ClusterSecret string            `json:"cluster_secret" yaml:"cluster_secret"`
	Discovery     DiscoveryConfig   `json:"discovery" yaml:"discovery"`
	Raft          RaftConfig        `json:"raft" yaml:"raft"`
	Distribution  DistributionConfig `json:"distribution" yaml:"distribution"`
	Capabilities  CapabilitiesConfig `json:"capabilities" yaml:"capabilities"`
}

// DiscoveryConfig defines node discovery settings
type DiscoveryConfig struct {
	Mode  string   `json:"mode" yaml:"mode"` // mdns, gossip, manual
	Seeds []string `json:"seeds" yaml:"seeds"`
}

// RaftConfig defines Raft consensus settings
type RaftConfig struct {
	NodeID            string            `json:"node_id" yaml:"node_id"`
	BindAddr          string            `json:"bind_addr" yaml:"bind_addr"`
	AdvertiseAddr     string            `json:"advertise_addr" yaml:"advertise_addr"`
	Bootstrap         bool              `json:"bootstrap" yaml:"bootstrap"`
	ElectionTimeout   Duration          `json:"election_timeout" yaml:"election_timeout"`
	HeartbeatTimeout  Duration          `json:"heartbeat_timeout" yaml:"heartbeat_timeout"`
	CommitTimeout     Duration          `json:"commit_timeout" yaml:"commit_timeout"`
	SnapshotInterval  Duration          `json:"snapshot_interval" yaml:"snapshot_interval"`
	SnapshotThreshold int               `json:"snapshot_threshold" yaml:"snapshot_threshold"`
	MaxAppendEntries  int               `json:"max_append_entries" yaml:"max_append_entries"`
	TrailingLogs      int               `json:"trailing_logs" yaml:"trailing_logs"`
	Peers             []RaftPeer        `json:"peers" yaml:"peers"`
	TLS               *TLSPeerConfig    `json:"tls" yaml:"tls"`
	Role              RaftRole          `json:"role" yaml:"role"`
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
	Strategy          string   `json:"strategy" yaml:"strategy"` // round-robin, region-aware, latency-optimized, redundant
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
	Enabled       bool         `json:"enabled" yaml:"enabled"`
	Isolation     string       `json:"isolation" yaml:"isolation"` // strict, shared
	DefaultQuotas QuotaConfig  `json:"default_quotas" yaml:"default_quotas"`
}

// QuotaConfig defines resource limits
type QuotaConfig struct {
	MaxSouls         int      `json:"max_souls" yaml:"max_souls"`
	MaxJourneys      int      `json:"max_journeys" yaml:"max_journeys"`
	MaxAlertChannels int      `json:"max_alert_channels" yaml:"max_alert_channels"`
	MaxTeamMembers   int      `json:"max_team_members" yaml:"max_team_members"`
	RetentionDays    int      `json:"retention_days" yaml:"retention_days"`
	CheckIntervalMin Duration `json:"check_interval_min" yaml:"check_interval_min"`
}

// DashboardConfig defines dashboard settings
type DashboardConfig struct {
	Enabled   bool              `json:"enabled" yaml:"enabled"`
	Branding  DashboardBranding `json:"branding" yaml:"branding"`
}

// DashboardBranding defines dashboard customization
type DashboardBranding struct {
	Title  string `json:"title" yaml:"title"`
	Logo   string `json:"logo" yaml:"logo"`
	Theme  string `json:"theme" yaml:"theme"` // auto, dark, light
}

// AuthConfig defines authentication settings
type AuthConfig struct {
	Type  string      `json:"type" yaml:"type"` // local, oidc, ldap
	Local LocalAuth   `json:"local" yaml:"local"`
	OIDC  OIDCAuth    `json:"oidc" yaml:"oidc"`
	LDAP  LDAPAuth    `json:"ldap" yaml:"ldap"`
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
	return nil
}

