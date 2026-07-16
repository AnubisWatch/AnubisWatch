package storage

import (
	"bytes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

var _ io.Reader = (*failReader)(nil)

func TestEncryptor_DeriveKeyFailure(t *testing.T) {
	old := hkdfReadFull
	hkdfReadFull = func(_ io.Reader, _ []byte) (int, error) {
		return 0, errors.New("hkdf injection")
	}
	t.Cleanup(func() { hkdfReadFull = old })

	enc := &encryptor{masterKey: []byte("test")}
	if _, err := enc.deriveKey(make([]byte, 32)); err == nil {
		t.Fatal("expected HKDF failure")
	}
}

func TestEncryptor_BuildCipherFailure(t *testing.T) {
	old := buildCipherSeam
	buildCipherSeam = func(_ cipher.Block) (cipher.AEAD, error) {
		return nil, errors.New("gcm injection")
	}
	t.Cleanup(func() { buildCipherSeam = old })

	enc := &encryptor{masterKey: []byte("test-key-12345678")}
	if _, err := enc.encrypt([]byte("data")); err == nil || !strings.Contains(err.Error(), "gcm injection") {
		t.Fatalf("encrypt error = %v", err)
	}
}

func TestNewEngine_WALFailure(t *testing.T) {
	old := newWALSeam
	newWALSeam = func(_ string) (*writeAheadLog, error) {
		return nil, errors.New("wal injection")
	}
	t.Cleanup(func() { newWALSeam = old })

	if _, err := NewEngine(core.StorageConfig{Path: t.TempDir()}, nil); err == nil || !strings.Contains(err.Error(), "wal injection") {
		t.Fatalf("NewEngine error = %v", err)
	}
}

func TestNewEngine_EncryptionEnabledEmptyKey(t *testing.T) {
	cfg := core.StorageConfig{
		Path: t.TempDir(),
		Encryption: core.EncryptionConfig{
			Enabled: true,
			Key:     "",
		},
	}
	if _, err := NewEngine(cfg, nil); err == nil {
		t.Fatal("expected error when encryption is enabled but key is empty")
	}
}

func TestNewEngine_EncryptionEnabledValidKey(t *testing.T) {
	cfg := core.StorageConfig{
		Path: t.TempDir(),
		Encryption: core.EncryptionConfig{
			Enabled: true,
			Key:     "a-valid-key-for-testing-purposes!!",
		},
	}
	db, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewEngine with valid key should succeed: %v", err)
	}
	defer db.Close()
	if db.encryptor == nil {
		t.Fatal("encryptor should be initialized when encryption is enabled")
	}
}

func TestEncryptor_RandReaderFailure(t *testing.T) {
	old := rand.Reader
	rand.Reader = &failReader{}
	t.Cleanup(func() { rand.Reader = old })

	enc := &encryptor{masterKey: []byte("test-key-12345678")}
	if _, err := enc.encrypt([]byte("data")); err == nil {
		t.Fatal("expected rand.Reader failure")
	}
}

func TestEncryptor_EncryptDecrypt(t *testing.T) {
	enc, err := newEncryptor("test-secret-key")
	if err != nil {
		t.Fatalf("newEncryptor failed: %v", err)
	}

	plaintext := []byte(`{"key":"value","nested":{"data":42}}`)

	ciphertext, err := enc.encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	if len(ciphertext) <= len(plaintext) {
		t.Errorf("ciphertext (%d bytes) should be longer than plaintext (%d bytes)",
			len(ciphertext), len(plaintext))
	}

	if bytes.Equal(ciphertext, plaintext) {
		t.Error("ciphertext should differ from plaintext")
	}

	decrypted, err := enc.decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("decrypted != plaintext\ngot:  %s\nwant: %s", decrypted, plaintext)
	}
}

func TestEncryptor_DifferentKeys(t *testing.T) {
	enc1, _ := newEncryptor("key-one")
	enc2, _ := newEncryptor("key-two")

	plaintext := []byte("secret data")
	ciphertext, _ := enc1.encrypt(plaintext)

	_, err := enc2.decrypt(ciphertext)
	if err == nil {
		t.Error("expected error when decrypting with wrong key")
	}
}

func TestEncryptor_EmptyKey(t *testing.T) {
	_, err := newEncryptor("")
	if err == nil {
		t.Error("expected error for empty key")
	}
}

func TestEncryptor_DecryptTampered(t *testing.T) {
	enc, _ := newEncryptor("test-key")

	plaintext := []byte("original data")
	ciphertext, _ := enc.encrypt(plaintext)

	ciphertext[len(ciphertext)-1] ^= 0xFF

	_, err := enc.decrypt(ciphertext)
	if err == nil {
		t.Error("expected error when decrypting tampered data")
	}
}

func TestEncryptor_IsEncrypted(t *testing.T) {
	enc, _ := newEncryptor("test-key")

	plaintext := []byte("hello")
	ciphertext, _ := enc.encrypt(plaintext)

	if !enc.isEncrypted(ciphertext) {
		t.Error("should detect encrypted data")
	}

	if enc.isEncrypted([]byte{0x01, 0x02}) {
		t.Error("short data should not be detected as encrypted")
	}
}

func TestCobaltDB_EncryptedPutGet(t *testing.T) {
	dir := t.TempDir()
	cfg := core.StorageConfig{
		Path: dir,
		Encryption: core.EncryptionConfig{
			Enabled: true,
			Key:     "test-encryption-key-32bytes!!",
		},
	}

	db, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer db.Close()

	if db.encryptor == nil {
		t.Fatal("encryptor should be set when encryption is enabled")
	}

	key := "test/encrypted/key"
	value := []byte(`{"soul_id":"abc123","status":"alive"}`)

	if err := db.Put(key, value); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	got, err := db.Get(key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if !bytes.Equal(got, value) {
		t.Errorf("Get returned wrong value\ngot:  %s\nwant: %s", got, value)
	}
}

func TestCobaltDB_NoEncryption(t *testing.T) {
	dir := t.TempDir()
	cfg := core.StorageConfig{Path: dir}

	db, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer db.Close()

	if db.encryptor != nil {
		t.Fatal("encryptor should be nil when encryption is disabled")
	}

	key := "test/noencrypt/key"
	value := []byte(`{"data":"plain"}`)

	if err := db.Put(key, value); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	got, err := db.Get(key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if !bytes.Equal(got, value) {
		t.Errorf("Get returned wrong value\ngot:  %s\nwant: %s", got, value)
	}
}

func TestCobaltDB_EncryptedSoulRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := core.StorageConfig{
		Path: dir,
		Encryption: core.EncryptionConfig{
			Enabled: true,
			Key:     "another-test-key-for-souls!!",
		},
	}

	db, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer db.Close()

	soul := &core.Soul{
		ID:          "enc-soul-1",
		WorkspaceID: "default",
		Name:        "Encrypted Soul",
		Type:        core.CheckHTTP,
		Target:      "example.com",
		Enabled:     true,
	}

	if err := db.SaveSoul(nil, soul); err != nil {
		t.Fatalf("SaveSoul failed: %v", err)
	}

	got, err := db.GetSoul(nil, "default", "enc-soul-1")
	if err != nil {
		t.Fatalf("GetSoul failed: %v", err)
	}

	if got.ID != soul.ID || got.Name != soul.Name || got.Target != soul.Target {
		t.Errorf("GetSoul returned wrong soul\ngot:  %+v\nwant: %+v", got, soul)
	}
}
