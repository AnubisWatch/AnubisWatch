package grpcapi

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// streamTokenKey carries a stream's bearer token in its context.
type streamTokenKey struct{}

// streamRevalidateInterval is how often an idle stream re-checks its token,
// matching the WebSocket/SSE streams.
var streamRevalidateInterval = 30 * time.Second

// streamGuard re-authenticates a server stream: before the first message of
// every batch, and at least every streamRevalidateInterval while idle. A
// revoked key or session, a disabled account, or a role that no longer holds
// the stream's permission in its workspace ends the stream.
type streamGuard struct {
	s          *Server
	ctx        context.Context
	permission string
	workspace  string
	last       time.Time
	checked    bool
}

func (s *Server) newStreamGuard(ctx context.Context, permission, workspace string) *streamGuard {
	return &streamGuard{s: s, ctx: ctx, permission: permission, workspace: workspace, last: time.Now()}
}

// tick starts a polling round.
func (g *streamGuard) tick() error {
	g.checked = false
	if time.Since(g.last) >= streamRevalidateInterval {
		return g.check()
	}
	return nil
}

// beforeSend checks once per round, before anything is delivered.
func (g *streamGuard) beforeSend() error {
	if g.checked {
		return nil
	}
	return g.check()
}

func (g *streamGuard) check() error {
	g.checked, g.last = true, time.Now()
	token, _ := g.ctx.Value(streamTokenKey{}).(string)
	if token == "" || g.s.auth == nil {
		// Not opened through the interceptor (in-process callers).
		return nil
	}
	user, err := g.s.auth.Authenticate(token)
	if err != nil {
		return status.Error(codes.Unauthenticated, "credentials revoked")
	}
	ws := user.Workspace
	if ws == "" {
		ws = "default"
	}
	if ws != g.workspace || !core.MemberRole(user.Role).Can(g.permission) {
		return status.Error(codes.PermissionDenied, "access to this stream was withdrawn")
	}
	return nil
}
