# AnubisWatch Full Architecture Documentation

**Version:** 1.0
**Generated:** 2026-05-22
**Language:** English

## Table of Contents

1. System Overview
2. High-Level Architecture
3. Core Components
4. Data Models & Storage
5. API Endpoints Reference
6. Process Flows
7. Cluster Architecture (Raft)
8. Probe & Monitoring Engine
9. Alert System
10. Multi-Tenancy & Workspaces
11. Time-Series & Metrics
12. Real-Time Communication (WebSocket)
13. AI Integration (MCP)
14. Security Architecture
15. Deployment Topologies
---

## 3. Core Components

### 3.1 RESTServer (internal/api/rest.go)

The main API gateway written in Go, handling all HTTP traffic.

**Middleware Stack (in order):**
1. loggingMiddleware - Request/response logging
2. securityHeadersMiddleware - CSP, X-Frame-Options, etc.
3. corsMiddleware - Cross-origin resource sharing
4. recoveryMiddleware - Panic recovery
5. validateJSONMiddleware - JSON depth/size validation (max depth: 32, max size: 1MB)
6. validatePathParams - Parameter validation
7. rateLimitMiddleware - Per-IP rate limiting

### 3.2 Probe Engine (internal/probe/engine.go)

Schedules and executes health checks across assigned souls.

**Key Features:**
- Soul lifecycle management (assign, start, stop)
- Check scheduling with tickers
- Concurrency limiting via semaphore (max: 100 concurrent)
- Circuit breaker implementation (5 failures to open, 3 to close)
- Judgment persistence with retry (3 retries with exponential backoff)
- Alert dispatcher notification

### 3.3 Alert Manager (internal/alert/manager.go)

Handles alert routing, deduplication, and delivery.

**Supported Channel Types:** email, slack, discord, telegram, pagerduty, opsgenie, ntfy, webhook, sms, mcp

**Alert Condition Types:** consecutive_failures, threshold, percentage, anomaly, compound, status_change, status_for, failure_rate

### 3.4 Raft Node (internal/raft/node.go)

Implements the consensus algorithm for cluster coordination.

**Raft States:** follower, candidate, leader

**Log Entry Types:** LogCommand (regular FSM command), LogNoOp (leader heartbeat), LogConfiguration (cluster config), LogMembershipChange (joint consensus)

### 3.5 Storage Engine - CobaltDB (internal/storage/engine.go)

B+Tree-based embedded database with WAL for crash recovery.

**Key Features:**
- B+Tree index with configurable order (default: 32, min: 4, max: 256)
- Write-Ahead Log (WAL) for durability
- AES-256-GCM encryption at rest (optional)
- MVCC (Multi-Version Concurrency Control) support
- Prefix scanning for efficient queries

### 3.6 Cluster Manager (internal/cluster/manager.go)

Handles cluster coordination and soul distribution.

---

## 4. Data Models & Storage

### 4.1 Soul (Monitored Target)

```go
type Soul struct {
    ID          string           `json:"id"`
    WorkspaceID string           `json:"workspace_id"`
    Name        string           `json:"name"`
    Type        CheckType        `json:"type"`
    Target      string           `json:"target"`
    Weight      Duration         `json:"weight"`
    Timeout     Duration         `json:"timeout"`
    Enabled     bool             `json:"enabled"`
    Tags        []string         `json:"tags"`
    Regions     []string         `json:"regions"`
    HTTP        *HTTPConfig      `json:"http,omitempty"`
    TCP         *TCPConfig       `json:"tcp,omitempty"`
    DNS         *DNSConfig       `json:"dns,omitempty"`
    SMTP        *SMTPConfig      `json:"smtp,omitempty"`
    TLS         *TLSConfig       `json:"tls,omitempty"`
    GRPC        *GRPCConfig      `json:"grpc,omitempty"`
    WebSocket   *WebSocketConfig `json:"websocket,omitempty"`
    ICMP        *ICMPConfig      `json:"icmp,omitempty"`
    UDP         *UDPConfig       `json:"udp,omitempty"`
    IMAP        *IMAPConfig      `json:"imap,omitempty"`
    CreatedAt   time.Time        `json:"created_at"`
    UpdatedAt   time.Time        `json:"updated_at"`
}
```

**Check Types:** http, tcp, udp, dns, smtp, imap, icmp, grpc, websocket, tls

**Soul Status:** alive (Aaru/paradise), dead (Ammit/devoured), degraded, unknown, embalmed (maintenance)

### 4.2 HTTPConfig

```go
type HTTPConfig struct {
    Method             string            `json:"method"`
    Headers            map[string]string `json:"headers"`
    Body               string            `json:"body"`
    ValidStatus        []int             `json:"valid_status"`
    BodyContains       string            `json:"body_contains"`
    BodyRegex          string            `json:"body_regex"`
    JSONPath           map[string]string `json:"json_path"`
    JSONSchema         string            `json:"json_schema"`
    JSONSchemaStrict   bool              `json:"json_schema_strict"`
    ResponseHeaders    map[string]string `json:"response_headers"`
    Feather            Duration          `json:"feather"`
    FollowRedirects    bool              `json:"follow_redirects"`
    MaxRedirects       int               `json:"max_redirects"`
    InsecureSkipVerify bool              `json:"insecure_skip_verify"`
}
```

### 4.3 Judgment (Check Result)

```go
type Judgment struct {
    ID          string           `json:"id"`
    SoulID      string           `json:"soul_id"`
    WorkspaceID string           `json:"workspace_id"`
    JackalID    string           `json:"jackal_id"`
    Region      string           `json:"region"`
    Timestamp   time.Time        `json:"timestamp"`
    Duration    time.Duration    `json:"duration"`
    Status      SoulStatus       `json:"status"`
    StatusCode  int              `json:"status_code"`
    Message     string           `json:"message"`
    Details     *JudgmentDetails `json:"details,omitempty"`
    TLSInfo     *TLSInfo         `json:"tls_info,omitempty"`
}

type JudgmentDetails struct {
    ResponseHeaders map[string]string `json:"response_headers,omitempty"`
    ResponseBody    string            `json:"response_body,omitempty"`
    RedirectChain   []string          `json:"redirect_chain,omitempty"`
    ResolvedAddresses []string        `json:"resolved_addresses,omitempty"`
    DNSSECValid       *bool           `json:"dnssec_valid,omitempty"`
    PacketsSent     int     `json:"packets_sent,omitempty"`
    PacketsReceived int     `json:"packets_received,omitempty"`
    PacketLoss      float64 `json:"packet_loss,omitempty"`
    Banner string `json:"banner,omitempty"`
    Capabilities []string `json:"capabilities,omitempty"`
    ServiceStatus string `json:"service_status,omitempty"`
    CloseCode int `json:"close_code,omitempty"`
    Assertions []AssertionResult `json:"assertions,omitempty"`
}

type TLSInfo struct {
    Protocol        string    `json:"protocol"`
    CipherSuite     string    `json:"cipher_suite"`
    Issuer          string    `json:"issuer"`
    Subject         string    `json:"subject"`
    SANs            []string  `json:"sans"`
    NotBefore       time.Time `json:"not_before"`
    NotAfter        time.Time `json:"not_after"`
    DaysUntilExpiry int       `json:"days_until_expiry"`
    KeyType         string    `json:"key_type"`
    KeyBits         int       `json:"key_bits"`
    OCSPStapled     bool      `json:"ocsp_stapled"`
    ChainValid      bool      `json:"chain_valid"`
    ChainLength     int       `json:"chain_length"`
}

type AssertionResult struct {
    Type     string `json:"type"`
    Expected string `json:"expected"`
    Actual   string `json:"actual"`
    Passed   bool   `json:"passed"`
}
```

### 4.4 AlertChannel

```go
type AlertChannel struct {
    ID          string                 `json:"id"`
    Name        string                 `json:"name"`
    Type        AlertChannelType       `json:"type"`
    Enabled     bool                   `json:"enabled"`
    WorkspaceID string                 `json:"workspace_id"`
    Config      map[string]interface{} `json:"config"`
    Filters     []AlertFilter          `json:"filters"`
    RateLimit   RateLimitConfig        `json:"rate_limit"`
    RetryPolicy RetryPolicyConfig      `json:"retry_policy"`
    CreatedAt   time.Time              `json:"created_at"`
    UpdatedAt   time.Time              `json:"updated_at"`
}

type AlertFilter struct {
    Field    string   `json:"field"`
    Operator string   `json:"operator"`
    Value    string   `json:"value"`
    Values   []string `json:"values"`
}

type RateLimitConfig struct {
    Enabled     bool     `json:"enabled"`
    MaxAlerts   int      `json:"max_alerts"`
    Window      Duration `json:"window"`
    GroupingKey string   `json:"grouping_key"`
}

type RetryPolicyConfig struct {
    MaxRetries  int      `json:"max_retries"`
    InitialWait Duration `json:"initial_wait"`
    MaxWait     Duration `json:"max_wait"`
    Backoff     string   `json:"backoff"`
}
```

### 4.5 AlertRule

```go
type AlertRule struct {
    ID          string            `json:"id"`
    Name        string            `json:"name"`
    Enabled     bool              `json:"enabled"`
    WorkspaceID string            `json:"workspace_id"`
    Scope       RuleScope         `json:"scope"`
    Conditions  []AlertCondition  `json:"conditions"`
    Channels    []string          `json:"channels"`
    Severity    Severity          `json:"severity"`
    Cooldown    Duration          `json:"cooldown"`
    AutoResolve bool              `json:"auto_resolve"`
    Escalation  *EscalationPolicy `json:"escalation,omitempty"`
    CreatedAt   time.Time         `json:"created_at"`
}

type RuleScope struct {
    Type       string   `json:"type"`
    Tags       []string `json:"tags"`
    SoulTypes  []string `json:"soul_types"`
    SoulIDs    []string `json:"soul_ids"`
    Workspaces []string `json:"workspaces"`
}

type AlertCondition struct {
    Type      string   `json:"type"`
    Threshold int      `json:"threshold"`
    Metric    string   `json:"metric"`
    Operator  string   `json:"operator"`
    Value     any      `json:"value"`
    Window    Duration `json:"window"`
    From      string   `json:"from"`
    To        string   `json:"to"`
    Status    string   `json:"status"`
    Duration  Duration `json:"duration"`
    SubConditions []AlertCondition `json:"sub_conditions,omitempty"`
    Logic     string   `json:"logic"`
    AnomalyStdDev float64 `json:"anomaly_std_dev,omitempty"`
}

type Severity string
const (
    SeverityCritical Severity = "critical"
    SeverityWarning  Severity = "warning"
    SeverityInfo     Severity = "info"
)

type EscalationPolicy struct {
    Stages []EscalationStage `json:"stages"`
}

type EscalationStage struct {
    Wait      Duration `json:"wait"`
    Channels []string  `json:"channels"`
}
```

### 4.6 Incident

