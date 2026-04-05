# ADR-001: Language Choice - Go 1.26

## Status

Accepted

## Context

AnubisWatch requires a language that provides:
- High performance for concurrent health checks
- Low memory footprint for efficient scaling
- Strong standard library for network programming
- Easy deployment (single binary)
- Good concurrency primitives for probe parallelization

## Decision

We chose **Go 1.26** as the implementation language.

### Rationale

1. **Concurrency Model**: Go's goroutines provide lightweight concurrency for running thousands of simultaneous health checks without thread overhead.

2. **Standard Library**: Rich networking support (HTTP, TLS, DNS, TCP) reduces external dependencies.

3. **Single Binary Deployment**: Static compilation simplifies deployment - no runtime dependencies.

4. **Performance**: Go provides near-C performance for network I/O bound workloads.

5. **Type Safety**: Strong static typing catches errors at compile time.

6. **Testing Support**: Built-in testing framework encourages good test coverage.

## Consequences

### Positive
- Easy horizontal scaling with goroutines
- Simple deployment process
- Good performance characteristics
- Large ecosystem of tools and libraries

### Negative
- Generic programming limited until Go 1.18+
- Error handling can be verbose
- Less expressive than some modern languages

## Alternatives Considered

### Rust
- **Pros**: Memory safety, zero-cost abstractions, excellent performance
- **Cons**: Steeper learning curve, longer compile times, more complex dependency management
- **Rejected**: Development speed prioritized over marginal performance gains

### Python
- **Pros**: Easy to write, rich ecosystem
- **Cons**: GIL limits concurrency, higher memory usage, slower execution
- **Rejected**: Performance requirements not met

### Node.js
- **Pros**: Good async model, large ecosystem
- **Cons**: Single-threaded event loop, JavaScript type system
- **Rejected**: Go provides better CPU utilization for compute-heavy tasks
