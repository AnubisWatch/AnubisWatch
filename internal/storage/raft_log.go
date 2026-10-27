package storage

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/raft"
)

// CobaltDBLogStore implements the raft.LogStore interface using CobaltDB
type CobaltDBLogStore struct {
	db *CobaltDB
}

// NewCobaltDBLogStore creates a new LogStore backed by CobaltDB
func NewCobaltDBLogStore(db *CobaltDB) *CobaltDBLogStore {
	return &CobaltDBLogStore{db: db}
}

// FirstIndex returns the first index in the log
func (s *CobaltDBLogStore) FirstIndex() (uint64, error) {
	prefix := "raft/log/"
	results, err := s.db.PrefixScan(prefix)
	if err != nil {
		return 0, err
	}

	if len(results) == 0 {
		return 0, nil
	}

	// Extract indices from keys and find minimum
	indices := make([]uint64, 0, len(results))
	for key := range results {
		idxStr := key[len(prefix):]
		var idx uint64
		// Keys are written as raft/log/%d; a parse failure yields idx 0,
		// which sorts to the front and is caught by FirstIndex's caller.
		_, _ = fmt.Sscanf(idxStr, "%d", &idx)
		indices = append(indices, idx)
	}

	if len(indices) == 0 {
		return 0, nil
	}

	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })
	return indices[0], nil
}

// LastIndex returns the last index in the log
func (s *CobaltDBLogStore) LastIndex() (uint64, error) {
	prefix := "raft/log/"
	results, err := s.db.PrefixScan(prefix)
	if err != nil {
		return 0, err
	}

	if len(results) == 0 {
		return 0, nil
	}

	// Extract indices from keys and find maximum
	indices := make([]uint64, 0, len(results))
	for key := range results {
		idxStr := key[len(prefix):]
		var idx uint64
		// Keys are written as raft/log/%d; a parse failure yields idx 0,
		// which is a no-op for LastIndex (max is what matters).
		_, _ = fmt.Sscanf(idxStr, "%d", &idx)
		indices = append(indices, idx)
	}

	if len(indices) == 0 {
		return 0, nil
	}

	sort.Slice(indices, func(i, j int) bool { return indices[i] > indices[j] })
	return indices[0], nil
}

// GetLog retrieves a log entry at the given index
func (s *CobaltDBLogStore) GetLog(index uint64, log *core.RaftLogEntry) error {
	key := fmt.Sprintf("raft/log/%d", index)
	data, err := s.db.Get(key)
	if err != nil {
		return err
	}

	var entry struct {
		Term uint64            `json:"term"`
		Type core.LogEntryType `json:"type"`
		Data json.RawMessage   `json:"data"`
	}
	if err := json.Unmarshal(data, &entry); err != nil {
		return fmt.Errorf("failed to unmarshal log entry: %w", err)
	}

	decoded := core.RaftLogEntry{Index: index, Term: entry.Term, Type: entry.Type}
	var dataStr *string
	if json.Unmarshal(entry.Data, &dataStr) == nil && dataStr != nil {
		if data, err := base64.StdEncoding.DecodeString(*dataStr); err == nil {
			decoded.Data = data
		}
	}
	*log = decoded

	return nil
}

// StoreLog stores a single log entry
func (s *CobaltDBLogStore) StoreLog(log *core.RaftLogEntry) error {
	key, value, err := encodeLogEntry(log)
	if err != nil {
		return err
	}
	return s.db.Put(key, value)
}

func encodeLogEntry(log *core.RaftLogEntry) (string, []byte, error) {
	entry := map[string]any{
		"index": log.Index,
		"term":  log.Term,
		"type":  log.Type,
		"data":  log.Data,
	}
	jsonData, err := json.Marshal(entry)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal log entry: %w", err)
	}
	return fmt.Sprintf("raft/log/%d", log.Index), jsonData, nil
}

// logBatchBytes bounds the raw size of one batched WAL frame; base64, JSON
// and encryption overhead keep the encoded frame under maxWALEntryBytes.
const logBatchBytes = 512 * 1024

// StoreLogs stores entries in as few WAL frames as fit, each written and
// fsynced once, instead of one fsync per entry: a follower catching up
// receives up to MaxAppendEntries entries per AppendEntries.
func (s *CobaltDBLogStore) StoreLogs(logs []core.RaftLogEntry) error {
	batch := make(map[string][]byte)
	size := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		err := s.db.putMany(batch)
		batch, size = make(map[string][]byte), 0
		return err
	}
	for i := range logs {
		key, value, err := encodeLogEntry(&logs[i])
		if err != nil {
			return err
		}
		if size > 0 && size+len(value) > logBatchBytes {
			if err := flush(); err != nil {
				return err
			}
		}
		batch[key] = value
		size += len(value)
	}
	return flush()
}

