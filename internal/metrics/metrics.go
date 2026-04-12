package metrics

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Metric types
const (
	TypeCounter   = "counter"
	TypeGauge     = "gauge"
	TypeHistogram = "histogram"
	TypeSummary   = "summary"
)

// Registry holds all metrics
type Registry struct {
	mu      sync.RWMutex
	metrics map[string]Metric
	prefix  string
}

// Metric is the interface for all metric types
type Metric interface {
	Name() string
	Type() string
	Describe() string
}

// NewRegistry creates a new metrics registry
func NewRegistry(prefix string) *Registry {
	return &Registry{
		metrics: make(map[string]Metric),
		prefix:  prefix,
	}
}

// Register adds a metric to the registry
func (r *Registry) Register(metric Metric) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := metric.Name()
	if _, exists := r.metrics[name]; exists {
		return fmt.Errorf("metric %s already registered", name)
	}

	r.metrics[name] = metric
	return nil
}

// Get retrieves a metric by name
func (r *Registry) Get(name string) (Metric, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	metric, exists := r.metrics[name]
	return metric, exists
}

// List returns all registered metrics
func (r *Registry) List() []Metric {
	r.mu.RLock()
	defer r.mu.RUnlock()

	metrics := make([]Metric, 0, len(r.metrics))
	for _, m := range r.metrics {
		metrics = append(metrics, m)
	}
	return metrics
}

// Counter is a monotonically increasing counter
type Counter struct {
	name  string
	help  string
	value int64
}

// NewCounter creates a new counter
func NewCounter(name, help string) *Counter {
	return &Counter{
		name: name,
		help: help,
	}
}

// Name returns the counter name
func (c *Counter) Name() string { return c.name }

// Type returns the metric type
func (c *Counter) Type() string { return TypeCounter }

// Describe returns the help text
func (c *Counter) Describe() string { return c.help }

// Inc increments the counter by 1
func (c *Counter) Inc() {
	atomic.AddInt64(&c.value, 1)
}

// Add adds the given value to the counter
func (c *Counter) Add(value int64) {
	atomic.AddInt64(&c.value, value)
}

// Get returns the current value
func (c *Counter) Get() int64 {
	return atomic.LoadInt64(&c.value)
}

// Reset resets the counter to 0
func (c *Counter) Reset() {
	atomic.StoreInt64(&c.value, 0)
}

// Gauge is a value that can go up and down
type Gauge struct {
	name  string
	help  string
	value int64
}

// NewGauge creates a new gauge
func NewGauge(name, help string) *Gauge {
	return &Gauge{
		name: name,
		help: help,
	}
}

// Name returns the gauge name
func (g *Gauge) Name() string { return g.name }

// Type returns the metric type
func (g *Gauge) Type() string { return TypeGauge }

// Describe returns the help text
func (g *Gauge) Describe() string { return g.help }

// Set sets the gauge to the given value
func (g *Gauge) Set(value float64) {
	atomic.StoreInt64(&g.value, int64(value*1000))
}

// Inc increments the gauge by 1
func (g *Gauge) Inc() {
	atomic.AddInt64(&g.value, 1000)
}

// Dec decrements the gauge by 1
func (g *Gauge) Dec() {
	atomic.AddInt64(&g.value, -1000)
}

// Add adds the given value to the gauge
func (g *Gauge) Add(value float64) {
	atomic.AddInt64(&g.value, int64(value*1000))
}

// Get returns the current value
func (g *Gauge) Get() float64 {
	return float64(atomic.LoadInt64(&g.value)) / 1000
}

// Histogram tracks the distribution of values
type Histogram struct {
	name    string
	help    string
	buckets []float64
	counts  []int64
	sum     int64
	count   int64
	mu      sync.RWMutex
}

// NewHistogram creates a new histogram
func NewHistogram(name, help string, buckets []float64) *Histogram {
	if len(buckets) == 0 {
		buckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}
	}
	return &Histogram{
		name:    name,
		help:    help,
		buckets: buckets,
		counts:  make([]int64, len(buckets)+1),
	}
}

// Name returns the histogram name
func (h *Histogram) Name() string { return h.name }

// Type returns the metric type
func (h *Histogram) Type() string { return TypeHistogram }

