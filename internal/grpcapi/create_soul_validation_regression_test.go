package grpcapi

import (
	"testing"

	v1 "github.com/AnubisWatch/anubiswatch/internal/grpcapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CreateSoul used to store any payload unvalidated and schedule disabled souls.
func TestCreateSoulValidatesAndRespectsEnabled(t *testing.T) {
	store := newMockGRPCStore()
	probe := &mockGRPCProbe{}
	srv := NewServer(":0", store, probe, &mockAuthenticator{}, nil, nil, true)

	_, err := srv.CreateSoul(testUserContext(), &v1.CreateSoulRequest{Name: "bad", Type: "tcp", Target: "no-port"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("invalid soul: err = %v, want InvalidArgument", err)
	}
	if len(store.souls) != 0 {
		t.Fatalf("invalid soul was stored")
	}

	if _, err := srv.CreateSoul(testUserContext(), &v1.CreateSoulRequest{
		Name: "off", Type: "http", Target: "https://example.com", Enabled: false,
	}); err != nil {
		t.Fatal(err)
	}
	if len(probe.upserted) != 0 {
		t.Fatalf("disabled soul was scheduled")
	}
	if len(probe.removed) != 1 {
		t.Fatalf("disabled soul was not unscheduled: removed=%v", probe.removed)
	}
}
