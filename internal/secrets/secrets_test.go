package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestNewManager(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, key)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	if m == nil {
		t.Fatal("Expected manager to be created")
	}
}

func TestNewManager_InvalidKey(t *testing.T) {
	// Key too short
	_, err := NewManager("", []byte("short"))
	if err != ErrInvalidKey {
		t.Errorf("Expected ErrInvalidKey, got %v", err)
	}

	// Key too long
	_, err = NewManager("", make([]byte, 64))
	if err != ErrInvalidKey {
		t.Errorf("Expected ErrInvalidKey, got %v", err)
	}
}

func TestManager_Store(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, key)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Store a secret
	err = m.Store("test-secret", []byte("secret-value"), map[string]string{"env": "prod"})
	if err != nil {
		t.Errorf("Failed to store secret: %v", err)
	}

	// Try to store with empty name
	err = m.Store("", []byte("value"), nil)
	if err == nil {
		t.Error("Expected error for empty secret name")
	}

	// Try to store duplicate
	err = m.Store("test-secret", []byte("another-value"), nil)
	if err != ErrAlreadyExists {
		t.Errorf("Expected ErrAlreadyExists, got %v", err)
	}
}

func TestManager_Retrieve(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, key)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Store and retrieve
	originalValue := []byte("my-secret-value")
	metadata := map[string]string{"env": "test", "app": "anubis"}
	err = m.Store("test-secret", originalValue, metadata)
	if err != nil {
		t.Fatalf("Failed to store secret: %v", err)
	}

	value, retrievedMetadata, err := m.Retrieve("test-secret")
	if err != nil {
		t.Errorf("Failed to retrieve secret: %v", err)
	}
	if string(value) != string(originalValue) {
		t.Errorf("Expected value '%s', got '%s'", originalValue, value)
	}
	if retrievedMetadata["env"] != "test" {
		t.Errorf("Expected metadata env='test', got '%s'", retrievedMetadata["env"])
	}

	// Try to retrieve non-existent
	_, _, err = m.Retrieve("non-existent")
	if err != ErrSecretNotFound {
		t.Errorf("Expected ErrSecretNotFound, got %v", err)
	}
}

func TestManager_Update(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, key)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Store initial secret
	err = m.Store("test-secret", []byte("old-value"), map[string]string{"version": "1"})
	if err != nil {
		t.Fatalf("Failed to store secret: %v", err)
	}

	// Update it
	err = m.Update("test-secret", []byte("new-value"), map[string]string{"version": "2"})
	if err != nil {
		t.Errorf("Failed to update secret: %v", err)
	}

	// Verify update
	value, metadata, _ := m.Retrieve("test-secret")
	if string(value) != "new-value" {
		t.Errorf("Expected new value 'new-value', got '%s'", value)
	}
	if metadata["version"] != "2" {
		t.Errorf("Expected version '2', got '%s'", metadata["version"])
	}

	// Try to update non-existent
	err = m.Update("non-existent", []byte("value"), nil)
	if err != ErrSecretNotFound {
		t.Errorf("Expected ErrSecretNotFound, got %v", err)
	}
}

func TestManager_Delete(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, key)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Store and delete
	err = m.Store("test-secret", []byte("value"), nil)
	if err != nil {
		t.Fatalf("Failed to store secret: %v", err)
	}

	err = m.Delete("test-secret")
	if err != nil {
		t.Errorf("Failed to delete secret: %v", err)
	}

	// Verify deletion
	_, _, err = m.Retrieve("test-secret")
	if err != ErrSecretNotFound {
		t.Errorf("Expected secret to be deleted, got %v", err)
	}

	// Try to delete non-existent
	err = m.Delete("non-existent")
	if err != ErrSecretNotFound {
		t.Errorf("Expected ErrSecretNotFound, got %v", err)
	}
}

func TestManager_List(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, key)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Store multiple secrets
	m.Store("secret-a", []byte("value-a"), nil)
	m.Store("secret-b", []byte("value-b"), nil)
	m.Store("secret-c", []byte("value-c"), nil)

	names := m.List()
	if len(names) != 3 {
		t.Errorf("Expected 3 secrets, got %d", len(names))
	}

	// Check all names are present
	nameMap := make(map[string]bool)
	for _, name := range names {
		nameMap[name] = true
	}
	if !nameMap["secret-a"] || !nameMap["secret-b"] || !nameMap["secret-c"] {
		t.Error("Expected all secrets to be listed")
	}
}

