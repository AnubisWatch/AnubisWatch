package core

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration for AnubisWatch
type Config struct {
	Server     ServerConfig     `yaml:"server"`
	Storage    StorageConfig    `yaml:"storage"`
	Necropolis NecropolisConfig `yaml:"necropolis"`
	Regions    RegionsConfig    `yaml:"regions"`
	Tenants    TenantsConfig    `yaml:"tenants"`
	Auth       AuthConfig       `yaml:"auth"`
	Dashboard  DashboardConfig  `yaml:"dashboard"`
	Souls      []Soul           `yaml:"souls"`
	Channels   []ChannelConfig  `yaml:"channels"`
	Verdicts   VerdictsConfig   `yaml:"verdicts"`
	Feathers   []FeatherConfig  `yaml:"feathers"`
	Journeys   []JourneyConfig  `yaml:"journeys"`
	Logging    LoggingConfig    `yaml:"logging"`
}

// LoadConfig reads and parses the configuration file (YAML or JSON)
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables: ${VAR} and ${VAR:-default}
	expanded := expandEnvVars(string(data))

	var config Config

	// Try YAML first, then JSON
	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		if err := yaml.Unmarshal([]byte(expanded), &config); err != nil {
			return nil, fmt.Errorf("failed to parse YAML config: %w", err)
		}
	} else {
		if err := json.Unmarshal([]byte(expanded), &config); err != nil {
			return nil, fmt.Errorf("failed to parse JSON config: %w", err)
		}
	}

	// Apply environment variable overrides
	config.applyEnvOverrides()

	// Set defaults
	config.setDefaults()

	// Validate
	if err := config.validate(); err != nil {
		return nil, err
	}

	return &config, nil
}

// expandEnvVars expands ${VAR} and ${VAR:-default} syntax in config
var envVarRegex = regexp.MustCompile(`\$\{([^}]+)\}`)

func expandEnvVars(s string) string {
	return envVarRegex.ReplaceAllStringFunc(s, func(match string) string {
		inner := match[2 : len(match)-1] // strip ${ and }
		parts := strings.SplitN(inner, ":-", 2)
		key := parts[0]
		val := os.Getenv(key)
		if val == "" && len(parts) == 2 {
			return parts[1] // default value
		}
		return val
	})
}

// applyEnvOverrides applies environment variable overrides for specific config values
func (c *Config) applyEnvOverrides() {
	// Server settings
	if v := os.Getenv("ANUBIS_HOST"); v != "" {
		c.Server.Host = v
	}
	if v := os.Getenv("ANUBIS_PORT"); v != "" {
		if port, err := parseInt(v); err == nil {
			c.Server.Port = port
		}
	}

	// Storage settings
	if v := os.Getenv("ANUBIS_DATA_DIR"); v != "" {
		c.Storage.Path = v
	}
	if v := os.Getenv("ANUBIS_ENCRYPTION_KEY"); v != "" {
		c.Storage.Encryption.Key = v
		c.Storage.Encryption.Enabled = true
	}

	// Cluster settings
	if v := os.Getenv("ANUBIS_CLUSTER_SECRET"); v != "" {
		c.Necropolis.ClusterSecret = v
	}

	// Auth settings
	if v := os.Getenv("ANUBIS_ADMIN_PASSWORD"); v != "" {
		c.Auth.Local.AdminPassword = v
	}

	// Logging
	if v := os.Getenv("ANUBIS_LOG_LEVEL"); v != "" {
		c.Logging.Level = v
	}
}

func parseInt(s string) (int, error) {
	var result int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid integer: %s", s)
		}
		result = result*10 + int(c-'0')
	}
	return result, nil
}

// BoolPtr returns a pointer to a bool value.
func BoolPtr(v bool) *bool {
	return &v
}

