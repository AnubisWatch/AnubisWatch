# AnubisWatch — TASKS.md

> **Development Task Breakdown**
> **Version:** 1.0.0 · **Date:** 2026-03-30

---

## Phase 1 — Foundation (Week 1-2)

### 1.1 Project Bootstrap
- [ ] Initialize Go module: `github.com/AnubisWatch/anubiswatch`
- [ ] Add allowed dependencies: `golang.org/x/crypto`, `golang.org/x/sys`, `golang.org/x/net`, `gopkg.in/yaml.v3`
- [ ] Create directory structure per SPECIFICATION.md Section 2.3
- [ ] Create Makefile with build, test, lint, cross-compile, docker targets
- [ ] Create Dockerfile (FROM scratch, multi-arch)
- [ ] Create `.github/workflows/ci.yml` (test + lint on PR)
- [ ] Create `.github/workflows/release.yml` (cross-compile + Docker on tag)
- [ ] Create `.golangci.yml` linter config
- [ ] Create `LICENSE` (Apache 2.0)
- [ ] Create initial `README.md` with badge, description, quick start

### 1.2 Core Types
- [ ] Implement `internal/core/soul.go` — Soul, SoulStatus, CheckType, all protocol config structs
- [ ] Implement `internal/core/judgment.go` — Judgment, JudgmentDetails, TLSInfo, AssertionResult
- [ ] Implement `internal/core/verdict.go` — Verdict, Severity, VerdictStatus
- [ ] Implement `internal/core/journey.go` — JourneyConfig, JourneyStep, extraction rules
- [ ] Implement `internal/core/channel.go` — ChannelConfig, all channel-specific config structs
- [ ] Implement `internal/core/feather.go` — FeatherConfig (performance budgets)
- [ ] Implement `internal/core/errors.go` — AppError interface, NotFoundError, ConfigError, ValidationError
- [ ] Implement `internal/core/id.go` — ULID generator (custom, no external dep)
- [ ] Write unit tests for all core types

### 1.3 Configuration
- [ ] Implement `internal/core/config.go` — Full Config struct tree
- [ ] Implement YAML parsing with `gopkg.in/yaml.v3`
- [ ] Implement environment variable expansion (`${VAR}` and `${VAR:-default}`)
- [ ] Implement config defaults (setDefaults)
- [ ] Implement config validation (validate)
- [ ] Implement custom Duration YAML marshaling/unmarshaling
- [ ] Write unit tests for config parsing, env expansion, validation
- [ ] Create `configs/anubis.example.yaml` with full documented example

### 1.4 CobaltDB Storage Integration
- [ ] Implement `internal/storage/engine.go` — CobaltDB wrapper, open/close, key namespace management
- [ ] Implement Soul CRUD operations (save, get, list, delete with prefix scan)
- [ ] Implement Judgment storage (append, query by time range, latest)
- [ ] Implement Verdict storage (save, get, list, update status)
- [ ] Implement Journey storage (CRUD)
- [ ] Implement Channel config storage
- [ ] Implement System config storage (jackals, tenants)
- [ ] Implement `internal/storage/timeseries.go` — time-series key format, range queries
- [ ] Implement `internal/storage/retention.go` — background retention purge goroutine
- [ ] Write integration tests for all storage operations
- [ ] Write benchmark tests for write/read throughput

### 1.5 CLI Entrypoint
- [ ] Implement `cmd/anubis/main.go` — command routing (serve, init, watch, judge, summon, banish, necropolis, version, health)
- [ ] Implement `anubis version` — print version, commit, build date
- [ ] Implement `anubis init` — generate default anubis.yaml
- [ ] Implement `anubis health` — self health check
- [ ] Implement CLI output formatting (colored, themed output per SPEC Section 10.2)
- [ ] Implement `anubis serve --single` for single node mode

---

## Phase 2 — Probe Engine (Week 3-5)

