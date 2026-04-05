package storage

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"sort"

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
		fmt.Sscanf(idxStr, "%d", &idx)
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
		fmt.Sscanf(idxStr, "%d", &idx)
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

	var entry map[string]interface{}
	if err := json.Unmarshal(data, &entry); err != nil {
		return fmt.Errorf("failed to unmarshal log entry: %w", err)
	}

	log.Index = index
	if term, ok := entry["term"].(float64); ok {
		log.Term = uint64(term)
	}
	if dataBytes, ok := entry["data"].([]byte); ok {
		log.Data = dataBytes
	}

	return nil
}

// StoreLog stores a single log entry
func (s *CobaltDBLogStore) StoreLog(log *core.RaftLogEntry) error {
	entry := map[string]interface{}{
		"index": log.Index,
		"term":  log.Term,
		"data":  log.Data,
	}

	jsonData, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal log entry: %w", err)
	}

	key := fmt.Sprintf("raft/log/%d", log.Index)
	return s.db.Put(key, jsonData)
}

// StoreLogs stores multiple log entries in batch
func (s *CobaltDBLogStore) StoreLogs(logs []core.RaftLogEntry) error {
	for _, log := range logs {
		if err := s.StoreLog(&log); err != nil {
			return err
		}
	}
	return nil
}

// DeleteRange deletes all log entries in the given range (inclusive)
func (s *CobaltDBLogStore) DeleteRange(min, max uint64) error {
	for i := min; i <= max; i++ {
		key := fmt.Sprintf("raft/log/%d", i)
		if err := s.db.Delete(key); err != nil {
			return err
		}
	}
	return nil
}

// CobaltDBSnapshotStore implements raft.SnapshotStore
type CobaltDBSnapshotStore struct {
	db *CobaltDB
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
	data, err := s.db.Get("raft/snapshot")
	if err != nil {
		return nil, err
	}

	source := &cobaltDBSnapshotSource{
		data: data,
		pos:  0,
	}
	return source, nil
}

// cobaltDBSnapshotSink implements raft.SnapshotSink
type cobaltDBSnapshotSink struct {
	store   *CobaltDBSnapshotStore
	id      string
	index   uint64
	term    uint64
	version uint64
	config  []byte
	buf     *bytes.Buffer
	closed  bool
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
		return nil
	}
	s.closed = true

	// Save snapshot data
	if err := s.store.db.Put("raft/snapshot", s.buf.Bytes()); err != nil {
		return err
	}

	// Save metadata
	meta := raft.SnapshotMeta{
		ID:      s.id,
		Index:   s.index,
		Term:    s.term,
		Size:    int64(s.buf.Len()),
		Version: s.version,
	}
	metas, _ := s.store.List()
	metas = []raft.SnapshotMeta{meta} // Keep only latest snapshot
	metaData, _ := json.Marshal(metas)
	return s.store.db.Put("raft/snapshot-meta", metaData)
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
