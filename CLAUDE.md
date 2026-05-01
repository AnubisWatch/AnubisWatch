# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

AnubisWatch is a zero-dependency, single-binary uptime and synthetic monitoring platform written in Go. It uses Egyptian mythology theming throughout the codebase.

## Common Commands

### Prerequisites

- Go 1.26+
- Node.js 22+ (for dashboard)
- Make (optional)

### Build
```bash
# Build the binary (requires dashboard built first due to Go embed)
make build
# Or directly: CGO_ENABLED=0 go build -ldflags "-s -w" -o bin/anubis ./cmd/anubis

# Build dashboard (React 19 + Tailwind 4.1, embedded in binary)
make dashboard
# Or directly: cd web && pnpm ci && pnpm run build

# Dashboard dev server (hot reload)
make dashboard-dev
# Or directly: cd web && pnpm run dev

# Build everything (dashboard + binary)
make all

# Cross-compile for all platforms
make build-all

# Build Docker image
make docker
```

### Test
```bash
# Run all tests with race detection and coverage (takes ~2-3 minutes)
make test
# Or directly: go test -race -coverprofile=coverage.out ./...

# Run short tests only (skips long-running tests)
make test-short

# Run a single test by exact name
go test -race -run TestName ./path/to/package

# Run benchmarks (e.g., in internal/probe or internal/storage)
go test -bench=. -benchmem ./internal/probe

# Run integration tests (requires running server)
go test -v -tags=integration ./...
```

### Development
```bash
# Run in development mode (single node, uses ./anubis.yaml)
make dev
# Or directly: go run ./cmd/anubis serve --single --config ./anubis.yaml

# Run after building (uses bin/anubis)
make run

# Initialize default config
./bin/anubis init

# Quick-add a monitor (requires TARGET env var)
make watch TARGET=https://example.com

# Show current judgments
make judge

# Run with custom config
anubis serve --config ./anubis.yaml

# Format code
make fmt

# Run linter
make lint

# Run go vet
make vet

# Download dependencies
make deps

# Update dependencies
make deps-update

# Tidy go modules
make tidy
```

### CLI Commands
```bash
# Show version
anubis version
anubis version --json    # JSON output

# Initialize configuration
anubis init

# Quick-add a monitor
anubis watch https://example.com --name "Example"

# Show current judgments
anubis judge

# Server management
anubis serve --single                    # Single node mode
anubis serve --config ./anubis.yaml      # Custom config
anubis status                            # Show server status
anubis logs --follow                     # View logs
anubis config validate                   # Validate config
anubis config show                       # Show current config
anubis export --format json              # Export data

# Backup & Restore
anubis backup --output ./backup.tar.gz
anubis restore --input ./backup.tar.gz

# Cluster management
anubis necropolis              # Show cluster status
anubis summon 10.0.0.2:7946    # Add node to cluster
anubis banish jackal-02        # Remove node from cluster
```

## Architecture

### Egyptian Mythology Theming

| Term | Meaning | File Location |
|------|---------|---------------|
| **Soul** | A monitored target (HTTP endpoint, TCP port, etc.) | `internal/core/soul.go` |
| **Judgment** | A single check execution result | `internal/core/judgment.go` |
| **Verdict** | An alert decision based on judgment patterns | `internal/core/verdict.go` |
| **Jackal** | A probe node that performs health checks | `internal/probe/` |
| **Pharaoh** | The Raft leader in a cluster | `internal/raft/` |
| **Necropolis** | The distributed cluster | `internal/cluster/` |
| **Feather** | The embedded B+Tree storage engine (CobaltDB) | `internal/storage/engine.go` |
| **Ma'at** | The alert engine (goddess of truth) | `internal/alert/` |
| **Duat** | The WebSocket real-time layer | `internal/api/websocket.go` |
| **Journey** | Multi-step synthetic monitoring | `internal/journey/` |

### Dependency Injection

`cmd/anubis/server.go` → `BuildServerDependencies()` is the central DI function that wires everything:
Config → CobaltDB → Authenticator → AlertManager → ProbeEngine → JourneyExecutor → ClusterManager → Dashboard → StatusPage → MCPServer → RESTServer → WebSocket callback → gRPCServer

Adapter pattern bridges between packages: `probeStorageAdapter`, `restStorageAdapter`, `grpcStorageAdapter`, `grpcProbeAdapter`, `clusterAdapter`, `alertStorageAdapter`.

### Probe Engine

All checkers implement the `Checker` interface (`internal/probe/checker.go`):
```go
Type() core.CheckType
Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error)
Validate(soul *core.Soul) error
```
10 protocols registered via `CheckerRegistry`: HTTP, TCP, UDP, DNS, SMTP, IMAP, ICMP, gRPC, WebSocket, TLS.

Security: HTTP checker includes SSRF protection that blocks private/reserved IP ranges (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 127.0.0.0/8, 169.254.0.0/16, 0.0.0.0/8, 192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24, 192.0.0.0/24, 240.0.0.0/4, fc00::/7, fe80::/10, ::1/128) with support for hex/octal IP notation and CIDR parsing.

### Storage Engine (Feather/CobaltDB)

In-memory B+Tree with configurable order (default 32, range 4–256), WAL for crash recovery, optional AES-256-GCM encryption. Keys are workspace-scoped (`{workspaceID}/souls/{soulID}`). Supports `Get`, `Put`, `Delete`, `List`, `PrefixScan`, `RangeScan`. Leaf node chaining enables efficient range scans. Storage is at `internal/storage/engine.go`.

