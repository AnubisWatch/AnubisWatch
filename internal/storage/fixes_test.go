package storage

import (
	"bytes"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TestGetLog_DataRoundTrip verifies that the C8 fix works:
// []byte data survives StoreLog -> GetLog round-trip.
func TestGetLog_DataRoundTrip(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	store := NewCobaltDBLogStore(db)

	testData := []byte(`{"op":"SET","key":"test","value":"hello"}`)
	log := &core.RaftLogEntry{
		Index: 42,
		Term:  7,
		Data:  testData,
	}

	if err := store.StoreLog(log); err != nil {
		t.Fatalf("StoreLog failed: %v", err)
	}

	var retrieved core.RaftLogEntry
	if err := store.GetLog(42, &retrieved); err != nil {
		t.Fatalf("GetLog failed: %v", err)
	}

	if retrieved.Index != 42 {
		t.Errorf("Index: got %d, want 42", retrieved.Index)
	}
	if retrieved.Term != 7 {
		t.Errorf("Term: got %d, want 7", retrieved.Term)
	}
	if !bytes.Equal(retrieved.Data, testData) {
		t.Errorf("Data: got %q, want %q", retrieved.Data, testData)
	}
}

// TestGetLog_EmptyData verifies empty data doesn't crash.
func TestGetLog_EmptyData(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	store := NewCobaltDBLogStore(db)

	log := &core.RaftLogEntry{
		Index: 1,
		Term:  1,
		Data:  []byte{},
	}
	if err := store.StoreLog(log); err != nil {
		t.Fatalf("StoreLog failed: %v", err)
	}

	var retrieved core.RaftLogEntry
	if err := store.GetLog(1, &retrieved); err != nil {
		t.Fatalf("GetLog failed: %v", err)
	}

	if len(retrieved.Data) != 0 {
		t.Errorf("Expected empty data, got %q", retrieved.Data)
	}
}
