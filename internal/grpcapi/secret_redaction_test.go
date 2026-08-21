package grpcapi

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	v1 "github.com/AnubisWatch/anubiswatch/internal/grpcapi/v1"
)

func TestChannelToPB_RedactsNestedSecrets(t *testing.T) {
	pb := channelToPB(&core.AlertChannel{
		ID:          "channel-1",
		WorkspaceID: "tenant-a",
		Name:        "Slack",
		Type:        core.ChannelSlack,
		Enabled:     true,
		Config: map[string]interface{}{
			"url":      "keep-url",
			"password": "keep-pass",
			"nested":   map[string]interface{}{"secret": "nested-secret"},
		},
		CreatedAt: time.Now(),
	})
	if pb == nil {
		t.Fatal("channelToPB returned nil")
	}
	if pb.Config["url"] != redactedSecretValue {
		t.Fatalf("expected url to be redacted, got %q", pb.Config["url"])
	}
	if pb.Config["password"] != redactedSecretValue {
		t.Fatalf("expected password to be redacted, got %q", pb.Config["password"])
	}
	if strings.Contains(pb.Config["nested"], "nested-secret") {
		t.Fatalf("nested secret leaked: %s", pb.Config["nested"])
	}
}

func TestSoulToPB_PopulatesRedactedHTTPCheck(t *testing.T) {
	pb := soulToPB(&core.Soul{
		ID:          "soul-1",
		WorkspaceID: "tenant-a",
		Name:        "API",
		Type:        core.CheckHTTP,
		Target:      "https://example.com",
		Weight:      core.Duration{Duration: 30 * time.Second},
		Timeout:     core.Duration{Duration: 5 * time.Second},
		Enabled:     true,
		HTTP: &core.HTTPConfig{
			Method:      "POST",
			ValidStatus: []int{200},
			Headers:     map[string]string{"X-Test": "keep-me"},
			Body:        "keep-body",
		},
	})
	if pb == nil {
		t.Fatal("soulToPB returned nil")
	}
	httpCfg := pb.GetHttp()
	if httpCfg == nil {
		t.Fatal("expected HTTP check config to be populated")
	}
	if httpCfg.Headers["X-Test"] != redactedSecretValue {
		t.Fatalf("expected redacted headers, got %#v", httpCfg.Headers)
	}
	if httpCfg.Body != redactedSecretValue {
		t.Fatalf("expected redacted body, got %q", httpCfg.Body)
	}
}

func TestListChannels_RedactsSecretsForViewer(t *testing.T) {
	store := newMockGRPCStore()
	store.channels["channel-1"] = &core.AlertChannel{
		ID:          "channel-1",
		WorkspaceID: "default",
		Name:        "Ops",
		Type:        core.ChannelSlack,
		Enabled:     true,
		Config: map[string]interface{}{
			"url":    "keep-url",
			"nested": map[string]interface{}{"password": "keep-pass"},
		},
		CreatedAt: time.Now(),
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, false)
	viewer := &api.User{ID: "viewer", Role: "viewer", Workspace: "default"}
	ctx := context.WithValue(context.Background(), userContextKey, viewer)

	resp, err := srv.ListChannels(ctx, &v1.ListChannelsRequest{})
	if err != nil {
		t.Fatalf("ListChannels failed: %v", err)
	}
	if len(resp.Channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(resp.Channels))
	}
	if resp.Channels[0].Config["url"] != redactedSecretValue {
		t.Fatalf("expected redacted url, got %#v", resp.Channels[0].Config)
	}
	if strings.Contains(resp.Channels[0].Config["nested"], "keep-pass") {
		t.Fatalf("nested secret leaked: %#v", resp.Channels[0].Config)
	}
}

