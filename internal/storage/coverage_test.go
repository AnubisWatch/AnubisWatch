package storage

import (
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TestRebuildSecondaryIndexes verifies the secondary index rebuild
// correctly maps all entities after a restart.
func TestRebuildSecondaryIndexes(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	// Save souls in different workspaces
	soul1 := &core.Soul{ID: "soul-1", WorkspaceID: "ws-a", Name: "Soul A"}
	soul2 := &core.Soul{ID: "soul-2", WorkspaceID: "ws-b", Name: "Soul B"}
	if err := db.SaveSoul(nil, soul1); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSoul(nil, soul2); err != nil {
		t.Fatal(err)
	}

	// Clear indexes to simulate post-restart state
	db.mu.Lock()
	db.soulIndex = make(map[string]string)
	db.judgmentIndex = make(map[string]string)
	db.channelIndex = make(map[string]string)
	db.ruleIndex = make(map[string]string)
	db.journeyIndex = make(map[string]string)
	db.mu.Unlock()

	// Rebuild
	if err := db.rebuildSecondaryIndexes(); err != nil {
		t.Fatalf("rebuildSecondaryIndexes failed: %v", err)
	}

	// Verify souls are indexed
	db.mu.RLock()
	ws1, ok1 := db.soulIndex["soul-1"]
	ws2, ok2 := db.soulIndex["soul-2"]
	db.mu.RUnlock()

	if !ok1 || ws1 != "ws-a" {
		t.Errorf("Expected soul-1 in ws-a, got %s (found=%v)", ws1, ok1)
	}
	if !ok2 || ws2 != "ws-b" {
		t.Errorf("Expected soul-2 in ws-b, got %s (found=%v)", ws2, ok2)
	}

	// Verify GetSoulNoCtx works after rebuild
	retrieved, err := db.GetSoulNoCtx("soul-1")
	if err != nil {
		t.Errorf("GetSoulNoCtx failed after rebuild: %v", err)
	}
	if retrieved.ID != "soul-1" {
		t.Errorf("Expected soul-1, got %s", retrieved.ID)
	}
}

// TestRecoverFromWAL_PreservesData verifies that WAL recovery correctly
// replays entries into the in-memory B+Tree.
func TestRecoverFromWAL_PreservesData(t *testing.T) {
	db := newTestDB(t)

	// Put some data
	db.Put("test-key-1", []byte("value1"))
	db.Put("test-key-2", []byte("value2"))
	db.Put("test-key-3", []byte("value3"))

	// Delete one
	db.Delete("test-key-2")

	// Close and reopen
	path := db.path
	db.Close()

	// Reopen — should recover from WAL
	db2, err := NewEngine(core.StorageConfig{Path: path}, newTestLogger())
	if err != nil {
		t.Fatalf("Failed to reopen: %v", err)
	}
	defer db2.Close()

	// Verify data survived
	v1, err := db2.Get("test-key-1")
	if err != nil {
		t.Errorf("Expected key-1 to survive recovery: %v", err)
	}
	if string(v1) != "value1" {
		t.Errorf("Expected value1, got %s", v1)
	}

	// key-2 should be deleted (nil value in simplified B+Tree)
	v2, err := db2.Get("test-key-2")
	if err != nil {
		t.Errorf("Get returned error for deleted key: %v", err)
	}
	if v2 != nil {
		t.Errorf("Expected nil value for deleted key-2, got %q", v2)
	}

	// key-3 should survive
	v3, err := db2.Get("test-key-3")
	if err != nil {
		t.Errorf("Expected key-3 to survive recovery: %v", err)
	}
	if string(v3) != "value3" {
		t.Errorf("Expected value3, got %s", v3)
	}
}

// TestEncryptDecryptRoundTrip verifies the encryption module
// correctly encrypts and decrypts data.
func TestEncryptDecryptRoundTrip(t *testing.T) {
	enc, err := newEncryptor("test-secret-key")
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("sensitive monitoring data")

	encrypted, err := enc.encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	// Verify encrypted differs from plaintext
	if string(encrypted) == string(plaintext) {
		t.Error("Encryption did not change the data")
	}

	// Verify isEncrypted detects it
	if !enc.isEncrypted(encrypted) {
		t.Error("isEncrypted should return true for encrypted data")
	}

	// Decrypt
	decrypted, err := enc.decrypt(encrypted)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("Expected %q, got %q", plaintext, decrypted)
	}
}

// TestEncryptDecrypt_LargeData verifies encryption works with larger payloads.
func TestEncryptDecrypt_LargeData(t *testing.T) {
	enc, err := newEncryptor("test-key-for-large")
	if err != nil {
		t.Fatal(err)
	}

	// Create 1KB of data
	large := make([]byte, 1024)
	for i := range large {
		large[i] = byte(i % 256)
	}

	encrypted, err := enc.encrypt(large)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	decrypted, err := enc.decrypt(encrypted)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if len(decrypted) != len(large) {
		t.Errorf("Length mismatch: expected %d, got %d", len(large), len(decrypted))
	}

	for i := range large {
		if decrypted[i] != large[i] {
			t.Errorf("Byte mismatch at %d", i)
			break
		}
	}
}
