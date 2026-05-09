# AnubisWatch v0.0.1 — Release Readiness Report

**Date:** April 4, 2026  
**Version:** 0.0.1 (Aaru)  
**Status:** Ready for Release

---

## Executive Summary

AnubisWatch v0.0.1 is a zero-dependency, single-binary uptime monitoring platform built in Go 1.26 with an embedded React 19 dashboard. This release includes all core functionality for production use.

### Key Metrics

| Metric | Value |
|--------|-------|
| Binary Size | ~24 MB |
| RAM Usage (idle) | < 64 MB |
| Protocol Support | 10 checkers |
| Alert Channels | 9+ |
| Test Coverage | ~60% |
| Documentation | 15+ files |

---

## Feature Completeness

### Phase 1 — Foundation ✅ 100%
- [x] Go module initialization
- [x] Directory structure
- [x] Core types (Soul, Judgment, Verdict, etc.)
- [x] Configuration system with env expansion
- [x] CobaltDB storage engine
- [x] CLI entrypoint

### Phase 2 — Probe Engine ✅ 100%
- [x] HTTP/HTTPS checker (with JSON path, schema validation)
- [x] TCP checker
- [x] UDP checker
- [x] DNS checker (with propagation monitoring)
- [x] SMTP/IMAP checker
- [x] ICMP ping checker
- [x] gRPC health checker
- [x] WebSocket checker
- [x] TLS certificate checker
- [x] Synthetic monitoring (Duat Journeys)

### Phase 3 — Raft Consensus ✅ 90%
- [x] Raft core (Follower/Candidate/Leader)
- [x] Log replication
- [x] Transport layer
- [x] Snapshots
- [x] Auto-discovery (mDNS, gossip, manual)
- [ ] Check distribution (partially implemented)

### Phase 4 — Alert System ✅ 95%
- [x] Alert dispatcher with rule evaluation
- [x] 9 notification channels (Slack, Discord, Telegram, Email, PagerDuty, OpsGenie, SMS, Ntfy, Webhook)
- [x] Escalation policies
- [ ] Alert CLI commands (partially implemented)

### Phase 5 — API Layer ✅ 95%
- [x] REST API (all endpoints)
- [x] WebSocket server
- [x] gRPC API
- [x] MCP server
- [x] Prometheus metrics

### Phase 6 — Dashboard ✅ 90%
- [x] React 19 + Tailwind 4.1 embedded
- [x] Hall of Ma'at (main dashboard)
- [x] Souls management
- [x] Soul detail view
- [x] Status page generator
- [x] Custom domains
- [x] Password protection
- [ ] Custom dashboards (Grafana-style)
- [ ] PWA support

### Phase 7 — Advanced Features ✅ 75%
- [x] Multi-tenant isolation
- [x] Public status pages
- [x] Subscriber management (email, RSS, webhook)
- [x] Embeddable badges
- [x] Explicit TLS / ingress-managed TLS path
- [ ] OIDC/LDAP authentication
- [ ] DNSSEC validation

### Phase 8 — Polish & Release ✅ 90%
- [x] README.md
- [x] BRANDING.md
- [x] CONFIGURATION.md
- [x] DEPLOYMENT.md
- [x] GHCR.md
- [x] openapi.yaml
- [x] WEBSITE.md
- [x] CONTRIBUTING.md
- [x] install.sh
- [x] Homebrew formula
- [x] systemd service
- [x] Docker Compose examples
- [x] Helm chart
- [x] GitHub Actions workflow
- [x] Release template
- [ ] 80%+ code coverage
- [ ] Load testing
- [ ] Security audit

---

## Documentation Status

| Document | Status | Size |
|----------|--------|------|
| README.md | ✅ Complete | 13 KB |
| CHANGELOG.md | ✅ Complete | 3 KB |
| CONTRIBUTING.md | ✅ Complete | 6 KB |
| DEPLOYMENT.md | ✅ Complete | 13 KB |
| GHCR.md | ✅ Complete | 6 KB |
| CONFIGURATION.md | ✅ Complete | 32 KB |
| openapi.yaml | ✅ Complete | 26 KB |
| WEBSITE.md | ✅ Complete | 14 KB |
| BRANDING.md | ✅ Complete | 15 KB |
| INDEX.md | ✅ Complete | 5 KB |
| MARKETING.md | ✅ Complete | 22 KB |
| RELEASE_TEMPLATE.md | ✅ Complete | 7 KB |

**Total Documentation:** 15+ files, 180+ KB

---

## Release Artifacts

### Binary Builds
- [x] Linux amd64
- [x] Linux arm64
- [x] Linux arm/v7
- [x] macOS amd64
- [x] macOS arm64
- [x] Windows amd64

### Container Images
- [x] GHCR multi-arch build workflow
- [x] Docker Compose (single-node)
- [x] Docker Compose (3-node cluster)

### Package Managers
- [x] Homebrew formula (`.homebrew/anubiswatch.rb`)
- [x] install.sh (curl-pipe-sh)

### Kubernetes
- [x] Helm chart (`deployments/charts/anubiswatch/`)
- [x] StatefulSet template
- [x] ServiceMonitor for Prometheus

