package raft

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Transport authentication.
//
// With a cluster secret configured, every frame on the Raft TCP transport --
// RequestVote, PreVote, AppendEntries, Heartbeat, InstallSnapshot, Forward and
// Join -- carries an auth line between the method and length lines:
//
//	<method>\n
//	AUTH <origin> <epoch> <seq> <unix-nanos> <hex hmac>\n
//	<length>\n
//	<payload>
//
// The MAC is HMAC-SHA256 under the secret over the method, every auth field
// and the payload, so nothing in the frame can be altered. The receiver checks,
// in order: the MAC, that the timestamp is within transportMaxSkew of its own
// clock, that origin is a cluster member, and that (origin, epoch, seq) was not
// seen before. The response carries "<length>\n<hex hmac>\n<payload>", its MAC
// binding the payload to the request's origin, epoch and seq, so a response
// cannot be forged or replayed onto another request either.
//
// Replay protection is a per-sender sliding window (as in IPsec, RFC 4303)
// instead of a nonce set: each process picks a random epoch at startup and
// numbers its frames 1, 2, 3, ... The receiver keeps, per (origin, epoch), the
// highest seq seen and a bitmap of the replayWindowBits below it. Accepting a
// frame is O(1) with no allocation, and the state is a fixed few hundred
// bytes per live sender incarnation -- the hot AppendEntries/heartbeat path
// never grows a map or sweeps one.
//
// Without a secret frames are unsigned, as before, and the cluster manager
// logs that cluster traffic is unauthenticated.
const (
	transportAuthPrefix = "AUTH "
	transportMACDomain  = "anubis-raft-rpc-v1"
	transportRespDomain = "anubis-raft-resp-v1"

	// transportMaxSkew bounds how far a frame's timestamp may be from the
	// receiver's clock (node clocks must agree within it).
	transportMaxSkew = 60 * time.Second

	// replayWindowBits is how far below the highest seq seen from a sender a
	// frame may arrive out of order. Frames are numbered just before they are
	// written, so reordering is limited to frames in transit at once.
	replayWindowBits = 4096

	// maxEpochsPerOrigin and maxReplayOrigins bound the replay state. An
	// epoch idle for longer than 2*transportMaxSkew is dropped: no frame of it
	// can still pass the timestamp check unless it was never delivered.
	maxEpochsPerOrigin = 8
	maxReplayOrigins   = 1024
)

// errTransportAuth marks an inbound frame refused by transport authentication.
var errTransportAuth = errors.New("raft transport authentication failed")

// transportAuth signs outbound frames and verifies inbound ones.
type transportAuth struct {
	secret []byte
	nodeID string
	epoch  uint64
	seq    atomic.Uint64

	// isMember reports whether origin may send method to this node. nil
	// admits every origin that holds the secret.
	isMember func(origin, method string) bool

	mu     sync.Mutex
	replay map[string]*originReplay
}

type originReplay struct {
	epochs   map[uint64]*replayWindow
	lastSeen time.Time
}

type replayWindow struct {
	highest  uint64
	bits     [replayWindowBits / 64]uint64
	lastSeen time.Time
}

func newTransportAuth(nodeID, secret string, isMember func(origin, method string) bool) *transportAuth {
	var b [8]byte
	epoch := uint64(time.Now().UnixNano())
	if _, err := rand.Read(b[:]); err == nil {
		epoch = binary.BigEndian.Uint64(b[:])
	}
	return &transportAuth{
		secret:   []byte(secret),
		nodeID:   nodeID,
		epoch:    epoch,
		isMember: isMember,
		replay:   make(map[string]*originReplay),
	}
}

// frameAuth is the parsed auth line of one frame.
type frameAuth struct {
	origin string
	epoch  uint64
	seq    uint64
	ts     int64
	mac    string
}

