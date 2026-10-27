package raft

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type gatedSnapshotPublication struct {
	SnapshotStore
	published chan struct{}
	release   chan struct{}
}
type gatedPublicationSink struct {
	SnapshotSink
	owner *gatedSnapshotPublication
}

func (s *gatedSnapshotPublication) Create(version, index, term uint64, c []byte) (SnapshotSink, error) {
	sink, e := s.SnapshotStore.Create(version, index, term, c)
	if e != nil {
		return nil, e
	}
	return &gatedPublicationSink{sink, s}, nil
}
func (s *gatedPublicationSink) Close() error {
	if e := s.SnapshotSink.Close(); e != nil {
		return e
	}
	close(s.owner.published)
	<-s.owner.release
	return nil
}

type failingSnapshotHardState struct {
	*InMemoryStableStore
	fail bool
}

func (s *failingSnapshotHardState) Set(k string, v []byte) error {
	if s.fail {
		return errors.New("injected stable write failure")
	}
	return s.InMemoryStableStore.Set(k, v)
}

func TestSnapshotInstallRejectsTermChangedDuringPublication(t *testing.T) {
	store := NewInMemoryStorage()
	fsm := NewStorageFSM(store)
	store.Set("k", []byte("latest"))
	fsm.SetLastApplied(1)
	snapshots := &gatedSnapshotPublication{NewInMemorySnapshotStore(), make(chan struct{}), make(chan struct{})}
	n, e := NewNode(newTestRaftNodeConfig(), NewInMemoryLogStore(), snapshots, fsm, newTestRaftLogger())
	if e != nil {
		t.Fatal(e)
	}
	n.currentTerm = 2
	n.commitIndex = 1
	n.lastApplied = 1
	payload, _ := json.Marshal(map[string][]byte{"k": []byte("old-leader-image")})
	done := make(chan *core.InstallSnapshotResponse, 1)
	go func() {
		done <- n.handleInstallSnapshot(&core.InstallSnapshotRequest{Term: 2, LeaderID: "old", LastIncludedIndex: 2, LastIncludedTerm: 2, Data: payload, Done: true})
	}()
	<-snapshots.published
	n.handleHeartbeat(&core.HeartbeatRequest{Term: 3, LeaderID: "new"})
	close(snapshots.release)
	response := <-done
	value, e := store.Get("k")
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("EXPECTED: old leader transfer rejected after newer term; ACTUAL: success=%v term=%d value=%q", response.Success, n.currentTerm, value)
	if response.Success || n.currentTerm != 3 || string(value) != "latest" {
		t.Fatal("PROBLEM CONFIRMED")
	}
	t.Log("PROBLEM NOT REPRODUCED")
}

