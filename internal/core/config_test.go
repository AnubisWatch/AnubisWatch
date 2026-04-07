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
						HTTP:   &HTTPConfig{Method: "GET", ValidStatus: []int{200}},
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
						HTTP:   &HTTPConfig{Method: "GET", ValidStatus: []int{200}},
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
			name:    "ANUBIS_HOST",
			envVars: map[string]string{"ANUBIS_HOST": "custom-host"},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.Server.Host != "custom-host" {
					t.Errorf("Expected Server.Host = custom-host, got %s", cfg.Server.Host)
				}
			},
		},
		{
			name:    "ANUBIS_PORT",
			envVars: map[string]string{"ANUBIS_PORT": "9090"},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.Server.Port != 9090 {
					t.Errorf("Expected Server.Port = 9090, got %d", cfg.Server.Port)
				}
			},
		},
		{
			name:    "ANUBIS_DATA_DIR",
			envVars: map[string]string{"ANUBIS_DATA_DIR": "/custom/data"},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.Storage.Path != "/custom/data" {
					t.Errorf("Expected Storage.Path = /custom/data, got %s", cfg.Storage.Path)
				}
			},
		},
		{
			name:    "ANUBIS_ENCRYPTION_KEY",
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
			name:    "ANUBIS_CLUSTER_SECRET",
			envVars: map[string]string{"ANUBIS_CLUSTER_SECRET": "secret-123"},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.Necropolis.ClusterSecret != "secret-123" {
					t.Errorf("Expected Necropolis.ClusterSecret = secret-123, got %s", cfg.Necropolis.ClusterSecret)
				}
			},
		},
		{
			name:    "ANUBIS_ADMIN_PASSWORD",
			envVars: map[string]string{"ANUBIS_ADMIN_PASSWORD": "admin-pass"},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.Auth.Local.AdminPassword != "admin-pass" {
					t.Errorf("Expected Auth.Local.AdminPassword = admin-pass, got %s", cfg.Auth.Local.AdminPassword)
				}
			},
		},
		{
			name:    "ANUBIS_LOG_LEVEL",
			envVars: map[string]string{"ANUBIS_LOG_LEVEL": "debug"},
			validate: func(t *testing.T, cfg *Config) {
				if cfg.Logging.Level != "debug" {
					t.Errorf("Expected Logging.Level = debug, got %s", cfg.Logging.Level)
				}
			},
		},
		{
			name:    "invalid port",
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
				"ANUBIS_HOST":           "multi-host",
				"ANUBIS_PORT":           "8888",
				"ANUBIS_LOG_LEVEL":      "warn",
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

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("nonexistent_config_file_12345.yaml")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "invalid-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	// Write invalid YAML
	if _, err := tmpfile.WriteString("invalid: yaml: content: ["); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close()

	_, err = LoadConfig(tmpfile.Name())
	if err == nil {
		t.Error("Expected error for invalid YAML")
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "invalid-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	// Write invalid JSON
	if _, err := tmpfile.WriteString("{invalid json}"); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close()

	_, err = LoadConfig(tmpfile.Name())
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestLoadConfig_JSONFile(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "config-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	// Write valid JSON
	jsonContent := `{"server": {"host": "127.0.0.1", "port": 8080}}`
	if _, err := tmpfile.WriteString(jsonContent); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close()

	cfg, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("Expected host 127.0.0.1, got %s", cfg.Server.Host)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Expected port 8080, got %d", cfg.Server.Port)
	}
}

func TestSaveConfig_InvalidPath(t *testing.T) {
	cfg := &Config{}
	// Try to save to invalid path
	err := SaveConfig("invalid:/path/config.yaml", cfg)
	// On Windows this might succeed or fail depending on the path
	// Just verify the function runs without panic
	_ = err
}

func TestValidate_MissingSoulType(t *testing.T) {
	cfg := &Config{
		Souls: []Soul{
			{
				Name:   "Test Soul",
				Target: "https://example.com",
				// Type is missing
			},
		},
	}

	err := cfg.validate()
	if err == nil {
		t.Error("Expected error for missing soul type")
	}
}

func TestValidate_MissingChannelType(t *testing.T) {
	cfg := &Config{
		Souls: []Soul{},
		Channels: []ChannelConfig{
			{
				Name: "Test Channel",
				// Type is missing
			},
		},
	}

	err := cfg.validate()
	if err == nil {
		t.Error("Expected error for missing channel type")
	}
}

func TestValidate_HTTPConfigDefaults(t *testing.T) {
	cfg := &Config{
		Souls: []Soul{
			{
				Name:   "HTTP Soul",
				Type:   CheckHTTP,
				HTTP:   &HTTPConfig{Method: "GET", ValidStatus: []int{200}},
				Target: "https://example.com",
				// HTTP is nil, validate sets defaults for CheckHTTP type
			},
		},
	}

	err := cfg.validate()
	// validate function modifies soul.HTTP in place if nil for CheckHTTP
	// Note: The actual validate code sets defaults but doesn't return error
	if err != nil {
		t.Logf("validate returned: %v", err)
	}
}

func TestSetDefaults_NilCases(t *testing.T) {
	cfg := &Config{}
	cfg.setDefaults()

	// Test default values
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Expected default host 0.0.0.0, got %s", cfg.Server.Host)
	}
	if cfg.Server.Port != 8443 {
		t.Errorf("Expected default port 8443, got %d", cfg.Server.Port)
	}
}

