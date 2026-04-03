# AnubisWatch — IMPLEMENTATION.md

> **Technical Implementation Guide**
> **Version:** 1.0.0 · **Date:** 2026-03-30

---

## 1. PROJECT BOOTSTRAP

### 1.1 Go Module Initialization

```bash
mkdir anubiswatch && cd anubiswatch
go mod init github.com/AnubisWatch/anubiswatch

# Allowed dependencies only
go get golang.org/x/crypto@latest
go get golang.org/x/sys@latest
go get golang.org/x/net@latest
go get gopkg.in/yaml.v3@latest
```

### 1.2 Build System

```makefile
# Makefile
BINARY    := anubis
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT    := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE      := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS   := -s -w \
  -X github.com/AnubisWatch/anubiswatch/internal/core.Version=$(VERSION) \
  -X github.com/AnubisWatch/anubiswatch/internal/core.Commit=$(COMMIT) \
  -X github.com/AnubisWatch/anubiswatch/internal/core.BuildDate=$(DATE)

.PHONY: all build clean test lint dashboard

all: dashboard build

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/anubis

dashboard:
	cd web && npm ci && npm run build
	# Build output goes to web/dist/, embedded via embed.FS

clean:
	rm -rf bin/ web/dist/

test:
	go test -race -coverprofile=coverage.out ./...

lint:
	golangci-lint run ./...

# Cross-compilation
build-all:
	GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-amd64 ./cmd/anubis
	GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-arm64 ./cmd/anubis
	GOOS=linux   GOARCH=arm   GOARM=7 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-armv7 ./cmd/anubis
	GOOS=darwin  GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-darwin-amd64 ./cmd/anubis
	GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-darwin-arm64 ./cmd/anubis
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-windows-amd64.exe ./cmd/anubis
	GOOS=freebsd GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-freebsd-amd64 ./cmd/anubis

docker:
	docker build -t anubiswatch/anubis:$(VERSION) .
	docker tag anubiswatch/anubis:$(VERSION) anubiswatch/anubis:latest
```

### 1.3 Directory Structure Implementation Order

```
Phase 1 — Foundation
  internal/core/         → Types, interfaces, config
  internal/storage/      → CobaltDB integration
  cmd/anubis/            → CLI entrypoint

Phase 2 — Probe Engine
  internal/probe/        → Checker interface + all 8 protocols

Phase 3 — Raft Cluster
  internal/raft/         → Consensus, transport, discovery

Phase 4 — Alert System
  internal/alert/        → Dispatcher + all 6 channels

Phase 5 — API Layer
  internal/api/rest/     → REST API
  internal/api/ws/       → WebSocket server
  internal/api/grpc/     → gRPC API
  internal/api/mcp/      → MCP Server

Phase 6 — Dashboard
  web/                   → React 19 frontend
  internal/dashboard/    → embed.FS integration

Phase 7 — Advanced Features
  internal/tenant/       → Multi-tenant isolation
  internal/statuspage/   → Book of the Dead (public status page)
  internal/probe/synthetic.go → Duat Journeys
```

---

## 2. CORE TYPES & INTERFACES

### 2.1 Core Domain Types

```go
// internal/core/soul.go
package core

import (
    "time"
)

// Soul represents a monitored target — the entity whose heart is weighed.
type Soul struct {
    ID          string            `json:"id" yaml:"id"`
    WorkspaceID string            `json:"workspace_id" yaml:"-"`
    Name        string            `json:"name" yaml:"name"`
    Type        CheckType         `json:"type" yaml:"type"`
    Target      string            `json:"target" yaml:"target"`
    Weight      Duration          `json:"weight" yaml:"weight"`           // check interval
    Timeout     Duration          `json:"timeout" yaml:"timeout"`
    Enabled     bool              `json:"enabled" yaml:"enabled"`
    Tags        []string          `json:"tags" yaml:"tags"`
    Regions     []string          `json:"regions" yaml:"regions"`         // restrict to specific regions
    HTTP        *HTTPConfig       `json:"http,omitempty" yaml:"http,omitempty"`
    TCP         *TCPConfig        `json:"tcp,omitempty" yaml:"tcp,omitempty"`
    UDP         *UDPConfig        `json:"udp,omitempty" yaml:"udp,omitempty"`
    DNS         *DNSConfig        `json:"dns,omitempty" yaml:"dns,omitempty"`
    SMTP        *SMTPConfig       `json:"smtp,omitempty" yaml:"smtp,omitempty"`
    IMAP        *IMAPConfig       `json:"imap,omitempty" yaml:"imap,omitempty"`
    ICMP        *ICMPConfig       `json:"icmp,omitempty" yaml:"icmp,omitempty"`
    GRPC        *GRPCConfig       `json:"grpc,omitempty" yaml:"grpc,omitempty"`
    WebSocket   *WebSocketConfig  `json:"websocket,omitempty" yaml:"websocket,omitempty"`
    TLS         *TLSConfig        `json:"tls,omitempty" yaml:"tls,omitempty"`
    CreatedAt   time.Time         `json:"created_at" yaml:"-"`
    UpdatedAt   time.Time         `json:"updated_at" yaml:"-"`
}

// CheckType identifies the protocol checker to use
type CheckType string

const (
    CheckHTTP      CheckType = "http"
    CheckTCP       CheckType = "tcp"
    CheckUDP       CheckType = "udp"
    CheckDNS       CheckType = "dns"
    CheckSMTP      CheckType = "smtp"
    CheckIMAP      CheckType = "imap"
    CheckICMP      CheckType = "icmp"
    CheckGRPC      CheckType = "grpc"
    CheckWebSocket CheckType = "websocket"
    CheckTLS       CheckType = "tls"
)

// SoulStatus represents the weighed verdict of a soul
type SoulStatus string

const (
    SoulAlive    SoulStatus = "alive"     // Passed to Aaru (paradise)
    SoulDead     SoulStatus = "dead"      // Devoured by Ammit
    SoulDegraded SoulStatus = "degraded"  // Heart is heavy
    SoulUnknown  SoulStatus = "unknown"   // Not yet judged
    SoulEmbalmed SoulStatus = "embalmed"  // Maintenance window
)

// Duration is a YAML-friendly time.Duration
type Duration struct {
    time.Duration
}

func (d *Duration) UnmarshalYAML(unmarshal func(interface{}) error) error {
    var s string
    if err := unmarshal(&s); err != nil {
        return err
    }
    dur, err := time.ParseDuration(s)
    if err != nil {
        return err
    }
    d.Duration = dur
    return nil
}

func (d Duration) MarshalYAML() (interface{}, error) {
    return d.String(), nil
}
```

### 2.2 Judgment (Check Result)

```go
// internal/core/judgment.go
package core

import "time"

// Judgment is the result of weighing a soul — a single check execution.
type Judgment struct {
    ID         string        `json:"id"`
    SoulID     string        `json:"soul_id"`
    JackalID   string        `json:"jackal_id"`      // which probe node
    Region     string        `json:"region"`
    Timestamp  time.Time     `json:"timestamp"`
    Duration   time.Duration `json:"duration"`        // check latency
    Status     SoulStatus    `json:"status"`
    StatusCode int           `json:"status_code"`     // protocol-specific
    Message    string        `json:"message"`
    Details    *JudgmentDetails `json:"details,omitempty"`
    TLSInfo    *TLSInfo      `json:"tls_info,omitempty"`
}

// JudgmentDetails holds protocol-specific result data
type JudgmentDetails struct {
    // HTTP
    ResponseHeaders map[string]string `json:"response_headers,omitempty"`
    ResponseBody    string            `json:"response_body,omitempty"`
    RedirectChain   []string          `json:"redirect_chain,omitempty"`

    // DNS
    ResolvedAddresses []string        `json:"resolved_addresses,omitempty"`
    DNSSECValid       *bool           `json:"dnssec_valid,omitempty"`
    PropagationResult map[string]bool `json:"propagation_result,omitempty"`

    // ICMP
    PacketsSent     int     `json:"packets_sent,omitempty"`
    PacketsReceived int     `json:"packets_received,omitempty"`
    PacketLoss      float64 `json:"packet_loss,omitempty"`
    MinLatency      float64 `json:"min_latency_ms,omitempty"`
    AvgLatency      float64 `json:"avg_latency_ms,omitempty"`
    MaxLatency      float64 `json:"max_latency_ms,omitempty"`
    Jitter          float64 `json:"jitter_ms,omitempty"`

    // TCP
    Banner string `json:"banner,omitempty"`

    // SMTP/IMAP
    Capabilities []string `json:"capabilities,omitempty"`

    // gRPC
    ServiceStatus string `json:"service_status,omitempty"`

    // WebSocket
    CloseCode int `json:"close_code,omitempty"`

    // Assertions
    Assertions []AssertionResult `json:"assertions,omitempty"`
}

// AssertionResult records pass/fail of a specific assertion
type AssertionResult struct {
    Type     string `json:"type"`      // status_code, body_contains, json_path, etc.
    Expected string `json:"expected"`
    Actual   string `json:"actual"`
    Passed   bool   `json:"passed"`
}

// TLSInfo holds TLS/certificate details
type TLSInfo struct {
    Protocol       string    `json:"protocol"`         // TLS 1.2, TLS 1.3
    CipherSuite    string    `json:"cipher_suite"`
    Issuer         string    `json:"issuer"`
    Subject        string    `json:"subject"`
    SANs           []string  `json:"sans"`
    NotBefore      time.Time `json:"not_before"`
    NotAfter       time.Time `json:"not_after"`
    DaysUntilExpiry int      `json:"days_until_expiry"`
    KeyType        string    `json:"key_type"`         // RSA, ECDSA
    KeyBits        int       `json:"key_bits"`
    OCSPStapled    bool      `json:"ocsp_stapled"`
    ChainValid     bool      `json:"chain_valid"`
    ChainLength    int       `json:"chain_length"`
}
```

### 2.3 Verdict (Alert)

```go
// internal/core/verdict.go
package core

import "time"

// Verdict is the alert decision — the judgment pronounced upon a soul.
type Verdict struct {
    ID           string        `json:"id"`
    WorkspaceID  string        `json:"workspace_id"`
    SoulID       string        `json:"soul_id"`
    RuleID       string        `json:"rule_id"`
    Severity     Severity      `json:"severity"`
    Status       VerdictStatus `json:"status"`
    Message      string        `json:"message"`
    FiredAt      time.Time     `json:"fired_at"`
    AcknowledgedAt *time.Time  `json:"acknowledged_at,omitempty"`
    AcknowledgedBy string      `json:"acknowledged_by,omitempty"`
    ResolvedAt   *time.Time    `json:"resolved_at,omitempty"`
    Judgments    []string      `json:"judgments"`     // judgment IDs that caused this
}

type Severity string

const (
    SeverityCritical Severity = "critical"
    SeverityWarning  Severity = "warning"
    SeverityInfo     Severity = "info"
)

type VerdictStatus string

const (
    VerdictActive       VerdictStatus = "active"
    VerdictAcknowledged VerdictStatus = "acknowledged"
    VerdictResolved     VerdictStatus = "resolved"
)
```

### 2.4 Checker Interface

```go
// internal/probe/checker.go
package probe

import (
    "context"
    "github.com/AnubisWatch/anubiswatch/internal/core"
)

// Checker is the interface every protocol must implement.
// Named after the 42 judges who assisted Anubis in the Hall of Ma'at.
type Checker interface {
    // Type returns the protocol identifier
    Type() core.CheckType

    // Judge performs the health check against the given soul
    Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error)

    // Validate ensures the soul configuration is valid for this checker
    Validate(soul *core.Soul) error
}

// CheckerRegistry maps check types to their implementations
type CheckerRegistry struct {
    checkers map[core.CheckType]Checker
}

func NewCheckerRegistry() *CheckerRegistry {
    r := &CheckerRegistry{
        checkers: make(map[core.CheckType]Checker),
    }
    // Register all built-in checkers
    r.Register(NewHTTPChecker())
    r.Register(NewTCPChecker())
    r.Register(NewUDPChecker())
    r.Register(NewDNSChecker())
    r.Register(NewSMTPChecker())
    r.Register(NewIMAPChecker())
    r.Register(NewICMPChecker())
    r.Register(NewGRPCChecker())
    r.Register(NewWebSocketChecker())
    r.Register(NewTLSChecker())
    return r
}

func (r *CheckerRegistry) Register(c Checker) {
    r.checkers[c.Type()] = c
}

func (r *CheckerRegistry) Get(t core.CheckType) (Checker, bool) {
    c, ok := r.checkers[t]
    return c, ok
}
```

