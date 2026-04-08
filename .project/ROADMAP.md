# AnubisWatch — Production Readiness Roadmap

> **Version:** 2.0.0  
> **Date:** 2026-04-08  
> **Based on:** ANALYSIS.md v2.0.0 findings  
> **Time Horizon:** 8 weeks to production readiness  
> **Current Score:** 60/100  
> **Target Score:** 85/100  

---

## Executive Summary

This roadmap addresses the critical gaps identified in the comprehensive codebase analysis. The current production readiness score of **60/100** can be improved to **85/100** within 8 weeks of focused development, assuming immediate attention to the critical race conditions and error handling issues discovered.

**Critical Path:** Fix race conditions and mutex contention issues BEFORE any production deployment.

---

## Phase 1: Critical Fixes (Week 1-2) — START HERE

**Goal:** Address race conditions, mutex contention, and error handling gaps that could cause production outages.

**Risk Level:** CRITICAL — These issues can cause data corruption, deadlocks, or system unavailability.

### Week 1: Concurrency & Race Condition Fixes

| Task | ID | Priority | Effort | Status |
|------|-----|----------|--------|--------|
| Fix circuit breaker race condition | TD-001 | P0 | 4h | 🔴 Critical |
| Fix alert manager mutex contention | TD-002 | P0 | 8h | 🔴 Critical |
| Add HTTP transport connection pooling | TD-003 | P0 | 4h | 🔴 Critical |
| Fix JSON path parsing | TD-004 | P0 | 8h | 🔴 Critical |
| Propagate storage errors to callers | TD-006 | P0 | 16h | 🔴 Critical |

**Detailed Fixes Required:**

#### TD-001: Circuit Breaker Race Condition
**Location:** `internal/probe/engine.go:534-544`

```go
// CURRENT (BROKEN):
func (cb *circuitBreaker) isOpen(cfg CircuitBreakerConfig) bool {
    cb.mu.RLock()
    defer cb.mu.RUnlock()
    // ... state checks ...
    if time.Since(cb.lastStateChange) >= cfg.Timeout {
        cb.mu.RUnlock()  // ❌ WRONG: releasing lock
        cb.mu.Lock()     // ❌ WRONG: re-acquiring without guarantee
        // ... state change ...
        cb.mu.Unlock()
        cb.mu.RLock()    // ❌ WRONG: inconsistent locking
    }
}

// FIX: Use proper atomic operations or channel-based state machine
```

**Fix Strategy:** Replace mutex-based state machine with atomic operations or channels.

#### TD-002: Alert Manager Mutex Contention
**Location:** `internal/probe/engine.go:336-348`

```go
// CURRENT (BROKEN):
func (m *Manager) dispatch(event *AlertEvent) {
    m.mu.RLock()
    channels := // ... copy channels ...
    m.mu.RUnlock()
    
    for _, channel := range channels {
        // ❌ PROBLEM: HTTP call under lock
        err := m.sendToChannel(ctx, event, channel)  // This can block for seconds!
    }
}

// FIX: Queue events, dispatch outside of lock
```

**Fix Strategy:** Implement event queue, process dispatch asynchronously.

#### TD-003: HTTP Transport Per Check
**Location:** `internal/probe/http.go:87-95`

```go
// CURRENT (INEFFICIENT):
func (c *HTTPChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
    // ❌ PROBLEM: New transport for every check
    transport := &http.Transport{
        TLSClientConfig: &tls.Config{...},
    }
    client := &http.Client{Transport: transport, ...}
}

// FIX: Cache and reuse transports per soul configuration
```

### Week 2: Error Handling & Integration Tests

| Task | ID | Priority | Effort | Status |
|------|-----|----------|--------|--------|
| Add storage error propagation | TD-006 | P0 | 16h | 🔴 Critical |
| Implement Raft membership changes | TD-005 | P0 | 40h | 🔴 Critical |
| Add chaos tests for cluster failures | | P1 | 16h | 🟡 High |
| Set up CI with race detector | | P1 | 4h | 🟡 High |

**Definition of Done:**
- [ ] All P0 fixes merged and tested
- [ ] `go test -race` passes with no warnings
- [ ] Chaos tests verify cluster survives node failures
- [ ] Error handling audit complete (all errors propagated or intentionally ignored with comments)

---

## Phase 2: Testing Foundation (Week 3-4)