func TestSnapshotInstallHardStateFailureKeepsMatchingFSMBoundary(t *testing.T) {
	store := NewInMemoryStorage()
	fsm := NewStorageFSM(store)
	store.Set("k", []byte("before"))
	fsm.SetLastApplied(1)
	stable := &failingSnapshotHardState{InMemoryStableStore: NewInMemoryStableStore()}
	snapshots := NewInMemorySnapshotStore()
	n, e := NewNodeWithStableStore(newTestRaftNodeConfig(), NewInMemoryLogStore(), stable, snapshots, fsm, newTestRaftLogger())
	if e != nil {
		t.Fatal(e)
	}
	n.currentTerm = 2
	n.commitIndex = 1
	n.lastApplied = 1
	if e := n.persistHardStateLocked(); e != nil {
		t.Fatal(e)
	}
	stable.fail = true
	payload, _ := json.Marshal(map[string][]byte{"k": []byte("durable-snapshot")})
	response := n.handleInstallSnapshot(&core.InstallSnapshotRequest{Term: 2, LeaderID: "leader", LastIncludedIndex: 2, LastIncludedTerm: 2, Data: payload, Done: true})
	if response.Success {
		t.Fatal("control failed: stable storage failure acknowledged")
	}
	t.Logf("EXPECTED: replaced FSM/log/index agree at durable snapshot boundary; ACTUAL: fsm=%d applied=%d base=%d log0=%d", fsm.LastApplied(), n.lastApplied, n.logBase, n.log[0].Index)
	if fsm.LastApplied() != n.lastApplied || n.logBase != n.log[0].Index {
		t.Fatal("PROBLEM CONFIRMED")
	}
	stable.fail = false
	recoveredFSM := NewStorageFSM(NewInMemoryStorage())
	recovered, err := NewNodeWithStableStore(newTestRaftNodeConfig(), n.storage, stable, snapshots, recoveredFSM, newTestRaftLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err = recovered.restoreHardState(); err != nil {
		t.Fatal(err)
	}
	if err = recovered.restoreLatestSnapshot(); err != nil {
		t.Fatal(err)
	}
	if recovered.lastApplied != 2 || recovered.commitIndex != 2 || recoveredFSM.LastApplied() != 2 {
		t.Fatalf("restart did not recover durable image boundary: %d/%d/%d", recovered.lastApplied, recovered.commitIndex, recoveredFSM.LastApplied())
	}
	t.Log("PROBLEM NOT REPRODUCED")
}

type failSnapshotPublication struct{ SnapshotStore }
type failPublicationSink struct{ SnapshotSink }

func (s *failSnapshotPublication) Create(version, index, term uint64, c []byte) (SnapshotSink, error) {
	sink, e := s.SnapshotStore.Create(version, index, term, c)
	if e != nil {
		return nil, e
	}
	return &failPublicationSink{sink}, nil
}
func (s *failPublicationSink) Close() error {
	return errors.New("injected snapshot publication failure")
}

func TestSnapshotInstallPublicationFailurePreservesLiveState(t *testing.T) {
	store := NewInMemoryStorage()
	fsm := NewStorageFSM(store)
	store.Set("k", []byte("latest"))
	fsm.SetLastApplied(1)
	n, e := NewNode(newTestRaftNodeConfig(), NewInMemoryLogStore(), &failSnapshotPublication{NewInMemorySnapshotStore()}, fsm, newTestRaftLogger())
	if e != nil {
		t.Fatal(e)
	}
	n.currentTerm = 2
	n.commitIndex = 1
	n.lastApplied = 1
	payload, _ := json.Marshal(map[string][]byte{"k": []byte("replacement")})
	r := n.handleInstallSnapshot(&core.InstallSnapshotRequest{Term: 2, LeaderID: "leader", LastIncludedIndex: 2, LastIncludedTerm: 2, Data: payload, Done: true})
	v, e := store.Get("k")
	if r.Success || e != nil || string(v) != "latest" || fsm.LastApplied() != 1 || n.lastApplied != 1 {
		t.Fatalf("failed publication replaced live state: response=%+v value=%q fsm=%d applied=%d", r, v, fsm.LastApplied(), n.lastApplied)
	}
}

func TestSnapshotInstallMalformedPayloadKeepsDurableImage(t *testing.T) {
	store := NewInMemoryStorage()
	fsm := NewStorageFSM(store)
	snapshots := NewInMemorySnapshotStore()
	n, e := NewNode(newTestRaftNodeConfig(), NewInMemoryLogStore(), snapshots, fsm, newTestRaftLogger())
	if e != nil {
		t.Fatal(e)
	}
	n.currentTerm = 2
	payload, _ := json.Marshal(map[string][]byte{"k": []byte("latest")})
	r := n.handleInstallSnapshot(&core.InstallSnapshotRequest{Term: 2, LeaderID: "leader", LastIncludedIndex: 1, LastIncludedTerm: 2, Data: payload, Done: true})
	if !r.Success {
		t.Fatal("fresh snapshot rejected")
	}
	r = n.handleInstallSnapshot(&core.InstallSnapshotRequest{Term: 2, LeaderID: "leader", LastIncludedIndex: 2, LastIncludedTerm: 2, Data: []byte("invalid json"), Done: true})
	metas, e := snapshots.List()
	if r.Success || e != nil || len(metas) != 1 || metas[0].Index != 1 {
		t.Fatalf("malformed image replaced durable snapshot: %+v %v", metas, e)
	}
}

func TestSnapshotInstallObserverCanReadNode(t *testing.T) {
	store := NewInMemoryStorage()
	fsm := NewStorageFSM(store)
	n, e := NewNode(newTestRaftNodeConfig(), NewInMemoryLogStore(), NewInMemorySnapshotStore(), fsm, newTestRaftLogger())
	if e != nil {
		t.Fatal(e)
	}
	n.currentTerm = 2
	observed := false
	fsm.SetRestoreObserver(func() {
		n.mu.RLock()
		defer n.mu.RUnlock()
		if n.lastApplied != 1 {
			t.Errorf("observer saw inconsistent boundary %d", n.lastApplied)
		}
		observed = true
	})
	payload, _ := json.Marshal(map[string][]byte{"k": []byte("value")})
	if !n.handleInstallSnapshot(&core.InstallSnapshotRequest{Term: 2, LeaderID: "leader", LastIncludedIndex: 1, LastIncludedTerm: 2, Data: payload, Done: true}).Success || !observed {
		t.Fatal("snapshot observer missing")
	}
}
