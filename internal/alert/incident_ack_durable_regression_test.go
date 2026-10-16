package alert

import (
	"errors"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// durableStorage records which save path was used and can fail it.
type durableStorage struct {
	*mockAlertStorage
	durableCalls int
	failDurable  bool
}

func (d *durableStorage) SaveIncidentDurable(incident *core.Incident) error {
	d.durableCalls++
	if d.failDurable {
		return errors.New("no leader")
	}
	return d.mockAlertStorage.SaveIncident(incident)
}

func newDurableManager(t *testing.T, fail bool) (*Manager, *durableStorage) {
	t.Helper()
	store := &durableStorage{
		mockAlertStorage: &mockAlertStorage{
			channels:  map[string]*core.AlertChannel{},
			rules:     map[string]*core.AlertRule{},
			incidents: map[string]*core.Incident{},
		},
		failDurable: fail,
	}
	m := NewManager(store, newTestLogger())
	m.incidents["inc-1"] = &core.Incident{ID: "inc-1", WorkspaceID: "default", Status: core.IncidentOpen}
	return m, store
}

// Acknowledge/resolve used to log a failed save and report success, and in
// cluster mode the save only queued the write — so an ack could be lost
// after the API returned 200.
func TestIncidentAckUsesDurableSaveAndReportsFailure(t *testing.T) {
	m, store := newDurableManager(t, true)
	if err := m.AcknowledgeIncident("inc-1", "admin", "default"); err == nil {
		t.Fatal("acknowledge succeeded although the durable save failed")
	}
	if got := m.incidents["inc-1"].Status; got != core.IncidentOpen {
		t.Fatalf("status after failed ack = %s, want open", got)
	}
	if store.durableCalls != 1 {
		t.Fatalf("durable save calls = %d, want 1", store.durableCalls)
	}

	m, store = newDurableManager(t, false)
	if err := m.AcknowledgeIncident("inc-1", "admin", "default"); err != nil {
		t.Fatal(err)
	}
	if err := m.ResolveIncident("inc-1", "admin", "default"); err != nil {
		t.Fatal(err)
	}
	inc := m.incidents["inc-1"]
	if inc.Status != core.IncidentResolved || inc.AckedBy != "admin" || inc.ResolvedBy != "admin" {
		t.Fatalf("incident = %+v", inc)
	}
	if store.incidents["inc-1"].Status != core.IncidentResolved {
		t.Fatal("resolution not stored")
	}
	if store.durableCalls != 2 {
		t.Fatalf("durable save calls = %d, want 2", store.durableCalls)
	}

	if err := m.AcknowledgeIncident("inc-1", "admin", "other"); err == nil {
		t.Fatal("cross-workspace acknowledge allowed")
	}
}
