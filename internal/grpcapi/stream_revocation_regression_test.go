package grpcapi

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/auth"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	v1 "github.com/AnubisWatch/anubiswatch/internal/grpcapi/v1"
)

type revokedKeyJudgmentStream struct {
	baseServerStream
	ctx  context.Context
	sent atomic.Int32
}

func (m *revokedKeyJudgmentStream) Context() context.Context  { return m.ctx }
func (m *revokedKeyJudgmentStream) Send(j *v1.Judgment) error { m.sent.Add(1); return nil }

// Regression: a gRPC judgment stream opened with an API key kept delivering after the
// key is revoked: the stream is authenticated once and never re-checked.
func TestGRPCStreamEndsWhenItsKeyIsRevoked(t *testing.T) {
	keys := auth.NewAPIKeyService(&grpcKeyStore{keys: map[string]core.APIKey{}}, nil)
	creator := &api.User{ID: "user-1", Email: "admin@example.com", Role: "admin", Workspace: "default"}
	key, plaintext, err := keys.CreateKey(creator, "ci", core.RoleViewer, nil)
	if err != nil {
		t.Fatal(err)
	}
	authn := &api.KeyAwareAuthenticator{Authenticator: &sessionAuthStub{}, Keys: keys}
	store := newMockGRPCStore()
	store.souls["s1"] = &core.Soul{ID: "s1", WorkspaceID: "default", Name: "s1"}
	store.judgments = []*core.Judgment{{ID: "j1", SoulID: "s1", WorkspaceID: "default", Status: core.SoulAlive, Timestamp: time.Now()}}
	srv := NewServer(":0", store, &mockGRPCProbe{}, authn, nil, nil, false)

	ctx, cancel := context.WithTimeout(metadata.NewIncomingContext(context.Background(),
		metadata.New(map[string]string{"authorization": "Bearer " + plaintext})), 1500*time.Millisecond)
	defer cancel()
	stream := &revokedKeyJudgmentStream{ctx: ctx}
	soulID := "s1"
	out := &revokedKeyJudgmentStream{}
	_ = srv.authStreamInterceptor(nil, stream, &grpc.StreamServerInfo{FullMethod: "/StreamJudgments"},
		func(_ interface{}, ss grpc.ServerStream) error {
			// Revoke right after the stream is authenticated, before the first tick.
			if _, err := keys.RevokeKey("default", key.ID, "user-1"); err != nil {
				t.Fatal(err)
			}
			if _, err := keys.AuthenticateKey(plaintext); err == nil {
				t.Fatal("key still authenticates")
			}
			out.ctx = ss.Context()
			return srv.StreamJudgments(&v1.StreamRequest{SoulId: &soulID}, out)
		})
	if n := out.sent.Load(); n > 0 {
		t.Fatalf("revoked API key's gRPC stream delivered %d judgment(s) after revocation", n)
	}
}