### 2.1 Probe Engine Core
- [ ] Implement `internal/probe/checker.go` — Checker interface, CheckerRegistry
- [ ] Implement `internal/probe/engine.go` — Engine with scheduler, per-soul goroutine management
- [ ] Implement AssignSouls (add/remove/update running checks)
- [ ] Implement TriggerImmediate (force-check a soul)
- [ ] Implement graceful Stop with WaitGroup
- [ ] Write unit tests for engine lifecycle, assignment changes

### 2.2 HTTP/HTTPS Checker
- [ ] Implement full HTTP checker: GET, POST, PUT, DELETE, HEAD, OPTIONS
- [ ] Implement status code assertion (exact, range, list)
- [ ] Implement body contains assertion
- [ ] Implement body regex assertion
- [ ] Implement JSON path extraction and assertion (custom `$.key.subkey` parser)
- [ ] Implement JSON Schema validation (draft 2020-12 subset: type, required, properties, items, enum, pattern, minimum, maximum)
- [ ] Implement response header assertion
- [ ] Implement Feather (performance budget) assertion
- [ ] Implement redirect handling (follow/no-follow, max redirects)
- [ ] Implement custom request headers and body
- [ ] Implement TLS info extraction from response
- [ ] Implement HTTP/2 support
- [ ] Implement User-Agent: `AnubisWatch/1.0 (The Judgment Never Sleeps)`
- [ ] Write comprehensive unit tests (mock HTTP server)

### 2.3 TCP/UDP Checker
- [ ] Implement TCP connect check with timeout
- [ ] Implement TCP banner grab (read first bytes)
- [ ] Implement TCP send/expect (send payload, assert response)
- [ ] Implement TCP regex matching on banner
- [ ] Implement UDP send/receive with timeout
- [ ] Implement hex payload support for UDP
- [ ] Write unit tests (mock TCP/UDP servers)

### 2.4 DNS Checker
- [ ] Implement DNS resolution using custom UDP dialer (net.Resolver with custom Dial)
- [ ] Implement record type support: A, AAAA, CNAME, MX, TXT, NS, SOA, SRV, PTR, CAA
- [ ] Implement expected value assertion
- [ ] Implement multi-nameserver propagation check
- [ ] Implement propagation percentage calculation
- [ ] Implement DNSSEC validation (raw DNS packet with DO bit, RRSIG/DNSKEY chain) — can be Phase 7 stretch
- [ ] Write unit tests (mock DNS responses)

### 2.5 SMTP/IMAP Checker
- [ ] Implement SMTP connect + EHLO handshake
- [ ] Implement STARTTLS upgrade verification
- [ ] Implement SMTP AUTH test
- [ ] Implement IMAP connect + LOGIN
- [ ] Implement IMAP mailbox status check
- [ ] Implement banner/capability assertion
- [ ] Implement TLS info extraction
- [ ] Write unit tests (mock SMTP/IMAP servers)

### 2.6 ICMP Ping Checker
- [ ] Implement ICMP Echo Request/Reply (IPv4)
- [ ] Implement ICMP Echo Request/Reply (IPv6)
- [ ] Implement configurable packet count and interval
- [ ] Implement packet loss percentage calculation
- [ ] Implement min/avg/max/jitter latency calculation
- [ ] Implement privileged (raw socket) and unprivileged (UDP) modes
- [ ] Implement max_loss_percent threshold
- [ ] Implement Feather (latency budget)
- [ ] Write unit tests (requires CAP_NET_RAW or unprivileged mode)

### 2.7 gRPC Health Checker
- [ ] Implement gRPC Health Checking Protocol client (grpc.health.v1.Health/Check)
- [ ] Implement using raw HTTP/2 frames + protobuf encoding (no google.golang.org/grpc)
- [ ] Implement service-specific health check
- [ ] Implement TLS/mTLS support
- [ ] Implement metadata/header injection
- [ ] Implement response time measurement
- [ ] Write unit tests (mock gRPC server)

