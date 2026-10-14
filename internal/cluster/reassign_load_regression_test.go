package cluster

import (
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
)

func auditReassignLoad(t *testing.T, fail bool, attempts int, recoverNode bool) {
	t.Helper()
	d := NewDistributor("node", "local", StrategyRoundRobin, newTestLogger())
	d.RegisterNode("node", "local")
	if _, err := d.AssignSoul(&core.Soul{ID: "soul"}); err != nil {
		t.Fatal(err)
	}
	if fail {
		d.mu.Lock()
		d.nodeLoads["node"].Healthy = false
		d.mu.Unlock()
	}
	for i := 0; i < attempts; i++ {
		err := d.ReassignSoul("soul")
		if (err != nil) != fail {
			t.Fatalf("expected failed=%v got %v", fail, err)
		}
		owner, ok := d.GetNodeForSoul("soul")
		if !ok || owner != "node" {
			t.Fatal("owner not preserved")
		}
		load := d.GetLoadDistribution()["node"].SoulCount
		t.Logf("fail=%v iteration=%d EXPECTED: load=1 ACTUAL: load=%d", fail, i, load)
		if load != 1 {
			t.Fatal("regression mismatch")
		}
	}
	if recoverNode {
		d.RegisterNode("node", "local")
		if err := d.ReassignSoul("soul"); err != nil {
			t.Fatal(err)
		}
	}
	d.UnassignSoul("soul")
	load := d.GetLoadDistribution()["node"].SoulCount
	if load != 0 {
		t.Fatalf("expected empty load=0 got %d", load)
	}
}
func TestAuditReassignLoadRollback(t *testing.T) {
	t.Run("healthy-control", func(t *testing.T) { auditReassignLoad(t, false, 1, false) })
	t.Run("failure", func(t *testing.T) { auditReassignLoad(t, true, 1, false) })
	t.Run("repeated-failures", func(t *testing.T) { auditReassignLoad(t, true, 3, false) })
	t.Run("recover-after-failure", func(t *testing.T) { auditReassignLoad(t, true, 2, true) })
	t.Run("repeated-healthy-control", func(t *testing.T) { auditReassignLoad(t, false, 3, false) })
}
