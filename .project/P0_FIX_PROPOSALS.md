# P0 Blocking Issues — Fix Proposals

**Generated:** 2026-04-05  
**Priority:** Critical (Production Blocking)  
**Total Effort:** 15.5 hours

---

## Status

| Fix | Status | Notes |
|-----|--------|-------|
| FIX-001: WebSocket key | ✅ Complete | Uses `crypto/rand` |
| FIX-002: TLS default | ✅ Complete | `InsecureSkipVerify: false` default |
| FIX-003: Size limits | ✅ Complete | Applied to TCP, UDP, HTTP, WebSocket, gRPC |
| FIX-004: Session mgmt | ✅ Complete | File-based persistence + background expiration cleanup |
| FIX-005: gRPC HTTP/2 | ✅ Complete | Uses `golang.org/x/net/http2` |

---

## Overview

This document provides detailed fix proposals for the 5 P0 blocking issues identified in the codebase audit. Each proposal includes:
- Problem description
- Security/functional impact
- Exact code changes required
- Test validation steps
- Rollback plan

---

## FIX-001: WebSocket Key Generation (0.5h)

### Problem

**File:** `internal/probe/websocket.go:248-256`

```go
func generateWebSocketKey() string {
    b := make([]byte, 16)
    // Use simple random bytes (in production, use crypto/rand)
    for i := range b {
        b[i] = byte(i * 7) // Not actually random, but valid base64
    }
    return base64.StdEncoding.EncodeToString(b)
}
```

**Issue:** WebSocket key is **deterministic**, not random. Every connection sends identical key: `AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=` 

**Impact:**
- RFC 6455 violation (Section 4.2.1 requires "randomly chosen" key)
- Potential connection hijacking if attacker can predict key
- Some servers may reject duplicate keys

### Fix

**Change:** Use `crypto/rand` for cryptographically secure random bytes.

```go
// generateWebSocketKey generates a random WebSocket key per RFC 6455
func generateWebSocketKey() string {
    b := make([]byte, 16)
    if _, err := rand.Read(b); err != nil {
        // Fallback to deterministic (should never happen)
        for i := range b {
            b[i] = byte(i * 7)
        }
    }
    return base64.StdEncoding.EncodeToString(b)
}
```

**Import Update:** Add `"crypto/rand"` to imports in `websocket.go`.

### Validation

```bash
# Run WebSocket checker twice, verify different keys
go test -v ./internal/probe -run TestWebSocketKeyRandomness
```

**Expected:** Two consecutive key generations produce different values.

### Rollback

Revert commit — deterministic key doesn't break functionality, just non-compliant.

---

## FIX-002: TLS Verification by Default (2h)

### Problem

**Files:** Multiple

| File | Line | Current Behavior |
|------|------|------------------|
| `probe/websocket.go` | 81 | `InsecureSkipVerify: true` (hardcoded) |
| `probe/smtp.go` | 153 | `InsecureSkipVerify: true` (TODO comment) |
| `probe/grpc.go` | 63 | `InsecureSkipVerify: true` (TODO comment) |
| `probe/tls.go` | 65 | `InsecureSkipVerify: true` (explicit for testing) |

**Issue:** TLS certificate verification disabled by default, allowing MITM attacks.

**Impact:**
- HIGH severity security vulnerability
- Man-in-the-middle attacks possible
- Certificate validity, hostname, chain not verified

### Fix

**Approach:** Enable verification by default, add config option to disable.

#### websocket.go (line 80-84)

```go
// Before:
tlsConfig := &tls.Config{
    InsecureSkipVerify: true,
    ServerName:         u.Hostname(),
}

// After:
tlsConfig := &tls.Config{
    InsecureSkipVerify: cfg.InsecureSkipVerify, // Default: false
    ServerName:         u.Hostname(),
}
```

#### smtp.go (line 152-155)

```go
// Before:
tlsConn := tls.Client(conn, &tls.Config{
    InsecureSkipVerify: true, // TODO: Make configurable
    ServerName:         ehloDomain,
})

// After:
tlsConfig := &tls.Config{
    InsecureSkipVerify: cfg.InsecureSkipVerify, // Default: false
    ServerName:         ehloDomain,
}
tlsConn := tls.Client(conn, tlsConfig)
```

#### grpc.go (line 62-65)

```go
// Before:
tlsConfig := &tls.Config{
    InsecureSkipVerify: true, // TODO: Use CA cert
}

// After:
tlsConfig := &tls.Config{
    InsecureSkipVerify: cfg.InsecureSkipVerify, // Default: false
    ServerName:         host,
}
```

#### core/smtp.go (add config field)

