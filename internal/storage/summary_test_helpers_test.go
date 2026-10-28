package storage

import (
	"context"
	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Test-only single-resolution seam; production saves use compound WAL batches.
// updateSummary updates the aggregated summary for a judgment
func (ts *TimeSeriesStore) updateSummary(ctx context.Context, j *core.Judgment, resolution TimeResolution) error {
	ts.summaryMu.Lock()
	defer ts.summaryMu.Unlock()
	key, data, err := ts.prepareSummary(ctx, j, resolution)
	if err != nil {
		return err
	}
	return ts.db.Put(key, data)
}
