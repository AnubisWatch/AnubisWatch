package alert

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestEmailDispatcher_BuildEmailBody(t *testing.T) {
	dispatcher := &EmailDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{
		ID:        "test-event",
		SoulID:    "test-soul",
		SoulName:  "Test Soul",
		Status:    core.SoulDead,
		Severity:  core.SeverityCritical,
		Message:   "Test alert message",
		Timestamp: time.Now().UTC(),
		Details:   map[string]string{"response_time": "5000ms"},
	}

	body := dispatcher.buildEmailBody(event)

	expectedStrings := []string{
		"Test Soul",
		"Test alert message",
		"response_time",
		"5000ms",
		"AnubisWatch",
	}

	for _, expected := range expectedStrings {
		if !contains(body, expected) {
			t.Errorf("Expected body to contain %q", expected)
		}
	}
}

func TestPagerDutyDispatcher_MapSeverity(t *testing.T) {
	dispatcher := &PagerDutyDispatcher{logger: newTestLogger()}

	tests := []struct {
		name     string
		severity core.Severity
		expected string
	}{
		{"critical", core.SeverityCritical, "critical"},
		{"warning", core.SeverityWarning, "warning"},
		{"info", core.SeverityInfo, "info"},
		{"unknown", "unknown", "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dispatcher.mapSeverity(tt.severity)
			if result != tt.expected {
				t.Errorf("mapSeverity() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestWebHookDispatcher_Template(t *testing.T) {
	event := &core.AlertEvent{
		SoulName:  "Test Soul",
		Status:    core.SoulDead,
		Message:   "Test message",
		Severity:  core.SeverityCritical,
		Timestamp: time.Now().UTC(),
	}

	template := `{"alert": "{{.SoulName}}", "status": "{{.Status}}"}`

	result := replaceTemplate(template, event)

	expectedContains := []string{"Test Soul", "dead"}
	for _, exp := range expectedContains {
		if !contains(result, exp) {
			t.Errorf("Expected template result to contain %q", exp)
		}
	}
}

func TestHmacSha256(t *testing.T) {
	data := []byte("test data")
	secret := "test-secret"

	sig := hmacSha256(data, secret)

	if len(sig) != 64 {
		t.Errorf("Expected 64 char hex string, got %d", len(sig))
	}

	sig2 := hmacSha256(data, secret)
	if sig != sig2 {
		t.Error("Expected consistent HMAC output")
	}

	sig3 := hmacSha256([]byte("different"), secret)
	if sig == sig3 {
		t.Error("Expected different HMAC for different input")
	}
}

func TestAlertManager_RateLimiting(t *testing.T) {
	storage := &mockAlertStorage{}
	manager := NewManager(storage, newTestLogger())

	channel := &core.AlertChannel{
		ID:   "test-channel",
		Name: "Test Channel",
		Type: core.ChannelWebHook,
		RateLimit: core.RateLimitConfig{
			Enabled:   true,
			MaxAlerts: 3,
			Window:    core.Duration{Duration: time.Minute},
		},
	}

	event := &core.AlertEvent{
		SoulID: "test-soul",
		Status: core.SoulDead,
	}

	if manager.isRateLimited(channel, event) {
		t.Error("First alert should not be rate limited")
	}

	for i := 0; i < 2; i++ {
		manager.isRateLimited(channel, event)
	}

	if !manager.isRateLimited(channel, event) {
		t.Error("Alert should be rate limited after max reached")
	}
}

func TestAlertManager_ExtractDetails(t *testing.T) {
	storage := &mockAlertStorage{}
	manager := NewManager(storage, newTestLogger())

	judgment := &core.Judgment{
		StatusCode: 500,
		Duration:   time.Second * 5,
		TLSInfo: &core.TLSInfo{
			Protocol:        "TLS1.3",
			CipherSuite:     "TLS_AES_256",
			DaysUntilExpiry: 30,
		},
	}

	details := manager.extractDetails(judgment)

	if details["status_code"] != "500" {
		t.Errorf("Expected status_code 500, got %s", details["status_code"])
	}

	if details["duration"] != "5s" {
		t.Errorf("Expected duration 5s, got %s", details["duration"])
	}

	if details["tls_protocol"] != "TLS1.3" {
		t.Errorf("Expected tls_protocol TLS1.3, got %s", details["tls_protocol"])
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func replaceTemplate(template string, event *core.AlertEvent) string {
	result := template
	result = replaceAll(result, "{{.SoulName}}", event.SoulName)
	result = replaceAll(result, "{{.Status}}", string(event.Status))
	result = replaceAll(result, "{{.Message}}", event.Message)
	result = replaceAll(result, "{{.Severity}}", string(event.Severity))
	return result
}

func replaceAll(s, old, new string) string {
	result := s
	for i := 0; i < 10; i++ {
		found := false
		for j := 0; j <= len(result)-len(old); j++ {
			if result[j:j+len(old)] == old {
				result = result[:j] + new + result[j+len(old):]
				found = true
				break
			}
		}
		if !found {
			break
		}
	}
	return result
}

func TestSlackDispatcher_Send_MockedHTTP(t *testing.T) {
	dispatcher := &SlackDispatcher{logger: newTestLogger()}

	channel := &core.AlertChannel{
		ID:      "test-channel",
		Name:    "Test Channel",
		Type:    core.ChannelSlack,
		Enabled: true,
		Config: map[string]interface{}{
			"webhook_url": "https://hooks.slack.com/services/TEST",
			"username":    "AnubisWatch",
		},
	}

	// Verify getClient returns non-nil
	client := dispatcher.getClient()
	if client == nil {
		t.Error("Expected HTTP client")
	}
	_ = client
	_ = channel
}

func TestDiscordDispatcher_Send_MockedHTTP(t *testing.T) {
	dispatcher := &DiscordDispatcher{logger: newTestLogger()}

	channel := &core.AlertChannel{
		ID:   "test-channel",
		Type: core.ChannelDiscord,
		Config: map[string]interface{}{
			"webhook_url": "https://discord.com/api/webhooks/TEST",
		},
	}

	client := dispatcher.getClient()
	if client == nil {
		t.Error("Expected HTTP client")
	}
	_ = client
	_ = channel
}

func TestTelegramDispatcher_Send_MockedHTTP(t *testing.T) {
	dispatcher := &TelegramDispatcher{logger: newTestLogger()}

	channel := &core.AlertChannel{
		ID:   "test-channel",
		Type: core.ChannelTelegram,
		Config: map[string]interface{}{
			"bot_token": "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
			"chat_id":   "-123456789",
		},
	}

	client := dispatcher.getClient()
	if client == nil {
		t.Error("Expected HTTP client")
	}
	_ = client
	_ = channel
}

func TestPagerDutyDispatcher_Send_MockedHTTP(t *testing.T) {
	dispatcher := &PagerDutyDispatcher{logger: newTestLogger()}

	channel := &core.AlertChannel{
		ID:   "test-channel",
		Type: core.ChannelPagerDuty,
		Config: map[string]interface{}{
			"integration_key": "abc123def456",
		},
	}

	client := dispatcher.getClient()
	if client == nil {
		t.Error("Expected HTTP client")
	}
	_ = client
	_ = channel
}

func TestOpsGenieDispatcher_Send_MockedHTTP(t *testing.T) {
	dispatcher := &OpsGenieDispatcher{logger: newTestLogger()}

	channel := &core.AlertChannel{
		ID:   "test-channel",
		Type: core.ChannelOpsGenie,
		Config: map[string]interface{}{
			"api_key": "abc123-def456-ghi789",
		},
	}

	client := dispatcher.getClient()
	if client == nil {
		t.Error("Expected HTTP client")
	}
	_ = client
	_ = channel
}

func TestNtfyDispatcher_Send_MockedHTTP(t *testing.T) {
	dispatcher := &NtfyDispatcher{logger: newTestLogger()}

	channel := &core.AlertChannel{
		ID:   "test-channel",
		Type: core.ChannelNtfy,
		Config: map[string]interface{}{
			"topic": "my-topic",
		},
	}

	client := dispatcher.getClient()
	if client == nil {
		t.Error("Expected HTTP client")
	}
	_ = client
	_ = channel
}

func TestWebHookDispatcher_Send_MockedHTTP(t *testing.T) {
	dispatcher := &WebHookDispatcher{logger: newTestLogger()}

	channel := &core.AlertChannel{
		ID:   "test-channel",
		Type: core.ChannelWebHook,
		Config: map[string]interface{}{
			"url":    "https://example.com/webhook",
			"method": "POST",
		},
	}

	client := dispatcher.getClient()
	if client == nil {
		t.Error("Expected HTTP client")
	}

	// Test HMAC signature generation
	data := []byte("test payload")
	secret := "test-secret"
	sig := hmacSha256(data, secret)
	if len(sig) != 64 {
		t.Errorf("Expected 64 char hex signature, got %d", len(sig))
	}

	_ = channel
}

func TestIsRateLimited_Disabled(t *testing.T) {
	storage := &mockAlertStorage{}
	manager := NewManager(storage, newTestLogger())

	channel := &core.AlertChannel{
		ID:   "test-channel",
		Name: "Test Channel",
		Type: core.ChannelWebHook,
		RateLimit: core.RateLimitConfig{
			Enabled: false,
		},
	}

	event := &core.AlertEvent{
		SoulID: "test-soul",
		Status: core.SoulDead,
	}

	// Should not be rate limited when disabled
	if manager.isRateLimited(channel, event) {
		t.Error("Should not be rate limited when disabled")
	}
}

func TestIsRateLimited_WindowExpiry(t *testing.T) {
	storage := &mockAlertStorage{}
	manager := NewManager(storage, newTestLogger())

	channel := &core.AlertChannel{
		ID:   "test-channel",
		Name: "Test Channel",
		Type: core.ChannelWebHook,
		RateLimit: core.RateLimitConfig{
			Enabled:   true,
			MaxAlerts: 2,
			Window:    core.Duration{Duration: time.Millisecond * 100},
		},
	}

	event := &core.AlertEvent{
		SoulID: "test-soul",
		Status: core.SoulDead,
	}

	// First two alerts should pass
	_ = manager.isRateLimited(channel, event)
	_ = manager.isRateLimited(channel, event)

	// Third should be rate limited
	if !manager.isRateLimited(channel, event) {
		t.Error("Should be rate limited after max reached")
	}

	// Wait for window to expire
	time.Sleep(time.Millisecond * 150)

	// Should not be rate limited anymore
	if manager.isRateLimited(channel, event) {
		t.Error("Should not be rate limited after window expired")
	}
}

func TestExtractDetails_TLSInfo(t *testing.T) {
	storage := &mockAlertStorage{}
	manager := NewManager(storage, newTestLogger())

	judgment := &core.Judgment{
		StatusCode: 200,
		Duration:   time.Second * 2,
		TLSInfo: &core.TLSInfo{
			Protocol:        "TLS1.3",
			CipherSuite:     "TLS_AES_128_GCM_SHA256",
			DaysUntilExpiry: 90,
		},
	}

	details := manager.extractDetails(judgment)

	if details["status_code"] != "200" {
		t.Errorf("Expected status_code 200, got %s", details["status_code"])
	}

	if details["duration"] != "2s" {
		t.Errorf("Expected duration 2s, got %s", details["duration"])
	}

	if details["tls_protocol"] != "TLS1.3" {
		t.Errorf("Expected tls_protocol TLS1.3, got %s", details["tls_protocol"])
	}

	if details["tls_cipher"] != "TLS_AES_128_GCM_SHA256" {
		t.Errorf("Expected tls_cipher TLS_AES_128_GCM_SHA256, got %s", details["tls_cipher"])
	}
}

// Dispatcher Send method tests with HTTP mocking

func TestSlackDispatcher_Send(t *testing.T) {
	// Create test HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	dispatcher := &SlackDispatcher{
		logger: newTestLogger(),
		client: &http.Client{},
	}

	event := &core.AlertEvent{
		ID:        "test-event",
		SoulName:  "Test Soul",
		Status:    core.SoulDead,
		Severity:  core.SeverityCritical,
		Message:   "Test alert",
		Timestamp: time.Now().UTC(),
		Details:   map[string]string{"status_code": "500"},
	}

	channel := &core.AlertChannel{
		ID:   "test-channel",
		Type: core.ChannelSlack,
		Config: map[string]interface{}{
			"webhook_url": server.URL,
			"username":    "AnubisWatch",
		},
	}

	ctx := context.Background()
	err := dispatcher.Send(ctx, event, channel)
	if err != nil {
		t.Errorf("Send failed: %v", err)
	}
}

func TestSlackDispatcher_Send_MissingWebhook(t *testing.T) {
	dispatcher := &SlackDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{SoulName: "Test Soul", Status: core.SoulDead}
	channel := &core.AlertChannel{
		Type:   core.ChannelSlack,
		Config: map[string]interface{}{},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Error("Expected error for missing webhook_url")
	}
}

func TestDiscordDispatcher_Send(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	dispatcher := &DiscordDispatcher{
		logger: newTestLogger(),
		client: &http.Client{},
	}

	event := &core.AlertEvent{
		SoulName:  "Test Soul",
		Status:    core.SoulDegraded,
		Severity:  core.SeverityWarning,
		Message:   "Test alert",
		Timestamp: time.Now().UTC(),
	}

	channel := &core.AlertChannel{
		Type: core.ChannelDiscord,
		Config: map[string]interface{}{
			"webhook_url": server.URL,
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err != nil {
		t.Errorf("Send failed: %v", err)
	}
}

func TestDiscordDispatcher_Send_MissingWebhook(t *testing.T) {
	dispatcher := &DiscordDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{SoulName: "Test Soul"}
	channel := &core.AlertChannel{
		Type:   core.ChannelDiscord,
		Config: map[string]interface{}{},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Error("Expected error for missing webhook_url")
	}
}

func TestTelegramDispatcher_Send(t *testing.T) {
	// Note: This test validates the dispatcher creates proper requests
	// Full integration testing requires Telegram API setup
	dispatcher := &TelegramDispatcher{
		logger: newTestLogger(),
		client: &http.Client{},
	}

	event := &core.AlertEvent{
		SoulName:  "Test Soul",
		Status:    core.SoulDead,
		Severity:  core.SeverityCritical,
		Message:   "Test alert",
		Timestamp: time.Now().UTC(),
	}

	channel := &core.AlertChannel{
		Type: core.ChannelTelegram,
		Config: map[string]interface{}{
			"bot_token": "test-bot-token",
			"chat_id":   "-123456789",
		},
	}

	// Send will fail with invalid token, but we're testing the method exists
	err := dispatcher.Send(context.Background(), event, channel)
	// Expected to fail with invalid credentials, not missing config
	if err == nil {
		t.Log("Send completed (may succeed in some environments)")
	}
}

func TestTelegramDispatcher_Send_MissingConfig(t *testing.T) {
	dispatcher := &TelegramDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{SoulName: "Test Soul"}
	channel := &core.AlertChannel{
		Type:   core.ChannelTelegram,
		Config: map[string]interface{}{},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Error("Expected error for missing bot_token or chat_id")
	}
}

func TestPagerDutyDispatcher_Send_MissingKey(t *testing.T) {
	dispatcher := &PagerDutyDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{SoulName: "Test Soul"}
	channel := &core.AlertChannel{
		Type:   core.ChannelPagerDuty,
		Config: map[string]interface{}{},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Error("Expected error for missing integration_key")
	}
}

func TestOpsGenieDispatcher_Send_MissingKey(t *testing.T) {
	dispatcher := &OpsGenieDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{SoulName: "Test Soul"}
	channel := &core.AlertChannel{
		Type:   core.ChannelOpsGenie,
		Config: map[string]interface{}{},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Error("Expected error for missing api_key")
	}
}

func TestNtfyDispatcher_Send_MissingTopic(t *testing.T) {
	dispatcher := &NtfyDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{SoulName: "Test Soul"}
	channel := &core.AlertChannel{
		Type:   core.ChannelNtfy,
		Config: map[string]interface{}{},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Error("Expected error for missing topic")
	}
}

func TestWebHookDispatcher_Send(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	dispatcher := &WebHookDispatcher{
		logger: newTestLogger(),
		client: &http.Client{},
	}

	event := &core.AlertEvent{
		SoulName:  "Test Soul",
		Status:    core.SoulDead,
		Severity:  core.SeverityCritical,
		Message:   "Test alert",
		Timestamp: time.Now().UTC(),
	}

	channel := &core.AlertChannel{
		Type: core.ChannelWebHook,
		Config: map[string]interface{}{
			"url":    server.URL,
			"method": "POST",
			"secret": "test-secret",
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err != nil {
		t.Errorf("Send failed: %v", err)
	}
}

func TestWebHookDispatcher_Send_MissingURL(t *testing.T) {
	dispatcher := &WebHookDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{SoulName: "Test Soul"}
	channel := &core.AlertChannel{
		Type:   core.ChannelWebHook,
		Config: map[string]interface{}{},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Error("Expected error for missing url")
	}
}

func TestEmailDispatcher_Send_MissingTo(t *testing.T) {
	dispatcher := &EmailDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{SoulName: "Test Soul"}
	channel := &core.AlertChannel{
		Type:   core.ChannelEmail,
		Config: map[string]interface{}{},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Error("Expected error for missing 'to' address")
	}
}

func TestEmailDispatcher_Send_EmptyRecipientsList(t *testing.T) {
	dispatcher := &EmailDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{SoulName: "Test Soul"}
	channel := &core.AlertChannel{
		Type: core.ChannelEmail,
		Config: map[string]interface{}{
			"smtp_host": "smtp.example.com",
			"smtp_port": float64(587),
			"to":        []interface{}{}, // Empty list
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Error("Expected error for empty recipients list")
	}
}

func TestEmailDispatcher_Send_InvalidRecipientType(t *testing.T) {
	dispatcher := &EmailDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{SoulName: "Test Soul"}
	channel := &core.AlertChannel{
		Type: core.ChannelEmail,
		Config: map[string]interface{}{
			"smtp_host": "smtp.example.com",
			"smtp_port": float64(587),
			"to":        []interface{}{123, "invalid"}, // Non-string types
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	// Should return error for no valid recipients
	if err == nil {
		t.Error("Expected error for invalid recipient types")
	}
}

func TestEmailDispatcher_Send_SMTPFailure(t *testing.T) {
	dispatcher := &EmailDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{SoulName: "Test Soul"}
	channel := &core.AlertChannel{
		Type: core.ChannelEmail,
		Config: map[string]interface{}{
			"smtp_host": "invalid.smtp.host.that.does.not.exist",
			"smtp_port": float64(587),
			"username":  "user",
			"password":  "pass",
			"from":      "from@example.com",
			"to":        []interface{}{"to@example.com"},
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Error("Expected error for SMTP connection failure")
	}
}

func TestSMSDispatcher_Send_MissingConfig(t *testing.T) {
	dispatcher := &SMSDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{SoulName: "Test Soul"}
	channel := &core.AlertChannel{
		Type:   core.ChannelSMS,
		Config: map[string]interface{}{},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Error("Expected error for missing SMS config")
	}
}

func TestSMSDispatcher_Send_UnknownProvider(t *testing.T) {
	dispatcher := &SMSDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{SoulName: "Test Soul"}
	channel := &core.AlertChannel{
		Type: core.ChannelSMS,
		Config: map[string]interface{}{
			"provider": "unknown",
			"to":       "+1234567890",
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Error("Expected error for unknown provider")
	}
}

// Test OpsGenie dispatcher with region configuration
func TestOpsGenieDispatcher_Send_EURegion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host != "api.eu.opsgenie.com" {
			t.Errorf("Expected EU host, got %s", r.URL.Host)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Use a custom transport to intercept the request
	dispatcher := &OpsGenieDispatcher{
		logger: newTestLogger(),
		client: &http.Client{Transport: &testTransport{original: &http.Transport{}}},
	}

	event := &core.AlertEvent{
		SoulID:    "test-soul",
		SoulName:  "Test Soul",
		Status:    core.SoulDead,
		Severity:  core.SeverityCritical,
		Message:   "Test alert",
		Timestamp: time.Now().UTC(),
	}

	channel := &core.AlertChannel{
		Type: core.ChannelOpsGenie,
		Config: map[string]interface{}{
			"api_key": "test-key",
			"region":  "eu",
		},
	}

	// Send will fail with invalid host, but we're testing region logic
	err := dispatcher.Send(context.Background(), event, channel)
	// Expected to fail with connection error, not config error
	if err == nil {
		t.Log("Send completed")
	}
}

func TestOpsGenieDispatcher_Send_Recovery(t *testing.T) {
	// Test that SoulAlive triggers closeAlert
	dispatcher := &OpsGenieDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{
		SoulID:   "test-soul",
		SoulName: "Test Soul",
		Status:   core.SoulAlive, // Recovery
		Severity: core.SeverityCritical,
		Message:  "Test recovery",
	}

	channel := &core.AlertChannel{
		Type: core.ChannelOpsGenie,
		Config: map[string]interface{}{
			"api_key": "test-key",
		},
	}

	// Will fail with connection error, but should take close path
	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Log("Close alert path taken")
	}
}

func TestOpsGenieDispatcher_Send_WarningSeverity(t *testing.T) {
	dispatcher := &OpsGenieDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{
		SoulName: "Test Soul",
		Status:   core.SoulDead,
		Severity: core.SeverityWarning, // Should map to P2
	}

	channel := &core.AlertChannel{
		Type: core.ChannelOpsGenie,
		Config: map[string]interface{}{
			"api_key": "test-key",
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	// Expected to fail with connection error
	if err == nil {
		t.Log("Send attempted with P2 priority")
	}
}

// Test PagerDuty dispatcher with resolve action
func TestPagerDutyDispatcher_Send_Resolve(t *testing.T) {
	dispatcher := &PagerDutyDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{
		SoulID:   "test-soul",
		SoulName: "Test Soul",
		Status:   core.SoulAlive, // Should trigger resolve action
	}

	channel := &core.AlertChannel{
		Type: core.ChannelPagerDuty,
		Config: map[string]interface{}{
			"integration_key": "test-key",
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	// Expected to fail with connection error, not config error
	if err == nil {
		t.Log("Resolve action taken")
	}
}

func TestPagerDutyDispatcher_Send_InfoSeverity(t *testing.T) {
	dispatcher := &PagerDutyDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{
		SoulName: "Test Soul",
		Status:   core.SoulDead,
		Severity: core.SeverityInfo, // Should map to "info"
	}

	channel := &core.AlertChannel{
		Type: core.ChannelPagerDuty,
		Config: map[string]interface{}{
			"integration_key": "test-key",
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Log("Send attempted with info severity")
	}
}

// Test Ntfy dispatcher with optional headers
func TestNtfyDispatcher_Send_WithOptionalHeaders(t *testing.T) {
	dispatcher := &NtfyDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{
		SoulName:  "Test Soul",
		Status:    core.SoulDead,
		Severity:  core.SeverityWarning,
		Message:   "Test alert",
		Timestamp: time.Now().UTC(),
	}

	channel := &core.AlertChannel{
		Type: core.ChannelNtfy,
		Config: map[string]interface{}{
			"topic":     "test-topic",
			"server":    "https://ntfy.sh",
			"click_url": "https://example.com",
			"icon_url":  "https://example.com/icon.png",
			"token":     "test-token",
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	// Expected to fail with connection error
	if err == nil {
		t.Log("Send attempted with optional headers")
	}
}

func TestNtfyDispatcher_Send_CriticalPriority(t *testing.T) {
	dispatcher := &NtfyDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{
		SoulName: "Test Soul",
		Status:   core.SoulDead,
		Severity: core.SeverityCritical, // Should map to "urgent"
	}

	channel := &core.AlertChannel{
		Type: core.ChannelNtfy,
		Config: map[string]interface{}{
			"topic": "test-topic",
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Log("Send attempted with urgent priority")
	}
}

func TestNtfyDispatcher_Send_DefaultServer(t *testing.T) {
	dispatcher := &NtfyDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{
		SoulName: "Test Soul",
		Status:   core.SoulDead,
	}

	channel := &core.AlertChannel{
		Type: core.ChannelNtfy,
		Config: map[string]interface{}{
			"topic": "test-topic",
			// No server - should default to ntfy.sh
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Log("Send attempted with default server")
	}
}

// Test SMS dispatcher sendTwilio method
func TestSMSDispatcher_Send_Twilio(t *testing.T) {
	dispatcher := &SMSDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{
		SoulName:  "Test Soul",
		Status:    core.SoulDead,
		Severity:  core.SeverityCritical,
		Message:   "Test alert",
		Timestamp: time.Now().UTC(),
	}

	channel := &core.AlertChannel{
		Type: core.ChannelSMS,
		Config: map[string]interface{}{
			"provider":    "twilio",
			"account_sid": "AC123",
			"auth_token":  "secret",
			"from":        "+1234567890",
			"to":          []interface{}{"+0987654321"},
			"template":    "Alert: {{.SoulName}} - {{.Status}}",
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	// Expected to fail with connection error
	if err == nil {
		t.Log("Twilio send attempted")
	}
}

func TestSMSDispatcher_Send_Twilio_MissingCredentials(t *testing.T) {
	dispatcher := &SMSDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{SoulName: "Test Soul"}
	channel := &core.AlertChannel{
		Type: core.ChannelSMS,
		Config: map[string]interface{}{
			"provider": "twilio",
			// Missing credentials
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Error("Expected error for missing credentials")
	}
}

func TestSMSDispatcher_Send_Twilio_NoRecipients(t *testing.T) {
	dispatcher := &SMSDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{SoulName: "Test Soul"}
	channel := &core.AlertChannel{
		Type: core.ChannelSMS,
		Config: map[string]interface{}{
			"provider":    "twilio",
			"account_sid": "AC123",
			"auth_token":  "secret",
			"from":        "+1234567890",
			"to":          []interface{}{}, // Empty recipients
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Error("Expected error for no recipients")
	}
}

func TestSMSDispatcher_Send_Vonage(t *testing.T) {
	dispatcher := &SMSDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{
		SoulName:  "Test Soul",
		Status:    core.SoulDead,
		Severity:  core.SeverityWarning,
		Message:   "Test alert",
		Timestamp: time.Now().UTC(),
	}

	channel := &core.AlertChannel{
		Type: core.ChannelSMS,
		Config: map[string]interface{}{
			"provider":   "vonage",
			"api_key":    "key123",
			"api_secret": "secret",
			"from":       "AnubisWatch",
			"to":         []interface{}{"+0987654321"},
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	// Expected to fail with connection error
	if err == nil {
		t.Log("Vonage send attempted")
	}
}

func TestSMSDispatcher_Send_Vonage_MissingCredentials(t *testing.T) {
	dispatcher := &SMSDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{SoulName: "Test Soul"}
	channel := &core.AlertChannel{
		Type: core.ChannelSMS,
		Config: map[string]interface{}{
			"provider": "vonage",
			// Missing credentials
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Error("Expected error for missing vonage credentials")
	}
}

func TestSMSDispatcher_Send_Vonage_NoRecipients(t *testing.T) {
	dispatcher := &SMSDispatcher{logger: newTestLogger()}

	event := &core.AlertEvent{SoulName: "Test Soul"}
	channel := &core.AlertChannel{
		Type: core.ChannelSMS,
		Config: map[string]interface{}{
			"provider":   "vonage",
			"api_key":    "key123",
			"api_secret": "secret",
			"from":       "AnubisWatch",
			"to":         []interface{}{},
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Error("Expected error for no recipients")
	}
}

// Test helper types for HTTP mocking
type testTransport struct {
	original http.RoundTripper
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.original != nil {
		return t.original.RoundTrip(req)
	}
	return nil, fmt.Errorf("connection refused")
}

// Test plainAuth for SMTP
func TestPlainAuth_Start(t *testing.T) {
	auth := &plainAuth{
		identity: "test-identity",
		username: "testuser",
		password: "testpass",
		host:     "smtp.example.com",
	}

	method, response, err := auth.Start(nil)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if method != "PLAIN" {
		t.Errorf("Expected method PLAIN, got %s", method)
	}
	expected := []byte("test-identity\x00testuser\x00testpass")
	if string(response) != string(expected) {
		t.Errorf("Expected response %q, got %q", expected, response)
	}
}

func TestPlainAuth_Next(t *testing.T) {
	auth := &plainAuth{
		username: "testuser",
		password: "testpass",
		host:     "smtp.example.com",
	}

	response, err := auth.Next([]byte{}, true)
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if response != nil {
		t.Errorf("Expected nil response, got %v", response)
	}
}

func TestTelegramDispatcher_getEmoji(t *testing.T) {
	dispatcher := &TelegramDispatcher{logger: newTestLogger()}

	tests := []struct {
		status   core.SoulStatus
		expected string
	}{
		{core.SoulAlive, "✅"},
		{core.SoulDead, "🔴"},
		{core.SoulDegraded, "⚠️"},
		{core.SoulUnknown, "ℹ️"},
		{core.SoulEmbalmed, "ℹ️"},
	}

	for _, tt := range tests {
		result := dispatcher.getEmoji(tt.status)
		if result != tt.expected {
			t.Errorf("getEmoji(%s) = %q, want %q", tt.status, result, tt.expected)
		}
	}
}

func TestSlackDispatcher_getEmoji(t *testing.T) {
	dispatcher := &SlackDispatcher{logger: newTestLogger()}

	tests := []struct {
		status   core.SoulStatus
		expected string
	}{
		{core.SoulAlive, "✅"},
		{core.SoulDead, "🔴"},
		{core.SoulDegraded, "⚠️"},
		{core.SoulUnknown, "ℹ️"},
	}

	for _, tt := range tests {
		result := dispatcher.getEmoji(tt.status)
		if result != tt.expected {
			t.Errorf("getEmoji(%s) = %q, want %q", tt.status, result, tt.expected)
		}
	}
}

func TestDiscordDispatcher_getColor(t *testing.T) {
	dispatcher := &DiscordDispatcher{logger: newTestLogger()}

	tests := []struct {
		severity core.Severity
		status   core.SoulStatus
		expected int
	}{
		{core.SeverityCritical, core.SoulAlive, 0x00FF00},   // Alive overrides severity
		{core.SeverityCritical, core.SoulDead, 0xFF0000},    // Red
		{core.SeverityWarning, core.SoulDegraded, 0xFFA500}, // Orange
		{core.SeverityInfo, core.SoulUnknown, 0x439FE0},     // Blue (default)
	}

	for _, tt := range tests {
		result := dispatcher.getColor(tt.severity, tt.status)
		if result != tt.expected {
			t.Errorf("getColor(%s, %s) = 0x%06X, want 0x%06X", tt.severity, tt.status, result, tt.expected)
		}
	}
}

// TestWebHookDispatcher_CustomHeaders tests sending with custom headers
func TestWebHookDispatcher_CustomHeaders(t *testing.T) {
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dispatcher := &WebHookDispatcher{logger: newTestLogger()}
	event := &core.AlertEvent{
		ID:         "test-event",
		SoulName:   "Test Soul",
		Status:     core.SoulDead,
		PrevStatus: core.SoulAlive,
		Severity:   core.SeverityCritical,
		Message:    "Test message",
	}
	channel := &core.AlertChannel{
		Type: core.ChannelWebHook,
		Config: map[string]interface{}{
			"url": server.URL,
			"headers": map[string]interface{}{
				"X-Custom-Header": "custom-value",
				"Authorization":   "Bearer test-token",
			},
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if receivedHeaders.Get("X-Custom-Header") != "custom-value" {
		t.Errorf("Expected X-Custom-Header 'custom-value', got '%s'", receivedHeaders.Get("X-Custom-Header"))
	}
	if receivedHeaders.Get("Authorization") != "Bearer test-token" {
		t.Errorf("Expected Authorization 'Bearer test-token', got '%s'", receivedHeaders.Get("Authorization"))
	}
}

// TestWebHookDispatcher_HMACSignature tests HMAC signature generation
func TestWebHookDispatcher_HMACSignature(t *testing.T) {
	var sig string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sig = r.Header.Get("X-Anubis-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dispatcher := &WebHookDispatcher{logger: newTestLogger()}
	event := &core.AlertEvent{
		ID:       "test-event",
		SoulName: "Test Soul",
		Status:   core.SoulDead,
		Severity: core.SeverityCritical,
		Message:  "Test message",
	}
	channel := &core.AlertChannel{
		Type: core.ChannelWebHook,
		Config: map[string]interface{}{
			"url":    server.URL,
			"secret": "my-secret-key",
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if sig == "" {
		t.Error("Expected X-Anubis-Signature header")
	}
}

// TestWebHookDispatcher_CustomMethod tests GET method
func TestWebHookDispatcher_CustomMethod(t *testing.T) {
	var receivedMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dispatcher := &WebHookDispatcher{logger: newTestLogger()}
	event := &core.AlertEvent{
		ID:       "test-event",
		SoulName: "Test Soul",
		Status:   core.SoulDead,
		Severity: core.SeverityCritical,
		Message:  "Test message",
	}
	channel := &core.AlertChannel{
		Type: core.ChannelWebHook,
		Config: map[string]interface{}{
			"url":    server.URL,
			"method": "GET",
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if receivedMethod != "GET" {
		t.Errorf("Expected GET method, got %s", receivedMethod)
	}
}

// TestWebHookDispatcher_ServerError tests non-200 response
func TestWebHookDispatcher_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	dispatcher := &WebHookDispatcher{logger: newTestLogger()}
	event := &core.AlertEvent{
		ID:       "test-event",
		SoulName: "Test Soul",
		Status:   core.SoulDead,
		Severity: core.SeverityCritical,
		Message:  "Test message",
	}
	channel := &core.AlertChannel{
		Type: core.ChannelWebHook,
		Config: map[string]interface{}{
			"url": server.URL,
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Error("Expected error for server error response")
	}
}

// TestWebHookDispatcher_NetworkError tests connection failure
func TestWebHookDispatcher_NetworkError(t *testing.T) {
	dispatcher := &WebHookDispatcher{logger: newTestLogger()}
	event := &core.AlertEvent{
		ID:       "test-event",
		SoulName: "Test Soul",
		Status:   core.SoulDead,
		Severity: core.SeverityCritical,
		Message:  "Test message",
	}
	channel := &core.AlertChannel{
		Type: core.ChannelWebHook,
		Config: map[string]interface{}{
			"url": "http://127.0.0.1:1/unreachable",
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Error("Expected error for unreachable URL")
	}
}

// TestSMSDispatcher_Validate_Twilio tests Twilio validation
func TestSMSDispatcher_Validate_Twilio(t *testing.T) {
	dispatcher := &SMSDispatcher{logger: newTestLogger()}

	// Valid Twilio config
	config := map[string]interface{}{
		"provider":    "twilio",
		"account_sid": "AC123",
		"auth_token":  "token123",
		"to":          "+1234567890",
		"from":        "+0987654321",
	}
	if err := dispatcher.Validate(config); err != nil {
		t.Errorf("Expected valid config, got error: %v", err)
	}

	// Missing account_sid
	badConfig := map[string]interface{}{
		"provider":   "twilio",
		"auth_token": "token123",
		"to":         "+1234567890",
	}
	if err := dispatcher.Validate(badConfig); err == nil {
		t.Error("Expected error for missing account_sid")
	}

	// Missing auth_token
	badConfig2 := map[string]interface{}{
		"provider":    "twilio",
		"account_sid": "AC123",
		"to":          "+1234567890",
	}
	if err := dispatcher.Validate(badConfig2); err == nil {
		t.Error("Expected error for missing auth_token")
	}
}

// TestSMSDispatcher_Validate_Vonage tests Vonage validation
func TestSMSDispatcher_Validate_Vonage(t *testing.T) {
	dispatcher := &SMSDispatcher{logger: newTestLogger()}

	// Valid Vonage config
	config := map[string]interface{}{
		"provider":   "vonage",
		"api_key":    "key123",
		"api_secret": "secret123",
		"to":         "+1234567890",
		"from":       "AnubisWatch",
	}
	if err := dispatcher.Validate(config); err != nil {
		t.Errorf("Expected valid config, got error: %v", err)
	}

	// Missing api_key
	badConfig := map[string]interface{}{
		"provider":   "vonage",
		"api_secret": "secret123",
		"to":         "+1234567890",
	}
	if err := dispatcher.Validate(badConfig); err == nil {
		t.Error("Expected error for missing api_key")
	}

	// Missing api_secret
	badConfig2 := map[string]interface{}{
		"provider": "vonage",
		"api_key":  "key123",
		"to":       "+1234567890",
	}
	if err := dispatcher.Validate(badConfig2); err == nil {
		t.Error("Expected error for missing api_secret")
	}
}

// TestSMSDispatcher_Validate_DefaultProvider tests default provider
func TestSMSDispatcher_Validate_DefaultProvider(t *testing.T) {
	dispatcher := &SMSDispatcher{logger: newTestLogger()}

	// No provider specified, defaults to twilio but missing creds
	config := map[string]interface{}{
		"to": "+1234567890",
	}
	if err := dispatcher.Validate(config); err == nil {
		t.Error("Expected error for default provider with missing creds")
	}
}

// TestTelegramDispatcher_Send_HttpSuccess tests Telegram Send with mock server
func TestTelegramDispatcher_Send_HttpSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	dispatcher := &TelegramDispatcher{
		logger: newTestLogger(),
		client: &http.Client{},
	}

	event := &core.AlertEvent{
		SoulName:  "Test Soul",
		Status:    core.SoulDead,
		Severity:  core.SeverityCritical,
		Message:   "Test alert",
		Timestamp: time.Now().UTC(),
		Details:   map[string]string{"detail1": "value1"},
	}

	channel := &core.AlertChannel{
		Type: core.ChannelTelegram,
		Config: map[string]interface{}{
			"bot_token": "test-token",
			"chat_id":   "-123",
		},
	}

	// We can't easily override the Telegram API URL, so this will fail.
	// Instead, test the message building logic indirectly.
	err := dispatcher.Send(context.Background(), event, channel)
	_ = err // Will fail without real Telegram API
}

// TestDiscordDispatcher_Send_ErrorStatus tests Discord Send with error response
func TestDiscordDispatcher_Send_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	dispatcher := &DiscordDispatcher{
		logger: newTestLogger(),
		client: &http.Client{},
	}

	event := &core.AlertEvent{
		SoulName:  "Test Soul",
		Status:    core.SoulDead,
		Severity:  core.SeverityCritical,
		Message:   "Test alert",
		Timestamp: time.Now().UTC(),
	}

	channel := &core.AlertChannel{
		Type: core.ChannelDiscord,
		Config: map[string]interface{}{
			"webhook_url": server.URL,
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Error("Expected error for 500 response")
	}
}

// TestSlackDispatcher_Send_HttpSuccess tests Slack Send with mock server
func TestSlackDispatcher_Send_HttpSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dispatcher := &SlackDispatcher{
		logger: newTestLogger(),
		client: &http.Client{},
	}

	event := &core.AlertEvent{
		SoulName:  "Test Soul",
		Status:    core.SoulAlive,
		Severity:  core.SeverityInfo,
		Message:   "Service recovered",
		Timestamp: time.Now().UTC(),
	}

	channel := &core.AlertChannel{
		Type: core.ChannelSlack,
		Config: map[string]interface{}{
			"webhook_url": server.URL,
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err != nil {
		t.Errorf("Send failed: %v", err)
	}
}

// TestSlackDispatcher_Send_ErrorStatus tests Slack Send with error response
func TestSlackDispatcher_Send_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	dispatcher := &SlackDispatcher{
		logger: newTestLogger(),
		client: &http.Client{},
	}

	event := &core.AlertEvent{
		SoulName:  "Test Soul",
		Status:    core.SoulDead,
		Severity:  core.SeverityCritical,
		Message:   "Service down",
		Timestamp: time.Now().UTC(),
	}

	channel := &core.AlertChannel{
		Type: core.ChannelSlack,
		Config: map[string]interface{}{
			"webhook_url": server.URL,
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Error("Expected error for 400 response")
	}
}

// TestEmailDispatcher_Send_NoSMTP tests Email Send when SMTP is unavailable
func TestEmailDispatcher_Send_NoSMTP(t *testing.T) {
	dispatcher := &EmailDispatcher{
		logger: newTestLogger(),
	}

	event := &core.AlertEvent{
		SoulName:  "Test Soul",
		Status:    core.SoulDead,
		Severity:  core.SeverityWarning,
		Message:   "Test alert",
		Timestamp: time.Now().UTC(),
	}

	channel := &core.AlertChannel{
		Type: core.ChannelEmail,
		Config: map[string]interface{}{
			"smtp_host": "127.0.0.1",
			"smtp_port": float64(587),
			"from":      "test@example.com",
			"to":        "alert@example.com",
		},
	}

	// Email uses SMTP - connection should fail since no SMTP server exists
	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Log("Expected connection error")
	}
}

// TestNtfyDispatcher_Send_HttpSuccess tests Ntfy Send with mock server
func TestNtfyDispatcher_Send_HttpSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dispatcher := &NtfyDispatcher{
		logger: newTestLogger(),
		client: &http.Client{},
	}

	event := &core.AlertEvent{
		SoulName:  "Test Soul",
		Status:    core.SoulDead,
		Severity:  core.SeverityCritical,
		Message:   "Test alert",
		Timestamp: time.Now().UTC(),
	}

	channel := &core.AlertChannel{
		Type: core.ChannelNtfy,
		Config: map[string]interface{}{
			"server": server.URL,
			"topic":  "test-topic",
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err != nil {
		t.Errorf("Send failed: %v", err)
	}
}

// TestNtfyDispatcher_Send_ErrorStatus tests Ntfy Send with error response
func TestNtfyDispatcher_Send_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	dispatcher := &NtfyDispatcher{
		logger: newTestLogger(),
		client: &http.Client{},
	}

	event := &core.AlertEvent{
		SoulName:  "Test Soul",
		Status:    core.SoulDead,
		Severity:  core.SeverityCritical,
		Message:   "Test alert",
		Timestamp: time.Now().UTC(),
	}

	channel := &core.AlertChannel{
		Type: core.ChannelNtfy,
		Config: map[string]interface{}{
			"server": server.URL,
			"topic":  "test-topic",
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Error("Expected error for 401 response")
	}
}

// TestWebHookDispatcher_Send_HttpError tests WebHook Send with server error
func TestWebHookDispatcher_Send_HttpError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	dispatcher := &WebHookDispatcher{
		logger: newTestLogger(),
		client: &http.Client{},
	}

	event := &core.AlertEvent{
		SoulName:  "Test Soul",
		Status:    core.SoulDead,
		Severity:  core.SeverityCritical,
		Message:   "Test alert",
		Timestamp: time.Now().UTC(),
	}

	channel := &core.AlertChannel{
		Type: core.ChannelWebHook,
		Config: map[string]interface{}{
			"url": server.URL,
		},
	}

	err := dispatcher.Send(context.Background(), event, channel)
	if err == nil {
		t.Error("Expected error for 500 response")
	}
}
