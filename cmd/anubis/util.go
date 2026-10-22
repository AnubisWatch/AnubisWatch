package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

var startTime = time.Now()

// testDataDir is set by TestMain (testmain_test.go) to a temporary directory
var testDataDir string

// openLocalStorage opens storage directly for CLI operations. It uses the
// server's storage settings (notably encryption) so data written here is
// readable by the server and vice versa. It fails while the server is running,
// because two engines on one directory lose writes.
func openLocalStorage() (*storage.CobaltDB, error) {
	var cfg core.StorageConfig
	if c := loadCLIConfig(); c != nil {
		cfg = c.Storage
	}
	cfg.Path = getDataDir()
	if v := os.Getenv("ANUBIS_ENCRYPTION_KEY"); v != "" {
		cfg.Encryption.Enabled = true
		cfg.Encryption.Key = v
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	store, err := storage.NewEngine(cfg, logger)
	if errors.Is(err, storage.ErrDataDirLocked) {
		return nil, fmt.Errorf("%w; the server is running, so use the API instead (run `anubis login` first) or stop the server", err)
	}
	return store, err
}

func getDataDir() string {
	// Check environment variable first
	if dir := os.Getenv("ANUBIS_DATA_DIR"); dir != "" {
		return dir
	}
	// Check test default (set by TestMain)
	if testDataDir != "" {
		return testDataDir
	}
	// Then the config file the server would use, so `anubis init` followed
	// by client commands agrees on where the data lives.
	if cfg := loadCLIConfig(); cfg != nil && cfg.Storage.Path != "" {
		return cfg.Storage.Path
	}
	return defaultDataDirForOS(runtime.GOOS)
}

// loadCLIConfig loads the config file the server would pick up (see
// findConfigFromArgs), or returns nil if there is none or it does not parse.
// Client commands use it only for connection defaults; env vars still win.
func loadCLIConfig() *core.Config {
	path := findConfigFromArgs(os.Args[1:])
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return nil
	}
	cfg, err := core.LoadConfig(path)
	if err != nil {
		return nil
	}
	return cfg
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
	scheme := "http"
	if host == "" || port == "" {
		if cfg := loadCLIConfig(); cfg != nil {
			if host == "" {
				host = cfg.Server.Host
			}
			if port == "" && cfg.Server.Port > 0 {
				port = fmt.Sprintf("%d", cfg.Server.Port)
			}
			if cfg.Server.TLS.Enabled {
				scheme = "https"
			}
		}
	}
	// A wildcard bind address is not something a client can dial.
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "localhost"
	}
	if port == "" {
		port = "8443"
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		if parsed, err := url.Parse("http://" + host); err == nil {
			host = parsed.Hostname()
		} else {
			host = host[1 : len(host)-1]
		}
	}
	return (&url.URL{Scheme: scheme, Host: net.JoinHostPort(host, port)}).String()
}

// getAPIToken returns ANUBIS_API_TOKEN, or the session token saved by
// `anubis login`.
func getAPIToken() string {
	if token := os.Getenv("ANUBIS_API_TOKEN"); token != "" {
		return token
	}
	data, err := os.ReadFile(tokenFilePath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// tokenFilePath is where `anubis login` stores the session token.
// ANUBIS_TOKEN_FILE overrides it.
func tokenFilePath() string {
	if p := os.Getenv("ANUBIS_TOKEN_FILE"); p != "" {
		return p
	}
	return filepath.Join(filepath.Dir(getUserConfigPath()), "token")
}

func httpGet(url, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return http.DefaultClient.Do(req)
}

func httpPost(url, contentType string, body []byte, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
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
	end := maxLen - 3
	// Keep the byte budget without splitting a UTF-8 character.
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end] + "..."
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
	// Walk errors (permission-denied subtrees, vanished files) yield a
	// partial size, which is acceptable for a stats display.
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
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
