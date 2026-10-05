package api

import (
	"encoding/json"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
	"time"
)

type audit50BucketStore struct {
	*mockStorage
	rows []*core.Judgment
}

func (s *audit50BucketStore) ListJudgmentsNoCtx(_ string, _, _ time.Time, _ int) ([]*core.Judgment, error) {
	return s.rows, nil
}
func audit50BucketQuery(t *testing.T, stamps []time.Time) []map[string]any {
	t.Helper()
	store := &audit50BucketStore{mockStorage: newMockStorage()}
	store.souls["s1"] = &core.Soul{ID: "s1", WorkspaceID: "default"}
	for _, ts := range stamps {
		store.rows = append(store.rows, &core.Judgment{SoulID: "s1", Status: core.SoulAlive, Timestamp: ts, Duration: time.Millisecond})
	}
	server := newTestServerWithStorage(store)
	result, err := server.queryJudgments(core.WidgetQuery{TimeRange: "7d", Filters: map[string]string{"soul_id": "s1"}}, "default")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	return got
}
func TestDashboardJudgmentBucketsPreserveDates(t *testing.T) {
	ts := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		stamps []time.Time
		want   map[string]float64
	}{
		{"same hour", []time.Time{ts, ts.Add(10 * time.Minute)}, map[string]float64{"2024-01-01T10:00:00Z": 2}},
		{"next day", []time.Time{ts, ts.Add(24 * time.Hour)}, map[string]float64{"2024-01-01T10:00:00Z": 1, "2024-01-02T10:00:00Z": 1}},
		{"midnight", []time.Time{ts.Add(13 * time.Hour), ts.Add(14 * time.Hour)}, map[string]float64{"2024-01-01T23:00:00Z": 1, "2024-01-02T00:00:00Z": 1}},
		{"same instant another zone", []time.Time{ts, ts.In(time.FixedZone("offset", 2*3600))}, map[string]float64{"2024-01-01T10:00:00Z": 2}},
		{"empty", nil, map[string]float64{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := audit50BucketQuery(t, tc.stamps)
			if len(got) != len(tc.want) {
				t.Fatalf("EXPECTED: %d buckets ACTUAL: %v", len(tc.want), got)
			}
			for _, b := range got {
				key := b["time"].(string)
				want, ok := tc.want[key]
				if !ok || b["count"] != want {
					t.Fatalf("EXPECTED: %v ACTUAL: %v", tc.want, got)
				}
			}
		})
	}
}
