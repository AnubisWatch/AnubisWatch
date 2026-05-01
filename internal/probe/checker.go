package probe

import (
	"context"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Size limits for reading data from network connections
// Prevents memory exhaustion DoS from malicious servers
const (
	maxReadSize    = 1024 * 1024     // 1MB default limit
	maxBannerSize  = 64 * 1024       // 64KB for banners
	maxMessageSize = 4 * 1024 * 1024 // 4MB for explicit messages
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

// Global registry instance
var globalRegistry = NewCheckerRegistry()

// GetChecker returns a checker by type from the global registry
func GetChecker(t core.CheckType) Checker {
	c, ok := globalRegistry.Get(t)
	if !ok {
		return nil
	}
	return c
}

// RegisterChecker registers a checker with the global registry
func RegisterChecker(c Checker) {
	globalRegistry.Register(c)
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

// ConfigError creates a config error for a field
func configError(field, message string) error {
	return &core.ConfigError{Field: field, Message: message}
}
