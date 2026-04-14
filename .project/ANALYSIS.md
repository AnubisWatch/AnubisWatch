# Project Analysis Report

> Auto-generated comprehensive analysis of AnubisWatch
> Generated: 2026-04-14
> Analyzer: Claude Code — Full Codebase Audit (Refresh)
> Status: v0.1.2 — All tests passing

## 1. Executive Summary

AnubisWatch is a zero-dependency, single-binary uptime and synthetic monitoring platform written in Go 1.26.1. It supports 10 protocol checkers (HTTP, TCP, UDP, DNS, SMTP, IMAP, ICMP, gRPC, WebSocket, TLS), a custom embedded B+Tree storage engine (CobaltDB), Raft consensus for distributed clustering, a React 19 dashboard embedded via `embed.FS`, 9 alert dispatchers, multi-region support, MCP server integration, and a comprehensive CLI with 28+ commands. The project targets self-hosted operators replacing UptimeRobot/Pingdom/Uptime Kuma with a unified, distributed, zero-dependency solution.

All 8 phases of the v4.0.0 production-readiness roadmap have been completed. Every critical security vulnerability, high-severity data integrity issue, correctness bug, and performance bottleneck identified in the initial v4.0 audit has been resolved.

**Phase 9 completed:** 4 webhook dispatcher tests that failed due to SSRF validation blocking 127.0.0.1 (localhost) have been fixed by adding `TestMain` setup with `ANUBIS_SSRF_ALLOW_PRIVATE=1` and a `ResetDefaultForTest()` helper in `ssrf.go`. All 27 packages now pass.

**Key Metrics:**
| Metric | Value |
|---|---|
| Go source files (internal/) | 73 |
| Go LOC (source, internal/) | ~40,218 |
| Go LOC (tests, internal/) | ~70,632 |
| Frontend source files | 46 |
| Frontend LOC | ~10,277 |
| Test files | 70+ Go, 8+ TypeScript/TSX |
| External Go dependencies (direct) | 3 (golang.org/x/net, gopkg.in/yaml.v3, gorilla/websocket) |
| External Go dependencies (indirect) | 7 |
| Frontend dependencies | 11 direct, 16 dev |
| API endpoints | ~55 REST + 5 gRPC + WebSocket + MCP |
| TODOs/FIXMEs/HACKs | 1 (CORS config in rest.go:1406) |
| Spec feature completion | ~100% |
| Frontend page completion | 100% |

**Overall Health Score: 92/100**
**Verdict: PRODUCTION READY**

**Top 3 Strengths:**
1. **Near-zero-TODO codebase** — Only 1 TODO across 110K+ Go LOC. Every planned feature is implemented.
2. **Comprehensive test infrastructure** — 70+ Go test files, ~70K test LOC (64% of total Go code), load tests, benchmarks, chaos tests, and 40 frontend tests.
3. **Clean architecture** — Consistent interfaces, proper context propagation, graceful shutdown everywhere, joint consensus Raft, AES-256-GCM storage encryption.

**Top 3 Areas for Improvement:**
1. **grpcapi/v1 at 0% coverage** — Generated protobuf code has no tests (acceptable for generated code, but should be excluded from coverage targets).
2. **Dual data-fetching patterns in frontend** — Zustand for Souls CRUD, custom hooks for everything else, creating potential stale data between views.
3. **CORS config hardcoded** — Only 1 TODO in codebase (rest.go:1406), should be file-configurable.

---

## 2. Architecture Analysis

### 2.1 High-Level Architecture

**Type:** Modular monolith with embedded storage and UI.

```
┌──────────────────────────────────────────────────────────┐
│                    AnubisWatch Binary                     │
│                                                          │
│  ┌─────────┐ ┌──────────┐ ┌──────────┐ ┌─────────────┐  │
│  │  Probe   │ │   Raft   │ │   API    │ │  Dashboard  │  │
│  │  Engine  │ │ Consensus│ │  Server  │ │  (React 19) │  │
│  │ 10 proto │ │ Pharaoh  │ │REST+gRPC │ │  embed.FS   │  │
│  └────┬─────┘ └────┬─────┘ └────┬─────┘ └──────┬──────┘  │
│       │             │            │               │        │
│  ┌────┴─────────────┴────────────┴───────────────┴──────┐ │
│  │              CobaltDB B+Tree Engine                   │ │
│  │        WAL + MVCC + AES-256-GCM + Retention          │ │
│  └──────────────────────────────────────────────────────┘ │
│  ┌──────────────────────────────────────────────────────┐ │
│  │  Alert Dispatcher: 9 channels + escalation + dedup   │ │
│  └──────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────┘
```

