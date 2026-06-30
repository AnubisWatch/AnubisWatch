package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// CobaltDB implements a B+Tree-based embedded storage engine
// optimized for time-series monitoring data with MVCC support.
type CobaltDB struct {
	path       string
	data       *btreeIndex
	wal        *writeAheadLog
	mu         sync.RWMutex
	logger     *slog.Logger
	closed     bool
	closedMu   sync.Mutex
	btreeOrder int        // Configurable B+Tree order
	encryptor  *encryptor // AES-256-GCM encryption (nil if disabled)

	// Secondary indexes for O(1) lookups by ID (replaces O(n) PrefixScan)
	soulIndex        map[string]string // soulID -> workspaceID
	judgmentIndex    map[string]string // judgmentID -> workspaceID
	channelIndex     map[string]string // channelID -> workspaceID
	ruleIndex        map[string]string // ruleID -> workspaceID
	journeyIndex     map[string]string // journeyID -> workspaceID
	incidentIndex    map[string]string // incidentID -> workspaceID
	statusPageIndex  map[string]string // statusPageID -> workspaceID
	dashboardIndex   map[string]string // dashboardID -> workspaceID
	maintenanceIndex map[string]string // maintenanceWindowID -> workspaceID
	alertEventIndex  map[string]string // alertEventID -> workspaceID

	// workspaceIndex tracks all known workspace IDs so cross-workspace
	// queries (ListStatusPages, ListDashboards, retention, GetStorageStats)
	// can iterate workspaces instead of scanning every key in the B+Tree.
	// Order of first appearance is preserved for deterministic listing.
	workspaceIndex map[string]struct{}
	workspaceOrder []string
}

// btreeIndex is an in-memory B+Tree index (simplified for Phase 1)
type btreeIndex struct {
	root       *btreeNode
	nextSeq    uint64
	mu         sync.RWMutex
	btreeOrder int // B+Tree order
}

// scanAllNodes returns all key-value pairs in the B+Tree
func (idx *btreeIndex) scanAllNodes() (map[string][]byte, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	result := make(map[string][]byte)

	// Find leftmost leaf node
	node := idx.root
	for !node.isLeaf {
		if len(node.children) == 0 {
			return result, nil
		}
		node = node.children[0]
	}

	// Scan through all leaf nodes
	for node != nil {
		for i, key := range node.keys {
			if node.values[i] != nil {
				result[key] = node.values[i]
			}
		}
		node = node.next
	}

	return result, nil
}

type btreeNode struct {
	isLeaf   bool
	keys     []string
	values   [][]byte
	children []*btreeNode
	next     *btreeNode // For leaf node chaining
}

const (
	// Default order of B+Tree (balanced for most workloads)
	defaultBTreeOrder = 32

	// Minimum B+Tree order
	minBTreeOrder = 4

	// Maximum B+Tree order
	maxBTreeOrder = 256
)

// writeAheadLog provides crash recovery
type writeAheadLog struct {
	path string
	file *os.File
	mu   sync.Mutex
}

// NewEngine creates a new CobaltDB storage engine
func NewEngine(config core.StorageConfig, logger *slog.Logger) (*CobaltDB, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}

	// Validate and apply B+Tree order configuration
	btreeOrder := defaultBTreeOrder
	if config.BTreeOrder > 0 {
		btreeOrder = config.BTreeOrder
		if btreeOrder < minBTreeOrder {
			btreeOrder = minBTreeOrder
			logger.Warn("B+Tree order too low, using minimum", "configured", config.BTreeOrder, "minimum", minBTreeOrder)
		} else if btreeOrder > maxBTreeOrder {
			btreeOrder = maxBTreeOrder
			logger.Warn("B+Tree order too high, using maximum", "configured", config.BTreeOrder, "maximum", maxBTreeOrder)
		}
	}

	// Ensure data directory exists (0700 = owner read/write/execute only)
	if err := os.MkdirAll(config.Path, 0700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Initialize WAL
	walPath := filepath.Join(config.Path, "wal.log")
	wal, err := newWAL(walPath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize WAL: %w", err)
	}

	// Initialize B+Tree index with configured order
	index := &btreeIndex{
		root: &btreeNode{
			isLeaf: true,
			keys:   make([]string, 0),
			values: make([][]byte, 0),
		},
		btreeOrder: btreeOrder,
	}

	// Initialize encryption if configured
	var enc *encryptor
	if config.Encryption.Enabled && config.Encryption.Key != "" {
		var err error
		enc, err = newEncryptor(config.Encryption.Key)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize encryption: %w", err)
		}
		logger.Info("Encryption enabled (AES-256-GCM)")
	}

	db := &CobaltDB{
		path:       config.Path,
		data:       index,
		wal:        wal,
		logger:     logger.With("component", "cobaltdb"),
		btreeOrder: btreeOrder,
		encryptor:  enc,
		// Initialize secondary indexes for O(1) lookups
		soulIndex:        make(map[string]string),
		judgmentIndex:    make(map[string]string),
		channelIndex:     make(map[string]string),
		ruleIndex:        make(map[string]string),
		journeyIndex:     make(map[string]string),
		incidentIndex:    make(map[string]string),
		statusPageIndex:  make(map[string]string),
		dashboardIndex:   make(map[string]string),
		maintenanceIndex: make(map[string]string),
		alertEventIndex:  make(map[string]string),
		workspaceIndex:   make(map[string]struct{}),
		workspaceOrder:   nil,
	}

	// Recover from WAL
	if err := db.recoverFromWAL(); err != nil {
		logger.Warn("WAL recovery failed, starting fresh", "err", err)
	}

	// Rebuild secondary indexes from existing data
	if err := db.rebuildSecondaryIndexes(); err != nil {
		logger.Warn("Secondary index rebuild failed", "err", err)
	}

	logger.Info("CobaltDB initialized", "path", config.Path, "btree_order", btreeOrder)
	return db, nil
}

