package raft

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// StorageFSM implements the FSM interface using CobaltDB
// The sacred records inscribed on the tablets of the Necropolis
type StorageFSM struct {
	mu      sync.RWMutex
	store   Storage
	index   uint64
}

// Storage is the interface for key-value storage
type Storage interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte) error
	Delete(key string) error
	DeletePrefix(prefix string) error
	List(prefix string) ([]string, error)
}

// NewStorageFSM creates a new FSM backed by storage
func NewStorageFSM(store Storage) *StorageFSM {
	return &StorageFSM{
		store: store,
		index: 0,
	}
}

// Apply applies a Raft log entry to the FSM
func (f *StorageFSM) Apply(log *core.RaftLogEntry) interface{} {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Skip if already applied
	if log.Index <= f.index {
		return nil
	}

	switch log.Type {
	case core.LogCommand:
		return f.applyCommand(log)
	case core.LogConfiguration:
		return f.applyConfiguration(log)
	case core.LogNoOp:
		f.index = log.Index
		return nil
	default:
		return fmt.Errorf("unknown log entry type: %d", log.Type)
	}
}

// applyCommand applies a command log entry
func (f *StorageFSM) applyCommand(log *core.RaftLogEntry) interface{} {
	cmd, err := f.decodeCommand(log.Data)
	if err != nil {
		return fmt.Errorf("failed to decode command: %w", err)
	}

	var result interface{}
	switch cmd.Op {
	case core.FSMSet:
		err = f.store.Set(cmd.Key, cmd.Value)
		result = err

	case core.FSMDelete:
		err = f.store.Delete(cmd.Key)
		result = err

	case core.FSMDeletePrefix:
		err = f.store.DeletePrefix(cmd.Key)
		result = err

	default:
		return fmt.Errorf("unknown command op: %d", cmd.Op)
	}

	f.index = log.Index
	return result
}

// applyConfiguration applies a configuration change
func (f *StorageFSM) applyConfiguration(log *core.RaftLogEntry) interface{} {
	// Configuration changes are handled by the Raft layer
	// We just persist them here
	key := fmt.Sprintf("raft/config/%d", log.Index)
	err := f.store.Set(key, log.Data)
	f.index = log.Index
	return err
}

// decodeCommand decodes a command from bytes
func (f *StorageFSM) decodeCommand(data []byte) (*core.FSMCommand, error) {
	var cmd core.FSMCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		return nil, err
	}
	return &cmd, nil
}

// encodeCommand encodes a command to bytes
func (f *StorageFSM) encodeCommand(cmd *core.FSMCommand) ([]byte, error) {
	return json.Marshal(cmd)
}

// Snapshot creates a snapshot of the FSM
func (f *StorageFSM) Snapshot() (core.FSMCommand, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Get all keys
	keys, err := f.store.List("")
	if err != nil {
		return core.FSMCommand{}, err
	}

	// Build snapshot
	snapshot := make(map[string][]byte)
	for _, key := range keys {
		value, err := f.store.Get(key)
		if err != nil {
			continue
		}
		snapshot[key] = value
	}

	// Serialize
	data, err := json.Marshal(snapshot)
	if err != nil {
		return core.FSMCommand{}, err
	}

	return core.FSMCommand{
		Op:    core.FSMSet,
		Key:   "snapshot",
		Value: data,
	}, nil
}

// Restore restores the FSM from a snapshot
func (f *StorageFSM) Restore(snapshot []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Deserialize
	data := make(map[string][]byte)
	if err := json.Unmarshal(snapshot, &data); err != nil {
		return err
	}

	// Clear existing data
	if err := f.store.DeletePrefix(""); err != nil {
		return err
	}

	// Restore data
	for key, value := range data {
		if err := f.store.Set(key, value); err != nil {
			return err
		}
	}

	return nil
}

// LastApplied returns the last applied index
func (f *StorageFSM) LastApplied() uint64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.index
}

// InMemoryStorage is a simple in-memory storage for testing
type InMemoryStorage struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// NewInMemoryStorage creates a new in-memory storage
func NewInMemoryStorage() *InMemoryStorage {
	return &InMemoryStorage{
		data: make(map[string][]byte),
	}
}

// Get retrieves a value
func (s *InMemoryStorage) Get(key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	value, ok := s.data[key]
	if !ok {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return value, nil
}

// Set stores a value
func (s *InMemoryStorage) Set(key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[key] = value
	return nil
}

// Delete removes a key
func (s *InMemoryStorage) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, key)
	return nil
}

// DeletePrefix removes all keys with the given prefix
func (s *InMemoryStorage) DeletePrefix(prefix string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key := range s.data {
		if len(prefix) == 0 || (len(key) >= len(prefix) && key[:len(prefix)] == prefix) {
			delete(s.data, key)
		}
	}
	return nil
}

