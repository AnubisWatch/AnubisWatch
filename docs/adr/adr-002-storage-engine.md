# ADR-002: Storage Engine - CobaltDB (Custom Embedded)

## Status

Accepted

## Context

AnubisWatch needs persistent storage for:
- Soul (monitor) configurations
- Judgment (check result) history
- Alert channels and rules
- Incident tracking
- Time-series metrics

Requirements:
- Embedded (no external database dependency)
- ACID guarantees for configuration changes
- Efficient time-series storage for judgments
- Simple backup and recovery
- Low operational overhead

## Decision

We built **CobaltDB**, a custom embedded key-value storage engine using Go's standard library.

### Design Choices

1. **WAL-backed persistence**: `wal.log` stores checksummed writes and atomic
   compound batches. Recovery replays live values into an in-memory B+Tree;
   background checkpointing bounds superseded log growth.

2. **In-memory data and indexes**: retained values and resource/workspace lookup
   indexes consume memory. Capacity and startup time grow with retained data.

3. **Judgment identity**: new keys are
   `{workspace}/judgments/{soul}/{timestamp}/{judgmentID}` so results sharing a
   clock instant coexist. Legacy timestamp-only records remain readable.

4. **Namespace Isolation**: Workspace-based key prefixing for multi-tenancy.

## Consequences

### Positive
- Zero external dependencies
- Simple deployment (single binary)
- Cold backup of the complete stopped data directory
- Workspace isolation built-in
- No database connection pooling needed

### Negative
- Limited query capabilities (no SQL)
- Memory usage grows with data size
- No built-in replication (handled by Raft layer)
- Retention and checkpoint policies require workload validation

## Alternatives Considered

### SQLite
- **Pros**: Mature, SQL queries, good tooling
- **Cons**: CGO dependencies, connection management, more complex deployment
- **Rejected**: Deployment simplicity prioritized

### BadgerDB
- **Pros**: Fast key-value, LSM tree, built-in TTL
- **Cons**: External dependency, larger binary
- **Rejected**: Zero-dependency policy

### PostgreSQL
- **Pros**: Full SQL, replication, mature
- **Cons**: External service, operational complexity, overkill for embedded use
- **Rejected**: Embedded requirement