```go
type Incident struct {
    ID              string         `json:"id"`
    RuleID          string         `json:"rule_id"`
    SoulID          string         `json:"soul_id"`
    SoulName        string         `json:"soul_name,omitempty"`
    WorkspaceID     string         `json:"workspace_id"`
    Status          IncidentStatus `json:"status"`
    Severity        Severity       `json:"severity"`
    StartedAt       time.Time      `json:"started_at"`
    AckedAt         *time.Time     `json:"acked_at,omitempty"`
    ResolvedAt      *time.Time     `json:"resolved_at,omitempty"`
    AckedBy         string         `json:"acked_by,omitempty"`
    ResolvedBy      string         `json:"resolved_by,omitempty"`
    Notes           []IncidentNote `json:"notes"`
    Events          []AlertEvent   `json:"events"`
    EscalationLevel int            `json:"escalation_level"`
    LastEscalatedAt *time.Time     `json:"last_escalated_at,omitempty"`
}

type IncidentStatus string
const (
    IncidentOpen     IncidentStatus = "open"
    IncidentAcked     IncidentStatus = "acknowledged"
    IncidentResolved  IncidentStatus = "resolved"
)

type IncidentNote struct {
    Author    string    `json:"author"`
    Message   string    `json:"message"`
    Timestamp time.Time `json:"timestamp"`
}

type AlertEvent struct {
    ID           string            `json:"id"`
    ChannelID    string            `json:"channel_id"`
    ChannelType  AlertChannelType  `json:"channel_type"`
    SoulID       string            `json:"soul_id"`
    SoulName     string            `json:"soul_name"`
    WorkspaceID  string            `json:"workspace_id"`
    Status       SoulStatus        `json:"status"`
    PrevStatus   SoulStatus        `json:"prev_status"`
    Judgment     *Judgment         `json:"judgment"`
    Message      string            `json:"message"`
    Details      map[string]string `json:"details"`
    Severity     Severity          `json:"severity"`
    Timestamp    time.Time         `json:"timestamp"`
    Acknowledged bool              `json:"acknowledged"`
    Resolved     bool              `json:"resolved"`
    AckedAt      *time.Time        `json:"acked_at,omitempty"`
    ResolvedAt   *time.Time        `json:"resolved_at,omitempty"`
}
```

### 4.7 Workspace

```go
type Workspace struct {
    ID          string              `json:"id"`
    Name        string              `json:"name"`
    Slug        string              `json:"slug"`
    Description string              `json:"description,omitempty"`
    OwnerID     string              `json:"owner_id"`
    Quotas      WorkspaceQuotas      `json:"quotas"`
    Features    WorkspaceFeatures    `json:"features"`
    Status      WorkspaceStatus      `json:"status"`
    CreatedAt   time.Time           `json:"created_at"`
    UpdatedAt   time.Time          `json:"updated_at"`
    DeletedAt   *time.Time          `json:"deleted_at,omitempty"`
}

type WorkspaceQuotas struct {
    MaxSouls      int `json:"max_souls"`
    MaxChannels    int `json:"max_channels"`
    MaxRules       int `json:"max_rules"`
    MaxJourneys    int `json:"max_journeys"`
    MaxMembers     int `json:"max_members"`
    MaxApiCalls    int `json:"max_api_calls"`
    MaxStorageMb   int `json:"max_storage_mb"`
}

type WorkspaceFeatures struct {
    MultiRegion    bool `json:"multi_region"`
    CustomHeaders  bool `json:"custom_headers"`
    APIKeyAuth     bool `json:"api_key_auth"`
    LDAPAuth       bool `json:"ldap_auth"`
    OIDCAuth       bool `json:"oidc_auth"`
    JourneyEnabled bool `json:"journey_enabled"`
}

type WorkspaceStatus string
const (
    WorkspaceActive   WorkspaceStatus = "active"
    WorkspaceSuspended WorkspaceStatus = "suspended"
    WorkspaceDeleted  WorkspaceStatus = "deleted"
)
```

### 4.8 Journey (Multi-Step Synthetic Monitor)

```go
type JourneyConfig struct {
    Name              string            `json:"name"`
    ID                string            `json:"id"`
    Description       string           `json:"description,omitempty"`
    WorkspaceID       string           `json:"workspace_id"`
    Weight            Duration          `json:"weight"`
    Timeout           Duration          `json:"timeout"`
    ContinueOnFailure bool              `json:"continue_on_failure"`
    Variables         map[string]string `json:"variables"`
    Steps             []JourneyStep     `json:"steps"`
    Enabled           bool              `json:"enabled"`
    CreatedAt         time.Time         `json:"created_at"`
    UpdatedAt          time.Time         `json:"updated_at"`
}

type JourneyStep struct {
    Name       string                    `json:"name"`
    Type       CheckType                 `json:"type"`
    Target     string                    `json:"target"`
    Timeout    Duration                  `json:"timeout"`
    HTTP       *HTTPConfig               `json:"http,omitempty"`
    TCP        *TCPConfig                `json:"tcp,omitempty"`
    UDP        *UDPConfig                `json:"udp,omitempty"`
    DNS        *DNSConfig                `json:"dns,omitempty"`
    TLS        *TLSConfig                `json:"tls,omitempty"`
    Extract    map[string]ExtractionRule `json:"extract"`
    Assertions []Assertion               `json:"assertions,omitempty"`
}

type ExtractionRule struct {
    From  string `json:"from"`
    Path  string `json:"path"`
    Regex string `json:"regex"`
}

type Assertion struct {
    Type     string `json:"type"`
    Target   string `json:"target,omitempty"`
    Operator string `json:"operator"`
    Expected string `json:"expected"`
    Message  string `json:"message,omitempty"`
}

type JourneyRun struct {
    ID          string              `json:"id"`
    JourneyID   string              `json:"journey_id"`
    WorkspaceID string              `json:"workspace_id"`
    JackalID    string              `json:"jackal_id"`
    Region      string              `json:"region"`
    StartedAt   int64               `json:"started_at"`
    CompletedAt int64               `json:"completed_at"`
    Duration    int64               `json:"duration"`
    Status      SoulStatus          `json:"status"`
    Steps       []JourneyStepResult `json:"steps"`
    Variables   map[string]string   `json:"variables"`
}

type JourneyStepResult struct {
    Name      string            `json:"name"`
    StepIndex int               `json:"step_index"`
    Status    SoulStatus        `json:"status"`
    Duration  int64             `json:"duration"`
    Message   string            `json:"message"`
    Extracted map[string]string `json:"extracted,omitempty"`
}
```

---

## 5. API Endpoints Reference

### 5.1 System Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | /health | Health check |
| GET | /ready | Readiness check |
| GET | /metrics | Prometheus metrics |
| GET | /api/openapi.json | OpenAPI 3.0 spec |
| GET | /api/docs | Swagger UI |

**GET /health**
```json
{"status": "healthy", "timestamp": "2026-05-22T10:30:00Z"}
```

**GET /ready**
```json
{"status": "ready", "checks": {"storage": "ok", "alert_manager": "ok", "cluster": "standalone"}}
```
Status: 200 OK, 503 Service Unavailable

### 5.2 Authentication Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | /api/v1/auth/login | Login with email/password |
| POST | /api/v1/auth/logout | Logout and clear cookie |
| GET | /api/v1/auth/me | Get current user |
| POST | /api/v1/auth/workspace | Switch workspace |
| PUT | /api/v1/auth/change-password | Change password |
| POST | /api/v1/auth/reset-password | Request password reset |
| POST | /api/v1/auth/reset-password/confirm | Confirm password reset |
| GET | /api/v1/auth/oidc/login | OIDC login redirect |
| GET | /api/v1/auth/oidc/callback | OIDC callback |

**POST /api/v1/auth/login**
Request: `{"email": "admin@example.com", "password": "your-password"}`

Response (200):
```json
{
  "user": {"id": "user_abc123", "email": "admin@example.com", "name": "Admin User", "role": "admin", "workspace": "default", "created_at": "2026-01-01T00:00:00Z"},
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```
Cookie set: `auth_token` (HttpOnly, Secure, SameSite=Strict, 7-day expiry)

**POST /api/v1/auth/logout**
Response (200): `{"message": "logged out"}`

### 5.3 Souls (Monitors) Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/souls | List souls with pagination |
| POST | /api/v1/souls | Create new soul |
| GET | /api/v1/souls/{soul_id} | Get soul by ID |
| PUT | /api/v1/souls/{soul_id} | Update soul |
| DELETE | /api/v1/souls/{soul_id} | Delete soul |
| POST | /api/v1/souls/{soul_id}/pause | Pause soul (embalmed) |
| POST | /api/v1/souls/{soul_id}/resume | Resume soul |
| GET | /api/v1/souls/{soul_id}/judgments | Get soul judgment history |
| GET | /api/v1/souls/{soul_id}/metrics | Get soul time-series metrics |

**GET /api/v1/souls?offset=0&limit=20**
```json
{
  "data": [
    {
      "id": "soul_abc123",
      "name": "Example API",
      "type": "http",
      "target": "https://api.example.com/health",
      "status": "healthy",
      "last_check": "2026-05-22T10:00:00Z",
      "latency": 45,
      "weight": "60s",
      "timeout": "10s",
      "enabled": true,
      "tags": ["production", "api"],
      "regions": ["us-east-1"],
      "created_at": "2026-01-01T00:00:00Z"
    }
  ],
  "pagination": {"total": 50, "offset": 0, "limit": 20, "has_more": true, "next_offset": 20}
}
```

**POST /api/v1/souls**
```json
{
  "name": "My API Monitor",
  "type": "http",
  "target": "https://api.example.com/health",
  "weight": "60s",
  "timeout": "10s",
  "enabled": true,
  "tags": ["production", "api"],
  "http": {
    "method": "GET",
    "valid_status": [200, 201],
    "body_contains": "\"status\":\"ok\"",
    "response_headers": {"Content-Type": "application/json"},
    "feather": "2s"
  }
}
```
Required: name, type, target, valid_status (for HTTP type)

### 5.4 Alert Channels Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/channels | List alert channels |
| POST | /api/v1/channels | Create alert channel |
| GET | /api/v1/channels/{channel_id} | Get channel |
| PUT | /api/v1/channels/{channel_id} | Update channel |
| DELETE | /api/v1/channels/{channel_id} | Delete channel |

**POST /api/v1/channels (Slack example)**
```json
{
  "name": "Slack Alerts",
  "type": "slack",
  "enabled": true,
  "config": {"webhook_url": "https://hooks.slack.com/services/xxx"},
  "filters": [{"field": "severity", "operator": "in", "values": ["critical", "warning"]}]
}
```

### 5.5 Alert Rules Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/rules | List alert rules |
| POST | /api/v1/rules | Create alert rule |
| GET | /api/v1/rules/{rule_id} | Get rule |
| PUT | /api/v1/rules/{rule_id} | Update rule |
| DELETE | /api/v1/rules/{rule_id} | Delete rule |

**POST /api/v1/rules**
```json
{
  "name": "API Down Alert",
  "enabled": true,
  "scope": {"type": "all"},
  "conditions": [{"type": "consecutive_failures", "threshold": 3}],
  "channels": ["channel_abc123"],
  "severity": "critical",
  "cooldown": "5m",
  "auto_resolve": true
}
```

### 5.6 Incidents Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/incidents | List incidents |
| GET | /api/v1/incidents/{incident_id} | Get incident |
| POST | /api/v1/incidents/{incident_id}/ack | Acknowledge incident |
| POST | /api/v1/incidents/{incident_id}/resolve | Resolve incident |
| POST | /api/v1/incidents/{incident_id}/notes | Add note to incident |

