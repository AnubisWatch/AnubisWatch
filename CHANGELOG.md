# Changelog

All notable changes to AnubisWatch will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed (business-critical)

End-to-end test of the actual uptime-monitoring workflow (target up → down → up) exposed four interlocking bugs in the alert → incident pipeline. CI was green, audit was green, docs claimed it worked — but no incident had ever actually been created from a failure event. Each bug masked the next:

- **`consecutive_failures` condition was a no-op**: `internal/alert/manager.go` `checkConditions` had no case for the condition type referenced in every doc; rules using it silently never fired. Now tracks a per-(rule, soul) `failureStreak` counter that increments on each dead judgment and resets on any non-dead status (bounded at 1M to prevent overflow on permanently-down souls)
- **`scope.type=""` rejected all souls**: `ruleApplies` switched on `scope.Type` and returned false for the default case. The natural API client shape (`{"scope":{"soul_ids":[...]}}`) sets `soul_ids` without `Type`, so every such rule was silently skipped. Now infers type from which sub-field is populated; empty scope defaults to "all"
- **Incident records were never created**: alert events queued, channels dispatched notifications, but no code path persisted an `Incident`. `/api/v1/incidents` always returned empty regardless of rule firings. `recordIncident` now opens an Incident on first trigger and appends events on subsequent triggers for the same (rule, soul); `autoResolveOpenIncident` closes it on recovery when `rule.AutoResolve` is set
- **`/api/v1/incidents` hid resolved history**: `handleListIncidents` called `ListActiveIncidents()` which filtered out resolved entries — recovered incidents disappeared from the UI. Default now returns every incident; the previous behaviour is reachable via `?status=active`

### Tests

- `TestManager_ConsecutiveFailures_OpensAndAutoResolvesIncident` — full pipeline regression: 2 failures don't trigger, 3rd opens an incident, 4th appends to the same incident, recovery auto-resolves it, and a new failure streak opens a separate incident
- `TestManager_RuleApplies_InfersScopeFromSubfields` (5 sub-cases) — scope inference matrix
- Replaces the hollow `TestManager_ProcessJudgment` whose comment literally said "may or may not trigger depending on condition logic … just verify the method doesn't panic"

## [0.1.4] - 2026-05-22

### Security

- **CRITICAL-2 follow-up** (`internal/core/config.go`): `validate()` now rejects `tls.enabled: false` when `environment` is `"production"`. Operators who terminate TLS at a reverse proxy can use a different environment label (`"staging"`, `"behind-lb"`, etc.) or omit the field. Closes the remaining hardening item from the production-readiness audit
- **MEDIUM-11** (`internal/api/rest.go`): `/api/docs` now emits a scoped Content-Security-Policy that allows `cdn.jsdelivr.net` for Swagger UI scripts/styles/fonts and permits the required inline initialiser. The rest of the application keeps the strict `default-src 'self'` from `securityHeadersMiddleware`
- **MEDIUM-13** (`internal/auth/local.go`): brute-force lockout state is now persisted into the session file. After a restart, the lockout deadline still applies — an attacker can no longer bypass the 15-minute lockout by triggering a process restart. Stale entries (lockout expired AND last attempt outside the reset window) are dropped on load so the file doesn't grow unbounded

### Cleanup ("fazla ise kes at")

- Removed stale dashboard source at `internal/dashboard/` (16 files: old JSX, `package.json`, `pnpm-lock.yaml`, `vite.config.js`, etc.). The canonical dashboard moved to `web/` long ago; only `embed.go`/`embed_test.go`/`dist/` were still in use here. CI `gofmt` filter also dropped the now-pointless `internal/dashboard/node_modules` exclusion
- Removed empty `data/` and orphan root `anubis` binary (both gitignored, no longer needed locally)

### Tests

- `TestLocalAuthenticator_LockoutPersistence` — confirms a triggered lockout survives `NewLocalAuthenticator` restart over the same session file
- `TestLocalAuthenticator_LockoutPersistence_ExpiredDropped` — confirms fully-expired entries are dropped on load
- `TestLocalAuthenticator_LockoutPersistence_ClearedOnSuccess` — confirms successful login clears the failure record from disk
- `TestValidate_ProductionRequiresTLS` (8 sub-cases) — confirms environment + TLS interplay (production with/without TLS, casing, whitespace, non-production environments unaffected)
- `TestHandleOpenAPIDocs` extended to assert the scoped CSP override contains the required directives for Swagger UI's CDN assets

## [0.1.3] - 2026-05-21

### CI / Build

