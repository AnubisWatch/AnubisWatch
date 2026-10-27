package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMailConfigValidation(t *testing.T) {
	ok := MailConfig{Host: "smtp.example.com", From: "AnubisWatch <status@example.com>"}
	cases := []struct {
		name    string
		mail    MailConfig
		public  string
		wantErr string
	}{
		{"disabled needs nothing", MailConfig{}, "", ""},
		{"valid", ok, "https://status.example.com", ""},
		{"needs public url", ok, "", "server.public_url"},
		{"bad public url", ok, "status.example.com", "server.public_url"},
		{"public url with query", ok, "https://status.example.com/?a=b", "server.public_url"},
		{"missing from", MailConfig{Host: "smtp.example.com"}, "https://s.example.com", "mail.from"},
		{"bad from", MailConfig{Host: "smtp.example.com", From: "not an address"}, "https://s.example.com", "mail.from"},
		{"from injection", MailConfig{Host: "smtp.example.com", From: "a@example.com\r\nBcc: b@example.com"}, "https://s.example.com", "mail.from"},
		{"bad tls", MailConfig{Host: "smtp.example.com", From: "a@example.com", TLS: "ssl"}, "https://s.example.com", "mail.tls"},
		{"bad port", MailConfig{Host: "smtp.example.com", From: "a@example.com", Port: 70000}, "https://s.example.com", "mail.port"},
		{"bad host", MailConfig{Host: "smtp example.com", From: "a@example.com"}, "https://s.example.com", "mail.host"},
		{"credentials in clear", MailConfig{Host: "smtp.example.com", From: "a@example.com", TLS: "none", Username: "u", Password: "p"}, "https://s.example.com", "mail.tls"},
		{"credentials to local relay", MailConfig{Host: "localhost", From: "a@example.com", TLS: "none", Username: "u", Password: "p"}, "https://s.example.com", ""},
		{"timeout too short", MailConfig{Host: "smtp.example.com", From: "a@example.com", Timeout: Duration{time.Millisecond}}, "https://s.example.com", "mail.timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{Mail: tc.mail}
			cfg.Server.PublicURL = tc.public
			err := cfg.validateMail()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want mention of %s", err, tc.wantErr)
			}
		})
	}
}

func TestMailConfigDefaults(t *testing.T) {
	c := MailConfig{Host: "h"}
	if c.EffectivePort() != 587 || c.EffectiveTLS() != MailTLSStartTLS || c.EffectiveTimeout() != DefaultMailTimeout {
		t.Fatalf("defaults = %d %s %s", c.EffectivePort(), c.EffectiveTLS(), c.EffectiveTimeout())
	}
	if (MailConfig{Port: 465}).EffectiveTLS() != MailTLSImplicit || (MailConfig{TLS: "implicit"}).EffectivePort() != 465 {
		t.Fatal("port 465 / implicit defaults wrong")
	}
	if (ServerConfig{PublicURL: " https://x.example.com/ "}).PublicBaseURL() != "https://x.example.com" {
		t.Fatal("PublicBaseURL does not trim")
	}
}

func TestLoadConfigMailPasswordFromEnv(t *testing.T) {
	t.Setenv("TEST_ANUBIS_MAIL_PASSWORD", "s3cret-from-env")
	path := filepath.Join(t.TempDir(), "anubis.yaml")
	data := `server:
  port: 8443
  public_url: https://status.example.com
storage:
  path: ` + t.TempDir() + `
mail:
  host: smtp.example.com
  port: 465
  username: anubis@example.com
  password: ${TEST_ANUBIS_MAIL_PASSWORD}
  from: "AnubisWatch <status@example.com>"
  timeout: 20s
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.Mail.Enabled() || cfg.Mail.Password != "s3cret-from-env" || cfg.Mail.EffectiveTLS() != MailTLSImplicit || cfg.Mail.EffectiveTimeout() != 20*time.Second {
		t.Fatalf("mail = %+v", cfg.Mail)
	}
}
