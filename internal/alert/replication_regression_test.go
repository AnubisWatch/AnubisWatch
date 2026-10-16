package alert

import (
	"errors"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// In a cluster, channels, rules and incidents written on one node replicate to
// the others' storage; the Load*/Unload*/Resync methods bring each node's
// in-memory alert state in line without writing the record a second time.

func newReplicationTestManager() (*Manager, *mockAlertStorage) {
	storage := &mockAlertStorage{
		channels:  map[string]*core.AlertChannel{},
		rules:     map[string]*core.AlertRule{},
		incidents: map[string]*core.Incident{},
	}
	return NewManager(storage, newTestLogger()), storage
}

func TestReplicatedAlertStateLoadsWithoutPersisting(t *testing.T) {
	m, storage := newReplicationTestManager()

	m.LoadChannel(&core.AlertChannel{ID: "ch", Name: "c", Type: core.ChannelWebHook})
	m.LoadRule(&core.AlertRule{ID: "r", Name: "r", Enabled: true})
	var hooked []string
	m.SetIncidentHook(func(inc *core.Incident) { hooked = append(hooked, inc.ID) })
	m.LoadIncident(&core.Incident{ID: "inc", SoulID: "s", Status: core.IncidentOpen})

	if _, err := m.GetChannel("ch"); err != nil {
		t.Fatalf("loaded channel not visible: %v", err)
	}
	if _, err := m.GetRule("r"); err != nil {
		t.Fatalf("loaded rule not visible: %v", err)
	}
	if got := m.ListIncidents(); len(got) != 1 || got[0].ID != "inc" {
		t.Fatalf("loaded incident not listed: %v", got)
	}
	if len(hooked) != 1 {
		t.Fatalf("a replicated incident must reach this node's live clients; hook ran %d times", len(hooked))
	}
	if len(storage.channels)+len(storage.rules)+len(storage.incidents) != 0 {
		t.Fatal("loading replicated state wrote it to storage again")
	}

	m.UnloadChannel("ch")
	m.UnloadRule("r")
	if _, err := m.GetChannel("ch"); err == nil {
		t.Fatal("unloaded channel still visible")
	}
	if _, err := m.GetRule("r"); err == nil {
		t.Fatal("unloaded rule still visible")
	}
}

func TestResyncReplacesChannelsAndRulesFromStorage(t *testing.T) {
	m, storage := newReplicationTestManager()
	m.LoadChannel(&core.AlertChannel{ID: "stale", Type: core.ChannelWebHook})
	storage.channels["fresh"] = &core.AlertChannel{ID: "fresh", Type: core.ChannelWebHook}
	storage.rules["rule"] = &core.AlertRule{ID: "rule"}
	storage.incidents["open"] = &core.Incident{ID: "open", Status: core.IncidentOpen}

	m.Resync()

	if _, err := m.GetChannel("stale"); err == nil {
		t.Fatal("a channel no longer in storage survived the resync")
	}
	if _, err := m.GetChannel("fresh"); err != nil {
		t.Fatalf("resync did not load the stored channel: %v", err)
	}
	if _, err := m.GetRule("rule"); err != nil {
		t.Fatalf("resync did not load the stored rule: %v", err)
	}
	if len(m.ListActiveIncidents()) != 1 {
		t.Fatal("resync did not load the active incident")
	}
}

// failingSaveStorage fails channel and rule saves, as a clustered store does
// when the write cannot be committed.
type failingSaveStorage struct {
	*mockAlertStorage
}

func (f *failingSaveStorage) SaveChannel(*core.AlertChannel) error { return errors.New("no leader") }
func (f *failingSaveStorage) SaveRule(*core.AlertRule) error       { return errors.New("no leader") }

func TestRegisterFailsAndRollsBackWhenTheSaveFails(t *testing.T) {
	base, _ := newReplicationTestManager()
	m := NewManager(&failingSaveStorage{mockAlertStorage: base.storage.(*mockAlertStorage)}, newTestLogger())

	// An existing channel is kept as it was when its update cannot be saved.
	m.LoadChannel(&core.AlertChannel{ID: "kept", Name: "before", Type: core.ChannelWebHook, Config: map[string]any{"url": "https://1.1.1.1/hook"}})
	updated := &core.AlertChannel{ID: "kept", Name: "after", Type: core.ChannelWebHook, Config: map[string]any{"url": "https://1.1.1.1/hook"}}
	if err := m.RegisterChannel(updated); err == nil || !strings.Contains(err.Error(), "save channel") {
		t.Fatalf("RegisterChannel must report the failed save, got %v", err)
	}
	if ch, _ := m.GetChannel("kept"); ch == nil || ch.Name != "before" {
		t.Fatalf("a failed update replaced the channel in memory: %+v", ch)
	}

	if err := m.RegisterRule(&core.AlertRule{ID: "new", Name: "n"}); err == nil {
		t.Fatal("RegisterRule reported success for a rule it could not save")
	}
	if _, err := m.GetRule("new"); err == nil {
		t.Fatal("a rule that was never saved fires on this node alone")
	}
}
