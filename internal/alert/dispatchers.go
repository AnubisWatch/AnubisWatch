package alert

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// SlackDispatcher sends alerts to Slack via webhooks
type SlackDispatcher struct {
	logger  *slog.Logger
	client  *http.Client
}

// Send sends an alert to Slack
func (d *SlackDispatcher) Send(ctx context.Context, event *core.AlertEvent, channel *core.AlertChannel) error {
	webhookURL, _ := channel.Config["webhook_url"].(string)
	if webhookURL == "" {
		return fmt.Errorf("missing webhook_url")
	}

	// Build Slack message
	color := d.getColor(event.Severity, event.Status)
	emoji := d.getEmoji(event.Status)
	title := fmt.Sprintf("%s %s: %s", emoji, event.Status, event.SoulName)

	fields := []slackField{
		{Title: "Soul", Value: event.SoulName, Short: true},
		{Title: "Status", Value: string(event.Status), Short: true},
		{Title: "Time", Value: event.Timestamp.Format(time.RFC3339), Short: true},
	}

	for key, value := range event.Details {
		fields = append(fields, slackField{
			Title: key,
			Value: value,
			Short: true,
		})
	}

	attachment := slackAttachment{
		Color:     color,
		Title:     title,
		Text:      event.Message,
		Fields:    fields,
		Timestamp: event.Timestamp.Unix(),
		Footer:    "AnubisWatch",
	}

	payload := slackPayload{
		Text:        fmt.Sprintf("Soul %s is %s", event.SoulName, event.Status),
		Attachments: []slackAttachment{attachment},
	}

	if username, ok := channel.Config["username"].(string); ok {
		payload.Username = username
	}
	if icon, ok := channel.Config["icon_emoji"].(string); ok {
		payload.IconEmoji = icon
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := d.getClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

// Validate validates Slack configuration
func (d *SlackDispatcher) Validate(config map[string]interface{}) error {
	if _, ok := config["webhook_url"]; !ok {
		return fmt.Errorf("webhook_url is required")
	}
	return nil
}

func (d *SlackDispatcher) getClient() *http.Client {
	if d.client == nil {
		d.client = &http.Client{Timeout: 30 * time.Second}
	}
	return d.client
}

func (d *SlackDispatcher) getColor(severity core.Severity, status core.SoulStatus) string {
	if status == core.SoulAlive {
		return "good"
	}
	switch severity {
	case core.SeverityCritical:
		return "danger"
	case core.SeverityWarning:
		return "warning"
	default:
		return "#439FE0"
	}
}

func (d *SlackDispatcher) getEmoji(status core.SoulStatus) string {
	switch status {
	case core.SoulAlive:
		return "✅"
	case core.SoulDead:
		return "🔴"
	case core.SoulDegraded:
		return "⚠️"
	default:
		return "ℹ️"
	}
}

// Slack types

type slackPayload struct {
	Text        string            `json:"text"`
	Username    string            `json:"username,omitempty"`
	IconEmoji   string            `json:"icon_emoji,omitempty"`
	Attachments []slackAttachment `json:"attachments"`
}

type slackAttachment struct {
	Color     string       `json:"color"`
	Title     string       `json:"title"`
	Text      string       `json:"text"`
	Fields    []slackField `json:"fields"`
	Timestamp int64        `json:"ts"`
	Footer    string       `json:"footer"`
}

type slackField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

// DiscordDispatcher sends alerts to Discord via webhooks
type DiscordDispatcher struct {
	logger *slog.Logger
	client *http.Client
}

// Send sends an alert to Discord
func (d *DiscordDispatcher) Send(ctx context.Context, event *core.AlertEvent, channel *core.AlertChannel) error {
	webhookURL, _ := channel.Config["webhook_url"].(string)
	if webhookURL == "" {
		return fmt.Errorf("missing webhook_url")
	}

	// Build Discord embed
	color := d.getColor(event.Severity, event.Status)
	title := fmt.Sprintf("%s is %s", event.SoulName, event.Status)

	fields := []discordField{
		{Name: "Soul", Value: event.SoulName, Inline: true},
		{Name: "Status", Value: string(event.Status), Inline: true},
	}

	for key, value := range event.Details {
		fields = append(fields, discordField{
			Name:   key,
			Value:  value,
			Inline: true,
		})
	}

	embed := discordEmbed{
		Title:       title,
		Description: event.Message,
		Color:       color,
		Fields:      fields,
		Timestamp:   event.Timestamp.Format(time.RFC3339),
		Footer: &discordFooter{
			Text: "AnubisWatch",
		},
	}

	payload := discordPayload{
		Content: fmt.Sprintf("Soul status changed: **%s**", event.Status),
		Embeds:  []discordEmbed{embed},
	}

	if username, ok := channel.Config["username"].(string); ok {
		payload.Username = username
	}
	if avatar, ok := channel.Config["avatar_url"].(string); ok {
		payload.AvatarURL = avatar
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := d.getClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

// Validate validates Discord configuration
func (d *DiscordDispatcher) Validate(config map[string]interface{}) error {
	if _, ok := config["webhook_url"]; !ok {
		return fmt.Errorf("webhook_url is required")
	}
	return nil
}

func (d *DiscordDispatcher) getClient() *http.Client {
	if d.client == nil {
		d.client = &http.Client{Timeout: 30 * time.Second}
	}
	return d.client
}

func (d *DiscordDispatcher) getColor(severity core.Severity, status core.SoulStatus) int {
	if status == core.SoulAlive {
		return 0x00FF00 // Green
	}
	switch severity {
	case core.SeverityCritical:
		return 0xFF0000 // Red
	case core.SeverityWarning:
		return 0xFFA500 // Orange
	default:
		return 0x439FE0 // Blue
	}
}

// Discord types

type discordPayload struct {
	Content   string          `json:"content"`
	Username  string          `json:"username,omitempty"`
	AvatarURL string          `json:"avatar_url,omitempty"`
	Embeds    []discordEmbed  `json:"embeds"`
}

type discordEmbed struct {
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Color       int            `json:"color"`
	Fields      []discordField `json:"fields"`
	Timestamp   string         `json:"timestamp"`
	Footer      *discordFooter `json:"footer,omitempty"`
}

type discordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type discordFooter struct {
	Text string `json:"text"`
}

// EmailDispatcher sends alerts via SMTP
type EmailDispatcher struct {
	logger *slog.Logger
}

// Send sends an email alert
func (d *EmailDispatcher) Send(ctx context.Context, event *core.AlertEvent, channel *core.AlertChannel) error {
	smtpHost, _ := channel.Config["smtp_host"].(string)
	smtpPort := 587
	if port, ok := channel.Config["smtp_port"].(float64); ok {
		smtpPort = int(port)
	}
	username, _ := channel.Config["username"].(string)
	password, _ := channel.Config["password"].(string)
	from, _ := channel.Config["from"].(string)

	toList, ok := channel.Config["to"].([]interface{})
	if !ok || len(toList) == 0 {
		return fmt.Errorf("no recipients configured")
	}

	// Build email body
	subject := fmt.Sprintf("[%s] %s - %s", event.Status, event.SoulName, event.Severity)
	body := d.buildEmailBody(event)

	// Send email
	addr := fmt.Sprintf("%s:%d", smtpHost, smtpPort)
	auth := &plainAuth{
		identity: "",
		username: username,
		password: password,
		host:     smtpHost,
	}

	recipients := make([]string, 0, len(toList))
	for _, r := range toList {
		if email, ok := r.(string); ok {
			recipients = append(recipients, email)
		}
	}

	msg := []byte(fmt.Sprintf("To: %s\r\nSubject: %s\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		strings.Join(recipients, ", "), subject, body))

	if err := smtp.SendMail(addr, auth, from, recipients, msg); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

// Validate validates email configuration
func (d *EmailDispatcher) Validate(config map[string]interface{}) error {
	if _, ok := config["smtp_host"]; !ok {
		return fmt.Errorf("smtp_host is required")
	}
	if _, ok := config["to"]; !ok {
		return fmt.Errorf("to is required")
	}
	return nil
}

func (d *EmailDispatcher) buildEmailBody(event *core.AlertEvent) string {
	var statusColor string
	switch event.Status {
	case core.SoulAlive:
		statusColor = "#28a745"
	case core.SoulDead:
		statusColor = "#dc3545"
	case core.SoulDegraded:
		statusColor = "#ffc107"
	default:
		statusColor = "#6c757d"
	}

	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>AnubisWatch Alert</title></head>
<body style="font-family: Arial, sans-serif; line-height: 1.6; color: #333;">
<h2 style="color: %s;">%s Alert</h2>
<p><strong>Soul:</strong> %s</p>
<p><strong>Status:</strong> %s</p>
<p><strong>Time:</strong> %s</p>
<p><strong>Message:</strong> %s</p>
<h3>Details:</h3>
<ul>`, statusColor, event.Status, event.SoulName, event.Status, event.Timestamp.Format(time.RFC3339), event.Message)

	for key, value := range event.Details {
		html += fmt.Sprintf("<li><strong>%s:</strong> %s</li>", key, value)
	}

	html += `</ul>
<hr>
<p style="font-size: 12px; color: #666;">This alert was sent by AnubisWatch - The Judgment Never Sleeps</p>
</body>
</html>`

	return html
}

// Simple auth for SMTP
type plainAuth struct {
	identity string
	username string
	password string
	host     string
}

func (a *plainAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	return "PLAIN", []byte(a.identity + "\x00" + a.username + "\x00" + a.password), nil
}

func (a *plainAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	return nil, nil
}

// PagerDutyDispatcher sends alerts to PagerDuty
type PagerDutyDispatcher struct {
	logger *slog.Logger
	client *http.Client
}

// Send sends an alert to PagerDuty
func (d *PagerDutyDispatcher) Send(ctx context.Context, event *core.AlertEvent, channel *core.AlertChannel) error {
	integrationKey, _ := channel.Config["integration_key"].(string)
	if integrationKey == "" {
		return fmt.Errorf("missing integration_key")
	}

	// Build PagerDuty event
	severity := d.mapSeverity(event.Severity)
	action := "trigger"
	if event.Status == core.SoulAlive {
		action = "resolve"
	}

	payload := pagerDutyPayload{
		RoutingKey:  integrationKey,
		EventAction: action,
		DedupKey:    fmt.Sprintf("anubis-%s", event.SoulID),
		Payload: pagerDutyEventPayload{
			Summary:   fmt.Sprintf("%s: %s", event.SoulName, event.Message),
			Severity:  severity,
			Source:    "anubiswatch",
			Component: event.SoulName,
			Group:     string(event.Status),
			Class:     string(event.ChannelType),
			CustomDetails: map[string]interface{}{
				"soul_id":     event.SoulID,
				"status":      event.Status,
				"prev_status": event.PrevStatus,
				"message":     event.Message,
			},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://events.pagerduty.com/v2/enqueue", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := d.getClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

// Validate validates PagerDuty configuration
func (d *PagerDutyDispatcher) Validate(config map[string]interface{}) error {
	if _, ok := config["integration_key"]; !ok {
		return fmt.Errorf("integration_key is required")
	}
	return nil
}

func (d *PagerDutyDispatcher) getClient() *http.Client {
	if d.client == nil {
		d.client = &http.Client{Timeout: 30 * time.Second}
	}
	return d.client
}

func (d *PagerDutyDispatcher) mapSeverity(severity core.Severity) string {
	switch severity {
	case core.SeverityCritical:
		return "critical"
	case core.SeverityWarning:
		return "warning"
	case core.SeverityInfo:
		return "info"
	default:
		return "error"
	}
}

// PagerDuty types
type pagerDutyPayload struct {
	RoutingKey  string               `json:"routing_key"`
	EventAction string               `json:"event_action"`
	DedupKey    string               `json:"dedup_key"`
	Payload     pagerDutyEventPayload `json:"payload"`
}

type pagerDutyEventPayload struct {
	Summary       string                 `json:"summary"`
	Severity      string                 `json:"severity"`
	Source        string                 `json:"source"`
	Component     string                 `json:"component"`
	Group         string                 `json:"group"`
	Class         string                 `json:"class"`
	CustomDetails map[string]interface{} `json:"custom_details"`
}

// OpsGenieDispatcher sends alerts to OpsGenie
type OpsGenieDispatcher struct {
	logger *slog.Logger
	client *http.Client
}

// Send sends an alert to OpsGenie
func (d *OpsGenieDispatcher) Send(ctx context.Context, event *core.AlertEvent, channel *core.AlertChannel) error {
	apiKey, _ := channel.Config["api_key"].(string)
	if apiKey == "" {
		return fmt.Errorf("missing api_key")
	}

	region := "us"
	if r, ok := channel.Config["region"].(string); ok && r != "" {
		region = r
	}

	host := "api.opsgenie.com"
	if region == "eu" {
		host = "api.eu.opsgenie.com"
	}

	// Build alert
	priority := "P3"
	if event.Severity == core.SeverityCritical {
		priority = "P1"
	} else if event.Severity == core.SeverityWarning {
		priority = "P2"
	}

	payload := opsGeniePayload{
		Message:     fmt.Sprintf("%s: %s", event.SoulName, event.Message),
		Description: event.Message,
		Priority:    priority,
		Alias:       fmt.Sprintf("anubis-%s", event.SoulID),
		Details:     event.Details,
		Entity:      event.SoulName,
		Tags:        []string{string(event.Status), string(event.Severity)},
	}

	// Close alert on recovery
	if event.Status == core.SoulAlive {
		return d.closeAlert(ctx, host, apiKey, payload.Alias)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	url := fmt.Sprintf("https://%s/v2/alerts", host)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "GenieKey "+apiKey)

	client := d.getClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

func (d *OpsGenieDispatcher) closeAlert(ctx context.Context, host, apiKey, alias string) error {
	url := fmt.Sprintf("https://%s/v2/alerts/%s/close?identifierType=alias", host, alias)
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "GenieKey "+apiKey)

	client := d.getClient()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// Validate validates OpsGenie configuration
func (d *OpsGenieDispatcher) Validate(config map[string]interface{}) error {
	if _, ok := config["api_key"]; !ok {
		return fmt.Errorf("api_key is required")
	}
	return nil
}

func (d *OpsGenieDispatcher) getClient() *http.Client {
	if d.client == nil {
		d.client = &http.Client{Timeout: 30 * time.Second}
	}
	return d.client
}

// OpsGenie types
type opsGeniePayload struct {
	Message     string            `json:"message"`
	Description string            `json:"description"`
	Priority    string            `json:"priority"`
	Alias       string            `json:"alias"`
	Details     map[string]string `json:"details"`
	Entity      string            `json:"entity"`
	Tags        []string          `json:"tags"`
}

// NtfyDispatcher sends alerts to Ntfy
type NtfyDispatcher struct {
	logger *slog.Logger
	client *http.Client
}

// Send sends an alert to Ntfy
func (d *NtfyDispatcher) Send(ctx context.Context, event *core.AlertEvent, channel *core.AlertChannel) error {
	server, _ := channel.Config["server"].(string)
	if server == "" {
		server = "https://ntfy.sh"
	}
	topic, _ := channel.Config["topic"].(string)
	if topic == "" {
		return fmt.Errorf("missing topic")
	}

	priority := "default"
	if event.Severity == core.SeverityCritical {
		priority = "urgent"
	} else if event.Severity == core.SeverityWarning {
		priority = "high"
	}

	message := fmt.Sprintf("%s: %s", event.SoulName, event.Message)
	url := fmt.Sprintf("%s/%s", server, topic)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader([]byte(message)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Title", fmt.Sprintf("AnubisWatch - %s", event.Status))
	req.Header.Set("Priority", priority)
	req.Header.Set("Tags", fmt.Sprintf("anubiswatch,%s", event.Status))

	if clickURL, ok := channel.Config["click_url"].(string); ok {
		req.Header.Set("Click", clickURL)
	}
	if iconURL, ok := channel.Config["icon_url"].(string); ok {
		req.Header.Set("Icon", iconURL)
	}

	if token, ok := channel.Config["token"].(string); ok && token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := d.getClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

// Validate validates Ntfy configuration
func (d *NtfyDispatcher) Validate(config map[string]interface{}) error {
	if _, ok := config["topic"]; !ok {
		return fmt.Errorf("topic is required")
	}
	return nil
}

func (d *NtfyDispatcher) getClient() *http.Client {
	if d.client == nil {
		d.client = &http.Client{Timeout: 30 * time.Second}
	}
	return d.client
}

// TelegramDispatcher sends alerts to Telegram
type TelegramDispatcher struct {
	logger *slog.Logger
	client *http.Client
}

// Send sends an alert to Telegram
func (d *TelegramDispatcher) Send(ctx context.Context, event *core.AlertEvent, channel *core.AlertChannel) error {
	botToken, _ := channel.Config["bot_token"].(string)
	if botToken == "" {
		return fmt.Errorf("missing bot_token")
	}
	chatID, _ := channel.Config["chat_id"].(string)
	if chatID == "" {
		return fmt.Errorf("missing chat_id")
	}

	// Build message
	emoji := d.getEmoji(event.Status)
	message := fmt.Sprintf("%s *%s Alert*\n\n*Soul:* %s\n*Status:* %s\n*Severity:* %s\n*Time:* %s\n\n*Message:*\n%s",
		emoji,
		event.Status,
		event.SoulName,
		event.Status,
		event.Severity,
		event.Timestamp.Format(time.RFC3339),
		event.Message)

	// Add details
	if len(event.Details) > 0 {
		message += "\n\n*Details:*\n"
		for key, value := range event.Details {
			message += fmt.Sprintf("• *%s:* %s\n", key, value)
		}
	}

	message += "\n_AnubisWatch — The Judgment Never Sleeps_"

	// Build request
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	payload := map[string]string{
		"chat_id":      chatID,
		"text":         message,
		"parse_mode":   "Markdown",
	}

	// Disable notifications for non-critical alerts
	if event.Severity != core.SeverityCritical {
		payload["disable_notification"] = "true"
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := d.getClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

// Validate validates Telegram configuration
func (d *TelegramDispatcher) Validate(config map[string]interface{}) error {
	if _, ok := config["bot_token"]; !ok {
		return fmt.Errorf("bot_token is required")
	}
	if _, ok := config["chat_id"]; !ok {
		return fmt.Errorf("chat_id is required")
	}
	return nil
}

func (d *TelegramDispatcher) getClient() *http.Client {
	if d.client == nil {
		d.client = &http.Client{Timeout: 30 * time.Second}
	}
	return d.client
}

func (d *TelegramDispatcher) getEmoji(status core.SoulStatus) string {
	switch status {
	case core.SoulAlive:
		return "✅"
	case core.SoulDead:
		return "🔴"
	case core.SoulDegraded:
		return "⚠️"
	default:
		return "ℹ️"
	}
}

// SMSDispatcher sends alerts via SMS (Twilio/Vonage)
type SMSDispatcher struct {
	logger *slog.Logger
	client *http.Client
}

// Send sends an SMS alert
func (d *SMSDispatcher) Send(ctx context.Context, event *core.AlertEvent, channel *core.AlertChannel) error {
	provider, _ := channel.Config["provider"].(string)
	if provider == "" {
		provider = "twilio"
	}

	switch provider {
	case "twilio":
		return d.sendTwilio(ctx, event, channel)
	case "vonage":
		return d.sendVonage(ctx, event, channel)
	default:
		return d.sendTwilio(ctx, event, channel)
	}
}

func (d *SMSDispatcher) sendTwilio(ctx context.Context, event *core.AlertEvent, channel *core.AlertChannel) error {
	accountSID, _ := channel.Config["account_sid"].(string)
	authToken, _ := channel.Config["auth_token"].(string)
	from, _ := channel.Config["from"].(string)

	if accountSID == "" || authToken == "" || from == "" {
		return fmt.Errorf("missing twilio credentials")
	}

	toList, ok := channel.Config["to"].([]interface{})
	if !ok || len(toList) == 0 {
		return fmt.Errorf("no recipients configured")
	}

	template, _ := channel.Config["template"].(string)
	if template == "" {
		template = "AnubisWatch: {{.SoulName}} is {{.Status}} - {{.Message}}"
	}

	// Simple template replacement
	message := template
	message = strings.ReplaceAll(message, "{{.SoulName}}", event.SoulName)
	message = strings.ReplaceAll(message, "{{.Status}}", string(event.Status))
	message = strings.ReplaceAll(message, "{{.Message}}", event.Message)
	message = strings.ReplaceAll(message, "{{.Severity}}", string(event.Severity))
	message = strings.ReplaceAll(message, "{{.Time}}", event.Timestamp.Format(time.RFC3339))

	twilioURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", accountSID)

	data := make(url.Values)
	data.Set("From", from)
	data.Set("Body", message)
	for _, r := range toList {
		if to, ok := r.(string); ok {
			data.Add("To", to)
		}
	}

	req, err := http.NewRequestWithContext(ctx, "POST", twilioURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(accountSID, authToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := d.getClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

func (d *SMSDispatcher) sendVonage(ctx context.Context, event *core.AlertEvent, channel *core.AlertChannel) error {
	apiKey, _ := channel.Config["api_key"].(string)
	apiSecret, _ := channel.Config["api_secret"].(string)
	from, _ := channel.Config["from"].(string)

	if apiKey == "" || apiSecret == "" || from == "" {
		return fmt.Errorf("missing vonage credentials")
	}

	toList, ok := channel.Config["to"].([]interface{})
	if !ok || len(toList) == 0 {
		return fmt.Errorf("no recipients configured")
	}

	template, _ := channel.Config["template"].(string)
	if template == "" {
		template = "AnubisWatch: {{.SoulName}} is {{.Status}} - {{.Message}}"
	}

	message := template
	message = strings.ReplaceAll(message, "{{.SoulName}}", event.SoulName)
	message = strings.ReplaceAll(message, "{{.Status}}", string(event.Status))
	message = strings.ReplaceAll(message, "{{.Message}}", event.Message)

	url := "https://rest.nexmo.com/sms/json"

	for _, r := range toList {
		if to, ok := r.(string); ok {
			payload := map[string]string{
				"api_key":    apiKey,
				"api_secret": apiSecret,
				"from":       from,
				"to":         to,
				"text":       message,
			}

			data, _ := json.Marshal(payload)
			req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
			req.Header.Set("Content-Type", "application/json")

			client := d.getClient()
			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
		}
	}

	return nil
}

// Validate validates SMS configuration
func (d *SMSDispatcher) Validate(config map[string]interface{}) error {
	provider, _ := config["provider"].(string)
	if provider == "" {
		provider = "twilio"
	}

	if provider == "twilio" {
		if _, ok := config["account_sid"]; !ok {
			return fmt.Errorf("account_sid is required for twilio")
		}
		if _, ok := config["auth_token"]; !ok {
			return fmt.Errorf("auth_token is required for twilio")
		}
	} else if provider == "vonage" {
		if _, ok := config["api_key"]; !ok {
			return fmt.Errorf("api_key is required for vonage")
		}
		if _, ok := config["api_secret"]; !ok {
			return fmt.Errorf("api_secret is required for vonage")
		}
	}

	if _, ok := config["to"]; !ok {
		return fmt.Errorf("to is required")
	}

	return nil
}

func (d *SMSDispatcher) getClient() *http.Client {
	if d.client == nil {
		d.client = &http.Client{Timeout: 30 * time.Second}
	}
	return d.client
}

// WebHookDispatcher sends alerts to generic webhooks
type WebHookDispatcher struct {
	logger *slog.Logger
	client *http.Client
}

// Send sends an alert to a webhook
func (d *WebHookDispatcher) Send(ctx context.Context, event *core.AlertEvent, channel *core.AlertChannel) error {
	webhookURL, _ := channel.Config["url"].(string)
	if webhookURL == "" {
		return fmt.Errorf("missing url")
	}

	method := "POST"
	if m, ok := channel.Config["method"].(string); ok && m != "" {
		method = m
	}

	// Build payload
	payload := map[string]interface{}{
		"id":           event.ID,
		"soul_id":      event.SoulID,
		"soul_name":    event.SoulName,
		"status":       event.Status,
		"prev_status":  event.PrevStatus,
		"severity":     event.Severity,
		"message":      event.Message,
		"details":      event.Details,
		"timestamp":    event.Timestamp,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Apply template if provided
	if template, ok := channel.Config["template"].(string); ok && template != "" {
		// Simple template replacement
		templated := template
		templated = strings.ReplaceAll(templated, "{{.SoulName}}", event.SoulName)
		templated = strings.ReplaceAll(templated, "{{.Status}}", string(event.Status))
		templated = strings.ReplaceAll(templated, "{{.Message}}", event.Message)
		data = []byte(templated)
	}

	req, err := http.NewRequestWithContext(ctx, method, webhookURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "AnubisWatch/1.0")

	// Add custom headers
	if headers, ok := channel.Config["headers"].(map[string]interface{}); ok {
		for k, v := range headers {
			if val, ok := v.(string); ok {
				req.Header.Set(k, val)
			}
		}
	}

	// Add HMAC signature if secret is configured
	if secret, ok := channel.Config["secret"].(string); ok && secret != "" {
		sig := hmacSha256(data, secret)
		req.Header.Set("X-Anubis-Signature", sig)
	}

	client := d.getClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

// Validate validates webhook configuration
func (d *WebHookDispatcher) Validate(config map[string]interface{}) error {
	if _, ok := config["url"]; !ok {
		return fmt.Errorf("url is required")
	}
	return nil
}

func (d *WebHookDispatcher) getClient() *http.Client {
	if d.client == nil {
		d.client = &http.Client{Timeout: 30 * time.Second}
	}
	return d.client
}

func hmacSha256(data []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
