package grpcapi

// Regression coverage for gRPC rule writes reaching the live alert engine.
//
// CreateRule/UpdateRule/DeleteRule wrote only to storage, which the alert
// manager reads once at startup, so a rule created over gRPC never fired and a
// deleted one kept firing until the next restart. CreateRule also dropped the
// request's condition_type and config, storing a rule with no condition at
// all, and neither RPC validated the rule.

import (
	"io"
	"log/slog"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/AnubisWatch/anubiswatch/internal/alert"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	v1 "github.com/AnubisWatch/anubiswatch/internal/grpcapi/v1"
)

var _ AlertRegistrar = (*alert.Manager)(nil)

func newRuleActivationServer(t *testing.T) (*Server, *mockGRPCStore, *alert.Manager) {
	t.Helper()
	store := newMockGRPCStore()
	// As in production, the alert engine persists into the same store the
	// gRPC server reads: Create/Update write once, through the engine.
	mgr := alert.NewManager(&grpcBackedAlertStorage{store: store}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, false).SetAlertRegistrar(mgr)
	return srv, store, mgr
}

func TestGRPCCreateRuleActivatesRuleWithItsCondition(t *testing.T) {
	srv, _, mgr := newRuleActivationServer(t)

	resp, err := srv.CreateRule(testUserContext(), &v1.CreateRuleRequest{
		Name:          "pager on 3 failures",
		ConditionType: "consecutive_failures",
		ChannelId:     "ch_1",
		Enabled:       true,
		Config:        map[string]string{"threshold": "3", "severity": "critical"},
	})
	if err != nil {
		t.Fatalf("CreateRule: %v", err)
	}

	live, err := mgr.GetRule(resp.Id)
	if err != nil {
		t.Fatalf("rule created over gRPC is not active in the alert manager: %v", err)
	}
	if len(live.Conditions) != 1 || live.Conditions[0].Type != "consecutive_failures" || live.Conditions[0].Threshold != 3 {
		t.Fatalf("live conditions = %+v, want one consecutive_failures with threshold 3", live.Conditions)
	}
	if len(live.Channels) != 1 || live.Channels[0] != "ch_1" {
		t.Fatalf("live channels = %v, want [ch_1]", live.Channels)
	}
	if live.Severity != core.Severity("critical") || live.WorkspaceID != "default" || !live.Enabled {
		t.Fatalf("live rule = severity:%q ws:%q enabled:%v", live.Severity, live.WorkspaceID, live.Enabled)
	}
}

func TestGRPCCreateRuleRejectsInvalidRule(t *testing.T) {
	cases := map[string]*v1.CreateRuleRequest{
		"no condition":      {Name: "r", ChannelId: "ch_1"},
		"no channel":        {Name: "r", ConditionType: "consecutive_failures"},
		"unknown condition": {Name: "r", ConditionType: "recovery", ChannelId: "ch_1"},
		"bad threshold":     {Name: "r", ConditionType: "consecutive_failures", ChannelId: "ch_1", Config: map[string]string{"threshold": "three"}},
		"bad value":         {Name: "r", ConditionType: "threshold", ChannelId: "ch_1", Config: map[string]string{"value": "fast"}},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			srv, store, mgr := newRuleActivationServer(t)
			_, err := srv.CreateRule(testUserContext(), req)
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("CreateRule error = %v, want InvalidArgument", err)
			}
			if len(store.rules) != 0 || len(mgr.ListRules()) != 0 {
				t.Fatalf("invalid rule was stored (%d) or activated (%d)", len(store.rules), len(mgr.ListRules()))
			}
		})
	}
}

func TestGRPCUpdateRuleActivatesChange(t *testing.T) {
	srv, _, mgr := newRuleActivationServer(t)
	resp, err := srv.CreateRule(testUserContext(), &v1.CreateRuleRequest{
		Name: "r", ConditionType: "consecutive_failures", ChannelId: "ch_1", Enabled: true,
		Config: map[string]string{"threshold": "3"},
	})
	if err != nil {
		t.Fatalf("CreateRule: %v", err)
	}

	disabled := false
	if _, err := srv.UpdateRule(testUserContext(), &v1.UpdateRuleRequest{
		Id: resp.Id, Enabled: &disabled, Config: map[string]string{"threshold": "5"},
	}); err != nil {
		t.Fatalf("UpdateRule: %v", err)
	}

	live, err := mgr.GetRule(resp.Id)
	if err != nil {
		t.Fatalf("GetRule: %v", err)
	}
	if live.Enabled || live.Conditions[0].Threshold != 5 {
		t.Fatalf("live rule after update = enabled:%v threshold:%d, want disabled with threshold 5",
			live.Enabled, live.Conditions[0].Threshold)
	}
}

