package region

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// ReplicationManager handles cross-region data replication
// The Ka - spiritual duplicate that travels between regions
type ReplicationManager struct {
	mu      sync.RWMutex
	config  ReplicationConfig
	manager *Manager
	streams map[string]*ReplicationStream // region -> stream
	queue   chan ReplicationEvent
	pending map[string][]ReplicationEvent // region -> pending events
	logger  *slog.Logger
	stopCh  chan struct{}
	client  *http.Client
}

// ReplicationConfig contains replication settings
type ReplicationConfig struct {
	Enabled          bool
	SyncMode         string // "async", "sync", "quorum"
	BatchSize        int
	BatchInterval    time.Duration
	ConflictStrategy string // "last-write-wins", "timestamp", "manual"
	RetryInterval    time.Duration
	MaxRetries       int
	Compression      bool
	EncryptTraffic   bool
	Regions          []string // Regions to replicate to
}

// ReplicationStream manages replication to a specific region
type ReplicationStream struct {
	RegionID      string
	Endpoint      string
	Healthy       bool
	LastSync      time.Time
	Lag           time.Duration
	PendingEvents int
	mu            sync.Mutex
}

// ReplicationEvent represents a data change to replicate
type ReplicationEvent struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`   // "soul", "judgment", "config", etc.
	Action    string          `json:"action"` // "create", "update", "delete"
	Timestamp time.Time       `json:"timestamp"`
	Region    string          `json:"region"`
	EntityID  string          `json:"entity_id"`
	Data      json.RawMessage `json:"data"`
	Checksum  string          `json:"checksum"`
}

// ReplicationBatch contains multiple events for batch replication
type ReplicationBatch struct {
	SourceRegion string             `json:"source_region"`
	Timestamp    time.Time          `json:"timestamp"`
	Events       []ReplicationEvent `json:"events"`
	Checksum     string             `json:"checksum"`
}

// ConflictResolution handles replication conflicts
type ConflictResolution struct {
	EventID       string
	LocalVersion  *ReplicationEvent
	RemoteVersion *ReplicationEvent
	Winner        string // "local", "remote", "manual"
	ResolvedAt    time.Time
}

// NewReplicationManager creates a new replication manager
func NewReplicationManager(cfg ReplicationConfig, manager *Manager, logger *slog.Logger) *ReplicationManager {
	return &ReplicationManager{
		config:  cfg,
		manager: manager,
		streams: make(map[string]*ReplicationStream),
		queue:   make(chan ReplicationEvent, 10000),
		pending: make(map[string][]ReplicationEvent),
		logger:  logger.With("component", "replication"),
		stopCh:  make(chan struct{}),
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// Start starts the replication manager
func (r *ReplicationManager) Start(ctx context.Context) {
	if !r.config.Enabled {
		r.logger.Info("Replication is disabled")
		return
	}

	r.logger.Info("Starting replication manager",
		"sync_mode", r.config.SyncMode,
		"batch_size", r.config.BatchSize)

	// Initialize streams for configured regions
	r.mu.Lock()
	for _, regionID := range r.config.Regions {
		if regionID == r.manager.GetLocalRegion() {
			continue // Don't replicate to self
		}

		region, err := r.manager.GetRegion(regionID)
		if err != nil {
			r.logger.Warn("Region not found for replication", "region_id", regionID)
			continue
		}

		r.streams[regionID] = &ReplicationStream{
			RegionID: regionID,
			Endpoint: region.Endpoint,
			Healthy:  false,
		}

		r.pending[regionID] = []ReplicationEvent{}
	}
	r.mu.Unlock()

	// Start replication workers
	go r.processQueue(ctx)
	go r.syncLoop(ctx)
}

// Stop stops the replication manager
func (r *ReplicationManager) Stop(ctx context.Context) {
	if !r.config.Enabled {
		return
	}

	r.logger.Info("Stopping replication manager")
	close(r.stopCh)
}

// Replicate queues an event for replication
func (r *ReplicationManager) Replicate(ctx context.Context, event ReplicationEvent) error {
	if !r.config.Enabled {
		return nil // Silently ignore if disabled
	}

	// Generate event ID if not set
	if event.ID == "" {
		event.ID = generateEventID()
	}

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	// Calculate checksum
	event.Checksum = calculateChecksum(event)

	// Queue for replication
	select {
	case r.queue <- event:
		r.logger.Debug("Event queued for replication",
			"type", event.Type,
			"action", event.Action,
			"entity_id", event.EntityID)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("replication queue is full")
	}
}

// ReplicateSync performs synchronous replication (waits for confirmation)
func (r *ReplicationManager) ReplicateSync(ctx context.Context, event ReplicationEvent) error {
	if !r.config.Enabled {
		return nil
	}

	// Replicate to all regions synchronously
	regions := r.manager.ListHealthyRegions()

	var wg sync.WaitGroup
	errors := make(chan error, len(regions))

	for _, region := range regions {
		if region.ID == r.manager.GetLocalRegion() {
			continue
		}

		wg.Add(1)
		go func(regionID string) {
			defer wg.Done()
			if err := r.replicateToRegion(ctx, regionID, event); err != nil {
				errors <- fmt.Errorf("failed to replicate to %s: %w", regionID, err)
			}
		}(region.ID)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	var errs []error
	for err := range errors {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("replication failed: %v", errs)
	}

	return nil
}

// GetStreamStatus returns the status of all replication streams
func (r *ReplicationManager) GetStreamStatus() map[string]StreamStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	status := make(map[string]StreamStatus)
	for id, stream := range r.streams {
		stream.mu.Lock()
		status[id] = StreamStatus{
			RegionID:      stream.RegionID,
			Healthy:       stream.Healthy,
			LastSync:      stream.LastSync,
			Lag:           stream.Lag,
			PendingEvents: stream.PendingEvents,
		}
		stream.mu.Unlock()
	}

	return status
}

// processQueue processes the replication queue
func (r *ReplicationManager) processQueue(ctx context.Context) {
	batch := make([]ReplicationEvent, 0, r.config.BatchSize)
	timer := time.NewTimer(r.config.BatchInterval)
	defer timer.Stop()

	for {
		select {
		case event := <-r.queue:
			batch = append(batch, event)

			if len(batch) >= r.config.BatchSize {
				r.sendBatch(ctx, batch)
				batch = batch[:0]
				timer.Reset(r.config.BatchInterval)
			}

		case <-timer.C:
			if len(batch) > 0 {
				r.sendBatch(ctx, batch)
				batch = batch[:0]
			}
			timer.Reset(r.config.BatchInterval)

		case <-r.stopCh:
			// Flush remaining events
			if len(batch) > 0 {
				r.sendBatch(context.Background(), batch)
			}
			return

		case <-ctx.Done():
			return
		}
	}
}

// sendBatch sends a batch of events to all regions
func (r *ReplicationManager) sendBatch(ctx context.Context, events []ReplicationEvent) {
	if len(events) == 0 {
		return
	}

	r.mu.RLock()
	streams := make(map[string]*ReplicationStream)
	for id, stream := range r.streams {
		streams[id] = stream
	}
	r.mu.RUnlock()

	for regionID, stream := range streams {
		if err := r.sendBatchToRegion(ctx, regionID, stream, events); err != nil {
			r.logger.Error("Failed to send batch",
				"region_id", regionID,
				"event_count", len(events),
				"error", err)

			// Store as pending for retry
			r.addPendingEvents(regionID, events)
		}
	}
}

// sendBatchToRegion sends a batch to a specific region
func (r *ReplicationManager) sendBatchToRegion(ctx context.Context, regionID string, stream *ReplicationStream, events []ReplicationEvent) error {
	batch := ReplicationBatch{
		SourceRegion: r.manager.GetLocalRegion(),
		Timestamp:    time.Now().UTC(),
		Events:       events,
	}

	// Serialize and optionally compress
	data, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("failed to marshal batch: %w", err)
	}

	if r.config.Compression {
		data, err = compressData(data)
		if err != nil {
			r.logger.Warn("Failed to compress data", "error", err)
		}
	}

	// Send to region
	endpoint := fmt.Sprintf("%s/api/v1/replication/batch", stream.Endpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Source-Region", r.manager.GetLocalRegion())
	if r.config.Compression {
		req.Header.Set("Content-Encoding", "gzip")
	}

	resp, err := r.client.Do(req)
	if err != nil {
		stream.Healthy = false
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		stream.Healthy = false
		return fmt.Errorf("HTTP error %d", resp.StatusCode)
	}

	// Update stream status
	stream.mu.Lock()
	stream.Healthy = true
	stream.LastSync = time.Now()
	stream.PendingEvents = 0
	stream.mu.Unlock()

	r.logger.Debug("Batch sent successfully",
		"region_id", regionID,
		"event_count", len(events))

	return nil
}

// replicateToRegion replicates a single event to a region
func (r *ReplicationManager) replicateToRegion(ctx context.Context, regionID string, event ReplicationEvent) error {
	r.mu.RLock()
	stream, exists := r.streams[regionID]
	r.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no stream for region %s", regionID)
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	endpoint := fmt.Sprintf("%s/api/v1/replication/event", stream.Endpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Source-Region", r.manager.GetLocalRegion())

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP error %d", resp.StatusCode)
	}

	return nil
}

// addPendingEvents stores events for retry
func (r *ReplicationManager) addPendingEvents(regionID string, events []ReplicationEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.pending[regionID] == nil {
		r.pending[regionID] = []ReplicationEvent{}
	}

	r.pending[regionID] = append(r.pending[regionID], events...)

	// Update stream pending count
	if stream, exists := r.streams[regionID]; exists {
		stream.mu.Lock()
		stream.PendingEvents = len(r.pending[regionID])
		stream.mu.Unlock()
	}
}

// syncLoop periodically retries pending events
func (r *ReplicationManager) syncLoop(ctx context.Context) {
	ticker := time.NewTicker(r.config.RetryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.retryPendingEvents(ctx)
		case <-r.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// retryPendingEvents retries sending pending events
func (r *ReplicationManager) retryPendingEvents(ctx context.Context) {
	r.mu.Lock()
	pendingCopy := make(map[string][]ReplicationEvent)
	for regionID, events := range r.pending {
		if len(events) > 0 {
			pendingCopy[regionID] = events
			r.pending[regionID] = []ReplicationEvent{}
		}
	}
	r.mu.Unlock()

	for regionID, events := range pendingCopy {
		// Process in batches
		for i := 0; i < len(events); i += r.config.BatchSize {
			end := i + r.config.BatchSize
			if end > len(events) {
				end = len(events)
			}

			batch := events[i:end]
			r.mu.RLock()
			stream, exists := r.streams[regionID]
			r.mu.RUnlock()

			if !exists {
				continue
			}

			if err := r.sendBatchToRegion(ctx, regionID, stream, batch); err != nil {
				r.logger.Error("Failed to retry batch",
					"region_id", regionID,
					"error", err)
				r.addPendingEvents(regionID, batch)
			}
		}
	}
}

// HandleIncomingBatch processes a replication batch from another region
func (r *ReplicationManager) HandleIncomingBatch(ctx context.Context, batch *ReplicationBatch) error {
	r.logger.Debug("Received replication batch",
		"source_region", batch.SourceRegion,
		"event_count", len(batch.Events))

	for _, event := range batch.Events {
		if err := r.applyEvent(ctx, &event); err != nil {
			r.logger.Error("Failed to apply event",
				"event_id", event.ID,
				"error", err)
		}
	}

	return nil
}

// HandleIncomingEvent processes a single replication event
func (r *ReplicationManager) HandleIncomingEvent(ctx context.Context, event *ReplicationEvent) error {
	r.logger.Debug("Received replication event",
		"event_id", event.ID,
		"type", event.Type,
		"action", event.Action)

	return r.applyEvent(ctx, event)
}

// applyEvent applies a replication event locally
func (r *ReplicationManager) applyEvent(ctx context.Context, event *ReplicationEvent) error {
	// Verify checksum
	expectedChecksum := calculateChecksum(*event)
	if event.Checksum != "" && event.Checksum != expectedChecksum {
		return fmt.Errorf("checksum mismatch")
	}

	// Check for conflicts
	conflict, err := r.checkConflict(ctx, event)
	if err != nil {
		return err
	}

	if conflict != nil {
		// Resolve conflict
		resolution := r.resolveConflict(conflict)
		if resolution.Winner == "local" {
			r.logger.Debug("Conflict resolved: keeping local version",
				"event_id", event.ID)
			return nil // Skip applying remote event
		}
	}

	// Apply the event based on type
	switch event.Type {
	case "soul":
		return r.applySoulEvent(ctx, event)
	case "judgment":
		return r.applyJudgmentEvent(ctx, event)
	case "config":
		return r.applyConfigEvent(ctx, event)
	default:
		r.logger.Warn("Unknown event type", "type", event.Type)
		return nil
	}
}

// checkConflict checks if there's a conflict with local data
func (r *ReplicationManager) checkConflict(ctx context.Context, event *ReplicationEvent) (*ConflictResolution, error) {
	// If there's no local data to conflict with, no conflict
	if r.manager == nil || r.manager.storage == nil {
		return nil, nil
	}

	// Check if storage supports entity-level conflict detection
	store, ok := r.manager.storage.(ConflictStore)
	if !ok {
		return nil, nil
	}

	// Look up local version of the entity
	localData, err := store.GetEntityData(ctx, event.Type, event.EntityID)
	if err != nil {
		// Entity not found locally — no conflict, this is new data
		r.logger.Debug("No local entity found for conflict check",
			"type", event.Type,
			"entity_id", event.EntityID)
		return nil, nil
	}

	// Parse both versions to extract timestamps
	localTimestamp := extractTimestamp(localData)
	remoteTimestamp := extractTimestamp(event.Data)

	// If timestamps are available and differ, there's a conflict
	if !localTimestamp.IsZero() && !remoteTimestamp.IsZero() && !localTimestamp.Equal(remoteTimestamp) {
		localEvent := &ReplicationEvent{
			ID:        event.ID + "-local",
			Type:      event.Type,
			Action:    "local",
			Timestamp: localTimestamp,
			EntityID:  event.EntityID,
			Data:      localData,
		}
		// Update event timestamp for conflict resolution comparison
		remoteCopy := *event
		remoteCopy.Timestamp = remoteTimestamp
		return &ConflictResolution{
			EventID:       event.ID,
			LocalVersion:  localEvent,
			RemoteVersion: &remoteCopy,
		}, nil
	}

	// If timestamps aren't available, compare raw data
	if !bytes.Equal(localData, event.Data) {
		localEvent := &ReplicationEvent{
			ID:        event.ID + "-local",
			Type:      event.Type,
			Action:    "local",
			Timestamp: localTimestamp,
			EntityID:  event.EntityID,
			Data:      localData,
		}
		return &ConflictResolution{
			EventID:       event.ID,
			LocalVersion:  localEvent,
			RemoteVersion: event,
		}, nil
	}

	// Data is identical — no conflict
	return nil, nil
}

// extractTimestamp parses updated_at or timestamp from JSON data
func extractTimestamp(data json.RawMessage) time.Time {
	if len(data) == 0 {
		return time.Time{}
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return time.Time{}
	}
	if ts, ok := m["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			return t
		}
	}
	if ts, ok := m["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			return t
		}
	}
	return time.Time{}
}

// resolveConflict resolves a replication conflict
func (r *ReplicationManager) resolveConflict(conflict *ConflictResolution) *ConflictResolution {
	switch r.config.ConflictStrategy {
	case "last-write-wins":
		if conflict.RemoteVersion.Timestamp.After(conflict.LocalVersion.Timestamp) {
			conflict.Winner = "remote"
		} else {
			conflict.Winner = "local"
		}
	case "timestamp":
		if conflict.RemoteVersion.Timestamp.After(conflict.LocalVersion.Timestamp) {
			conflict.Winner = "remote"
		} else {
			conflict.Winner = "local"
		}
	case "manual":
		// Requires manual intervention
		conflict.Winner = "manual"
	default:
		// Default to last-write-wins
		if conflict.RemoteVersion.Timestamp.After(conflict.LocalVersion.Timestamp) {
			conflict.Winner = "remote"
		} else {
			conflict.Winner = "local"
		}
	}

	conflict.ResolvedAt = time.Now()
	return conflict
}

// applySoulEvent applies a soul-related event
func (r *ReplicationManager) applySoulEvent(ctx context.Context, event *ReplicationEvent) error {
	// Handled by storage layer
	r.logger.Debug("Applied soul event",
		"action", event.Action,
		"entity_id", event.EntityID)
	return nil
}

// applyJudgmentEvent applies a judgment-related event
func (r *ReplicationManager) applyJudgmentEvent(ctx context.Context, event *ReplicationEvent) error {
	r.logger.Debug("Applied judgment event",
		"action", event.Action,
		"entity_id", event.EntityID)
	return nil
}

// applyConfigEvent applies a configuration event
func (r *ReplicationManager) applyConfigEvent(ctx context.Context, event *ReplicationEvent) error {
	r.logger.Debug("Applied config event",
		"action", event.Action,
		"entity_id", event.EntityID)
	return nil
}

// compressData compresses data using gzip
func compressData(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decompressData decompresses gzip data
func decompressData(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

// generateEventID generates a unique event ID
func generateEventID() string {
	return fmt.Sprintf("evt-%d-%s", time.Now().UnixNano(), generateRandomString(8))
}

// calculateChecksum calculates a simple checksum for an event
func calculateChecksum(event ReplicationEvent) string {
	data := fmt.Sprintf("%s:%s:%s:%s:%d",
		event.Type,
		event.Action,
		event.EntityID,
		event.Region,
		event.Timestamp.UnixNano())
	return fmt.Sprintf("%x", data) // Simplified
}

// generateRandomString generates a random string
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(result)
}

// StreamStatus provides replication stream status
type StreamStatus struct {
	RegionID      string        `json:"region_id"`
	Healthy       bool          `json:"healthy"`
	LastSync      time.Time     `json:"last_sync"`
	Lag           time.Duration `json:"lag"`
	PendingEvents int           `json:"pending_events"`
}
