package storage

import (
	"context"
	"errors"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
	"time"
)

func TestAuditLatestChronology(t *testing.T) {
	cases := []struct {
		name   string
		stamps []int64
		want   int64
		empty  bool
	}{
		{"control-same-width", []int64{1000000000000000001, 1000000000000000002}, 1000000000000000002, false},
		{"digit-boundary", []int64{999999999999999999, 1000000000000000000}, 1000000000000000000, false},
		{"reverse-insertion", []int64{1000000000000000000, 999999999999999999}, 1000000000000000000, false}, {"negative", []int64{-9, -10}, -9, false}, {"epoch", []int64{-1, 0, 1}, 1, false}, {"empty", nil, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			defer db.Close()
			ctx := context.Background()
			for _, stamp := range tc.stamps {
				if err := db.SaveJudgment(ctx, &core.Judgment{SoulID: "chronology", Timestamp: time.Unix(0, stamp), Status: core.SoulAlive}); err != nil {
					t.Fatal(err)
				}
			}
			latest, err := db.GetLatestJudgment(ctx, "default", "chronology")
			if tc.empty {
				var nf *core.NotFoundError
				if !errors.As(err, &nf) {
					t.Fatalf("expected NotFound, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := latest.Timestamp.UnixNano()
			t.Logf("EXPECTED: latest timestamp=%d ACTUAL: latest timestamp=%d", tc.want, got)
			if got != tc.want {
				t.Error("latest judgment is not chronological")
			}
		})
	}

}
