package main

import (
	"context"
	"log/slog"
	"sync"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/journey"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

// standaloneJourneys keeps the journey executor in step with storage when the
// node is not clustered (clusterWork does this in cluster mode). Without it
// only config-file journeys ran: journeys created, edited or deleted through
// the API were never (re)scheduled, and stored journeys were not loaded after
// a restart.
type standaloneJourneys struct {
	exec   *journey.Executor
	cfg    *core.Config
	store  *storage.CobaltDB
	logger *slog.Logger

	mu  sync.Mutex
	ctx context.Context // nil until start; changes before then are picked up by start
}

func (s *standaloneJourneys) start(ctx context.Context) {
	s.mu.Lock()
	s.ctx = ctx
	s.mu.Unlock()
	s.sync()
}

// sync schedules every enabled stored or configured journey and stops the
// rest. Executor.Assign keeps unchanged journeys on their current schedule.
func (s *standaloneJourneys) sync() {
	if s == nil || s.exec == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx == nil || s.ctx.Err() != nil {
		return
	}
	s.exec.Assign(s.ctx, collectJourneys(s.ctx, s.cfg, s.store, s.logger))
}
