package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// logging.* used to be parsed and ignored: the server always logged JSON to
// stdout at ANUBIS_LOG_LEVEL, and `anubis logs` never found a file.
func TestNewServerLoggerWritesConfiguredFile(t *testing.T) {
	t.Setenv("ANUBIS_LOG_LEVEL", "")
	dir := t.TempDir()
	cfg := &core.Config{}
	cfg.Storage.Path = dir
	cfg.Logging = core.LoggingConfig{Level: "warn", Format: "text", Output: "file"}

	logger, closer, err := newServerLogger(cfg)
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("hidden-info")
	logger.Warn("visible-warn")
	closer.Close()

	path := filepath.Join(dir, "logs", "anubis.log")
	if got := serverLogFilePath(cfg); got != path {
		t.Fatalf("serverLogFilePath = %s, want %s", got, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if strings.Contains(out, "hidden-info") {
		t.Errorf("info line written at warn level: %s", out)
	}
	if !strings.Contains(out, "msg=visible-warn") {
		t.Errorf("expected text-format warn line, got: %s", out)
	}
}

func TestNewServerLoggerRejectsUnknownValues(t *testing.T) {
	for _, l := range []core.LoggingConfig{{Output: "syslog"}, {Format: "xml"}} {
		cfg := &core.Config{Logging: l}
		if _, _, err := newServerLogger(cfg); err == nil {
			t.Errorf("logging %+v accepted", l)
		}
	}
}

func TestServerLogFilePathPrefersExplicitFile(t *testing.T) {
	cfg := &core.Config{Logging: core.LoggingConfig{File: "/tmp/x/anubis.log"}}
	if got := serverLogFilePath(cfg); got != "/tmp/x/anubis.log" {
		t.Fatalf("got %s", got)
	}
}
