package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// `anubis summon` and `anubis banish` called POST /api/v1/cluster/nodes and
// DELETE /api/v1/cluster/nodes/:id, which were never registered. And a write
// the cluster could not commit for want of a leader surfaced as a 500, which
// tells a client nothing about whether to retry.

type membershipCluster struct {
	mockClusterManager
	addErr, removeErr error
	added             []string
	removed           []string
}

func (m *membershipCluster) IsClustered() bool { return true }

func (m *membershipCluster) AddNode(id, address, region string) error {
	if m.addErr != nil {
		return m.addErr
	}
	m.added = append(m.added, id+"@"+address+"/"+region)
	return nil
}

func (m *membershipCluster) RemoveNode(id string) error {
	if m.removeErr != nil {
		return m.removeErr
	}
	m.removed = append(m.removed, id)
	return nil
}

func serveMembership(t *testing.T, cluster ClusterManager, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	server := NewRESTServer(core.ServerConfig{Port: 8080}, core.AuthConfig{Enabled: core.BoolPtr(true)}, newMockStorage(),
		&mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, cluster, nil, nil, nil, nil, newTestLogger())
	t.Cleanup(func() { _ = server.Stop(context.Background()) })
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)
	return rec
}

func TestClusterNodeRoutes(t *testing.T) {
	leaderDown := &core.RaftError{Code: core.ErrNoLeader, Message: "no leader"}
	tests := []struct {
		name    string
		cluster ClusterManager
		method  string
		path    string
		token   string
		body    string
		want    int
	}{
		{"add", &membershipCluster{}, "POST", "/api/v1/cluster/nodes", "valid-token", `{"node_id":"jackal-4","address":"10.0.0.4:7946","region":"eu"}`, http.StatusCreated},
		{"add requires members role", &membershipCluster{}, "POST", "/api/v1/cluster/nodes", "viewer-token", `{"node_id":"jackal-4","address":"10.0.0.4:7946"}`, http.StatusForbidden},
		{"add rejects a bad address", &membershipCluster{}, "POST", "/api/v1/cluster/nodes", "valid-token", `{"node_id":"jackal-4","address":"nowhere"}`, http.StatusBadRequest},
		{"add rejects a bad id", &membershipCluster{}, "POST", "/api/v1/cluster/nodes", "valid-token", `{"node_id":"a/b","address":"10.0.0.4:7946"}`, http.StatusBadRequest},
		{"add existing", &membershipCluster{addErr: ErrClusterNodeExists}, "POST", "/api/v1/cluster/nodes", "valid-token", `{"node_id":"jackal-1","address":"10.0.0.1:7946"}`, http.StatusConflict},
		{"add without a leader", &membershipCluster{addErr: leaderDown}, "POST", "/api/v1/cluster/nodes", "valid-token", `{"node_id":"jackal-4","address":"10.0.0.4:7946"}`, http.StatusServiceUnavailable},
		{"add when standalone", &mockClusterManager{}, "POST", "/api/v1/cluster/nodes", "valid-token", `{"node_id":"jackal-4","address":"10.0.0.4:7946"}`, http.StatusConflict},
		{"remove", &membershipCluster{}, "DELETE", "/api/v1/cluster/nodes/jackal-2", "valid-token", "", http.StatusNoContent},
		{"remove requires members role", &membershipCluster{}, "DELETE", "/api/v1/cluster/nodes/jackal-2", "viewer-token", "", http.StatusForbidden},
		{"remove unknown", &membershipCluster{removeErr: ErrClusterNodeAbsent}, "DELETE", "/api/v1/cluster/nodes/ghost", "valid-token", "", http.StatusNotFound},
		{"remove leader", &membershipCluster{removeErr: ErrClusterIsLeader}, "DELETE", "/api/v1/cluster/nodes/jackal-0", "valid-token", "", http.StatusConflict},
		{"remove failure", &membershipCluster{removeErr: errors.New("boom")}, "DELETE", "/api/v1/cluster/nodes/jackal-2", "valid-token", "", http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serveMembership(t, tt.cluster, tt.method, tt.path, tt.token, tt.body)
			if rec.Code != tt.want {
				t.Fatalf("%s %s = %d (%s), want %d", tt.method, tt.path, rec.Code, strings.TrimSpace(rec.Body.String()), tt.want)
			}
			if tt.want == http.StatusServiceUnavailable && rec.Header().Get("Retry-After") == "" {
				t.Fatal("a 503 for a missing leader must carry Retry-After")
			}
		})
	}

	cluster := &membershipCluster{}
	serveMembership(t, cluster, "POST", "/api/v1/cluster/nodes", "valid-token", `{"node_id":"jackal-4","address":"10.0.0.4:7946","region":"eu"}`)
	serveMembership(t, cluster, "DELETE", "/api/v1/cluster/nodes/jackal-4", "valid-token", "")
	if len(cluster.added) != 1 || cluster.added[0] != "jackal-4@10.0.0.4:7946/eu" || len(cluster.removed) != 1 || cluster.removed[0] != "jackal-4" {
		t.Fatalf("membership calls = added %v removed %v", cluster.added, cluster.removed)
	}
}

