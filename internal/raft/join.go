package raft

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Joining a running cluster by address.
//
// A node started with raft.join addresses, no bootstrap flag and no known
// peers is "awaiting join": it never elects itself (a node with no peers would
// otherwise become a one-node leader and split from the cluster) and it does
// not restrict which authenticated origins may replicate to it, because the
// leader that adds it is not yet in its configuration. joinLoop asks the join
// addresses to add it under its real node ID. The member that answers adds it
// through the leader's joint-consensus change (forwarding from a follower)
// and replies with every member's real ID and address, which the joiner
// installs as its peers -- from then on it is an ordinary voter.
//
// A cluster with a cluster secret admits a joiner automatically: the Join
// frame is authenticated at the transport, so only a holder of the secret can
// ask. Without a secret the answering member only confirms a node an operator
// already added (anubis summon <addr>, which identifies the node by address);
// anyone else is refused with ErrCodeJoinNotAdmitted and keeps retrying.
const (
	methodJoin     = "Join"
	methodIdentify = "Identify"

	// ErrCodeJoinNotAdmitted refuses a Join to a cluster without a secret
	// from a node no operator added.
	ErrCodeJoinNotAdmitted = "JOIN_NOT_ADMITTED"
	errCodeJoinInvalid     = "JOIN_INVALID"

	joinPoolPrefix    = "join:"
	joinRPCDeadline   = 50 * time.Second
	joinChangeTimeout = 40 * time.Second
	joinRetryMin      = 250 * time.Millisecond
	joinRetryMax      = 5 * time.Second
)

// AwaitingJoin reports whether this node is waiting to be added to a cluster
// (see joinLoop). It never campaigns while it is.
func (n *Node) AwaitingJoin() bool { return n.awaitingJoin.Load() }

// startJoining marks the node as awaiting join when its configuration asks to
// join and it has no members yet. Called from Start, after restored state.
func (n *Node) startJoining() {
	if len(n.config.Join) == 0 || n.config.Bootstrap {
		return
	}
	n.peerMu.RLock()
	known := len(n.peers)
	n.peerMu.RUnlock()
	if known > 0 {
		// Already a member (a persisted configuration, or explicit peers).
		return
	}
	n.awaitingJoin.Store(true)
	n.logger.Info("Waiting to be added to the cluster; this node will not elect itself", "join", n.config.Join)
	go n.joinLoop()
}

func (n *Node) joinLoop() {
	tt, ok := n.transport.(*TCPTransport)
	if !ok {
		n.logger.Warn("Transport cannot send Join requests; add this node from a member with anubis summon")
		return
	}
	req := &core.JoinRequest{
		NodeID:  n.nodeID,
		Address: firstNonEmptyString(n.advertiseAddr, n.bindAddr),
		Region:  n.config.Region,
		Role:    core.RoleVoter,
	}
	delay := joinRetryMin
	for n.awaitingJoin.Load() && !n.shutdown.Load() {
		for _, addr := range n.config.Join {
			if !n.awaitingJoin.Load() || n.shutdown.Load() {
				return
			}
			addr = strings.TrimSpace(addr)
			if addr == "" || addr == req.Address {
				continue
			}
			resp, err := tt.SendJoin(addr, req, joinRPCDeadline)
			switch {
			case err != nil:
				n.logger.Debug("Join request failed", "address", addr, "error", err)
			case !resp.Success:
				level := n.logger.Info
				if resp.ErrorCode != ErrCodeJoinNotAdmitted {
					level = n.logger.Warn
				}
				level("Join request refused", "address", addr, "code", resp.ErrorCode, "error", resp.Error)
			default:
				n.installJoinedMembers(resp.Peers, addr)
				n.logger.Info("Joined the cluster", "via", addr, "members", len(resp.Peers)+1)
				return
			}
		}
		select {
		case <-time.After(delay):
		case <-n.shutdownCh:
			return
		}
		if delay *= 2; delay > joinRetryMax {
			delay = joinRetryMax
		}
	}
}

// installJoinedMembers makes the members a Join answer listed this node's
// peers and ends the wait. via is the address the answer came from.
func (n *Node) installJoinedMembers(members []core.RaftPeer, via string) {
	tt, _ := n.transport.(*TCPTransport)
	usable := make([]core.RaftPeer, 0, len(members))
	for i, member := range members {
		if member.ID == "" || member.ID == n.nodeID || member.Address == "" {
			continue
		}
		if !dialableAddr(member.Address) {
			// The answering node lists itself first (membersExcept); without
			// advertise_addr its address is the wildcard bind, but we just
			// reached it at via. Recording 0.0.0.0 would make this node dial
			// itself for that member.
			if i == 0 && dialableAddr(via) {
				member.Address = via
			} else {
				n.logger.Warn("Ignoring cluster member with an undialable address; set necropolis advertise_addr on it",
					"member", member.ID, "address", member.Address)
				continue
			}
		}
		usable = append(usable, member)
	}
	n.peerMu.Lock()
	for _, member := range usable {
		if _, exists := n.peers[member.ID]; !exists {
			n.peers[member.ID] = &Peer{ID: member.ID, Address: member.Address, Region: member.Region, Role: member.Role}
		}
	}
	n.peerMu.Unlock()
	for _, member := range usable {
		if tt != nil {
			tt.RegisterPeer(member.ID, member.Address)
		}
	}

	n.membership.mu.Lock()
	inConfig := make(map[string]bool, len(n.membership.config))
	for _, id := range n.membership.config {
		inConfig[id] = true
	}
	for _, member := range members {
		if member.ID != "" && !inConfig[member.ID] {
			n.membership.config = append(n.membership.config, member.ID)
			inConfig[member.ID] = true
		}
	}
	n.membership.mu.Unlock()

	n.persistPeers()
	n.awaitingJoin.Store(false)
}

