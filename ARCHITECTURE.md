# AnubisWatch Architecture

> **"The Judgment Never Sleeps"** — Zero-dependency uptime monitoring platform with Egyptian mythology theme.

## Overview

AnubisWatch is a distributed, highly-available uptime monitoring platform built in Go with a React/TypeScript frontend. It features autonomous health checking ("Judgments"), intelligent alerting, multi-step synthetic monitoring ("Journeys"), and distributed consensus via Raft ("Necropolis").

## System Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              CLIENT LAYER                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│  React + TypeScript    │   WebSocket (Real-time)    │   CLI Tool            │
│  - Vite Build System   │   - Live Updates             - anubis watch        │
│  - Tailwind CSS v4     │   - Streaming Logs           - anubis verdict      │
│  - Recharts            │                              - anubis necropolis   │
└─────────────────────────────────────────────────────────────────────────────┘
                                       │
                                       ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                              API LAYER                                       │
├─────────────────────────────────────────────────────────────────────────────┤
│  REST API (v1)         │   MCP Server         │   WebSocket Server          │
│  - /api/v1/souls       │   - Model Context    │   - /ws/live                │
│  - /api/v1/judgments   │     Protocol         │   - Event streaming         │
│  - /api/v1/channels    │   - Claude/AI        │   - Real-time updates       │
│  - /api/v1/rules       │     Integration      │                             │
│  - /api/v1/stats       │                      │                             │
└─────────────────────────────────────────────────────────────────────────────┘
                                       │
                                       ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                           CORE SERVICES                                      │
├──────────────────────┬──────────────────────┬────────────────────────────────┤
│   PROBE ENGINE       │   ALERT MANAGER      │   JOURNEY EXECUTOR             │
│   (internal/probe)   │   (internal/alert)   │   (internal/journey)           │
├──────────────────────┼──────────────────────┼────────────────────────────────┤
│  - HTTP/HTTPS checks │  - Channel mgmt      │  - Multi-step workflows        │
│  - TCP/UDP probes    │  - Rule evaluation   │  - Synthetic monitoring        │
│  - DNS resolution    │  - Dispatchers       │  - Step sequencing             │
│  - ICMP ping         │    * Email           │  - Failure handling            │
│  - TLS validation    │    * Slack           │                                │
│  - gRPC health       │    * Discord         │                                │
│  - WebSocket         │    * PagerDuty       │                                │
│  - SMTP checks       │    * Webhook         │                                │
└──────────────────────┴──────────────────────┴────────────────────────────────┘
                                       │
                                       ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         CLUSTER LAYER (Necropolis)                           │
├─────────────────────────────────────────────────────────────────────────────┤
│  RAFT Consensus        │   Node Discovery     │   State Machine              │
│  (internal/raft)       │   (internal/cluster) │   (internal/raft/fsm.go)     │
├────────────────────────┼──────────────────────┼──────────────────────────────┤
│  - Leader election     │  - Gossip protocol   │  - Log replication           │
│  - Log replication     │  - Auto-join         │  - State snapshots           │
│  - Snapshots           │  - Health checks     │  - Membership changes        │
└────────────────────────┴──────────────────────┴──────────────────────────────┘
                                       │
                                       ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         STORAGE LAYER (CobaltDB)                             │