func (c *Config) setDefaults() {
	// Server defaults
	if c.Server.Host == "" {
		c.Server.Host = "0.0.0.0"
	}
	if c.Server.Port == 0 {
		c.Server.Port = 8443
	}
	if c.Server.TLS.Enabled && c.Server.TLS.Cert == "" && c.Server.TLS.Key == "" {
		c.Server.TLS.AutoCert = true
	}

	// Storage defaults
	if c.Storage.Path == "" {
		c.Storage.Path = "/var/lib/anubis/data"
	}

	// Time series defaults
	if c.Storage.TimeSeries.Compaction.RawToMinute.Duration == 0 {
		c.Storage.TimeSeries.Compaction.RawToMinute.Duration = 48 * 60 * 60 * 1e9 // 48h
	}
	if c.Storage.TimeSeries.Compaction.MinuteToFive.Duration == 0 {
		c.Storage.TimeSeries.Compaction.MinuteToFive.Duration = 7 * 24 * 60 * 60 * 1e9 // 7d
	}
	if c.Storage.TimeSeries.Compaction.FiveToHour.Duration == 0 {
		c.Storage.TimeSeries.Compaction.FiveToHour.Duration = 30 * 24 * 60 * 60 * 1e9 // 30d
	}
	if c.Storage.TimeSeries.Compaction.HourToDay.Duration == 0 {
		c.Storage.TimeSeries.Compaction.HourToDay.Duration = 365 * 24 * 60 * 60 * 1e9 // 365d
	}
	if c.Storage.TimeSeries.Retention.Raw.Duration == 0 {
		c.Storage.TimeSeries.Retention.Raw.Duration = 48 * 60 * 60 * 1e9 // 48h
	}
	if c.Storage.TimeSeries.Retention.Minute.Duration == 0 {
		c.Storage.TimeSeries.Retention.Minute.Duration = 30 * 24 * 60 * 60 * 1e9 // 30d
	}
	if c.Storage.TimeSeries.Retention.FiveMin.Duration == 0 {
		c.Storage.TimeSeries.Retention.FiveMin.Duration = 90 * 24 * 60 * 60 * 1e9 // 90d
	}
	if c.Storage.TimeSeries.Retention.Hour.Duration == 0 {
		c.Storage.TimeSeries.Retention.Hour.Duration = 365 * 24 * 60 * 60 * 1e9 // 365d
	}
	if c.Storage.TimeSeries.Retention.Day == "" {
		c.Storage.TimeSeries.Retention.Day = "unlimited"
	}

	// Necropolis defaults
	if c.Necropolis.BindAddr == "" {
		c.Necropolis.BindAddr = "0.0.0.0:7946"
	}
	if c.Necropolis.Discovery.Mode == "" {
		c.Necropolis.Discovery.Mode = "mdns"
	}
	if c.Necropolis.Raft.ElectionTimeout.Duration == 0 {
		c.Necropolis.Raft.ElectionTimeout.Duration = 1000 * 1e6 // 1000ms
	}
	if c.Necropolis.Raft.HeartbeatTimeout.Duration == 0 {
		c.Necropolis.Raft.HeartbeatTimeout.Duration = 300 * 1e6 // 300ms
	}
	if c.Necropolis.Raft.SnapshotInterval.Duration == 0 {
		c.Necropolis.Raft.SnapshotInterval.Duration = 300 * 1e9 // 300s
	}
	if c.Necropolis.Raft.SnapshotThreshold == 0 {
		c.Necropolis.Raft.SnapshotThreshold = 8192
	}
	if c.Necropolis.Distribution.Strategy == "" {
		c.Necropolis.Distribution.Strategy = "round-robin"
	}
	if c.Necropolis.Distribution.Redundancy == 0 {
		c.Necropolis.Distribution.Redundancy = 1
	}
	if c.Necropolis.Distribution.RebalanceInterval.Duration == 0 {
		c.Necropolis.Distribution.RebalanceInterval.Duration = 60 * 1e9 // 60s
	}

	// Tenants defaults
	if c.Tenants.Isolation == "" {
		c.Tenants.Isolation = "strict"
	}
	if c.Tenants.DefaultQuotas.MaxSouls == 0 {
		c.Tenants.DefaultQuotas.MaxSouls = 100
	}
	if c.Tenants.DefaultQuotas.MaxJourneys == 0 {
		c.Tenants.DefaultQuotas.MaxJourneys = 20
	}
	if c.Tenants.DefaultQuotas.MaxAlertChannels == 0 {
		c.Tenants.DefaultQuotas.MaxAlertChannels = 10
	}
	if c.Tenants.DefaultQuotas.MaxTeamMembers == 0 {
		c.Tenants.DefaultQuotas.MaxTeamMembers = 25
	}
	if c.Tenants.DefaultQuotas.RetentionDays == 0 {
		c.Tenants.DefaultQuotas.RetentionDays = 90
	}
	if c.Tenants.DefaultQuotas.CheckIntervalMin.Duration == 0 {
		c.Tenants.DefaultQuotas.CheckIntervalMin.Duration = 30 * 1e9 // 30s
	}

	// Logging defaults
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.Logging.Format == "" {
		c.Logging.Format = "json"
	}
	if c.Logging.Output == "" {
		c.Logging.Output = "stdout"
	}

	// Dashboard defaults
	if c.Dashboard.Branding.Title == "" {
		c.Dashboard.Branding.Title = "AnubisWatch"
	}
	if c.Dashboard.Branding.Theme == "" {
		c.Dashboard.Branding.Theme = "auto"
	}

	// Auth defaults
	if c.Auth.Type == "" {
		c.Auth.Type = "local"
	}
	// Auto-enable auth only if the user did not explicitly set enabled=false
	// and credentials are configured.
	if c.Auth.Enabled == nil {
		enabled := false
		if c.Auth.Type == "local" && c.Auth.Local.AdminEmail != "" && c.Auth.Local.AdminPassword != "" {
			enabled = true
		}
		if c.Auth.Type == "oidc" && c.Auth.OIDC.Issuer != "" {
			enabled = true
		}
		if c.Auth.Type == "ldap" && c.Auth.LDAP.URL != "" {
			enabled = true
		}
		c.Auth.Enabled = &enabled
	}
}