// handleJoin answers a Join request on a member.
func (n *Node) handleJoin(req *core.JoinRequest) *core.JoinResponse {
	refuse := func(code, msg string) *core.JoinResponse {
		return &core.JoinResponse{ErrorCode: code, Error: msg, LeaderID: n.Leader()}
	}
	if n.awaitingJoin.Load() {
		return refuse(core.ErrNoLeader, "this node is not a cluster member yet")
	}
	req.NodeID = strings.TrimSpace(req.NodeID)
	req.Address = strings.TrimSpace(req.Address)
	if req.NodeID == "" || len(req.NodeID) > 128 || strings.ContainsAny(req.NodeID, "/ \t\n") {
		return refuse(errCodeJoinInvalid, "a joining node needs a node ID without '/' or whitespace")
	}
	if !dialableAddr(req.Address) {
		return refuse(errCodeJoinInvalid, fmt.Sprintf("advertise address %q is not dialable; set necropolis.advertise_addr on the joining node", req.Address))
	}
	if req.NodeID == n.nodeID {
		return refuse(errCodeJoinInvalid, fmt.Sprintf("node ID %q is already used by this member", req.NodeID))
	}

	member, sameAddr := n.memberState(req.NodeID, req.Address)
	switch {
	case member && sameAddr:
		// Already admitted (an earlier Join, or anubis summon): just tell it
		// who the members are.
	case member:
		return refuse(errCodeJoinInvalid, fmt.Sprintf("node ID %q is already a member at another address", req.NodeID))
	case n.config.ClusterSecret == "":
		return refuse(ErrCodeJoinNotAdmitted, "this cluster has no cluster_secret, so joining nodes are not admitted automatically; add the node with: anubis summon "+req.Address)
	default:
		if id := n.memberAt(req.Address); id != "" {
			return refuse(errCodeJoinInvalid, fmt.Sprintf("address %s already belongs to member %q", req.Address, id))
		}
		role := req.Role
		if role == "" {
			role = core.RoleVoter
		}
		peer := core.RaftPeer{ID: req.NodeID, Address: req.Address, Region: req.Region, Role: role}
		if err := n.AddPeerOrForward(peer, joinChangeTimeout); err != nil {
			// A retried Join may race its own earlier success.
			if ok, same := n.memberState(req.NodeID, req.Address); !ok || !same {
				var raftErr *core.RaftError
				if errors.As(err, &raftErr) {
					return refuse(raftErr.Code, raftErr.Message)
				}
				return refuse("", err.Error())
			}
		}
		n.logger.Info("Admitted joining node", "peer_id", req.NodeID, "address", req.Address)
	}
	return &core.JoinResponse{Success: true, LeaderID: n.Leader(), Peers: n.membersExcept(req.NodeID)}
}

// handleIdentify tells a summoning node who this node is.
func (n *Node) handleIdentify() *core.IdentifyResponse {
	return &core.IdentifyResponse{
		ID:           n.nodeID,
		Address:      firstNonEmptyString(n.advertiseAddr, n.bindAddr),
		Region:       n.config.Region,
		AwaitingJoin: n.awaitingJoin.Load(),
	}
}

// IdentifyPeer asks the node listening on the Raft address addr for its node
// ID and region, so it can be added by address alone.
func (n *Node) IdentifyPeer(addr string, timeout time.Duration) (*core.IdentifyResponse, error) {
	tt, ok := n.transport.(*TCPTransport)
	if !ok {
		return nil, errors.New("transport cannot identify peers")
	}
	resp, err := tt.SendIdentify(addr, timeout)
	if err != nil {
		return nil, err
	}
	if resp.ID == "" {
		return nil, fmt.Errorf("node at %s did not report a node ID", addr)
	}
	return resp, nil
}

// memberState reports whether id is a peer, and whether it is at addr.
func (n *Node) memberState(id, addr string) (member, sameAddr bool) {
	n.peerMu.RLock()
	defer n.peerMu.RUnlock()
	peer, ok := n.peers[id]
	if !ok {
		return false, false
	}
	return true, peer.Address == addr
}

// memberAt returns the ID of the member at addr, if any.
func (n *Node) memberAt(addr string) string {
	if addr == firstNonEmptyString(n.advertiseAddr, n.bindAddr) {
		return n.nodeID
	}
	n.peerMu.RLock()
	defer n.peerMu.RUnlock()
	for id, peer := range n.peers {
		if peer.Address == addr {
			return id
		}
	}
	return ""
}

// membersExcept lists this node and its peers, leaving out id.
func (n *Node) membersExcept(id string) []core.RaftPeer {
	members := []core.RaftPeer{{
		ID:      n.nodeID,
		Address: firstNonEmptyString(n.advertiseAddr, n.bindAddr),
		Region:  n.config.Region,
		Role:    core.RoleVoter,
	}}
	for _, peer := range n.GetPeers() {
		if peer.ID != id && peer.ID != n.nodeID {
			members = append(members, peer)
		}
	}
	return members
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
