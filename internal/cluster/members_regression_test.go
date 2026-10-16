package cluster

import (
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Regression: the cluster manager could only report a peer count, so
// GET /api/v1/cluster/peers had no member list to return.
func TestManager_MembersReportsThisNode(t *testing.T) {
	cases := []struct {
		name         string
		necro        core.NecropolisConfig
		wantState    string
		wantLeader   bool
		wantContact  bool
		wantID       string
		wantName     string
		wantAddress  string
		wantRegion   string
		mutateConfig func(*core.RaftConfig)
	}{
		{
			name:      "standalone is its own leader",
			necro:     core.NecropolisConfig{NodeName: "jackal-a", Region: "eu"},
			wantState: "leader", wantLeader: true, wantContact: true,
			wantID: "test-node-1", wantName: "jackal-a", wantAddress: "127.0.0.1:7000", wantRegion: "eu",
		},
		{
			name:      "clustered before the raft node starts",
			necro:     core.NecropolisConfig{Enabled: true},
			wantState: "unknown", wantLeader: false, wantContact: false,
			wantID: "test-node-1", wantName: "test-node-1", wantAddress: "127.0.0.1:7000",
		},
		{
			name:      "no node id falls back to a stable name",
			necro:     core.NecropolisConfig{},
			wantState: "leader", wantLeader: true, wantContact: true,
			wantID: "standalone", wantName: "standalone", wantAddress: "127.0.0.1:0",
			mutateConfig: func(c *core.RaftConfig) { c.NodeID = ""; c.AdvertiseAddr = "" },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			defer db.Close()
			raftCfg := newTestRaftConfig()
			if tc.mutateConfig != nil {
				tc.mutateConfig(&raftCfg)
			}
			tc.necro.Raft = raftCfg
			manager, err := NewManager(tc.necro, db, newTestLogger())
			if err != nil {
				t.Fatal(err)
			}
			members := manager.Members()
			if len(members) != 1 {
				t.Fatalf("expected only this node, got %+v", members)
			}
			self := members[0]
			if !self.IsSelf || self.State != tc.wantState || self.IsLeader != tc.wantLeader || self.LastContact.IsZero() == tc.wantContact {
				t.Fatalf("unexpected self entry: %+v", self)
			}
			if self.ID != tc.wantID || self.Name != tc.wantName || self.Address != tc.wantAddress || self.Region != tc.wantRegion {
				t.Fatalf("unexpected identity: %+v", self)
			}
		})
	}
}