// DeleteRange deletes all log entries in the given range (inclusive)
func (s *CobaltDBLogStore) DeleteRange(minIdx, maxIdx uint64) error {
	if minIdx > maxIdx {
		return nil
	}
	for i := minIdx; ; i++ {
		key := fmt.Sprintf("raft/log/%d", i)
		if err := s.db.Delete(key); err != nil {
			return err
		}
		if i == maxIdx {
			break
		}
	}
	return nil
}

// CobaltDBSnapshotStore implements raft.SnapshotStore
type CobaltDBSnapshotStore struct {
	db *CobaltDB
}

// Each immutable chunk stays well below the encoded WAL record limit, including
// JSON/base64 and encryption overhead. Publishing one small manifest commits the
// image atomically; an interrupted writer cannot replace the previous snapshot.
const snapshotChunkSize = 256 * 1024

type snapshotManifest struct {
	raft.SnapshotMeta
	ChunkPrefix string `json:"chunk_prefix,omitempty"`
	Chunks      int    `json:"chunks,omitempty"`
}

// NewCobaltDBSnapshotStore creates a new SnapshotStore backed by CobaltDB
func NewCobaltDBSnapshotStore(db *CobaltDB) *CobaltDBSnapshotStore {
	return &CobaltDBSnapshotStore{db: db}
}

// Create creates a new snapshot
func (s *CobaltDBSnapshotStore) Create(version, index, term uint64, configuration []byte) (raft.SnapshotSink, error) {
	sink := &cobaltDBSnapshotSink{
		store:   s,
		id:      fmt.Sprintf("%d-%d", term, index),
		index:   index,
		term:    term,
		version: version,
		config:  configuration,
		buf:     &bytes.Buffer{},
	}
	return sink, nil
}

// List returns metadata for all snapshots
func (s *CobaltDBSnapshotStore) List() ([]raft.SnapshotMeta, error) {
	data, err := s.db.Get("raft/snapshot-meta")
	if err != nil {
		return nil, err
	}

	var metas []raft.SnapshotMeta
	if err := json.Unmarshal(data, &metas); err != nil {
		return nil, err
	}
	return metas, nil
}

// Open opens a snapshot for reading
func (s *CobaltDBSnapshotStore) Open(id string) (raft.SnapshotSource, error) {
	s.db.snapshotMu.Lock()
	defer s.db.snapshotMu.Unlock()
	metadata, err := s.db.Get("raft/snapshot-meta")
	if err != nil {
		return nil, err
	}
	var manifests []snapshotManifest
	if err := json.Unmarshal(metadata, &manifests); err != nil {
		return nil, err
	}
	if len(manifests) != 1 || manifests[0].ID != id {
		return nil, fmt.Errorf("snapshot not found: %s", id)
	}
	manifest := manifests[0]
	var data []byte
	if manifest.ChunkPrefix == "" {
		// Compatibility with snapshots produced before chunked storage.
		data, err = s.db.Get("raft/snapshot")
		if err != nil {
			return nil, err
		}
	} else {
		if manifest.Chunks < 0 || manifest.Size < 0 ||
			int64(manifest.Chunks) != (manifest.Size+snapshotChunkSize-1)/snapshotChunkSize {
			return nil, fmt.Errorf("invalid snapshot manifest: %s", id)
		}
		for i := 0; i < manifest.Chunks; i++ {
			chunk, err := s.db.Get(fmt.Sprintf("%s%d", manifest.ChunkPrefix, i))
			if err != nil {
				return nil, fmt.Errorf("read snapshot chunk %d: %w", i, err)
			}
			expected := snapshotChunkSize
			if i == manifest.Chunks-1 {
				expected = int(manifest.Size - int64(i)*snapshotChunkSize)
			}
			if len(chunk) != expected {
				return nil, fmt.Errorf("invalid snapshot chunk %d size", i)
			}
			data = append(data, chunk...)
		}
	}

	source := &cobaltDBSnapshotSource{
		data: data,
		pos:  0,
	}
	return source, nil
}