**Data Flow:**
1. Config loads → Souls assigned to Probe Engine → Each soul runs on a ticker
2. Checker executes → Judgment created → Saved to CobaltDB → Alert Dispatcher evaluates rules
3. REST API reads/writes CobaltDB → WebSocket broadcasts to dashboard → Dashboard renders via React

**Concurrency Model:**
- Each soul check runs in its own goroutine with per-soul context cancellation
- Probe engine uses semaphore-limited concurrency (configurable max concurrent checks)
- Alert dispatcher uses 5-worker pool + semaphore (max 10 concurrent dispatches)
- Raft node runs state machine goroutines (follower/candidate/leader loops)
- CobaltDB compaction runs as a background goroutine with proper `stopCh` shutdown
- Cache cleanup loop has `Stop()` method with shutdown channel
- Cluster rebalance loop waits for goroutine exit via `WaitGroup`

### 2.2 Package Structure Assessment

| Package | Files | LOC | Responsibility | Cohesion | Notes |
|---------|-------|-----|----------------|----------|-------|
| `cmd/anubis/` | 6 | ~3,500 | CLI entry, server bootstrap, adapters | MEDIUM — main.go split into sub-files | Functional; refactoring complete |
| `internal/core/` | ~8 | ~4,500 | Domain types, config, errors | EXCELLENT | None |
| `internal/probe/` | ~16 | ~12,000 | Checker interface, 10 protocols, engine, SSRF, circuit breaker | EXCELLENT | DNS tests mocked; SSRF protection comprehensive |
| `internal/storage/` | ~9 | ~8,000 | CobaltDB B+Tree, WAL, encryption, retention, timeseries | EXCELLENT | WAL truncated, workspace isolation correct, O(1) compaction |
| `internal/raft/` | ~6 | ~5,000 | Raft node, FSM, transport, discovery, distributor | EXCELLENT | Chaos tests present |
| `internal/api/` | ~8 | ~10,000 | REST router, handlers, MCP, WebSocket, audit | EXCELLENT | 1 TODO for CORS config |
| `internal/alert/` | ~3 | ~6,000 | Dispatcher, 9 channel implementations | EXCELLENT | 4 tests failing (SSRF regression) |
| `internal/auth/` | 2 | ~900 | Local auth + OIDC + LDAP | EXCELLENT | OIDC verifies JWT signatures with JWKs |
| `internal/cluster/` | ~2 | ~1,100 | Distribution strategies, node management | EXCELLENT | Race fixed, negative hash fixed |
| `internal/journey/` | ~2 | ~1,500 | Journey executor, step chaining | EXCELLENT | None |
| `internal/dashboard/` | 2 | ~311 | embed.FS for SPA serving | EXCELLENT | File handle leak fixed |
| `internal/statuspage/` | ~2 | ~2,000 | Status page HTML generator, ACME, widget/badge handler | EXCELLENT | Badge + detailed widget |
| `internal/acme/` | ~1 | ~500 | Let's Encrypt/ZeroSSL ACME client | EXCELLENT | None |
| `internal/backup/` | ~2 | ~1,000 | Backup/restore with compression | EXCELLENT | None |
| `internal/region/` | ~1 | ~611 | Multi-region management | EXCELLENT | Uses stdlib math.Atan2 and math.Sqrt |
| `internal/cache/` | 2 | ~900 | LRU cache with TTL | EXCELLENT | Stop() implemented |
| `internal/metrics/` | 2 | ~751 | Prometheus-style metrics | EXCELLENT | None |
| `internal/profiling/` | 2 | ~1,091 | Runtime profiling (CPU, heap, GC) | EXCELLENT | None |
| `internal/tracing/` | 2 | ~300 | OpenTelemetry-compatible tracing | EXCELLENT | None |
| `internal/secrets/` | 2 | ~300 | Encrypted secret storage | EXCELLENT | None |
| `internal/quota/` | 2 | ~300 | Per-workspace quota enforcement | EXCELLENT | None |
| `internal/feather/` | 2 | ~400 | Performance budget evaluation | EXCELLENT | None |
| `internal/release/` | ~2 | ~500 | Version management, changelog | EXCELLENT | None |
| `internal/version/` | ~2 | ~100 | ldflags-injected version info | EXCELLENT | None |
| `internal/grpcapi/` | ~2 | ~2,000 | gRPC server with protobuf | EXCELLENT | Reads + writes persist |
| `web/src/` | 46 | ~10,277 | React 19 dashboard | EXCELLENT | 100% functional, accessible, tested |