```go
// SMTPConfig struct - add field:
type SMTPConfig struct {
    // ... existing fields ...
    InsecureSkipVerify bool `yaml:"insecure_skip_verify" json:"insecure_skip_verify"`
}
```

Repeat for `GRPCConfig`, `WebSocketConfig`.

### Default Value

Ensure `InsecureSkipVerify` defaults to `false`:

```go
// In config defaults (or zero-value is already false)
func setDefaults(cfg *SMTPConfig) {
    if cfg == nil {
        return
    }
    // InsecureSkipVerify defaults to false (secure)
}
```

### Validation

```bash
# Test against expired cert server - should fail
go test -v ./internal/probe -run TestWebSocketTLSVerification

# Test with self-signed cert + InsecureSkipVerify:true - should pass
go test -v ./internal/probe -run TestWebSocketSelfSigned
```

### Rollback

Revert commit — but this leaves production vulnerable. Not recommended.

---

## FIX-003: Request Size Limits (3h)

### Problem

**Files:** Multiple protocol checkers

HTTP checker limits body to 1MB:
```go
// probe/http.go:128
bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
```

But other protocols have no limits:
- TCP/UDP: Unlimited banner read
- WebSocket: Unlimited frame read
- SMTP/IMAP: Unlimited response read

**Impact:**
- Memory exhaustion DoS
- Malicious servers can send gigabytes of data

### Fix

Add `maxRead` constant and apply across all protocols.

#### probe/checker.go (add constant)

```go
const (
    maxReadSize    = 1024 * 1024       // 1MB default limit
    maxBannerSize  = 64 * 1024         // 64KB for banners
    maxMessageSize = 4 * 1024 * 1024   // 4MB for explicit messages
)
```

#### probe/tcp.go (line 93-99)

```go
// Before:
banner, err := reader.ReadString('\n')
if err != nil && banner == "" {
    // Try reading without delimiter
    buf := make([]byte, 4096)
    n, _ := reader.Read(buf)
    banner = string(buf[:n])
}

// After:
banner, err := reader.ReadString('\n')
if err != nil && banner == "" {
    // Try reading without delimiter (limited to maxBannerSize)
    buf := make([]byte, maxBannerSize)
    n, _ := io.ReadFull(reader, buf[:])
    banner = string(buf[:n])
}
```

#### probe/websocket.go (line 175-179)

```go
// Before:
responseBuf := make([]byte, 4096)
n, err := conn.Read(responseBuf)

// After:
responseBuf := make([]byte, maxMessageSize)
conn.SetReadLimit(maxMessageSize) // Enforce limit
n, err := conn.Read(responseBuf)
```

#### probe/smtp.go, probe/imap.go

Apply similar limiting to all `ReadString`, `Read`, `ReadFull` calls.

### Validation

```bash
# Test with oversized response
go test -v ./internal/probe -run TestSizeLimits

# Verify memory doesn't grow unbounded
go test -bench=BenchmarkLargeResponse ./internal/probe
```

### Rollback

Low risk — removing limits doesn't break functionality, just exposes to DoS.

---

## FIX-004: JWT Token Expiration (2h)

### Problem

**File:** `internal/auth/local.go`

Tokens expire after 24h (line 92), but:
- No expiration validation in `Authenticate()` beyond checking stored expiry
- No JWT tokens actually used — current impl uses random tokens stored in memory
- No token refresh mechanism
- Tokens stored in map (lost on restart)

**Note:** The current implementation doesn't use JWT — it uses opaque tokens stored in a map. The "JWT" concern from the audit is partially inaccurate — the actual issue is **session management**.

### Actual Issues Found

1. Sessions stored in-memory only (lost on restart)
2. No mechanism to rotate/expire tokens proactively
3. No audit log of authentication events

### Fix

#### Option A: Persist Sessions to Storage (Recommended)

```go
// Add to LocalAuthenticator:
store Storage // CobaltDB reference

// Persist token to storage
func (a *LocalAuthenticator) storeToken(token string, sess *session) error {
    key := "auth/session/" + token
    data, _ := json.Marshal(sess)
    return a.store.Put(key, data)
}

// Load from storage on startup
func (a *LocalAuthenticator) loadSessions() error {
    sessions, err := a.store.PrefixScan("auth/session/")
    // ... restore sessions ...
}
```

#### Option B: Switch to Actual JWT

