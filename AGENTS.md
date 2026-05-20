# AGENTS.md

This file is loaded into WrongStack's system prompt as project context.
Keep it concise, factual, and durable: write the information future agents
need before they touch this codebase.

## Project brief

- **Purpose:** Self-hosted uptime and synthetic monitoring platform. Weighs service health (Souls), records Judgments, fires Verdicts, serves a real-time dashboard.
- **Primary users:** DevOps engineers, SREs, platform teams running their own monitoring infrastructure.
- **Runtime/deployment:** Single `anubis` binary. Runs as CLI server (`anubis serve`). Embedded React dashboard served from the binary.
- **Main entry points:** `cmd/anubis/` — start with `main.go`, `server.go`.

## How to work safely

- **Build:** `go build ./...`
- **Test:** `go test ./...`
- **Run locally:** `go run ./cmd/anubis serve --single`
- Never commit generated binaries, `.db` files, or credentials.
- `configs/anubis.yaml` is the canonical example config; runtime config is typically at `data/anubis.json`.

## Architecture notes

- **CobaltDB** (`internal/storage/`): Embedded B+Tree engine with WAL. Key format: `{workspaceID}/souls/{soulID}`, `{workspaceID}/judgments/{soulID}/{timestamp}`.
- **Probe Engine** (`internal/probe/`): Scheduler + worker pool. Check types live in `internal/probe/*.go` (http, tcp, dns, icmp, smtp, imap, grpc, websocket, tls).
- **Alert Engine / Ma'at** (`internal/alert/`): Routes Verdicts to dispatchers (webhook, Slack, Discord, email, PagerDuty, etc.).
- **Journey Executor / Duat** (`internal/journey/`): Multi-step synthetic monitoring. Steps produce probe Judgments; JourneyContext carries state between steps.
- **Cluster / Necropolis** (`internal/cluster/`, `internal/raft/`): Raft-backed distributed coordination. Pharaoh = Raft leader, Jackal = follower.
- **Storage key prefix convention:** Workspace-scoped. Always include `workspaceID` in keys. Tenant isolation is enforced at the API layer.

## Domain knowledge

- **Soul** = monitored target. **Judgment** = single check result. **Verdict** = alert decision. **Journey** = multi-step synthetic scenario.
- **SoulStatus values:** `alive`, `dead`, `degraded`, `unknown`, `embalmed` (maintenance window).
- **Auth auto-enable:** When `auth.type=local` with admin credentials, or `oidc` with issuer, or `ldap` with URL — auth is automatically enabled.
- **Config defaults:** Set by `c.SetDefaults()` in `internal/core/config.go`. Call it before `validate()`.

## Verification checklist

- `go build ./...` — must compile clean
- `go test ./...` — all tests pass (one pre-existing failure in `TestGenerateConfig_Basic` is unrelated)
- `go vet ./...` — no warnings
- Manual smoke: `go run ./cmd/anubis serve --single` → hit `http://localhost:8080`, login, create a Soul, trigger a judge

## Useful pointers

- Dashboard: `http://localhost:8080` (default creds generated on first run, stored in `data/.admin_password`)
- API docs: `http://localhost:8080/api/docs`
- Prometheus metrics: `http://localhost:8080/metrics`
- Config reference: `configs/anubis.yaml`
- Architecture: `ARCHITECTURE.md`
- Deployment runbook: `docs/deployment/production-runbook.md`