package raft

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// StorageFSM implements the FSM interface using CobaltDB
// The sacred records inscribed on the tablets of the Necropolis
type StorageFSM struct {
	mu    sync.RWMutex
	store Storage
	index uint64

	// Application hooks, guarded by hookMu (see SetCommandHandler,
	// SetApplyObserver, SetRestoreObserver).
	hookMu          sync.RWMutex
	handlers        map[string]CommandHandler
	batchHandlers   map[string]BatchCommandHandler
	observer        func(cmd core.FSMCommand)
	restoreObserver func()
	logger          *slog.Logger
}

// CommandHandler applies a command whose Table needs application logic beyond
// a keyed write -- a judgment also folds into its time-series summaries. It
// runs under the FSM lock on every node, in log order, and must be
// deterministic and idempotent: a forwarded command can be applied twice.
type CommandHandler func(cmd *core.FSMCommand) error

// BatchCommandHandler applies consecutive commands of one table from a
// replicated batch together. It skips (and logs) a command that can never be
// applied, returns the commands it applied, and returns an error only for a
// failure a later attempt may overcome; the run is then retried whole, so it
// must be idempotent like CommandHandler.
type BatchCommandHandler func(cmds []core.FSMCommand) ([]core.FSMCommand, error)

// SetBatchCommandHandler adds a batch route for table, used for runs of two
// or more commands inside a batch; single commands still go to the table's
// CommandHandler. A nil h removes the route.
func (f *StorageFSM) SetBatchCommandHandler(table string, h BatchCommandHandler) {
	f.hookMu.Lock()
	defer f.hookMu.Unlock()
	if f.batchHandlers == nil {
		f.batchHandlers = make(map[string]BatchCommandHandler)
	}
	if h == nil {
		delete(f.batchHandlers, table)
		return
	}
	f.batchHandlers[table] = h
}

// SetCommandHandler routes commands whose Table equals table to h instead of
// the store. A nil h removes the route.
func (f *StorageFSM) SetCommandHandler(table string, h CommandHandler) {
	f.hookMu.Lock()
	defer f.hookMu.Unlock()
	if f.handlers == nil {
		f.handlers = make(map[string]CommandHandler)
	}
	if h == nil {
		delete(f.handlers, table)
		return
	}
	f.handlers[table] = h
}

// SetApplyObserver registers fn to be told about every command applied to the
// store (each element of a batch individually), after the FSM lock is
// released. It runs on the Raft apply path, so it must not block.
func (f *StorageFSM) SetApplyObserver(fn func(cmd core.FSMCommand)) {
	f.hookMu.Lock()
	f.observer = fn
	f.hookMu.Unlock()
}

// SetRestoreObserver registers fn to be called after a snapshot replaced the
// whole keyspace, which no per-command observation describes.
func (f *StorageFSM) SetRestoreObserver(fn func()) {
	f.hookMu.Lock()
	f.restoreObserver = fn
	f.hookMu.Unlock()
}

// SetLogger sets the logger used to report batch elements that failed to apply.
func (f *StorageFSM) SetLogger(logger *slog.Logger) {
	f.hookMu.Lock()
	f.logger = logger
	f.hookMu.Unlock()
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
	applied, result := f.apply(log)
	// Only commands that were durably applied may reach the observer. A batch
	// that fails part-way on a transient error leaves the applied index
	// unadvanced, so its successful prefix is replayed by the next attempt;
	// notifying for that prefix now would deliver the same command twice and
	// describe a state the FSM never committed to. An entry that was consumed
	// -- applied, or carried past a command that can never be applied -- is
	// reported exactly once.
	if result == nil && len(applied) > 0 {
		f.hookMu.RLock()
		observer := f.observer
		f.hookMu.RUnlock()
		if observer != nil {
			for _, cmd := range applied {
				observer(cmd)
			}
		}
	}
	return result
}

func (f *StorageFSM) apply(log *core.RaftLogEntry) ([]core.FSMCommand, interface{}) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Skip if already applied
	if log.Index <= f.index {
		return nil, nil
	}

	switch log.Type {
	case core.LogCommand:
		return f.applyCommand(log)
	case core.LogConfiguration:
		return nil, f.applyConfiguration(log)
	case core.LogNoOp:
		f.index = log.Index
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown log entry type: %d", log.Type)
	}
}

