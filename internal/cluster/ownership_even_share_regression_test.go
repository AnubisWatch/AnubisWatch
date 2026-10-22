package cluster

import (
	"testing"
)

// A member that joins a cluster must receive a share of the souls, or it
// joins, is admitted, becomes a voter -- and then idles while the members
// that were there before keep every soul.
//
// The default "hash" strategy cannot promise that: rendezvous hashing gives a
// member only the souls it outranks, so with a dozen souls over four members
// a member can win none of them. The even strategies do promise it, and a
// cluster test that waits for a joining member to take over work depends on
// that promise. This pins the promise, and the boundary at which it holds.

func TestEvenStrategyGivesEveryMemberAShare(t *testing.T) {
	for _, strategy := range []DistributionStrategy{StrategyRoundRobin, StrategyLoadBased} {
		for _, memberCount := range []int{2, 3, 4, 5} {
			for _, soulsPerMember := range []int{1, 2, 3, 7} {
				soulCount := memberCount * soulsPerMember
				members := ownershipMembers("jackal-0", "jackal-1", "jackal-2", "jackal-3", "jackal-4")[:memberCount]
				souls := ownershipSouls(soulCount)

				plan := PlanOwnership(strategy, souls, members)

				counts := map[string]int{}
				for _, owner := range plan {
					counts[owner]++
				}
				if len(plan) != soulCount {
					t.Fatalf("%s: planned %d of %d souls", strategy, len(plan), soulCount)
				}
				for _, member := range members {
					if counts[member.NodeID] == 0 {
						t.Errorf("%s: member %s received no soul of %d (%v)", strategy, member.NodeID, soulCount, counts)
					}
				}
				for _, member := range members {
					want := soulCount / memberCount
					if counts[member.NodeID] != want {
						t.Errorf("%s: member %s owns %d souls, want %d (%v)", strategy, member.NodeID, counts[member.NodeID], want, counts)
					}
				}
			}
		}
	}
}

// The join case: a member joins a cluster that already owns every soul. The
// new member must get a share of them, and every soul must still have
// exactly one owner, or the members would disagree about who probes what.
func TestEvenStrategyGivesJoiningMemberAShare(t *testing.T) {
	for _, strategy := range []DistributionStrategy{StrategyRoundRobin, StrategyLoadBased} {
		souls := ownershipSouls(12)
		before := ownershipMembers("jackal-0", "jackal-1", "jackal-2")
		after := ownershipMembers("jackal-0", "jackal-1", "jackal-2", "jackal-3")

		plan := PlanOwnership(strategy, souls, after)

		counts := map[string]int{}
		owners := map[string]int{}
		for id, owner := range plan {
			counts[owner]++
			owners[id]++
		}
		if len(plan) != len(souls) {
			t.Fatalf("%s: planned %d of %d souls", strategy, len(plan), len(souls))
		}
		for id, n := range owners {
			if n != 1 {
				t.Errorf("%s: soul %s has %d owners", strategy, id, n)
			}
		}
		if counts["jackal-3"] == 0 {
			t.Errorf("%s: the joining member received no soul (%v)", strategy, counts)
		}
		for _, member := range after {
			if counts[member.NodeID] != len(souls)/len(after) {
				t.Errorf("%s: member %s owns %d souls, want %d (%v)", strategy, member.NodeID, counts[member.NodeID], len(souls)/len(after), counts)
			}
		}
		// Control: the members that were there first keep a share too, so the
		// join does not drain them.
		for _, member := range before {
			if counts[member.NodeID] == 0 {
				t.Errorf("%s: the joining member took every soul from %s (%v)", strategy, member.NodeID, counts)
			}
		}
	}
}

// The boundary: with fewer souls than members, no strategy can give every
// member one. The even strategies spread what there is as evenly as they
// can, so a member can still own nothing -- the join test's cluster has more
// souls than members, which is what makes its share guaranteed.
func TestEvenStrategyWithFewerSoulsThanMembers(t *testing.T) {
	souls := ownershipSouls(2)
	members := ownershipMembers("jackal-0", "jackal-1", "jackal-2")

	plan := PlanOwnership(StrategyLoadBased, souls, members)

	counts := map[string]int{}
	for _, owner := range plan {
		counts[owner]++
	}
	if len(plan) != len(souls) {
		t.Fatalf("planned %d of %d souls", len(plan), len(souls))
	}
	if counts["jackal-0"] != 1 || counts["jackal-1"] != 1 {
		t.Errorf("the two souls were not spread one each: %v", counts)
	}
	if counts["jackal-2"] != 0 {
		t.Errorf("the third member owns a soul although there are only %d souls: %v", len(souls), counts)
	}
}
