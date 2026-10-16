package cluster

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Batcher defaults. An entry stays far below the transport's 16MB payload cap
// even when a full AppendEntries round (MaxAppendEntries entries) carries
// nothing but batches: the value is base64-encoded twice on its way into the
// log, so 32KB of commands is roughly 80KB on the wire.
const (
	defaultBatchInterval    = time.Second
	defaultBatchMaxCommands = 32
	defaultBatchMaxBytes    = 32 << 10
	defaultBatchMaxPending  = 10000
	batchApplyTimeout       = 10 * time.Second
)

// Batcher coalesces high-volume replicated writes -- judgments, incident
// updates -- into FSMBatch log entries, flushed every interval or as soon as a
// batch is full, so the log grows by about one entry per second per probing
// node instead of one per check. Commands are kept in arrival order. A flush
// that fails (no leader, say) is retried on the next tick; while the backlog is
// over maxPending the oldest commands are dropped.
type Batcher struct {
	apply       func(cmd core.FSMCommand, timeout time.Duration) (uint64, uint64, error)
	interval    time.Duration
	maxCommands int
	maxBytes    int
	maxPending  int
	logger      *slog.Logger

	mu      sync.Mutex
	pending []core.FSMCommand
	dropped int
	failing bool

	kick      chan struct{}
	stopCh    chan struct{}
	done      chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
}

// NewBatcher creates a batcher that submits batches through apply (normally
// Manager.ApplyMutation). interval <= 0 selects the default.
func NewBatcher(apply func(cmd core.FSMCommand, timeout time.Duration) (uint64, uint64, error), interval time.Duration, logger *slog.Logger) *Batcher {
	if interval <= 0 {
		interval = defaultBatchInterval
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Batcher{
		apply:       apply,
		interval:    interval,
		maxCommands: defaultBatchMaxCommands,
		maxBytes:    defaultBatchMaxBytes,
		maxPending:  defaultBatchMaxPending,
		logger:      logger.With("component", "replication_batcher"),
		kick:        make(chan struct{}, 1),
		stopCh:      make(chan struct{}),
		done:        make(chan struct{}),
	}
}

// Start begins periodic flushing. It is a no-op after the first call.
func (b *Batcher) Start() {
	b.startOnce.Do(func() { go b.loop() })
}

// Enqueue queues cmd for the next batch. It never blocks on consensus.
func (b *Batcher) Enqueue(cmd core.FSMCommand) {
	b.mu.Lock()
	b.pending = append(b.pending, cmd)
	if over := len(b.pending) - b.maxPending; over > 0 {
		b.pending = append([]core.FSMCommand(nil), b.pending[over:]...)
		b.dropped += over
	}
	full := len(b.pending) >= b.maxCommands
	b.mu.Unlock()
	if full {
		select {
		case b.kick <- struct{}{}:
		default:
		}
	}
}

// Pending returns the number of queued commands.
func (b *Batcher) Pending() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}

// Stop stops the loop and makes a final attempt to flush what is queued,
// bounded by ctx.
func (b *Batcher) Stop(ctx context.Context) {
	b.stopOnce.Do(func() { close(b.stopCh) })
	b.startOnce.Do(func() { close(b.done) }) // never started: nothing to wait for
	select {
	case <-b.done:
	case <-ctx.Done():
		return
	}
	for b.Pending() > 0 && ctx.Err() == nil {
		if err := b.flushOnce(); err != nil {
			b.logger.Warn("final replication flush failed", "pending", b.Pending(), "err", err)
			return
		}
	}
}

func (b *Batcher) loop() {
	defer close(b.done)
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()
	for {
		select {
		case <-b.stopCh:
			return
		case <-ticker.C:
		case <-b.kick:
		}
		// Drain everything queued, one bounded batch per entry, stopping at the
		// first failure so it is retried on the next tick.
		for b.Pending() > 0 {
			if err := b.flushOnce(); err != nil {
				break
			}
			select {
			case <-b.stopCh:
				return
			default:
			}
		}
	}
}

// flushOnce submits the oldest commands as one batch entry. On failure they
// are put back at the front of the queue.
func (b *Batcher) flushOnce() error {
	b.mu.Lock()
	n, size := 0, 0
	for n < len(b.pending) && n < b.maxCommands {
		size += len(b.pending[n].Value) + len(b.pending[n].Key)
		n++
		if size >= b.maxBytes {
			break
		}
	}
	batch := append([]core.FSMCommand(nil), b.pending[:n]...)
	b.pending = b.pending[n:]
	dropped := b.dropped
	b.dropped = 0
	b.mu.Unlock()
	if len(batch) == 0 {
		return nil
	}
	if dropped > 0 {
		b.logger.Warn("replication backlog overflowed; oldest commands dropped", "dropped", dropped)
	}

	data, err := json.Marshal(batch)
	if err != nil {
		b.logger.Error("failed to encode replication batch; dropping it", "commands", len(batch), "err", err)
		return nil
	}
	_, _, err = b.apply(core.FSMCommand{Op: core.FSMBatch, Value: data}, batchApplyTimeout)
	b.mu.Lock()
	defer b.mu.Unlock()
	if err != nil {
		b.pending = append(batch, b.pending...)
		if over := len(b.pending) - b.maxPending; over > 0 {
			b.pending = b.pending[over:]
			b.dropped += over
		}
		if !b.failing {
			b.logger.Warn("replication batch failed; will retry", "commands", len(batch), "err", err)
			b.failing = true
		}
		return err
	}
	if b.failing {
		b.logger.Info("replication batches flowing again")
		b.failing = false
	}
	return nil
}
