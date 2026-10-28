package grpcapi

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	v1 "github.com/AnubisWatch/anubiswatch/internal/grpcapi/v1"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

// streamStore serves souls and judgments from a real CobaltDB, so the
// workspace resolution under test is the production one.
type streamStore struct {
	*mockGRPCStore
	db *storage.CobaltDB
}

func (s *streamStore) GetSoulNoCtx(id string) (*core.Soul, error) { return s.db.GetSoulNoCtx(id) }
func (s *streamStore) ListSoulsNoCtx(ws string, offset, limit int) ([]*core.Soul, error) {
	return s.db.ListSoulsNoCtx(ws, offset, limit)
}
func (s *streamStore) ListJudgmentsNoCtx(id string, start, end time.Time, limit int) ([]*core.Judgment, error) {
	return s.db.ListJudgmentsNoCtx(id, start, end, limit)
}

type collectingJudgmentStream struct {
	mockJudgmentsStream
	mu   sync.Mutex
	seen []string
}

func (c *collectingJudgmentStream) Send(j *v1.Judgment) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, j.Id)
	return nil
}

func (c *collectingJudgmentStream) ids() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.seen...)
}

// StreamJudgments without a soul_id must stream the caller's workspace, as
// ListJudgments does. It handed the empty soul ID to the store, which reads
// the "default" workspace whatever the caller's workspace is, so a stream for
// any other workspace never delivered a single judgment.
func TestStreamJudgmentsWithoutSoulIDFollowsCallerWorkspace(t *testing.T) {
	db, err := storage.NewEngine(core.StorageConfig{Path: t.TempDir()}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now().UTC()
	seed := func(workspace, soulID, judgmentID string) {
		t.Helper()
		if err := db.SaveSoul(context.Background(), &core.Soul{ID: soulID, Name: soulID, Type: core.CheckHTTP, Target: "http://example.com", WorkspaceID: workspace, Enabled: true}); err != nil {
			t.Fatal(err)
		}
		ctx := core.ContextWithWorkspaceID(context.Background(), workspace)
		if err := db.SaveJudgment(ctx, &core.Judgment{ID: judgmentID, SoulID: soulID, WorkspaceID: workspace, Status: core.SoulAlive, Timestamp: now.Add(-time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	seed("default", "soul-default", "j-default")
	seed("team-b", "soul-b", "j-team-b")

	srv := NewServer(":0", &streamStore{mockGRPCStore: newMockGRPCStore(), db: db}, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, false)

	run := func(workspace string, req *v1.StreamRequest) []string {
		t.Helper()
		user := &api.User{ID: "u", Email: "u@example.com", Role: "owner", Workspace: workspace}
		ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), userContextKey, user), 2500*time.Millisecond)
		defer cancel()
		stream := &collectingJudgmentStream{mockJudgmentsStream: mockJudgmentsStream{ctx: ctx}}
		if err := srv.StreamJudgments(req, stream); err != nil {
			t.Fatalf("StreamJudgments: %v", err)
		}
		return stream.ids()
	}
	contains := func(ids []string, want string) bool {
		for _, id := range ids {
			if id == want {
				return true
			}
		}
		return false
	}

	// Controls: the default workspace, and an explicit soul_id in team-b,
	// already stream correctly.
	if got := run("default", &v1.StreamRequest{}); !contains(got, "j-default") || contains(got, "j-team-b") {
		t.Fatalf("control default workspace: got %v, want only j-default", got)
	}
	soulB := "soul-b"
	if got := run("team-b", &v1.StreamRequest{SoulId: &soulB}); !contains(got, "j-team-b") {
		t.Fatalf("control explicit soul_id in team-b: got %v, want j-team-b", got)
	}

	got := run("team-b", &v1.StreamRequest{})
	if !contains(got, "j-team-b") {
		t.Errorf("team-b stream without soul_id delivered %v, want j-team-b", got)
	}
	if contains(got, "j-default") {
		t.Errorf("team-b stream leaked a default-workspace judgment: %v", got)
	}
}
