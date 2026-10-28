package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/auth"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

func TestClusterAuthFSMCASPersistence(t *testing.T) {
	path := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.NewEngine(core.StorageConfig{Path: path}, logger)
	if err != nil {
		t.Fatal(err)
	}
	shared := &clusterAuthStore{store: db}
	if got, err := shared.Load(); err != nil || got != nil {
		t.Fatal("new store not empty", err)
	}
	apply := func(expected uint64, id string) {
		t.Helper()
		raw, _ := json.Marshal(auth.ClusterAuthChange{Expected: expected, WriteID: id, Data: json.RawMessage(`{"users":{},"tokens":{}}`)})
		if err := shared.apply(&core.FSMCommand{Op: core.FSMSet, Table: clusterAuthTable, Key: clusterAuthStateKey, Value: raw}); err != nil {
			t.Fatal(err)
		}
	}
	apply(0, "first")
	apply(0, "conflict")
	apply(0, "first")
	state, err := shared.Load()
	if err != nil || state.Revision != 1 || len(state.Receipts) != 1 {
		t.Fatalf("replay/conflict mutated state: %+v %v", state, err)
	}
	apply(1, "second")
	state, _ = shared.Load()
	if ok, err := auth.ClusterAuthCommitOutcome(state, 0, "first"); !ok || err != nil {
		t.Fatal("later writer hid committed receipt")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = storage.NewEngine(core.StorageConfig{Path: path}, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	shared.store = db
	state, err = shared.Load()
	if err != nil || state.Revision != 2 {
		t.Fatalf("durability: %+v %v", state, err)
	}
	if err := shared.apply(&core.FSMCommand{Op: core.FSMDelete, Key: clusterAuthStateKey}); err == nil {
		t.Fatal("unsupported delete accepted")
	}
	if err := shared.apply(&core.FSMCommand{Op: core.FSMSet, Key: "another-key"}); err == nil {
		t.Fatal("unexpected key accepted")
	}
	if err := shared.apply(&core.FSMCommand{Op: core.FSMSet, Key: clusterAuthStateKey, Value: []byte("{")}); err == nil {
		t.Fatal("malformed command accepted")
	}
}