func (a *transportAuth) requestMAC(method string, f *frameAuth, payload []byte) string {
	mac := hmac.New(sha256.New, a.secret)
	writeMACFields(mac, transportMACDomain, method, f.origin,
		strconv.FormatUint(f.epoch, 10), strconv.FormatUint(f.seq, 10), strconv.FormatInt(f.ts, 10))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *transportAuth) responseMAC(f *frameAuth, payload []byte) string {
	mac := hmac.New(sha256.New, a.secret)
	writeMACFields(mac, transportRespDomain, f.origin,
		strconv.FormatUint(f.epoch, 10), strconv.FormatUint(f.seq, 10))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func writeMACFields(h hash.Hash, fields ...string) {
	for _, field := range fields {
		h.Write([]byte(field))
		h.Write([]byte{'\n'})
	}
}

// sign numbers and signs an outbound frame and returns its auth line (with
// the trailing newline) and the fields the response must be bound to.
func (a *transportAuth) sign(method string, payload []byte) (string, *frameAuth) {
	f := &frameAuth{
		origin: a.nodeID,
		epoch:  a.epoch,
		seq:    a.seq.Add(1),
		ts:     time.Now().UnixNano(),
	}
	f.mac = a.requestMAC(method, f, payload)
	line := fmt.Sprintf("%s%s %d %d %d %s\n", transportAuthPrefix, f.origin, f.epoch, f.seq, f.ts, f.mac)
	return line, f
}

func parseAuthLine(line string) (*frameAuth, error) {
	if !strings.HasPrefix(line, transportAuthPrefix) {
		return nil, fmt.Errorf("%w: unsigned frame", errTransportAuth)
	}
	fields := strings.Fields(strings.TrimPrefix(line, transportAuthPrefix))
	if len(fields) != 5 {
		return nil, fmt.Errorf("%w: malformed auth line", errTransportAuth)
	}
	f := &frameAuth{origin: fields[0], mac: fields[4]}
	var err error
	if f.epoch, err = strconv.ParseUint(fields[1], 10, 64); err != nil {
		return nil, fmt.Errorf("%w: malformed epoch", errTransportAuth)
	}
	if f.seq, err = strconv.ParseUint(fields[2], 10, 64); err != nil || f.seq == 0 {
		return nil, fmt.Errorf("%w: malformed sequence", errTransportAuth)
	}
	if f.ts, err = strconv.ParseInt(fields[3], 10, 64); err != nil {
		return nil, fmt.Errorf("%w: malformed timestamp", errTransportAuth)
	}
	return f, nil
}

// precheck rejects a frame whose timestamp or origin is already unacceptable,
// before its payload is read, so a client without the secret cannot make the
// node buffer a large payload just to fail the MAC.
func (a *transportAuth) precheck(method string, f *frameAuth, now time.Time) error {
	if f.origin == "" {
		return fmt.Errorf("%w: no origin", errTransportAuth)
	}
	if skew := now.Sub(time.Unix(0, f.ts)); skew > transportMaxSkew || skew < -transportMaxSkew {
		return fmt.Errorf("%w: timestamp from %q outside the %v window", errTransportAuth, f.origin, transportMaxSkew)
	}
	if a.isMember != nil && !a.isMember(f.origin, method) {
		return fmt.Errorf("%w: %q is not a cluster member (each entry in necropolis.raft.peers must use the node's own node_name as its id)", errTransportAuth, f.origin)
	}
	return nil
}

// verify authenticates an inbound frame. It is called only after the whole
// payload was read, so the MAC covers exactly what will be decoded.
func (a *transportAuth) verify(method string, f *frameAuth, payload []byte, now time.Time) error {
	if f.origin == "" {
		return fmt.Errorf("%w: no origin", errTransportAuth)
	}
	if !hmac.Equal([]byte(a.requestMAC(method, f, payload)), []byte(f.mac)) {
		return fmt.Errorf("%w: signature mismatch from %q", errTransportAuth, f.origin)
	}
	if skew := now.Sub(time.Unix(0, f.ts)); skew > transportMaxSkew || skew < -transportMaxSkew {
		return fmt.Errorf("%w: timestamp from %q outside the %v window", errTransportAuth, f.origin, transportMaxSkew)
	}
	if a.isMember != nil && !a.isMember(f.origin, method) {
		return fmt.Errorf("%w: %q is not a cluster member (each entry in necropolis.raft.peers must use the node's own node_name as its id)", errTransportAuth, f.origin)
	}
	if !a.claim(f.origin, f.epoch, f.seq, now) {
		return fmt.Errorf("%w: replayed frame from %q", errTransportAuth, f.origin)
	}
	return nil
}

// claim records (origin, epoch, seq) and reports whether it was new.
func (a *transportAuth) claim(origin string, epoch, seq uint64, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	o := a.replay[origin]
	if o == nil {
		if len(a.replay) >= maxReplayOrigins {
			a.pruneOriginsLocked(now)
		}
		o = &originReplay{epochs: make(map[uint64]*replayWindow, 1)}
		a.replay[origin] = o
	}
	o.lastSeen = now

	w := o.epochs[epoch]
	if w == nil {
		if len(o.epochs) >= maxEpochsPerOrigin {
			pruneEpochs(o, now)
		}
		w = &replayWindow{}
		o.epochs[epoch] = w
	}
	w.lastSeen = now
	return w.accept(seq)
}

// pruneEpochs drops idle epochs, and the least recently seen one if all of
// them are still live.
func pruneEpochs(o *originReplay, now time.Time) {
	var oldest uint64
	var oldestSeen time.Time
	for epoch, w := range o.epochs {
		if now.Sub(w.lastSeen) > 2*transportMaxSkew {
			delete(o.epochs, epoch)
			continue
		}
		if oldestSeen.IsZero() || w.lastSeen.Before(oldestSeen) {
			oldest, oldestSeen = epoch, w.lastSeen
		}
	}
	if len(o.epochs) >= maxEpochsPerOrigin {
		delete(o.epochs, oldest)
	}
}

func (a *transportAuth) pruneOriginsLocked(now time.Time) {
	var oldest string
	var oldestSeen time.Time
	for origin, o := range a.replay {
		if now.Sub(o.lastSeen) > 2*transportMaxSkew {
			delete(a.replay, origin)
			continue
		}
		if oldestSeen.IsZero() || o.lastSeen.Before(oldestSeen) {
			oldest, oldestSeen = origin, o.lastSeen
		}
	}
	if len(a.replay) >= maxReplayOrigins {
		delete(a.replay, oldest)
	}
}

// accept implements the sliding anti-replay window: bit i of bits records
// whether highest-i was seen.
func (w *replayWindow) accept(seq uint64) bool {
	switch {
	case seq > w.highest:
		w.shift(seq - w.highest)
		w.highest = seq
		w.bits[0] |= 1
		return true
	default:
		d := w.highest - seq
		if d >= replayWindowBits {
			return false
		}
		word, bit := d/64, d%64
		if w.bits[word]&(1<<bit) != 0 {
			return false
		}
		w.bits[word] |= 1 << bit
		return true
	}
}

// shift moves the window up by n positions.
func (w *replayWindow) shift(n uint64) {
	if n >= replayWindowBits {
		w.bits = [replayWindowBits / 64]uint64{}
		return
	}
	words, bitsN := int(n/64), n%64
	if words > 0 {
		for i := len(w.bits) - 1; i >= 0; i-- {
			if i-words >= 0 {
				w.bits[i] = w.bits[i-words]
			} else {
				w.bits[i] = 0
			}
		}
	}
	if bitsN > 0 {
		for i := len(w.bits) - 1; i > 0; i-- {
			w.bits[i] = w.bits[i]<<bitsN | w.bits[i-1]>>(64-bitsN)
		}
		w.bits[0] <<= bitsN
	}
}
