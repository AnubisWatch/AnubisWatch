# AnubisWatch — SPECIFICATION.md

> **"The Judgment Never Sleeps"**

**Version:** 1.0.0
**Author:** Ersin Koç — ECOSTACK TECHNOLOGY OÜ
**Date:** 2026-03-30
**License:** Apache 2.0 (core) + Commercial (enterprise features)
**Repository:** github.com/AnubisWatch/anubiswatch
**Domains:** anubis.watch · anubiswatch.com
**Language:** Go 1.24+
**Binary:** `anubis`
**Tagline:** "The Judgment Never Sleeps"
**Alt Taglines:** "Weighing Your Uptime" · "The Uptime Judge"

---

## 1. PROJECT OVERVIEW

### 1.1 What is AnubisWatch?

AnubisWatch is a **zero-dependency, single-binary uptime and synthetic monitoring platform** built in pure Go. It replaces the fragmented monitoring stack (UptimeRobot + Pingdom + Uptime Kuma + Checkly) with a unified, self-hosted solution that scales from a single Raspberry Pi to a globally distributed cluster.

Every node in an AnubisWatch cluster is both a **probe** and a **controller** — powered by Raft consensus, there is no single point of failure. Monitoring checks are distributed across nodes (Jackals), results are synchronized via consensus, and alerts fire from the current Raft leader.

### 1.2 Egyptian Mythology Theme

AnubisWatch embraces the mythology of **Anubis**, the Egyptian god of the afterlife who weighs the hearts of the dead against the feather of Ma'at to determine their fate. This maps perfectly to uptime monitoring:

