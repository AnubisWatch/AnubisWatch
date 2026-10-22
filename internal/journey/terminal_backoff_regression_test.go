package journey

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

func TestRetryBackoffStopsAfterFinalAttempt(t *testing.T) {
	operationErr := errors.New("fixture operation failed")
	cases := []struct {
		name                           string
		attempts, succeedAt, wantCalls int
		deadline, elapsed              time.Duration
		wantErr                        error
	}{
		{"first_success_control", 3, 1, 1, 0, 0, nil},
		{"second_success_control", 3, 2, 2, 0, 100 * time.Millisecond, nil},
		{"last_success_control", 3, 3, 3, 0, 300 * time.Millisecond, nil},
		{"exhausted", 3, 0, 3, 0, 300 * time.Millisecond, operationErr},
		{"exhausted_before_deadline", 3, 0, 3, 500 * time.Millisecond, 300 * time.Millisecond, operationErr},
		{"one_attempt_boundary", 1, 0, 1, 0, 0, operationErr},
		{"deadline_during_real_backoff_control", 3, 0, 1, 50 * time.Millisecond, 50 * time.Millisecond, context.DeadlineExceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				if tc.deadline > 0 {
					cancel()
					ctx, cancel = context.WithTimeout(context.Background(), tc.deadline)
				}
				defer cancel()
				calls := 0
				start := time.Now()
				// Call the real retry helper; only the external operation is simulated.
				err := retryWithBackoff(ctx, tc.attempts, 100*time.Millisecond, func() error {
					calls++
					if calls == tc.succeedAt {
						return nil
					}
					return operationErr
				})
				if calls != tc.wantCalls {
					t.Errorf("calls=%d, want %d", calls, tc.wantCalls)
				}
				if got := time.Since(start); got != tc.elapsed {
					t.Errorf("FAIL: return took %s, want %s; no backoff is needed after the last attempt", got, tc.elapsed)
				}
				if tc.wantErr == nil {
					if err != nil {
						t.Errorf("unexpected failure: %v", err)
					}
				} else if !errors.Is(err, tc.wantErr) {
					t.Errorf("FAIL: final error=%v, want cause %v", err, tc.wantErr)
				}
			})
		})
	}
}
