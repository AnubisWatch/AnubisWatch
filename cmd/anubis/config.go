package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// getSystemConfigPath returns system-wide config path
func getSystemConfigPath() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("PROGRAMDATA"), "AnubisWatch", "anubis.json")
	case "darwin":
		return "/Library/Application Support/AnubisWatch/anubis.json"
	default: // Linux
		return "/etc/anubis/anubis.json"
	}
}

func getSystemConfigPaths() []string {
	jsonPath := getSystemConfigPath()
	return []string{jsonPath, strings.TrimSuffix(jsonPath, ".json") + ".yaml"}
}

// getUserConfigPath returns user-specific config path
func getUserConfigPath() string {
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = os.Getenv("LOCALAPPDATA")
		}
		return filepath.Join(appData, "AnubisWatch", "anubis.json")
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "AnubisWatch", "anubis.json")
	default: // Linux
		home, _ := os.UserHomeDir()
		// XDG Config standard
		if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
			return filepath.Join(xdgConfig, "anubis", "anubis.json")
		}
		return filepath.Join(home, ".config", "anubis", "anubis.json")
	}
}

func getUserConfigPaths() []string {
	jsonPath := getUserConfigPath()
	return []string{jsonPath, strings.TrimSuffix(jsonPath, ".json") + ".yaml"}
}

// findConfig finds the first existing config file in order of priority
// Priority: ANUBIS_CONFIG env > local config > user config > system config
func findConfig() string {
	// 1. Environment variable (highest priority)
	if env := os.Getenv("ANUBIS_CONFIG"); env != "" {
		return env
	}

	for _, candidate := range []string{"./anubis.json", "./anubis.yaml"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	for _, candidate := range getUserConfigPaths() {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	for _, candidate := range getSystemConfigPaths() {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	// Default: local directory (will be created)
	return "./anubis.json"
}

func configPathFromArgs(args []string) string {
	for i, arg := range args {
		if (arg == "--config" || arg == "-c") && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, "--config=") {
			return strings.TrimPrefix(arg, "--config=")
		}
	}
	return ""
}

func findConfigFromArgs(args []string) string {
	if configPath := configPathFromArgs(args); configPath != "" {
		return configPath
	}
	return findConfig()
}

// ensureConfigDir creates config directory if needed
func ensureConfigDir(configPath string) error {
	dir := filepath.Dir(configPath)
	if dir == "" || dir == "." {
		return nil
	}
	return os.MkdirAll(dir, 0755)
}

// getInstanceName returns a name for the current instance based on config location
func getInstanceName(configPath string) string {
	if configPath == "" {
		configPath = findConfig()
	}

	// Extract name from config path or use directory name
	dir := filepath.Dir(configPath)
	base := filepath.Base(dir)

	if base == "." {
		return "default"
	}

	// If it's a standard path, use "default" or derive from context
	switch configPath {
	case getUserConfigPaths()[0], getUserConfigPaths()[1]:
		return "user-default"
	case getSystemConfigPaths()[0], getSystemConfigPaths()[1]:
		return "system"
	default:
		// Use directory name as instance name
		if base == "anubis" || base == "AnubisWatch" {
			parent := filepath.Base(filepath.Dir(dir))
			if parent != "." {
				return parent
			}
		}
		return base
	}
}
