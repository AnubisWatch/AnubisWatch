package statuspage

import (
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Regression: a soul inside an active maintenance window was shown with its
// latest judgment's status (often "dead", turning the page red), because the
// embalmed status was never set anywhere.

type maintenanceRepo struct {
	*mockRepository
	windows []*core.MaintenanceWindow
}

func (r *maintenanceRepo) ListMaintenanceWindows() ([]*core.MaintenanceWindow, error) {
	return r.windows, nil
}

func TestStatusPage_SoulUnderMaintenanceIsEmbalmed(t *testing.T) {
	now := time.Now()
	active := func(ws string, soulIDs ...string) *core.MaintenanceWindow {
		return &core.MaintenanceWindow{ID: "w-" + ws, WorkspaceID: ws, SoulIDs: soulIDs, Enabled: true, StartTime: now.Add(-time.Hour), EndTime: now.Add(time.Hour)}
	}
	cases := []struct {
		name    string
		windows []*core.MaintenanceWindow
		want    map[string]string
	}{
		{"no window", nil, map[string]string{"soul-1": "alive", "soul-2": "alive"}},
		{"window on soul-1", []*core.MaintenanceWindow{active("default", "soul-1")}, map[string]string{"soul-1": "embalmed", "soul-2": "alive"}},
		{"other workspace window", []*core.MaintenanceWindow{active("other")}, map[string]string{"soul-1": "alive", "soul-2": "alive"}},
		{"expired window", []*core.MaintenanceWindow{{ID: "old", Enabled: true, StartTime: now.Add(-2 * time.Hour), EndTime: now.Add(-time.Hour)}}, map[string]string{"soul-1": "alive", "soul-2": "alive"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &maintenanceRepo{mockRepository: &mockRepository{}, windows: tc.windows}
			h := NewHandler(repo)
			page, _ := repo.GetStatusPageBySlug("test")
			data, err := h.buildStatusPageData(page)
			if err != nil {
				t.Fatal(err)
			}
			for _, soul := range data.Souls {
				if soul.Status != tc.want[soul.ID] {
					t.Errorf("%s status = %q, want %q", soul.ID, soul.Status, tc.want[soul.ID])
				}
			}
			if data.Status.Status != "operational" {
				t.Errorf("maintenance must not mark the page as an outage, got %q", data.Status.Status)
			}
		})
	}
}
