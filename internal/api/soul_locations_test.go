package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type fakeVoteSource struct {
	votes map[string][]core.ProbeVote // workspace/soul -> votes
	err   error
	asked []string
}

func (f *fakeVoteSource) ListProbeVotes(_ context.Context, ws, soul string) ([]core.ProbeVote, error) {
	f.asked = append(f.asked, ws+"/"+soul)
	return f.votes[ws+"/"+soul], f.err
}

func locationsServer(store *mockStorage, votes ProbeVoteSource) *RESTServer {
	server := soulUptimeServer(store)
	server.router.Handle("GET", "/api/v1/souls/:id/locations", server.requireAuth(server.handleSoulLocations))
	server.probeVotes = votes
	return server
}

func getLocations(t *testing.T, server *RESTServer, soulID string) (*httptest.ResponseRecorder, SoulLocationsResponse) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/souls/"+soulID+"/locations", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)
	var body SoulLocationsResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return rec, body
}

func TestSoulLocationsListsPrimaryThenVotes(t *testing.T) {
	store := newMockStorage()
	store.souls["s1"] = &core.Soul{ID: "s1", Name: "s1", WorkspaceID: "default", Weight: core.Duration{Duration: time.Minute}}
	now := time.Now()
	store.judgments["j1"] = &core.Judgment{
		ID: "j1", SoulID: "s1", JackalID: "tr-1", Region: "tr", Status: core.SoulDegraded,
		Duration: 40 * time.Millisecond, Timestamp: now.Add(-5 * time.Second),
	}
	votes := &fakeVoteSource{votes: map[string][]core.ProbeVote{"default/s1": {
		{JackalID: "us-1", Status: core.SoulDead, Timestamp: now.Add(-10 * time.Second), Latency: 300 * time.Millisecond, Region: "us"},
		{JackalID: "fr-1", Status: core.SoulAlive, Timestamp: now.Add(-10 * time.Minute), Latency: 80 * time.Millisecond, Region: "fr"},
		{JackalID: "tr-1", Status: core.SoulAlive, Timestamp: now, Region: "tr"}, // the primary's own id: not listed twice
	}}}

	rec, body := getLocations(t, locationsServer(store, votes), "s1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if len(body.Locations) != 3 {
		t.Fatalf("want 3 locations, got %+v", body.Locations)
	}
	primary := body.Locations[0]
	if !primary.Primary || primary.JackalID != "tr-1" || primary.Status != core.SoulDegraded || primary.LatencyMs != 40 || primary.Stale {
		t.Errorf("primary entry wrong: %+v", primary)
	}
	fr, us := body.Locations[1], body.Locations[2]
	if fr.JackalID != "fr-1" || us.JackalID != "us-1" {
		t.Fatalf("votes not sorted by jackal id: %+v", body.Locations)
	}
	if fr.Primary || us.Primary {
		t.Error("a vote was marked primary")
	}
	if !fr.Stale {
		t.Error("a vote older than two intervals must be stale")
	}
	if us.Stale || us.LatencyMs != 300 || us.Region != "us" || us.Status != core.SoulDead {
		t.Errorf("fresh vote wrong: %+v", us)
	}
}

func TestSoulLocationsWithoutVotesHoldsOnlyTheLatestJudgment(t *testing.T) {
	store := newMockStorage()
	store.souls["s1"] = &core.Soul{ID: "s1", Name: "s1", WorkspaceID: "default"}

	// Before any check, and with no vote source, the list is empty, not null.
	rec, body := getLocations(t, locationsServer(store, nil), "s1")
	if rec.Code != http.StatusOK || body.Locations == nil || len(body.Locations) != 0 {
		t.Fatalf("empty case: code %d locations %+v", rec.Code, body.Locations)
	}
	if got := rec.Body.String(); !json.Valid([]byte(got)) || !strings.Contains(got, `"locations":[]`) {
		t.Errorf("locations must encode as [], got %s", got)
	}

	addJudgments(store, "s1", time.Second, core.SoulAlive, 1, 25*time.Millisecond)
	_, body = getLocations(t, locationsServer(store, nil), "s1")
	if len(body.Locations) != 1 || !body.Locations[0].Primary || body.Locations[0].Stale {
		t.Fatalf("single prober: %+v", body.Locations)
	}
}

func TestSoulLocationsRespectsWorkspaceAndMissingSouls(t *testing.T) {
	store := newMockStorage()
	store.souls["theirs"] = &core.Soul{ID: "theirs", Name: "theirs", WorkspaceID: "other"}
	votes := &fakeVoteSource{votes: map[string][]core.ProbeVote{"other/theirs": {{JackalID: "x", Status: core.SoulDead, Timestamp: time.Now()}}}}
	server := locationsServer(store, votes)

	if rec, _ := getLocations(t, server, "theirs"); rec.Code != http.StatusForbidden {
		t.Errorf("other workspace: want 403, got %d", rec.Code)
	}
	if rec, _ := getLocations(t, server, "nope"); rec.Code != http.StatusNotFound {
		t.Errorf("missing soul: want 404, got %d", rec.Code)
	}
	if len(votes.asked) != 0 {
		t.Errorf("votes of a forbidden or missing soul were read: %v", votes.asked)
	}
}

func TestSoulLocationsReportsVoteStoreFailure(t *testing.T) {
	store := newMockStorage()
	store.souls["s1"] = &core.Soul{ID: "s1", Name: "s1", WorkspaceID: "default"}
	votes := &fakeVoteSource{err: errors.New("disk on fire")}
	rec, _ := getLocations(t, locationsServer(store, votes), "s1")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "disk on fire") {
		t.Errorf("internal error text leaked to the client: %s", rec.Body)
	}
}