├─────────────────────────────────────────────────────────────────────────────┤
│  Core Storage          │   Time Series        │   Distributed Storage        │
│  (internal/storage)    │   (internal/storage) │   (internal/storage)         │
├────────────────────────┼──────────────────────┼──────────────────────────────┤
│  - B-tree indexing     │  - Retention policies│  - Raft log storage          │
│  - ACID transactions   │  - Compaction        │  - Replication               │
│  - JSON documents      │  - Downsampling      │  - Consensus logs            │
│  - Soul records        │  - Judgment history  │                              │
│  - Judgment records    │  - Metric aggregation│                              │
└────────────────────────┴──────────────────────┴──────────────────────────────┘
```

## Core Domain Concepts

### Egyptian Mythology Theme

| Concept | Egyptian Reference | Technical Meaning |
|---------|-------------------|-------------------|
| **Soul** | Ka (Spirit) | Monitored target/service |
| **Judgment** | Weighing of the Heart | Health check execution |
| **Ma'at** | Truth/Balance | Status page (public transparency) |
| **Necropolis** | City of the Dead | Cluster/Distributed system |
| **Jackal** | Anubis' form | Cluster node |
| **Feather** | Feather of Ma'at | Alert threshold/trigger |
| **Journey** | Duat (Underworld journey) | Multi-step synthetic test |
| **Verdict** | Final judgment | Alert rule decision |

## Frontend Architecture (web/)

### Tech Stack
- **Framework**: React 19 + TypeScript
- **Build Tool**: Vite 6
- **Styling**: Tailwind CSS v4 with custom animations
- **Charts**: Recharts
- **State**: Zustand
- **Icons**: Lucide React
- **Routing**: React Router v7

### Project Structure

```
web/src/
├── api/
│   ├── client.ts          # ApiClient class with JWT auth
│   └── hooks.ts           # React Query-style hooks (useSouls, useJudgments, etc.)
├── components/
│   ├── Layout.tsx         # App shell with Sidebar + Header
│   ├── Sidebar.tsx        # Navigation sidebar
│   ├── Header.tsx         # Top bar with user menu
│   └── ProtectedRoute.tsx # Auth guard
├── pages/
│   ├── Dashboard.tsx      # Real-time stats + Recharts graphs
│   ├── Souls.tsx          # CRUD for monitored targets
│   ├── SoulDetail.tsx     # Individual soul analytics
│   ├── Judgments.tsx      # Health check history
│   ├── Alerts.tsx         # Channels + Rules management
│   ├── Journeys.tsx       # Multi-step workflows
│   ├── Cluster.tsx        # Node status + Raft info
│   ├── StatusPages.tsx    # Public status pages
│   ├── Settings.tsx       # Configuration UI
│   └── Login.tsx          # Auth page
├── stores/
│   └── soulStore.ts       # Zustand state management
└── index.css              # Tailwind + custom animations
```

### Key Features Implemented

1. **Real-time Updates**: WebSocket integration for live judgment updates
2. **Interactive Charts**: Latency trends and check volume visualization
3. **CRUD Operations**: Full Create, Read, Update, Delete for all entities
4. **Authentication**: JWT-based with local storage persistence
5. **Responsive Design**: Mobile-first with dark theme
6. **Protected Routes**: Auth guards for all internal pages

## Backend Architecture (internal/)

### 1. Core Package (internal/core/)

**Domain Models:**
- `Soul`: Monitored target (HTTP, TCP, DNS, etc.)
- `Judgment`: Health check result
- `Journey`: Multi-step synthetic test
- `Verdict`: Alert rule evaluation
- `Feather`: Lightweight alert trigger
- `Workspace`: Multi-tenancy boundary

### 2. Probe Engine (internal/probe/)

**Protocol Support:**
```go
type Checker interface {
    Check(ctx context.Context, target string, config Config) (*Result, error)
}
```

- **HTTP/HTTPS**: Full HTTP check with custom headers, body validation
- **TCP/UDP**: Connection testing with TLS support
- **DNS**: Resolution testing with record type validation
- **ICMP**: Ping checks (requires privileged mode)
- **TLS**: Certificate validation and expiration monitoring
- **gRPC**: Health check via gRPC protocol
- **WebSocket**: Connection and message testing
- **SMTP**: Email server health checks

**Engine Features:**
- Concurrent execution (goroutine pool)
- Configurable intervals per soul
- Timeout handling with context cancellation
- Result aggregation and storage

### 3. Alert Manager (internal/alert/)

**Components:**
- **Manager**: Central orchestration
- **Dispatchers**: Channel-specific senders
  - Email (SMTP)
  - Slack (Webhooks)
  - Discord (Webhooks)
  - PagerDuty (Events API)
  - Generic Webhook

**Rule Engine:**
- Condition evaluation (thresholds, duration)
- Severity levels (critical, warning, info)
- Channel routing
- Rate limiting
- Acknowledgment tracking

### 4. Storage Layer (internal/storage/)

**CobaltDB - Custom Database:**
```go
type Engine struct {
    souls       *BTree      // B-tree index for souls
    judgments   *TimeSeries // Time-series for check results
    raftLog     *RaftLog    // Distributed log storage
}
```

**Features:**
- B-tree indexing (order 32) for O(log n) lookups
- Time-series storage with automatic compaction
- Configurable retention policies
- ACID transaction support
- Optional encryption at rest
- Raft integration for distributed consensus

### 5. Clustering (internal/raft/, internal/cluster/)

**Necropolis - Distributed Mode:**
- **Raft Consensus**: Leader election, log replication
- **Auto-Discovery**: Gossip-based node discovery
- **Distribution**: Soul assignment across nodes
- **Failover**: Automatic leader election on node failure

**Node States:**
- Solo: Single-node mode
- Leader: Coordinating cluster
- Follower: Replicating logs
- Candidate: Seeking election

### 6. API Layer (internal/api/)

**REST Endpoints:**
```
GET    /api/v1/souls              # List souls
POST   /api/v1/souls              # Create soul
GET    /api/v1/souls/:id          # Get soul details
PUT    /api/v1/souls/:id          # Update soul
DELETE /api/v1/souls/:id          # Delete soul
POST   /api/v1/souls/:id/check    # Force immediate check

