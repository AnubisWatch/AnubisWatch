package core

import (
	"sort"
	"time"
)

// IncidentImpact is how badly an operator-posted status page incident affects
// the components it names.
type IncidentImpact string

const (
	ImpactNone     IncidentImpact = "none"
	ImpactMinor    IncidentImpact = "minor"
	ImpactMajor    IncidentImpact = "major"
	ImpactCritical IncidentImpact = "critical"
)

// ValidIncidentImpact reports whether v is one of the documented impacts.
func ValidIncidentImpact(v IncidentImpact) bool {
	switch v {
	case ImpactNone, ImpactMinor, ImpactMajor, ImpactCritical:
		return true
	}
	return false
}

// ValidStatusPageIncidentStatus reports whether v is a lifecycle status an
// operator may post: investigating, identified, monitoring or resolved.
func ValidStatusPageIncidentStatus(v IncidentStatus) bool {
	switch v {
	case StatusInvestigating, StatusIdentified, StatusMonitoring, StatusResolved:
		return true
	}
	return false
}

// ImpactRank orders impacts; a larger rank is worse.
func ImpactRank(v IncidentImpact) int {
	switch v {
	case ImpactMinor:
		return 1
	case ImpactMajor:
		return 2
	case ImpactCritical:
		return 3
	}
	return 0
}

// EffectiveImpact is the incident's impact, falling back to its legacy
// severity for incidents written before impact existed.
func (i StatusIncident) EffectiveImpact() IncidentImpact {
	if i.Impact != "" {
		return i.Impact
	}
	switch i.Severity {
	case SeverityCritical:
		return ImpactCritical
	case "high":
		return ImpactMajor
	case SeverityWarning, "medium":
		return ImpactMinor
	}
	return ImpactNone
}

// IsActive reports whether the incident is still open (any status but resolved).
func (i StatusIncident) IsActive() bool {
	switch i.Status {
	case StatusOngoing, StatusInvestigating, StatusIdentified, StatusMonitoring:
		return true
	}
	return false
}

// Announcement phases, derived from the schedule at read time.
const (
	AnnouncementUpcoming   = "upcoming"
	AnnouncementInProgress = "in_progress"
	AnnouncementCompleted  = "completed"
)

// StatusAnnouncement is a scheduled maintenance notice shown on a status page.
// It may be linked to a one-off maintenance window, whose schedule and souls
// it then mirrors.
type StatusAnnouncement struct {
	ID                  string    `json:"id" yaml:"id"`
	Title               string    `json:"title" yaml:"title"`
	Message             string    `json:"message" yaml:"message"`
	AffectedSouls       []string  `json:"affected_souls" yaml:"affected_souls"`
	StartsAt            time.Time `json:"starts_at" yaml:"starts_at"`
	EndsAt              time.Time `json:"ends_at" yaml:"ends_at"`
	MaintenanceWindowID string    `json:"maintenance_window_id,omitempty" yaml:"maintenance_window_id,omitempty"`
	CreatedAt           time.Time `json:"created_at" yaml:"-"`
	UpdatedAt           time.Time `json:"updated_at" yaml:"-"`
	// Phase is derived on the way out (upcoming, in_progress, completed) and
	// never persisted.
	Phase string `json:"phase,omitempty" yaml:"-"`
}

// PhaseAt returns the announcement's phase at now.
func (a StatusAnnouncement) PhaseAt(now time.Time) string {
	switch {
	case now.Before(a.StartsAt):
		return AnnouncementUpcoming
	case now.Before(a.EndsAt):
		return AnnouncementInProgress
	default:
		return AnnouncementCompleted
	}
}

// StatusIncidentHistoryWindow is how far back the public page lists incidents
// and completed maintenance.
const StatusIncidentHistoryWindow = 90 * 24 * time.Hour

// PublicIncidentHistory returns the page's incidents that started or were
// resolved within the history window, newest first.
func PublicIncidentHistory(incidents []StatusIncident, now time.Time) []StatusIncident {
	cutoff := now.Add(-StatusIncidentHistoryWindow)
	out := make([]StatusIncident, 0, len(incidents))
	for _, inc := range incidents {
		recent := !inc.StartedAt.Before(cutoff) || inc.IsActive() ||
			(inc.ResolvedAt != nil && !inc.ResolvedAt.Before(cutoff))
		if recent {
			out = append(out, inc)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

// PublicAnnouncements returns announcements that are upcoming, in progress or
// completed within the history window, with Phase set, soonest first.
func PublicAnnouncements(announcements []StatusAnnouncement, now time.Time) []StatusAnnouncement {
	cutoff := now.Add(-StatusIncidentHistoryWindow)
	out := make([]StatusAnnouncement, 0, len(announcements))
	for _, a := range announcements {
		if a.EndsAt.Before(cutoff) {
			continue
		}
		a.Phase = a.PhaseAt(now)
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartsAt.Before(out[j].StartsAt) })
	return out
}

// CompactStatusPage drops what the public page no longer shows: resolved
// incidents that ended before the history window, and announcements that
// ended before it. Active incidents and upcoming work are always kept. It
// reports how many records were removed.
func CompactStatusPage(page *StatusPage, now time.Time) int {
	cutoff := now.Add(-StatusIncidentHistoryWindow)
	removed := 0
	incidents := page.Incidents[:0:0]
	for _, inc := range page.Incidents {
		ended := inc.StartedAt
		if inc.ResolvedAt != nil {
			ended = *inc.ResolvedAt
		}
		if n := len(inc.Updates); n > 0 && inc.Updates[n-1].UpdatedAt.After(ended) {
			ended = inc.Updates[n-1].UpdatedAt
		}
		if !inc.IsActive() && ended.Before(cutoff) {
			removed++
			continue
		}
		incidents = append(incidents, inc)
	}
	announcements := page.Announcements[:0:0]
	for _, a := range page.Announcements {
		if a.EndsAt.Before(cutoff) {
			removed++
			continue
		}
		announcements = append(announcements, a)
	}
	if removed > 0 {
		page.Incidents, page.Announcements = incidents, announcements
	}
	return removed
}
