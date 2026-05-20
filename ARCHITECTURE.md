# AnubisWatch Architecture

## Table of Contents

1. [Overview](#overview)
2. [Egyptian Mythology Naming](#egyptian-mythology-naming)
3. [System Architecture](#system-architecture)
4. [Core Domain Objects](#core-domain-objects)
5. [Storage Engine (Feather/CobaltDB)](#storage-engine-feathercobaltdb)
6. [Probe Engine & Checkers](#probe-engine--checkers)
7. [Alert System (Ma'at)](#alert-system-maat)
8. [Cluster & Distribution (Necropolis)](#cluster--distribution-necropolis)
9. [Journey (Synthetic Monitoring)](#journey-synthetic-monitoring)
10. [Authentication](#authentication)
11. [API Layer](#api-layer)
12. [Dashboard (React)](#dashboard-react)
13. [Data Flow](#data-flow)
14. [Deployment Patterns](#deployment-patterns)
15. [Technology Stack](#technology-stack)
16. [Directory Structure](#directory-structure)

---

## Overview

AnubisWatch is a **zero-dependency, single-binary uptime and synthetic monitoring platform** written in Go. It ships as a single `anubis` binary with:

- An embedded B+Tree storage engine (**CobaltDB**) with WAL and optional AES-256-GCM encryption
- An embedded React 19 dashboard (Tailwind 4 + Zustand 5)
- REST, WebSocket/SSE, gRPC, Prometheus metrics, OpenAPI, and MCP endpoints
- Raft-backed clustering for distributed probe coordination
- Multi-step synthetic monitoring (Journeys)
- Local, OIDC, and LDAP authentication with workspace-aware APIs

**Core purpose:** Monitor services (Souls), store results (Judgments), make alert decisions (Verdicts), and serve a real-time dashboard.

---

## Egyptian Mythology Naming

The codebase uses Egyptian mythology terminology as domain language:

| Term | Mythology | Real-World Meaning |
|------|-----------|-------------------|
| **Soul** | Ka – the life force | Monitored target (HTTP, TCP, DNS, TLS, etc.) |
| **Judgment** | Ma'at's feather | Single health check execution result |
| **Verdict** | Trial outcome | Alert decision based on judgment patterns |
| **Jackal** | Anubis's companion | Probe node that executes health checks |
| **Pharaoh** | Ra – the sun god | Raft leader in a cluster |
| **Necropolis** | City of the dead | Distributed cluster network |
| **Feather** | Ma'at's feather | CobaltDB B+Tree storage engine |
| **Ma'at** | Goddess of truth | Alert engine |
| **Duat** | Egyptian underworld | Real-time WebSocket/SSE event layer |
| **Journey** | Travel of the soul | Multi-step synthetic monitoring scenario |
| **Aaru** | Paradise | Passed health check (alive) |
| **Ammit** | Devourer | Failed health check (dead) |

---

## System Architecture

```
┌────────────────────────────────────────────────────────────────────────────────┐
│                              AnubisWatch Binary                                 │
├────────────────────────────────────────────────────────────────────────────────┤
│                                                                                 │
│  ┌─────────────────────────────────────────────────────────────────────────┐  │
│  │                         Web Layer (Embedded)                             │  │
│  │            React 19 + Tailwind 4 + Zustand 5 + Vite 6                   │  │
│  └─────────────────────────────────────────────────────────────────────────┘  │
│                                                                                 │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌───────────────────┐  │
│  │   REST API   │  │  WebSocket   │  │   gRPC API   │  │      MCP          │  │
│  │   :8443      │  │   (Duat)     │  │   :9090      │  │   Server          │  │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘  └─────────┬─────────┘  │
│         │                 │                  │                   │             │
│  ┌──────┴─────────────────┴──────────────────┴───────────────────┴──────────┐  │
│  │                         Middleware Layer                                  │  │
│  │  Logging → Security Headers → CORS → Recovery → JSON Validation → Rate   │  │
│  └────────────────────────────────┬──────────────────────────────────────────┘  │
│                                   │                                            │
│  ┌────────────────────────────────┴──────────────────────────────────────────┐  │
│  │                        Service Layer (Dependency Injection)                │  │
│  │                                                                          │  │
│  │  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌────────────────────┐   │  │
│  │  │   Auth     │  │   Alert    │  │   Probe    │  │     Journey       │   │  │
│  │  │  Manager   │  │   Ma'at    │  │  Engine    │  │     Executor      │   │  │
│  │  └────────────┘  └────────────┘  └────────────┘  └────────────────────┘   │  │
│  │                                                                          │  │
│  │  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌────────────────────┐   │  │
│  │  │  Cluster   │  │  Dashboard │  │   Status  │  │       gRPC         │   │  │
│  │  │  Manager   │  │   Embed    │  │   Page    │  │      Server       │   │  │
│  │  └────────────┘  └────────────┘  └────────────┘  └────────────────────┘   │  │
│  └────────────────────────────────┬──────────────────────────────────────────┘  │
│                                   │                                            │
│  ┌────────────────────────────────┴──────────────────────────────────────────┐  │
│  │                    Storage Layer (Feather/CobaltDB)                        │  │
│  │                                                                          │  │
│  │   ┌─────────────────────────────────────────────────────────────────┐    │  │
│  │   │              B+Tree Index (configurable order 4–256)            │    │  │
│  │   │                    Leaf node chaining                            │    │  │
│  │   └─────────────────────────────────────────────────────────────────┘    │  │
│  │   ┌─────────────────────────────────────────────────────────────────┐    │  │
│  │   │              WAL (Write-Ahead Log) + AES-256-GCM                │    │  │
│  │   └─────────────────────────────────────────────────────────────────┘    │  │
│  └───────────────────────────────────────────────────────────────────────────┘  │
│                                                                                 │
│  ┌───────────────────────────────────────────────────────────────────────────┐  │
│  │                    Cluster Layer (Necropolis/Raft)                        │  │
│  │                                                                          │  │
│  │  ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌────────────────────┐   │  │
│  │  │   Raft   │    │   Gossip │    │  Probe   │    │   Raft Consensus   │   │  │
│  │  │   Log    │    │ Protocol │    │  Coord.   │    │   (Pharaoh Node)   │   │  │
│  │  └──────────┘    └──────────┘    └──────────┘    └────────────────────┘   │  │
│  └───────────────────────────────────────────────────────────────────────────┘  │
│                                                                                 │
└────────────────────────────────────────────────────────────────────────────────┘
```

---

## Core Domain Objects

### Soul (`internal/core/soul.go`)

A **Soul** is a monitored target — the entity whose heart is weighed on Ma'at's scale.

```go
type Soul struct {
    ID          string           // unique identifier
    WorkspaceID string           // multi-tenant workspace
    Name        string           // human-readable name
    Type        CheckType        // http, tcp, dns, icmp, smtp, imap, grpc, websocket, tls
    Target      string           // host:port or URL
    Weight      Duration         // check interval (e.g. "30s")
    Timeout     Duration         // check timeout
    Enabled     bool             // active or paused
    Tags        []string         // optional labels
    Regions     []string         // restrict to specific regions
    Region      string           // assigned region
    // Type-specific config
    HTTP        *HTTPConfig      `json:"http,omitempty"`
    TCP         *TCPConfig       `json:"tcp,omitempty"`
    TLS         *TLSConfig       `json:"tls,omitempty"`
    // ... SMTP, IMAP, DNS, ICMP, gRPC, WebSocket
}
```

**CheckType values:** `http`, `tcp`, `udp`, `dns`, `icmp`, `smtp`, `imap`, `grpc`, `websocket`, `tls`

**SoulStatus values:** `alive` (passed to Aaru), `dead` (devoured by Ammit), `degraded` (heart is heavy), `unknown` (not yet judged), `embalmed` (maintenance window)

### Judgment (`internal/core/judgment.go`)

A **Judgment** is the result of a single check execution — the weighed heart of a soul.

```go
type Judgment struct {
    ID          string           // unique identifier
    SoulID      string           // which soul
    WorkspaceID string           // for WebSocket routing
    JackalID    string           // which probe node executed it
    Region      string
    Timestamp   time.Time
    Duration    time.Duration    // check latency
    Status      SoulStatus       // alive, dead, degraded
    StatusCode  int              // protocol-specific status code
    Message     string           // human-readable result
    Details     *JudgmentDetails  // protocol-specific data (headers, body, etc.)
    TLSInfo     *TLSInfo         // TLS certificate info
}
```

### Verdict (`internal/core/verdict.go`)

A **Verdict** is the alerting decision made by Ma'at when a Soul's status changes or a rule condition is met.

---

## Storage Engine (Feather/CobaltDB)

Located at `internal/storage/engine.go` — a custom embedded B+Tree storage engine with zero external dependencies.

**Key characteristics:**
- Configurable B+Tree order (default: 32, range: 4–256)
- Write-Ahead Log (WAL) for crash recovery
- Optional AES-256-GCM encryption at rest
- Leaf node chaining for efficient range scans
- MVCC support for concurrent readers
- Snapshot and time-series optimized judgment queries

**Key format:** `{workspaceID}/souls/{soulID}`, `{workspaceID}/judgments/{soulID}/{timestamp}`

**WAL recovery:** On startup, the WAL is replayed to restore the B+Tree to the last consistent state. Typical recovery takes under 1 second.

**Storage sub-packages:**

| File | Purpose |
|------|---------|
| `engine.go` | CobaltDB core (B+Tree + WAL) |
| `storage.go` | High-level storage API (souls, journeys, dashboards, etc.) |
| `encryption.go` | AES-256-GCM encryptor |
| `retention.go` | Time-based data expiration |
| `timeseries.go` | Time-series optimized queries |
| `judgments.go` | Judgment CRUD operations |
| `engine_journey.go` | Journey persistence |
| `raft_log.go` | Raft log store adapter |

---

## Probe Engine & Checkers

Located at `internal/probe/engine.go` and `internal/probe/*.go`.

The **Jackal** probe engine schedules and executes health checks across all protocol types.

```
Scheduler (cron-like) ──▶ CheckerRegistry ──▶ Checker (per soul type)
                                     │
          ┌──────────────────────────┼──────────────────────────┐
          ▼                          ▼                          ▼
    ┌─────────┐               ┌─────────┐               ┌──────────┐
    │  HTTP   │               │   TCP   │               │   DNS    │
    └─────────┘               └─────────┘               └──────────┘
          │                          │                          │
          ▼                          ▼                          ▼
    ┌─────────┐               ┌─────────┐               ┌──────────┐
    │   TLS   │               │  SMTP   │               │   ICMP   │
    └─────────┘               └─────────┘               └──────────┘
          │                          │                          │
          ▼                          ▼                          ▼
    ┌─────────┐               ┌─────────┐               ┌──────────┐
    │  gRPC   │               │  IMAP   │               │WebSocket │
    └─────────┘               └─────────┘               └──────────┘
```

**Engine features:**
- Worker pool with semaphore limiting (default: 100 concurrent checks)
- Circuit breaker pattern for failing targets
- SSRF protection: blocks private IP ranges (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 127.0.0.0/8, etc.)
- Connection pooling for HTTP/HTTPS checks
- Exponential backoff for retries
- Region-aware probe distribution

**Checker implementations (`internal/probe/`):**

| File | Protocol | Key Features |
|------|---------|-------------|
| `http.go` | HTTP/HTTPS | Method, headers, body, JSON path, redirects, SSRF block |
| `tcp.go` | TCP | Banner matching, send/expect regex |
| `dns.go` | DNS | A/AAAA/CNAME/MX/TXT/NS/SOA/PTR/SRV, DNSSEC, propagation |
| `icmp.go` | ICMP | Packet count, interval, loss%, latency thresholds |
| `smtp.go` | SMTP | EHLO, STARTTLS, auth, banner match |
| `imap.go` | IMAP | TLS, auth, mailbox check |
| `grpc.go` | gRPC | TLS, metadata, service name |
| `websocket.go` | WebSocket | Headers, subprotocols, ping/pong |
| `tls.go` | TLS | Expiry, issuer, OCSP, cipher strength, key bits |
| `ssrf.go` | — | SSRF protection layer |

---

## Alert System (Ma'at)

Located at `internal/alert/manager.go` and `internal/alert/dispatchers.go`.

**Ma'at** — the goddess of truth — evaluates judgment results and dispatches notifications.

```go
type AlertRule struct {
    Name      string
    SoulIDs   []string          // target souls
    Condition string            // e.g. "status == dead", "response_time > 500ms"
    Severity  string            // critical, warning, info
    Channels  []string          // channel IDs
    Cooldown  Duration          // minimum time between alerts
}

type AlertChannel struct {
    ID     string
    Type   AlertChannelType     // email, slack, discord, webhook, pagerduty, opsgenie, twilio, ntfy
    Config map[string]any       // channel-specific settings
}
```

**Dispatcher implementations:**

| Dispatcher | Description |
|------------|-------------|
| `email` | SMTP with TLS |
| `slack` | Slack incoming webhooks |
| `discord` | Discord webhooks |
| `webhook` | Generic HTTP POST |
| `pagerduty` | PagerDuty Events API v2 |
| `opsgenie` | OpsGenie API |
| `twilio` | Twilio SMS |
| `ntfy` | ntfy.sh push notifications |

**Features:**
- Rule-based alert triggers with cooldown periods
- Incident management (acknowledge, resolve)
- Alert deduplication and rate limiting
- Per-channel retry with exponential backoff

---

## Cluster & Distribution (Necropolis)

Located at `internal/cluster/manager.go`, `internal/cluster/distribution.go`, and `internal/raft/node.go`.

**Necropolis** is the distributed cluster layer built on Raft consensus.

### Raft Node (`internal/raft/node.go`)

```go
type Node struct {
    config        core.RaftConfig
    nodeID        string
    state         core.RaftState   // follower, candidate, leader
    currentTerm   uint64
    votedFor      string
    log           []core.RaftLogEntry
    commitIndex   uint64
    lastApplied   uint64
    nextIndex     map[string]uint64   // for leaders
    matchIndex    map[string]uint64    // for leaders
    peers         map[string]*Peer
    membership                   // joint consensus tracking
    storage       LogStore
    snapshot      SnapshotStore
    fsm           FSM
    transport     Transport
}
```

**Key Raft features:**
- Leader election with pre-vote optimization
- Log replication with majority acknowledgment
- Snapshotting for log compaction
- Joint consensus for safe membership changes (add/remove/replace peers)
- TCP transport with optional TLS/mTLS

### StorageFSM (`internal/raft/fsm.go`)

```go
type StorageFSM struct {
    mu    sync.RWMutex
    store Storage
    index uint64
}
```

Applies Raft log entries to CobaltDB. All cluster state changes (souls, journeys, rules, channels) go through the FSM.

### Distributor (`internal/raft/distributor.go`)

Assigns souls to probe nodes based on configurable strategies.

### Cluster Manager (`internal/cluster/manager.go`)

```go
type Manager struct {
    necroConfig   core.NecropolisConfig
    node          *raft.Node
    db            *storage.CobaltDB
    logStore      *storage.CobaltDBLogStore
    snapshotStore *storage.CobaltDBSnapshotStore
    fsm           *raft.StorageFSM
    isClustered   bool
}
```

**Discovery modes:** `manual`, `gossip`, `mdns`

**Distribution strategies:**

| Strategy | Behavior |
|----------|---------|
| `round_robin` | Evenly distribute souls across nodes |
| `region_aware` | Prefer same-region probes |
| `redundant` | Assign soul to multiple nodes |
| `weighted` | By node capacity |
| `latency_optimal` | By probe latency |

**Cluster commands:**

```bash
anubis necropolis              # show cluster status
anubis summon 10.0.0.2:7946   # add node
anubis banish jackal-02       # remove node
```

**State transitions:**

```
Follower ──(election timeout)──▶ Candidate
Candidate ──(votes majority)────▶ Leader
Leader ────(higher term)────────▶ Follower
```

---

## Journey (Synthetic Monitoring)

Located at `internal/journey/executor.go`.

A **Journey** is a multi-step synthetic monitoring scenario — a sequence of checks with assertions that mimic real user flows.

```go
type Journey struct {
    ID       string
    Name     string
    Steps    []JourneyStep
    Interval Duration
    Enabled  bool
}

type JourneyStep struct {
    Name       string
    Type       CheckType   // http, tcp, dns, grpc, websocket
    Target     string
    Config     interface{} // type-specific config
    Assertions []Assertion // pass/fail conditions
}
```

**Step types:** HTTP, TCP, DNS, gRPC, WebSocket

**Assertion types:** `status` (HTTP status code), `body` (contains/matches), `header` (response header), `latency` (response time threshold), `json` (JSON path)

**Execution flow:**
1. Journey scheduled at configured interval
2. Each step executed in sequence
3. Assertions evaluated after each step
4. Results stored as Judgments with step context
5. Failure aborts the journey (unless configured to continue)

---

## Authentication

Located at `internal/auth/`.

Three authentication backends with a common interface:

```go
type Authenticator interface {
    Authenticate(ctx context.Context, email, password string) (*User, error)
    UserInfo(ctx context.Context, userID string) (*User, error)
}
```

### Local (`internal/auth/local.go`)

- bcrypt cost 12 password hashing
- Brute-force protection: 5 attempts → 15-minute lockout
- Password policy: 12+ characters, 3 of 4 character classes
- Session tokens stored to disk with `0600` permissions
- Timing-attack resistant user enumeration

### OIDC (`internal/auth/oidc.go`)

- OpenID Connect protocol
- JWK key caching (24h TTL)
- RSA and EC algorithm support
- HMAC state parameter for CSRF protection
- Automatic user creation on first login

### LDAP (`internal/auth/ldap.go`)

- StartTLS for encrypted binds
- DN escaping to prevent injection
- User search with configurable base DN
- Fallback to direct bind if search fails

**Security features across all backends:**
- Constant-time comparison for secrets
- CSPRNG for all random generation (tokens, IDs)
- `httpOnly, secure, sameSite=strict` session cookies
- Security headers on all responses

---

## API Layer

Located at `internal/api/rest.go` — 80+ routes served by the `RESTServer` struct.

```
Middleware chain:
Logging → Security Headers → CORS → Recovery →
JSON Validation → Depth Limit (max 32) → Rate Limiting

Route prefix: /api/v1/{resource}/:id/:action
```

**Core REST endpoints:**

| Resource | Methods | Description |
|----------|---------|-------------|
| `/api/v1/souls` | GET, POST | List/create souls |
| `/api/v1/souls/:id` | GET, PUT, DELETE | Single soul CRUD |
| `/api/v1/souls/:id/judgments` | GET | Soul's judgment history |
| `/api/v1/souls/:id/verdicts` | GET | Soul's verdict history |
| `/api/v1/journeys` | GET, POST | List/create journeys |
| `/api/v1/journeys/:id/run` | POST | Trigger journey execution |
| `/api/v1/rules` | GET, POST | Alert rules |
| `/api/v1/channels` | GET, POST | Alert channels |
| `/api/v1/config` | GET, PUT | Server config |
| `/api/v1/status-pages` | GET | Status page data |
| `/api/v1/audit` | GET | Audit log |
| `/api/v1/metrics` | GET | Prometheus metrics |

**Real-time endpoints:**

| Path | Protocol | Purpose |
|------|----------|---------|
| `/ws` | WebSocket | Duat real-time event stream |
| `/api/v1/events` | SSE | Server-sent events |

**Public endpoints:**

| Path | Purpose |
|------|---------|
| `/` | Embedded React dashboard |
| `/login` | Dashboard login |
| `/health` | Liveness check |
| `/ready` | Readiness check |
| `/metrics` | Prometheus metrics |
| `/api/docs` | OpenAPI documentation UI |
| `/api/openapi.json` | OpenAPI JSON spec |
| `/api/v1/mcp` | MCP JSON-RPC endpoint |
| `/api/v1/mcp/tools` | MCP tool listing |
| `/status`, `/status.html`, `/public/status` | Public status pages |

---

## Dashboard (React)

Located at `web/` and built into `internal/dashboard/src/` for embedding.

**Tech stack:**
- React 19 (concurrent features)
- React Router DOM 7
- Tailwind CSS 4
- Zustand 5 (state management)
- Recharts (visualizations)
- Lucide React (icons)
- Vitest 4 + Playwright (testing)

**Theme:** Egyptian mythology, dark mode default with gold accents.

**Zustand stores:**

| Store | Purpose |
|-------|---------|
| `useSoulStore` | Soul list, CRUD |
| `useThemeStore` | Theme (dark/light/system) |

**Pages:** Dashboard (/) · Souls (/souls) · Soul Detail (/souls/:id) · Journeys (/journeys) · Alerts (/alerts) · Incidents (/incidents) · Maintenance (/maintenance) · Cluster (/cluster) · Status Pages (/status-pages) · Settings (/settings)

**Dashboard widgets (`web/src/components/widgets/`):**

| Widget | Purpose |
|--------|---------|
| `StatWidget` | Single KPI value display |
| `LineChartWidget` | Time-series line chart |
| `BarChartWidget` | Bar chart visualization |
| `GaugeWidget` | Radial gauge for percentage metrics |
| `TableWidget` | Tabular data display |

**Build:** `npm run build:embed` writes the built dashboard into `internal/dashboard/src/` so the Go binary embeds and serves it.

---

## Data Flow

```
1. Soul Registration
   Client → POST /api/v1/souls → REST API → CobaltDB (Put)

2. Health Check Execution
   Scheduler → Probe Engine → Checker (HTTP/TCP/DNS/...)
       │
       ├──▶ Judgment (core) ──▶ CobaltDB (Put)
       ├──▶ Soul Status Update
       └──▶ Alert Engine ──▶ Dispatchers (Slack/Email/...)

3. Real-time Updates
   Judgment Created → WebSocket Server → Dashboard Store (Zustand) → React UI

4. Cluster Replication
   Pharaoh (Leader) ──▶ Raft Log ──▶ StorageFSM ──▶ CobaltDB
```

---

## Deployment Patterns

### Single Node (default)

```
┌─────────────────────────────────────┐
│           Single Node                │
│  ┌─────────┐ ┌─────────┐ ┌───────┐│
│  │   API   │ │  Probe  │ │Storage││
│  └─────────┘ └─────────┘ └───────┘│
└─────────────────────────────────────┘
```

### Multi-Node Cluster (Necropolis)

```
┌─────────────┐       ┌─────────────┐       ┌─────────────┐
│   Node 1    │◀─────▶│   Node 2    │◀─────▶│   Node 3    │
│  (Pharaoh)  │       │  (Jackal)   │       │  (Jackal)   │
│   :7946     │       │   :7946     │       │   :7946     │
└──────┬──────┘       └─────────────┘       └─────────────┘
       │
       │              Load Balancer
       └──────────────┼────────────────┘
                      │
                 ┌────┴────┐
                 │ Clients │
                 └─────────┘
```

---

## Technology Stack

### Backend (Go)

| Package | Purpose |
|---------|---------|
| Go 1.25+ | Language |
| `github.com/coder/websocket` v1.8.14 | WebSocket |
| `github.com/go-ldap/ldap/v3` v3.4.13 | LDAP auth |
| `golang.org/x/crypto` | bcrypt, Argon2 |
| `golang.org/x/net` | Networking |
| `google.golang.org/grpc` v1.80.0 | gRPC |
| `google.golang.org/protobuf` v1.36.11 | Protobuf |
| `gopkg.in/yaml.v3` | YAML config |
| `go.opentelemetry.io/otel` | Distributed tracing |

### Frontend (React)

| Package | Purpose |
|---------|---------|
| React 19 | UI framework |
| react-router-dom 7 | Routing |
| Tailwind CSS 4 | Styling |
| Zustand 5 | State management |
| Recharts | Charts |
| Lucide React | Icons |
| Vitest 4 + Playwright | Testing |

---

## Directory Structure

```
AnubisWatch/
├── cmd/anubis/              # CLI entry point + DI wiring
│   ├── main.go              # main(), flag parsing
│   ├── server.go             # Server struct, Start(), Stop()
│   ├── init.go              # init command (config scaffold)
│   ├── soul.go              # soul CRUD commands
│   ├── judge.go             # judge command (manual check trigger)
│   ├── cluster.go           # necropolis/summon/banish commands
│   ├── backup.go            # backup/restore commands
│   └── config.go            # config validation/show/set
│
├── internal/
│   ├── core/                # Domain models
│   │   ├── soul.go          # Soul, CheckType, SoulStatus
│   │   ├── judgment.go      # Judgment, JudgmentDetails, TLSInfo
│   │   ├── verdict.go       # Verdict, AlertRule, AlertChannel
│   │   ├── config.go        # Config, ServerConfig, StorageConfig
│   │   ├── errors.go        # ConfigError, RaftError
│   │   ├── journey.go       # Journey, JourneyStep, Assertion
│   │   ├── workspace.go     # Workspace model
│   │   ├── feather.go       # Feather metrics definitions
│   │   ├── dashboard.go     # Dashboard and widget models
│   │   ├── statuspage.go    # StatusPage model
│   │   ├── id.go            # ID generation utilities
│   │   ├── context.go       # Context helpers
│   │   └── raft_rpc.go      # Raft RPC types
│   │
│   ├── storage/             # CobaltDB engine
│   │   ├── engine.go        # B+Tree + WAL core (CobaltDB)
│   │   ├── storage.go       # High-level storage API wrapper
│   │   ├── encryption.go    # AES-256-GCM
│   │   ├── retention.go     # Time-based expiration
│   │   ├── timeseries.go    # Time-series queries
│   │   ├── judgments.go     # Judgment CRUD
│   │   ├── engine_journey.go # Journey persistence
│   │   ├── statuspage.go    # Status page storage
│   │   └── raft_log.go      # Raft log store adapter
│   │
│   ├── probe/                # Probe engine + checkers
│   │   ├── engine.go        # Scheduler, worker pool, circuit breaker
│   │   ├── checker.go       # CheckerRegistry
│   │   ├── http.go          # HTTP/HTTPS checker
│   │   ├── tcp.go           # TCP checker
│   │   ├── dns.go           # DNS checker
│   │   ├── icmp.go          # ICMP ping checker
│   │   ├── smtp.go          # SMTP checker
│   │   ├── imap.go          # IMAP checker
│   │   ├── grpc.go          # gRPC health checker
│   │   ├── tls.go           # TLS certificate checker
│   │   ├── websocket.go     # WebSocket checker
│   │   └── ssrf.go          # SSRF protection layer
│   │
│   ├── alert/                # Ma'at alert engine
│   │   ├── manager.go        # Alert routing, incident management
│   │   └── dispatchers.go   # Email, Slack, Discord, Webhook, PagerDuty, etc.
│   │
│   ├── journey/              # Synthetic monitoring
│   │   └── executor.go      # Multi-step journey runner
│   │
│   ├── raft/                 # Raft consensus
│   │   ├── node.go          # Raft state machine (Pharaoh/Jackal)
│   │   ├── fsm.go           # StorageFSM — applies log entries to CobaltDB
│   │   ├── transport.go     # TCP transport with TLS
│   │   ├── discovery.go     # Node discovery (gossip/mdns/manual)
│   │   └── distributor.go   # Probe work distribution (soul → node assignment)
│   │
│   ├── cluster/              # Cluster management
│   │   ├── manager.go       # Necropolis controller
│   │   └── distribution.go   # Distribution strategies
│   │
│   ├── api/                  # HTTP API layer
│   │   ├── rest.go          # RESTServer (80+ routes)
│   │   ├── websocket.go     # Duat real-time layer
│   │   ├── metrics.go       # Prometheus metrics
│   │   ├── audit.go         # Audit log
│   │   ├── mcp.go           # MCP server (AI integration)
│   │   ├── statuspage.go    # Status page API
│   │   └── handlers_extra.go # Extended handler utilities
│   │
│   ├── auth/                 # Authentication
│   │   ├── local.go         # bcrypt + sessions
│   │   ├── oidc.go          # OpenID Connect
│   │   └── ldap.go          # LDAP/Active Directory
│   │
│   ├── backup/               # Backup & restore
│   │   └── manager.go
│   │
│   ├── statuspage/           # Public status page
│   │   └── handler.go
│   │
│   ├── dashboard/            # Embedded dashboard assets
│   │   └── embed.go
│   │
│   ├── grpcapi/              # gRPC API server
│   │   └── server.go
│   │
│   └── telemetry/            # Observability
│       └── tracer.go          # OpenTelemetry setup
│
├── web/                      # React dashboard source
│   ├── src/
│   │   ├── components/
│   │   │   └── widgets/     # StatWidget, LineChartWidget, BarChartWidget,
│   │   │                    # GaugeWidget, TableWidget
│   │   ├── pages/           # Route pages
│   │   ├── stores/          # Zustand stores (soulStore, themeStore)
│   │   ├── api/             # API client
│   │   └── hooks/           # React hooks
│   └── package.json
│
├── configs/                  # Config examples (JSON/YAML)
├── deploy/                   # Kubernetes, Helm, Docker
│   ├── k8s/                  # K8s manifests
│   ├── helm/anubiswatch/     # Helm chart
│   └── docker/               # Docker compose
├── docs/                     # Documentation
│   ├── adr/                  # Architecture Decision Records
│   └── api/                  # OpenAPI spec
├── scripts/                  # Helper scripts
├── proto/v1/                 # Protocol Buffer definitions
├── Makefile
└── Dockerfile
```

---

## Performance Characteristics

| Metric | Value |
|--------|-------|
| Binary size | ~18 MB (with embedded dashboard) |
| Memory (idle) | ~50 MB |
| Memory (active) | ~150 MB (100 souls, 60s interval) |
| Check latency | <10ms for local network targets |
| WAL recovery | <1s typical |
| B+Tree operations | O(log n) with configurable order |
| Concurrent checks | 100+ (configurable worker pool) |