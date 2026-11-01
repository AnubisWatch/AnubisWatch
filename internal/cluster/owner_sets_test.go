package cluster

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func ownerSetFixture(souls, nodes int) ([]*core.Soul, []NodeLoad) {
	ss := make([]*core.Soul, souls)
	for i := range ss {
		ss[i] = &core.Soul{ID: fmt.Sprintf("soul-%03d", i)}
	}
	ms := make([]NodeLoad, nodes)
	for i := range ms {
		ms[i] = NodeLoad{NodeID: fmt.Sprintf("node-%d", i), Region: "default"}
	}
	return ss, ms
}

var allStrategies = []DistributionStrategy{StrategyHashBased, StrategyRoundRobin, StrategyRegionAware, StrategyLoadBased}

func TestPlanOwnerSetsPrimaryMatchesSingleOwnerPlan(t *testing.T) {
	souls, members := ownerSetFixture(60, 4)
	for _, strategy := range allStrategies {
		single := PlanOwnership(strategy, souls, members)
		for _, redundancy := range []int{0, 1, 2, 3, 4} {
			sets := PlanOwnerSets(strategy, souls, members, redundancy)
			if len(sets) != len(single) {
				t.Fatalf("%s/%d: %d souls planned, want %d", strategy, redundancy, len(sets), len(single))
			}
			for id, owners := range sets {
				if owners[0] != single[id] {
					t.Fatalf("%s/%d: primary of %s = %s, single-owner plan says %s", strategy, redundancy, id, owners[0], single[id])
				}
			}
		}
	}
}

func TestPlanOwnerSetsAreDistinctAndSized(t *testing.T) {
	souls, members := ownerSetFixture(60, 4)
	for _, strategy := range allStrategies {
		for redundancy, want := range map[int]int{1: 1, 2: 2, 3: 3, 4: 4, 9: 4} {
			for id, owners := range PlanOwnerSets(strategy, souls, members, redundancy) {
				if len(owners) != want {
					t.Fatalf("%s/%d: %s has %d owners, want %d", strategy, redundancy, id, len(owners), want)
				}
				seen := map[string]bool{}
				for _, o := range owners {
					if seen[o] {
						t.Fatalf("%s/%d: %s lists %s twice: %v", strategy, redundancy, id, o, owners)
					}
					seen[o] = true
				}
			}
		}
	}
}

func TestPlanOwnerSetsIsDeterministic(t *testing.T) {
	souls, members := ownerSetFixture(40, 3)
	reversed := make([]NodeLoad, len(members))
	for i := range members {
		reversed[len(members)-1-i] = members[i]
	}
	for _, strategy := range allStrategies {
		a := PlanOwnerSets(strategy, souls, members, 2)
		b := PlanOwnerSets(strategy, souls, reversed, 2)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("%s: plan depends on member order", strategy)
		}
	}
}

func TestPlanOwnerSetsHonoursSoulRegions(t *testing.T) {
	souls, members := ownerSetFixture(30, 4)
	members[0].Region, members[1].Region = "eu", "eu"
	members[2].Region, members[3].Region = "us", "us"
	for _, s := range souls {
		s.Regions = []string{"eu"}
	}
	for _, strategy := range allStrategies {
		for id, owners := range PlanOwnerSets(strategy, souls, members, 3) {
			if len(owners) != 2 {
				t.Fatalf("%s: %s has %d owners, want the 2 eu nodes", strategy, id, len(owners))
			}
			for _, o := range owners {
				if o != "node-0" && o != "node-1" {
					t.Fatalf("%s: %s given %s outside its regions", strategy, id, o)
				}
			}
		}
	}
}

func TestPlanOwnerSetsHashMovesLittleWhenAMemberLeaves(t *testing.T) {
	souls, members := ownerSetFixture(200, 5)
	before := PlanOwnerSets(StrategyHashBased, souls, members, 2)
	after := PlanOwnerSets(StrategyHashBased, souls, members[:4], 2)
	moved := 0
	for id, owners := range before {
		involved := false
		for _, o := range owners {
			if o == "node-4" {
				involved = true
			}
		}
		if !involved && !reflect.DeepEqual(owners, after[id]) {
			t.Fatalf("%s changed owners although node-4 was not involved: %v -> %v", id, owners, after[id])
		}
		if involved {
			moved++
		}
	}
	if moved == 0 || moved > 120 {
		t.Fatalf("%d of 200 sets involved the removed member; want roughly 2/5", moved)
	}
}

func TestPlanOwnerSetsSpreadsBackupsEvenly(t *testing.T) {
	souls, members := ownerSetFixture(120, 4)
	for _, strategy := range []DistributionStrategy{StrategyRoundRobin, StrategyLoadBased} {
		counts := map[string]int{}
		for _, owners := range PlanOwnerSets(strategy, souls, members, 2) {
			counts[owners[1]]++
		}
		for node, n := range counts {
			if n < 25 || n > 35 {
				t.Fatalf("%s: %s backs up %d souls, want about 30: %v", strategy, node, n, counts)
			}
		}
	}
}
