package core

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration for AnubisWatch
type Config struct {
	Server      ServerConfig     `yaml:"server"`
	Storage     StorageConfig    `yaml:"storage"`
	Necropolis  NecropolisConfig `yaml:"necropolis"`
	Tenants     TenantsConfig    `yaml:"tenants"`
	Auth        AuthConfig       `yaml:"auth"`
	Dashboard   DashboardConfig  `yaml:"dashboard"`
	Souls       []Soul           `yaml:"souls"`
	Channels    []ChannelConfig  `yaml:"channels"`
	Verdicts    VerdictsConfig   `yaml:"verdicts"`
	Journeys    []JourneyConfig  `yaml:"journeys"`
	Logging     LoggingConfig    `yaml:"logging"`
	Telemetry   TelemetryConfig  `yaml:"telemetry"`
	Security    SecurityConfig   `yaml:"security"`
	Probe       ProbeConfig      `yaml:"probe" json:"probe"`
	Mail        MailConfig       `yaml:"mail" json:"mail"` // outgoing SMTP for subscriptions and password resets
	Alerting    AlertingConfig   `yaml:"alerting" json:"alerting"`
	Environment string           `yaml:"environment"`
}

// DefaultMaxConcurrentChecks is the probe engine's concurrency limit when
// probe.max_concurrent_checks is unset (0).
const DefaultMaxConcurrentChecks = 100

// MaxMaxConcurrentChecks bounds probe.max_concurrent_checks. Each in-flight
// check holds a goroutine and usually a socket; beyond this a typo (an extra
// zero or two) would exhaust file descriptors long before it helped.
const MaxMaxConcurrentChecks = 10000

// ProbeConfig tunes the local probe engine (the Jackal).
type ProbeConfig struct {
	// MaxConcurrentChecks caps how many checks (scheduled and manual) run at
	// once on this node. 0 means DefaultMaxConcurrentChecks (100); the valid
	// explicit range is 1..MaxMaxConcurrentChecks.
	MaxConcurrentChecks int `yaml:"max_concurrent_checks" json:"max_concurrent_checks"`
	// FlapDetection is the default flap-detection policy; a soul's own
	// flap_detection block overrides it. Disabled unless enabled here or per soul.
	FlapDetection FlapDetectionConfig `yaml:"flap_detection" json:"flap_detection"`
}

// EffectiveMaxConcurrentChecks returns the configured limit, or the default
// when unset.
func (c ProbeConfig) EffectiveMaxConcurrentChecks() int {
	if c.MaxConcurrentChecks <= 0 {
		return DefaultMaxConcurrentChecks
	}
	return c.MaxConcurrentChecks
}

func (c ProbeConfig) validate() error {
	if c.MaxConcurrentChecks < 0 || c.MaxConcurrentChecks > MaxMaxConcurrentChecks {
		return &ConfigError{
			Field:   "probe.max_concurrent_checks",
			Message: fmt.Sprintf("must be between 1 and %d (or 0 for the default of %d), got %d", MaxMaxConcurrentChecks, DefaultMaxConcurrentChecks, c.MaxConcurrentChecks),
		}
	}
	return c.FlapDetection.validate("probe.flap_detection")
}

