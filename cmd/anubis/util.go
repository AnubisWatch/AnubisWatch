package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

var startTime = time.Now()

// testDataDir is set by init() in main_test.go to provide a default test directory
var testDataDir string

// openLocalStorage opens storage directly for CLI operations
func openLocalStorage() (*storage.CobaltDB, error) {
	dataDir := getDataDir()

	cfg := core.StorageConfig{
		Path: dataDir,
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	return storage.NewEngine(cfg, logger)
}

func getDataDir() string {
	// Check environment variable first
	if dir := os.Getenv("ANUBIS_DATA_DIR"); dir != "" {
		return dir
	}
	// Check test default (set by main_test.go init)
	if testDataDir != "" {
		return testDataDir
	}
	return defaultDataDirForOS(runtime.GOOS)
}

// defaultDataDirForOS returns the platform-specific data directory.
func defaultDataDirForOS(goos string) string {
	switch goos {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "anubis")
	default:
		return filepath.Join("/var", "lib", "anubis")
	}
}

// Helper functions for API calls
func getAPIURL() string {
	host := os.Getenv("ANUBIS_HOST")
	port := os.Getenv("ANUBIS_PORT")
	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "8443"
	}
	return fmt.Sprintf("http://%s:%s", host, port)
}

func getAPIToken() string {
	return os.Getenv("ANUBIS_API_TOKEN")
}

func httpGet(url, token string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return http.DefaultClient.Do(req)
}

func httpPost(url, contentType string, body []byte, token string) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+token)
	return http.DefaultClient.Do(req)
}

func truncate(s string, maxLen int) string {
	if maxLen <= 3 {
		if len(s) <= maxLen {
			return s
		}
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// parseConfigValue converts a string value to an appropriate type for config
func parseConfigValue(s string) interface{} {
	// Try bool
	switch strings.ToLower(s) {
	case "true":
		return true
	case "false":
		return false
	}
	// Try int
	var i int
	if _, err := fmt.Sscanf(s, "%d", &i); err == nil && fmt.Sprintf("%d", i) == s {
		return i
	}
	// Try float
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err == nil && fmt.Sprintf("%g", f) == s {
		return f
	}
	// Default: string
	return s
}

func getLogLevel() slog.Level {
	level := os.Getenv("ANUBIS_LOG_LEVEL")
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// formatBytes formats byte size to human-readable string
func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// getFileSize returns the size of a file
func getFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// dirSize calculates the total size of a directory
func dirSize(path string) int64 {
	var size int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}

func checkMemory() map[string]interface{} {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return map[string]interface{}{
		"alloc_mb":   m.Alloc / 1024 / 1024,
		"sys_mb":     m.Sys / 1024 / 1024,
		"num_gc":     m.NumGC,
		"goroutines": runtime.NumGoroutine(),
	}
}
