package grpcapi

import (
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	v1 "github.com/AnubisWatch/anubiswatch/internal/grpcapi/v1"
)

type failingRegistrar struct{ calls int }

func (f *failingRegistrar) RegisterRule(*core.AlertRule) error {
	f.calls++
	return errors.New("no leader")
}
func (f *failingRegistrar) DeleteRule(string) error { return nil }
func (f *failingRegistrar) ValidateChannel(*core.AlertChannel) error {
	return nil
}
func (f *failingRegistrar) RegisterChannel(*core.AlertChannel) error {
	f.calls++
	return errors.New("no leader")
}
func (f *failingRegistrar) DeleteChannel(string) error { return nil }

// gRPC channel/rule writes used to save to storage and then register with the
// alert engine (a second write), logging a failed registration and still
// reporting success.
func TestGRPCChannelAndRuleWritesFailWhenActivationFails(t *testing.T) {
	store := newMockGRPCStore()
	reg := &failingRegistrar{}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, false).SetAlertRegistrar(reg)

	_, err := srv.CreateChannel(testUserContext(), &v1.CreateChannelRequest{
		Name: "hook", Type: "webhook", Enabled: true, Config: map[string]string{"url": "https://hooks.example.com/x"},
	})
	if status.Code(err) == codes.OK {
		t.Fatal("CreateChannel succeeded although activation failed")
	}
	if len(store.channels) != 0 {
		t.Fatal("channel written to storage outside the alert engine")
	}

	_, err = srv.CreateRule(testUserContext(), &v1.CreateRuleRequest{
		Name: "r", ConditionType: "consecutive_failures", ChannelId: "ch_1", Enabled: true,
		Config: map[string]string{"threshold": "3"},
	})
	if status.Code(err) == codes.OK {
		t.Fatal("CreateRule succeeded although activation failed")
	}
	if reg.calls != 2 {
		t.Fatalf("registrar calls = %d, want 2", reg.calls)
	}
}
