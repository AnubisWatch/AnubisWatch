package storage

import (
	"context"
	"fmt"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
	"testing/synctest"
	"time"
)

func uptimeUTCRegressionSeed(t *testing.T, db *CobaltDB, soul string, at time.Time, status core.SoulStatus) {
	t.Helper()
	if e := db.SaveJudgment(context.Background(), &core.Judgment{ID: soul, SoulID: soul, Timestamp: at, Status: status}); e != nil {
		t.Fatal(e)
	}
}
func TestUptimeHistoryUsesUTCCalendarDays(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db := newTestDB(t)
		defer db.Close()
		now := time.Now().UTC()
		day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		for i, tc := range []struct {
			at   time.Time
			zone int
		}{{day.Add(time.Hour), -4 * 3600}, {day.Add(23 * time.Hour), 4 * 3600}, {day.Add(12 * time.Hour), 0}} {
			soul := fmt.Sprintf("fixture-%d", i)
			uptimeUTCRegressionSeed(t, db, soul, tc.at.In(time.FixedZone("fixture", tc.zone)), core.SoulAlive)
			history, e := db.GetUptimeHistory(soul, 1)
			if e != nil || len(history) != 1 || history[0].Date != day.Format("2006-01-02") || history[0].Uptime != 100 {
				t.Fatal("timezone bucket")
			}
		}
		uptimeUTCRegressionSeed(t, db, "yesterday", day.Add(-time.Hour).In(time.FixedZone("fixture", 4*3600)), core.SoulDead)
		history, e := db.GetUptimeHistory("yesterday", 2)
		if e != nil || len(history) != 2 || history[0].Status != "dead" || history[1].Status != "unknown" {
			t.Fatal("previous UTC day")
		}
		history, e = db.GetUptimeHistory("empty", 0)
		if e != nil || len(history) != 0 {
			t.Fatal("zero days")
		}
	})
}