### 2.8 WebSocket Checker
- [ ] Implement WebSocket upgrade handshake (RFC 6455)
- [ ] Implement custom WebSocket client over net/http Hijacker
- [ ] Implement message send + response assertion
- [ ] Implement subprotocol negotiation
- [ ] Implement custom upgrade headers
- [ ] Implement ping/pong frame validation
- [ ] Implement close code assertion
- [ ] Implement Feather (connection time budget)
- [ ] Write unit tests (mock WebSocket server)

### 2.9 TLS Certificate Checker
- [ ] Implement TLS handshake with crypto/tls
- [ ] Implement certificate expiry monitoring (days until expiry)
- [ ] Implement certificate chain validation
- [ ] Implement cipher suite audit (flag weak ciphers)
- [ ] Implement protocol version check (TLS 1.2/1.3 minimum)
- [ ] Implement SAN (Subject Alternative Name) validation
- [ ] Implement OCSP stapling check
- [ ] Implement key type and size validation
- [ ] Implement issuer validation
- [ ] Write unit tests (self-signed test certs)

### 2.10 Synthetic Monitoring (Duat Journeys)
- [ ] Implement `internal/probe/synthetic.go` — Journey executor
- [ ] Implement sequential step execution with context propagation
- [ ] Implement variable extraction from: body (JSON path), header (exact/regex), cookie
- [ ] Implement variable interpolation in subsequent steps (`${var_name}`)
- [ ] Implement continue_on_failure option
- [ ] Implement per-step and total journey timeout
- [ ] Implement journey result aggregation (all steps → single JourneyRun result)
- [ ] Write unit tests with multi-step mock server scenarios

---

## Phase 3 — Raft Consensus (Week 6-8)

### 3.1 Raft Core
- [ ] Implement `internal/raft/node.go` — NodeState (Follower/Candidate/Leader), state machine
- [ ] Implement Follower behavior (election timeout → become candidate)
- [ ] Implement Candidate behavior (vote request, majority → become leader)
- [ ] Implement Leader behavior (heartbeats, log replication)
- [ ] Implement AppendEntries RPC handler
- [ ] Implement RequestVote RPC handler
- [ ] Implement Pre-vote extension (prevent disruption from partitioned nodes)
- [ ] Implement randomized election timeouts using crypto/rand
- [ ] Write comprehensive unit tests for state transitions

### 3.2 Raft Log
- [ ] Implement `internal/raft/log.go` — Log backed by CobaltDB
- [ ] Implement Append, Get, LastInfo, EntriesFrom, TruncateFrom
- [ ] Implement persistent state storage (currentTerm, votedFor)
- [ ] Write unit tests for log operations

### 3.3 Raft Transport
- [ ] Implement `internal/raft/transport.go` — Transport interface
- [ ] Implement TCP transport with length-prefixed binary encoding
- [ ] Implement TLS mutual authentication for transport
- [ ] Implement connection pooling (reuse connections to peers)
- [ ] Implement message type routing (AppendEntries, RequestVote, InstallSnapshot)
- [ ] Write integration tests with multiple nodes

### 3.4 Raft Snapshots
- [ ] Implement `internal/raft/snapshot.go` — snapshot creation from CobaltDB state
- [ ] Implement InstallSnapshot RPC (leader sends snapshot to far-behind follower)
- [ ] Implement snapshot compaction (trim log after snapshot)
- [ ] Implement configurable snapshot interval and threshold

### 3.5 Auto-Discovery
- [ ] Implement `internal/raft/discovery.go` — Discovery interface
- [ ] Implement mDNS discovery (`_anubis._tcp.local`) for LAN
- [ ] Implement gossip discovery (SWIM-based) for WAN
- [ ] Implement manual mode (explicit peer list)
- [ ] Implement OnJoin/OnLeave callbacks for dynamic membership
- [ ] Write integration tests for each discovery mode

### 3.6 Check Distribution
- [ ] Implement leader-side check distribution algorithm
- [ ] Implement `round-robin` strategy
- [ ] Implement `region-aware` strategy (prefer local region)
- [ ] Implement `latency-optimized` strategy
- [ ] Implement `redundant` strategy (each soul checked by N jackals)
- [ ] Implement rebalancing on node join/leave
- [ ] Implement check assignment propagation via Raft log

