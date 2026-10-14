package main

// Regression coverage for storage maintenance. The retention manager and the
// time-series compaction were implemented but never started, so check results
// accumulated forever and the storage.timeseries settings did nothing. They
// are now built in BuildServerDependencies and started with the server, with
// a 90-day raw default so the status page's 90 days of uptime bars, which are
// computed from raw results, keep their history.

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

func buildMaintenanceDeps(t *testing.T) *ServerDependencies {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", filepath.Join(dir, "data"))
	configPath := filepath.Join(dir, "anubis.json")
	config := `{"storage": {"path": "` + filepath.ToSlash(filepath.Join(dir, "data")) + `"},
		"server": {"host": "127.0.0.1", "port": 0, "grpc_port": 0}}`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	deps, err := BuildServerDependencies(ServerOptions{
		ConfigPath: configPath,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("BuildServerDependencies: %v", err)
	}
	t.Cleanup(func() { deps.Store.Close() })
	return deps
}

func TestServerDependenciesIncludeStorageMaintenance(t *testing.T) {
	deps := buildMaintenanceDeps(t)
	if deps.TimeSeries == nil || deps.Retention == nil {
		t.Fatalf("storage maintenance not built: timeseries=%v retention=%v", deps.TimeSeries != nil, deps.Retention != nil)
	}
	if got := deps.Config.Storage.TimeSeries.Retention.Raw.Duration; got != 90*24*time.Hour {
		t.Fatalf("default raw retention = %s, want 2160h (90d)", got)
	}
}

func TestDefaultRetentionKeepsNinetyDaysOfRawResults(t *testing.T) {
	deps := buildMaintenanceDeps(t)
	ctx := context.Background()
	now := time.Now()
	for id, age := range map[string]time.Duration{
		"expired": 100 * 24 * time.Hour,
		"kept":    60 * 24 * time.Hour, // inside the status page's 90-day window
		"fresh":   time.Hour,
	} {
		j := &core.Judgment{ID: id, SoulID: "soul", WorkspaceID: "default", Status: core.SoulAlive, Timestamp: now.Add(-age)}
		if err := deps.Store.SaveJudgment(ctx, j); err != nil {
			t.Fatalf("SaveJudgment(%s): %v", id, err)
		}
	}

	// The retention loop cleans once before it first checks for stop, so
	// Start then Stop runs exactly one pass.
	deps.Retention.Start()
	deps.Retention.Stop()

	left, err := deps.Store.ListJudgmentsNoCtx("soul", time.Time{}, now.Add(time.Hour), 0)
	if err != nil {
		t.Fatalf("ListJudgmentsNoCtx: %v", err)
	}
	ids := map[string]bool{}
	for _, j := range left {
		ids[j.ID] = true
	}
	if ids["expired"] {
		t.Error("judgment 100 days old survived retention: retention did not run")
	}
	if !ids["kept"] || !ids["fresh"] {
		t.Errorf("retention deleted results inside 90 days: remaining %v", ids)
	}
}

func TestProbeJudgmentsFeedTimeSeriesSummaries(t *testing.T) {
	deps := buildMaintenanceDeps(t)
	adapter := &probeStorageAdapter{store: deps.Store, timeseries: deps.TimeSeries}
	at := time.Now().Add(-10 * time.Minute)
	j := &core.Judgment{ID: "j1", SoulID: "soul", WorkspaceID: "default", Status: core.SoulAlive, Timestamp: at, Duration: 40 * time.Millisecond}
	if err := adapter.SaveJudgment(context.Background(), j); err != nil {
		t.Fatalf("SaveJudgment: %v", err)
	}

	summaries, err := deps.TimeSeries.QuerySummaries(context.Background(), "default", "soul",
		storage.Resolution1Min, at.Add(-time.Minute), at.Add(time.Minute))
	if err != nil {
		t.Fatalf("QuerySummaries: %v", err)
	}
	if len(summaries) != 1 || summaries[0].Count != 1 {
		t.Fatalf("1-minute summaries = %d, want one counting the judgment: compaction would have nothing to downsample", len(summaries))
	}
	if got, _ := deps.Store.ListJudgmentsNoCtx("soul", time.Time{}, time.Now(), 0); len(got) != 1 {
		t.Fatalf("raw judgment not saved alongside the summary (%d found)", len(got))
	}
}

func TestStopWithoutStartDoesNotHang(t *testing.T) {
	deps := buildMaintenanceDeps(t)
	srv := NewServer(deps)
	done := make(chan struct{})
	go func() {
		_ = srv.Stop(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Server.Stop hung waiting for a retention loop that was never started")
	}
}

func TestDefaultCompactionAndRetentionDoNotConflict(t *testing.T) {
	cfg := buildMaintenanceDeps(t).Config // loaded config, defaults applied
	if conflicts := compactionRetentionConflicts(cfg.Storage.TimeSeries); len(conflicts) != 0 {
		t.Fatalf("default config conflicts: %v", conflicts)
	}

	// The previous hour_to_day default equalled the hourly retention, so a
	// day's first hours were deleted before the day was rolled up.
	cfg.Storage.TimeSeries.Compaction.HourToDay = core.Duration{Duration: 365 * 24 * time.Hour}
	conflicts := compactionRetentionConflicts(cfg.Storage.TimeSeries)
	if len(conflicts) != 1 || !strings.Contains(conflicts[0], "1hour summaries") {
		t.Fatalf("conflicts = %v, want one about 1hour summaries", conflicts)
	}
}

// The REST store adapter must expose TallyChecks with the exact signature the
// soul uptime endpoint looks for; otherwise the endpoint silently falls back
// to decoding a month of raw judgments per request.
var _ interface {
	TallyChecks(soulID string, ranges []core.TimeRange) ([]core.CheckTally, error)
} = (*restStorageAdapter)(nil)
