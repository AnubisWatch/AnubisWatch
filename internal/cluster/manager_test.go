package cluster

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
}

func newTestDB(t *testing.T) *storage.CobaltDB {
	dir := t.TempDir()
	cfg := core.StorageConfig{Path: dir}
	db, err := storage.NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	return db
}

func newTestRaftConfig() core.RaftConfig {
	return core.RaftConfig{
		NodeID:           "test-node-1",
		BindAddr:         "127.0.0.1:0",
		AdvertiseAddr:    "127.0.0.1:7000",
		Bootstrap:        false,
		ElectionTimeout:  core.Duration{Duration: 1 * time.Second},
		HeartbeatTimeout: core.Duration{Duration: 500 * time.Millisecond},
		CommitTimeout:    core.Duration{Duration: 100 * time.Millisecond},
		MaxAppendEntries: 64,
	}
}

func TestManager_NewManager(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := newTestRaftConfig()
	manager, err := NewManager(cfg, db, newTestLogger())

	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	if manager == nil {
		t.Fatal("Expected manager to be created")
	}

	if manager.config.NodeID != cfg.NodeID {
		t.Errorf("Expected node ID %s, got %s", cfg.NodeID, manager.config.NodeID)
	}

	if manager.db != db {
		t.Error("Expected db to be set")
	}

	if manager.fsm == nil {
		t.Error("Expected FSM to be created")
	}

	if manager.logStore == nil {
		t.Error("Expected log store to be created")
	}

	if manager.snapshotStore == nil {
		t.Error("Expected snapshot store to be created")
	}

	if manager.stableStore == nil {
		t.Error("Expected stable store to be created")
	}
}

func TestManager_IsClustered(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	tests := []struct {
		name              string
		bootstrap         bool
		peers             []core.RaftPeer
		expectedClustered bool
	}{
		{
			name:              "bootstrap mode",
			bootstrap:         true,
			peers:             []core.RaftPeer{},
			expectedClustered: true,
		},
		{
			name:              "with peers",
			bootstrap:         false,
			peers:             []core.RaftPeer{{ID: "node-2", Address: "127.0.0.1:7001"}},
			expectedClustered: true,
		},
		{
			name:              "standalone mode",
			bootstrap:         false,
			peers:             []core.RaftPeer{},
			expectedClustered: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newTestRaftConfig()
			cfg.Bootstrap = tt.bootstrap
			cfg.Peers = tt.peers

			manager, err := NewManager(cfg, db, newTestLogger())
			if err != nil {
				t.Fatalf("NewManager failed: %v", err)
			}

			if manager.IsClustered() != tt.expectedClustered {
				t.Errorf("IsClustered() = %v, want %v", manager.IsClustered(), tt.expectedClustered)
			}
		})
	}
}

