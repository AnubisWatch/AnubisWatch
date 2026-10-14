package main

// Regression coverage for the clustered-write key format of the alert-resource
// branches of restStorageAdapter.
//
// In a clustered deployment every write is replicated as an FSMCommand
// (cmd/anubis/server.go:585 sets restStore.replicator when the cluster is
// active), so the adapter must use exactly the key CobaltDB's read paths scan.
//
// SaveChannelNoCtx/DeleteChannelNoCtx replicated under "{ws}/channels/{id}" and
// SaveRuleNoCtx/DeleteRuleNoCtx under "{ws}/rules/{id}", while every engine read
// path resolves alert channels and rules under "{ws}/alerts/channels/{id}" and
// "{ws}/alerts/rules/{id}" (internal/storage/storage.go: GetAlertChannel,
// ListAlertChannels, GetAlertRule, ListAlertRules). A clustered create reported
// success and the record was then permanently unreadable; delete removed a key
// that had never been written, orphaning the record.
//
// The production consumer is the gRPC API, whose CreateChannel/CreateRule save
// and then immediately read back through the same adapter.

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

// alertKeyFSMReplicator stands in for clusterAdapter.ApplyMutation and applies
// the committed command exactly the way the production Raft FSM does
// (internal/raft/fsm.go -> CobaltDB.ApplyFSMCommand), so these tests observe the
// same state a real cluster node would.
type alertKeyFSMReplicator struct {
	store *storage.CobaltDB
}

func (r *alertKeyFSMReplicator) ApplyMutation(cmd core.FSMCommand, _ time.Duration) (uint64, uint64, error) {
	if err := r.store.ApplyFSMCommand(&cmd); err != nil {
		return 0, 0, err
	}
	return 1, 1, nil
}

