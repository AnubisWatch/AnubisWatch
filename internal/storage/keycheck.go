package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// keyCheckFile holds a small value sealed with the storage encryption key. It
// lives beside wal.log rather than in the keyspace so it never shows up in
// List/PrefixScan results, Raft snapshots or backups (which carry decrypted
// records and are re-encrypted under the restoring node's own key).
const keyCheckFile = "encryption.keycheck"

const (
	keyCheckVersion   = 1
	keyCheckPlaintext = "anubiswatch-cobaltdb-key-check-v1"
	// keyCheckSamples bounds how many existing records are tried when a
	// store encrypted before the key check existed is opened for the first
	// time. One record that decrypts proves the key: AES-GCM cannot
	// authenticate under a different key.
	keyCheckSamples = 32
)

type keyCheckRecord struct {
	Version int    `json:"version"`
	Canary  []byte `json:"canary"`
}

// verifyEncryptionKey refuses to open a store whose data was sealed with a
// different key, or an encrypted store without any key. When the store has no
// key-check record yet it is created, after confirming that existing records
// (if any) decrypt with the configured key.
func (db *CobaltDB) verifyEncryptionKey() error {
	path := filepath.Join(db.path, keyCheckFile)
	raw, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read encryption key check %s: %w", path, err)
	}

	if db.encryptor == nil {
		if exists {
			return fmt.Errorf("data directory %s is encrypted (%s exists) but storage encryption is disabled; "+
				"set storage.encryption.key or ANUBIS_ENCRYPTION_KEY to the key it was created with", db.path, keyCheckFile)
		}
		return nil
	}

	if exists {
		var rec keyCheckRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			return fmt.Errorf("encryption key check %s is unreadable: %w", path, err)
		}
		plain, err := db.encryptor.decrypt(rec.Canary)
		if err != nil || string(plain) != keyCheckPlaintext {
			return fmt.Errorf("storage encryption key does not match the key data directory %s was encrypted with; "+
				"refusing to start so data is not mixed under two keys", db.path)
		}
		return nil
	}

	// No key check yet: either a new store or one encrypted before the check
	// was introduced. Prove the key against stored records first.
	samples := db.sampleStoredValues(keyCheckSamples)
	if len(samples) > 0 {
		ok := false
		for _, v := range samples {
			if _, err := db.encryptor.decrypt(v); err == nil {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("none of the existing records in %s decrypt with the configured storage encryption key "+
				"(wrong key, or the store was written without encryption); refusing to start", db.path)
		}
	}
	return db.writeKeyCheck(path)
}

// sampleStoredValues returns up to n live physical values in key order.
func (db *CobaltDB) sampleStoredValues(n int) [][]byte {
	db.data.mu.RLock()
	defer db.data.mu.RUnlock()
	node := db.data.root
	for node != nil && !node.isLeaf {
		if len(node.children) == 0 {
			return nil
		}
		node = node.children[0]
	}
	var out [][]byte
	for ; node != nil && len(out) < n; node = node.next {
		for _, v := range node.values {
			if v != nil {
				out = append(out, v)
				if len(out) == n {
					break
				}
			}
		}
	}
	return out
}

// writeKeyCheck atomically writes a fresh key-check record (0600).
func (db *CobaltDB) writeKeyCheck(path string) error {
	canary, err := db.encryptor.encrypt([]byte(keyCheckPlaintext))
	if err != nil {
		return fmt.Errorf("seal encryption key check: %w", err)
	}
	data, err := json.Marshal(keyCheckRecord{Version: keyCheckVersion, Canary: canary})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), keyCheckFile+".tmp-*")
	if err != nil {
		return fmt.Errorf("write encryption key check: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write encryption key check: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("write encryption key check: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write encryption key check: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("write encryption key check: %w", err)
	}
	if err := syncDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("sync data directory after key check: %w", err)
	}
	return nil
}
