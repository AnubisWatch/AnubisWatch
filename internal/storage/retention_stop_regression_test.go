package storage

import (
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
)

func auditRetentionStopPanic(rm *RetentionManager) (panicked bool) {
	defer func() { panicked = recover() != nil }()
	rm.Stop()
	return
}
func TestAuditRetentionRepeatedStop(t *testing.T) {
	cases := []struct {
		name       string
		repeat     int
		concurrent bool
	}{
		{"control-once", 0, false}, {"twice", 1, false},
		{"three-times", 2, false}, {"concurrent-repeat", 3, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rm := NewRetentionManager(nil, core.RetentionConfig{Day: "unlimited"}, "", newTestLogger())
			rm.Start()
			stopped := make(chan bool, 1)
			go func() { stopped <- auditRetentionStopPanic(rm) }()
			if <-stopped {
				t.Fatal("first stop panicked")
			}
			// Barrier: the first Stop and the cleanup goroutine finish before duplicate calls.
			<-rm.stoppedCh
			results := make(chan bool, tc.repeat)
			for i := 0; i < tc.repeat; i++ {
				if tc.concurrent {
					go func() { results <- auditRetentionStopPanic(rm) }()
				} else {
					results <- auditRetentionStopPanic(rm)
				}
			}
			panicked := false
			for i := 0; i < tc.repeat; i++ {
				panicked = <-results || panicked
			}
			t.Logf("EXPECTED: panic=false ACTUAL: panic=%v", panicked)
			if panicked {
				t.Error("regression mismatch")
			}
		})
	}
}