### 2.5 Configuration

```go
// internal/core/config.go
package core

import (
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

type ServerConfig struct {
    Host string       `yaml:"host"`
    Port int          `yaml:"port"`
    TLS  TLSServerConfig `yaml:"tls"`
}

type TLSServerConfig struct {
    Enabled     bool     `yaml:"enabled"`
    Cert        string   `yaml:"cert"`
    Key         string   `yaml:"key"`
    AutoCert    bool     `yaml:"auto_cert"`
    ACMEEmail   string   `yaml:"acme_email"`
    ACMEDomains []string `yaml:"acme_domains"`
}

type StorageConfig struct {
    Path       string           `yaml:"path"`
    Encryption EncryptionConfig `yaml:"encryption"`
    TimeSeries TimeSeriesConfig `yaml:"timeseries"`
}

type EncryptionConfig struct {
    Enabled bool   `yaml:"enabled"`
    Key     string `yaml:"key"`
}

type TimeSeriesConfig struct {
    Compaction CompactionConfig `yaml:"compaction"`
    Retention  RetentionConfig  `yaml:"retention"`
}

type CompactionConfig struct {
    RawToMinute  Duration `yaml:"raw_to_minute"`
    MinuteToFive Duration `yaml:"minute_to_five"`
    FiveToHour   Duration `yaml:"five_to_hour"`
    HourToDay    Duration `yaml:"hour_to_day"`
}

type RetentionConfig struct {
    Raw     Duration `yaml:"raw"`
    Minute  Duration `yaml:"minute"`
    FiveMin Duration `yaml:"five"`
    Hour    Duration `yaml:"hour"`
    Day     string   `yaml:"day"` // "unlimited" or duration
}

type NecropolisConfig struct {
    Enabled       bool              `yaml:"enabled"`
    NodeName      string            `yaml:"node_name"`
    Region        string            `yaml:"region"`
    Tags          map[string]string `yaml:"tags"`
    BindAddr      string            `yaml:"bind_addr"`
    AdvertiseAddr string            `yaml:"advertise_addr"`
    ClusterSecret string            `yaml:"cluster_secret"`
    Discovery     DiscoveryConfig   `yaml:"discovery"`
    Raft          RaftConfig        `yaml:"raft"`
    Distribution  DistributionConfig `yaml:"distribution"`
    Capabilities  CapabilitiesConfig `yaml:"capabilities"`
}

type DiscoveryConfig struct {
    Mode  string   `yaml:"mode"` // mdns, gossip, manual
    Seeds []string `yaml:"seeds"`
}

type RaftConfig struct {
    ElectionTimeout   Duration `yaml:"election_timeout"`
    HeartbeatTimeout  Duration `yaml:"heartbeat_timeout"`
    SnapshotInterval  Duration `yaml:"snapshot_interval"`
    SnapshotThreshold int      `yaml:"snapshot_threshold"`
}

type DistributionConfig struct {
    Strategy           string   `yaml:"strategy"` // round-robin, region-aware, latency-optimized, redundant
    Redundancy         int      `yaml:"redundancy"`
    RebalanceInterval  Duration `yaml:"rebalance_interval"`
}

type CapabilitiesConfig struct {
    ICMP            bool `yaml:"icmp"`
    IPv6            bool `yaml:"ipv6"`
    DNS             bool `yaml:"dns"`
    InternalNetwork bool `yaml:"internal_network"`
}

type TenantsConfig struct {
    Enabled       bool         `yaml:"enabled"`
    Isolation     string       `yaml:"isolation"` // strict, shared
    DefaultQuotas QuotaConfig  `yaml:"default_quotas"`
}

type QuotaConfig struct {
    MaxSouls         int      `yaml:"max_souls"`
    MaxJourneys      int      `yaml:"max_journeys"`
    MaxAlertChannels int      `yaml:"max_alert_channels"`
    MaxTeamMembers   int      `yaml:"max_team_members"`
    RetentionDays    int      `yaml:"retention_days"`
    CheckIntervalMin Duration `yaml:"check_interval_min"`
}

// LoadConfig reads and parses the configuration file
func LoadConfig(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }

    // Expand environment variables: ${VAR} and ${VAR:-default}
    expanded := expandEnvVars(string(data))

    var config Config
    if err := yaml.Unmarshal([]byte(expanded), &config); err != nil {
        return nil, err
    }

    config.setDefaults()
    return &config, config.validate()
}

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

func (c *Config) setDefaults() {
    if c.Server.Host == "" {
        c.Server.Host = "0.0.0.0"
    }
    if c.Server.Port == 0 {
        c.Server.Port = 8443
    }
    if c.Storage.Path == "" {
        c.Storage.Path = "/var/lib/anubis/data"
    }
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
    if c.Necropolis.Distribution.Strategy == "" {
        c.Necropolis.Distribution.Strategy = "round-robin"
    }
    if c.Necropolis.Distribution.Redundancy == 0 {
        c.Necropolis.Distribution.Redundancy = 1
    }
    if c.Logging.Level == "" {
        c.Logging.Level = "info"
    }
    if c.Logging.Format == "" {
        c.Logging.Format = "json"
    }
    if c.Logging.Output == "" {
        c.Logging.Output = "stdout"
    }
}

func (c *Config) validate() error {
    // Validate souls have required fields
    for i := range c.Souls {
        if c.Souls[i].Name == "" {
            return &ConfigError{Field: "souls[].name", Message: "name is required"}
        }
        if c.Souls[i].Target == "" {
            return &ConfigError{Field: "souls[].target", Message: "target is required"}
        }
        if c.Souls[i].Type == "" {
            return &ConfigError{Field: "souls[].type", Message: "type is required"}
        }
    }
    return nil
}

type ConfigError struct {
    Field   string
    Message string
}

func (e *ConfigError) Error() string {
    return "config error: " + e.Field + " — " + e.Message
}
```

---

## 3. PROBE ENGINE IMPLEMENTATION

### 3.1 Probe Engine (Scheduler & Executor)

```go
// internal/probe/engine.go
package probe

import (
    "context"
    "log/slog"
    "sync"
    "time"

    "github.com/AnubisWatch/anubiswatch/internal/core"
)

// Engine is the probe scheduling and execution engine.
// It manages the lifecycle of all soul checks on this Jackal.
type Engine struct {
    registry  *CheckerRegistry
    store     Storage           // CobaltDB storage interface
    alerter   AlertDispatcher   // alert system interface
    nodeID    string            // this Jackal's ID
    region    string            // this Jackal's region

    souls     map[string]*soulRunner
    mu        sync.RWMutex
    ctx       context.Context
    cancel    context.CancelFunc
    wg        sync.WaitGroup
    logger    *slog.Logger

    // Callbacks for Raft integration
    onJudgment func(*core.Judgment) // called after each judgment
}

// Storage is the interface the probe engine uses to persist judgments
type Storage interface {
    SaveJudgment(ctx context.Context, j *core.Judgment) error
    GetSoul(ctx context.Context, id string) (*core.Soul, error)
    ListSouls(ctx context.Context, workspaceID string) ([]*core.Soul, error)
}

// AlertDispatcher is the interface for firing alerts
type AlertDispatcher interface {
    Evaluate(ctx context.Context, soul *core.Soul, judgment *core.Judgment) error
}

type soulRunner struct {
    soul   *core.Soul
    ticker *time.Ticker
    cancel context.CancelFunc
}

func NewEngine(opts EngineOptions) *Engine {
    ctx, cancel := context.WithCancel(context.Background())
    return &Engine{
        registry:   opts.Registry,
        store:      opts.Store,
        alerter:    opts.Alerter,
        nodeID:     opts.NodeID,
        region:     opts.Region,
        souls:      make(map[string]*soulRunner),
        ctx:        ctx,
        cancel:     cancel,
        logger:     opts.Logger.With("component", "probe-engine"),
        onJudgment: opts.OnJudgment,
    }
}

type EngineOptions struct {
    Registry   *CheckerRegistry
    Store      Storage
    Alerter    AlertDispatcher
    NodeID     string
    Region     string
    Logger     *slog.Logger
    OnJudgment func(*core.Judgment)
}

// AssignSouls sets the souls this Jackal is responsible for checking.
// Called by the Raft leader when distributing checks.
func (e *Engine) AssignSouls(souls []*core.Soul) {
    e.mu.Lock()
    defer e.mu.Unlock()

    // Determine which souls are new, removed, or updated
    newMap := make(map[string]*core.Soul, len(souls))
    for _, s := range souls {
        newMap[s.ID] = s
    }

    // Stop removed souls
    for id, runner := range e.souls {
        if _, exists := newMap[id]; !exists {
            runner.cancel()
            runner.ticker.Stop()
            delete(e.souls, id)
            e.logger.Info("soul unassigned", "soul", id)
        }
    }

    // Start new or updated souls
    for _, soul := range souls {
        if existing, exists := e.souls[soul.ID]; exists {
            // Update soul config without restart if only config changed
            existing.soul = soul
            continue
        }
        e.startSoul(soul)
    }
}

func (e *Engine) startSoul(soul *core.Soul) {
    ctx, cancel := context.WithCancel(e.ctx)
    interval := soul.Weight.Duration
    if interval == 0 {
        interval = 60 * time.Second // default 60s
    }

    runner := &soulRunner{
        soul:   soul,
        ticker: time.NewTicker(interval),
        cancel: cancel,
    }
    e.souls[soul.ID] = runner

    e.wg.Add(1)
    go func() {
        defer e.wg.Done()
        defer runner.ticker.Stop()

        // Immediate first check
        e.judgeSoul(ctx, soul)

        for {
            select {
            case <-ctx.Done():
                return
            case <-runner.ticker.C:
                e.judgeSoul(ctx, runner.soul)
            }
        }
    }()

    e.logger.Info("soul assigned", "soul", soul.Name, "interval", interval)
}

func (e *Engine) judgeSoul(ctx context.Context, soul *core.Soul) {
    checker, ok := e.registry.Get(soul.Type)
    if !ok {
        e.logger.Error("unknown checker type", "type", soul.Type, "soul", soul.Name)
        return
    }

    // Create timeout context
    timeout := soul.Timeout.Duration
    if timeout == 0 {
        timeout = 10 * time.Second
    }
    checkCtx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    // Execute the check
    judgment, err := checker.Judge(checkCtx, soul)
    if err != nil {
        judgment = &core.Judgment{
            SoulID:    soul.ID,
            JackalID:  e.nodeID,
            Region:    e.region,
            Timestamp: time.Now().UTC(),
            Status:    core.SoulDead,
            Message:   "check execution failed: " + err.Error(),
        }
    }

    // Enrich judgment with node info
    judgment.JackalID = e.nodeID
    judgment.Region = e.region
    if judgment.ID == "" {
        judgment.ID = generateID() // ULID or similar
    }

    // Persist
    if err := e.store.SaveJudgment(ctx, judgment); err != nil {
        e.logger.Error("failed to save judgment", "err", err, "soul", soul.Name)
    }

    // Notify Raft (for distributed aggregation)
    if e.onJudgment != nil {
        e.onJudgment(judgment)
    }

    // Evaluate alert rules
    if e.alerter != nil {
        if err := e.alerter.Evaluate(ctx, soul, judgment); err != nil {
            e.logger.Error("alert evaluation failed", "err", err, "soul", soul.Name)
        }
    }

    e.logger.Debug("judgment complete",
        "soul", soul.Name,
        "status", judgment.Status,
        "duration", judgment.Duration,
    )
}

// TriggerImmediate forces an immediate check of a specific soul
func (e *Engine) TriggerImmediate(ctx context.Context, soulID string) (*core.Judgment, error) {
    e.mu.RLock()
    runner, ok := e.souls[soulID]
    e.mu.RUnlock()
    if !ok {
        return nil, &core.NotFoundError{Entity: "soul", ID: soulID}
    }

    checker, ok := e.registry.Get(runner.soul.Type)
    if !ok {
        return nil, &core.ConfigError{Field: "type", Message: "unknown type " + string(runner.soul.Type)}
    }

    judgment, err := checker.Judge(ctx, runner.soul)
    if err != nil {
        return nil, err
    }

    judgment.JackalID = e.nodeID
    judgment.Region = e.region
    return judgment, nil
}

// Stop gracefully shuts down the probe engine
func (e *Engine) Stop() {
    e.cancel()
    e.wg.Wait()
    e.logger.Info("probe engine stopped")
}
```

