package grpcapi

import (
	"context"
	"fmt"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	v1 "github.com/AnubisWatch/anubiswatch/internal/grpcapi/v1"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
	"google.golang.org/protobuf/types/known/timestamppb"
	"log/slog"
	"testing"
	"time"
)

type judgmentUntilStore struct {
	*mockGRPCStore
	db *storage.CobaltDB
}

func (s *judgmentUntilStore) ListJudgmentsNoCtx(id string, start, end time.Time, limit int) ([]*core.Judgment, error) {
	return s.db.ListJudgmentsNoCtx(id, start, end, limit)
}
func TestJudgmentUntilRegression(t *testing.T) {
	db, err := storage.NewEngine(core.StorageConfig{Path: t.TempDir()}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := &judgmentUntilStore{mockGRPCStore: newMockGRPCStore(), db: db}
	id := "s1"
	store.souls[id] = &core.Soul{ID: id, WorkspaceID: "default"}
	base := time.Now().Add(-time.Hour)
	ctx := core.ContextWithWorkspaceID(context.Background(), "default")
	for i, stamp := range []time.Time{base, base.Add(2 * time.Hour)} {
		if err := db.SaveJudgment(ctx, &core.Judgment{ID: fmt.Sprint(i), SoulID: id, WorkspaceID: "default", Timestamp: stamp, Status: core.SoulAlive}); err != nil {
			t.Fatal(err)
		}
	}
	s := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, false)
	for _, tc := range []struct {
		name string
		req  *v1.ListJudgmentsRequest
		want int
	}{{"omitted", &v1.ListJudgmentsRequest{SoulId: &id}, 1}, {"explicit", &v1.ListJudgmentsRequest{SoulId: &id, Until: timestamppb.New(base.Add(time.Minute))}, 1}, {"earlier", &v1.ListJudgmentsRequest{SoulId: &id, Until: timestamppb.New(base.Add(-time.Minute))}, 0}, {"since", &v1.ListJudgmentsRequest{SoulId: &id, Since: timestamppb.New(base.Add(time.Minute))}, 0}, {"future override", &v1.ListJudgmentsRequest{SoulId: &id, Until: timestamppb.New(base.Add(3 * time.Hour))}, 2}, {"all souls", &v1.ListJudgmentsRequest{}, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := s.ListJudgments(testUserContext(), tc.req)
			if err != nil {
				t.Fatal(err)
			}
			if len(out.Judgments) != tc.want {
				t.Fatalf("count=%d want=%d", len(out.Judgments), tc.want)
			}
		})
	}
	out, err := s.GetSoulJudgments(testUserContext(), &v1.GetSoulJudgmentsRequest{SoulId: id})
	if err != nil || len(out.Judgments) != 1 {
		t.Fatalf("delegated query %v", err)
	}
}