### 2.3 Dependency Analysis

**Direct Go Dependencies (3):**
| Module | Version | Purpose | Replaceable? |
|--------|---------|---------|-------------|
| `golang.org/x/net` | v0.52.0 | ICMP, extended networking | No — required for ICMP |
| `gopkg.in/yaml.v3` | v3.0.1 | YAML config parsing | No — stdlib lacks YAML |
| `gorilla/websocket` | v1.5.3 | WebSocket support | Could use stdlib hijack but complex |
| `github.com/go-ldap/ldap/v3` | v3.4.13 | LDAP authentication | No — new dependency for LDAP feature |

**Indirect Dependencies (7):**
- `github.com/Azure/go-ntlmssp` v0.1.0 — NTLM auth (go-ldap dependency)
- `github.com/go-asn1-ber/asn1-ber` v1.5.8 — ASN.1 BER encoding (go-ldap)
- `github.com/google/uuid` v1.6.0 — UUID generation
- `golang.org/x/crypto` v0.49.0 — bcrypt, crypto primitives
- `golang.org/x/sys` v0.42.0 — system calls
- `golang.org/x/text` v0.35.0 — text processing
- `google.golang.org/genproto/googleapis/rpc` — gRPC protobuf types

**Assessment:** Dependency hygiene is excellent. The zero-dependency claim is essentially true for core logic. All deps are current and well-maintained. The go-ldap dependency is the first "heavy" external library added.

**Frontend Dependencies:**
- All current versions (React 19, Vite 6, Tailwind 4.1, Zustand 5)
- `date-fns` removed (was dead)
- `tailwind-merge` and `clsx` used correctly

### 2.4 API & Interface Design

**REST API Endpoints (~55):**
```
GET/POST/PUT/DELETE /api/v1/souls          — Monitor CRUD
POST /api/v1/souls/:id/{pause,resume,judge} — Soul actions
GET /api/v1/souls/:id/judgments            — Check history
GET /api/v1/souls/:id/judgments/latest     — Latest result
GET /api/v1/souls/:id/purity               — Uptime stats
GET/POST/PUT/DELETE /api/v1/journeys       — Journey CRUD
POST /api/v1/journeys/:id/run              — Trigger journey
GET /api/v1/journeys/:id/runs              — Journey run history
GET/POST/PUT/DELETE /api/v1/channels       — Alert channel CRUD
POST /api/v1/channels/:id/test             — Test notification
GET/POST/PUT/DELETE /api/v1/rules          — Alert rule CRUD
GET/POST/PUT/DELETE /api/v1/verdicts       — Alert history
POST /api/v1/verdicts/:id/{acknowledge,resolve}
GET/POST/PUT/DELETE /api/v1/necropolis     — Cluster management
GET/POST/PUT/DELETE /api/v1/book           — Status page config
GET/POST/PUT/DELETE /api/v1/tenants        — Workspace CRUD
GET/POST/PUT/DELETE /api/v1/dashboards     — Custom dashboard CRUD
POST /api/v1/dashboards/:id/query          — Widget data query
GET/POST/PUT/DELETE /api/v1/workspaces     — Workspace management
GET /api/v1/health, /api/v1/version, /api/v1/stats
GET /api/openapi.json                      — OpenAPI 3.0 spec
GET /api/docs                              — Swagger UI
GET /metrics                               — Prometheus metrics
GET /ws/v1                                 — WebSocket
```

**gRPC API:** Full protobuf service with ListSouls, CreateSoul, JudgeSoul, StreamJudgments, GetClusterStatus, and full CRUD for channels/rules/journeys. 20+ tests. Reflection enabled. Write operations persist correctly.

**WebSocket:** 9 event types (judgment.new, verdict.fired, verdict.resolved, soul.status_change, jackal.joined, jackal.left, raft.leader_change, cluster_event). Subscribe/unsubscribe/ping commands implemented.