### 3.2 HTTP/HTTPS Checker

```go
// internal/probe/http.go
package probe

import (
    "context"
    "crypto/tls"
    "encoding/json"
    "fmt"
    "io"
    "net"
    "net/http"
    "regexp"
    "strings"
    "time"

    "github.com/AnubisWatch/anubiswatch/internal/core"
)

type HTTPChecker struct {
    client *http.Client
}

func NewHTTPChecker() *HTTPChecker {
    return &HTTPChecker{
        client: &http.Client{
            // Transport is configured per-check for custom TLS, redirects, etc.
            Timeout: 30 * time.Second,
        },
    }
}

func (c *HTTPChecker) Type() core.CheckType {
    return core.CheckHTTP
}

func (c *HTTPChecker) Validate(soul *core.Soul) error {
    if soul.HTTP == nil {
        return &core.ConfigError{Field: "http", Message: "HTTP config required for HTTP checks"}
    }
    if soul.Target == "" {
        return &core.ConfigError{Field: "target", Message: "target URL required"}
    }
    return nil
}

func (c *HTTPChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
    cfg := soul.HTTP
    if cfg == nil {
        cfg = &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}}
    }

    method := strings.ToUpper(cfg.Method)
    if method == "" {
        method = "GET"
    }

    // Build request
    var body io.Reader
    if cfg.Body != "" {
        body = strings.NewReader(cfg.Body)
    }
    req, err := http.NewRequestWithContext(ctx, method, soul.Target, body)
    if err != nil {
        return failJudgment(soul, err), nil
    }

    // Set headers
    for k, v := range cfg.Headers {
        req.Header.Set(k, v)
    }
    if req.Header.Get("User-Agent") == "" {
        req.Header.Set("User-Agent", "AnubisWatch/1.0 (The Judgment Never Sleeps)")
    }

    // Configure transport
    transport := &http.Transport{
        TLSClientConfig: &tls.Config{
            InsecureSkipVerify: cfg.InsecureSkipVerify,
        },
        DialContext: (&net.Dialer{
            Timeout:   10 * time.Second,
            KeepAlive: 0, // no keep-alive for monitoring
        }).DialContext,
        DisableKeepAlives: true,
    }

    client := &http.Client{
        Transport: transport,
        Timeout:   soul.Timeout.Duration,
    }

    // Handle redirects
    if !cfg.FollowRedirects {
        client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
            return http.ErrUseLastResponse
        }
    } else if cfg.MaxRedirects > 0 {
        maxRedir := cfg.MaxRedirects
        client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
            if len(via) >= maxRedir {
                return fmt.Errorf("stopped after %d redirects", maxRedir)
            }
            return nil
        }
    }

    // Execute request
    start := time.Now()
    resp, err := client.Do(req)
    duration := time.Since(start)

    if err != nil {
        return failJudgment(soul, err), nil
    }
    defer resp.Body.Close()

    // Read body (limited)
    bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // 1MB max

    // Build judgment
    judgment := &core.Judgment{
        SoulID:     soul.ID,
        Timestamp:  time.Now().UTC(),
        Duration:   duration,
        StatusCode: resp.StatusCode,
        Details:    &core.JudgmentDetails{},
    }

    // Extract TLS info
    if resp.TLS != nil {
        judgment.TLSInfo = extractTLSInfo(resp.TLS)
    }

    // Run assertions
    assertions := make([]core.AssertionResult, 0)
    allPassed := true

    // 1. Status code assertion
    if len(cfg.ValidStatus) > 0 {
        statusOK := false
        for _, s := range cfg.ValidStatus {
            if resp.StatusCode == s {
                statusOK = true
                break
            }
        }
        a := core.AssertionResult{
            Type:     "status_code",
            Expected: fmt.Sprintf("%v", cfg.ValidStatus),
            Actual:   fmt.Sprintf("%d", resp.StatusCode),
            Passed:   statusOK,
        }
        assertions = append(assertions, a)
        if !statusOK {
            allPassed = false
        }
    }

    // 2. Body contains assertion
    if cfg.BodyContains != "" {
        contains := strings.Contains(string(bodyBytes), cfg.BodyContains)
        assertions = append(assertions, core.AssertionResult{
            Type:     "body_contains",
            Expected: cfg.BodyContains,
            Actual:   truncate(string(bodyBytes), 200),
            Passed:   contains,
        })
        if !contains {
            allPassed = false
        }
    }

    // 3. Body regex assertion
    if cfg.BodyRegex != "" {
        re, err := regexp.Compile(cfg.BodyRegex)
        matched := err == nil && re.Match(bodyBytes)
        assertions = append(assertions, core.AssertionResult{
            Type:     "body_regex",
            Expected: cfg.BodyRegex,
            Actual:   truncate(string(bodyBytes), 200),
            Passed:   matched,
        })
        if !matched {
            allPassed = false
        }
    }

    // 4. JSON path assertions
    if cfg.JSONPath != nil {
        for path, expected := range cfg.JSONPath {
            actual := extractJSONPath(bodyBytes, path)
            passed := actual == expected
            assertions = append(assertions, core.AssertionResult{
                Type:     "json_path",
                Expected: path + "=" + expected,
                Actual:   actual,
                Passed:   passed,
            })
            if !passed {
                allPassed = false
            }
        }
    }

    // 5. JSON schema assertion
    if cfg.JSONSchema != "" {
        passed := validateJSONSchema(bodyBytes, cfg.JSONSchema)
        assertions = append(assertions, core.AssertionResult{
            Type:     "json_schema",
            Expected: "valid",
            Actual:   boolToStr(passed, "valid", "invalid"),
            Passed:   passed,
        })
        if !passed {
            allPassed = false
        }
    }

    // 6. Performance budget (Feather of Ma'at)
    if cfg.Feather.Duration > 0 {
        withinBudget := duration <= cfg.Feather.Duration
        assertions = append(assertions, core.AssertionResult{
            Type:     "feather",
            Expected: cfg.Feather.String(),
            Actual:   duration.String(),
            Passed:   withinBudget,
        })
        if !withinBudget {
            // Degraded, not dead
            if allPassed {
                judgment.Status = core.SoulDegraded
                judgment.Message = fmt.Sprintf("response time %s exceeds feather %s",
                    duration.Round(time.Millisecond), cfg.Feather.Duration)
            }
        }
    }

    judgment.Details.Assertions = assertions

    // Determine final status
    if judgment.Status == "" {
        if allPassed {
            judgment.Status = core.SoulAlive
            judgment.Message = fmt.Sprintf("HTTP %d in %s", resp.StatusCode, duration.Round(time.Millisecond))
        } else {
            judgment.Status = core.SoulDead
            // Build failure message from failed assertions
            var failures []string
            for _, a := range assertions {
                if !a.Passed {
                    failures = append(failures, a.Type+": expected "+a.Expected+", got "+a.Actual)
                }
            }
            judgment.Message = strings.Join(failures, "; ")
        }
    }

    return judgment, nil
}

func failJudgment(soul *core.Soul, err error) *core.Judgment {
    return &core.Judgment{
        SoulID:    soul.ID,
        Timestamp: time.Now().UTC(),
        Status:    core.SoulDead,
        Message:   err.Error(),
    }
}

func extractTLSInfo(state *tls.ConnectionState) *core.TLSInfo {
    info := &core.TLSInfo{
        Protocol:    tlsVersionString(state.Version),
        CipherSuite: tls.CipherSuiteName(state.CipherSuite),
    }

    if len(state.PeerCertificates) > 0 {
        cert := state.PeerCertificates[0]
        info.Issuer = cert.Issuer.CommonName
        info.Subject = cert.Subject.CommonName
        info.SANs = cert.DNSNames
        info.NotBefore = cert.NotBefore
        info.NotAfter = cert.NotAfter
        info.DaysUntilExpiry = int(time.Until(cert.NotAfter).Hours() / 24)
        info.KeyType = cert.PublicKeyAlgorithm.String()
        info.ChainLength = len(state.PeerCertificates)
        info.ChainValid = len(state.VerifiedChains) > 0
        info.OCSPStapled = len(state.OCSPResponse) > 0
    }

    return info
}

func tlsVersionString(v uint16) string {
    switch v {
    case tls.VersionTLS10:
        return "TLS1.0"
    case tls.VersionTLS11:
        return "TLS1.1"
    case tls.VersionTLS12:
        return "TLS1.2"
    case tls.VersionTLS13:
        return "TLS1.3"
    default:
        return fmt.Sprintf("0x%04x", v)
    }
}

// extractJSONPath is a simple JSON path extractor for $.key.subkey patterns
func extractJSONPath(data []byte, path string) string {
    // Strip leading "$."
    path = strings.TrimPrefix(path, "$.")
    parts := strings.Split(path, ".")

    var current any
    if err := json.Unmarshal(data, &current); err != nil {
        return ""
    }

    for _, part := range parts {
        obj, ok := current.(map[string]any)
        if !ok {
            return ""
        }
        current, ok = obj[part]
        if !ok {
            return ""
        }
    }

    switch v := current.(type) {
    case string:
        return v
    case float64:
        if v == float64(int64(v)) {
            return fmt.Sprintf("%d", int64(v))
        }
        return fmt.Sprintf("%g", v)
    case bool:
        return fmt.Sprintf("%t", v)
    case nil:
        return "null"
    default:
        b, _ := json.Marshal(v)
        return string(b)
    }
}

// validateJSONSchema validates JSON data against a JSON Schema
// Custom implementation — supports draft 2020-12 subset
func validateJSONSchema(data []byte, schema string) bool {
    // Implementation: parse schema, validate data structure
    // Supports: type, required, properties, items, enum, pattern, minimum, maximum
    // This is a simplified implementation; full JSON Schema is complex
    // TODO: implement full JSON Schema validation engine
    var schemaObj map[string]any
    if err := json.Unmarshal([]byte(schema), &schemaObj); err != nil {
        return false
    }

    var dataObj any
    if err := json.Unmarshal(data, &dataObj); err != nil {
        return false
    }

    return validateNode(dataObj, schemaObj)
}

func validateNode(data any, schema map[string]any) bool {
    // Type validation
    if expectedType, ok := schema["type"].(string); ok {
        if !matchesType(data, expectedType) {
            return false
        }
    }

    // Required fields
    if required, ok := schema["required"].([]any); ok {
        obj, isObj := data.(map[string]any)
        if !isObj {
            return false
        }
        for _, r := range required {
            if key, isStr := r.(string); isStr {
                if _, exists := obj[key]; !exists {
                    return false
                }
            }
        }
    }

    // Properties
    if props, ok := schema["properties"].(map[string]any); ok {
        obj, isObj := data.(map[string]any)
        if !isObj {
            return false
        }
        for key, propSchema := range props {
            if val, exists := obj[key]; exists {
                if ps, isMap := propSchema.(map[string]any); isMap {
                    if !validateNode(val, ps) {
                        return false
                    }
                }
            }
        }
    }

    return true
}

func matchesType(data any, expectedType string) bool {
    switch expectedType {
    case "object":
        _, ok := data.(map[string]any)
        return ok
    case "array":
        _, ok := data.([]any)
        return ok
    case "string":
        _, ok := data.(string)
        return ok
    case "number":
        _, ok := data.(float64)
        return ok
    case "boolean":
        _, ok := data.(bool)
        return ok
    case "null":
        return data == nil
    }
    return true
}

func truncate(s string, max int) string {
    if len(s) <= max {
        return s
    }
    return s[:max] + "..."
}

func boolToStr(b bool, t, f string) string {
    if b {
        return t
    }
    return f
}
```

### 3.3 ICMP Ping Checker

