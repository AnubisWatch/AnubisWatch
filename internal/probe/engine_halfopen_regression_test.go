package probe

import (
	"testing"
	"time"
)

func auditHalfOpen(t *testing.T, events string, successThreshold int, want string, wantBlocked bool) {
	t.Helper()
	e := newTestEngine(t, EngineOptions{Config: EngineConfig{CircuitBreaker: CircuitBreakerConfig{Enabled: true, FailureThreshold: 3, SuccessThreshold: successThreshold, Timeout: time.Hour}}})
	defer e.Stop()
	id := "half-open"
	for i := 0; i < 3; i++ {
		e.recordFailure(id)
	}
	cb := e.getCircuitBreaker(id)
	cb.mu.Lock()
	cb.lastStateChange = time.Time{}
	cb.mu.Unlock()
	if cb.isOpen(e.Config().CircuitBreaker) {
		t.Fatal("expired open circuit did not permit recovery")
	}
	for _, event := range events {
		if event == 'F' {
			e.recordFailure(id)
		} else {
			e.recordSuccess(id)
		}
	}
	cb.mu.RLock()
	state := cb.state
	cb.mu.RUnlock()
	blocked := cb.isOpen(e.Config().CircuitBreaker)
	t.Logf("events=%s EXPECTED: state=%s blocked=%v ACTUAL: state=%s blocked=%v", events, want, wantBlocked, state, blocked)
	if state != want || blocked != wantBlocked {
		t.Error("regression mismatch")
	}
}
func TestAuditFailedHalfOpenRecovery(t *testing.T) {
	t.Run("successful-control", func(t *testing.T) { auditHalfOpen(t, "S", 1, "closed", false) })
	t.Run("failed-recovery", func(t *testing.T) { auditHalfOpen(t, "F", 1, "open", true) })
	t.Run("failure-after-partial-success", func(t *testing.T) { auditHalfOpen(t, "SF", 2, "open", true) })
	t.Run("success-threshold-preserved", func(t *testing.T) { auditHalfOpen(t, "S", 2, "half-open", false) })
	t.Run("closed-threshold-preserved", func(t *testing.T) { auditHalfOpen(t, "SF", 1, "closed", false) })
}
