package alert

import (
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// A threshold condition on a metric the engine does not measure must never
// fire. evaluateCondition read every unrecognized metric as latency, so a rule
// such as `cpu > 90` paged on each check slower than 90ms and stayed silent on
// the CPU problem it was written for.

func TestThresholdOnUnknownMetricNeverFires(t *testing.T) {
	m := NewManager(nil, newTestLogger())
	slow := &core.Judgment{SoulID: "s", Status: core.SoulAlive, Duration: 5 * time.Second, StatusCode: 200}

	for _, metric := range []string{"cpu", "memory", "response_size", "latancy"} {
		cond := core.AlertCondition{Type: "threshold", Metric: metric, Operator: ">", Value: 90.0}
		if m.evaluateCondition(cond, core.SoulAlive, slow) {
			t.Errorf("threshold on unmeasured metric %q fired on a 5s check: it was read as latency", metric)
		}
		compound := core.AlertCondition{Type: "compound", Logic: "or", SubConditions: []core.AlertCondition{cond}}
		if m.evaluateCondition(compound, core.SoulAlive, slow) {
			t.Errorf("compound threshold on unmeasured metric %q fired on a 5s check", metric)
		}
	}
}

// TestThresholdOnKnownMetricsStillFires is the neighbouring control, including
// an unset metric, which means latency.
func TestThresholdOnKnownMetricsStillFires(t *testing.T) {
	m := NewManager(nil, newTestLogger())
	j := &core.Judgment{
		SoulID: "s", Status: core.SoulAlive, Duration: 5 * time.Second, StatusCode: 503,
		TLSInfo: &core.TLSInfo{DaysUntilExpiry: 7},
	}

	cases := map[string]core.AlertCondition{
		"unset metric is latency": {Type: "threshold", Operator: ">", Value: 90.0},
		"latency":                 {Type: "threshold", Metric: "latency", Operator: ">", Value: 90.0},
		"duration":                {Type: "threshold", Metric: "duration", Operator: ">", Value: 90.0},
		"status_code":             {Type: "threshold", Metric: "status_code", Operator: ">=", Value: 500.0},
		// The two metrics the dashboard's rule form sends.
		"latency_ms":      {Type: "threshold", Metric: "latency_ms", Operator: ">", Value: 90.0},
		"tls_expiry_days": {Type: "threshold", Metric: "tls_expiry_days", Operator: "<", Value: 30.0},
	}
	for name, cond := range cases {
		if !m.evaluateCondition(cond, core.SoulAlive, j) {
			t.Errorf("%s: threshold did not fire", name)
		}
	}
}

// TestTLSExpiryThreshold pins tls_expiry_days to the certificate, not to latency:
// it was read as milliseconds, so "expires in under 30 days" fired on every
// check faster than 30ms and never on an expiring certificate.
func TestTLSExpiryThreshold(t *testing.T) {
	m := NewManager(nil, newTestLogger())
	cond := core.AlertCondition{Type: "threshold", Metric: "tls_expiry_days", Operator: "<", Value: 30.0}

	fastHealthyCert := &core.Judgment{Duration: 5 * time.Millisecond, TLSInfo: &core.TLSInfo{DaysUntilExpiry: 200}}
	if m.evaluateCondition(cond, core.SoulAlive, fastHealthyCert) {
		t.Error("fired for a certificate with 200 days left")
	}
	slowExpiringCert := &core.Judgment{Duration: 5 * time.Second, TLSInfo: &core.TLSInfo{DaysUntilExpiry: 3}}
	if !m.evaluateCondition(cond, core.SoulAlive, slowExpiringCert) {
		t.Error("did not fire for a certificate with 3 days left")
	}
	if m.evaluateCondition(cond, core.SoulAlive, &core.Judgment{Duration: 5 * time.Millisecond}) {
		t.Error("fired for a check with no certificate data")
	}
}
