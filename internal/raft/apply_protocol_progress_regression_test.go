package raft

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type protocolGateFSM struct {
	FSM
	gate, entered chan struct{}
	once          sync.Once
	calls         atomic.Int64
}

func (f *protocolGateFSM) Apply(entry *core.RaftLogEntry) interface{} {
	f.calls.Add(1)
	f.once.Do(func() { close(f.entered) })
	<-f.gate
	return f.FSM.Apply(entry)
}

func protocolGateNode(t *testing.T) (*Node, *protocolGateFSM, func()) {
	t.Helper()
	n := createTestNode(t)
	n.currentTerm = 1
	n.electionTimeout, n.heartbeatTimeout, n.commitTimeout = time.Hour, time.Hour, time.Hour
	fsm := &protocolGateFSM{FSM: n.fsm, gate: make(chan struct{}), entered: make(chan struct{})}
	n.fsm = fsm
	n.SetTransport(&mockTransport{})
	if err := n.Start(); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release := func() { once.Do(func() { close(fsm.gate) }) }
	appendProtocolEntry(t, n, "committed")
	synctest.Wait()
	return n, fsm, release
}

func appendProtocolEntry(t *testing.T, n *Node, key string) {
	t.Helper()
	data, err := json.Marshal(core.FSMCommand{Op: core.FSMSet, Key: key, Value: []byte("value")})
	if err != nil {
		t.Fatal(err)
	}
	n.mu.Lock()
	err = n.appendEntry(core.RaftLogEntry{Term: 1, Type: core.LogCommand, Data: data})
	n.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
}

func gateCommittedApply(t *testing.T, n *Node, fsm *protocolGateFSM) {
	t.Helper()
	n.mu.Lock()
	n.commitIndex = 1
	n.mu.Unlock()
	n.notifyCommit(1)
	synctest.Wait()
	select {
	case <-fsm.entered:
	default:
		t.Fatal("committed application did not reach gate")
	}
}

func TestCommittedApplyDoesNotBlockProtocol(t *testing.T) {
	for _, method := range []string{"heartbeat", "vote"} {
		t.Run(method, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				n, fsm, release := protocolGateNode(t)
				defer func() { release(); _ = n.Stop(); synctest.Wait() }()
				control := make(chan interface{}, 1)
				n.rpcCh <- &rpcWrapper{cmd: &core.AppendEntriesRequest{Term: 1, LeaderID: "leader", PrevLogIndex: 1, PrevLogTerm: 1}, respCh: control}
				synctest.Wait()
				select {
				case resp := <-control:
					if !resp.(*core.AppendEntriesResponse).Success {
						t.Fatal("initial heartbeat control rejected")
					}
				default:
					t.Fatal("initial heartbeat control not serviced")
				}
				gateCommittedApply(t, n, fsm)
				response := make(chan interface{}, 1)
				var command interface{} = &core.AppendEntriesRequest{Term: 1, LeaderID: "leader", PrevLogIndex: 1, PrevLogTerm: 1, LeaderCommit: 1}
				if method == "vote" {
					command = &core.RequestVoteRequest{Term: 2, CandidateID: "candidate", LastLogIndex: 1, LastLogTerm: 1}
				}
				n.rpcCh <- &rpcWrapper{cmd: command, respCh: response}
				synctest.Wait()
				select {
				case resp := <-response:
					switch r := resp.(type) {
					case *core.AppendEntriesResponse:
						if !r.Success {
							t.Fatal("heartbeat rejected during apply")
						}
					case *core.RequestVoteResponse:
						if !r.VoteGranted || r.Term != 2 {
							t.Fatalf("higher-term vote rejected during apply: %+v", r)
						}
					}
				default:
					t.Fatal("FSM apply blocked protocol response")
				}
				if n.GetState().LastApplied != 0 {
					t.Fatal("protocol response incorrectly advanced applied index")
				}
				release()
				synctest.Wait()
				if n.GetState().LastApplied != 1 || fsm.calls.Load() != 1 {
					t.Fatal("committed work did not apply exactly once after release")
				}
			})
		})
	}
}

func TestNodeStopAndDoneWaitForApplicationLoop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		n, fsm, release := protocolGateNode(t)
		defer func() { release(); _ = n.Stop(); synctest.Wait() }()
		gateCommittedApply(t, n, fsm)
		stopped := make(chan error, 1)
		go func() { stopped <- n.Stop() }()
		synctest.Wait()
		select {
		case <-n.Done():
			t.Fatal("Done closed while FSM still owned application work")
		default:
		}
		select {
		case <-stopped:
			t.Fatal("Stop returned before application gate was released")
		default:
		}
		release()
		synctest.Wait()
		if err := <-stopped; err != nil {
			t.Fatal(err)
		}
		select {
		case <-n.Done():
		default:
			t.Fatal("Done remained open after both loops stopped")
		}
	})
}

func TestApplicationLoopCoalescesPendingCommitNotifications(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		n, fsm, release := protocolGateNode(t)
		defer func() { release(); _ = n.Stop(); synctest.Wait() }()
		gateCommittedApply(t, n, fsm)
		appendProtocolEntry(t, n, "second")
		n.mu.Lock()
		n.commitIndex = 2
		n.mu.Unlock()
		for i := 0; i < 100; i++ {
			n.notifyCommit(2)
		}
		synctest.Wait()
		if fsm.calls.Load() != 1 {
			t.Fatal("pending notifications bypassed serialized apply gate")
		}
		release()
		synctest.Wait()
		if n.GetState().LastApplied != 2 || fsm.calls.Load() != 2 {
			t.Fatalf("latest commit not drained once: applied=%d calls=%d", n.GetState().LastApplied, fsm.calls.Load())
		}
	})
}
