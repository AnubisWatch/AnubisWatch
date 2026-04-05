package core

import (
	"strings"
	"sync"
	"time"
)

// Verdict is the alert decision — the judgment pronounced upon a soul.
type Verdict struct {
	ID             string        `json:"id"`
	WorkspaceID    string        `json:"workspace_id"`
	SoulID         string        `json:"soul_id"`
	RuleID         string        `json:"rule_id"`
	Severity       Severity      `json:"severity"`
	Status         VerdictStatus `json:"status"`
	Message        string        `json:"message"`
	FiredAt        time.Time     `json:"fired_at"`
	AcknowledgedAt *time.Time    `json:"acknowledged_at,omitempty"`
	AcknowledgedBy string        `json:"acknowledged_by,omitempty"`
	ResolvedAt     *time.Time    `json:"resolved_at,omitempty"`
	Judgments      []string      `json:"judgments"` // judgment IDs that caused this
}

// Severity represents alert severity levels
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
	SeverityInfo     Severity = "info"
)

// VerdictStatus represents the current state of a verdict
type VerdictStatus string

const (
	VerdictActive       VerdictStatus = "active"
	VerdictAcknowledged VerdictStatus = "acknowledged"
	VerdictResolved     VerdictStatus = "resolved"
)

// AlertRule defines when and how to fire a verdict
type AlertRule struct {
	ID          string            `json:"id" yaml:"id"`
	Name        string            `json:"name" yaml:"name"`
	Enabled     bool              `json:"enabled" yaml:"enabled"`
	Scope       RuleScope         `json:"scope" yaml:"scope"`
	Conditions  []AlertCondition  `json:"conditions" yaml:"conditions"`
	Channels    []string          `json:"channels" yaml:"channels"`
	Cooldown    Duration          `json:"cooldown" yaml:"cooldown"`
	AutoResolve bool              `json:"auto_resolve" yaml:"auto_resolve"`
	Escalation  *EscalationPolicy `json:"escalation,omitempty" yaml:"escalation,omitempty"`
	CreatedAt   time.Time         `json:"created_at" yaml:"-"`
}

// RuleScope defines what souls the rule applies to
type RuleScope struct {
	Type       string   `json:"type" yaml:"type"` // all, tag, type, specific
	Tags       []string `json:"tags" yaml:"tags"`
	SoulTypes  []string `json:"soul_types" yaml:"soul_types"`
	SoulIDs    []string `json:"soul_ids" yaml:"soul_ids"`
	Workspaces []string `json:"workspaces" yaml:"workspaces"`
}

// AlertCondition defines the trigger condition for an alert
type AlertCondition struct {
	Type      string   `json:"type" yaml:"type"` // consecutive_failures, threshold, percentage, anomaly, compound, status_change, status_for, failure_rate
	Threshold int      `json:"threshold" yaml:"threshold"`
	Metric    string   `json:"metric" yaml:"metric"`
	Operator  string   `json:"operator" yaml:"operator"` // >, <, ==, >=, <=, !=
	Value     any      `json:"value" yaml:"value"`
	Window    Duration `json:"window" yaml:"window"`
	// Additional fields for status-based conditions
	From     string   `json:"from" yaml:"from"`         // for status_change
	To       string   `json:"to" yaml:"to"`             // for status_change
	Status   string   `json:"status" yaml:"status"`     // for status_for
	Duration Duration `json:"duration" yaml:"duration"` // for status_for
}

