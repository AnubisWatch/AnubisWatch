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

1. **File-Based Storage**: Data stored in JSON files under structured directories:
   ```
   data/
   ├── default/
   │   ├── souls/{id}.json
   │   ├── judgments/{soul_id}/{timestamp}.json
   │   ├── channels/{id}.json
   │   └── rules/{id}.json
   ```

2. **In-Memory Index**: Fast lookups via in-memory maps with file-backed persistence.

3. **Append-Only Judgments**: Check results are append-only for audit trail.

4. **Namespace Isolation**: Workspace-based key prefixing for multi-tenancy.

## Consequences

### Positive
- Zero external dependencies
- Simple deployment (single binary)
- Easy backup (copy data directory)
- Workspace isolation built-in
- No database connection pooling needed

### Negative
- Limited query capabilities (no SQL)
- Memory usage grows with data size
- No built-in replication (handled by Raft layer)
- Manual compaction needed for old judgments

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
