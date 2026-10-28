package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// newServerLogger builds the server logger from the logging section of the
// config. ANUBIS_LOG_LEVEL overrides logging.level. The returned closer
// releases the log file, if any.
//
//	logging.output: stdout (default) | stderr | file | both (stdout + file)
//	logging.format: json (default) | text
//	logging.file:   path for file output (default <storage.path>/logs/anubis.log)
//	logging.max_size_mb / logging.max_backups: file rotation (default 100 MB, 5 files)
func newServerLogger(cfg *core.Config) (*slog.Logger, io.Closer, error) {
	level := getLogLevel()
	if os.Getenv("ANUBIS_LOG_LEVEL") == "" && cfg != nil {
		level = parseLogLevel(cfg.Logging.Level)
	}

	output := "stdout"
	format := "json"
	if cfg != nil {
		if cfg.Logging.Output != "" {
			output = strings.ToLower(cfg.Logging.Output)
		}
		if cfg.Logging.Format != "" {
			format = strings.ToLower(cfg.Logging.Format)
		}
	}

	var w io.Writer
	var closer io.Closer = io.NopCloser(nil)
	switch output {
	case "stdout":
		w = os.Stdout
	case "stderr":
		w = os.Stderr
	case "file", "both":
		path := serverLogFilePath(cfg)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, nil, fmt.Errorf("logging.file: %w", err)
		}
		f, err := openRotatingFile(path, cfg.Logging.MaxSizeMB, cfg.Logging.MaxBackups)
		if err != nil {
			return nil, nil, fmt.Errorf("logging.file: %w", err)
		}
		closer = f
		w = f
		if output == "both" {
			w = io.MultiWriter(os.Stdout, f)
		}
	default:
		return nil, nil, fmt.Errorf("logging.output: unknown value %q (want stdout, stderr, file or both)", output)
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	switch format {
	case "json":
		handler = slog.NewJSONHandler(w, opts)
	case "text":
		handler = slog.NewTextHandler(w, opts)
	default:
		return nil, nil, fmt.Errorf("logging.format: unknown value %q (want json or text)", format)
	}
	return slog.New(handler), closer, nil
}

// serverLogFilePath is where the server writes its log file when
// logging.output is "file" or "both", and where `anubis logs` reads it.
func serverLogFilePath(cfg *core.Config) string {
	if cfg != nil && cfg.Logging.File != "" {
		return cfg.Logging.File
	}
	dataDir := ""
	if cfg != nil {
		dataDir = cfg.Storage.Path
	}
	if dataDir == "" {
		dataDir = getDataDir()
	}
	return filepath.Join(dataDir, "logs", "anubis.log")
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