// ChannelConfig defines an alert notification channel
type ChannelConfig struct {
	Name      string           `json:"name" yaml:"name"`
	Type      string           `json:"type" yaml:"type"` // webhook, slack, discord, telegram, email, pagerduty, opsgenie, sms, ntfy
	Webhook   *WebhookConfig   `json:"webhook,omitempty" yaml:"webhook,omitempty"`
	Slack     *SlackConfig     `json:"slack,omitempty" yaml:"slack,omitempty"`
	Discord   *DiscordConfig   `json:"discord,omitempty" yaml:"discord,omitempty"`
	Telegram  *TelegramConfig  `json:"telegram,omitempty" yaml:"telegram,omitempty"`
	Email     *EmailConfig     `json:"email,omitempty" yaml:"email,omitempty"`
	PagerDuty *PagerDutyConfig `json:"pagerduty,omitempty" yaml:"pagerduty,omitempty"`
	OpsGenie  *OpsGenieConfig  `json:"opsgenie,omitempty" yaml:"opsgenie,omitempty"`
	SMS       *SMSConfig       `json:"sms,omitempty" yaml:"sms,omitempty"`
	Ntfy      *NtfyConfig      `json:"ntfy,omitempty" yaml:"ntfy,omitempty"`
}

// WebhookConfig for generic webhook notifications
type WebhookConfig struct {
	URL      string            `json:"url" yaml:"url"`
	Method   string            `json:"method" yaml:"method"`
	Headers  map[string]string `json:"headers" yaml:"headers"`
	Template string            `json:"template" yaml:"template"`
}

// SlackConfig for Slack webhook notifications
type SlackConfig struct {
	WebhookURL        string   `json:"webhook_url" yaml:"webhook_url"`
	Channel           string   `json:"channel" yaml:"channel"`
	Username          string   `json:"username" yaml:"username"`
	IconEmoji         string   `json:"icon_emoji" yaml:"icon_emoji"`
	MentionOnCritical []string `json:"mention_on_critical" yaml:"mention_on_critical"`
}

// DiscordConfig for Discord webhook notifications
type DiscordConfig struct {
	WebhookURL string `json:"webhook_url" yaml:"webhook_url"`
	Username   string `json:"username" yaml:"username"`
	AvatarURL  string `json:"avatar_url" yaml:"avatar_url"`
}

// TelegramConfig for Telegram bot notifications
type TelegramConfig struct {
	BotToken            string `json:"bot_token" yaml:"bot_token"`
	ChatID              string `json:"chat_id" yaml:"chat_id"`
	ParseMode           string `json:"parse_mode" yaml:"parse_mode"`
	DisableNotification bool   `json:"disable_notification" yaml:"disable_notification"`
}

// EmailConfig for SMTP email notifications
type EmailConfig struct {
	SMTPHost        string   `json:"smtp_host" yaml:"smtp_host"`
	SMTPPort        int      `json:"smtp_port" yaml:"smtp_port"`
	StartTLS        bool     `json:"starttls" yaml:"starttls"`
	Username        string   `json:"username" yaml:"username"`
	Password        string   `json:"password" yaml:"password"`
	From            string   `json:"from" yaml:"from"`
	To              []string `json:"to" yaml:"to"`
	SubjectTemplate string   `json:"subject_template" yaml:"subject_template"`
}

// PagerDutyConfig for PagerDuty integration
type PagerDutyConfig struct {
	IntegrationKey string            `json:"integration_key" yaml:"integration_key"`
	SeverityMap    map[string]string `json:"severity_map" yaml:"severity_map"`
	AutoResolve    bool              `json:"auto_resolve" yaml:"auto_resolve"`
}

// OpsGenieConfig for OpsGenie integration
type OpsGenieConfig struct {
	APIKey      string            `json:"api_key" yaml:"api_key"`
	PriorityMap map[string]string `json:"priority_map" yaml:"priority_map"`
	Tags        []string          `json:"tags" yaml:"tags"`
	AutoClose   bool              `json:"auto_close" yaml:"auto_close"`
}

// SMSConfig for SMS notifications via Twilio/Vonage
type SMSConfig struct {
	Provider   string   `json:"provider" yaml:"provider"` // twilio, vonage
	AccountSID string   `json:"account_sid" yaml:"account_sid"`
	AuthToken  string   `json:"auth_token" yaml:"auth_token"`
	From       string   `json:"from" yaml:"from"`
	To         []string `json:"to" yaml:"to"`
	Template   string   `json:"template" yaml:"template"`
}

