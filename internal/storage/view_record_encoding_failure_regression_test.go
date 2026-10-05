package storage

import (
	"bytes"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"math"
	"testing"
	"time"
)

func viewRecordEncodingRegressionSave(db *CobaltDB, kind, id string, value any) (func() error, string) {
	if kind == "dashboard" {
		return func() error {
			return db.SaveDashboard(&core.CustomDashboard{ID: id, Widgets: []core.WidgetConfig{{ID: "w", Thresholds: []core.WidgetThreshold{{Value: value.(float64)}}}}})
		}, "default/dashboards/" + id
	}
	return func() error {
		return db.SaveMaintenanceWindow(&core.MaintenanceWindow{ID: id, StartTime: value.(time.Time)})
	}, "default/maintenance/" + id
}
func viewRecordEncodingRegressionValues(kind string) (any, []any) {
	if kind == "dashboard" {
		return float64(0), []any{math.NaN(), math.Inf(1), math.Inf(-1)}
	}
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), []any{time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC)}
}
func TestDashboardMaintenanceEncodingPreservesRecords(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	for _, kind := range []string{"dashboard", "maintenance"} {
		valid, bad := viewRecordEncodingRegressionValues(kind)
		save, key := viewRecordEncodingRegressionSave(db, kind, "fixture-"+kind, valid)
		if e := save(); e != nil {
			t.Fatal(e)
		}
		before, e := db.Get(key)
		if e != nil {
			t.Fatal(e)
		}
		for _, value := range bad {
			invalid, _ := viewRecordEncodingRegressionSave(db, kind, "fixture-"+kind, value)
			if e := invalid(); e == nil {
				t.Fatal("missing error")
			}
			after, e := db.Get(key)
			if e != nil || !bytes.Equal(before, after) {
				t.Fatal("failed update clobbered record")
			}
		}
		invalid, newKey := viewRecordEncodingRegressionSave(db, kind, "invalid-"+kind, bad[0])
		if e := invalid(); e == nil {
			t.Fatal("invalid create succeeded")
		}
		if _, e := db.Get(newKey); e == nil {
			t.Fatal("invalid record persisted")
		}
		db.mu.RLock()
		_, di := db.dashboardIndex["invalid-"+kind]
		_, mi := db.maintenanceIndex["invalid-"+kind]
		db.mu.RUnlock()
		if di || mi {
			t.Fatal("invalid record indexed")
		}
	}
	for _, value := range []float64{0, -1, 1.5} {
		save, _ := viewRecordEncodingRegressionSave(db, "dashboard", "valid-dashboard", value)
		if e := save(); e != nil {
			t.Fatal(e)
		}
		d, e := db.GetDashboard("valid-dashboard")
		if e != nil || d.Widgets[0].Thresholds[0].Value != value {
			t.Fatal("supported threshold")
		}
	}
	for _, year := range []int{0, 2026, 9999} {
		instant := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
		save, _ := viewRecordEncodingRegressionSave(db, "maintenance", "valid-maintenance", instant)
		if e := save(); e != nil {
			t.Fatal(e)
		}
		m, e := db.GetMaintenanceWindow("valid-maintenance")
		if e != nil || !m.StartTime.Equal(instant) {
			t.Fatal("supported timestamp")
		}
	}
}
