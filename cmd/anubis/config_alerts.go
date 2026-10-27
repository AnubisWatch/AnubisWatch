package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/alert"
	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Config-managed alert channels and rules get deterministic IDs derived from
// their names, so a restart updates them in place instead of duplicating them,
// and entries removed from the config file can be pruned from storage.
const (
	configChannelIDPrefix = "config-channel-"
	configRuleIDPrefix    = "config-rule-"
)

func configEntityID(prefix, name string) string {
	sum := sha256.Sum256([]byte(name))
	return prefix + hex.EncodeToString(sum[:8])
}

// channelFromConfig converts a config-file channel into the runtime form the
// dispatchers read. The typed sub-config is round-tripped through JSON so the
// resulting map has exactly the shape of an API-created channel (numbers as
// float64, lists as []any).
func channelFromConfig(cc core.ChannelConfig, now time.Time) (*core.AlertChannel, error) {
	var sub any
	switch cc.Type {
	case "webhook":
		sub = cc.Webhook
	case "slack":
		sub = cc.Slack
	case "discord":
		sub = cc.Discord
	case "telegram":
		sub = cc.Telegram
	case "email":
		sub = cc.Email
	case "pagerduty":
		sub = cc.PagerDuty
	case "opsgenie":
		sub = cc.OpsGenie
	case "sms":
		sub = cc.SMS
	case "ntfy":
		sub = cc.Ntfy
	default:
		return nil, fmt.Errorf("unsupported channel type %q", cc.Type)
	}

	raw, err := json.Marshal(sub)
	if err != nil {
		return nil, fmt.Errorf("encode %s settings: %w", cc.Type, err)
	}
	config := map[string]any{}
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("decode %s settings: %w", cc.Type, err)
	}

	enabled := cc.Enabled == nil || *cc.Enabled
	return &core.AlertChannel{
		ID:          configEntityID(configChannelIDPrefix, cc.Name),
		Name:        cc.Name,
		Type:        core.AlertChannelType(cc.Type),
		Enabled:     enabled,
		WorkspaceID: "default",
		Config:      config,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// resolveChannelRefs maps config channel names to their IDs. References that
// are not config channel names are kept as-is so a rule can still point at a
// channel created through the API by its ID.
func resolveChannelRefs(refs []string, byName map[string]string) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if id, ok := byName[ref]; ok {
			out = append(out, id)
		} else {
			out = append(out, ref)
		}
	}
	return out
}

// loadConfiguredAlerts registers the channels and verdict rules declared in
// the config file with the alert manager and removes config-managed entries
// that are no longer declared. The config file is authoritative for these
// entries: API edits to them are overwritten on the next restart.
//
// It must run after the alert manager has started, so the stored entries are
// already loaded and stale config-managed ones can be found.
func loadConfiguredAlerts(cfg *core.Config, mgr *alert.Manager, logger *slog.Logger) {
	if cfg == nil || mgr == nil {
		return
	}
	now := time.Now().UTC()

	channelIDs := make(map[string]string, len(cfg.Channels))
	for _, cc := range cfg.Channels {
		if _, dup := channelIDs[cc.Name]; dup {
			logger.Warn("duplicate channel name in config; skipping", "channel", cc.Name)
			continue
		}
		ch, err := channelFromConfig(cc, now)
		if err == nil {
			err = mgr.RegisterChannel(ch)
		}
		if err != nil {
			logger.Warn("failed to register configured channel", "channel", cc.Name, "err", err)
			continue
		}
		channelIDs[cc.Name] = ch.ID
	}

	ruleIDs := make(map[string]struct{}, len(cfg.Verdicts.Rules))
	for i := range cfg.Verdicts.Rules {
		rule := cfg.Verdicts.Rules[i]
		if rule.ID == "" {
			rule.ID = configEntityID(configRuleIDPrefix, rule.Name)
		}
		if _, dup := ruleIDs[rule.ID]; dup {
			logger.Warn("duplicate rule in config; skipping", "rule", rule.Name)
			continue
		}
		if rule.WorkspaceID == "" {
			rule.WorkspaceID = "default"
		}
		if rule.CreatedAt.IsZero() {
			rule.CreatedAt = now
		}
		rule.Channels = resolveChannelRefs(rule.Channels, channelIDs)
		if rule.Escalation != nil && len(rule.Escalation.Stages) == 0 {
			// The rule names a shared policy from verdicts.escalation.
			policy, ok := cfg.Verdicts.EscalationPolicyByName(rule.Escalation.Name)
			if !ok {
				logger.Warn("configured rule names an unknown escalation policy; skipping", "rule", rule.Name, "policy", rule.Escalation.Name)
				continue
			}
			rule.Escalation = &policy
		}
		if rule.Escalation != nil {
			escalation := *rule.Escalation
			escalation.Stages = make([]core.EscalationStage, len(rule.Escalation.Stages))
			for j, stage := range rule.Escalation.Stages {
				stage.Channels = resolveChannelRefs(stage.Channels, channelIDs)
				escalation.Stages[j] = stage
			}
			rule.Escalation = &escalation
		}
		if err := rule.Validate(); err != nil {
			logger.Warn("invalid configured rule; skipping", "rule", rule.Name, "err", err)
			continue
		}
		if err := rule.ValidateMetrics(); err != nil {
			logger.Warn("configured rule has a condition that can never fire", "rule", rule.Name, "err", err)
		}
		if err := mgr.RegisterRule(&rule); err != nil {
			logger.Warn("failed to register configured rule", "rule", rule.Name, "err", err)
			continue
		}
		ruleIDs[rule.ID] = struct{}{}
	}

	for _, rule := range mgr.ListRules() {
		if _, keep := ruleIDs[rule.ID]; !keep && strings.HasPrefix(rule.ID, configRuleIDPrefix) {
			if err := mgr.DeleteRule(rule.ID); err != nil {
				logger.Warn("failed to remove stale configured rule", "rule", rule.Name, "err", err)
			}
		}
	}
	keepChannels := make(map[string]struct{}, len(channelIDs))
	for _, id := range channelIDs {
		keepChannels[id] = struct{}{}
	}
	for _, ch := range mgr.ListChannels() {
		if _, keep := keepChannels[ch.ID]; !keep && strings.HasPrefix(ch.ID, configChannelIDPrefix) {
			if err := mgr.DeleteChannel(ch.ID); err != nil {
				logger.Warn("failed to remove stale configured channel", "channel", ch.Name, "err", err)
			}
		}
	}

	if len(channelIDs) > 0 || len(ruleIDs) > 0 {
		logger.Info("configured alerts loaded", "channels", len(channelIDs), "rules", len(ruleIDs))
	}
}
