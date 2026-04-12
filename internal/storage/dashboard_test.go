package storage

import (
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestDashboardCRUD(t *testing.T) {
	dir := t.TempDir()
	db, err := NewEngine(core.StorageConfig{Path: dir}, nil)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer db.Close()

	dashboard := &core.CustomDashboard{
		ID:          "dash-1",
		Name:        "Test Dashboard",
		WorkspaceID: "default",
		Description: "A test dashboard",
		Widgets: []core.WidgetConfig{
			{
				ID:    "w1",
				Title: "Total Souls",
				Type:  core.WidgetStat,
				Grid:  core.WidgetGrid{X: 0, Y: 0, Width: 3, Height: 2},
				Query: core.WidgetQuery{Source: "souls", Metric: "count", TimeRange: "24h"},
			},
		},
		RefreshSec: 30,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	// Save
	if err := db.SaveDashboard(dashboard); err != nil {
		t.Fatalf("SaveDashboard failed: %v", err)
	}

	// Get
	got, err := db.GetDashboard("dash-1")
	if err != nil {
		t.Fatalf("GetDashboard failed: %v", err)
	}
	if got.Name != dashboard.Name {
		t.Errorf("expected name %q, got %q", dashboard.Name, got.Name)
	}
	if len(got.Widgets) != 1 {
		t.Errorf("expected 1 widget, got %d", len(got.Widgets))
	}
	if got.Widgets[0].Title != "Total Souls" {
		t.Errorf("expected widget title %q, got %q", "Total Souls", got.Widgets[0].Title)
	}
	if got.RefreshSec != 30 {
		t.Errorf("expected refresh_sec 30, got %d", got.RefreshSec)
	}

	// Get non-existent
	_, err = db.GetDashboard("non-existent")
	if err == nil {
		t.Error("expected error for non-existent dashboard")
	}

	// List
	dashboard2 := &core.CustomDashboard{
		ID:         "dash-2",
		Name:       "Second Dashboard",
		Widgets:    []core.WidgetConfig{},
		RefreshSec: 0,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := db.SaveDashboard(dashboard2); err != nil {
		t.Fatalf("SaveDashboard 2 failed: %v", err)
	}

	list, err := db.ListDashboards()
	if err != nil {
		t.Fatalf("ListDashboards failed: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 dashboards, got %d", len(list))
	}

	// Delete
	if err := db.DeleteDashboard("dash-1"); err != nil {
		t.Fatalf("DeleteDashboard failed: %v", err)
	}

	list, err = db.ListDashboards()
	if err != nil {
		t.Fatalf("ListDashboards after delete failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 dashboard after delete, got %d", len(list))
	}
}

func TestDashboardNoCtx(t *testing.T) {
	dir := t.TempDir()
	db, err := NewEngine(core.StorageConfig{Path: dir}, nil)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer db.Close()

	dashboard := &core.CustomDashboard{
		ID:         "dash-nc",
		Name:       "NoCtx Test",
		Widgets:    []core.WidgetConfig{},
		RefreshSec: 15,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	if err := db.SaveDashboardNoCtx(dashboard); err != nil {
		t.Fatalf("SaveDashboardNoCtx failed: %v", err)
	}

	got, err := db.GetDashboardNoCtx("dash-nc")
	if err != nil {
		t.Fatalf("GetDashboardNoCtx failed: %v", err)
	}
	if got.Name != "NoCtx Test" {
		t.Errorf("expected name %q, got %q", "NoCtx Test", got.Name)
	}

	list, err := db.ListDashboardsNoCtx()
	if err != nil {
		t.Fatalf("ListDashboardsNoCtx failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 dashboard, got %d", len(list))
	}

	if err := db.DeleteDashboardNoCtx("dash-nc"); err != nil {
		t.Fatalf("DeleteDashboardNoCtx failed: %v", err)
	}

	list, err = db.ListDashboardsNoCtx()
	if err != nil {
		t.Fatalf("ListDashboardsNoCtx after delete failed: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0 dashboards after delete, got %d", len(list))
	}
}

func TestDashboardWidgetTypes(t *testing.T) {
	dir := t.TempDir()
	db, err := NewEngine(core.StorageConfig{Path: dir}, nil)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer db.Close()

	widgetTypes := []core.WidgetType{
		core.WidgetLineChart,
		core.WidgetBarChart,
		core.WidgetGauge,
		core.WidgetStat,
		core.WidgetTable,
	}

	dashboard := &core.CustomDashboard{
		ID:        "dash-types",
		Name:      "Widget Types Test",
		Widgets:   make([]core.WidgetConfig, len(widgetTypes)),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	for i, wt := range widgetTypes {
		dashboard.Widgets[i] = core.WidgetConfig{
			ID:    string(wt),
			Type:  wt,
			Grid:  core.WidgetGrid{X: 0, Y: i, Width: 6, Height: 2},
			Query: core.WidgetQuery{Source: "souls", Metric: "count", TimeRange: "24h"},
		}
	}

	if err := db.SaveDashboard(dashboard); err != nil {
		t.Fatalf("SaveDashboard failed: %v", err)
	}

	got, err := db.GetDashboard("dash-types")
	if err != nil {
		t.Fatalf("GetDashboard failed: %v", err)
	}

	for i, wt := range widgetTypes {
		if got.Widgets[i].Type != wt {
			t.Errorf("widget %d: expected type %q, got %q", i, wt, got.Widgets[i].Type)
		}
	}
}
