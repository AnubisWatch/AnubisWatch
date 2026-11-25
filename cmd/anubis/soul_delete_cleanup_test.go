package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

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

// probeVoteSource must return only the asked soul's votes (soul-1 must not
// match soul-10) and skip a record it cannot decode instead of failing.
func TestProbeVoteSourceListsOnlyThatSoulsVotes(t *testing.T) {
	db := newAlertKeyDB(t)
	put := func(key string, v core.ProbeVote) {
		t.Helper()
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Put(key, data); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Now().UTC()
	put(core.ProbeVoteKey("default", "soul-1", "a"), core.ProbeVote{JackalID: "a", Status: core.SoulDead, Timestamp: at, Latency: 90 * time.Millisecond, Region: "tr"})
	put(core.ProbeVoteKey("default", "soul-1", "b"), core.ProbeVote{JackalID: "b", Status: core.SoulAlive, Timestamp: at})
	put(core.ProbeVoteKey("default", "soul-10", "z"), core.ProbeVote{JackalID: "z", Status: core.SoulDead, Timestamp: at})
	put(core.ProbeVoteKey("other", "soul-1", "o"), core.ProbeVote{JackalID: "o", Status: core.SoulDead, Timestamp: at})
	if err := db.Put(core.ProbeVoteKey("default", "soul-1", "bad"), []byte("not json")); err != nil {
		t.Fatal(err)
	}

	got, err := probeVoteSource{store: db}.ListProbeVotes(context.Background(), "default", "soul-1")
	if err != nil {
		t.Fatalf("ListProbeVotes: %v", err)
	}
	ids := map[string]core.ProbeVote{}
	for _, v := range got {
		ids[v.JackalID] = v
	}
	if len(got) != 2 || ids["a"].Latency != 90*time.Millisecond || ids["a"].Region != "tr" || ids["b"].Status != core.SoulAlive {
		t.Fatalf("votes = %+v", got)
	}
}
