package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

// encryptionSaltLen is the length of the random salt for HKDF key derivation
const encryptionSaltLen = 32

// encryptionKDFInfo provides domain separation for HKDF
const encryptionKDFInfo = "anubiswatch-storage-encryption-v1"

// encryptor handles AES-256-GCM encryption/decryption for data at rest.
type encryptor struct {
	masterKey []byte // original key material, used with HKDF + salt
}

// newEncryptor creates an AES-256-GCM encryptor from a key string.
// The key is derived via HKDF-SHA256 with a random salt for each encryption
// operation, providing resistance against rainbow table attacks.
func newEncryptor(key string) (*encryptor, error) {
	if key == "" {
		return nil, fmt.Errorf("encryption key is empty")
	}
	return &encryptor{masterKey: []byte(key)}, nil
}

// hkdfReadFull is overwritten by tests to inject HKDF read failures.
var hkdfReadFull = func(r io.Reader, b []byte) (int, error) { return io.ReadFull(r, b) }

// deriveKey uses HKDF-SHA256 to derive an AES-256 key from the master key
// and a random salt. This replaces the simple SHA-256 hash with a proper
// KDF that supports domain separation and per-encryption salt.
func (e *encryptor) deriveKey(salt []byte) ([]byte, error) {
	derivedKey := make([]byte, 32)
	hkdfReader := hkdf.New(sha256.New, e.masterKey, salt, []byte(encryptionKDFInfo))
	if _, err := hkdfReadFull(hkdfReader, derivedKey); err != nil {
		return nil, fmt.Errorf("HKDF key derivation failed: %w", err)
	}
	return derivedKey, nil
}

// buildCipher creates an AES-GCM cipher from a derived key.
// Tests can override buildCipherSeam to inject cipher creation failures.
var buildCipherSeam = func(block cipher.Block) (cipher.AEAD, error) {
	return cipher.NewGCM(block)
}

func (e *encryptor) buildCipher(derivedKey []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}
	gcm, err := buildCipherSeam(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}
	return gcm, nil
}

// encrypt encrypts plaintext and returns salt + nonce + ciphertext.
// Format: [32-byte salt][12-byte nonce][ciphertext + 16-byte auth tag]
func (e *encryptor) encrypt(plaintext []byte) ([]byte, error) {
	// Generate random salt for HKDF
	salt := make([]byte, encryptionSaltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	// Derive AES key via HKDF
	derivedKey, err := e.deriveKey(salt)
	if err != nil {
		return nil, err
	}
	defer func() {
		for i := range derivedKey {
			derivedKey[i] = 0
		}
	}()

	gcm, err := e.buildCipher(derivedKey)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Seal appends ciphertext + auth tag to the nonce slice
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)

	// Prepend salt to output
	result := make([]byte, len(salt)+len(ciphertext))
	copy(result, salt)
	copy(result[len(salt):], ciphertext)
	return result, nil
}

// decrypt decrypts data encrypted with encrypt.
// Expects format: [32-byte salt][12-byte nonce][ciphertext + 16-byte auth tag]
func (e *encryptor) decrypt(data []byte) ([]byte, error) {
	if len(data) < encryptionSaltLen {
		return nil, fmt.Errorf("encrypted data too short")
	}

	salt := data[:encryptionSaltLen]
	nonceAndCiphertext := data[encryptionSaltLen:]

	// Derive AES key via HKDF (must use same salt)
	derivedKey, err := e.deriveKey(salt)
	if err != nil {
		return nil, err
	}
	defer func() {
		for i := range derivedKey {
			derivedKey[i] = 0
		}
	}()

	gcm, err := e.buildCipher(derivedKey)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(nonceAndCiphertext) < nonceSize {
		return nil, fmt.Errorf("encrypted data too short")
	}

	nonce, ciphertext := nonceAndCiphertext[:nonceSize], nonceAndCiphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}

	return plaintext, nil
}

// isEncrypted checks if data looks encrypted.
// We check that data is at least saltSize + nonceSize + tagSize bytes.
func (e *encryptor) isEncrypted(data []byte) bool {
	return len(data) >= encryptionSaltLen+12+16 // salt + min nonce + GCM tag
}
