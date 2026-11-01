package main

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestQuorumStatus(t *testing.T) {
	alive, dead, deg := core.SoulAlive, core.SoulDead, core.SoulDegraded
	tests := []struct {
		name   string
		own    core.SoulStatus
		others []core.SoulStatus
		want   core.SoulStatus
	}{
		{"alone alive", alive, nil, alive},
		{"alone dead", dead, nil, dead},
		{"two agree alive", alive, []core.SoulStatus{alive}, alive},
		{"two agree dead", dead, []core.SoulStatus{dead}, dead},
		{"two split: primary dead", dead, []core.SoulStatus{alive}, deg},
		{"two split: secondary dead", alive, []core.SoulStatus{dead}, deg},
		{"three, majority dead", dead, []core.SoulStatus{dead, alive}, dead},
		{"three, minority dead", alive, []core.SoulStatus{dead, alive}, deg},
		{"three, all dead", dead, []core.SoulStatus{dead, dead}, dead},
		{"degraded probe, nobody dead", alive, []core.SoulStatus{deg}, deg},
		{"unknown stays unknown", core.SoulUnknown, []core.SoulStatus{alive}, core.SoulUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := quorumStatus(tt.own, tt.others); got != tt.want {
				t.Fatalf("quorumStatus(%s, %v) = %s, want %s", tt.own, tt.others, got, tt.want)
			}
		})
	}
}

type fakeBoard struct {
	self      string
	owners    []string
	published map[string]vote
	stored    map[string][]byte
	failRead  bool
}

func (b *fakeBoard) ownersOf(string) []string { return b.owners }
func (b *fakeBoard) selfID() string           { return b.self }
func (b *fakeBoard) publishVote(key string, v vote) error {
	if b.published == nil {
		b.published = map[string]vote{}
	}
	b.published[key] = v
	return nil
}
func (b *fakeBoard) readVote(key string) ([]byte, error) {
	if b.failRead {
		return nil, errors.New("read failed")
	}
	if data, ok := b.stored[key]; ok {
		return data, nil
	}
	return nil, errors.New("not found")
}

func (b *fakeBoard) store(t *testing.T, soul *core.Soul, member string, status core.SoulStatus, at time.Time) {
	t.Helper()
	data, err := json.Marshal(vote{JackalID: member, Status: status, Timestamp: at})
	if err != nil {
		t.Fatal(err)
	}
	if b.stored == nil {
		b.stored = map[string][]byte{}
	}
	b.stored[voteKey(soul.WorkspaceID, soul.ID, member)] = data
}

func newTestConsolidator(b *fakeBoard, now time.Time) *quorumConsolidator {
	return &quorumConsolidator{board: b, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), now: func() time.Time { return now }}
}

func redundantSoul() *core.Soul {
	return &core.Soul{ID: "soul-1", Weight: core.Duration{Duration: 10 * time.Second}}
}

func TestConsolidateSecondaryOnlyVotes(t *testing.T) {
	now := time.Now()
	b := &fakeBoard{self: "b", owners: []string{"a", "b"}}
	soul := redundantSoul()
	j := &core.Judgment{Status: core.SoulDead, Timestamp: now}

	if newTestConsolidator(b, now).Consolidate(soul, j) {
		t.Fatal("a secondary published a judgment")
	}
	got, ok := b.published[voteKey("", soul.ID, "b")]
	if !ok || got.Status != core.SoulDead || got.JackalID != "b" {
		t.Fatalf("published votes = %+v", b.published)
	}
	if j.Status != core.SoulDead {
		t.Fatal("a vote's judgment was rewritten")
	}
}

func TestConsolidatePrimaryUsesFreshVotes(t *testing.T) {
	now := time.Now()
	soul := redundantSoul()

	t.Run("both dead is dead", func(t *testing.T) {
		b := &fakeBoard{self: "a", owners: []string{"a", "b"}}
		b.store(t, soul, "b", core.SoulDead, now.Add(-5*time.Second))
		j := &core.Judgment{Status: core.SoulDead}
		if !newTestConsolidator(b, now).Consolidate(soul, j) || j.Status != core.SoulDead {
			t.Fatalf("status = %s, want dead", j.Status)
		}
	})
	t.Run("split is degraded and says so", func(t *testing.T) {
		b := &fakeBoard{self: "a", owners: []string{"a", "b"}}
		b.store(t, soul, "b", core.SoulAlive, now.Add(-5*time.Second))
		j := &core.Judgment{Status: core.SoulDead, Message: "connection refused"}
		if !newTestConsolidator(b, now).Consolidate(soul, j) || j.Status != core.SoulDegraded {
			t.Fatalf("status = %s, want degraded", j.Status)
		}
		if want := "connection refused (1 of 2 probers report dead)"; j.Message != want {
			t.Fatalf("message = %q, want %q", j.Message, want)
		}
	})
	t.Run("a stale vote is ignored", func(t *testing.T) {
		b := &fakeBoard{self: "a", owners: []string{"a", "b"}}
		b.store(t, soul, "b", core.SoulAlive, now.Add(-time.Minute)) // older than 2 intervals
		j := &core.Judgment{Status: core.SoulDead}
		newTestConsolidator(b, now).Consolidate(soul, j)
		if j.Status != core.SoulDead {
			t.Fatalf("status = %s: a silent prober outvoted the primary", j.Status)
		}
	})
	t.Run("a missing vote is ignored", func(t *testing.T) {
		b := &fakeBoard{self: "a", owners: []string{"a", "b", "c"}, failRead: true}
		j := &core.Judgment{Status: core.SoulDead}
		newTestConsolidator(b, now).Consolidate(soul, j)
		if j.Status != core.SoulDead {
			t.Fatalf("status = %s, want the primary's own dead", j.Status)
		}
	})
	t.Run("only members of the set vote", func(t *testing.T) {
		b := &fakeBoard{self: "a", owners: []string{"a", "b"}}
		b.store(t, soul, "x", core.SoulAlive, now) // not an owner
		j := &core.Judgment{Status: core.SoulDead}
		newTestConsolidator(b, now).Consolidate(soul, j)
		if j.Status != core.SoulDead {
			t.Fatalf("status = %s: a non-owner's vote counted", j.Status)
		}
	})
}

func TestConsolidateSingleOwnerIsUntouched(t *testing.T) {
	b := &fakeBoard{self: "a", owners: []string{"a"}}
	j := &core.Judgment{Status: core.SoulDead}
	if !newTestConsolidator(b, time.Now()).Consolidate(redundantSoul(), j) || j.Status != core.SoulDead || len(b.published) != 0 {
		t.Fatal("a single-owner soul was consolidated")
	}
}