// Close shuts down the storage engine
func (db *CobaltDB) Close() error {
	db.closedMu.Lock()
	if db.closed {
		db.closedMu.Unlock()
		return nil
	}
	db.closed = true
	db.closedMu.Unlock()

	db.mu.Lock()
	defer db.mu.Unlock()

	// Sync WAL
	if db.wal != nil {
		db.wal.Close()
	}

	db.logger.Info("CobaltDB closed")
	return nil
}

// Get retrieves a value by key
func (db *CobaltDB) Get(key string) ([]byte, error) {
	db.closedMu.Lock()
	if db.closed {
		db.closedMu.Unlock()
		return nil, fmt.Errorf("database is closed")
	}
	db.closedMu.Unlock()

	db.data.mu.RLock()
	defer db.data.mu.RUnlock()

	node := db.data.root
	for !node.isLeaf {
		idx := findChildIndex(node.keys, key)
		if idx >= len(node.children) {
			return nil, &core.NotFoundError{Entity: "key", ID: key}
		}
		node = node.children[idx]
	}

	idx := findKeyIndex(node.keys, key)
	if idx >= len(node.keys) || node.keys[idx] != key {
		return nil, &core.NotFoundError{Entity: "key", ID: key}
	}

	value := node.values[idx]

	// Decrypt value if encryption is enabled
	if db.encryptor != nil {
		decrypted, err := db.encryptor.decrypt(value)
		if err != nil {
			return nil, fmt.Errorf("decryption failed: %w", err)
		}
		return decrypted, nil
	}

	return value, nil
}

// Put stores a key-value pair
func (db *CobaltDB) Put(key string, value []byte) error {
	db.closedMu.Lock()
	if db.closed {
		db.closedMu.Unlock()
		return fmt.Errorf("database is closed")
	}
	db.closedMu.Unlock()

	// Encrypt value if encryption is enabled
	storeValue := value
	if db.encryptor != nil {
		encrypted, err := db.encryptor.encrypt(value)
		if err != nil {
			return fmt.Errorf("encryption failed: %w", err)
		}
		storeValue = encrypted
	}

	// Write to WAL first
	if err := db.wal.Append(key, storeValue); err != nil {
		return fmt.Errorf("WAL append failed: %w", err)
	}

	db.data.mu.Lock()
	defer db.data.mu.Unlock()

	// Insert into B+Tree
	if err := db.data.insert(key, storeValue); err != nil {
		return err
	}

	db.data.nextSeq++
	return nil
}

// Delete removes a key-value pair
func (db *CobaltDB) Delete(key string) error {
	db.closedMu.Lock()
	if db.closed {
		db.closedMu.Unlock()
		return fmt.Errorf("database is closed")
	}
	db.closedMu.Unlock()

	// Write delete marker to WAL
	if err := db.wal.AppendDelete(key); err != nil {
		return fmt.Errorf("WAL append failed: %w", err)
	}

	db.data.mu.Lock()
	defer db.data.mu.Unlock()

	// Mark as deleted (empty value means deleted in this simplified version)
	return db.data.insert(key, nil)
}

// Set stores a key-value pair (alias for Put to satisfy raft.Storage interface)
func (db *CobaltDB) Set(key string, value []byte) error {
	return db.Put(key, value)
}

// DeletePrefix removes all key-value pairs with the given prefix
func (db *CobaltDB) DeletePrefix(prefix string) error {
	db.closedMu.Lock()
	if db.closed {
		db.closedMu.Unlock()
		return fmt.Errorf("database is closed")
	}
	db.closedMu.Unlock()

	// Get all keys with prefix
	keys, err := db.List(prefix)
	if err != nil {
		return err
	}

	// Delete each key
	for _, key := range keys {
		if err := db.Delete(key); err != nil {
			return err
		}
	}

	return nil
}

