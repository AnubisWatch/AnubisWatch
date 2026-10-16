package raft

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

const (
	// forwardMaxSkew bounds how old (or how far in the future) a signed
	// Forward request may be. Together with the nonce cache it limits replay.
	forwardMaxSkew = 60 * time.Second

	// errCodeForwardRejected is returned to a follower whose Forward request
	// failed authentication or came from a node outside the membership.
	errCodeForwardRejected = "FORWARD_REJECTED"

	applyPollInterval = 5 * time.Millisecond
)

// forwardTransport is implemented by transports that can carry Forward RPCs
// (TCPTransport). Test transports that do not implement it simply cannot
// forward, and followers report ErrNoLeader.
type forwardTransport interface {
	SendForward(peerID string, req *core.ForwardRequest, deadline time.Duration) (*core.ForwardResponse, error)
}

// forwardNonces remembers the nonces of recently accepted signed Forward
// requests so a captured request cannot be replayed inside the skew window.
type forwardNonces struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

// claim records nonce and reports whether it was unused.
func (c *forwardNonces) claim(nonce string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen == nil {
		c.seen = make(map[string]time.Time)
	}
	for k, expiry := range c.seen {
		if now.After(expiry) {
			delete(c.seen, k)
		}
	}
	if _, used := c.seen[nonce]; used {
		return false
	}
	c.seen[nonce] = now.Add(2 * forwardMaxSkew)
	return true
}

