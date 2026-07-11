# AnubisWatch Project Audit Report
**Date:** 2026-06-30  
**Target:** AnubisWatch Codebase (Go Backend, React Frontend, Docker/K8s/CI infrastructure)

---

## 1. Executive Summary

A comprehensive automated and manual scan of the AnubisWatch project was conducted. The codebase consists of a modern Go-based monitoring backend utilizing a custom B+Tree database engine (**CobaltDB**) and a Raft consensus engine (**Necropolis**), paired with a React 19 single-page-app dashboard frontend.

### Overall Status: **STABLE & VERIFIED**
The current main branch is in a highly secure, race-clean, and stable state. All 16 Go backend packages and E2E scripts pass test suites successfully, and the frontend builds and type-checks cleanly. Our audit found that while major security bugs and race conditions have been stabilized previously, several critical architectural efficiency bottlenecks, logical concurrency bugs in the gRPC layer, and technical debt items remain.

---

## 2. Critical & Architectural Issues

### 2.1. B+Tree `PrefixScan` Traversal Complexity: $O(N)$ Full Table Scan
*   **Location:** `internal/storage/engine.go:389-419`
*   **Severity:** **HIGH (Architectural/Efficiency)**
*   **Impact:** Performance degradation as the database grows.
*   **Description:**  
    `PrefixScan(prefix)` is intended to return key-value pairs matching a prefix. However, its current implementation finds the leftmost leaf node in the B+Tree and traverses *every single leaf node to the end of the database* checking `strings.HasPrefix(key, prefix)`.
    
    ```go
    // Find leftmost leaf node
    node := db.data.root
    for !node.isLeaf {
        ...
        node = node.children[0]
    }
    // Scan through leaf nodes
    for node != nil {
        for i, key := range node.keys {
            if strings.HasPrefix(key, prefix) && node.values[i] != nil {
                result[key] = node.values[i]
            }
        }
        node = node.next
    }
    ```
    This turns every prefix scan into a full table scan ($O(N)$ where $N$ is the total amount of keys in the database). 
    
    Even though the secondary indexes and workspace tracking optimizations were added to reduce sweeps, they still query the database using `PrefixScan("{ws}/...")`, which internally performs a full leaf traversal.
*   **Remediation:**  
    Implement binary search inside `PrefixScan` to seek to the first key greater than or equal to the prefix, and then traverse leaves sequentially, terminating once the keys no longer match the prefix.

---

## 3. Logical & Concurrency Issues

### 3.1. Race Condition in gRPC Mutation Handlers (`CreateSoul`, `CreateChannel`, `CreateRule`)
*   **Location:** `internal/grpcapi/server.go:1032`, `1420`, `1562`
*   **Severity:** **HIGH (Logical/Concurrency)**
*   **Impact:** Data leakage and returning incorrect resource details under concurrent use.
*   **Description:**  
    The gRPC handlers convert request payloads into map interfaces before calling the database. The ID generation occurs in the adapter layer (e.g. `grpcMapToSoul` in `cmd/anubis/server.go:330`), but it does not write the generated ID back to the map reference argument. Because of this, the gRPC handler has no direct way to learn which ID was created.
    
    To work around this limitation, the handlers list all entities in the workspace and assume the last (or first) element is the newly created resource:
    
    ```go
    souls, err := s.store.ListSoulsNoCtx(user.Workspace, 0, 1) // CreateSoul
    // ...
    channels, _ := s.store.ListChannelsNoCtx(workspace) // CreateChannel
    if len(channels) > 0 {
        if pb := channelToPB(channels[len(channels)-1]); pb != nil {
            return pb, nil
        }
    }
    ```
    If two users concurrently create a resource (e.g., adding two different souls or channels within the same workspace at the same time), both requests may return the same resource (whichever appeared first or last in the list), leading to a validation race condition.
*   **Remediation:**  
    Modify the converter signatures or the write-back adapter code to return the generated ID from the `Save` operations, or generate the UUID in the gRPC handlers before calling `Save` so that the ID is known and can be queried or returned directly.

---

## 4. Security & Vulnerability Concerns

### 4.1. gRPC Layer Lacks Role-Based Access Control (RBAC)
*   **Location:** `internal/grpcapi/server.go`
*   **Severity:** **MEDIUM**
*   **Impact:** Privilege escalation.
*   **Description:**  
    The gRPC server uses interceptors (`authUnaryInterceptor` and `authStreamInterceptor`) to authenticate incoming connections. However, once a connecting token is authenticated, there are no checks validating user roles. Any authenticated user is allowed to execute mutations (e.g., creating and deleting souls, rules, or channels), which bypasses read-only or restricted role constraints enforced in the dashboard REST API.
