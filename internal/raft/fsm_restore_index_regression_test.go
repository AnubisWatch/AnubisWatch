package raft_test

// Regression coverage for secondary-index refresh after a Raft snapshot install.
//
// StorageFSM.Restore replaces the application keyspace with raw store calls
// (Set/Delete), which bypass CobaltDB's in-memory secondary indexes. Without a
// refresh afterwards, db.judgmentIndex still describes the pre-restore keyspace,
// so GetJudgmentNoCtx reports NotFound for judgments that are durably present,
// and the workspace index stays stale. internal/cluster/manager.go wires the FSM
// over that same CobaltDB, so this is the production path for any node that
// falls behind and installs a leader snapshot.
//
// This file is package raft_test rather than raft because internal/storage
// imports internal/raft (raft_log.go), so an in-package test cannot import it.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/raft"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

func restoreIdxLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func restoreIdxDB(t *testing.T) *storage.CobaltDB {
	t.Helper()
	db, err := storage.NewEngine(core.StorageConfig{Path: t.TempDir()}, restoreIdxLogger(t))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// leaderFSMImage builds the FSM image a leader ships for a workspace the
// receiving node has never seen, encoded exactly as StorageFSM.Snapshot does.
func leaderFSMImage(t *testing.T, wsID, soulID, judgmentID string) []byte {
	t.Helper()
	ts := time.Now().UTC().Add(-2 * time.Hour)
	primary := fmt.Sprintf("%s/judgments/%s/%d", wsID, soulID, ts.UnixNano())

	judgmentBytes, err := json.Marshal(core.Judgment{
		ID: judgmentID, SoulID: soulID, WorkspaceID: wsID,
		Timestamp: ts, Status: core.SoulAlive, Message: "from leader",
	})
	if err != nil {
		t.Fatalf("marshal judgment: %v", err)
	}
	soulBytes, err := json.Marshal(core.Soul{
		ID: soulID, WorkspaceID: wsID, Name: soulID, Type: core.CheckHTTP,
		Target: "http://leader.example", Enabled: true,
	})
	if err != nil {
		t.Fatalf("marshal soul: %v", err)
	}
	wsBytes, err := json.Marshal(core.Workspace{ID: wsID, Name: wsID, Slug: wsID})
	if err != nil {
		t.Fatalf("marshal workspace: %v", err)
	}

	image := map[string][]byte{
		"workspaces/" + wsID:                 wsBytes,
		wsID + "/souls/" + soulID:            soulBytes,
		primary:                              judgmentBytes,
		wsID + "/judgment-idx/" + judgmentID: []byte(primary),
	}
	data, err := json.Marshal(image)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	return data
}

func seedLocalJudgment(t *testing.T, db *storage.CobaltDB, wsID, soulID, judgmentID string) {
	t.Helper()
	ctx := context.Background()
	if err := db.SaveWorkspace(ctx, &core.Workspace{ID: wsID, Name: wsID, Slug: wsID}); err != nil {
		t.Fatalf("SaveWorkspace: %v", err)
	}
	if err := db.SaveSoul(ctx, &core.Soul{
		ID: soulID, WorkspaceID: wsID, Name: soulID, Type: core.CheckHTTP,
		Target: "http://example.com", Enabled: true,
	}); err != nil {
		t.Fatalf("SaveSoul: %v", err)
	}
	if err := db.SaveJudgment(ctx, &core.Judgment{
		ID: judgmentID, SoulID: soulID, WorkspaceID: wsID,
		Timestamp: time.Now().UTC().Add(-time.Hour),
		Status:    core.SoulAlive, Message: "ok",
	}); err != nil {
		t.Fatalf("SaveJudgment: %v", err)
	}
}

// TestRestoreRefreshesJudgmentIndex is the regression's core: a judgment
// installed from a snapshot must be reachable by ID immediately afterwards.
func TestRestoreRefreshesJudgmentIndex(t *testing.T) {
	db := restoreIdxDB(t)
	fsm := raft.NewStorageFSM(db)

	if err := fsm.Restore(leaderFSMImage(t, "leader-ws", "leader-soul", "judgment-leader")); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	judgment, err := db.GetJudgmentNoCtx("judgment-leader")
	if err != nil {
		t.Fatalf("judgment restored from the snapshot is unreachable by ID (%v); "+
			"Restore must refresh the store's in-memory secondary indexes", err)
	}
	if judgment.WorkspaceID != "leader-ws" {
		t.Fatalf("restored judgment workspace = %q, want \"leader-ws\"", judgment.WorkspaceID)
	}
}

// TestRestoreRefreshesWorkspaceIndex pins the second half of the contract: the
// workspace index must list the workspace that arrived with the snapshot, not
// the one the node had before.
func TestRestoreRefreshesWorkspaceIndex(t *testing.T) {
	db := restoreIdxDB(t)
	seedLocalJudgment(t, db, "local-ws", "local-soul", "judgment-local")
	fsm := raft.NewStorageFSM(db)

	if err := fsm.Restore(leaderFSMImage(t, "leader-ws", "leader-soul", "judgment-leader")); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	workspaces := db.ListWorkspaceIDs()
	var found bool
	for _, ws := range workspaces {
		if ws == "leader-ws" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ListWorkspaceIDs() = %v, want it to include \"leader-ws\" restored from the snapshot",
			workspaces)
	}
}

