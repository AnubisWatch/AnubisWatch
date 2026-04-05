package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestNewRetentionManager(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	config := core.RetentionConfig{
		Raw:     core.Duration{Duration: 24 * time.Hour},
		Minute:  core.Duration{Duration: 7 * 24 * time.Hour},
		FiveMin: core.Duration{Duration: 30 * 24 * time.Hour},
		Hour:    core.Duration{Duration: 90 * 24 * time.Hour},
		Day:     "365d",
	}

	logger := newTestLogger()
	rm := NewRetentionManager(db, config, t.TempDir(), logger)

	if rm == nil {
		t.Fatal("Expected retention manager to be created")
	}
	if rm.db != db {
		t.Error("Expected database to be set")
	}
}

func TestRetentionManager_Start(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	config := core.RetentionConfig{
		Raw:    core.Duration{Duration: 24 * time.Hour},
		Day:    "unlimited",
	}

	logger := newTestLogger()
	rm := NewRetentionManager(db, config, t.TempDir(), logger)

	// Start should not panic
	rm.Start()

	// Give it time to run
	time.Sleep(100 * time.Millisecond)
}

func TestRetentionManager_runCleanup(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	config := core.RetentionConfig{
		Raw:     core.Duration{Duration: 1 * time.Hour},
		Minute:  core.Duration{Duration: 24 * time.Hour},
		FiveMin: core.Duration{Duration: 24 * time.Hour},
		Hour:    core.Duration{Duration: 24 * time.Hour},
		Day:     "7d",
	}

	logger := newTestLogger()
	rm := NewRetentionManager(db, config, t.TempDir(), logger)

	// Run cleanup - should not panic
	rm.runCleanup()
}

func TestRetentionManager_runCleanup_UnlimitedDay(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	config := core.RetentionConfig{
		Raw:  core.Duration{Duration: 1 * time.Hour},
		Day:  "unlimited",
	}

	logger := newTestLogger()
	rm := NewRetentionManager(db, config, t.TempDir(), logger)

	rm.runCleanup()
	// Should skip day cleanup when unlimited
}

func TestRetentionManager_runCleanup_ZeroDuration(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	config := core.RetentionConfig{
		Raw:  core.Duration{Duration: 0},
		Day:  "7d",
	}

	logger := newTestLogger()
	rm := NewRetentionManager(db, config, t.TempDir(), logger)

	rm.runCleanup()
	// Should skip raw cleanup when duration is 0
}

func TestRetentionManager_purgeRawData(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	config := core.RetentionConfig{}
	logger := newTestLogger()
	rm := NewRetentionManager(db, config, t.TempDir(), logger)

	// Purge with cutoff in future - should delete nothing
	cutoff := time.Now().Add(1 * time.Hour)
	err := rm.purgeRawData(cutoff)
	if err != nil {
		t.Fatalf("purgeRawData failed: %v", err)
	}
}

func TestRetentionManager_purgeRawData_WithOldData(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	config := core.RetentionConfig{}
	logger := newTestLogger()
	rm := NewRetentionManager(db, config, t.TempDir(), logger)

	ctx := context.Background()
	soul := &core.Soul{
		ID:          "purge-soul",
		WorkspaceID: "default",
		Name:        "Purge Soul",
		Type:        core.CheckHTTP,
	}
	if err := db.SaveSoul(ctx, soul); err != nil {
		t.Fatalf("SaveSoul failed: %v", err)
	}

	now := time.Now().UTC()
	// Create old judgments
	oldTs := now.Add(-48 * time.Hour).UnixNano()
	newTs := now.Add(-1 * time.Hour).UnixNano()

	oldKey := fmt.Sprintf("default/judgments/purge-soul/%d", oldTs)
	newKey := fmt.Sprintf("default/judgments/purge-soul/%d", newTs)

	db.Put(oldKey, []byte(`{"id":"old","soul_id":"purge-soul"}`))
	db.Put(newKey, []byte(`{"id":"new","soul_id":"purge-soul"}`))

	// Purge data older than 24 hours
	cutoff := now.Add(-24 * time.Hour)
	err := rm.purgeRawData(cutoff)
	if err != nil {
		t.Fatalf("purgeRawData failed: %v", err)
	}

	// Verify old data is deleted
	oldVal, _ := db.Get(oldKey)
	if oldVal != nil {
		t.Error("Expected old data to be deleted")
	}

	// Verify new data still exists
	newVal, err := db.Get(newKey)
	if err != nil || newVal == nil {
		t.Error("Expected new data to exist")
	}
}

func TestRetentionManager_purgeSummaries(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	config := core.RetentionConfig{}
	logger := newTestLogger()
	rm := NewRetentionManager(db, config, t.TempDir(), logger)

	// Purge with cutoff in future - should delete nothing
	cutoff := time.Now().Add(1 * time.Hour)
	err := rm.purgeSummaries("1min", cutoff)
	if err != nil {
		t.Fatalf("purgeSummaries failed: %v", err)
	}
}

