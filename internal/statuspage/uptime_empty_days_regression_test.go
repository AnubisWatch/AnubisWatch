package statuspage

// Regression coverage for the per-soul uptime figure published on a status page.
//
// buildStatusPageData averages UptimeDay.Uptime across every day the repository
// returns. The repository returns one entry per requested day, including days
// that carry no judgments at all (Status "unknown"), so every empty day was
// being charged as 24 hours of downtime. Consequences on a public page:
//
//   - a healthy monitor created today reported 100/30 = 3.33% uptime;
//   - a monitor whose only judgment is weeks old reported 0%;
//   - a monitor with one dead and one alive judgment today reported 50/30 =
//     1.67% instead of 50%.
//
// Only days that actually recorded judgments may contribute. These tests drive
// the real storage.CobaltDB through the real Repository interface into the real
// Handler.buildStatusPageData, so they cover the arithmetic as shipped.

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

// uptimeRepo adapts a real *storage.CobaltDB to Repository, so the history the
// handler averages is produced by the production GetUptimeHistory.
type uptimeRepo struct {
	db *storage.CobaltDB
}

func (r *uptimeRepo) GetStatusPageByDomain(string) (*core.StatusPage, error) {
	return nil, errors.New("not found")
}

func (r *uptimeRepo) GetStatusPageBySlug(string) (*core.StatusPage, error) {
	return nil, errors.New("not found")
}

func (r *uptimeRepo) GetSoul(id string) (*core.Soul, error) {
	return r.db.GetSoulNoCtx(id)
}

func (r *uptimeRepo) GetSoulJudgments(soulID string, limit int) ([]core.Judgment, error) {
	return r.db.GetSoulJudgments(soulID, limit)
}

func (r *uptimeRepo) GetIncidentsByPage(string) ([]core.Incident, error) { return nil, nil }

func (r *uptimeRepo) GetUptimeHistory(soulID string, days int) ([]core.UptimeDay, error) {
	return r.db.GetUptimeHistory(soulID, days)
}

func (r *uptimeRepo) SaveSubscription(*core.StatusPageSubscription) error { return nil }

func (r *uptimeRepo) GetSubscriptionsByPage(string) ([]*core.StatusPageSubscription, error) {
	return nil, nil
}

func (r *uptimeRepo) DeleteSubscription(string) error { return nil }

