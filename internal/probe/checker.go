package probe

import (
	"context"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Checker is the interface every protocol must implement.
// Named after the priests who assisted Anubis in the weighing ceremony.
type Checker interface {
	// Type returns the protocol identifier
	Type() core.CheckType

	// Judge performs the health check against the given soul
	Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error)

	// Validate ensures the soul configuration is valid for this checker
	Validate(soul *core.Soul) error
}

// CheckerRegistry maps check types to their implementations
type CheckerRegistry struct {
	checkers map[core.CheckType]Checker
}

// NewCheckerRegistry creates a new registry with all built-in checkers
func NewCheckerRegistry() *CheckerRegistry {
	r := &CheckerRegistry{
		checkers: make(map[core.CheckType]Checker),
	}

	// Register all built-in checkers
	r.Register(NewHTTPChecker())
	r.Register(NewTCPChecker())
	r.Register(NewUDPChecker())
	r.Register(NewDNSChecker())
	r.Register(NewSMTPChecker())
	r.Register(NewIMAPChecker())
	r.Register(NewICMPChecker())
	r.Register(NewGRPCChecker())
	r.Register(NewWebSocketChecker())
	r.Register(NewTLSChecker())

	return r
}

// Register adds a checker to the registry
func (r *CheckerRegistry) Register(c Checker) {
	r.checkers[c.Type()] = c
}

// Get retrieves a checker by type
func (r *CheckerRegistry) Get(t core.CheckType) (Checker, bool) {
	c, ok := r.checkers[t]
	return c, ok
}

// List returns all registered checker types
func (r *CheckerRegistry) List() []core.CheckType {
	types := make([]core.CheckType, 0, len(r.checkers))
	for t := range r.checkers {
		types = append(types, t)
	}
	return types
}

// baseChecker provides common utilities for all checkers
type baseChecker struct {
	checkType core.CheckType
}

// failJudgment creates a failed judgment with the given error
func failJudgment(soul *core.Soul, err error) *core.Judgment {
	return &core.Judgment{
		ID:        core.GenerateID(),
		SoulID:    soul.ID,
		Timestamp: time.Now().UTC(),
		Duration:  0,
		Status:    core.SoulDead,
		Message:   err.Error(),
		Details:   &core.JudgmentDetails{},
	}
}

// successJudgment creates a successful judgment
func successJudgment(soul *core.Soul, duration time.Duration, message string) *core.Judgment {
	return &core.Judgment{
		ID:        core.GenerateID(),
		SoulID:    soul.ID,
		Timestamp: time.Now().UTC(),
		Duration:  duration,
		Status:    core.SoulAlive,
		Message:   message,
		Details:   &core.JudgmentDetails{},
	}
}

// degradedJudgment creates a degraded judgment (performance issue)
func degradedJudgment(soul *core.Soul, duration time.Duration, message string) *core.Judgment {
	return &core.Judgment{
		ID:        core.GenerateID(),
		SoulID:    soul.ID,
		Timestamp: time.Now().UTC(),
		Duration:  duration,
		Status:    core.SoulDegraded,
		Message:   message,
		Details:   &core.JudgmentDetails{},
	}
}

// truncateString truncates a string to max length
func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// boolToString converts bool to string
func boolToString(b bool, t, f string) string {
	if b {
		return t
	}
	return f
}

// parseDuration parses a duration string, returning default on error
func parseDuration(s string, defaultVal time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultVal
	}
	return d
}

// ConfigError creates a config error for a field
func configError(field, message string) error {
	return &core.ConfigError{Field: field, Message: message}
}

// validationError creates a validation error
func validationError(field, message string) error {
	return &core.ValidationError{Field: field, Message: message}
}