func TestGetSoul_RedactsSecretsForViewer(t *testing.T) {
	store := newMockGRPCStore()
	store.souls["soul-1"] = &core.Soul{
		ID:          "soul-1",
		WorkspaceID: "default",
		Name:        "API",
		Type:        core.CheckHTTP,
		Target:      "https://example.com",
		Enabled:     true,
		HTTP: &core.HTTPConfig{
			Method:      "POST",
			ValidStatus: []int{200},
			Headers:     map[string]string{"X-Test": "keep-me"},
			Body:        "keep-body",
		},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, false)
	viewer := &api.User{ID: "viewer", Role: "viewer", Workspace: "default"}
	ctx := context.WithValue(context.Background(), userContextKey, viewer)

	resp, err := srv.GetSoul(ctx, &v1.GetSoulRequest{Id: "soul-1"})
	if err != nil {
		t.Fatalf("GetSoul failed: %v", err)
	}
	if resp.GetHttp() == nil {
		t.Fatal("expected HTTP config on protobuf soul")
	}
	if resp.GetHttp().Headers["X-Test"] != redactedSecretValue {
		t.Fatalf("expected redacted header, got %#v", resp.GetHttp().Headers)
	}
	if resp.GetHttp().Body != redactedSecretValue {
		t.Fatalf("expected redacted body, got %q", resp.GetHttp().Body)
	}
}

func TestUpdateChannel_PreservesPlaceholdersForSameDestination(t *testing.T) {
	store := newMockGRPCStore()
	store.channels["channel-1"] = &core.AlertChannel{
		ID:          "channel-1",
		WorkspaceID: "default",
		Name:        "Webhook",
		Type:        core.ChannelWebHook,
		Config: map[string]interface{}{
			"webhook_url": "https://hooks.example.com/old",
			"api_key":     "keep-api-key",
			"secret":      "keep-secret",
		},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, false)

	_, err := srv.UpdateChannel(testUserContext(), &v1.UpdateChannelRequest{
		Id: "channel-1",
		Config: map[string]string{
			"webhook_url": redactedSecretValue,
			"api_key":     redactedSecretValue,
			"secret":      "replacement-secret",
		},
	})
	if err != nil {
		t.Fatalf("UpdateChannel failed: %v", err)
	}
	stored := store.channels["channel-1"].Config
	if stored["webhook_url"] != "https://hooks.example.com/old" {
		t.Fatalf("destination placeholder was persisted: %#v", stored)
	}
	if stored["api_key"] != "keep-api-key" {
		t.Fatalf("secret placeholder was persisted: %#v", stored)
	}
	if stored["secret"] != "replacement-secret" {
		t.Fatalf("explicit secret did not overwrite existing value: %#v", stored)
	}
}

func TestUpdateChannel_PreservesNestedRedactedConfig(t *testing.T) {
	store := newMockGRPCStore()
	nested := map[string]interface{}{"Authorization": "Bearer secret"}
	store.channels["channel-1"] = &core.AlertChannel{
		ID:          "channel-1",
		WorkspaceID: "default",
		Type:        core.ChannelWebHook,
		Config: map[string]interface{}{
			"webhook_url": "https://hooks.example.com/old",
			"headers":     nested,
		},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, false)

	_, err := srv.UpdateChannel(testUserContext(), &v1.UpdateChannelRequest{
		Id:     "channel-1",
		Config: map[string]string{"headers": `{"Authorization":"[REDACTED]"}`},
	})
	if err != nil {
		t.Fatalf("UpdateChannel failed: %v", err)
	}
	stored, ok := store.channels["channel-1"].Config["headers"].(map[string]interface{})
	if !ok || stored["Authorization"] != "Bearer secret" {
		t.Fatalf("nested placeholder did not preserve typed config: %#v", store.channels["channel-1"].Config)
	}
}

