package main

import (
	"context"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

// Deleting a soul must also remove its heartbeat state and its secondary
// probers' votes, in both standalone and clustered mode, without touching a
// different soul whose ID shares a prefix.
func TestDeleteSoulRemovesHeartbeatStateAndVotes(t *testing.T) {
	for _, mode := range []string{"standalone", "clustered"} {
		t.Run(mode, func(t *testing.T) {
			var adapter *restStorageAdapter
			var db *storage.CobaltDB
			if mode == "clustered" {
				adapter, db = clusteredAlertAdapter(t)
			} else {
				db = newAlertKeyDB(t)
				adapter = &restStorageAdapter{store: db}
			}
			ctx := context.Background()
			for _, id := range []string{"soul-1", "soul-10"} {
				if err := db.SaveSoul(ctx, &core.Soul{ID: id, Name: id, Type: core.CheckHTTP, WorkspaceID: "default"}); err != nil {
					t.Fatalf("SaveSoul %s: %v", id, err)
				}
				for _, key := range []string{
					core.HeartbeatStateKey("default", id),
					voteKey("default", id, "jackal-a"),
					voteKey("default", id, "jackal-b"),
				} {
					if err := db.Put(key, []byte(`{}`)); err != nil {
						t.Fatalf("Put %s: %v", key, err)
					}
				}
			}

			if err := adapter.DeleteSoul(ctx, "soul-1"); err != nil {
				t.Fatalf("DeleteSoul: %v", err)
			}

			for _, key := range []string{
				core.HeartbeatStateKey("default", "soul-1"),
				voteKey("default", "soul-1", "jackal-a"),
				voteKey("default", "soul-1", "jackal-b"),
			} {
				if _, err := db.Get(key); err == nil {
					t.Errorf("%s survived the soul delete", key)
				}
			}
			for _, key := range []string{
				core.HeartbeatStateKey("default", "soul-10"),
				voteKey("default", "soul-10", "jackal-a"),
			} {
				if _, err := db.Get(key); err != nil {
					t.Errorf("%s of an unrelated soul was removed: %v", key, err)
				}
			}
		})
	}
}