func TestManager_StartStop_Standalone(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := newTestRaftConfig()
	cfg.Bootstrap = false
	cfg.Peers = []core.RaftPeer{}

	manager, err := NewManager(cfg, db, newTestLogger())
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	ctx := context.Background()

	// Start in standalone mode
	err = manager.Start(ctx)
	if err != nil {
		t.Errorf("Start failed: %v", err)
	}

	// Stop
	err = manager.Stop(ctx)
	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

func TestManager_GetStatus_Standalone(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := newTestRaftConfig()
	cfg.Bootstrap = false

	manager, err := NewManager(cfg, db, newTestLogger())
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	status := manager.GetStatus()

	if status == nil {
		t.Fatal("Expected status to be returned")
	}

	if status.IsClustered != manager.IsClustered() {
		t.Errorf("Expected IsClustered to match manager state")
	}

	if status.NodeID != cfg.NodeID {
		t.Errorf("Expected node ID %s, got %s", cfg.NodeID, status.NodeID)
	}

	// In standalone mode, these should be empty/zero
	if status.State != "" {
		t.Errorf("Expected empty state in standalone, got %s", status.State)
	}

	if status.Leader != "" {
		t.Errorf("Expected empty leader in standalone, got %s", status.Leader)
	}

	if status.Term != 0 {
		t.Errorf("Expected zero term in standalone, got %d", status.Term)
	}

	if status.PeerCount != 0 {
		t.Errorf("Expected zero peer count in standalone, got %d", status.PeerCount)
	}
}

func TestManager_IsLeader_NotRunning(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := newTestRaftConfig()
	manager, err := NewManager(cfg, db, newTestLogger())
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Should return false when node is not running
	if manager.IsLeader() {
		t.Error("Expected IsLeader to return false when node not running")
	}

	if leader := manager.Leader(); leader != "" {
		t.Errorf("Expected empty leader, got %s", leader)
	}
}

func TestManager_ClusterStatus_Structure(t *testing.T) {
	status := &ClusterStatus{
		IsClustered: true,
		NodeID:      "test-node",
		State:       "leader",
		Leader:      "test-node",
		Term:        5,
		PeerCount:   2,
	}

	if !status.IsClustered {
		t.Error("Expected IsClustered to be true")
	}

	if status.NodeID != "test-node" {
		t.Errorf("Expected node ID test-node, got %s", status.NodeID)
	}

	if status.State != "leader" {
		t.Errorf("Expected state leader, got %s", status.State)
	}

	if status.Leader != "test-node" {
		t.Errorf("Expected leader test-node, got %s", status.Leader)
	}

	if status.Term != 5 {
		t.Errorf("Expected term 5, got %d", status.Term)
	}

	if status.PeerCount != 2 {
		t.Errorf("Expected peer count 2, got %d", status.PeerCount)
	}
}

func TestManager_StartStop_Clustered(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := newTestRaftConfig()
	cfg.Bootstrap = true
	cfg.BindAddr = "127.0.0.1:0"

	manager, err := NewManager(cfg, db, newTestLogger())
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	ctx := context.Background()

	// Start in clustered mode
	err = manager.Start(ctx)
	if err != nil {
		t.Errorf("Start failed: %v", err)
	}

	// Verify clustered state
	if !manager.IsClustered() {
		t.Error("Expected clustered mode")
	}

	// Stop
	err = manager.Stop(ctx)
	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

func TestManager_ConcurrentAccess(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := newTestRaftConfig()
	manager, err := NewManager(cfg, db, newTestLogger())
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Test concurrent access to getters
	done := make(chan bool, 3)

	go func() {
		for i := 0; i < 100; i++ {
			_ = manager.IsClustered()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = manager.GetStatus()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = manager.IsLeader()
			_ = manager.Leader()
		}
		done <- true
	}()

	// Wait for all goroutines to complete
	for i := 0; i < 3; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for concurrent access test")
		}
	}
}

func TestManager_Logger(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := newTestRaftConfig()
	logger := newTestLogger()
	manager, err := NewManager(cfg, db, logger)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	if manager.logger == nil {
		t.Error("Expected logger to be set")
	}
}

// TestManager_IsLeader_WithNilNode tests IsLeader when node is nil
func TestManager_IsLeader_WithNilNode(t *testing.T) {
	manager := &Manager{node: nil}

	if manager.IsLeader() {
		t.Error("Expected IsLeader to return false with nil node")
	}
}

// TestManager_Leader_WithNilNode tests Leader when node is nil
func TestManager_Leader_WithNilNode(t *testing.T) {
	manager := &Manager{node: nil}

	leader := manager.Leader()
	if leader != "" {
		t.Errorf("Expected Leader to return empty string with nil node, got %q", leader)
	}
}

// TestManager_GetStatus_WithRunningNode tests GetStatus when node is running
func TestManager_GetStatus_WithRunningNode(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := newTestRaftConfig()
	cfg.Bootstrap = true
	cfg.BindAddr = "127.0.0.1:0"

	manager, err := NewManager(cfg, db, newTestLogger())
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	ctx := context.Background()

	// Start the node
	err = manager.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer manager.Stop(ctx)

	// Get status with running node
	status := manager.GetStatus()

	if status == nil {
		t.Fatal("Expected status to be returned")
	}

	if !status.IsClustered {
		t.Error("Expected IsClustered to be true")
	}

	if status.NodeID != cfg.NodeID {
		t.Errorf("Expected node ID %s, got %s", cfg.NodeID, status.NodeID)
	}

	// State should be populated (might be "follower", "candidate", or "leader")
	if status.State == "" {
		t.Error("Expected state to be populated")
	}

	// Term should be >= 0 (unsigned, so always true - verify field exists)
	_ = status.Term

	// PeerCount should be >= 0
	if status.PeerCount < 0 {
		t.Errorf("Expected non-negative peer count, got %d", status.PeerCount)
	}
}
