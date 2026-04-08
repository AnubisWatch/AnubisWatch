# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

AnubisWatch is a zero-dependency, single-binary uptime and synthetic monitoring platform written in Go. It uses Egyptian mythology theming throughout the codebase.

## Common Commands

### Build
```bash
# Build the binary (requires dashboard build first)
make build
# Or directly: CGO_ENABLED=0 go build -ldflags "-s -w" -o bin/anubis ./cmd/anubis

# Build dashboard (React 19 + Tailwind 4.1)
make dashboard
# Or directly: cd web && npm ci && npm run build

# Build everything
make all
```

### Test
```bash
# Run all tests with race detection and coverage
make test
# Or directly: go test -race -coverprofile=coverage.out ./...

# Run short tests only
make test-short

# Run a single test
rtk go test -race -run TestName ./path/to/package

# Run integration tests (requires running server)
go test -v -tags=integration ./...
```

### Development
```bash
# Run in development mode (single node, uses ./anubis.yaml)
make dev
# Or directly: go run ./cmd/anubis serve --single --config ./anubis.yaml

# Initialize default config
anubis init

# Run with custom config
anubis serve --config ./anubis.yaml

# Format code
make fmt

# Run linter
make lint
```

### CLI Commands
```bash
# Show version
anubis version

# Initialize configuration
anubis init

# Quick-add a monitor
anubis watch https://example.com --name "Example"

# Show current status
anubis judge

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

### Project Structure

```
cmd/anubis/              # CLI entry point
├── main.go              # Command routing and CLI handling
├── server.go            # Server initialization and dependency injection
├── init.go              # Config initialization (interactive and simple)
└── config.go            # Config file discovery and loading

internal/
├── core/                # Domain types (Soul, Judgment, Verdict, Config)
├── api/                 # REST API, WebSocket, MCP server
├── probe/               # Protocol checkers (HTTP, TCP, DNS, ICMP, etc.)
├── storage/             # CobaltDB B+Tree storage engine with WAL
├── alert/               # Alert engine (Ma'at) and dispatchers
├── raft/                # Raft consensus implementation
├── cluster/             # Cluster coordination and node distribution
├── journey/             # Synthetic monitoring executor
├── auth/                # Local authentication
├── acme/                # Let's Encrypt/ZeroSSL integration
├── statuspage/          # Public status page handler
└── dashboard/           # React dashboard embedding

web/                     # React 19 + Tailwind 4.1 dashboard source
```

### Key Components

#### Probe Engine (`internal/probe/`)
- `checker.go` - Checker interface and registry
- `engine.go` - Scheduling and execution
- `http.go`, `tcp.go`, `dns.go`, etc. - Protocol implementations
- All checkers implement the `Checker` interface with `Type()`, `Judge()`, and `Validate()` methods

#### Storage (`internal/storage/`)
- `engine.go` - CobaltDB B+Tree implementation with configurable order (default 32)
- `judgments.go` - Time-series judgment storage
- `raft_log.go` - Raft log storage adapter
- Uses WAL for crash recovery

#### API Layer (`internal/api/`)
- `rest.go` - REST API server with custom router
- `websocket.go` - Real-time updates (Duat)
- `mcp.go` - Model Context Protocol server for AI integration

#### Raft Consensus (`internal/raft/`)
- `node.go` - Raft node implementation
- `fsm.go` - Finite state machine for log application
- `transport.go` - HTTP transport for Raft RPC
- `distributor.go` - Work distribution across nodes

## Domain Types

### Soul Status Values
- `alive` - Service healthy
- `dead` - Service failing
- `degraded` - Performance issues
- `unknown` - Not yet checked
- `embalmed` - Maintenance mode

### Check Types
`http`, `tcp`, `udp`, `dns`, `smtp`, `imap`, `icmp`, `grpc`, `websocket`, `tls`

## Configuration

Config files support JSON or YAML format. Default locations checked in order:
1. `./anubis.json`
2. `./anubis.yaml`
3. `~/.config/anubis/anubis.json`
4. `/etc/anubis/anubis.json`

Environment variables:
- `ANUBIS_CONFIG` - Config file path
- `ANUBIS_DATA_DIR` - Data directory (default: `/var/lib/anubis`)
- `ANUBIS_LOG_LEVEL` - Log level (debug, info, warn, error)

## Testing Guidelines

- All packages should maintain >80% test coverage
- Use table-driven tests for multiple scenarios
- Mock external dependencies (network calls, time)
- Run with `-race` flag to detect race conditions
- Integration tests use `//go:build integration` tag

## Dependencies

Minimal external dependencies (zero-dependency goal):
- `golang.org/x/net` - Extended networking
- `gopkg.in/yaml.v3` - YAML parsing
- `github.com/gorilla/websocket` - WebSocket support

Dashboard (Node.js):
- React 19, Tailwind 4.1, Vite 6
- Recharts for visualizations, Zustand for state