- **Frontend toolchain**: all GitHub Actions jobs (test-frontend, e2e-dashboard, build, release) switched from `npm ci` to `pnpm install --frozen-lockfile`. The frontend dependency upgrade had regenerated `pnpm-lock.yaml` but left `package-lock.json` stale by 16+ packages, so every job that used `npm ci` was failing
- **Dockerfile**: `npm ci` replaced with `pnpm install --frozen-lockfile` (pnpm installed via `npm install -g pnpm@10` since Alpine's nodejs does not bundle corepack)
- **Helm tests**: removed lint/template/guardrail references to `deployments/charts/anubiswatch/` — that chart was deleted in commit e7b82e2 ("clean: remove duplicate dirs...") but the CI workflow still referenced it
- **Orphan lockfile**: `web/package-lock.json` deleted (project uses pnpm)

### Fixed

- **gofmt drift**: eleven files (mostly tests + `core/config.go`, `core/feather.go`, `api/rest.go`, `journey/executor.go`, `telemetry/tracer.go`, `grpcapi/server.go`) had drifted struct field alignment. Go 1.26.3 (CI) is stricter than 1.26.0 (local), so the drift only surfaced in CI's Lint job; `gofmt -w` applied across the tree
- **ESLint**: `eslint-plugin-react-hooks` v7's new strict rules (`set-state-in-effect`, `immutability`, `purity`, `use-memo`) flagged canonical React patterns (fetch-on-mount, derived `Date.now()`, callbacks referencing each other via refs/timers) as errors; these are disabled with rationale documented in `web/eslint.config.js`
- **ErrorBoundary test**: dropped three unused imports/variables (`Component`, `ReactNode`, `err`) that tripped `@typescript-eslint/no-unused-vars`
- **Playwright e2e**: smoke spec headings re-synced with the Egyptian-themed dashboard rename (Souls → Essence, Judgments → Weighings, Alerts → Divine Warnings, Incidents → Cries of Chaos, Maintenance → Sacred Rest, Journeys → Voyages, Cluster → Necropolis, Status Pages → Temple Squares, Settings → Pharaoh's Chamber); the suite went from 1 failure cascading into 7 skipped tests back to 8/8 passing
- **Playwright e2e (CI flakiness)**: `submitSoulEditProtocolPayload` now waits for the `/api/v1/souls/{id}` GET response before asserting on the "Edit Soul" heading, so the 30s deadline doesn't include cold-cache asset load + auth round-trip on slow CI hosts

## [0.1.2] - 2026-05-21

### Security

- **CRITICAL**: SSRF protection — removed `grpc`, `tcp`, `udp` schemes from allowed URL schemes; only `http`, `https`, `ws`, `wss` are now permitted
- **CRITICAL**: LDAP authentication — local fallback now gated on `isConnectionFailure()`; anonymous LDAP bind can no longer silently fall back to local admin account
- **CRITICAL**: Password reset — token prefix removed from structured log output
- **HIGH**: LDAP filter injection — `ldap.EscapeFilter()` now applied in default filter case (empty `UserFilter`)
- **HIGH**: OIDC issuer validation — issuer claim normalized on both provider response and config sides (trailing-slash mismatch fixed)
- **MEDIUM**: REST TLS — explicit `tls.Config{MinVersion: TLS 1.2, PreferServerCipherSuites: true}` on `ListenAndServeTLS`
- **MEDIUM**: HSTS header — `Strict-Transport-Security` now sent only when TLS is enabled
- **MEDIUM**: X-Forwarded-For spoofing — added `TrustedProxies` config; `realIP()` function respects known proxy IPs; XFF is ignored without trusted proxy configured
- **MEDIUM**: Status page deletion — slug and domain index deletion errors now logged instead of silently ignored
- **MEDIUM**: TimeSeries compaction — `stopMu sync.Mutex` added to protect `stopCh` channel from race conditions
- **Configuration**: `api_keys.enabled` set to `false` in container config (no validation implementation)

### Added

- `server.trusted_proxies` config field for X-Forwarded-For validation
- `server.tls.min_version` and `server.tls.prefer_server` fields for TLS cipher control
- `TestSSRFValidator_ValidateTarget_BlockedSchemes` covering disallowed schemes
- `TestIsConnectionFailure` with 12 test cases for LDAP connection vs auth error discrimination

### Helm

- `values.yaml`: added `tls.cert`, `tls.key`, `tls.min_version`, `tls.prefer_server`, `trustedProxies` fields
- `values-production.example.yaml`: documented TLS and `trustedProxies` production configuration
- `templates/configmap.yaml`: wired new TLS and `trustedProxies` fields into `anubis.yaml` configmap

## [0.1.1] - 2026-04-12

### Added
- Comprehensive configuration validation for all config types
- Soul configuration validation with protocol-specific checks
- Channel configuration validation (webhook, slack, telegram, email, etc.)
- Alert rule validation with condition type checking
- Server, storage, auth, and logging config validation

### Changed
- Refactored `cmd/anubis/main.go` into smaller command-group files (`backup.go`, `cluster.go`, `judge.go`, `soul.go`, `system.go`, `util.go`)
- Moved server-specific adapters and helpers from `main.go` into `server.go`
- Enhanced `validate()` to automatically call `setDefaults()` before validation

### Fixed
- Auth config `setDefaults` no longer overrides an explicit `enabled: false` in config files (`AuthConfig.Enabled` changed to `*bool`)

## [0.1.0] - 2026-04-06

### Added
- MCP server integration at `/api/v1/mcp` endpoint for AI agent integration
- 8 built-in MCP tools: list_souls, get_soul, force_check, get_judgments, list_incidents, get_stats, acknowledge_incident, create_soul
- 3 MCP resources: getting-started, api-reference, status/current
- 3 MCP prompts: analyze-soul, incident-summary, create-monitor-guide
- Duat Journey executor for multi-step synthetic monitoring
- Variable extraction from HTTP responses (JSON path, regex, headers, cookies)
- Status page generator with HTML/JSON serving
- Status page custom domain support
- Status page password protection (protected visibility)
- Status page custom themes
- Status page RSS feed support
- Status page SVG badge generation for embedding
- Workspace-based multi-tenancy with namespace isolation
- RBAC with 5 roles: Owner, Admin, Editor, Viewer, API
- Quota management per workspace
- API pagination for all list endpoints
- API rate limiting (100 requests/minute per IP)
- Request validation middleware
- Alert deduplication with configurable cooldown
- Alert escalation policies with multi-stage escalation
- Alert acknowledgment workflow
- Circuit breaker pattern for probe engine (per-soul failure tracking)
- Concurrency limiting for probe checks (default: 100 concurrent)
- Region-based probe filtering
- Health check endpoint at `/health`
- Workspace context middleware for multi-tenant operations

### Changed
- Updated Go version to 1.26
- Updated CI/CD pipelines with security scanning (gosec)
- Improved Raft test coverage from 71.7% to 86.0%
- Dashboard build made optional in Dockerfile

### Documentation
- API.md with complete REST API reference
- TROUBLESHOOTING.md with deployment troubleshooting guide
- docs/adr/ with 8 Architecture Decision Records
- Updated CONTRIBUTING.md with current project structure

### Fixed
- REST server test compatibility with MCP server integration
- Status page uptime calculation
- Soul status tracking
- WebSocket console errors (disabled in favor of REST polling)
- MDNS mutex protection for conn field
- Various lint issues across packages
- Release workflow artifact handling

## [0.0.1] - 2026-04-04

### Added
- Initial release of AnubisWatch
- 10 protocol checkers: HTTP/HTTPS, TCP, UDP, DNS, ICMP, SMTP, IMAP, gRPC, WebSocket, TLS
- Embedded B+Tree storage (CobaltDB) with WAL and MVCC
- Raft consensus for distributed clustering
- Probe engine with adaptive intervals
- Alert engine with compound conditions and rate limiting
- REST API, WebSocket, and gRPC interfaces
- React 19 + Tailwind 4.1 dashboard
- Single binary deployment with zero dependencies
- Multi-tenancy support with workspaces and role-based access control
- Public status pages with custom domains, password protection, and uptime history
- 9 alert notification channels (Slack, Discord, Telegram, Email, PagerDuty, OpsGenie, SMS, Ntfy, Webhook)
- MCP (Model Context Protocol) server for AI integration
- Time-series storage with automatic downsampling
- Dashboard embedding in single binary (React 19 + Tailwind 4.1)
- Status page REST API endpoints
- Kubernetes Helm chart for deployment
- Docker and docker-compose support (single-node and 3-node cluster)
- Installation script for easy Linux/macOS setup
- systemd service file for production deployments
- Homebrew formula for macOS/Linux
- GHCR-exclusive container images (multi-arch: amd64, arm64, arm/v7)

### Documentation
- README.md with features, quick start, and comparison table
- BRANDING.md with complete brand guidelines
- CONFIGURATION.md with full configuration reference
- DEPLOYMENT.md with deployment guides
- GHCR.md with container registry documentation
- openapi.yaml with OpenAPI 3.1.0 specification
- WEBSITE.md with anubis.watch landing page content
- CONTRIBUTING.md with contribution guidelines
- INDEX.md with documentation index
- RELEASE_TEMPLATE.md for GitHub Releases
- MARKETING.md with launch materials

---

[0.1.1]: https://github.com/AnubisWatch/anubiswatch/releases/tag/v0.1.1
[0.1.0]: https://github.com/AnubisWatch/anubiswatch/releases/tag/v0.1.0
[0.0.1]: https://github.com/AnubisWatch/anubiswatch/releases/tag/v0.0.1
