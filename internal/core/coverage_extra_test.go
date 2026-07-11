package core

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSoulValidationAllProtocolErrors(t *testing.T) {
	base := func(kind CheckType, target string) Soul {
		return Soul{Name: "test", Type: kind, Target: target}
	}
	cases := []struct {
		name string
		soul Soul
	}{
		{"http malformed URL", base(CheckHTTP, "://bad")},
		{"http wrong scheme", base(CheckHTTP, "ftp://example.com")},
		{"http nil config", base(CheckHTTP, "https://example.com")},
		{"http missing method", func() Soul {
			s := base(CheckHTTP, "https://example.com")
			s.HTTP = &HTTPConfig{ValidStatus: []int{200}}
			return s
		}()},
		{"http invalid method", func() Soul {
			s := base(CheckHTTP, "https://example.com")
			s.HTTP = &HTTPConfig{Method: "BOGUS", ValidStatus: []int{200}}
			return s
		}()},
		{"http no statuses", func() Soul {
			s := base(CheckHTTP, "https://example.com")
			s.HTTP = &HTTPConfig{Method: "GET"}
			return s
		}()},
		{"tcp malformed target", func() Soul { s := base(CheckTCP, "host"); s.TCP = &TCPConfig{}; return s }()},
		{"tcp nil config", base(CheckTCP, "host:80")},
		{"udp malformed target", func() Soul { s := base(CheckUDP, "host"); s.UDP = &UDPConfig{}; return s }()},
		{"udp nil config", base(CheckUDP, "host:80")},
		{"dns nil config", base(CheckDNS, "example.com")},
		{"dns missing record", func() Soul { s := base(CheckDNS, "example.com"); s.DNS = &DNSConfig{}; return s }()},
		{"dns invalid record", func() Soul { s := base(CheckDNS, "example.com"); s.DNS = &DNSConfig{RecordType: "BAD"}; return s }()},
		{"smtp malformed target", func() Soul { s := base(CheckSMTP, "host"); s.SMTP = &SMTPConfig{}; return s }()},
		{"smtp nil config", base(CheckSMTP, "host:25")},
		{"imap malformed target", func() Soul { s := base(CheckIMAP, "host"); s.IMAP = &IMAPConfig{}; return s }()},
		{"imap nil config", base(CheckIMAP, "host:143")},
		{"icmp nil config", base(CheckICMP, "127.0.0.1")},
		{"icmp negative count", func() Soul { s := base(CheckICMP, "127.0.0.1"); s.ICMP = &ICMPConfig{Count: -1}; return s }()},
		{"icmp negative interval", func() Soul {
			s := base(CheckICMP, "127.0.0.1")
			s.ICMP = &ICMPConfig{Interval: Duration{Duration: -time.Second}}
			return s
		}()},
		{"icmp low loss", func() Soul { s := base(CheckICMP, "127.0.0.1"); s.ICMP = &ICMPConfig{MaxLossPercent: -1}; return s }()},
		{"icmp high loss", func() Soul { s := base(CheckICMP, "127.0.0.1"); s.ICMP = &ICMPConfig{MaxLossPercent: 101}; return s }()},
		{"grpc malformed target", func() Soul { s := base(CheckGRPC, "host"); s.GRPC = &GRPCConfig{}; return s }()},
		{"grpc nil config", base(CheckGRPC, "host:443")},
		{"websocket malformed URL", func() Soul { s := base(CheckWebSocket, "://bad"); s.WebSocket = &WebSocketConfig{}; return s }()},
		{"websocket wrong scheme", func() Soul {
			s := base(CheckWebSocket, "https://example.com")
			s.WebSocket = &WebSocketConfig{}
			return s
		}()},
		{"websocket nil config", base(CheckWebSocket, "wss://example.com")},
		{"tls malformed target", func() Soul { s := base(CheckTLS, "bad:::"); s.TLS = &TLSConfig{}; return s }()},
		{"tls nil config", base(CheckTLS, "example.com")},
		{"tls negative warning", func() Soul { s := base(CheckTLS, "example.com"); s.TLS = &TLSConfig{ExpiryWarnDays: -1}; return s }()},
		{"tls negative critical", func() Soul { s := base(CheckTLS, "example.com"); s.TLS = &TLSConfig{ExpiryCriticalDays: -1}; return s }()},
		{"tls critical exceeds warning", func() Soul {
			s := base(CheckTLS, "example.com")
			s.TLS = &TLSConfig{ExpiryWarnDays: 1, ExpiryCriticalDays: 2}
			return s
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.soul.Validate(); err == nil {
				t.Fatal("Validate() unexpectedly succeeded")
			}
		})
	}
}