// List returns all keys with the given prefix
func (db *CobaltDB) List(prefix string) ([]string, error) {
	db.closedMu.Lock()
	if db.closed {
		db.closedMu.Unlock()
		return nil, fmt.Errorf("database is closed")
	}
	db.closedMu.Unlock()

	db.data.mu.RLock()
	defer db.data.mu.RUnlock()

	var keys []string
	if db.data.root == nil {
		return keys, nil
	}

	// Traverse down to find the leaf node where the prefix could exist
	node := db.data.root
	for !node.isLeaf {
		idx := findChildIndex(node.keys, prefix)
		if idx >= len(node.children) {
			if len(node.children) > 0 {
				node = node.children[len(node.children)-1]
			} else {
				return keys, nil
			}
		} else {
			node = node.children[idx]
		}
	}

	// Scan through leaf nodes starting from this node
	startIdx := findKeyIndex(node.keys, prefix)
	isFirstNode := true
	for node != nil {
		keysToScan := node.keys
		valuesToScan := node.values
		if isFirstNode {
			keysToScan = node.keys[startIdx:]
			valuesToScan = node.values[startIdx:]
			isFirstNode = false
		}
		for i, key := range keysToScan {
			if strings.HasPrefix(key, prefix) {
				if valuesToScan[i] != nil {
					keys = append(keys, key)
				}
			} else if key > prefix {
				// Since keys are sorted, once we see a key > prefix that doesn't start with prefix,
				// there can be no more matches.
				return keys, nil
			}
		}
		node = node.next
	}
	return keys, nil
}

func (db *CobaltDB) PrefixScan(prefix string) (map[string][]byte, error) {
	db.closedMu.Lock()
	if db.closed {
		db.closedMu.Unlock()
		return nil, fmt.Errorf("database is closed")
	}
	db.closedMu.Unlock()

	db.data.mu.RLock()
	defer db.data.mu.RUnlock()

	result := make(map[string][]byte)

	if db.data.root == nil {
		return result, nil
	}

	// Traverse down to find the leaf node where the prefix could exist
	node := db.data.root
	for !node.isLeaf {
		idx := findChildIndex(node.keys, prefix)
		if idx >= len(node.children) {
			if len(node.children) > 0 {
				node = node.children[len(node.children)-1]
			} else {
				return result, nil
			}
		} else {
			node = node.children[idx]
		}
	}

	// Scan through leaf nodes starting from this node
	startIdx := findKeyIndex(node.keys, prefix)
	isFirstNode := true
	for node != nil {
		keysToScan := node.keys
		valuesToScan := node.values
		if isFirstNode {
			keysToScan = node.keys[startIdx:]
			valuesToScan = node.values[startIdx:]
			isFirstNode = false
		}
		for i, key := range keysToScan {
			if strings.HasPrefix(key, prefix) {
				if valuesToScan[i] != nil {
					result[key] = valuesToScan[i]
				}
			} else if key > prefix {
				// Since keys are sorted, once we see a key > prefix that doesn't start with prefix,
				// there can be no more matches.
				return result, nil
			}
		}
		node = node.next
	}

	return result, nil
}

// RangeScan returns all key-value pairs in the given range [start, end)
func (db *CobaltDB) RangeScan(start, end string) (map[string][]byte, error) {
	db.closedMu.Lock()
	if db.closed {
		db.closedMu.Unlock()
		return nil, fmt.Errorf("database is closed")
	}
	db.closedMu.Unlock()

	db.data.mu.RLock()
	defer db.data.mu.RUnlock()

	result := make(map[string][]byte)

	if db.data.root == nil {
		return result, nil
	}

	// Traverse down to find the leaf node where start could exist
	node := db.data.root
	for !node.isLeaf {
		idx := findChildIndex(node.keys, start)
		if idx >= len(node.children) {
			if len(node.children) > 0 {
				node = node.children[len(node.children)-1]
			} else {
				return result, nil
			}
		} else {
			node = node.children[idx]
		}
	}

	// Scan through leaf nodes starting from this node
	for node != nil {
		for i, key := range node.keys {
			if key >= start {
				if key >= end {
					return result, nil
				}
				if node.values[i] != nil {
					result[key] = node.values[i]
				}
			}
		}
		node = node.next
	}

	return result, nil
}

// B+Tree operations

func (idx *btreeIndex) insert(key string, value []byte) error {
	// If root is full, split it
	if len(idx.root.keys) >= idx.btreeOrder-1 {
		newRoot := &btreeNode{
			isLeaf:   false,
			children: []*btreeNode{idx.root},
		}
		newRoot.splitChild(0, idx.btreeOrder)
		idx.root = newRoot
	}

	idx.root.insertNonFull(key, value, idx.btreeOrder)
	return nil
}

func (n *btreeNode) splitChild(idx int, order int) {
	child := n.children[idx]

	// Create new node
	newNode := &btreeNode{
		isLeaf: child.isLeaf,
		keys:   make([]string, 0, order-1),
		values: make([][]byte, 0, order-1),
	}

	if !child.isLeaf {
		newNode.children = make([]*btreeNode, 0, order)
	}

	// Move second half to new node
	mid := (order - 1) / 2
	newNode.keys = append(newNode.keys, child.keys[mid+1:]...)

	if child.isLeaf {
		newNode.values = append(newNode.values, child.values[mid+1:]...)
		child.values = child.values[:mid+1]
		newNode.next = child.next
		child.next = newNode
	} else {
		newNode.children = append(newNode.children, child.children[mid+1:]...)
		child.children = child.children[:mid+1]
	}

	// Move middle key to parent
	n.keys = insertString(n.keys, idx, child.keys[mid])
	child.keys = child.keys[:mid]

	// Insert new node as sibling
	n.children = insertNode(n.children, idx+1, newNode)
}

