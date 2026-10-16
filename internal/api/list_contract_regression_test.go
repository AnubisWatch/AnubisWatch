package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Regressions in list endpoint contracts the dashboard depends on.

func callHandler(t *testing.T, handler Handler, method, target, workspace string, params map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:   httptest.NewRequest(method, target, nil),
		Response:  rec,
		Workspace: workspace,
		Params:    params,
		User:      &User{ID: "u1", Workspace: workspace},
	}
	if err := handler(ctx); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return rec
}

// /cluster/peers answered {peer_count,is_leader} when clustered and [] when
// standalone, while the Cluster page expects an array of peers.

type clusteredPeerManager struct {
	*mockClusterManager
	peers []ClusterPeer
}

func (m *clusteredPeerManager) IsClustered() bool           { return true }
func (m *clusteredPeerManager) ClusterPeers() []ClusterPeer { return m.peers }

func TestClusterPeers_AlwaysAnArrayOfPeers(t *testing.T) {
	clustered := &clusteredPeerManager{
		mockClusterManager: &mockClusterManager{isLeader: true},
		peers: []ClusterPeer{
			{ID: "jackal-1", Name: "jackal-1", Address: "10.0.0.1:7946", State: "leader", IsLeader: true, LastContact: "2026-01-01T00:00:00Z"},
			{ID: "jackal-2", Name: "jackal-2", Address: "10.0.0.2:7946", State: "follower"},
		},
	}
	cases := []struct {
		name      string
		cluster   ClusterManager
		wantIDs   []string
		wantState string
	}{
		{"standalone", &mockClusterManager{}, []string{"standalone"}, "leader"},
		{"no cluster manager", nil, []string{"standalone"}, "leader"},
		{"clustered", clustered, []string{"jackal-1", "jackal-2"}, "leader"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newTestServerWithStorage(newMockStorage())
			server.cluster = tc.cluster
			rec := callHandler(t, server.handleClusterPeers, http.MethodGet, "/api/v1/cluster/peers", "default", nil)

			var peers []map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &peers); err != nil {
				t.Fatalf("response is not a JSON array: %s", rec.Body.String())
			}
			if len(peers) != len(tc.wantIDs) {
				t.Fatalf("got %d peers, want %d: %s", len(peers), len(tc.wantIDs), rec.Body.String())
			}
			for i, peer := range peers {
				for _, field := range []string{"id", "name", "address", "state", "last_contact", "is_leader", "region"} {
					if _, ok := peer[field]; !ok {
						t.Errorf("peer %d missing %q: %v", i, field, peer)
					}
				}
				if peer["id"] != tc.wantIDs[i] {
					t.Errorf("peer %d id = %v, want %s", i, peer["id"], tc.wantIDs[i])
				}
			}
			if peers[0]["state"] != tc.wantState || peers[0]["is_leader"] != true {
				t.Errorf("first peer should be this node as leader, got %v", peers[0])
			}
		})
	}
}

// /stats/overview counted never-checked souls as dead, and no status
// reported a soul under maintenance as embalmed.

