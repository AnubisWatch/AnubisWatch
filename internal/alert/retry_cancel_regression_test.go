package alert

import (
	"context"
	"errors"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
)

type auditCancelDispatcher struct {
	calls  int
	cancel context.CancelFunc
}

func (*auditCancelDispatcher) Validate(map[string]any) error { return nil }
func (d *auditCancelDispatcher) Send(context.Context, *core.AlertEvent, *core.AlertChannel) error {
	d.calls++
	if d.cancel != nil {
		d.cancel()
		return errors.New("local failure")
	}
	return nil
}
func auditRetryCancellation(t *testing.T, strategy string, canceled, cancelInside bool, want int) {
	t.Helper()
	m := NewManager(nil, newTestLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &auditCancelDispatcher{}
	if cancelInside {
		d.cancel = cancel
	}
	m.dispatchers[core.ChannelWebHook] = d
	ch := &core.AlertChannel{ID: "cancel-test", Name: "Local test", Type: core.ChannelWebHook, Enabled: true, RetryPolicy: core.RetryPolicyConfig{Backoff: strategy, MaxRetries: 3}}
	m.channels[ch.ID] = ch
	if canceled {
		cancel()
		<-ctx.Done()
	}
	err := m.TestChannel(ctx, ch.ID, "default")
	t.Logf("strategy=%s pre-canceled=%v EXPECTED: sends=%d ACTUAL: sends=%d", strategy, canceled, want, d.calls)
	if d.calls != want {
		t.Fatal("regression mismatch")
	}
	if canceled || cancelInside {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation got %v", err)
		}
	} else if err != nil {
		t.Fatal(err)
	}
}
func TestAuditExponentialRetryCancellation(t *testing.T) {
	t.Run("linear-canceled-control", func(t *testing.T) { auditRetryCancellation(t, "linear", true, false, 0) })
	t.Run("exponential-canceled", func(t *testing.T) { auditRetryCancellation(t, "exponential", true, false, 0) })
	t.Run("exponential-success", func(t *testing.T) { auditRetryCancellation(t, "exponential", false, false, 1) })
	t.Run("linear-success", func(t *testing.T) { auditRetryCancellation(t, "linear", false, false, 1) })
	t.Run("cancel-in-first-exponential-send", func(t *testing.T) { auditRetryCancellation(t, "exponential", false, true, 1) })
	t.Run("cancel-in-first-linear-send", func(t *testing.T) { auditRetryCancellation(t, "linear", false, true, 1) })
}
