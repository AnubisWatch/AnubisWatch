package alert

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
)

// sharedHTTPClient is a concurrency-safe, lazily-initialized HTTP client
// used by all dispatchers. This eliminates the race condition in the
// previous per-dispatcher nil-check-then-initialize pattern.
var sharedHTTPClient struct {
	once sync.Once
	c    *http.Client
}

func getHTTPClient() *http.Client {
	sharedHTTPClient.once.Do(func() {
		sharedHTTPClient.c = &http.Client{
			Timeout: 30 * time.Second,
			// Disable redirects to prevent SSRF via HTTP redirects
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	})
	return sharedHTTPClient.c
}

// redactURL renders a request URL without the parts that can carry a credential:
// a Slack/Discord webhook URL is the secret itself, and a Telegram bot token lives
// in the path. The scheme and host are kept so a failure stays diagnosable.
func redactURL(u *url.URL) string {
	if u == nil || u.Host == "" {
		return "<redacted-url>"
	}
	return u.Scheme + "://" + u.Host + "/<redacted>"
}

// redactedRequestError renders a transport failure without leaking the request URL.
// Go's url.Error prints the full URL, and the alert manager logs the error it gets
// back ("Failed to send alert"), so a credential embedded in the URL would land in
// the log stream.
func redactedRequestError(err error, req *http.Request) error {
	if req == nil || req.URL == nil {
		return fmt.Errorf("request failed: %w", err)
	}
	redacted := redactURL(req.URL)
	return fmt.Errorf("failed to send request to %s: %s", redacted,
		strings.ReplaceAll(err.Error(), req.URL.String(), redacted))
}

// closeResponseBody drains and closes the response body to ensure connection reuse
// and prevent resource leaks. This helper should be used instead of defer resp.Body.Close()
func closeResponseBody(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		// Best-effort drain so the connection can be reused; the body is
		// closed regardless and a partial drain is not actionable.
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

// SlackDispatcher sends alerts to Slack via webhooks
type SlackDispatcher struct {
	logger *slog.Logger
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

	data, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := getHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return redactedRequestError(err, req)
	}
	defer closeResponseBody(resp)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

// Validate validates Slack configuration
func (d *SlackDispatcher) Validate(config map[string]any) error {
	webhookURL, ok := config["webhook_url"]
	if !ok {
		return fmt.Errorf("webhook_url is required")
	}
	if s, ok := webhookURL.(string); ok {
		if err := probe.ValidateTarget(s); err != nil {
			return fmt.Errorf("webhook_url SSRF validation failed: %w", err)
		}
	}
	return nil
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

	data, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := getHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return redactedRequestError(err, req)
	}
	defer closeResponseBody(resp)

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

// Validate validates Discord configuration
func (d *DiscordDispatcher) Validate(config map[string]any) error {
	webhookURL, ok := config["webhook_url"]
	if !ok {
		return fmt.Errorf("webhook_url is required")
	}
	if s, ok := webhookURL.(string); ok {
		if err := probe.ValidateTarget(s); err != nil {
			return fmt.Errorf("webhook_url SSRF validation failed: %w", err)
		}
	}
	return nil
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
	Content   string         `json:"content"`
	Username  string         `json:"username,omitempty"`
	AvatarURL string         `json:"avatar_url,omitempty"`
	Embeds    []discordEmbed `json:"embeds"`
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

// Send sends an email alert.
//
// Channel config: smtp_host, smtp_port (default 587), from, to (list),
// optional username/password, and tls: "auto" (default; implicit TLS on port
// 465, otherwise STARTTLS when offered), "implicit", "starttls" (required),
// or "none".
func (d *EmailDispatcher) Send(ctx context.Context, event *core.AlertEvent, channel *core.AlertChannel) error {
	smtpHost, _ := channel.Config["smtp_host"].(string)
	smtpPort := 587
	switch port := channel.Config["smtp_port"].(type) {
	case float64:
		smtpPort = int(port)
	case int:
		smtpPort = port
	case string:
		if n, err := strconv.Atoi(port); err == nil {
			smtpPort = n
		}
	}
	username, _ := channel.Config["username"].(string)
	password, _ := channel.Config["password"].(string)
	from, _ := channel.Config["from"].(string)
	tlsMode, _ := channel.Config["tls"].(string)

	toList, ok := channel.Config["to"].([]any)
	if !ok || len(toList) == 0 {
		return fmt.Errorf("no recipients configured")
	}
	recipients := make([]string, 0, len(toList))
	for _, r := range toList {
		if email, ok := r.(string); ok && strings.TrimSpace(email) != "" {
			recipients = append(recipients, strings.TrimSpace(email))
		}
	}
	if len(recipients) == 0 {
		return fmt.Errorf("no recipients configured")
	}
	if from == "" {
		from = "anubis@" + smtpHost
	}
	for _, addr := range append([]string{from}, recipients...) {
		if strings.ContainsAny(addr, "\r\n") {
			return fmt.Errorf("invalid email address %q", addr)
		}
	}

	subject := fmt.Sprintf("[%s] %s - %s", event.Status, event.SoulName, event.Severity)
	msg := buildEmailMessage(from, recipients, subject, d.buildEmailBody(event), time.Now())

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	if err := sendSMTP(ctx, smtpHost, smtpPort, tlsMode, username, password, from, recipients, msg); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	return nil
}

// buildEmailMessage renders an RFC 5322 message. Header values are
// RFC 2047-encoded, which also neutralises CR/LF in soul names.
func buildEmailMessage(from string, to []string, subject, htmlBody string, now time.Time) []byte {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	b.WriteString("Date: " + now.UTC().Format(time.RFC1123Z) + "\r\n")
	domain := "anubis.local"
	if at := strings.LastIndex(from, "@"); at >= 0 && at < len(from)-1 {
		domain = strings.Trim(from[at+1:], "<> ")
	}
	b.WriteString("Message-ID: <" + core.GenerateID() + "@" + domain + ">\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(strings.ReplaceAll(htmlBody, "\r\n", "\n"), "\n", "\r\n"))
	return []byte(b.String())
}

// sendSMTP delivers msg honouring ctx for dial and I/O.
func sendSMTP(ctx context.Context, host string, port int, tlsMode, username, password, from string, to []string, msg []byte) error {
	if host == "" {
		return fmt.Errorf("smtp_host is empty")
	}
	tlsMode = strings.ToLower(tlsMode)
	if tlsMode == "" || tlsMode == "auto" {
		tlsMode = "auto"
		if port == 465 {
			tlsMode = "implicit"
		}
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}

	dialer := &net.Dialer{}
	var conn net.Conn
	var err error
	if tlsMode == "implicit" {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsConfig}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer client.Close()

	if tlsMode == "auto" || tlsMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("STARTTLS: %w", err)
			}
		} else if tlsMode == "starttls" {
			return fmt.Errorf("server %s does not offer STARTTLS", host)
		}
	}
	if username != "" {
		if ok, _ := client.Extension("AUTH"); !ok {
			return fmt.Errorf("server %s does not support AUTH but credentials are configured", host)
		}
		if err := client.Auth(&plainAuth{username: username, password: password, host: host}); err != nil {
			return err
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// Validate validates email configuration
func (d *EmailDispatcher) Validate(config map[string]any) error {
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

	var b strings.Builder
	fmt.Fprintf(&b, `<!DOCTYPE html>
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
		b.WriteString("<li><strong>")
		b.WriteString(key)
		b.WriteString(":</strong> ")
		b.WriteString(value)
		b.WriteString("</li>")
	}

	b.WriteString(`</ul>
<hr>
<p style="font-size: 12px; color: #666;">This alert was sent by AnubisWatch - The Judgment Never Sleeps</p>
</body>
</html>`)

	return b.String()
}

// Simple auth for SMTP
type plainAuth struct {
	identity string
	username string
	password string
	host     string
}

// isLocalSMTPHost mirrors net/smtp's isLocalhost: the standard library allows PLAIN
// credentials on a local relay without TLS.
func isLocalSMTPHost(name string) bool {
	switch name {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// Start answers the SMTP AUTH PLAIN challenge. net/smtp's PlainAuth refuses to send
// credentials over a connection that is neither TLS-protected nor local, and
// refuses a server presenting a name other than the configured host. Without those
// checks EmailDispatcher.Send handed the operator's SMTP username and password to
// smtp.SendMail, which transmits them in the clear whenever the server advertises
// AUTH but not STARTTLS.
//
// A nil ServerInfo means the caller did not describe the connection; net/smtp always
// supplies one, so the checks apply on every real send.
func (a *plainAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if server != nil {
		if !server.TLS && !isLocalSMTPHost(server.Name) {
			return "", nil, fmt.Errorf("refusing to send SMTP credentials over an unencrypted connection to %q", server.Name)
		}
		if server.Name != a.host {
			return "", nil, fmt.Errorf("refusing to send SMTP credentials to %q, which is not the configured host %q", server.Name, a.host)
		}
	}
	return "PLAIN", []byte(a.identity + "\x00" + a.username + "\x00" + a.password), nil
}

func (a *plainAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	return nil, nil
}

// PagerDutyDispatcher sends alerts to PagerDuty
type PagerDutyDispatcher struct {
	logger *slog.Logger
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
			CustomDetails: map[string]any{
				"soul_id":     event.SoulID,
				"status":      event.Status,
				"prev_status": event.PrevStatus,
				"message":     event.Message,
			},
		},
	}

	data, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://events.pagerduty.com/v2/enqueue", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := getHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return redactedRequestError(err, req)
	}
	defer closeResponseBody(resp)

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

// Validate validates PagerDuty configuration
func (d *PagerDutyDispatcher) Validate(config map[string]any) error {
	if _, ok := config["integration_key"]; !ok {
		return fmt.Errorf("integration_key is required")
	}
	return nil
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
	RoutingKey  string                `json:"routing_key"`
	EventAction string                `json:"event_action"`
	DedupKey    string                `json:"dedup_key"`
	Payload     pagerDutyEventPayload `json:"payload"`
}

type pagerDutyEventPayload struct {
	Summary       string         `json:"summary"`
	Severity      string         `json:"severity"`
	Source        string         `json:"source"`
	Component     string         `json:"component"`
	Group         string         `json:"group"`
	Class         string         `json:"class"`
	CustomDetails map[string]any `json:"custom_details"`
}

// OpsGenieDispatcher sends alerts to OpsGenie
type OpsGenieDispatcher struct {
	logger *slog.Logger
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
	switch event.Severity {
	case core.SeverityCritical:
		priority = "P1"
	case core.SeverityWarning:
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

	data, _ := json.Marshal(payload)

	url := fmt.Sprintf("https://%s/v2/alerts", host)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "GenieKey "+apiKey)

	client := getHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return redactedRequestError(err, req)
	}
	defer closeResponseBody(resp)

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

	client := getHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer closeResponseBody(resp)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

// Validate validates OpsGenie configuration
func (d *OpsGenieDispatcher) Validate(config map[string]any) error {
	if _, ok := config["api_key"]; !ok {
		return fmt.Errorf("api_key is required")
	}
	return nil
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
	switch event.Severity {
	case core.SeverityCritical:
		priority = "urgent"
	case core.SeverityWarning:
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

	client := getHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return redactedRequestError(err, req)
	}
	defer closeResponseBody(resp)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

// Validate validates Ntfy configuration
func (d *NtfyDispatcher) Validate(config map[string]any) error {
	server, ok := config["server"]
	if ok {
		if s, ok := server.(string); ok && s != "" {
			if err := probe.ValidateTarget(s); err != nil {
				return fmt.Errorf("server SSRF validation failed: %w", err)
			}
		}
	}
	if _, ok := config["topic"]; !ok {
		return fmt.Errorf("topic is required")
	}
	return nil
}

// TelegramDispatcher sends alerts to Telegram
type TelegramDispatcher struct {
	logger *slog.Logger
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
		var detailsBuilder strings.Builder
		detailsBuilder.WriteString("\n\n*Details:*\n")
		for key, value := range event.Details {
			fmt.Fprintf(&detailsBuilder, "• *%s:* %s\n", key, value)
		}
		message += detailsBuilder.String()
	}

	message += "\n_AnubisWatch — The Judgment Never Sleeps_"

	// Build request
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	payload := map[string]string{
		"chat_id":    chatID,
		"text":       message,
		"parse_mode": "Markdown",
	}

	// Disable notifications for non-critical alerts
	if event.Severity != core.SeverityCritical {
		payload["disable_notification"] = "true"
	}

	data, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := getHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return redactedRequestError(err, req)
	}
	defer closeResponseBody(resp)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

// Validate validates Telegram configuration
func (d *TelegramDispatcher) Validate(config map[string]any) error {
	if _, ok := config["bot_token"]; !ok {
		return fmt.Errorf("bot_token is required")
	}
	if _, ok := config["chat_id"]; !ok {
		return fmt.Errorf("chat_id is required")
	}
	return nil
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

	toList, ok := channel.Config["to"].([]any)
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

	client := getHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return redactedRequestError(err, req)
	}
	defer closeResponseBody(resp)

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

	toList, ok := channel.Config["to"].([]any)
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

			client := getHTTPClient()
			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			defer closeResponseBody(resp)
			if resp.StatusCode >= 400 {
				return fmt.Errorf("unexpected status: %d", resp.StatusCode)
			}
		}
	}

	return nil
}

// Validate validates SMS configuration
func (d *SMSDispatcher) Validate(config map[string]any) error {
	provider, _ := config["provider"].(string)
	if provider == "" {
		provider = "twilio"
	}

	switch provider {
	case "twilio":
		if _, ok := config["account_sid"]; !ok {
			return fmt.Errorf("account_sid is required for twilio")
		}
		if _, ok := config["auth_token"]; !ok {
			return fmt.Errorf("auth_token is required for twilio")
		}
	case "vonage":
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

// WebHookDispatcher sends alerts to generic webhooks
type WebHookDispatcher struct {
	logger *slog.Logger
}

// Send sends an alert to a webhook
func (d *WebHookDispatcher) Send(ctx context.Context, event *core.AlertEvent, channel *core.AlertChannel) error {
	webhookURL, _ := channel.Config["url"].(string)
	if webhookURL == "" {
		return fmt.Errorf("missing url")
	}

	// SSRF protection - validate webhook URL
	if err := probe.ValidateTarget(webhookURL); err != nil {
		return fmt.Errorf("SSRF validation failed: %w", err)
	}

	method := "POST"
	if m, ok := channel.Config["method"].(string); ok && m != "" {
		method = m
	}

	// Build payload
	payload := map[string]any{
		"id":          event.ID,
		"soul_id":     event.SoulID,
		"soul_name":   event.SoulName,
		"status":      event.Status,
		"prev_status": event.PrevStatus,
		"severity":    event.Severity,
		"message":     event.Message,
		"details":     event.Details,
		"timestamp":   event.Timestamp,
	}

	data, _ := json.Marshal(payload)

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
	if headers, ok := channel.Config["headers"].(map[string]any); ok {
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

	client := getHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return redactedRequestError(err, req)
	}
	defer closeResponseBody(resp)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

// Validate validates webhook configuration
func (d *WebHookDispatcher) Validate(config map[string]any) error {
	url, ok := config["url"]
	if !ok {
		return fmt.Errorf("url is required")
	}

	webhookURL, ok := url.(string)
	if !ok || webhookURL == "" {
		return fmt.Errorf("url must be a non-empty string")
	}

	// SSRF protection - validate webhook URL at configuration time
	if err := probe.ValidateTarget(webhookURL); err != nil {
		return fmt.Errorf("SSRF validation failed: %w", err)
	}

	return nil
}

func hmacSha256(data []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
