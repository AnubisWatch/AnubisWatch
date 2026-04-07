# AnubisWatch v0.1.0 Release Notes

**Release Date:** 2026-04-06

> ⚖️ *The Judgment Never Sleeps*

---

## 🎉 What's New

AnubisWatch v0.1.0 is a major milestone release featuring comprehensive monitoring capabilities, distributed clustering, and AI-native integrations.

### 🔥 Highlights

- **MCP-Native Architecture** — Built-in Model Context Protocol server for seamless AI agent integration
- **8 Protocol Support** — HTTP/HTTPS, TCP, UDP, DNS, ICMP, SMTP, IMAP, gRPC, WebSocket, TLS
- **Distributed Clustering** — Built-in Raft consensus for multi-node Necropolis clusters
- **Multi-Tenancy** — Workspace-based isolation with RBAC and quotas
- **Synthetic Monitoring** — Multi-step HTTP journeys with variable extraction
- **Zero Dependencies** — Single binary with only Go stdlib + 4 extended packages
- **Beautiful Dashboard** — React 19 + Tailwind 4.1 embedded in binary

---

## 📦 Installation

### Quick Install (Linux/macOS)

```bash
curl -fsSL https://anubis.watch/install.sh | sh
```

### Docker

```bash
docker pull ghcr.io/anubiswatch/anubiswatch:v0.1.0
```

### Download Binary

| Platform | Architecture | Download |
|----------|--------------|----------|
| Linux | amd64 | [anubis-linux-amd64](https://github.com/AnubisWatch/anubiswatch/releases/download/v0.1.0/anubis-linux-amd64) |
| Linux | arm64 | [anubis-linux-arm64](https://github.com/AnubisWatch/anubiswatch/releases/download/v0.1.0/anubis-linux-arm64) |
| macOS | amd64 | [anubis-darwin-amd64](https://github.com/AnubisWatch/anubiswatch/releases/download/v0.1.0/anubis-darwin-amd64) |
| macOS | arm64 | [anubis-darwin-arm64](https://github.com/AnubisWatch/anubiswatch/releases/download/v0.1.0/anubis-darwin-arm64) |
| Windows | amd64 | [anubis-windows-amd64.exe](https://github.com/AnubisWatch/anubiswatch/releases/download/v0.1.0/anubis-windows-amd64.exe) |

---

## 🚀 Getting Started

```bash
# Initialize configuration
anubis init

# Start server
anubis serve

# Access dashboard
open https://localhost:8443
```

---

## ✨ New Features

### MCP Server Integration
- Endpoint: `/api/v1/mcp`
- 8 built-in tools for AI agent integration
- 3 MCP resources (getting-started, api-reference, status/current)
- 3 MCP prompts for common workflows

### Synthetic Monitoring (Duat Journeys)
- Multi-step HTTP journey execution
- Variable extraction from responses
- JSON path, regex, header, and cookie extraction
- Continue-on-failure support

### Status Pages
- Public status page generation
- Custom domain support with ACME
- Password protection option
- Custom themes
- RSS feed support
- SVG badge generation

### Multi-Tenancy
- Workspace-based isolation
- 5 RBAC roles (Owner, Admin, Editor, Viewer, API)
- Quota management per workspace
- Namespace isolation

### Alert System
- 9 notification channels
- Escalation policies
- Alert acknowledgment workflow
- Deduplication with cooldown
- Rate limiting

### API Features
- REST API with pagination
- Rate limiting (100 req/min per IP)
- Request validation middleware
- WebSocket real-time updates
- gRPC support

---

## 📊 Test Coverage

| Package | Coverage |
|---------|----------|
| `internal/core` | 98.9% |
| `internal/cluster` | 90.0% |
| `internal/alert` | 89.3% |
| `internal/raft` | 86.1% |
| **Average** | **86.0%** |

---

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    AnubisWatch v0.1.0                        │
│                                                             │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────────┐  │
│  │  Probe   │  │   Raft   │  │   API    │  │  Dashboard  │  │
│  │  Engine  │  │ Consensus│  │  Server  │  │  (React 19) │  │
│  │ 8 proto- │  │  Leader  │  │ REST +   │  │  Tailwind   │  │
│  │ col chk  │  │  Election│  │ gRPC +   │  │  4.1 +      │  │
│  │          │  │  Log Rep │  │ MCP Svr  │  │  Lucide     │  │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └──────┬─────┘  │
│       │              │              │               │        │
│  ┌────┴──────────────┴──────────────┴───────────────┴─────┐  │
│  │                    CobaltDB Engine                      │  │
│  │         Embedded Storage (B+Tree, WAL, MVCC)           │  │
│  │     Time-Series Optimized · AES-256-GCM Encryption     │  │
│  └────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

---

## 🐛 Known Issues

- WebSocket disabled in favor of REST polling (will be re-enabled in v0.2.0)
- Dashboard build requires Node.js 20+ (optional for runtime)

---

## 🔮 Roadmap

- [ ] v0.2.0: WebSocket re-enable with proper handshake
- [ ] v0.3.0: Additional protocol checkers (SSH, FTP)
- [ ] v0.4.0: Advanced analytics and reporting
- [ ] v1.0.0: Production stable release

---

## 📚 Documentation

- [README.md](README.md) — Project overview and quick start
- [API.md](API.md) — Complete REST API reference
- [CONFIGURATION.md](CONFIGURATION.md) — Configuration guide
- [DEPLOYMENT.md](DEPLOYMENT.md) — Deployment options
- [TROUBLESHOOTING.md](TROUBLESHOOTING.md) — Common issues and solutions

---

## 🙏 Acknowledgments

Special thanks to everyone who contributed to this release!

---

## 📄 License

Apache License 2.0 — See [LICENSE](LICENSE) for details.

---

<div align="center">

**[anubis.watch](https://anubis.watch)** · **[GitHub](https://github.com/AnubisWatch/anubiswatch)**

*The Judgment Never Sleeps* ⚖️

</div>
