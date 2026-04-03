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
}

func (c *Config) validate() error {
	// Validate souls have required fields
	for i, soul := range c.Souls {
		if soul.Name == "" {
			return &ConfigError{Field: fmt.Sprintf("souls[%d].name", i), Message: "name is required"}
		}
		if soul.Target == "" {
			return &ConfigError{Field: fmt.Sprintf("souls[%d].target", i), Message: "target is required"}
		}
		if soul.Type == "" {
			return &ConfigError{Field: fmt.Sprintf("souls[%d].type", i), Message: "type is required"}
		}
		// Validate type-specific config
		switch soul.Type {
		case CheckHTTP:
			if soul.HTTP == nil {
				soul.HTTP = &HTTPConfig{Method: "GET", ValidStatus: []int{200}}
			}
		}
	}

	// Validate channels have required fields
	for i, ch := range c.Channels {
		if ch.Name == "" {
			return &ConfigError{Field: fmt.Sprintf("channels[%d].name", i), Message: "name is required"}
		}
		if ch.Type == "" {
			return &ConfigError{Field: fmt.Sprintf("channels[%d].type", i), Message: "type is required"}
		}
	}

	// Validate alert rules
	for i, rule := range c.Verdicts.Rules {
		if rule.Name == "" {
			return &ConfigError{Field: fmt.Sprintf("verdicts.rules[%d].name", i), Message: "name is required"}
		}
		if len(rule.Conditions) == 0 {
			return &ConfigError{Field: fmt.Sprintf("verdicts.rules[%d].conditions", i), Message: "at least one condition is required"}
		}
		for j, cond := range rule.Conditions {
			if cond.Type == "" {
				return &ConfigError{Field: fmt.Sprintf("verdicts.rules[%d].conditions[%d].type", i, j), Message: "condition type is required"}
			}
		}
		if len(rule.Channels) == 0 {
			return &ConfigError{Field: fmt.Sprintf("verdicts.rules[%d].channels", i), Message: "at least one channel is required"}
		}
	}

	return nil
}

// SaveConfig writes the configuration to a file (YAML format)
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
			Host: "0.0.0.0",
			Port: 8443,
			TLS: TLSServerConfig{
				Enabled: true,
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
			Type: "local",
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