### 3.7 Cluster CLI Commands
- [ ] Implement `anubis serve --cluster` mode
- [ ] Implement `anubis summon <address>` — add node
- [ ] Implement `anubis banish <node-id>` — remove node
- [ ] Implement `anubis necropolis` — cluster status display
- [ ] Implement `anubis necropolis status` — detailed Raft state

---

## Phase 4 — Alert System (Week 9-10)

### 4.1 Alert Dispatcher Core
- [ ] Implement `internal/alert/dispatcher.go` — Dispatcher with rule evaluation
- [ ] Implement AlertState tracking (consecutive failures, cooldowns, active verdicts)
- [ ] Implement consecutive_failures condition evaluator
- [ ] Implement threshold condition evaluator (metric comparison)
- [ ] Implement percentage condition evaluator (failure rate over window)
- [ ] Implement anomaly condition evaluator (deviation from baseline)
- [ ] Implement compound condition evaluator (AND/OR logic)
- [ ] Implement cooldown enforcement
- [ ] Implement recovery detection (Resurrection)
- [ ] Implement rule-to-soul matching (scope: all, tag:xxx, soul:xxx)
- [ ] Write unit tests for all condition types

### 4.2 Alert Channels
- [ ] Implement `internal/alert/webhook.go` — generic webhook with Go template rendering
- [ ] Implement `internal/alert/slack.go` — Slack webhook with Block Kit formatting
- [ ] Implement `internal/alert/discord.go` — Discord webhook with rich embeds
- [ ] Implement `internal/alert/telegram.go` — Telegram Bot API (sendMessage)
- [ ] Implement `internal/alert/email.go` — built-in SMTP client (custom, no net/smtp)
- [ ] Implement `internal/alert/pagerduty.go` — PagerDuty Events API v2 (trigger, acknowledge, resolve)
- [ ] Implement `internal/alert/opsgenie.go` — OpsGenie Alert API
- [ ] Implement `internal/alert/sms.go` — Twilio REST API for SMS
- [ ] Implement `internal/alert/ntfy.go` — Ntfy.sh HTTP API
- [ ] Implement retry logic for all HTTP-based channels (exponential backoff, max 3 attempts)
- [ ] Write unit tests for each channel (mock HTTP endpoints)

### 4.3 Escalation Policies
- [ ] Implement escalation stage sequencing (wait duration → fire channels)
- [ ] Implement condition checks (not_acknowledged, not_resolved)
- [ ] Implement escalation state tracking per active verdict
- [ ] Write unit tests for escalation flow

### 4.4 Alert CLI Commands
- [ ] Implement `anubis verdict test <channel>` — send test notification
- [ ] Implement `anubis verdict history` — show recent alerts
- [ ] Implement `anubis verdict ack <id>` — acknowledge alert

---

## Phase 5 — API Layer (Week 11-13)

### 5.1 REST API
- [ ] Implement `internal/api/rest/router.go` — custom HTTP router with param extraction
- [ ] Implement middleware: logging, CORS, rate limiting, authentication
- [ ] Implement `internal/api/rest/middleware.go` — JWT validation, API key validation
- [ ] Implement Souls endpoints: list, create, get, update, delete, pause, resume, judge
- [ ] Implement Judgments endpoints: list, latest, purity
- [ ] Implement Journeys endpoints: list, create, get, update, delete, run, runs
- [ ] Implement Verdicts endpoints: list, get, acknowledge, resolve
- [ ] Implement Channels endpoints: list, create, update, delete, test
- [ ] Implement Necropolis endpoints: cluster status, list jackals, summon, banish, raft state
- [ ] Implement Book of the Dead endpoints: get config, update, public data
- [ ] Implement Tenants endpoints: list, create, update, delete
- [ ] Implement System endpoints: health, version, Prometheus metrics
- [ ] Write API integration tests for all endpoints

