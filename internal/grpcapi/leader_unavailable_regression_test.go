package grpcapi

import (
	"errors"
	"fmt"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	v1 "github.com/AnubisWatch/anubiswatch/internal/grpcapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// writeFailingStore fails every write with err, as a cluster store does when
// no Raft leader can take the write.
type writeFailingStore struct {
	*mockGRPCStore
	err error
}

func (s *writeFailingStore) SaveSoulNoCtx(*core.Soul) error             { return s.err }
func (s *writeFailingStore) DeleteSoulNoCtx(string) error               { return s.err }
func (s *writeFailingStore) SaveChannelNoCtx(*core.AlertChannel) error  { return s.err }
func (s *writeFailingStore) DeleteChannelNoCtx(string, string) error    { return s.err }
func (s *writeFailingStore) SaveRuleNoCtx(*core.AlertRule) error        { return s.err }
func (s *writeFailingStore) DeleteRuleNoCtx(string, string) error       { return s.err }
func (s *writeFailingStore) SaveJourneyNoCtx(*core.JourneyConfig) error { return s.err }
func (s *writeFailingStore) DeleteJourneyNoCtx(string) error            { return s.err }

// Mutating RPCs used to answer a write refused for want of a leader with
// codes.Internal, so clients treated a transient election as a server bug and
// did not retry. They must answer codes.Unavailable for exactly the errors the
// REST API answers with 503, and keep codes.Internal for real failures.
func TestMutatingRPCsMapLeaderLossToUnavailable(t *testing.T) {
	name := "renamed"
	rpcs := []struct {
		name string
		call func(*Server) error
	}{
		{"CreateSoul", func(s *Server) error {
			_, err := s.CreateSoul(testUserContext(), &v1.CreateSoulRequest{Name: "s", Type: "http", Target: "https://example.com", Enabled: true})
			return err
		}},
		{"UpdateSoul", func(s *Server) error {
			_, err := s.UpdateSoul(testUserContext(), &v1.UpdateSoulRequest{Id: "soul-1", Name: &name})
			return err
		}},
		{"DeleteSoul", func(s *Server) error {
			_, err := s.DeleteSoul(testUserContext(), &v1.DeleteSoulRequest{Id: "soul-1"})
			return err
		}},
		{"CreateChannel", func(s *Server) error {
			_, err := s.CreateChannel(testUserContext(), &v1.CreateChannelRequest{Name: "c", Type: "slack", Enabled: true, Config: map[string]string{"webhook_url": "https://hooks.slack.com/x"}})
			return err
		}},
		{"UpdateChannel", func(s *Server) error {
			_, err := s.UpdateChannel(testUserContext(), &v1.UpdateChannelRequest{Id: "ch-1", Name: &name})
			return err
		}},
		{"DeleteChannel", func(s *Server) error {
			_, err := s.DeleteChannel(testUserContext(), &v1.DeleteChannelRequest{Id: "ch-1"})
			return err
		}},
		{"CreateRule", func(s *Server) error {
			_, err := s.CreateRule(testUserContext(), &v1.CreateRuleRequest{Name: "r", ConditionType: "consecutive_failures", ChannelId: "ch-1", Workspace: "default"})
			return err
		}},
		{"UpdateRule", func(s *Server) error {
			_, err := s.UpdateRule(testUserContext(), &v1.UpdateRuleRequest{Id: "rule-1", Name: &name})
			return err
		}},
		{"DeleteRule", func(s *Server) error {
			_, err := s.DeleteRule(testUserContext(), &v1.DeleteRuleRequest{Id: "rule-1"})
			return err
		}},
		{"CreateJourney", func(s *Server) error {
			_, err := s.CreateJourney(testUserContext(), &v1.CreateJourneyRequest{Name: "j", Interval: 60, Enabled: true, Workspace: "default",
				Steps: []*v1.JourneyStep{{Name: "step", Type: "http", Target: "https://example.com", Timeout: 10}}})
			return err
		}},
		{"UpdateJourney", func(s *Server) error {
			_, err := s.UpdateJourney(testUserContext(), &v1.UpdateJourneyRequest{Id: "j-1", Name: &name})
			return err
		}},
		{"DeleteJourney", func(s *Server) error {
			_, err := s.DeleteJourney(testUserContext(), &v1.DeleteJourneyRequest{Id: "j-1"})
			return err
		}},
	}
	failures := []struct {
		name string
		err  error
		want codes.Code
	}{
		{"no leader", &core.RaftError{Code: core.ErrNoLeader, Message: "no leader"}, codes.Unavailable},
		{"not leader", &core.RaftError{Code: core.ErrNotLeader, Message: "forward failed", NodeID: "jackal-1"}, codes.Unavailable},
		{"leadership lost (wrapped)", fmt.Errorf("replicate: %w", &core.RaftError{Code: core.ErrLeadershipLost, Message: "lost"}), codes.Unavailable},
		{"other raft error", &core.RaftError{Code: core.ErrTimeout, Message: "timeout"}, codes.Internal},
		{"storage error", errors.New("disk full"), codes.Internal},
	}

	for _, rpc := range rpcs {
		for _, failure := range failures {
			t.Run(rpc.name+"/"+failure.name, func(t *testing.T) {
				store := &writeFailingStore{mockGRPCStore: newMockGRPCStore(), err: failure.err}
				store.souls["soul-1"] = &core.Soul{ID: "soul-1", Name: "s", Type: core.CheckHTTP, Target: "https://example.com", WorkspaceID: "default"}
				store.channels["ch-1"] = &core.AlertChannel{ID: "ch-1", Name: "c", Type: "slack", WorkspaceID: "default", Config: map[string]interface{}{"webhook_url": "https://hooks.slack.com/x"}}
				store.rules["rule-1"] = &core.AlertRule{ID: "rule-1", Name: "r", WorkspaceID: "default", Conditions: []core.AlertCondition{{Type: "consecutive_failures", Threshold: 1}}, Channels: []string{"ch-1"}}
				store.journeys["j-1"] = &core.JourneyConfig{ID: "j-1", Name: "j", WorkspaceID: "default"}
				srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

				err := rpc.call(srv)
				if got := status.Code(err); got != failure.want {
					t.Fatalf("%s with %v: code = %v (%v), want %v", rpc.name, failure.err, got, err, failure.want)
				}
			})
		}
	}
}