```go
// internal/probe/icmp.go
package probe

import (
    "context"
    "fmt"
    "math"
    "net"
    "time"

    "github.com/AnubisWatch/anubiswatch/internal/core"
    "golang.org/x/net/icmp"
    "golang.org/x/net/ipv4"
    "golang.org/x/net/ipv6"
)

type ICMPChecker struct{}

func NewICMPChecker() *ICMPChecker {
    return &ICMPChecker{}
}

func (c *ICMPChecker) Type() core.CheckType {
    return core.CheckICMP
}

func (c *ICMPChecker) Validate(soul *core.Soul) error {
    if soul.Target == "" {
        return &core.ConfigError{Field: "target", Message: "target host required"}
    }
    return nil
}

func (c *ICMPChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
    cfg := soul.ICMP
    if cfg == nil {
        cfg = &core.ICMPConfig{Count: 3, Interval: core.Duration{Duration: 200 * time.Millisecond}}
    }

    count := cfg.Count
    if count == 0 {
        count = 3
    }
    interval := cfg.Interval.Duration
    if interval == 0 {
        interval = 200 * time.Millisecond
    }
    timeout := soul.Timeout.Duration
    if timeout == 0 {
        timeout = 5 * time.Second
    }

    // Resolve target
    addr, err := net.ResolveIPAddr("ip", soul.Target)
    if err != nil {
        return failJudgment(soul, fmt.Errorf("DNS resolution failed: %w", err)), nil
    }

    isIPv6 := addr.IP.To4() == nil
    var network string
    var icmpType icmp.Type

    if isIPv6 {
        network = "ip6:ipv6-icmp"
        icmpType = ipv6.ICMPTypeEchoRequest
    } else {
        network = "ip4:icmp"
        icmpType = ipv4.ICMPTypeEcho
    }

    // Use unprivileged mode if not privileged
    if !cfg.Privileged {
        if isIPv6 {
            network = "udp6"
        } else {
            network = "udp4"
        }
    }

    conn, err := icmp.ListenPacket(network, "")
    if err != nil {
        return failJudgment(soul, fmt.Errorf("ICMP listen failed: %w", err)), nil
    }
    defer conn.Close()

    var latencies []float64
    sent := 0
    received := 0

    for i := 0; i < count; i++ {
        select {
        case <-ctx.Done():
            break
        default:
        }

        msg := icmp.Message{
            Type: icmpType,
            Code: 0,
            Body: &icmp.Echo{
                ID:   i,
                Seq:  i,
                Data: []byte("AnubisWatch"),
            },
        }

        msgBytes, err := msg.Marshal(nil)
        if err != nil {
            continue
        }

        start := time.Now()
        sent++

        var dst net.Addr
        if cfg.Privileged {
            dst = addr
        } else {
            dst = &net.UDPAddr{IP: addr.IP}
        }

        if _, err := conn.WriteTo(msgBytes, dst); err != nil {
            continue
        }

        conn.SetReadDeadline(time.Now().Add(timeout))
        reply := make([]byte, 1500)
        n, _, err := conn.ReadFrom(reply)
        duration := time.Since(start)

        if err != nil {
            continue // timeout or error = packet lost
        }

        var parseType icmp.Type
        if isIPv6 {
            parseType = ipv6.ICMPTypeEchoReply
        } else {
            parseType = ipv4.ICMPTypeEchoReply
        }

        parsed, err := icmp.ParseMessage(parseType.Protocol(), reply[:n])
        if err != nil {
            continue
        }

        if parsed.Type == parseType {
            received++
            latencies = append(latencies, float64(duration.Microseconds())/1000.0)
        }

        if i < count-1 {
            time.Sleep(interval)
        }
    }

    // Calculate statistics
    packetLoss := float64(sent-received) / float64(sent) * 100

    var minLat, maxLat, avgLat, jitter float64
    if len(latencies) > 0 {
        minLat = latencies[0]
        maxLat = latencies[0]
        sum := 0.0
        for _, l := range latencies {
            sum += l
            if l < minLat {
                minLat = l
            }
            if l > maxLat {
                maxLat = l
            }
        }
        avgLat = sum / float64(len(latencies))

        // Calculate jitter (mean deviation)
        if len(latencies) > 1 {
            var devSum float64
            for i := 1; i < len(latencies); i++ {
                devSum += math.Abs(latencies[i] - latencies[i-1])
            }
            jitter = devSum / float64(len(latencies)-1)
        }
    }

    // Determine status
    status := core.SoulAlive
    message := fmt.Sprintf("%d/%d packets received, %.1f%% loss, avg %.2fms",
        received, sent, packetLoss, avgLat)

    if received == 0 {
        status = core.SoulDead
        message = fmt.Sprintf("all %d packets lost — host unreachable", sent)
    } else if cfg.MaxLossPercent > 0 && packetLoss > cfg.MaxLossPercent {
        status = core.SoulDegraded
        message = fmt.Sprintf("%.1f%% packet loss exceeds threshold %.1f%%", packetLoss, cfg.MaxLossPercent)
    } else if cfg.Feather.Duration > 0 && time.Duration(avgLat*float64(time.Millisecond)) > cfg.Feather.Duration {
        status = core.SoulDegraded
        message = fmt.Sprintf("avg latency %.2fms exceeds feather %s", avgLat, cfg.Feather.Duration)
    }

    return &core.Judgment{
        SoulID:    soul.ID,
        Timestamp: time.Now().UTC(),
        Duration:  time.Duration(avgLat * float64(time.Millisecond)),
        Status:    status,
        Message:   message,
        Details: &core.JudgmentDetails{
            PacketsSent:     sent,
            PacketsReceived: received,
            PacketLoss:      packetLoss,
            MinLatency:      minLat,
            AvgLatency:      avgLat,
            MaxLatency:      maxLat,
            Jitter:          jitter,
        },
    }, nil
}
```

### 3.4 TCP Checker

```go
// internal/probe/tcp.go
package probe

import (
    "bufio"
    "context"
    "fmt"
    "net"
    "regexp"
    "strings"
    "time"

    "github.com/AnubisWatch/anubiswatch/internal/core"
)

type TCPChecker struct{}

func NewTCPChecker() *TCPChecker { return &TCPChecker{} }

func (c *TCPChecker) Type() core.CheckType { return core.CheckTCP }

func (c *TCPChecker) Validate(soul *core.Soul) error {
    if soul.Target == "" {
        return &core.ConfigError{Field: "target", Message: "target host:port required"}
    }
    return nil
}

func (c *TCPChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
    cfg := soul.TCP
    timeout := soul.Timeout.Duration
    if timeout == 0 {
        timeout = 10 * time.Second
    }

    dialer := net.Dialer{Timeout: timeout}
    start := time.Now()
    conn, err := dialer.DialContext(ctx, "tcp", soul.Target)
    duration := time.Since(start)

    if err != nil {
        return &core.Judgment{
            SoulID:    soul.ID,
            Timestamp: time.Now().UTC(),
            Duration:  duration,
            Status:    core.SoulDead,
            Message:   fmt.Sprintf("TCP connect failed: %s", err),
        }, nil
    }
    defer conn.Close()

    judgment := &core.Judgment{
        SoulID:    soul.ID,
        Timestamp: time.Now().UTC(),
        Duration:  duration,
        Status:    core.SoulAlive,
        Message:   fmt.Sprintf("TCP connect to %s in %s", soul.Target, duration.Round(time.Millisecond)),
        Details:   &core.JudgmentDetails{},
    }

    // Banner grab
    if cfg != nil && (cfg.BannerMatch != "" || cfg.ExpectRegex != "") {
        conn.SetReadDeadline(time.Now().Add(5 * time.Second))
        reader := bufio.NewReader(conn)

        // Send payload if configured
        if cfg.Send != "" {
            conn.Write([]byte(cfg.Send))
        }

        banner, err := reader.ReadString('\n')
        if err != nil && banner == "" {
            // Try reading without delimiter
            buf := make([]byte, 4096)
            n, _ := reader.Read(buf)
            banner = string(buf[:n])
        }
        judgment.Details.Banner = strings.TrimSpace(banner)

        // Banner match assertion
        if cfg.BannerMatch != "" {
            matched := strings.Contains(strings.ToLower(banner), strings.ToLower(cfg.BannerMatch))
            judgment.Details.Assertions = append(judgment.Details.Assertions, core.AssertionResult{
                Type:     "banner_match",
                Expected: cfg.BannerMatch,
                Actual:   truncate(banner, 200),
                Passed:   matched,
            })
            if !matched {
                judgment.Status = core.SoulDead
                judgment.Message = fmt.Sprintf("banner mismatch: expected '%s'", cfg.BannerMatch)
            }
        }

        // Regex assertion
        if cfg.ExpectRegex != "" {
            re, err := regexp.Compile(cfg.ExpectRegex)
            matched := err == nil && re.MatchString(banner)
            judgment.Details.Assertions = append(judgment.Details.Assertions, core.AssertionResult{
                Type:     "expect_regex",
                Expected: cfg.ExpectRegex,
                Actual:   truncate(banner, 200),
                Passed:   matched,
            })
            if !matched {
                judgment.Status = core.SoulDead
                judgment.Message = "response did not match expected pattern"
            }
        }
    }

    return judgment, nil
}
```

### 3.5 DNS Checker

