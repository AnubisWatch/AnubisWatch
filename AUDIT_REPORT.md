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