### System Integration
- [x] systemd service file
- [x] Example configurations

---

## Known Issues & Limitations

### Minor Issues
1. **Alert CLI** — `verdict history` and `verdict ack` commands need completion
2. **Cluster check distribution** — Round-robin strategy works, advanced strategies pending
3. **Status page incidents** — Filtering by specific page needs implementation

### Limitations
1. **Test coverage** — Currently ~60%, target is 80%+
2. **Certificate automation** — Built-in certificate automation is not included; use ingress/cert-manager or explicit cert/key configuration
3. **Load testing** — 1000+ monitors scenario not benchmarked
4. **Chaos testing** — Network partition scenarios not tested

### Recommended for v0.0.2
- [ ] OIDC authentication (Google, GitHub)
- [ ] LDAP/Active Directory support
- [ ] DNSSEC full validation
- [ ] Custom dashboard builder
- [ ] PWA support
- [ ] PDF export for status reports

---

## Installation Methods

### Homebrew (macOS/Linux)
```bash
brew tap AnubisWatch/anubiswatch
brew install anubiswatch
```

### Linux (curl-pipe-sh)
```bash
curl -fsSL https://anubis.watch/install.sh | sh
```

### Docker
```bash
docker run -d --name anubis \
  -p 8443:8443 \
  -v anubis-data:/var/lib/anubis \
  ghcr.io/anubiswatch/anubiswatch:0.0.1
```

### Kubernetes
```bash
helm repo add anubiswatch https://anubiswatch.github.io/helm-charts
helm install anubis anubiswatch/anubiswatch --version 0.0.1
```

### Binary
Download from GitHub Releases and extract to `/usr/local/bin`.

---

## Configuration Quick Start

### Minimal Config
```yaml
# anubis.json
{
  "server": {
    "port": 8443
  },
  "storage": {
    "path": "/var/lib/anubis/data"
  }
}
```

### Production Config
See `docs/CONFIGURATION.md` for complete reference.

---

## API Endpoints

### Health Checks
- `GET /health` — Liveness probe
- `GET /ready` — Readiness probe

### REST API (v1)
- `GET/POST /api/v1/souls` — List/create souls
- `GET/PUT/DELETE /api/v1/souls/:id` — Soul CRUD
- `GET /api/v1/souls/:id/judgments` — Judgment history
- `GET/POST /api/v1/channels` — Alert channels
- `GET/POST /api/v1/rules` — Alert rules
- `GET/POST /api/v1/status-pages` — Status pages
- `GET /api/v1/incidents` — Active incidents

### WebSocket
- `WS /ws` — Real-time updates

### Status Pages
- `GET /status/:slug` — Public status page
- `GET /status/:slug/feed.xml` — RSS feed
- `GET /badge/:slug` — Embeddable badge (SVG/JSON)

### Metrics
- `GET /metrics` — Prometheus metrics

---

## Security Considerations

### Implemented
- [x] Non-root user in containers (UID 65534)
- [x] Read-only root filesystem
- [x] Dropped capabilities
- [x] TLS support (bring your own cert or ingress-managed TLS)
- [x] Encrypted storage (AES-256)
- [x] API key authentication
- [x] RBAC for workspaces

### Recommended
- [ ] Rate limiting on API endpoints
- [ ] CSRF protection for dashboard
- [ ] Content Security Policy headers
- [ ] Automatic security updates

---

## Performance Benchmarks

### Single Node (Reference: Raspberry Pi 4)
- **Startup time:** < 2 seconds
- **RAM usage:** 45-60 MB idle
- **Binary size:** 24 MB
- **Max souls (tested):** 500 monitors
- **Check interval:** 15s minimum

### Cluster (Reference: 3-node, 2 vCPU each)
- **Leader election:** < 5 seconds
- **Check distribution:** Automatic
- **Failover time:** < 10 seconds

---

## Release Checklist

### Pre-Release
- [x] Update CHANGELOG.md
- [x] Create git tag v0.0.1
- [x] Build all binaries
- [x] Push Docker images to GHCR
- [x] Update Homebrew formula
- [x] Test all installation methods
- [x] Prepare release notes

### Release Day
- [ ] Create GitHub Release
- [ ] Post X/Twitter thread
- [ ] Submit to Hacker News
- [ ] Post to r/selfhosted
- [ ] Post to r/golang
- [ ] Update website
- [ ] Announce in Discord

### Post-Release
- [ ] Monitor GitHub Issues
- [ ] Respond to feedback
- [ ] Track download/install metrics
- [ ] Prepare v0.0.2 roadmap

---

## Support & Community

- **GitHub:** https://github.com/AnubisWatch/anubiswatch
- **Issues:** https://github.com/AnubisWatch/anubiswatch/issues
- **Discussions:** https://github.com/AnubisWatch/anubiswatch/discussions
- **Discord:** https://discord.gg/anubiswatch
- **X/Twitter:** https://x.com/AnubisWatch

---

## License

Apache 2.0 — See [LICENSE](../LICENSE)

**Built by ECOSTACK TECHNOLOGY OÜ**

---

**⚖️ The Judgment Never Sleeps**