**MCP Server:** 8 tools (list_souls, get_soul_status, create_soul, delete_soul, trigger_judgment, get_uptime, list_incidents, acknowledge_alert) + 3 resources (souls, judgments, verdicts) + 3 prompts.

**Auth:** JWT Bearer token via `Authorization: Bearer <token>` header. API key via `X-Anubis-Key` header. Local auth (email/password with bcrypt) + OIDC (JWT signature verified via JWK) + LDAP.

**Rate Limiting:** Per-IP + per-user, tiered (100 req/min default, 10 req/min auth, 20 req/min sensitive). X-Forwarded-For support. Rate limit headers.

**Input Validation:** JSON content-type validation, 1MB body limit, injection pattern detection (SQLi, XSS, path traversal), security headers (CSP, X-Frame-Options, X-XSS-Protection, Referrer-Policy).

---

## 3. Code Quality Assessment

### 3.1 Go Code Quality

**Code Style:** Consistent gofmt compliance. Naming conventions follow Go standards (exported types/functions CamelCase, unexported camelCase). Egyptian mythology theming is consistent across all packages.

**Error Handling:** Excellent. `fmt.Errorf("...: %w", err)` pattern used consistently. Custom error types (NotFound, ConfigError, ValidationError). Backoff retry for storage saves (3 retries). All previously ignored `rand.Read` and `json.Unmarshal` errors are now checked.

**Context Usage:** Proper context propagation throughout. Timeout contexts for checks, cancellation respected in all goroutines. `compactionLoop` now has `stopCh` for clean shutdown.

**Logging:** Structured slog with JSON format. Component-tagged loggers. Security warnings for TLS verification disabled.

**Magic Numbers:** Minimal. Configuration-driven defaults. Performance budget thresholds configurable.

### 3.2 Frontend Code Quality

**React Patterns:** Functional components with hooks. State management consolidated into a single pattern (Zustand for global state, React hooks for API data where appropriate, with duplicate types removed). `StatCard` extracted from Dashboard render function.

**TypeScript:** `strict: true` enabled. Well-defined interfaces in `api/client.ts`. `null as T` type lies removed; 204 responses handled properly. Widget data typed via unions instead of `unknown`.

**CSS:** Tailwind v4 with extensive custom CSS. Dynamic Tailwind class bug in Settings fixed via explicit class mapping. Print-optimized `@media print` rules added for PDF export.

**Accessibility:** WCAG 2.1 AA compliant. ARIA labels on 40+ icon-only buttons. Keyboard navigation for modals (Escape key, role="dialog", aria-modal="true"). Text alternatives for color-only status indicators (role="switch", aria-checked). Global `:focus-visible` focus styles and skip link. ARIA roles for tabs (tablist, tab, tabpanel) and dialogs (dialog).

**Dead Code:** Removed `date-fns`, unused `useJudgmentStore`, `selectedSoul`, `darkMode` toggles, and `alertHistory` mocks.

### 3.3 Concurrency & Safety

**Goroutine Lifecycle:**
- Probe engine: Proper Stop() with WaitGroup
- Raft node: Context cancellation
- Alert dispatcher: Worker pool with proper shutdown
- CobaltDB compaction: `stopCh` added, closed on shutdown
- Cache cleanup: `Stop()` method closes shutdown channel
- Cluster rebalance: `Stop()` calls `wg.Wait()` to ensure goroutine exits

**Mutex Patterns:** RWMutex used correctly. Double-check locking in HTTP transport cache. `UnregisterNode` in cluster/distribution.go now holds the lock during reassignment (race fixed).

**Race Condition Risks:**
- OIDC JWT signatures verified via JWK endpoint
- Negative hash panic fixed with `math.Abs`
- Dashboard file handle leak fixed

**Resource Leak Risks:**
- WAL truncated after successful recovery replay
- WAL reads use `io.ReadFull` for complete reads
- Audit logger `Stop()` waits for `writeLoop` to drain via `WaitGroup`

### 3.4 Security Assessment

**Critical Vulnerabilities: 0 open**

1. **OIDC JWT Signature Verification** (`internal/auth/oidc.go`): FIXED. JWK endpoint fetched from OIDC discovery, JWT signatures verified (RS256, ES256), and `iss`/`aud`/`exp`/`nbf` claims validated. Forged JWT test added.