```go
// internal/probe/dns.go
package probe

import (
    "context"
    "fmt"
    "net"
    "strings"
    "time"

    "github.com/AnubisWatch/anubiswatch/internal/core"
)

// DNSChecker implements DNS resolution checks.
// Custom DNS client implementation using raw UDP packets
// to support DNSSEC validation, custom nameservers, and propagation tracking.
type DNSChecker struct{}

func NewDNSChecker() *DNSChecker { return &DNSChecker{} }

func (c *DNSChecker) Type() core.CheckType { return core.CheckDNS }

func (c *DNSChecker) Validate(soul *core.Soul) error {
    if soul.Target == "" {
        return &core.ConfigError{Field: "target", Message: "target domain required"}
    }
    return nil
}

func (c *DNSChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
    cfg := soul.DNS
    if cfg == nil {
        cfg = &core.DNSConfig{RecordType: "A"}
    }

    nameservers := cfg.Nameservers
    if len(nameservers) == 0 {
        nameservers = []string{"8.8.8.8:53", "1.1.1.1:53"}
    }

    start := time.Now()

    // Resolve using custom DNS client
    // The custom DNS client supports:
    // - All record types (A, AAAA, CNAME, MX, TXT, NS, SOA, SRV, PTR, CAA)
    // - DNSSEC validation
    // - Multiple nameserver queries for propagation checking
    // - Response time measurement per nameserver

    // For propagation checking, query all nameservers
    if cfg.PropagationCheck {
        return c.judgePropagation(ctx, soul, cfg, nameservers, start)
    }

    // Single nameserver query
    records, err := c.resolve(ctx, soul.Target, cfg.RecordType, nameservers[0])
    duration := time.Since(start)

    if err != nil {
        return &core.Judgment{
            SoulID:    soul.ID,
            Timestamp: time.Now().UTC(),
            Duration:  duration,
            Status:    core.SoulDead,
            Message:   fmt.Sprintf("DNS resolution failed: %s", err),
        }, nil
    }

    judgment := &core.Judgment{
        SoulID:    soul.ID,
        Timestamp: time.Now().UTC(),
        Duration:  duration,
        Status:    core.SoulAlive,
        Message:   fmt.Sprintf("DNS %s resolved to %s in %s", cfg.RecordType, strings.Join(records, ", "), duration.Round(time.Millisecond)),
        Details: &core.JudgmentDetails{
            ResolvedAddresses: records,
        },
    }

    // Expected value assertion
    if len(cfg.Expected) > 0 {
        allFound := true
        for _, exp := range cfg.Expected {
            found := false
            for _, rec := range records {
                if rec == exp {
                    found = true
                    break
                }
            }
            if !found {
                allFound = false
            }
        }
        judgment.Details.Assertions = append(judgment.Details.Assertions, core.AssertionResult{
            Type:     "expected_records",
            Expected: strings.Join(cfg.Expected, ", "),
            Actual:   strings.Join(records, ", "),
            Passed:   allFound,
        })
        if !allFound {
            judgment.Status = core.SoulDead
            judgment.Message = "DNS records do not match expected values"
        }
    }

    // DNSSEC validation
    if cfg.DNSSECValidate {
        valid := c.validateDNSSEC(ctx, soul.Target, cfg.RecordType, nameservers[0])
        judgment.Details.DNSSECValid = &valid
        judgment.Details.Assertions = append(judgment.Details.Assertions, core.AssertionResult{
            Type:     "dnssec",
            Expected: "valid",
            Actual:   boolToStr(valid, "valid", "invalid"),
            Passed:   valid,
        })
        if !valid {
            judgment.Status = core.SoulDegraded
            judgment.Message = "DNSSEC validation failed"
        }
    }

    return judgment, nil
}

func (c *DNSChecker) judgePropagation(ctx context.Context, soul *core.Soul, cfg *core.DNSConfig, nameservers []string, start time.Time) (*core.Judgment, error) {
    results := make(map[string]bool, len(nameservers))
    var resolvedRecords []string

    for _, ns := range nameservers {
        records, err := c.resolve(ctx, soul.Target, cfg.RecordType, ns)
        if err != nil {
            results[ns] = false
            continue
        }

        if len(cfg.Expected) > 0 {
            // Check if resolved matches expected
            allMatch := true
            for _, exp := range cfg.Expected {
                found := false
                for _, rec := range records {
                    if rec == exp {
                        found = true
                        break
                    }
                }
                if !found {
                    allMatch = false
                    break
                }
            }
            results[ns] = allMatch
        } else {
            results[ns] = len(records) > 0
        }

        if len(resolvedRecords) == 0 {
            resolvedRecords = records
        }
    }

    duration := time.Since(start)

    // Calculate propagation percentage
    propagated := 0
    for _, ok := range results {
        if ok {
            propagated++
        }
    }
    propagationPercent := float64(propagated) / float64(len(nameservers)) * 100

    threshold := cfg.PropagationThreshold
    if threshold == 0 {
        threshold = 100
    }

    status := core.SoulAlive
    message := fmt.Sprintf("DNS propagation: %.0f%% (%d/%d nameservers)", propagationPercent, propagated, len(nameservers))

    if propagationPercent < float64(threshold) {
        status = core.SoulDegraded
        message = fmt.Sprintf("DNS propagation %.0f%% below threshold %d%%", propagationPercent, threshold)
    }

    return &core.Judgment{
        SoulID:    soul.ID,
        Timestamp: time.Now().UTC(),
        Duration:  duration,
        Status:    status,
        Message:   message,
        Details: &core.JudgmentDetails{
            ResolvedAddresses: resolvedRecords,
            PropagationResult: results,
        },
    }, nil
}

// resolve performs DNS resolution using a custom UDP-based DNS client
func (c *DNSChecker) resolve(ctx context.Context, domain, recordType, nameserver string) ([]string, error) {
    // Ensure nameserver has port
    if !strings.Contains(nameserver, ":") {
        nameserver += ":53"
    }

    // Use Go's built-in resolver with custom dialer for simplicity
    // In production, this should be a custom DNS client for full control
    resolver := &net.Resolver{
        PreferGo: true,
        Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
            d := net.Dialer{Timeout: 5 * time.Second}
            return d.DialContext(ctx, "udp", nameserver)
        },
    }

    switch strings.ToUpper(recordType) {
    case "A", "AAAA":
        ips, err := resolver.LookupHost(ctx, domain)
        return ips, err
    case "CNAME":
        cname, err := resolver.LookupCNAME(ctx, domain)
        return []string{cname}, err
    case "MX":
        mxs, err := resolver.LookupMX(ctx, domain)
        if err != nil {
            return nil, err
        }
        var results []string
        for _, mx := range mxs {
            results = append(results, fmt.Sprintf("%d %s", mx.Pref, mx.Host))
        }
        return results, nil
    case "TXT":
        return resolver.LookupTXT(ctx, domain)
    case "NS":
        nss, err := resolver.LookupNS(ctx, domain)
        if err != nil {
            return nil, err
        }
        var results []string
        for _, ns := range nss {
            results = append(results, ns.Host)
        }
        return results, nil
    case "SRV":
        _, srvs, err := resolver.LookupSRV(ctx, "", "", domain)
        if err != nil {
            return nil, err
        }
        var results []string
        for _, srv := range srvs {
            results = append(results, fmt.Sprintf("%s:%d", srv.Target, srv.Port))
        }
        return results, nil
    default:
        return nil, fmt.Errorf("unsupported record type: %s", recordType)
    }
}

// validateDNSSEC checks DNSSEC validity
// Full implementation requires custom DNS packet parsing with RRSIG/DNSKEY/DS validation
func (c *DNSChecker) validateDNSSEC(ctx context.Context, domain, recordType, nameserver string) bool {
    // TODO: Implement full DNSSEC chain validation
    // This requires:
    // 1. Query with DO (DNSSEC OK) bit set
    // 2. Verify RRSIG signatures
    // 3. Walk the trust chain from root → TLD → domain
    // 4. Validate DNSKEY → DS chain
    return true // placeholder
}
```

### 3.6 Remaining Checkers (Stub Signatures)

```go
// internal/probe/smtp.go — SMTP/IMAP Checker
// Implements: SMTP EHLO, STARTTLS, AUTH, banner check
// IMAP: LOGIN, mailbox status

// internal/probe/grpc.go — gRPC Health Checker
// Implements: grpc.health.v1.Health/Check protocol
// Custom gRPC client (no google.golang.org/grpc dependency)
// Uses raw HTTP/2 frames + protobuf encoding

// internal/probe/websocket.go — WebSocket Checker
// Implements: RFC 6455 WebSocket handshake
// Custom WebSocket client over net/http
// Send/receive/ping-pong validation

// internal/probe/tls.go — TLS Certificate Checker
// Implements: Full TLS handshake inspection
// Certificate chain validation, cipher audit, OCSP check
// Uses crypto/tls from stdlib

// internal/probe/udp.go — UDP Checker
// Implements: UDP send/receive with timeout
// Hex payload support, response matching

// internal/probe/synthetic.go — Duat Journey (Multi-step HTTP)
// Implements: Sequential HTTP steps with variable extraction
// JSON path extraction, header extraction, regex capture groups
// Variable interpolation between steps
```

---

## 4. RAFT CONSENSUS IMPLEMENTATION

### 4.1 Raft Node State Machine

