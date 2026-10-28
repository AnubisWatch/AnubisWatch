package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// `anubis init` asks for "Data Retention (days)" and writes
// storage.retention_days, which used to be ignored: every install kept 90 days.
func TestStorageRetentionDaysSetsRawRetention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anubis.json")
	if err := os.WriteFile(path, []byte(`{"storage":{"path":"/tmp/x","retention_days":7}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := cfg.Storage.TimeSeries.Retention.Raw.Duration; got != 7*24*time.Hour {
		t.Fatalf("raw retention = %v, want 168h", got)
	}
}

func TestExplicitRawRetentionWinsOverRetentionDays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anubis.json")
	if err := os.WriteFile(path, []byte(`{"storage":{"path":"/tmp/x","retention_days":7,"timeseries":{"retention":{"raw":"240h"}}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := cfg.Storage.TimeSeries.Retention.Raw.Duration; got != 240*time.Hour {
		t.Fatalf("raw retention = %v, want 240h", got)
	}
}