func newAlertKeyDB(t *testing.T) *storage.CobaltDB {
	t.Helper()
	db, err := storage.NewEngine(
		core.StorageConfig{Path: t.TempDir()},
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func clusteredAlertAdapter(t *testing.T) (*restStorageAdapter, *storage.CobaltDB) {
	t.Helper()
	db := newAlertKeyDB(t)
	return &restStorageAdapter{store: db, replicator: &alertKeyFSMReplicator{store: db}}, db
}

func proofChannel() *core.AlertChannel {
	return &core.AlertChannel{
		ID:          "chan-1",
		Name:        "Ops Slack",
		Type:        core.ChannelSlack,
		Enabled:     true,
		WorkspaceID: "default",
	}
}

func proofRule() *core.AlertRule {
	return &core.AlertRule{
		ID:          "rule-1",
		Name:        "Latency budget",
		Enabled:     true,
		WorkspaceID: "default",
		Severity:    core.SeverityWarning,
	}
}

// TestClusteredAlertChannelWriteUsesCanonicalKey mirrors gRPC CreateChannel:
// save, then read back immediately.
func TestClusteredAlertChannelWriteUsesCanonicalKey(t *testing.T) {
	adapter, _ := clusteredAlertAdapter(t)

	if err := adapter.SaveChannelNoCtx(proofChannel()); err != nil {
		t.Fatalf("SaveChannelNoCtx: %v", err)
	}

	got, err := adapter.GetChannelNoCtx("chan-1", "default")
	if err != nil || got == nil {
		t.Fatalf("clustered alert channel 'chan-1' is unreadable right after a successful write: err=%v", err)
	}
	if got.Name != "Ops Slack" {
		t.Fatalf("clustered alert channel round-trip lost fields: %+v", got)
	}

	all, err := adapter.ListChannelsNoCtx("default")
	if err != nil {
		t.Fatalf("ListChannelsNoCtx: %v", err)
	}
	for _, c := range all {
		if c != nil && c.ID == "chan-1" {
			return
		}
	}
	t.Fatalf("clustered alert channel 'chan-1' missing from the channel list (%d listed)", len(all))
}

// TestClusteredAlertRuleWriteUsesCanonicalKey mirrors gRPC CreateRule.
func TestClusteredAlertRuleWriteUsesCanonicalKey(t *testing.T) {
	adapter, _ := clusteredAlertAdapter(t)

	if err := adapter.SaveRuleNoCtx(proofRule()); err != nil {
		t.Fatalf("SaveRuleNoCtx: %v", err)
	}

	got, err := adapter.GetRuleNoCtx("rule-1", "default")
	if err != nil || got == nil {
		t.Fatalf("clustered alert rule 'rule-1' is unreadable right after a successful write: err=%v", err)
	}
	if got.Name != "Latency budget" {
		t.Fatalf("clustered alert rule round-trip lost fields: %+v", got)
	}

	all, err := adapter.ListRulesNoCtx("default")
	if err != nil {
		t.Fatalf("ListRulesNoCtx: %v", err)
	}
	for _, r := range all {
		if r != nil && r.ID == "rule-1" {
			return
		}
	}
	t.Fatalf("clustered alert rule 'rule-1' missing from the rule list (%d listed)", len(all))
}

// TestClusteredAlertChannelDeleteRemovesTheStoredRecord covers the secondary
// branch the fix touches: delete must use the same canonical key as save.
func TestClusteredAlertChannelDeleteRemovesTheStoredRecord(t *testing.T) {
	adapter, _ := clusteredAlertAdapter(t)

	if err := adapter.SaveChannelNoCtx(proofChannel()); err != nil {
		t.Fatalf("SaveChannelNoCtx: %v", err)
	}
	if err := adapter.DeleteChannelNoCtx("chan-1", "default"); err != nil {
		t.Fatalf("DeleteChannelNoCtx: %v", err)
	}

	all, err := adapter.ListChannelsNoCtx("default")
	if err != nil {
		t.Fatalf("ListChannelsNoCtx: %v", err)
	}
	for _, c := range all {
		if c != nil && c.ID == "chan-1" {
			t.Fatalf("alert channel 'chan-1' still present after delete (delete used a different key than save)")
		}
	}
}

// TestClusteredAlertRuleDeleteRemovesTheStoredRecord is the rule-branch
// equivalent of the delete check.
func TestClusteredAlertRuleDeleteRemovesTheStoredRecord(t *testing.T) {
	adapter, _ := clusteredAlertAdapter(t)

	if err := adapter.SaveRuleNoCtx(proofRule()); err != nil {
		t.Fatalf("SaveRuleNoCtx: %v", err)
	}
	if err := adapter.DeleteRuleNoCtx("rule-1", "default"); err != nil {
		t.Fatalf("DeleteRuleNoCtx: %v", err)
	}

	all, err := adapter.ListRulesNoCtx("default")
	if err != nil {
		t.Fatalf("ListRulesNoCtx: %v", err)
	}
	for _, r := range all {
		if r != nil && r.ID == "rule-1" {
			t.Fatalf("alert rule 'rule-1' still present after delete (delete used a different key than save)")
		}
	}
}

// TestStandaloneAlertWriteRoundTrips is the control: with no replicator the
// adapter must still read back what it wrote, so a green clustered result cannot
// come from a broken harness.
func TestStandaloneAlertWriteRoundTrips(t *testing.T) {
	db := newAlertKeyDB(t)
	adapter := &restStorageAdapter{store: db} // replicator == nil: standalone

	if err := adapter.SaveChannelNoCtx(proofChannel()); err != nil {
		t.Fatalf("SaveChannelNoCtx: %v", err)
	}
	if err := adapter.SaveRuleNoCtx(proofRule()); err != nil {
		t.Fatalf("SaveRuleNoCtx: %v", err)
	}

	ch, err := adapter.GetChannelNoCtx("chan-1", "default")
	if err != nil || ch == nil || ch.Name != "Ops Slack" {
		t.Fatalf("control broken: standalone alert channel not readable: %+v err=%v", ch, err)
	}
	rule, err := adapter.GetRuleNoCtx("rule-1", "default")
	if err != nil || rule == nil || rule.Name != "Latency budget" {
		t.Fatalf("control broken: standalone alert rule not readable: %+v err=%v", rule, err)
	}
}
