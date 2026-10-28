package storage

import (
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestConfirmedSubscriberCountsCountsOnlyConfirmed(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	subs := []*core.StatusPageSubscription{
		{ID: "s1", PageID: "p1", Email: "a@example.com", Confirmed: true},
		{ID: "s2", PageID: "p1", Email: "b@example.com", Confirmed: true},
		{ID: "s3", PageID: "p1", Email: "c@example.com"},
		{ID: "s4", PageID: "p2", Email: "d@example.com", Confirmed: true},
		{ID: "s5", PageID: "p3", Email: "e@example.com"},
	}
	for _, sub := range subs {
		if err := db.SaveStatusPageSubscription(sub); err != nil {
			t.Fatal(err)
		}
	}
	counts, err := db.ConfirmedSubscriberCounts()
	if err != nil {
		t.Fatal(err)
	}
	if counts["p1"] != 2 || counts["p2"] != 1 || counts["p3"] != 0 || len(counts) != 2 {
		t.Errorf("counts = %v, want p1:2 p2:1", counts)
	}
}