func TestUpdateChannel_RejectsDestinationChangeWithPreservedSecrets(t *testing.T) {
	store := newMockGRPCStore()
	store.channels["channel-1"] = &core.AlertChannel{
		ID:          "channel-1",
		WorkspaceID: "default",
		Type:        core.ChannelWebHook,
		Config: map[string]interface{}{
			"webhook_url": "https://hooks.example.com/old",
			"api_key":     "keep-api-key",
		},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, false)

	_, err := srv.UpdateChannel(testUserContext(), &v1.UpdateChannelRequest{
		Id: "channel-1",
		Config: map[string]string{
			"webhook_url": "https://hooks.example.com/new",
			"api_key":     redactedSecretValue,
		},
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected FailedPrecondition, got %v", err)
	}
	stored := store.channels["channel-1"].Config
	if stored["webhook_url"] != "https://hooks.example.com/old" || stored["api_key"] != "keep-api-key" {
		t.Fatalf("rejected update mutated stored config: %#v", stored)
	}
}

func TestUpdateChannel_AllowsDestinationChangeWithReenteredSecrets(t *testing.T) {
	store := newMockGRPCStore()
	store.channels["channel-1"] = &core.AlertChannel{
		ID:          "channel-1",
		WorkspaceID: "default",
		Type:        core.ChannelWebHook,
		Config: map[string]interface{}{
			"url":     "https://hooks.example.com/old",
			"api_key": "old-api-key",
		},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, false)

	_, err := srv.UpdateChannel(testUserContext(), &v1.UpdateChannelRequest{
		Id: "channel-1",
		Config: map[string]string{
			"url":     "https://hooks.example.com/new",
			"api_key": "new-api-key",
		},
	})
	if err != nil {
		t.Fatalf("UpdateChannel failed: %v", err)
	}
	stored := store.channels["channel-1"].Config
	if stored["url"] != "https://hooks.example.com/new" || stored["api_key"] != "new-api-key" {
		t.Fatalf("explicit destination and secret were not stored: %#v", stored)
	}
}

func TestUpdateChannel_RejectsPlaceholderWithoutExistingValue(t *testing.T) {
	store := newMockGRPCStore()
	store.channels["channel-1"] = &core.AlertChannel{
		ID:          "channel-1",
		WorkspaceID: "default",
		Type:        core.ChannelWebHook,
		Config:      map[string]interface{}{},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, false)

	_, err := srv.UpdateChannel(testUserContext(), &v1.UpdateChannelRequest{
		Id:     "channel-1",
		Config: map[string]string{"api_key": redactedSecretValue},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
	if len(store.channels["channel-1"].Config) != 0 {
		t.Fatalf("rejected placeholder mutated config: %#v", store.channels["channel-1"].Config)
	}
}

func TestSoulToPB_PopulatesRedactedGRPCCheck(t *testing.T) {
	pb := soulToPB(&core.Soul{
		ID:          "soul-2",
		WorkspaceID: "tenant-a",
		Name:        "gRPC",
		Type:        core.CheckGRPC,
		Target:      "grpc.example.com:443",
		Weight:      core.Duration{Duration: 30 * time.Second},
		Timeout:     core.Duration{Duration: 5 * time.Second},
		Enabled:     true,
		GRPC: &core.GRPCConfig{
			Service:  "grpc.health.v1.Health",
			TLS:      true,
			TLSCA:    "ca-data",
			Metadata: map[string]string{"trace": "keep-meta"},
		},
	})
	if pb == nil {
		t.Fatal("soulToPB returned nil")
	}
	grpcCfg := pb.GetGrpc()
	if grpcCfg == nil {
		t.Fatal("expected gRPC check config to be populated")
	}
	if grpcCfg.Metadata["trace"] != redactedSecretValue {
		t.Fatalf("expected redacted metadata, got %#v", grpcCfg.Metadata)
	}
	if grpcCfg.Service != "grpc.health.v1.Health" || !grpcCfg.Tls || grpcCfg.TlsCa != "ca-data" {
		t.Fatalf("unexpected non-secret gRPC fields: %#v", grpcCfg)
	}
}
