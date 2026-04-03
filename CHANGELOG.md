# Changelog

All notable changes to AnubisWatch will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Multi-tenancy support with workspaces and role-based access control
- Public status pages with custom domains and ACME integration
- 7 alert notification channels (Slack, Discord, Email, PagerDuty, OpsGenie, Ntfy, Webhook)
- MCP (Model Context Protocol) server for AI integration
- Time-series storage with automatic downsampling
- Docker and docker-compose support
- Installation script for easy setup

### Changed
- Migrated configuration from YAML to JSON (zero external dependencies)

## [0.1.0] - 2024-XX-XX

### Added
- Initial release of AnubisWatch
- 10 protocol checkers: HTTP/HTTPS, TCP, UDP, DNS, ICMP, SMTP, IMAP, gRPC, WebSocket, TLS
- Embedded B+Tree storage (CobaltDB) with WAL and MVCC
- Raft consensus for distributed clustering
- Probe engine with adaptive intervals
- Alert engine with compound conditions and rate limiting
- REST API, WebSocket, and gRPC interfaces
- React 19 + Tailwind 4.1 dashboard
- Single binary deployment with zero dependencies

---

[unreleased]: https://github.com/AnubisWatch/anubiswatch/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/AnubisWatch/anubiswatch/releases/tag/v0.1.0