// forwardMAC is the hex HMAC-SHA256 over the request's canonical form. Every
// field that changes what the leader does is covered, so a signed request
// cannot be altered in flight.
func forwardMAC(secret []byte, req *core.ForwardRequest) string {
	var payload []byte
	switch {
	case req.Command != nil:
		payload, _ = json.Marshal(req.Command)
	case req.Peer != nil:
		payload, _ = json.Marshal(req.Peer)
	}
	canonical := strings.Join([]string{
		"anubis-forward-v1",
		req.Kind,
		req.Origin,
		req.PeerID,
		strconv.FormatInt(req.TimeoutMs, 10),
		strconv.FormatInt(req.Timestamp, 10),
		req.Nonce,
		string(payload),
	}, "\n")
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

func newForwardNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

// verifyForward authenticates an inbound Forward request. The sender must be
// a member of this node's configuration; with a cluster secret configured the
// request must also carry a valid, fresh, unreplayed MAC.
func (n *Node) verifyForward(req *core.ForwardRequest) error {
	n.peerMu.RLock()
	_, member := n.peers[req.Origin]
	n.peerMu.RUnlock()
	if !member {
		return fmt.Errorf("forward from %q, which is not a cluster member", req.Origin)
	}

	secret := n.config.ClusterSecret
	if secret == "" {
		return nil
	}
	if req.MAC == "" {
		return errors.New("unsigned forward request")
	}
	expected := forwardMAC([]byte(secret), req)
	if !hmac.Equal([]byte(expected), []byte(req.MAC)) {
		return errors.New("forward request signature mismatch")
	}
	now := time.Now()
	if skew := now.Sub(time.Unix(0, req.Timestamp)); skew > forwardMaxSkew || skew < -forwardMaxSkew {
		return fmt.Errorf("forward request timestamp outside the %v window", forwardMaxSkew)
	}
	if req.Nonce == "" || !n.forwardNonces.claim(req.Nonce, now) {
		return errors.New("forward request replayed")
	}
	return nil
}

// handleForward runs a follower's leader-only request on the leader. It runs
// on its own goroutine, never on the run loop: an apply waits for the commit
// the run loop produces.
func (n *Node) handleForward(req *core.ForwardRequest) *core.ForwardResponse {
	if err := n.verifyForward(req); err != nil {
		n.logger.Warn("Rejected forwarded request", "origin", req.Origin, "kind", req.Kind, "error", err)
		return &core.ForwardResponse{ErrorCode: errCodeForwardRejected, Error: err.Error()}
	}

	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 || timeout > maxForwardDeadline {
		timeout = maxForwardDeadline
	}

	var err error
	resp := &core.ForwardResponse{}
	switch req.Kind {
	case core.ForwardApply:
		if req.Command == nil {
			return &core.ForwardResponse{Error: "forward apply without a command"}
		}
		resp.Index, resp.Term, _, err = n.Apply(*req.Command, timeout)
	case core.ForwardAddPeer:
		if req.Peer == nil {
			return &core.ForwardResponse{Error: "forward add_peer without a peer"}
		}
		err = n.AddPeer(*req.Peer)
	case core.ForwardRemovePeer:
		err = n.RemovePeer(req.PeerID)
	default:
		return &core.ForwardResponse{Error: fmt.Sprintf("unknown forward kind %q", req.Kind)}
	}
	if err != nil {
		var raftErr *core.RaftError
		if errors.As(err, &raftErr) {
			resp.ErrorCode, resp.Error, resp.LeaderID = raftErr.Code, raftErr.Message, raftErr.NodeID
		} else {
			resp.Error = err.Error()
		}
	}
	return resp
}

// forwardResponseError rebuilds the leader's error on the follower, keeping
// RaftError codes typed so callers can still branch on them.
func forwardResponseError(resp *core.ForwardResponse) error {
	if resp.ErrorCode != "" {
		return &core.RaftError{Code: resp.ErrorCode, Message: resp.Error, NodeID: resp.LeaderID}
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

func isRaftCode(err error, codes ...string) bool {
	var raftErr *core.RaftError
	if !errors.As(err, &raftErr) {
		return false
	}
	for _, code := range codes {
		if raftErr.Code == code {
			return true
		}
	}
	return false
}

// noLeaderWait bounds how long a write waits for an election to produce a
// leader before it is refused with ErrNoLeader: long enough to ride out one
// election, short enough that an API client gets a prompt 503.
func (n *Node) noLeaderWait(timeout time.Duration) time.Duration {
	wait := 5 * n.electionTimeout
	if wait <= 0 || wait > timeout {
		wait = timeout
	}
	return wait
}

func noLeaderError(cause error) error {
	msg := "no cluster leader is available"
	if cause != nil {
		msg += ": " + cause.Error()
	}
	return &core.RaftError{Code: core.ErrNoLeader, Message: msg}
}

// sendForward sends req to the current leader and waits for its answer. It
// retries while there is no reachable leader (an election in progress, a
// leader that just died) until the no-leader budget runs out, and while the
// node it reached says it is no longer the leader.
func (n *Node) sendForward(req core.ForwardRequest, timeout time.Duration) (*core.ForwardResponse, error) {
	ft, ok := n.transport.(forwardTransport)
	if !ok {
		return nil, noLeaderError(errors.New("transport cannot forward"))
	}
	deadline := time.Now().Add(timeout)
	leaderDeadline := time.Now().Add(n.noLeaderWait(timeout))
	retryDelay := n.heartbeatTimeout / 2
	if retryDelay <= 0 {
		retryDelay = 50 * time.Millisecond
	}

	var lastErr error
	for {
		if n.shutdown.Load() {
			return nil, &core.RaftError{Code: core.ErrShutdown, Message: "node is shutting down"}
		}
		if n.IsLeader() {
			return nil, &core.RaftError{Code: core.ErrNotLeader, Message: "this node became the leader", NodeID: n.nodeID}
		}
		leader := n.Leader()
		if leader != "" && leader != n.nodeID {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return nil, &core.RaftError{Code: core.ErrTimeout, Message: "timeout forwarding to the leader"}
			}
			req.Origin = n.nodeID
			req.TimeoutMs = remaining.Milliseconds()
			req.Timestamp = time.Now().UnixNano()
			req.Nonce = newForwardNonce()
			req.MAC = ""
			if secret := n.config.ClusterSecret; secret != "" {
				req.MAC = forwardMAC([]byte(secret), &req)
			}
			resp, err := ft.SendForward(leader, &req, remaining+forwardRPCSlack)
			switch {
			case err != nil:
				lastErr = fmt.Errorf("forward to leader %s: %w", leader, err)
			case isRaftCode(forwardResponseError(resp), core.ErrNotLeader, core.ErrLeadershipLost):
				lastErr = forwardResponseError(resp)
			default:
				return resp, nil
			}
		}
		if time.Now().After(leaderDeadline) {
			return nil, noLeaderError(lastErr)
		}
		select {
		case <-time.After(retryDelay):
		case <-n.shutdownCh:
		}
	}
}

// WaitForApplied blocks until this node's state machine has applied index, so
// a read on this node observes every write up to it.
func (n *Node) WaitForApplied(index uint64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		n.mu.RLock()
		applied := n.lastApplied
		n.mu.RUnlock()
		if applied >= index {
			return nil
		}
		if n.shutdown.Load() {
			return &core.RaftError{Code: core.ErrShutdown, Message: "node is shutting down"}
		}
		if time.Now().After(deadline) {
			return &core.RaftError{Code: core.ErrTimeout, Message: fmt.Sprintf("timeout waiting for index %d to apply locally (applied %d)", index, applied)}
		}
		time.Sleep(applyPollInterval)
	}
}

// ApplyOrForward commits cmd through consensus from any node. On the leader it
// is Apply; on a follower the command is forwarded to the leader over the Raft
// transport and the call returns only once this node has applied the entry, so
// a read-your-write on the follower sees the change. With no leader reachable
// within the no-leader budget it returns a RaftError with ErrNoLeader.
//
// A forward whose response is lost may be retried and so applied twice; every
// command the application submits is an idempotent keyed write.
func (n *Node) ApplyOrForward(cmd core.FSMCommand, timeout time.Duration) (uint64, uint64, error) {
	deadline := time.Now().Add(timeout)
	for {
		if n.shutdown.Load() {
			return 0, 0, &core.RaftError{Code: core.ErrShutdown, Message: "node is shutting down"}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return 0, 0, &core.RaftError{Code: core.ErrTimeout, Message: "timeout applying command"}
		}
		if n.IsLeader() {
			index, term, _, err := n.Apply(cmd, remaining)
			if isRaftCode(err, core.ErrNotLeader) {
				// Lost leadership before the entry was appended: forward instead.
				continue
			}
			return index, term, err
		}

		resp, err := n.sendForward(core.ForwardRequest{Kind: core.ForwardApply, Command: &cmd}, remaining)
		if isRaftCode(err, core.ErrNotLeader) && n.IsLeader() {
			continue
		}
		if err != nil {
			return 0, 0, err
		}
		if err := forwardResponseError(resp); err != nil {
			return 0, 0, err
		}
		if err := n.WaitForApplied(resp.Index, time.Until(deadline)); err != nil {
			return resp.Index, resp.Term, err
		}
		return resp.Index, resp.Term, nil
	}
}

// AddPeerOrForward adds a peer through the leader's joint-consensus change,
// forwarding the request when this node is a follower.
func (n *Node) AddPeerOrForward(peer core.RaftPeer, timeout time.Duration) error {
	if n.IsLeader() {
		return n.AddPeer(peer)
	}
	resp, err := n.sendForward(core.ForwardRequest{Kind: core.ForwardAddPeer, Peer: &peer}, timeout)
	if err != nil {
		return err
	}
	return forwardResponseError(resp)
}

// RemovePeerOrForward removes a peer through the leader's joint-consensus
// change, forwarding the request when this node is a follower.
func (n *Node) RemovePeerOrForward(peerID string, timeout time.Duration) error {
	if n.IsLeader() {
		return n.RemovePeer(peerID)
	}
	resp, err := n.sendForward(core.ForwardRequest{Kind: core.ForwardRemovePeer, PeerID: peerID}, timeout)
	if err != nil {
		return err
	}
	return forwardResponseError(resp)
}