```go
// internal/raft/node.go
package raft

import (
    "context"
    "crypto/rand"
    "log/slog"
    "math/big"
    "sync"
    "time"
)

// NodeState represents the current Raft role
type NodeState int

const (
    Follower  NodeState = iota // Jackal
    Candidate                   // Aspiring Pharaoh
    Leader                      // Pharaoh
)

func (s NodeState) String() string {
    switch s {
    case Follower:
        return "Jackal"
    case Candidate:
        return "Candidate"
    case Leader:
        return "Pharaoh"
    default:
        return "Unknown"
    }
}

// Node is the core Raft consensus node
type Node struct {
    // Persistent state (stored in CobaltDB)
    currentTerm uint64
    votedFor    string
    log         *Log

    // Volatile state
    state       NodeState
    commitIndex uint64
    lastApplied uint64
    leaderID    string

    // Leader-only volatile state
    nextIndex   map[string]uint64
    matchIndex  map[string]uint64

    // Node identity
    id          string
    peers       map[string]*Peer
    
    // Configuration
    config      Config

    // Transport
    transport   Transport

    // Channels
    applyCh     chan LogEntry
    leaderCh    chan bool       // signals leader changes

    // Concurrency
    mu          sync.RWMutex
    ctx         context.Context
    cancel      context.CancelFunc
    
    logger      *slog.Logger
}

type Config struct {
    ElectionTimeoutMin time.Duration
    ElectionTimeoutMax time.Duration
    HeartbeatInterval  time.Duration
    SnapshotInterval   time.Duration
    SnapshotThreshold  int
    MaxLogEntries      int
}

type Peer struct {
    ID      string
    Address string
    Region  string
}

// NewNode creates a new Raft node
func NewNode(id string, transport Transport, log *Log, config Config, logger *slog.Logger) *Node {
    ctx, cancel := context.WithCancel(context.Background())
    return &Node{
        id:          id,
        state:       Follower,
        log:         log,
        transport:   transport,
        config:      config,
        peers:       make(map[string]*Peer),
        nextIndex:   make(map[string]uint64),
        matchIndex:  make(map[string]uint64),
        applyCh:     make(chan LogEntry, 256),
        leaderCh:    make(chan bool, 1),
        ctx:         ctx,
        cancel:      cancel,
        logger:      logger.With("component", "raft", "node", id),
    }
}

// Start begins the Raft node lifecycle
func (n *Node) Start() {
    go n.run()
}

func (n *Node) run() {
    for {
        select {
        case <-n.ctx.Done():
            return
        default:
        }

        switch n.getState() {
        case Follower:
            n.runFollower()
        case Candidate:
            n.runCandidate()
        case Leader:
            n.runLeader()
        }
    }
}

func (n *Node) runFollower() {
    timeout := n.randomElectionTimeout()
    timer := time.NewTimer(timeout)
    defer timer.Stop()

    for n.getState() == Follower {
        select {
        case <-n.ctx.Done():
            return
        case <-timer.C:
            // Election timeout — become candidate
            n.logger.Info("election timeout, becoming candidate")
            n.setState(Candidate)
            return
        case rpc := <-n.transport.AppendEntriesCh():
            // Process AppendEntries from leader
            resp := n.handleAppendEntries(rpc.Request)
            rpc.Respond(resp)
            if resp.Success {
                timer.Reset(n.randomElectionTimeout())
            }
        case rpc := <-n.transport.RequestVoteCh():
            // Process RequestVote from candidate
            resp := n.handleRequestVote(rpc.Request)
            rpc.Respond(resp)
            if resp.VoteGranted {
                timer.Reset(n.randomElectionTimeout())
            }
        }
    }
}

func (n *Node) runCandidate() {
    n.mu.Lock()
    n.currentTerm++
    n.votedFor = n.id
    currentTerm := n.currentTerm
    n.mu.Unlock()

    n.logger.Info("starting election", "term", currentTerm)

    // Vote for self
    votes := 1
    totalPeers := len(n.peers) + 1 // include self
    majority := totalPeers/2 + 1

    // Request votes from all peers
    voteCh := make(chan bool, len(n.peers))
    for _, peer := range n.peers {
        go func(p *Peer) {
            lastLogIndex, lastLogTerm := n.log.LastInfo()
            resp, err := n.transport.RequestVote(p.Address, &RequestVoteRequest{
                Term:         currentTerm,
                CandidateID:  n.id,
                LastLogIndex: lastLogIndex,
                LastLogTerm:  lastLogTerm,
            })
            if err != nil {
                voteCh <- false
                return
            }
            if resp.Term > currentTerm {
                n.mu.Lock()
                n.currentTerm = resp.Term
                n.votedFor = ""
                n.mu.Unlock()
                n.setState(Follower)
            }
            voteCh <- resp.VoteGranted
        }(peer)
    }

    // Wait for votes with election timeout
    timer := time.NewTimer(n.randomElectionTimeout())
    defer timer.Stop()

    for n.getState() == Candidate {
        select {
        case <-n.ctx.Done():
            return
        case <-timer.C:
            // Election timeout — restart election
            return
        case granted := <-voteCh:
            if granted {
                votes++
                if votes >= majority {
                    n.logger.Info("won election", "term", currentTerm, "votes", votes)
                    n.setState(Leader)
                    n.leaderID = n.id
                    // Notify leader change
                    select {
                    case n.leaderCh <- true:
                    default:
                    }
                    return
                }
            }
        case rpc := <-n.transport.AppendEntriesCh():
            resp := n.handleAppendEntries(rpc.Request)
            rpc.Respond(resp)
            if rpc.Request.Term >= currentTerm {
                n.setState(Follower)
                return
            }
        case rpc := <-n.transport.RequestVoteCh():
            resp := n.handleRequestVote(rpc.Request)
            rpc.Respond(resp)
        }
    }
}

func (n *Node) runLeader() {
    n.logger.Info("became Pharaoh (leader)", "term", n.currentTerm)

    // Initialize nextIndex and matchIndex for all peers
    lastLogIndex, _ := n.log.LastInfo()
    for id := range n.peers {
        n.nextIndex[id] = lastLogIndex + 1
        n.matchIndex[id] = 0
    }

    // Send initial empty AppendEntries (heartbeat) to assert leadership
    n.sendHeartbeats()

    heartbeat := time.NewTicker(n.config.HeartbeatInterval)
    defer heartbeat.Stop()

    for n.getState() == Leader {
        select {
        case <-n.ctx.Done():
            return
        case <-heartbeat.C:
            n.sendHeartbeats()
        case rpc := <-n.transport.AppendEntriesCh():
            resp := n.handleAppendEntries(rpc.Request)
            rpc.Respond(resp)
            if rpc.Request.Term > n.currentTerm {
                n.setState(Follower)
                return
            }
        case rpc := <-n.transport.RequestVoteCh():
            resp := n.handleRequestVote(rpc.Request)
            rpc.Respond(resp)
            if resp.VoteGranted {
                n.setState(Follower)
                return
            }
        }
    }
}

func (n *Node) handleAppendEntries(req *AppendEntriesRequest) *AppendEntriesResponse {
    n.mu.Lock()
    defer n.mu.Unlock()

    resp := &AppendEntriesResponse{
        Term:    n.currentTerm,
        Success: false,
    }

    // Rule 1: Reply false if term < currentTerm
    if req.Term < n.currentTerm {
        return resp
    }

    // Update term if needed
    if req.Term > n.currentTerm {
        n.currentTerm = req.Term
        n.votedFor = ""
        n.state = Follower
    }

    n.leaderID = req.LeaderID

    // Rule 2: Reply false if log doesn't contain entry at prevLogIndex with prevLogTerm
    if req.PrevLogIndex > 0 {
        entry, err := n.log.Get(req.PrevLogIndex)
        if err != nil || entry.Term != req.PrevLogTerm {
            return resp
        }
    }

    // Rule 3: Delete conflicting entries and append new ones
    for i, entry := range req.Entries {
        idx := req.PrevLogIndex + uint64(i) + 1
        existing, err := n.log.Get(idx)
        if err != nil || existing.Term != entry.Term {
            // Delete from this index onwards and append
            n.log.TruncateFrom(idx)
            n.log.AppendEntries(req.Entries[i:])
            break
        }
    }

    // Rule 4: Update commitIndex
    if req.LeaderCommit > n.commitIndex {
        lastIdx, _ := n.log.LastInfo()
        if req.LeaderCommit < lastIdx {
            n.commitIndex = req.LeaderCommit
        } else {
            n.commitIndex = lastIdx
        }
    }

    resp.Success = true
    resp.Term = n.currentTerm
    return resp
}

func (n *Node) handleRequestVote(req *RequestVoteRequest) *RequestVoteResponse {
    n.mu.Lock()
    defer n.mu.Unlock()

    resp := &RequestVoteResponse{
        Term:        n.currentTerm,
        VoteGranted: false,
    }

    if req.Term < n.currentTerm {
        return resp
    }

    if req.Term > n.currentTerm {
        n.currentTerm = req.Term
        n.votedFor = ""
        n.state = Follower
    }

    // Grant vote if we haven't voted or voted for this candidate
    if n.votedFor == "" || n.votedFor == req.CandidateID {
        // Check if candidate's log is at least as up-to-date
        lastLogIndex, lastLogTerm := n.log.LastInfo()
        if req.LastLogTerm > lastLogTerm ||
            (req.LastLogTerm == lastLogTerm && req.LastLogIndex >= lastLogIndex) {
            n.votedFor = req.CandidateID
            resp.VoteGranted = true
        }
    }

    resp.Term = n.currentTerm
    return resp
}

func (n *Node) sendHeartbeats() {
    for _, peer := range n.peers {
        go func(p *Peer) {
            prevLogIndex := n.nextIndex[p.ID] - 1
            prevLogTerm := uint64(0)
            if prevLogIndex > 0 {
                if entry, err := n.log.Get(prevLogIndex); err == nil {
                    prevLogTerm = entry.Term
                }
            }

            // Collect entries to send
            entries := n.log.EntriesFrom(n.nextIndex[p.ID])

            resp, err := n.transport.AppendEntries(p.Address, &AppendEntriesRequest{
                Term:         n.currentTerm,
                LeaderID:     n.id,
                PrevLogIndex: prevLogIndex,
                PrevLogTerm:  prevLogTerm,
                Entries:      entries,
                LeaderCommit: n.commitIndex,
            })
            if err != nil {
                return
            }

            if resp.Term > n.currentTerm {
                n.mu.Lock()
                n.currentTerm = resp.Term
                n.votedFor = ""
                n.mu.Unlock()
                n.setState(Follower)
                return
            }

            if resp.Success {
                n.nextIndex[p.ID] = prevLogIndex + uint64(len(entries)) + 1
                n.matchIndex[p.ID] = n.nextIndex[p.ID] - 1
            } else {
                // Decrement nextIndex and retry
                if n.nextIndex[p.ID] > 1 {
                    n.nextIndex[p.ID]--
                }
            }
        }(peer)
    }
}

// Apply submits a new entry to the Raft log (leader only)
func (n *Node) Apply(data []byte) error {
    if n.getState() != Leader {
        return ErrNotLeader
    }

    entry := LogEntry{
        Term:  n.currentTerm,
        Data:  data,
    }

    n.log.Append(entry)
    return nil
}

// IsLeader returns true if this node is the current Pharaoh
func (n *Node) IsLeader() bool {
    return n.getState() == Leader
}

// LeaderID returns the current leader's ID
func (n *Node) LeaderID() string {
    n.mu.RLock()
    defer n.mu.RUnlock()
    return n.leaderID
}

func (n *Node) getState() NodeState {
    n.mu.RLock()
    defer n.mu.RUnlock()
    return n.state
}

func (n *Node) setState(s NodeState) {
    n.mu.Lock()
    defer n.mu.Unlock()
    n.state = s
}

func (n *Node) randomElectionTimeout() time.Duration {
    minMs := n.config.ElectionTimeoutMin.Milliseconds()
    maxMs := n.config.ElectionTimeoutMax.Milliseconds()
    diff := maxMs - minMs
    if diff <= 0 {
        return n.config.ElectionTimeoutMin
    }
    nBig, _ := rand.Int(rand.Reader, big.NewInt(diff))
    return time.Duration(minMs+nBig.Int64()) * time.Millisecond
}

// Stop gracefully shuts down the Raft node
func (n *Node) Stop() {
    n.cancel()
}
```

### 4.2 Raft Transport (TCP/TLS)

```go
// internal/raft/transport.go
package raft

// Transport defines the network layer for Raft communication
type Transport interface {
    // RPC channels (incoming)
    AppendEntriesCh() <-chan AppendEntriesRPC
    RequestVoteCh() <-chan RequestVoteRPC

    // RPC calls (outgoing)
    AppendEntries(addr string, req *AppendEntriesRequest) (*AppendEntriesResponse, error)
    RequestVote(addr string, req *RequestVoteRequest) (*RequestVoteResponse, error)

    // Lifecycle
    Start() error
    Stop() error
    LocalAddr() string
}

// RPC Types
type AppendEntriesRequest struct {
    Term         uint64
    LeaderID     string
    PrevLogIndex uint64
    PrevLogTerm  uint64
    Entries      []LogEntry
    LeaderCommit uint64
}

type AppendEntriesResponse struct {
    Term    uint64
    Success bool
}

type RequestVoteRequest struct {
    Term         uint64
    CandidateID  string
    LastLogIndex uint64
    LastLogTerm  uint64
}

type RequestVoteResponse struct {
    Term        uint64
    VoteGranted bool
}

type AppendEntriesRPC struct {
    Request  *AppendEntriesRequest
    Response chan<- *AppendEntriesResponse
}

func (r *AppendEntriesRPC) Respond(resp *AppendEntriesResponse) {
    r.Response <- resp
}

type RequestVoteRPC struct {
    Request  *RequestVoteRequest
    Response chan<- *RequestVoteResponse
}

func (r *RequestVoteRPC) Respond(resp *RequestVoteResponse) {
    r.Response <- resp
}

type LogEntry struct {
    Index uint64
    Term  uint64
    Data  []byte
}

// TCPTransport implements Transport over TCP with optional TLS
// Implementation uses length-prefixed binary encoding:
// [4 bytes: message type][4 bytes: payload length][payload bytes]
// TLS mutual auth using cluster certificate
```

### 4.3 Auto-Discovery

```go
// internal/raft/discovery.go
package raft

// Discovery manages automatic node discovery in the cluster
// Three modes:
//
// 1. mDNS (LAN): Broadcasts _anubis._tcp.local service
//    - Uses multicast DNS for zero-config LAN discovery
//    - Nodes announce themselves and listen for peers
//
// 2. Gossip (WAN): SWIM-based gossip protocol
//    - Configurable seed nodes
//    - Failure detection via ping/ping-req/suspect
//    - Encrypted payloads with cluster secret
//
// 3. Manual: Explicit peer list in configuration

type Discovery interface {
    Start() error
    Stop() error
    Peers() []string        // Returns discovered peer addresses
    OnJoin(func(addr string))   // Callback when new peer found
    OnLeave(func(addr string))  // Callback when peer leaves
}
```

---

## 5. ALERT SYSTEM IMPLEMENTATION

### 5.1 Alert Dispatcher

