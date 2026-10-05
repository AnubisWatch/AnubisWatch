package api

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

func TestWebSocketStopIdempotentRegression(t *testing.T) {
	for _, started := range []bool{false, true} {
		for _, concurrent := range []bool{false, true} {
			t.Run(fmt.Sprintf("started=%v/concurrent=%v", started, concurrent), func(t *testing.T) {
				s := NewWebSocketServer(newTestLogger(), &mockAuthenticator{}, nil)
				var canceled atomic.Int32
				c := &WSClient{ID: "c", send: make(chan []byte), cancel: func() { canceled.Add(1) }}
				s.clients[c.ID] = c
				s.rooms["room"] = map[string]bool{c.ID: true}
				if started {
					s.Start()
				}
				if concurrent {
					barrier := make(chan struct{})
					var wg sync.WaitGroup
					for range 4 {
						wg.Go(func() { <-barrier; s.Stop() })
					}
					close(barrier)
					wg.Wait()
				} else {
					s.Stop()
					s.Stop()
				}
				if s.ctx.Err() == nil {
					t.Fatal("server context not canceled")
				}
				if canceled.Load() != 1 {
					t.Fatalf("client canceled %d times", canceled.Load())
				}
				if len(s.clients) != 0 || len(s.rooms) != 0 {
					t.Fatal("retained clients/rooms")
				}
				select {
				case _, open := <-c.send:
					if open {
						t.Fatal("client channel open")
					}
				default:
					t.Fatal("client channel not closed")
				}
			})
		}
	}
}