// SecurityConfig controls process-wide security gates. Every per-soul
// "insecure" toggle (InsecureSkipVerify on HTTP/SMTP/IMAP/gRPC/WebSocket
// checks) is AND-ed with the matching field here, so a process started
// in production refuses to honour insecure flags regardless of what a
// tampered config file or stored soul record claims. The default for every
// field is the secure option (false / disabled).
//
// To override at runtime:
//   - CLI:    anubis serve --insecure-skip-verify
//   - ENV:    ANUBIS_ALLOW_INSECURE_TLS=1
//   - CONFIG: security.allow_insecure_skip_verify: true
//
// K7: this closes a MITM vector where a misconfigured soul (or a config
// file injected via a compromised admin path) could silently disable TLS
// verification for individual checks.
type SecurityConfig struct {
	// AllowInsecureSkipVerify is the master switch for TLS verification.
	// When false (the default), every probe checker refuses to honour
	// per-soul InsecureSkipVerify=true and instead fails the check
	// with a clear error. Set to true only in dev/test environments.
	AllowInsecureSkipVerify bool `yaml:"allow_insecure_skip_verify" json:"allow_insecure_skip_verify"`

	// AllowPrivateTargets lets probes and alert channels reach private and
	// loopback ranges (10/8, 172.16/12, 192.168/16, 127/8, fc00::/7, ::1).
	// Off by default as SSRF protection; turn it on to monitor LAN services.
	// Cloud metadata endpoints and link-local ranges stay blocked regardless.
	// Also enabled by ANUBIS_SSRF_ALLOW_PRIVATE=1.
	AllowPrivateTargets bool `yaml:"allow_private_targets" json:"allow_private_targets"`

	// AllowedNetworks is a narrower alternative to AllowPrivateTargets: CIDR
	// ranges (e.g. "10.20.0.0/16") that probes may reach even though they are
	// private.
	AllowedNetworks []string `yaml:"allowed_networks" json:"allowed_networks"`
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

// expandEnvVars expands ${VAR}, ${VAR:-default}, and ${VAR:+alternate} syntax in config.
var envVarRegex = regexp.MustCompile(`\$\{([^}]+)\}`)

func expandEnvVars(s string) string {
	return envVarRegex.ReplaceAllStringFunc(s, func(match string) string {
		inner := match[2 : len(match)-1] // strip ${ and }
		defaultIndex := strings.Index(inner, ":-")
		alternateIndex := strings.Index(inner, ":+")
		if alternateIndex >= 0 && (defaultIndex < 0 || alternateIndex < defaultIndex) {
			key := inner[:alternateIndex]
			if _, set := os.LookupEnv(key); set {
				return inner[alternateIndex+2:]
			}
			return ""
		}

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
	maxInt := int(^uint(0) >> 1)
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid integer: %s", s)
		}
		digit := int(c - '0')
		if result > (maxInt-digit)/10 {
			return 0, fmt.Errorf("integer out of range: %s", s)
		}
		result = result*10 + digit
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
	// Storage defaults
	if c.Storage.Path == "" {
		// Check environment variable first
		if envDir := os.Getenv("ANUBIS_DATA_DIR"); envDir != "" {
			c.Storage.Path = envDir
		} else {
			c.Storage.Path = "/var/lib/anubis/data"
		}
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
		// 30d, well inside the 365d hourly retention: a day is rolled up only
		// once all its hours are past this threshold, so a threshold equal to
		// the hourly retention would have its first hours deleted first.
		c.Storage.TimeSeries.Compaction.HourToDay.Duration = 30 * 24 * 60 * 60 * 1e9 // 30d
	}
	if c.Storage.TimeSeries.Retention.Raw.Duration == 0 && c.Storage.RetentionDays > 0 {
		c.Storage.TimeSeries.Retention.Raw.Duration = time.Duration(c.Storage.RetentionDays) * 24 * time.Hour
	}
	if c.Storage.TimeSeries.Retention.Raw.Duration == 0 {
		// 90d: status pages draw 90 days of uptime bars, and soul logs and
		// dashboard queries read raw judgments too. A shorter default would
		// delete history those views still show.
		c.Storage.TimeSeries.Retention.Raw.Duration = 90 * 24 * 60 * 60 * 1e9 // 90d
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
	if c.Necropolis.Raft.CommitTimeout.Duration == 0 {
		c.Necropolis.Raft.CommitTimeout.Duration = 50 * 1e6 // 50ms
	}
	if c.Necropolis.Raft.MaxAppendEntries == 0 {
		c.Necropolis.Raft.MaxAppendEntries = 64
	}
	if c.Necropolis.Raft.TrailingLogs == 0 {
		c.Necropolis.Raft.TrailingLogs = 1024
	}
	if c.Necropolis.Raft.SnapshotInterval.Duration == 0 {
		c.Necropolis.Raft.SnapshotInterval.Duration = 300 * 1e9 // 300s
	}
	if c.Necropolis.Raft.SnapshotThreshold == 0 {
		c.Necropolis.Raft.SnapshotThreshold = 8192
	}
	if c.Necropolis.Distribution.Strategy == "" {
		// Hash ownership moves only the souls of a node that joins or leaves;
		// round-robin keeps counts exactly even but reshuffles owners whenever
		// a soul is added or removed (see cluster.PlanOwnership).
		c.Necropolis.Distribution.Strategy = "hash"
	}
	if c.Necropolis.Distribution.Redundancy == 0 {
		c.Necropolis.Distribution.Redundancy = 1
	}
	if c.Necropolis.Distribution.RebalanceInterval.Duration == 0 {
		c.Necropolis.Distribution.RebalanceInterval.Duration = 60 * 1e9 // 60s
	}

	// Journey defaults match the REST creation path.
	for i := range c.Journeys {
		journey := &c.Journeys[i]
		if journey.Weight.Duration <= 0 {
			journey.Weight.Duration = 60 * 1e9
		}
		if journey.Timeout.Duration <= 0 {
			journey.Timeout.Duration = 30 * 1e9
		}
		for j := range journey.Steps {
			step := &journey.Steps[j]
			if step.Type == "" {
				step.Type = CheckHTTP
			}
			if step.Timeout.Duration <= 0 {
				step.Timeout.Duration = 10 * 1e9
			}
		}
	}

	// Tenants defaults
	if c.Tenants.Isolation == "" {
		c.Tenants.Isolation = "strict"
	}
	// Quotas are not defaulted: 0 means unlimited, and they apply only when
	// tenants.enabled is set. They were filled with 100/20/10/... here while
	// nothing enforced them; defaulting them now that they are enforced would
	// suddenly block existing installs that turned tenants on.

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
	// Auto-enable auth when credentials/issuer is configured (user can still set
	// enabled=false explicitly to disable). This makes the product secure by default.
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

	if err := c.Probe.validate(); err != nil {
		return err
	}

	if err := c.validateMail(); err != nil {
		return err
	}
	if err := c.Alerting.validate(); err != nil {
		return err
	}

	// Production deployments must not expose bearer credentials over a
	// plaintext management listener. HTTP may be plaintext in the explicit
	// production-proxied topology because the ingress terminates TLS, but gRPC
	// is not routed by the shipped ingress and therefore remains disabled unless
	// the process itself has TLS enabled.
	environment := strings.ToLower(strings.TrimSpace(c.Environment))
	if environment == "production" && !c.Server.TLS.Enabled {
		return fmt.Errorf("config error: server.tls.enabled must be true when environment is \"production\" (use environment=\"production-proxied\" when HTTP TLS is terminated upstream)")
	}
	if (environment == "production" || environment == "production-proxied") && c.Server.GRPCPort > 0 && !c.Server.TLS.Enabled && !isLoopbackHost(c.Server.Host) {
		return fmt.Errorf("config error: server.grpc_port must be 0, server.tls.enabled must be true, or server.host must be a loopback IP in %q; bearer-token gRPC must not use plaintext transport", environment)
	}

	// K9: gossip messages are authenticated by a shared-secret HMAC. With no
	// secret the receiver accepts unsigned messages, so anyone who can reach
	// the gossip port can inject peers. Fail fast here with an actionable
	// message; raft.NewDiscovery enforces the same rule at startup for
	// clustering enabled via CLI flags (which are applied after validation).
	// Discovery mode "manual" never gossips, so it is exempt.
	if c.Necropolis.Enabled && !c.Necropolis.SingleNode && c.Necropolis.gossips() &&
		strings.TrimSpace(c.Necropolis.ClusterSecret) == "" {
		return fmt.Errorf("config error: necropolis.cluster_secret must be set when necropolis.enabled is true and peer discovery is active (set ANUBIS_CLUSTER_SECRET, or use necropolis.discovery.mode=\"manual\" to disable gossip)")
	}

	return nil
}

// isLoopbackHost deliberately accepts only literal loopback IPs. Hostnames such
// as "localhost" can be reconfigured or resolved inconsistently across
// environments, so they are not a sufficiently explicit plaintext exception.
func isLoopbackHost(host string) bool {
	ip := net.ParseIP(strings.TrimSpace(strings.Trim(host, "[]")))
	return ip != nil && ip.IsLoopback()
}

// saveConfigMarshal is overwritten by tests to inject marshal failures.
var saveConfigMarshal = func(path string, config *Config) ([]byte, error) {
	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		return yaml.Marshal(config)
	}
	return json.MarshalIndent(config, "", "  ")
}

func SaveConfig(path string, config *Config) error {
	data, err := saveConfigMarshal(path, config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	// Config files often carry secrets (admin password, encryption
	// key, cluster secret). G306: tighten permissions to 0600 so the
	// file isn't world-readable on a multi-user host.
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	return nil
}

// GenerateDefaultConfig creates a default configuration file
func GenerateDefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:     "0.0.0.0",
			Port:     8443,
			GRPCPort: 0,
			TLS: TLSServerConfig{
				Enabled: false,
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
			Enabled: BoolPtr(true),
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
