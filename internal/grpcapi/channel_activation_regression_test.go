package grpcapi

// Regression coverage for gRPC channel writes reaching the live alert engine.
//
// CreateChannel/UpdateChannel/DeleteChannel wrote only to storage, which the
// alert manager reads once at startup, so a channel created over gRPC received
// nothing until a restart and a deleted one kept receiving alerts. Channels
// were not validated either: an unknown type or a private-network webhook was
// stored, and an email channel kept "to" and "smtp_port" as strings, which the
// email dispatcher cannot read, so every send failed with "no recipients".

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	v1 "github.com/AnubisWatch/anubiswatch/internal/grpcapi/v1"
)

func emailChannelRequest() *v1.CreateChannelRequest {
	return &v1.CreateChannelRequest{
		Name:    "on-call email",
		Type:    "email",
		Enabled: true,
		Config: map[string]string{
			"smtp_host": "smtp.example.com",
			"smtp_port": "2525",
			"from":      "anubis@example.com",
			"to":        "a@example.com, b@example.com",
		},
	}
}

func TestGRPCCreateChannelActivatesChannelInDispatchableShape(t *testing.T) {
	srv, _, mgr := newRuleActivationServer(t)

	resp, err := srv.CreateChannel(testUserContext(), emailChannelRequest())
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	live, err := mgr.GetChannel(resp.Id)
	if err != nil {
		t.Fatalf("channel created over gRPC is not active in the alert manager: %v", err)
	}
	if !live.Enabled || live.WorkspaceID != "default" {
		t.Fatalf("live channel = enabled:%v ws:%q", live.Enabled, live.WorkspaceID)
	}
	to, ok := live.Config["to"].([]any)
	if !ok || len(to) != 2 || to[0] != "a@example.com" || to[1] != "b@example.com" {
		t.Fatalf("to = %#v, want []any{a@example.com b@example.com}", live.Config["to"])
	}
	if port, ok := live.Config["smtp_port"].(float64); !ok || port != 2525 {
		t.Fatalf("smtp_port = %#v, want float64(2525)", live.Config["smtp_port"])
	}
	if got := resp.Config["to"]; got != "a@example.com,b@example.com" {
		t.Fatalf("response to = %q, want comma-separated recipients", got)
	}
}

func TestGRPCCreateChannelRejectsInvalidChannel(t *testing.T) {
	cases := map[string]*v1.CreateChannelRequest{
		"no name":         {Type: "slack", Config: map[string]string{"webhook_url": "https://hooks.slack.com/services/T/B/X"}},
		"unknown type":    {Name: "c", Type: "carrier-pigeon"},
		"email no host":   {Name: "c", Type: "email", Config: map[string]string{"to": "a@example.com"}},
		"bad smtp port":   {Name: "c", Type: "email", Config: map[string]string{"smtp_host": "smtp.example.com", "to": "a@example.com", "smtp_port": "smtp"}},
		"private webhook": {Name: "c", Type: "webhook", Config: map[string]string{"url": "http://169.254.169.254/latest/meta-data"}},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			srv, store, mgr := newRuleActivationServer(t)
			_, err := srv.CreateChannel(testUserContext(), req)
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("CreateChannel error = %v, want InvalidArgument", err)
			}
			if len(store.channels) != 0 || len(mgr.ListChannels()) != 0 {
				t.Fatalf("invalid channel was stored (%d) or activated (%d)", len(store.channels), len(mgr.ListChannels()))
			}
		})
	}
}

func TestGRPCUpdateChannelActivatesChangeAndKeepsRecipientList(t *testing.T) {
	srv, store, mgr := newRuleActivationServer(t)
	resp, err := srv.CreateChannel(testUserContext(), emailChannelRequest())
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	// Read-modify-write: send back the recipients exactly as they were read.
	disabled := false
	if _, err := srv.UpdateChannel(testUserContext(), &v1.UpdateChannelRequest{
		Id: resp.Id, Enabled: &disabled, Config: map[string]string{"to": resp.Config["to"]},
	}); err != nil {
		t.Fatalf("UpdateChannel: %v", err)
	}

	live, err := mgr.GetChannel(resp.Id)
	if err != nil {
		t.Fatalf("GetChannel: %v", err)
	}
	if live.Enabled {
		t.Fatal("live channel still enabled after gRPC update disabled it")
	}
	if to, ok := live.Config["to"].([]any); !ok || len(to) != 2 {
		t.Fatalf("recipients after round trip = %#v, want the same two addresses", live.Config["to"])
	}

	// An update that breaks the channel is refused and changes nothing.
	if _, err := srv.UpdateChannel(testUserContext(), &v1.UpdateChannelRequest{
		Id: resp.Id, Config: map[string]string{"smtp_port": "0"},
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("invalid update error = %v, want InvalidArgument", err)
	}
	if port := store.channels[resp.Id].Config["smtp_port"]; port != float64(2525) {
		t.Fatalf("stored smtp_port after refused update = %#v, want 2525", port)
	}
}

func TestGRPCDeleteChannelDeactivatesChannel(t *testing.T) {
	srv, store, mgr := newRuleActivationServer(t)
	// Seed storage and the live engine directly, as a startup load would.
	ch := &core.AlertChannel{
		ID: "live", Name: "live", Type: core.ChannelSlack, Enabled: true, WorkspaceID: "default",
		Config: map[string]any{"webhook_url": "https://hooks.slack.com/services/T/B/X"},
	}
	store.channels[ch.ID] = ch
	if err := mgr.RegisterChannel(ch); err != nil {
		t.Fatalf("RegisterChannel: %v", err)
	}

	if _, err := srv.DeleteChannel(testUserContext(), &v1.DeleteChannelRequest{Id: ch.ID}); err != nil {
		t.Fatalf("DeleteChannel: %v", err)
	}
	if _, err := mgr.GetChannel(ch.ID); err == nil {
		t.Fatal("channel deleted over gRPC is still active in the alert manager")
	}
}
