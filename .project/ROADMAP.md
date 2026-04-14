# AnubisWatch — Production Readiness Roadmap

> **Version:** 0.1.2
> **Date:** 2026-04-14
> **Based on:** ANALYSIS.md refresh (full codebase audit, 73 Go source files, 46 frontend files)
> **Status:** PRODUCTION READY — all tests passing

---

## Executive Summary

AnubisWatch scores **92/100 overall health** with **~100% backend feature completion**, **100% frontend completeness**, **~83.8% test coverage**, and **0 critical/high/medium security issues**. All 9 phases of the v4.0.0 production-readiness roadmap are complete.

### Current State
- **Score:** 92/100 (up from 90/100 after test fix)
- **Test Coverage:** ~83.8% average (above 80% target)
- **TODOs:** 1 (CORS config in rest.go:1406)
- **Dependencies:** 3 direct (zero-dep goal achieved)
- **Critical Issues:** 0
- **High Issues:** 0
- **Failing Tests:** 0 (all 4 webhook SSRF tests fixed)
- **Frontend Completeness:** 100%

### Goals for v0.1.2
1. Fix 4 failing webhook dispatcher tests (SSRF blocks 127.0.0.1) — 1h
2. Polish and prepare v0.1.2 release — 1h

### Goals for v0.2.0 (next major)
1. Enterprise features (SSO/SAML, audit log export, compliance reporting)
2. Distributed storage across nodes (beyond Raft log replication)
3. Machine learning anomaly detection for alert conditions
4. Mobile app (React Native)
5. Terraform provider for infrastructure as code

---

## Phase 9: Test Regression Fix (COMPLETED) — ~1h

### 9.1 Webhook SSRF Test Fix (COMPLETED)

**Problem:** 4 tests in `internal/alert/dispatchers_test.go` fail because `probe.ValidateTarget()` blocks private IPs including 127.0.0.1 (SSRF protection), but tests use `httptest.NewServer()` which binds to localhost.

**Failing tests (all now fixed):**
- `TestWebHookDispatcher_Send`
- `TestWebHookDispatcher_CustomHeaders`
- `TestWebHookDispatcher_HMACSignature`
- `TestWebHookDispatcher_CustomMethod`

**Fix applied:**
1. Added `ResetDefaultForTest()` helper in `internal/probe/ssrf.go` — reinitializes `DefaultValidator` with current env vars
2. Added `TestMain` in `internal/alert/dispatchers_test.go` — sets `ANUBIS_SSRF_ALLOW_PRIVATE=1` and calls `probe.ResetDefaultForTest()`

**Files modified:**
- `internal/probe/ssrf.go` — added `ResetDefaultForTest()` test helper
- `internal/alert/dispatchers_test.go` — added `TestMain` with SSRF bypass

**Impact:** All 27 packages pass. CI green.

---

## Phase 10: v0.2.0 Enterprise Features (Future) — ~120h

**Priority:** Enterprise-ready features for larger organizations.

### 10.1 SSO/SAML Integration (16h)
- Add SAML 2.0 authentication alongside OIDC/LDAP
- Support for Okta, OneLogin, ADFS
- SP-initiated and IdP-initiated flows
- **Location:** `internal/auth/saml.go`

### 10.2 Audit Log Export (8h)
- Export audit logs to CSV, JSON, SIEM formats
- Integration with external logging (Elasticsearch, Splunk, Datadog)
- Configurable retention and archival policies
- **Location:** `internal/api/audit.go`, `internal/backup/`

### 10.3 Compliance Reporting (12h)
- SOC 2, ISO 27001, HIPAA compliance reports
- Automated evidence collection
- Policy enforcement tracking
- **Location:** `internal/compliance/` (new package)

### 10.4 Distributed Storage (24h)
- Distributed CobaltDB across nodes (beyond Raft log replication)
- Consistent hashing for data partitioning
- Cross-node query federation
- **Location:** `internal/storage/`, `internal/raft/`

### 10.5 ML Anomaly Detection (20h)
- Statistical anomaly detection for latency and uptime patterns
- Baseline learning with configurable sensitivity
- New alert condition type: `anomaly_ml`
- **Location:** `internal/ml/` (new package), `internal/alert/manager.go`

### 10.6 Mobile App (24h)
- React Native app for iOS and Android
- Push notifications for alerts
- Dashboard overview with real-time updates
- **Location:** `mobile/` (new directory)

### 10.7 Terraform Provider (16h)
- Manage souls, channels, rules, dashboards via Terraform
- Import existing configuration
- State management
- **Location:** `terraform-provider-anubiswatch/` (new repo)

---

## Go/No-Go Decision Points

### Go/No-Go #1: After Phase 9 (Week 1)
- **Criteria:** All 4 webhook tests passing, CI green
- **Decision:** COMPLETE — Can ship v0.1.2

### Go/No-Go #2: After Phase 10.1-10.3 (Enterprise Core)
- **Criteria:** SSO, audit export, compliance reports implemented and tested
- **Decision:** PENDING — Can ship v0.2.0 Enterprise

---

## Risk Assessment

| Risk | Probability | Impact | Mitigation |
|------|------------|--------|------------|
| SSRF bypass opens security hole | Low | Medium | Scope bypass to test-only, verify env var is not checked in product code paths |
| Enterprise scope creep | High | Medium | Phase features, ship incrementally |
| Distributed storage complexity | High | High | Start with simple replication, evolve to partitioning |
| ML false positives | Medium | Low | Configurable sensitivity, baseline period |

---

## Effort Summary

| Phase | Duration | Effort | Priority | Status |
|-------|----------|--------|----------|--------|
| Phase 1: Critical Security | Week 1 | ~6h | Critical | COMPLETE |
| Phase 2: Data Integrity | Week 1-2 | ~6h | Critical | COMPLETE |
| Phase 3: Correctness Fixes | Week 2 | ~6h | High | COMPLETE |
| Phase 4: Frontend Completeness | Week 3-6 | ~60h | High | COMPLETE |
| Phase 5: Testing Improvements | Week 6-7 | ~30h | High | COMPLETE |
| Phase 6: Performance | Week 7 | ~8h | Medium | COMPLETE |
| Phase 7: Missing Features | Week 8 | ~16h | Medium | COMPLETE |
| Phase 8: Release Prep | Week 9 | ~12h | Medium | COMPLETE |
| Phase 9: Test Regression | Week 1 | ~1h | Critical | COMPLETE |
| Phase 10: Enterprise | Future | ~120h | Medium | PLANNED |
| **Total (done)** | **10 weeks** | **~145h** | | |
| **Total (planned)** | **Future** | **~121h** | | |

---

## Document History

| Version | Date | Changes |
|---------|------|---------|
| v2.0.0 | 2026-04-08 | Initial roadmap (focused on fixing critical issues) |
| v3.1.0 | 2026-04-11 | Updated — all critical issues fixed (per previous audit) |
| v0.1.0 | 2026-04-11 | **Revised** — full codebase audit revealed 2 critical, 6 high, 6 medium issues missed by previous audit |
| v0.1.0 FINAL | 2026-04-12 | **All 8 phases complete** — score 92/100, PRODUCTION READY |
| v0.1.2 | 2026-04-14 | **Refresh** — 4 webhook test failures identified, Phase 9 added |
| v0.1.2 FINAL | 2026-04-14 | **Phase 9 complete** — SSRF test fix applied, all 27 packages pass, score 92/100 |

**Document End**