### 5.2 WebSocket Server
- [ ] Implement `internal/api/ws/hub.go` — connection management, broadcast
- [ ] Implement custom WebSocket handshake (RFC 6455, no gorilla/websocket)
- [ ] Implement event types: judgment.new, verdict.fired, verdict.resolved, soul.status_change, jackal.joined, jackal.left, raft.leader_change
- [ ] Implement subscription filtering (subscribe to specific souls/event types)
- [ ] Implement ping/pong keep-alive
- [ ] Implement authentication on upgrade
- [ ] Write WebSocket integration tests

### 5.3 gRPC API
- [ ] Define protobuf schemas (proto/ directory)
- [ ] Implement custom gRPC server (HTTP/2 frames + protobuf, no google.golang.org/grpc)
- [ ] Implement ListSouls, CreateSoul, JudgeSoul, StreamJudgments, GetClusterStatus
- [ ] Write gRPC integration tests

### 5.4 MCP Server
- [ ] Implement `internal/api/mcp/server.go` — MCP protocol handler
- [ ] Implement tools: anubis_list_souls, anubis_get_soul_status, anubis_create_soul, anubis_delete_soul, anubis_trigger_judgment, anubis_get_uptime, anubis_list_incidents, anubis_acknowledge_alert, anubis_cluster_status, anubis_add_node
- [ ] Implement resources: anubis://souls, anubis://judgments/latest, anubis://verdicts/active, anubis://necropolis, anubis://book
- [ ] Implement stdio and HTTP/SSE transports
- [ ] Write MCP integration tests

### 5.5 Prometheus Metrics
- [ ] Implement `/metrics` endpoint with custom text format exporter
- [ ] Expose: soul_status, soul_latency, soul_uptime_ratio, judgments_total, verdicts_total, cluster_nodes, cluster_leader, raft_term, raft_commit_index
- [ ] Write metrics format tests

---

## Phase 6 — Dashboard (Week 14-17)

### 6.1 Frontend Setup
- [ ] Initialize React 19 + Vite 6 project in `web/`
- [ ] Configure Tailwind CSS 4.1 with Egyptian color palette
- [ ] Install and configure shadcn/ui components
- [ ] Install Lucide React icons
- [ ] Configure Zustand for state management
- [ ] Configure React Router 7 / TanStack Router
- [ ] Configure React Hook Form + Zod for form validation
- [ ] Set up WebSocket client hook for real-time updates
- [ ] Implement dark/light theme toggle (Tomb Interior / Desert Sun)

### 6.2 Hall of Ma'at (Main Dashboard)
- [ ] Global overview cards: total souls, alive/dead/degraded/embalmed counts
- [ ] Uptime heatmap grid (GitHub contribution graph style, 90-day view)
- [ ] Active incidents (Curses) panel with severity badges
- [ ] Response time sparklines per soul
- [ ] Regional map showing Jackal locations and status
- [ ] Real-time heartbeat animation via WebSocket

### 6.3 Souls Management
- [ ] Soul list with search, filter by tag/status/type, sort
- [ ] Create soul wizard (protocol selector → protocol-specific config form)
- [ ] Edit soul form with live validation
- [ ] Delete soul with confirmation
- [ ] Pause/resume (Embalm/Resurrect) toggle
- [ ] Bulk import from YAML
- [ ] Export all souls as YAML
- [ ] Tag management

### 6.4 Soul Detail View
- [ ] Current status with animated EKG heartbeat line
- [ ] Response time chart (1h, 24h, 7d, 30d, 90d range selector)
- [ ] Uptime percentage (Purity) over time chart
- [ ] Incident (Curse) timeline
- [ ] TLS certificate details panel (expiry countdown, chain, cipher)
- [ ] Multi-region comparison chart (per-Jackal latency overlay)
- [ ] Raw judgment log table with pagination

### 6.5 Book of the Dead (Public Status Page)
- [ ] Status page configuration form (branding, groups, components)
- [ ] Preview mode
- [ ] Public URL generation and display
- [ ] Component group management (drag-and-drop ordering)
- [ ] Incident post/update creation
- [ ] 90-day uptime bars per component
- [ ] Subscribe form (email, RSS, webhook)
- [ ] Embeddable badge/widget code generator
- [ ] Custom domain configuration (CNAME instructions)
- [ ] Optional password protection toggle

