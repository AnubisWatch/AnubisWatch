package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestGetSystemConfigPath tests getSystemConfigPath function
func TestGetSystemConfigPath(t *testing.T) {
	path := getSystemConfigPath()

	if path == "" {
		t.Error("Expected system config path to not be empty")
	}

	// Verify path contains expected components based on OS
	goos := runtime.GOOS
	switch goos {
	case "windows":
		if !contains(path, "AnubisWatch") {
			t.Error("Expected Windows path to contain AnubisWatch")
		}
	case "darwin":
		if !contains(path, "/Library/Application Support/") {
			t.Error("Expected Darwin path to contain /Library/Application Support/")
		}
	default: // Linux
		if !contains(path, "/etc/anubis/") {
			t.Error("Expected Linux path to contain /etc/anubis/")
		}
	}
}

// TestGetUserConfigPath tests getUserConfigPath function
func TestGetUserConfigPath(t *testing.T) {
	path := getUserConfigPath()

	if path == "" {
		t.Error("Expected user config path to not be empty")
	}

	// Verify path contains expected components based on OS
	goos := runtime.GOOS
	switch goos {
	case "windows":
		if !contains(path, "AnubisWatch") {
			t.Error("Expected Windows path to contain AnubisWatch")
		}
	case "darwin":
		if !contains(path, "Library/Application Support/") {
			t.Error("Expected Darwin path to contain Library/Application Support/")
		}
	default: // Linux
		if !contains(path, ".config") && !contains(path, "/config/") {
			t.Error("Expected Linux path to contain .config or /config/")
		}
	}
}

// TestGetUserConfigPath_XDGConfig tests XDG_CONFIG_HOME handling
func TestGetUserConfigPath_XDGConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping XDG test on Windows")
	}

	// Set XDG_CONFIG_HOME
	origXDG := os.Getenv("XDG_CONFIG_HOME")
	defer os.Setenv("XDG_CONFIG_HOME", origXDG)

	testDir := "/tmp/test-xdg-config"
	os.Setenv("XDG_CONFIG_HOME", testDir)

	path := getUserConfigPath()
	if !contains(path, testDir) {
		t.Errorf("Expected path to contain XDG_CONFIG_HOME %s, got %s", testDir, path)
	}
}

// TestFindConfig_EnvVar tests findConfig with environment variable
func TestFindConfig_EnvVar(t *testing.T) {
	// Set ANUBIS_CONFIG
	origEnv := os.Getenv("ANUBIS_CONFIG")
	defer os.Setenv("ANUBIS_CONFIG", origEnv)

	testPath := "/tmp/test-anubis.json"
	os.Setenv("ANUBIS_CONFIG", testPath)

	result := findConfig()
	if result != testPath {
		t.Errorf("Expected %s, got %s", testPath, result)
	}
}

// TestFindConfig_Default tests findConfig default behavior
func TestFindConfig_Default(t *testing.T) {
	// Clear environment variable
	origEnv := os.Getenv("ANUBIS_CONFIG")
	defer os.Setenv("ANUBIS_CONFIG", origEnv)
	os.Unsetenv("ANUBIS_CONFIG")

	result := findConfig()

	// Should return default path
	if result == "" {
		t.Error("Expected findConfig to return a non-empty path")
	}
}

// TestEnsureConfigDir tests ensureConfigDir function
func TestEnsureConfigDir(t *testing.T) {
	// Test with directory path
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "subdir", "anubis.json")

	err := ensureConfigDir(configPath)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Verify directory was created
	if _, err := os.Stat(filepath.Dir(configPath)); os.IsNotExist(err) {
		t.Error("Expected directory to be created")
	}
}

// TestEnsureConfigDir_CurrentDir tests ensureConfigDir with current directory
func TestEnsureConfigDir_CurrentDir(t *testing.T) {
	err := ensureConfigDir("anubis.json")
	if err != nil {
		t.Errorf("Expected no error for current dir, got %v", err)
	}
}

// TestGetInstanceName tests getInstanceName function
func TestGetInstanceName(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "empty path uses default",
			path:     "",
			expected: "", // Will use findConfig
		},
		{
			name:     "current directory",
			path:     "./anubis.json",
			expected: "default",
		},
		{
			name:     "custom directory",
			path:     "/tmp/myproject/anubis.json",
			expected: "myproject",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getInstanceName(tt.path)
			if tt.expected != "" && result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
			if result == "" {
				t.Error("Expected non-empty instance name")
			}
		})
	}
}

// TestGetInstanceName_UserConfig tests getInstanceName with user config path
func TestGetInstanceName_UserConfig(t *testing.T) {
	userConfig := getUserConfigPath()
	name := getInstanceName(userConfig)

	if name != "user-default" {
		t.Errorf("Expected 'user-default', got %s", name)
	}
}

// TestGetInstanceName_SystemConfig tests getInstanceName with system config path
func TestGetInstanceName_SystemConfig(t *testing.T) {
	systemConfig := getSystemConfigPath()
	name := getInstanceName(systemConfig)

	if name != "system" {
		t.Errorf("Expected 'system', got %s", name)
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
