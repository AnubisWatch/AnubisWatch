package api

import (
	"context"
	"crypto/subtle"
	"sync"
	"time"
)

// defaultSessionRevalidateInterval is how often a live WebSocket or SSE
// stream re-checks the session token it was opened with. Explicit
// revocation (logout, password change) closes streams at once; the periodic
// check catches everything else -- expiry, revocation on another node or by
// another component.
const defaultSessionRevalidateInterval = 30 * time.Second

// liveSessionRegistry tracks the long-lived streams (WebSocket, SSE) opened
// with each session token. Those streams authenticate only at their
// handshake, so without this a logged-out or revoked session kept receiving
// events for as long as the connection stayed up.
//
// A nil registry is valid and tracks nothing.
type liveSessionRegistry struct {
	mu      sync.Mutex
	next    uint64
	streams map[uint64]liveStream
}

type liveStream struct {
	token  string
	userID string
	close  func()
}

func newLiveSessionRegistry() *liveSessionRegistry {
	return &liveSessionRegistry{streams: make(map[uint64]liveStream)}
}

// add registers a stream opened with token by userID. closeFn ends the
// stream; it may be called more than once and from any goroutine. The
// returned function unregisters the stream and must be called when it ends.
func (r *liveSessionRegistry) add(token, userID string, closeFn func()) func() {
	if r == nil {
		return func() {}
	}
	r.mu.Lock()
	r.next++
	id := r.next
	r.streams[id] = liveStream{token: token, userID: userID, close: closeFn}
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.streams, id)
		r.mu.Unlock()
	}
}

// revoke closes every stream matching match and returns how many it closed.
// The close functions run after the registry lock is released, so they may
// take other locks (the WebSocket server's) freely.
func (r *liveSessionRegistry) revoke(match func(liveStream) bool) int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	var closers []func()
	for id, st := range r.streams {
		if match(st) {
			closers = append(closers, st.close)
			delete(r.streams, id)
		}
	}
	r.mu.Unlock()
	for _, closeFn := range closers {
		closeFn()
	}
	return len(closers)
}

// revokeToken closes the streams opened with token.
func (r *liveSessionRegistry) revokeToken(token string) int {
	if token == "" {
		return 0
	}
	return r.revoke(func(st liveStream) bool {
		return subtle.ConstantTimeCompare([]byte(st.token), []byte(token)) == 1
	})
}

// watchSession re-authenticates token every interval until ctx ends, and
// calls onRevoked (once) as soon as the authenticator rejects it. The
// returned channel closes when the watcher exits.
func watchSession(ctx context.Context, auth Authenticator, token string, interval time.Duration, onRevoked func()) <-chan struct{} {
	done := make(chan struct{})
	if auth == nil || interval <= 0 {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := auth.Authenticate(token); err != nil {
					onRevoked()
					return
				}
			}
		}
	}()
	return done
}
