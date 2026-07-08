# Production Readiness Report — AnubisWatch

**Date:** 2026-07-08  
**Branch reviewed:** `feat/api-auth-hardening`  
**Assessment type:** Repo evidence review + focused local verification + external readiness baseline  
**Verdict:** **PRODUCTION CANDIDATE for single-node, TLS-proxied deployment after final release gates; NOT PRODUCTION-CLEARED for multi-node/cluster mode.**

## Executive summary

AnubisWatch has the core ingredients for a production candidate: strong CI gates, focused security hardening, production deployment scripts, Kubernetes/Helm assets, non-root container defaults, readiness/liveness endpoints, structured logging defaults, and regression tests for prior critical issues.

This report does **not** grant an unconditional production-ready signoff. The defensible public claim is narrower: **single-node, TLS-proxied production candidate, pending a clean release branch, full release-gate rerun, and captured preflight/smoke evidence.** Multi-node/Raft cluster mode remains outside the production-cleared scope.

## Research baseline used

This assessment maps local evidence against current production-readiness guidance:

- **CISA Secure by Design**: security should be a core product requirement; products should ship secure-by-default with logging, MFA/SSO-style controls where applicable, and measurable security outcomes. Source: <https://www.cisa.gov/securebydesign>
- **OWASP ASVS 5.0**: ASVS provides a vendor-neutral basis for testing web application technical security controls and secure development requirements. Source: <https://owasp.org/www-project-application-security-verification-standard/>
- **Kubernetes production readiness guidance**: production workloads should have stdout/stderr structured logging, externalized config, graceful shutdown, health signals, persistent storage strategy, non-root/read-only filesystem hardening, resource requests/limits, stable image tags/digests, runbooks, rollback, and smoke checks. Source: <https://learnkube.com/production-best-practices>
- **SRE-style production readiness review criteria**: clear ownership, monitoring, alerting, SLOs, capacity, rollback, incident response, runbooks, and go/no-go criteria.

## Current repo state that affects release confidence

`git status` at assessment time showed many uncommitted modifications and untracked tests/docs, including backend, config, docs, and web files. Parallel-agent mailbox updates also reported recent scoped commits on related areas.

**Release implication:** do not cut a production release directly from this dirty working tree. First merge/rebase to a clean release candidate branch, rerun the full CI matrix, and capture deployment evidence from that exact commit/image digest.

## Verification performed in this pass

The initial combined Go test slice timed out, so verification was retried in smaller package chunks, which is the known stable approach for this repo.

| Check | Result |
|---|---:|
| `go test ./internal/api ./internal/grpcapi -count=1` | PASS |
| `go test ./internal/storage -run 'TestBTreeSplitNoDataLoss|TestWALTornTailRecovery|TestWALCheckpointCompactsAndPreserves|TestCheckpointConcurrentWithWrites' -count=1` | PASS |
| `go test ./internal/probe -run 'TestSSRF|TestHTTP|TestTLS' -count=1` | PASS |
| `go test ./internal/core -run 'TestContainerConfigValidates|Test.*Config' -count=1` | PASS |
| `go test ./cmd/anubis -run 'Test.*Config|Test.*Serve|TestMain' -count=1` | PASS |
| `cd web && npm run lint` | PASS |
| `cd web && npm run build` | PASS |

Not run in this pass: full `go test ./...`, race suite, full web coverage, Playwright E2E, Docker image build, Helm render/preflight, live smoke test, dependency audit. Existing CI and peer-status evidence indicate those are covered elsewhere, but this report only claims the checks above as directly rerun here.

## Readiness assessment by domain

### 1. Build, CI, and release gates — **Strong, pending clean branch rerun**

Evidence:

- `.github/workflows/ci.yml` runs backend tests with `-race` and coverage, frontend lint/tests/coverage, dashboard E2E, static analysis, `gosec`, `govulncheck`, script validation, binary build, Docker build, Docker context leak check, Helm tests, chaos/load/benchmarks on main push paths.
- `Makefile` exposes `build`, `test`, `test-short`, `benchmark`, `preflight-production`, `smoke-production`, and `capture-production-evidence` targets.
- Web `npm run build` passed locally and produces split chunks with no >500 kB warning in this run.

Caveat:

- The current working tree is dirty. Full CI must pass on the exact release commit.

### 2. Security posture — **Substantially improved, still needs final release-gate evidence**

Evidence:

- SSRF validator now permits only `http`, `https`, `ws`, and `wss` schemes for URL targets and blocks private/link-local/loopback/multicast/reserved networks by default.
- Container config uses `environment: "production-proxied"` with TLS disabled only behind a TLS-terminating proxy; strict `environment: "production"` rejects plaintext serving.
- `configs/anubis-prod.json` enables TLS with `min_version: 3` (TLS 1.2 minimum) and storage encryption.
- gRPC handlers now use `checkPermission` with `core.MemberRole(user.Role).Can(permission)` and return `codes.PermissionDenied` for insufficient roles.
- gRPC create handlers generate IDs before saving and retrieve by generated ID, avoiding prior race-prone list-after-create behavior.
- REST security headers include nosniff, frame denial, referrer policy, CSP, and HSTS only when TLS is enabled.
- CI includes `gosec` and `govulncheck` gates.

Caveats:

- No dependency/vulnerability audit was rerun in this pass.
- No external penetration test or ASVS control-by-control signoff is recorded.
- Metrics are public by default unless `server.metrics_auth` is enabled; acceptable for Prometheus-only private networks, but should be explicitly configured for internet-exposed deployments.

### 3. Reliability and data durability — **Good for single-node; cluster mode not cleared**

Evidence:

