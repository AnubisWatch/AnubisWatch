package grpcapi

import (
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	v1 "github.com/AnubisWatch/anubiswatch/internal/grpcapi/v1"
)

// The alert engine never fires a threshold or anomaly condition on a metric it
// does not measure, so gRPC must refuse such a rule, and a refused update must
// leave the stored rule exactly as it was.

func TestGRPCCreateRuleRejectsUnknownMetric(t *testing.T) {
	srv, store, mgr := newRuleActivationServer(t)

	_, err := srv.CreateRule(testUserContext(), &v1.CreateRuleRequest{
		Name: "cpu", ConditionType: "anomaly", ChannelId: "ch_1", Enabled: true,
		Config: map[string]string{"metric": "cpu"},
	})
	if status.Code(err) != codes.InvalidArgument || !strings.Contains(err.Error(), "unsupported metric") {
		t.Fatalf("CreateRule with metric cpu = %v, want InvalidArgument naming the metric", err)
	}
	if len(store.rules) != 0 || len(mgr.ListRules()) != 0 {
		t.Fatal("rule with an unsupported metric was stored or activated")
	}
}

func TestGRPCUpdateRuleRejectsUnknownMetricWithoutTouchingStoredRule(t *testing.T) {
	srv, store, _ := newRuleActivationServer(t)
	stored := &core.AlertRule{
		ID: "r", Name: "latency", Enabled: true, WorkspaceID: "default", Channels: []string{"ch_1"},
		Conditions: []core.AlertCondition{{Type: "threshold", Metric: "latency", Operator: ">", Value: 500.0}},
	}
	store.rules["r"] = stored // the mock hands out this same pointer

	name := "renamed"
	_, err := srv.UpdateRule(testUserContext(), &v1.UpdateRuleRequest{
		Id: "r", Name: &name, Config: map[string]string{"metric": "cpu", "value": "90"},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("UpdateRule to metric cpu = %v, want InvalidArgument", err)
	}
	if stored.Name != "latency" || stored.Conditions[0].Metric != "latency" || stored.Conditions[0].Value != 500.0 {
		t.Fatalf("refused update changed the stored rule: name=%q condition=%+v", stored.Name, stored.Conditions[0])
	}
}