### 5.7 Journeys Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/journeys | List journeys |
| POST | /api/v1/journeys | Create journey |
| GET | /api/v1/journeys/{journey_id} | Get journey |
| PUT | /api/v1/journeys/{journey_id} | Update journey |
| DELETE | /api/v1/journeys/{journey_id} | Delete journey |
| POST | /api/v1/journeys/{journey_id}/run | Run journey manually |
| GET | /api/v1/journeys/{journey_id}/runs | List journey runs |
| GET | /api/v1/journeys/{journey_id}/runs/{run_id} | Get journey run details |

### 5.8 Workspaces Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/workspaces | List workspaces |
| POST | /api/v1/workspaces | Create workspace |
| GET | /api/v1/workspaces/{workspace_id} | Get workspace |
| PUT | /api/v1/workspaces/{workspace_id} | Update workspace |
| DELETE | /api/v1/workspaces/{workspace_id} | Delete workspace |

### 5.9 Users Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/users | List users |
| POST | /api/v1/users | Create user |
| GET | /api/v1/users/{user_id} | Get user |
| PUT | /api/v1/users/{user_id} | Update user |
| DELETE | /api/v1/users/{user_id} | Delete user |

### 5.10 Status Pages Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/status-pages | List status pages |
| POST | /api/v1/status-pages | Create status page |
| GET | /api/v1/status-pages/{page_id} | Get status page |
| PUT | /api/v1/status-pages/{page_id} | Update status page |
| DELETE | /api/v1/status-pages/{page_id} | Delete status page |
| GET | /public/status-pages/{slug} | Get public status page |

### 5.11 MCP Endpoints (AI Integration)

| Method | Path | Description |
|--------|------|-------------|
| POST | /api/v1/mcp/query | Send MCP query for AI analysis |
| GET | /api/v1/mcp/resources | List available MCP resources |

---

## 6. Process Flows

### 6.1 Health Check Flow

```
1. Probe Engine schedules check based on Soul.Weight interval
2. Check executed against target (HTTP/TCP/DNS/etc)
3. Judgment created with status (alive/dead)
4. Judgment stored in CobaltDB
5. Alert Manager evaluates rules against new judgment
6. If rule conditions met, Verdict (alert) created
7. Alert dispatched to configured channels
8. Incident created/updated in storage
```

### 6.2 Alert Dispatch Flow

```
1. Verdict triggered by AlertRule condition
2. AlertManager checks deduplication (cooldown)
3. Rate limit checked for channel
4. AlertEvent created with retry policy
5. Dispatcher sends to channel (Slack/Email/PagerDuty/etc)
6. On failure, retry with exponential backoff
7. Escalation triggered if configured and no ACK
```

### 6.3 Raft Consensus Flow

```
1. Node starts as Follower
2. Election timeout triggers RequestVote to other nodes
3. If majority votes, node becomes Candidate then Leader
4. Leader sends AppendEntries heartbeats to followers
5. Commands replicated via log entries
6. FSM applies committed entries to state machine
7. On leader failure, new election triggered
```

---

## 7. Cluster Architecture (Raft)

### 7.1 Node Roles

- **Leader (Pharaoh)**: Coordinates cluster, replicates logs, accepts commands
- **Follower (Jackal)**: Participates in consensus, votes, receives heartbeats
- **Candidate**: Temporary state during election

### 7.2 Consensus Protocol

**Leader Election:**
1. Follower doesn't receive heartbeat within election timeout
2. Node becomes Candidate, increments term
3. RequestVote sent to all nodes
4. If majority votes, become Leader
5. Leader sends AppendEntries heartbeats

**Log Replication:**
1. Client sends command to Leader
2. Leader appends to local log
3. Leader sends AppendEntries to followers
4. If majority acknowledged, apply to FSM
5. Response sent to client

### 7.3 Membership Changes

- Joint consensus for adding/removing nodes
- Log entries ensure consistency during transitions
- Snapshots reduce log size and speed up recovery

---

## 8. Probe & Monitoring Engine

### 8.1 Check Scheduling

```go
type Scheduler struct {
    souls     map[string]*Soul
    tickers   map[string]*time.Ticker
    engine    *Engine
    mutex     sync.RWMutex
}
```

- Each enabled Soul has a ticker based on its Weight interval
- Checks executed in goroutines with semaphore limiting concurrency
- Failed checks trigger retry with exponential backoff

### 8.2 Circuit Breaker

```go
type CircuitBreaker struct {
    State       CircuitState
    Failures    int
    Successes   int
    LastFailure time.Time
    Threshold   int
    Timeout     time.Duration
}
```

States: Closed (normal) -> Open (blocked) -> Half-Open (probe)

### 8.3 Check Types

| Type | Protocol | Key Metrics |
|------|----------|-------------|
| HTTP | TCP/HTTP | status_code, latency, body_match |
| TCP | TCP | banner_match, connection_time |
| DNS | UDP/TCP | resolved_ips, dnssec_valid |
| TLS | TCP | cert_expiry, cipher_suite |
| SMTP | TCP | connection_time, capabilities |
| IMAP | TCP | connection_time, capabilities |
| ICMP | IP/ICMP | packet_loss, latency, jitter |
| gRPC | HTTP/2 | status_code, latency |
| WebSocket | TCP | connection_time, close_code |

---

## 9. Alert System

### 9.1 Alert Condition Evaluation

When a Judgment is created, the AlertManager:
1. Loads all enabled rules for the workspace
2. Evaluates each rule's scope against the soul
3. For matching rules, evaluates all conditions
4. If conditions met and cooldown expired, triggers verdict

### 9.2 Deduplication

- Each rule has a Cooldown duration
- After a verdict fires, no new verdicts for same rule/soul until cooldown expires
- Prevents alert fatigue from flapping

### 9.3 Escalation

```go
type EscalationPolicy struct {
    Stages []EscalationStage
}

type EscalationStage struct {
    Wait      Duration
    Channels []string
}
```

- If incident not acknowledged within stage Wait time
- Next stage channels are notified
- Process repeats until incident is acknowledged or all stages exhausted

### 9.4 Rate Limiting

Per-channel rate limits prevent notification spam:
- MaxAlerts per Window
- GroupingKey determines how alerts are grouped (soul_id, rule_id, etc.)

---

## 10. Multi-Tenancy & Workspaces

### 10.1 Workspace Isolation

- All data is scoped to a workspace_id
- Cross-workspace access is prohibited (IDOR protection)
- API requests include workspace context from auth token

### 10.2 Role-Based Access Control

- **Admin**: Full access within workspace
- **Editor**: Create/modify souls, channels, rules, journeys
- **Viewer**: Read-only access
- **ApiKey**: Scoped to specific permissions

### 10.3 Resource Quotas

Each workspace has quotas enforced:
- MaxSouls
- MaxChannels
- MaxRules
- MaxJourneys
- MaxApiCalls

---

## 11. Time-Series & Metrics

### 11.1 Metrics Storage

Time-series data stored in aggregation engine:
- Aggregated metrics per resolution (1m, 5m, 1h, 1d)
- Metrics: up, latency_p50, latency_p95, latency_p99

### 11.2 Query Parameters

| Parameter | Description |
|-----------|-------------|
| from | Start time (RFC3339) |
| to | End time (RFC3339) |
| resolution | 1m, 5m, 1h, 1d |

---

## 12. Real-Time Communication (WebSocket)

### 12.1 Connection

WebSocket endpoint: `/api/v1/ws`

Authentication via token in URL: `/api/v1/ws?token=xxx`

### 12.2 Events Pushed to Client

| Event | Description |
|-------|-------------|
| judgment.created | New judgment result |
| soul.status_changed | Soul status transition |
| incident.created | New incident |
| incident.updated | Incident state change |
| verdict.created | Alert triggered |

### 12.3 Payload Format

```json
{
  "type": "judgment.created",
  "data": {...},
  "timestamp": "2026-05-22T10:00:00Z"
}
```

---

## 13. AI Integration (MCP)

### 13.1 Model Context Protocol

AnubisWatch implements MCP for Claude integration:
- Tools: query_incidents, query_souls, query_judgments, query_metrics
- Resources: real-time monitoring data, historical analysis
- Prompts: incident summary, root cause analysis

### 13.2 MCP Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | /api/v1/mcp/query | Send MCP JSON-RPC query |
| GET | /api/v1/mcp/resources | List available resources |

---

## 14. Security Architecture

### 14.1 Authentication

- **Local**: Email/password with bcrypt hashing
- **LDAP**: Enterprise directory integration
- **OIDC**: OAuth2/OIDC provider integration
- **API Key**: For programmatic access

### 14.2 Session Management

- JWT tokens (7-day expiry)
- HttpOnly, Secure, SameSite=Strict cookies
- Workspace-scoped sessions

### 14.3 Data Protection

- TLS 1.2+ for all connections
- AES-256-GCM encryption at rest (optional)
- Input validation and sanitization
- SQL injection prevention (parameterized queries)

### 14.4 Rate Limiting

- Per-IP rate limiting on API endpoints
- Per-user quotas on API calls

---

## 15. Deployment Topologies

### 15.1 Single Node (Standalone)

```
┌─────────────────────┐
│     AnubisWatch     │
│                     │
│  +---------------+ │
│  |   RESTServer   | │
│  +---------------+ │
│                     │
│  +---------------+ │
│  | Probe Engine | │
│  +---------------+ │
│                     │
│  +---------------+ │
│  |  CobaltDB     | │
│  +---------------+ │
└─────────────────────┘
```

### 15.2 Multi-Node Cluster

```
┌─────────────┐   ┌─────────────┐   ┌─────────────┐
│  Jackal-1   │   │  Jackal-2   │   │  Jackal-3   │
│  (Leader)    │   │ (Follower)  │   │ (Follower)  │
│              │◄──│              │◄──│              │
│ +---------+ │   │ +---------+ │   │ +---------+ │
│ │ Probe    │ │   │ │ Probe    │ │   │ │ Probe    │ │
│ +---------+ │   │ +---------+ │   │ +---------+ │
│ +---------+ │   │ +---------+ │   │ +---------+ │
│ │ CobaltDB │ │   │ │ CobaltDB │ │   │ │ CobaltDB │ │
│ +---------+ │   │ +---------+ │   │ +---------+ │
└─────────────┘   └─────────────┘   └─────────────┘
        ▲
        │ Raft Consensus
        ▼
   ┌─────────────────┐
   │  Load Balancer  │
   └─────────────────┘
```

### 15.3 Kubernetes Deployment

- StatefulSets for Raft cluster
- Services for internal discovery
- Ingress for external access
- PersistentVolumes for CobaltDB

---

## 17. Detailed API Reference

This section provides comprehensive documentation for all API endpoints including full request/response payloads and underlying database operations.

### 17.1 Common Headers

| Header | Required | Description |
|--------|----------|-------------|
| `Authorization` | Yes | Bearer token from login |
| `Content-Type` | Yes (POST/PUT) | `application/json` |
| `X-Workspace-ID` | No | Workspace ID for workspace-scoped requests |
| `X-Request-ID` | No | Request correlation ID for tracing |

### 17.2 Common Response Structure

All responses follow this structure:

