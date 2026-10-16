package backup

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// A fresh install stores no workspace records — the "default" workspace is
// implicit — so backups used to contain zero souls.
func TestCreateIncludesImplicitDefaultWorkspace(t *testing.T) {
	storage := &mockStorage{
		souls: []*core.Soul{
			{ID: "s1", Name: "one", WorkspaceID: "default"},
			{ID: "s2", Name: "two", WorkspaceID: "default"},
		},
		alertRules: []*core.AlertRule{{ID: "r1", Name: "rule", WorkspaceID: "default"}},
		journeys:   []*core.JourneyConfig{{ID: "j1", Name: "journey", WorkspaceID: "default"}},
	}
	m := NewManager(storage, t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	b, _, err := m.Create(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if b.Metadata.Souls != 2 || len(b.Data.Souls) != 2 {
		t.Fatalf("souls in backup = %d, want 2", len(b.Data.Souls))
	}
	if b.Metadata.AlertRules != 1 || b.Metadata.Journeys != 1 {
		t.Fatalf("rules=%d journeys=%d, want 1 each", b.Metadata.AlertRules, b.Metadata.Journeys)
	}
}

func TestBackupWorkspaceIDsDeduplicatesDefault(t *testing.T) {
	ids := backupWorkspaceIDs([]*core.Workspace{{ID: "default"}, {ID: "ws1"}, nil, {ID: "ws1"}})
	if len(ids) != 2 || ids[0] != "default" || ids[1] != "ws1" {
		t.Fatalf("ids = %v", ids)
	}
}
