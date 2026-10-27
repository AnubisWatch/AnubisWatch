package core

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

// Heartbeat (push) monitor limits. A heartbeat soul inverts the usual probe:
// the monitored job calls the ping URL, and the soul is judged dead when no
// ping arrives within its interval (soul weight) plus the grace period.
const (
	// MaxHeartbeatInterval bounds the expected ping period: a monthly job is
	// the longest cadence a heartbeat soul is meant for.
	MaxHeartbeatInterval = 31 * 24 * time.Hour
	// MinHeartbeatGrace and MaxHeartbeatGrace bound an explicit grace period.
	MinHeartbeatGrace = time.Second
	MaxHeartbeatGrace = 7 * 24 * time.Hour
	// DefaultHeartbeatInterval is used when a heartbeat soul has no weight.
	DefaultHeartbeatInterval = 60 * time.Second
	// MaxHeartbeatMessage is the longest message a /fail ping may record.
	MaxHeartbeatMessage = 1024

	heartbeatTokenBytes = 32
	minHeartbeatToken   = 32
	maxHeartbeatToken   = 128
)

// HeartbeatConfig defines push-monitor settings. Token is the per-soul
// secret in the ping URL; the server generates it and only the reveal and
// rotate endpoints return it in clear.
type HeartbeatConfig struct {
	Grace Duration `json:"grace" yaml:"grace"`
	Token string   `json:"token,omitempty" yaml:"token,omitempty"`
}

// HeartbeatEvent names the kind of ping that was last received.
type HeartbeatEvent string

const (
	HeartbeatSuccess HeartbeatEvent = "success"
	HeartbeatStart   HeartbeatEvent = "start"
	HeartbeatFail    HeartbeatEvent = "fail"
)

// HeartbeatState is the replicated record of the pings a heartbeat soul has
// received. Any node may write it (a ping can land on any node); only the
// soul's owner turns it into judgments.
type HeartbeatState struct {
	SoulID      string `json:"soul_id"`
	WorkspaceID string `json:"workspace_id"`
	// LastPingAt is when the last success or fail ping arrived; zero means
	// the job never reported. A start ping does not move it.
	LastPingAt time.Time `json:"last_ping_at,omitempty"`
	// LastEvent is the kind of the last success/fail ping.
	LastEvent HeartbeatEvent `json:"last_event,omitempty"`
	// LastStartAt is when the last /start ping arrived (zero if none).
	LastStartAt time.Time `json:"last_start_at,omitempty"`
	// LastDuration is the run time measured from the matching /start ping.
	LastDuration time.Duration `json:"last_duration,omitempty"`
	// LastMessage is the (truncated) body of the last /fail ping.
	LastMessage string `json:"last_message,omitempty"`
	// LastSource is the client address of the last ping.
	LastSource string `json:"last_source,omitempty"`
	// LastNode is the node that received the last ping.
	LastNode string `json:"last_node,omitempty"`
	// Pings counts success and fail pings.
	Pings uint64 `json:"pings"`
}

// Apply folds one ping into the state.
func (s *HeartbeatState) Apply(event HeartbeatEvent, at time.Time, message, source, node string) {
	switch event {
	case HeartbeatStart:
		s.LastStartAt = at
	case HeartbeatSuccess, HeartbeatFail:
		var duration time.Duration
		if !s.LastStartAt.IsZero() && !s.LastStartAt.After(at) && s.LastStartAt.After(s.LastPingAt) {
			duration = at.Sub(s.LastStartAt)
		}
		s.LastPingAt = at
		s.LastEvent = event
		s.LastDuration = duration
		s.Pings++
		if event == HeartbeatFail {
			if len(message) > MaxHeartbeatMessage {
				message = message[:MaxHeartbeatMessage]
			}
			s.LastMessage = message
		} else {
			s.LastMessage = ""
		}
	default:
		return
	}
	s.LastSource = source
	s.LastNode = node
}

// HeartbeatStateKey is the storage key of a soul's heartbeat state.
func HeartbeatStateKey(workspaceID, soulID string) string {
	if workspaceID == "" {
		workspaceID = "default"
	}
	return workspaceID + "/heartbeats/" + soulID
}

// HeartbeatInterval is the expected period between pings.
func (s *Soul) HeartbeatInterval() time.Duration {
	if s.Weight.Duration >= MinSoulInterval {
		return s.Weight.Duration
	}
	return DefaultHeartbeatInterval
}

// HeartbeatGrace is the extra time a ping may be late before the soul is
// judged dead: the configured grace, else half the interval.
func (s *Soul) HeartbeatGrace() time.Duration {
	if s.Heartbeat != nil && s.Heartbeat.Grace.Duration > 0 {
		return s.Heartbeat.Grace.Duration
	}
	return s.HeartbeatInterval() / 2
}

// GenerateHeartbeatToken returns a fresh, URL-safe 256-bit ping token.
func GenerateHeartbeatToken() (string, error) {
	b := make([]byte, heartbeatTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HeartbeatTokenHash is the index key for a ping token: tokens are looked up
// by their SHA-256 so the comparison never branches on the secret itself.
func HeartbeatTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ValidHeartbeatToken reports whether token has the shape of a ping token.
func ValidHeartbeatToken(token string) bool {
	if len(token) < minHeartbeatToken || len(token) > maxHeartbeatToken {
		return false
	}
	for i := 0; i < len(token); i++ {
		c := token[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

// EnsureHeartbeatToken generates a ping token for a heartbeat soul that has
// none. It is a no-op for other types.
func (s *Soul) EnsureHeartbeatToken() error {
	if s.Type != CheckHeartbeat {
		return nil
	}
	if s.Heartbeat == nil {
		s.Heartbeat = &HeartbeatConfig{}
	}
	if s.Heartbeat.Token != "" {
		return nil
	}
	token, err := GenerateHeartbeatToken()
	if err != nil {
		return fmt.Errorf("generate heartbeat token: %w", err)
	}
	s.Heartbeat.Token = token
	return nil
}

func (s Soul) validateHeartbeat(prefix string) error {
	if s.Heartbeat == nil {
		return &ConfigError{Field: prefix + "heartbeat", Message: "heartbeat configuration is required for heartbeat souls"}
	}
	if s.Weight.Duration > MaxHeartbeatInterval {
		return &ConfigError{Field: prefix + "weight", Message: fmt.Sprintf("heartbeat interval must be at most %s", MaxHeartbeatInterval)}
	}
	grace := s.Heartbeat.Grace.Duration
	if grace < 0 {
		return &ConfigError{Field: prefix + "heartbeat.grace", Message: "grace period cannot be negative"}
	}
	if grace > 0 && (grace < MinHeartbeatGrace || grace > MaxHeartbeatGrace) {
		return &ConfigError{Field: prefix + "heartbeat.grace", Message: fmt.Sprintf("grace period must be between %s and %s (got %s; use a duration string such as \"5m\")", MinHeartbeatGrace, MaxHeartbeatGrace, grace)}
	}
	if !ValidHeartbeatToken(s.Heartbeat.Token) {
		return &ConfigError{Field: prefix + "heartbeat.token", Message: fmt.Sprintf("heartbeat token must be %d-%d URL-safe characters (A-Z, a-z, 0-9, - and _; e.g. openssl rand -hex 32)", minHeartbeatToken, maxHeartbeatToken)}
	}
	return nil
}
