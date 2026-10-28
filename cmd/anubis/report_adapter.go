package main

import (
	"context"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/sla"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

// CollectSLA lets the SLA report read rollups instead of raw judgments.
func (a *restStorageAdapter) CollectSLA(ctx context.Context, workspaceID, soulID string, start, end time.Time, excluded []core.TimeRange, rawSince, hourSince time.Time) (*sla.Tally, error) {
	return a.store.CollectSLA(ctx, workspaceID, soulID, start, end, excluded, storage.SLAHorizons{RawSince: rawSince, HourSince: hourSince})
}

// StreamJudgments lets the judgment export stream from storage an hour at a
// time.
func (a *restStorageAdapter) StreamJudgments(ctx context.Context, workspaceID, soulID string, start, end time.Time, limit int, fn func(*core.Judgment) error) (int, bool, error) {
	return a.store.StreamJudgments(ctx, workspaceID, soulID, start, end, limit, fn)
}
