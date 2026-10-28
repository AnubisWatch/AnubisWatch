package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

// Client commands used to open the data directory next to a running server,
// which orphaned the server's WAL and lost its later writes.
func TestOpenLocalStorageRefusesWhileServerHoldsDataDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", dir)
	t.Setenv("ANUBIS_CONFIG", "")

	server, err := storage.NewEngine(core.StorageConfig{Path: dir}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer server.Close()

	store, err := openLocalStorage()
	if !errors.Is(err, storage.ErrDataDirLocked) {
		if store != nil {
			_ = store.Close()
		}
		t.Fatalf("openLocalStorage err = %v, want ErrDataDirLocked", err)
	}
}

// Client commands used to open an encrypted store without its key: backups
// came out empty and restore wrote plaintext the server could not decrypt.
func TestOpenLocalStorageUsesEncryptionKey(t *testing.T) {
	dir := t.TempDir()
	const key = "local-storage-regression-key-32-bytes!"
	t.Setenv("ANUBIS_DATA_DIR", dir)
	t.Setenv("ANUBIS_CONFIG", "")
	t.Setenv("ANUBIS_ENCRYPTION_KEY", key)

	server, err := storage.NewEngine(core.StorageConfig{
		Path:       dir,
		Encryption: core.EncryptionConfig{Enabled: true, Key: key},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	ctx := core.ContextWithWorkspaceID(context.Background(), "default")
	soul := &core.Soul{ID: "enc-soul", WorkspaceID: "default", Name: "Encrypted", Type: core.CheckHTTP, Target: "https://example.com", Enabled: true}
	if err := server.SaveSoul(ctx, soul); err != nil {
		t.Fatalf("SaveSoul: %v", err)
	}
	if err := server.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("openLocalStorage: %v", err)
	}
	defer store.Close()
	souls, err := store.ListSouls(ctx, "default", 0, 0)
	if err != nil {
		t.Fatalf("ListSouls: %v", err)
	}
	if len(souls) != 1 || souls[0].ID != soul.ID {
		t.Fatalf("ListSouls = %v, want the encrypted soul", souls)
	}
}