**Goal:** Achieve 80% test coverage and add comprehensive integration tests.

### Week 3: Unit Test Coverage

| Component | Current Coverage | Target | Effort |
|-----------|-----------------|--------|--------|
| Protocol Checkers | ~70% | 85% | 16h |
| Storage Engine | ~60% | 80% | 12h |
| Alert Dispatchers | ~65% | 80% | 8h |
| Raft Consensus | ~50% | 75% | 20h |
| API Handlers | ~55% | 80% | 12h |
| Journey Executor | ~60% | 80% | 8h |

**Priority Test Additions:**

1. **Raft Tests:**
   - Leader election with network partition
   - Log replication under load
   - Snapshot transfer correctness
   - Membership change safety

2. **Storage Tests:**
   - WAL corruption recovery
   - Concurrent read/write safety
   - Retention policy execution
   - Time-series query performance

3. **Protocol Tests:**
   - HTTP with all assertion types
   - TCP with banner matching
   - DNS with all record types
   - TLS with certificate validation

### Week 4: Integration & Chaos Tests

| Task | Priority | Effort |
|------|----------|--------|
| 3-node cluster integration test | P1 | 12h |
| Network partition simulation | P1 | 8h |
| Storage failure injection | P1 | 8h |
| Alert dispatcher end-to-end test | P1 | 8h |
| Load test: 1000 souls | P1 | 8h |

**Definition of Done:**
- [ ] Overall coverage >80%
- [ ] Integration tests run in CI
- [ ] Load test validates 1000 souls / node
- [ ] Chaos tests pass (random node kills)

---

## Phase 3: Production Hardening (Week 5-6)

**Goal:** Add production-grade features for reliability, observability, and operations.

### Week 5: Reliability & Observability

| Task | Priority | Effort |
|------|----------|--------|
| Add distributed tracing (OpenTelemetry) | P1 | 16h |
| Implement health check endpoints | P1 | 4h |
| Add Prometheus metrics for all subsystems | P1 | 8h |
| Implement graceful shutdown handling | P1 | 4h |
| Add request logging middleware | P2 | 4h |

### Week 6: Operations & Security

| Task | Priority | Effort |
|------|----------|--------|
| Implement backup/restore procedures | P1 | 8h |
| Add rate limiting (per-IP, per-user) | P1 | 8h |
| Implement API authentication hardening | P1 | 8h |
| Add input validation on all endpoints | P1 | 8h |
| Create operational runbooks | P2 | 8h |

**Definition of Done:**
- [ ] Health endpoints return accurate status
- [ ] Metrics exposed for Prometheus scraping
- [ ] Rate limiting prevents abuse
- [ ] All user inputs validated
- [ ] Runbooks cover common failures

---

## Phase 4: Feature Completion (Week 7-8)

**Goal:** Complete missing specification features and polish for release.

### Week 7: Missing Features

| Feature | Spec Status | Implementation | Effort |
|---------|-------------|----------------|--------|
| gRPC API | Required | Missing | 40h |
| PWA Service Worker | Required | Missing | 16h |
| Multi-tenant quotas | Required | Missing | 8h |
| Dashboard custom charts | Nice-to-have | Partial | 24h |

### Week 8: Documentation & Release Prep

| Task | Priority | Effort |
|------|----------|--------|
| API documentation (OpenAPI) | P1 | 16h |
| Deployment guide | P1 | 8h |
| Troubleshooting guide | P1 | 8h |
| Architecture decision records | P2 | 8h |
| Release notes | P1 | 4h |
| Security audit | P0 | 16h |

**Definition of Done:**
- [ ] All P0/P1 features complete
- [ ] Documentation complete
- [ ] Security audit passed
- [ ] Release candidate tagged

---

## Effort Summary

### By Phase

| Phase | Duration | Total Effort | Critical | High | Medium |
|-------|----------|--------------|----------|------|--------|
| Phase 1: Critical Fixes | 2 weeks | 80h | 40h | 20h | 20h |
| Phase 2: Testing | 2 weeks | 84h | 0h | 84h | 0h |
| Phase 3: Hardening | 2 weeks | 68h | 0h | 44h | 24h |
| Phase 4: Completion | 2 weeks | 140h | 16h | 40h | 84h |
| **Total** | **8 weeks** | **372h** | **56h** | **188h** | **128h** |

### Resource Requirements