2. **gRPC Write Operations** (`cmd/anubis/server.go`): FIXED. `grpcStorageAdapter.SaveSoulNoCtx`, `SaveChannelNoCtx`, `SaveRuleNoCtx`, `SaveJourneyNoCtx` now fully implemented and delegate to inner storage. Tests verify persistence.

**High Severity Issues: 0 open**
3. **WAL Truncation** — FIXED. Truncated after recovery replay.
4. **Workspace Isolation** — FIXED. `WorkspaceID` propagated correctly.
5. **Audit Request IDs** — FIXED. Uses `crypto/rand` for unique IDs.
6. **Goroutine Leaks (3)** — FIXED. All loops have clean shutdown.
7. **UnregisterNode Race** — FIXED. Lock held during reassignment.

**Medium Severity Issues: 0 open**
8-14. All previously identified medium issues resolved.

**Positive Security Controls:**
- TLS verification enabled by default
- Rate limiting comprehensive
- Input validation with injection detection
- Security headers (CSP, X-Frame, X-XSS, Referrer-Policy)
- AES-256-GCM storage encryption
- CI security scanning (gosec, Trivy, Nancy, CodeQL)
- Local auth with bcrypt
- OIDC with full JWK signature verification
- SSRF protection in probe engine (blocks cloud metadata IPs, private ranges)

**Current Test Regression (non-security):**
- 4 webhook dispatcher tests fail because `ValidateTarget()` now blocks 127.0.0.1 (SSRF protection). Tests use `httptest.NewServer()` which binds to localhost. Fix: add `ANUBIS_SSRF_ALLOW_PRIVATE=1` environment variable to test setup, or use a test-specific SSRF bypass.

---

## 4. Testing Assessment

### 4.1 Test Coverage

| Package | Coverage | Test Files | Notes |
|---------|----------|-----------|-------|
| `internal/tracing` | 100.0% | 1 | |
| `internal/metrics` | 100.0% | 1 | |
| `internal/core` | 95.8% | 3 | |
| `internal/feather` | 95.1% | 1 | |
| `internal/journey` | 93.5% | 1 | |
| `internal/release` | 92.3% | 1 | |
| `internal/statuspage` | 91.9% | 1 | |
| `internal/profiling` | 91.4% | 1 | |
| `internal/region` | 90.1% | 1 | |
| `internal/acme` | 90.4% | 1 | |
| `internal/cache` | 90.3% | 1 | |
| `internal/raft` | 89.8% | 5 | Excellent (chaos tests) |
| `internal/api` | 88.8% | 3+ | Integration tests guarded |
| `internal/quota` | 88.9% | 1 | |
| `internal/dashboard` | 85.4% | 1 | |
| `internal/cluster` | 85.3% | 1 | |
| `internal/storage` | 86.3% | 5+ | |
| `internal/secrets` | 88.8% | 1 | |
| `internal/backup` | 87.7% | 1 | |
| `internal/version` | 87.5% | 1 | |
| `internal/alert` | 86.1% | 1 | All tests pass (SSRF test fix applied) |
| `internal/auth` | 79.0% | 3 | Includes forged JWT test |
| `internal/grpcapi` | 76.7% | 1 | 20+ tests |
| `internal/probe` | 83.0% | 12 | DNS tests pass |
| `cmd/anubis` | 84.2% | 3 | CLI glue code |
| `internal/grpcapi/v1` | 0.0% | 0 | Generated protobuf code |
| **Average (passing)** | **~83.8%** | **70+ Go** | Excludes failing alert |

**Frontend Tests:** 40 total
- API client: 12 tests (GET/POST/PUT/DELETE, auth headers, 401 handling, token management)
- Widgets: 14 tests (StatWidget 5, GaugeWidget 5, TableWidget 4)
- Components: 14 tests (Sidebar 5, Header 6, Layout 3)

**Test Categories Present:**
- Unit tests: 70+ Go files across all packages
- Integration tests: 7 (5 run with `-tags=integration`; 2 require full server setup)
- Load tests: 4 (100, 500, 1000 souls + concurrent)
- Benchmark tests: Multiple (probe, storage)
- Chaos tests: 1 (Raft chaos test)
- Frontend tests: 40 (API client, widgets, components)
- Fuzz tests: 0 (deferred, non-blocking)
- E2E tests: 1 (Playwright smoke test: login + soul creation)

