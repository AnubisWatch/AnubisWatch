package main

// Regression coverage for the clustered-write key format of restStorageAdapter.
//
// In a clustered deployment every write is replicated as an FSMCommand
// (cmd/anubis/server.go:585 sets restStore.replicator when the cluster is
// active). The adapter must therefore use exactly the key that CobaltDB's read
// paths use, or the record is written where nothing can find it.
//
// Workspaces previously replicated under "{ws}/workspaces/{ws}" while every read
// path resolves "workspaces/{id}" (internal/storage/engine.go: GetWorkspace,
// ListWorkspaces, SaveWorkspace, DeleteWorkspace). A workspace created through
// POST /api/v1/workspaces returned 201 and was then permanently unreadable:
// GET returned not-found, the list omitted it, and DELETE removed a key that had
// never been written.

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

// workspaceKeyFSMReplicator stands in for clusterAdapter.ApplyMutation and
// applies the committed command exactly the way the production Raft FSM does
// (internal/raft/fsm.go: StorageFSM -> CobaltDB.ApplyFSMCommand), so this test
// observes the same state a real cluster node would.
type workspaceKeyFSMReplicator struct {
	store *storage.CobaltDB
}

func (r *workspaceKeyFSMReplicator) ApplyMutation(cmd core.FSMCommand, _ time.Duration) (uint64, uint64, error) {
	if err := r.store.ApplyFSMCommand(&cmd); err != nil {
		return 0, 0, err
	}
	return 1, 1, nil
}

func newWorkspaceKeyDB(t *testing.T) *storage.CobaltDB {
	t.Helper()
	db, err := storage.NewEngine(
		core.StorageConfig{Path: t.TempDir()},
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestClusteredWorkspaceWriteUsesCanonicalKey is the failing case: a clustered
// write must land on the key the read paths scan.
func TestClusteredWorkspaceWriteUsesCanonicalKey(t *testing.T) {
	db := newWorkspaceKeyDB(t)
	adapter := &restStorageAdapter{store: db, replicator: &workspaceKeyFSMReplicator{store: db}}

	ws := &core.Workspace{
		ID:      "acme",
		Name:    "Acme Corp",
		Slug:    "acme",
		OwnerID: "user-1",
		Status:  core.WorkspaceActive,
	}
	if err := adapter.SaveWorkspaceNoCtx(ws); err != nil {
		t.Fatalf("SaveWorkspaceNoCtx: %v", err)
	}

	got, err := adapter.GetWorkspaceNoCtx("acme")
	if err != nil || got == nil {
		t.Fatalf("clustered workspace 'acme' is unreadable right after a successful write: err=%v", err)
	}
	if got.Name != "Acme Corp" || got.OwnerID != "user-1" {
		t.Fatalf("clustered workspace round-trip lost fields: %+v", got)
	}

	all, err := adapter.ListWorkspacesNoCtx()
	if err != nil {
		t.Fatalf("ListWorkspacesNoCtx: %v", err)
	}
	found := false
	for _, w := range all {
		if w != nil && w.ID == "acme" {
			found = true
		}
	}
	if !found {
		t.Fatalf("clustered workspace 'acme' missing from the workspace list (%d listed)", len(all))
	}
}

// TestClusteredWorkspaceDeleteRemovesTheStoredRecord covers the secondary
// branch the fix touches: the delete must use the same canonical key, otherwise
// the record is orphaned forever.
func TestClusteredWorkspaceDeleteRemovesTheStoredRecord(t *testing.T) {
	db := newWorkspaceKeyDB(t)
	adapter := &restStorageAdapter{store: db, replicator: &workspaceKeyFSMReplicator{store: db}}

	ws := &core.Workspace{ID: "acme", Name: "Acme Corp", Slug: "acme", Status: core.WorkspaceActive}
	if err := adapter.SaveWorkspaceNoCtx(ws); err != nil {
		t.Fatalf("SaveWorkspaceNoCtx: %v", err)
	}
	if err := adapter.DeleteWorkspaceNoCtx("acme"); err != nil {
		t.Fatalf("DeleteWorkspaceNoCtx: %v", err)
	}

	all, err := adapter.ListWorkspacesNoCtx()
	if err != nil {
		t.Fatalf("ListWorkspacesNoCtx: %v", err)
	}
	for _, w := range all {
		if w != nil && w.ID == "acme" {
			t.Fatalf("workspace 'acme' still present after delete (delete used a different key than save)")
		}
	}
}

// TestStandaloneWorkspaceWriteRoundTrips is the control: with no replicator the
// adapter must still read back what it wrote, so a green clustered result cannot
// come from a broken harness.
func TestStandaloneWorkspaceWriteRoundTrips(t *testing.T) {
	db := newWorkspaceKeyDB(t)
	adapter := &restStorageAdapter{store: db} // replicator == nil: standalone

	ws := &core.Workspace{ID: "acme", Name: "Acme Corp", Slug: "acme", Status: core.WorkspaceActive}
	if err := adapter.SaveWorkspaceNoCtx(ws); err != nil {
		t.Fatalf("SaveWorkspaceNoCtx: %v", err)
	}

	got, err := adapter.GetWorkspaceNoCtx("acme")
	if err != nil || got == nil {
		t.Fatalf("standalone workspace 'acme' unreadable: err=%v", err)
	}
	if got.Name != "Acme Corp" {
		t.Fatalf("standalone workspace name = %q, want %q", got.Name, "Acme Corp")
	}
}