*   **Remediation:**  
    Check user roles (e.g., Admin vs. Reader) within the gRPC interceptors or handlers before allowing mutating mutations.

### 4.2. JSON Nesting Depth Checker False-Rejections
*   **Location:** `internal/api/rest.go:35-57`
*   **Severity:** **LOW**
*   **Impact:** Legitimate API request bodies could be blocked.
*   **Description:**  
    `maxDepthReader` is used to prevent stack overflows by counting `{` and `[` characters. Because it does not parse strings or check escape states, any JSON request carrying large raw client queries, SQL code, or query matrices containing JSON braces/brackets in string fields can hit false positives and get blocked.
*   **Remediation:**  
    Use a lightweight tokenized depth parsing, or let the standard library `json.Unmarshal` handle parser limits where possible.

---

## 5. Code Quality & Technical Debt

### 5.1. Unused Dead Code Repository `StatusPageRepository`
*   **Location:** `internal/storage/statuspage.go` and `internal/storage/statuspage_test.go`
*   **Severity:** **LOW (Technical Debt)**
*   **Impact:** Increased compile-time and code maintainability overhead.
*   **Description:**  
    The codebase includes `StatusPageRepository` which implements status page lookups using old prefix-less `statuspage/` keys. This was superseded by `SaveStatusPage`, `GetStatusPage`, etc. directly implemented under `CobaltDB`. The repository file is completely unused in the production code and is only referenced in its own tests.
*   **Remediation:**  
    Delete `internal/storage/statuspage.go` and `internal/storage/statuspage_test.go`.

### 5.2. Strict Production Verification / TLS constraint
*   **Location:** `internal/core/config.go:356-360`
*   **Severity:** **LOW (Usability)**
*   **Description:**  
    Setting `environment: "production"` rejects configurations that do not have `server.tls.enabled` set to true. This blocks deployment configurations where the container is deployed behind a TLS-terminating ingress or proxy utilizing plain HTTP calls internally.
*   **Remediation:**  
    Provide clear runbook documentation instructing operators to configure `environment: "production-proxied"` or trailing settings when using ingress-level TLS termination.

---

## 6. Recommendations & Roadmap

1.  **Refactor B+Tree prefix scanner range scans:** Implement binary search on B+Tree nodes for prefix searches to reduce scans from $O(N)$ to $O(\log N + M)$ where $M$ is the number of matching keys.
2.  **Fix gRPC Create Mutations:** Generate IDs before saving or modify `Save` adapter methods to propagate IDs, removing race-prone list lookups.
3.  **Purge Unused Code:** Remove the legacy `StatusPageRepository` implementation to keep `storage/` package clean and unified.

---

## 7. Second Review Pass (2026-07-03) — Deep Correctness & Security Audit

A second, deeper pass (parallel subsystem audits + empirical reproduction) was
run over storage, probe, API/auth, cluster/raft and ops. Findings were verified
against the code before acting; false positives were discarded (e.g. a claimed
badge-endpoint panic — `CalculateOverallStatus` never returns an empty status).

### 7.1 Fixed in this pass (with regression tests)

*   **CRITICAL — B+Tree leaf split silently dropped a key on every split**
    (`internal/storage/engine.go` `splitChild`). The separator key was *moved*
    to the parent instead of *copied*, so it existed in no leaf. Reproduced via
    the public API: **61/1000 keys lost at the default order 32**; loss scaled
    with tree size. This corrupted every entity type (souls, judgments, rules,
    …) once a workspace exceeded one leaf. Fixed to retain all keys in leaves
    and route consistently with `findChildIndex`. Regression: `TestBTreeSplitNoDataLoss`.
*   **CRITICAL — Shipped container config crash-looped.** `configs/container.anubis.json`
    declared `environment: "production"` with TLS disabled, which `validate()`
    rejects; the Docker image bakes and runs it, so every `docker run` exited on
    startup. Changed to `environment: "production-proxied"` (TLS terminated
    upstream) and documented the gate. Regression: `TestContainerConfigValidates`.
*   **HIGH — MCP endpoint bypassed RBAC.** `/api/v1/mcp` was auth-only, but its
    `create_soul` / `acknowledge_incident` tools mutate state, letting a viewer
    bypass REST RBAC. Now gated per-tool at dispatch by the caller's role.
    Regression: `TestMCPCallToolRBAC`.
*   **HIGH — SSRF DNS-rebinding.** The HTTP dialer validated a hostname's IPs
    then re-passed the *hostname* to dial (a second, unvalidated resolution).
    Now pins the connection to a validated IP (`WrapDialerContext`).