func (c *Config) validate() error {
	// Ensure defaults are set before validation
	c.setDefaults()

	// Validate server config
	if err := c.Server.validate(); err != nil {
		return err
	}

	// Validate storage config
	if err := c.Storage.validate(); err != nil {
		return err
	}

	// Validate auth config
	if err := c.Auth.validate(); err != nil {
		return err
	}

	// Validate souls
	for i, soul := range c.Souls {
		if err := soul.validate(i); err != nil {
			return err
		}
	}

	// Validate channels
	for i, ch := range c.Channels {
		if err := ch.validate(i); err != nil {
			return err
		}
	}

	// Validate alert rules
	for i, rule := range c.Verdicts.Rules {
		if err := rule.validate(i); err != nil {
			return err
		}
	}

	// Validate feathers
	for i, feather := range c.Feathers {
		if err := feather.validate(i); err != nil {
			return err
		}
	}

	// Validate journeys
	for i, journey := range c.Journeys {
		if err := journey.validate(i); err != nil {
			return err
		}
	}

	// Validate logging config
	if err := c.Logging.validate(); err != nil {
		return err
	}

	return nil
}

// RegionsConfig defines multi-region configuration
type RegionsConfig struct {
	Enabled       bool                 `json:"enabled" yaml:"enabled"`
	LocalRegion   string               `json:"local_region" yaml:"local_region"`
	Replication   ReplicationConfig    `json:"replication" yaml:"replication"`
	Routing       RoutingConfig        `json:"routing" yaml:"routing"`
	HealthCheck   HealthCheckConfig    `json:"health_check" yaml:"health_check"`
	RemoteRegions []RemoteRegionConfig `json:"remote_regions" yaml:"remote_regions"`
}

// RemoteRegionConfig defines a remote region
type RemoteRegionConfig struct {
	ID        string  `json:"id" yaml:"id"`
	Name      string  `json:"name" yaml:"name"`
	Endpoint  string  `json:"endpoint" yaml:"endpoint"`
	Location  string  `json:"location" yaml:"location"`
	Latitude  float64 `json:"latitude" yaml:"latitude"`
	Longitude float64 `json:"longitude" yaml:"longitude"`
	Priority  int     `json:"priority" yaml:"priority"`
	Enabled   bool    `json:"enabled" yaml:"enabled"`
	Secret    string  `json:"secret" yaml:"secret"`
}

// ReplicationConfig defines cross-region replication settings
type ReplicationConfig struct {
	Enabled          bool     `json:"enabled" yaml:"enabled"`
	SyncMode         string   `json:"sync_mode" yaml:"sync_mode"`
	BatchSize        int      `json:"batch_size" yaml:"batch_size"`
	BatchInterval    Duration `json:"batch_interval" yaml:"batch_interval"`
	ConflictStrategy string   `json:"conflict_strategy" yaml:"conflict_strategy"`
	RetryInterval    Duration `json:"retry_interval" yaml:"retry_interval"`
	MaxRetries       int      `json:"max_retries" yaml:"max_retries"`
	Compression      bool     `json:"compression" yaml:"compression"`
	EncryptTraffic   bool     `json:"encrypt_traffic" yaml:"encrypt_traffic"`
}

// RoutingConfig defines region-aware routing settings
type RoutingConfig struct {
	Enabled         bool     `json:"enabled" yaml:"enabled"`
	LatencyBased    bool     `json:"latency_based" yaml:"latency_based"`
	GeoBased        bool     `json:"geo_based" yaml:"geo_based"`
	HealthBased     bool     `json:"health_based" yaml:"health_based"`
	FailoverTimeout Duration `json:"failover_timeout" yaml:"failover_timeout"`
}

// HealthCheckConfig defines region health monitoring settings
type HealthCheckConfig struct {
	Enabled          bool              `json:"enabled" yaml:"enabled"`
	Interval         Duration          `json:"interval" yaml:"interval"`
	Timeout          Duration          `json:"timeout" yaml:"timeout"`
	FailureThreshold int               `json:"failure_threshold" yaml:"failure_threshold"`
	SuccessThreshold int               `json:"success_threshold" yaml:"success_threshold"`
	Endpoints        map[string]string `json:"endpoints" yaml:"endpoints"`
}

func SaveConfig(path string, config *Config) error {
	var data []byte
	var err error

	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		data, err = yaml.Marshal(config)
	} else {
		data, err = json.MarshalIndent(config, "", "  ")
	}

	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// GenerateDefaultConfig creates a default configuration file
func GenerateDefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:     "0.0.0.0",
			Port:     8443,
			GRPCPort: 9090,
			TLS: TLSServerConfig{
				Enabled:  true,
				AutoCert: true,
			},
		},
		Storage: StorageConfig{
			Path: "/var/lib/anubis/data",
		},
		Necropolis: NecropolisConfig{
			Enabled: false,
		},
		Tenants: TenantsConfig{
			Enabled: false,
		},
		Auth: AuthConfig{
			Type:    "local",
			Enabled: new(bool),
		},
		Dashboard: DashboardConfig{
			Enabled: true,
			Branding: DashboardBranding{
				Title: "AnubisWatch",
				Theme: "auto",
			},
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
			Output: "stdout",
		},
	}
}