- Storage regression checks for B+Tree split data loss, WAL torn-tail recovery, WAL checkpoint compaction, and concurrent checkpoint/write behavior passed locally.
- WAL checkpointing and write locking are present in `internal/storage/engine.go`.
- Health and readiness endpoints are present and wired into deployment manifests.
- Production smoke script checks `/health`, `/ready`, `/metrics`, `/api/openapi.json`, dashboard shell, and optional authenticated login/API paths.

Caveats:

- Multi-node/Raft production readiness was not verified here. Existing audit notes identify cluster mode as outside the safe production envelope unless separately validated.
- No long-running soak, restore drill, or real workload capacity test was run in this pass.

### 4. Deployment and Kubernetes readiness — **Good artifacts; requires environment-specific evidence**

Evidence:

- Dockerfile is multi-stage, pins Go and Alpine versions, runs as non-root `anubis:anubis`, includes a healthcheck, and documents read-only root filesystem requirements.
- Helm defaults include non-root, dropped capabilities, `readOnlyRootFilesystem: true`, resource requests/limits, persistent volume support, ServiceAccount creation, liveness/readiness probes, optional PDB, optional ServiceMonitor, and secret validation.
- `values-production.example.yaml` documents immutable image preference, TLS, trusted proxy configuration, allowed origins, persistent storage, secrets, logging, and production resource sizes.
- Raw Kubernetes deployment uses non-root, no privilege escalation, dropped capabilities, secret refs for admin/encryption key, probes, and resource limits.
- Production runbook covers preflight, deploy, rollout status, smoke test, rollback, and evidence capture.

Caveats:

- Helm preflight was not run because no environment-specific `VALUES` file or cluster context was supplied.
- Production values currently require operator-supplied secrets, TLS/cert-manager/ingress configuration, DNS, and image digest.
- Dockerfile cannot itself enforce `readOnlyRootFilesystem`; this must be enforced by Kubernetes/security policy or `docker run --read-only`.

### 5. Observability and operations — **Usable baseline; SLOs/alerts still need environment ownership**

Evidence:

- App supports JSON logging to stdout by config.
- `/metrics`, `/health`, and `/ready` are documented and smoke-tested by script.
- Deployment evidence script records rollout, pods, services, ingress, workload YAML, Helm history/status, events, values checksum, image reference, and optional smoke output while avoiding secret-bearing values file capture.
- Production runbook documents rollback and evidence capture.

Caveats:

- No production SLO/SLA, alert routing policy, on-call owner, dashboard screenshots, or live Prometheus/Grafana alert evidence is present in this repo.
- `/metrics` auth posture must be chosen deliberately per environment.

### 6. Frontend readiness — **Build/lint clean in this pass; coverage exceptions documented**

Evidence:

- `npm run lint` passed.
- `npm run build` passed.
- `TEST_COVERAGE_EXCEPTIONS.md` documents practical remaining coverage exceptions and stable coverage command expectations.
- Peer-status evidence reports recent web validation with Vitest and Playwright E2E passing after bundle splitting and test-noise cleanup.

Caveats:

- Full web coverage and Playwright E2E were not rerun in this pass.
- Some large UI/state matrices remain documented as coverage exceptions.

## Go/no-go decision

### Conditional go scope

**GO only after final gates pass** for:

- Single-node deployment.
- TLS terminated at ingress/reverse proxy, or app TLS enabled with TLS 1.2+.
- Clean release branch/commit.
- Full CI green on the release commit.
- `scripts/production-preflight.sh` and `scripts/production-smoke.sh` passing for the target environment.
- Deployment evidence captured and archived.

### No-go scope

**NO-GO as of this report** for:

- Multi-node/Raft cluster production claims without a dedicated cluster PRR, chaos/failure testing, restart/recovery validation, and data-consistency evidence.
- Internet-exposed deployments without explicit allowed origins, trusted proxy config, TLS/HSTS posture, metrics exposure decision, and secret management review.
- Release directly from the current dirty working tree.

## Blockers before public “production-ready” claim

1. **Clean release candidate required**  
   Resolve/commit/discard the current uncommitted changes, then rerun full CI on the exact release commit.

2. **Live deployment evidence missing**  
   Run Helm preflight and smoke checks against the target environment and archive output from `scripts/capture-deployment-evidence.sh`.

3. **Cluster mode not production-cleared**  
   Update docs/release notes to explicitly scope the production claim to single-node/proxied mode unless a separate cluster readiness package is completed.

4. **Final supply-chain/security checks not rerun in this pass**  
   Run `govulncheck ./...`, frontend package audit, Docker image scan, and image digest promotion on the release candidate.

## Recommended release checklist

Run on a clean release branch:

```bash
# Backend and static gates
go test -race -coverprofile=coverage.out -covermode=atomic ./...
go vet ./...
govulncheck ./...

# Frontend
cd web
npm run lint
npm run test:coverage
npm run build
npm run e2e
cd ..

# Container and deployment
docker build -t ghcr.io/anubiswatch/anubiswatch:<sha> .
VALUES=values-production.yaml ANUBIS_PREFLIGHT_CREATE_NAMESPACE=true scripts/production-preflight.sh
scripts/production-smoke.sh https://anubiswatch.example.com
ANUBIS_EVIDENCE_VALUES=values-production.yaml \
ANUBIS_EVIDENCE_IMAGE=ghcr.io/anubiswatch/anubiswatch@sha256:<digest> \
ANUBIS_EVIDENCE_BASE_URL=https://anubiswatch.example.com \
ANUBIS_EVIDENCE_RUN_SMOKE=true \
scripts/capture-deployment-evidence.sh
```

## Final statement

AnubisWatch should be described as a **single-node, TLS-proxied production candidate**, not as broadly production-ready. A production-ready signoff requires a clean release candidate, full gate results, captured deployment evidence, and separate cluster-specific validation before any multi-node/Raft claim.