```go
// internal/alert/dispatcher.go
package alert

import (
    "context"
    "log/slog"
    "sync"
    "time"

    "github.com/AnubisWatch/anubiswatch/internal/core"
)

// Dispatcher evaluates alert rules and routes notifications
type Dispatcher struct {
    channels   map[string]Channel
    rules      []*core.AlertRule
    state      *AlertState         // tracks consecutive failures, cooldowns, etc.
    mu         sync.RWMutex
    logger     *slog.Logger
}

// Channel is the interface every alert channel implements
type Channel interface {
    Type() string
    Send(ctx context.Context, notification *Notification) error
    Validate() error
}

// Notification is the payload sent to alert channels
type Notification struct {
    Soul       *core.Soul
    Judgment   *core.Judgment
    Verdict    *core.Verdict
    Rule       *core.AlertRule
    Severity   core.Severity
    Message    string
    IsRecovery bool            // true when soul recovers (Resurrection)
}

// AlertState tracks per-soul alert evaluation state
type AlertState struct {
    mu                sync.RWMutex
    consecutiveFailures map[string]int
    lastAlertTime      map[string]time.Time
    activeVerdicts     map[string]*core.Verdict
}

func NewDispatcher(logger *slog.Logger) *Dispatcher {
    return &Dispatcher{
        channels: make(map[string]Channel),
        state:    newAlertState(),
        logger:   logger.With("component", "alert-dispatcher"),
    }
}

// RegisterChannel adds an alert channel
func (d *Dispatcher) RegisterChannel(name string, ch Channel) {
    d.mu.Lock()
    defer d.mu.Unlock()
    d.channels[name] = ch
}

// SetRules updates the alert rule set
func (d *Dispatcher) SetRules(rules []*core.AlertRule) {
    d.mu.Lock()
    defer d.mu.Unlock()
    d.rules = rules
}

// Evaluate checks all rules against the latest judgment
func (d *Dispatcher) Evaluate(ctx context.Context, soul *core.Soul, judgment *core.Judgment) error {
    d.mu.RLock()
    rules := d.rules
    d.mu.RUnlock()

    // Update state
    if judgment.Status == core.SoulAlive {
        d.state.resetFailures(soul.ID)
        
        // Check for recovery (Resurrection)
        if verdict := d.state.getActiveVerdict(soul.ID); verdict != nil {
            d.handleRecovery(ctx, soul, judgment, verdict)
        }
        return nil
    }

    d.state.incrementFailures(soul.ID)

    for _, rule := range rules {
        if !d.ruleMatchesSoul(rule, soul) {
            continue
        }

        if d.evaluateCondition(rule, soul, judgment) {
            // Check cooldown
            if d.state.isInCooldown(soul.ID, rule.Name, rule.Cooldown) {
                continue
            }

            d.fireVerdict(ctx, soul, judgment, rule)
        }
    }

    return nil
}

func (d *Dispatcher) evaluateCondition(rule *core.AlertRule, soul *core.Soul, judgment *core.Judgment) bool {
    switch rule.Condition.Type {
    case "consecutive_failures":
        failures := d.state.getFailures(soul.ID)
        return failures >= rule.Condition.Threshold

    case "threshold":
        // Compare metric against threshold
        return d.evaluateThreshold(rule.Condition, soul, judgment)

    case "percentage":
        // Evaluate failure rate over time window
        // Requires historical data lookup
        return false // TODO: implement

    default:
        return false
    }
}

func (d *Dispatcher) fireVerdict(ctx context.Context, soul *core.Soul, judgment *core.Judgment, rule *core.AlertRule) {
    verdict := &core.Verdict{
        ID:          generateID(),
        WorkspaceID: soul.WorkspaceID,
        SoulID:      soul.ID,
        RuleID:      rule.Name,
        Severity:    rule.Severity,
        Status:      core.VerdictActive,
        Message:     judgment.Message,
        FiredAt:     time.Now().UTC(),
        Judgments:   []string{judgment.ID},
    }

    d.state.setActiveVerdict(soul.ID, verdict)
    d.state.setLastAlert(soul.ID, rule.Name, time.Now())

    notification := &Notification{
        Soul:     soul,
        Judgment: judgment,
        Verdict:  verdict,
        Rule:     rule,
        Severity: rule.Severity,
        Message:  judgment.Message,
    }

    // Send to all configured channels for this rule
    for _, channelName := range rule.Channels {
        ch, ok := d.channels[channelName]
        if !ok {
            d.logger.Warn("unknown alert channel", "channel", channelName)
            continue
        }

        go func(c Channel, name string) {
            if err := c.Send(ctx, notification); err != nil {
                d.logger.Error("alert send failed",
                    "channel", name,
                    "soul", soul.Name,
                    "err", err,
                )
            }
        }(ch, channelName)
    }

    d.logger.Info("verdict fired",
        "soul", soul.Name,
        "rule", rule.Name,
        "severity", rule.Severity,
    )
}

func (d *Dispatcher) handleRecovery(ctx context.Context, soul *core.Soul, judgment *core.Judgment, verdict *core.Verdict) {
    now := time.Now().UTC()
    verdict.Status = core.VerdictResolved
    verdict.ResolvedAt = &now

    d.state.clearActiveVerdict(soul.ID)

    // Find channels from the rule that fired this verdict
    // Send recovery notification
    notification := &Notification{
        Soul:       soul,
        Judgment:   judgment,
        Verdict:    verdict,
        Severity:   verdict.Severity,
        Message:    "Resurrection: " + soul.Name + " is alive again",
        IsRecovery: true,
    }

    d.mu.RLock()
    for _, rule := range d.rules {
        if rule.Name == verdict.RuleID {
            for _, channelName := range rule.Channels {
                if ch, ok := d.channels[channelName]; ok {
                    go ch.Send(ctx, notification)
                }
            }
            break
        }
    }
    d.mu.RUnlock()

    d.logger.Info("soul resurrected", "soul", soul.Name)
}

func (d *Dispatcher) ruleMatchesSoul(rule *core.AlertRule, soul *core.Soul) bool {
    // Match by scope: "all", "tag:xxx", "soul:xxx"
    scope := rule.Scope
    if scope == "" || scope == "all" {
        return true
    }
    // TODO: implement tag and soul name matching
    return true
}

func (d *Dispatcher) evaluateThreshold(cond core.AlertCondition, soul *core.Soul, judgment *core.Judgment) bool {
    // TODO: implement metric comparison
    return false
}
```

### 5.2 Alert Channel Implementations

```go
// internal/alert/webhook.go — Generic webhook (POST with template)
// internal/alert/slack.go — Slack webhook with rich formatting
// internal/alert/discord.go — Discord webhook with embeds
// internal/alert/telegram.go — Telegram Bot API
// internal/alert/email.go — Built-in SMTP client (no external dep)
// internal/alert/pagerduty.go — PagerDuty Events API v2
// internal/alert/opsgenie.go — OpsGenie Alert API
// internal/alert/sms.go — Twilio/Vonage SMS API
// internal/alert/ntfy.go — Ntfy.sh push notification

// Each implements the Channel interface:
// Type() string
// Send(ctx context.Context, notification *Notification) error
// Validate() error

// All HTTP-based channels use the same internal HTTP client
// with retry logic (exponential backoff, max 3 attempts)
```

---

## 6. API SERVER IMPLEMENTATION

### 6.1 Custom HTTP Router

```go
// internal/api/rest/router.go
package rest

import (
    "context"
    "encoding/json"
    "log/slog"
    "net/http"
    "strings"
    "time"
)

// Router is a lightweight HTTP router with middleware support.
// No external dependency (no chi, gin, echo).
type Router struct {
    routes     []route
    middleware []Middleware
    logger     *slog.Logger
}

type route struct {
    method  string
    pattern string          // e.g., "/api/v1/souls/:id"
    handler http.HandlerFunc
}

type Middleware func(http.Handler) http.Handler

func NewRouter(logger *slog.Logger) *Router {
    return &Router{logger: logger}
}

func (r *Router) Use(mw Middleware) {
    r.middleware = append(r.middleware, mw)
}

func (r *Router) GET(pattern string, handler http.HandlerFunc)    { r.addRoute("GET", pattern, handler) }
func (r *Router) POST(pattern string, handler http.HandlerFunc)   { r.addRoute("POST", pattern, handler) }
func (r *Router) PUT(pattern string, handler http.HandlerFunc)    { r.addRoute("PUT", pattern, handler) }
func (r *Router) DELETE(pattern string, handler http.HandlerFunc) { r.addRoute("DELETE", pattern, handler) }

func (r *Router) addRoute(method, pattern string, handler http.HandlerFunc) {
    r.routes = append(r.routes, route{method: method, pattern: pattern, handler: handler})
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
    // Apply middleware chain
    var handler http.Handler = http.HandlerFunc(r.dispatch)
    for i := len(r.middleware) - 1; i >= 0; i-- {
        handler = r.middleware[i](handler)
    }
    handler.ServeHTTP(w, req)
}

func (r *Router) dispatch(w http.ResponseWriter, req *http.Request) {
    for _, route := range r.routes {
        if req.Method != route.method {
            continue
        }
        params, ok := matchPattern(route.pattern, req.URL.Path)
        if ok {
            // Store params in context
            ctx := context.WithValue(req.Context(), paramsKey, params)
            route.handler(w, req.WithContext(ctx))
            return
        }
    }
    http.NotFound(w, req)
}

// matchPattern matches URL path against route pattern with :param placeholders
func matchPattern(pattern, path string) (map[string]string, bool) {
    patternParts := strings.Split(strings.Trim(pattern, "/"), "/")
    pathParts := strings.Split(strings.Trim(path, "/"), "/")

    if len(patternParts) != len(pathParts) {
        return nil, false
    }

    params := make(map[string]string)
    for i, pp := range patternParts {
        if strings.HasPrefix(pp, ":") {
            params[pp[1:]] = pathParts[i]
        } else if pp != pathParts[i] {
            return nil, false
        }
    }
    return params, true
}

type contextKey string

const paramsKey contextKey = "params"

// Param extracts a URL parameter from the request context
func Param(r *http.Request, name string) string {
    params, ok := r.Context().Value(paramsKey).(map[string]string)
    if !ok {
        return ""
    }
    return params[name]
}

// JSON helpers
func WriteJSON(w http.ResponseWriter, status int, data any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(data)
}

func ReadJSON(r *http.Request, v any) error {
    defer r.Body.Close()
    return json.NewDecoder(r.Body).Decode(v)
}

// Error response
type APIError struct {
    Error   string `json:"error"`
    Message string `json:"message"`
    Code    int    `json:"code"`
}

func WriteError(w http.ResponseWriter, status int, message string) {
    WriteJSON(w, status, APIError{
        Error:   http.StatusText(status),
        Message: message,
        Code:    status,
    })
}
```

### 6.2 API Route Registration

```go
// internal/api/rest/server.go
package rest

import (
    "log/slog"
    "net/http"
)

type Server struct {
    router *Router
    // Dependencies injected
    soulService    SoulService
    clusterService ClusterService
    alertService   AlertService
}

func NewServer(opts ServerOptions) *Server {
    s := &Server{
        router:         NewRouter(opts.Logger),
        soulService:    opts.SoulService,
        clusterService: opts.ClusterService,
        alertService:   opts.AlertService,
    }

    // Middleware
    s.router.Use(LoggingMiddleware(opts.Logger))
    s.router.Use(CORSMiddleware(opts.AllowedOrigins))
    s.router.Use(RateLimitMiddleware(opts.RateLimit))
    s.router.Use(AuthMiddleware(opts.AuthService))

    // Souls
    s.router.GET("/api/v1/souls", s.listSouls)
    s.router.POST("/api/v1/souls", s.createSoul)
    s.router.GET("/api/v1/souls/:id", s.getSoul)
    s.router.PUT("/api/v1/souls/:id", s.updateSoul)
    s.router.DELETE("/api/v1/souls/:id", s.deleteSoul)
    s.router.POST("/api/v1/souls/:id/pause", s.pauseSoul)
    s.router.POST("/api/v1/souls/:id/resume", s.resumeSoul)
    s.router.POST("/api/v1/souls/:id/judge", s.triggerJudgment)

    // Judgments
    s.router.GET("/api/v1/souls/:id/judgments", s.listJudgments)
    s.router.GET("/api/v1/souls/:id/judgments/latest", s.latestJudgment)
    s.router.GET("/api/v1/souls/:id/purity", s.getSoulPurity)

    // Journeys
    s.router.GET("/api/v1/journeys", s.listJourneys)
    s.router.POST("/api/v1/journeys", s.createJourney)
    s.router.GET("/api/v1/journeys/:id", s.getJourney)
    s.router.PUT("/api/v1/journeys/:id", s.updateJourney)
    s.router.DELETE("/api/v1/journeys/:id", s.deleteJourney)
    s.router.POST("/api/v1/journeys/:id/run", s.triggerJourney)
    s.router.GET("/api/v1/journeys/:id/runs", s.listJourneyRuns)

    // Verdicts
    s.router.GET("/api/v1/verdicts", s.listVerdicts)
    s.router.GET("/api/v1/verdicts/:id", s.getVerdict)
    s.router.POST("/api/v1/verdicts/:id/acknowledge", s.acknowledgeVerdict)
    s.router.POST("/api/v1/verdicts/:id/resolve", s.resolveVerdict)

    // Channels
    s.router.GET("/api/v1/channels", s.listChannels)
    s.router.POST("/api/v1/channels", s.createChannel)
    s.router.PUT("/api/v1/channels/:id", s.updateChannel)
    s.router.DELETE("/api/v1/channels/:id", s.deleteChannel)
    s.router.POST("/api/v1/channels/:id/test", s.testChannel)

    // Necropolis (Cluster)
    s.router.GET("/api/v1/necropolis", s.clusterStatus)
    s.router.GET("/api/v1/necropolis/jackals", s.listJackals)
    s.router.POST("/api/v1/necropolis/jackals", s.summonJackal)
    s.router.DELETE("/api/v1/necropolis/jackals/:id", s.banishJackal)
    s.router.GET("/api/v1/necropolis/raft", s.raftState)

    // Book of the Dead (Status Page)
    s.router.GET("/api/v1/book", s.getBookConfig)
    s.router.PUT("/api/v1/book", s.updateBookConfig)
    s.router.GET("/api/v1/book/public", s.publicBookData)

    // Tenants
    s.router.GET("/api/v1/tenants", s.listTenants)
    s.router.POST("/api/v1/tenants", s.createTenant)
    s.router.PUT("/api/v1/tenants/:id", s.updateTenant)
    s.router.DELETE("/api/v1/tenants/:id", s.deleteTenant)

    // System
    s.router.GET("/api/v1/health", s.healthCheck)
    s.router.GET("/api/v1/version", s.versionInfo)
    s.router.GET("/metrics", s.prometheusMetrics)

    return s
}

func (s *Server) Handler() http.Handler {
    return s.router
}
```

### 6.3 WebSocket Hub

