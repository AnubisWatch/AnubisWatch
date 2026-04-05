# ADR-004: Probe Architecture - Per-Soul Circuit Breakers

## Status

Accepted

## Context

The probe engine must handle:
- Thousands of concurrent health checks
- Failing services without cascading failures
- Configurable probe distribution
- Region-aware checking
- Efficient resource utilization

## Decision

We implemented **per-soul circuit breakers** with concurrency limiting.

### Architecture

```
┌─────────────────────────────────────────────────────┐
│                  Probe Engine                        │
├─────────────────────────────────────────────────────┤
│  ┌───────────┐  ┌───────────┐  ┌───────────┐       │
│  │ Circuit   │  │ Circuit   │  │ Circuit   │       │
│  │ Breaker 1 │  │ Breaker 2 │  │ Breaker N │       │
│  └─────┬─────┘  └─────┬─────┘  └─────┬─────┘       │
│        │              │              │              │
│  ┌─────┴─────┐  ┌─────┴─────┐  ┌─────┴─────┐       │
│  │  Soul 1   │  │  Soul 2   │  │  Soul N   │       │
│  └───────────┘  └───────────┘  └───────────┘       │
├─────────────────────────────────────────────────────┤
│              Concurrency Semaphore                   │
│              (max 100 concurrent checks)             │
└─────────────────────────────────────────────────────┘
```

### Circuit Breaker States

1. **Closed**: Normal operation, checks execute
2. **Open**: Service failing, checks paused (timeout period)
3. **Half-Open**: Testing recovery, single check allowed

### Configuration

```go
type CircuitBreakerConfig struct {
    Enabled         bool
    FailureThreshold int  // failures before opening
    SuccessThreshold int  // successes before closing
    Timeout         time.Duration
}
```

## Consequences

### Positive
- Prevents probe overload on failing services
- Reduces unnecessary network traffic
- Configurable per deployment needs
- Per-soul isolation (one failing service doesn't affect others)

### Negative
- Additional state to track
- Memory overhead for circuit breaker map
- Complexity in state transitions

## Alternatives Considered

### Global Rate Limiting Only
- **Pros**: Simpler implementation
- **Cons**: No per-service backoff, wasted probes on dead services
- **Rejected**: Insufficient for large-scale deployments

### No Circuit Breaking
- **Pros**: Simplest implementation
- **Cons**: Wasted resources, potential network storms
- **Rejected**: Production reliability requirement

### External Circuit Breaker Library
- **Pros**: Battle-tested implementation
- **Cons**: External dependency, less control
- **Rejected**: Simple enough to implement inline