GET    /api/v1/judgments          # List judgments
GET    /api/v1/souls/:id/judgments # Get soul's judgments

GET    /api/v1/channels           # List alert channels
POST   /api/v1/channels           # Create channel
PUT    /api/v1/channels/:id       # Update channel
DELETE /api/v1/channels/:id       # Delete channel
POST   /api/v1/channels/:id/test  # Test channel

GET    /api/v1/rules              # List alert rules
POST   /api/v1/rules              # Create rule
PUT    /api/v1/rules/:id          # Update rule
DELETE /api/v1/rules/:id          # Delete rule

GET    /api/v1/stats/overview     # Dashboard statistics
GET    /api/v1/cluster/status     # Cluster information

POST   /api/v1/auth/login         # Authenticate
POST   /api/v1/auth/logout        # Logout
GET    /api/v1/auth/me            # Current user
```

**MCP Server:**
- Model Context Protocol for AI integration
- Claude Code compatibility
- Tool definitions for monitoring operations

**WebSocket:**
- `/ws` endpoint for real-time updates
- Event streaming for judgments
- Connection multiplexing

### 7. Authentication (internal/auth/)

**Methods:**
- Local: Email/password with bcrypt
- OIDC: OpenID Connect support
- LDAP: Active Directory integration

**JWT Features:**
- HS256 signing
- Configurable expiration
- Refresh token support
- Role-based access control (RBAC)

## Data Flow

### Health Check Flow
```
1. Probe Engine (scheduler)
   ↓
2. Execute Check (HTTP/TCP/etc.)
   ↓
3. Store Judgment (CobaltDB)
   ↓
4. Evaluate Rules (Alert Manager)
   ↓
5. Send Notifications (if triggered)
   ↓
6. Broadcast via WebSocket (real-time UI update)
```

### Cluster Replication Flow
```
1. Leader receives write
   ↓
2. Append to Raft log
   ↓
3. Replicate to followers
   ↓
4. Majority acknowledgment
   ↓
5. Commit to state machine
   ↓
6. Apply to CobaltDB
```

## Configuration

### anubis.json
```json
{
  "Server": {
    "host": "0.0.0.0",
    "port": 8080,
    "tls": { "enabled": true, "auto_cert": true }
  },
  "Storage": {
    "path": "./data",
    "encryption": { "enabled": false }
  },
  "Auth": {
    "enabled": true,
    "type": "local",
    "local": { "admin_email": "admin@anubis.watch", "admin_password": "admin" }
  },
  "Necropolis": {
    "enabled": false,
    "node_name": "jackal-01",
    "raft": { "bootstrap": false }
  }
}
```

## Deployment Options

### Standalone (Default)
- Single binary: `anubis serve`
- Embedded frontend
- SQLite-like storage (CobaltDB)

### Cluster Mode
- Multiple nodes: `anubis necropolis --join <seed>`
- Raft consensus
- Distributed storage

### Docker
```dockerfile
FROM scratch
COPY anubis /anubis
COPY dist /dist
EXPOSE 8080
ENTRYPOINT ["/anubis", "serve"]
```

## Development

### Frontend
```bash
cd web
npm install
npm run dev      # Development server
npm run build    # Production build
```

### Backend
```bash
go mod download
go build -o anubis ./cmd/anubis
./anubis serve
```

### Testing
```bash
go test ./...                    # Unit tests
go test -bench=. ./internal/...  # Benchmarks
```

## Security Considerations

1. **Authentication**: JWT with secure defaults
2. **Authorization**: Role-based access control
3. **Data**: Optional encryption at rest
4. **Network**: TLS support with ACME auto-certs
5. **Secrets**: Environment variable injection
6. **Validation**: Input sanitization on all APIs

## Monitoring & Observability

- **Metrics**: Prometheus-compatible endpoints
- **Logs**: Structured JSON logging
- **Health**: `/health` endpoint for load balancers
- **Profiling**: Built-in pprof support

## Future Roadmap

1. **Multi-Region**: Geographic distribution
2. **ML Insights**: Anomaly detection
3. **Mobile App**: React Native client
4. **Terraform Provider**: Infrastructure as code
5. **GitOps Integration**: Configuration syncing

---

*"Anubis weighs the hearts of your services against the feather of Ma'at. May your uptime be eternal."*