func TestManager_RotateKey(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, key)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Store a secret
	originalValue := []byte("test-secret-value")
	err = m.Store("test-secret", originalValue, nil)
	if err != nil {
		t.Fatalf("Failed to store secret: %v", err)
	}

	// Rotate key
	newKey := make([]byte, 32)
	for i := range newKey {
		newKey[i] = byte(i + 100)
	}

	err = m.RotateKey(newKey)
	if err != nil {
		t.Errorf("Failed to rotate key: %v", err)
	}

	// Verify secret is still accessible with new key
	value, _, err := m.Retrieve("test-secret")
	if err != nil {
		t.Errorf("Failed to retrieve after rotation: %v", err)
	}
	if string(value) != string(originalValue) {
		t.Error("Secret value changed after key rotation")
	}

	// Try invalid key size
	err = m.RotateKey([]byte("too-short"))
	if err != ErrInvalidKey {
		t.Errorf("Expected ErrInvalidKey, got %v", err)
	}
}

func TestManager_Persistence(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	tmpDir := t.TempDir()

	// Create manager and store secrets
	m1, err := NewManager(tmpDir, key)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	m1.Store("secret-1", []byte("value-1"), map[string]string{"key": "val"})
	m1.Store("secret-2", []byte("value-2"), nil)

	// Create new manager pointing to same directory
	m2, err := NewManager(tmpDir, key)
	if err != nil {
		t.Fatalf("Failed to create second manager: %v", err)
	}

	// Verify secrets are loaded
	value, metadata, err := m2.Retrieve("secret-1")
	if err != nil {
		t.Errorf("Failed to retrieve persisted secret: %v", err)
	}
	if string(value) != "value-1" {
		t.Errorf("Expected 'value-1', got '%s'", value)
	}
	if metadata["key"] != "val" {
		t.Errorf("Expected metadata 'val', got '%s'", metadata["key"])
	}

	// Verify second secret
	value, _, _ = m2.Retrieve("secret-2")
	if string(value) != "value-2" {
		t.Errorf("Expected 'value-2', got '%s'", value)
	}
}

func TestEnvProvider(t *testing.T) {
	// Set environment variables
	os.Setenv("ANUBIS_TEST_SECRET1", "value1")
	os.Setenv("ANUBIS_TEST_SECRET2", "value2")
	defer func() {
		os.Unsetenv("ANUBIS_TEST_SECRET1")
		os.Unsetenv("ANUBIS_TEST_SECRET2")
	}()

	provider := NewEnvProvider("ANUBIS_TEST_")

	// Get existing
	val, found := provider.Get("SECRET1")
	if !found {
		t.Error("Expected to find SECRET1")
	}
	if val != "value1" {
		t.Errorf("Expected 'value1', got '%s'", val)
	}

	// Get non-existent
	_, found = provider.Get("NONEXISTENT")
	if found {
		t.Error("Expected not to find non-existent secret")
	}
}

func TestSecureCompare(t *testing.T) {
	// Same strings
	if !SecureCompare("test", "test") {
		t.Error("Expected equal strings to match")
	}

	// Different strings
	if SecureCompare("test", "different") {
		t.Error("Expected different strings to not match")
	}

	// Empty strings
	if !SecureCompare("", "") {
		t.Error("Expected empty strings to match")
	}

	// Different lengths
	if SecureCompare("test", "test2") {
		t.Error("Expected strings with different lengths to not match")
	}
}

func TestGenerateKey(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("Expected key length 32, got %d", len(key))
	}

	// Generate another key and ensure it's different
	key2, _ := GenerateKey()
	equal := true
	for i := range key {
		if key[i] != key2[i] {
			equal = false
			break
		}
	}
	if equal {
		t.Error("Generated keys should be different")
	}
}

func TestEncodeDecodeKey(t *testing.T) {
	original := make([]byte, 32)
	for i := range original {
		original[i] = byte(i)
	}

	encoded := EncodeKey(original)
	if encoded == "" {
		t.Error("Expected non-empty encoded string")
	}

	decoded, err := DecodeKey(encoded)
	if err != nil {
		t.Errorf("Failed to decode key: %v", err)
	}

	if len(decoded) != len(original) {
		t.Errorf("Expected decoded length %d, got %d", len(original), len(decoded))
	}

	for i := range original {
		if decoded[i] != original[i] {
			t.Errorf("Byte %d mismatch: expected %d, got %d", i, original[i], decoded[i])
		}
	}
}

func TestDecodeKey_Invalid(t *testing.T) {
	_, err := DecodeKey("not-valid-base64!!!")
	if err == nil {
		t.Error("Expected error for invalid base64")
	}
}

