package core

import (
	"errors"
	"fmt"
	"time"
)

// Quota names, as they appear under tenants.default_quotas in the config and in
// QuotaExceededError.Quota.
const (
	QuotaMaxSouls         = "max_souls"
	QuotaMaxJourneys      = "max_journeys"
	QuotaMaxAlertChannels = "max_alert_channels"
	QuotaMaxTeamMembers   = "max_team_members"
	QuotaCheckIntervalMin = "check_interval_min"
)

// QuotaExceededError is returned by a write that would take a workspace past
// one of its quotas. Every API surface maps it the same way: HTTP 409, gRPC
// ResourceExhausted, an MCP tool error carrying this message.
type QuotaExceededError struct {
	Workspace string
	// Quota is the config key that refused the write (QuotaMaxSouls, ...).
	Quota string
	// Limit and Current describe a count quota: the workspace already holds
	// Current resources and may hold at most Limit.
	Limit   int
	Current int
	// Interval and Floor describe a check_interval_min refusal.
	Interval time.Duration
	Floor    time.Duration
}

func (e *QuotaExceededError) Error() string {
	if e.Quota == QuotaCheckIntervalMin {
		return fmt.Sprintf("workspace quota %s exceeded: check interval %s is below the minimum of %s for workspace %q",
			e.Quota, e.Interval, e.Floor, e.Workspace)
	}
	return fmt.Sprintf("workspace quota %s exceeded: workspace %q already has %d of %d allowed",
		e.Quota, e.Workspace, e.Current, e.Limit)
}

// AsQuotaExceeded reports whether err (or anything it wraps) is a quota refusal.
func AsQuotaExceeded(err error) (*QuotaExceededError, bool) {
	var qe *QuotaExceededError
	if errors.As(err, &qe) {
		return qe, true
	}
	return nil, false
}

// QuotaCounter is one count quota: Used resources against Limit. Limit 0 means
// unlimited.
type QuotaCounter struct {
	Used  int `json:"used"`
	Limit int `json:"limit"`
}

// WorkspaceQuotaUsage is what GET /api/v1/quota reports for the caller's
// workspace. Enforced is false when tenants.enabled is off, in which case
// every limit is 0 (unlimited).
type WorkspaceQuotaUsage struct {
	Workspace     string       `json:"workspace"`
	Enforced      bool         `json:"enforced"`
	Souls         QuotaCounter `json:"souls"`
	Journeys      QuotaCounter `json:"journeys"`
	AlertChannels QuotaCounter `json:"alert_channels"`
	// TeamMembers counts members plus pending invitations.
	TeamMembers      QuotaCounter `json:"team_members"`
	CheckIntervalMin Duration     `json:"check_interval_min"`
	RetentionDays    int          `json:"retention_days"`
}

// CheckQuotaCount refuses adding one more resource to a workspace that already
// holds current of them when limit is positive and reached. limit <= 0 is
// unlimited.
func CheckQuotaCount(workspace, quota string, limit, current int) error {
	if limit <= 0 || current < limit {
		return nil
	}
	return &QuotaExceededError{Workspace: workspace, Quota: quota, Limit: limit, Current: current}
}

// CheckQuotaInterval refuses a check interval below floor. floor <= 0 means no
// floor; interval <= 0 is left to the resource's own defaults and validation.
func CheckQuotaInterval(workspace string, floor, interval time.Duration) error {
	if floor <= 0 || interval <= 0 || interval >= floor {
		return nil
	}
	return &QuotaExceededError{Workspace: workspace, Quota: QuotaCheckIntervalMin, Interval: interval, Floor: floor}
}

// CapRetention lowers every time-series retention tier to at most days days.
// A tier kept forever (0, or "unlimited" for the daily tier) is capped too.
// days <= 0 leaves the configuration unchanged. It reports whether anything
// changed.
func CapRetention(r *RetentionConfig, days int) bool {
	if r == nil || days <= 0 {
		return false
	}
	limit := time.Duration(days) * 24 * time.Hour
	changed := false
	for _, tier := range []*Duration{&r.Raw, &r.Minute, &r.FiveMin, &r.Hour} {
		if tier.Duration <= 0 || tier.Duration > limit {
			tier.Duration = limit
			changed = true
		}
	}
	day, err := time.ParseDuration(r.Day)
	if r.Day == "" || r.Day == "unlimited" || err != nil || day <= 0 || day > limit {
		r.Day = limit.String()
		changed = true
	}
	return changed
}
