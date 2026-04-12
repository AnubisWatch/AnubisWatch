package tracing

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Span represents a single operation within a trace
type Span struct {
	TraceID    string            `json:"trace_id"`
	SpanID     string            `json:"span_id"`
	ParentID   string            `json:"parent_id,omitempty"`
	Name       string            `json:"name"`
	StartTime  time.Time         `json:"start_time"`
	EndTime    time.Time         `json:"end_time,omitempty"`
	Duration   int64             `json:"duration_ms,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
	Logs       []LogEntry        `json:"logs,omitempty"`
	Status     SpanStatus        `json:"status"`
	Attributes map[string]any    `json:"attributes,omitempty"`
}

// SpanStatus represents the status of a span
type SpanStatus int

const (
	StatusOK SpanStatus = iota
	StatusError
	StatusCancelled
)

func (s SpanStatus) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusError:
		return "error"
	case StatusCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// LogEntry represents a log entry within a span
type LogEntry struct {
	Timestamp time.Time      `json:"timestamp"`
	Fields    map[string]any `json:"fields"`
}

// Tracer handles span creation and management
type Tracer struct {
	mu       sync.RWMutex
	spans    []*Span
	exporter SpanExporter
	enabled  bool
}

// SpanExporter defines the interface for exporting spans
type SpanExporter interface {
	Export(span *Span) error
}

// NewTracer creates a new tracer
func NewTracer(enabled bool, exporter SpanExporter) *Tracer {
	return &Tracer{
		spans:    make([]*Span, 0),
		exporter: exporter,
		enabled:  enabled,
	}
}

// StartSpan begins a new span
func (t *Tracer) StartSpan(ctx context.Context, name string) (context.Context, *Span) {
	if !t.enabled {
		return ctx, nil
	}

	// Get parent span from context if exists
	parent := SpanFromContext(ctx)

	span := &Span{
		TraceID:    generateTraceID(),
		SpanID:     generateSpanID(),
		Name:       name,
		StartTime:  time.Now(),
		Tags:       make(map[string]string),
		Attributes: make(map[string]any),
		Status:     StatusOK,
	}

	if parent != nil {
		span.TraceID = parent.TraceID
		span.ParentID = parent.SpanID
	}

	// Store span in context
	ctx = ContextWithSpan(ctx, span)

	return ctx, span
}

// Finish completes a span and exports it
func (t *Tracer) Finish(span *Span) {
	if span == nil || !t.enabled {
		return
	}

	span.EndTime = time.Now()
	span.Duration = span.EndTime.Sub(span.StartTime).Milliseconds()

	t.mu.Lock()
	t.spans = append(t.spans, span)
	t.mu.Unlock()

	if t.exporter != nil {
		go t.exporter.Export(span)
	}
}

// SetTag adds a tag to a span
func (s *Span) SetTag(key, value string) {
	if s == nil {
		return
	}
	s.Tags[key] = value
}

// SetAttribute adds an attribute to a span
func (s *Span) SetAttribute(key string, value any) {
	if s == nil {
		return
	}
	s.Attributes[key] = value
}

// Log adds a log entry to a span
func (s *Span) Log(fields map[string]any) {
	if s == nil {
		return
	}
	s.Logs = append(s.Logs, LogEntry{
		Timestamp: time.Now(),
		Fields:    fields,
	})
}

// SetError marks the span as having an error
func (s *Span) SetError(err error) {
	if s == nil {
		return
	}
	s.Status = StatusError
	s.SetAttribute("error", err.Error())
}

// contextKey is used to store span in context
type contextKey struct{}

var spanKey = &contextKey{}

// ContextWithSpan adds a span to the context
func ContextWithSpan(ctx context.Context, span *Span) context.Context {
	if span == nil {
		return ctx
	}
	return context.WithValue(ctx, spanKey, span)
}

// SpanFromContext retrieves a span from the context
func SpanFromContext(ctx context.Context) *Span {
	if ctx == nil {
		return nil
	}
	if span, ok := ctx.Value(spanKey).(*Span); ok {
		return span
	}
	return nil
}

// generateTraceID generates a unique trace ID
func generateTraceID() string {
	return fmt.Sprintf("trace-%d", time.Now().UnixNano())
}

// generateSpanID generates a unique span ID
func generateSpanID() string {
	return fmt.Sprintf("span-%d", time.Now().UnixNano())
}

// ConsoleExporter exports spans to console
type ConsoleExporter struct{}

// Export writes span to console
func (e *ConsoleExporter) Export(span *Span) error {
	fmt.Printf("[TRACE] %s %s: %s (%dms) status=%s\n",
		span.TraceID[:8],
		span.SpanID[:8],
		span.Name,
		span.Duration,
		span.Status,
	)
	return nil
}

// MemoryExporter stores spans in memory for testing
type MemoryExporter struct {
	mu    sync.RWMutex
	spans []*Span
}

// NewMemoryExporter creates a new memory exporter
func NewMemoryExporter() *MemoryExporter {
	return &MemoryExporter{
		spans: make([]*Span, 0),
	}
}

// Export stores span in memory
func (e *MemoryExporter) Export(span *Span) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.spans = append(e.spans, span)
	return nil
}

// GetSpans returns all recorded spans
func (e *MemoryExporter) GetSpans() []*Span {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]*Span, len(e.spans))
	copy(result, e.spans)
	return result
}

// Clear removes all recorded spans
func (e *MemoryExporter) Clear() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.spans = e.spans[:0]
}

// TracedFunction wraps a function with tracing
func TracedFunction(tracer *Tracer, name string, fn func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		ctx, span := tracer.StartSpan(ctx, name)
		if span == nil {
			return fn(ctx)
		}
		defer tracer.Finish(span)

		err := fn(ctx)
		if err != nil {
			span.SetError(err)
		}
		return err
	}
}

// NoopTracer returns a tracer that does nothing (for disabling tracing)
func NoopTracer() *Tracer {
	return &Tracer{
		enabled: false,
	}
}

// SpanStats provides statistics about spans
type SpanStats struct {
	TotalSpans  int
	TotalErrors int
	AvgDuration int64
	MaxDuration int64
	MinDuration int64
	SpanCounts  map[string]int
}

// GetStats returns statistics about recorded spans
func (t *Tracer) GetStats() SpanStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats := SpanStats{
		TotalSpans: len(t.spans),
		SpanCounts: make(map[string]int),
	}

	if len(t.spans) == 0 {
		return stats
	}

	var totalDuration int64
	stats.MaxDuration = 0
	stats.MinDuration = 1<<63 - 1

	for _, span := range t.spans {
		stats.SpanCounts[span.Name]++
		totalDuration += span.Duration

		if span.Duration > stats.MaxDuration {
			stats.MaxDuration = span.Duration
		}
		if span.Duration < stats.MinDuration {
			stats.MinDuration = span.Duration
		}

		if span.Status == StatusError {
			stats.TotalErrors++
		}
	}

	stats.AvgDuration = totalDuration / int64(len(t.spans))
	return stats
}