*   **HIGH — Two unrecovered panics reachable from attacker-influenced input:**
    SSRF `parseIP` slice bounds on short hosts (operator-precedence bug) and TLS
    `Issuer.Organization[0]` on issuers with no O field. Both crashed the daemon.
*   **HIGH — WAL torn-tail aborted recovery.** A partial tail entry (normal after
    a crash) discarded every preceding entry; now treated as end-of-log.
    Regression: `TestWALTornTailRecovery`.
*   **HIGH — WAL grew unbounded at runtime → disk exhaustion.** The in-memory
    B+Tree's only durable store was an append-only WAL truncated solely at
    startup, so a long-running node's `wal.log` grew forever. Added a background
    checkpoint (`CobaltDB.Checkpoint` + `checkpointLoop`) that atomically
    rewrites the WAL to just the live key/values (dropping superseded writes and
    tombstones) once it exceeds max(8 MiB, 2× live footprint). The rewrite is
    crash-safe (temp file + fsync + atomic rename; old WAL intact until the
    swap). Regressions: `TestWALCheckpointCompactsAndPreserves`,
    `TestCheckpointConcurrentWithWrites` (race).
*   **MEDIUM — Non-atomic WAL append vs. tree apply** (concurrent same-key writes
    could diverge memory from the durable log). Put/Delete now hold a single
    `writeMu` across the WAL append and the B+Tree apply, so durable order always
    matches applied order.
*   **MEDIUM — Data races** on `soulRunner.soul` / `lastStatus` (probe engine)
    and unbounded DNS name decompression recursion (stack-overflow DoS).
*   **MEDIUM — `Get` on a deleted key** returned `(nil, nil)` instead of NotFound.
*   **MEDIUM — gRPC `StreamVerdicts`** lacked the RBAC check its siblings have.
*   **MEDIUM — Readiness `/ready`** never reflected storage health (no `Ping`
    method existed for the assertion to find); added `CobaltDB.Ping`.
*   **MEDIUM — Process exited 0 on REST bind failure**; now exits non-zero.
*   **MEDIUM — OIDC token-validation hardening.** The `azp` (authorized party)
    claim is now enforced (required and equal to the client ID when the ID token
    carries multiple audiences), closing the aud-array acceptance gap, and an
    ID token whose `email_verified` is explicitly `false` is rejected so an
    unverified address can't establish identity. Regression: `TestParseIDToken_Hardening`.
*   **MEDIUM — WebSocket per-IP limits were spoofable** via `X-Forwarded-For`.
    The WS path now derives the client IP the same way the REST path does —
    trusting XFF only from configured `trusted_proxies`. Regression:
    `TestWebSocketRealIP`.
*   **MEDIUM — gRPC shutdown could hang** past the SIGTERM grace period on a
    long-lived stream. `StopWithContext` now force-stops when the shutdown
    deadline elapses.
*   **MEDIUM — `/metrics` cross-tenant exposure** now has an opt-in guard:
    setting `server.metrics_auth: true` requires a bearer token on `/metrics`
    (default stays open for Prometheus). Regression: `TestMetricsRoute_RequiresAuthWhenEnabled`.
*   **LOW — Dockerfile** now has a `HEALTHCHECK`.

### 7.2 Known remaining work (NOT yet fixed — require dedicated design)

*   **MEDIUM — In-memory tombstones/index maps not reclaimed at runtime.** WAL
    checkpointing now drops tombstones from the durable log, but the in-memory
    B+Tree still retains `nil` tombstone slots and the `judgmentIndex` map still
    accumulates entries for purged judgments until the next process restart
    (which rebuilds a clean tree from the compacted WAL). A live tree-compaction
    / index-eviction pass would remove the restart dependency.
*   **CRITICAL for clustering (N/A to default single-node) — Raft is not
    production-ready.** Persistent term/vote/log are never written to storage,
    and `TCPTransport` RPC handlers are never registered, so multi-node
    consensus cannot function or survive restarts. Clustering ships disabled
    (`necropolis.enabled=false`, `serve --single`); treat it as experimental.
*   **LOW — `/api/v1/events` (SSE) is unauthenticated.** It currently streams
    only heartbeats (no data leak) but holds a goroutine per client bounded
    solely by the IP rate limiter; adding auth on the SSE upgrade would match
    the `/ws` path. (`/metrics` now has the `server.metrics_auth` opt-in guard.)
*   **MEDIUM — Critical subsystem start failures** (alert manager, cluster) are
    logged as warnings; the process stays "healthy" while blind to alerts.
    (Note: `AlertManager.Start` currently never returns an error, so the visible
    risk is limited; a readiness signal reflecting subsystem health is the
    proper fix.)