### 6.6 Grafana-Style Custom Dashboards
- [ ] Dashboard creation with grid layout (react-grid-layout or custom)
- [ ] Widget types: line chart, bar chart, gauge, stat number, table, heatmap, map
- [ ] Query builder: select soul → metric → aggregation → time range
- [ ] Widget resize and drag
- [ ] Dashboard template presets
- [ ] Auto-refresh with configurable interval selector
- [ ] Dashboard sharing: link, embed code, PDF export
- [ ] Dashboard save/load from storage

### 6.7 Necropolis (Cluster View)
- [ ] Node (Jackal) list: name, region, status, role, load, uptime
- [ ] Raft state visualization (leader indicator, term number, log index)
- [ ] Check distribution viewer (which soul → which jackal)
- [ ] Add/remove node forms
- [ ] Region map with node markers

### 6.8 Journeys (Synthetic Checks)
- [ ] Journey list with status indicators
- [ ] Journey builder (step-by-step form, variable extraction config)
- [ ] Journey run history with per-step breakdown
- [ ] Variable flow visualization (which step sets what variable)

### 6.9 Settings Pages
- [ ] Workspace settings
- [ ] Team member management with RBAC (Admin/Editor/Viewer)
- [ ] Alert channel configuration forms (per channel type)
- [ ] Alert rule builder (condition type → parameters → channels → severity)
- [ ] Escalation policy builder
- [ ] Performance budget (Feather) configuration
- [ ] API key management (create, rotate, revoke)
- [ ] Theme customization (dark/light toggle, accent color picker)
- [ ] Billing/quota display (for SaaS mode)

### 6.10 PWA & Mobile
- [ ] Create service worker for offline dashboard caching
- [ ] Create web app manifest (icons, theme color)
- [ ] Implement responsive breakpoints (mobile < 640, tablet 640-1024, desktop > 1024)
- [ ] Test touch interactions
- [ ] Test PWA install flow on iOS and Android

### 6.11 Dashboard Embedding
- [ ] Implement `internal/dashboard/embed.go` — embed.FS for compiled React build
- [ ] Implement SPA routing fallback (serve index.html for non-file routes)
- [ ] Implement cache headers for static assets
- [ ] Test embedded dashboard in single binary

---

## Phase 7 — Advanced Features (Week 18-20)

### 7.1 Multi-Tenant Isolation
- [ ] Implement `internal/tenant/workspace.go` — workspace CRUD
- [ ] Implement `internal/tenant/isolation.go` — CobaltDB key prefix enforcement
- [ ] Implement `internal/tenant/quota.go` — resource quota checking
- [ ] Implement workspace-scoped authentication
- [ ] Implement workspace-scoped API responses
- [ ] Write integration tests for cross-workspace isolation

### 7.2 Public Status Page Generator
- [ ] Implement `internal/statuspage/generator.go` — static HTML generator
- [ ] Create status page HTML templates (responsive, customizable)
- [ ] Implement component group rendering
- [ ] Implement incident history rendering
- [ ] Implement 90-day uptime bar rendering
- [ ] Implement subscriber management (email, RSS, webhook)
- [ ] Implement custom domain serving
- [ ] Implement embeddable badge endpoint

### 7.3 ACME/Let's Encrypt Auto-TLS
- [ ] Implement ACME client (custom, no golang.org/x/crypto/acme/autocert)
- [ ] Implement HTTP-01 challenge solver
- [ ] Implement certificate storage in CobaltDB
- [ ] Implement auto-renewal goroutine
- [ ] Test with Let's Encrypt staging

### 7.4 Authentication Providers
- [ ] Implement local auth (bcrypt + JWT)
- [ ] Implement OIDC provider integration (Google, GitHub, Okta)
- [ ] Implement LDAP bind authentication
- [ ] Implement session management
- [ ] Implement RBAC enforcement in API middleware

