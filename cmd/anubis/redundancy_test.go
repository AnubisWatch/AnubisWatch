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
			if got := quorumStatus(tt.own, tt.others, quorumPolicy{}); got != tt.want {
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
	j := &core.Judgment{Status: core.SoulDead, Timestamp: now, Duration: 250 * time.Millisecond, Region: "fr"}

	if newTestConsolidator(b, now).Consolidate(soul, j) {
		t.Fatal("a secondary published a judgment")
	}
	got, ok := b.published[voteKey("", soul.ID, "b")]
	if !ok || got.Status != core.SoulDead || got.JackalID != "b" {
		t.Fatalf("published votes = %+v", b.published)
	}
	if got.Latency != 250*time.Millisecond || got.Region != "fr" {
		t.Fatalf("vote lost its latency/region: %+v", got)
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
		if want := "connection refused (1 of 2 probers report dead: a)"; j.Message != want {
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

// A secondary's vote carries its probe latency and region, and a vote written
// by an older build (without those fields) still decodes.
func TestVoteCarriesLatencyAndRegionAndReadsOldRecords(t *testing.T) {
	at := time.Unix(1_700_000_000, 0).UTC()
	data, err := json.Marshal(vote{JackalID: "jackal-b", Status: core.SoulDead, Timestamp: at, Latency: 120 * time.Millisecond, Region: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	var got vote
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Latency != 120*time.Millisecond || got.Region != "fr" {
		t.Fatalf("latency/region lost in round trip: %+v", got)
	}

	old := []byte(`{"jackal_id":"jackal-c","status":"alive","timestamp":"2023-11-14T22:13:20Z"}`)
	var legacy vote
	if err := json.Unmarshal(old, &legacy); err != nil {
		t.Fatalf("old vote record no longer decodes: %v", err)
	}
	if legacy.JackalID != "jackal-c" || legacy.Status != core.SoulAlive || legacy.Latency != 0 || legacy.Region != "" {
		t.Fatalf("old vote decoded wrongly: %+v", legacy)
	}
}

// The default policy is the strict majority it always was; any, all and
// min_down move only the line between degraded and dead.
func TestQuorumPolicyThresholds(t *testing.T) {
	alive, dead, deg := core.SoulAlive, core.SoulDead, core.SoulDegraded
	five := func(downs int) (core.SoulStatus, []core.SoulStatus) {
		others := make([]core.SoulStatus, 4)
		for i := range others {
			others[i] = alive
			if i < downs-1 {
				others[i] = dead
			}
		}
		if downs == 0 {
			return alive, others
		}
		return dead, others
	}
	tests := []struct {
		name   string
		policy quorumPolicy
		downs  int // of 5 probers
		want   core.SoulStatus
	}{
		{"default: 2 of 5 is degraded", quorumPolicy{}, 2, deg},
		{"default: 3 of 5 is dead", quorumPolicy{}, 3, dead},
		{"explicit majority equals default", quorumPolicy{mode: core.QuorumMajority}, 2, deg},
		{"any: 1 of 5 is dead", quorumPolicy{mode: core.QuorumAny}, 1, dead},
		{"any: none is alive", quorumPolicy{mode: core.QuorumAny}, 0, alive},
		{"all: 4 of 5 is degraded", quorumPolicy{mode: core.QuorumAll}, 4, deg},
		{"all: 5 of 5 is dead", quorumPolicy{mode: core.QuorumAll}, 5, dead},
		{"min_down 2: 1 of 5 is degraded", quorumPolicy{minDown: 2}, 1, deg},
		{"min_down 2: 2 of 5 is dead", quorumPolicy{minDown: 2}, 2, dead},
		{"min_down beats the mode", quorumPolicy{mode: core.QuorumAll, minDown: 2}, 2, dead},
		{"min_down 4: 3 of 5 is degraded", quorumPolicy{minDown: 4}, 3, deg},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			own, others := five(tt.downs)
			if got := quorumStatus(own, others, tt.policy); got != tt.want {
				t.Fatalf("%d of 5 down under %+v = %s, want %s", tt.downs, tt.policy, got, tt.want)
			}
		})
	}

	// A threshold above the probers still alive never hides an outage: with
	// only two voters left, min_down 3 behaves like "both".
	if got := quorumStatus(dead, []core.SoulStatus{dead}, quorumPolicy{minDown: 3}); got != dead {
		t.Errorf("two silent peers: min_down 3 with both remaining voters dead = %s, want dead", got)
	}
	if got := quorumStatus(dead, nil, quorumPolicy{minDown: 3}); got != dead {
		t.Errorf("lone voter under min_down 3 = %s, want its own dead", got)
	}
}

func TestConsolidateNamesTheProbersThatSawItDown(t *testing.T) {
	now := time.Now()
	soul := redundantSoul()
	b := &fakeBoard{self: "tr-1", owners: []string{"tr-1", "fr-1", "us-1", "de-1", "jp-1"}}
	b.store(t, soul, "fr-1", core.SoulDead, now.Add(-time.Second))
	b.store(t, soul, "us-1", core.SoulAlive, now.Add(-time.Second))
	b.store(t, soul, "de-1", core.SoulAlive, now.Add(-time.Second))
	b.store(t, soul, "jp-1", core.SoulAlive, now.Add(-time.Second))

	// 2 of 5 under "at least 2": dead, and the message names both.
	c := newTestConsolidator(b, now)
	c.policy = quorumPolicy{minDown: 2}
	j := &core.Judgment{Status: core.SoulDead, Message: "timeout"}
	c.Consolidate(soul, j)
	// The primary is dead and so is fr-1: that is the status it already had,
	// so the message is untouched (it only explains a changed verdict).
	if j.Status != core.SoulDead || j.Message != "timeout" {
		t.Fatalf("min_down 2: status %s message %q", j.Status, j.Message)
	}

	// Under "at least 3" the same split is degraded, and the message says who.
	c.policy = quorumPolicy{minDown: 3}
	j = &core.Judgment{Status: core.SoulDead, Message: "timeout"}
	c.Consolidate(soul, j)
	if j.Status != core.SoulDegraded {
		t.Fatalf("min_down 3: status %s, want degraded", j.Status)
	}
	if want := "timeout (2 of 5 probers report dead: tr-1, fr-1)"; j.Message != want {
		t.Fatalf("message = %q, want %q", j.Message, want)
	}
}
