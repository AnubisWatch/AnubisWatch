package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Replicated writes reach a follower through ApplyFSMCommand, not the Save*
// helpers. updateIndexForSet only knew the top-level resource layout, so a
// replicated alert channel, rule, alert event or judgment index entry was
// stored but missing from the secondary index that lookups go through.
func TestReplicatedWritesUpdateSecondaryIndexes(t *testing.T) {
	db := newTestDB(t)

	apply := func(key string, value any) {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.ApplyFSMCommand(&core.FSMCommand{Op: core.FSMSet, Key: key, Value: data}); err != nil {
			t.Fatalf("apply %s: %v", key, err)
		}
	}

	apply("default/alerts/channels/ch1", &core.AlertChannel{ID: "ch1", WorkspaceID: "default", Name: "c", Type: core.ChannelWebHook})
	apply("default/alerts/rules/r1", &core.AlertRule{ID: "r1", WorkspaceID: "default", Name: "r"})
	apply("default/alerts/events/s1/123/ev1", &core.AlertEvent{ID: "ev1", SoulID: "s1", WorkspaceID: "default"})

	j := &core.Judgment{ID: "j1", SoulID: "s1", WorkspaceID: "default", Timestamp: time.Unix(0, 42).UTC()}
	primary := fmt.Sprintf("default/judgments/s1/%d", j.Timestamp.UnixNano())
	apply(primary, j)
	if err := db.ApplyFSMCommand(&core.FSMCommand{Op: core.FSMSet, Key: "default/judgment-idx/j1", Value: []byte(primary)}); err != nil {
		t.Fatal(err)
	}

	db.mu.RLock()
	channelWS, rule, event := db.channelIndex["ch1"], db.ruleIndex["r1"], db.alertEventIndex["ev1"]
	db.mu.RUnlock()
	if channelWS != "default" || rule != "default" || event != "default" {
		t.Fatalf("replicated alert records missing from the index: channel=%q rule=%q event=%q", channelWS, rule, event)
	}
	if got, err := db.GetJudgmentNoCtx("j1"); err != nil || got.ID != "j1" {
		t.Fatalf("a replicated judgment is unreachable by ID: %v, %v", got, err)
	}

	for _, key := range []string{"default/alerts/channels/ch1", "default/alerts/rules/r1", "default/alerts/events/s1/123/ev1", "default/judgment-idx/j1"} {
		if err := db.ApplyFSMCommand(&core.FSMCommand{Op: core.FSMDelete, Key: key}); err != nil {
			t.Fatal(err)
		}
	}
	db.mu.RLock()
	_, ch := db.channelIndex["ch1"]
	_, ru := db.ruleIndex["r1"]
	_, ev := db.alertEventIndex["ev1"]
	_, ji := db.judgmentIndex["j1"]
	db.mu.RUnlock()
	if ch || ru || ev || ji {
		t.Fatalf("replicated deletes left index entries: channel=%v rule=%v event=%v judgment=%v", ch, ru, ev, ji)
	}
	if _, err := db.GetJudgment(context.Background(), "default", "s1", j.Timestamp); err != nil {
		t.Fatalf("the judgment record itself should remain: %v", err)
	}
}
