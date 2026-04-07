package core

import (
	"encoding/json"
	"fmt"
	"time"
)

// Soul represents a monitored target — the entity whose heart is weighed.
type Soul struct {
	ID          string           `json:"id" yaml:"id"`
	WorkspaceID string           `json:"workspace_id" yaml:"-"`
	Name        string           `json:"name" yaml:"name"`
	Type        CheckType        `json:"type" yaml:"type"`
	Target      string           `json:"target" yaml:"target"`
	Weight      Duration         `json:"weight" yaml:"weight"` // check interval
	Timeout     Duration         `json:"timeout" yaml:"timeout"`
	Enabled     bool             `json:"enabled" yaml:"enabled"`
	Tags        []string         `json:"tags" yaml:"tags"`
	Regions     []string         `json:"regions" yaml:"regions"` // restrict to specific regions
	Region      string           `json:"region" yaml:"region"`   // assigned region
	HTTP        *HTTPConfig      `json:"http,omitempty" yaml:"http,omitempty"`
	TCP         *TCPConfig       `json:"tcp,omitempty" yaml:"tcp,omitempty"`
	UDP         *UDPConfig       `json:"udp,omitempty" yaml:"udp,omitempty"`
	DNS         *DNSConfig       `json:"dns,omitempty" yaml:"dns,omitempty"`
	SMTP        *SMTPConfig      `json:"smtp,omitempty" yaml:"smtp,omitempty"`
	IMAP        *IMAPConfig      `json:"imap,omitempty" yaml:"imap,omitempty"`
	ICMP        *ICMPConfig      `json:"icmp,omitempty" yaml:"icmp,omitempty"`
	GRPC        *GRPCConfig      `json:"grpc,omitempty" yaml:"grpc,omitempty"`
	WebSocket   *WebSocketConfig `json:"websocket,omitempty" yaml:"websocket,omitempty"`
	TLS         *TLSConfig       `json:"tls,omitempty" yaml:"tls,omitempty"`
	CreatedAt   time.Time        `json:"created_at" yaml:"-"`
	UpdatedAt   time.Time        `json:"updated_at" yaml:"-"`
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
	SoulAlive    SoulStatus = "alive"    // Passed to Aaru (paradise)
	SoulDead     SoulStatus = "dead"     // Devoured by Ammit
	SoulDegraded SoulStatus = "degraded" // Heart is heavy
	SoulUnknown  SoulStatus = "unknown"  // Not yet judged
	SoulEmbalmed SoulStatus = "embalmed" // Maintenance window
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

// UnmarshalJSON implements json.Unmarshaler for Duration
func (d *Duration) UnmarshalJSON(data []byte) error {
	// Remove quotes from JSON string
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		dur, err := time.ParseDuration(s)
		if err != nil {
			return err
		}
		d.Duration = dur
		return nil
	}

	// Try parsing as nanoseconds (number)
	var ns int64
	if err := json.Unmarshal(data, &ns); err != nil {
		return err
	}
	d.Duration = time.Duration(ns)
	return nil
}

// MarshalJSON implements json.Marshaler for Duration
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