// NtfyConfig for Ntfy.sh notifications
type NtfyConfig struct {
	Server      string            `json:"server" yaml:"server"`
	Topic       string            `json:"topic" yaml:"topic"`
	PriorityMap map[string]string `json:"priority_map" yaml:"priority_map"`
	Auth        struct {
		Username string `json:"username" yaml:"username"`
		Password string `json:"password" yaml:"password"`
	} `json:"auth" yaml:"auth"`
}

// AlertChannel represents a notification destination
type AlertChannel struct {
	ID          string                 `json:"id" yaml:"id"`
	Name        string                 `json:"name" yaml:"name"`
	Type        AlertChannelType       `json:"type" yaml:"type"`
	Enabled     bool                   `json:"enabled" yaml:"enabled"`
	Config      map[string]interface{} `json:"config" yaml:"config"`
	Filters     []AlertFilter          `json:"filters" yaml:"filters"`
	RateLimit   RateLimitConfig        `json:"rate_limit" yaml:"rate_limit"`
	RetryPolicy RetryPolicyConfig      `json:"retry_policy" yaml:"retry_policy"`
	CreatedAt   time.Time              `json:"created_at" yaml:"-"`
	UpdatedAt   time.Time              `json:"updated_at" yaml:"-"`
}

// AlertChannelType identifies the notification mechanism
type AlertChannelType string

const (
	ChannelEmail     AlertChannelType = "email"
	ChannelSlack     AlertChannelType = "slack"
	ChannelDiscord   AlertChannelType = "discord"
	ChannelTelegram  AlertChannelType = "telegram"
	ChannelPagerDuty AlertChannelType = "pagerduty"
	ChannelOpsGenie  AlertChannelType = "opsgenie"
	ChannelNtfy      AlertChannelType = "ntfy"
	ChannelWebHook   AlertChannelType = "webhook"
	ChannelSMS       AlertChannelType = "sms"
	ChannelMCP       AlertChannelType = "mcp"
)

// AlertFilter determines when alerts are sent through a channel
type AlertFilter struct {
	Field    string   `json:"field" yaml:"field"`
	Operator string   `json:"operator" yaml:"operator"`
	Value    string   `json:"value" yaml:"value"`
	Values   []string `json:"values" yaml:"values"`
}

// RateLimitConfig controls alert flooding
type RateLimitConfig struct {
	Enabled     bool     `json:"enabled" yaml:"enabled"`
	MaxAlerts   int      `json:"max_alerts" yaml:"max_alerts"`
	Window      Duration `json:"window" yaml:"window"`
	GroupingKey string   `json:"grouping_key" yaml:"grouping_key"`
}

// RetryPolicyConfig controls failed delivery retries
type RetryPolicyConfig struct {
	MaxRetries  int      `json:"max_retries" yaml:"max_retries"`
	InitialWait Duration `json:"initial_wait" yaml:"initial_wait"`
	MaxWait     Duration `json:"max_wait" yaml:"max_wait"`
	Backoff     string   `json:"backoff" yaml:"backoff"`
}

// AlertEvent represents a single alert notification
type AlertEvent struct {
	ID           string            `json:"id"`
	ChannelID    string            `json:"channel_id"`
	ChannelType  AlertChannelType  `json:"channel_type"`
	SoulID       string            `json:"soul_id"`
	SoulName     string            `json:"soul_name"`
	WorkspaceID  string            `json:"workspace_id"`
	Status       SoulStatus        `json:"status"`
	PrevStatus   SoulStatus        `json:"prev_status"`
	Judgment     *Judgment         `json:"judgment"`
	Message      string            `json:"message"`
	Details      map[string]string `json:"details"`
	Severity     Severity          `json:"severity"`
	Timestamp    time.Time         `json:"timestamp"`
	Acknowledged bool              `json:"acknowledged"`
	Resolved     bool              `json:"resolved"`
	AckedAt      *time.Time        `json:"acked_at,omitempty"`
	ResolvedAt   *time.Time        `json:"resolved_at,omitempty"`
}

// AlertHistory tracks sent alerts for deduplication and rate limiting
type AlertHistory struct {
	Mu      sync.RWMutex
	Entries map[string]*AlertHistoryEntry
}

