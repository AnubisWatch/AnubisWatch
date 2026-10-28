package raft

import (
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
	"time"
)

// Every voter response completes before requestVotes/requestPreVotes return.
// This forces the higher term to be observed before promotion, without sleeps.
type electionEpochTransport struct {
	mockTransport
	higherPhase string
}

func (tr *electionEpochTransport) SendPreVote(id string, req *core.PreVoteRequest) (*core.PreVoteResponse, error) {
	if tr.higherPhase == "pre-vote" && id == "higher" {
		return &core.PreVoteResponse{Term: req.Term + 1, VoteGranted: false}, nil
	}
	return &core.PreVoteResponse{Term: req.Term, VoteGranted: true}, nil
}
func (tr *electionEpochTransport) SendRequestVote(id string, req *core.RequestVoteRequest) (*core.RequestVoteResponse, error) {
	if tr.higherPhase == "vote" && id == "higher" {
		return &core.RequestVoteResponse{Term: req.Term + 1, VoteGranted: false}, nil
	}
	return &core.RequestVoteResponse{Term: req.Term, VoteGranted: true}, nil
}
func TestNodeElectionHigherTermFencesStaleMajority(t *testing.T) {
	for _, phase := range []string{"control", "pre-vote", "vote"} {
		t.Run(phase, func(t *testing.T) {
			cfg := newTestRaftNodeConfig()
			cfg.ElectionTimeout = core.Duration{Duration: time.Hour}
			cfg.Peers = []core.RaftPeer{{ID: "grant", Role: core.RoleVoter}, {ID: "higher", Role: core.RoleVoter}}
			n, err := NewNode(cfg, NewInMemoryLogStore(), NewInMemorySnapshotStore(), NewStorageFSM(NewInMemoryStorage()), newTestRaftLogger())
			if err != nil {
				t.Fatal(err)
			}
			n.transport = &electionEpochTransport{higherPhase: phase}
			n.startElection()
			n.mu.RLock()
			term, state := n.currentTerm, n.state.Load().(core.RaftState)
			n.mu.RUnlock()
			if phase == "control" {
				if term != 1 || state != core.StateLeader {
					t.Fatalf("same-term majority failed control: term%d state%v", term, state)
				}
				return
			}
			if term != 2 || state != core.StateFollower {
				t.Fatalf("higher term must fence %s majority: want term2 follower, got term%d %v", phase, term, state)
			}
		})
	}
}
