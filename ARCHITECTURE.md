# AnubisWatch Architecture

## Table of Contents

1. [Purpose and Product Scope](#purpose-and-product-scope)
2. [Architectural Goals](#architectural-goals)
3. [Domain Language](#domain-language)
4. [High-Level System View](#high-level-system-view)
5. [Runtime Composition](#runtime-composition)
6. [Core Domain Model](#core-domain-model)
7. [Configuration Model](#configuration-model)
8. [Storage Architecture](#storage-architecture)
9. [Probe Engine](#probe-engine)
10. [Alerting Architecture](#alerting-architecture)
11. [Journey Synthetic Monitoring](#journey-synthetic-monitoring)
12. [API Layer](#api-layer)
13. [Authentication and Authorization](#authentication-and-authorization)
14. [Real-Time Delivery](#real-time-delivery)
15. [Dashboard Architecture](#dashboard-architecture)
16. [Status Pages](#status-pages)
17. [Clustering and Replication](#clustering-and-replication)
18. [gRPC and MCP Interfaces](#grpc-and-mcp-interfaces)
19. [Observability](#observability)
20. [Security Architecture](#security-architecture)
21. [Backup, Retention, and Maintenance](#backup-retention-and-maintenance)
22. [Deployment Topologies](#deployment-topologies)
23. [Repository Layout](#repository-layout)
24. [End-to-End Data Flows](#end-to-end-data-flows)
25. [Architectural Decisions](#architectural-decisions)

---

## Purpose and Product Scope

AnubisWatch is a self-hosted uptime, synthetic monitoring, alerting, and status-page platform implemented primarily as a Go single binary named `anubis`. It monitors external and internal targets, stores check results, evaluates alert rules, exposes APIs, serves an embedded React dashboard, and can optionally run as a Raft-backed cluster.

The platform is designed around embedded operation: the server, storage engine, API layer, probe scheduler, alert manager, dashboard assets, and operational endpoints are assembled in-process rather than requiring a separate database or application server.

---

## Architectural Goals

- **Single-binary operation:** distribute and run AnubisWatch as one executable with embedded storage and dashboard assets.
- **Self-hosted control:** keep monitoring data, alert state, and configuration under the operator's control.
- **Protocol breadth:** support HTTP, TCP, DNS, SMTP, ICMP, TLS, gRPC, and WebSocket checks through a common probe engine.
- **Synthetic workflows:** execute multi-step journeys for user-flow and API-flow monitoring.
- **Workspace isolation:** isolate user-facing resources by workspace/tenant.
- **Real-time visibility:** stream judgments, verdicts, incidents, and dashboard updates over WebSocket/SSE-style channels.
- **Optional high availability:** run as a standalone node by default or as a Raft cluster when configured.
- **Security-by-default:** require authentication for privileged APIs, enforce role checks, validate inputs, and keep insecure TLS behavior behind an explicit process-wide gate.
- **Operational transparency:** expose health, readiness, metrics, OpenAPI, logs, and tracing hooks.

---

## Domain Language

The codebase uses Egyptian mythology terms as the product vocabulary.

| Term | Meaning | Primary Code Area |
| --- | --- | --- |
| **Soul** | A monitored target, such as an HTTP endpoint, TCP service, DNS name, or gRPC service. | `internal/core/soul.go` |
| **Judgment** | The result of one probe execution against a soul. | `internal/core/judgment.go` |
| **Verdict** | An alert decision produced from one or more judgments. | `internal/core/verdict.go` |
| **Ma'at** | The alerting/rule-evaluation subsystem. | `internal/alert/` |
| **Feather** | The storage abstraction backed by CobaltDB. | `internal/storage/` |
| **CobaltDB** | Embedded B+Tree storage engine with WAL, secondary indexes, and optional encryption. | `internal/storage/engine.go` |
| **Jackal** | A probe node/worker that executes checks. | `internal/probe/` |
| **Necropolis** | Cluster/distribution layer. | `internal/cluster/`, `internal/raft/` |
| **Journey** | Multi-step synthetic monitoring workflow. | `internal/journey/` |
| **Duat** | Real-time WebSocket layer for live events. | `internal/api/websocket.go` |

---

## High-Level System View

```text
+--------------------------+        HTTPS / WebSocket        +----------------------+
| React Dashboard / Users  | <-----------------------------> | REST API + WS Server |
+--------------------------+                                  +----------+-----------+
                                                                         |
                                                                         |
                       +----------------+   judgments   +----------------v---------+
                       | Probe Engine   | ------------> | Storage / CobaltDB      |
                       | protocol checks|               | B+Tree + WAL + indexes  |
                       +-------+--------+               +-------------+------------+
                               |                                      |
                               | results                              | queries/events
                               v                                      v
                       +----------------+                    +---------------------+
                       | Alert Manager  | ---- verdicts ---> | Dashboard / Status  |
                       | rules/channels |                    | Pages / APIs        |
                       +----------------+                    +---------------------+

Optional cluster mode:

+-----------+       Raft log / peer transport       +-----------+       +-----------+
| Node A    | <-----------------------------------> | Node B    | <---> | Node C    |
| leader or |                                      | follower  |       | follower  |
| follower  |                                      |           |       |           |
+-----------+                                      +-----------+       +-----------+
```

At runtime, `cmd/anubis/server.go` composes the main subsystems: configuration, storage, authentication, probe engine, journey executor, alert manager, cluster manager, REST server, gRPC server, dashboard handler, status page handler, MCP server, and telemetry provider.

---

## Runtime Composition

The `anubis` CLI exposes operational commands from `cmd/anubis/`, including server startup, initialization, monitoring shortcuts, status/judgment commands, backup handling, cluster commands, and system utilities.

The server process is composed in layers:

1. **Configuration loading** from YAML/JSON and environment/CLI overrides.
2. **Storage initialization** through CobaltDB and repository adapters.
3. **Authentication setup** for local, OIDC, or LDAP-backed identity.
4. **Probe engine startup** with protocol checkers, concurrency limits, and circuit breakers.
5. **Alert manager startup** with rule evaluation and notification dispatchers.
6. **Journey executor startup** for multi-step synthetic checks.
7. **Cluster manager startup** if Necropolis/Raft mode is enabled.
8. **HTTP REST/WebSocket server startup** for API, dashboard, status pages, metrics, OpenAPI, and MCP.
9. **gRPC server startup** where configured.
10. **Graceful shutdown** for HTTP, gRPC, probe execution, storage, cluster, and telemetry resources.

---

## Core Domain Model

### Soul

A `Soul` is the central monitored resource. It contains identity, workspace ownership, check type, target, interval (`Weight`), timeout, tags, region restrictions, and protocol-specific configuration blocks.

Supported check families include:

- HTTP/HTTPS
- TCP
- UDP data model support
- DNS
- SMTP
- IMAP data model support
- ICMP
- gRPC
- WebSocket
- TLS certificate checks

### Judgment

A `Judgment` records one probe execution. It includes:

- soul and workspace identifiers
- jackal/node identifier and region
- timestamp and duration
- final status
- protocol-specific status code
- human-readable message
- protocol-specific details such as response headers, DNS answers, packet-loss metrics, TLS metadata, or redirect chains

Judgments are the append-heavy time-series data feeding charts, incident detection, and alert rules.

### Verdict

A `Verdict` is an alert decision. It binds a workspace, soul, alert rule, severity, status, message, timestamps, and the judgments that caused it. Verdicts move through active, acknowledged, and resolved states.

### Workspace

Workspace IDs are carried through souls, judgments, verdicts, dashboards, status pages, channels, rules, journeys, and indexes. This gives the API and storage layers a consistent tenant boundary.

---

## Configuration Model

`internal/core/config.go` defines the root configuration. Major sections include:

- `server`: HTTP/gRPC host, ports, TLS, CORS, proxy, and API behavior.
- `storage`: embedded storage path, encryption, retention, and engine configuration.
- `necropolis`: clustering and peer/raft settings.
- `tenants`: workspace defaults and multi-tenancy behavior.
- `auth`: local, OIDC, and LDAP authentication settings.
- `dashboard`: embedded dashboard behavior.
- `souls`: declarative monitored targets.
- `channels`: notification destinations.
- `verdicts`: alert rules and escalation policy.
- `journeys`: declarative synthetic monitoring workflows.
- `logging`: log level/format/output settings.
- `telemetry`: OpenTelemetry trace exporter settings.
- `security`: process-wide security gates.
- `environment`: runtime environment label.

A key security setting is `security.allow_insecure_skip_verify`. Individual soul configurations may request insecure TLS behavior, but the probe engine only honors that if the process-wide master switch is enabled.

---

## Storage Architecture

The storage subsystem lives under `internal/storage/` and provides the embedded persistence layer used by API handlers, probe results, alerts, status pages, dashboards, journeys, and Raft log storage.

### CobaltDB

`CobaltDB` is the embedded storage engine. Its implementation is optimized for monitoring workloads:

- B+Tree-backed key/value storage.
- Write-ahead logging for durability.
- MVCC-oriented design elements.
- Optional AES-256-GCM encryption.
- Secondary indexes for O(1) lookup by resource ID to workspace ID.
- Workspace index for cross-workspace administrative operations and retention scans.
- Dedicated storage files/modules for judgments, time-series data, dashboards, status pages, journeys, retention, encryption, and Raft logs.

### Indexing Strategy

The engine maintains in-memory secondary indexes such as:

- soul ID to workspace ID
- judgment ID to workspace ID
- channel/rule/journey/incident/status-page/dashboard IDs to workspace ID
- ordered workspace index for deterministic listing and retention operations

This avoids scanning all B+Tree keys for common API and background-maintenance operations.

### Retention and Time Series

Retention logic is separated into `internal/storage/retention.go`, while time-series compaction and metric-oriented data handling live in `internal/storage/timeseries.go`. The architecture treats raw judgments and aggregated time-series data as related but distinct storage concerns.

---

## Probe Engine

The probe engine in `internal/probe/` schedules and executes checks for assigned souls. It is responsible for:

- maintaining the lifecycle of active monitored targets
- dispatching protocol-specific checkers
- enforcing maximum concurrent checks
- applying per-soul circuit breakers
- attaching node/region metadata
- producing judgments
- storing results
- notifying the alert manager

`EngineConfig` includes defaults such as 100 concurrent checks and a circuit breaker with failure and success thresholds. The engine is intentionally protocol-agnostic: protocol-specific behavior is implemented in checker files such as `http.go`, `tcp.go`, `dns.go`, `smtp.go`, `icmp.go`, `grpc.go`, `tls.go`, and `websocket.go`.

### Circuit Breaker Behavior

Circuit breakers prevent repeatedly failing targets from consuming excessive resources. A soul can transition into an open state after repeated failures, then later attempt recovery after a timeout and close after sufficient successes.

### SSRF and TLS Controls

The probe package contains SSRF protection and checker-level security tests. TLS verification bypasses require both a per-check configuration and the global `AllowInsecureSkipVerify` gate.

---

## Alerting Architecture

The alert manager in `internal/alert/` evaluates judgments against configured rules and manages alert delivery. Its responsibilities include:

- evaluating rule conditions and scopes
- tracking failure streaks for consecutive-failure conditions
- creating and updating verdicts/incidents
- deduplicating repeated alerts
- routing notifications to channels
- supporting acknowledgements and resolutions
- tracking sent/failed alert metrics

Alert channels are represented in core models and persisted through the storage interface. Dispatchers implement channel-specific delivery behavior. The codebase includes support for common notification classes such as email, Slack, Discord, Telegram, PagerDuty, generic webhooks, SMS, and Opsgenie-style integrations.

Alert rules are workspace-aware and can be scoped to selected souls, tags, workspaces, regions, or global criteria depending on the rule configuration.

---

## Journey Synthetic Monitoring

Journeys are multi-step synthetic monitoring workflows implemented under `internal/journey/`. A journey can represent a user flow or API transaction that requires ordered steps rather than a single target check.

The executor provides:

- scheduled journey execution
- per-journey HTTP client state
- cookie jar support across steps
- variable extraction and substitution
- assertions against responses
- result persistence
- failure reporting into the same observability and alerting ecosystem as normal checks

Journeys complement souls: a soul asks "is this endpoint/service healthy?" while a journey asks "does this workflow still work end to end?"

---

## API Layer

The REST API lives primarily in `internal/api/rest.go`. It uses a custom router and middleware stack rather than relying on a large external web framework.

### Middleware Stack

Route setup applies middleware in this order:

1. request/response logging
2. security headers
3. CORS
4. panic recovery
5. JSON depth and request-size validation
6. path-parameter validation
7. rate limiting

### Public and Operational Endpoints

Unauthenticated operational/public endpoints include:

- `GET /health`
- `GET /ready`
- `GET /metrics`
- `GET /api/openapi.json`
- `GET /api/docs`
- public status-page endpoints such as `/status`, `/status.html`, and `/public/status`

### Authenticated API Resources

The versioned API namespace is `/api/v1`. Major resource groups include:

- `auth`: login, logout, current user, workspace switching, password management, OIDC login/callback
- `souls`: CRUD, force checks, per-soul judgments
- `judgments`: direct judgment retrieval and listing
- `channels`: alert channel CRUD and test delivery
- `rules`: alert-rule CRUD
- `workspaces`: workspace CRUD
- `stats`: overview and dashboard statistics
- `cluster`: cluster status and peer data
- `config`: runtime configuration inspection/update
- `incidents`: list, acknowledge, resolve
- `status-pages`: status-page CRUD
- `mcp`: Model Context Protocol endpoint
- `alerts/*`: frontend-compatible aliases for channels and rules

The API layer depends on interfaces for storage, probe execution, alert management, authentication, clustering, and journey execution, keeping HTTP request handling separated from subsystem implementations.

---

## Authentication and Authorization

Authentication code lives in `internal/auth/` and supports:

- local username/password authentication
- OIDC login and callback handling
- LDAP-backed authentication

The REST server protects most `/api/v1` resources with authentication and applies role/permission checks for mutating operations. Examples include permissions such as `souls:*`, `channels:*`, `rules:*`, `settings:write`, and `members:*`.

Workspace switching is modeled as an authenticated operation, allowing the same user session to work against a selected tenant context.

---

## Real-Time Delivery

Real-time event delivery is handled by `internal/api/websocket.go`. The WebSocket server maintains connected clients, rooms, and broadcast channels. It supports:

- authenticated WebSocket clients
- allowed-origin checks for CSRF protection
- per-IP and per-user connection limits
- connection-attempt rate limiting
- message rate limiting
- room-based broadcasts
- structured WebSocket message types

This layer is used by the dashboard to show live monitoring data without polling every resource continuously.

---

## Dashboard Architecture

The dashboard lives under `web/` and is embedded into the Go binary for production serving.

Technology stack:

- React 19
- React Router 7
- Zustand 5 for client state
- Recharts for charts
- Tailwind CSS 4
- Vite 8
- TypeScript 6
- Vitest and Testing Library for tests
- Playwright for end-to-end tests

The web app is organized around API clients, hooks, reusable components, dashboard pages, widgets, stores, styles, and utilities. A build/embed script produces dashboard assets that the Go server can serve through the dashboard handler.

The dashboard consumes REST endpoints for resource operations and WebSocket events for live updates.

---

## Status Pages

Status page support spans:

- core status-page models in `internal/core/statuspage.go`
- storage persistence in `internal/storage/statuspage.go`
- API handlers in `internal/api/statuspage.go`
- runtime handlers in `cmd/anubis/server.go`
- public endpoints under `/status`, `/status.html`, and `/public/status`

Status pages provide a public, unauthenticated view of selected service health while keeping administrative operations behind authenticated API routes.

---

## Clustering and Replication

Clustering is implemented through `internal/cluster/` and `internal/raft/`. The system can run standalone or in a distributed Necropolis mode.

Major responsibilities:

- node lifecycle management
- Raft consensus and leader/follower state
- peer discovery
- Raft transport
- replicated log storage
- cluster status reporting
- optional peer TLS and mutual TLS

Peer TLS configuration supports certificate/key loading, custom CA pools, and optional client-certificate verification. When clustering is disabled, the same application stack runs against local embedded storage only.

---

## gRPC and MCP Interfaces

### gRPC

The gRPC server lives under `internal/grpcapi/`, with generated protocol files in `internal/grpcapi/v1/`. It provides an additional typed API surface for integrations that prefer gRPC over REST.

### MCP

The MCP server lives in `internal/api/mcp.go` and is exposed through `POST /api/v1/mcp`. It registers built-in tools, resources, and prompts so model-driven clients can inspect and operate against AnubisWatch through a structured protocol.

---

## Observability

AnubisWatch exposes several observability surfaces:

- `GET /health` for liveness
- `GET /ready` for readiness
- `GET /metrics` for Prometheus-style metrics
- structured logs through Go's `slog`
- OpenAPI JSON and docs for API discovery
- OpenTelemetry tracing through `internal/telemetry/tracer.go`

Telemetry configuration supports enabling tracing, configuring an OTLP endpoint, and controlling sampling rate. The server owns shutdown of telemetry providers during graceful termination.

---

## Security Architecture

Security is enforced at multiple layers:

- **Transport:** HTTP TLS and optional peer TLS/mTLS for cluster traffic.
- **Authentication:** local, OIDC, and LDAP providers.
- **Authorization:** route-level role and permission checks.
- **Tenant isolation:** workspace IDs on domain resources and storage indexes.
- **Input validation:** JSON depth/size limits and path parameter validation in API middleware.
- **Rate limiting:** REST and WebSocket rate limits.
- **CORS and WebSocket origins:** explicit allowed-origin handling.
- **Security headers:** HTTP response hardening middleware.
- **Probe safety:** SSRF protections and global gates for insecure TLS behavior.
- **Storage confidentiality:** optional AES-256-GCM encryption for embedded storage.
- **Panic recovery:** API middleware prevents panics from crashing request handling.

The most important design pattern is defense in depth: individual subsystems validate their own inputs, while the API and configuration layers provide process-wide constraints.

---

## Backup, Retention, and Maintenance

Backup logic lives under `internal/backup/` and is exposed through CLI/server code in `cmd/anubis/backup.go`. Storage retention and compaction are handled inside the storage package.

Operational maintenance concerns include:

- creating and restoring backups
- pruning old judgments/time-series data according to retention policy
- compacting time-series data
- preserving Raft logs where clustering is enabled
- clean shutdown of background workers

---

## Deployment Topologies

### Single-Node Embedded Deployment

The default topology runs one `anubis` process with embedded CobaltDB storage and an embedded dashboard. This is the simplest deployment and requires the fewest moving parts.

```text
operator/users -> anubis server -> local CobaltDB files
```

### Reverse-Proxy Deployment

A common production topology places AnubisWatch behind Nginx, Caddy, Traefik, or a cloud load balancer for TLS termination, request filtering, and access control.

```text
users -> reverse proxy / load balancer -> anubis server -> local storage
```

### Clustered Deployment

For high availability or distributed probe coordination, multiple AnubisWatch nodes can participate in a Raft cluster. One node acts as leader while followers replicate state and can take over after failure.

```text
users -> load balancer -> anubis nodes -> Raft replication + local stores
```

### Container/Kubernetes Deployment

The repository includes deployment documentation for production operation. In containers, persistent volumes should back the storage directory and configuration directory. Probes that need network visibility must run in a network context that can reach monitored targets.

---

## Repository Layout

```text
.
├── cmd/anubis/              # CLI commands and server composition
├── internal/alert/          # Alert manager, rule evaluation, dispatchers
├── internal/api/            # REST API, WebSocket server, MCP, metrics, status handlers
├── internal/auth/           # Local, OIDC, and LDAP authentication providers
├── internal/backup/         # Backup management
├── internal/cluster/        # Cluster manager and peer TLS setup
├── internal/core/           # Domain models and configuration types
├── internal/dashboard/      # Embedded dashboard serving support
├── internal/grpcapi/        # gRPC server and generated protobuf bindings
├── internal/journey/        # Synthetic journey executor
├── internal/probe/          # Probe scheduler and protocol checkers
├── internal/raft/           # Raft consensus, discovery, and transport
├── internal/statuspage/     # Status page support
├── internal/storage/        # CobaltDB, repositories, WAL, retention, indexes
├── internal/telemetry/      # OpenTelemetry tracing support
├── web/                     # React dashboard source, tests, and build scripts
├── docs/                    # User, deployment, API, ADR, and architecture docs
├── assets/                  # Static assets such as the project banner
└── bin/                     # Local binary/output helpers
```

---

## End-to-End Data Flows

### Creating a Monitor

1. A user creates a soul through the dashboard or `POST /api/v1/souls`.
2. The REST API authenticates the request and checks permissions.
3. The soul is validated and persisted to CobaltDB under the active workspace.
4. The probe engine receives or reloads the soul assignment.
5. Future checks produce judgments for that soul.

### Running a Probe

1. The probe engine scheduler selects a due soul.
2. A protocol checker executes with timeout, SSRF, TLS, and circuit-breaker controls.
3. The checker returns status, duration, protocol metadata, and error context.
4. The engine creates a judgment and persists it.
5. The alert manager evaluates the judgment against rules.
6. WebSocket subscribers and dashboard views receive updated state.

### Alert Evaluation

1. A new judgment reaches the alert manager.
2. Enabled rules matching the soul/workspace/scope are evaluated.
3. Failure streaks and thresholds are updated.
4. A verdict/incident is created, updated, deduplicated, or resolved.
5. Notification dispatchers send alerts to configured channels.
6. API and WebSocket clients observe the changed alert state.

### Journey Execution

1. A journey schedule becomes due.
2. The journey executor builds a stateful HTTP client and variable context.
3. Steps execute in order with assertions and extraction.
4. The result is persisted and surfaced through APIs/dashboard.
5. Failures can feed alerting and incident workflows.

### Clustered Write Path

1. A mutating operation is accepted by the active node.
2. In cluster mode, changes are coordinated through Raft semantics.
3. The log is replicated to peers.
4. Committed state is applied to local embedded storage.
5. Cluster status APIs expose peer and leadership state.

---

## Architectural Decisions

The repository contains Architecture Decision Records in `docs/adr/`. Current ADR topics include:

- Go language choice
- CobaltDB as the custom embedded storage engine
- Raft as the consensus algorithm
- probe architecture with per-soul circuit breakers
- alert deduplication strategy
- workspace-based multi-tenancy
- MCP integration
- zero-external-dependencies policy

These ADRs provide historical context for the major design choices and should be updated when a future change alters a core architectural constraint.