func TestStatsOverview_UnknownDegradedAndEmbalmedAreCounted(t *testing.T) {
	store := newMockStorage()
	now := time.Now()
	add := func(id string, status core.SoulStatus, tags ...string) {
		store.souls[id] = &core.Soul{ID: id, Name: id, WorkspaceID: "default", Tags: tags}
		if status != "" {
			store.judgments["j-"+id] = &core.Judgment{ID: "j-" + id, SoulID: id, Status: status, Timestamp: now.Add(-time.Minute)}
		}
	}
	add("alive", core.SoulAlive)
	add("dead", core.SoulDead)
	add("degraded", core.SoulDegraded)
	add("never-checked", "")
	add("in-maintenance", core.SoulDead, "db")
	store.windows["w1"] = &core.MaintenanceWindow{ID: "w1", WorkspaceID: "default", Tags: []string{"db"}, Enabled: true, StartTime: now.Add(-time.Hour), EndTime: now.Add(time.Hour)}

	server := newTestServerWithStorage(store)
	rec := callHandler(t, server.handleStatsOverview, http.MethodGet, "/api/v1/stats/overview", "default", nil)
	var got struct {
		Souls map[string]int `json:"souls"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"total": 5, "healthy": 1, "dead": 1, "degraded": 1, "unknown": 1, "embalmed": 1}
	for key, value := range want {
		if got.Souls[key] != value {
			t.Errorf("souls.%s = %d, want %d (all: %v)", key, got.Souls[key], value, got.Souls)
		}
	}
}

func TestSoulStatus_DegradedAndEmbalmedPassThrough(t *testing.T) {
	store := newMockStorage()
	now := time.Now()
	cases := []struct {
		id          string
		judgment    core.SoulStatus
		maintenance bool
		want        string
	}{
		{"alive", core.SoulAlive, false, "healthy"},
		{"dead", core.SoulDead, false, "unhealthy"},
		{"degraded", core.SoulDegraded, false, "degraded"},
		{"unchecked", "", false, "unknown"},
		{"maintained-dead", core.SoulDead, true, "embalmed"},
		{"maintained-unchecked", "", true, "embalmed"},
	}
	var maintained []string
	for _, tc := range cases {
		store.souls[tc.id] = &core.Soul{ID: tc.id, Name: tc.id, WorkspaceID: "default"}
		if tc.judgment != "" {
			store.judgments["j-"+tc.id] = &core.Judgment{ID: "j-" + tc.id, SoulID: tc.id, Status: tc.judgment, Timestamp: now.Add(-time.Minute)}
		}
		if tc.maintenance {
			maintained = append(maintained, tc.id)
		}
	}
	store.windows["w1"] = &core.MaintenanceWindow{ID: "w1", WorkspaceID: "default", SoulIDs: maintained, Enabled: true, StartTime: now.Add(-time.Hour), EndTime: now.Add(time.Hour)}
	// Another workspace's window must not embalm souls here.
	store.windows["w2"] = &core.MaintenanceWindow{ID: "w2", WorkspaceID: "other", Enabled: true, StartTime: now.Add(-time.Hour), EndTime: now.Add(time.Hour)}
	server := newTestServerWithStorage(store)

	rec := callHandler(t, server.handleListSouls, http.MethodGet, "/api/v1/souls?limit=100", "default", nil)
	var list struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	byID := map[string]string{}
	for _, soul := range list.Data {
		byID[soul["id"].(string)] = soul["status"].(string)
	}
	for _, tc := range cases {
		if byID[tc.id] != tc.want {
			t.Errorf("list status of %s = %q, want %q", tc.id, byID[tc.id], tc.want)
		}
		single := callHandler(t, server.handleGetSoul, http.MethodGet, "/api/v1/souls/"+tc.id, "default", map[string]string{"id": tc.id})
		var soul map[string]any
		_ = json.Unmarshal(single.Body.Bytes(), &soul)
		if soul["status"] != tc.want {
			t.Errorf("get status of %s = %v, want %q", tc.id, soul["status"], tc.want)
		}
	}
	// Maintenance is a reporting overlay: stored judgments keep their status.
	if store.judgments["j-maintained-dead"].Status != core.SoulDead {
		t.Fatal("stored judgment was rewritten")
	}
}

// Judgment lists carried only soul_id; the Judgments page searches and
// labels rows by soul_name.

func TestJudgmentLists_IncludeSoulName(t *testing.T) {
	store := newMockStorage()
	now := time.Now()
	for i, name := range []string{"Checkout", "Search"} {
		id := fmt.Sprintf("soul-%d", i)
		store.souls[id] = &core.Soul{ID: id, Name: name, WorkspaceID: "default"}
		store.judgments["j-"+id] = &core.Judgment{ID: "j-" + id, SoulID: id, Status: core.SoulAlive, Timestamp: now.Add(-time.Minute)}
	}
	server := newTestServerWithStorage(store)
	want := map[string]string{"soul-0": "Checkout", "soul-1": "Search"}

	rec := callHandler(t, server.handleListAllJudgments, http.MethodGet, "/api/v1/judgments", "default", nil)
	var all []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if len(all) != 2 {
		t.Fatalf("got %d judgments, want 2", len(all))
	}
	for _, j := range all {
		if j["soul_name"] != want[j["soul_id"].(string)] {
			t.Errorf("GET /judgments row %v has soul_name %v", j["soul_id"], j["soul_name"])
		}
	}

	rec = callHandler(t, server.handleListJudgments, http.MethodGet, "/api/v1/souls/soul-1/judgments", "default", map[string]string{"id": "soul-1"})
	var page struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 1 || page.Data[0]["soul_name"] != "Search" {
		t.Fatalf("per-soul judgments should carry soul_name, got %s", rec.Body.String())
	}
}

// Chart buckets were built from a Go map and returned in random order.

func TestJudgmentChartBucketsAreTimeOrdered(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Hour).Add(-20 * time.Hour)
	var stamps []time.Time
	// Insert newest first so insertion order cannot pass for sorting.
	for i := 19; i >= 0; i-- {
		stamps = append(stamps, base.Add(time.Duration(i)*time.Hour+time.Minute))
	}
	for attempt := 0; attempt < 5; attempt++ {
		got := audit50BucketQuery(t, stamps)
		if len(got) != 20 {
			t.Fatalf("got %d buckets, want 20", len(got))
		}
		for i := 1; i < len(got); i++ {
			if got[i-1]["time"].(string) >= got[i]["time"].(string) {
				t.Fatalf("buckets out of order at %d: %v then %v", i, got[i-1]["time"], got[i]["time"])
			}
		}
	}
}

// /journeys returned at most 20 rows with no way to learn the total.

type journeyListStore struct {
	*mockStorage
	all []*core.JourneyConfig
}

func (s *journeyListStore) ListJourneysNoCtx(_ string, offset, limit int) ([]*core.JourneyConfig, error) {
	if offset >= len(s.all) {
		return []*core.JourneyConfig{}, nil
	}
	end := len(s.all)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return s.all[offset:end], nil
}

func TestListJourneys_TotalCountHeaderAndPaging(t *testing.T) {
	store := &journeyListStore{mockStorage: newMockStorage()}
	for i := 0; i < 130; i++ {
		store.all = append(store.all, &core.JourneyConfig{ID: fmt.Sprintf("j-%03d", i), Name: "journey", WorkspaceID: "default"})
	}
	server := newTestServerWithStorage(store)

	cases := []struct {
		query     string
		wantLen   int
		wantFirst string
	}{
		{"", 20, "j-000"},
		{"?limit=100", 100, "j-000"},
		{"?limit=500", 100, "j-000"}, // clamped, not reset to 20
		{"?offset=100&limit=100", 30, "j-100"},
		{"?offset=500", 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			rec := callHandler(t, server.handleListJourneys, http.MethodGet, "/api/v1/journeys"+tc.query, "default", nil)
			if got := rec.Header().Get("X-Total-Count"); got != "130" {
				t.Fatalf("X-Total-Count = %q, want 130", got)
			}
			var items []map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
				t.Fatalf("response must stay a bare array: %v", err)
			}
			if len(items) != tc.wantLen {
				t.Fatalf("got %d journeys, want %d", len(items), tc.wantLen)
			}
			if tc.wantLen > 0 && items[0]["id"] != tc.wantFirst {
				t.Fatalf("first id = %v, want %s", items[0]["id"], tc.wantFirst)
			}
		})
	}
}