```json
// Success (200/201)
{ "data": { ... }, "pagination": { ... } }

// Error (4xx/5xx)
{ "error": { "code": "ERR_CODE", "message": "Human readable" } }
```

### 17.3 Pagination

List endpoints support pagination:

| Parameter | Default | Max | Description |
|-----------|---------|-----|-------------|
| `offset` | 0 | - | Records to skip |
| `limit` | 20 | 100 | Records per page |

### 17.4 Authentication API

#### POST /api/v1/auth/login

Authenticate with email and password.

**Request:**
```json
{ "email": "admin@example.com", "password": "your-password" }
```

**Response (200 OK):**
```json
{
  "user": {
    "id": "user_abc123",
    "email": "admin@example.com",
    "name": "Admin User",
    "role": "admin",
    "workspace": "ws_default",
    "created_at": "2026-01-01T00:00:00Z"
  },
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

**Response Headers:**
- `Set-Cookie: auth_token=<token>; HttpOnly; Secure; SameSite=Strict; MaxAge=604800`

**Errors:** `401` invalid credentials, `400` invalid request

**DB Operations:**
- Reads `users` by email
- Updates `users.last_login_at`
- Writes to `audit_log`

---

#### POST /api/v1/auth/logout

Invalidate current session.

**Response (200 OK):** `{ "message": "logged out" }`
**Response Headers:** `Set-Cookie: auth_token=; ...; MaxAge=0`

**DB Operations:** Invalidates token in `sessions` table

---

#### GET /api/v1/auth/me

Get current authenticated user.

**Response (200 OK):**
```json
{
  "id": "user_abc123",
  "email": "admin@example.com",
  "name": "Admin User",
  "role": "admin",
  "workspace": "ws_default"
}
```

**Errors:** `401` unauthorized

---

#### PUT /api/v1/auth/change-password

Change current user's password.

**Request:**
```json
{
  "current_password": "old-password",
  "new_password": "new-secure-password"
}
```

**Response (200 OK):** `{ "message": "password changed successfully" }`

**Password Requirements:** Min 12 chars, 1 uppercase, 1 lowercase, 1 number, 1 special char

**Errors:** `401` wrong password, `422` policy violation

**DB Operations:**
- Verifies current password hash
- Updates `users.password_hash`
- Writes to `audit_log`

---

#### POST /api/v1/auth/reset-password

Request password reset email.

**Request:** `{ "email": "admin@example.com" }`
**Response (200 OK):** `{ "message": "reset email sent if account exists" }`

**Note:** Always returns 200 to prevent email enumeration.

**DB Operations:**
- Creates `password_reset_tokens` record (1-hour expiry)
- Sends email via SMTP

---

#### POST /api/v1/auth/reset-password/confirm

Reset password using token.

**Request:**
```json
{
  "token": "reset_token_from_email",
  "new_password": "new-secure-password"
}
```

**Response (200 OK):** `{ "message": "password reset successful" }`
**Errors:** `400` token expired/used, `422` policy violation

**DB Operations:**
- Validates token in `password_reset_tokens`
- Updates `users.password_hash`
- Marks token as used

---

### 17.5 Souls (Monitors) API

Souls represent monitored targets with types: `http`, `tcp`, `dns`, `smtp`, `imap`, `icmp`, `grpc`, `websocket`, `tls`

#### GET /api/v1/souls

List souls with pagination.

**Query Parameters:** `offset`, `limit`, `status`, `type`, `tags`

**Response (200 OK):**
```json
{
  "data": [{
    "id": "soul_abc123",
    "workspace_id": "ws_default",
    "name": "Example API",
    "type": "http",
    "target": "https://api.example.com/health",
    "status": "alive",
    "last_judgment": {
      "id": "jdm_xyz789",
      "status": "alive",
      "latency_ms": 45,
      "started_at": "2026-05-22T10:00:00Z",
      "finished_at": "2026-05-22T10:00:00.045Z"
    },
    "weight": "60s",
    "timeout": "10s",
    "enabled": true,
    "tags": ["production", "api"],
    "regions": ["us-east-1"],
    "region": "us-east-1",
    "created_at": "2026-01-01T00:00:00Z",
    "updated_at": "2026-05-22T09:00:00Z"
  }],
  "pagination": { "total": 50, "offset": 0, "limit": 20, "has_more": true, "next_offset": 20 }
}
```

**Soul Status Values:** `alive`, `dead`, `degraded`, `unknown`, `embalmed`

**DB Operations:**
- Reads from `souls` table with workspace filter
- Joins latest judgment from `judgments` (subquery)

---

#### POST /api/v1/souls

Create a new soul (monitor).

**Required Role:** `souls:*`

**Request:**
```json
{
  "name": "My HTTP Monitor",
  "type": "http",
  "target": "https://api.example.com/health",
  "weight": "60s",
  "timeout": "10s",
  "enabled": true,
  "tags": ["production"],
  "regions": ["us-east-1", "eu-west-1"],
  "http": {
    "method": "GET",
    "headers": { "User-Agent": "AnubisWatch/1.0" },
    "expect_status": 200,
    "expect_body": "{\"status\":\"ok\"}",
    "insecure_ssl": false
  }
}
```

**HTTP Config Fields:**
| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `method` | string | GET | HTTP method |
| `headers` | object | {} | Custom headers |
| `body` | string | - | Request body |
| `expect_status` | int | 200 | Expected status |
| `expect_body` | string | - | Body must contain |
| `insecure_ssl` | bool | false | Skip SSL verify |

**Response (201 Created):**
```json
{
  "id": "soul_new123",
  "workspace_id": "ws_default",
  "name": "My HTTP Monitor",
  "type": "http",
  "status": "unknown",
  "weight": "60s",
  "timeout": "10s",
  ...
}
```

**Validation Rules:**
- `name`: Required, 1-255 chars
- `type`: Required, valid CheckType
- `target`: Required, valid URL
- `weight`: Min 10s, max 1h
- `timeout`: Min 1s, max 300s

**Errors:** `400` validation failed, `403` missing role, `422` invalid config

**DB Operations:**
- Generates `soul_<ulid>` ID
- Inserts into `souls` table
- Triggers probe reassignment

---

#### GET /api/v1/souls/:id

Get soul by ID.

**Response (200 OK):** Full soul object with `last_judgment`

**Errors:** `403` workspace mismatch (IDOR protection), `404` not found

**DB Operations:**
- Reads from `souls` by ID
- Joins latest judgment

---

#### PUT /api/v1/souls/:id

Update soul (partial update supported).

**Required Role:** `souls:*`

**Request:** Include only fields to update:
```json
{ "name": "Updated Name", "weight": "120s", "enabled": false }
```

**Response (200 OK):** Updated soul object

**DB Operations:**
- Reads current, merges updates
- Updates `souls.updated_at`
- Invalidates probe cache if config changed

---

#### DELETE /api/v1/souls/:id

Delete soul permanently.

**Required Role:** `souls:*`
**Response (200 OK):** `{ "message": "soul deleted" }`

**DB Operations:** CASCADE delete from `souls` (also deletes judgments)

---

#### POST /api/v1/souls/:id/check

Force immediate check.

**Required Role:** `souls:*`

**Response (200 OK):**
```json
{
  "judgment": {
    "id": "jdm_forced123",
    "soul_id": "soul_abc123",
    "status": "alive",
    "latency_ms": 42,
    "started_at": "2026-05-22T10:35:00Z",
    "finished_at": "2026-05-22T10:35:00.042Z",
    "probe_id": "probe_us_east_1"
  }
}
```

**Errors:** `409` check in progress, `503` no probe available

**DB Operations:**
- Creates judgment in `judgments` table
- Updates soul status
- Returns synchronously (max: timeout + 10s)

---

#### GET /api/v1/souls/:id/judgments

Get judgment history.

**Query Parameters:** `offset`, `limit`, `start` (RFC3339), `end` (RFC3339)

**Response (200 OK):**
```json
{
  "data": [{
    "id": "jdm_xyz789",
    "soul_id": "soul_abc123",
    "soul_name": "Example API",
    "status": "alive",
    "latency_ms": 45,
    "message": "HTTP 200 OK",
    "started_at": "2026-05-22T10:00:00Z",
    "finished_at": "2026-05-22T10:00:00.045Z",
    "probe_id": "probe_us_east_1",
    "region": "us-east-1"
  }],
  "pagination": { "total": 1440, "offset": 0, "limit": 20, "has_more": true }
}
```

**DB Operations:** Reads from `judgments` with index on `(soul_id, started_at)`

---

### 17.6 Channels API

Channels define alert delivery methods.

**Channel Types:** `slack`, `webhook`, `pagerduty`, `email`, `opsgenie`

#### GET /api/v1/channels

List channels in workspace.

**Response (200 OK):**
```json
{
  "data": [{
    "id": "chann_def456",
    "workspace_id": "ws_default",
    "name": "Production Alerts",
    "type": "slack",
    "enabled": true,
    "settings": {
      "webhook_url": "https://hooks.slack.com/...",
      "channel": "#alerts"
    },
    "filters": {
      "status": ["dead", "degraded"],
      "severity": ["critical", "warning"]
    },
    "created_at": "2026-01-01T00:00:00Z"
  }],
  "pagination": {...}
}
```

**DB Operations:** Reads from `channels` filtered by workspace_id

---

#### POST /api/v1/channels

Create alert channel.

**Required Role:** `channels:*`

**Request:**
```json
{
  "name": "Critical Alerts",
  "type": "slack",
  "enabled": true,
  "settings": {
    "webhook_url": "https://hooks.slack.com/services/xxx/yyy/zzz",
    "channel": "#critical-alerts"
  },
  "filters": {
    "status": ["dead"],
    "severity": ["critical"]
  }
}
```

**Response (201 Created):** Full channel object with ID

**Errors:** `400` invalid type, `422` invalid settings

**DB Operations:** Inserts into `channels`, writes to `audit_log`

---

#### POST /api/v1/channels/:id/test

Send test notification.

**Response (200 OK):** `{ "message": "test notification sent", "delivered": true }`
**Errors:** `500` delivery failed

**DB Operations:**
- Reads channel from `channels`
- Dispatches test (does NOT write to `alerts`)
- Logs to `audit_log`

---

### 17.7 Rules API

Rules define alert trigger conditions.

#### GET /api/v1/rules

List rules in workspace.

**Response (200 OK):**
```json
{
  "data": [{
    "id": "rule_ghi012",
    "workspace_id": "ws_default",
    "name": "API Down Alert",
    "description": "Alert when API is down for 2+ minutes",
    "enabled": true,
    "conditions": [{
      "type": "status",
      "status": "dead",
      "duration": "2m"
    }],
    "condition_logic": "all",
    "severity": "critical",
    "cooldown": "5m",
    "auto_resolve": true,
    "channels": ["chann_def456"],
    "scope": { "type": "tags", "tags": ["production"] }
  }],
  "pagination": {...}
}
```

**Condition Types:** `status`, `latency`, `status_code`, `ssl_expiry`, `dns_lookup`, `connection`
**Condition Logic:** `all` (AND), `any` (OR)

**DB Operations:** Reads from `rules` filtered by workspace_id

---

#### POST /api/v1/rules

Create alert rule.

**Required Role:** `rules:*`

**Request:**
```json
{
  "name": "High Latency Alert",
  "description": "Alert when latency > 500ms",
  "enabled": true,
  "conditions": [
    { "type": "latency", "operator": "gt", "value": 500, "window": "5m" },
    { "type": "status", "status": "degraded" }
  ],
  "condition_logic": "all",
  "severity": "warning",
  "cooldown": "3m",
  "auto_resolve": true,
  "channels": ["chann_def456"],
  "scope": { "type": "tags", "tags": ["production"] }
}
```

**Response (201 Created):** Full rule object with ID

**Validation:** `name` required, `channels` required (at least one)

**DB Operations:** Inserts into `rules`, writes to `audit_log`

---

### 17.8 Workspaces API

#### GET /api/v1/workspaces

List workspaces user has access to.

**Response (200 OK):**
```json
{
  "data": [{
    "id": "ws_default",
    "name": "Default Workspace",
    "slug": "default",
    "description": "Default workspace",
    "owner_id": "user_abc123",
    "quotas": {
      "max_souls": 100,
      "max_channels": 20,
      "max_rules": 50,
      "max_journeys": 10,
      "max_members": 25,
      "max_api_calls": 10000,
      "max_storage_mb": 1000
    },
    "features": {
      "multi_region": true,
      "custom_headers": true,
      "api_key_auth": false,
      "ldap_auth": false,
      "oidc_auth": true,
      "journey_enabled": true
    },
    "status": "active",
    "created_at": "2026-01-01T00:00:00Z"
  }],
  "pagination": {...}
}
```

**DB Operations:** Reads `workspaces` joined with user membership

---

#### POST /api/v1/workspaces

Create workspace.

**Required Role:** `members:*`

**Request:**
```json
{ "name": "Staging", "slug": "staging", "description": "Staging workspace" }
```

**Slug Rules:** Unique, 3-50 chars, lowercase alphanumeric and hyphens

**Response (201 Created):** Full workspace object with default quotas

**DB Operations:**
- Inserts into `workspaces`
- Creates admin membership for creator
- Writes to `audit_log`

---

### 17.9 Incidents API

#### GET /api/v1/incidents

List incidents. Query: `status`, `severity`

**Response (200 OK):**
```json
{
  "data": [{
    "id": "inc_789abc",
    "rule_id": "rule_ghi012",
    "rule_name": "API Down Alert",
    "soul_id": "soul_abc123",
    "soul_name": "Example API",
    "workspace_id": "ws_default",
    "status": "open",
    "severity": "critical",
    "started_at": "2026-05-22T09:00:00Z",
    "acked_at": null,
    "resolved_at": null,
    "escalation_level": 0
  }],
  "pagination": {...}
}
```

**Status Values:** `open`, `acknowledged`, `resolved`

**DB Operations:** Reads from `incidents` joined with rules/souls

---

#### POST /api/v1/incidents/:id/acknowledge

Acknowledge incident.

**Required Role:** `rules:*`
**Response (200 OK):** `{ "id": "inc_789abc", "status": "acknowledged", "acked_at": "...", "acked_by": "user_abc123" }`
**Errors:** `404` not found, `409` already acknowledged

**DB Operations:** Updates `incidents`, stops escalation timer, logs to `audit_log`

---

#### POST /api/v1/incidents/:id/resolve

Resolve incident.

**Required Role:** `rules:*`

**Request:** `{ "resolution_note": "API redeployed" }`
**Response (200 OK):** `{ "id": "inc_789abc", "status": "resolved", "resolved_at": "...", "resolved_by": "user_abc123" }`

**DB Operations:** Updates incident, closes alerts, logs to `audit_log`

---

### 17.10 Journeys API

Multi-step synthetic monitoring flows.

#### GET /api/v1/journeys

List journeys.

**Response (200 OK):**
```json
{
  "data": [{
    "id": "jour_mno456",
    "workspace_id": "ws_default",
    "name": "Checkout Flow",
    "description": "End-to-end checkout",
    "enabled": true,
    "weight": "5m",
    "timeout": "30s",
    "continue_on_failure": false,
    "variables": { "base_url": "https://shop.example.com" },
    "steps": [
      {
        "name": "Homepage",
        "type": "http",
        "target": "{{base_url}}/",
        "timeout": "10s",
        "assertions": [
          { "type": "status", "operator": "eq", "expected": "200" },
          { "type": "body", "operator": "contains", "expected": "Welcome" }
        ]
      }
    ],
    "created_at": "2026-01-01T00:00:00Z"
  }],
  "pagination": {...}
}
```

**Assertion Types:** `status`, `body`, `header`, `json`

**DB Operations:** Reads from `journeys` filtered by workspace

---

#### POST /api/v1/journeys

Create journey.

**Required Role:** `souls:*`

**Request:**
```json
{
  "name": "User Login Flow",
  "enabled": true,
  "weight": "10m",
  "timeout": "30s",
  "continue_on_failure": false,
  "variables": { "base_url": "https://app.example.com" },
  "steps": [
    {
      "name": "Login Page",
      "type": "http",
      "target": "{{base_url}}/login",
      "extract": { "csrf": { "from": "body", "regex": "value=\"([^\"]+)\"" } }
    },
    {
      "name": "Submit Login",
      "type": "http",
      "target": "{{base_url}}/login",
      "http": { "method": "POST", "body": "{\"csrf\":\"{{csrf}}\"}" }
    }
  ]
}
```

**Response (201 Created):** Full journey object with ID

**DB Operations:** Inserts into `journeys`, writes to `audit_log`

---

#### POST /api/v1/journeys/:id/run

Trigger immediate journey run.

**Required Role:** `souls:*`

**Response (200 OK):**
```json
{
  "id": "run_pqr789",
  "journey_id": "jour_mno456",
  "status": "running",
  "started_at": "2026-05-22T10:30:00Z"
}
```

**Note:** Returns immediately, execution is async.

**DB Operations:** Creates `journey_runs` record, dispatches to probe

---

#### GET /api/v1/journeys/:id/runs/:runId

Get journey run results.

**Response (200 OK):**
```json
{
  "id": "run_pqr789",
  "journey_id": "jour_mno456",
  "status": "completed",
  "started_at": "2026-05-22T10:30:00Z",
  "finished_at": "2026-05-22T10:30:25Z",
  "step_results": [
    {
      "step_name": "Homepage",
      "status": "passed",
      "latency_ms": 120,
      "started_at": "...",
      "finished_at": "..."
    },
    {
      "step_name": "Submit Login",
      "status": "passed",
      "latency_ms": 350,
      "started_at": "...",
      "finished_at": "..."
    }
  ],
  "error": null
}
```

**Run Status Values:** `pending`, `running`, `completed`, `failed`, `timeout`

---

### 17.11 Stats API

#### GET /api/v1/stats

Get workspace statistics for last 24 hours.

**Response (200 OK):**
```json
{
  "total_souls": 50,
  "alive_souls": 45,
  "dead_souls": 3,
  "degraded_souls": 2,
  "total_checks": 72000,
  "failed_checks": 150,
  "avg_latency_ms": 85,
  "p95_latency_ms": 250,
  "uptime_percentage": 99.79,
  "checks_by_region": {
    "us-east-1": 30000,
    "eu-west-1": 24000,
    "ap-southeast-1": 18000
  }
}
```

**DB Operations:** Aggregates from `judgments` table for time range

---

#### GET /api/v1/stats/overview

Get quick overview stats.

**Response (200 OK):**
```json
{
  "healthy": 45,
  "degraded": 2,
  "dead": 3,
  "today_checks": 1200,
  "today_failures": 5,
  "avg_latency_ms": 85
}
```

---

### 17.12 Cluster API

#### GET /api/v1/cluster/status

Get Raft cluster status.

**Response (200 OK):**
```json
{
  "is_clustered": true,
  "node_id": "node_1",
  "state": "leader",
  "leader": "node_1",
  "term": 42,
  "peer_count": 3,
  "commit_index": 15234
}
```

**DB Operations:** Reads from Raft state

---

#### GET /api/v1/cluster/peers

List cluster peers.

**Response (200 OK):**
```json
{
  "peers": [
    { "id": "node_1", "address": "10.0.0.1:7946", "state": "voter", "last_contact": null },
    { "id": "node_2", "address": "10.0.0.2:7946", "state": "voter", "last_contact": "2026-05-22T10:29:55Z" },
    { "id": "node_3", "address": "10.0.0.3:7946", "state": "voter", "last_contact": null }
  ]
}
```

---

### 17.13 Maintenance Windows API

#### GET /api/v1/maintenance

List maintenance windows.

**Response (200 OK):**
```json
{
  "data": [{
    "id": "mw_abc123",
    "name": "Planned Deployment",
    "description": "Database migration window",
    "workspace_id": "ws_default",
    "start_time": "2026-05-25T02:00:00Z",
    "end_time": "2026-05-25T04:00:00Z",
    "timezone": "UTC",
    "enabled": true,
    "affected_souls": ["soul_1", "soul_2"],
    "created_at": "2026-05-20T00:00:00Z"
  }],
  "pagination": {...}
}
```

---

#### POST /api/v1/maintenance

Create maintenance window.

**Required Role:** `settings:write`

**Request:**
```json
{
  "name": "Planned Deployment",
  "description": "Database migration",
  "start_time": "2026-05-25T02:00:00Z",
  "end_time": "2026-05-25T04:00:00Z",
  "timezone": "UTC",
  "enabled": true,
  "affected_souls": ["soul_abc123", "soul_def456"]
}
```

**Response (201 Created):** Full maintenance window object

**Effect:** Souls in `affected_souls` receive `embalmed` status during window

**DB Operations:** Inserts into `maintenance_windows`

---

### 17.14 Dashboards API

#### GET /api/v1/dashboards

List custom dashboards.

**Response (200 OK):**
```json
{
  "data": [{
    "id": "dash_xyz789",
    "workspace_id": "ws_default",
    "name": "Production Overview",
    "description": "Main production monitoring dashboard",
    "widgets": [
      {
        "type": "chart",
        "title": "Uptime Trend",
        "query": "soul_uptime",
        "time_range": "24h"
      },
      {
        "type": "stat",
        "title": "Total Souls",
        "query": "count(souls)"
      }
    ],
    "created_at": "2026-01-01T00:00:00Z"
  }],
  "pagination": {...}
}
```

---

### 17.15 Status Pages API

#### GET /api/v1/status-pages

List status pages.

**Response (200 OK):**
```json
{
  "data": [{
    "id": "spg_123",
    "workspace_id": "ws_default",
    "name": "System Status",
    "slug": "status",
    "title": "System Status Page",
    "description": "Current system status",
    "components": [
      { "name": "API", "status": "operational" },
      { "name": "Dashboard", "status": "operational" }
    ],
    "created_at": "2026-01-01T00:00:00Z"
  }],
  "pagination": {...}
}
```

**Component Status Values:** `operational`, `degraded`, `partial_outage`, `major_outage`, `maintenance`

---

### 17.16 Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `ERR_UNAUTHORIZED` | 401 | Invalid or missing auth token |
| `ERR_FORBIDDEN` | 403 | Insufficient permissions |
| `ERR_NOT_FOUND` | 404 | Resource doesn't exist |
| `ERR_VALIDATION_FAILED` | 400 | Request validation failed |
| `ERR_CONFLICT` | 409 | Resource conflict (e.g., duplicate slug) |
| `ERR_RATE_LIMITED` | 429 | Too many requests |
| `ERR_INTERNAL` | 500 | Internal server error |
| `CHANNEL_DELIVERY_FAILED` | 500 | Alert channel delivery failed |
| `JOURNEY_TIMEOUT` | 504 | Journey execution timed out |

---

### 17.17 WebSocket / SSE Real-time Events

#### WebSocket (GET /ws)

Connect for real-time updates.

**Authentication:** Pass token as query param: `/ws?token=<jwt>`

**Server Messages:**
```json
// Soul status change
{ "type": "soul_update", "data": { "id": "soul_abc123", "status": "dead", "latency_ms": 5000 } }