// Describe returns the help text
func (h *Histogram) Describe() string { return h.help }

// Observe records a value
func (h *Histogram) Observe(value float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	atomic.AddInt64(&h.sum, int64(value*1000))
	atomic.AddInt64(&h.count, 1)

	for i, bucket := range h.buckets {
		if value <= bucket {
			atomic.AddInt64(&h.counts[i], 1)
			return
		}
	}
	atomic.AddInt64(&h.counts[len(h.buckets)], 1)
}

// GetCount returns the total count
func (h *Histogram) GetCount() int64 {
	return atomic.LoadInt64(&h.count)
}

// GetSum returns the sum of all values
func (h *Histogram) GetSum() float64 {
	return float64(atomic.LoadInt64(&h.sum)) / 1000
}

// GetBuckets returns bucket counts
func (h *Histogram) GetBuckets() ([]float64, []int64) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	counts := make([]int64, len(h.counts))
	for i, c := range h.counts {
		counts[i] = atomic.LoadInt64(&c)
	}
	return h.buckets, counts
}

// Timer is a helper for timing operations
type Timer struct {
	histogram *Histogram
	start     time.Time
}

// NewTimer creates a new timer
func NewTimer(histogram *Histogram) *Timer {
	return &Timer{
		histogram: histogram,
		start:     time.Now(),
	}
}

// ObserveDuration records the duration since the timer was created
func (t *Timer) ObserveDuration() float64 {
	duration := time.Since(t.start).Seconds()
	if t.histogram != nil {
		t.histogram.Observe(duration)
	}
	return duration
}

// DefaultRegistry is the global metrics registry
var DefaultRegistry = NewRegistry("anubis")

// Global metric functions

// RegisterCounter creates and registers a counter
func RegisterCounter(name, help string) *Counter {
	c := &Counter{name: name, help: help}
	DefaultRegistry.Register(c)
	return c
}

// RegisterGauge creates and registers a gauge
func RegisterGauge(name, help string) *Gauge {
	g := &Gauge{name: name, help: help}
	DefaultRegistry.Register(g)
	return g
}

// RegisterHistogram creates and registers a histogram
func RegisterHistogram(name, help string, buckets []float64) *Histogram {
	h := NewHistogram(name, help, buckets)
	DefaultRegistry.Register(h)
	return h
}

// Get retrieves a metric from the default registry
func Get(name string) (Metric, bool) {
	return DefaultRegistry.Get(name)
}

// PrometheusExporter exports metrics in Prometheus format
type PrometheusExporter struct {
	registry *Registry
}

// NewPrometheusExporter creates a new Prometheus exporter
func NewPrometheusExporter(registry *Registry) *PrometheusExporter {
	return &PrometheusExporter{registry: registry}
}

// Export returns metrics in Prometheus text format
func (e *PrometheusExporter) Export() string {
	metrics := e.registry.List()
	output := ""

	for _, m := range metrics {
		switch metric := m.(type) {
		case *Counter:
			output += fmt.Sprintf("# HELP %s %s\n", metric.name, metric.help)
			output += fmt.Sprintf("# TYPE %s counter\n", metric.name)
			output += fmt.Sprintf("%s %d\n", metric.name, metric.Get())

		case *Gauge:
			output += fmt.Sprintf("# HELP %s %s\n", metric.name, metric.help)
			output += fmt.Sprintf("# TYPE %s gauge\n", metric.name)
			output += fmt.Sprintf("%s %f\n", metric.name, metric.Get())

		case *Histogram:
			output += fmt.Sprintf("# HELP %s %s\n", metric.name, metric.help)
			output += fmt.Sprintf("# TYPE %s histogram\n", metric.name)
			buckets, counts := metric.GetBuckets()
			for i, bucket := range buckets {
				output += fmt.Sprintf("%s_bucket{le=\"%g\"} %d\n", metric.name, bucket, counts[i])
			}
			output += fmt.Sprintf("%s_bucket{le=\"+Inf\"} %d\n", metric.name, counts[len(counts)-1])
			output += fmt.Sprintf("%s_sum %f\n", metric.name, metric.GetSum())
			output += fmt.Sprintf("%s_count %d\n", metric.name, metric.GetCount())
		}
	}

	return output
}
