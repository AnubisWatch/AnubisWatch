package grpcapi

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/auth"
	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type grpcKeyStore struct {
	mu   sync.Mutex
	keys map[string]core.APIKey
}

func (s *grpcKeyStore) SaveAPIKey(k *core.APIKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[k.ID] = *k
	return nil
}
func (s *grpcKeyStore) GetAPIKey(id string) (*core.APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.keys[id]
	if !ok {
		return nil, &core.NotFoundError{Entity: "api_key", ID: id}
	}
	return &k, nil
}
func (s *grpcKeyStore) ListAPIKeys(string) ([]*core.APIKey, error)    { return nil, nil }
func (s *grpcKeyStore) RecordAPIKeyUse(*core.APIKey, time.Time) error { return nil }

// sessionAuthStub completes api.Authenticator around the gRPC test mock.
type sessionAuthStub struct{ mockAuthenticator }

func (sessionAuthStub) Login(string, string) (*api.User, string, error) { return nil, "", nil }
func (sessionAuthStub) Logout(string) error                             { return nil }
func (sessionAuthStub) Shutdown()                                       {}
func (sessionAuthStub) ChangePassword(string, string, string) error     { return nil }
func (sessionAuthStub) RequestPasswordReset(string) (string, error)     { return "", nil }
func (sessionAuthStub) ConfirmPasswordReset(string, string) error       { return nil }

// TestGRPCAuthenticatesAPIKeys drives the unary interceptor with an API key in
// the authorization metadata, exactly as session tokens are sent today.
func TestGRPCAuthenticatesAPIKeys(t *testing.T) {
	keys := auth.NewAPIKeyService(&grpcKeyStore{keys: map[string]core.APIKey{}}, nil)
	creator := &api.User{ID: "user-1", Email: "admin@example.com", Role: "admin", Workspace: "default"}
	key, plaintext, err := keys.CreateKey(creator, "grpc-ci", core.RoleViewer, nil)
	if err != nil {
		t.Fatal(err)
	}
	authn := &api.KeyAwareAuthenticator{Authenticator: &sessionAuthStub{}, Keys: keys}
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, authn, nil, nil, false)

	call := func(token, permission string) error {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
			"authorization": "Bearer " + token,
		}))
		_, err := srv.authInterceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test"},
			func(ctx context.Context, _ interface{}) (interface{}, error) {
				user, _ := GetUserFromContext(ctx)
				if token == plaintext && (user == nil || user.APIKeyID != key.ID || user.Workspace != "default") {
					t.Errorf("principal = %+v, want key %s in default", user, key.ID)
				}
				return srv.checkPermission(ctx, permission)
			})
		return err
	}

	tests := []struct {
		name       string
		permission string
		want       codes.Code
	}{
		{"viewer key reads", "souls:read", codes.OK},
		{"viewer key cannot write", "souls:*", codes.PermissionDenied},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := status.Code(call(plaintext, tt.permission)); got != tt.want {
				t.Fatalf("code = %v, want %v", got, tt.want)
			}
		})
	}

	// Session tokens still work beside keys.
	if err := call("valid-token", "souls:read"); status.Code(err) == codes.Unauthenticated {
		t.Fatalf("session token rejected: %v", err)
	}

	if _, err := keys.RevokeKey("default", key.ID, "user-1"); err != nil {
		t.Fatal(err)
	}
	if got := status.Code(call(plaintext, "souls:read")); got != codes.Unauthenticated {
		t.Fatalf("revoked key: code = %v, want Unauthenticated", got)
	}
}
