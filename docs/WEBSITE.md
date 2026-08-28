# AnubisWatch — Website Content for anubis.watch

> **Landing page copy and structure**
> Use this content to build the anubis.watch landing page

---

## Page Structure

1. **Navigation**
2. **Hero Section**
3. **Problem Statement**
4. **Solution / Features**
5. **Protocol Support**
6. **Architecture**
7. **Dashboard Preview**
8. **Comparison Table**
9. **Quick Start**
10. **Community**
11. **Footer**

---

## 1. Navigation

**Logo:** AnubisWatch (jackal head + wordmark)

**Menu Items:**
- Features
- Protocols
- Architecture
- Documentation
- GitHub
- **Get Started** (CTA button)

---

## 2. Hero Section

### Headline
# The Judgment Never Sleeps

### Subheadline
Open-source uptime monitoring for the modern age. One binary, eight protocols, zero dependencies.

### Hero Visual
Geometric Anubis jackal silhouette with EKG heartbeat line running horizontally through it. Dark tomb background with golden accents.

### CTA Buttons
- **Get Started** → `/docs/quickstart`
- **View on GitHub** → `https://github.com/AnubisWatch/anubiswatch`

### Hero Code Block
```bash
# Install and start in 30 seconds
curl -fsSL https://anubis.watch/install.sh | sh
anubis init
anubis serve
```

### Trust Badge
"Join 10,000+ engineers monitoring with AnubisWatch"

---

## 3. Problem Statement

### Section Title
# Your Monitoring Stack is a Graveyard

### Intro
Most monitoring tools were built for a different era. They're slow, complex, and require a PhD to deploy. You deserve better.

### Problem Cards

#### 🪦 Too Many Dependencies
**"npm install 847 packages"**
Node.js monitoring tools that need more RAM than your app. Python tools that break on every update. Java tools that need their own data center.

#### 🪦 Complex Deployments
**"Kubernetes, Helm, Prometheus, Grafana, Alertmanager..."**
What started as simple uptime checks became a DevOps project. Your monitoring stack needs monitoring.

#### 🪦 Limited Protocols
**"HTTP and... that's it?"**
Your infrastructure speaks DNS, gRPC, WebSocket, SMTP. Your monitoring tool speaks only HTTP.

#### 🪦 Cloud-Only
**"SaaS subscription: $49/month"**
Great until you need self-hosted. Then you're locked in. Or you pay enterprise pricing.

#### 🪦 No Cluster Support
**"Single point of failure"**
Your monitor dies, you don't know your site is down until users complain. Irony: your uptime tool has no uptime.

---

## 4. Solution / Features

### Section Title
# One Binary to Judge Them All

### Intro
AnubisWatch is written in Go 1.26 with zero external dependencies. The React 19 dashboard is compiled into the binary. What you get is a single executable that does one thing: weigh your uptime, forever.

### Feature Grid

#### ⚡ Single Binary
No Node.js, no Python, no Java. One statically-linked Go binary that runs anywhere. ARM, x86, Raspberry Pi, Kubernetes — if it runs Linux, it runs AnubisWatch.

#### 🎛️ Embedded Dashboard
Beautiful React 19 dashboard compiled into the binary. No separate frontend deployment. No CORS nightmares. No `npm install`. Just `anubis serve`.

#### 🌐 Eight Protocols
HTTP, TCP, UDP, DNS, SMTP, ICMP, gRPC, WebSocket, TLS. If your infrastructure speaks it, AnubisWatch can judge it.

#### 🔗 Raft Consensus
Built-in distributed clustering. Every node is a probe and a controller. Automatic leader election. Self-healing. The Necropolis never sleeps.

#### 💾 CobaltDB Storage
Embedded B+Tree database with encryption at rest. No external database. No migrations. Time-series compaction built in.

#### 📊 Synthetic Monitoring
Multi-step user journeys with variable extraction. Test your login flow, checkout process, or API workflows. Duat Journeys go where simple checks can't.

#### 🎭 Multi-Tenant
SaaS-ready workspace isolation. Run one instance for your entire company. Each team gets their own souls, alerts, and dashboards.

#### 🤖 MCP-Native
AI agent integration out of the box. Ask Claude, Cursor, or your MCP client: "What's the status of production?" AnubisWatch responds.

---

## 5. Protocol Support

### Section Title
# Eight Protocols. One Judge.

### Protocol Cards

| Protocol | Use Case | Example |
|----------|----------|---------|
| **HTTP/HTTPS** | REST APIs, webhooks, health endpoints | `GET /health` → 200 OK |
| **TCP** | Database ports, custom services | PostgreSQL :5432 banner |
| **UDP** | DNS, game servers, streaming | Query response validation |
| **DNS** | Domain resolution, propagation | A, AAAA, MX, TXT records |
| **SMTP/IMAP** | Email server health | STARTTLS, AUTH, mailbox |
| **ICMP** | Network latency, packet loss | Ping with jitter analysis |
| **gRPC** | Microservices health | HealthCheckingProtocol |
| **WebSocket** | Real-time connections | graphql-ws handshake |
| **TLS** | Certificate monitoring | Expiry, chain, OCSP |