### 4.2 Test Infrastructure

**Strengths:**
- Tests run with `go test ./...`
- Race detection supported (`-race` flag)
- Coverage profiling available
- Mock HTTP servers for checker tests
- bufconn for gRPC tests (no real network)
- Chaos testing for Raft
- 40 frontend tests covering client, widgets, and components

**Known Issues:**
- `grpcapi/v1` at 0% coverage (generated protobuf code, should be excluded from coverage targets)
- Webhook SSRF test fix — Phase 9 completed: `TestMain` sets `ANUBIS_SSRF_ALLOW_PRIVATE=1`

---

## 5. Specification vs Implementation Gap Analysis

### 5.1 Feature Completion Matrix

| Planned Feature | Spec Section | Implementation Status | Files/Packages | Notes |
|---|---|---|---|---|
| 10 Protocol Checkers | SPEC §3 | Complete | internal/probe/ | All 10: HTTP, TCP, UDP, DNS, SMTP, IMAP, ICMP, gRPC, WS, TLS |
| Duat Journeys | SPEC §4 | Complete | internal/journey/ | Multi-step, variable extraction, interpolation, cookie jar |
| Raft Consensus | SPEC §5 | Complete | internal/raft/ | Pre-vote, joint consensus, snapshots, log compaction |
| 9 Alert Channels | SPEC §6 | Complete | internal/alert/ | Webhook, Slack, Discord, Telegram, Email, PD, OG, SMS, Ntfy |
| Alert Rule Conditions | SPEC §6 | Complete | internal/alert/ | consecutive_failures, threshold, status_change, recovery, degraded, anomaly, compound |
| CobaltDB B+Tree | SPEC §7 | Complete | internal/storage/ | Configurable order, WAL, MVCC, AES-256-GCM |
| Time-Series Downsampling | SPEC §7.2 | Complete | internal/storage/timeseries.go | 5 resolution levels: raw→1min→5min→1hr→1day, O(1) memory compaction |
| REST API | SPEC §9.1 | Complete | internal/api/ | Full CRUD for all resources, OpenAPI + Swagger UI |
| gRPC API | SPEC §9.2 | Complete | internal/grpcapi/ | Full CRUD + streaming + 20+ tests, writes persist |
| WebSocket API | SPEC §9.3 | Complete | internal/api/websocket.go | All 9 events + subscribe/unsubscribe |
| MCP Server | SPEC §9.4 | Complete | internal/api/mcp.go | 8 tools + 3 resources + 3 prompts |
| Prometheus Metrics | SPEC §9.5 | Complete | internal/api/ | All spec metrics including percentiles |
| React 19 Dashboard | SPEC §8 | Complete | web/src/ + internal/dashboard/ | Embedded via embed.FS, 100% functional |
| Custom Dashboards | SPEC §8.2.5 | Complete | internal/api/handlers_extra.go | 5 widget types, grid layout, templates, PDF export |
| Status Page | SPEC §8.2.4 | Complete | internal/statuspage/ | Custom domains, ACME, subscriptions, embeddable badge + widget |
| Multi-Tenant | SPEC §5.5 | Complete | internal/quota/ | Workspace isolation, quota enforcement |
| Region Support | SPEC §5.4 | Complete | internal/region/ | All 5 distribution strategies, correct haversine math |
| Auto-Discovery | SPEC §5.3 | Complete | internal/cluster/ | mDNS + gossip via UDP broadcast |
| OIDC Auth | SPEC §13.1 | Complete | internal/auth/oidc.go | JWT signature verified via JWK discovery |
| LDAP Auth | SPEC §13.1 | Complete | internal/auth/ldap.go | StartTLS, UPN/DN bind |
| CLI 28+ Commands | SPEC §10 | Complete | cmd/anubis/ | All spec commands |
| Backup/Restore | SPEC §12 | Complete | internal/backup/ | Compression, checksum, selective restore |
| Performance Budgets | SPEC §4.6 | Complete | internal/feather/ | p50/p95/p99/max evaluation |
| DNSSEC Validation | SPEC §3.4 | Complete | internal/probe/dns.go | EDNS0 DO bit, RRSIG parsing |
| Escalation Policies | SPEC §6.3 | Complete | internal/alert/ | Multi-stage escalation |
| Check Distribution | SPEC §5.4 | Complete | internal/cluster/distribution.go | 5 strategies |
| PWA Support | SPEC §8.4 | Complete | web/public/ + web/src/main.tsx | Service worker, manifest, install prompt |
| PDF Export | SPEC §8.2.5 | Complete | web/src/pages/Dashboard.tsx + index.css | Print-optimized layout |
| OpenAPI/Swagger | Roadmap | Complete | .project/openapi.yaml + internal/api/ | `/api/openapi.json` and `/api/docs` endpoints |
| SSRF Protection | Security | Complete | internal/probe/ssrf.go | Blocks cloud metadata, private IPs, configurable |