func newUptimeDB(t *testing.T) *storage.CobaltDB {
	t.Helper()
	db, err := storage.NewEngine(
		core.StorageConfig{Path: t.TempDir()},
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func addSoul(t *testing.T, db *storage.CobaltDB, id string) {
	t.Helper()
	if err := db.SaveSoul(context.Background(), &core.Soul{
		ID: id, Name: "API", Type: core.CheckHTTP,
		Target: "http://example.com", WorkspaceID: "default", Enabled: true,
	}); err != nil {
		t.Fatalf("SaveSoul: %v", err)
	}
}

func addJudgment(t *testing.T, db *storage.CobaltDB, soulID string, status core.SoulStatus, at time.Time) {
	t.Helper()
	if err := db.SaveJudgment(context.Background(), &core.Judgment{
		ID: core.GenerateID(), SoulID: soulID, WorkspaceID: "default",
		Timestamp: at.UTC(), Status: status,
	}); err != nil {
		t.Fatalf("SaveJudgment: %v", err)
	}
}

func uptimeFor(t *testing.T, db *storage.CobaltDB, soulID string) float64 {
	t.Helper()
	h := NewHandler(&uptimeRepo{db: db})
	data, err := h.buildStatusPageData(&core.StatusPage{
		ID: "p1", Name: "Status", Visibility: core.VisibilityPublic,
		Souls: []string{soulID}, UptimeDays: 30,
		Theme: core.GetDefaultTheme(), WorkspaceID: "default",
	})
	if err != nil {
		t.Fatalf("buildStatusPageData: %v", err)
	}
	if len(data.Souls) != 1 {
		t.Fatalf("expected 1 soul, got %d", len(data.Souls))
	}
	return data.Souls[0].UptimePercent
}

// TestStatusPageUptimeIgnoresDaysWithoutJudgments pins that empty days are not
// charged as downtime for a healthy monitor created today.
func TestStatusPageUptimeIgnoresDaysWithoutJudgments(t *testing.T) {
	db := newUptimeDB(t)
	addSoul(t, db, "soul-new")
	addJudgment(t, db, "soul-new", core.SoulAlive, time.Now())

	if got := uptimeFor(t, db, "soul-new"); got != 100 {
		t.Fatalf("healthy new monitor reported %.2f%% uptime; days without judgments must not count as downtime", got)
	}
}

// TestStatusPageUptimeForIdleMonitor covers the boundary where the only
// judgment is old: it must not report 0% uptime publicly.
func TestStatusPageUptimeForIdleMonitor(t *testing.T) {
	db := newUptimeDB(t)
	addSoul(t, db, "soul-idle")
	addJudgment(t, db, "soul-idle", core.SoulAlive, time.Now().Add(-20*24*time.Hour))

	if got := uptimeFor(t, db, "soul-idle"); got != 100 {
		t.Fatalf("monitor with only a healthy judgment reported %.2f%% uptime", got)
	}
}

// TestStatusPageUptimeStillCountsRealDowntime is the branch check that guards
// against a fix that simply hardcodes 100: genuine failures must still lower
// the published figure.
func TestStatusPageUptimeStillCountsRealDowntime(t *testing.T) {
	db := newUptimeDB(t)
	addSoul(t, db, "soul-flaky")
	now := time.Now().UTC()
	addJudgment(t, db, "soul-flaky", core.SoulDead, now)
	addJudgment(t, db, "soul-flaky", core.SoulAlive, now.Add(-time.Minute))

	got := uptimeFor(t, db, "soul-flaky")
	if got != 50 {
		t.Fatalf("one dead + one alive today should publish 50%% uptime, got %.2f%%", got)
	}
}

// TestStatusPageUptimeForFullyPopulatedHistory is the control: a monitor with
// 30 days of all-alive history keeps publishing 100%, so the change cannot be
// satisfied by breaking the well-covered case.
func TestStatusPageUptimeForFullyPopulatedHistory(t *testing.T) {
	db := newUptimeDB(t)
	addSoul(t, db, "soul-full")
	now := time.Now().UTC()
	for day := 0; day < 30; day++ {
		addJudgment(t, db, "soul-full", core.SoulAlive, now.AddDate(0, 0, -day).Add(time.Hour))
	}

	if got := uptimeFor(t, db, "soul-full"); got != 100 {
		t.Fatalf("fully populated all-alive history reported %.2f%% uptime, want 100%%", got)
	}
}

// TestStatusPageUptimeWithRepositoryError pins the fallback: when history cannot
// be read the published figure stays at the 100% default rather than dropping
// to zero.
func TestStatusPageUptimeWithRepositoryError(t *testing.T) {
	h := NewHandler(&uptimeErrorRepo{})
	data, err := h.buildStatusPageData(&core.StatusPage{
		ID: "p1", Name: "Status", Visibility: core.VisibilityPublic,
		Souls: []string{"soul-1"}, UptimeDays: 30,
		Theme: core.GetDefaultTheme(), WorkspaceID: "default",
	})
	if err != nil {
		t.Fatalf("buildStatusPageData: %v", err)
	}
	if data.Souls[0].UptimePercent != 100 {
		t.Fatalf("history error should keep the 100%% default, got %.2f%%", data.Souls[0].UptimePercent)
	}
}

// uptimeErrorRepo fails history lookups.
type uptimeErrorRepo struct {
	mockRepository
}

func (r *uptimeErrorRepo) GetUptimeHistory(string, int) ([]core.UptimeDay, error) {
	return nil, errors.New("no history")
}