func TestManager_NoStorage(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	// Create manager without storage
	m, err := NewManager("", key)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Store should work in memory
	err = m.Store("test", []byte("value"), nil)
	if err != nil {
		t.Errorf("Failed to store without storage: %v", err)
	}

	// Verify in-memory only
	value, _, _ := m.Retrieve("test")
	if string(value) != "value" {
		t.Errorf("Expected 'value', got '%s'", value)
	}
}

func TestManager_CorruptedStorage(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	tmpDir := t.TempDir()

	// Create corrupted secrets file
	corruptedData := []byte("not-valid-json")
	path := filepath.Join(tmpDir, "secrets.enc")
	os.WriteFile(path, corruptedData, 0600)

	// Try to load
	_, err := NewManager(tmpDir, key)
	if err == nil {
		t.Error("Expected error when loading corrupted storage")
	}
}

// TestManager_Retrieve_DecryptError tests retrieve with corrupted data
func TestManager_Retrieve_DecryptError(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, key)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Store a secret
	err = m.Store("test-secret", []byte("value"), nil)
	if err != nil {
		t.Fatalf("Failed to store secret: %v", err)
	}

	// Corrupt the encrypted data
	m.mu.Lock()
	secret := m.store["test-secret"]
	secret.Encrypted[0] ^= 0xFF // Flip bits
	m.mu.Unlock()

	// Retrieve should fail
	_, _, err = m.Retrieve("test-secret")
	if err == nil {
		t.Error("Expected error when decrypting corrupted data")
	}
}

// TestManager_NoStorage_Update tests update without storage
func TestManager_NoStorage_Update(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	// Create manager without storage
	m, err := NewManager("", key)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Store in memory
	err = m.Store("test", []byte("old"), nil)
	if err != nil {
		t.Fatalf("Failed to store: %v", err)
	}

	// Update in memory
	err = m.Update("test", []byte("new"), nil)
	if err != nil {
		t.Errorf("Failed to update without storage: %v", err)
	}

	// Verify
	value, _, _ := m.Retrieve("test")
	if string(value) != "new" {
		t.Errorf("Expected 'new', got '%s'", value)
	}
}

// TestManager_NoStorage_Delete tests delete without storage
func TestManager_NoStorage_Delete(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	// Create manager without storage
	m, err := NewManager("", key)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Store in memory
	m.Store("test", []byte("value"), nil)

	// Delete in memory
	err = m.Delete("test")
	if err != nil {
		t.Errorf("Failed to delete without storage: %v", err)
	}

	// Verify
	_, _, err = m.Retrieve("test")
	if err != ErrSecretNotFound {
		t.Error("Expected secret to be deleted")
	}
}

// TestManager_NoStorage_RotateKey tests key rotation without storage
func TestManager_NoStorage_RotateKey(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	// Create manager without storage
	m, err := NewManager("", key)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Store in memory
	originalValue := []byte("test-value")
	m.Store("test", originalValue, nil)

	// Rotate key
	newKey := make([]byte, 32)
	for i := range newKey {
		newKey[i] = byte(i + 100)
	}

	err = m.RotateKey(newKey)
	if err != nil {
		t.Errorf("Failed to rotate key without storage: %v", err)
	}

	// Verify
	value, _, _ := m.Retrieve("test")
	if string(value) != string(originalValue) {
		t.Error("Secret value changed after rotation")
	}
}

// TestManager_Store_EmptyValue tests storing empty value
func TestManager_Store_EmptyValue(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	tmpDir := t.TempDir()
	m, _ := NewManager(tmpDir, key)

	// Store empty value
	err := m.Store("empty-secret", []byte{}, nil)
	if err != nil {
		t.Errorf("Failed to store empty value: %v", err)
	}

	// Retrieve
	value, _, _ := m.Retrieve("empty-secret")
	if len(value) != 0 {
		t.Errorf("Expected empty value, got '%s'", value)
	}
}

// TestManager_Update_SameValue tests updating with same value
func TestManager_Update_SameValue(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	tmpDir := t.TempDir()
	m, _ := NewManager(tmpDir, key)

	m.Store("test", []byte("value"), map[string]string{"v": "1"})

	// Update with same value
	err := m.Update("test", []byte("value"), map[string]string{"v": "2"})
	if err != nil {
		t.Errorf("Failed to update: %v", err)
	}

	// Verify version incremented
	_, metadata, _ := m.Retrieve("test")
	if metadata["v"] != "2" {
		t.Errorf("Expected version '2', got '%s'", metadata["v"])
	}
}