// New judgment
{ "type": "judgment", "data": { "id": "jdm_xyz", "soul_id": "soul_abc123", "status": "dead" } }

// Incident created
{ "type": "incident", "data": { "id": "inc_789", "status": "open", "severity": "critical" } }

// Incident resolved
{ "type": "incident_resolved", "data": { "id": "inc_789" } }
```

**Client Messages:**
```json
// Subscribe to workspace updates
{ "action": "subscribe", "workspace": "ws_default" }

// Unsubscribe
{ "action": "unsubscribe", "workspace": "ws_default" }

// Ping/pong for keepalive
{ "action": "ping" }
```

---

#### SSE (GET /api/v1/events)

Server-Sent Events for real-time updates (fallback for restricted environments).

**Headers:** `Accept: text/event-stream`, `Authorization: Bearer <token>`

**Event Types:** Same as WebSocket

**Format:**
```
event: soul_update
data: {"id":"soul_abc123","status":"alive"}

event: judgment
data: {"id":"jdm_xyz","soul_id":"soul_abc123","status":"alive"}
```

## 18. Database Schema

Complete schema documentation for CobaltDB storage engine. Tables include: workspaces, users, sessions, souls, judgments, channels, rules, incidents, journeys, journey_runs, maintenance_windows, dashboards, status_pages, audit_log, password_reset_tokens. Each table has defined columns with types, constraints, indexes, and descriptions.

## 19. Configuration Reference

Configuration options for config.yaml including server, tls, auth, database, cluster, probes, alerts, rate_limit, observability, and maintenance settings. Environment variables and CLI flags are also documented.

## 20. Deployment Guide

Deployment instructions for Docker Compose, Docker single-container, Kubernetes (with StatefulSet, ConfigMap, Service, Ingress), and bare metal with systemd.

## 21. Troubleshooting Guide

Common issues and solutions for startup, authentication, monitoring, cluster, and performance problems. Includes health check commands.

## 22. Security Guide

Authentication flows, RBAC permissions, rate limiting, security headers, and security checklist.

## 23. Monitoring & Observability

Prometheus metrics endpoint documentation and available metrics with types, labels, and descriptions. Grafana dashboard configuration and alerting rules are also covered.

## 18. Database Schema

Complete schema for CobaltDB storage engine.

### 18.1 Entity Relationship Diagram

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  workspaces │────<│    users    │     │   souls     │
├─────────────┤     ├─────────────┤     ├─────────────┤
│ id          │     │ id          │     │ id          │
│ name        │     │ email       │     │ workspace_id│────┐
│ slug        │     │ password    │     │ name        │    │
│ owner_id    │─────┤ workspace_id│─────┤ type        │    │
│ quotas      │     │ role        │     │ target      │    │
│ features    │     │ created_at  │     │ weight      │    │
│ status      │     └─────────────┘     │ timeout     │    │
└─────────────┘                         │ status      │    │
      │                                 └─────────────┘    │
      │                                       │            │
      ▼                                       │            │
┌─────────────┐     ┌─────────────┐           │            ▼
│   channels  │     │    rules    │           │     ┌─────────────┐
├─────────────┤     ├─────────────┤           │     │ judgments  │
│ id          │     │ id          │           │     ├─────────────┤
│ workspace_id│─────┤ workspace_id│───────────┘     │ id          │
│ name        │     │ name        │                 │ soul_id     │──┐
│ type        │     │ conditions  │                 │ status      │  │
│ settings    │     │ channels    │                 │ latency_ms  │  │
│ filters     │     │ severity    │                 │ message     │  │
│ enabled     │     │ cooldown    │                 │ started_at  │  │
└─────────────┘     └─────────────┘                 └─────────────┘  │
      │                                           │                │
      ▼                                           ▼                │
┌─────────────┐     ┌─────────────┐     ┌─────────────────┐        │
│  incidents  │     │ journeys    │     │ maintenance_win │        │
├─────────────┤     ├─────────────┤     ├─────────────────┤        │
│ id          │     │ id          │     │ id              │        │
│ rule_id     │─────┤ workspace_id│─────┤ workspace_id    │        │
│ soul_id     │─────┤ name        │     │ start_time      │        │
│ status      │     │ steps       │     │ end_time        │        │
│ severity    │     │ variables   │     │ affected_souls  │        │
│ started_at  │     └─────────────┘     └─────────────────┘        │
│ acked_at    │           │                                        │
│ resolved_at │           ▼                                        │
└─────────────┘     ┌─────────────┐                                │
                    │journey_runs │                                │
                    ├─────────────┤                                │
                    │ id          │                                │
                    │ journey_id  │──────┘                        │
                    │ status      │                               │
                    │ started_at  │                               │
                    └─────────────┘                               │
                                                          ┌──────┴──────┐
                                                          │ audit_log   │
                                                          ├─────────────┤
                                                          │ id          │
                                                          │ workspace_id│
                                                          │ action      │
                                                          │ actor_id    │
                                                          │ details     │
                                                          │ created_at  │
                                                          └─────────────┘
```