func TestRetentionManager_purgeSummaries_WithOldData(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	config := core.RetentionConfig{}
	logger := newTestLogger()
	rm := NewRetentionManager(db, config, t.TempDir(), logger)

	now := time.Now().UTC()
	oldTs := now.Add(-48 * time.Hour).Unix()
	newTs := now.Add(-1 * time.Hour).Unix()

	oldKey := fmt.Sprintf("default/ts/purge-soul/1min/%d", oldTs)
	newKey := fmt.Sprintf("default/ts/purge-soul/1min/%d", newTs)

	db.Put(oldKey, []byte(`{"count":10}`))
	db.Put(newKey, []byte(`{"count":20}`))

	// Purge summaries older than 24 hours
	cutoff := now.Add(-24 * time.Hour)
	err := rm.purgeSummaries("1min", cutoff)
	if err != nil {
		t.Fatalf("purgeSummaries failed: %v", err)
	}

	// Verify old summary is deleted
	oldVal, _ := db.Get(oldKey)
	if oldVal != nil {
		t.Error("Expected old summary to be deleted")
	}

	// Verify new summary still exists
	newVal, err := db.Get(newKey)
	if err != nil || newVal == nil {
		t.Error("Expected new summary to exist")
	}
}

func TestRetentionManager_GetStorageStats(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	// Add some data
	db.Put("/souls/workspace1/soul-1", []byte("soul data"))
	db.Put("/judgments/workspace1/soul-1/123456", []byte("judgment data"))
	db.Put("/ts/workspace1/soul-1/1min/123456", []byte("timeseries data"))
	db.Put("/verdicts/workspace1/verdict-1", []byte("verdict data"))
	db.Put("/journeys/workspace1/journey-1", []byte("journey data"))
	db.Put("/channels/workspace1/channel-1", []byte("channel data"))
	db.Put("system/config", []byte("system data"))
	db.Put("raft/log/1", []byte("raft data"))
	db.Put("other-key", []byte("other data"))

	config := core.RetentionConfig{}
	logger := newTestLogger()
	rm := NewRetentionManager(db, config, t.TempDir(), logger)

	ctx := context.Background()
	stats, err := rm.GetStorageStats(ctx)
	if err != nil {
		t.Fatalf("GetStorageStats failed: %v", err)
	}

	if stats.TotalKeys < 1 {
		t.Error("Expected some keys")
	}
	if stats.TotalSize < 1 {
		t.Error("Expected some data size")
	}
	if stats.KeyCounts["souls"] < 1 {
		t.Error("Expected souls to be counted")
	}
}

func TestStorageStats_Structure(t *testing.T) {
	stats := &StorageStats{
		TotalKeys: 100,
		TotalSize: 1024,
		KeyCounts: map[string]int{
			"souls":      10,
			"judgments":  50,
			"timeseries": 40,
		},
		TypeSizes: map[string]int64{
			"souls":      100,
			"judgments":  500,
			"timeseries": 424,
		},
	}

	if stats.TotalKeys != 100 {
		t.Errorf("Expected TotalKeys 100, got %d", stats.TotalKeys)
	}
	if stats.KeyCounts["souls"] != 10 {
		t.Errorf("Expected 10 souls, got %d", stats.KeyCounts["souls"])
	}
}

func TestCategorizeKey(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		{"/workspace/souls/soul-1", "souls"},
		{"/workspace/judgments/soul-1/123", "judgments"},
		{"/workspace/ts/soul-1/1min/123", "timeseries"},
		{"/workspace/verdicts/verdict-1", "verdicts"},
		{"/workspace/journeys/journey-1", "journeys"},
		{"/workspace/channels/channel-1", "channels"},
		{"system/config", "system"},
		{"raft/log/1", "raft"},
		{"random-key", "other"},
	}

	for _, tt := range tests {
		result := categorizeKey(tt.key)
		if result != tt.expected {
			t.Errorf("categorizeKey(%q) = %q, expected %q", tt.key, result, tt.expected)
		}
	}
}

func TestRetentionManager_retentionLoop(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	config := core.RetentionConfig{
		Raw:  core.Duration{Duration: 1 * time.Hour},
		Day:  "7d",
	}

	logger := newTestLogger()
	rm := NewRetentionManager(db, config, t.TempDir(), logger)

	// Start the retention loop
	go rm.retentionLoop()

	// Let it run briefly
	time.Sleep(150 * time.Millisecond)

	// Should not panic - test passes if no crash
}

func TestNewRetentionManager_InvalidDayConfig(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	config := core.RetentionConfig{
		Day: "invalid-duration",
	}

	logger := newTestLogger()
	rm := NewRetentionManager(db, config, t.TempDir(), logger)

	// runCleanup should handle invalid duration gracefully
	rm.runCleanup()
	// Should not panic
}
