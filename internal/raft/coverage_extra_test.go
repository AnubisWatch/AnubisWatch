package raft

import (
	"errors"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type failingStorage struct {
	data            map[string][]byte
	setErr          error
	deletePrefixErr error
	failSetKey      string
}

func (s *failingStorage) Get(key string) ([]byte, error) { return s.data[key], nil }
func (s *failingStorage) Set(key string, value []byte) error {
	if s.setErr != nil && (s.failSetKey == "" || s.failSetKey == key) {
		return s.setErr
	}
	s.data[key] = value
	return nil
}
func (s *failingStorage) Delete(key string) error { delete(s.data, key); return nil }
func (s *failingStorage) DeletePrefix(string) error {
	if s.deletePrefixErr != nil {
		return s.deletePrefixErr
	}
	for key := range s.data {
		delete(s.data, key)
	}
	return nil
}
func (s *failingStorage) List(string) ([]string, error) { return nil, nil }

func TestStorageFSMAdditionalErrorPaths(t *testing.T) {
	boom := errors.New("boom")
	store := &failingStorage{data: make(map[string][]byte), setErr: boom}
	fsm := NewStorageFSM(store)
	if got := fsm.Apply(&core.RaftLogEntry{Index: 1, Type: core.LogCommand, Data: []byte("not-json")}); got == nil {
		t.Fatal("expected command decode error")
	}
	cmd, err := fsm.encodeCommand(&core.FSMCommand{Op: core.FSMOp(255)})
	if err != nil {
		t.Fatal(err)
	}
	if got := fsm.Apply(&core.RaftLogEntry{Index: 1, Type: core.LogCommand, Data: cmd}); got == nil {
		t.Fatal("expected unknown operation error")
	}
	if got := fsm.Apply(&core.RaftLogEntry{Index: 1, Type: core.LogConfiguration, Data: []byte("config")}); !errors.Is(got.(error), boom) {
		t.Fatalf("configuration error = %v", got)
	}

	store.setErr = nil
	store.deletePrefixErr = boom
	if err := fsm.Restore([]byte(`{"key":"dmFsdWU="}`)); !errors.Is(err, boom) {
		t.Fatalf("delete-prefix error = %v", err)
	}
	store.deletePrefixErr = nil
	store.setErr = boom
	store.failSetKey = "key"
	if err := fsm.Restore([]byte(`{"key":"dmFsdWU="}`)); !errors.Is(err, boom) {
		t.Fatalf("restore set error = %v", err)
	}
}

func TestRaftDistributorAdditionalBranches(t *testing.T) {
	d := NewDistributor("local", "east", "")
	if d.strategy != core.StrategyRoundRobin {
		t.Fatalf("default strategy = %q", d.strategy)
	}

	node := &core.NodeInfo{ID: "n1", Region: "east", CanProbe: true, MaxSouls: 1, LoadAvg: .1, MemoryUsage: .1}
	d.AddNode(node)
	d.UpdateNode(&core.NodeInfo{ID: "n1", Region: "east", CanProbe: true, MaxSouls: 1})
	d.UpdateNode(&core.NodeInfo{ID: "n2", Region: "west", CanProbe: true, MaxSouls: 1})
	d.RemoveNode("n2")
	d.AddSoul(&core.Soul{ID: "s1", Region: "missing"})
	d.RemoveSoul("missing")

	if got := d.distributeRedundant([]*core.NodeInfo{node}); len(got) != 1 {
		t.Fatalf("single-node redundant = %v", got)
	}
	if got := d.distributeLatencyOptimal(nil); got != nil {
		t.Fatalf("empty latency distribution = %v", got)
	}
	if got := d.pickLeastLoaded(nil); got != nil {
		t.Fatalf("empty least-loaded = %v", got)
	}
	if got := d.pickBestWeighted(nil, 1); got != nil {
		t.Fatalf("empty weighted = %v", got)
	}

	over := &core.NodeInfo{ID: "over", MaxSouls: 1, AssignedSouls: 3}
	valid := &core.NodeInfo{ID: "valid", MaxSouls: 5, AssignedSouls: 1}
	if got := d.pickBestWeighted([]*core.NodeInfo{over, valid}, 4); got != valid {
		t.Fatalf("weighted pick = %#v", got)
	}
	if score := d.latencyScore(&core.NodeInfo{LoadAvg: 2, MemoryUsage: 2}); score != 1 {
		t.Fatalf("capped score = %v", score)
	}

	d.souls = map[string]*core.Soul{"s1": {ID: "s1"}}
	if err := d.ValidatePlan(core.DistributionPlan{}); err == nil {
		t.Fatal("expected incomplete plan error")
	}
	if err := d.ValidatePlan(core.DistributionPlan{Assignments: []core.SoulAssignment{{SoulID: "s1", NodeID: "unknown"}}}); err == nil {
		t.Fatal("expected unknown node error")
	}
	d.nodes["n1"] = node
	if err := d.ValidatePlan(core.DistributionPlan{Assignments: []core.SoulAssignment{{SoulID: "s1", NodeID: "n1"}}}); err != nil {
		t.Fatal(err)
	}
}
