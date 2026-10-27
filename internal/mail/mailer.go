package mail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	netmail "net/mail"
	"strings"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Sender delivers one message.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// Mailer sends messages through the server-level relay (mail: in config).
type Mailer struct {
	server  Server
	from    *netmail.Address
	timeout time.Duration
	now     func() time.Time
}

// New returns a Mailer for cfg, or nil when no relay is configured.
func New(cfg core.MailConfig) (*Mailer, error) {
	if !cfg.Enabled() {
		return nil, nil
	}
	if strings.ContainsAny(cfg.From, "\r\n") {
		return nil, ErrHeaderInjection
	}
	from, err := netmail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("mail.from: %w", err)
	}
	return &Mailer{
		server: Server{
			Host:     strings.TrimSpace(cfg.Host),
			Port:     cfg.EffectivePort(),
			TLS:      cfg.EffectiveTLS(),
			Username: cfg.Username,
			Password: cfg.Password,
		},
		from:    from,
		timeout: cfg.EffectiveTimeout(),
		now:     time.Now,
	}, nil
}

// Send renders msg and delivers it, bounded by the configured timeout.
func (m *Mailer) Send(ctx context.Context, msg Message) error {
	if m == nil {
		return errors.New("mail: no relay configured")
	}
	r, err := msg.render(m.from, m.now())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	return SendRaw(ctx, m.server, r.from, []string{r.to}, r.bytes)
}

// QueueOptions tunes a Queue. Zero values pick the defaults.
type QueueOptions struct {
	// Workers is the number of concurrent deliveries (default 2).
	Workers int
	// Capacity bounds queued messages; Enqueue drops beyond it (default 1000).
	Capacity int
	// RetryDelays are the waits before each retry (default 30s, 2m, 10m).
	RetryDelays []time.Duration
}

// Queue delivers messages asynchronously with retries, so callers on the
// request, probe or alert paths never wait for SMTP.
type Queue struct {
	sender Sender
	logger *slog.Logger
	delays []time.Duration
	ch     chan queued

	mu     sync.Mutex
	closed bool
	stop   chan struct{}
	wg     sync.WaitGroup
}

type queued struct {
	msg     Message
	attempt int
}

// NewQueue starts a delivery queue in front of sender.
func NewQueue(sender Sender, logger *slog.Logger, opts QueueOptions) *Queue {
	if logger == nil {
		logger = slog.Default()
	}
	if opts.Workers <= 0 {
		opts.Workers = 2
	}
	if opts.Capacity <= 0 {
		opts.Capacity = 1000
	}
	if opts.RetryDelays == nil {
		opts.RetryDelays = []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute}
	}
	q := &Queue{
		sender: sender,
		logger: logger,
		delays: opts.RetryDelays,
		ch:     make(chan queued, opts.Capacity),
		stop:   make(chan struct{}),
	}
	for i := 0; i < opts.Workers; i++ {
		q.wg.Add(1)
		go q.worker()
	}
	return q
}

// Enqueue schedules msg for delivery. It never blocks; it reports false when
// the queue is full or closed.
func (q *Queue) Enqueue(msg Message) bool {
	return q.push(queued{msg: msg})
}

func (q *Queue) push(item queued) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false
	}
	select {
	case q.ch <- item:
		return true
	default:
		q.logger.Warn("mail queue full; message dropped", "kind", item.msg.Kind)
		return false
	}
}

// Close stops the workers. Messages still queued or waiting for a retry are
// dropped.
func (q *Queue) Close() {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.closed = true
	close(q.stop)
	q.mu.Unlock()
	q.wg.Wait()
}

func (q *Queue) worker() {
	defer q.wg.Done()
	for {
		select {
		case <-q.stop:
			return
		case item := <-q.ch:
			q.deliver(item)
		}
	}
}

func (q *Queue) deliver(item queued) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-q.stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	err := q.sender.Send(ctx, item.msg)
	cancel()
	if err == nil {
		q.logger.Debug("mail delivered", "kind", item.msg.Kind, "attempt", item.attempt+1)
		return
	}
	// Errors name the relay and SMTP reply, never the message body, so a
	// token in a link cannot reach the log.
	if errors.Is(err, ErrHeaderInjection) || item.attempt >= len(q.delays) {
		q.logger.Error("mail delivery failed", "kind", item.msg.Kind, "attempts", item.attempt+1, "error", err)
		return
	}
	delay := q.delays[item.attempt]
	q.logger.Warn("mail delivery failed; will retry", "kind", item.msg.Kind, "attempt", item.attempt+1, "retry_in", delay, "error", err)
	item.attempt++
	time.AfterFunc(delay, func() { q.push(item) })
}