func TestGRPCUpdateRuleCanRepairConditionlessRule(t *testing.T) {
	srv, store, mgr := newRuleActivationServer(t)
	// The shape older CreateRule calls stored: no condition.
	store.rules["legacy"] = &core.AlertRule{ID: "legacy", Name: "legacy", Enabled: true, WorkspaceID: "default", Channels: []string{"ch_1"}}

	enabled := true
	if _, err := srv.UpdateRule(testUserContext(), &v1.UpdateRuleRequest{Id: "legacy", Enabled: &enabled}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("update leaving the rule without a condition = %v, want InvalidArgument", err)
	}

	if _, err := srv.UpdateRule(testUserContext(), &v1.UpdateRuleRequest{
		Id: "legacy", Config: map[string]string{"condition_type": "status_change", "from": "alive", "to": "dead"},
	}); err != nil {
		t.Fatalf("UpdateRule with condition_type: %v", err)
	}
	live, err := mgr.GetRule("legacy")
	if err != nil {
		t.Fatalf("repaired rule not active: %v", err)
	}
	if c := live.Conditions[0]; c.Type != "status_change" || c.From != "alive" || c.To != "dead" {
		t.Fatalf("repaired condition = %+v", c)
	}
}

func TestGRPCDeleteRuleDeactivatesRule(t *testing.T) {
	srv, store, mgr := newRuleActivationServer(t)
	// Seed storage and the live engine directly, as a startup load would.
	rule := &core.AlertRule{
		ID: "live", Name: "live", Enabled: true, WorkspaceID: "default", Channels: []string{"ch_1"},
		Conditions: []core.AlertCondition{{Type: "consecutive_failures", Threshold: 1}},
	}
	store.rules[rule.ID] = rule
	if err := mgr.RegisterRule(rule); err != nil {
		t.Fatalf("RegisterRule: %v", err)
	}

	if _, err := srv.DeleteRule(testUserContext(), &v1.DeleteRuleRequest{Id: rule.ID}); err != nil {
		t.Fatalf("DeleteRule: %v", err)
	}
	if _, err := mgr.GetRule(rule.ID); err == nil {
		t.Fatal("rule deleted over gRPC is still active in the alert manager")
	}
}

// grpcBackedAlertStorage stores alert channels and rules in the mock gRPC
// store; events and incidents are not needed by these tests.
type grpcBackedAlertStorage struct{ store *mockGRPCStore }

func (a *grpcBackedAlertStorage) SaveChannel(ch *core.AlertChannel) error {
	return a.store.SaveChannelNoCtx(ch)
}
func (a *grpcBackedAlertStorage) GetChannel(id, ws string) (*core.AlertChannel, error) {
	return a.store.GetChannelNoCtx(id, ws)
}
func (a *grpcBackedAlertStorage) ListChannels(ws string) ([]*core.AlertChannel, error) {
	return a.store.ListChannelsNoCtx(ws)
}
func (a *grpcBackedAlertStorage) DeleteChannel(id, ws string) error {
	return a.store.DeleteChannelNoCtx(id, ws)
}
func (a *grpcBackedAlertStorage) SaveRule(rule *core.AlertRule) error {
	return a.store.SaveRuleNoCtx(rule)
}
func (a *grpcBackedAlertStorage) GetRule(id, ws string) (*core.AlertRule, error) {
	return a.store.GetRuleNoCtx(id, ws)
}
func (a *grpcBackedAlertStorage) ListRules(ws string) ([]*core.AlertRule, error) {
	return a.store.ListRulesNoCtx(ws)
}
func (a *grpcBackedAlertStorage) DeleteRule(id, ws string) error {
	return a.store.DeleteRuleNoCtx(id, ws)
}
func (a *grpcBackedAlertStorage) SaveEvent(*core.AlertEvent) error { return nil }
func (a *grpcBackedAlertStorage) ListEvents(string, int) ([]*core.AlertEvent, error) {
	return nil, nil
}
func (a *grpcBackedAlertStorage) SaveIncident(*core.Incident) error          { return nil }
func (a *grpcBackedAlertStorage) GetIncident(string) (*core.Incident, error) { return nil, nil }
func (a *grpcBackedAlertStorage) ListActiveIncidents() ([]*core.Incident, error) {
	return nil, nil
}
