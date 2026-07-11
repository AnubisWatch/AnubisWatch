package alert

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type barrierDispatcher struct {
	entered chan struct{}
	release chan struct{}
	mu      sync.Mutex
	events  []*core.AlertEvent
	ids     []string
}

func (d *barrierDispatcher) Validate(map[string]any) error { return nil }

func (d *barrierDispatcher) Send(_ context.Context, event *core.AlertEvent, _ *core.AlertChannel) error {
	d.mu.Lock()
	d.events = append(d.events, event)
	d.mu.Unlock()
	d.entered <- struct{}{}
	<-d.release
	d.mu.Lock()
	d.ids = append(d.ids, event.ChannelID)
	d.mu.Unlock()
	return nil
}

func TestManagerDispatchClonesEventPerChannel(t *testing.T) {
	dispatcher := &barrierDispatcher{
		entered: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
	manager := NewManager(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	manager.dispatchers[core.ChannelWebHook] = dispatcher
	manager.channels["one"] = &core.AlertChannel{ID: "one", Type: core.ChannelWebHook, Enabled: true}
	manager.channels["two"] = &core.AlertChannel{ID: "two", Type: core.ChannelWebHook, Enabled: true}

	done := make(chan struct{})
	go func() {
		manager.dispatch(&core.AlertEvent{ID: "event", Severity: core.SeverityCritical})
		close(done)
	}()
	<-dispatcher.entered
	<-dispatcher.entered
	close(dispatcher.release)
	<-done

	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	if len(dispatcher.events) != 2 || dispatcher.events[0] == dispatcher.events[1] {
		t.Fatalf("dispatchers received shared event pointers: %#v", dispatcher.events)
	}
	seen := map[string]bool{}
	for _, id := range dispatcher.ids {
		seen[id] = true
	}
	if !seen["one"] || !seen["two"] {
		t.Fatalf("channel routing fields were not isolated: %v", dispatcher.ids)
	}
}

func TestManagerStopDoesNotHoldMutexWhileWaiting(t *testing.T) {
	dispatcher := &barrierDispatcher{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	manager := NewManager(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	manager.dispatchers[core.ChannelWebHook] = dispatcher
	manager.channels["one"] = &core.AlertChannel{ID: "one", Type: core.ChannelWebHook, Enabled: true}
	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	manager.queue <- &core.AlertEvent{ID: "event", Severity: core.SeverityCritical}
	<-dispatcher.entered

	stopped := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_ = manager.Stop()
			stopped <- struct{}{}
		}()
	}
	close(dispatcher.release)
	for i := 0; i < 2; i++ {
		select {
		case <-stopped:
		case <-time.After(2 * time.Second):
			t.Fatal("concurrent Stop deadlocked with a finishing dispatcher")
		}
	}

	// Stop remains safe after shutdown has completed.
	if err := manager.Stop(); err != nil {
		t.Fatal(err)
	}
}
