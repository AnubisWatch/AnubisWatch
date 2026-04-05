package core

import (
	"os"
	"testing"
	"time"
)

func TestExpandEnvVars(t *testing.T) {
	// Set test env vars
	os.Setenv("TEST_VAR", "hello")
	os.Setenv("TEST_DEFAULT", "")
	defer os.Unsetenv("TEST_VAR")
	defer os.Unsetenv("TEST_DEFAULT")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple variable",
			input:    "value: ${TEST_VAR}",
			expected: "value: hello",
		},
		{
			name:     "variable with default",
			input:    "value: ${TEST_DEFAULT:-world}",
			expected: "value: world",
		},
		{
			name:     "variable without default",
			input:    "value: ${NONEXISTENT:-fallback}",
			expected: "value: fallback",
		},
		{
			name:     "no variables",
			input:    "value: static",
			expected: "value: static",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandEnvVars(tt.input)
			if result != tt.expected {
				t.Errorf("expandEnvVars(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestDurationMarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
	}{
		{"seconds", "30s", 30 * time.Second},
		{"minutes", "5m", 5 * time.Minute},
		{"hours", "1h", time.Hour},
		{"complex", "1h30m", 90 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dur, err := time.ParseDuration(tt.input)
			if err != nil {
				t.Fatalf("ParseDuration failed: %v", err)
			}

			if dur != tt.expected {
				t.Errorf("Duration(%q) = %v, want %v", tt.input, dur, tt.expected)
			}
		})
	}
}

