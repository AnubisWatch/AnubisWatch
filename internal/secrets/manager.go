package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

var (
	ErrSecretNotFound = errors.New("secret not found")
	ErrInvalidKey     = errors.New("invalid encryption key")
	ErrAlreadyExists  = errors.New("secret already exists")
)

// Manager handles secure storage and retrieval of secrets
type Manager struct {
	mu         sync.RWMutex
	store      map[string]*Secret
	key        []byte
	storageDir string
}

// Secret represents an encrypted secret value
type Secret struct {
	Name      string            `json:"name"`
	Encrypted []byte            `json:"encrypted"`
	Nonce     []byte            `json:"nonce"`
	Salt      []byte            `json:"salt"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Version   int               `json:"version"`
}

// NewManager creates a new secrets manager
func NewManager(storageDir string, masterKey []byte) (*Manager, error) {
	if len(masterKey) != 32 {
		return nil, ErrInvalidKey
	}

	m := &Manager{
		store:      make(map[string]*Secret),
		key:        masterKey,
		storageDir: storageDir,
	}

	// Load existing secrets
	if err := m.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load secrets: %w", err)
	}

	return m, nil
}

// Store saves a secret securely
func (m *Manager) Store(name string, value []byte, metadata map[string]string) error {
	if name == "" {
		return errors.New("secret name cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already exists
	if _, exists := m.store[name]; exists {
		return ErrAlreadyExists
	}

	// Encrypt the value
	encrypted, nonce, salt, err := m.encrypt(value)
	if err != nil {
		return fmt.Errorf("failed to encrypt secret: %w", err)
	}

	secret := &Secret{
		Name:      name,
		Encrypted: encrypted,
		Nonce:     nonce,
		Salt:      salt,
		Metadata:  metadata,
		Version:   1,
	}

	m.store[name] = secret

	// Persist to storage
	if err := m.save(); err != nil {
		delete(m.store, name)
		return fmt.Errorf("failed to save secret: %w", err)
	}

	return nil
}

// Retrieve gets a secret by name
func (m *Manager) Retrieve(name string) ([]byte, map[string]string, error) {
	m.mu.RLock()
	secret, exists := m.store[name]
	m.mu.RUnlock()

	if !exists {
		return nil, nil, ErrSecretNotFound
	}

	// Decrypt the value
	value, err := m.decrypt(secret)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decrypt secret: %w", err)
	}

	return value, secret.Metadata, nil
}

// Update modifies an existing secret
func (m *Manager) Update(name string, value []byte, metadata map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	secret, exists := m.store[name]
	if !exists {
		return ErrSecretNotFound
	}

	// Encrypt the new value
	encrypted, nonce, salt, err := m.encrypt(value)
	if err != nil {
		return fmt.Errorf("failed to encrypt secret: %w", err)
	}

	// Store old values for rollback
	oldEncrypted := secret.Encrypted
	oldNonce := secret.Nonce
	oldSalt := secret.Salt
	oldMetadata := secret.Metadata
	oldVersion := secret.Version

	// Update secret
	secret.Encrypted = encrypted
	secret.Nonce = nonce
	secret.Salt = salt
	secret.Metadata = metadata
	secret.Version++

	// Persist to storage
	if err := m.save(); err != nil {
		// Rollback
		secret.Encrypted = oldEncrypted
		secret.Nonce = oldNonce
		secret.Salt = oldSalt
		secret.Metadata = oldMetadata
		secret.Version = oldVersion
		return fmt.Errorf("failed to save secret: %w", err)
	}

	return nil
}

// Delete removes a secret
func (m *Manager) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.store[name]; !exists {
		return ErrSecretNotFound
	}

	delete(m.store, name)

	if err := m.save(); err != nil {
		return fmt.Errorf("failed to save after delete: %w", err)
	}

	return nil
}

// List returns all secret names
func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.store))
	for name := range m.store {
		names = append(names, name)
	}

	return names
}

// RotateKey re-encrypts all secrets with a new master key
func (m *Manager) RotateKey(newKey []byte) error {
	if len(newKey) != 32 {
		return ErrInvalidKey
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Decrypt all secrets with old key
	decrypted := make(map[string][]byte)
	for name, secret := range m.store {
		value, err := m.decrypt(secret)
		if err != nil {
			// Rollback decrypted secrets
			for n, v := range decrypted {
				// Securely clear memory
				for i := range v {
					v[i] = 0
				}
				_ = n
			}
			return fmt.Errorf("failed to decrypt secret %s: %w", name, err)
		}
		decrypted[name] = value
	}

	// Update key
	oldKey := m.key
	m.key = newKey

	// Re-encrypt all secrets
	for name, value := range decrypted {
		encrypted, nonce, salt, err := m.encrypt(value)
		if err != nil {
			m.key = oldKey
			return fmt.Errorf("failed to re-encrypt secret %s: %w", name, err)
		}

		m.store[name].Encrypted = encrypted
		m.store[name].Nonce = nonce
		m.store[name].Salt = salt
		m.store[name].Version++
	}

	// Save with new key
	if err := m.save(); err != nil {
		m.key = oldKey
		return fmt.Errorf("failed to save after key rotation: %w", err)
	}

	// Securely clear old key from memory
	for i := range oldKey {
		oldKey[i] = 0
	}

	// Securely clear decrypted values
	for _, value := range decrypted {
		for i := range value {
			value[i] = 0
		}
	}

	return nil
}

// encrypt encrypts data using AES-GCM
func (m *Manager) encrypt(plaintext []byte) ([]byte, []byte, []byte, error) {
	// Generate salt
	salt := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, nil, nil, err
	}

	// Derive key using XOR with salt (simple approach, consider HKDF for production)
	derivedKey := make([]byte, 32)
	for i := range m.key {
		derivedKey[i] = m.key[i] ^ salt[i%len(salt)]
	}

	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, nil, nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)

	// Securely clear derived key
	for i := range derivedKey {
		derivedKey[i] = 0
	}

	return ciphertext, nonce, salt, nil
}

// decrypt decrypts data using AES-GCM
func (m *Manager) decrypt(secret *Secret) ([]byte, error) {
	// Derive key using XOR with salt
	derivedKey := make([]byte, 32)
	for i := range m.key {
		derivedKey[i] = m.key[i] ^ secret.Salt[i%len(secret.Salt)]
	}

	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(secret.Encrypted) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, ciphertext := secret.Encrypted[:nonceSize], secret.Encrypted[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	// Securely clear derived key
	for i := range derivedKey {
		derivedKey[i] = 0
	}

	return plaintext, nil
}

// save persists secrets to storage
func (m *Manager) save() error {
	if m.storageDir == "" {
		return nil
	}

	data, err := json.Marshal(m.store)
	if err != nil {
		return err
	}

	path := filepath.Join(m.storageDir, "secrets.enc")
	return os.WriteFile(path, data, 0600)
}

// load reads secrets from storage
func (m *Manager) load() error {
	if m.storageDir == "" {
		return nil
	}

	path := filepath.Join(m.storageDir, "secrets.enc")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &m.store)
}

// EnvProvider loads secrets from environment variables
type EnvProvider struct {
	prefix string
}

// NewEnvProvider creates an environment-based secret provider
func NewEnvProvider(prefix string) *EnvProvider {
	return &EnvProvider{prefix: prefix}
}

// Get retrieves a secret from environment
func (p *EnvProvider) Get(name string) (string, bool) {
	key := p.prefix + name
	value := os.Getenv(key)
	return value, value != ""
}

// SecureCompare performs constant-time comparison of two strings
func SecureCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// GenerateKey generates a new random 32-byte key
func GenerateKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	return key, nil
}

// EncodeKey encodes a key to base64 string
func EncodeKey(key []byte) string {
	return base64.StdEncoding.EncodeToString(key)
}

// DecodeKey decodes a key from base64 string
func DecodeKey(encoded string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(encoded)
}