// noLeaderStorage refuses soul writes the way a clustered store does when no
// leader is reachable.
type noLeaderStorage struct {
	*mockStorage
	err error
}

func (s *noLeaderStorage) SaveSoul(context.Context, *core.Soul) error { return s.err }

func TestSoulWriteWithoutLeaderIs503(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		want  int
		retry bool
	}{
		{"no leader", &core.RaftError{Code: core.ErrNoLeader, Message: "no leader"}, http.StatusServiceUnavailable, true},
		{"wrapped no leader", fmt.Errorf("consensus write failed: %w", &core.RaftError{Code: core.ErrNoLeader}), http.StatusServiceUnavailable, true},
		{"timeout stays 500", &core.RaftError{Code: core.ErrTimeout, Message: "slow"}, http.StatusInternalServerError, false},
		{"other error stays 500", errors.New("disk full"), http.StatusInternalServerError, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &noLeaderStorage{mockStorage: newMockStorage(), err: tt.err}
			server := newTestServerWithStorage(store)
			t.Cleanup(func() { _ = server.Stop(context.Background()) })
			req := httptest.NewRequest("POST", "/api/v1/souls", strings.NewReader(`{"name":"s","type":"tcp","target":"10.1.1.1:80"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer valid-token")
			rec := httptest.NewRecorder()
			server.router.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d (%s), want %d", rec.Code, strings.TrimSpace(rec.Body.String()), tt.want)
			}
			if got := rec.Header().Get("Retry-After") != ""; got != tt.retry {
				t.Fatalf("Retry-After present = %v, want %v", got, tt.retry)
			}
			// Exactly one JSON document: the 503 used to be followed by a
			// second "internal server error" body.
			dec := json.NewDecoder(rec.Body)
			var first map[string]any
			if err := dec.Decode(&first); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if dec.More() {
				t.Fatalf("response has more than one JSON body after %v", first)
			}
		})
	}
}

// addressMembershipCluster can add a node by address, learning its ID.
type addressMembershipCluster struct {
	membershipCluster
	identifyErr error
}

func (m *addressMembershipCluster) AddNodeByAddress(address, region string) (string, string, error) {
	if m.identifyErr != nil {
		return "", "", m.identifyErr
	}
	if region == "" {
		region = "learned-region"
	}
	m.added = append(m.added, "jackal-real@"+address+"/"+region)
	return "jackal-real", region, nil
}

// `anubis summon <addr>` used to invent a node ID from the address, which no
// node actually used. Without node_id the server now learns the ID from the
// node itself and reports it.
func TestAddClusterNodeByAddress(t *testing.T) {
	tests := []struct {
		name     string
		cluster  ClusterManager
		body     string
		want     int
		wantBody string
	}{
		{"learns the id", &addressMembershipCluster{}, `{"address":"10.0.0.4:7946"}`, http.StatusCreated, `"node_id":"jackal-real"`},
		{"keeps a given region", &addressMembershipCluster{}, `{"address":"10.0.0.4:7946","region":"eu"}`, http.StatusCreated, `"region":"eu"`},
		{"unreachable node", &addressMembershipCluster{identifyErr: fmt.Errorf("%w: dial refused", ErrClusterNodeUnreachable)}, `{"address":"10.0.0.4:7946"}`, http.StatusBadGateway, "could not be reached"},
		{"already a member", &addressMembershipCluster{identifyErr: ErrClusterNodeExists}, `{"address":"10.0.0.4:7946"}`, http.StatusConflict, ""},
		{"no by-address support needs an id", &membershipCluster{}, `{"address":"10.0.0.4:7946"}`, http.StatusBadRequest, "node_id is required"},
		{"bad address", &addressMembershipCluster{}, `{"address":"nowhere"}`, http.StatusBadRequest, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serveMembership(t, tt.cluster, "POST", "/api/v1/cluster/nodes", "valid-token", tt.body)
			if rec.Code != tt.want {
				t.Fatalf("status = %d (%s), want %d", rec.Code, strings.TrimSpace(rec.Body.String()), tt.want)
			}
			if tt.wantBody != "" && !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("body %s does not contain %s", rec.Body.String(), tt.wantBody)
			}
		})
	}
}