```go
import "github.com/golang-jwt/jwt/v5"

type Claims struct {
    UserID    string `json:"user_id"`
    Workspace string `json:"workspace"`
    jwt.RegisteredClaims
}

func (a *LocalAuthenticator) generateJWT(user *api.User) (string, error) {
    claims := Claims{
        UserID:    user.ID,
        Workspace: user.Workspace,
        RegisteredClaims: jwt.RegisteredClaims{
            ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
            IssuedAt:  jwt.NewNumericDate(time.Now()),
            Issuer:    "anubiswatch",
        },
    }
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    return token.SignedString(a.secretKey)
}
```

**Trade-off:** Option B adds dependency (jwt/v5) vs zero-dep goal.

### Recommendation

Use **Option A** (persist sessions) to maintain zero-dependency goal. Add:
- Session persistence to CobaltDB
- Background goroutine to purge expired sessions
- Audit logging for login/logout events

### Validation

```bash
# Test session survives restart
go test -v ./internal/auth -run TestSessionPersistence

# Test expired session purged
go test -v ./internal/auth -run TestSessionExpiration
```

---

## FIX-005: gRPC HTTP/2 Frame Encoding (8h)

### Problem

**File:** `internal/probe/grpc.go:163-189`

```go
func buildHTTP2HeadersFrame(host, port string, contentLength int) []byte {
    // This is a simplified implementation - real HPACK is complex
    // For production, use proper HPACK encoding
    _ = host
    _ = port
    _ = contentLength

    // Return placeholder frame
    frame := make([]byte, 9)
    frame[3] = 0x01 // HEADERS type
    frame[4] = 0x04 // END_HEADERS flag
    frame[5] = 0x00 // Stream ID = 1
    frame[6] = 0x00
    frame[7] = 0x00
    frame[8] = 0x01

    return frame
}
```

**Issue:** HPACK encoding is **completely skipped**. The frame contains no actual headers — just a 9-byte header with no payload. Real gRPC servers will reject this.

**Impact:**
- gRPC health checks always fail
- Feature is non-functional
- Misleading to users expecting gRPC support

### Options

#### Option A: Implement HPACK Encoding (4-8h)

HPACK (RFC 7541) is complex but implementable for the subset needed:

```go
// Minimal HPACK encoder for HTTP/2 headers
func buildHTTP2HeadersFrame(host, port, path string) []byte {
    // Static table entries (RFC 7541, Appendix A):
    // :method: POST = index 3
    // :scheme: http = index 6
    // :path: /grpc.health.v1.Health/Check = indexed literal
    
    headers := []hpack.HeaderField{
        {Name: ":method", Value: "POST"},
        {Name: ":scheme", Value: "https"},
        {Name: ":path", Value: path},
        {Name: ":authority", Value: host + ":" + port},
        {Name: "content-type", Value: "application/grpc"},
        {Name: "te", Value: "trailers"},
    }
    
    encoded := hpackEncode(headers)
    
    // Build frame with encoded headers
    frame := make([]byte, 9+len(encoded))
    // ... set length, type, flags, stream ID ...
    copy(frame[9:], encoded)
    
    return frame
}
```

**Problem:** Go's `net/http/h2/hpack` is internal. Would need to:
1. Copy HPACK implementation (adds ~500 LOC)
2. Or use `golang.org/x/net/http2/hpack` (adds dependency)

#### Option B: Use golang.org/x/net/http2 (2-4h)

The `golang.org/x/net` dependency already exists. Use its HTTP/2 support:

```go
import "golang.org/x/net/http2"

// Use http2.ConfigureTransport or direct HTTP/2 client
func (c *gRPCChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
    // Use http2.Transport for proper HTTP/2 handling
    transport := &http2.Transport{
        TLSClientConfig: &tls.Config{
            InsecureSkipVerify: cfg.InsecureSkipVerify,
        },
    }
    client := &http.Client{Transport: transport}
    
    // Make standard HTTP request - HTTP/2 handled automatically
    req, _ := http.NewRequest("POST", url, bytes.NewReader(grpcBody))
    req.Header.Set("Content-Type", "application/grpc")
    // ...
    resp, err := client.Do(req)
    // ...
}
```

**Trade-off:** Still zero *new* dependencies (x/net already required).

#### Option C: Mark as Experimental (0.5h)

Document that gRPC support is experimental and requires `google.golang.org/grpc`:

```go
// gRPC Health Checker
// WARNING: This implementation uses raw HTTP/2 frames and may not work
// with all gRPC servers. For production use, consider using the official
// google.golang.org/grpc health check client.
```

### Recommendation

**Use Option B** — leverages existing `golang.org/x/net` dependency, provides working implementation, maintains zero *new* dependency goal.

### Implementation (Option B)

#### internal/probe/grpc.go

