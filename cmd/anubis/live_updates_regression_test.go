package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/cluster"
	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Regression: soul writes from REST, MCP and gRPC never produced a live
// soul_update event. They all write through restStorageAdapter, which now
// reports each successful write.
func TestRestStorageAdapter_ReportsSoulWrites(t *testing.T) {
	db := setupTestStore(t)
	type change struct{ id, action string }
	var got []change
	adapter := &restStorageAdapter{store: db, onSoulChange: func(soul *core.Soul, action string) {
		got = append(got, change{soul.ID, action})
	}}
	ctx := context.Background()
	soul := &core.Soul{ID: "soul-1", Name: "Test", Type: core.CheckHTTP, Target: "https://example.com", WorkspaceID: "default"}

	if err := adapter.SaveSoul(ctx, soul); err != nil {
		t.Fatal(err)
	}
	soul.Name = "Renamed"
	if err := adapter.SaveSoul(ctx, soul); err != nil {
		t.Fatal(err)
	}
	if err := adapter.DeleteSoul(ctx, soul.ID); err != nil {
		t.Fatal(err)
	}
	// A failed delete reports nothing.
	_ = adapter.DeleteSoul(ctx, "missing")

	want := []change{{"soul-1", "created"}, {"soul-1", "updated"}, {"soul-1", "deleted"}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("change %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// Regression: /api/v1/cluster/peers needs a member list from the adapter.
func TestClusterAdapter_ClusterPeersStandalone(t *testing.T) {
	db := setupTestStore(t)
	mgr, err := cluster.NewManager(core.NecropolisConfig{
		NodeName: "jackal-a",
		Raft:     core.RaftConfig{NodeID: "node-a", AdvertiseAddr: "10.0.0.5:7946", Region: "eu"},
	}, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	peers := (&clusterAdapter{mgr: mgr}).ClusterPeers()
	if len(peers) != 1 {
		t.Fatalf("expected one peer, got %+v", peers)
	}
	p := peers[0]
	if p.ID != "node-a" || p.Name != "jackal-a" || p.Address != "10.0.0.5:7946" || p.Region != "eu" || p.State != "leader" || !p.IsLeader || p.LastContact == "" {
		t.Fatalf("unexpected standalone peer: %+v", p)
	}
	if (&clusterAdapter{}).ClusterPeers() != nil {
		t.Fatal("a nil manager should defer to the API's standalone entry")
	}
}
