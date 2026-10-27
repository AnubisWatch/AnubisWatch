package core

import (
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"
)

// MailConfig is the server-level outgoing mail (SMTP) relay. It delivers
// status page subscription emails and password reset links; alert email
// channels keep their own per-channel SMTP settings. Mail is enabled when
// Host is set.
//
//	mail:
//	  host: smtp.example.com
//	  port: 587
//	  username: anubis@example.com
//	  password: ${ANUBIS_MAIL_PASSWORD}
//	  from: "AnubisWatch <status@example.com>"
//	  tls: starttls      # starttls (default), implicit (465), none
//	  timeout: 30s
type MailConfig struct {
	Host     string   `json:"host" yaml:"host"`
	Port     int      `json:"port" yaml:"port"`
	Username string   `json:"username" yaml:"username"`
	Password string   `json:"password" yaml:"password"`
	From     string   `json:"from" yaml:"from"`
	TLS      string   `json:"tls" yaml:"tls"`
	Timeout  Duration `json:"timeout" yaml:"timeout"`
}

// Mail TLS modes.
const (
	MailTLSStartTLS = "starttls"
	MailTLSImplicit = "implicit"
	MailTLSNone     = "none"
)

// DefaultMailTimeout bounds one SMTP delivery when mail.timeout is unset.
const DefaultMailTimeout = 30 * time.Second

// Enabled reports whether a mail relay is configured.
func (c MailConfig) Enabled() bool {
	return strings.TrimSpace(c.Host) != ""
}

// EffectivePort is the configured port, or 465 for implicit TLS and 587
// otherwise.
func (c MailConfig) EffectivePort() int {
	if c.Port > 0 {
		return c.Port
	}
	if c.EffectiveTLS() == MailTLSImplicit {
		return 465
	}
	return 587
}

// EffectiveTLS is the configured TLS mode: starttls unless set, or implicit
// when only port 465 is given.
func (c MailConfig) EffectiveTLS() string {
	mode := strings.ToLower(strings.TrimSpace(c.TLS))
	if mode == "" {
		if c.Port == 465 {
			return MailTLSImplicit
		}
		return MailTLSStartTLS
	}
	return mode
}

// EffectiveTimeout is the configured timeout or DefaultMailTimeout.
func (c MailConfig) EffectiveTimeout() time.Duration {
	if c.Timeout.Duration > 0 {
		return c.Timeout.Duration
	}
	return DefaultMailTimeout
}

func (c MailConfig) validate() error {
	if !c.Enabled() {
		return nil
	}
	host := strings.TrimSpace(c.Host)
	if strings.ContainsAny(host, " \t\r\n/@") {
		return &ConfigError{Field: "mail.host", Message: "must be a host name or IP address"}
	}
	if c.Port < 0 || c.Port > 65535 {
		return &ConfigError{Field: "mail.port", Message: "must be between 1 and 65535 (or 0 for the default)"}
	}
	switch c.EffectiveTLS() {
	case MailTLSStartTLS, MailTLSImplicit, MailTLSNone:
	default:
		return &ConfigError{Field: "mail.tls", Message: fmt.Sprintf("unknown mode %q; use starttls, implicit or none", c.TLS)}
	}
	if strings.TrimSpace(c.From) == "" {
		return &ConfigError{Field: "mail.from", Message: "is required when mail.host is set"}
	}
	if strings.ContainsAny(c.From, "\r\n") {
		return &ConfigError{Field: "mail.from", Message: "must not contain line breaks"}
	}
	if _, err := mail.ParseAddress(c.From); err != nil {
		return &ConfigError{Field: "mail.from", Message: fmt.Sprintf("is not a valid address: %v", err)}
	}
	if strings.ContainsAny(c.Username, "\r\n") || strings.ContainsAny(c.Password, "\r\n") {
		return &ConfigError{Field: "mail.username", Message: "credentials must not contain line breaks"}
	}
	if c.Username != "" && c.EffectiveTLS() == MailTLSNone && !isLocalMailHost(host) {
		return &ConfigError{Field: "mail.tls", Message: "credentials are never sent over an unencrypted connection; use starttls or implicit, or a relay on localhost"}
	}
	if c.Timeout.Duration < 0 || (c.Timeout.Duration > 0 && (c.Timeout.Duration < time.Second || c.Timeout.Duration > 5*time.Minute)) {
		return &ConfigError{Field: "mail.timeout", Message: "must be between 1s and 5m"}
	}
	return nil
}

func isLocalMailHost(host string) bool {
	switch strings.Trim(host, "[]") {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// validatePublicURL checks server.public_url, the externally reachable base
// URL used to build links in emails. It must be an absolute http(s) URL with
// no query or fragment.
func validatePublicURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return &ConfigError{Field: "server.public_url", Message: "must be an absolute http(s) URL such as https://status.example.com"}
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return &ConfigError{Field: "server.public_url", Message: "must not contain credentials, a query or a fragment"}
	}
	return nil
}

// PublicBaseURL returns server.public_url without a trailing slash.
func (c ServerConfig) PublicBaseURL() string {
	return strings.TrimRight(strings.TrimSpace(c.PublicURL), "/")
}

// validateMail checks the mail section and the public URL its links need.
func (c *Config) validateMail() error {
	if err := validatePublicURL(c.Server.PublicURL); err != nil {
		return err
	}
	if err := c.Mail.validate(); err != nil {
		return err
	}
	if c.Mail.Enabled() && c.Server.PublicBaseURL() == "" {
		return &ConfigError{Field: "server.public_url", Message: "is required when mail.host is set: confirmation, unsubscribe and password reset emails link back to this URL"}
	}
	return nil
}