### API Layer

Custom router (no third-party library) with parameterized routes (`:param` syntax). Middleware chain: logging → security headers → CORS → recovery → JSON validation → path param validation → rate limiting. Auth via `requireAuth()` (token) and `requireRole()` (RBAC). 80+ routes under `/api/v1/`. SSE fallback at `/api/v1/events`. OpenAPI 3.0.3 spec at `.project/openapi.yaml` and served inline at `/api/v1/spec`.

### Authentication (`internal/auth/`)

Three authenticator implementations selected by `auth.type` in config:
- **Local** (`local.go`) — bcrypt (cost 12), session persistence, brute-force protection (5 attempts / 15-min lockout), password policy (12+ chars, 3 of 4 classes), timing-attack resistant enumeration prevention, password reset tokens (24h expiry)
- **OIDC** (`oidc.go`) — OpenID Connect with `.well-known` discovery, JWK caching (24h TTL), RSA/EC key support, full JWT validation (exp, nbf, iss, aud, azp), HMAC state for clusters, local auth fallback
- **LDAP** (`ldap.go`) — LDAP/Active Directory with StartTLS, service account search, DN/filter escaping (injection prevention), local auth fallback

All implementations use constant-time comparison for secrets and secure random token generation.

### Soul Status Values
- `alive` — Service healthy
- `dead` — Service failing
- `degraded` — Performance issues
- `unknown` — Not yet checked
- `embalmed` — Maintenance mode

### Check Types
`http`, `tcp`, `udp`, `dns`, `smtp`, `imap`, `icmp`, `grpc`, `websocket`, `tls`

## Configuration

Config files support JSON or YAML format. Default locations checked in order:
1. `./anubis.json`
2. `./anubis.yaml`
3. `~/.config/anubis/anubis.json`
4. `/etc/anubis/anubis.json`

Environment variable expansion in config values: `${VAR}` and `${VAR:-default}` syntax.

Key environment variable overrides:
- `ANUBIS_CONFIG` — Config file path
- `ANUBIS_DATA_DIR` — Data directory (default: `/var/lib/anubis`)
- `ANUBIS_LOG_LEVEL` — Log level (debug, info, warn, error)
- `ANUBIS_PORT` — Server port (default: 8443)
- `ANUBIS_ENCRYPTION_KEY` — Storage encryption key (hex-encoded 32 bytes)
- `ANUBIS_CLUSTER_SECRET` — Cluster shared secret (for HMAC validation)
- `ANUBIS_ADMIN_PASSWORD` — Initial admin password (only on first startup)

Default ports: server 8443, gRPC 9090, cluster bind 0.0.0.0:7946.

Example config generation: `anubis init` creates `anubis.yaml` with sensible defaults.

## Testing Guidelines

- All packages should maintain >80% test coverage
- Standard `testing` package only (no testify or assertion libraries)
- Table-driven tests for multiple scenarios
- `httptest.NewServer` for HTTP checker tests, `t.TempDir()` for storage tests
- Run with `-race` flag to detect race conditions
- Integration tests use `//go:build integration` tag
- Chaos tests (`internal/raft/chaos_test.go`) and load tests (`internal/probe/load_test.go`) run on main branch only in CI
- Benchmark tests available in probe, storage, and API packages
- Security tests (e.g., SSRF protection in `internal/probe/ssrf_test.go`) validate security controls

## Security Considerations

- SSRF protection: HTTP checker validates targets against private/reserved IP ranges
- Authentication: bcrypt cost 12, constant-time comparison, brute-force protection
- Storage: Optional AES-256-GCM encryption for data at rest
- Cluster: HMAC-signed messages, shared secret validation
- API: Rate limiting, input validation, CORS, security headers

## CI Pipeline

10 jobs in `.github/workflows/ci.yml`:
1. `test-backend` — 80% coverage minimum
2. `test-frontend` — Vitest + Playwright
3. `lint` — golangci-lint with custom config
4. `build` — Binary build verification
5. `chaos-tests` — Raft cluster fault injection (main only)
6. `load-tests` — Performance benchmarks (main only)
7. `integration-tests` — Full stack integration (main only)
8. `helm-tests` — Kubernetes chart validation
9. `security` — gosec + Nancy dependency scanning
10. `docker-security` — Trivy container scanning

Additional workflows: `docker-build.yml` (multi-arch images), `release.yml` (automated releases with Homebrew).

## Dependencies

Direct Go dependencies:
- `github.com/coder/websocket` v1.8.14 — WebSocket support
- `github.com/go-ldap/ldap/v3` v3.4.13 — LDAP authentication
- `golang.org/x/crypto` v0.49.0 — bcrypt password hashing
- `golang.org/x/net` v0.52.0 — Extended networking
- `google.golang.org/grpc` v1.80.0 — gRPC server
- `google.golang.org/protobuf` v1.36.11 — Protocol buffers
- `gopkg.in/yaml.v3` v3.0.1 — YAML config parsing

Dashboard (web/):
- React 19, React Router DOM 7, Tailwind 4.1, Vite 6
- Recharts, Zustand 5, Lucide React icons
- Vitest 4 + React Testing Library for unit tests, Playwright for e2e
- Uses pnpm for package management

Module: `github.com/AnubisWatch/anubiswatch`
Go version: 1.26.2
