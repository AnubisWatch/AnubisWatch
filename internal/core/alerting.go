package core

import (
	"fmt"
	"net/url"
	"strings"
)

// AlertingConfig holds settings shared by every alert channel.
type AlertingConfig struct {
	// Proxy routes HTTP alert channels (webhook, Slack, Discord, Teams,
	// PagerDuty, OpsGenie, ntfy, Telegram, Twilio) through this
	// http:// or https:// proxy. Alert channels ignore HTTP(S)_PROXY, because
	// an implicit proxy would connect to the target itself and bypass the
	// SSRF policy; this is the explicit opt-in for networks that can only
	// reach the internet through one. Targets are still checked against the
	// policy before each send, but the proxy does its own DNS resolution.
	Proxy string `yaml:"proxy" json:"proxy"`
}

func (c AlertingConfig) validate() error {
	if strings.TrimSpace(c.Proxy) == "" {
		return nil
	}
	u, err := url.Parse(c.Proxy)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return &ConfigError{Field: "alerting.proxy", Message: fmt.Sprintf("must be an http:// or https:// URL, got %q", c.Proxy)}
	}
	return nil
}
