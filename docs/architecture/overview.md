# AnubisWatch Architecture

## Overview

AnubisWatch is a zero-dependency, single-binary uptime and synthetic monitoring platform built with Go and React. It uses Raft consensus for distributed state machine replication and provides real-time monitoring via WebSockets.

## Egyptian Mythology Theming

| Term | Meaning | Component |
|------|---------|-----------|
| **Soul** | Monitored target | `internal/core/soul.go` |
| **Judgment** | Health check result | `internal/core/judgment.go` |
| **Verdict** | Alert decision | `internal/core/verdict.go` |
| **Jackal** | Probe node | `internal/probe/` |
| **Pharaoh** | Raft leader | `internal/raft/` |
| **Necropolis** | Distributed cluster | `internal/cluster/` |
| **Feather** | Storage engine (CobaltDB) | `internal/storage/` |
| **Ma'at** | Alert engine | `internal/alert/` |
| **Duat** | WebSocket layer | `internal/api/websocket.go` |
| **Journey** | Synthetic monitoring | `internal/journey/` |

## System Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Web Layer                             │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │  React 19   │  │   Vite 8    │  │  Tailwind CSS 4.1   │  │
│  │  Dashboard  │  │   Build     │  │    Styling          │  │
│  └──────┬──────┘  └─────────────┘  └─────────────────────┘  │
└─────────┼───────────────────────────────────────────────────┘
          │ HTTPS / WebSocket
          ▼
┌─────────────────────────────────────────────────────────────┐
│                        API Layer                             │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │   REST API  │  │  WebSocket  │  │   MCP Server        │  │
│  │   (HTTP)    │  │   (Duat)    │  │  (AI Integration)   │  │
│  └──────┬──────┘  └──────┬──────┘  └─────────────────────┘  │
└─────────┼────────────────┼───────────────────────────────────┘
          │                │
          ▼                ▼
┌─────────────────────────────────────────────────────────────┐
│                       Core Services                          │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │   Souls     │  │  Judgments  │  │     Verdicts        │  │
│  │  (Targets)  │  │   (Checks)  │  │    (Alerts)         │  │
│  └──────┬──────┘  └─────────────┘  └─────────────────────┘  │
└─────────┼───────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────────────┐
│                     Probe Engine (Jackal)                    │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌────────────────┐  │
│  │   HTTP   │ │   TCP    │ │   DNS    │ │   ICMP/UDP/    │  │
│  │  Checker │ │  Checker │ │  Checker │ │   SMTP/IMAP    │  │
│  └──────────┘ └──────────┘ └──────────┘ └────────────────┘  │
└─────────┬───────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────────────┐
│                    Storage (Feather)                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │  CobaltDB   │  │    WAL      │  │  B+Tree Engine      │  │
│  │  (B+Tree)   │  │  (Recovery) │  │    (Order 32)       │  │
│  └─────────────┘  └─────────────┘  └─────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
          ▲
          │ Raft Consensus
          │
┌─────────┴───────────────────────────────────────────────────┐
│                  Cluster (Necropolis)                        │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │  Raft Node  │  │  Discovery  │  │  Distributor        │  │
│  │  (Pharaoh)  │  │   (Gossip)  │  │  (Work Allocation)  │  │
│  └─────────────┘  └─────────────┘  └─────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

## Component Details

### Probe Engine (internal/probe/)

The probe engine schedules and executes health checks across multiple protocols.

**Key Features:**
- Concurrent check execution with semaphore limiting
- Circuit breaker pattern for failing targets
- Exponential backoff for retries
- Connection pooling for HTTP checks
- Configurable check intervals and timeouts

**Supported Protocols:**
- HTTP/HTTPS (with TLS verification)
- TCP/UDP
- DNS
- ICMP (ping)
- SMTP/IMAP
- gRPC
- WebSocket
- TLS certificate validation

### Raft Consensus (internal/raft/)

Raft implementation for distributed state machine replication.

**Key Features:**
- Leader election with pre-vote optimization
- Log replication
- Snapshot management
- Joint consensus for membership changes
- TCP transport with TLS support
- Automatic leader failover

**State Machine:**
```
Follower → Candidate → Leader
    ↑___________|
```

### Storage (internal/storage/)

Embedded B+Tree storage engine with WAL for crash recovery.

**Key Features:**
- Zero external dependencies
- Configurable B+Tree order (default: 32)
- Write-ahead logging
- Snapshot support
- Time-series optimized judgments storage
- Automatic data retention

### Alert Engine (internal/alert/)

Ma'at - The goddess of truth determines when to send alerts.

**Key Features:**
- Multi-channel alerting (Email, Slack, Discord, PagerDuty, Webhook)
- Rule-based alert triggers
- Incident management (acknowledge, resolve)
- Alert deduplication
- Rate limiting

### WebSocket Layer (internal/api/websocket.go)

Duat - Real-time updates for the dashboard.

**Key Features:**
- Bidirectional communication
- Room-based subscriptions
- Automatic reconnection
- Heartbeat/ping-pong
- Broadcast to workspaces

## Data Flow

### Health Check Flow

```
1. Probe Engine schedules check
   ↓
2. Checker executes protocol-specific check
   ↓
3. Judgment stored in CobaltDB
   ↓
4. Alert Engine evaluates rules
   ↓
5. WebSocket broadcasts update
   ↓
6. Dashboard displays real-time status
```

### Cluster Replication Flow

```
1. Leader receives write request
   ↓
2. Entry appended to leader's log
   ↓
3. Entry replicated to followers
   ↓
4. Majority acknowledges
   ↓
5. Entry committed
   ↓
6. Applied to state machine
```

## Deployment Patterns

### Single Node

```
┌─────────────────────────────────────┐
│           Single Node               │
│  ┌─────────┐ ┌─────────┐ ┌───────┐ │
│  │   API   │ │  Probe  │ │Storage│ │
│  └─────────┘ └─────────┘ └───────┘ │
└─────────────────────────────────────┘
```

### Multi-Node Cluster

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Node 1    │◄───►│   Node 2    │◄───►│   Node 3    │
│  (Leader)   │     │  (Follower) │     │  (Follower) │
└─────────────┘     └─────────────┘     └─────────────┘
       ▲                  ▲                  ▲
       └──────────────────┼──────────────────┘
                          │
                    Load Balancer
                          │
                     ┌────┴────┐
                     │ Clients │
                     └─────────┘
```

## Performance Characteristics

- **Check Throughput:** 1000+ souls per node
- **Check Latency:** <1ms for local checks, network-dependent for remote
- **Raft Latency:** <10ms for log replication (LAN)
- **Storage:** ~100 bytes per judgment, configurable retention
- **Memory:** ~50MB base + ~100KB per 1000 souls

## Security

- TLS 1.3 for all communications
- JWT-based authentication
- RBAC for authorization
- Rate limiting per IP and user
- Input validation and sanitization
- SQL/XSS injection protection

## Monitoring

- Prometheus metrics endpoint
- Structured logging with slog
- Distributed tracing support
- Health and readiness checks

## Scaling Considerations

### Horizontal Scaling
- Add nodes to Raft cluster (odd number for quorum)
- Use distributor for work allocation
- Load balancer for API requests

### Vertical Scaling
- Increase CPU for more concurrent checks
- Increase memory for larger log buffers
- Faster disks for storage-intensive workloads