func (n *btreeNode) insertNonFull(key string, value []byte, order int) {
	if n.isLeaf {
		// Insert into leaf
		idx := findKeyIndex(n.keys, key)
		if idx < len(n.keys) && n.keys[idx] == key {
			// Update existing key
			n.values[idx] = value
		} else {
			n.keys = insertString(n.keys, idx, key)
			n.values = insertBytes(n.values, idx, value)
		}
	} else {
		// Find child to descend
		idx := findChildIndex(n.keys, key)

		// Split child if full
		if len(n.children[idx].keys) >= order-1 {
			n.splitChild(idx, order)
			if key > n.keys[idx] {
				idx++
			}
		}

		n.children[idx].insertNonFull(key, value, order)
	}
}

// Helper functions

func findKeyIndex(keys []string, key string) int {
	return sort.Search(len(keys), func(i int) bool {
		return keys[i] >= key
	})
}

func findChildIndex(keys []string, key string) int {
	return sort.Search(len(keys), func(i int) bool {
		return key < keys[i]
	})
}

func insertString(slice []string, idx int, val string) []string {
	result := make([]string, len(slice)+1)
	copy(result[:idx], slice[:idx])
	result[idx] = val
	copy(result[idx+1:], slice[idx:])
	return result
}

func insertBytes(slice [][]byte, idx int, val []byte) [][]byte {
	result := make([][]byte, len(slice)+1)
	copy(result[:idx], slice[:idx])
	result[idx] = val
	copy(result[idx+1:], slice[idx:])
	return result
}

func insertNode(slice []*btreeNode, idx int, val *btreeNode) []*btreeNode {
	result := make([]*btreeNode, len(slice)+1)
	copy(result[:idx], slice[:idx])
	result[idx] = val
	copy(result[idx+1:], slice[idx:])
	return result
}

// WAL operations

func newWAL(path string) (*writeAheadLog, error) {
	// G302: WAL contains committed judgments and may carry
	// sensitive verdicts; 0600 keeps multi-user hosts from
	// peeking at the on-disk log via /proc/<pid>/fd races.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}

	return &writeAheadLog{
		path: path,
		file: f,
	}, nil
}

func (w *writeAheadLog) Append(key string, value []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	entry := walEntry{
		Op:    "PUT",
		Key:   key,
		Value: value,
		Time:  time.Now().UnixNano(),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	// Write length-prefixed entry
	length := make([]byte, 4)
	length[0] = byte(len(data) >> 24 & 0xff)
	length[1] = byte(len(data) >> 16 & 0xff)
	length[2] = byte(len(data) >> 8 & 0xff)
	length[3] = byte(len(data) & 0xff)

	if _, err := w.file.Write(length); err != nil {
		return err
	}
	if _, err := w.file.Write(data); err != nil {
		return err
	}

	return w.file.Sync()
}

func (w *writeAheadLog) AppendDelete(key string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	entry := walEntry{
		Op:   "DELETE",
		Key:  key,
		Time: time.Now().UnixNano(),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	length := make([]byte, 4)
	length[0] = byte(len(data) >> 24 & 0xff)
	length[1] = byte(len(data) >> 16 & 0xff)
	length[2] = byte(len(data) >> 8 & 0xff)
	length[3] = byte(len(data) & 0xff)

	if _, err := w.file.Write(length); err != nil {
		return err
	}
	if _, err := w.file.Write(data); err != nil {
		return err
	}

	return w.file.Sync()
}

func (w *writeAheadLog) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}

// Truncate resets the WAL file to empty (called after recovery)
func (w *writeAheadLog) Truncate() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.file.Truncate(0); err != nil {
		return err
	}
	_, err := w.file.Seek(0, io.SeekStart)
	return err
}

type walEntry struct {
	Op    string `json:"op"`
	Key   string `json:"key"`
	Value []byte `json:"value,omitempty"`
	Time  int64  `json:"time"`
}

