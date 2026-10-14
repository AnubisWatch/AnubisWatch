package raft

import (
	"fmt"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestDistributorRecomputePlanOwnership(t *testing.T) {
	for _, count := range []int{0, 1, 3} {
		t.Run(fmt.Sprintf("souls_%d", count), func(t *testing.T) {
			d := NewDistributor("node", "region", core.StrategyRoundRobin)
			d.AddNode(&core.NodeInfo{ID: "node", CanProbe: true, MaxSouls: 10})
			for i := 0; i < count; i++ {
				d.AddSoul(&core.Soul{ID: fmt.Sprint(i)})
			}
			for iteration := 0; iteration < 2; iteration++ {
				entered, release := make(chan struct{}), make(chan struct{})
				observed := make(chan []core.SoulAssignment, 1)
				d.SetOnRebalanceCallback(func(plan core.DistributionPlan) {
					close(entered)
					<-release
					snapshot := append([]core.SoulAssignment(nil), plan.Assignments...)
					for i := range plan.Assignments {
						plan.Assignments[i].NodeID = "callback-edit"
					}
					observed <- snapshot
				})
				returned, err := d.Recompute()
				if err != nil {
					t.Fatal(err)
				}
				<-entered
				control := d.GetPlan()
				for i := range control.Assignments {
					control.Assignments[i].NodeID = "getter-edit"
				}
				for i := range returned.Assignments {
					returned.Assignments[i].NodeID = "caller-edit"
				}
				internalBefore := d.GetPlan()
				close(release)
				callbackSaw := <-observed
				for i, a := range internalBefore.Assignments {
					if a.NodeID != "node" {
						t.Errorf("return value changed retained plan[%d] to %q", i, a.NodeID)
					}
				}
				for i, a := range callbackSaw {
					if a.NodeID != "node" {
						t.Errorf("return value changed callback plan[%d] to %q", i, a.NodeID)
					}
				}
				for i, a := range returned.Assignments {
					if a.NodeID != "caller-edit" {
						t.Errorf("callback changed return plan[%d] to %q", i, a.NodeID)
					}
				}
				for i, a := range d.GetPlan().Assignments {
					if a.NodeID != "node" {
						t.Errorf("callback changed retained plan[%d] to %q", i, a.NodeID)
					}
				}
				if len(d.GetNodeAssignments("node")) != count {
					t.Errorf("node index disagrees with plan")
				}
			}
		})
	}
}