// AlertHistoryEntry tracks when alerts were sent
type AlertHistoryEntry struct {
	Key        string     `json:"key"`
	ChannelID  string     `json:"channel_id"`
	Count      int        `json:"count"`
	FirstSent  time.Time  `json:"first_sent"`
	LastSent   time.Time  `json:"last_sent"`
	SoulStatus SoulStatus `json:"soul_status"`
}

// NotificationResult tracks delivery status
type NotificationResult struct {
	Success   bool          `json:"success"`
	Error     string        `json:"error,omitempty"`
	ChannelID string        `json:"channel_id"`
	Timestamp time.Time     `json:"timestamp"`
	Duration  time.Duration `json:"duration"`
	Attempt   int           `json:"attempt"`
}

// AlertManagerStats tracks alert system performance
type AlertManagerStats struct {
	TotalAlerts        uint64    `json:"total_alerts"`
	SentAlerts         uint64    `json:"sent_alerts"`
	FailedAlerts       uint64    `json:"failed_alerts"`
	AcknowledgedAlerts uint64    `json:"acknowledged_alerts"`
	ResolvedAlerts     uint64    `json:"resolved_alerts"`
	RateLimitedAlerts  uint64    `json:"rate_limited_alerts"`
	FilteredAlerts     uint64    `json:"filtered_alerts"`
	ActiveIncidents    int       `json:"active_incidents"`
	LastAlertTime      time.Time `json:"last_alert_time"`
}

// Incident represents an active or resolved alert incident
type Incident struct {
	ID              string         `json:"id"`
	RuleID          string         `json:"rule_id"`
	SoulID          string         `json:"soul_id"`
	WorkspaceID     string         `json:"workspace_id"`
	Status          IncidentStatus `json:"status"`
	Severity        Severity       `json:"severity"`
	StartedAt       time.Time      `json:"started_at"`
	AckedAt         *time.Time     `json:"acked_at,omitempty"`
	ResolvedAt      *time.Time     `json:"resolved_at,omitempty"`
	AckedBy         string         `json:"acked_by,omitempty"`
	ResolvedBy      string         `json:"resolved_by,omitempty"`
	Notes           []IncidentNote `json:"notes"`
	Events          []AlertEvent   `json:"events"`
	EscalationLevel int            `json:"escalation_level"`
	LastEscalatedAt *time.Time     `json:"last_escalated_at,omitempty"`
}

// IncidentNote is a user annotation on an incident
type IncidentNote struct {
	Author    string    `json:"author"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// ShouldNotify determines if an alert should be sent based on filters
func (c *AlertChannel) ShouldNotify(event *AlertEvent) bool {
	if !c.Enabled {
		return false
	}

	for _, filter := range c.Filters {
		if !filter.Matches(event) {
			return false
		}
	}

	return true
}

// Matches checks if an event matches the filter
func (f *AlertFilter) Matches(event *AlertEvent) bool {
	value := ""
	switch f.Field {
	case "status":
		value = string(event.Status)
	case "type":
		value = string(event.ChannelType)
	case "severity":
		value = string(event.Severity)
	case "soul_id":
		value = event.SoulID
	default:
		if event.Details != nil {
			value = event.Details[f.Field]
		}
	}

	switch f.Operator {
	case "eq":
		return value == f.Value
	case "ne":
		return value != f.Value
	case "in":
		for _, v := range f.Values {
			if value == v {
				return true
			}
		}
		return false
	case "not_in":
		for _, v := range f.Values {
			if value == v {
				return false
			}
		}
		return true
	case "contains":
		return strings.Contains(value, f.Value)
	default:
		return true
	}
}

// Validate checks if the alert channel configuration is valid
func (c *AlertChannel) Validate() error {
	if c.ID == "" {
		return &ValidationError{Field: "id", Message: "channel ID is required"}
	}
	if c.Name == "" {
		return &ValidationError{Field: "name", Message: "channel name is required"}
	}
	if c.Type == "" {
		return &ValidationError{Field: "type", Message: "channel type is required"}
	}
	return nil
}
