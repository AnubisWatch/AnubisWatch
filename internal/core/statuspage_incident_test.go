package core

import (
	"testing"
	"time"
)

func TestCompactStatusPageKeepsOnlyPublicHistory(t *testing.T) {
	now := time.Now()
	old := now.Add(-91 * 24 * time.Hour)
	lateUpdate := now.Add(-5 * 24 * time.Hour)
	page := &StatusPage{
		Incidents: []StatusIncident{
			{ID: "aged", Status: StatusResolved, StartedAt: old, ResolvedAt: &old},
			{ID: "active", Status: StatusInvestigating, StartedAt: old},
			{ID: "late-update", Status: StatusResolved, StartedAt: old, ResolvedAt: &old,
				Updates: []IncidentUpdate{{UpdatedAt: lateUpdate}}},
		},
		Announcements: []StatusAnnouncement{
			{ID: "aged-a", StartsAt: old, EndsAt: old.Add(time.Hour)},
			{ID: "soon", StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour)},
		},
	}
	if n := CompactStatusPage(page, now); n != 2 {
		t.Fatalf("removed %d, want 2", n)
	}
	if len(page.Incidents) != 2 || page.Incidents[0].ID != "active" || page.Incidents[1].ID != "late-update" ||
		len(page.Announcements) != 1 || page.Announcements[0].ID != "soon" {
		t.Fatalf("kept %+v / %+v", page.Incidents, page.Announcements)
	}
	if n := CompactStatusPage(page, now); n != 0 {
		t.Fatalf("second pass removed %d", n)
	}
}
