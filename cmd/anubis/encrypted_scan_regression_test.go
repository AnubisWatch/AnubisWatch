package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

func newEncryptedAdapterTestStore(t *testing.T) *storage.CobaltDB {
	t.Helper()

	db, err := storage.NewEngine(core.StorageConfig{
		Path: t.TempDir(),
		Encryption: core.EncryptionConfig{
			Enabled: true,
			Key:     "encrypted-adapter-regression-key-32-bytes",
		},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return db
}

func TestSystemSoulsReadsEncryptedPrefixScan(t *testing.T) {
	db := newEncryptedAdapterTestStore(t)
	ctx := core.ContextWithWorkspaceID(context.Background(), "default")
	soul := &core.Soul{
		ID:          "encrypted-system-soul",
		WorkspaceID: "default",
		Name:        "Encrypted System Soul",
		Type:        core.CheckHTTP,
		Target:      "https://example.com",
		Enabled:     true,
	}
	if err := db.SaveSoul(ctx, soul); err != nil {
		t.Fatalf("SaveSoul: %v", err)
	}

	souls, err := systemSouls(ctx, db)
	if err != nil {
		t.Fatalf("systemSouls: %v", err)
	}
	if len(souls) != 1 || souls[0].ID != soul.ID {
		t.Fatalf("systemSouls = %#v, want soul %q", souls, soul.ID)
	}
}

func TestStorageGetLatestJudgmentReadsEncryptedPrefixScan(t *testing.T) {
	db := newEncryptedAdapterTestStore(t)
	ctx := core.ContextWithWorkspaceID(context.Background(), "default")
	now := time.Now().UTC().Truncate(time.Second)

	older := &core.Judgment{
		ID:          "encrypted-older-judgment",
		SoulID:      "encrypted-latest-soul",
		WorkspaceID: "default",
		Status:      core.SoulDead,
		Timestamp:   now.Add(-time.Minute),
	}
	latest := &core.Judgment{
		ID:          "encrypted-latest-judgment",
		SoulID:      older.SoulID,
		WorkspaceID: "default",
		Status:      core.SoulAlive,
		Timestamp:   now,
	}
	for _, judgment := range []*core.Judgment{older, latest} {
		if err := db.SaveJudgment(ctx, judgment); err != nil {
			t.Fatalf("SaveJudgment %s: %v", judgment.ID, err)
		}
	}

	got, err := storageGetLatestJudgment(db, ctx, "default", latest.SoulID)
	if err != nil {
		t.Fatalf("storageGetLatestJudgment: %v", err)
	}
	if got.ID != latest.ID {
		t.Fatalf("storageGetLatestJudgment ID = %q, want %q", got.ID, latest.ID)
	}
}
