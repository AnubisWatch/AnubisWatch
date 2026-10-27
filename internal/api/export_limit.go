package api

import (
	"net/http"
	"sync"
	"time"
)

// Judgment exports stream for as long as the client reads, holding a
// goroutine and up to an hour of a soul's judgments each, so they are
// limited per user and per server, and a client that stops reading is cut
// off after exportIdleTimeout instead of holding the slot.
const (
	maxExportsPerUser = 2
	maxExportsTotal   = 8
	exportBufferSize  = 32 << 10
)

var (
	// exportIdleTimeout is how long one buffered write may wait for the
	// client: an export must move at least one buffer (32 KiB) per window.
	exportIdleTimeout = 30 * time.Second
	// exportMaxDuration caps a whole export.
	exportMaxDuration = 15 * time.Minute
)

// exportLimiter counts running exports. The zero value is ready to use.
type exportLimiter struct {
	mu     sync.Mutex
	byUser map[string]int
	total  int
}

// acquire reserves a slot for user, false when the user or the server is at
// its limit.
func (l *exportLimiter) acquire(user string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.total >= maxExportsTotal || l.byUser[user] >= maxExportsPerUser {
		return false
	}
	if l.byUser == nil {
		l.byUser = make(map[string]int)
	}
	l.byUser[user]++
	l.total++
	return true
}

func (l *exportLimiter) release(user string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.byUser[user] <= 1 {
		delete(l.byUser, user)
	} else {
		l.byUser[user]--
	}
	if l.total > 0 {
		l.total--
	}
}

// idleDeadlineWriter renews the connection's write deadline before every
// write, so the response may run long while each write must finish within
// idle.
type idleDeadlineWriter struct {
	w    http.ResponseWriter
	rc   *http.ResponseController
	idle time.Duration
}

func newIdleDeadlineWriter(w http.ResponseWriter, idle time.Duration) *idleDeadlineWriter {
	return &idleDeadlineWriter{w: w, rc: http.NewResponseController(w), idle: idle}
}

func (d *idleDeadlineWriter) Write(p []byte) (int, error) {
	_ = d.rc.SetWriteDeadline(time.Now().Add(d.idle))
	return d.w.Write(p)
}

func (d *idleDeadlineWriter) Flush() {
	_ = d.rc.SetWriteDeadline(time.Now().Add(d.idle))
	_ = d.rc.Flush()
}
