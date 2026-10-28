package statuspage

import (
	"fmt"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"reflect"
	"testing"
)

type auditTimelineRepo struct {
	*mockRepository
	history map[string][]core.UptimeDay
}

func (r *auditTimelineRepo) GetUptimeHistory(id string, days int) ([]core.UptimeDay, error) {
	data, ok := r.history[id]
	if !ok {
		return nil, fmt.Errorf("history unavailable")
	}
	return data, nil
}
func auditTimelineOrder(t *testing.T, history map[string][]core.UptimeDay, ids []string) {
	t.Helper()
	h := NewHandler(&auditTimelineRepo{mockRepository: &mockRepository{}, history: history})
	souls := make([]core.SoulStatusInfo, 0, len(ids))
	wantCounts := make(map[core.UptimeDay]int)
	total := 0.0
	count := 0
	judged := 0
	for _, id := range ids {
		souls = append(souls, core.SoulStatusInfo{ID: id})
		for _, day := range history[id] {
			wantCounts[day]++
			count++
			// Days without judgments ("unknown") are not part of the average.
			if day.Status == "unknown" {
				continue
			}
			total += day.Uptime
			judged++
		}
	}
	got := h.buildUptimeData(&core.StatusPage{UptimeDays: 2}, souls)
	dates := make([]string, 0, len(got.Days))
	actualCounts := make(map[core.UptimeDay]int)
	ordered := true
	for i, day := range got.Days {
		dates = append(dates, day.Date)
		actualCounts[day]++
		if i > 0 && got.Days[i-1].Date > day.Date {
			ordered = false
		}
	}
	t.Logf("EXPECTED: chronological=%v count=%d ACTUAL: chronological=%v dates=%v count=%d", true, count, ordered, dates, len(got.Days))
	if !ordered {
		t.Error("regression mismatch")
	}
	if !reflect.DeepEqual(wantCounts, actualCounts) {
		t.Fatal("regression mismatch", "samples changed")
	}
	wantOverall := 0.0
	if judged > 0 {
		wantOverall = total / float64(judged)
	}
	if got.Overall != wantOverall {
		t.Fatal("regression mismatch", "overall uptime changed")
	}
}
func TestAuditUptimeTimelineOrder(t *testing.T) {
	history := map[string][]core.UptimeDay{
		"a": {{Date: "2026-09-01", Status: "dead", Uptime: 90}, {Date: "2026-09-02", Status: "operational", Uptime: 100}},
		"b": {{Date: "2026-09-01", Status: "dead", Uptime: 80}, {Date: "2026-09-02", Status: "operational", Uptime: 100}},
	}
	t.Run("single-soul-control", func(t *testing.T) { auditTimelineOrder(t, history, []string{"a"}) })
	t.Run("two-soul-overlap", func(t *testing.T) { auditTimelineOrder(t, history, []string{"a", "b"}) })
	t.Run("disjoint-history", func(t *testing.T) {
		auditTimelineOrder(t, map[string][]core.UptimeDay{"late": {{Date: "2026-09-02", Status: "operational", Uptime: 100}}, "early": {{Date: "2026-09-01", Status: "unknown", Uptime: 0}}}, []string{"late", "early"})
	})
	t.Run("repository-error-skipped", func(t *testing.T) { auditTimelineOrder(t, history, []string{"a", "missing", "b"}) })
	t.Run("empty-page", func(t *testing.T) { auditTimelineOrder(t, nil, nil) })
}