### 18.2 Tables

#### workspaces
Primary workspace/tenant table.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | `ws_<ulid>` |
| `name` | TEXT | NOT NULL | Display name |
| `slug` | TEXT | UNIQUE NOT NULL | URL-safe identifier |
| `owner_id` | TEXT | NOT NULL | Owner user ID |
| `quotas` | TEXT | | JSON: max_souls, max_channels |
| `features` | TEXT | | JSON: enabled features |
| `status` | TEXT | DEFAULT 'active' | active, suspended, deleted |
| `created_at` | TIMESTAMP | NOT NULL | Creation time |
| `updated_at` | TIMESTAMP | NOT NULL | Last update |
| `deleted_at` | TIMESTAMP | | Soft delete time |

**Indexes:** `idx_workspaces_slug`, `idx_workspaces_status`

#### users
User accounts.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | `user_<ulid>` |
| `email` | TEXT | UNIQUE NOT NULL | Email address |
| `password_hash` | TEXT | | bcrypt hash |
| `name` | TEXT | NOT NULL | Display name |
| `role` | TEXT | NOT NULL DEFAULT 'viewer' | admin, editor, viewer |
| `workspace_id` | TEXT | REFERENCES workspaces(id) | Default workspace |
| `last_login_at` | TIMESTAMP | | Last successful login |
| `created_at` | TIMESTAMP | NOT NULL | Creation time |
| `updated_at` | TIMESTAMP | NOT NULL | Last update |
| `deleted_at` | TIMESTAMP | | Soft delete |

**Indexes:** `idx_users_email`, `idx_users_workspace`

#### sessions
Active authentication sessions.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | `sess_<ulid>` |
| `user_id` | TEXT | NOT NULL REFERENCES users(id) | Session owner |
| `token_hash` | TEXT | NOT NULL | SHA256 of JWT |
| `expires_at` | TIMESTAMP | NOT NULL | Expiration time |
| `created_at` | TIMESTAMP | NOT NULL | Creation time |
| `ip_address` | TEXT | | Client IP |
| `user_agent` | TEXT | | Client user agent |

**Indexes:** `idx_sessions_user`, `idx_sessions_token`, `idx_sessions_expires`

#### souls
Monitored targets (hearts being weighed).

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | `soul_<ulid>` |
| `workspace_id` | TEXT | NOT NULL REFERENCES workspaces(id) | Workspace |
| `name` | TEXT | NOT NULL | Display name |
| `type` | TEXT | NOT NULL | http, tcp, dns, etc. |
| `target` | TEXT | NOT NULL | URL or host:port |
| `weight` | TEXT | NOT NULL | Check interval duration |
| `timeout` | TEXT | NOT NULL | Timeout duration |
| `enabled` | INTEGER | DEFAULT 1 | 1=active, 0=paused |
| `status` | TEXT | DEFAULT 'unknown' | alive, dead, degraded, unknown, embalmed |
| `region` | TEXT | | Assigned probe region |
| `last_check_at` | TIMESTAMP | | Last judgment time |
| `config` | TEXT | | JSON: HTTP, TCP, DNS, TLS configs |
| `tags` | TEXT | | JSON array of tags |
| `regions` | TEXT | | JSON array: allowed regions |
| `created_at` | TIMESTAMP | NOT NULL | Creation time |
| `updated_at` | TIMESTAMP | NOT NULL | Last update |

**Indexes:** `idx_souls_workspace`, `idx_souls_status`, `idx_souls_region`, `idx_souls_enabled`

#### judgments
Results of soul checks (the weighing).

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | `jdm_<ulid>` |
| `soul_id` | TEXT | NOT NULL REFERENCES souls(id) | Checked soul |
| `status` | TEXT | NOT NULL | alive, dead, degraded |
| `latency_ms` | INTEGER | | Response time |
| `message` | TEXT | | Human-readable result |
| `started_at` | TIMESTAMP | NOT NULL | Check start time |
| `finished_at` | TIMESTAMP | | Check end time |
| `probe_id` | TEXT | | Probe that performed check |
| `region` | TEXT | | Probe region |
| `checks` | TEXT | | JSON: ssl, dns, etc. sub-check results |
| `error` | TEXT | | Error message if failed |

**Indexes:** `idx_judgments_soul`, `idx_judgments_started`, `idx_judgments_soul_started` ON `(soul_id, started_at DESC)`, `idx_judgments_status`

**TTL Policy:** Auto-delete judgments older than 30 days (configurable)

#### channels
Alert notification channels.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | `chann_<ulid>` |
| `workspace_id` | TEXT | NOT NULL REFERENCES workspaces(id) | Workspace |
| `name` | TEXT | NOT NULL | Display name |
| `type` | TEXT | NOT NULL | slack, webhook, pagerduty, email, opsgenie |
| `enabled` | INTEGER | DEFAULT 1 | 1=enabled, 0=disabled |
| `settings` | TEXT | NOT NULL | JSON: webhook_url, api_key, etc. |
| `filters` | TEXT | | JSON: status, severity, tag filters |
| `created_at` | TIMESTAMP | NOT NULL | Creation time |
| `updated_at` | TIMESTAMP | NOT NULL | Last update |

**Indexes:** `idx_channels_workspace`

