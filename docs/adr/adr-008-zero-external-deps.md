# ADR-008: Embedded Runtime and Dependency Policy

## Status

Accepted

## Context

Go projects often accumulate many dependencies:
- Version conflicts between libraries
- Security vulnerabilities in transitive deps
- Increased binary size
- Supply chain attack surface
- Complex license compliance

## Decision

AnubisWatch requires no external database or coordination service for its core
runtime. It uses reviewed Go modules for protocol, identity, cryptography,
telemetry and retry behavior, and frontend build dependencies. `go.mod`,
`go.sum` and `web/pnpm-lock.yaml` define the actual dependency inventory.

### What We Use

| Component | Implementation |
|-----------|----------------|
| HTTP Server | `net/http` (stdlib) |
| JSON | `encoding/json` (stdlib) |
| TLS | `crypto/tls` (stdlib) |
| Storage | Custom CobaltDB |
| Raft Consensus | Custom implementation |
| Time-series | Custom downsampling |
| Templates | `text/template` (stdlib) |

### Allowed Exceptions

Prefer the standard library; review external dependencies for:
1. **Protocol-specific libraries** where stdlib is insufficient (e.g., HPACK for HTTP/2)
2. **Well-established security libraries** (cryptographic primitives if needed)
3. **Development tools** (linters, formatters) - not shipped in binary

### Implementation Strategy

```go
// Instead of: import "github.com/pkg/errors"
// We use: Go 1.13+ error wrapping
err := doSomething()
if err != nil {
    return fmt.Errorf("operation failed: %w", err)
}

// Instead of: import "github.com/gorilla/mux"
// We use: net/http with custom router
type Router struct {
    routes map[string]map[string]Handler
}
```

## Consequences

### Positive
- Minimal attack surface
- Explicit dependency inventory and blocking vulnerability checks
- Full control over all code paths
- Easier security audits
- Smaller binary size
- Dependency licenses and advisories still require review
- Faster builds (fewer dependencies)

### Negative
- More code to maintain
- Reinventing some wheels
- Missing out on some ecosystem improvements
- Development time for complex features (Raft)

## Alternatives Considered

### Use Popular Libraries
- **Pros**: Faster development, community support
- **Cons**: Dependency risk, less control
- **Rejected**: Security-first approach

### Hybrid Approach
- **Pros**: Balance of speed and control
- **Cons**: Still has dependency risks
- **Rejected**: Clear policy is easier to enforce

### Accept All Dependencies
- **Pros**: Fastest development
- **Cons**: Vulnerable to supply chain attacks
- **Rejected**: Security requirement for monitoring tool

## Enforcement

1. **go mod tidy** regularly to remove unused deps
2. **Review go.sum** in PRs for new dependencies
3. **Prefer stdlib** in code reviews
4. **Document exceptions** when absolutely necessary