// validate validates the soul configuration
func (s Soul) validate(index int) error {
	if s.Name == "" {
		return &ConfigError{Field: fmt.Sprintf("souls[%d].name", index), Message: "name is required"}
	}
	if s.Target == "" {
		return &ConfigError{Field: fmt.Sprintf("souls[%d].target", index), Message: "target is required"}
	}
	if s.Type == "" {
		return &ConfigError{Field: fmt.Sprintf("souls[%d].type", index), Message: "type is required"}
	}

	// Validate type-specific config
	validTypes := map[CheckType]bool{
		CheckHTTP: true, CheckTCP: true, CheckUDP: true, CheckDNS: true,
		CheckSMTP: true, CheckIMAP: true, CheckICMP: true, CheckGRPC: true,
		CheckWebSocket: true, CheckTLS: true,
	}
	if !validTypes[s.Type] {
		return &ConfigError{Field: fmt.Sprintf("souls[%d].type", index), Message: fmt.Sprintf("invalid type: %s", s.Type)}
	}

	// Validate interval
	if s.Weight.Duration < 0 {
		return &ConfigError{Field: fmt.Sprintf("souls[%d].weight", index), Message: "interval cannot be negative"}
	}

	// Validate timeout
	if s.Timeout.Duration < 0 {
		return &ConfigError{Field: fmt.Sprintf("souls[%d].timeout", index), Message: "timeout cannot be negative"}
	}

	// Type-specific validation
	switch s.Type {
	case CheckHTTP:
		if s.HTTP == nil {
			return &ConfigError{Field: fmt.Sprintf("souls[%d].http", index), Message: "http configuration is required for HTTP checks"}
		}
		if s.HTTP.Method == "" {
			return &ConfigError{Field: fmt.Sprintf("souls[%d].http.method", index), Message: "HTTP method is required"}
		}
		validMethods := map[string]bool{"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true, "HEAD": true, "OPTIONS": true}
		if !validMethods[s.HTTP.Method] {
			return &ConfigError{Field: fmt.Sprintf("souls[%d].http.method", index), Message: fmt.Sprintf("invalid HTTP method: %s", s.HTTP.Method)}
		}
		if len(s.HTTP.ValidStatus) == 0 {
			return &ConfigError{Field: fmt.Sprintf("souls[%d].http.valid_status", index), Message: "at least one valid status code is required"}
		}
	case CheckTCP:
		if s.TCP == nil {
			return &ConfigError{Field: fmt.Sprintf("souls[%d].tcp", index), Message: "tcp configuration is required for TCP checks"}
		}
	case CheckDNS:
		if s.DNS == nil {
			return &ConfigError{Field: fmt.Sprintf("souls[%d].dns", index), Message: "dns configuration is required for DNS checks"}
		}
		if s.DNS.RecordType == "" {
			return &ConfigError{Field: fmt.Sprintf("souls[%d].dns.record_type", index), Message: "DNS record type is required"}
		}
		validRecordTypes := map[string]bool{"A": true, "AAAA": true, "CNAME": true, "MX": true, "TXT": true, "NS": true, "SOA": true, "PTR": true, "SRV": true}
		if !validRecordTypes[s.DNS.RecordType] {
			return &ConfigError{Field: fmt.Sprintf("souls[%d].dns.record_type", index), Message: fmt.Sprintf("invalid DNS record type: %s", s.DNS.RecordType)}
		}
	}

	return nil
}

// HTTPConfig defines HTTP/HTTPS check settings
type HTTPConfig struct {
	Method             string            `json:"method" yaml:"method"`
	Headers            map[string]string `json:"headers" yaml:"headers"`
	Body               string            `json:"body" yaml:"body"`
	ValidStatus        []int             `json:"valid_status" yaml:"valid_status"`
	BodyContains       string            `json:"body_contains" yaml:"body_contains"`
	BodyRegex          string            `json:"body_regex" yaml:"body_regex"`
	JSONPath           map[string]string `json:"json_path" yaml:"json_path"`
	JSONSchema         string            `json:"json_schema" yaml:"json_schema"`
	JSONSchemaStrict   bool              `json:"json_schema_strict" yaml:"json_schema_strict"`
	ResponseHeaders    map[string]string `json:"response_headers" yaml:"response_headers"`
	Feather            Duration          `json:"feather" yaml:"feather"` // Performance budget
	FollowRedirects    bool              `json:"follow_redirects" yaml:"follow_redirects"`
	MaxRedirects       int               `json:"max_redirects" yaml:"max_redirects"`
	InsecureSkipVerify bool              `json:"insecure_skip_verify" yaml:"insecure_skip_verify"`
}

// TCPConfig defines TCP check settings
type TCPConfig struct {
	BannerMatch string `json:"banner_match" yaml:"banner_match"`
	Send        string `json:"send" yaml:"send"`
	ExpectRegex string `json:"expect_regex" yaml:"expect_regex"`
}

// UDPConfig defines UDP check settings
type UDPConfig struct {
	SendHex        string `json:"send_hex" yaml:"send_hex"`
	ExpectContains string `json:"expect_contains" yaml:"expect_contains"`
}

// DNSConfig defines DNS check settings
type DNSConfig struct {
	RecordType           string   `json:"record_type" yaml:"record_type"`
	Expected             []string `json:"expected" yaml:"expected"`
	Nameservers          []string `json:"nameservers" yaml:"nameservers"`
	DNSSECValidate       bool     `json:"dnssec_validate" yaml:"dnssec_validate"`
	PropagationCheck     bool     `json:"propagation_check" yaml:"propagation_check"`
	PropagationThreshold int      `json:"propagation_threshold" yaml:"propagation_threshold"`
}

// SMTPConfig defines SMTP check settings
type SMTPConfig struct {
	EHLODomain         string     `json:"ehlo_domain" yaml:"ehlo_domain"`
	StartTLS           bool       `json:"starttls" yaml:"starttls"`
	InsecureSkipVerify bool       `json:"insecure_skip_verify" yaml:"insecure_skip_verify"`
	Auth               *AuthCreds `json:"auth,omitempty" yaml:"auth,omitempty"`
	BannerContains     string     `json:"banner_contains" yaml:"banner_contains"`
}

// IMAPConfig defines IMAP check settings
type IMAPConfig struct {
	TLS                bool       `json:"tls" yaml:"tls"`
	Auth               *AuthCreds `json:"auth,omitempty" yaml:"auth,omitempty"`
	CheckMailbox       string     `json:"check_mailbox" yaml:"check_mailbox"`
	InsecureSkipVerify bool       `json:"insecure_skip_verify" yaml:"insecure_skip_verify"`
}

// AuthCreds holds authentication credentials
type AuthCreds struct {
	Username string `json:"username" yaml:"username"`
	Password string `json:"password" yaml:"password"`
}

// ICMPConfig defines ICMP ping check settings
type ICMPConfig struct {
	Count          int      `json:"count" yaml:"count"`
	Interval       Duration `json:"interval" yaml:"interval"`
	MaxLossPercent float64  `json:"max_loss_percent" yaml:"max_loss_percent"`
	Feather        Duration `json:"feather" yaml:"feather"`
	IPv6           bool     `json:"ipv6" yaml:"ipv6"`
	Privileged     bool     `json:"privileged" yaml:"privileged"`
}

// GRPCConfig defines gRPC health check settings
type GRPCConfig struct {
	Service            string            `json:"service" yaml:"service"`
	TLS                bool              `json:"tls" yaml:"tls"`
	TLSCA              string            `json:"tls_ca" yaml:"tls_ca"`
	InsecureSkipVerify bool              `json:"insecure_skip_verify" yaml:"insecure_skip_verify"`
	Metadata           map[string]string `json:"metadata" yaml:"metadata"`
	Feather            Duration          `json:"feather" yaml:"feather"`
}

// WebSocketConfig defines WebSocket check settings
type WebSocketConfig struct {
	Headers            map[string]string `json:"headers" yaml:"headers"`
	Subprotocols       []string          `json:"subprotocols" yaml:"subprotocols"`
	Send               string            `json:"send" yaml:"send"`
	ExpectContains     string            `json:"expect_contains" yaml:"expect_contains"`
	PingCheck          bool              `json:"ping_check" yaml:"ping_check"`
	Feather            Duration          `json:"feather" yaml:"feather"`
	InsecureSkipVerify bool              `json:"insecure_skip_verify" yaml:"insecure_skip_verify"`
}

// TLSConfig defines TLS certificate check settings
type TLSConfig struct {
	ExpiryWarnDays     int      `json:"expiry_warn_days" yaml:"expiry_warn_days"`
	ExpiryCriticalDays int      `json:"expiry_critical_days" yaml:"expiry_critical_days"`
	MinProtocol        string   `json:"min_protocol" yaml:"min_protocol"`
	ForbiddenCiphers   []string `json:"forbidden_ciphers" yaml:"forbidden_ciphers"`
	ExpectedIssuer     string   `json:"expected_issuer" yaml:"expected_issuer"`
	ExpectedSAN        []string `json:"expected_san" yaml:"expected_san"`
	CheckOCSP          bool     `json:"check_ocsp" yaml:"check_ocsp"`
	MinKeyBits         int      `json:"min_key_bits" yaml:"min_key_bits"`
}

// RaftState represents the state of a Raft node
type RaftState string

const (
	StateFollower  RaftState = "follower"
	StateCandidate RaftState = "candidate"
	StateLeader    RaftState = "leader"
)

// IsLeader returns true if the state is Leader
func (s RaftState) IsLeader() bool {
	return s == StateLeader
}

// IsFollower returns true if the state is Follower
func (s RaftState) IsFollower() bool {
	return s == StateFollower
}

// IsCandidate returns true if the state is Candidate
func (s RaftState) IsCandidate() bool {
	return s == StateCandidate
}

// String returns the string representation of the state
func (s RaftState) String() string {
	return string(s)
}

// RaftLogEntry represents a single entry in the Raft log
type RaftLogEntry struct {
	Index uint64       `json:"index"`
	Term  uint64       `json:"term"`
	Type  LogEntryType `json:"type"`
	Data  []byte       `json:"data"`
}

// LogEntryType distinguishes different types of log entries
type LogEntryType uint8

const (
	LogCommand LogEntryType = iota
	LogNoOp
	LogConfiguration
)

func (t LogEntryType) String() string {
	switch t {
	case LogCommand:
		return "command"
	case LogNoOp:
		return "noop"
	case LogConfiguration:
		return "configuration"
	default:
		return "unknown"
	}
}

// RaftPeerInfo extends RaftPeer with runtime information
type RaftPeerInfo struct {
	RaftPeer
	IsConnected  bool      `json:"is_connected"`
	LastContact  time.Time `json:"last_contact"`
	NextIndex    uint64    `json:"next_index"`
	MatchIndex   uint64    `json:"match_index"`
	Inflight     uint64    `json:"inflight"`
	HeartbeatRTT Duration  `json:"heartbeat_rtt"`
}

// Stats represents system statistics
type Stats struct {
	TotalSouls      int          `json:"total_souls"`
	AliveSouls      int          `json:"alive_souls"`
	DeadSouls       int          `json:"dead_souls"`
	DegradedSouls   int          `json:"degraded_souls"`
	TotalJudgments  int64        `json:"total_judgments"`
	AvgResponseTime Duration     `json:"avg_response_time"`
	UptimePercent   float64      `json:"uptime_percent"`
	ProbeStatus     *ProbeStatus `json:"probe_status"`
}

// ProbeStatus represents the probe engine status
type ProbeStatus struct {
	Running         bool     `json:"running"`
	ActiveChecks    int      `json:"active_checks"`
	ChecksRunning   int      `json:"checks_running"`
	ChecksPerSecond float64  `json:"checks_per_second"`
	AvgLatency      Duration `json:"avg_latency"`
	FailedChecks    int64    `json:"failed_checks"`
	TotalChecks     int64    `json:"total_checks"`
	NodeID          string   `json:"node_id,omitempty"`
	Region          string   `json:"region,omitempty"`
}

// ClusterState represents the current state of the cluster
type ClusterState struct {
	NodeID       string         `json:"node_id"`
	State        RaftState      `json:"state"`
	Term         uint64         `json:"term"`
	LastLogIndex uint64         `json:"last_log_index"`
	LastLogTerm  uint64         `json:"last_log_term"`
	CommitIndex  uint64         `json:"commit_index"`
	LastApplied  uint64         `json:"last_applied"`
	LeaderID     string         `json:"leader_id"`
	VotedFor     string         `json:"voted_for"`
	Peers        []RaftPeerInfo `json:"peers"`
	Stats        ClusterStats   `json:"stats"`
	LastContact  time.Time      `json:"last_contact"`
	Uptime       Duration       `json:"uptime"`
}

// ClusterStats holds cluster-wide statistics
type ClusterStats struct {
	TotalCommands   uint64 `json:"total_commands"`
	AppliedCommands uint64 `json:"applied_commands"`
	FailedCommands  uint64 `json:"failed_commands"`
	SnapshotsTaken  int    `json:"snapshots_taken"`
	ElectionsWon    uint64 `json:"elections_won"`
	ElectionsLost   uint64 `json:"elections_lost"`
	LeaderChanges   uint64 `json:"leader_changes"`
	HeartbeatCount  uint64 `json:"heartbeat_count"`
}

// NodeCapabilities describes what a node can do
type NodeCapabilities struct {
	CanProbe     bool `json:"can_probe"`
	CanStore     bool `json:"can_store"`
	CanAlert     bool `json:"can_alert"`
	CanCompute   bool `json:"can_compute"`
	MaxSouls     int  `json:"max_souls"`
	MaxCheckRate int  `json:"max_check_rate"`
}

// NodeInfo represents runtime node information
type NodeInfo struct {
	ID            string            `json:"id"`
	Region        string            `json:"region"`
	Address       string            `json:"address"`
	Version       string            `json:"version"`
	State         RaftState         `json:"state"`
	Role          RaftRole          `json:"role"`
	Capabilities  NodeCapabilities  `json:"capabilities"`
	AssignedSouls int               `json:"assigned_souls"`
	ActiveChecks  int               `json:"active_checks"`
	LoadAvg       float64           `json:"load_avg"`
	MemoryUsage   float64           `json:"memory_usage"`
	DiskUsage     float64           `json:"disk_usage"`
	LastHeartbeat time.Time         `json:"last_heartbeat"`
	StartTime     time.Time         `json:"start_time"`
	Tags          map[string]string `json:"tags"`
	CanProbe      bool              `json:"can_probe"`
	MaxSouls      int               `json:"max_souls"`
}

// FSMCommand represents a command to be applied to the FSM
type FSMCommand struct {
	Op    FSMOp  `json:"op"`
	Table string `json:"table"`
	Key   string `json:"key"`
	Value []byte `json:"value"`
}

// FSMOp represents FSM operation types
type FSMOp uint8

const (
	FSMSet FSMOp = iota
	FSMDelete
	FSMDeletePrefix
)

// DistributionPlan represents the current distribution of souls across the cluster
type DistributionPlan struct {
	Version     uint64               `json:"version"`
	Timestamp   time.Time            `json:"timestamp"`
	Strategy    DistributionStrategy `json:"strategy"`
	Assignments []SoulAssignment     `json:"assignments"`
	Revision    uint64               `json:"revision"`
}

// SoulAssignment represents a soul-to-node assignment
type SoulAssignment struct {
	SoulID   string `json:"soul_id"`
	NodeID   string `json:"node_id"`
	Region   string `json:"region"`
	Priority int    `json:"priority"`
	IsBackup bool   `json:"is_backup"`
}

// DistributionStrategy determines how souls are distributed across nodes
type DistributionStrategy string

const (
	StrategyRoundRobin  DistributionStrategy = "round_robin"
	StrategyRegionAware DistributionStrategy = "region_aware"
	StrategyRedundant   DistributionStrategy = "redundant"
	StrategyWeighted    DistributionStrategy = "weighted"
)

// SnapshotMeta holds metadata about a snapshot
type SnapshotMeta struct {
	ID      string `json:"id"`
	Index   uint64 `json:"index"`
	Term    uint64 `json:"term"`
	Size    int64  `json:"size"`
	Version uint64 `json:"version"`
}

// RaftError represents Raft-specific errors
type RaftError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	NodeID  string `json:"node_id,omitempty"`
}

func (e *RaftError) Error() string {
	return fmt.Sprintf("raft error [%s]: %s", e.Code, e.Message)
}

// Common Raft error codes
const (
	ErrNotLeader          = "NOT_LEADER"
	ErrLeadershipLost     = "LEADERSHIP_LOST"
	ErrTimeout            = "TIMEOUT"
	ErrShutdown           = "SHUTDOWN"
	ErrUnknownPeer        = "UNKNOWN_PEER"
	ErrPeerExists         = "PEER_EXISTS"
	ErrTooManyPeers       = "TOO_MANY_PEERS"
	ErrInvalidConfig      = "INVALID_CONFIG"
	ErrLogNotFound        = "LOG_NOT_FOUND"
	ErrSnapshotInProgress = "SNAPSHOT_IN_PROGRESS"
)
