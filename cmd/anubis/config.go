package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ConfigPaths holds all possible config locations
type ConfigPaths struct {
	System string // System-wide config
	User   string // User config
	Local  string // Current directory
}

// getConfigPaths returns possible config file locations
func getConfigPaths() ConfigPaths {
	return ConfigPaths{
		System: getSystemConfigPath(),
		User:   getUserConfigPath(),
		Local:  "./anubis.json",
	}
}

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

// findConfig finds the first existing config file in order of priority
// Priority: ANUBIS_CONFIG env > ./anubis.json > User config > System config
func findConfig() string {
	// 1. Environment variable (highest priority)
	if env := os.Getenv("ANUBIS_CONFIG"); env != "" {
		return env
	}

	// 2. Local directory (for project-specific configs)
	if _, err := os.Stat("./anubis.json"); err == nil {
		return "./anubis.json"
	}

	// 3. User config
	userConfig := getUserConfigPath()
	if _, err := os.Stat(userConfig); err == nil {
		return userConfig
	}

	// 4. System config
	systemConfig := getSystemConfigPath()
	if _, err := os.Stat(systemConfig); err == nil {
		return systemConfig
	}

	// Default: local directory (will be created)
	return "./anubis.json"
}

// ensureConfigDir creates config directory if needed
func ensureConfigDir(configPath string) error {
	dir := filepath.Dir(configPath)
	if dir == "" || dir == "." {
		return nil
	}
	return os.MkdirAll(dir, 0755)
}

// listInstances lists all Anubis instances (config files)
func listInstances() []string {
	var instances []string

	// Local
	if _, err := os.Stat("./anubis.json"); err == nil {
		wd, _ := os.Getwd()
		instances = append(instances, fmt.Sprintf("local: %s/anubis.json", wd))
	}

	// User
	userConfig := getUserConfigPath()
	if _, err := os.Stat(userConfig); err == nil {
		instances = append(instances, fmt.Sprintf("user: %s", userConfig))
	}

	// System
	systemConfig := getSystemConfigPath()
	if _, err := os.Stat(systemConfig); err == nil {
		instances = append(instances, fmt.Sprintf("system: %s", systemConfig))
	}

	// Additional instances from ANUBIS_CONFIGS (comma-separated)
	if configs := os.Getenv("ANUBIS_CONFIGS"); configs != "" {
		// Parse multiple configs
		fmt.Sscanf(configs, "%s")
	}

	return instances
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
	case getUserConfigPath():
		return "user-default"
	case getSystemConfigPath():
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