### 7.5 Full DNSSEC Validation
- [ ] Implement raw DNS packet construction with DO bit
- [ ] Implement RRSIG signature parsing
- [ ] Implement DNSKEY/DS trust chain walking
- [ ] Implement root → TLD → domain chain validation
- [ ] Write tests against known DNSSEC-signed domains

---

## Phase 8 — Polish & Release (Week 21-22)

### 8.1 Documentation
- [ ] Write comprehensive README.md (badges, features, screenshots, quick start)
- [ ] Write BRANDING.md (full branding guide)
- [ ] Write API documentation (OpenAPI/Swagger spec)
- [ ] Write deployment guide (Docker, Docker Compose, Kubernetes Helm chart, systemd)
- [ ] Write configuration reference
- [ ] Write contributor guide (CONTRIBUTING.md)
- [ ] Create website content for anubis.watch

### 8.2 Testing & Quality
- [ ] Achieve 80%+ code coverage
- [ ] Run all fuzz tests with sufficient corpus
- [ ] Perform load testing (1000+ monitors, 5-node cluster)
- [ ] Perform chaos testing (kill nodes, network partition)
- [ ] Security audit (input validation, TLS config, auth flow)
- [ ] Performance profiling (pprof, trace)

### 8.3 Release Artifacts
- [ ] Create install.sh (curl-pipe-sh installer)
- [ ] Create Homebrew formula
- [ ] Create Docker image (multi-arch: amd64, arm64, armv7)
- [ ] Create Kubernetes Helm chart
- [ ] Create systemd service file
- [ ] Create example Docker Compose (1-node, 3-node cluster)
- [ ] Create GitHub Release with changelog

### 8.4 Marketing & Launch
- [ ] Create product infographic (Nano Banana 2 prompt)
- [ ] Create X launch post (Turkish, developer-focused)
- [ ] Create X Article (deep dive into architecture)
- [ ] Create demo GIF/video (CLI + dashboard)
- [ ] Submit to Hacker News, Reddit r/selfhosted, r/golang
- [ ] Create comparison table vs competitors for README

---

## Task Priority Matrix

| Priority | Task | Dependency |
|---|---|---|
| P0 — Critical Path | Core types, Config, Storage, HTTP Checker, Probe Engine, CLI serve | None |
| P0 — Critical Path | REST API (souls, judgments), WebSocket, Dashboard (Hall of Ma'at, Soul detail) | Phase 1-2 |
| P1 — High | All 8 checkers complete, Alert dispatcher + Slack/Webhook/Email | Phase 2 core |
| P1 — High | Raft core (election, log replication, heartbeat) | Phase 1 storage |
| P2 — Medium | Synthetic Journeys, gRPC API, MCP Server | Phase 2-3 |
| P2 — Medium | Custom dashboards, Status page, Multi-tenant | Phase 5-6 |
| P3 — Low | DNSSEC full validation, OIDC/LDAP auth, Helm chart | Phase 7 |
| P3 — Low | PWA, PDF export, Anomaly detection | Phase 6 |

---

## Estimated Timeline

| Phase | Duration | Milestone |
|---|---|---|
| Phase 1 — Foundation | 2 weeks | Config loads, CobaltDB stores/retrieves, CLI boots |
| Phase 2 — Probe Engine | 3 weeks | All 8 checkers work, HTTP synthetic journeys |
| Phase 3 — Raft Cluster | 3 weeks | 3-node cluster elects leader, distributes checks |
| Phase 4 — Alert System | 2 weeks | Alerts fire to Slack/webhook/email on soul death |
| Phase 5 — API Layer | 3 weeks | Full REST API, WebSocket, MCP server operational |
| Phase 6 — Dashboard | 4 weeks | React dashboard embedded, all pages functional |
| Phase 7 — Advanced | 3 weeks | Multi-tenant, status page, ACME, auth providers |
| Phase 8 — Polish | 2 weeks | Docs, tests, release artifacts, launch |
| **Total** | **~22 weeks** | **v1.0.0 release** |

---

*Each task is a weighing. Each completion, a judgment passed.* ⚖️