// List returns all keys with the given prefix
func (s *InMemoryStorage) List(prefix string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keys := make([]string, 0)
	for key := range s.data {
		if len(prefix) == 0 || (len(key) >= len(prefix) && key[:len(prefix)] == prefix) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

// LogStore interface implementation (placeholder)

// InMemoryLogStore is an in-memory log store for testing
type InMemoryLogStore struct {
	mu      sync.RWMutex
	entries []core.RaftLogEntry
}

// NewInMemoryLogStore creates a new in-memory log store
func NewInMemoryLogStore() *InMemoryLogStore {
	return &InMemoryLogStore{
		entries: make([]core.RaftLogEntry, 1), // Index 0 is unused
	}
}

// FirstIndex returns the first log index
func (s *InMemoryLogStore) FirstIndex() (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.entries) <= 1 {
		return 0, nil
	}
	return 1, nil
}

// LastIndex returns the last log index
func (s *InMemoryLogStore) LastIndex() (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return uint64(len(s.entries) - 1), nil
}

// GetLog retrieves a log entry
func (s *InMemoryLogStore) GetLog(index uint64, log *core.RaftLogEntry) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if index == 0 || index >= uint64(len(s.entries)) {
		return fmt.Errorf("log entry not found: %d", index)
	}
	*log = s.entries[index]
	return nil
}

// StoreLog stores a log entry
func (s *InMemoryLogStore) StoreLog(log *core.RaftLogEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if log.Index == 0 {
		log.Index = uint64(len(s.entries))
	}
	if log.Index >= uint64(len(s.entries)) {
		// Extend slice
		for uint64(len(s.entries)) <= log.Index {
			s.entries = append(s.entries, core.RaftLogEntry{})
		}
	}
	s.entries[log.Index] = *log
	return nil
}

// StoreLogs stores multiple log entries
func (s *InMemoryLogStore) StoreLogs(logs []core.RaftLogEntry) error {
	for _, log := range logs {
		if err := s.StoreLog(&log); err != nil {
			return err
		}
	}
	return nil
}

// DeleteRange deletes log entries in a range
func (s *InMemoryLogStore) DeleteRange(min, max uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if max >= uint64(len(s.entries)) {
		max = uint64(len(s.entries) - 1)
	}

	for i := min; i <= max && i < uint64(len(s.entries)); i++ {
		s.entries[i] = core.RaftLogEntry{}
	}
	return nil
}

// SnapshotStore interface implementation (placeholder)

// InMemorySnapshotStore is an in-memory snapshot store for testing
type InMemorySnapshotStore struct {
	mu        sync.RWMutex
	snapshots map[string]*InMemorySnapshot
}

// InMemorySnapshot represents a stored snapshot
type InMemorySnapshot struct {
	meta SnapshotMeta
	data []byte
}

// NewInMemorySnapshotStore creates a new in-memory snapshot store
func NewInMemorySnapshotStore() *InMemorySnapshotStore {
	return &InMemorySnapshotStore{
		snapshots: make(map[string]*InMemorySnapshot),
	}
}

// Create creates a new snapshot
func (s *InMemorySnapshotStore) Create(version, index, term uint64, configuration []byte) (SnapshotSink, error) {
	id := fmt.Sprintf("snapshot-%d-%d", term, index)
	return &InMemorySnapshotSink{
		id: id,
		meta: SnapshotMeta{
			ID:      id,
			Index:   index,
			Term:    term,
			Version: version,
		},
		store: s,
	}, nil
}

// List returns all snapshots
func (s *InMemorySnapshotStore) List() ([]SnapshotMeta, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	metas := make([]SnapshotMeta, 0, len(s.snapshots))
	for _, snap := range s.snapshots {
		metas = append(metas, snap.meta)
	}
	return metas, nil
}

// Open opens a snapshot for reading
func (s *InMemorySnapshotStore) Open(id string) (SnapshotSource, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snap, ok := s.snapshots[id]
	if !ok {
		return nil, fmt.Errorf("snapshot not found: %s", id)
	}

	return &InMemorySnapshotSource{data: snap.data}, nil
}

// InMemorySnapshotSink implements SnapshotSink
type InMemorySnapshotSink struct {
	id     string
	meta   SnapshotMeta
	data   []byte
	store  *InMemorySnapshotStore
	closed bool
}

// Write writes data to the snapshot
func (s *InMemorySnapshotSink) Write(p []byte) (n int, err error) {
	if s.closed {
		return 0, fmt.Errorf("snapshot closed")
	}
	s.data = append(s.data, p...)
	s.meta.Size = int64(len(s.data))
	return len(p), nil
}

// Close closes the snapshot sink
func (s *InMemorySnapshotSink) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true

	s.store.mu.Lock()
	s.store.snapshots[s.id] = &InMemorySnapshot{
		meta: s.meta,
		data: s.data,
	}
	s.store.mu.Unlock()

	return nil
}

// ID returns the snapshot ID
func (s *InMemorySnapshotSink) ID() string {
	return s.id
}

// Cancel cancels the snapshot
func (s *InMemorySnapshotSink) Cancel() error {
	s.closed = true
	return nil
}

// InMemorySnapshotSource implements SnapshotSource
type InMemorySnapshotSource struct {
	data   []byte
	offset int
}

// Read reads data from the snapshot
func (s *InMemorySnapshotSource) Read(p []byte) (n int, err error) {
	if s.offset >= len(s.data) {
		return 0, io.EOF
	}

	n = len(p)
	if s.offset+n > len(s.data) {
		n = len(s.data) - s.offset
	}

	copy(p, s.data[s.offset:s.offset+n])
	s.offset += n
	return n, nil
}

// Close closes the snapshot source
func (s *InMemorySnapshotSource) Close() error {
	return nil
}
