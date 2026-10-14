package backup

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Options.IncludeJudgments is documented as "Include recent judgment history" and
// cmd/anubis/backup.go passes the operator's --include-history flag straight into
// Create. The option used to be accepted and silently ignored: the archive
// contained no history and nothing said so. These tests pin that a requested
// backup really carries the history, within the day window the operator chose.

const (
	judgmentRegressionMarker = "JUDGMENT-REGRESSION-MARKER"
	judgmentRegressionSoul   = "judgment-regression-soul"
)

type judgmentRegressionStorage struct {
	workspaces []*core.Workspace
	souls      []*core.Soul
	judgments  []*core.Judgment
}

func (s *judgmentRegressionStorage) ListWorkspaces(context.Context) ([]*core.Workspace, error) {
	return s.workspaces, nil
}

func (s *judgmentRegressionStorage) ListSouls(_ context.Context, workspaceID string, _, _ int) ([]*core.Soul, error) {
	out := make([]*core.Soul, 0, len(s.souls))
	for _, soul := range s.souls {
		if soul.WorkspaceID == workspaceID {
			out = append(out, soul)
		}
	}
	return out, nil
}

func (s *judgmentRegressionStorage) ListAlertChannels(string) ([]*core.AlertChannel, error) {
	return nil, nil
}

func (s *judgmentRegressionStorage) ListAlertRules(string) ([]*core.AlertRule, error) {
	return nil, nil
}

func (s *judgmentRegressionStorage) ListStatusPages() ([]*core.StatusPage, error) {
	return nil, nil
}

func (s *judgmentRegressionStorage) ListJourneys(context.Context, string) ([]*core.JourneyConfig, error) {
	return nil, nil
}

func (s *judgmentRegressionStorage) ListJudgments(_ context.Context, soulID string, start, end time.Time, _ int) ([]*core.Judgment, error) {
	out := make([]*core.Judgment, 0, len(s.judgments))
	for _, j := range s.judgments {
		if j.SoulID != soulID || j.Timestamp.Before(start) || j.Timestamp.After(end) {
			continue
		}
		out = append(out, j)
	}
	return out, nil
}

func (s *judgmentRegressionStorage) GetSystemConfig(context.Context, string) ([]byte, error) {
	return nil, nil
}

// judgmentRegressionArchive builds a backup over a store holding one recent
// judgment and one far older than any window used here.
func judgmentRegressionArchive(t *testing.T, opts Options) string {
	t.Helper()

	now := time.Now()
	store := &judgmentRegressionStorage{
		workspaces: []*core.Workspace{{ID: "default", Name: "Default"}},
		souls:      []*core.Soul{{ID: judgmentRegressionSoul, WorkspaceID: "default", Name: "Soul", Type: core.CheckHTTP}},
		judgments: []*core.Judgment{
			{ID: "recent", SoulID: judgmentRegressionSoul, WorkspaceID: "default", Status: core.SoulDead,
				Message: judgmentRegressionMarker, Timestamp: now.Add(-1 * time.Hour)},
			{ID: "ancient", SoulID: judgmentRegressionSoul, WorkspaceID: "default", Status: core.SoulDead,
				Message: "ANCIENT-JUDGMENT-OUTSIDE-THE-WINDOW", Timestamp: now.AddDate(0, 0, -30)},
		},
	}

	m := NewManager(store, t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := m.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	_, path, err := m.Create(context.Background(), opts)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	return string(data)
}

func TestBackupIncludesRequestedJudgmentHistory(t *testing.T) {
	archive := judgmentRegressionArchive(t, Options{IncludeJudgments: true, JudgmentDays: 7, Compress: false})

	if !strings.Contains(archive, judgmentRegressionMarker) {
		t.Error("the archive does not contain the judgment history IncludeJudgments asked for")
	}
	if strings.Contains(archive, "ANCIENT-JUDGMENT-OUTSIDE-THE-WINDOW") {
		t.Error("the archive contains history older than JudgmentDays, so the window is not applied")
	}
}

// TestBackupOmitsJudgmentHistoryWhenNotRequested is the neighbouring control:
// the option still means something in both directions.
func TestBackupOmitsJudgmentHistoryWhenNotRequested(t *testing.T) {
	archive := judgmentRegressionArchive(t, Options{IncludeJudgments: false, Compress: false})

	if strings.Contains(archive, judgmentRegressionMarker) {
		t.Error("the archive contains judgment history although IncludeJudgments was false")
	}
}

// TestBackupJudgmentWindowDefaultsWhenUnset pins the boundary: an unset
// JudgmentDays must fall back to a real window rather than to "everything" or to
// nothing.
func TestBackupJudgmentWindowDefaultsWhenUnset(t *testing.T) {
	archive := judgmentRegressionArchive(t, Options{IncludeJudgments: true, Compress: false})

	if !strings.Contains(archive, judgmentRegressionMarker) {
		t.Error("with JudgmentDays unset the recent history was dropped instead of using the default window")
	}
	if strings.Contains(archive, "ANCIENT-JUDGMENT-OUTSIDE-THE-WINDOW") {
		t.Error("with JudgmentDays unset the default window was not applied")
	}
}