// cobaltDBSnapshotSink implements raft.SnapshotSink
type cobaltDBSnapshotSink struct {
	store    *CobaltDBSnapshotStore
	id       string
	index    uint64
	term     uint64
	version  uint64
	config   []byte
	buf      *bytes.Buffer
	closed   bool
	closeErr error
}

// Write writes data to the snapshot
func (s *cobaltDBSnapshotSink) Write(p []byte) (n int, err error) {
	if s.closed {
		return 0, fmt.Errorf("sink is closed")
	}
	return s.buf.Write(p)
}

// Close finalizes the snapshot
func (s *cobaltDBSnapshotSink) Close() error {
	if s.closed {
		return s.closeErr
	}
	s.closed = true
	s.store.db.snapshotMu.Lock()
	defer s.store.db.snapshotMu.Unlock()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		s.closeErr = err
		return err
	}
	prefix := fmt.Sprintf("raft/snapshot-data/%x/", nonce)
	manifest := snapshotManifest{SnapshotMeta: raft.SnapshotMeta{
		ID:      s.id,
		Index:   s.index,
		Term:    s.term,
		Size:    int64(s.buf.Len()),
		Version: s.version,
	}, ChunkPrefix: prefix}
	data := s.buf.Bytes()
	for start := 0; start < len(data); start += snapshotChunkSize {
		end := min(start+snapshotChunkSize, len(data))
		if err := s.store.db.Put(fmt.Sprintf("%s%d", prefix, manifest.Chunks), data[start:end]); err != nil {
			s.closeErr = err
			_ = s.store.db.DeletePrefix(prefix)
			return err
		}
		manifest.Chunks++
	}
	metadata, _ := json.Marshal([]snapshotManifest{manifest})
	if err := s.store.db.Put("raft/snapshot-meta", metadata); err != nil {
		s.closeErr = err
		_ = s.store.db.DeletePrefix(prefix)
		return err
	}
	// The new image is visible. Failed cleanup can only leave unreachable
	// chunks, never invalidate the committed manifest or the previous image.
	// Include abandoned generations left by a crash before manifest commit.
	if keys, err := s.store.db.List("raft/snapshot-data/"); err == nil {
		for _, key := range keys {
			if !strings.HasPrefix(key, prefix) {
				_ = s.store.db.Delete(key)
			}
		}
	}
	_ = s.store.db.Delete("raft/snapshot")
	return nil
}

// ID returns the snapshot ID
func (s *cobaltDBSnapshotSink) ID() string {
	return s.id
}

// Cancel cancels the snapshot creation
func (s *cobaltDBSnapshotSink) Cancel() error {
	s.closed = true
	return nil
}

// cobaltDBSnapshotSource implements raft.SnapshotSource
type cobaltDBSnapshotSource struct {
	data []byte
	pos  int
}

// Read reads from the snapshot
func (s *cobaltDBSnapshotSource) Read(p []byte) (n int, err error) {
	if s.pos >= len(s.data) {
		return 0, io.EOF
	}
	n = copy(p, s.data[s.pos:])
	s.pos += n
	return n, nil
}

// Close closes the snapshot source
func (s *cobaltDBSnapshotSource) Close() error {
	return nil
}

// CobaltDBStableStore implements raft.StableStore for stable storage
type CobaltDBStableStore struct {
	db *CobaltDB
}

// NewCobaltDBStableStore creates a new StableStore backed by CobaltDB
func NewCobaltDBStableStore(db *CobaltDB) *CobaltDBStableStore {
	return &CobaltDBStableStore{db: db}
}

// SetUint64 stores a uint64 value
func (s *CobaltDBStableStore) SetUint64(key string, val uint64) error {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, val)
	return s.db.Put(fmt.Sprintf("raft/stable/%s", key), buf)
}

// GetUint64 retrieves a uint64 value
func (s *CobaltDBStableStore) GetUint64(key string) (uint64, error) {
	data, err := s.db.Get(fmt.Sprintf("raft/stable/%s", key))
	if err != nil {
		return 0, err
	}
	if len(data) != 8 {
		return 0, fmt.Errorf("invalid uint64 value for key %q: got %d bytes, want 8", key, len(data))
	}
	return binary.BigEndian.Uint64(data), nil
}

// Set stores a byte slice
func (s *CobaltDBStableStore) Set(key string, val []byte) error {
	return s.db.Put(fmt.Sprintf("raft/stable/%s", key), val)
}

// Get retrieves a byte slice
func (s *CobaltDBStableStore) Get(key string) ([]byte, error) {
	return s.db.Get(fmt.Sprintf("raft/stable/%s", key))
}