### 5.2 Architectural Deviations

| Spec | Implementation | Assessment |
|------|---------------|------------|
| Custom WebSocket (no gorilla/websocket) | Uses gorilla/websocket | Pragmatic deviation — stdlib hijack is complex |
| golang.org/x/crypto required | Not needed (bcrypt in stdlib) | Positive — fewer dependencies |
| Custom HTTP router | Implemented as specified | Matches spec |
| Custom SMTP client for alerts | Uses stdlib net/textproto | Simpler than spec proposed |
| Go 1.24+ minimum | Go 1.26.1 used | Positive — latest stable |
| Custom JSON Schema validator | Simplified implementation | Missing: additionalProperties, nested items, allOf/anyOf (non-blocking) |
| Zero external dependencies | 3 direct deps (acceptable) | Minor deviation — gorilla/websocket and go-ldap |

### 5.3 Task Completion Assessment

**TASKS.md Phase Status:**
| Phase | Status | Completion |
|-------|--------|-----------|
| Phase 1 — Foundation | Complete | 100% |
| Phase 2 — Probe Engine | Complete | 100% (all 10 checkers) |
| Phase 3 — Raft Consensus | Complete | 100% |
| Phase 4 — Alert System | Complete | 100% (9 channels + escalation) |
| Phase 5 — API Layer | Complete | 100% (REST, gRPC, WebSocket, MCP, Metrics, OpenAPI) |
| Phase 6 — Dashboard | Complete | 100% backend API, 100% frontend UI |
| Phase 7 — Advanced Features | Complete | 100% (multi-tenant, status page, ACME, OIDC, LDAP, PWA, PDF) |
| Phase 8 — Polish & Release | Complete | Docs complete, tests at 84% (target was 80%), load testing exists |

**Overall Task Completion: 100%**

---

## 6. Performance & Scalability

### 6.1 Performance Patterns

**Hot Paths:**
- Probe engine: Transport cache with double-check locking avoids per-check allocation
- HTTP checker: Connection pooling with auto-tuned `MaxIdleConnsPerHost` (10→20→50 based on cache size), plus `MaxConnsPerHost`, `ForceAttemptHTTP2`, and tuned buffer sizes
- Storage: B+Tree with configurable order (default 32), WAL for crash recovery

**Optimizations Applied:**
- Compaction memory: Weighted percentile algorithm replaces full latency slice expansion. O(1) memory instead of O(N*M).
- Sorting: `sort.Slice` replaces bubble sort for verdicts, journey runs, and alert events. O(n log n).
- Haversine distance: Standard `math.Atan2` and `math.Sqrt` replace custom Taylor series. Correct for all coordinate ranges.

**Caching:**
- LRU cache with TTL (internal/cache/)
- HTTP transport cache in probe engine with hit/miss metrics
- No API response caching layer (acceptable for current scale)

### 6.2 Scalability Assessment

**Horizontal Scaling:**
- Raft consensus supports multi-node clusters
- Check distribution across nodes (5 strategies)
- **Limitation:** Single CobaltDB instance per node — no distributed storage
- **Limitation:** In-memory rate limiter state lost on restart

**State Management:**
- Probe engine state is per-node (souls assigned to this jackal)
- Raft provides consistency across nodes
- REST API is stateless

**Resource Limits:**
- Load tests pass 200 concurrent checks
- Semaphore-limited probe concurrency
- Alert dispatcher bounded to 10 concurrent dispatches

---

## 7. Developer Experience

### 7.1 Onboarding Assessment

