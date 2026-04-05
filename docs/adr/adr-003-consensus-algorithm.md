# ADR-003: Consensus Algorithm - Raft

## Status

Accepted

## Context

For clustered deployments (Necropolis), AnubisWatch needs:
- Consistent configuration across nodes
- Leader election for coordinated writes
- Fault tolerance (survive node failures)
- Log replication for state machine consistency

## Decision

We implemented the **Raft consensus algorithm** for cluster coordination.

### Architecture

```
        ┌───────────┐
        │  Pharaoh  │  ← Raft leader (handles writes)
        └─────┬─────┘
              │
    ┌─────────┼─────────┐
    │         │         │
┌───┴───┐ ┌───┴───┐ ┌───┴───┐
│Jackal1│ │Jackal2│ │Jackal3│  ← Followers (can serve reads)
└───────┘ └───────┘ └───────┘
```

### Implementation Details

1. **Custom Raft Implementation**: Built from scratch using Raft paper for full control.

2. **Terminology Mapping**:
   - Leader → Pharaoh
   - Followers → Jackals
   - Cluster → Necropolis

3. **Log Entries**: Soul configurations, alert rules replicated via Raft log.

4. **Snapshots**: Periodic snapshots for log compaction.

5. **Pre-Vote Extension**: Prevents disruptive leaders.

## Consequences

### Positive
- Strong consistency guarantees
- Automatic leader election
- Fault tolerance (N/2-1 failures)
- Linearizable reads and writes

### Negative
- Increased complexity
- Write latency (consensus overhead)
- Minimum 3 nodes for HA
- Network partition handling complexity

## Alternatives Considered

### HashiCorp Raft
- **Pros**: Battle-tested, production use
- **Cons**: External dependency, less educational
- **Rejected**: Learning opportunity and full control

### etcd
- **Pros**: Mature, widely used
- **Cons**: External service, operational overhead
- **Rejected**: Embedded requirement

### Single Node
- **Pros**: Simple, fast
- **Cons**: No fault tolerance, single point of failure
- **Rejected**: HA requirement