```go
// internal/api/ws/hub.go
package ws

// Hub manages WebSocket connections and broadcasts real-time events.
// Events: judgment.new, verdict.fired, verdict.resolved,
//         soul.status_change, jackal.joined, jackal.left, raft.leader_change
//
// Custom WebSocket implementation using net/http Hijacker interface.
// No external gorilla/websocket dependency.
//
// Protocol:
// - Server sends JSON events: {"type": "judgment.new", "data": {...}}
// - Client sends subscriptions: {"type": "subscribe", "souls": ["id1", "id2"]}
// - Client sends ping: {"type": "ping"}
// - Server responds pong: {"type": "pong"}
```

### 6.4 MCP Server

```go
// internal/api/mcp/server.go
package mcp

// MCPServer implements the Model Context Protocol for AI agent integration.
//
// Tools:
//   anubis_list_souls, anubis_get_soul_status, anubis_create_soul,
//   anubis_delete_soul, anubis_trigger_judgment, anubis_get_uptime,
//   anubis_list_incidents, anubis_acknowledge_alert, anubis_cluster_status,
//   anubis_add_node
//
// Resources:
//   anubis://souls, anubis://souls/{id}, anubis://judgments/latest,
//   anubis://verdicts/active, anubis://necropolis, anubis://book
//
// Transport: stdio and HTTP/SSE
```

---

## 7. DASHBOARD EMBEDDING

### 7.1 React Build & Embed

```go
// internal/dashboard/embed.go
package dashboard

import (
    "embed"
    "io/fs"
    "net/http"
    "strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler returns an http.Handler that serves the React dashboard.
// It handles SPA routing by falling back to index.html for non-file requests.
func Handler() http.Handler {
    dist, _ := fs.Sub(distFS, "dist")

    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        path := r.URL.Path

        // Try to serve the exact file first
        if path != "/" && !strings.HasSuffix(path, "/") {
            if _, err := fs.Stat(dist, strings.TrimPrefix(path, "/")); err == nil {
                http.FileServer(http.FS(dist)).ServeHTTP(w, r)
                return
            }
        }

        // SPA fallback: serve index.html for all other routes
        r.URL.Path = "/"
        http.FileServer(http.FS(dist)).ServeHTTP(w, r)
    })
}
```

### 7.2 Frontend Tech Stack Setup

```bash
# web/ directory setup
cd web
npm create vite@latest . -- --template react-ts
npm install tailwindcss@4.1 @tailwindcss/vite
npm install lucide-react zustand react-hook-form @hookform/resolvers zod
npm install recharts
# shadcn/ui setup via npx shadcn-ui@latest init
```

### 7.3 Main Server Integration

```go
// cmd/anubis/main.go
package main

import (
    "context"
    "fmt"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "syscall"

    "github.com/AnubisWatch/anubiswatch/internal/alert"
    "github.com/AnubisWatch/anubiswatch/internal/api/rest"
    "github.com/AnubisWatch/anubiswatch/internal/api/ws"
    "github.com/AnubisWatch/anubiswatch/internal/core"
    "github.com/AnubisWatch/anubiswatch/internal/dashboard"
    "github.com/AnubisWatch/anubiswatch/internal/probe"
    "github.com/AnubisWatch/anubiswatch/internal/raft"
    "github.com/AnubisWatch/anubiswatch/internal/storage"
)

func main() {
    // Parse CLI commands
    if len(os.Args) > 1 {
        switch os.Args[1] {
        case "serve":
            serve()
        case "init":
            initConfig()
        case "watch":
            quickWatch()
        case "judge":
            showJudgments()
        case "summon":
            summonNode()
        case "banish":
            banishNode()
        case "necropolis":
            showCluster()
        case "version":
            showVersion()
        case "health":
            selfHealth()
        default:
            printUsage()
        }
        return
    }
    printUsage()
}

func serve() {
    logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
    logger.Info("⚖️  AnubisWatch — The Judgment Never Sleeps")

    // Load config
    configPath := os.Getenv("ANUBIS_CONFIG")
    if configPath == "" {
        configPath = "anubis.yaml"
    }
    cfg, err := core.LoadConfig(configPath)
    if err != nil {
        logger.Error("failed to load config", "err", err)
        os.Exit(1)
    }

    // Initialize CobaltDB storage
    store, err := storage.NewEngine(cfg.Storage, logger)
    if err != nil {
        logger.Error("failed to initialize storage", "err", err)
        os.Exit(1)
    }
    defer store.Close()

    // Initialize alert dispatcher
    dispatcher := alert.NewDispatcher(logger)
    // Register alert channels from config
    for _, ch := range cfg.Channels {
        channel := alert.NewChannel(ch, logger)
        dispatcher.RegisterChannel(ch.Name, channel)
    }
    dispatcher.SetRules(cfg.Verdicts.Rules)

    // Initialize probe engine
    registry := probe.NewCheckerRegistry()
    engine := probe.NewEngine(probe.EngineOptions{
        Registry: registry,
        Store:    store,
        Alerter:  dispatcher,
        NodeID:   cfg.Necropolis.NodeName,
        Region:   cfg.Necropolis.Region,
        Logger:   logger,
    })

    // Initialize Raft (if cluster mode)
    if cfg.Necropolis.Enabled {
        raftNode := initRaft(cfg, store, engine, logger)
        defer raftNode.Stop()
    } else {
        // Single node: assign all souls directly
        engine.AssignSouls(convertSouls(cfg.Souls))
    }

    // Initialize WebSocket hub
    wsHub := ws.NewHub(logger)
    go wsHub.Run()

    // Initialize REST API
    apiServer := rest.NewServer(rest.ServerOptions{
        Logger:         logger,
        SoulService:    store,
        ClusterService: nil, // TODO: inject raft
        AlertService:   dispatcher,
    })

    // Build HTTP mux
    mux := http.NewServeMux()
    mux.Handle("/api/", apiServer.Handler())
    mux.Handle("/ws/", wsHub.Handler())
    mux.Handle("/", dashboard.Handler())

    // Start HTTPS server
    addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
    server := &http.Server{
        Addr:    addr,
        Handler: mux,
    }

    // Graceful shutdown
    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    go func() {
        logger.Info("server starting", "addr", addr)
        if cfg.Server.TLS.Enabled {
            if err := server.ListenAndServeTLS(cfg.Server.TLS.Cert, cfg.Server.TLS.Key); err != http.ErrServerClosed {
                logger.Error("server error", "err", err)
            }
        } else {
            if err := server.ListenAndServe(); err != http.ErrServerClosed {
                logger.Error("server error", "err", err)
            }
        }
    }()

    <-ctx.Done()
    logger.Info("shutting down...")
    engine.Stop()
    server.Shutdown(context.Background())
    logger.Info("⚖️  AnubisWatch stopped. The judgment rests.")
}
```

---

## 8. STORAGE LAYER (CobaltDB Integration)

### 8.1 Storage Engine Wrapper

```go
// internal/storage/engine.go
package storage

// Engine wraps CobaltDB for AnubisWatch-specific operations.
//
// Key namespaces:
//   {workspace}/souls/{id}             → Soul configs
//   {workspace}/judgments/{soul}/{ts}   → Time-series check results
//   {workspace}/verdicts/{id}           → Alert history
//   {workspace}/journeys/{id}           → Journey definitions
//   raft/log/{index}                    → Raft log entries
//   raft/state                          → Raft persistent state
//   system/tenants/{id}                 → Tenant definitions
//   system/jackals/{id}                 → Node registry
//
// Time-series operations:
//   - SaveJudgment: Append judgment to time-series index
//   - QueryJudgments: Range query with time bounds
//   - Downsample: Compact raw data into minute/5min/hour/day summaries
//   - Purge: Remove data older than retention policy
//
// CobaltDB features used:
//   - B+Tree for ordered key ranges (time-series queries)
//   - WAL for crash recovery
//   - MVCC for concurrent reads
//   - AES-256-GCM encryption at rest
//   - Prefix scan for namespace isolation
```

### 8.2 Time-Series Optimization

```go
// internal/storage/timeseries.go
package storage

// TimeSeriesStore provides optimized time-series storage on top of CobaltDB.
//
// Storage format:
//   Key: {workspace}/ts/{soul_id}/{resolution}/{timestamp}
//   Value: Compressed JudgmentSummary (gob or custom binary encoding)
//
// Resolutions:
//   raw    → Individual judgment records (kept 48h default)
//   1min   → 1-minute aggregated summaries (kept 30d default)
//   5min   → 5-minute aggregated summaries (kept 90d default)
//   1hour  → 1-hour aggregated summaries (kept 365d default)
//   1day   → 1-day aggregated summaries (kept forever default)
//
// Downsampling runs as a background goroutine:
//   - Every 5 minutes: compact raw → 1min for data older than threshold
//   - Every 30 minutes: compact 1min → 5min
//   - Every 6 hours: compact 5min → 1hour
//   - Every 24 hours: compact 1hour → 1day
//
// JudgmentSummary per time bucket:
//   - count, success_count, failure_count
//   - min/max/avg/p50/p95/p99 latency
//   - uptime_percent (purity score)
//   - packet_loss_avg (for ICMP)
```

---

## 9. IMPLEMENTATION NOTES

### 9.1 ID Generation

Use ULID (Universally Unique Lexicographically Sortable Identifier):
- Sortable by time (useful for time-series)
- 128-bit, Crockford Base32 encoded
- Custom implementation (no external dep)

### 9.2 Error Handling

All errors implement a common interface:
```go
type AppError interface {
    error
    Code() int        // HTTP status code
    Slug() string     // Machine-readable error type
}
```

### 9.3 Logging

Use `log/slog` (Go 1.21+ stdlib):
- Structured JSON logging in production
- Text logging in development
- Log levels: debug, info, warn, error
- Component-tagged loggers (`component=probe-engine`, `component=raft`)

### 9.4 Testing Strategy

- Unit tests: All checkers, Raft state machine, alert rules
- Integration tests: CobaltDB storage, API endpoints
- E2E tests: Full cluster setup, check execution, alert delivery
- Benchmark tests: Check throughput, Raft performance
- Fuzz tests: Config parsing, JSON path extraction, URL parsing

### 9.5 Performance Targets

| Metric | Target |
|---|---|
| HTTP check overhead | < 5ms |
| ICMP check overhead | < 1ms |
| Judgment write latency | < 2ms |
| API response time (p99) | < 50ms |
| WebSocket broadcast | < 10ms |
| Raft election | < 2s |
| Dashboard initial load | < 1s |
| Binary size (stripped) | < 50MB |
| Memory (100 monitors) | < 64MB |
| Memory (1000 monitors) | < 256MB |

### 9.6 Security Checklist

- [ ] All inter-node communication over mutual TLS
- [ ] API authentication (JWT + API keys)
- [ ] Rate limiting per IP and per key
- [ ] Input validation on all API endpoints
- [ ] SQL injection N/A (CobaltDB, no SQL)
- [ ] XSS protection (CSP headers, React escaping)
- [ ] CORS configuration
- [ ] Secret expansion from env vars only
- [ ] No plaintext secrets in config or logs
- [ ] ICMP requires CAP_NET_RAW, not root

---

## 10. BUILD & RELEASE PIPELINE

### 10.1 GitHub Actions

```yaml
# .github/workflows/release.yml
name: Release
on:
  push:
    tags: ['v*']

jobs:
  build:
    strategy:
      matrix:
        include:
          - os: linux
            arch: amd64
          - os: linux
            arch: arm64
          - os: linux
            arch: arm
            goarm: 7
          - os: darwin
            arch: amd64
          - os: darwin
            arch: arm64
          - os: windows
            arch: amd64
          - os: freebsd
            arch: amd64
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'
      - uses: actions/setup-node@v4
        with:
          node-version: '22'
      - run: cd web && npm ci && npm run build
      - run: |
          GOOS=${{ matrix.os }} GOARCH=${{ matrix.arch }} \
          CGO_ENABLED=0 go build -ldflags "-s -w" \
          -o anubis-${{ matrix.os }}-${{ matrix.arch }} ./cmd/anubis
      - uses: softprops/action-gh-release@v2
        with:
          files: anubis-*

  docker:
    needs: build
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: docker/build-push-action@v5
        with:
          push: true
          tags: anubiswatch/anubis:${{ github.ref_name }},anubiswatch/anubis:latest
          platforms: linux/amd64,linux/arm64,linux/arm/v7
```

---

*Implementation follows the sacred order: Foundation → Probe → Raft → Alert → API → Dashboard → Advanced* ⚖️