// fsmCommandApplier is the optional interface that Storage implementations
// can implement to handle FSM commands with proper secondary-index updates.
type fsmCommandApplier interface {
	ApplyFSMCommand(cmd *core.FSMCommand) error
}

// applicationStateReplacer is implemented by stores that can replace every
// non-"raft/" key with a snapshot image in batched writes.
type applicationStateReplacer interface {
	ReplaceApplicationState(data map[string][]byte) error
}

// durableState is implemented by stores whose contents survive a restart.
type durableState interface {
	DurableState() bool
}

// StateIsDurable reports whether the FSM's state survives a restart. A node
// then need not rebuild it from its snapshot when it already applied past it.
func (f *StorageFSM) StateIsDurable() bool {
	d, ok := f.store.(durableState)
	return ok && d.DurableState()
}

// fsmSetBatcher is implemented by stores that can apply several FSMSet
// commands as one durable write. CobaltDB writes them as one WAL frame, so a
// replicated batch of judgments costs one fsync instead of one per command.
// The result must equal applying the commands in order with ApplyFSMCommand.
type fsmSetBatcher interface {
	ApplyFSMSets(cmds []core.FSMCommand) error
}

// indexRebuilder is the optional interface a Storage implementation can
// implement when it keeps in-memory secondary indexes. Restore replaces the
// keyspace with raw Set calls, which bypass those indexes, so it must be
// refreshed afterwards or index-backed lookups report NotFound for data that
// is durably present.
type indexRebuilder interface {
	RebuildIndexes()
}

// applyCommand applies a command log entry and returns the commands that
// reached the store, for the apply observer.
func (f *StorageFSM) applyCommand(log *core.RaftLogEntry) ([]core.FSMCommand, interface{}) {
	cmd, err := f.decodeCommand(log.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode command: %w", err)
	}

	if cmd.Op != core.FSMBatch {
		if err := f.applyOne(cmd); err != nil {
			return nil, err
		}
		f.index = log.Index
		return []core.FSMCommand{*cmd}, nil
	}

	var batch []core.FSMCommand
	if err := json.Unmarshal(cmd.Value, &batch); err != nil {
		return nil, fmt.Errorf("failed to decode command batch: %w", err)
	}
	// A committed batch must remain retryable after a storage failure. Commands
	// are idempotent, so replaying its successful prefix is safe; advancing the
	// applied index past a failed element would permanently lose that write.
	// A deterministic failure is different: no attempt can ever apply it, so
	// the element is skipped and the entry is consumed, exactly as before the
	// retry path existed. Retrying it would wedge the node at this index.
	//
	// Runs of consecutive commands are applied together where possible: plain
	// sets through the store's fsmSetBatcher, commands of one table through
	// its batch handler. A replicated batch of judgments then costs one
	// durable write instead of one per judgment.
	for i := range batch {
		if batch[i].Origin == "" {
			batch[i].Origin = cmd.Origin
		}
	}
	applied := make([]core.FSMCommand, 0, len(batch))
	for i := 0; i < len(batch); {
		end, run := f.batchRun(batch, i)
		if end-i > 1 {
			done, err := run(batch[i:end])
			applied = append(applied, done...)
			if err != nil {
				return applied, fmt.Errorf("apply batch elements %d-%d at index %d: %w", i, end-1, log.Index, err)
			}
			i = end
			continue
		}
		sub := &batch[i]
		var subErr error
		if sub.Op == core.FSMBatch {
			subErr = fmt.Errorf("nested command batch")
		} else {
			subErr = f.applyOne(sub)
		}
		if subErr != nil {
			if isRetryableApplyError(subErr) {
				return applied, fmt.Errorf("apply batch element %d at index %d: %w", i, log.Index, subErr)
			}
			f.batchLogger().Error("Skipping batch element that failed to apply",
				"index", log.Index, "element", i, "table", sub.Table, "key", sub.Key, "error", subErr)
		} else {
			applied = append(applied, *sub)
		}
		i++
	}
	f.index = log.Index
	return applied, nil
}