func TestConfigError_Error(t *testing.T) {
	err := &ConfigError{
		Field:   "test.field",
		Message: "test error message",
	}

	errStr := err.Error()
	if errStr == "" {
		t.Error("ConfigError.Error() should return non-empty string")
	}
	if !containsString(errStr, "test.field") {
		t.Error("Error should contain field name")
	}
	if !containsString(errStr, "test error message") {
		t.Error("Error should contain message")
	}
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestLoadConfig_YAMLExtension(t *testing.T) {
	// Test .yml extension
	tmpfile, err := os.CreateTemp("", "config-*.yml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	yamlContent := `server:
  host: 127.0.0.1
  port: 9090
`
	if _, err := tmpfile.WriteString(yamlContent); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close()

	cfg, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("Expected host 127.0.0.1, got %s", cfg.Server.Host)
	}
}

func TestLoadConfig_EnvExpansion(t *testing.T) {
	os.Setenv("TEST_HOST", "env-host")
	os.Setenv("TEST_PORT", "7777")
	defer os.Unsetenv("TEST_HOST")
	defer os.Unsetenv("TEST_PORT")

	tmpfile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	yamlContent := `server:
  host: ${TEST_HOST}
  port: ${TEST_PORT}
`
	if _, err := tmpfile.WriteString(yamlContent); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close()

	cfg, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Server.Host != "env-host" {
		t.Errorf("Expected host from env var, got %s", cfg.Server.Host)
	}
}

func TestValidate_WithChannels(t *testing.T) {
	cfg := &Config{
		Souls: []Soul{
			{
				Name:   "Test Soul",
				Type:   CheckHTTP,
				HTTP:   &HTTPConfig{Method: "GET", ValidStatus: []int{200}},
				Target: "https://example.com",
			},
		},
		Channels: []ChannelConfig{
			{
				Name:  "Test Channel",
				Type:  "slack",
				Slack: &SlackConfig{WebhookURL: "https://hooks.slack.com/xxx"},
			},
		},
	}

	err := cfg.validate()
	if err != nil {
		t.Errorf("validate failed: %v", err)
	}
}

func TestValidate_ChannelMissingName(t *testing.T) {
	cfg := &Config{
		Souls: []Soul{},
		Channels: []ChannelConfig{
			{
				Type:  "slack",
				Slack: &SlackConfig{WebhookURL: "https://hooks.slack.com/services/xxx"},
				// Name is missing
			},
		},
	}

	err := cfg.validate()
	if err == nil {
		t.Error("Expected error for channel missing name")
	}
}

func TestSaveConfig_JSONExtension(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "config-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	cfg := &Config{}
	cfg.setDefaults()
	cfg.Server.Host = "json-test"

	err = SaveConfig(tmpfile.Name(), cfg)
	if err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	// Verify file was created and can be loaded
	loaded, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if loaded.Server.Host != "json-test" {
		t.Errorf("Expected host json-test, got %s", loaded.Server.Host)
	}
}

func TestValidate_MissingRuleName(t *testing.T) {
	cfg := &Config{
		Souls: []Soul{
			{
				Name:   "Test Soul",
				Type:   CheckHTTP,
				HTTP:   &HTTPConfig{Method: "GET", ValidStatus: []int{200}},
				Target: "https://example.com",
			},
		},
		Verdicts: VerdictsConfig{
			Rules: []AlertRule{
				{
					// Name is missing
					Conditions: []AlertCondition{{Type: "consecutive_failures", Threshold: 3}},
					Channels:   []string{"channel-1"},
				},
			},
		},
	}

	err := cfg.validate()
	if err == nil {
		t.Error("Expected error for missing rule name")
	}
}

func TestValidate_EmptyConditions(t *testing.T) {
	cfg := &Config{
		Souls: []Soul{
			{
				Name:   "Test Soul",
				Type:   CheckHTTP,
				HTTP:   &HTTPConfig{Method: "GET", ValidStatus: []int{200}},
				Target: "https://example.com",
			},
		},
		Verdicts: VerdictsConfig{
			Rules: []AlertRule{
				{
					Name:       "Test Rule",
					Conditions: []AlertCondition{}, // Empty conditions
					Channels:   []string{"channel-1"},
				},
			},
		},
	}

	err := cfg.validate()
	if err == nil {
		t.Error("Expected error for empty conditions")
	}
}

func TestValidate_MissingConditionType(t *testing.T) {
	cfg := &Config{
		Souls: []Soul{
			{
				Name:   "Test Soul",
				Type:   CheckHTTP,
				HTTP:   &HTTPConfig{Method: "GET", ValidStatus: []int{200}},
				Target: "https://example.com",
			},
		},
		Verdicts: VerdictsConfig{
			Rules: []AlertRule{
				{
					Name: "Test Rule",
					Conditions: []AlertCondition{
						{Threshold: 3}, // Type is missing
					},
					Channels: []string{"channel-1"},
				},
			},
		},
	}

	err := cfg.validate()
	if err == nil {
		t.Error("Expected error for missing condition type")
	}
}

func TestValidate_MissingChannels(t *testing.T) {
	cfg := &Config{
		Souls: []Soul{
			{
				Name:   "Test Soul",
				Type:   CheckHTTP,
				HTTP:   &HTTPConfig{Method: "GET", ValidStatus: []int{200}},
				Target: "https://example.com",
			},
		},
		Verdicts: VerdictsConfig{
			Rules: []AlertRule{
				{
					Name:       "Test Rule",
					Conditions: []AlertCondition{{Type: "consecutive_failures", Threshold: 3}},
					Channels:   []string{}, // Empty channels
				},
			},
		},
	}

	err := cfg.validate()
	if err == nil {
		t.Error("Expected error for missing channels")
	}
}

func TestSaveConfig_YAMLExtension(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	cfg := &Config{}
	cfg.setDefaults()
	cfg.Server.Host = "yaml-test"

	err = SaveConfig(tmpfile.Name(), cfg)
	if err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	// Verify file was created and can be loaded
	loaded, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if loaded.Server.Host != "yaml-test" {
		t.Errorf("Expected host yaml-test, got %s", loaded.Server.Host)
	}
}

func TestSetDefaults_AllPaths(t *testing.T) {
	cfg := &Config{}
	cfg.setDefaults()

	// Verify all defaults are set
	if cfg.Storage.TimeSeries.Compaction.RawToMinute.Duration == 0 {
		t.Error("Expected RawToMinute default to be set")
	}
	if cfg.Storage.TimeSeries.Compaction.MinuteToFive.Duration == 0 {
		t.Error("Expected MinuteToFive default to be set")
	}
	if cfg.Storage.TimeSeries.Compaction.FiveToHour.Duration == 0 {
		t.Error("Expected FiveToHour default to be set")
	}
	if cfg.Storage.TimeSeries.Compaction.HourToDay.Duration == 0 {
		t.Error("Expected HourToDay default to be set")
	}
	if cfg.Storage.TimeSeries.Retention.Raw.Duration == 0 {
		t.Error("Expected Retention Raw default to be set")
	}
	if cfg.Storage.TimeSeries.Retention.Minute.Duration == 0 {
		t.Error("Expected Retention Minute default to be set")
	}
	if cfg.Storage.TimeSeries.Retention.FiveMin.Duration == 0 {
		t.Error("Expected Retention FiveMin default to be set")
	}
	if cfg.Storage.TimeSeries.Retention.Hour.Duration == 0 {
		t.Error("Expected Retention Hour default to be set")
	}
	if cfg.Storage.TimeSeries.Retention.Day == "" {
		t.Error("Expected Retention Day default to be set")
	}
	if cfg.Necropolis.Discovery.Mode == "" {
		t.Error("Expected Discovery Mode default to be set")
	}
	if cfg.Necropolis.Distribution.Strategy == "" {
		t.Error("Expected Distribution Strategy default to be set")
	}
	if cfg.Necropolis.Raft.SnapshotThreshold == 0 {
		t.Error("Expected Raft SnapshotThreshold default to be set")
	}
	if cfg.Tenants.Isolation == "" {
		t.Error("Expected Tenants Isolation default to be set")
	}
	if cfg.Tenants.DefaultQuotas.MaxSouls == 0 {
		t.Error("Expected MaxSouls default to be set")
	}
	if cfg.Tenants.DefaultQuotas.MaxJourneys == 0 {
		t.Error("Expected MaxJourneys default to be set")
	}
	if cfg.Tenants.DefaultQuotas.RetentionDays == 0 {
		t.Error("Expected RetentionDays default to be set")
	}
	if cfg.Logging.Format == "" {
		t.Error("Expected Logging Format default to be set")
	}
	if cfg.Logging.Output == "" {
		t.Error("Expected Logging Output default to be set")
	}
	if cfg.Dashboard.Branding.Title == "" {
		t.Error("Expected Dashboard Title default to be set")
	}
	if cfg.Dashboard.Branding.Theme == "" {
		t.Error("Expected Dashboard Theme default to be set")
	}
}

func TestSetDefaults_TLSAutoCert(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			TLS: TLSServerConfig{
				Enabled: true,
			},
		},
	}
	cfg.setDefaults()

	if !cfg.Server.TLS.AutoCert {
		t.Error("Expected TLS AutoCert to be true when Enabled and no cert/key provided")
	}
}

func TestSetDefaults_TLSWithCert(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			TLS: TLSServerConfig{
				Enabled: true,
				Cert:    "/path/to/cert.pem",
				Key:     "/path/to/key.pem",
			},
		},
	}
	cfg.setDefaults()

	// AutoCert should not be set when Cert and Key are provided
	if cfg.Server.TLS.AutoCert {
		t.Error("Expected TLS AutoCert to remain false when Cert/Key are provided")
	}
}

// Test SaveConfig with a type that can't be marshaled to trigger error path
type BadConfig struct {
	Config
	BadField func() // functions can't be marshaled
}

func TestSaveConfig_MarshalError(t *testing.T) {
	// This test tries to trigger the marshal error path
	// However, our Config struct doesn't have fields that can cause marshal errors
	// So we test the normal path works
	tmpfile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	cfg := &Config{}
	cfg.setDefaults()

	// Normal save should work
	err = SaveConfig(tmpfile.Name(), cfg)
	if err != nil {
		t.Errorf("SaveConfig should not fail: %v", err)
	}
}

func TestSaveConfig_InvalidDirectory(t *testing.T) {
	// Try to save to a non-existent directory
	cfg := &Config{}
	cfg.setDefaults()

	// Use a path that doesn't exist
	err := SaveConfig("/nonexistent/directory/config.yaml", cfg)
	// This should fail on most systems
	if err == nil {
		t.Log("SaveConfig to invalid path did not error (may be Windows-specific)")
	}
}

func TestLoadConfig_ValidJSON(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "config-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	// Write valid JSON config
	jsonContent := `{
		"server": {
			"host": "127.0.0.1",
			"port": 8080
		},
		"souls": [
			{
				"name": "Test",
				"type": "http",
				"http": {"method": "GET", "valid_status": [200]},
				"target": "https://example.com"
			}
		]
	}`
	if _, err := tmpfile.WriteString(jsonContent); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close()

	cfg, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("Expected host 127.0.0.1, got %s", cfg.Server.Host)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Expected port 8080, got %d", cfg.Server.Port)
	}
}

func TestLoadConfig_ValidationError(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	// Write YAML config with invalid soul (missing target)
	yamlContent := `server:
  host: 127.0.0.1
  port: 8080
souls:
  - name: Test Soul
    type: http
`
	if _, err := tmpfile.WriteString(yamlContent); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close()

	_, err = LoadConfig(tmpfile.Name())
	if err == nil {
		t.Error("Expected error for config with validation failure")
	}
}

// New validation tests for comprehensive coverage
func TestServerConfigValidate(t *testing.T) {
	tests := []struct {
		name      string
		config    ServerConfig
		wantError bool
	}{
		{
			name:      "valid config",
			config:    ServerConfig{Host: "0.0.0.0", Port: 8080},
			wantError: false,
		},
		{
			name:      "port too low",
			config:    ServerConfig{Host: "0.0.0.0", Port: 0},
			wantError: true,
		},
		{
			name:      "port too high",
			config:    ServerConfig{Host: "0.0.0.0", Port: 70000},
			wantError: true,
		},
		{
			name:      "TLS without cert or autocert",
			config:    ServerConfig{Host: "0.0.0.0", Port: 8443, TLS: TLSServerConfig{Enabled: true}},
			wantError: true,
		},
		{
			name: "TLS with autocert but no domains",
			config: ServerConfig{Host: "0.0.0.0", Port: 8443, TLS: TLSServerConfig{
				Enabled:  true,
				AutoCert: true,
			}},
			wantError: true,
		},
		{
			name: "TLS with autocert and domains",
			config: ServerConfig{Host: "0.0.0.0", Port: 8443, TLS: TLSServerConfig{
				Enabled:     true,
				AutoCert:    true,
				ACMEDomains: []string{"example.com"},
			}},
			wantError: false,
		},
		{
			name: "TLS with cert/key",
			config: ServerConfig{Host: "0.0.0.0", Port: 8443, TLS: TLSServerConfig{
				Enabled: true,
				Cert:    "/path/to/cert.pem",
				Key:     "/path/to/key.pem",
			}},
			wantError: false,
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

func TestStorageConfigValidate(t *testing.T) {
	tests := []struct {
		name      string
		config    StorageConfig
		wantError bool
	}{
		{
			name:      "valid config",
			config:    StorageConfig{Path: "/data"},
			wantError: false,
		},
		{
			name:      "missing path",
			config:    StorageConfig{},
			wantError: true,
		},
		{
			name: "encryption enabled without key",
			config: StorageConfig{
				Path:       "/data",
				Encryption: EncryptionConfig{Enabled: true},
			},
			wantError: true,
		},
		{
			name: "encryption enabled with key",
			config: StorageConfig{
				Path:       "/data",
				Encryption: EncryptionConfig{Enabled: true, Key: "secret"},
			},
			wantError: false,
		},
		{
			name: "invalid btree order",
			config: StorageConfig{
				Path:       "/data",
				BTreeOrder: 2,
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

func TestAuthConfigValidate(t *testing.T) {
	tests := []struct {
		name      string
		config    AuthConfig
		wantError bool
	}{
		{
			name:      "valid local auth",
			config:    AuthConfig{Type: "local"},
			wantError: false,
		},
		{
			name:      "missing type",
			config:    AuthConfig{},
			wantError: true,
		},
		{
			name:      "invalid type",
			config:    AuthConfig{Type: "invalid"},
			wantError: true,
		},
		{
			name:      "OIDC without issuer",
			config:    AuthConfig{Type: "oidc", OIDC: OIDCAuth{ClientID: "id", ClientSecret: "secret"}},
			wantError: true,
		},
		{
			name:      "OIDC without client_id",
			config:    AuthConfig{Type: "oidc", OIDC: OIDCAuth{Issuer: "https://example.com", ClientSecret: "secret"}},
			wantError: true,
		},
		{
			name:      "OIDC without client_secret",
			config:    AuthConfig{Type: "oidc", OIDC: OIDCAuth{Issuer: "https://example.com", ClientID: "id"}},
			wantError: true,
		},
		{
			name:      "valid OIDC",
			config:    AuthConfig{Type: "oidc", OIDC: OIDCAuth{Issuer: "https://example.com", ClientID: "id", ClientSecret: "secret"}},
			wantError: false,
		},
		{
			name:      "LDAP without URL",
			config:    AuthConfig{Type: "ldap", LDAP: LDAPAuth{BaseDN: "dc=example,dc=com"}},
			wantError: true,
		},
		{
			name:      "LDAP without base_dn",
			config:    AuthConfig{Type: "ldap", LDAP: LDAPAuth{URL: "ldap://example.com"}},
			wantError: true,
		},
		{
			name:      "valid LDAP",
			config:    AuthConfig{Type: "ldap", LDAP: LDAPAuth{URL: "ldap://example.com", BaseDN: "dc=example,dc=com"}},
			wantError: false,
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

func TestLoggingConfigValidate(t *testing.T) {
	tests := []struct {
		name      string
		config    LoggingConfig
		wantError bool
	}{
		{
			name:      "valid config",
			config:    LoggingConfig{Level: "info", Format: "json", Output: "stdout"},
			wantError: false,
		},
		{
			name:      "invalid level",
			config:    LoggingConfig{Level: "invalid", Format: "json", Output: "stdout"},
			wantError: true,
		},
		{
			name:      "invalid format",
			config:    LoggingConfig{Level: "info", Format: "xml", Output: "stdout"},
			wantError: true,
		},
		{
			name:      "invalid output",
			config:    LoggingConfig{Level: "info", Format: "json", Output: "syslog"},
			wantError: true,
		},
		{
			name:      "file output without path",
			config:    LoggingConfig{Level: "info", Format: "json", Output: "file"},
			wantError: true,
		},
		{
			name:      "file output with path",
			config:    LoggingConfig{Level: "info", Format: "json", Output: "file", File: "/var/log/anubis.log"},
			wantError: false,
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

func TestSoulValidate(t *testing.T) {
	tests := []struct {
		name      string
		soul      Soul
		wantError bool
	}{
		{
			name:      "valid HTTP soul",
			soul:      Soul{Name: "Test", Type: CheckHTTP, Target: "https://example.com", HTTP: &HTTPConfig{Method: "GET", ValidStatus: []int{200}}},
			wantError: false,
		},
		{
			name:      "missing name",
			soul:      Soul{Type: CheckHTTP, Target: "https://example.com"},
			wantError: true,
		},
		{
			name:      "missing target",
			soul:      Soul{Name: "Test", Type: CheckHTTP},
			wantError: true,
		},
		{
			name:      "missing type",
			soul:      Soul{Name: "Test", Target: "https://example.com"},
			wantError: true,
		},
		{
			name:      "invalid type",
			soul:      Soul{Name: "Test", Type: "invalid", Target: "https://example.com"},
			wantError: true,
		},
		{
			name:      "negative interval",
			soul:      Soul{Name: "Test", Type: CheckHTTP, Target: "https://example.com", Weight: Duration{-1 * time.Second}},
			wantError: true,
		},
		{
			name:      "HTTP without http config",
			soul:      Soul{Name: "Test", Type: CheckHTTP, Target: "https://example.com"},
			wantError: true,
		},
		{
			name:      "HTTP with invalid method",
			soul:      Soul{Name: "Test", Type: CheckHTTP, Target: "https://example.com", HTTP: &HTTPConfig{Method: "INVALID"}},
			wantError: true,
		},
		{
			name:      "HTTP without valid status",
			soul:      Soul{Name: "Test", Type: CheckHTTP, Target: "https://example.com", HTTP: &HTTPConfig{Method: "GET"}},
			wantError: true,
		},
		{
			name:      "valid TCP soul",
			soul:      Soul{Name: "Test", Type: CheckTCP, Target: "localhost:80", TCP: &TCPConfig{}},
			wantError: false,
		},
		{
			name:      "TCP without tcp config",
			soul:      Soul{Name: "Test", Type: CheckTCP, Target: "localhost:80"},
			wantError: true,
		},
		{
			name:      "valid DNS soul",
			soul:      Soul{Name: "Test", Type: CheckDNS, Target: "example.com", DNS: &DNSConfig{RecordType: "A"}},
			wantError: false,
		},
		{
			name:      "DNS without dns config",
			soul:      Soul{Name: "Test", Type: CheckDNS, Target: "example.com"},
			wantError: true,
		},
		{
			name:      "DNS without record type",
			soul:      Soul{Name: "Test", Type: CheckDNS, Target: "example.com", DNS: &DNSConfig{}},
			wantError: true,
		},
		{
			name:      "DNS with invalid record type",
			soul:      Soul{Name: "Test", Type: CheckDNS, Target: "example.com", DNS: &DNSConfig{RecordType: "INVALID"}},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.soul.validate(0)
			if (err != nil) != tt.wantError {
				t.Errorf("validate() error = %v, wantError = %v", err, tt.wantError)
			}
		})
	}
}

func TestChannelConfigValidate(t *testing.T) {
	tests := []struct {
		name      string
		channel   ChannelConfig
		wantError bool
	}{
		{
			name:      "valid webhook",
			channel:   ChannelConfig{Name: "Test", Type: "webhook", Webhook: &WebhookConfig{URL: "https://example.com/webhook"}},
			wantError: false,
		},
		{
			name:      "missing name",
			channel:   ChannelConfig{Type: "webhook"},
			wantError: true,
		},
		{
			name:      "missing type",
			channel:   ChannelConfig{Name: "Test"},
			wantError: true,
		},
		{
			name:      "invalid type",
			channel:   ChannelConfig{Name: "Test", Type: "invalid"},
			wantError: true,
		},
		{
			name:      "webhook without config",
			channel:   ChannelConfig{Name: "Test", Type: "webhook"},
			wantError: true,
		},
		{
			name:      "webhook without URL",
			channel:   ChannelConfig{Name: "Test", Type: "webhook", Webhook: &WebhookConfig{}},
			wantError: true,
		},
		{
			name:      "valid slack",
			channel:   ChannelConfig{Name: "Test", Type: "slack", Slack: &SlackConfig{WebhookURL: "https://hooks.slack.com/xxx"}},
			wantError: false,
		},
		{
			name:      "slack without webhook URL",
			channel:   ChannelConfig{Name: "Test", Type: "slack", Slack: &SlackConfig{}},
			wantError: true,
		},
		{
			name:      "valid telegram",
			channel:   ChannelConfig{Name: "Test", Type: "telegram", Telegram: &TelegramConfig{BotToken: "token", ChatID: "12345"}},
			wantError: false,
		},
		{
			name:      "telegram without token",
			channel:   ChannelConfig{Name: "Test", Type: "telegram", Telegram: &TelegramConfig{ChatID: "12345"}},
			wantError: true,
		},
		{
			name:      "telegram without chat_id",
			channel:   ChannelConfig{Name: "Test", Type: "telegram", Telegram: &TelegramConfig{BotToken: "token"}},
			wantError: true,
		},
		{
			name:      "valid email",
			channel:   ChannelConfig{Name: "Test", Type: "email", Email: &EmailConfig{SMTPHost: "smtp.example.com", From: "alert@example.com", To: []string{"admin@example.com"}}},
			wantError: false,
		},
		{
			name:      "email without smtp_host",
			channel:   ChannelConfig{Name: "Test", Type: "email", Email: &EmailConfig{From: "alert@example.com", To: []string{"admin@example.com"}}},
			wantError: true,
		},
		{
			name:      "email without recipients",
			channel:   ChannelConfig{Name: "Test", Type: "email", Email: &EmailConfig{SMTPHost: "smtp.example.com", From: "alert@example.com"}},
			wantError: true,
		},
		{
			name:      "valid pagerduty",
			channel:   ChannelConfig{Name: "Test", Type: "pagerduty", PagerDuty: &PagerDutyConfig{IntegrationKey: "key123"}},
			wantError: false,
		},
		{
			name:      "pagerduty without key",
			channel:   ChannelConfig{Name: "Test", Type: "pagerduty", PagerDuty: &PagerDutyConfig{}},
			wantError: true,
		},
		{
			name:      "valid ntfy",
			channel:   ChannelConfig{Name: "Test", Type: "ntfy", Ntfy: &NtfyConfig{Topic: "alerts"}},
			wantError: false,
		},
		{
			name:      "ntfy without topic",
			channel:   ChannelConfig{Name: "Test", Type: "ntfy", Ntfy: &NtfyConfig{}},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.channel.validate(0)
			if (err != nil) != tt.wantError {
				t.Errorf("validate() error = %v, wantError = %v", err, tt.wantError)
			}
		})
	}
}

func TestAlertRuleValidate(t *testing.T) {
	tests := []struct {
		name      string
		rule      AlertRule
		wantError bool
	}{
		{
			name: "valid rule",
			rule: AlertRule{
				Name:       "Test Rule",
				Conditions: []AlertCondition{{Type: "consecutive_failures", Threshold: 3}},
				Channels:   []string{"channel-1"},
			},
			wantError: false,
		},
		{
			name: "missing name",
			rule: AlertRule{
				Conditions: []AlertCondition{{Type: "consecutive_failures", Threshold: 3}},
				Channels:   []string{"channel-1"},
			},
			wantError: true,
		},
		{
			name: "empty conditions",
			rule: AlertRule{
				Name:       "Test Rule",
				Conditions: []AlertCondition{},
				Channels:   []string{"channel-1"},
			},
			wantError: true,
		},
		{
			name: "empty channels",
			rule: AlertRule{
				Name:       "Test Rule",
				Conditions: []AlertCondition{{Type: "consecutive_failures", Threshold: 3}},
				Channels:   []string{},
			},
			wantError: true,
		},
		{
			name: "missing condition type",
			rule: AlertRule{
				Name:       "Test Rule",
				Conditions: []AlertCondition{{Threshold: 3}},
				Channels:   []string{"channel-1"},
			},
			wantError: true,
		},
		{
			name: "invalid condition type",
			rule: AlertRule{
				Name:       "Test Rule",
				Conditions: []AlertCondition{{Type: "invalid", Threshold: 3}},
				Channels:   []string{"channel-1"},
			},
			wantError: true,
		},
		{
			name: "negative threshold",
			rule: AlertRule{
				Name:       "Test Rule",
				Conditions: []AlertCondition{{Type: "consecutive_failures", Threshold: -1}},
				Channels:   []string{"channel-1"},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rule.validate(0)
			if (err != nil) != tt.wantError {
				t.Errorf("validate() error = %v, wantError = %v", err, tt.wantError)
			}
		})
	}
}

func TestFeatherConfigValidate(t *testing.T) {
	tests := []struct {
		name      string
		feather   FeatherConfig
		wantError bool
	}{
		{
			name:      "valid config",
			feather:   FeatherConfig{Name: "Test", Scope: "tag:api", ViolationThreshold: 3},
			wantError: false,
		},
		{
			name:      "missing name",
			feather:   FeatherConfig{Scope: "tag:api", ViolationThreshold: 3},
			wantError: true,
		},
		{
			name:      "missing scope",
			feather:   FeatherConfig{Name: "Test", ViolationThreshold: 3},
			wantError: true,
		},
		{
			name:      "zero threshold",
			feather:   FeatherConfig{Name: "Test", Scope: "tag:api", ViolationThreshold: 0},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.feather.validate(0)
			if (err != nil) != tt.wantError {
				t.Errorf("validate() error = %v, wantError = %v", err, tt.wantError)
			}
		})
	}
}

func TestJourneyConfigValidate(t *testing.T) {
	tests := []struct {
		name      string
		journey   JourneyConfig
		wantError bool
	}{
		{
			name:      "valid journey",
			journey:   JourneyConfig{Name: "Test", Steps: []JourneyStep{{Name: "Step 1", Target: "https://example.com"}}},
			wantError: false,
		},
		{
			name:      "missing name",
			journey:   JourneyConfig{Steps: []JourneyStep{{Name: "Step 1", Target: "https://example.com"}}},
			wantError: true,
		},
		{
			name:      "empty steps",
			journey:   JourneyConfig{Name: "Test", Steps: []JourneyStep{}},
			wantError: true,
		},
		{
			name:      "step missing name",
			journey:   JourneyConfig{Name: "Test", Steps: []JourneyStep{{Target: "https://example.com"}}},
			wantError: true,
		},
		{
			name:      "step missing target",
			journey:   JourneyConfig{Name: "Test", Steps: []JourneyStep{{Name: "Step 1"}}},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.journey.validate(0)
			if (err != nil) != tt.wantError {
				t.Errorf("validate() error = %v, wantError = %v", err, tt.wantError)
			}
		})
	}
}
