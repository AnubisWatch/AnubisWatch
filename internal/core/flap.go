package core

import (
	"fmt"
	"time"
)

// Confirmation retries.
//
// A soul with Retries > 0 is not judged dead on one failed attempt: the probe
// engine re-runs the check up to Retries more times, RetryInterval apart, and
// records exactly one judgment per scheduled check — the final outcome — with
// the number of attempts in JudgmentDetails.Attempts. Heartbeat souls do not
// dial anything and ignore Retries.
const (
	// MaxSoulRetries bounds soul.retries.
	MaxSoulRetries = 5
	// DefaultRetryInterval is used when retries are enabled and
	// retry_interval is unset.
	DefaultRetryInterval = 5 * time.Second
	// MinRetryInterval is the smallest explicit retry_interval.
	MinRetryInterval = 100 * time.Millisecond
	// defaultCheckInterval mirrors the probe engine's interval for a soul
	// without an explicit weight.
	defaultCheckInterval = 60 * time.Second
)

// EffectiveRetryInterval returns the pause between confirmation attempts.
func (s *Soul) EffectiveRetryInterval() time.Duration {
	if s.RetryInterval.Duration > 0 {
		return s.RetryInterval.Duration
	}
	return DefaultRetryInterval
}

// EffectiveRetries returns how many confirmation retries the engine runs for
// this soul: heartbeat souls never retry, and out-of-range values written
// before validation existed are clamped.
func (s *Soul) EffectiveRetries() int {
	if s.Type == CheckHeartbeat || s.Retries <= 0 {
		return 0
	}
	if s.Retries > MaxSoulRetries {
		return MaxSoulRetries
	}
	return s.Retries
}

func (s Soul) validateRetries(prefix string) error {
	if s.Retries < 0 || s.Retries > MaxSoulRetries {
		return &ConfigError{Field: prefix + "retries", Message: fmt.Sprintf("retries must be between 0 and %d, got %d", MaxSoulRetries, s.Retries)}
	}
	ri := s.RetryInterval.Duration
	if ri < 0 {
		return &ConfigError{Field: prefix + "retry_interval", Message: "retry interval cannot be negative"}
	}
	if ri == 0 {
		return nil
	}
	if ri < MinRetryInterval {
		return &ConfigError{Field: prefix + "retry_interval", Message: fmt.Sprintf("retry interval must be at least %s (got %s; use a duration string such as \"5s\")", MinRetryInterval, ri)}
	}
	interval := s.Weight.Duration
	if interval < MinSoulInterval {
		interval = defaultCheckInterval
	}
	if ri >= interval {
		return &ConfigError{Field: prefix + "retry_interval", Message: fmt.Sprintf("retry interval (%s) must be shorter than the check interval (%s)", ri, interval)}
	}
	return nil
}

// Flap detection.
//
// A soul whose status changes at least Threshold times within Window is
// flapping. While it flaps, the owning node keeps recording judgments but
// alert rules stop firing for it; one "flapping started" and one
// "flapping stopped" notification go through each covering rule instead.
// Flapping ends once fewer than half the threshold (rounded up) changes
// remain in the window.
const (
	DefaultFlapThreshold = 5
	DefaultFlapWindow    = 10 * time.Minute
	MinFlapThreshold     = 2
	MaxFlapThreshold     = 50
	MinFlapWindow        = 10 * time.Second
	MaxFlapWindow        = 24 * time.Hour
)

// FlapDetectionConfig configures flap detection, globally under
// probe.flap_detection or per soul under flap_detection. Zero threshold or
// window means "inherit" (the global value, then the default).
type FlapDetectionConfig struct {
	Enabled   bool     `json:"enabled" yaml:"enabled"`
	Threshold int      `json:"threshold,omitempty" yaml:"threshold,omitempty"`
	Window    Duration `json:"window,omitzero" yaml:"window,omitempty"`
}

// FlapSettings is the resolved flap-detection policy for one soul.
type FlapSettings struct {
	Enabled   bool
	Threshold int
	Window    time.Duration
}

// ResolveFlapSettings combines a soul's override (nil = inherit) with the
// global setting.
func ResolveFlapSettings(soul *FlapDetectionConfig, global FlapDetectionConfig) FlapSettings {
	out := FlapSettings{Enabled: global.Enabled, Threshold: DefaultFlapThreshold, Window: DefaultFlapWindow}
	if global.Threshold > 0 {
		out.Threshold = global.Threshold
	}
	if global.Window.Duration > 0 {
		out.Window = global.Window.Duration
	}
	if soul != nil {
		out.Enabled = soul.Enabled
		if soul.Threshold > 0 {
			out.Threshold = soul.Threshold
		}
		if soul.Window.Duration > 0 {
			out.Window = soul.Window.Duration
		}
	}
	if out.Threshold < MinFlapThreshold {
		out.Threshold = MinFlapThreshold
	}
	return out
}

// StopBelow is the transition count under which a flapping soul is
// considered stable again.
func (f FlapSettings) StopBelow() int {
	return (f.Threshold + 1) / 2
}

func (f *FlapDetectionConfig) validate(field string) error {
	if f == nil {
		return nil
	}
	if f.Threshold != 0 && (f.Threshold < MinFlapThreshold || f.Threshold > MaxFlapThreshold) {
		return &ConfigError{Field: field + ".threshold", Message: fmt.Sprintf("threshold must be between %d and %d (or 0 for the default of %d), got %d", MinFlapThreshold, MaxFlapThreshold, DefaultFlapThreshold, f.Threshold)}
	}
	w := f.Window.Duration
	if w != 0 && (w < MinFlapWindow || w > MaxFlapWindow) {
		return &ConfigError{Field: field + ".window", Message: fmt.Sprintf("window must be between %s and %s (or unset for the default of %s), got %s", MinFlapWindow, MaxFlapWindow, DefaultFlapWindow, w)}
	}
	return nil
}

// Flap state changes recorded in JudgmentDetails.FlapChange.
const (
	FlapStarted = "started"
	FlapStopped = "stopped"
)

// Alert event kinds for flap notifications (AlertEvent.Kind).
const (
	AlertKindFlappingStarted = "flapping_started"
	AlertKindFlappingStopped = "flapping_stopped"
)