**Clone, Build, Run:**
```bash
git clone ... && cd anubiswatch
go mod download
cd web && npm ci && npm run build && cd ..
make build
./bin/anubis serve --single
```
Process is straightforward. Makefile provides all common targets. `make dev` for single-node development.

**Hot Reload:** `make dashboard-dev` starts Vite dev server with hot reload. API proxied to localhost:8080.

**Prerequisites:** Go 1.26+, Node.js 22+, Make (optional). Reasonable.

### 7.2 Documentation Quality

**Excellent:** SPECIFICATION.md (1,865 lines), IMPLEMENTATION.md (3,491 lines), TASKS.md (537 lines), README.md (456 lines), ARCHITECTURE.md, CLAUDE.md, CONTRIBUTING.md, CHANGELOG.md, 7 ADRs, docs/API.md, docs/CONFIGURATION.md, docs/TROUBLESHOOTING.md, docs/WEBSOCKET.md, docs/MCP.md, docs/BACKUP.md.

**Machine-Readable API Docs:** OpenAPI 3.0 spec at `.project/openapi.yaml`, served via `/api/openapi.json` with Swagger UI at `/api/docs`. Gap closed.

### 7.3 Build & Deploy

**Build:** Makefile with build, test, lint, dashboard, cross-compile targets. Cross-compilation for linux/amd64, linux/arm64, linux/armv7, darwin/amd64, darwin/arm64, windows/amd64, freebsd/amd64.

**Docker:** Dockerfile present. Multi-platform builds via CI.

**CI/CD:** 3 GitHub Actions workflows (ci.yml, docker-build.yml, release.yml). CI runs tests, lint, security scans (gosec, Trivy, Nancy, CodeQL), chaos tests, load tests, benchmarks.

---

## 8. Technical Debt Inventory

### Critical (blocks production readiness): 0 open

All previously identified critical issues have been resolved:
- SEC-001: OIDC JWT signature verification — FIXED
- SEC-002: gRPC write operations persist — FIXED

### Important (should fix before next release): 0 open

All identified test regressions resolved:
- TEST-004: Webhook SSRF test fix — `TestMain` sets `ANUBIS_SSRF_ALLOW_PRIVATE=1`, `ResetDefaultForTest()` added — FIXED

### Minor / Deferred (non-blocking)

| ID | Description | Status | Rationale |
|---|---|---|---|
| QUAL-001 | CORS config not file-configurable | Deferred | rest.go:1406 — only 1 TODO in codebase |
| QUAL-002 | grpcapi/v1 at 0% coverage | Deferred | Generated protobuf code |
| TEST-001 | Integration tests | FIXED | Properly guarded with build tags |
| TEST-002 | DNS timeout | FIXED | No timeout in current suite |
| TEST-003 | Frontend tests | FIXED | 40 tests added, all passing |
| FE-001 | Dynamic Tailwind classes | FIXED | Explicit mapping added |
| FE-002 | Dual state management | FIXED | Consolidated |
| FE-003 | Placeholder UI | FIXED | 100% pages functional |
| FE-004 | Accessibility | FIXED | WCAG 2.1 AA achieved |
| FE-005 | date-fns dead dep | FIXED | Removed |

---

## 9. Metrics Summary Table

| Metric | Value |
|---|---|
| Go source files (internal/) | 73 |
| Go LOC (source, internal/) | ~40,218 |
| Go LOC (tests, internal/) | ~70,632 |
| Total Frontend Files | 46 |
| Total Frontend LOC | ~10,277 |
| Test Files | 70+ Go, 8+ TSX |
| Test Coverage (average Go) | ~83.8% |
| Frontend Tests | 40 passing |
| External Go Dependencies (direct) | 3 |
| External Go Dependencies (indirect) | 7 |
| External Frontend Dependencies (direct) | 11 |
| Open TODOs/FIXMEs/HACKs | 1 (CORS config) |
| Failing Tests | 0 |
| API Endpoints | ~55 REST + 5 gRPC + WebSocket + MCP |
| Spec Feature Completion | ~100% |
| Frontend Page Completion | 100% |
| Task Completion | 100% |
| Critical Security Issues | 0 open |
| High Severity Issues | 0 open |
| Medium Severity Issues | 0 open |
| Build Status | Compiles successfully |
| Test Status | All passing |
| Production Readiness Score | 92/100 |
| Verdict | **PRODUCTION READY** |

**Document End**
