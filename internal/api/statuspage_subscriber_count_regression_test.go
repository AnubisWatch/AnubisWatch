package api

// Status page responses carry subscriber_count: the number of confirmed email
// subscribers, computed on read. Pending subscriptions are not counted, no
// address leaves the server, and a page outside the caller's workspace is
// neither listed nor readable.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// subscriberCountStorage adds ConfirmedSubscriberCounts to the mock store,
// counting the way CobaltDB does.
type subscriberCountStorage struct {
	*mockStorage
	subs []*core.StatusPageSubscription
}

func (s *subscriberCountStorage) ConfirmedSubscriberCounts() (map[string]int, error) {
	counts := map[string]int{}
	for _, sub := range s.subs {
		if sub.Confirmed {
			counts[sub.PageID]++
		}
	}
	return counts, nil
}

func TestStatusPageSubscriberCountIsConfirmedAndWorkspaceScoped(t *testing.T) {
	base := newMockStorage()
	base.statusPages["mine"] = &core.StatusPage{ID: "mine", WorkspaceID: "default", Name: "Mine", Slug: "mine", Enabled: true}
	base.statusPages["theirs"] = &core.StatusPage{ID: "theirs", WorkspaceID: "other", Name: "Theirs", Slug: "theirs", Enabled: true}
	store := &subscriberCountStorage{mockStorage: base, subs: []*core.StatusPageSubscription{
		{ID: "s1", PageID: "mine", Email: "a@example.com", Confirmed: true},
		{ID: "s2", PageID: "mine", Email: "b@example.com", Confirmed: true},
		{ID: "s3", PageID: "mine", Email: "pending@example.com", Confirmed: false},
		{ID: "s4", PageID: "theirs", Email: "c@example.com", Confirmed: true},
	}}
	server := statusPagePasswordServer(store)

	list := statusPagePasswordRequest(t, server, "GET", "/api/v1/status-pages", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list: status %d: %s", list.Code, list.Body.String())
	}
	if strings.Contains(list.Body.String(), "@example.com") {
		t.Fatalf("list exposes subscriber addresses: %s", list.Body.String())
	}
	var pages []map[string]any
	if err := json.Unmarshal(list.Body.Bytes(), &pages); err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || pages[0]["id"] != "mine" {
		t.Fatalf("list = %v, want only the caller's page", pages)
	}
	if got := pages[0]["subscriber_count"]; got != float64(2) {
		t.Errorf("list subscriber_count = %v, want 2 (confirmed only)", got)
	}

	get := statusPagePasswordRequest(t, server, "GET", "/api/v1/status-pages/mine", nil)
	var page map[string]any
	if err := json.Unmarshal(get.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if got := page["subscriber_count"]; got != float64(2) {
		t.Errorf("get subscriber_count = %v, want 2", got)
	}

	if rec := statusPagePasswordRequest(t, server, "GET", "/api/v1/status-pages/theirs", nil); rec.Code != http.StatusForbidden {
		t.Errorf("another workspace's page: status %d, want 403", rec.Code)
	}

	// A client echoing subscriber_count on PUT cannot change it or persist it.
	put := statusPagePasswordRequest(t, server, "PUT", "/api/v1/status-pages/mine",
		map[string]any{"name": "Mine", "slug": "mine", "subscriber_count": 99})
	if put.Code != http.StatusOK {
		t.Fatalf("put: status %d: %s", put.Code, put.Body.String())
	}
	var updated map[string]any
	if err := json.Unmarshal(put.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if got := updated["subscriber_count"]; got != float64(2) {
		t.Errorf("put subscriber_count = %v, want 2", got)
	}
}

// Without a counting store the field is still present, as 0.
func TestStatusPageSubscriberCountDefaultsToZero(t *testing.T) {
	server := statusPagePasswordServer(newMockStorage())
	rec := statusPagePasswordRequest(t, server, "POST", "/api/v1/status-pages", map[string]any{"name": "New", "slug": "new"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status %d: %s", rec.Code, rec.Body.String())
	}
	var page map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if got, ok := page["subscriber_count"]; !ok || got != float64(0) {
		t.Errorf("subscriber_count = %v (present %v), want 0", got, ok)
	}
}