func TestConfigDefaults(t *testing.T) {
	config := &Config{}
	config.setDefaults()

	if config.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host = %q, want %q", config.Server.Host, "0.0.0.0")
	}

	if config.Server.Port != 8443 {
		t.Errorf("Server.Port = %d, want %d", config.Server.Port, 8443)
	}

	if config.Storage.Path != "/var/lib/anubis/data" {
		t.Errorf("Storage.Path = %q, want %q", config.Storage.Path, "/var/lib/anubis/data")
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		config    *Config
		wantError bool
	}{
		{
			name: "valid config",
			config: &Config{
				Souls: []Soul{
					{
						Name:   "Test Soul",
						Type:   CheckHTTP,
						Target: "https://example.com",
					},
				},
				Channels: []ChannelConfig{},
				Verdicts: VerdictsConfig{
					Rules: []AlertRule{
						{
							Name:       "Test Rule",
							Conditions: []AlertCondition{{Type: "consecutive_failures", Threshold: 3}},
							Channels:   []string{"channel-1"},
						},
					},
				},
			},
			wantError: false,
		},
		{
			name: "missing soul name",
			config: &Config{
				Souls: []Soul{
					{
						Type:   CheckHTTP,
						Target: "https://example.com",
					},
				},
			},
			wantError: true,
		},
		{
			name: "missing soul target",
			config: &Config{
				Souls: []Soul{
					{Name: "Test", Type: CheckHTTP},
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			if (err != nil) != tt.wantError {
				t.Errorf("validate() error = %v, wantError = %v", err, tt.wantError)
			}
		})
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	// Create temp file
	tmpfile, err := os.CreateTemp("", "anubis-config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	config := &Config{}
	config.setDefaults()
	config.Server.Host = "localhost"
	config.Server.Port = 9090

	// Save config
	if err := SaveConfig(tmpfile.Name(), config); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	// Load config
	loaded, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if loaded.Server.Host != "localhost" {
		t.Errorf("Loaded Server.Host = %q, want %q", loaded.Server.Host, "localhost")
	}

	if loaded.Server.Port != 9090 {
		t.Errorf("Loaded Server.Port = %d, want %d", loaded.Server.Port, 9090)
	}
}

func TestGenerateDefaultConfig(t *testing.T) {
	config := GenerateDefaultConfig()

	if config == nil {
		t.Fatal("Expected non-nil config")
	}

	if config.Server.Host != "0.0.0.0" {
		t.Errorf("Expected Server.Host = 0.0.0.0, got %q", config.Server.Host)
	}

	if config.Server.Port != 8443 {
		t.Errorf("Expected Server.Port = 8443, got %d", config.Server.Port)
	}

	if !config.Server.TLS.Enabled {
		t.Error("Expected Server.TLS.Enabled to be true")
	}

	if config.Storage.Path == "" {
		t.Error("Expected Storage.Path to be set")
	}
}

func TestParseInt(t *testing.T) {
	tests := []struct {
		input    string
		expected int
		hasError bool
	}{
		{"42", 42, false},
		{"0", 0, false},
		{"123456", 123456, false},
		{"", 0, false},
		{"abc", 0, true},
		{"12a34", 0, true},
	}

	for _, tt := range tests {
		result, err := parseInt(tt.input)
		if (err != nil) != tt.hasError {
			t.Errorf("parseInt(%q) error = %v, hasError = %v", tt.input, err, tt.hasError)
		}
		if !tt.hasError && result != tt.expected {
			t.Errorf("parseInt(%q) = %d, want %d", tt.input, result, tt.expected)
		}
	}
}

func TestRaftConfig_Validate(t *testing.T) {
	// Valid config
	cfg := &RaftConfig{
		NodeID:        "node-1",
		BindAddr:      "127.0.0.1:7000",
		AdvertiseAddr: "127.0.0.1:7000",
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}

	// Missing NodeID
	cfg = &RaftConfig{
		BindAddr: "127.0.0.1:7000",
	}
	if err := cfg.Validate(); err == nil {
		t.Error("Expected error for missing NodeID")
	}

	// Missing BindAddr
	cfg = &RaftConfig{
		NodeID: "node-1",
	}
	if err := cfg.Validate(); err == nil {
		t.Error("Expected error for missing BindAddr")
	}

	// Empty AdvertiseAddr should default to BindAddr
	cfg = &RaftConfig{
		NodeID:   "node-1",
		BindAddr: "127.0.0.1:7000",
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}
	if cfg.AdvertiseAddr != "127.0.0.1:7000" {
		t.Errorf("Expected AdvertiseAddr to default to BindAddr")
	}
}

func TestConfig_applyEnvOverrides(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		validate func(*testing.T, *Config)
	}{
		{
			name: "ANUBIS_HOST",
			envVars: map[string]string{"ANUBIS_HOST": "custom-host"},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.Server.Host != "custom-host" {
					t.Errorf("Expected Server.Host = custom-host, got %s", cfg.Server.Host)
				}
			},
		},
		{
			name: "ANUBIS_PORT",
			envVars: map[string]string{"ANUBIS_PORT": "9090"},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.Server.Port != 9090 {
					t.Errorf("Expected Server.Port = 9090, got %d", cfg.Server.Port)
				}
			},
		},
		{
			name: "ANUBIS_DATA_DIR",
			envVars: map[string]string{"ANUBIS_DATA_DIR": "/custom/data"},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.Storage.Path != "/custom/data" {
					t.Errorf("Expected Storage.Path = /custom/data, got %s", cfg.Storage.Path)
				}
			},
		},
		{
			name: "ANUBIS_ENCRYPTION_KEY",
			envVars: map[string]string{"ANUBIS_ENCRYPTION_KEY": "test-key-123"},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.Storage.Encryption.Key != "test-key-123" {
					t.Errorf("Expected Storage.Encryption.Key = test-key-123, got %s", cfg.Storage.Encryption.Key)
				}
				if !cfg.Storage.Encryption.Enabled {
					t.Error("Expected Storage.Encryption.Enabled = true")
				}
			},
		},
		{
			name: "ANUBIS_CLUSTER_SECRET",
			envVars: map[string]string{"ANUBIS_CLUSTER_SECRET": "secret-123"},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.Necropolis.ClusterSecret != "secret-123" {
					t.Errorf("Expected Necropolis.ClusterSecret = secret-123, got %s", cfg.Necropolis.ClusterSecret)
				}
			},
		},
		{
			name: "ANUBIS_ADMIN_PASSWORD",
			envVars: map[string]string{"ANUBIS_ADMIN_PASSWORD": "admin-pass"},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.Auth.Local.AdminPassword != "admin-pass" {
					t.Errorf("Expected Auth.Local.AdminPassword = admin-pass, got %s", cfg.Auth.Local.AdminPassword)
				}
			},
		},
		{
			name: "ANUBIS_LOG_LEVEL",
			envVars: map[string]string{"ANUBIS_LOG_LEVEL": "debug"},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.Logging.Level != "debug" {
					t.Errorf("Expected Logging.Level = debug, got %s", cfg.Logging.Level)
				}
			},
		},
		{
			name: "invalid port",
			envVars: map[string]string{"ANUBIS_PORT": "invalid"},
			validate: func(t *testing.T, cfg *Config) {
				// Invalid port should not update the value (defaults to 0)
				if cfg.Server.Port != 0 {
					t.Errorf("Expected Server.Port = 0 (unchanged), got %d", cfg.Server.Port)
				}
			},
		},
		{
			name: "multiple overrides",
			envVars: map[string]string{
				"ANUBIS_HOST":         "multi-host",
				"ANUBIS_PORT":         "8888",
				"ANUBIS_LOG_LEVEL":    "warn",
				"ANUBIS_ADMIN_PASSWORD": "multi-pass",
			},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.Server.Host != "multi-host" {
					t.Errorf("Expected Server.Host = multi-host, got %s", cfg.Server.Host)
				}
				if cfg.Server.Port != 8888 {
					t.Errorf("Expected Server.Port = 8888, got %d", cfg.Server.Port)
				}
				if cfg.Logging.Level != "warn" {
					t.Errorf("Expected Logging.Level = warn, got %s", cfg.Logging.Level)
				}
				if cfg.Auth.Local.AdminPassword != "multi-pass" {
					t.Errorf("Expected Auth.Local.AdminPassword = multi-pass, got %s", cfg.Auth.Local.AdminPassword)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set env vars
			for k, v := range tt.envVars {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			cfg := &Config{}
			cfg.applyEnvOverrides()

			tt.validate(t, cfg)
		})
	}
}