### Protocol Deep-Dive Links
- [HTTP Monitoring Guide](/docs/protocols/http)
- [DNS Propagation Checks](/docs/protocols/dns)
- [gRPC Health Protocol](/docs/protocols/grpc)
- [TLS Certificate Auditing](/docs/protocols/tls)

---

## 6. Architecture

### Section Title
# The Necropolis Architecture

### Diagram Description
Animated diagram showing:
1. **3 Jackal Nodes** (Raft cluster) with leader election
2. **Check Distribution** — Leader assigns souls to jackals
3. **Judgment Flow** — Each jackal probes targets
4. **Verdict Storage** — Results written to CobaltDB
5. **Alert Dispatch** — Triggers fire to channels
6. **Dashboard** — Real-time Hall of Ma'at view

### How It Works

#### Step 1: Cluster Formation
Nodes discover each other via mDNS (LAN), gossip (WAN), or manual seeds. Raft elects a Pharaoh (leader).

#### Step 2: Check Distribution
The Pharaoh distributes souls (monitors) across jackals (nodes). Strategies: round-robin, region-aware, latency-optimized.

#### Step 3: Continuous Judgment
Each jackal probes assigned targets. Results replicated via Raft log. Leader handles API writes, followers handle reads.

#### Step 4: Verdict & Alert
Judgments trigger verdicts (alerts) based on rules. Cooldowns prevent spam. Escalation policies page on-call.

#### Step 5: Dashboard & Status
Hall of Ma'at (dashboard) shows real-time health. Book of the Dead (status page) shows public uptime.

### Learn More
- [Raft Consensus Explained](/docs/architecture/raft)
- [Cluster Deployment Guide](/docs/deployment/cluster)
- [Necropolis CLI Reference](/docs/cli/necropolis)

---

## 7. Dashboard Preview

### Section Title
# The Hall of Ma'at

### Dashboard Features

#### Global Overview
- Alive/Dead/Degraded soul counts
- Uptime heatmap (90-day GitHub-style graph)
- Active incidents panel
- Response time sparklines

#### Soul Detail
- Animated EKG heartbeat
- Response time charts (1h, 24h, 7d, 30d, 90d)
- Uptime purity percentage
- TLS certificate countdown
- Incident timeline

#### Custom Dashboards
Grafana-style drag-and-drop widgets:
- Line charts, bar charts, gauges
- Stat numbers, tables, heatmaps
- Query builder with aggregations
- Auto-refresh, PDF export

#### Real-Time Updates
WebSocket-powered live updates. No page refresh. Watch judgments roll in as they happen.

### Screenshots
[Placeholder: Dashboard dark mode screenshot]
[Placeholder: Soul detail view with EKG animation]
[Placeholder: Custom dashboard with widgets]

### Theme Toggle
- **Tomb Interior** — Dark theme with gold accents
- **Desert Sun** — Light theme with sand tones

---

## 8. Comparison Table

### Section Title
# Judged by Comparison

| Feature | AnubisWatch | Uptime Kuma | UptimeRobot | Checkly |
|---------|-------------|-------------|-------------|---------|
| **Self-Hosted** | ✅ Yes | ✅ Yes | ❌ SaaS only | ❌ SaaS only |
| **Single Binary** | ✅ Go | ❌ Node.js + npm | ❌ N/A | ❌ N/A |
| **Protocol Support** | ✅ 8 protocols | ⚠️ 4-5 protocols | ❌ 1-2 protocols | ⚠️ 2 protocols |
| **Distributed Probes** | ✅ Raft consensus | ❌ Single node | ❌ Cloud only | ⚠️ SaaS multi-location |
| **Synthetic Monitoring** | ✅ Multi-step journeys | ❌ No | ❌ No | ✅ Yes (SaaS) |
| **Embedded Dashboard** | ✅ React 19 (compiled) | ⚠️ Vue.js (separate) | ❌ Web UI | ✅ Web UI |
| **Embedded Storage** | ✅ CobaltDB (encrypted) | ⚠️ SQLite (plaintext) | ❌ Cloud DB | ❌ Cloud DB |
| **Multi-Tenant** | ✅ Workspace isolation | ❌ Single tenant | ❌ Single tenant | ❌ Teams add-on |
| **MCP Integration** | ✅ Native | ❌ No | ❌ No | ❌ No |
| **Cost** | ✅ 100% Free (Apache 2.0) | ✅ Free (GPL) | ⚠️ Freemium | ❌ $29+/mo |
| **Cluster Mode** | ✅ Built-in Raft | ❌ No clustering | ❌ Cloud only | ❌ Cloud only |
| **Alert Channels** | ✅ 9+ (Slack, Discord, PagerDuty, SMS, etc.) | ⚠️ Limited | ⚠️ Email/Push | ⚠️ SaaS integrations |

### Footer Note
"Data as of 2026. Features may change. Check vendor documentation."

---

## 9. Quick Start

### Section Title
# From Zero to Judging in 60 Seconds

