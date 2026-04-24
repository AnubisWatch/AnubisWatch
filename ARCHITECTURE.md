# AnubisWatch Architecture

## Table of Contents

1. [Overview](#overview)
2. [System Architecture](#system-architecture)
3. [Egyptian Mythology Naming](#egyptian-mythology-naming)
4. [Core Components](#core-components)
5. [Storage Engine (Feather/CobaltDB)](#storage-engine-feathercobaltdb)
6. [Probe Engine & Checkers](#probe-engine--checkers)
7. [Alert System (Ma'at)](#alert-system-maat)
8. [Authentication](#authentication)
9. [API Layer](#api-layer)
10. [Cluster & Distribution (Necropolis)](#cluster--distribution-necropolis)
11. [Journey (Synthetic Monitoring)](#journey-synthetic-monitoring)
12. [Dashboard (React)](#dashboard-react)
13. [Configuration](#configuration)
14. [Security](#security)
15. [Data Flow](#data-flow)
16. [Technology Stack](#technology-stack)

---

## Overview

AnubisWatch is a **zero-dependency, single-binary uptime and synthetic monitoring platform** written in Go. It uses Egyptian mythology theming throughout the codebase and features an embedded React dashboard built with React 19, Tailwind 4.1, and Zustand 5.

The system monitors HTTP/HTTPS endpoints, TCP/UDP ports, DNS servers, SMTP/IMAP mail servers, ICMP ping, gRPC services, WebSocket endpoints, and TLS certificates across distributed probe nodes.

**Key Characteristics:**
- Single binary deployment (no external dependencies)
- Embedded B+Tree storage engine (CobaltDB) with WAL
- Distributed monitoring via Raft-based clustering
- Synthetic monitoring with multi-step journeys
- Real-time WebSocket updates with SSE fallback
- Multi-auth backend: Local, OIDC, LDAP

---

## System Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              AnubisWatch Binary                             │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐    ┌────────────┐ │
│  │   REST API   │    │  WebSocket  │    │   gRPC API   │    │    MCP     │ │
│  │   :8443      │    │   :8443     │    │   :9090      │    │   Server   │ │
│  └──────┬───────┘    └──────┬───────┘    └──────┬───────┘    └─────┬──────┘ │
│         │                   │                   │                   │        │
│  ┌──────┴───────────────────┴───────────────────┴───────────────────┴────┐ │
│  │                           Middleware Layer                              │ │
│  │  Logging → Security Headers → CORS → Recovery → Rate Limiting            │ │
│  └─────────────────────────────────┬──────────────────────────────────────┘ │
│                                    │                                         │
│  ┌─────────────────────────────────┴──────────────────────────────────────┐ │
│  │                        Service Layer (DI Wired)                          │ │
│  │                                                                           │ │
│  │  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌──────────────────┐   │ │
│  │  │   Auth     │  │   Alert    │  │   Probe    │  │  Journey         │   │ │
│  │  │  Manager   │  │   Ma'at    │  │  Engine    │  │  Executor        │   │ │
│  │  └────────────┘  └────────────┘  └────────────┘  └──────────────────┘   │ │
│  │                                                                           │ │
│  │  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌──────────────────┐   │ │
│  │  │  Cluster   │  │  Dashboard │  │   Status   │  │  Quota Manager   │   │ │
│  │  │  Manager   │  │   Embed    │  │   Page     │  │     (Sobek)      │   │ │
│  │  └────────────┘  └────────────┘  └────────────┘  └──────────────────┘   │ │
│  └─────────────────────────────────┬──────────────────────────────────────┘ │
│                                    │                                         │
│  ┌─────────────────────────────────┴──────────────────────────────────────┐ │
│  │                        Storage Layer (CobaltDB)                          │ │
│  │                                                                           │ │
│  │         ┌─────────────────────────────────────────────┐                  │ │
│  │         │              B+Tree Index                   │                  │ │
│  │         │         (Configurable Order 4-256)           │                  │ │
│  │         └─────────────────────────────────────────────┘                  │ │
│  │                        WAL (Write-Ahead Log)                            │ │
│  │                     AES-256-GCM Encryption                               │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────────┐│
│  │                     Cluster Layer (Necropolis)                           ││
│  │                                                                           ││
│  │  ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────────────┐    ││
│  │  │   Raft   │    │   Gossip │    │  Probe   │    │  Raft Consensus  │    ││
│  │  │   Log    │    │ Protocol │    │  Coord.  │    │  (Pharaoh Node)  │    ││
│  │  └──────────┘    └──────────┘    └──────────┘    └──────────────────┘    ││
│  └─────────────────────────────────────────────────────────────────────────┘│
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────────┐ │
│  │  ┌─────────────────────────────────────────────────────────────────┐   │ │
│  │  │              React Dashboard (Embedded HTML/CSS/JS)            │   │ │
│  │  │                    React 19 + Tailwind 4.1                       │   │ │
│  │  └─────────────────────────────────────────────────────────────────┘   │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Egyptian Mythology Naming

The codebase uses Egyptian mythology terminology to describe system components:

| Term | Mythology | Real-World Meaning |
|------|-----------|-------------------|
| **Soul** | Ka - the life force | Monitored target (HTTP, TCP, DNS, etc.) |
| **Judgment** | Ma'at's feather | Single health check execution result |
| **Verdict** | Trial outcome | Alert decision based on judgment patterns |
| **Jackal** | Anubis's companion | Probe node that executes health checks |
| **Pharaoh** | Ra - the sun god | Raft leader in a cluster |
| **Necropolis** | City of the dead | Distributed cluster network |
| **Feather** | Ma'at's feather | Embedded B+Tree storage engine |
| **Ma'at** | Goddess of truth | Alert engine |
| **Duat** | Egyptian underworld | WebSocket real-time layer |
| **Journey** | Travel of the soul | Multi-step synthetic monitoring |
| **Sobek** | Crocodile god | Quota management |
| **Djed** | Stability pillar | Configuration persistence |
| **Eye of Horus** | Protection symbol | Health check status indicator |

---

## Core Components

### 1. Storage Engine (Feather/CobaltDB)

Located at `internal/storage/engine.go`, CobaltDB is a custom B+Tree storage engine:

```
┌─────────────────────────────────────────────────────────────┐
│                       CobaltDB                              │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│   WAL (Write-Ahead Log)          B+Tree Index                │
│   ┌─────────────────┐           ┌─────────────────┐         │
│   │  wal.log        │           │  Root Node      │         │
│   │  - PUT key val  │  ───────▶ │  ├─ keys[]      │         │
│   │  - DELETE key   │   replay  │  ├─ values[]    │         │
│   │  - length prefix│           │  └─ children[]  │         │
│   └─────────────────┘           └────────┬────────┘         │
│                                           │                  │
│                      ┌────────────────────┼────────────┐    │
│                      │                    │            │    │
│                ┌─────┴─────┐        ┌────┴────┐  ┌────┴──┐│
│                │ Leaf Node │ ────▶  │ Internal│  │ Leaf  ││
│                │ (chain)  │        │  Node   │  │(chain)││
│                └───────────┘        └─────────┘ └───────┘│
│                                                             │
│   Key Format: {workspaceID}/souls/{soulID}                │
│   Key Format: {workspaceID}/judgments/{soulID}/{ts}        │
│                                                             │
│   Features:                                                 │
│   ✓ Configurable B+Tree order (default 32, range 4-256)   │
│   ✓ WAL for crash recovery                                  │
│   ✓ Optional AES-256-GCM encryption                         │
│   ✓ Leaf node chaining for efficient range scans          │
│   ✓ MVCC support                                            │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### 2. Probe Engine & Checkers

Located at `internal/probe/`, the probe engine executes health checks:

```
┌────────────────────────────────────────────────────────────────┐
│                     Probe Engine                                 │
├────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌──────────────┐    ┌──────────────────┐    ┌──────────────┐ │
│  │ Scheduler   │───▶│ CheckerRegistry  │───▶│   Checker    │ │
│  │ (cron-like) │    │ (10 protocols)   │    │ (per-soul)   │ │
│  └──────────────┘    └──────────────────┘    └──────────────┘ │
│                                                        │       │
│                                                        ▼       │
│                     ┌─────────────────────────────────────┐  │
│                     │         Checker Implementations       │  │
│                     ├─────────────────────────────────────┤  │
│                     │  HTTP     │  TCP    │  UDP          │  │
│                     │  DNS      │  SMTP   │  IMAP         │  │
│                     │  ICMP     │  gRPC   │  WebSocket    │  │
│                     │  TLS      │                     │  │
│                     └─────────────────────────────────────┘  │
│                                                              │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │              SSRF Protection Layer                        │ │
│  │  Blocks: 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16,      │ │
│  │          127.0.0.0/8, 169.254.0.0/16, 0.0.0.0/8,        │ │
│  │          192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24,  │ │
│  │          192.0.0.0/24, 240.0.0.0/4, fc00::/7, fe80::/10, │ │
│  │          ::1/128                                         │ │
│  │  Supports: hex/octal IP notation, CIDR parsing           │ │
│  └──────────────────────────────────────────────────────────┘ │
│                                                              │
│  Output: Judgment {SoulID, Status, Latency, Response, ...}  │
│                                                              │
└────────────────────────────────────────────────────────────┘
```

### 3. Alert System (Ma'at)

Located at `internal/alert/`, Ma'at is the alert engine:

```
┌─────────────────────────────────────────────────────────────────┐
│                     Ma'at - Alert Engine                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Verdict        Rule Engine         Dispatchers                 │
│   ┌───────┐      ┌───────────┐       ┌───────────┐             │
│   │Failed │────▶│ condition │──────▶│  Slack    │             │
│   │Degraded│     │ evaluator │       │  Discord  │             │
│   │Alive   │     └───────────┘       │  Email    │             │
│   └───────┘                         │  PagerDuty│             │
│                                    │  Webhook   │             │
│                                    └───────────┘             │
│                                                                  │
│   Alert Rule Structure:                                          │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │  name: "High Latency Alert"                               │  │
│   │  condition: "response_time > 500ms"                      │  │
│   │  severity: "warning"                                     │  │
│   │  channels: ["slack", "email"]                            │  │
│   │  cooldown: 5m                                            │  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                  │
│   Severity Levels:                                              │
│   ┌─────────┐  ┌────────────┐  ┌──────────┐  ┌──────────────┐  │
│   │ critical│  │   warning  │  │   info   │  │   ok        │  │
│   │ 🔴-red  │  │ 🟡-yellow  │  │ 🔵-blue  │  │ 🟢-green    │  │
│   └─────────┘  └────────────┘  └──────────┘  └──────────────┘  │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

---

## Authentication

Located at `internal/auth/`, three authenticator implementations:

```
┌─────────────────────────────────────────────────────────────────┐
│                   Authentication Backends                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   ┌──────────────────┐  ┌──────────────────┐  ┌──────────────┐ │
│   │      LOCAL        │  │      OIDC        │  │     LDAP     │ │
│   │   (local.go)      │  │    (oidc.go)     │  │   (ldap.go)  │ │
│   ├──────────────────┤  ├──────────────────┤  ├──────────────┤ │
│   │  bcrypt cost 12   │  │ OpenID Connect   │  │ StartTLS     │ │
│   │  brute-force prot │  │ JWK caching 24h  │  │ DN escaping  │ │
│   │  password policy  │  │ RSA/EC support   │  │ Inj. prevent │ │
│   │  timing attack   │  │ JWT validation   │  │ Fallback     │ │
│   │  password reset  │  │ HMAC state       │  │              │ │
│   └──────────────────┘  └──────────────────┘  └──────────────┘ │
│                                                                  │
│   Security Features:                                             │
│   ✓ Constant-time comparison for secrets                         │
│   ✓ Secure random token generation (CSPRNG)                    │
│   ✓ Session persistence to disk (0600 permissions)              │
│   ✓ Brute-force protection (5 attempts / 15-min lockout)       │
│   ✓ Password policy (12+ chars, 3 of 4 classes)                 │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

---

## API Layer

Located at `internal/api/rest.go` with 80+ routes:

```
┌─────────────────────────────────────────────────────────────────┐
│                      API Architecture                            │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Middleware Chain:                                              │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │  Logging → Security Headers → CORS → Recovery →         │   │
│   │  JSON Validation → Path Param → Rate Limiting           │   │
│   └─────────────────────────────────────────────────────────┘   │
│                            │                                     │
│   Route Pattern: /api/v1/{resource}/:id/:action                │
│                                                                  │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │                    API Endpoints                         │   │
│   │                                                         │   │
│   │  Souls:       GET/POST   /api/v1/souls                  │   │
│   │               GET        /api/v1/souls/:id              │   │
│   │               PUT        /api/v1/souls/:id              │   │
│   │               DELETE     /api/v1/souls/:id              │   │
│   │                                                         │   │
│   │  Judgments:   GET        /api/v1/souls/:id/judgments    │   │
│   │                                                         │   │
│   │  Vericts:    GET        /api/v1/souls/:id/verdicts     │   │
│   │                                                         │   │
│   │  Alerts:     GET/POST   /api/v1/rules                  │   │
│   │               GET       /api/v1/rules/:id              │   │
│   │               PUT       /api/v1/rules/:id              │   │
│   │               DELETE    /api/v1/rules/:id              │   │
│   │                                                         │   │
│   │  Channels:   GET/POST   /api/v1/channels               │   │
│   │               GET       /api/v1/channels/:id           │   │
│   │                                                         │   │
│   │  Journey:    GET/POST   /api/v1/journeys               │   │
│   │               POST      /api/v1/journeys/:id/run       │   │
│   │                                                         │   │
│   │  Config:     GET/PUT    /api/v1/config                 │   │
│   │                                                         │   │
│   │  SSE:        GET        /api/v1/events                  │   │
│   │                                                         │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│   Auth Middleware:                                               │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │  requireAuth()     - Validates token                     │   │
│   │  requireRole(admin) - RBAC role check                    │   │
│   │  Cookie: httpOnly, secure, sameSite=strict               │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│   OpenAPI 3.0.3 specification at .project/openapi.yaml          │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

---

## Cluster & Distribution (Necropolis)

Located at `internal/cluster/` and `internal/raft/`:

```
┌─────────────────────────────────────────────────────────────────────┐
│                  Necropolis - Cluster Architecture                  │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   ┌─────────────────┐         ┌─────────────────┐                │
│   │  Pharaoh Node    │◀───────▶│   Jackal Node    │                │
│   │  (Raft Leader)   │         │  (Follower)      │                │
│   │  port: 7946     │         │  port: 7946      │                │
│   └────────┬────────┘         └────────┬────────┘                │
│            │                           │                          │
│            │    Gossip Protocol         │                          │
│            │◀──────────────────────────▶│                          │
│            │                           │                          │
│   ┌────────┴────────────────────────────┴────────┐                │
│   │                  Raft Consensus                     │                │
│   │  - Leader election (Pharaoh)                       │                │
│   │  - Log replication                                 │                │
│   │  - Membership changes                              │                │
│   │  - Snapshotting                                    │                │
│   └───────────────────────────────────────────────────┘                │
│                                                                      │
│   Commands:                                                         │
│   ┌──────────────────────────────────────────────────────────────┐  │
│   │  anubis necropolis          # Show cluster status            │  │
│   │  anubis summon 10.0.0.2     # Add node to cluster            │  │
│   │  anubis banish jackal-02    # Remove node from cluster      │  │
│   └──────────────────────────────────────────────────────────────┘  │
│                                                                      │
│   Probe Coordination:                                               │
│   ┌──────────────────────────────────────────────────────────────┐  │
│   │  - Pharaoh assigns souls to Jackals                         │  │
│   │  - Jackals report judgments back to Pharaoh                 │  │
│   │  - Load balancing across probe nodes                        │  │
│   │  - Heartbeat monitoring                                      │  │
│   └──────────────────────────────────────────────────────────────┘  │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

---

## Journey (Synthetic Monitoring)

Located at `internal/journey/`:

```
┌─────────────────────────────────────────────────────────────────┐
│               Journey - Synthetic Monitoring                     │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Journey Definition:                                            │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │  name: "Checkout Flow"                                    │   │
│   │  steps:                                                   │   │
│   │    - step: 1                                              │   │
│   │      name: "Homepage"                                     │   │
│   │      type: "http"                                         │   │
│   │      url: "https://example.com"                           │   │
│   │      assertions:                                          │   │
│   │        - type: "status"                                   │   │
│   │          operator: "=="                                   │   │
│   │          value: 200                                        │   │
│   │        - type: "body"                                     │   │
│   │          operator: "contains"                             │   │
│   │          value: "Login"                                   │   │
│   │    - step: 2                                              │   │
│   │      name: "API Health"                                   │   │
│   │      type: "http"                                         │   │
│   │      url: "https://api.example.com/health"               │   │
│   │    - step: 3                                              │   │
│   │      name: "WebSocket Connect"                            │   │
│   │      type: "websocket"                                   │   │
│   │      url: "wss://example.com/realtime"                   │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│   Step Types: HTTP, HTTPS, TCP, DNS, WebSocket, gRPC            │
│                                                                  │
│   Assertion Types:                                               │
│   - status (HTTP status code)                                   │
│   - body (response body contains/matches)                      │
│   - header (response header check)                              │
│   - latency (response time threshold)                          │
│   - json (JSON path assertion)                                  │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

---

## Dashboard (React)

Located at `web/`:

```
┌─────────────────────────────────────────────────────────────────┐
│                    React Dashboard Architecture                  │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Tech Stack:                                                    │
│   - React 19 (concurrent features)                              │
│   - React Router DOM 7                                          │
│   - Tailwind CSS 4.1                                             │
│   - Zustand 5 (state management)                               │
│   - Recharts (visualizations)                                   │
│   - Lucide React (icons)                                        │
│   - Vitest 4 (testing)                                          │
│   - Playwright (e2e testing)                                    │
│                                                                  │
│   Theme System:                                                  │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │  Dark Mode          │  Light Mode                       │   │
│   │  ──────────────     │  ─────────────                    │   │
│   │  bg: #0a0a15        │  bg: #f9fafb                      │   │
│   │  bg-card: #1a1a2e   │  bg-card: #ffffff                 │   │
│   │  text: #ffffff      │  text: #111827                    │   │
│   │  accent: #D4AF37    │  accent: #b8860b (darkened gold) │   │
│   │  border: #D4AF37/20 │  border: #e5e7eb                  │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│   State Management (Zustand stores):                            │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │  useAuthStore     - Authentication state, logout      │   │
│   │  useThemeStore    - Theme (dark/light/system)         │   │
│   │  useSoulStore     - Souls list, CRUD operations       │   │
│   │  useAlertStore    - Alert rules, channels              │   │
│   │  useJourneyStore  - Journey definitions                │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│   Pages:                                                         │
│   - Dashboard     (/)          - Overview, stats              │
│   - Souls         (/souls)     - Monitor list                   │
│   - Soul Detail   (/souls/:id) - Individual monitor            │
│   - Journeys      (/journeys)  - Synthetic checks               │
│   - Alerts        (/alerts)    - Alert rules & channels         │
│   - Status Page   (/status)    - Public status page             │
│   - Settings      (/settings)  - Configuration                  │
│                                                                  │
│   API Integration:                                              │
│   Base URL: /api/v1 (proxied via Vite dev server)              │
│   Auth: Bearer token in Authorization header                   │
│   Real-time: WebSocket at /api/v1/events                        │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

---

## Configuration

Configuration files support JSON or YAML:

```
Default locations checked in order:
1. ./anubis.json
2. ./anubis.yaml
3. ~/.config/anubis/anubis.json
4. /etc/anubis/anubis.json
```

### Environment Variable Expansion

```yaml
database:
  host: "${DB_HOST:-localhost}"
  port: "${DB_PORT:-5432}"
  credentials:
    username: "${DB_USER}"
    password: "${DB_PASSWORD}"
```

### Key Configuration Sections

```yaml
server:
  host: "0.0.0.0"
  port: 8443
  # TLS configuration

auth:
  enabled: true
  type: "local"  # local, oidc, ldap
  local:
    admin_email: "admin@example.com"
    admin_password: "${ANUBIS_ADMIN_PASSWORD}"
  oidc:
    issuer: "https://auth.example.com"
    client_id: "${OIDC_CLIENT_ID}"
    client_secret: "${OIDC_CLIENT_SECRET}"
  ldap:
    url: "ldap://ldap.example.com:389"
    base_dn: "dc=example,dc=com"
    bind_dn: "${LDAP_BIND_DN}"
    bind_password: "${LDAP_BIND_PASSWORD}"

cluster:
  enabled: false
  bind_addr: "0.0.0.0:7946"
  gossip_addr: "0.0.0.0:7946"

probe:
  interval: 60s  # Default check interval
  timeout: 10s   # Check timeout
  workers: 10     # Concurrent checkers

storage:
  path: "/var/lib/anubis"
  encryption:
    enabled: false
    key: "${ANUBIS_ENCRYPTION_KEY}"
  btree_order: 32

logging:
  level: "info"  # debug, info, warn, error
  format: "json"  # json, text
```

### Key Environment Variables

| Variable | Description | Default |
|---------|-------------|---------|
| `ANUBIS_CONFIG` | Config file path | - |
| `ANUBIS_DATA_DIR` | Data directory | `/var/lib/anubis` |
| `ANUBIS_LOG_LEVEL` | Log level | `info` |
| `ANUBIS_PORT` | Server port | `8443` |
| `ANUBIS_ENCRYPTION_KEY` | Storage encryption key | - |
| `ANUBIS_CLUSTER_SECRET` | Cluster HMAC secret | - |
| `ANUBIS_ADMIN_PASSWORD` | Initial admin password | - |

---

## Security

```
┌─────────────────────────────────────────────────────────────────┐
│                      Security Architecture                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Transport Security:                                             │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │  TLS 1.2+ required for all external connections          │   │
│   │  HTTP/2 with ALPN negotiation                            │   │
│   │  Strong cipher suites only                              │   │
│   │  Certificate auto-renewal via ACME (Let's Encrypt)      │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│   Authentication Security:                                       │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │  bcrypt cost 12 (password hashing)                       │   │
│   │  Constant-time comparison (prevent timing attacks)       │   │
│   │  CSPRNG for all random generation (tokens, IDs)         │   │
│   │  Brute-force protection (5 attempts / 15-min lockout)   │   │
│   │  Password policy (12+ chars, 3 of 4 character classes)  │   │
│   │  httpOnly, secure, sameSite=strict cookies              │   │
│   │  Timing-attack resistant user enumeration                │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│   Network Security:                                              │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │  SSRF Protection (HTTP checker blocks private ranges)    │   │
│   │  - 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16             │   │
│   │  - 127.0.0.0/8, 169.254.0.0/16, 0.0.0.0/8               │   │
│   │  - 192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24        │   │
│   │  - 192.0.0.0/24, 240.0.0.0/4, fc00::/7, fe80::/10       │   │
│   │  CIDR parsing support, hex/octal IP notation            │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│   API Security:                                                  │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │  Rate limiting (configurable per-endpoint)               │   │
│   │  Input validation (JSON, path parameters)               │   │
│   │  Security headers (HSTS, CSP, X-Frame-Options, etc.)   │   │
│   │  CORS with configurable origin whitelist                 │   │
│   │  HMAC validation for cluster messages                     │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│   Storage Security:                                              │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │  Optional AES-256-GCM encryption at rest                │   │
│   │  WAL written with restrictive permissions (0600)       │   │
│   │  Atomic file operations (temp file + rename)            │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│   Data at Rest Encryption:                                        │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │  Algorithm: AES-256-GCM                                 │   │
│   │  Key: 32 bytes (hex-encoded or env var)                 │   │
│   │  Nonce: 12 bytes (unique per encryption)                │   │
│   │  Auth tag: 16 bytes                                     │   │
│   │  Encrypted keys: {workspaceID}/souls/{soulID}          │   │
│   │  Encrypted values: JSON-encoded data                    │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

---

## Data Flow

```
┌─────────────────────────────────────────────────────────────────────────┐
│                           Data Flow Diagram                               │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│   1. Soul Registration                                                   │
│   ┌─────────┐     POST /api/v1/souls      ┌─────────┐                   │
│   │ Client  │ ──────────────────────────▶ │   REST  │                   │
│   │ (Web)   │                             │   API   │                   │
│   └─────────┘                             └────┬────┘                   │
│                                                │                        │
│                                                ▼                        │
│                                        ┌─────────────┐                 │
│                                        │  CobaltDB  │                 │
│                                        │   (Put)    │                 │
│                                        └─────────────┘                 │
│                                                                          │
│   2. Health Check Execution                                              │
│   ┌─────────────┐     ┌─────────────┐     ┌─────────────┐             │
│   │  Scheduler │ ──▶ │   Probe     │ ──▶ │  Checker   │             │
│   │  (cron)    │     │   Engine    │     │ (HTTP/TCP) │             │
│   └─────────────┘     └─────────────┘     └──────┬──────┘             │
│                                                    │                   │
│                     ┌──────────────────────────────┼───────┐            │
│                     ▼                              ▼       ▼            │
│              ┌───────────┐                   ┌────────┐ ┌────────┐     │
│              │ Judgment  │                   │ Soul   │ │ Alert  │     │
│              │  (core)   │                   │ Status │ │ Engine │     │
│              └─────┬─────┘                   └────────┘ └────┬──┘     │
│                    │                         ▲                │         │
│                    ▼                         │                ▼         │
│              ┌───────────┐                   │         ┌───────────┐  │
│              │ CobaltDB  │                   │         │ Notifiers  │  │
│              │ (Put)     │                   │         │(Slack/Email)│ │
│              └───────────┘                   │         └───────────┘  │
│                                               │                        │
│   3. Real-time Updates                                                │
│              ┌───────────┐     ┌──────────────┐     ┌───────────┐  │
│              │ WebSocket │ ◀── │  Dashboard   │ ◀── │ Judgment  │  │
│              │  Server   │     │   Store      │     │  Created  │  │
│              └─────┬─────┘     │  (Zustand)   │     └───────────┘  │
│                    │           └──────────────┘                     │
│                    ▼                                                 │
│              ┌───────────┐                                           │
│              │  Browser  │                                           │
│              │ (React)   │                                           │
│              └───────────┘                                           │
│                                                                          │
│   4. Cluster Replication                                              │
│   ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐     │
│   │Pharaoh  │───▶│ Raft Log │───▶│ Jackal 1 │───▶│ Jackal 2 │     │
│   │(Leader) │    │          │    │          │    │          │     │
│   └──────────┘    └──────────┘    └──────────┘    └──────────┘     │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## Technology Stack

### Backend (Go)

| Package | Version | Purpose |
|---------|---------|---------|
| `github.com/coder/websocket` | v1.8.14 | WebSocket support |
| `github.com/go-ldap/ldap/v3` | v3.4.13 | LDAP authentication |
| `golang.org/x/crypto` | v0.49.0 | bcrypt, Argon2 |
| `golang.org/x/net` | v0.52.0 | Extended networking |
| `google.golang.org/grpc` | v1.80.0 | gRPC server |
| `google.golang.org/protobuf` | v1.36.11 | Protocol buffers |
| `gopkg.in/yaml.v3` | v3.0.1 | YAML config parsing |

### Frontend (React)

| Package | Version | Purpose |
|---------|---------|---------|
| React | 19 | UI framework |
| react-router-dom | 7 | Routing |
| tailwindcss | 4.1 | CSS framework |
| zustand | 5 | State management |
| recharts | latest | Charts |
| lucide-react | latest | Icons |
| vitest | 4 | Testing |

### Infrastructure

| Component | Technology |
|-----------|------------|
| Language | Go 1.26+ |
| Dashboard | React 19 + Vite 6 |
| Package Manager | pnpm |
| Cluster | Raft consensus |
| Serialization | Protocol Buffers |

---

## Soul Status Values

| Status | Meaning | Icon |
|--------|---------|------|
| `alive` | Service healthy | 🟢 |
| `dead` | Service failing | 🔴 |
| `degraded` | Performance issues | 🟡 |
| `unknown` | Not yet checked | ⚪ |
| `embalmed` | Maintenance mode | 🔵 |

## Check Types

| Type | Protocol | Description |
|------|----------|-------------|
| `http` | HTTP/HTTPS | HTTP health check with SSRF protection |
| `tcp` | TCP | TCP connection check |
| `udp` | UDP | UDP endpoint check |
| `dns` | DNS | DNS server resolution |
| `smtp` | SMTP | SMTP mail server |
| `imap` | IMAP | IMAP mail server |
| `icmp` | ICMP | Ping/echo check |
| `grpc` | gRPC | gRPC service health |
| `websocket` | WebSocket | WebSocket connection |
| `tls` | TLS | TLS certificate expiration |

---

## Directory Structure

```
AnubisWatch/
├── cmd/
│   └── anubis/              # Main application entry
│       └── server.go         # DI wiring, server setup
├── internal/
│   ├── acme/                 # ACME/Let's Encrypt certificate management
│   ├── alert/                # Alert engine (Ma'at)
│   │   └── dispatchers/      # Notification dispatchers
│   ├── api/                  # API layer
│   │   ├── rest/             # REST API handlers
│   │   ├── grpc/             # gRPC handlers
│   │   ├── ws/               # WebSocket handlers
│   │   └── mcp/              # Model Context Protocol server
│   ├── auth/                 # Authentication backends
│   │   ├── local.go          # Local (bcrypt) auth
│   │   ├── oidc.go           # OpenID Connect
│   │   └── ldap.go           # LDAP/Active Directory
│   ├── backup/               # Backup & restore
│   ├── cache/                # In-memory cache
│   ├── cluster/              # Cluster management (Necropolis)
│   ├── core/                 # Core domain types
│   ├── dashboard/            # Dashboard embedding
│   ├── feather/             # Storage manager
│   ├── grpcapi/              # gRPC API server
│   ├── journey/             # Synthetic monitoring
│   ├── metrics/              # Prometheus metrics
│   ├── probe/                # Probe engine & checkers
│   ├── profiling/            # Profiling endpoints
│   ├── quota/                # Quota management (Sobek)
│   ├── raft/                 # Raft consensus implementation
│   ├── region/               # Multi-region support
│   ├── release/              # Release management
│   ├── secrets/              # Secrets management
│   ├── statuspage/           # Status page generator
│   ├── storage/              # CobaltDB storage engine
│   ├── tenant/               # Multi-tenancy
│   ├── tracing/              # Distributed tracing
│   └── version/              # Version info
├── web/                      # React dashboard
│   ├── src/
│   │   ├── components/        # React components
│   │   ├── pages/             # Page components
│   │   ├── stores/            # Zustand stores
│   │   ├── styles/            # CSS files
│   │   ├── api/               # API client
│   │   ├── hooks/             # React hooks
│   │   └── App.tsx            # Main app
│   └── vite.config.ts        # Vite configuration
├── configs/                   # Configuration files
├── .project/                  # OpenAPI specs
├── .github/workflows/         # CI/CD pipelines
└── Makefile                   # Build automation
```

---

## CI Pipeline

Located at `.github/workflows/ci.yml`:

```
┌─────────────────────────────────────────────────────────────────┐
│                    CI Pipeline Jobs                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  1. test-backend          Go tests, 80% coverage minimum         │
│  2. test-frontend        Vitest + Playwright                    │
│  3. lint                 golangci-lint                          │
│  4. build                Binary build verification              │
│  5. chaos-tests          Raft fault injection (main only)       │
│  6. load-tests           Performance benchmarks (main only)     │
│  7. integration-tests    Full stack integration (main only)    │
│  8. helm-tests           Kubernetes chart validation           │
│  9. security             gosec + Nancy dependency scanning     │
│ 10. docker-security      Trivy container scanning              │
│                                                                  │
│  Additional:                                                       │
│  - docker-build.yml      Multi-arch Docker images                │
│  - release.yml           Automated releases + Homebrew          │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

---

## Performance Characteristics

| Metric | Value | Notes |
|--------|-------|-------|
| Binary Size | ~18MB | With embedded dashboard |
| Memory (idle) | ~50MB | Single node, no checks |
| Memory (active) | ~150MB | 100 souls, 60s interval |
| Check Latency | <10ms | Local network targets |
| WAL Recovery | <1s | Typical WAL size |
| B+Tree Operations | O(log n) | Configurable order |
| Concurrent Checks | 100+ | Worker pool configurable |

---

*Document generated: 2026-04-24*
*AnubisWatch v1.0.0*