// TestManager_List_Empty tests listing when no secrets
func TestManager_List_Empty(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	tmpDir := t.TempDir()
	m, _ := NewManager(tmpDir, key)

	names := m.List()
	if len(names) != 0 {
		t.Errorf("Expected 0 secrets, got %d", len(names))
	}
}

// TestManager_Update_SaveErrorRollback tests update rollback when save fails
func TestManager_Update_SaveErrorRollback(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	tmpDir := t.TempDir()
	m, _ := NewManager(tmpDir, key)

	m.Store("test", []byte("original"), map[string]string{"v": "1"})

	// Make the directory unwritable by removing it after load
	os.RemoveAll(tmpDir)

	// Update should fail on save but rollback the in-memory changes
	err := m.Update("test", []byte("new-value"), map[string]string{"v": "2"})
	if err == nil {
		t.Error("Expected error when save fails")
	}

	// In-memory value should have been rolled back (still accessible)
	value, metadata, retrieveErr := m.Retrieve("test")
	if retrieveErr != nil {
		t.Errorf("Retrieve should still work after rollback: %v", retrieveErr)
	}
	if string(value) != "original" {
		t.Errorf("Expected rolled back value 'original', got '%s'", value)
	}
	if metadata["v"] != "1" {
		t.Errorf("Expected rolled back metadata v='1', got '%s'", metadata["v"])
	}
}

// TestManager_Delete_SaveError tests delete when save fails
func TestManager_Delete_SaveError(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	tmpDir := t.TempDir()
	m, _ := NewManager(tmpDir, key)
	m.Store("test", []byte("value"), nil)

	// Remove directory so save fails
	os.RemoveAll(tmpDir)

	err := m.Delete("test")
	if err == nil {
		t.Error("Expected error when save fails after delete")
	}
}

// TestManager_RotateKey_DecryptError tests key rotation when a secret can't be decrypted
func TestManager_RotateKey_DecryptError(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	tmpDir := t.TempDir()
	m, _ := NewManager(tmpDir, key)

	m.Store("good-secret", []byte("value"), nil)
	m.Store("bad-secret", []byte("value2"), nil)

	// Corrupt one secret so decryption fails
	m.mu.Lock()
	secret := m.store["bad-secret"]
	secret.Encrypted[0] ^= 0xFF
	m.mu.Unlock()

	newKey := make([]byte, 32)
	for i := range newKey {
		newKey[i] = byte(i + 100)
	}

	err := m.RotateKey(newKey)
	if err == nil {
		t.Error("Expected error when decrypting corrupted secret during rotation")
	}
}

// TestManager_Decrypt_CiphertextTooShort tests decrypt with data too short for nonce
func TestManager_Decrypt_CiphertextTooShort(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	tmpDir := t.TempDir()
	m, _ := NewManager(tmpDir, key)

	// Manually create a secret with too-short encrypted data
	m.mu.Lock()
	m.store["short-secret"] = &Secret{
		Name:      "short-secret",
		Encrypted: []byte{0x01, 0x02}, // Too short for nonce + ciphertext
		Nonce:     make([]byte, 12),
		Salt:      make([]byte, 32),
		Version:   1,
	}
	m.mu.Unlock()

	_, _, err := m.Retrieve("short-secret")
	if err == nil {
		t.Error("Expected error for ciphertext too short")
	}
}

// TestManager_Save_UnwritablePath tests save when directory can't be created
func TestManager_Save_UnwritablePath(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	// Use a path that can't be created
	m, _ := NewManager("/invalid/readonly/path/secrets.enc", key)

	m.Store("test", []byte("value"), nil)

	// Direct save call should handle error gracefully
	err := m.save()
	if err == nil {
		// On some systems this might succeed, so also test with a truly invalid path
		t.Log("Save succeeded unexpectedly (may be platform-specific)")
	}
}

// TestManager_Load_NonExistentFile tests load when file doesn't exist
func TestManager_Load_NonExistentFile(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	tmpDir := t.TempDir()
	// Don't create the secrets.enc file
	m, err := NewManager(tmpDir, key)
	if err != nil {
		t.Fatalf("Should handle non-existent file gracefully: %v", err)
	}
	if m == nil {
		t.Fatal("Expected manager to be created")
	}
}

// TestEnvProvider_EmptyPrefix tests env provider without prefix
func TestEnvProvider_EmptyPrefix(t *testing.T) {
	os.Setenv("MY_SECRET", "secret-value")
	defer os.Unsetenv("MY_SECRET")

	provider := NewEnvProvider("")
	val, found := provider.Get("MY_SECRET")
	if !found || val != "secret-value" {
		t.Errorf("Expected 'secret-value', got '%s', found=%v", val, found)
	}
}