#### rules
Alert trigger conditions.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | `rule_<ulid>` |
| `workspace_id` | TEXT | NOT NULL REFERENCES workspaces(id) | Workspace |
| `name` | TEXT | NOT NULL | Display name |
| `description` | TEXT | | Rule description |
| `enabled` | INTEGER | DEFAULT 1 | 1=enabled, 0=disabled |
| `conditions` | TEXT | NOT NULL | JSON: condition array |
| `condition_logic` | TEXT | DEFAULT 'all' | all, any |
| `severity` | TEXT | NOT NULL | critical, warning, info |
| `cooldown` | TEXT | NOT NULL | Minimum time between alerts |
| `auto_resolve` | INTEGER | DEFAULT 1 | Auto-resolve when condition clears |
| `channels` | TEXT | NOT NULL | JSON: channel ID array |
| `scope` | TEXT | | JSON: tags, soul_types, soul_ids filter |
| `escalation` | TEXT | | JSON: escalation stages |
| `created_at` | TIMESTAMP | NOT NULL | Creation time |
| `updated_at` | TIMESTAMP | NOT NULL | Last update |

**Indexes:** `idx_rules_workspace`, `idx_rules_enabled`

#### incidents
Alert incidents (soul violation events).

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | `inc_<ulid>` |
| `rule_id` | TEXT | NOT NULL REFERENCES rules(id) | Triggering rule |
| `soul_id` | TEXT | NOT NULL REFERENCES souls(id) | Affected soul |
| `workspace_id` | TEXT | NOT NULL REFERENCES workspaces(id) | Workspace |
| `status` | TEXT | DEFAULT 'open' | open, acknowledged, resolved |
| `severity` | TEXT | NOT NULL | From rule |
| `started_at` | TIMESTAMP | NOT NULL | Incident start |
| `acked_at` | TIMESTAMP | | Acknowledged time |
| `acked_by` | TEXT | | User who acked |
| `resolved_at` | TIMESTAMP | | Resolved time |
| `resolved_by` | TEXT | | User who resolved |
| `notes` | TEXT | | JSON: incident notes |
| `escalation_level` | INTEGER | DEFAULT 0 | Current escalation stage |
| `last_escalated_at` | TIMESTAMP | | Last escalation time |

**Indexes:** `idx_incidents_workspace`, `idx_incidents_status`, `idx_incidents_rule`, `idx_incidents_soul`, `idx_incidents_started` ON `started_at DESC`

#### journeys
Multi-step synthetic monitoring flows.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | `jour_<ulid>` |
| `workspace_id` | TEXT | NOT NULL REFERENCES workspaces(id) | Workspace |
| `name` | TEXT | NOT NULL | Display name |
| `description` | TEXT | | Journey description |
| `enabled` | INTEGER | DEFAULT 1 | 1=enabled, 0=disabled |
| `weight` | TEXT | NOT NULL | Run interval |
| `timeout` | TEXT | NOT NULL | Max execution time |
| `continue_on_failure` | INTEGER | DEFAULT 0 | Continue steps on failure |
| `variables` | TEXT | | JSON: variable definitions |
| `steps` | TEXT | NOT NULL | JSON: step array |
| `created_at` | TIMESTAMP | NOT NULL | Creation time |
| `updated_at` | TIMESTAMP | NOT NULL | Last update |

**Indexes:** `idx_journeys_workspace`

#### journey_runs
Execution results for journeys.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | `run_<ulid>` |
| `journey_id` | TEXT | NOT NULL REFERENCES journeys(id) | Journey |
| `workspace_id` | TEXT | NOT NULL | Workspace |
| `jackal_id` | TEXT | | Probe that executed |
| `region` | TEXT | | Probe region |
| `status` | TEXT | NOT NULL | pending, running, completed, failed, timeout |
| `started_at` | TIMESTAMP | NOT NULL | Run start time |
| `finished_at` | TIMESTAMP | | Run end time |
| `step_results` | TEXT | | JSON: per-step results |
| `error` | TEXT | | Error message if failed |

**Indexes:** `idx_runs_journey`, `idx_runs_started` ON `started_at DESC`

#### maintenance_windows
Scheduled maintenance periods.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | `mw_<ulid>` |
| `workspace_id` | TEXT | NOT NULL REFERENCES workspaces(id) | Workspace |
| `name` | TEXT | NOT NULL | Display name |
| `description` | TEXT | | Description |
| `start_time` | TIMESTAMP | NOT NULL | Window start |
| `end_time` | TIMESTAMP | NOT NULL | Window end |
| `timezone` | TEXT | DEFAULT 'UTC' | IANA timezone |
| `enabled` | INTEGER | DEFAULT 1 | 1=active, 0=disabled |
| `affected_souls` | TEXT | | JSON: soul ID array |
| `created_at` | TIMESTAMP | NOT NULL | Creation time |

**Indexes:** `idx_maint_workspace`, `idx_maint_time` ON `(start_time, end_time)`

#### dashboards
Custom dashboard configurations.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | `dash_<ulid>` |
| `workspace_id` | TEXT | NOT NULL REFERENCES workspaces(id) | Workspace |
| `name` | TEXT | NOT NULL | Display name |
| `description` | TEXT | | Dashboard description |
| `widgets` | TEXT | NOT NULL | JSON: widget array |
| `created_at` | TIMESTAMP | NOT NULL | Creation time |
| `updated_at` | TIMESTAMP | NOT NULL | Last update |

**Indexes:** `idx_dashboards_workspace`

#### status_pages
Public status page configurations.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | `spg_<ulid>` |
| `workspace_id` | TEXT | NOT NULL REFERENCES workspaces(id) | Workspace |
| `name` | TEXT | NOT NULL | Display name |
| `slug` | TEXT | UNIQUE NOT NULL | URL slug |
| `title` | TEXT | NOT NULL | Page title |
| `description` | TEXT | | Page description |
| `logo_url` | TEXT | | Custom logo |
| `components` | TEXT | NOT NULL | JSON: component array |
| `created_at` | TIMESTAMP | NOT NULL | Creation time |
| `updated_at` | TIMESTAMP | NOT NULL | Last update |

**Indexes:** `idx_statuspages_slug`

#### audit_log
Immutable audit trail for compliance.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | `audit_<ulid>` |
| `workspace_id` | TEXT | NOT NULL | Workspace |
| `actor_id` | TEXT | | User who performed action |
| `actor_type` | TEXT | DEFAULT 'user' | user, system, api_key |
| `action` | TEXT | NOT NULL | login, create, update, delete |
| `resource_type` | TEXT | | e.g., soul, channel, rule |
| `resource_id` | TEXT | | ID of affected resource |
| `details` | TEXT | | JSON: action details |
| `ip_address` | TEXT | | Client IP |
| `user_agent` | TEXT | | Client user agent |
| `created_at` | TIMESTAMP | NOT NULL | Action time |

**Indexes:** `idx_audit_workspace`, `idx_audit_actor`, `idx_audit_resource` ON `(resource_type, resource_id)`, `idx_audit_created` ON `created_at DESC`

**Retention:** Immutable, no auto-deletion (compliance requirement)

#### password_reset_tokens
Password reset token storage.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | TEXT | PRIMARY KEY | `prst_<ulid>` |
| `user_id` | TEXT | NOT NULL REFERENCES users(id) | Target user |
| `token_hash` | TEXT | NOT NULL | SHA256 of token |
| `expires_at` | TIMESTAMP | NOT NULL | Token expiration |
| `used_at` | TIMESTAMP | | Token usage time |
| `created_at` | TIMESTAMP | NOT NULL | Creation time |

**Indexes:** `idx_prst_token`, `idx_prst_user`, `idx_prst_expires`

### 18.3 ID Naming Conventions

| Entity | ID Prefix | Example |
|--------|-----------|---------|
| Workspace | `ws_` | `ws_01ARZ3NDEKTSV4RRFFQ69G5FAV` |
| User | `user_` | `user_01ARZ3NDEKTSV4RRFFQ69G5FBG` |
| Soul | `soul_` | `soul_01ARZ3NDEKTSV4RRFFQ69G5FCH` |
| Judgment | `jdm_` | `jdm_01ARZ3NDEKTSV4RRFFQ69G5FDH` |
| Channel | `chann_` | `chann_01ARZ3NDEKTSV4RRFFQ69G5FEH` |
| Rule | `rule_` | `rule_01ARZ3NDEKTSV4RRFFQ69G5FFH` |
| Incident | `inc_` | `inc_01ARZ3NDEKTSV4RRFFQ69G5FGH` |
| Journey | `jour_` | `jour_01ARZ3NDEKTSV4RRFFQ69G5FHH` |
| Journey Run | `run_` | `run_01ARZ3NDEKTSV4RRFFQ69G5FIH` |

## 19. Configuration Reference

### 19.1 config.yaml

```yaml
server:
  host: "0.0.0.0"
  port: 8080
  read_timeout: 30s
  write_timeout: 30s
  idle_timeout: 120s
  shutdown_timeout: 30s

tls:
  enabled: false
  cert_file: "/path/to/cert.pem"
  key_file: "/path/to/key.pem"
  min_version: "1.2"

auth:
  jwt_secret: "${JWT_SECRET}"
  jwt_expiry: 168h
  cookie_secure: true
  cookie_same_site: "strict"
  password_min_length: 12
  password_require_uppercase: true
  password_require_lowercase: true
  password_require_number: true
  password_require_special: true
  max_sessions_per_user: 5

database:
  path: "./data/anubiswatch.db"
  wal_mode: true
  sync_mode: "NORMAL"
  cache_size: 10000
  auto_vacuum: "INCREMENTAL"
  max_open_conns: 25
  max_idle_conns: 5
  conn_max_lifetime: 1h
  backup_interval: 1h
  backup_retention: 7d
  backup_path: "./backups"

cluster:
  enabled: false
  node_id: "${HOSTNAME}"
  bind_addr: "0.0.0.0:7946"
  advertise_addr: "10.0.0.1:7946"
  seeds: []
  heartbeat_timeout: 1s
  election_timeout: 5s
  snapshot_interval: 5m
  snapshot_threshold: 8192

probes:
  region: "us-east-1"
  num_workers: 10
  queue_size: 1000
  dispatch_timeout: 30s
  regions:
    us-east-1:
      enabled: true
      min_workers: 5
      max_workers: 20
    eu-west-1:
      enabled: true
      min_workers: 3
      max_workers: 15

alerts:
  incident_cooldown: 30s
  max_incidents_per_rule: 100
  escalation_interval: 5m
  max_escalation_level: 3
  delivery_timeout: 10s
  max_retries: 3

rate_limit:
  enabled: true
  requests_per_second: 100
  burst_size: 200
  workspace_default_rps: 50
  workspace_max_rps: 500

observability:
  otel_enabled: false
  otel_endpoint: "localhost:4317"
  otel_service_name: "anubiswatch"
  metrics_enabled: true
  metrics_path: "/metrics"
  log_level: "info"
  log_format: "json"

maintenance:
  judgment_retention_days: 30
  cleanup_interval: 1h
```

### 19.2 Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `JWT_SECRET` | Yes | - | Secret for JWT signing (min 32 chars) |
| `DATABASE_PATH` | No | `./data/anubiswatch.db` | Database file path |
| `CLUSTER_SEEDS` | No | - | Comma-separated seed addresses |
| `OTEL_ENDPOINT` | No | - | OpenTelemetry collector endpoint |
| `SMTP_HOST` | No | - | SMTP server for email alerts |
| `SMTP_PORT` | No | 587 | SMTP port |
| `SMTP_USER` | No | - | SMTP username |
| `SMTP_PASS` | No | - | SMTP password |
| `SMTP_FROM` | No | `noreply@anubiswatch` | From address |
| `OIDC_ISSUER` | No | - | OIDC provider issuer URL |
| `OIDC_CLIENT_ID` | No | - | OIDC client ID |
| `OIDC_CLIENT_SECRET` | No | - | OIDC client secret |