func (db *CobaltDB) recoverFromWAL() error {
	// Lock ordering: acquire wal.mu first, then data.mu to avoid deadlock
	// This matches the order in Put() which does wal.Append() first, then data.mu
	db.wal.mu.Lock()
	defer db.wal.mu.Unlock()

	db.data.mu.Lock()
	defer db.data.mu.Unlock()

	// Seek to beginning for reading
	if _, err := db.wal.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek WAL: %w", err)
	}

	// Read entries
	buf := make([]byte, 4)
	for {
		// Read length prefix — use io.ReadFull to ensure we get all 4 bytes
		if _, err := io.ReadFull(db.wal.file, buf); err != nil {
			if err == io.EOF {
				break // Normal end of WAL
			}
			return fmt.Errorf("failed to read WAL entry length: %w", err)
		}

		length := int(buf[0])<<24 | int(buf[1])<<16 | int(buf[2])<<8 | int(buf[3])
		if length <= 0 || length > 1024*1024 {
			return fmt.Errorf("invalid WAL entry length: %d", length)
		}

		// Read entry data
		entryBuf := make([]byte, length)
		if _, err := io.ReadFull(db.wal.file, entryBuf); err != nil {
			return fmt.Errorf("failed to read WAL entry body: %w", err)
		}

		var entry walEntry
		if err := json.Unmarshal(entryBuf, &entry); err != nil {
			return err
		}

		// Replay operation
		switch entry.Op {
		case "PUT":
			value := entry.Value
			// If encryption is enabled, try to decrypt WAL entries (they were stored encrypted).
			// If decryption fails, store raw value (pre-encryption migration).
			if db.encryptor != nil && db.encryptor.isEncrypted(value) {
				if decrypted, err := db.encryptor.decrypt(value); err == nil {
					value = decrypted
				} else {
					// Log warning but continue with raw value - this handles
					// the case where WAL was written before encryption was enabled
					if db.logger != nil {
						db.logger.Warn("WAL entry decryption failed, storing raw value",
							"key", entry.Key,
							"error", err)
					}
				}
			}
			db.data.insert(entry.Key, value)
		case "DELETE":
			db.data.insert(entry.Key, nil)
		}
	}

	// Truncate WAL after successful recovery — entries have been replayed.
	// On Windows, File.Truncate may fail with "Access denied" when the file
	// was opened with O_APPEND. Close, remove, and recreate to work around this.
	walPath := db.wal.file.Name()
	walFile := db.wal.file
	if err := walFile.Close(); err != nil {
		return fmt.Errorf("failed to close WAL before truncate: %w", err)
	}
	if err := os.Remove(walPath); err != nil {
		return fmt.Errorf("failed to remove WAL after recovery: %w", err)
	}
	// G302: see newWAL above — same rationale.
	newFile, err := os.OpenFile(walPath, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("failed to recreate WAL: %w", err)
	}
	db.wal.file = newFile

	return nil
}

// Storage interface implementations for AnubisWatch types

// SaveSoul saves a soul to storage
func (db *CobaltDB) SaveSoul(ctx context.Context, soul *core.Soul) error {
	workspaceID := soul.WorkspaceID
	if workspaceID == "" {
		workspaceID = "default"
	}
	key := fmt.Sprintf("%s/souls/%s", workspaceID, soul.ID)

	data, err := json.Marshal(soul)
	if err != nil {
		return fmt.Errorf("failed to marshal soul: %w", err)
	}

	if err := db.Put(key, data); err != nil {
		return err
	}

	// Update secondary index for O(1) GetSoulNoCtx lookup.
	// db.mu protects soulIndex + workspaceOrder from concurrent mutation
	// (e.g. parallel SaveSoul calls racing with rebuildSecondaryIndexes).
	db.mu.Lock()
	db.soulIndex[soul.ID] = workspaceID
	db.recordWorkspaceLocked(workspaceID)
	db.mu.Unlock()
	return nil
}

// GetSoul retrieves a soul by ID
func (db *CobaltDB) GetSoul(ctx context.Context, workspaceID, soulID string) (*core.Soul, error) {
	key := fmt.Sprintf("%s/souls/%s", workspaceID, soulID)
	if workspaceID == "" {
		key = fmt.Sprintf("default/souls/%s", soulID)
	}

	data, err := db.Get(key)
	if err != nil {
		return nil, err
	}

	var soul core.Soul
	if err := json.Unmarshal(data, &soul); err != nil {
		return nil, fmt.Errorf("failed to unmarshal soul: %w", err)
	}

	return &soul, nil
}

// ListSouls returns all souls in a workspace with pagination
func (db *CobaltDB) ListSouls(ctx context.Context, workspaceID string, offset, limit int) ([]*core.Soul, error) {
	prefix := fmt.Sprintf("%s/souls/", workspaceID)
	if workspaceID == "" {
		prefix = "default/souls/"
	}

	results, err := db.PrefixScan(prefix)
	if err != nil {
		return nil, err
	}

	// Collect all souls first
	allSouls := make([]*core.Soul, 0, len(results))
	for _, data := range results {
		if data == nil {
			continue
		}
		var soul core.Soul
		if err := json.Unmarshal(data, &soul); err != nil {
			db.logger.Warn("failed to unmarshal soul", "err", err)
			continue
		}
		allSouls = append(allSouls, &soul)
	}

	// Apply pagination
	if offset < 0 {
		offset = 0
	}
	if offset >= len(allSouls) {
		return []*core.Soul{}, nil
	}

	end := offset + limit
	if limit <= 0 || end > len(allSouls) {
		end = len(allSouls)
	}

	return allSouls[offset:end], nil
}

// DeleteSoul removes a soul
func (db *CobaltDB) DeleteSoul(ctx context.Context, workspaceID, soulID string) error {
	key := fmt.Sprintf("%s/souls/%s", workspaceID, soulID)
	if workspaceID == "" {
		key = fmt.Sprintf("default/souls/%s", soulID)
	}
	return db.Delete(key)
}

