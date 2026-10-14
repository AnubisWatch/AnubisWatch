package cluster

import (
	"fmt"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
)

func auditRebalanceCapacity(t *testing.T, nodes, souls int, second bool) {
	t.Helper()
	d := NewDistributor("node-0", "test", StrategyRoundRobin, newTestLogger())
	d.RegisterNode("node-0", "test")
	for i := 0; i < souls; i++ {
		owner, err := d.AssignSoul(&core.Soul{ID: fmt.Sprintf("soul-%d", i)})
		if err != nil || owner != "node-0" {
			t.Fatalf("initial assignment: %s %v", owner, err)
		}
	}
	for i := 1; i < nodes; i++ {
		d.RegisterNode(fmt.Sprintf("node-%d", i), "test")
	}
	var moves []SoulMove
	calls := 0
	d.SetCallbacks(nil, nil, func(m []SoulMove) { calls++; moves = append(moves, m...) })
	d.checkAndRebalance()
	want := souls / nodes
	seen := make(map[string]bool)
	for _, move := range moves {
		if seen[move.SoulID] {
			t.Fatal("regression mismatch", "duplicate planned soul", move.SoulID)
		}
		seen[move.SoulID] = true
	}
	for i := 0; i < nodes; i++ {
		id := fmt.Sprintf("node-%d", i)
		actual := len(d.GetSoulsForNode(id))
		tracked := d.nodeLoads[id].SoulCount
		t.Logf("%s EXPECTED: assigned=%d tracked=%d ACTUAL: assigned=%d tracked=%d", id, want, want, actual, tracked)
		if actual != want || tracked != want {
			t.Error("regression mismatch")
		}
	}
	if len(d.soulMap) != souls {
		t.Fatal("regression mismatch", "lost assignments")
	}
	if second {
		firstCalls := calls
		d.checkAndRebalance()
		if calls != firstCalls {
			t.Fatal("regression mismatch", "balanced cluster moved again")
		}
	}
}
func TestAuditRebalanceDestinationCapacity(t *testing.T) {
	t.Run("two-node-control", func(t *testing.T) { auditRebalanceCapacity(t, 2, 4, false) })
	t.Run("three-node-capacity", func(t *testing.T) { auditRebalanceCapacity(t, 3, 6, false) })
	t.Run("four-node-capacity", func(t *testing.T) { auditRebalanceCapacity(t, 4, 8, false) })
	t.Run("second-rebalance-is-noop", func(t *testing.T) { auditRebalanceCapacity(t, 3, 6, true) })
}
