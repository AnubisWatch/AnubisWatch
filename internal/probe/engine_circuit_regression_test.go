package probe

import "testing"

func TestAuditCircuitFailureReset(t *testing.T) {
	engine := newTestEngine(t, EngineOptions{Config: EngineConfig{CircuitBreaker: CircuitBreakerConfig{Enabled: true, FailureThreshold: 3, SuccessThreshold: 2}}})
	cases := []struct {
		name     string
		events   string
		want     string
		failures int
	}{
		{"control-consecutive", "FFF", "open", 3},
		{"success-breaks-streak", "FFSFF", "closed", 2},
		{"empty", "", "closed", 0}, {"success-without-failure", "SS", "closed", 0}, {"threshold-after-reset", "FFSFFF", "open", 3}, {"repeated-recovery", "FSFSFS", "closed", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, event := range tc.events {
				if event == 'F' {
					engine.recordFailure(tc.name)
				} else {
					engine.recordSuccess(tc.name)
				}
			}
			cb := engine.getCircuitBreaker(tc.name)
			cb.mu.RLock()
			state, failures := cb.state, cb.failures
			cb.mu.RUnlock()
			t.Logf("EXPECTED: state=%s failures=%d ACTUAL: state=%s failures=%d", tc.want, tc.failures, state, failures)
			if state != tc.want || failures != tc.failures {
				t.Error("circuit failure streak mismatch")
			}
		})
	}

}
