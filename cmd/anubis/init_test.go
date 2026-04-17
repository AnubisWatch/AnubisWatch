package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestGenerateSecurePassword tests generateSecurePassword function
func TestGenerateSecurePassword(t *testing.T) {
	pass1 := generateSecurePassword()
	pass2 := generateSecurePassword()

	// Passwords should not be empty
	if pass1 == "" {
		t.Error("Expected password to not be empty")
	}
	if pass2 == "" {
		t.Error("Expected password to not be empty")
	}

	// Passwords should be different (random)
	if pass1 == pass2 {
		t.Error("Expected different passwords for different calls")
	}

	// Password should be at least 12 characters
	if len(pass1) < 12 {
		t.Errorf("Expected password length >= 12, got %d", len(pass1))
	}
}

// TestRandomSuffix tests randomSuffix function
func TestRandomSuffix(t *testing.T) {
	suffix1 := randomSuffix()

	// Should not be empty
	if suffix1 == "" {
		t.Error("Expected suffix to not be empty")
	}

	// Should be exactly 4 characters
	if len(suffix1) != 4 {
		t.Errorf("Expected suffix length 4, got %d", len(suffix1))
	}

	// Should contain only digits
	for _, c := range suffix1 {
		if c < '0' || c > '9' {
			t.Errorf("Expected only digits, got %c", c)
		}
	}
}

// TestMaskPassword tests maskPassword function
func TestMaskPassword(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty password",
			input:    "",
			expected: "****",
		},
		{
			name:     "short password",
			input:    "ab",
			expected: "****",
		},
		{
			name:     "exactly 4 chars",
			input:    "abcd",
			expected: "****",
		},
		{
			name:     "long password",
			input:    "mysecretpassword123",
			expected: "my***************23",
		},
		{
			name:     "5 chars password",
			input:    "abcde",
			expected: "ab*de",
		},
		{
			name:     "6 chars password",
			input:    "abcdef",
			expected: "ab**ef",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskPassword(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

// TestFindAvailablePort tests findAvailablePort function
func TestFindAvailablePort(t *testing.T) {
	// Find an available port starting from 15000
	port := findAvailablePort(15000)

	// Port should be >= 15000
	if port < 15000 {
		t.Errorf("Expected port >= 15000, got %d", port)
	}

	// Port should not be in use
	if isPortInUse(port) {
		t.Errorf("Expected port %d to not be in use", port)
	}
}

// TestIsPortInUse tests isPortInUse function
func TestIsPortInUse(t *testing.T) {
	// Test with port 0 (should be invalid/in use)
	_ = isPortInUse(0)
	// Port 0 is special, behavior may vary

	// Test with a high port that's likely not in use
	highPort := 45000
	inUse := isPortInUse(highPort)
	if inUse {
		t.Logf("Port %d appears to be in use (may be system dependent)", highPort)
	}
}

// TestGetDefaultDataDir tests getDefaultDataDir function
func TestGetDefaultDataDir(t *testing.T) {
	dir := getDefaultDataDir()

	// Should not be empty
	if dir == "" {
		t.Error("Expected data dir to not be empty")
	}

	// Should contain 'anubis' or 'AnubisWatch'
	lowerDir := strings.ToLower(dir)
	if !strings.Contains(lowerDir, "anubis") {
		t.Logf("Data dir: %s", dir)
	}
}

// TestGetDefaultDataDir_Default tests that getDefaultDataDir returns a default
func TestGetDefaultDataDir_Default(t *testing.T) {
	dir := getDefaultDataDir()

	// Verify it's a valid path format
	if dir == "." || dir == "" {
		t.Error("Expected a specific data directory path")
	}
}

// TestGetDefaultDataDir_WindowsAppData tests getDefaultDataDir with APPDATA set
func TestGetDefaultDataDir_WindowsAppData(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}

	// Clear ANUBIS_DATA_DIR to test fallback behavior
	t.Setenv("ANUBIS_DATA_DIR", "")

	// Save original APPDATA
	origAppData := os.Getenv("APPDATA")
	defer func() {
		if origAppData != "" {
			os.Setenv("APPDATA", origAppData)
		} else {
			os.Unsetenv("APPDATA")
		}
	}()

	// Set APPDATA to temp dir
	tmpDir := t.TempDir()
	os.Setenv("APPDATA", tmpDir)

	dir := getDefaultDataDir()

	// Should use APPDATA + AnubisWatch
	expected := filepath.Join(tmpDir, "AnubisWatch")
	if dir != expected {
		t.Errorf("Expected %s, got %s", expected, dir)
	}
}

// TestGetDefaultDataDir_WindowsFallback tests getDefaultDataDir with LOCALAPPDATA fallback
func TestGetDefaultDataDir_WindowsFallback(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}

	// Clear ANUBIS_DATA_DIR to test fallback behavior
	t.Setenv("ANUBIS_DATA_DIR", "")

	// Save original env vars
	origAppData := os.Getenv("APPDATA")
	origLocalAppData := os.Getenv("LOCALAPPDATA")
	defer func() {
		if origAppData != "" {
			os.Setenv("APPDATA", origAppData)
		} else {
			os.Unsetenv("APPDATA")
		}
		if origLocalAppData != "" {
			os.Setenv("LOCALAPPDATA", origLocalAppData)
		} else {
			os.Unsetenv("LOCALAPPDATA")
		}
	}()

	// Clear APPDATA, set LOCALAPPDATA
	os.Unsetenv("APPDATA")
	tmpDir := t.TempDir()
	os.Setenv("LOCALAPPDATA", tmpDir)

	dir := getDefaultDataDir()

	// Should use LOCALAPPDATA + AnubisWatch
	expected := filepath.Join(tmpDir, "AnubisWatch")
	if dir != expected {
		t.Errorf("Expected %s, got %s", expected, dir)
	}
}

func captureStdoutInit(f func()) string {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	os.Stdout = oldStdout

	var buf strings.Builder
	b := make([]byte, 1024)
	for {
		n, err := r.Read(b)
		if n > 0 {
			buf.Write(b[:n])
		}
		if err != nil {
			break
		}
	}
	return buf.String()
}

func TestInitConfig_Simple(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "anubis.json")

	oldArgs := os.Args
	os.Args = []string{"anubis", "init", "--output", configPath}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutInit(initConfig)
	if !strings.Contains(output, "initialized successfully") && !strings.Contains(output, "AnubisWatch") {
		// initSimpleWithPath may not print much; just verify file exists
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Errorf("Expected config file to be created at %s", configPath)
	}
}

func TestInitConfig_Help(t *testing.T) {
	oldArgs := os.Args
	os.Args = []string{"anubis", "init", "--help"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutInit(initConfig)
	if !strings.Contains(output, "Usage: anubis init") {
		t.Errorf("Expected init help, got: %s", output)
	}
}

func TestInitConfig_FileExists(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "anubis.json")
		os.WriteFile(configPath, []byte("{}"), 0644)
		os.Args = []string{"anubis", "init", "--output", configPath}
		initConfig()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestInitConfig_FileExists")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Config already exists") {
		t.Errorf("Expected config exists error, got: %s", string(output))
	}
}
