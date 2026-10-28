package storage

import (
	"fmt"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Every other retention tier treats a non-positive period as "do not purge".
// The day tier is a string ("unlimited" or a duration) and a "0s" or negative
// value produced a cutoff of now or later, so the hourly cleanup deleted every
// daily summary the install had ever accumulated.
func TestRetentionDayNonPositiveKeepsDailySummaries(t *testing.T) {
	dayKey := func(age time.Duration) string {
		bucket := time.Now().Add(-age).Truncate(24 * time.Hour)
		return fmt.Sprintf("default/ts/soul-1/%s/%d", Resolution1Day, bucket.Unix())
	}
	old, recent := dayKey(400*24*time.Hour), dayKey(2*24*time.Hour)

	run := func(t *testing.T, day string) (oldKept, recentKept bool) {
		t.Helper()
		db := newTestDB(t)
		t.Cleanup(func() { db.Close() })
		for _, k := range []string{old, recent} {
			if err := db.Put(k, []byte(`{"count":1}`)); err != nil {
				t.Fatal(err)
			}
		}
		rm := NewRetentionManager(db, core.RetentionConfig{Day: day}, t.TempDir(), newTestLogger())
		rm.runCleanup()
		_, oldErr := db.Get(old)
		_, recentErr := db.Get(recent)
		return oldErr == nil, recentErr == nil
	}

	// Controls: a real period purges only what is older than it; "unlimited"
	// purges nothing.
	if oldKept, recentKept := run(t, "720h"); oldKept || !recentKept {
		t.Fatalf("control 720h: oldKept=%v recentKept=%v, want old purged and recent kept", oldKept, recentKept)
	}
	if oldKept, recentKept := run(t, "unlimited"); !oldKept || !recentKept {
		t.Fatalf("control unlimited: oldKept=%v recentKept=%v, want both kept", oldKept, recentKept)
	}

	for _, day := range []string{"0s", "0", "-1h", "-720h"} {
		t.Run(day, func(t *testing.T) {
			oldKept, recentKept := run(t, day)
			if !oldKept || !recentKept {
				t.Errorf("day=%q purged daily summaries: oldKept=%v recentKept=%v, want both kept", day, oldKept, recentKept)
			}
		})
	}
}
