package grpcapi

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	v1 "github.com/AnubisWatch/anubiswatch/internal/grpcapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// quotaGRPCStore refuses new souls, channels and journeys past a limit the way
// the server's storage adapter does, and lets updates through.
type quotaGRPCStore struct {
	*mockGRPCStore
	limit int
}

func (s *quotaGRPCStore) SaveSoulNoCtx(soul *core.Soul) error {
	if _, ok := s.souls[soul.ID]; !ok {
		if err := core.CheckQuotaCount(soul.WorkspaceID, core.QuotaMaxSouls, s.limit, len(s.souls)); err != nil {
			return err
		}
	}
	return s.mockGRPCStore.SaveSoulNoCtx(soul)
}

func (s *quotaGRPCStore) SaveChannelNoCtx(ch *core.AlertChannel) error {
	if _, ok := s.channels[ch.ID]; !ok {
		if err := core.CheckQuotaCount(ch.WorkspaceID, core.QuotaMaxAlertChannels, s.limit, len(s.channels)); err != nil {
			return fmt.Errorf("save channel: %w", err)
		}
	}
	return s.mockGRPCStore.SaveChannelNoCtx(ch)
}

func (s *quotaGRPCStore) SaveJourneyNoCtx(j *core.JourneyConfig) error {
	if _, ok := s.journeys[j.ID]; !ok {
		if err := core.CheckQuotaCount(j.WorkspaceID, core.QuotaMaxJourneys, s.limit, len(s.journeys)); err != nil {
			return err
		}
	}
	return s.mockGRPCStore.SaveJourneyNoCtx(j)
}

func TestGRPCQuotaExceededIsResourceExhausted(t *testing.T) {
	name := "renamed"
	cases := []struct {
		name      string
		call      func(*Server) error
		wantCode  codes.Code
		wantQuota string
	}{
		{"CreateSoul", func(s *Server) error {
			_, err := s.CreateSoul(testUserContext(), &v1.CreateSoulRequest{Name: "s", Type: "http", Target: "https://example.com", Enabled: true})
			return err
		}, codes.ResourceExhausted, core.QuotaMaxSouls},
		{"CreateChannel", func(s *Server) error {
			_, err := s.CreateChannel(testUserContext(), &v1.CreateChannelRequest{Name: "c", Type: "slack", Enabled: true, Config: map[string]string{"webhook_url": "https://hooks.slack.com/x"}})
			return err
		}, codes.ResourceExhausted, core.QuotaMaxAlertChannels},
		{"CreateJourney", func(s *Server) error {
			_, err := s.CreateJourney(testUserContext(), &v1.CreateJourneyRequest{Name: "j", Interval: 60, Enabled: true, Workspace: "default",
				Steps: []*v1.JourneyStep{{Name: "step", Type: "http", Target: "https://example.com", Timeout: 10}}})
			return err
		}, codes.ResourceExhausted, core.QuotaMaxJourneys},
		// Existing over-quota resources stay editable.
		{"UpdateSoul", func(s *Server) error {
			_, err := s.UpdateSoul(testUserContext(), &v1.UpdateSoulRequest{Id: "soul-1", Name: &name})
			return err
		}, codes.OK, ""},
		{"UpdateJourney", func(s *Server) error {
			_, err := s.UpdateJourney(testUserContext(), &v1.UpdateJourneyRequest{Id: "j-1", Name: &name})
			return err
		}, codes.OK, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &quotaGRPCStore{mockGRPCStore: newMockGRPCStore(), limit: 1}
			store.souls["soul-1"] = &core.Soul{ID: "soul-1", Name: "s", Type: core.CheckHTTP, Target: "https://example.com", WorkspaceID: "default", Weight: core.Duration{Duration: time.Minute}}
			store.souls["soul-2"] = &core.Soul{ID: "soul-2", Name: "s2", Type: core.CheckHTTP, Target: "https://example.com", WorkspaceID: "default", Weight: core.Duration{Duration: time.Minute}}
			store.channels["ch-1"] = &core.AlertChannel{ID: "ch-1", Name: "c", Type: "slack", WorkspaceID: "default", Config: map[string]interface{}{"webhook_url": "https://hooks.slack.com/x"}}
			store.journeys["j-1"] = &core.JourneyConfig{ID: "j-1", Name: "j", WorkspaceID: "default"}
			srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

			err := tc.call(srv)
			if got := status.Code(err); got != tc.wantCode {
				t.Fatalf("code = %v (%v), want %v", got, err, tc.wantCode)
			}
			if tc.wantQuota != "" && !strings.Contains(status.Convert(err).Message(), tc.wantQuota) {
				t.Fatalf("message %q does not name %s", status.Convert(err).Message(), tc.wantQuota)
			}
		})
	}
}