// batchRun finds the run starting at batch[i] that can be applied together
// and how. A run of one is applied element by element.
func (f *StorageFSM) batchRun(batch []core.FSMCommand, i int) (int, func([]core.FSMCommand) ([]core.FSMCommand, error)) {
	if batch[i].Op == core.FSMBatch {
		return i + 1, nil
	}
	if batch[i].Table != "" {
		f.hookMu.RLock()
		h := f.batchHandlers[batch[i].Table]
		f.hookMu.RUnlock()
		if h == nil {
			return i + 1, nil
		}
		end := i + 1
		for end < len(batch) && batch[end].Table == batch[i].Table && batch[end].Op != core.FSMBatch {
			end++
		}
		return end, func(cmds []core.FSMCommand) ([]core.FSMCommand, error) {
			done, err := h(cmds)
			if err != nil {
				return done, retryable(err)
			}
			return done, nil
		}
	}
	batcher, ok := f.store.(fsmSetBatcher)
	if !ok || !f.plainSet(&batch[i]) {
		return i + 1, nil
	}
	end := i + 1
	for end < len(batch) && f.plainSet(&batch[end]) {
		end++
	}
	return end, func(cmds []core.FSMCommand) ([]core.FSMCommand, error) {
		if err := batcher.ApplyFSMSets(cmds); err != nil {
			return nil, retryable(err)
		}
		return cmds, nil
	}
}

// plainSet reports whether cmd is an FSMSet that no table handler owns.
func (f *StorageFSM) plainSet(cmd *core.FSMCommand) bool {
	if cmd.Op != core.FSMSet {
		return false
	}
	if cmd.Table == "" {
		return true
	}
	f.hookMu.RLock()
	defer f.hookMu.RUnlock()
	return f.handlers[cmd.Table] == nil
}

func (f *StorageFSM) batchLogger() *slog.Logger {
	f.hookMu.RLock()
	defer f.hookMu.RUnlock()
	if f.logger != nil {
		return f.logger
	}
	return slog.Default()
}

// applyOne applies a single (non-batch) command to the store.
//
// The error it returns says whether a later attempt may still succeed. A
// storage failure is transient, so it is marked retryable and the node
// retries the committed entry. A handler owns the meaning of its command and
// therefore the classification of its own failure: it returns a plain error
// for a deterministic rejection and wraps transient failures with Retryable.
func (f *StorageFSM) applyOne(cmd *core.FSMCommand) error {
	if cmd.Table != "" {
		f.hookMu.RLock()
		handler := f.handlers[cmd.Table]
		f.hookMu.RUnlock()
		if handler != nil {
			return handler(cmd)
		}
	}

	// Prefer the typed ApplyFSMCommand path when the store supports it.
	// This keeps secondary indexes consistent with the consensus log.
	if applier, ok := f.store.(fsmCommandApplier); ok {
		if err := applier.ApplyFSMCommand(cmd); err != nil {
			return retryable(err)
		}
		return nil
	}

	// Fallback: raw Set/Delete (does not update secondary indexes; used by
	// in-memory test stores where indexes are not needed).
	switch cmd.Op {
	case core.FSMSet:
		if err := f.store.Set(cmd.Key, cmd.Value); err != nil {
			return retryable(err)
		}
	case core.FSMDelete:
		if err := f.store.Delete(cmd.Key); err != nil {
			return retryable(err)
		}
	case core.FSMDeletePrefix:
		if err := f.store.DeletePrefix(cmd.Key); err != nil {
			return retryable(err)
		}
	default:
		return fmt.Errorf("unknown command op: %d", cmd.Op)
	}
	return nil
}

// applyConfiguration applies a configuration change
func (f *StorageFSM) applyConfiguration(log *core.RaftLogEntry) interface{} {
	// Configuration changes are handled by the Raft layer
	// We just persist them here
	key := fmt.Sprintf("raft/config/%d", log.Index)
	if err := f.store.Set(key, log.Data); err != nil {
		return retryable(err)
	}
	f.index = log.Index
	return nil
}