### 19.3 CLI Flags

```bash
anubiswatch [flags]

General:
  --config string          Config file path (default: ./config.yaml)
  --data-dir string         Data directory (default: ./data)
  --verbose                Enable verbose logging

Server:
  --host string            Listen host (default: 0.0.0.0)
  --port int               Listen port (default: 8080)
  --tls-cert string        TLS certificate file
  --tls-key string         TLS key file

Cluster:
  --cluster                Enable clustering
  --node-id string         Node ID (default: hostname)
  --bind string            Cluster bind address (default: 0.0.0.0:7946)
  --advertise string       Advertised cluster address
  --seeds strings          Seed node addresses

Database:
  --db-path string         Database path (default: ./data/anubiswatch.db)
  --backup-interval duration  Backup interval (default: 1h)
  --retention-days int     Judgment retention days (default: 30)

Observability:
  --metrics                Enable Prometheus metrics
  --otel-addr string       OpenTelemetry collector address
  --log-level string       Log level (default: info)
```

## 20. Deployment Guide

### 20.1 Docker Compose

```yaml
version: '3.8'

services:
  anubiswatch:
    image: anubiswatch/anubiswatch:latest
    container_name: anubiswatch
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      - JWT_SECRET=${JWT_SECRET}
    volumes:
      - ./data:/data
      - ./config.yaml:/config.yaml:ro
    command: --config /config.yaml
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3

  prometheus:
    image: prom/prometheus:latest
    ports:
      - "9090:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
```

**Quick start:**
```bash
mkdir -p data
export JWT_SECRET=$(openssl rand -base64 32)
cat > config.yaml << 'EOF'
server:
  port: 8080
auth:
  jwt_secret: "${JWT_SECRET}"
database:
  path: /data/anubiswatch.db
EOF
docker-compose up -d
```

### 20.2 Docker Single-Container

```bash
docker pull anubiswatch/anubiswatch:latest

mkdir -p /opt/anubiswatch/{data,config,backups}

docker run -d \
  --name anubiswatch \
  --restart unless-stopped \
  -p 8080:8080 \
  -v /opt/anubiswatch/data:/data \
  -v /opt/anubiswatch/config:/config:ro \
  -v /opt/anubiswatch/backups:/backups \
  -e JWT_SECRET="${JWT_SECRET}" \
  anubiswatch/anubiswatch:latest \
  --config /config/config.yaml
```

### 20.3 Kubernetes

```yaml
# configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: anubiswatch-config
  namespace: monitoring
data:
  config.yaml: |
    server:
      port: 8080
    auth:
      jwt_secret: "${JWT_SECRET}"
    database:
      path: /data/anubiswatch.db
```

```yaml
# statefulset.yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: anubiswatch
  namespace: monitoring
spec:
  serviceName: anubiswatch
  replicas: 3
  selector:
    matchLabels:
      app: anubiswatch
  template:
    metadata:
      labels:
        app: anubiswatch
    spec:
      containers:
        - name: anubiswatch
          image: anubiswatch/anubiswatch:latest
          ports:
            - containerPort: 8080
              name: http
            - containerPort: 7946
              name: raft
          env:
            - name: JWT_SECRET
              valueFrom:
                secretKeyRef:
                  name: anubiswatch-secrets
                  key: jwt-secret
          args:
            - --config
            - /config/config.yaml
          volumeMounts:
            - name: config
              mountPath: /config
              readOnly: true
            - name: data
              mountPath: /data
          resources:
            requests:
              cpu: 100m
              memory: 256Mi
            limits:
              cpu: 500m
              memory: 1Gi
          livenessProbe:
            httpGet:
              path: /health
              port: 8080
          readinessProbe:
            httpGet:
              path: /ready
              port: 8080
      volumes:
        - name: config
          configMap:
            name: anubiswatch-config
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: 10Gi
```

**Deploy:**
```bash
kubectl create namespace monitoring
kubectl apply -f configmap.yaml
kubectl apply -f secret.yaml
kubectl apply -f statefulset.yaml
kubectl -n monitoring get pods
```

### 20.4 Bare Metal / systemd

```bash
# Install
curl -L https://github.com/AnubisWatch/anubiswatch/releases/latest/anubiswatch-linux-amd64.tar.gz | tar xz
sudo mv anubiswatch /usr/local/bin/

# User & directories
sudo useradd -r -s /bin/false anubiswatch
sudo mkdir -p /var/lib/anubiswatch/{data,backups}
sudo mkdir -p /etc/anubiswatch
sudo chown -R anubiswatch:anubiswatch /var/lib/anubiswatch

# systemd unit
sudo cat > /etc/systemd/system/anubiswatch.service << 'EOF'
[Unit]
Description=AnubisWatch Monitoring
After=network.target

[Service]
Type=simple
User=anubiswatch
Group=anubiswatch
ExecStart=/usr/local/bin/anubiswatch --config /etc/anubiswatch/config.yaml
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/anubiswatch

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl enable anubiswatch
sudo systemctl start anubiswatch
```

## 21. Troubleshooting Guide

### 21.1 Startup Issues

**Port already in use:**
```bash
lsof -i :8080
# or change port in config
```

**Database locked:**
```
Error: database is locked
Solutions:
1. Check for other running instances: pgrep anubiswatch
2. Wait for background checkpoint to complete
3. Delete stale .wal file: rm /data/anubiswatch.db-wal
```

**JWT_SECRET not set:**
```
Error: JWT secret is required
Solution: export JWT_SECRET=$(openssl rand -base64 32)
```

### 21.2 Authentication Issues

| Issue | Cause | Solution |
|-------|-------|----------|
| Token expired | JWT expired (7 days default) | Re-login |
| Session not persisting | Cookie blocked | Check browser privacy settings |
| OIDC failing | Misconfigured OIDC | Verify callback URL |

### 21.3 Monitoring Issues

**Souls not being checked:**
1. Check soul is enabled: `GET /api/v1/souls/:id`
2. Verify probe is running: `GET /ready`
3. Check soul has valid region

**High latency alerts:** Check probe health, network latency, adjust thresholds

### 21.4 Cluster Issues

| Problem | Solution |
|---------|----------|
| Node can't join | Verify port 7946 open, check advertise address |
| Leader election failing | 3+ nodes required, check network connectivity |
| Split brain | Configure proper network isolation |

### 21.5 Performance Issues

**High memory:** Tune database cache or enable auto-vacuum

**Slow API:** Use pagination, enable query logging, check indexes

### 21.6 Health Check Commands

```bash
# Check if running
pgrep anubiswatch

# View logs
journalctl -u anubiswatch -f

# Check health
curl http://localhost:8080/health
curl http://localhost:8080/ready

# Get metrics
curl http://localhost:8080/metrics
```

## 22. Security Guide

### 22.1 Authentication Flows

```
+---------+      +---------+      +---------+
|  User   |      |   API   |      |   DB    |
+---------+      +---------+      +---------+
     |                |                |
     | 1. Login (email, password)      |
     |-------------->|                |
     |                | 2. Verify hash|
     |                |-------------->|
     |                |<--------------|
     |                | 3. JWT token  |
     |<--------------|                |
     |                |               |
     | 4. API request (Bearer token)   |
     |-------------->|                |
     |                | 5. Validate JWT|
     |                |-------------->| (check blacklist)
     |                |<--------------|
     | 6. Response    |               |
     |<--------------|               |
```

### 22.2 RBAC Permissions

| Role | Permissions |
|------|-------------|
| `admin` | Full access to all resources |
| `editor` | Create/update souls, channels, rules; view all |
| `viewer` | Read-only access to all resources |

**Permission Scopes:** `souls:*`, `souls:read`, `channels:*`, `rules:*`, `settings:write`, `members:*`

### 22.3 Rate Limiting

| Tier | Limit | Burst |
|------|-------|-------|
| Default | 100 req/s | 200 |
| Authenticated | 200 req/s | 400 |
| Workspace | 50-500 req/s | configurable |

### 22.4 Security Headers

Added to all responses:
```
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
X-XSS-Protection: 1; mode=block
Strict-Transport-Security: max-age=31536000; includeSubDomains
Content-Security-Policy: default-src 'self'
```

### 22.5 Security Checklist

- [ ] JWT_SECRET minimum 32 characters
- [ ] TLS enabled in production
- [ ] Cookie Secure flag enabled
- [ ] Rate limiting enabled
- [ ] Audit logging enabled
- [ ] Regular backups configured
- [ ] Password policy enforced
- [ ] OIDC/LDAP in production

## 23. Monitoring & Observability

### 23.1 Prometheus Metrics

**Metrics endpoint:** `GET /metrics`

**Available metrics:**

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `anubiswatch_souls_total` | Gauge | workspace, status | Total number of souls |
| `anubiswatch_judgments_total` | Counter | workspace, status, region | Total judgments |
| `anubiswatch_latency_seconds` | Histogram | workspace, soul_id, type | Check latency distribution |
| `anubiswatch_incidents_total` | Counter | workspace, severity | Total incidents |
| `anubiswatch_alerts_sent_total` | Counter | workspace, channel_type | Total alerts sent |
| `anubiswatch_http_requests_total` | Counter | method, path, status | HTTP request count |
| `anubiswatch_http_request_duration_seconds` | Histogram | method, path | HTTP latency |
| `anubiswatch_db_queries_total` | Counter | operation | Database queries |
| `anubiswatch_db_query_duration_seconds` | Histogram | operation | Database latency |
| `anubiswatch_cluster_members` | Gauge | state | Raft cluster members |
| `anubiswatch_raft_commit_index` | Gauge | | Raft commit index |
| `anubiswatch_queue_size` | Gauge | region | Probe queue size |
| `anubiswatch_active_checks` | Gauge | region | Currently running checks |

### 23.2 Grafana Dashboard

**Dashboard JSON example:**

```json
{
  "dashboard": {
    "title": "AnubisWatch Overview",
    "panels": [
      {
        "title": "Souls Status",
        "type": "piechart",
        "targets": [
          {
            "expr": "sum(anubiswatch_souls_total) by (status)"
          }
        ]
      },
      {
        "title": "Latency p95",
        "type": "graph",
        "targets": [
          {
            "expr": "histogram_quantile(0.95, rate(anubiswatch_latency_seconds_bucket[5m]))"
          }
        ]
      },
      {
        "title": "Checks/minute",
        "type": "graph",
        "targets": [
          {
            "expr": "rate(anubiswatch_judgments_total[1m]) * 60"
          }
        ]
      }
    ]
  }
}
```

### 23.3 Alerting Rules

```yaml
groups:
  - name: anubiswatch
    rules:
      - alert: HighLatency
        expr: histogram_quantile(0.95, rate(anubiswatch_latency_seconds_bucket[5m])) > 1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High latency detected"

      - alert: ManySoulsDown
        expr: sum(anubiswatch_souls_total{status="dead"}) > 5
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "{{ $value }} souls are down"

      - alert: ClusterUnhealthy
        expr: anubiswatch_cluster_members{state="leader"} == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "No Raft leader elected"
```
