<div align="center">

# ⚖️ AnubisWatch

### *The Judgment Never Sleeps*

**Zero-dependency, single-binary uptime and synthetic monitoring platform**

[![CI](https://github.com/AnubisWatch/anubiswatch/actions/workflows/ci.yml/badge.svg)](https://github.com/AnubisWatch/anubiswatch/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/AnubisWatch/anubiswatch)](https://github.com/AnubisWatch/anubiswatch/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/AnubisWatch/anubiswatch)](https://goreportcard.com/report/github.com/AnubisWatch/anubiswatch)
[![License](https://img.shields.io/github/license/AnubisWatch/anubiswatch)](LICENSE)
[![Docker](https://img.shields.io/docker/pulls/anubiswatch/anubis)](https://hub.docker.com/r/anubiswatch/anubis)

</div>

---

## 🎯 What is AnubisWatch?

AnubisWatch is a **zero-dependency, single-binary uptime monitoring platform** built in pure Go. Inspired by the Egyptian god of the afterlife who weighed the hearts of the dead, AnubisWatch "weighs" your services' health with precision and authority.

### Key Features

- 🔥 **Zero Dependencies** — Only requires Go stdlib + 4 extended stdlib packages
- 📦 **Single Binary** — Everything in one `anubis` executable
- 🌍 **8 Protocols** — HTTP/HTTPS, TCP, UDP, DNS, ICMP, SMTP, gRPC, WebSocket, TLS
- ⚡ **Distributed** — Built-in Raft consensus for multi-node clusters
- 🔬 **Synthetic Monitoring** — Multi-step HTTP journeys with variable extraction
- 🎨 **Beautiful Dashboard** — React 19 + Tailwind 4.1 embedded in binary
- 🤖 **MCP-Native** — Built-in Model Context Protocol server for AI integration
- 🔔 **Rich Alerts** — Slack, Discord, Telegram, Email, PagerDuty, OpsGenie, SMS, Ntfy
- 🏷️ **Multi-Tenancy** — Workspace isolation with quotas and RBAC
- 📋 **Status Pages** — Public status pages with custom domains and ACME

---

## 🏛️ Architecture

### Terminology

AnubisWatch uses Egyptian mythology theming throughout:

| Term | Meaning |
|------|---------|
| **Soul** | A monitored target (HTTP endpoint, TCP port, etc.) |
| **Judgment** | A single check execution result |
| **Verdict** | An alert decision based on judgment patterns |
| **Jackal** | A probe node that performs health checks |
| **Pharaoh** | The Raft leader in a cluster |
| **Necropolis** | The distributed cluster |
| **Feather** | The embedded B+Tree storage engine (CobaltDB) |
| **Ma'at** | The alert engine (goddess of truth) |
| **Duat** | The WebSocket real-time layer |
| **Journey** | Multi-step synthetic monitoring |

### Project Structure

```
AnubisWatch/
├── cmd/anubis/          # CLI entry point
├── internal/
│   ├── api/             # REST, WebSocket, gRPC, MCP APIs
│   ├── checkers/        # 10 protocol checkers
│   ├── core/            # Domain types (Soul, Judgment, Verdict, etc.)
│   ├── feather/         # B+Tree storage with WAL and MVCC
│   ├── jackal/          # Probe engine
│   ├── maat/            # Alert engine
│   ├── dispatch/        # 7 alert channel dispatchers
│   ├── raft/            # Raft consensus
│   ├── necropolis/      # Cluster coordination
│   ├── journey/         # Time-series storage
│   ├── acme/            # Let's Encrypt/ZeroSSL integration
│   ├── statuspage/      # Public status pages
│   └── storage/         # Repository implementations
└── web/                 # React 19 + Tailwind 4.1 dashboard
```

---

## 📊 Comparison

| Feature | UptimeRobot | Pingdom | Uptime Kuma | Checkly | **AnubisWatch** |
|---------|-------------|---------|-------------|---------|-----------------|
| Self-hosted | ❌ SaaS | ❌ SaaS | ✅ | ❌ SaaS | ✅ |
| Multi-Protocol | ❌ HTTP | ⚠️ Limited | ⚠️ Some | ⚠️ HTTP+API | ✅ **8 protocols** |
| Distributed | ❌ | ❌ | ❌ | ⚠️ SaaS only | ✅ **Raft cluster** |
| Synthetic | ❌ | ❌ | ❌ | ✅ | ✅ |
| Single Binary | ❌ N/A | ❌ N/A | ❌ Node.js | ❌ N/A | ✅ **Zero deps** |
| MCP Server | ❌ | ❌ | ❌ | ❌ | ✅ |

---

## 🚀 Quick Start

### Installation

```bash
# Linux/macOS (using install script)
curl -fsSL https://anubis.watch/install.sh | sh

# Or download from releases
wget https://github.com/AnubisWatch/anubiswatch/releases/latest/download/anubis-linux-amd64
chmod +x anubis-linux-amd64
mv anubis-linux-amd64 /usr/local/bin/anubis

# macOS with Homebrew (coming soon)
brew install anubiswatch/tap/anubis
```

### Docker

```bash
# Single node
docker run -d \
  --name anubis \
  -p 8443:8443 \
  -v anubis-data:/var/lib/anubis \
  anubiswatch/anubis:latest

# With custom config
docker run -d \
  --name anubis \
  -p 8443:8443 \
  -v $(pwd)/anubis.yaml:/etc/anubis/anubis.yaml \
  -v anubis-data:/var/lib/anubis \
  anubiswatch/anubis:latest
```

### Initialize & Run

```bash
# Create default configuration
anubis init

# Edit configuration
vim anubis.yaml

# Start server
anubis serve

# Access dashboard
open https://localhost:8443
```

---

## 🎮 CLI Usage

```bash
# Show version
anubis version

# Initialize configuration
anubis init

# Quick-add a monitor
anubis watch https://api.example.com --name "API"

# View current status
anubis judge

# Cluster management
anubis necropolis              # Show cluster status
anubis summon 10.0.0.2:7946    # Add node
anubis banish jackal-02        # Remove node
```

---

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    AnubisWatch Binary                        │
│                                                             │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────────┐  │
│  │  Probe   │  │   Raft   │  │   API    │  │  Dashboard  │  │
│  │  Engine  │  │ Consensus│  │  Server  │  │  (React 19) │  │
│  │          │  │          │  │          │  │  embedded   │  │
│  │ 8 proto- │  │  Leader  │  │ REST +   │  │  Tailwind   │  │
│  │ col chk  │  │  Election│  │ gRPC +   │  │  4.1 +      │  │
│  │          │  │  Log Rep │  │ WebSocket│  │  shadcn/ui  │  │
│  │          │  │  State   │  │ MCP Svr  │  │  Lucide     │  │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └──────┬─────┘  │
│       │              │              │               │        │
│  ┌────┴──────────────┴──────────────┴───────────────┴─────┐  │
│  │                    CobaltDB Engine                      │  │
│  │         Embedded Storage (B+Tree, WAL, MVCC)           │  │
│  │     Time-Series Optimized · AES-256-GCM Encryption     │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                             │
│  ┌────────────────────────────────────────────────────────┐  │
│  │                Alert Dispatcher                        │  │
│  │  Webhook · Slack · Discord · Telegram · Email(SMTP)   │  │
│  │  PagerDuty · OpsGenie · SMS(Twilio) · Ntfy.sh        │  │
│  └────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

---

## 🌍 Distributed Cluster (Necropolis)

Every node in an AnubisWatch cluster is both a **probe** and a **controller**. Powered by Raft consensus:

- **Pharaoh** (Leader): Schedules checks, dispatches alerts
- **Jackal** (Follower): Executes checks, replicates state
- Auto-discovery via mDNS or gossip protocol

```bash
# Node 1 (becomes leader)
anubis serve --node-name jackal-01 --region eu-west --cluster

# Node 2 (joins cluster)
anubis serve --node-name jackal-02 --region us-east --cluster \
  --join jackal-01:7946

# Node 3 (joins cluster)
anubis serve --node-name jackal-03 --region apac --cluster \
  --join jackal-01:7946
```

---

## 📡 Monitoring Protocols

| Protocol | Description | Example |
|----------|-------------|---------|
| HTTP/HTTPS | Full HTTP checks with assertions | Status codes, body, JSON path, schema |
| TCP | Port checks with banner grab | Connect + banner match |
| UDP | Packet send/receive | Hex payload support |
| DNS | Resolution and propagation | Multi-resolver checks |
| ICMP | Ping with packet loss | Latency stats, jitter |
| SMTP/IMAP | Email server checks | STARTTLS, AUTH |
| gRPC | Health checks | Custom metadata |
| WebSocket | Realtime connection | Subprotocols, ping/pong |
| TLS | Certificate monitoring | Expiry, cipher audit |

---

## 🔧 Configuration Example

```yaml
server:
  host: "0.0.0.0"
  port: 8443
  tls:
    enabled: true
    auto_cert: true
    acme_email: "admin@example.com"

souls:
  - name: "Production API"
    type: http
    target: "https://api.example.com/health"
    weight: 30s  # Check every 30s
    http:
      method: GET
      valid_status: [200]
      json_path:
        "$.status": "ok"
      feather: 500ms  # Max latency

  - name: "Database"
    type: tcp
    target: "db.example.com:5432"
    weight: 15s

checks:
  - name: "ops-slack"
    type: slack
    slack:
      webhook_url: "${SLACK_WEBHOOK}"

verdicts:
  rules:
    - name: "Service Down"
      condition:
        type: consecutive_failures
        threshold: 3
      severity: critical
      channels: ["ops-slack"]
```

---

## 🎨 Dashboard

The **Hall of Ma'at** dashboard is built with React 19 and embedded directly into the binary:

- Real-time WebSocket updates
- 90-day uptime heatmaps
- Response time sparklines
- Regional node map
- Dark/Light themes (Tomb Interior / Desert Sun)

---

## 🤖 MCP Integration

AnubisWatch includes a built-in MCP server for AI agent integration:

```bash
# List monitored targets
anubis_list_souls

# Get current status
anubis_get_soul_status api.example.com

# Trigger check
anubis_trigger_judgment api.example.com
```

---

## 📦 Development

```bash
# Clone repository
git clone https://github.com/AnubisWatch/anubiswatch.git
cd anubiswatch

# Install dependencies
go mod download
cd web && npm ci && cd ..

# Build dashboard
make dashboard

# Build binary
make build

# Run tests
make test

# Run locally
make dev
```

---

## 🗺️ Roadmap

- [x] Phase 1: Foundation (Go module, core types, config)
- [x] Phase 2: Probe Engine (8 protocols)
- [ ] Phase 3: Raft Consensus (distributed cluster)
- [ ] Phase 4: Alert System (9 channels, escalation)
- [ ] Phase 5: API Layer (REST, WebSocket, gRPC, MCP)
- [ ] Phase 6: Dashboard (React 19, Hall of Ma'at)
- [ ] Phase 7: Advanced Features (multi-tenant, status page)
- [ ] Phase 8: Polish & Release

---

## 📄 License

AnubisWatch is licensed under the Apache License 2.0. See [LICENSE](LICENSE) for details.

```
Copyright 2026 Ersin Koç — ECOSTACK TECHNOLOGY OÜ

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
```

---

## 🙏 Acknowledgments

- **Anubis**: Egyptian god of the afterlife, who inspired this project
- **Ma'at**: Goddess of truth and justice, whose feather weighs against our metrics
- **The Go Team**: For the amazing standard library

---

<div align="center">

**[anubis.watch](https://anubis.watch)** · **[Documentation](https://docs.anubis.watch)** · **[Discord](https://discord.gg/anubiswatch)**

*The Judgment Never Sleeps* ⚖️

</div>