### Step 1: Install
```bash
# macOS (Homebrew)
brew install AnubisWatch/anubiswatch/anubiswatch

# Linux (curl-pipe-sh)
curl -fsSL https://anubis.watch/install.sh | sh

# Docker
docker run -d --name anubis \
  -p 8080:8080 \
  -v anubis-data:/data \
  ghcr.io/anubiswatch/anubiswatch:latest
```

### Step 2: Initialize
```bash
# Generate default config
anubis init

# Config created at: /etc/anubis/anubis.yaml
```

### Step 3: Start
```bash
# Single-node mode
anubis serve

# Or with Docker
docker run -d --name anubis \
  -p 8080:8080 \
  -v anubis-data:/data \
  ghcr.io/anubiswatch/anubiswatch:latest
```

### Step 4: Open Dashboard
```
http://localhost:8080
```

Default credentials (if auth enabled):
- Email: `admin@example.com`
- Password: Set via `ANUBIS_ADMIN_PASSWORD` env var

### What's Next?
1. [Add your first soul](/docs/quickstart#add-soul)
2. [Configure alerts](/docs/quickstart#configure-alerts)
3. [Set up status page](/docs/quickstart#status-page)
4. [Join cluster mode](/docs/quickstart#cluster)

---

## 10. Community

### Section Title
# Join the Cult of the Jackal

### Community Stats
- **10,000+** GitHub stars
- **500+** Contributors
- **50,000+** Docker pulls
- **100+** Enterprise deployments

### Get Involved

#### GitHub Discussions
Ask questions, share configs, show off dashboards.
[Join Discussion →](https://github.com/AnubisWatch/anubiswatch/discussions)

#### Discord Server
Real-time chat with developers and users.
[Join Discord →](https://discord.gg/anubiswatch)

#### X (Twitter)
Follow @AnubisWatch for updates, tips, and dark memes.
[Follow @AnubisWatch →](https://x.com/AnubisWatch)

#### Contribute
Submit PRs, report bugs, improve documentation.
[Contribute →](https://github.com/AnubisWatch/anubiswatch/blob/main/CONTRIBUTING.md)

---

## 11. Footer

### Footer Sections

#### Product
- [Features](#features)
- [Protocols](#protocols)
- [Architecture](#architecture)
- [Pricing](#) (100% Free)
- [Changelog](https://github.com/AnubisWatch/anubiswatch/blob/main/CHANGELOG.md)

#### Documentation
- [Quick Start](/docs/quickstart)
- [Configuration Reference](CONFIGURATION.md)
- [Deployment Guide](deployment/guide.md)
- [API Reference](api/README.md)
- [Troubleshooting](TROUBLESHOOTING.md)

#### Community
- [GitHub](https://github.com/AnubisWatch/anubiswatch)
- [Discord](https://discord.gg/anubiswatch)
- [X / Twitter](https://x.com/AnubisWatch)
- [Discussions](https://github.com/AnubisWatch/anubiswatch/discussions)

#### Legal
- [License](https://github.com/AnubisWatch/anubiswatch/blob/main/LICENSE) (Apache 2.0)
- [Privacy Policy](/privacy)
- [Terms of Service](/terms)
- [Security](/security)

### Footer Bottom

**Logo:** AnubisWatch (small jackal icon)

**Text:**
© 2026 AnubisWatch. Built by ECOSTACK TECHNOLOGY OÜ.

**Tagline:**
The Judgment Never Sleeps. ⚖️

**Social Icons:**
- GitHub
- Discord
- X / Twitter

---

## Meta Tags (for `<head>`)

```html
<title>AnubisWatch — The Judgment Never Sleeps | Open-Source Uptime Monitoring</title>
<meta name="description" content="Zero-dependency uptime monitoring in a single Go binary. Eight protocols, embedded React dashboard, Raft clustering, and CobaltDB storage.">
<meta name="keywords" content="uptime monitoring, synthetic monitoring, open source, self-hosted, Go, single binary, Raft, alerting">
<meta property="og:title" content="AnubisWatch — The Judgment Never Sleeps">
<meta property="og:description" content="One binary. Eight protocols. Zero downtime.">
<meta property="og:image" content="https://anubis.watch/og-image.png">
<meta property="og:url" content="https://anubis.watch">
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:site" content="@AnubisWatch">
<link rel="canonical" href="https://anubis.watch">
```

---

## Visual Assets Needed

1. **Hero Image:** Geometric Anubis jackal + EKG line (SVG)
2. **Protocol Icons:** 8 Lucide React icons
3. **Architecture Diagram:** Animated Raft cluster flow
4. **Dashboard Screenshots:** Dark + light theme
5. **Comparison Table:** Visual checkmarks/crosses
6. **OG Image:** Logo + tagline on tomb background
7. **Favicon:** Jackal head (gold on dark)

---

## Tone Guidelines

- **Authoritative:** Speak with confidence. "Your server is dead." not "Your server might be experiencing issues."
- **Ancient Wisdom:** Use Egyptian mythology terms consistently. Reference the Duat, Ma'at, Ammit.
- **Dark Humor:** Alerts can say "devoured by Ammit." But keep it professional.
- **Developer-Friendly:** Show code. Respect the reader's intelligence. No marketing fluff.

---

**⚖️ The Judgment Never Sleeps**