// TestRestoreKeepsJudgmentDataDurable asserts the records really are on disk, so
// a failure above is an index problem rather than a missing-key problem.
func TestRestoreKeepsJudgmentDataDurable(t *testing.T) {
	db := restoreIdxDB(t)
	fsm := raft.NewStorageFSM(db)

	if err := fsm.Restore(leaderFSMImage(t, "leader-ws", "leader-soul", "judgment-leader")); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	primary, err := db.Get("leader-ws/judgment-idx/judgment-leader")
	if err != nil {
		t.Fatalf("durable judgment index key missing after Restore: %v", err)
	}
	raw, err := db.Get(string(primary))
	if err != nil {
		t.Fatalf("restored judgment record missing at %q: %v", primary, err)
	}
	var got core.Judgment
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal restored judgment: %v", err)
	}
	if got.ID != "judgment-leader" {
		t.Fatalf("restored judgment ID = %q, want judgment-leader", got.ID)
	}
}

// TestRestoreKeepsRaftMetadata is the control: Raft's own consensus keys must
// survive the snapshot install, because the restore is authorized by them.
func TestRestoreKeepsRaftMetadata(t *testing.T) {
	db := restoreIdxDB(t)
	fsm := raft.NewStorageFSM(db)

	if err := db.Set("raft/term", []byte("3")); err != nil {
		t.Fatalf("seed raft/term: %v", err)
	}
	if err := fsm.Restore(leaderFSMImage(t, "leader-ws", "leader-soul", "judgment-leader")); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	term, err := db.Get("raft/term")
	if err != nil {
		t.Fatalf("raft/term did not survive Restore: %v", err)
	}
	if string(term) != "3" {
		t.Fatalf("raft/term = %q, want the node's own \"3\"", term)
	}
}

// TestJudgmentLookupWorksBeforeRestore is a second control: O(1) lookup works on
// a freshly seeded database, so any failure is caused by Restore.
func TestJudgmentLookupWorksBeforeRestore(t *testing.T) {
	db := restoreIdxDB(t)
	seedLocalJudgment(t, db, "local-ws", "local-soul", "judgment-local")

	if _, err := db.GetJudgmentNoCtx("judgment-local"); err != nil {
		t.Fatalf("judgment lookup failed before Restore: %v", err)
	}
}

// TestRestoreOnStoreWithoutIndexRebuildStillWorks guards the optional-interface
// seam: in-memory test stores do not implement RebuildIndexes and must keep
// restoring exactly as before.
func TestRestoreOnStoreWithoutIndexRebuildStillWorks(t *testing.T) {
	store := raft.NewInMemoryStorage()
	fsm := raft.NewStorageFSM(store)

	image := leaderFSMImage(t, "leader-ws", "leader-soul", "judgment-leader")
	if err := fsm.Restore(image); err != nil {
		t.Fatalf("Restore on a store without RebuildIndexes: %v", err)
	}
	primary, err := store.Get("leader-ws/judgment-idx/judgment-leader")
	if err != nil {
		t.Fatalf("judgment index key missing after Restore: %v", err)
	}
	if string(primary) == "" {
		t.Fatal("judgment index key is empty after Restore")
	}
}