// Stats operations

// GetStats returns statistics for a workspace
func (db *CobaltDB) GetStats(ctx context.Context, workspaceID string, start, end time.Time) (*core.Stats, error) {
	workspaceID = defaultWorkspace(workspaceID)
	ctx = core.ContextWithWorkspaceID(ctx, workspaceID)

	// Count souls
	souls, err := db.ListSouls(ctx, workspaceID, 0, 1000)
	if err != nil {
		return nil, err
	}

	// Calculate soul status counts
	aliveCount := 0
	deadCount := 0
	degradedCount := 0

	for _, soul := range souls {
		// Get latest judgment for each soul
		judgments, err := db.ListJudgments(ctx, soul.ID, start, end, 1)
		if err != nil || len(judgments) == 0 {
			deadCount++
			continue
		}
		switch judgments[0].Status {
		case core.SoulAlive:
			aliveCount++
		case core.SoulDead:
			deadCount++
		case core.SoulDegraded:
			degradedCount++
		}
	}

	// Count total judgments
	judgmentCount := int64(0)
	for _, soul := range souls {
		judgments, err := db.ListJudgments(ctx, soul.ID, start, end, 1000)
		if err != nil {
			continue
		}
		judgmentCount += int64(len(judgments))
	}

	return &core.Stats{
		TotalSouls:     len(souls),
		AliveSouls:     aliveCount,
		DeadSouls:      deadCount,
		DegradedSouls:  degradedCount,
		TotalJudgments: judgmentCount,
	}, nil
}

// Workspace operations

// GetWorkspace retrieves a workspace by ID
func (db *CobaltDB) GetWorkspace(ctx context.Context, id string) (*core.Workspace, error) {
	data, err := db.Get("workspaces/" + id)
	if err != nil {
		return nil, err
	}
	var ws core.Workspace
	if err := json.Unmarshal(data, &ws); err != nil {
		return nil, fmt.Errorf("failed to unmarshal workspace: %w", err)
	}
	return &ws, nil
}

// ListWorkspaces returns all workspaces
func (db *CobaltDB) ListWorkspaces(ctx context.Context) ([]*core.Workspace, error) {
	results, err := db.PrefixScan("workspaces/")
	if err != nil {
		return nil, err
	}

	workspaces := make([]*core.Workspace, 0, len(results))
	for _, data := range results {
		if data == nil {
			continue
		}
		var ws core.Workspace
		if err := json.Unmarshal(data, &ws); err != nil {
			db.logger.Warn("failed to unmarshal workspace", "err", err)
			continue
		}
		workspaces = append(workspaces, &ws)
	}
	return workspaces, nil
}

// SaveWorkspace saves a workspace
func (db *CobaltDB) SaveWorkspace(ctx context.Context, ws *core.Workspace) error {
	key := "workspaces/" + ws.ID
	data, err := json.Marshal(ws)
	if err != nil {
		return fmt.Errorf("failed to marshal workspace: %w", err)
	}
	return db.Put(key, data)
}

// DeleteWorkspace removes a workspace
func (db *CobaltDB) DeleteWorkspace(ctx context.Context, id string) error {
	return db.Delete("workspaces/" + id)
}

// REST API Storage interface wrappers (without context for simpler interface)

// GetSoulNoCtx retrieves a soul by ID using O(1) index lookup (REST API compatible)
// Formerly O(n) PrefixScan(""), now O(1) via secondary index.
// db.mu.RLock pairs with the writer in SaveSoul (engine.go) and
// rebuildSecondaryIndexes — both take db.mu.Lock when mutating the map.
func (db *CobaltDB) GetSoulNoCtx(id string) (*core.Soul, error) {
	db.mu.RLock()
	workspaceID, ok := db.soulIndex[id]
	db.mu.RUnlock()
	if !ok {
		return nil, &core.NotFoundError{Entity: "soul", ID: id}
	}

	key := fmt.Sprintf("%s/souls/%s", workspaceID, id)
	data, err := db.Get(key)
	if err != nil {
		return nil, err
	}

	var soul core.Soul
	if err := json.Unmarshal(data, &soul); err != nil {
		return nil, fmt.Errorf("failed to unmarshal soul: %w", err)
	}
	return &soul, nil
}

// ListSoulsNoCtx lists souls for REST API (no context)
func (db *CobaltDB) ListSoulsNoCtx(workspace string, offset, limit int) ([]*core.Soul, error) {
	return db.ListSouls(context.Background(), workspace, offset, limit)
}