| Role | FTE | Duration |
|------|-----|----------|
| Senior Go Engineer | 1.0 | 8 weeks |
| QA/Test Engineer | 0.5 | 4 weeks (Week 3-6) |
| DevOps Engineer | 0.25 | 4 weeks (Week 5-8) |
| Technical Writer | 0.25 | 2 weeks (Week 7-8) |

**Total:** ~12 person-weeks of focused development

---

## Risk Assessment

### Critical Risks

| Risk | Impact | Likelihood | Mitigation |
|------|--------|------------|------------|
| Race conditions cause data corruption | Critical | High | Fix TD-001/TD-002 immediately, run race detector |
| Raft consensus fails in production | Critical | Medium | Extensive chaos testing, consider HashiCorp Raft |
| Test coverage delays release | High | High | Prioritize critical path tests first |
| Performance issues at scale | High | Medium | Load test early and often |

### Medium Risks

| Risk | Impact | Likelihood | Mitigation |
|------|--------|------------|------------|
| gRPC API takes longer than estimated | Medium | High | Can defer to v1.1 |
| Documentation delays | Low | Low | Parallel track with development |
| Security audit finds issues | Medium | Medium | Buffer time in Week 8 |

---

## Success Metrics

### Week 2 Checkpoint

- [ ] All race conditions fixed
- [ ] `go test -race` passes
- [ ] Critical error handling implemented
- [ ] Production readiness score >65/100

### Week 4 Checkpoint

- [ ] Test coverage >80%
- [ ] Integration tests passing
- [ ] Chaos tests verify resilience
- [ ] Production readiness score >75/100

### Week 8 Completion

- [ ] All P0 features implemented
- [ ] Security audit passed
- [ ] Documentation complete
- [ ] Production readiness score >85/100

---

## Post-Roadmap Considerations

### v1.1 Enhancements (After Production)

- [ ] gRPC API full implementation
- [ ] Advanced anomaly detection
- [ ] Machine learning baselines
- [ ] Mobile app (React Native)
- [ ] Grafana plugin

### v2.0 Considerations

- [ ] Evaluate HashiCorp Raft vs custom implementation
- [ ] Distributed tracing integration
- [ ] Multi-region probe distribution
- [ ] AI-powered incident correlation

---

## Technical Debt Tracking

| Debt ID | Phase | Week | Status |
|---------|-------|------|--------|
| TD-001 (Circuit breaker race) | Phase 1 | Week 1 | 🔴 Critical |
| TD-002 (Alert mutex) | Phase 1 | Week 1 | 🔴 Critical |
| TD-003 (HTTP transport) | Phase 1 | Week 1 | 🔴 Critical |
| TD-004 (JSON path) | Phase 1 | Week 1 | 🔴 Critical |
| TD-005 (Raft membership) | Phase 1 | Week 2 | 🔴 Critical |
| TD-006 (Error propagation) | Phase 1 | Week 2 | 🔴 Critical |
| TD-007 (B+Tree disk) | Backlog | v1.1 | 🟡 Medium |
| TD-008 (Query cache) | Backlog | v1.1 | 🟡 Medium |
| TD-009 (Test coverage) | Phase 2 | Week 3-4 | 🟡 Medium |
| TD-010 (Integration tests) | Phase 2 | Week 4 | 🟡 Medium |
| TD-011 (Chaos tests) | Phase 2 | Week 4 | 🟡 Medium |
| TD-012 (PWA) | Phase 4 | Week 7 | 🟢 Low |

---

## Go/No-Go Decision Points

### Go/No-Go #1: End of Week 2

**Criteria:**
- [ ] All race conditions fixed and verified
- [ ] No data corruption in chaos tests
- [ ] Error handling audit complete

**Decision:** If any race conditions remain, **NO-GO** for production.

### Go/No-Go #2: End of Week 4

**Criteria:**
- [ ] Test coverage >80%
- [ ] All integration tests passing
- [ ] Load test validates performance targets

**Decision:** If coverage <80% or tests failing, **NO-GO** for production.

### Go/No-Go #3: End of Week 8

**Criteria:**
- [ ] Security audit passed
- [ ] Documentation complete
- [ ] Production readiness score >85/100

**Decision:** Final production release decision.

---

**Document Version:** 2.0.0  
**Last Updated:** 2026-04-08  
**Next Review:** End of Week 2 (Go/No-Go #1)
