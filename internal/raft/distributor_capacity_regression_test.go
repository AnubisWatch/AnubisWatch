package raft

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestDistributorWeightedCapacity(t *testing.T) {
	check := func(ok bool, message string) {
		t.Helper()
		if !ok {
			t.Fatal(message)
		}
	}

	cases := []struct {
		name                 string
		max, assigned, souls int
		fail                 bool
	}{
		{name: "capacity boundary", max: 2, souls: 2},
		{name: "over capacity", max: 2, souls: 3, fail: true},
		{name: "zero capacity", souls: 1, fail: true},
		{name: "already full", max: 2, assigned: 2, souls: 1, fail: true},
		{name: "existing assignments consume capacity", max: 3, assigned: 1, souls: 3, fail: true},
		{name: "already over capacity", max: 2, assigned: 3, souls: 1, fail: true},
		{name: "empty input"},
	}
	for _, c := range cases {
		d := NewDistributor("node", "region", core.StrategyWeighted)
		input := &core.NodeInfo{ID: "node", CanProbe: true, MaxSouls: c.max, AssignedSouls: c.assigned}
		d.AddNode(input)
		for i := 0; i < c.souls; i++ {
			d.AddSoul(&core.Soul{ID: fmt.Sprint(i)})
		}
		previous := d.GetPlan()
		plan, err := d.Recompute()
		if c.fail {
			check(err != nil && len(plan.Assignments) == 0, c.name+": expected capacity error")
			check(reflect.DeepEqual(previous, d.GetPlan()), c.name+": failure changed published plan")
		} else {
			check(err == nil && len(plan.Assignments) == c.souls, c.name+": expected complete plan")
		}
		check(input.AssignedSouls == c.assigned, c.name+": input metadata mutated")
	}
	d := NewDistributor("node", "region", core.StrategyWeighted)
	d.AddNode(&core.NodeInfo{ID: "full", CanProbe: true, MaxSouls: 1, AssignedSouls: 1})
	d.AddNode(&core.NodeInfo{ID: "node", CanProbe: true, MaxSouls: 2})
	d.AddSoul(&core.Soul{ID: "a"})
	d.AddSoul(&core.Soul{ID: "b"})
	_, err := d.Recompute()
	check(err == nil, "mixed-capacity control failed")
	previous, stats := d.GetPlan(), d.GetStats()
	for _, a := range previous.Assignments {
		check(a.NodeID == "node", "assigned to exhausted node")
	}
	d.AddSoul(&core.Soul{ID: "c"})
	for i := 0; i < 2; i++ {
		_, err = d.Recompute()
		check(err != nil, "expected repeated capacity error")
		check(reflect.DeepEqual(previous, d.GetPlan()), "failed recompute changed previous valid plan")
		check(stats.Revision == d.GetStats().Revision && reflect.DeepEqual(stats.NodeDistribution, d.GetStats().NodeDistribution), "failed recompute changed assignment index/revision")
	}
	d.UpdateNode(&core.NodeInfo{ID: "node", CanProbe: true, MaxSouls: 3})
	recovered, err := d.Recompute()
	check(err == nil && len(recovered.Assignments) == 3, "capacity recovery failed")
}