```go
import (
    "context"
    "crypto/tls"
    "fmt"
    "io"
    "net"
    "net/http"
    "time"
    
    "golang.org/x/net/http2"
    
    "github.com/AnubisWatch/anubiswatch/internal/core"
)

// ... rest of file ...

func (c *gRPCChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
    cfg := soul.GRPC
    if cfg == nil {
        cfg = &core.GRPCConfig{}
    }

    timeout := soul.Timeout.Duration
    if timeout == 0 {
        timeout = 10 * time.Second
    }

    start := time.Now()

    // Use HTTP/2 transport
    transport := &http2.Transport{
        TLSClientConfig: &tls.Config{
            InsecureSkipVerify: cfg.InsecureSkipVerify, // Default: false after FIX-002
            ServerName:         soul.Target,
        },
        AllowHTTP: true, // Allow h2c (HTTP/2 over cleartext)
    }

    client := &http.Client{
        Transport: transport,
        Timeout:   timeout,
    }

    // Build gRPC health check URL
    scheme := "https"
    if cfg.TLS == false {
        scheme = "http"
    }
    url := fmt.Sprintf("%s://%s/grpc.health.v1.Health/Check", scheme, soul.Target)

    // Build request body (protobuf HealthCheckRequest)
    body := buildGRPCHealthCheckRequest(cfg.Service)

    req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
    if err != nil {
        return failJudgment(soul, fmt.Errorf("failed to create request: %w", err)), nil
    }

    req.Header.Set("Content-Type", "application/grpc")
    req.Header.Set("TE", "trailers")

    resp, err := client.Do(req)
    duration := time.Since(start)

    if err != nil {
        return failJudgment(soul, fmt.Errorf("gRPC request failed: %w", err)), nil
    }
    defer resp.Body.Close()

    // Read response
    respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxReadSize))
    if err != nil {
        return failJudgment(soul, fmt.Errorf("failed to read response: %w", err)), nil
    }

    // Parse gRPC status from trailers or response
    status := resp.Header.Get("Grpc-Status")
    
    judgment := &core.Judgment{
        ID:         core.GenerateID(),
        SoulID:     soul.ID,
        Timestamp:  time.Now().UTC(),
        Duration:   duration,
        StatusCode: resp.StatusCode,
        Status:     core.SoulAlive,
        Message:    fmt.Sprintf("gRPC health check OK in %s", duration.Round(time.Millisecond)),
        Details: &core.JudgmentDetails{
            ServiceStatus: "SERVING",
        },
    }

    // Check gRPC status
    if status != "0" && status != "" {
        judgment.Status = core.SoulDead
        judgment.Message = fmt.Sprintf("gRPC health check failed (status=%s)", status)
        judgment.Details.ServiceStatus = "NOT_SERVING"
    }

    // Performance budget check
    if cfg.Feather.Duration > 0 && duration > cfg.Feather.Duration {
        judgment.Status = core.SoulDegraded
        judgment.Message = fmt.Sprintf("gRPC health check OK in %s (exceeds feather %s)",
            duration.Round(time.Millisecond), cfg.Feather.Duration)
    }

    _ = respBody // Could parse response protobuf for more details

    return judgment, nil
}
```

Remove the unused `buildHTTP2SettingsFrame`, `buildHTTP2HeadersFrame`, `buildHTTP2DataFrame` functions.

### Validation

```bash
# Test against real gRPC server (e.g., grpc_health_probe test server)
go test -v ./internal/probe -run TestGRPCHello

# Test against non-gRPC server - should fail gracefully
go test -v ./internal/probe -run TestGRPCInvalidServer
```

### Rollback

Revert commit — gRPC checks will stop working but other protocols unaffected.

---

## Summary

| Fix | Effort | Risk | Dependencies | Status |
|-----|--------|------|--------------|--------|
| FIX-001: WebSocket key | 0.5h | Low | None | ✅ Complete |
| FIX-002: TLS default | 2h | Medium | Config changes | ✅ Complete |
| FIX-003: Size limits | 3h | Low | None | ✅ Complete |
| FIX-004: Session mgmt | 2h | Low | None (file-based) | ✅ Complete |
| FIX-005: gRPC HTTP/2 | 8h | High | Uses existing x/net | ✅ Complete |
| **Total** | **15.5h** | | | **5/5 Complete** |

---

## Testing Checklist

All P0 fixes completed:

- [x] WebSocket checker uses random key (FIX-001)
- [x] TLS verification enabled by default (FIX-002)
- [x] Size limits prevent DoS (FIX-003)
- [x] gRPC checker uses http2.Transport (FIX-005)
- [x] Sessions persist across restarts (FIX-004)
- [x] Background cleanup expires old sessions (FIX-004)

---

**Document End**
