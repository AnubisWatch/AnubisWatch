package core

import (
	"crypto/tls"
	"testing"
)

// server.tls.min_version was parsed into the config struct but never read by
// any listener, so an operator who set it got whatever the server happened to
// hardcode. These tests pin the mapping now that it is actually wired up.
func TestTLSServerConfigResolveMinVersion(t *testing.T) {
	tests := []struct {
		name       string
		minVersion int
		want       uint16
	}{
		{"unset floors at TLS 1.2", 0, tls.VersionTLS12},
		{"selector 3 is TLS 1.2", 3, tls.VersionTLS12},
		{"selector 4 is TLS 1.3", 4, tls.VersionTLS13},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := TLSServerConfig{MinVersion: tt.minVersion}
			if got := cfg.ResolveMinVersion(); got != tt.want {
				t.Errorf("ResolveMinVersion() = %#04x, want %#04x", got, tt.want)
			}
		})
	}
}

func TestServerConfigValidateMinVersion(t *testing.T) {
	tests := []struct {
		name       string
		minVersion int
		wantErr    bool
	}{
		{"unset is accepted", 0, false},
		{"TLS 1.2 is accepted", 3, false},
		{"TLS 1.3 is accepted", 4, false},
		{"TLS 1.0 is rejected", 1, true},
		{"TLS 1.1 is rejected", 2, true},
		{"unknown selector is rejected", 5, true},
		{"negative selector is rejected", -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ServerConfig{
				Port: 8443,
				TLS:  TLSServerConfig{MinVersion: tt.minVersion},
			}
			err := cfg.validate()
			if tt.wantErr && err == nil {
				t.Fatalf("validate() = nil, want an error for min_version=%d", tt.minVersion)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validate() = %v, want nil for min_version=%d", err, tt.minVersion)
			}
		})
	}
}

// The selector is validated even when TLS is switched off, so a bad value is
// caught at load time rather than on the day someone enables TLS.
func TestServerConfigValidateMinVersionWhenTLSDisabled(t *testing.T) {
	cfg := ServerConfig{
		Port: 8443,
		TLS:  TLSServerConfig{Enabled: false, MinVersion: 1},
	}
	if err := cfg.validate(); err == nil {
		t.Fatal("validate() = nil, want an error: min_version=1 (TLS 1.0) must be rejected even with tls.enabled=false")
	}
}
