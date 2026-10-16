package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Every node used to probe every soul: nothing consumed the distributor. Soul
// ownership is now a plan each node computes on its own from the replicated
// souls and live members, so the plan must be a pure function of those inputs.

func ownershipSouls(n int) []*core.Soul {
	souls := make([]*core.Soul, n)
	for i := range souls {
		souls[i] = &core.Soul{ID: fmt.Sprintf("soul-%03d", i)}
	}
	return souls
}

func ownershipMembers(ids ...string) []NodeLoad {
	members := make([]NodeLoad, len(ids))
	for i, id := range ids {
		members[i] = NodeLoad{NodeID: id, Region: "default"}
	}
	return members
}

func TestPlanOwnershipIsIndependentOfInputOrder(t *testing.T) {
	for _, strategy := range []DistributionStrategy{StrategyHashBased, StrategyRoundRobin, StrategyLoadBased, StrategyRegionAware} {
		t.Run(strategy.String(), func(t *testing.T) {
			souls := ownershipSouls(50)
			forward := PlanOwnership(strategy, souls, ownershipMembers("a", "b", "c"))

			reversed := make([]*core.Soul, len(souls))
			for i, s := range souls {
				reversed[len(souls)-1-i] = s
			}
			backward := PlanOwnership(strategy, reversed, ownershipMembers("c", "a", "b"))

			if len(forward) != len(souls) {
				t.Fatalf("planned %d of %d souls", len(forward), len(souls))
			}
			for id, owner := range forward {
				if backward[id] != owner {
					t.Fatalf("soul %s: owner %s vs %s depending on input order; nodes would disagree", id, owner, backward[id])
				}
			}
		})
	}
}

func TestHashOwnershipMovesOnlyTheDepartedNodesSouls(t *testing.T) {
	souls := ownershipSouls(200)
	before := PlanOwnership(StrategyHashBased, souls, ownershipMembers("a", "b", "c"))
	after := PlanOwnership(StrategyHashBased, souls, ownershipMembers("a", "b"))

	counts := map[string]int{}
	for id, owner := range before {
		counts[owner]++
		if owner != "c" && after[id] != owner {
			t.Fatalf("soul %s moved from %s to %s although only c left", id, owner, after[id])
		}
		if after[id] == "c" {
			t.Fatalf("soul %s still owned by the departed node", id)
		}
	}
	for _, id := range []string{"a", "b", "c"} {
		if counts[id] < 30 {
			t.Fatalf("hash ownership is badly skewed: %v", counts)
		}
	}
}

func TestRoundRobinOwnershipIsEven(t *testing.T) {
	plan := PlanOwnership(StrategyRoundRobin, ownershipSouls(9), ownershipMembers("a", "b", "c"))
	counts := map[string]int{}
	for _, owner := range plan {
		counts[owner]++
	}
	for _, id := range []string{"a", "b", "c"} {
		if counts[id] != 3 {
			t.Fatalf("round-robin counts = %v, want 3 each", counts)
		}
	}
}

func TestOwnershipHonoursSoulRegions(t *testing.T) {
	members := []NodeLoad{{NodeID: "east-1", Region: "east"}, {NodeID: "west-1", Region: "west"}}
	souls := []*core.Soul{
		{ID: "only-west", Regions: []string{"west"}},
		{ID: "nowhere", Regions: []string{"mars"}},
		{ID: "prefers-east", Region: "east"},
	}
	plan := PlanOwnership(StrategyRegionAware, souls, members)
	if plan["only-west"] != "west-1" {
		t.Fatalf("a west-only soul was given to %q", plan["only-west"])
	}
	if owner, ok := plan["nowhere"]; ok {
		t.Fatalf("a soul for a region no member serves was given to %q", owner)
	}
	if plan["prefers-east"] != "east-1" {
		t.Fatalf("region-aware ownership ignored the soul's region: %q", plan["prefers-east"])
	}
}

func TestParseStrategy(t *testing.T) {
	tests := []struct {
		in    string
		want  DistributionStrategy
		known bool
	}{
		{"", StrategyHashBased, true},
		{"hash", StrategyHashBased, true},
		{"consistent_hash", StrategyHashBased, true},
		{"round-robin", StrategyRoundRobin, true},
		{"Region-Aware", StrategyRegionAware, true},
		{"least-loaded", StrategyLoadBased, true},
		{"latency-optimized", StrategyHashBased, false},
		{"redundant", StrategyHashBased, false},
	}
	for _, tt := range tests {
		got, known := ParseStrategy(tt.in)
		if got != tt.want || known != tt.known {
			t.Errorf("ParseStrategy(%q) = %v, %v; want %v, %v", tt.in, got, known, tt.want, tt.known)
		}
	}
}