// decodeCommand decodes a command from bytes
func (f *StorageFSM) decodeCommand(data []byte) (*core.FSMCommand, error) {
	var cmd core.FSMCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		return nil, err
	}
	return &cmd, nil
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

	// Build an application-state snapshot. Raft's own log, hard-state, and
	// snapshot metadata live in the same embedded database but are not FSM
	// state; including them would make restore overwrite the consensus records
	// that authorize the restore itself.
	snapshot := make(map[string][]byte)
	for _, key := range keys {
		if strings.HasPrefix(key, "raft/") {
			continue
		}
		value, err := f.store.Get(key)
		if err != nil {
			return core.FSMCommand{}, fmt.Errorf("snapshot read key %q: %w", key, err)
		}
		snapshot[key] = value
	}

	// Serialize
	data, _ := json.Marshal(snapshot)

	return core.FSMCommand{
		Op:    core.FSMSet,
		Key:   "snapshot",
		Value: data,
	}, nil
}

// Restore restores the FSM from a snapshot
func (f *StorageFSM) Restore(snapshot []byte) error {
	if err := f.restore(snapshot); err != nil {
		return err
	}
	f.notifyRestored()
	return nil
}

func (f *StorageFSM) notifyRestored() {
	f.hookMu.RLock()
	observer := f.restoreObserver
	f.hookMu.RUnlock()
	if observer != nil {
		observer()
	}
}

func (f *StorageFSM) restore(snapshot []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Deserialize
	data := make(map[string][]byte)
	if err := json.Unmarshal(snapshot, &data); err != nil {
		return err
	}

	// Stores that can replace the image in batched writes do so: one fsync
	// per key took minutes for a real dataset.
	if r, ok := f.store.(applicationStateReplacer); ok {
		if err := r.ReplaceApplicationState(data); err != nil {
			return err
		}
		if ir, ok := f.store.(indexRebuilder); ok {
			ir.RebuildIndexes()
		}
		return nil
	}

	// Clear only application state. Raft metadata shares the database and must
	// remain intact while the FSM image is replaced.
	keys, err := f.store.List("")
	if err != nil {
		return err
	}
	for _, key := range keys {
		if strings.HasPrefix(key, "raft/") {
			continue
		}
		if err := f.store.Delete(key); err != nil {
			return err
		}
	}

	for key, value := range data {
		if strings.HasPrefix(key, "raft/") {
			continue
		}
		if err := f.store.Set(key, value); err != nil {
			return err
		}
	}

	// The writes above go through raw Set, so the store's in-memory secondary
	// indexes still describe the pre-restore keyspace. Refresh them, otherwise
	// every restored judgment is unreachable via GetJudgmentNoCtx and the
	// workspace index stays stale until the next process restart.
	if r, ok := f.store.(indexRebuilder); ok {
		r.RebuildIndexes()
	}

	return nil
}

// SetLastApplied records the snapshot boundary after a successful restore.
func (f *StorageFSM) SetLastApplied(index uint64) {
	f.mu.Lock()
	f.index = index
	f.mu.Unlock()
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
func (s *InMemoryLogStore) DeleteRange(minIdx, maxIdx uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if maxIdx >= uint64(len(s.entries)) {
		maxIdx = uint64(len(s.entries) - 1)
	}

	for i := minIdx; i <= maxIdx && i < uint64(len(s.entries)); i++ {
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

// InMemoryStableStore is an in-memory stable store for testing Raft durability.
type InMemoryStableStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// NewInMemoryStableStore creates a new in-memory stable store.
func NewInMemoryStableStore() *InMemoryStableStore {
	return &InMemoryStableStore{
		data: make(map[string][]byte),
	}
}

func (s *InMemoryStableStore) Get(key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.data[key]
	if !ok {
		return nil, &core.NotFoundError{Entity: "stable", ID: key}
	}
	return val, nil
}

func (s *InMemoryStableStore) Set(key string, val []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = val
	return nil
}

func (s *InMemoryStableStore) GetUint64(key string) (uint64, error) {
	val, err := s.Get(key)
	if err != nil {
		return 0, err
	}
	if len(val) != 8 {
		return 0, fmt.Errorf("invalid uint64 value for key %q: got %d bytes, want 8", key, len(val))
	}
	return binary.BigEndian.Uint64(val), nil
}

func (s *InMemoryStableStore) SetUint64(key string, val uint64) error {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, val)
	return s.Set(key, buf)
}