func TestConfigRemainingBranches(t *testing.T) {
	t.Run("data directory environment", func(t *testing.T) {
		t.Setenv("ANUBIS_DATA_DIR", "/tmp/anubis-custom")
		var c Config
		c.setDefaults()
		if c.Storage.Path != "/tmp/anubis-custom" {
			t.Fatalf("path = %q", c.Storage.Path)
		}
	})
	for _, tc := range []struct {
		name string
		auth AuthConfig
	}{
		{"local auto enabled", AuthConfig{Type: "local", Local: LocalAuth{AdminEmail: "a@b.test", AdminPassword: "ValidPassword1!"}}},
		{"oidc auto enabled", AuthConfig{Type: "oidc", OIDC: OIDCAuth{Issuer: "issuer"}}},
		{"ldap auto enabled", AuthConfig{Type: "ldap", LDAP: LDAPAuth{URL: "ldap://host"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{Auth: tc.auth}
			c.setDefaults()
			if c.Auth.Enabled == nil || !*c.Auth.Enabled {
				t.Fatal("auth was not enabled")
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Config)
	}{
		{"server", func(c *Config) { c.Server.Port = -1 }},
		{"storage", func(c *Config) { c.Storage.BTreeOrder = 2 }},
		{"auth", func(c *Config) { c.Auth.Type = "bad" }},
		{"journey", func(c *Config) { c.Journeys = []JourneyConfig{{}} }},
		{"logging", func(c *Config) { c.Logging.Level = "bad" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var c Config
			tc.mutate(&c)
			if c.validate() == nil {
				t.Fatal("validate succeeded")
			}
		})
	}
}

func TestDurationJSONBothUnmarshalFailures(t *testing.T) {
	var d Duration
	if err := d.UnmarshalJSON([]byte(`{}`)); err == nil {
		t.Fatal("object duration succeeded")
	}
}

func TestSaveConfigMarshalFailure(t *testing.T) {
	old := saveConfigMarshal
	saveConfigMarshal = func(_ string, _ *Config) ([]byte, error) {
		return nil, errors.New("marshal injection")
	}
	t.Cleanup(func() { saveConfigMarshal = old })

	path := filepath.Join(t.TempDir(), "test.json")
	if err := SaveConfig(path, &Config{}); err == nil {
		t.Fatal("expected marshal failure")
	}
}

func TestSaveConfigWriteFailure(t *testing.T) {
	var c Config
	path := t.TempDir()
	if err := SaveConfig(path, &c); err == nil {
		t.Fatal("writing to directory succeeded")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

type failingEntropyReader struct{}

func (failingEntropyReader) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }

func TestULIDEntropyFailures(t *testing.T) {
	original := rand.Reader
	rand.Reader = failingEntropyReader{}
	t.Cleanup(func() { rand.Reader = original })

	if got, err := GenerateULIDAt(time.Unix(0, 0)); err == nil || got != ZeroULID {
		t.Fatalf("GenerateULIDAt() = (%v, %v), want zero and error", got, err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("MustGenerateULID did not panic")
		}
	}()
	MustGenerateULID()
}

func TestULIDDecodeInvalidDecodedLength(t *testing.T) {
	if _, err := ulidDecode(strings.Repeat("0", 24)); err == nil {
		t.Fatal("short decoded ULID succeeded")
	}
}

func TestAuthAndPasswordRemainingErrors(t *testing.T) {
	enabled := true
	for _, c := range []AuthConfig{
		{Type: "local", Enabled: &enabled, Local: LocalAuth{AdminPassword: "ValidPassword1!"}},
		{Type: "local", Enabled: &enabled, Local: LocalAuth{AdminEmail: "admin@example.com"}},
	} {
		if err := c.validate(); err == nil {
			t.Fatal("enabled local auth with missing credential succeeded")
		}
	}
	if err := validateAdminPassword("AAAAAAAAAAAA"); err == nil {
		t.Fatal("single-class password succeeded")
	}
}

func TestJSONDurationRejectsArray(t *testing.T) {
	var d Duration
	if err := json.Unmarshal([]byte(`[]`), &d); err == nil {
		t.Fatal("array duration succeeded")
	}
}
