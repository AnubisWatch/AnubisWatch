package storage

import (
	"fmt"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// ListAlertEvents(soulID, limit) backs the gRPC ListVerdicts/StreamVerdicts
// RPCs, whose soul_id filter is optional. An empty soul ID built the prefix
// "{ws}/alerts/events//", which no stored key can match, so an unfiltered
// request always saw zero events.
func TestListAlertEventsWithoutSoulIDReturnsEveryWorkspaceEvent(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	save := func(workspace, soul, id string, age time.Duration) {
		t.Helper()
		ev := &core.AlertEvent{ID: id, SoulID: soul, WorkspaceID: workspace, Status: core.SoulDead, Timestamp: now.Add(-age)}
		if err := db.SaveAlertEvent(ev); err != nil {
			t.Fatal(err)
		}
	}
	save("default", "soul-a", "ev-a1", 3*time.Minute)
	save("default", "soul-a", "ev-a2", 2*time.Minute)
	save("default", "soul-b", "ev-b1", time.Minute)
	save("team-x", "soul-c", "ev-c1", 30*time.Second)

	ids := func(events []*core.AlertEvent) string {
		out := ""
		for _, e := range events {
			out += e.ID + " "
		}
		return out
	}

	// Control: a soul filter returns exactly that soul's events.
	got, err := db.ListAlertEvents("soul-a", 0)
	if err != nil || len(got) != 2 {
		t.Fatalf("control soul-a: %d events (%s) err=%v, want 2", len(got), ids(got), err)
	}

	// No filter: every event of every workspace, newest first.
	all, err := db.ListAlertEvents("", 0)
	if err != nil {
		t.Fatal(err)
	}
	want := "ev-c1 ev-b1 ev-a2 ev-a1 "
	if ids(all) != want {
		t.Fatalf("unfiltered events = %q, want %q", ids(all), want)
	}

	// The limit applies to the merged, newest-first list.
	limited, err := db.ListAlertEvents("", 2)
	if err != nil || ids(limited) != "ev-c1 ev-b1 " {
		t.Fatalf("limit 2 = %q err=%v, want %q", ids(limited), err, "ev-c1 ev-b1 ")
	}

	// A soul whose ID merely starts with another's must not be matched by
	// that other's filter.
	save("default", "soul-a2", "ev-prefix", 10*time.Second)
	got, _ = db.ListAlertEvents("soul-a", 0)
	if len(got) != 2 {
		t.Fatalf("soul-a filter matched %d events (%s), want 2 (soul-a2 is another soul)", len(got), ids(got))
	}
	_ = fmt.Sprint()
}
