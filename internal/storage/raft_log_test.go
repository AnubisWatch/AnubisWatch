package storage

import (
	"bytes"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestCobaltDBLogStore_StoreLog(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	store := NewCobaltDBLogStore(db)

	log := &core.RaftLogEntry{
		Index: 1,
		Term:  1,
		Data:  []byte("test log data"),
	}

	err := store.StoreLog(log)
	if err != nil {
		t.Fatalf("StoreLog failed: %v", err)
	}
}

func TestCobaltDBLogStore_StoreLogs(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	store := NewCobaltDBLogStore(db)

	logs := []core.RaftLogEntry{
		{Index: 1, Term: 1, Data: []byte("log 1")},
		{Index: 2, Term: 1, Data: []byte("log 2")},
		{Index: 3, Term: 1, Data: []byte("log 3")},
	}

	err := store.StoreLogs(logs)
	if err != nil {
		t.Fatalf("StoreLogs failed: %v", err)
	}
}

func TestCobaltDBLogStore_GetLog(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	store := NewCobaltDBLogStore(db)

	log := &core.RaftLogEntry{
		Index: 1,
		Term:  1,
		Data:  []byte("test log data"),
	}
	err := store.StoreLog(log)
	if err != nil {
		t.Fatalf("StoreLog failed: %v", err)
	}

	// Retrieve the log
	var retrieved core.RaftLogEntry
	err = store.GetLog(1, &retrieved)
	if err != nil {
		t.Fatalf("GetLog failed: %v", err)
	}

	if retrieved.Index != 1 {
		t.Errorf("Expected index 1, got %d", retrieved.Index)
	}
	if retrieved.Term != 1 {
		t.Errorf("Expected term 1, got %d", retrieved.Term)
	}
	// Verify Data field survives the round-trip (was previously a known bug)
	if !bytes.Equal(retrieved.Data, []byte("test log data")) {
		t.Errorf("Expected data 'test log data', got %q", retrieved.Data)
	}
}

func TestCobaltDBLogStore_GetLog_NonExistent(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	store := NewCobaltDBLogStore(db)

	var log core.RaftLogEntry
	err := store.GetLog(999, &log)
	if err == nil {
		t.Error("Expected error for non-existent log")
	}
}

func TestCobaltDBLogStore_FirstIndex(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	store := NewCobaltDBLogStore(db)

	// Empty log
	first, err := store.FirstIndex()
	if err != nil {
		t.Fatalf("FirstIndex failed: %v", err)
	}
	if first != 0 {
		t.Errorf("Expected first index 0 for empty log, got %d", first)
	}

	// Add logs
	logs := []core.RaftLogEntry{
		{Index: 5, Term: 1, Data: []byte("log 5")},
		{Index: 3, Term: 1, Data: []byte("log 3")},
		{Index: 7, Term: 1, Data: []byte("log 7")},
	}
	store.StoreLogs(logs)

	first, err = store.FirstIndex()
	if err != nil {
		t.Fatalf("FirstIndex failed: %v", err)
	}
	if first != 3 {
		t.Errorf("Expected first index 3, got %d", first)
	}
}

func TestCobaltDBLogStore_LastIndex(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	store := NewCobaltDBLogStore(db)

	// Empty log
	last, err := store.LastIndex()
	if err != nil {
		t.Fatalf("LastIndex failed: %v", err)
	}
	if last != 0 {
		t.Errorf("Expected last index 0 for empty log, got %d", last)
	}

	// Add logs
	logs := []core.RaftLogEntry{
		{Index: 5, Term: 1, Data: []byte("log 5")},
		{Index: 3, Term: 1, Data: []byte("log 3")},
		{Index: 7, Term: 1, Data: []byte("log 7")},
	}
	store.StoreLogs(logs)

	last, err = store.LastIndex()
	if err != nil {
		t.Fatalf("LastIndex failed: %v", err)
	}
	if last != 7 {
		t.Errorf("Expected last index 7, got %d", last)
	}
}

func TestCobaltDBLogStore_DeleteRange(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	store := NewCobaltDBLogStore(db)

	// Add logs
	logs := []core.RaftLogEntry{
		{Index: 1, Term: 1, Data: []byte("log 1")},
		{Index: 2, Term: 1, Data: []byte("log 2")},
		{Index: 3, Term: 1, Data: []byte("log 3")},
		{Index: 4, Term: 1, Data: []byte("log 4")},
		{Index: 5, Term: 1, Data: []byte("log 5")},
	}
	store.StoreLogs(logs)

	// Delete range 2-4
	err := store.DeleteRange(2, 4)
	if err != nil {
		t.Fatalf("DeleteRange failed: %v", err)
	}

	// Verify logs 1 and 5 remain
	var log1, log5 core.RaftLogEntry
	err = store.GetLog(1, &log1)
	if err != nil {
		t.Error("Expected log 1 to exist")
	}
	err = store.GetLog(5, &log5)
	if err != nil {
		t.Error("Expected log 5 to exist")
	}

	// Verify logs 2-4 are deleted
	var log2 core.RaftLogEntry
	err = store.GetLog(2, &log2)
	if err == nil {
		t.Error("Expected log 2 to be deleted")
	}
}

func TestCobaltDBSnapshotStore_Create(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	store := NewCobaltDBSnapshotStore(db)

	sink, err := store.Create(1, 100, 5, []byte("config"))
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if sink == nil {
		t.Fatal("Expected non-nil sink")
	}

	// Write some data
	data := []byte("snapshot data")
	_, err = sink.Write(data)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Close the sink
	err = sink.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestCobaltDBSnapshotStore_Cancel(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	store := NewCobaltDBSnapshotStore(db)

	sink, err := store.Create(1, 100, 5, []byte("config"))
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Cancel the snapshot
	err = sink.Cancel()
	if err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}

	// Try to write after cancel - should fail
	_, err = sink.Write([]byte("data"))
	if err == nil {
		t.Error("Expected error when writing to cancelled sink")
	}
}

func TestCobaltDBSnapshotSink_ID(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	store := NewCobaltDBSnapshotStore(db)

	sink, _ := store.Create(1, 100, 5, []byte("config"))
	id := sink.ID()
	if id == "" {
		t.Error("Expected non-empty snapshot ID")
	}
}

func TestCobaltDBSnapshotSink_CloseTwice(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	store := NewCobaltDBSnapshotStore(db)

	sink, _ := store.Create(1, 100, 5, []byte("config"))

	// First close
	err := sink.Close()
	if err != nil {
		t.Fatalf("First Close failed: %v", err)
	}

	// Second close should be idempotent
	err = sink.Close()
	if err != nil {
		t.Fatalf("Second Close failed: %v", err)
	}
}

func TestCobaltDBSnapshotStore_Open(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	store := NewCobaltDBSnapshotStore(db)

	// Create and save a snapshot
	sink, _ := store.Create(1, 100, 5, []byte("config"))
	data := []byte("snapshot content")
	sink.Write(data)
	sink.Close()

	// Open the snapshot
	source, err := store.Open("5-100")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if source == nil {
		t.Fatal("Expected non-nil source")
	}

	// Read the snapshot
	buf := make([]byte, 100)
	n, err := source.Read(buf)
	// EOF is expected when reaching end
	_ = err // Ignore error - could be EOF or partial read
	if n != len(data) {
		t.Errorf("Expected to read %d bytes, got %d", len(data), n)
	}

	source.Close()
}

func TestCobaltDBSnapshotSource_Read(t *testing.T) {
	source := &cobaltDBSnapshotSource{
		data: []byte("test snapshot data"),
		pos:  0,
	}

	buf := make([]byte, 4)
	n, err := source.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if n != 4 {
		t.Errorf("Expected to read 4 bytes, got %d", n)
	}
	if string(buf) != "test" {
		t.Errorf("Expected 'test', got %s", string(buf))
	}
}

func TestCobaltDBSnapshotSource_Read_EOF(t *testing.T) {
	source := &cobaltDBSnapshotSource{
		data: []byte("test"),
		pos:  4, // At end
	}

	buf := make([]byte, 4)
	n, err := source.Read(buf)
	if err == nil {
		t.Error("Expected EOF error")
	}
	if n != 0 {
		t.Errorf("Expected to read 0 bytes, got %d", n)
	}
}

func TestCobaltDBSnapshotSource_Close(t *testing.T) {
	source := &cobaltDBSnapshotSource{
		data: []byte("test"),
		pos:  0,
	}

	err := source.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestCobaltDBStableStore_SetUint64_GetUint64(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	store := NewCobaltDBStableStore(db)

	err := store.SetUint64("test-key", 12345)
	if err != nil {
		t.Fatalf("SetUint64 failed: %v", err)
	}

	val, err := store.GetUint64("test-key")
	if err != nil {
		t.Fatalf("GetUint64 failed: %v", err)
	}
	if val != 12345 {
		t.Errorf("Expected 12345, got %d", val)
	}
}

func TestCobaltDBStableStore_GetUint64_NonExistent(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	store := NewCobaltDBStableStore(db)

	_, err := store.GetUint64("non-existent")
	if err == nil {
		t.Error("Expected error for non-existent key")
	}
}

func TestCobaltDBStableStore_Set_Get(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	store := NewCobaltDBStableStore(db)

	data := []byte("test stable data")
	err := store.Set("test-key", data)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	retrieved, err := store.Get("test-key")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !bytes.Equal(retrieved, data) {
		t.Errorf("Expected %s, got %s", string(data), string(retrieved))
	}
}

func TestCobaltDBStableStore_Get_NonExistent(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	store := NewCobaltDBStableStore(db)

	_, err := store.Get("non-existent")
	if err == nil {
		t.Error("Expected error for non-existent key")
	}
}

func TestCobaltDBSnapshotStore_List(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	store := NewCobaltDBSnapshotStore(db)

	// Create and save a snapshot
	sink, _ := store.Create(1, 100, 5, []byte("config"))
	sink.Write([]byte("data"))
	sink.Close()

	// List snapshots
	metas, err := store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(metas) != 1 {
		t.Errorf("Expected 1 snapshot, got %d", len(metas))
	}
}

func TestCobaltDBSnapshotStore_List_Empty(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	store := NewCobaltDBSnapshotStore(db)

	metas, err := store.List()
	if err == nil {
		// May return error for non-existent key
		t.Logf("List returned error for empty: %v", err)
	}
	if len(metas) > 0 {
		t.Errorf("Expected empty list, got %d items", len(metas))
	}
}

func TestCobaltDBSnapshotStore_Open_NonExistent(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	store := NewCobaltDBSnapshotStore(db)

	_, err := store.Open("non-existent")
	if err == nil {
		t.Error("Expected error for non-existent snapshot")
	}
}

func TestCobaltDBLogStore_EmptyLogs(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	store := NewCobaltDBLogStore(db)

	err := store.StoreLogs([]core.RaftLogEntry{})
	if err != nil {
		t.Fatalf("StoreLogs with empty list failed: %v", err)
	}
}
