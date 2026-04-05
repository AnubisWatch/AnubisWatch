# ADR-005: Alert Deduplication Strategy

## Status

Accepted

## Context

Alert systems face the problem of notification flooding:
- A failing service triggers checks every 30-60 seconds
- Each failure generates an alert
- Without deduplication, operators receive hundreds of identical alerts
- Alert fatigue causes important notifications to be ignored

## Decision

We implemented **multi-layer deduplication** with configurable windows.

### Deduplication Layers

1. **Rule-Level Deduplication**:
   - Key: `{rule_id}:{soul_id}:{status}`
   - Window: Rule cooldown period (default: 5 minutes)
   - Prevents same rule firing for same soul/status repeatedly

2. **Channel-Level Rate Limiting**:
   - Key: `{channel_id}:{soul_id}:{grouping_key}`
   - Window: Configurable (default: 1 hour)
   - Limit: Max alerts per window (default: 10)
   - Prevents notification channel flooding

3. **Status Change Bypass**:
   - Status changes always notify (dead→alive, alive→dead)
   - Ensures recovery notifications aren't suppressed

### Implementation

```go
func (m *Manager) isDuplicate(rule *AlertRule, event *AlertEvent) bool {
    key := fmt.Sprintf("dedup:%s:%s:%s", rule.ID, event.SoulID, event.Status)
    
    entry, exists := history.Entries[key]
    if !exists {
        return false // First alert, allow through
    }
    
    // Check if window expired
    if time.Since(entry.LastSent) >= dedupWindow {
        return false // Window expired, allow through
    }
    
    // Status changed? Allow through
    if entry.SoulStatus != event.Status {
        return false
    }
    
    return true // Duplicate within window
}
```

## Consequences

### Positive
- Prevents alert storms
- Configurable per channel
- Recovery notifications always sent
- Grouping key allows flexible dedup (by soul, by severity, etc.)

### Negative
- State must be tracked in memory
- Dedup state lost on restart (acceptable trade-off)
- Complexity in testing edge cases

## Alternatives Considered

### Time-Based Suppression Only
- **Pros**: Simpler implementation
- **Cons**: No per-channel control, rigid
- **Rejected**: Flexibility requirement

### External Rate Limiter (Redis)
- **Pros**: Distributed, persistent
- **Cons**: External dependency, overkill
- **Rejected**: Embedded requirement

### No Deduplication
- **Pros**: Simplest, guarantees delivery
- **Cons**: Alert fatigue, notification storms
- **Rejected**: Operator experience requirement