| Monitoring Concept | Anubis Mythology | Internal Term |
|---|---|---|
| Health Check | Weighing of the Heart | **Judgment** |
| Monitor Target | Soul being judged | **Soul** |
| Probe Node | Jackal (Anubis's form) | **Jackal** |
| Alert Notification | Judgment verdict | **Verdict** |
| Public Status Page | Book of the Dead | **Book of the Dead** |
| Dashboard | Hall of Ma'at | **Hall of Ma'at** |
| Cluster | Necropolis (city of dead) | **Necropolis** |
| Downtime Event | Devouring by Ammit | **Devouring** |
| Recovery Event | Passage to Aaru (paradise) | **Resurrection** |
| Check Interval | Weighing frequency | **Weight** |
| Uptime Percentage | Soul's purity score | **Purity** |
| Incident | Curse of the Pharaoh | **Curse** |
| Maintenance Window | Embalming period | **Embalming** |
| Multi-step Check | Journey through Duat | **Duat Journey** |
| Performance Budget | Feather of Ma'at | **Feather** |

### 1.3 Why AnubisWatch?

| Problem | Existing Solution | AnubisWatch |
|---|---|---|
| SaaS lock-in, monitor limits | UptimeRobot, Pingdom | Self-hosted, unlimited monitors |
| Single maintainer, Node.js bloat | Uptime Kuma | Go, single binary, zero deps |
| Expensive developer monitoring | Checkly | Free & open source core |
| No multi-protocol single binary | All of them | 8 protocols, 1 binary |
| No true distributed probing | Most solutions | Raft cluster, every node is a probe |
| No synthetic monitoring in OSS | Checkly (SaaS only) | Built-in multi-step HTTP chains |
| No embedded storage | External DB required | CobaltDB embedded engine |

### 1.4 Design Principles

1. **Zero External Dependencies** — Only `golang.org/x/crypto`, `golang.org/x/sys`, and a YAML parser as extended stdlib
2. **Single Binary** — One `anubis` binary contains probe engine, web dashboard, API server, Raft consensus, storage engine, and alert dispatcher
3. **Every Node is Everything** — No separate probe/controller/scheduler binaries; Raft elects the leader, all nodes execute checks
4. **Mythology-Driven UX** — CLI commands, API endpoints, config keys, and UI elements use Egyptian mythology terminology consistently
5. **CobaltDB Inside** — Own embedded storage engine for time-series metrics, configuration, and state
6. **React 19 Embedded** — Dashboard compiled into the binary via `embed.FS`, zero external web server needed
7. **MCP-Native** — Built-in MCP (Model Context Protocol) server for AI agent integration
8. **Multi-Tenant from Day 1** — Workspace isolation for SaaS deployment without architectural changes

---

## 2. ARCHITECTURE

### 2.1 High-Level Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    AnubisWatch Binary                        │
│                                                             │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────────┐  │
│  │  Probe   │  │   Raft   │  │   API    │  │  Dashboard  │  │
│  │  Engine  │  │ Consensus│  │  Server  │  │  (React 19) │  │
│  │          │  │          │  │          │  │  embedded   │  │
│  │ 8 proto- │  │  Leader  │  │ REST +   │  │  Tailwind   │  │
│  │ col chk  │  │  Election│  │ gRPC +   │  │  4.1 +      │  │
│  │          │  │  Log Rep │  │ WebSocket│  │  shadcn/ui  │  │
│  │          │  │  State   │  │ MCP Svr  │  │  Lucide     │  │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └──────┬─────┘  │
│       │              │              │               │        │
│  ┌────┴──────────────┴──────────────┴───────────────┴─────┐  │
│  │                    CobaltDB Engine                      │  │
│  │         Embedded Storage (B+Tree, WAL, MVCC)           │  │
│  │     Time-Series Optimized · AES-256-GCM Encryption     │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                             │
│  ┌────────────────────────────────────────────────────────┐  │
│  │                Alert Dispatcher                        │  │
│  │  Webhook · Slack · Discord · Telegram · Email(SMTP)   │  │
│  │  PagerDuty · OpsGenie · SMS(Twilio) · Ntfy.sh        │  │
│  └────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

### 2.2 Cluster Architecture (Necropolis)

```
                    ┌─────────────────┐
                    │   Raft Leader    │
                    │  Jackal-EU-01   │
                    │  (Pharaoh)      │
                    │                 │
                    │  • Schedules    │
                    │  • Dispatches   │
                    │  • Alerts       │
                    └────┬───┬────────┘
                         │   │
              ┌──────────┘   └──────────┐
              │                         │
     ┌────────▼────────┐     ┌─────────▼────────┐
     │  Raft Follower   │     │  Raft Follower    │
     │  Jackal-US-01   │     │  Jackal-APAC-01  │
     │                 │     │                   │
     │  • Executes     │     │  • Executes       │
     │    checks       │     │    checks         │
     │  • Replicates   │     │  • Replicates     │
     │  • Ready to     │     │  • Ready to       │
     │    become leader│     │    become leader  │
     └─────────────────┘     └───────────────────┘

     mDNS/Gossip Auto-Discovery ←→ Manual Join
```

**Raft Roles:**
- **Pharaoh** (Leader): Schedules check distribution, dispatches alerts, serves dashboard, accepts config changes
- **Jackal** (Follower): Executes assigned checks, replicates state, promotes to Pharaoh if leader fails
- **Candidate**: Node in election process (standard Raft)

### 2.3 Module Architecture

```
anubiswatch/
├── cmd/
│   └── anubis/              # CLI entrypoint
│       └── main.go
├── internal/
│   ├── core/                # Core types, interfaces, config
│   │   ├── soul.go          # Monitor target definition
│   │   ├── judgment.go      # Check result type
│   │   ├── verdict.go       # Alert decision type
│   │   └── config.go        # YAML config parser
│   ├── probe/               # Probe engine
│   │   ├── engine.go        # Check scheduler & executor
│   │   ├── checker.go       # Checker interface
│   │   ├── http.go          # HTTP/HTTPS checker
│   │   ├── tcp.go           # TCP/UDP checker
│   │   ├── dns.go           # DNS checker
│   │   ├── smtp.go          # SMTP/IMAP checker
│   │   ├── icmp.go          # ICMP Ping checker
│   │   ├── grpc.go          # gRPC health checker
│   │   ├── websocket.go     # WebSocket checker
│   │   ├── tls.go           # TLS certificate checker
│   │   └── synthetic.go     # Multi-step HTTP chains (Duat Journey)
│   ├── raft/                # Raft consensus (custom implementation)
│   │   ├── node.go          # Raft node state machine
│   │   ├── log.go           # Raft log (backed by CobaltDB)
│   │   ├── transport.go     # TCP transport layer
│   │   ├── snapshot.go      # Snapshot management
│   │   ├── election.go      # Leader election
│   │   └── discovery.go     # mDNS + gossip auto-discovery
│   ├── storage/             # CobaltDB integration
│   │   ├── engine.go        # CobaltDB wrapper
│   │   ├── timeseries.go    # Time-series optimized storage
│   │   ├── retention.go     # Data retention & downsampling
│   │   └── migration.go     # Schema migration
│   ├── alert/               # Alert dispatcher
│   │   ├── dispatcher.go    # Alert routing engine
│   │   ├── webhook.go       # Generic webhook
│   │   ├── slack.go         # Slack integration
│   │   ├── discord.go       # Discord integration
│   │   ├── telegram.go      # Telegram bot
│   │   ├── email.go         # Built-in SMTP client
│   │   ├── pagerduty.go     # PagerDuty integration
│   │   ├── opsgenie.go      # OpsGenie integration
│   │   ├── sms.go           # SMS via Twilio/Vonage
│   │   ├── ntfy.go          # Ntfy.sh push
│   │   └── rules.go         # Alert rules engine
│   ├── api/                 # API server
│   │   ├── rest/            # REST API (JSON)
│   │   │   ├── router.go
│   │   │   ├── souls.go     # CRUD monitors
│   │   │   ├── judgments.go # Check results
│   │   │   ├── verdicts.go  # Alerts history
│   │   │   ├── necropolis.go# Cluster management
│   │   │   ├── tenants.go   # Multi-tenant mgmt
│   │   │   └── middleware.go# Auth, CORS, rate limit
│   │   ├── grpc/            # gRPC API
│   │   │   ├── server.go
│   │   │   └── proto/       # Protobuf definitions
│   │   ├── ws/              # WebSocket server
│   │   │   └── hub.go       # Real-time event broadcast
│   │   └── mcp/             # MCP Server
│   │       ├── server.go
│   │       ├── tools.go     # MCP tool definitions
│   │       └── resources.go # MCP resource definitions
│   ├── tenant/              # Multi-tenant isolation
│   │   ├── workspace.go     # Workspace management
│   │   ├── isolation.go     # Data isolation layer
│   │   └── quota.go         # Resource quotas
│   ├── statuspage/          # Public status page generator
│   │   ├── generator.go     # Static HTML generator
│   │   └── templates/       # Status page templates
│   └── dashboard/           # Embedded dashboard
│       └── embed.go         # embed.FS for React build
├── web/                     # React 19 frontend source
│   ├── src/
│   │   ├── App.tsx
│   │   ├── components/
│   │   │   ├── ui/          # shadcn/ui components
│   │   │   ├── hall/        # Hall of Ma'at (main dashboard)
│   │   │   ├── souls/       # Monitor management
│   │   │   ├── book/        # Book of the Dead (status page)
│   │   │   ├── necropolis/  # Cluster management
│   │   │   └── charts/      # Grafana-style custom dashboards
│   │   ├── hooks/
│   │   ├── lib/
│   │   └── stores/
│   ├── tailwind.config.ts   # Tailwind CSS 4.1
│   ├── package.json
│   └── vite.config.ts
├── configs/
│   └── anubis.example.yaml  # Example configuration
├── docs/
│   ├── SPECIFICATION.md     # This file
│   ├── IMPLEMENTATION.md
│   ├── TASKS.md
│   └── BRANDING.md
├── go.mod
├── go.sum
├── Makefile
├── Dockerfile
└── README.md
```

---

## 3. CHECK PROTOCOLS (Judgment Types)

### 3.1 Checker Interface

```go
// Checker is the core interface every protocol implements.
// Named after the priests who assisted Anubis in the weighing ceremony.
type Checker interface {
    // Type returns the protocol identifier
    Type() string
    
    // Judge performs the health check and returns a Judgment
    Judge(ctx context.Context, soul *Soul) (*Judgment, error)
    
    // Validate checks if the Soul configuration is valid for this checker
    Validate(soul *Soul) error
}

// Judgment represents the result of a single check
type Judgment struct {
    SoulID      string        `json:"soul_id"`
    JackalID    string        `json:"jackal_id"`    // Which probe executed
    Region      string        `json:"region"`        // Probe region
    Timestamp   time.Time     `json:"timestamp"`
    Duration    time.Duration `json:"duration"`      // Check latency
    Status      SoulStatus    `json:"status"`        // Alive, Dead, Degraded
    StatusCode  int           `json:"status_code"`   // Protocol-specific code
    Message     string        `json:"message"`       // Human-readable result
    Details     any           `json:"details"`       // Protocol-specific details
    Assertions  []Assertion   `json:"assertions"`    // Assertion results
    TLSInfo     *TLSInfo      `json:"tls_info"`      // TLS details if applicable
}

// SoulStatus represents the current state of a monitored target
type SoulStatus string

const (
    SoulAlive    SoulStatus = "alive"     // ✅ Passed to Aaru (paradise)
    SoulDead     SoulStatus = "dead"      // 💀 Devoured by Ammit
    SoulDegraded SoulStatus = "degraded"  // ⚠️ Heart is heavy but not condemned
    SoulUnknown  SoulStatus = "unknown"   // ❓ Not yet judged
    SoulEmbalmed SoulStatus = "embalmed"  // 🔧 Maintenance window (paused)
)
```

### 3.2 HTTP/HTTPS Checker

**Capabilities:**
- HTTP methods: GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS
- Status code assertion (exact, range, list)
- Response body matching: contains, regex, JSON path assertion
- Response header assertion
- Response time threshold (Feather of Ma'at)
- Follow/no-follow redirects
- Custom headers (auth tokens, etc.)
- Request body for POST/PUT
- TLS certificate inspection (see 3.9)
- HTTP/2 and HTTP/3 support
- Cookie jar persistence for multi-step

**Configuration:**
```yaml
souls:
  - name: "Production API"
    type: http
    target: "https://api.example.com/health"
    weight: 30s                    # check interval
    timeout: 10s
    http:
      method: GET
      headers:
        Authorization: "Bearer ${API_TOKEN}"
      valid_status: [200, 201]
      body_contains: "\"status\":\"ok\""
      body_regex: "version\":\\s*\"\\d+\\.\\d+"
      json_path:
        "$.status": "ok"
        "$.services.db": "connected"
      feather: 500ms               # max acceptable latency
      follow_redirects: true
      max_redirects: 5
      http_version: "2"
```

### 3.3 TCP/UDP Checker

**Capabilities:**
- TCP port open check (connect timeout)
- TCP banner grab (read first N bytes after connect)
- TCP send/expect (send payload, assert response)
- UDP packet send/receive
- Connection duration measurement

**Configuration:**
```yaml
souls:
  - name: "MySQL Server"
    type: tcp
    target: "db.example.com:3306"
    weight: 15s
    tcp:
      banner_match: "mysql"
      send: ""
      expect_regex: "^.{4}\\x0a"
      
  - name: "Game Server"
    type: udp
    target: "game.example.com:27015"
    weight: 60s
    udp:
      send_hex: "FFFFFFFF54536F7572636520456E67696E6520517565727900"
      expect_contains: "Team Fortress"
```

### 3.4 DNS Checker

**Capabilities:**
- Record resolution: A, AAAA, CNAME, MX, TXT, NS, SOA, SRV, PTR, CAA
- Expected value assertion
- DNSSEC validation
- Multi-resolver propagation check (query N resolvers, compare)
- Response time measurement
- Authority/additional section inspection
- Custom DNS server targeting

**Configuration:**
```yaml
souls:
  - name: "API DNS"
    type: dns
    target: "api.example.com"
    weight: 60s
    dns:
      record_type: A
      expected: ["1.2.3.4", "5.6.7.8"]
      nameservers:
        - "8.8.8.8:53"
        - "1.1.1.1:53"
        - "9.9.9.9:53"
      dnssec_validate: true
      propagation_check: true      # all nameservers must agree
      propagation_threshold: 100   # percentage agreement needed
```

### 3.5 SMTP/IMAP Checker

**Capabilities:**
- SMTP connection + EHLO handshake
- STARTTLS upgrade verification
- SMTP AUTH test (optional)
- IMAP connection + LOGIN test
- IMAP mailbox status (message count)
- Banner/capability assertion
- Certificate inspection on TLS

**Configuration:**
```yaml
souls:
  - name: "Mail Server SMTP"
    type: smtp
    target: "mail.example.com:587"
    weight: 120s
    smtp:
      ehlo_domain: "monitor.example.com"
      starttls: true
      auth:
        username: "${SMTP_USER}"
        password: "${SMTP_PASS}"
      banner_contains: "ESMTP"
      
  - name: "Mail Server IMAP"
    type: imap
    target: "mail.example.com:993"
    weight: 120s
    imap:
      tls: true
      auth:
        username: "${IMAP_USER}"
        password: "${IMAP_PASS}"
      check_mailbox: "INBOX"
```

### 3.6 ICMP Ping Checker

**Capabilities:**
- ICMP Echo Request/Reply (IPv4 + IPv6)
- Configurable packet count
- Packet loss percentage calculation
- Min/avg/max/jitter latency
- TTL assertion
- Privileged (raw socket) and unprivileged (UDP) modes

**Configuration:**
```yaml
souls:
  - name: "Edge Server"
    type: icmp
    target: "edge.example.com"
    weight: 15s
    icmp:
      count: 5
      interval: 200ms
      timeout: 3s
      max_loss_percent: 20         # alert if >20% packet loss
      feather: 100ms               # max avg latency
      ipv6: false
      privileged: true             # use raw sockets (requires cap_net_raw)
```

### 3.7 gRPC Health Checker

**Capabilities:**
- Standard gRPC Health Checking Protocol (grpc.health.v1.Health)
- Service-specific health check
- TLS/mTLS support
- Metadata/header injection
- Response time measurement
- Reflection-based service discovery

**Configuration:**
```yaml
souls:
  - name: "Payment Service"
    type: grpc
    target: "grpc.example.com:9090"
    weight: 30s
    grpc:
      service: "payment.PaymentService"
      tls: true
      tls_ca: "/path/to/ca.pem"
      metadata:
        x-api-key: "${GRPC_API_KEY}"
      feather: 200ms
```

### 3.8 WebSocket Checker

**Capabilities:**
- WebSocket upgrade handshake
- Message send + response assertion
- Connection duration measurement
- Subprotocol negotiation
- Custom headers for upgrade request
- Ping/pong frame validation
- Close code assertion

**Configuration:**
```yaml
souls:
  - name: "Realtime Feed"
    type: websocket
    target: "wss://ws.example.com/feed"
    weight: 60s
    websocket:
      headers:
        Authorization: "Bearer ${WS_TOKEN}"
      subprotocols: ["graphql-ws"]
      send: '{"type":"connection_init"}'
      expect_contains: "connection_ack"
      ping_check: true
      feather: 1s
```

### 3.9 TLS Certificate Checker

**Capabilities:**
- Certificate expiry monitoring (alert N days before)
- Certificate chain validation
- Cipher suite audit (flag weak ciphers)
- Protocol version check (TLS 1.2/1.3)
- Certificate transparency log check
- SAN (Subject Alternative Name) validation
- OCSP stapling check
- Key size validation
- Issuer validation

**Configuration:**
```yaml
souls:
  - name: "API Certificate"
    type: tls
    target: "api.example.com:443"
    weight: 3600s                  # check hourly
    tls:
      expiry_warn_days: 30
      expiry_critical_days: 7
      min_protocol: "TLS1.2"
      forbidden_ciphers:
        - "TLS_RSA_WITH_RC4_128_SHA"
        - "TLS_RSA_WITH_3DES_EDE_CBC_SHA"
      expected_issuer: "Let's Encrypt"
      expected_san: ["api.example.com", "*.api.example.com"]
      check_ocsp: true
      min_key_bits: 2048
```

---

## 4. SYNTHETIC MONITORING (Duat Journeys)

### 4.1 Concept

Duat Journeys are **multi-step HTTP check chains** that simulate real user workflows without a browser engine. Each step can extract values, set variables, and pass them to subsequent steps. Named after the Egyptian underworld (Duat) through which souls journey.

### 4.2 Journey Definition

```yaml
journeys:
  - name: "User Login Flow"
    weight: 300s                   # every 5 minutes
    timeout: 30s                   # total journey timeout
    continue_on_failure: false     # stop at first failure
    variables:
      base_url: "https://app.example.com"
      
    steps:
      - name: "Get CSRF Token"
        type: http
        target: "${base_url}/login"
        http:
          method: GET
          valid_status: [200]
        extract:
          csrf_token:
            from: body
            json_path: "$.csrf_token"
          session_cookie:
            from: header
            header: "Set-Cookie"
            regex: "session=([^;]+)"
            
      - name: "Perform Login"
        type: http
        target: "${base_url}/api/auth/login"
        http:
          method: POST
          headers:
            Content-Type: "application/json"
            Cookie: "session=${session_cookie}"
            X-CSRF-Token: "${csrf_token}"
          body: |
            {"email": "${TEST_EMAIL}", "password": "${TEST_PASSWORD}"}
          valid_status: [200]
          json_path:
            "$.success": "true"
        extract:
          auth_token:
            from: body
            json_path: "$.token"
            
      - name: "Fetch Dashboard"
        type: http
        target: "${base_url}/api/dashboard"
        http:
          method: GET
          headers:
            Authorization: "Bearer ${auth_token}"
          valid_status: [200]
          json_path:
            "$.user.role": "admin"
          feather: 2s               # dashboard must load < 2s
          
      - name: "Verify API Response Schema"
        type: http
        target: "${base_url}/api/data"
        http:
          method: GET
          headers:
            Authorization: "Bearer ${auth_token}"
          valid_status: [200]
          json_schema: |
            {
              "type": "object",
              "required": ["data", "meta"],
              "properties": {
                "data": { "type": "array" },
                "meta": {
                  "type": "object",
                  "required": ["total", "page"]
                }
              }
            }
```

### 4.3 SSL/TLS Handshake Validation & Cipher Audit

Built into the TLS checker (Section 3.9) but also available as a journey step:

```yaml
steps:
  - name: "TLS Audit"
    type: tls_audit
    target: "api.example.com:443"
    tls_audit:
      assert_protocol: "TLS1.3"
      deny_ciphers:
        - "TLS_RSA_*"
        - "*_CBC_*"
      assert_key_type: "ECDSA"
      assert_key_bits_min: 256
      check_certificate_transparency: true
```

### 4.4 DNS Propagation Tracking

```yaml
steps:
  - name: "DNS Propagation After Deploy"
    type: dns_propagation
    target: "api.example.com"
    dns_propagation:
      record_type: A
      expected_value: "203.0.113.50"
      resolvers:
        - "8.8.8.8:53"            # Google
        - "1.1.1.1:53"            # Cloudflare
        - "9.9.9.9:53"            # Quad9
        - "208.67.222.222:53"     # OpenDNS
        - "8.26.56.26:53"         # Comodo
      propagation_threshold: 80   # 80% of resolvers must agree
      max_wait: 300s              # wait up to 5 minutes
      poll_interval: 10s
```

### 4.5 API Response JSON Schema Validation

```yaml
steps:
  - name: "Validate API Contract"
    type: http
    target: "https://api.example.com/v2/users"
    http:
      method: GET
      valid_status: [200]
      json_schema: |
        {
          "$schema": "https://json-schema.org/draft/2020-12/schema",
          "type": "object",
          "required": ["users", "pagination"],
          "properties": {
            "users": {
              "type": "array",
              "items": {
                "type": "object",
                "required": ["id", "email", "created_at"]
              }
            }
          }
        }
      json_schema_strict: true     # fail on additional properties
```

### 4.6 Performance Budgets (Feather of Ma'at)

```yaml
feathers:                          # Global performance budgets
  - name: "API Latency Budget"
    scope: "tag:api"               # Apply to all souls tagged 'api'
    rules:
      p50: 200ms
      p95: 500ms
      p99: 1s
      max: 3s
    window: 5m                     # Evaluate over 5-minute window
    violation_threshold: 3         # Alert after 3 consecutive violations
    
  - name: "Homepage Speed"
    scope: "soul:homepage"
    rules:
      p95: 800ms
    window: 15m
```

---

## 5. RAFT CONSENSUS & CLUSTER (Necropolis)

### 5.1 Custom Raft Implementation

AnubisWatch implements Raft consensus from scratch (no external dependency), optimized for monitoring workloads:

**Core Raft:**
- Leader election with randomized timeouts
- Log replication with pipelining
- Snapshot compaction (backed by CobaltDB)
- Membership changes (joint consensus)
- Pre-vote extension (prevents disruption from partitioned nodes)

**Monitoring-Specific Extensions:**
- **Check Distribution:** Leader distributes check assignments across Jackals based on region, load, and probe capabilities
- **Result Aggregation:** Followers report check results to leader; leader applies consensus before storing
- **Alert Deduplication:** Only the leader fires alerts, preventing duplicate notifications during leader transition
- **Split-Brain Protection:** Minimum quorum required before any alerts fire; prevents false positives during partition

### 5.2 Transport Layer

```
┌──────────────────┐          ┌──────────────────┐
│  Jackal-EU-01    │          │  Jackal-US-01    │
│                  │  TCP/TLS │                  │
│  Raft Transport ◄──────────► Raft Transport   │
│  :7946 (raft)    │          │  :7946 (raft)    │
│  :7947 (gossip)  │          │  :7947 (gossip)  │
│  :8443 (api/ws)  │          │  :8443 (api/ws)  │
│  :9090 (grpc)    │          │  :9090 (grpc)    │
└──────────────────┘          └──────────────────┘
```

**Ports:**
- `7946/tcp` — Raft consensus transport (TLS mutual auth)
- `7947/udp` — Gossip/mDNS auto-discovery
- `8443/tcp` — HTTPS API + WebSocket + Dashboard
- `9090/tcp` — gRPC API

### 5.3 Auto-Discovery

**mDNS Mode (LAN):**
- Broadcast `_anubis._tcp.local` service
- Auto-join discovered nodes to cluster
- Zero configuration for single-network deployments

**Gossip Mode (WAN):**
- SWIM-based gossip protocol over UDP
- Seed nodes configured in YAML
- NAT traversal via relay nodes
- Encrypted gossip with cluster secret

**Manual Mode:**
- Explicit `peers` list in configuration
- `anubis summon <address>` CLI command

### 5.4 Region Tagging & Check Distribution

```yaml
necropolis:
  node_name: "jackal-eu-01"
  region: "eu-west"
  tags:
    datacenter: "amsterdam"
    provider: "hetzner"
  
  # Check distribution strategy
  distribution:
    strategy: "region-aware"       # checks prefer same-region jackals
    redundancy: 2                  # each soul checked by N jackals
    rebalance_interval: 60s
    
  # Probe capabilities (what this node can check)
  capabilities:
    icmp: true                     # requires cap_net_raw
    ipv6: true
    dns: true
    internal_network: true         # can reach private IPs
```

**Distribution Strategies:**
- `round-robin` — Equal distribution, no region awareness
- `region-aware` — Prefer local region, fallback to others
- `latency-optimized` — Route to lowest-latency Jackal
- `redundant` — Every soul checked by N Jackals, results compared

### 5.5 Multi-Tenant Workspace Isolation

```yaml
tenants:
  enabled: true
  isolation: "strict"              # strict = separate CobaltDB namespace per tenant
  
  default_quotas:
    max_souls: 100
    max_journeys: 20
    max_alert_channels: 10
    max_team_members: 25
    retention_days: 90
    check_interval_min: 30s        # minimum allowed interval
```

**Tenant Data Model:**
- Each tenant = a Workspace with UUID
- All CobaltDB keys prefixed with workspace UUID
- API authentication scoped to workspace
- Dashboard shows only workspace data
- Cross-workspace queries prohibited at storage level

---

## 6. ALERT SYSTEM (Verdicts)

### 6.1 Alert Rules Engine

```yaml
verdicts:
  rules:
    - name: "API Down"
      condition:
        type: consecutive_failures
        threshold: 3               # 3 consecutive failures
      severity: critical
      channels: ["ops-slack", "pagerduty-oncall", "sms-oncall"]
      
    - name: "API Degraded"
      condition:
        type: threshold
        metric: latency_p95
        operator: ">"
        value: 500ms
        window: 5m
      severity: warning
      channels: ["dev-slack"]
      cooldown: 15m                # don't re-alert within 15 min
      
    - name: "Certificate Expiring"
      condition:
        type: threshold
        metric: tls_days_until_expiry
        operator: "<"
        value: 14
      severity: warning
      channels: ["ops-email", "ops-slack"]
      cooldown: 24h
```

**Condition Types:**
- `consecutive_failures` — N failures in a row
- `threshold` — Metric crosses a boundary
- `percentage` — Failure rate over a time window
- `anomaly` — Deviation from baseline (rolling average)
- `compound` — AND/OR of multiple conditions

### 6.2 Alert Channels

#### 6.2.1 Webhook
```yaml
channels:
  - name: "custom-webhook"
    type: webhook
    webhook:
      url: "https://hooks.example.com/anubis"
      method: POST
      headers:
        X-Webhook-Secret: "${WEBHOOK_SECRET}"
      template: |
        {
          "soul": "{{ .Soul.Name }}",
          "status": "{{ .Judgment.Status }}",
          "region": "{{ .Judgment.Region }}",
          "duration": "{{ .Judgment.Duration }}",
          "message": "{{ .Judgment.Message }}"
        }
      retry:
        max_attempts: 3
        backoff: exponential
```

#### 6.2.2 Slack
```yaml
  - name: "ops-slack"
    type: slack
    slack:
      webhook_url: "${SLACK_WEBHOOK}"
      channel: "#ops-alerts"
      username: "AnubisWatch"
      icon_emoji: ":anubis:"
      mention_on_critical: ["@oncall-team"]
```

#### 6.2.3 Discord
```yaml
  - name: "dev-discord"
    type: discord
    discord:
      webhook_url: "${DISCORD_WEBHOOK}"
      username: "AnubisWatch"
      avatar_url: "https://anubis.watch/avatar.png"
```

#### 6.2.4 Telegram
```yaml
  - name: "ops-telegram"
    type: telegram
    telegram:
      bot_token: "${TELEGRAM_BOT_TOKEN}"
      chat_id: "${TELEGRAM_CHAT_ID}"
      parse_mode: "HTML"
      disable_notification: false
```

#### 6.2.5 Email (Built-in SMTP Client)
```yaml
  - name: "ops-email"
    type: email
    email:
      smtp_host: "smtp.example.com"
      smtp_port: 587
      starttls: true
      username: "${SMTP_USER}"
      password: "${SMTP_PASS}"
      from: "anubis@example.com"
      to: ["ops@example.com", "cto@example.com"]
      subject_template: "[{{ .Severity }}] {{ .Soul.Name }} — {{ .Judgment.Status }}"
```

#### 6.2.6 PagerDuty
```yaml
  - name: "pagerduty-oncall"
    type: pagerduty
    pagerduty:
      integration_key: "${PD_INTEGRATION_KEY}"
      severity_map:
        critical: "critical"
        warning: "warning"
        info: "info"
      auto_resolve: true           # resolve when soul recovers
```

#### 6.2.7 OpsGenie
```yaml
  - name: "opsgenie-oncall"
    type: opsgenie
    opsgenie:
      api_key: "${OG_API_KEY}"
      priority_map:
        critical: "P1"
        warning: "P3"
      tags: ["anubis", "uptime"]
      auto_close: true
```

#### 6.2.8 SMS (Twilio/Vonage)
```yaml
  - name: "sms-oncall"
    type: sms
    sms:
      provider: "twilio"           # or "vonage"
      account_sid: "${TWILIO_SID}"
      auth_token: "${TWILIO_TOKEN}"
      from: "+1234567890"
      to: ["+0987654321"]
      template: "🔴 {{ .Soul.Name }} is {{ .Judgment.Status }} — {{ .Judgment.Message }}"
```

#### 6.2.9 Ntfy.sh
```yaml
  - name: "ntfy-push"
    type: ntfy
    ntfy:
      server: "https://ntfy.sh"   # or self-hosted
      topic: "anubis-alerts"
      priority_map:
        critical: "urgent"
        warning: "high"
        info: "default"
      auth:
        username: "${NTFY_USER}"
        password: "${NTFY_PASS}"
```

### 6.3 Escalation Policies

```yaml
escalation:
  - name: "production-escalation"
    stages:
      - wait: 0s
        channels: ["ops-slack", "ntfy-push"]
      - wait: 5m
        channels: ["pagerduty-oncall"]
        condition: "not_acknowledged"
      - wait: 15m
        channels: ["sms-oncall", "ops-email"]
        condition: "not_acknowledged"
      - wait: 30m
        channels: ["management-email"]
        condition: "not_resolved"
```

---

## 7. STORAGE (CobaltDB Integration)

### 7.1 Data Model

CobaltDB stores all AnubisWatch data with the following key namespaces:

```
Namespace                    Description
─────────────────────────────────────────────────────
{ws}/souls/{id}             Monitor definitions
{ws}/judgments/{soul}/{ts}  Check results (time-series)
{ws}/verdicts/{id}          Alert history
{ws}/journeys/{id}          Synthetic journey definitions
{ws}/journey-runs/{id}/{ts} Journey execution results
{ws}/channels/{id}          Alert channel configs
{ws}/rules/{id}             Alert rule definitions
{ws}/feathers/{id}          Performance budget definitions
raft/log/{index}            Raft log entries
raft/state                  Raft persistent state
raft/snapshot/{id}          Raft snapshots
system/tenants/{id}         Tenant/workspace definitions
system/jackals/{id}         Cluster node registry
system/config               Global configuration
```

Where `{ws}` = workspace UUID for multi-tenant isolation.

### 7.2 Time-Series Optimization

```go
// TimeSeriesConfig configures CobaltDB for time-series workloads
type TimeSeriesConfig struct {
    // Compaction merges old data points into downsampled summaries
    Compaction CompactionPolicy `yaml:"compaction"`
    
    // Retention defines how long to keep data at each resolution
    Retention RetentionPolicy `yaml:"retention"`
}

type CompactionPolicy struct {
    // Raw data → 1-minute summaries after 48 hours
    RawToMinute Duration `yaml:"raw_to_minute"` // default: 48h
    
    // 1-minute → 5-minute summaries after 7 days
    MinuteToFive Duration `yaml:"minute_to_five"` // default: 7d
    
    // 5-minute → 1-hour summaries after 30 days
    FiveToHour Duration `yaml:"five_to_hour"` // default: 30d
    
    // 1-hour → 1-day summaries after 365 days
    HourToDay Duration `yaml:"hour_to_day"` // default: 365d
}

type RetentionPolicy struct {
    Raw     Duration `yaml:"raw"`     // default: 48h
    Minute  Duration `yaml:"minute"`  // default: 30d
    FiveMin Duration `yaml:"five"`    // default: 90d
    Hour    Duration `yaml:"hour"`    // default: 365d
    Day     Duration `yaml:"day"`     // default: unlimited
}
```

### 7.3 Downsampled Summary

Each downsampled point stores:
- `min`, `max`, `avg`, `p50`, `p95`, `p99` latency
- `total_checks`, `success_count`, `failure_count`
- `uptime_percent` (Purity score)
- `packet_loss_avg` (for ICMP)

---

## 8. WEB DASHBOARD (Hall of Ma'at)

### 8.1 Technology Stack

| Layer | Technology |
|---|---|
| Framework | React 19 |
| Build | Vite 6 |
| CSS | Tailwind CSS 4.1 |
| Components | shadcn/ui |
| Icons | Lucide React |
| Charts | Recharts / Custom SVG |
| State | Zustand |
| Real-time | WebSocket (native) |
| Router | React Router 7 / TanStack Router |
| Forms | React Hook Form + Zod |
| Embed | Go `embed.FS` (compiled into binary) |

### 8.2 Pages & Components

#### 8.2.1 Hall of Ma'at (Main Dashboard)
- Global overview: total souls, alive/dead/degraded counts
- Uptime heatmap grid (GitHub contribution graph style)
- Active incidents (Curses) panel
- Response time sparklines
- Regional map with Jackal status
- Real-time WebSocket updates (heartbeat animation)

#### 8.2.2 Souls Management
- CRUD interface for monitors
- Protocol-specific configuration forms
- Tag/group management
- Bulk import/export (YAML)
- Quick-add wizard

#### 8.2.3 Soul Detail View
- Current status with EKG-style heartbeat line
- Response time chart (1h, 24h, 7d, 30d, 90d)
- Uptime percentage (Purity) over time
- Incident (Curse) timeline
- TLS certificate details
- Multi-region comparison (per-Jackal results)
- Raw judgment log

#### 8.2.4 Book of the Dead (Public Status Page)
- Shareable URL per workspace
- Customizable branding (logo, colors, name)
- Component groups (e.g., "API", "Website", "Database")
- Incident history with updates
- Uptime bars (90-day view)
- Subscribe to updates (email, RSS, webhook)
- Optional password protection
- Custom domain support (CNAME)
- Embeddable badge/widget

#### 8.2.5 Grafana-Style Custom Dashboards
- Drag-and-drop widget placement
- Widget types: line chart, bar chart, gauge, stat, table, heatmap, map
- Query builder (select soul, metric, aggregation, time range)
- Dashboard templates (prebuilt layouts)
- Auto-refresh with configurable interval
- Dashboard sharing (link, embed, PDF export)

#### 8.2.6 Necropolis (Cluster Management)
- Node (Jackal) list with status, region, load
- Raft state visualization (leader, followers, candidates)
- Check distribution viewer
- Add/remove nodes
- Region map

#### 8.2.7 Settings & Configuration
- Workspace settings
- Team member management (RBAC)
- Alert channel configuration
- API key management
- Billing/quota (for SaaS mode)
- Theme customization (dark/light + custom)

### 8.3 Design System

**Color Palette (Egyptian Theme):**
```css
:root {
  /* Primary — Anubis Gold */
  --anubis-gold-50: #FFF9E6;
  --anubis-gold-500: #D4A843;
  --anubis-gold-900: #8B6914;
  
  /* Secondary — Nile Blue */
  --nile-blue-50: #E8F4FD;
  --nile-blue-500: #2563EB;
  --nile-blue-900: #1E3A5F;
  
  /* Accent — Papyrus Sand */
  --papyrus-50: #FEFCF3;
  --papyrus-500: #D4C5A0;
  --papyrus-900: #8B7D5E;
  
  /* Status Colors */
  --soul-alive: #22C55E;         /* Green — Aaru (paradise) */
  --soul-dead: #EF4444;          /* Red — Ammit's jaw */
  --soul-degraded: #F59E0B;      /* Amber — Heavy heart */
  --soul-embalmed: #8B5CF6;      /* Purple — Maintenance */
  --soul-unknown: #6B7280;       /* Gray — Not judged */
  
  /* Dark Theme — Tomb Interior */
  --tomb-bg: #0C0A09;
  --tomb-surface: #1C1917;
  --tomb-border: #292524;
  --tomb-text: #FAFAF9;
  
  /* Light Theme — Desert Sun */
  --desert-bg: #FEFCE8;
  --desert-surface: #FFFFFF;
  --desert-border: #E5E7EB;
  --desert-text: #1C1917;
}
```

### 8.4 Mobile Responsive PWA

- Service Worker for offline dashboard access
- Push notifications (via Ntfy.sh or native)
- Add to home screen
- Responsive breakpoints: mobile (< 640px), tablet (640-1024px), desktop (> 1024px)
- Touch-optimized interactions

---

## 9. API & INTEGRATIONS

### 9.1 REST API

Base URL: `https://<host>:8443/api/v1`

**Authentication:**
- API Key via `X-Anubis-Key` header
- JWT Bearer token (from dashboard login)
- Workspace scoped

**Endpoints:**

```
# Souls (Monitors)
GET    /souls                      List all monitors
POST   /souls                      Create monitor
GET    /souls/:id                  Get monitor details
PUT    /souls/:id                  Update monitor
DELETE /souls/:id                  Delete monitor
POST   /souls/:id/pause            Pause (Embalm)
POST   /souls/:id/resume           Resume
POST   /souls/:id/judge            Trigger immediate check

# Judgments (Check Results)
GET    /souls/:id/judgments         Get check history
GET    /souls/:id/judgments/latest  Get latest result
GET    /souls/:id/purity            Get uptime stats

# Journeys (Synthetic Checks)
GET    /journeys                    List journeys
POST   /journeys                    Create journey
GET    /journeys/:id                Get journey
PUT    /journeys/:id                Update journey
DELETE /journeys/:id                Delete journey
POST   /journeys/:id/run            Trigger immediate run
GET    /journeys/:id/runs            Get run history

# Verdicts (Alerts)
GET    /verdicts                    List alerts
GET    /verdicts/:id                Get alert details
POST   /verdicts/:id/acknowledge    Acknowledge alert
POST   /verdicts/:id/resolve        Resolve alert

# Channels (Alert Channels)
GET    /channels                    List channels
POST   /channels                    Create channel
PUT    /channels/:id                Update channel
DELETE /channels/:id                Delete channel
POST   /channels/:id/test           Send test notification

# Necropolis (Cluster)
GET    /necropolis                  Cluster status
GET    /necropolis/jackals          List nodes
POST   /necropolis/jackals          Add node (summon)
DELETE /necropolis/jackals/:id      Remove node (banish)
GET    /necropolis/raft              Raft state

# Status Page (Book of the Dead)
GET    /book                        Get status page config
PUT    /book                        Update status page
GET    /book/public                 Public status page data

# Tenants (Workspaces)
GET    /tenants                     List workspaces
POST   /tenants                     Create workspace
PUT    /tenants/:id                 Update workspace
DELETE /tenants/:id                 Delete workspace

# System
GET    /health                      API health check
GET    /metrics                     Prometheus metrics
GET    /version                     Version info
```

### 9.2 gRPC API

Protobuf definitions mirror REST API functionality. Key services:

```protobuf
service AnubisWatch {
  rpc ListSouls(ListSoulsRequest) returns (ListSoulsResponse);
  rpc CreateSoul(CreateSoulRequest) returns (Soul);
  rpc JudgeSoul(JudgeSoulRequest) returns (Judgment);
  rpc StreamJudgments(StreamRequest) returns (stream Judgment);
  rpc GetClusterStatus(Empty) returns (NecropolisStatus);
}
```

### 9.3 WebSocket API

```
ws://<host>:8443/ws/v1

Events (server → client):
  judgment.new       — New check result
  verdict.fired      — Alert triggered
  verdict.resolved   — Alert resolved
  soul.status_change — Monitor status changed
  jackal.joined      — New node joined cluster
  jackal.left        — Node left cluster
  raft.leader_change — New leader elected

Commands (client → server):
  subscribe          — Subscribe to specific soul/event types
  unsubscribe        — Unsubscribe
  ping               — Keep-alive
```

### 9.4 MCP Server Integration

AnubisWatch exposes an MCP server for AI agent integration:

**Tools:**
```
anubis_list_souls        — List all monitored targets
anubis_get_soul_status   — Get current status of a target
anubis_create_soul       — Create a new monitor
anubis_delete_soul       — Remove a monitor
anubis_trigger_judgment  — Force an immediate check
anubis_get_uptime        — Get uptime statistics
anubis_list_incidents    — List active incidents
anubis_acknowledge_alert — Acknowledge an alert
anubis_cluster_status    — Get cluster health
anubis_add_node          — Add a probe node
```

**Resources:**
```
anubis://souls             — All monitor configurations
anubis://souls/{id}        — Single monitor with history
anubis://judgments/latest   — Latest check results
anubis://verdicts/active    — Active alerts
anubis://necropolis         — Cluster topology
anubis://book               — Status page data
```

### 9.5 Prometheus Metrics Export

```
GET /metrics

# Metrics:
anubis_soul_status{soul="...", region="..."}           # 1=alive, 0=dead
anubis_soul_latency_seconds{soul="...", region="..."}  # Check duration
anubis_soul_uptime_ratio{soul="..."}                   # 0.0-1.0
anubis_judgments_total{soul="...", status="..."}        # Check count by status
anubis_verdicts_total{severity="..."}                  # Alert count by severity
anubis_cluster_nodes                                   # Number of nodes
anubis_cluster_leader{node="..."}                      # Current leader
anubis_raft_term                                       # Current Raft term
anubis_raft_commit_index                               # Raft commit index
```

---

## 10. CLI (Command Line Interface)

### 10.1 CLI Commands

All CLI commands use Egyptian mythology verbs:

```bash
# Initialization & Configuration
anubis init                          # Initialize new AnubisWatch instance
anubis init --cluster                # Initialize as cluster node
anubis config                        # Show current configuration
anubis config set <key> <value>      # Set configuration value

# Soul Management (Monitors)
anubis watch <target>                # Quick-add a monitor
anubis watch https://api.com --interval 30s --name "API"
anubis souls                         # List all monitors
anubis souls add <yaml-file>         # Add from YAML
anubis souls remove <name-or-id>     # Remove monitor
anubis souls import <file>           # Bulk import
anubis souls export                  # Export all as YAML

# Judgment (Check Execution)
anubis judge                         # Show all current verdicts (status table)
anubis judge <soul-name>             # Force-check a specific soul
anubis judge --all                   # Force-check all souls now
anubis judge --journey <name>        # Run a synthetic journey

# Cluster Management (Necropolis)
anubis summon <address>              # Add node to cluster
anubis banish <node-id>              # Remove node from cluster
anubis necropolis                    # Show cluster status
anubis necropolis status             # Detailed Raft state

# Alert Testing
anubis verdict test <channel>        # Send test alert to channel
anubis verdict history               # Show alert history
anubis verdict ack <id>              # Acknowledge alert

# Status Page
anubis book                          # Show status page URL
anubis book generate                 # Force regenerate status page

# System
anubis serve                         # Start AnubisWatch server
anubis serve --single                # Single node mode (no Raft)
anubis version                       # Show version
anubis health                        # Self health check
anubis migrate                       # Run storage migrations
```

### 10.2 CLI Output Style

```bash
$ anubis judge

  ⚖️  AnubisWatch — The Judgment Never Sleeps
  ────────────────────────────────────────────

  Soul                    Status    Latency   Region      Last Judged
  ──────────────────────  ────────  ────────  ──────────  ───────────
  Production API          ✅ Alive   42ms     eu-west     2s ago
  Payment Service         ✅ Alive  128ms     eu-west     15s ago
  Main Website            ⚠️ Degraded 2.1s    us-east     8s ago
  Mail Server (SMTP)      ✅ Alive   89ms     eu-west     45s ago
  DNS (Primary)           ✅ Alive   12ms     eu-west     30s ago
  Staging API             💀 Dead    —        us-east     3s ago
  Redis Cache             ✅ Alive    3ms     eu-west     20s ago
  WebSocket Feed          🔧 Embalmed —       —           —

  Purity: 85.7% (6/7 alive) · 1 Curse active · 1 Embalmed
  Necropolis: 3 Jackals · Leader: jackal-eu-01 · Term: 42
```

---

## 11. CONFIGURATION

### 11.1 Configuration File

Default location: `/etc/anubis/anubis.yaml` or `./anubis.yaml`

```yaml
# AnubisWatch Configuration
# ═══════════════════════════

# Server settings
server:
  host: "0.0.0.0"
  port: 8443
  tls:
    enabled: true
    cert: "/etc/anubis/tls/cert.pem"
    key: "/etc/anubis/tls/key.pem"
    auto_cert: true                # ACME/Let's Encrypt auto
    acme_email: "admin@example.com"
    acme_domains: ["anubis.example.com"]

# Storage (CobaltDB)
storage:
  path: "/var/lib/anubis/data"
  encryption:
    enabled: true
    key: "${ANUBIS_ENCRYPTION_KEY}"  # AES-256-GCM
  timeseries:
    compaction:
      raw_to_minute: 48h
      minute_to_five: 7d
      five_to_hour: 30d
      hour_to_day: 365d
    retention:
      raw: 48h
      minute: 30d
      five: 90d
      hour: 365d
      day: unlimited

# Cluster (Necropolis)
necropolis:
  enabled: true                    # false = single node mode
  node_name: "jackal-eu-01"
  region: "eu-west"
  bind_addr: "0.0.0.0:7946"
  advertise_addr: "203.0.113.10:7946"
  cluster_secret: "${ANUBIS_CLUSTER_SECRET}"
  
  discovery:
    mode: "gossip"                 # mdns | gossip | manual
    seeds:
      - "jackal-us-01.example.com:7946"
      - "jackal-apac-01.example.com:7946"
  
  raft:
    election_timeout: 1000ms
    heartbeat_timeout: 300ms
    snapshot_interval: 300s
    snapshot_threshold: 8192
  
  distribution:
    strategy: "region-aware"
    redundancy: 2

# Multi-tenant
tenants:
  enabled: false                   # true for SaaS mode
  default_quotas:
    max_souls: 100
    max_journeys: 20
    check_interval_min: 30s
    retention_days: 90

# Authentication
auth:
  type: "local"                    # local | oidc | ldap
  local:
    admin_email: "admin@example.com"
    admin_password: "${ANUBIS_ADMIN_PASSWORD}"
  oidc:
    issuer: "https://auth.example.com"
    client_id: "${OIDC_CLIENT_ID}"
    client_secret: "${OIDC_CLIENT_SECRET}"

# Dashboard
dashboard:
  enabled: true
  branding:
    title: "AnubisWatch"
    logo: ""                       # custom logo path
    theme: "auto"                  # auto | dark | light

# Souls (defined here or via API/dashboard)
souls:
  - name: "Production API"
    type: http
    target: "https://api.example.com/health"
    weight: 30s
    timeout: 10s
    tags: ["production", "api"]
    http:
      method: GET
      valid_status: [200]
      json_path:
        "$.status": "ok"
      feather: 500ms

# Alert Channels
channels:
  - name: "ops-slack"
    type: slack
    slack:
      webhook_url: "${SLACK_WEBHOOK}"

# Alert Rules
verdicts:
  rules:
    - name: "Default Down Alert"
      condition:
        type: consecutive_failures
        threshold: 3
      severity: critical
      channels: ["ops-slack"]

# Performance Budgets (Feathers)
feathers:
  - name: "API Performance"
    scope: "tag:api"
    rules:
      p95: 500ms
    window: 5m

# Synthetic Journeys
journeys: []

# Logging
logging:
  level: "info"                    # debug | info | warn | error
  format: "json"                   # json | text
  output: "stdout"                 # stdout | file
  file: "/var/log/anubis/anubis.log"

# Environment variable expansion
# All values support ${ENV_VAR} syntax with optional ${ENV_VAR:-default}
```

### 11.2 Environment Variables

```
ANUBIS_CONFIG              Config file path (default: ./anubis.yaml)
ANUBIS_HOST                Server bind host
ANUBIS_PORT                Server bind port
ANUBIS_ENCRYPTION_KEY      CobaltDB encryption key
ANUBIS_CLUSTER_SECRET      Raft cluster secret
ANUBIS_ADMIN_PASSWORD      Initial admin password
ANUBIS_LOG_LEVEL           Log level
ANUBIS_DATA_DIR            Data directory path
```

---

## 12. DEPLOYMENT

### 12.1 Single Binary

```bash
# Download
curl -fsSL https://anubis.watch/install.sh | sh

# Or from GitHub releases
wget https://github.com/AnubisWatch/anubiswatch/releases/latest/download/anubis-linux-amd64
chmod +x anubis-linux-amd64
mv anubis-linux-amd64 /usr/local/bin/anubis

# Quick start
anubis init
anubis watch https://mysite.com
anubis serve
```

### 12.2 Docker

```dockerfile
FROM scratch
COPY anubis /anubis
EXPOSE 8443 7946 9090
VOLUME /var/lib/anubis
ENTRYPOINT ["/anubis"]
CMD ["serve"]
```

```bash
docker run -d \
  --name anubis \
  -p 8443:8443 \
  -v anubis-data:/var/lib/anubis \
  anubiswatch/anubis:latest
```

### 12.3 Docker Compose (3-Node Cluster)

```yaml
version: "3.9"
services:
  jackal-1:
    image: anubiswatch/anubis:latest
    command: serve --cluster
    environment:
      - ANUBIS_NODE_NAME=jackal-1
      - ANUBIS_REGION=eu-west
      - ANUBIS_CLUSTER_SECRET=mysecret
    ports:
      - "8443:8443"
    volumes:
      - jackal1-data:/var/lib/anubis

  jackal-2:
    image: anubiswatch/anubis:latest
    command: serve --cluster --join jackal-1:7946
    environment:
      - ANUBIS_NODE_NAME=jackal-2
      - ANUBIS_REGION=us-east
      - ANUBIS_CLUSTER_SECRET=mysecret
    volumes:
      - jackal2-data:/var/lib/anubis

  jackal-3:
    image: anubiswatch/anubis:latest
    command: serve --cluster --join jackal-1:7946
    environment:
      - ANUBIS_NODE_NAME=jackal-3
      - ANUBIS_REGION=apac
      - ANUBIS_CLUSTER_SECRET=mysecret
    volumes:
      - jackal3-data:/var/lib/anubis

volumes:
  jackal1-data:
  jackal2-data:
  jackal3-data:
```

### 12.4 Systemd Service

```ini
[Unit]
Description=AnubisWatch — The Judgment Never Sleeps
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=anubis
Group=anubis
ExecStart=/usr/local/bin/anubis serve
Restart=always
RestartSec=5
LimitNOFILE=65535
AmbientCapabilities=CAP_NET_RAW CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
```

### 12.5 Supported Platforms

| Platform | Architecture | Binary |
|---|---|---|
| Linux | amd64, arm64, armv7 | ✅ |
| macOS | amd64 (Intel), arm64 (Apple Silicon) | ✅ |
| Windows | amd64 | ✅ |
| FreeBSD | amd64 | ✅ |
| Docker | multi-arch | ✅ |
| Kubernetes | Helm chart | ✅ |

### 12.6 Resource Requirements

| Mode | CPU | RAM | Disk |
|---|---|---|---|
| Single (< 50 monitors) | 1 vCPU | 64MB | 100MB |
| Single (< 500 monitors) | 2 vCPU | 256MB | 1GB |
| Cluster node | 2 vCPU | 512MB | 5GB |
| SaaS (multi-tenant) | 4+ vCPU | 2GB+ | 50GB+ |
| Raspberry Pi (ARM) | 1 core | 64MB | 500MB |

---

## 13. SECURITY

### 13.1 Authentication & Authorization

- **Local Auth:** bcrypt-hashed passwords, JWT sessions
- **OIDC:** OpenID Connect (Google, GitHub, Okta, Keycloak)
- **LDAP:** Active Directory / LDAP bind
- **API Keys:** Scoped, rotatable, workspace-bound
- **RBAC Roles:** Admin, Editor, Viewer (per workspace)

### 13.2 Encryption

- **Data at Rest:** AES-256-GCM via CobaltDB encryption layer
- **Data in Transit:** TLS 1.3 for all inter-node communication
- **Raft Transport:** Mutual TLS between cluster nodes
- **Secrets:** Environment variable expansion, no plaintext secrets in config
- **API Keys:** SHA-256 hashed in storage

### 13.3 Network Security

- **Rate Limiting:** Per-IP and per-API-key rate limits
- **CORS:** Configurable allowed origins
- **CSP:** Content Security Policy headers for dashboard
- **ICMP Privilege:** Requires `CAP_NET_RAW` (Linux capability, not root)

---

## 14. DEPENDENCY POLICY

### 14.1 Allowed Dependencies (Extended Stdlib)

| Dependency | Justification |
|---|---|
| `golang.org/x/crypto` | TLS, bcrypt, SSH, cryptographic primitives |
| `golang.org/x/sys` | Low-level system calls (ICMP raw sockets) |
| `golang.org/x/net` | HTTP/2, ICMP, network utilities |
| YAML parser (`gopkg.in/yaml.v3`) | Configuration file parsing |

### 14.2 Everything Else is Built From Scratch

- **Raft Consensus** — Custom implementation
- **CobaltDB** — Own embedded database
- **HTTP Router** — Custom (no gin, chi, echo)
- **WebSocket** — Custom implementation over `net/http`
- **gRPC** — Custom implementation or protobuf-only
- **mDNS/Gossip** — Custom implementation
- **SMTP Client** — Custom (for email alerts)
- **DNS Client** — Custom (for DNS checks)
- **Template Engine** — Go `text/template` + `html/template`
- **JSON Schema Validator** — Custom implementation
- **Prometheus Exporter** — Custom `/metrics` endpoint

---

## 15. BRANDING

### 15.1 Visual Identity

- **Logo:** Jackal head silhouette (Anubis) with EKG heartbeat line through it
- **Primary Color:** Anubis Gold (#D4A843)
- **Secondary Color:** Nile Blue (#2563EB)
- **Accent Color:** Papyrus Sand (#D4C5A0)
- **Dark Theme:** Tomb Interior palette
- **Light Theme:** Desert Sun palette
- **Font:** Inter (UI) + JetBrains Mono (code/terminal)

### 15.2 Taglines

- Primary: **"The Judgment Never Sleeps"**
- Technical: **"Weighing Your Uptime"**
- Short: **"The Uptime Judge"**
- CLI: **"Every heartbeat, judged."**

### 15.3 Mascot

An **stylized Anubis jackal** in a modern, geometric art style — Egyptian authority meets modern tech. Holding a weighing scale with a server/heart on one side and a feather (Ma'at) on the other.

---

## 16. PROJECT METADATA

| Field | Value |
|---|---|
| **Name** | AnubisWatch |
| **Binary** | `anubis` |
| **Repository** | github.com/AnubisWatch/anubiswatch |
| **Organization** | github.com/AnubisWatch |
| **Domains** | anubis.watch · anubiswatch.com |
| **License** | Apache 2.0 (core) |
| **Language** | Go 1.24+ |
| **Go Module** | `github.com/AnubisWatch/anubiswatch` |
| **Docker Image** | `anubiswatch/anubis` |
| **CLI Binary** | `anubis` |
| **Default Port** | 8443 (HTTPS) |
| **Raft Port** | 7946 |
| **gRPC Port** | 9090 |
| **Min Go Version** | 1.24 |
| **Author** | Ersin Koç — ECOSTACK TECHNOLOGY OÜ |
| **Country** | Estonia |

---

## 17. COMPETITIVE POSITIONING

```
                        Self-Hosted    Multi-Proto    Distributed    Synthetic    Single Binary
                        ───────────    ───────────    ───────────    ─────────    ─────────────
UptimeRobot             ❌ SaaS        ❌ HTTP only   ❌ No          ❌ No        ❌ N/A
Pingdom                 ❌ SaaS        ⚠️ Limited     ❌ No          ❌ No        ❌ N/A
Uptime Kuma             ✅ Yes         ⚠️ Some        ❌ No          ❌ No        ❌ Node.js
Checkly                 ❌ SaaS        ⚠️ HTTP+API    ⚠️ SaaS only  ✅ Yes       ❌ N/A
Grafana Synthetic       ❌ SaaS        ⚠️ Limited     ✅ Yes         ✅ Yes       ❌ N/A
AnubisWatch             ✅ Yes         ✅ 8 protocols ✅ Raft        ✅ Yes       ✅ Zero deps
```

---

*The Judgment Never Sleeps* ⚖️