func TestDistributorRebuildAnswersOwnershipQueries(t *testing.T) {
	d := NewDistributor("a", "default", StrategyHashBased, newTestLogger())
	d.RegisterNode("a", "default")
	d.RegisterNode("b", "default")
	plan := d.Rebuild(ownershipSouls(10), ownershipMembers("a", "b"))

	total := 0
	for _, node := range []string{"a", "b"} {
		souls := d.GetSoulsForNode(node)
		total += len(souls)
		for _, id := range souls {
			if owner, _ := d.GetNodeForSoul(id); owner != node || plan[id] != node {
				t.Fatalf("soul %s: table says %s, plan says %s, listed under %s", id, owner, plan[id], node)
			}
		}
		if load := d.GetLoadDistribution()[node]; load.SoulCount != len(souls) {
			t.Fatalf("node %s soul count %d, owns %d", node, load.SoulCount, len(souls))
		}
	}
	if total != 10 {
		t.Fatalf("rebuild assigned %d of 10 souls", total)
	}
}

func TestStandaloneManagerOwnsEverySoul(t *testing.T) {
	db := newTestDB(t)
	t.Cleanup(func() { db.Close() })
	m, err := NewManager(core.NecropolisConfig{NodeName: "solo"}, db, newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	plan := m.PlanSouls(ownershipSouls(5))
	for id, owner := range plan {
		if owner != m.NodeID() {
			t.Fatalf("standalone soul %s planned on %q, not this node %q", id, owner, m.NodeID())
		}
	}
	if len(plan) != 5 {
		t.Fatalf("standalone plan covers %d of 5 souls", len(plan))
	}
}

// fakeApplier records batches and can be told to fail.
type fakeApplier struct {
	mu      sync.Mutex
	fail    bool
	batches [][]core.FSMCommand
}

func (f *fakeApplier) apply(cmd core.FSMCommand, _ time.Duration) (uint64, uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return 0, 0, &core.RaftError{Code: core.ErrNoLeader, Message: "no leader"}
	}
	if cmd.Op != core.FSMBatch {
		return 0, 0, errors.New("not a batch")
	}
	var batch []core.FSMCommand
	if err := json.Unmarshal(cmd.Value, &batch); err != nil {
		return 0, 0, err
	}
	f.batches = append(f.batches, batch)
	return uint64(len(f.batches)), 1, nil
}

func (f *fakeApplier) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var keys []string
	for _, batch := range f.batches {
		for _, cmd := range batch {
			keys = append(keys, cmd.Key)
		}
	}
	return keys
}

func TestBatcherKeepsOrderAndBoundsBatches(t *testing.T) {
	applier := &fakeApplier{}
	b := NewBatcher(applier.apply, time.Hour, newTestLogger())
	for i := 0; i < 70; i++ {
		b.Enqueue(core.FSMCommand{Op: core.FSMSet, Key: fmt.Sprintf("k%02d", i), Value: []byte("v")})
	}
	for b.Pending() > 0 {
		if err := b.flushOnce(); err != nil {
			t.Fatal(err)
		}
	}
	keys := applier.keys()
	if len(keys) != 70 {
		t.Fatalf("flushed %d of 70 commands", len(keys))
	}
	for i, key := range keys {
		if key != fmt.Sprintf("k%02d", i) {
			t.Fatalf("command %d is %s: batches must keep arrival order", i, key)
		}
	}
	for _, batch := range applier.batches {
		if len(batch) > defaultBatchMaxCommands {
			t.Fatalf("a batch carried %d commands, cap %d", len(batch), defaultBatchMaxCommands)
		}
	}
}

func TestBatcherRetriesAfterFailure(t *testing.T) {
	applier := &fakeApplier{fail: true}
	b := NewBatcher(applier.apply, 10*time.Millisecond, newTestLogger())
	b.Start()
	b.Enqueue(core.FSMCommand{Op: core.FSMSet, Key: "first"})
	b.Enqueue(core.FSMCommand{Op: core.FSMSet, Key: "second"})
	time.Sleep(50 * time.Millisecond) // several failed flush attempts

	applier.mu.Lock()
	applier.fail = false
	applier.mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for b.Pending() > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	b.Stop(ctx)
	if keys := applier.keys(); len(keys) != 2 || keys[0] != "first" || keys[1] != "second" {
		t.Fatalf("after recovery the batcher replicated %v, want [first second]", keys)
	}
}

func TestBatcherDropsOldestWhenBacklogOverflows(t *testing.T) {
	b := NewBatcher((&fakeApplier{fail: true}).apply, time.Hour, newTestLogger())
	b.maxPending = 3
	for i := 0; i < 5; i++ {
		b.Enqueue(core.FSMCommand{Key: fmt.Sprintf("k%d", i)})
	}
	if b.Pending() != 3 || b.pending[0].Key != "k2" {
		t.Fatalf("backlog = %d starting at %q, want the newest 3 starting at k2", b.Pending(), b.pending[0].Key)
	}
}

func TestBatcherStopFlushesQueued(t *testing.T) {
	applier := &fakeApplier{}
	b := NewBatcher(applier.apply, time.Hour, newTestLogger())
	b.Start()
	b.Enqueue(core.FSMCommand{Op: core.FSMSet, Key: "queued"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	b.Stop(ctx)
	if keys := applier.keys(); len(keys) != 1 {
		t.Fatalf("Stop left queued commands unreplicated: %v", keys)
	}
}