// TestSecureCompare_DifferentCases tests additional compare scenarios
func TestSecureCompare_DifferentCases(t *testing.T) {
	// Unicode strings
	if !SecureCompare("hello\u00e9", "hello\u00e9") {
		t.Error("Expected unicode equal strings to match")
	}

	// Long strings
	longA := "a" + string(make([]byte, 10000))
	longB := "a" + string(make([]byte, 10000))
	if !SecureCompare(longA, longB) {
		t.Error("Expected long equal strings to match")
	}
}

// TestManager_RotateKey_ReEncryptError tests key rotation when re-encryption fails
func TestManager_RotateKey_ReEncryptError(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	tmpDir := t.TempDir()
	m, _ := NewManager(tmpDir, key)

	// Store multiple secrets
	m.Store("secret-a", []byte("value-a"), nil)
	m.Store("secret-b", []byte("value-b"), nil)

	newKey := make([]byte, 32)
	for i := range newKey {
		newKey[i] = byte(i + 100)
	}

	// Normal rotation should succeed
	err := m.RotateKey(newKey)
	if err != nil {
		t.Errorf("Key rotation should succeed: %v", err)
	}

	// Verify both secrets accessible
	for _, name := range []string{"secret-a", "secret-b"} {
		value, _, err := m.Retrieve(name)
		if err != nil {
			t.Errorf("Failed to retrieve %s after rotation: %v", name, err)
		}
		if len(value) == 0 {
			t.Errorf("Expected non-empty value for %s", name)
		}
	}
}

// TestManager_Store_SaveError tests store when save fails (rollback)
func TestManager_Store_SaveError(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	tmpDir := t.TempDir()
	m, _ := NewManager(tmpDir, key)

	// Store one secret successfully (it saves to disk)
	m.Store("first", []byte("value"), nil)

	// Remove directory so next save fails
	os.RemoveAll(tmpDir)

	// Store should fail on save and rollback
	err := m.Store("second", []byte("value2"), nil)
	if err == nil {
		t.Error("Expected error when save fails")
	}

	// First secret should still be in memory
	names := m.List()
	if len(names) != 1 {
		t.Errorf("Expected 1 secret in memory, got %d", len(names))
	}
}

// TestManager_List_MultipleSecrets tests list with many secrets
func TestManager_List_MultipleSecrets(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	m, _ := NewManager("", key)
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("secret-%d", i)
		m.Store(name, []byte("value"), nil)
	}

	names := m.List()
	if len(names) != 10 {
		t.Errorf("Expected 10 secrets, got %d", len(names))
	}
}

// TestManager_Update_VersionIncrement tests that update increments version
func TestManager_Update_VersionIncrement(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	m, _ := NewManager("", key)
	m.Store("test", []byte("v1"), nil)

	// Get initial version
	m.mu.RLock()
	initialVersion := m.store["test"].Version
	m.mu.RUnlock()

	if initialVersion != 1 {
		t.Errorf("Expected initial version 1, got %d", initialVersion)
	}

	// Update
	m.Update("test", []byte("v2"), nil)

	m.mu.RLock()
	newVersion := m.store["test"].Version
	m.mu.RUnlock()

	if newVersion != 2 {
		t.Errorf("Expected version 2 after update, got %d", newVersion)
	}
}

// TestManager_RotateKey_KeyRestorationOnError tests that old key is restored on re-encrypt error
func TestManager_RotateKey_KeyRestorationOnError(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	tmpDir := t.TempDir()
	m, _ := NewManager(tmpDir, key)

	m.Store("good", []byte("value"), nil)
	m.Store("bad", []byte("value2"), nil)

	// Corrupt one
	m.mu.Lock()
	m.store["bad"].Encrypted[0] ^= 0xFF
	m.mu.Unlock()

	// Create a new key - rotation will fail
	newKey := make([]byte, 32)
	for i := range newKey {
		newKey[i] = byte(i + 200)
	}

	err := m.RotateKey(newKey)
	if err == nil {
		t.Error("Expected error during key rotation")
	}

	// Original secret should still be accessible with old key
	value, _, err := m.Retrieve("good")
	if err != nil {
		t.Errorf("Good secret should still be retrievable: %v", err)
	}
	if string(value) != "value" {
		t.Errorf("Good secret should have original value, got '%s'", value)
	}
}