// GetJudgmentNoCtx retrieves a judgment by ID using O(1) index lookup
// Formerly O(n) PrefixScan(""), now O(1) via secondary index.
// db.mu.RLock pairs with the writer in SaveJudgment (judgments.go)
// and rebuildSecondaryIndexes — both take db.mu.Lock when mutating
// the map.
func (db *CobaltDB) GetJudgmentNoCtx(id string) (*core.Judgment, error) {
	// Look up the index key from our secondary index
	db.mu.RLock()
	idxKey, ok := db.judgmentIndex[id]
	db.mu.RUnlock()
	if !ok {
		return nil, &core.NotFoundError{Entity: "judgment", ID: id}
	}

	// Get the primary key from the index
	idxData, err := db.Get(idxKey)
	if err != nil {
		return nil, err
	}
	primaryKey := string(idxData)

	// Get the judgment data using the primary key
	judgmentData, err := db.Get(primaryKey)
	if err != nil {
		return nil, err
	}

	var j core.Judgment
	if err := json.Unmarshal(judgmentData, &j); err != nil {
		return nil, fmt.Errorf("failed to unmarshal judgment: %w", err)
	}
	return &j, nil
}

// ListJudgmentsNoCtx lists judgments in time range
func (db *CobaltDB) ListJudgmentsNoCtx(soulID string, start, end time.Time, limit int) ([]*core.Judgment, error) {
	workspaceID := "default"
	if soul, err := db.GetSoulNoCtx(soulID); err == nil {
		workspaceID = defaultWorkspace(soul.WorkspaceID)
	}
	ctx := core.ContextWithWorkspaceID(context.Background(), workspaceID)
	return db.ListJudgments(ctx, soulID, start, end, limit)
}

// GetChannelNoCtx retrieves a channel by ID
func (db *CobaltDB) GetChannelNoCtx(id string, workspace string) (*core.AlertChannel, error) {
	return db.GetAlertChannel(id, workspace)
}

// ListChannelsNoCtx lists channels
func (db *CobaltDB) ListChannelsNoCtx(workspace string) ([]*core.AlertChannel, error) {
	return db.ListAlertChannels(workspace)
}

// SaveChannelNoCtx saves a channel without context
func (db *CobaltDB) SaveChannelNoCtx(ch *core.AlertChannel) error {
	return db.SaveAlertChannel(ch)
}

// DeleteChannelNoCtx deletes a channel
func (db *CobaltDB) DeleteChannelNoCtx(id string, workspace string) error {
	return db.DeleteAlertChannel(id, workspace)
}

// GetRuleNoCtx retrieves a rule by ID
func (db *CobaltDB) GetRuleNoCtx(id string, workspace string) (*core.AlertRule, error) {
	return db.GetAlertRule(id, workspace)
}

// ListRulesNoCtx lists rules
func (db *CobaltDB) ListRulesNoCtx(workspace string) ([]*core.AlertRule, error) {
	return db.ListAlertRules(workspace)
}

// SaveRuleNoCtx saves a rule without context
func (db *CobaltDB) SaveRuleNoCtx(rule *core.AlertRule) error {
	return db.SaveAlertRule(rule)
}

// DeleteRuleNoCtx deletes a rule
func (db *CobaltDB) DeleteRuleNoCtx(id string, workspace string) error {
	return db.DeleteAlertRule(id, workspace)
}

// GetWorkspaceNoCtx retrieves a workspace
func (db *CobaltDB) GetWorkspaceNoCtx(id string) (*core.Workspace, error) {
	return db.GetWorkspace(context.Background(), id)
}

// ListWorkspacesNoCtx lists workspaces
func (db *CobaltDB) ListWorkspacesNoCtx() ([]*core.Workspace, error) {
	return db.ListWorkspaces(context.Background())
}

// SaveWorkspaceNoCtx saves a workspace
func (db *CobaltDB) SaveWorkspaceNoCtx(ws *core.Workspace) error {
	return db.SaveWorkspace(context.Background(), ws)
}

// DeleteWorkspaceNoCtx deletes a workspace
func (db *CobaltDB) DeleteWorkspaceNoCtx(id string) error {
	return db.DeleteWorkspace(context.Background(), id)
}

// GetStatsNoCtx gets stats without context
func (db *CobaltDB) GetStatsNoCtx(workspace string, start, end time.Time) (*core.Stats, error) {
	return db.GetStats(context.Background(), workspace, start, end)
}

// GetSoulJudgments retrieves recent judgments for a soul (for status page)
func (db *CobaltDB) GetSoulJudgments(soulID string, limit int) ([]core.Judgment, error) {
	workspaceID := "default"
	if soul, err := db.GetSoulNoCtx(soulID); err == nil {
		workspaceID = defaultWorkspace(soul.WorkspaceID)
	}

	keyPrefix := fmt.Sprintf("%s/judgments/%s/", workspaceID, soulID)
	results, err := db.PrefixScan(keyPrefix)
	if err != nil {
		return nil, err
	}

	judgments := make([]core.Judgment, 0, len(results))
	for _, data := range results {
		if data == nil {
			continue
		}
		var judgment core.Judgment
		if err := json.Unmarshal(data, &judgment); err != nil {
			db.logger.Warn("failed to unmarshal judgment", "err", err)
			continue
		}
		judgments = append(judgments, judgment)
	}

	sort.Slice(judgments, func(i, j int) bool {
		return judgments[i].Timestamp.After(judgments[j].Timestamp)
	})

	if limit > 0 && len(judgments) > limit {
		judgments = judgments[:limit]
	}

	return judgments, nil
}

