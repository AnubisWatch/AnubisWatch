# Production Readiness Checklist

**Date:** 2026-06-28  
**Status:** Ready for single-node deployment behind TLS-terminating proxy

## Build Verification

- [x] `go build ./...` — clean, zero errors
- [x] `go vet ./...` — clean, zero warnings
- [x] `govulncheck ./...` — no dependency vulnerabilities
- [x] `pnpm audit` (frontend devDeps) — 7 undici CVEs in devDependencies only (not in production binary)

## Test Verification

- [x] `go test -short ./...` — all packages pass
- [x] `go test -race -short ./...` — all packages pass with race detector
- [x] `go test -coverprofile=coverage.out ./...` — coverage report generated
- [x] Coverage: ~86% (excluding generated protobuf code)
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

## CI Pipeline

- [x] `.github/workflows/ci.yml` runs full test suite with `-race` flag
- [x] Coverage threshold enforced at 80% (codecov.yml)
- [x] Static analysis (gofmt, govet, gosec, govulncheck) gates merges
- [x] Frontend tests and E2E tests run on every PR

## Deployment Artifacts

- [x] Docker image build verified (`Dockerfile` multi-stage build)
- [x] `docker-compose.yml` for single-node and cluster deployment
- [x] Helm chart for Kubernetes (`deploy/helm/anubiswatch/`)
- [x] K8s manifests for raw deployment (`deploy/k8s/`)
- [x] Binary build: `go build -ldflags "-s -w" -o bin/anubis ./cmd/anubis`

## Known Limitations

1. **Multi-node cluster**: Raft `InstallSnapshot` is a stub — cluster log compaction not functional. Single-node mode is fully supported.
2. **Multi-tenancy**: REST API enforces workspace isolation. gRPC layer lacks role-based authorization (all authenticated users can mutate).
3. **TLS**: Container defaults to TLS disabled; operators should terminate TLS at ingress or configure `server.tls.enabled: true`.

## Deployment Instructions

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
  --set secrets.adminPassword='YourStrongPass123!' \
  --set config.server.tls.enabled=false \
  --set ingress.enabled=true \
  --set ingress.hosts[0].host=anubiswatch.example.com
```

### Smoke Test

```bash
# Health check
curl -s http://localhost:8080/health | jq .

# Create a monitor
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@anubis.watch","password":"YourStrongPass123!"}' | jq -r .token)

curl -X POST http://localhost:8080/api/v1/souls \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Example","type":"http","target":"https://example.com","weight":"60s","timeout":"10s","http":{"method":"GET","valid_status":[200]}}'
```
