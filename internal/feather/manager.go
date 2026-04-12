package feather

import (
	"math"
	"sort"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Manager evaluates performance budgets (Feathers) against recent judgments
type Manager struct {
	mu          sync.RWMutex
	feathers    map[string]core.FeatherConfig // name -> config
	latencyDB   map[string][]latencySample    // soulID -> recent latencies
	violations  map[string]int                // soulID -> consecutive violation count
	onViolation func(soulID, featherName string, sample FeatherViolation)
}

type latencySample struct {
	duration  time.Duration
	timestamp time.Time
	status    core.SoulStatus
}

// FeatherViolation records a budget violation
type FeatherViolation struct {
	Feather   string        `json:"feather"`
	SoulID    string        `json:"soul_id"`
	Metric    string        `json:"metric"` // p50, p95, p99, max
	Actual    time.Duration `json:"actual"`
	Threshold time.Duration `json:"threshold"`
	Timestamp time.Time     `json:"timestamp"`
}

// NewManager creates a feather manager
func NewManager(onViolation func(soulID, featherName string, sample FeatherViolation)) *Manager {
	return &Manager{
		feathers:    make(map[string]core.FeatherConfig),
		latencyDB:   make(map[string][]latencySample),
		violations:  make(map[string]int),
		onViolation: onViolation,
	}
}

// AddFeather registers a performance budget
func (m *Manager) AddFeather(cfg core.FeatherConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.feathers[cfg.Name] = cfg
}

// RemoveFeather removes a performance budget
func (m *Manager) RemoveFeather(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.feathers, name)
}

// GetFeathers returns all registered feathers
func (m *Manager) GetFeathers() map[string]core.FeatherConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]core.FeatherConfig, len(m.feathers))
	for k, v := range m.feathers {
		out[k] = v
	}
	return out
}

// RecordLatency records a new latency sample
func (m *Manager) RecordLatency(soulID string, duration time.Duration, status core.SoulStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.latencyDB[soulID] = append(m.latencyDB[soulID], latencySample{
		duration:  duration,
		timestamp: time.Now().UTC(),
		status:    status,
	})
}

// Evaluate checks all feathers against current latency data
func (m *Manager) Evaluate() []FeatherViolation {
	m.mu.Lock()
	defer m.mu.Unlock()

	var violations []FeatherViolation
	now := time.Now().UTC()

	for soulID, samples := range m.latencyDB {
		// Clean old samples
		samples = m.cleanOld(samples, 24*time.Hour)
		m.latencyDB[soulID] = samples

		if len(samples) == 0 {
			continue
		}

		// Find matching feathers
		matchingFeathers := m.matchingFeathers(soulID)

		for _, feather := range matchingFeathers {
			// Filter samples to the evaluation window
			windowStart := now.Add(-feather.Window.Duration)
			recentSamples := make([]time.Duration, 0)
			for _, s := range samples {
				if s.timestamp.After(windowStart) {
					recentSamples = append(recentSamples, s.duration)
				}
			}

			if len(recentSamples) < 2 {
				continue // Need at least 2 samples for percentiles
			}

			sort.Slice(recentSamples, func(i, j int) bool {
				return recentSamples[i] < recentSamples[j]
			})

			v := m.checkRules(recentSamples, feather, soulID)
			if v != nil {
				violations = append(violations, *v)
				m.violations[soulID]++
				if m.onViolation != nil && m.violations[soulID] >= feather.ViolationThreshold {
					go m.onViolation(soulID, feather.Name, *v)
					m.violations[soulID] = 0 // reset after firing
				}
			} else {
				m.violations[soulID] = 0 // reset on success
			}
		}
	}

	return violations
}

// checkRules checks latency samples against feather rules
func (m *Manager) checkRules(sorted []time.Duration, feather core.FeatherConfig, soulID string) *FeatherViolation {
	rules := feather.Rules

	// Check P50
	if rules.P50.Duration > 0 {
		p50 := percentile(sorted, 50)
		if p50 > rules.P50.Duration {
			return &FeatherViolation{
				Feather:   feather.Name,
				SoulID:    soulID,
				Metric:    "p50",
				Actual:    p50,
				Threshold: rules.P50.Duration,
				Timestamp: time.Now().UTC(),
			}
		}
	}

	// Check P95
	if rules.P95.Duration > 0 {
		p95 := percentile(sorted, 95)
		if p95 > rules.P95.Duration {
			return &FeatherViolation{
				Feather:   feather.Name,
				SoulID:    soulID,
				Metric:    "p95",
				Actual:    p95,
				Threshold: rules.P95.Duration,
				Timestamp: time.Now().UTC(),
			}
		}
	}

	// Check P99
	if rules.P99.Duration > 0 {
		p99 := percentile(sorted, 99)
		if p99 > rules.P99.Duration {
			return &FeatherViolation{
				Feather:   feather.Name,
				SoulID:    soulID,
				Metric:    "p99",
				Actual:    p99,
				Threshold: rules.P99.Duration,
				Timestamp: time.Now().UTC(),
			}
		}
	}

	// Check Max
	if rules.Max.Duration > 0 {
		max := sorted[len(sorted)-1]
		if max > rules.Max.Duration {
			return &FeatherViolation{
				Feather:   feather.Name,
				SoulID:    soulID,
				Metric:    "max",
				Actual:    max,
				Threshold: rules.Max.Duration,
				Timestamp: time.Now().UTC(),
			}
		}
	}

	return nil
}

// matchingFeathers finds feathers that apply to a soul
func (m *Manager) matchingFeathers(soulID string) []core.FeatherConfig {
	var matches []core.FeatherConfig
	for _, feather := range m.feathers {
		if m.featherMatches(feather, soulID) {
			matches = append(matches, feather)
		}
	}
	return matches
}

// featherMatches checks if a feather applies to a soul
func (m *Manager) featherMatches(feather core.FeatherConfig, soulID string) bool {
	scope := feather.Scope
	if scope == "" || scope == "all" {
		return true
	}
	if scope == soulID || scope == "soul:"+soulID {
		return true
	}
	// Tag-based matching handled externally (caller checks soul tags)
	return false
}

// cleanOld removes samples older than the cutoff
func (m *Manager) cleanOld(samples []latencySample, maxAge time.Duration) []latencySample {
	cutoff := time.Now().UTC().Add(-maxAge)
	idx := 0
	for idx < len(samples) && samples[idx].timestamp.Before(cutoff) {
		idx++
	}
	if idx == 0 {
		return samples
	}
	return samples[idx:]
}

// Stats returns feather statistics
func (m *Manager) Stats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := map[string]interface{}{
		"total_feathers":    len(m.feathers),
		"tracked_souls":     len(m.latencyDB),
		"active_violations": make(map[string]int),
	}

	activeViolations := make(map[string]int)
	for soulID, count := range m.violations {
		if count > 0 {
			activeViolations[soulID] = count
		}
	}
	stats["active_violations"] = activeViolations

	return stats
}

// percentile returns the p-th percentile from a sorted slice
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := p / 100.0 * float64(len(sorted)-1)
	lower := int(math.Floor(rank))
	upper := int(math.Ceil(rank))
	if lower == upper {
		return sorted[lower]
	}
	frac := rank - float64(lower)
	lowVal := float64(sorted[lower])
	highVal := float64(sorted[upper])
	return time.Duration(lowVal*(1-frac) + highVal*frac)
}
