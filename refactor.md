# AnubisWatch — Refactor & Issue Backlog

**Date:** 2026-06-18
**Scope:** Project-wide analysis of `github.com/AnubisWatch/anubiswatch` after the K6/K7/K9/K10 refactor wave.
**Method:** Manual read + race detector (`go test -race -short`) + gosec-tagged comment audit + YAML structure check.

The codebase is in a healthy state — `go build ./...` and `go test -short ./...` pass on every package — but a focused sweep surfaced real defects that should be fixed before the next release. The list is ranked by severity.

---

## Critical

### 1. CI workflow has duplicate job blocks (`.github/workflows/ci.yml`)
The same seven jobs are defined twice: `script-validation` (lines 281 and 561), `build` (lines 310 and 590), `chaos-tests` (lines 375 and 655), `load-tests` (lines 394 and 674), `benchmarks` (lines 413 and 693), `integration-tests` (lines 437 and 717), `helm-tests` (lines 477 and 757). `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml').read())"` silently drops the first occurrence, but GitHub Actions does **not** parse with Python — it fails the run, or worse, picks whichever block its parser likes. Additionally, the duplicated `helm-tests` block has the second occurrence missing a `Helm Chart Tests` comment header (line 477 has it, line 757 doesn't), and the trailing `docker-security` job has a malformed comment (`# Docker Security Scan  # Docker Security Scan`, line 840 — likely the result of a botched merge).

→ Delete the second copy of each job (lines 560–838). Keep the block at 281–559 (which already carries the K10 comments). Fix the duplicated comment on line 840.

### 2. Data race: `SaveJourney` mutates `db.journeyIndex` without `db.mu` (`internal/storage/storage.go:186`)
`db.journeyIndex[j.ID] = workspaceID` is written while every other Save* method in the same file (SaveAlertEvent, SaveIncident, SaveStatusPage, SaveDashboard, SaveMaintenanceWindow) takes `db.mu.Lock()` first. The matching reader `GetJourneyNoCtx` (`internal/storage/engine_journey.go:15`) reads `db.journeyIndex[id]` with no lock. The race detector doesn't trigger on the existing tests because no test reads and writes concurrently from multiple goroutines, but the data race is real. The race detector **does** catch it in `TestTimeSeriesStore_StartCompaction` (a related but separate race — see #3), so the storage layer has real concurrency bugs hiding.

→ Wrap the write in `db.mu.Lock()` / `db.mu.Unlock()`, mirroring the pattern in `judgments.go:46–49`. Either also take the read lock in `GetJourneyNoCtx`, or document that callers must serialize.

### 3. Data race: `TimeSeriesStore.compactionLoop` reads `ts.stopCh` without `ts.stopMu` (`internal/storage/timeseries.go:242`)
`StopCompaction` closes and nils `ts.stopCh` under `ts.stopMu.Lock()` (line 226–232). The goroutine spawned in `StartCompaction` reads `ts.stopCh` in the `select` (line 242) without holding the lock. The race detector flags this on `TestTimeSeriesStore_StartCompaction`. Under heavy load the loop may see a partially-zeroed channel struct.

→ Capture the channel into a local variable at the start of the loop, or move the read under the same `ts.stopMu` (use `RLock` since it's a read, or use `sync/atomic.Pointer[chan struct{}]`).

### 4. Data race: `Engine.AssignSouls` mutates `circuitBreakers` under the wrong lock (`internal/probe/engine.go:190`)
`AssignSouls` takes `e.mu.Lock()` (line 165) and then does `delete(e.circuitBreakers, id)` (line 190). `getCircuitBreaker` reads `circuitBreakers` under `e.cbMu.RLock()` (line 623). Two different mutexes protecting the same map = race. Race detector catches this in `TestEngine_RemoveSouls`. In production, a check that fires right after an `AssignSouls({})` can read freed memory.

→ Take `e.cbMu.Lock()` before line 190 (or guard both maps with the same mutex). The cleanest fix is to delete the `cbMu` field entirely and use `e.mu` for both — there's no reason for two locks.

### 5. Data race: `Engine.onJudgment` read without lock (`internal/probe/engine.go:350, 451`)
`SetOnJudgment` writes `e.onJudgment` under `e.mu.Lock()` (line 522), but `judgeSoul` (line 350) and `TriggerImmediate` (line 451) read it with no lock. A startup that calls `SetOnJudgment` while a check is in flight can race on the function pointer. Race detector didn't fire on the existing tests because the test mock doesn't set the callback concurrently with checks.

→ Read under `e.mu.RLock()` (cheap, the callback is rare) or use `atomic.Pointer[func(*core.Judgment)]`.

### 6. `DeleteAlertChannel`/`DeleteAlertRule`/`DeleteJourney` leak secondary index entries
`SaveAlertChannel` (storage.go:636) and `SaveAlertRule` (storage.go:701) don't update their secondary indexes (`channelIndex`, `ruleIndex`) at all. `DeleteAlertChannel` (storage.go:693) and `DeleteAlertRule` (storage.go:758) likewise don't clean them up. The O(1) `GetChannelNoCtx`/`GetRuleNoCtx`/`GetJourneyNoCtx` will return a workspaceID for a deleted entity, leading to a misleading `NotFoundError` from `db.Get` instead of "already gone" — and after enough churn, the in-memory index grows unbounded. Same for `DeleteJourney` (storage.go:241).

→ Add `db.channelIndex[ch.ID] = ws` in SaveAlertChannel (under `db.mu.Lock`); add `delete(db.channelIndex, id)` in DeleteAlertChannel. Same for rules and journeys. The `rebuildSecondaryIndexes` (engine.go:1213) handles this on restart, so the bug is invisible after a restart — but long-running clusters are affected.

---

## High

### 7. `signGossip` marshals the message twice (`internal/raft/discovery.go:871, 884`)
```go
body, err := json.Marshal(msg)   // line 871 — first marshal
if err != nil { ... }
clean := *msg
clean.HMAC = ""
body, _ = json.Marshal(&clean)   // line 884 — second marshal, discards first
```
The first marshal result is overwritten. Wasted CPU on every gossip tick (1 Hz by default). The comment claims this is "for safety in case the tag ever changes" but the first marshal already produces the canonical form (HMAC field is empty by default → `omitempty` drops it). Just use the first result.

→ Remove the second `json.Marshal(&clean)` call and use `body` from line 871.

### 8. K9 comment claims Join is protected; it isn't (`internal/raft/discovery.go:31, 105, 175, 856`)
The K9 docs and warning logs say "gossip and join messages" are HMAC-protected. Only gossip is — there's no Join path in the codebase (searched: no `JoinRequest`, `HandleJoin`, `joinListener` symbols exist). Either the join path was never implemented or the docs are wrong. Either way, the comment is misleading and a future implementer might trust it.

→ Either remove the join references, or add a comment explaining that the cluster currently only supports static peer configuration and gossip — both already gated by Raft TLS (K7) — so join is unnecessary.

### 9. K7 comment in `diagnoseTLSFailure` lies about the gate (`internal/probe/tls.go:309–315`)
```go
// G402 suppress: this code path only runs after the user has
// explicitly opted into skip-verify. The K7 master gate in
// internal/probe/engine.go's applySecurityGate enforces a
// process-wide policy; here we just turn the kernel TLS
// verification off and replace it with our own checks below.
InsecureSkipVerify: true, // #nosec G402 -- see comment + K7 gate
```
The K7 gate does **not** gate this path. `diagnoseTLSFailure` always sets `InsecureSkipVerify: true` regardless of the global flag. The custom `VerifyPeerCertificate` does the right thing (returns nil = "handshake passes", then we inspect the certs), so the behavior is OK — but the comment is documentation-by-lie. A future reader will believe TLS verification is enforced here, when in fact it's the only TLS probe path that disables kernel verification.

→ Replace the misleading comment with an accurate one: "On TLS-handshake failure, we dial again with skip-verify to capture the peer certs for diagnostic reporting. The cert chain is then validated by our own checks in `extractTLSCertsOnly`."

### 10. `LocalAuthenticator.Shutdown` is not idempotent (no `sync.Once`) (`internal/auth/local.go:279–288`)
LDAP and OIDC use `sync.Once` (ldap.go:309, oidc.go:793). Local uses `select`/`default` on `stopCleanup`. The pattern is "safe but fragile":
- If two goroutines call `Shutdown` simultaneously, both can pass the `default` branch before either reaches `close(a.stopCleanup)` → double-close panic.
- A single sequential second call returns early via the `<-a.stopCleanup` select arm. Safe but inconsistent with the other two auth backends.

→ Add a `stopOnce sync.Once` field and use `a.stopOnce.Do(func() { close(a.stopCleanup); <-a.cleanupDone })`. This also simplifies the code.

### 11. `applySecurityGate` runs only inside `judgeSoul` and `TriggerImmediate` — bypass on direct checker calls (`internal/probe/`)
The K7 memory says "production always goes through engine, and tests use self-signed certs" — the auth package and K7 release notes both acknowledge this. **But** the README and ARCHITECTURE.md don't say so, and there's no test asserting that the only path to a checker is through the engine. A future contributor adding a code path that calls `checker.Judge(ctx, soul)` directly (the api/grpcapi packages do exactly this with `grpcStorageAdapter` and similar) silently bypasses the gate. There's no static enforcement.

→ Add a docstring on the `Checker` interface or `EngineConfig` saying "all checkers must be invoked via `Engine.judgeSoul` / `Engine.TriggerImmediate`; direct `Checker.Judge` calls bypass the K7 master gate." Plus add a CI grep that fails if `\.Judge\(` is called outside `internal/probe/`.

---

## Medium

### 12. `GetStorageStats` does N+M+K prefix scans where N = workspaces × entity types, M = non-workspace prefixes, K = legacy "/" scans (`internal/storage/retention.go:265–287`)
The new code lists 3 non-workspace prefixes + 12 entity prefixes per workspace + 12 legacy "/" prefixes. For a 10-workspace cluster that's 135 prefix scans per call. The original `PrefixScan("")` was O(totalKeys) in memory but a single tree walk. The new approach may actually be **slower** for small datasets because each `PrefixScan` allocates a new map and sorts.

→ Profile on a real workload. If prefix scans are the bottleneck, batch them with a single B+Tree walk that classifies each key. If they're not (i.e. memory was the bottleneck), document the trade-off.

### 13. `categorizeKey` is missing categories added by the new entity types (`internal/storage/retention.go:342`)
The function handles `souls`, `judgments`, `ts`, `verdicts`, `journeys`, `channels`, `system`, `raft` — but the new GetStorageStats iterates over `alerts/`, `statuspages/`, `dashboards/`, `maintenance/`, `journey-runs/`. All of these fall into the `"other"` bucket, so the new entities are lumped together in stats output. The new dashboards showing "Other: 1.2 GB" are useless to operators.

→ Add cases for the new types so each is its own bucket.

### 14. Race detector times out `TestLocalAuthenticator_ChangePassword_WeakNewPassword` (`internal/auth/local_test.go:654`)
With `-race`, the bcrypt calls in the test helper add up to >60s of CPU (the default test timeout). This is not a code bug but a test ergonomics bug — `go test -race ./...` will routinely hit the default 30-min timeout in CI.

→ Add `{timeout: 120000}` to the test, or use `bcrypt.MinCost` in the test-only path. Document why the test is slow.

### 15. `e.exportloopref` is deprecated in Go 1.22+ (`.golangci.yml:15`)
Replaced by `copyloopvar` (the actual bug it caught is now a compile error in Go 1.22+). The linter may already be a no-op in CI; if so the linter is dead config. If it's still active, the linter is deprecated and should be removed.

→ Remove `exportloopref` from `.golangci.yml`. Verify with `golangci-lint --version`.

### 16. `compactToResolution` "tgtBucket" comment claims it's reserved (`internal/storage/timeseries.go:295`)
```go
"tgt_bucket", tgtBucket) // tgtBucket is reserved for future compaction-window refinements
```
`compactToResolution` no longer uses `tgtBucket` for any actual work — it computes `truncateToResolution(bucketTime, tgtRes)` later. The log line is decorative. Either remove it or wire it in.

→ Either remove the unused log field, or actually use `tgtBucket` to skip summaries already in their target window (an obvious optimization).

### 17. `authEnabled()` is dead code in production (`internal/raft/discovery.go:945`)
The function is only called from `discovery_test.go:1154`. The production gossip path inlines `len(d.clusterSecret) > 0` instead. Dead exported-style method on a non-exported struct — keep tests using the local expression for clarity.

→ Remove `authEnabled()`. Update the test to use `len(d.clusterSecret) > 0` directly.

### 18. `applySecurityGate` shallow-copies Soul struct fields not relevant to the gate (`internal/probe/engine.go:584`)
`c := *soul` copies every field of Soul (including DNSConfig, TCPConfig, etc.) and then the code re-walks HTTP/SMTP/IMAP/GRPC/WebSocket for the per-pointer deep copy. The `if !wasInsecure { return soul }` short-circuit mitigates the cost in the common case, but a soul with `HTTP.InsecureSkipVerify=true` triggers a full struct copy that touches every other config block. Not a correctness bug, just unnecessary work.

→ Move the short-circuit checks to early-return per type: if only HTTP has it set, only deep-copy `c.HTTP`. Or accept the cost (it's a shallow copy, not a deep clone of nested slices/maps) — but document the trade-off.

---

## Low

### 19. `e.mu` and `e.cbMu` are independently held (`internal/probe/engine.go`)
Even if #4 is fixed, the engine has two locks with overlapping access (`e.souls` is under `e.mu`; `e.circuitBreakers` was under `e.cbMu`). Locking discipline is fragile. The two could be merged.

→ Long-term: drop `e.cbMu` and use `e.mu` for both maps.

### 20. `gosec` exclude list documents G115 as "future work" (`.golangci.yml:26–32`)
The 54 G115 hits are tracked but not fixed. With `-race` and `-short` the build is green, but a future gosec release that tightens the rule would block CI. The CI excludes are listed in `gosec` in the workflow (line 217 of ci.yml), so the gate accepts them. This is documented debt, not a defect.

→ Either (a) budget a few hours to walk the 54 hits, or (b) leave a TODO with an owner and a date.

### 21. `deployment-guide.md:263` documents a deploy-time `ANUBIS_CLUSTER_SECRET` injection, but the Helm chart only requires it when `necropolis.enabled=true` (`deploy/helm/anubiswatch/templates/secret.yaml:26–28`).
This is fine, but the docs could be clearer that single-node (`--single`) deployments do **not** need a cluster secret — the K9 warning at startup is harmless in single-node mode.

→ Add a sentence in `docs/deployment/guide.md` clarifying that `--single` mode does not require `ANUBIS_CLUSTER_SECRET`.

### 22. `Dockerfile` uses `golang:1.26.4-alpine` — `alpine` is implicit `latest` for the `alpine` tag (Dockerfile)
The pinned image `golang:1.26.4-alpine` is good, but Alpine ships with musl libc, which has caused known CVEs in the past (e.g. CVE-2025-26519 in musl 1.2.5). Not currently vulnerable, but the image pinning strategy should pick an explicit musl version (`golang:1.26.4-alpine3.20` or similar) to keep the runtime reproducible.

→ Pin the Alpine minor version explicitly. Or document why the floating tag is acceptable.

### 23. `internal/probe/grpc.go:49` warns via `slog.Warn` even when K7 has accepted the request
```go
if soul.GRPC != nil && soul.GRPC.InsecureSkipVerify {
    slog.Warn("SECURITY WARNING: gRPC check has InsecureSkipVerify enabled. ...")
}
```
This runs in `Validate`, which is called by the engine AFTER `applySecurityGate` has set the flag to false. So in the K7-default-deny path, the soul struct still has `InsecureSkipVerify=true` when Validate sees it, and the warning fires even though the actual check will be secure. Operators get spammed with false-positive warnings.

→ Move the warning to run only on the per-checker path after `applySecurityGate` has run, or check the original (un-gated) flag was preserved in the audit event.

### 24. `internal/probe/http.go:60`, `internal/probe/smtp.go:48`, `internal/probe/smtp.go:339`, `internal/probe/websocket.go:55` — same issue as #23 in different checkers.

### 25. `cmd/anubis/main.go` docs say `ANUBIS_ADMIN_PASSWORD` is an env var (line 110) but it's never read from env in the codebase
`grep -rn "ANUBIS_ADMIN_PASSWORD" --include="*.go"` only finds the help text. The actual env loading happens in `c.applyEnvOverrides()` in `core/config.go`. This is fine — but a reader of the help text would think the CLI is the only way.

→ Add a line to the help text clarifying the env var path: `ANUBIS_ADMIN_PASSWORD  Initial admin password (or set in config file)`.

### 26. `cmd/anubis/main.go:243` warning fires once at startup but is hard to grep for
The K7 warning `"K7: InsecureSkipVerify enabled — TLS verification is disabled for any check that requests it. THIS IS A MITM VECTOR IN PRODUCTION."` is good but uses unstructured slog. A structured `event=k7_insecure_skip_verify_enabled source=cli` would be parseable by the audit-log agent.

→ Add structured fields.

### 27. `Dockerfile` `COPY . .` is opaque (Dockerfile)
The `.dockerignore` is now 113 lines, but nothing in CI verifies it. A future contributor could easily re-add `anubis.json` (which contains the admin password). The current `.dockerignore` excludes it (line 23), but a regression would only be caught at build time.

→ Add a CI step that runs `docker build --no-cache -t test .` and `docker run --rm test ls /` to assert no banned files are present.

---

## Verified-clean (no action)

- `go build ./...` clean on Go 1.26.4 toolchain.
- `go vet ./...` clean.
- `go test -short ./...` passes on every package.
- `govulncheck ./...` reports "No vulnerabilities found." (per the K10 status messages).
- `gosec` with the documented exclusions runs clean.
- All `AddUser` callers updated for the new `(user, err)` signature.
- All auth init errors propagate in `BuildServerDependencies` (server.go:634–652).
- Shutdown idempotency on LDAP (`sync.Once`) and OIDC (`sync.Once`) — good.
- K9 sign/verify constant-time comparison (`hmac.Equal`) — good.
- K7 engine-level warning, config YAML field, CLI flag, and env var all wired up.
- Config files written with 0600, WAL with 0600, session files 0600, backup temp 0600, backup dir 0750 — good.
- `.dockerignore` comprehensive for secrets/credentials.
- CI gate `static-analysis` (gofmt + vet + gosec + govulncheck) merged into one job — good for cache and PR surface.
- Helm chart enforces `secrets.clusterSecret` when `necropolis.enabled=true`.

---

## Suggested order of work

1. **CI YAML duplicate jobs** (#1) — quick fix, prevents a CI disaster.
2. **Storage index mutex coverage** (#2, #3, #4, #5, #6) — fix the data races. The race detector gives you the test cases for free.
3. **Auth Shutdown idempotency** (#10) — small, prevents a class of panic.
4. **K9 signGossip double-marshal** (#7) and K9 doc lie (#8) — quick cleanups.
5. **K7 misleading comment** (#9) and the false-positive warnings (#23, #24) — documentation/log correctness.
6. Everything else as time permits.

<next_steps>
1. [CRITICAL] .github/workflows/ci.yml:560-838 — delete the duplicate job block (script-validation, build, chaos-tests, load-tests, benchmarks, integration-tests, helm-tests) and fix the doubled comment on line 840
2. [CRITICAL] internal/storage/storage.go:186 — wrap `db.journeyIndex[j.ID] = workspaceID` in db.mu.Lock/Unlock; also take lock in GetJourneyNoCtx at engine_journey.go:15
3. [CRITICAL] internal/storage/timeseries.go:242 — capture `ts.stopCh` into a local variable inside compactionLoop or read it under `ts.stopMu`
4. [CRITICAL] internal/probe/engine.go:190 — take e.cbMu.Lock() before deleting from circuitBreakers, or drop cbMu entirely
5. [CRITICAL] internal/probe/engine.go:350,451 — read e.onJudgment under e.mu.RLock() or use atomic.Pointer
6. [CRITICAL] internal/storage/storage.go — add channelIndex/ruleIndex maintenance to SaveAlertChannel/SaveAlertRule/DeleteAlertChannel/DeleteAlertRule; add journeyIndex cleanup to DeleteJourney
7. [HIGH] internal/auth/local.go:279 — add sync.Once for idempotent Shutdown
8. [HIGH] internal/raft/discovery.go:871-884 — remove the redundant second json.Marshal in signGossip
9. [HIGH] internal/probe/tls.go:309 — rewrite the misleading K7 gate comment in diagnoseTLSFailure
10. [MEDIUM] internal/storage/retention.go:342 — extend categorizeKey with alerts/statuspages/dashboards/maintenance/journey-runs buckets
</next_steps>
