package main

// Regression coverage for alert channels and verdict rules declared in the
// config file. They were validated by core.Config.Validate but never handed to
// the alert manager, so the shipped configs/anubis.yaml "Critical Alert" rule
// and its email channel silently never existed at runtime.

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/alert"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

const configAlertsYAML = `
channels:
  - name: "Admin Alerts"
    type: "email"
    enabled: true
    email:
      smtp_host: "smtp.example.com"
      smtp_port: 2525
      from: "anubis@example.com"
      to:
        - "admin@example.com"
  - name: "Muted Slack"
    type: "slack"
    enabled: false
    slack:
      webhook_url: "https://hooks.slack.com/services/T000/B000/XXXX"

verdicts:
  rules:
    - name: "Critical Alert"
      enabled: true
      scope:
        type: "all"
      conditions:
        - type: "status_change"
          from: "alive"
          to: "dead"
      channels:
        - "Admin Alerts"
`

func newConfigAlertsDB(t *testing.T) *storage.CobaltDB {
	t.Helper()
	db, err := storage.NewEngine(core.StorageConfig{Path: t.TempDir()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func loadAlertsTestConfig(t *testing.T, body string) *core.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "anubis.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := core.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	return cfg
}

// startConfiguredAlertManager simulates one server start against db.
func startConfiguredAlertManager(t *testing.T, db *storage.CobaltDB, cfg *core.Config) *alert.Manager {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := alert.NewManager(&alertStorageAdapter{store: db}, logger)
	if err := mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop() })
	loadConfiguredAlerts(cfg, mgr, logger)
	return mgr
}

func TestConfiguredAlertsAreRegisteredWithAlertManager(t *testing.T) {
	db := newConfigAlertsDB(t)
	cfg := loadAlertsTestConfig(t, configAlertsYAML)
	mgr := startConfiguredAlertManager(t, db, cfg)

	emailID := configEntityID(configChannelIDPrefix, "Admin Alerts")
	email, err := mgr.GetChannel(emailID)
	if err != nil {
		t.Fatalf("configured email channel not registered: %v", err)
	}
	if !email.Enabled || email.Type != core.ChannelEmail || email.WorkspaceID != "default" {
		t.Fatalf("email channel = enabled:%v type:%q ws:%q", email.Enabled, email.Type, email.WorkspaceID)
	}
	// EmailDispatcher.Send reads JSON-shaped values; a typed int or []string
	// would silently fall back to port 587 and "no recipients configured".
	if port, ok := email.Config["smtp_port"].(float64); !ok || port != 2525 {
		t.Fatalf("smtp_port = %#v, want float64(2525)", email.Config["smtp_port"])
	}
	if to, ok := email.Config["to"].([]any); !ok || len(to) != 1 || to[0] != "admin@example.com" {
		t.Fatalf("to = %#v, want []any{\"admin@example.com\"}", email.Config["to"])
	}

	slack, err := mgr.GetChannel(configEntityID(configChannelIDPrefix, "Muted Slack"))
	if err != nil {
		t.Fatalf("configured slack channel not registered: %v", err)
	}
	if slack.Enabled {
		t.Fatal("channel with enabled: false was registered as enabled")
	}

	rule, err := mgr.GetRule(configEntityID(configRuleIDPrefix, "Critical Alert"))
	if err != nil {
		t.Fatalf("configured rule not registered: %v", err)
	}
	if !rule.Enabled || rule.WorkspaceID != "default" {
		t.Fatalf("rule = enabled:%v ws:%q", rule.Enabled, rule.WorkspaceID)
	}
	if len(rule.Channels) != 1 || rule.Channels[0] != emailID {
		t.Fatalf("rule channels = %v, want channel name resolved to [%s]", rule.Channels, emailID)
	}
	if cond := rule.Conditions[0]; cond.Type != "status_change" || cond.From != "alive" || cond.To != "dead" {
		t.Fatalf("rule condition = %+v", cond)
	}

	// The REST list endpoints read from the manager, so these are what the
	// dashboard shows.
	if got := len(mgr.ListChannels()); got != 2 {
		t.Fatalf("ListChannels = %d, want 2", got)
	}
	if got := len(mgr.ListRules()); got != 1 {
		t.Fatalf("ListRules = %d, want 1", got)
	}
}

func TestConfiguredAlertsSurviveRestartWithoutDuplicates(t *testing.T) {
	db := newConfigAlertsDB(t)
	cfg := loadAlertsTestConfig(t, configAlertsYAML)

	first := startConfiguredAlertManager(t, db, cfg)
	if err := first.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	second := startConfiguredAlertManager(t, db, cfg)
	if got := len(second.ListChannels()); got != 2 {
		t.Fatalf("channels after restart = %d, want 2 (no duplicates)", got)
	}
	if got := len(second.ListRules()); got != 1 {
		t.Fatalf("rules after restart = %d, want 1 (no duplicates)", got)
	}
}

func TestConfiguredAlertsRemovedFromConfigArePruned(t *testing.T) {
	db := newConfigAlertsDB(t)
	first := startConfiguredAlertManager(t, db, loadAlertsTestConfig(t, configAlertsYAML))

	// An API-created channel and rule must never be touched by the pruning.
	apiChannel := &core.AlertChannel{
		ID: core.GenerateID(), Name: "API Hook", Type: core.ChannelSlack, Enabled: true,
		WorkspaceID: "default",
		Config:      map[string]any{"webhook_url": "https://hooks.slack.com/services/T1/B1/YYYY"},
	}
	if err := first.RegisterChannel(apiChannel); err != nil {
		t.Fatalf("RegisterChannel: %v", err)
	}
	apiRule := &core.AlertRule{
		ID: core.GenerateID(), Name: "API Rule", Enabled: true, WorkspaceID: "default",
		Conditions: []core.AlertCondition{{Type: "consecutive_failures", Threshold: 3}},
		Channels:   []string{apiChannel.ID},
	}
	if err := first.RegisterRule(apiRule); err != nil {
		t.Fatalf("RegisterRule: %v", err)
	}
	if err := first.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Restart with the alert sections removed from the config file.
	second := startConfiguredAlertManager(t, db, loadAlertsTestConfig(t, "souls: []\n"))
	if err := second.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// A third start reads only what is persisted, proving the prune reached storage.
	third := startConfiguredAlertManager(t, db, loadAlertsTestConfig(t, "souls: []\n"))
	channels := third.ListChannels()
	if len(channels) != 1 || channels[0].ID != apiChannel.ID {
		t.Fatalf("channels after prune = %v, want only the API channel", channelIDsOf(channels))
	}
	rules := third.ListRules()
	if len(rules) != 1 || rules[0].ID != apiRule.ID {
		t.Fatalf("rules after prune = %d, want only the API rule", len(rules))
	}
}

func TestConfiguredRuleKeepsUnknownChannelReferenceAsID(t *testing.T) {
	refs := resolveChannelRefs([]string{"Admin Alerts", "01JAPICHANNELID"}, map[string]string{"Admin Alerts": "config-channel-x"})
	if len(refs) != 2 || refs[0] != "config-channel-x" || refs[1] != "01JAPICHANNELID" {
		t.Fatalf("resolveChannelRefs = %v", refs)
	}
}

func channelIDsOf(channels []*core.AlertChannel) []string {
	ids := make([]string, 0, len(channels))
	for _, ch := range channels {
		ids = append(ids, ch.ID)
	}
	return ids
}
