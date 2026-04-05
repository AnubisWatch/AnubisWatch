# ADR-006: Multi-Tenancy - Workspace-Based Isolation

## Status

Accepted

## Context

AnubisWatch must support multiple tenants (organizations/teams):
- Data isolation between tenants
- Role-based access control (RBAC)
- Resource quotas per tenant
- Independent configuration per tenant

## Decision

We implemented **workspace-based multi-tenancy** with namespace isolation.

### Architecture

```
┌──────────────────────────────────────────────────────┐
│                   API Layer                           │
│  Authentication → Extract Workspace → Enforce RBAC   │
├──────────────────────────────────────────────────────┤
│                 Storage Layer                         │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐     │
│  │ Workspace A│  │ Workspace B│  │ Workspace C│     │
│  │ souls/     │  │ souls/     │  │ souls/     │     │
│  │ judgments/ │  │ judgments/ │  │ judgments/ │     │
│  │ channels/  │  │ channels/  │  │ channels/  │     │
│  └────────────┘  └────────────┘  └────────────┘     │
└──────────────────────────────────────────────────────┘
```

### Namespace Key Pattern

```go
func (w *Workspace) NamespaceKey(key string) string {
    if w == nil || w.ID == "" {
        return key
    }
    return w.ID + "/" + key
}
// Result: "workspace_abc123/souls/soul_xyz789"
```

### RBAC Roles

| Role | Permissions |
|------|-------------|
| Owner | Full access (`*`) |
| Admin | souls:*, channels:*, rules:*, members:* |
| Editor | souls:*, channels:read, rules:read |
| Viewer | souls:read, judgments:read, channels:read |
| API | souls:*, judgments:read, api:* |

### Quota System

```go
type QuotaConfig struct {
    MaxSouls         int
    MaxChannels      int
    MaxRules         int
    MaxMembers       int
    MaxChecksPerHour int
    MaxStorageBytes  int64
}
```

## Consequences

### Positive
- Strong data isolation via namespace prefixing
- Flexible RBAC for different user types
- Quota enforcement prevents resource exhaustion
- Workspace-level statistics and billing potential

### Negative
- All queries must include workspace context
- Cross-workspace operations require special handling
- Additional validation for workspace switching

## Alternatives Considered

### Database-per-Tenant
- **Pros**: Strongest isolation, easy backup per tenant
- **Cons**: Operational complexity, resource inefficient
- **Rejected**: Overkill for SMB market

### Schema-per-Tenant
- **Pros**: Good isolation, shared infrastructure
- **Cons**: Schema migrations complex, doesn't apply to embedded DB
- **Rejected**: CobaltDB doesn't support schemas

### No Multi-Tenancy
- **Pros**: Simpler implementation
- **Cons**: Single tenant only, limits market
- **Rejected**: SaaS requirement