// StatusPage NoCtx wrappers

func (db *CobaltDB) GetStatusPageNoCtx(id string) (*core.StatusPage, error) {
	return db.GetStatusPage(id)
}

func (db *CobaltDB) ListStatusPagesNoCtx() ([]*core.StatusPage, error) {
	return db.ListStatusPages()
}

func (db *CobaltDB) SaveStatusPageNoCtx(page *core.StatusPage) error {
	return db.SaveStatusPage(page)
}

func (db *CobaltDB) DeleteStatusPageNoCtx(id string) error {
	return db.DeleteStatusPage(id)
}

func (db *CobaltDB) SaveDashboardNoCtx(dashboard *core.CustomDashboard) error {
	return db.SaveDashboard(dashboard)
}

func (db *CobaltDB) GetDashboardNoCtx(id string) (*core.CustomDashboard, error) {
	return db.GetDashboard(id)
}

func (db *CobaltDB) ListDashboardsNoCtx() ([]*core.CustomDashboard, error) {
	return db.ListDashboards()
}

func (db *CobaltDB) DeleteDashboardNoCtx(id string) error {
	return db.DeleteDashboard(id)
}

// rebuildSecondaryIndexes scans all existing data and populates the secondary
// indexes for O(1) lookups by ID. This is called once at startup.
func (db *CobaltDB) rebuildSecondaryIndexes() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Scan all keys and populate indexes based on key prefixes
	results, err := db.data.scanAllNodes()
	if err != nil {
		return fmt.Errorf("failed to scan all keys: %w", err)
	}

	for key := range results {
		// Parse key format: "workspaceID/<type>/resourceID[/...]"
		parts := strings.SplitN(key, "/", 3)
		if len(parts) < 2 {
			continue
		}
		workspaceID := parts[0]

		// Track every workspace we encounter so cross-workspace queries
		// (ListStatusPages, ListDashboards, retention, GetStorageStats)
		// don't need PrefixScan("").
		db.recordWorkspaceLocked(workspaceID)

		// Some keys (workspaces, system, raft) don't have a resourceID
		// after the type; the length-3 split yields the whole tail, which
		// is fine for the index — we just won't index them below.
		if len(parts) < 3 {
			continue
		}
		resourceType := parts[1]
		resourceID := parts[2]

		switch resourceType {
		case "souls":
			db.soulIndex[resourceID] = workspaceID
		case "judgment-idx":
			// Store judgmentID -> indexKey for O(1) lookup
			db.judgmentIndex[resourceID] = key
		case "channels":
			db.channelIndex[resourceID] = workspaceID
		case "rules":
			db.ruleIndex[resourceID] = workspaceID
		case "journeys":
			db.journeyIndex[resourceID] = workspaceID
		case "alerts":
			// alerts/<channel|rule|event|incident>/<id>[/...]
			// The third path segment tells us which sub-index to populate.
			subParts := strings.SplitN(resourceID, "/", 2)
			subType := subParts[0]
			subID := ""
			if len(subParts) == 2 {
				subID = subParts[1]
			}
			switch subType {
			case "channels":
				db.channelIndex[subID] = workspaceID
			case "rules":
				db.ruleIndex[subID] = workspaceID
			case "incidents":
				db.incidentIndex[subID] = workspaceID
			case "events":
				// events use a composite key: {soulID}/{tsNanos}/{eventID}
				if subID != "" {
					eventParts := strings.SplitN(subID, "/", 3)
					if len(eventParts) == 3 {
						db.alertEventIndex[eventParts[2]] = workspaceID
					}
				}
			}
		case "statuspages":
			// Skip statuspages/subscriptions/* — those are nested
			// resources of a status page, not a status page themselves.
			if !strings.HasPrefix(resourceID, "subscriptions/") {
				db.statusPageIndex[resourceID] = workspaceID
			}
		case "dashboards":
			db.dashboardIndex[resourceID] = workspaceID
		case "maintenance":
			db.maintenanceIndex[resourceID] = workspaceID
		}
	}

	return nil
}

// recordWorkspaceLocked records a workspace in workspaceIndex/workspaceOrder
// preserving first-seen order. Caller must hold db.mu.
func (db *CobaltDB) recordWorkspaceLocked(workspaceID string) {
	if workspaceID == "" {
		return
	}
	if _, ok := db.workspaceIndex[workspaceID]; ok {
		return
	}
	db.workspaceIndex[workspaceID] = struct{}{}
	db.workspaceOrder = append(db.workspaceOrder, workspaceID)
}

// ListWorkspaceIDs returns the known workspace IDs in first-seen order.
// Callers that previously did PrefixScan("") can iterate these instead
// and run a per-workspace PrefixScan, reducing the scan from O(totalKeys)
// to O(totalKeys) but partitioned into smaller, cache-friendly chunks
// (and skipping keys from unrelated resources entirely).
func (db *CobaltDB) ListWorkspaceIDs() []string {
	db.mu.RLock()
	defer db.mu.RUnlock()
	out := make([]string, len(db.workspaceOrder))
	copy(out, db.workspaceOrder)
	return out
}
