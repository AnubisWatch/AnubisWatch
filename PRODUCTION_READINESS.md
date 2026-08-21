# Production Readiness Checklist

**Date:** 2026-07-24  
**Status:** Single-node production candidate; requires environment-specific preflight, backup/restore rehearsal, rollout smoke evidence, and capacity validation. Multi-node deployment is not supported by the production chart.

## Build Verification

- [x] `go build ./...` — clean, zero errors
- [x] `go vet ./...` — clean, zero warnings
- [x] `govulncheck ./...` — no dependency vulnerabilities
- [x] `pnpm audit` (frontend devDeps) — 7 undici CVEs in devDependencies only (not in production binary)

## Test Verification

- [x] `go test -short ./...` — all packages pass
- [x] `go test -race -short ./...` — all packages pass with race detector
- [x] `scripts/check-go-coverage.sh` — measured 88.9% statement coverage; enforced minimum is 80% (generated protobuf excluded)
- [x] Frontend tests: `pnpm test` — all pass

## Security Fixes Applied

- [x] **GetLog data round-trip** (`raft_log.go`): Fixed type assertion bug that silently lost FSM command data on restart recovery
- [x] **Alert dispatch workspace filter** (`manager.go`): Events now only dispatched to channels in the same workspace
- [x] **WebSocket workspace isolation** (`websocket.go`): Client-supplied workspace query parameter no longer trusted; authenticated user's workspace always used
- [x] **WebSocket global broadcast leak** (`websocket.go`): Empty-workspace events scoped to "default" room only, never broadcast globally
- [x] **MCP workspace bypass** (`mcp.go`): Empty workspace context now defaults to "default" instead of granting access to all workspaces
- [x] **Incident acknowledge/resolve** (`manager.go`): Workspace check is fail-closed (empty workspace no longer skips authorization)
- [x] **TLS MinVersion** (all outbound checkers): HTTP, gRPC, SMTP, LDAP now enforce TLS 1.2 minimum
- [x] **SSRF dialer wrap** (`http.go`): HTTP checker transport now uses SSRF-protected dialer (DNS rebinding defense)
- [x] **Raft transport OOM** (`transport.go`): RPC payload size capped at 16MB
- [x] **Escalation message** (`manager.go`): Uses human-readable SoulName instead of opaque ULID

## Type Safety & API Hardening

- [x] **gRPC interface{} elimination** (`grpcapi/server.go`): Store interface, PB conversions, and handlers rewritten — 74 `interface{}` usages → 0 in production code
- [x] **Request ID middleware** (`api/rest.go`): Every API request gets a ULID, logged in structured logs and OpenTelemetry spans, returned as `X-Request-ID` header
- [x] **Key format validation** (`storage/engine.go`, `storage/storage.go`): `validateResourceID` guards against `/` in IDs across 8 entity types (souls, channels, rules, journeys, workspaces, status pages, dashboards, maintenance windows)
- [x] **Cursor-based pagination** (`api/rest.go`): Judgment list endpoint accepts `cursor` query param (ULID-based), returns `next_cursor`/`has_more` — prevents phantom reads on live streams
- [x] **CSP header** (`api/rest.go`): Content-Security-Policy set on all responses

## Frontend Hardening

- [x] **ConfirmDialog** (`components/ConfirmDialog.tsx`): Reusable accessible dialog with focus trapping, Escape-to-close, ARIA compliance. Integrated into Souls, SoulDetail, Alerts, Journeys, Dashboards, StatusPages, Maintenance pages
- [x] **Souls page decomposition** (`pages/Souls.tsx`): 716 → 466 lines. Extracted `SoulStatsCards`, `SoulFilterBar`, `SoulCreateModal`
- [x] **Color contrast** (`index.css`): Dark theme `--text-muted` lightened from `#94a3b8` → `#adbac7` for WCAG AA compliance

## CI Pipeline

- [x] `.github/workflows/ci.yml` runs full test suite with `-race` flag
- [x] Coverage threshold enforced at 80% (`scripts/check-go-coverage.sh` + `codecov.yml`), with current Go coverage at 88.9%
- [x] Static analysis (gofmt, govet, gosec, govulncheck) gates merges
- [x] Frontend tests and E2E tests run on every PR

## Deployment Artifacts

- [x] Docker image build verified (`Dockerfile` multi-stage build)
- [x] `docker-compose.yml` for single-node and cluster deployment
- [x] Helm chart for Kubernetes (`deploy/helm/anubiswatch/`)
- [x] K8s manifests for raw deployment (`deploy/k8s/`)
- [x] Binary build: `go build -ldflags "-s -w" -o bin/anubis ./cmd/anubis`

## Known Limitations

1. **WAL replay scale**: CobaltDB rebuilds its in-memory B+Tree from the WAL at startup. Recovery now checkpoints live state atomically and survives repeated restarts, but startup time and memory still grow with retained data; capacity-test the expected dataset.
2. **Multi-node deployment**: Raft persistence, snapshots, mutation replication, and method-level gRPC permissions exist, but the production chart intentionally rejects cluster mode because deterministic ordinal formation and Raft mTLS material are not yet packaged or environment-validated.
3. **Environment evidence**: Repository gates cannot prove DNS, certificates, ingress proxy ranges, storage latency/durability, backup restoration, alert delivery, or SLO capacity. Capture preflight, rollout, smoke, restore, and load evidence in the target environment before promotion.

## Deployment Instructions

### Production Preflight

```bash
VALUES=deploy/helm/anubiswatch/values-production.example.yaml \
ANUBIS_PREFLIGHT_CREATE_NAMESPACE=true \
  bash scripts/production-preflight.sh
```

### Single-node (recommended for most deployments)

```bash
# Build
go build -ldflags "-s -w" -o bin/anubis ./cmd/anubis

# Run with TLS behind a reverse proxy
ANUBIS_ADMIN_PASSWORD='YourStrongPass123!' ./bin/anubis serve --single

# Or via Docker
export ANUBIS_ADMIN_PASSWORD='YourStrongPass123!'
docker-compose up -d

# Verify
curl http://localhost:8080/health
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@anubis.watch","password":"YourStrongPass123!"}'
```

### Kubernetes (Helm)

```bash
helm install anubiswatch deploy/helm/anubiswatch \
  -f values-production.yaml \
  --set-string secrets.adminPassword="$ANUBIS_ADMIN_PASSWORD" \
  --set-string secrets.encryptionKey="$ANUBIS_ENCRYPTION_KEY" \
  --atomic \
  --timeout 10m
```

### Smoke Test

```bash
# Health check
curl -s http://localhost:8080/health | jq .

# Authenticate into an HttpOnly cookie jar, then create a monitor.
COOKIE_JAR=$(mktemp)
curl -fsS -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  --cookie-jar "$COOKIE_JAR" \
  -d '{"email":"admin@anubis.watch","password":"YourStrongPass123!"}'

curl -fsS -X POST http://localhost:8080/api/v1/souls \
  --cookie "$COOKIE_JAR" \
  -H "Content-Type: application/json" \
  -d '{"name":"Example","type":"http","target":"https://example.com","weight":"60s","timeout":"10s","http":{"method":"GET","valid_status":[200]}}'
rm -f "$COOKIE_JAR"
```
