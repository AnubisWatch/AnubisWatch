package core

import (
	"strings"
	"testing"
)

func TestAlertRuleValidateMetrics(t *testing.T) {
	cases := []struct {
		name      string
		conds     []AlertCondition
		wantField string // empty means valid
	}{
		{name: "unset metric is latency", conds: []AlertCondition{{Type: "threshold"}}},
		{name: "known metrics", conds: []AlertCondition{
			{Type: "threshold", Metric: "latency"},
			{Type: "threshold", Metric: "duration"},
			{Type: "anomaly", Metric: "status_code"},
		}},
		{name: "dashboard metrics", conds: []AlertCondition{
			{Type: "threshold", Metric: "latency_ms"},
			{Type: "threshold", Metric: "tls_expiry_days"},
			{Type: "anomaly", Metric: "latency_ms"},
		}},
		{name: "anomaly cannot use tls_expiry_days", conds: []AlertCondition{{Type: "anomaly", Metric: "tls_expiry_days"}}, wantField: "conditions[0].metric"},
		{name: "metric ignored by other condition types", conds: []AlertCondition{{Type: "status_change", Metric: "cpu"}}},
		{name: "threshold on cpu", conds: []AlertCondition{{Type: "threshold", Metric: "cpu"}}, wantField: "conditions[0].metric"},
		{name: "anomaly on memory", conds: []AlertCondition{
			{Type: "status_change"},
			{Type: "anomaly", Metric: "memory"},
		}, wantField: "conditions[1].metric"},
		{name: "nested in compound", conds: []AlertCondition{{Type: "compound", SubConditions: []AlertCondition{
			{Type: "threshold", Metric: "latency"},
			{Type: "compound", SubConditions: []AlertCondition{{Type: "threshold", Metric: "latancy"}}},
		}}}, wantField: "conditions[0].sub_conditions[1].sub_conditions[0].metric"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := AlertRule{Conditions: tc.conds}.ValidateMetrics()
			if tc.wantField == "" {
				if err != nil {
					t.Fatalf("ValidateMetrics() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateMetrics() = nil, want an error on %s", tc.wantField)
			}
			if !strings.Contains(err.Error(), tc.wantField) || !strings.Contains(err.Error(), "supported: latency, latency_ms") {
				t.Fatalf("error %q should name the field %s and list the supported metrics", err, tc.wantField)
			}
		})
	}
}
