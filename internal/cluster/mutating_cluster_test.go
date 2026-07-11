package cluster

import (
	"fmt"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestDistributorLifecycleIsIdempotent(t *testing.T) {
	d := NewDistributor("node-1", "region-1", StrategyRoundRobin, newTestLogger())
	d.rebalanceInterval = time.Millisecond

	d.Start()
	d.Start()
	d.Stop()
	d.Stop()
	d.Start()
}

func TestDistributorStopBeforeStart(t *testing.T) {
	d := NewDistributor("node-1", "region-1", StrategyRoundRobin, newTestLogger())
	d.Stop()
	d.Start()
	d.Stop()
}

func TestDistributorLoadDistributionIsDeepCopy(t *testing.T) {
	d := NewDistributor("node-1", "region-1", StrategyRoundRobin, newTestLogger())
	d.RegisterNode("node-1", "region-1")

	loads := d.GetLoadDistribution()
	loads["node-1"].SoulCount = 99

	fresh := d.GetLoadDistribution()
	if fresh["node-1"].SoulCount != 0 {
		t.Fatalf("caller mutated distributor state through returned pointer: %+v", fresh["node-1"])
	}
}

func TestDistributorRebalanceCallbackRunsOutsideLock(t *testing.T) {
	d := NewDistributor("node-1", "region-1", StrategyRoundRobin, newTestLogger())
	d.RegisterNode("node-1", "region-1")
	d.RegisterNode("node-2", "region-1")
	for i := 0; i < 4; i++ {
		if _, err := d.AssignSoul(&core.Soul{ID: fmt.Sprintf("soul-%d", i)}); err != nil {
			t.Fatal(err)
		}
	}

	d.mu.Lock()
	for soulID := range d.soulMap {
		d.soulMap[soulID] = "node-1"
	}
	d.nodeLoads["node-1"].SoulCount = 4
	d.nodeLoads["node-2"].SoulCount = 0
	d.mu.Unlock()

	called := make(chan struct{})
	d.SetCallbacks(nil, nil, func([]SoulMove) {
		_ = d.GetLoadDistribution()
		close(called)
	})

	done := make(chan struct{})
	go func() {
		d.checkAndRebalance()
		close(done)
	}()

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("rebalance callback deadlocked acquiring distributor lock")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("rebalance did not finish")
	}
}
