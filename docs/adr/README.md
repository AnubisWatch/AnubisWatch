# Architecture Decision Records (ADRs)

This directory contains Architecture Decision Records (ADRs) for AnubisWatch. Each ADR documents a significant architectural decision, its context, and consequences.

## ADR Index

| Number | Title | Status | Date |
|--------|-------|--------|------|
| [ADR-001](./adr-001-language-choice.md) | Language Choice: Go 1.26 | Accepted | 2026-01-15 |
| [ADR-002](./adr-002-storage-engine.md) | Storage Engine: CobaltDB (Custom Embedded) | Accepted | 2026-01-16 |
| [ADR-003](./adr-003-consensus-algorithm.md) | Consensus Algorithm: Raft | Accepted | 2026-01-17 |
| [ADR-004](./adr-004-probe-architecture.md) | Probe Architecture: Per-Soul Circuit Breakers | Accepted | 2026-02-01 |
| [ADR-005](./adr-005-alert-deduplication.md) | Alert Deduplication Strategy | Accepted | 2026-02-05 |
| [ADR-006](./adr-006-multi-tenancy.md) | Multi-Tenancy: Workspace-Based Isolation | Accepted | 2026-02-10 |
| [ADR-007](./adr-007-mcp-integration.md) | MCP Protocol for AI Integration | Accepted | 2026-03-01 |
| [ADR-008](./adr-008-zero-external-deps.md) | Zero External Dependencies Policy | Accepted | 2026-01-20 |

---

## ADR Template

```markdown
# ADR-XXX: Title

## Status

[Proposed | Accepted | Deprecated | Superseded]

## Context

What is the issue or decision we face? What are the forces at play?

## Decision

What is the change that we're proposing or have done?

## Consequences

What becomes easier or more difficult? What are the trade-offs?

## Alternatives Considered

What other options did we consider and why were they rejected?
```
