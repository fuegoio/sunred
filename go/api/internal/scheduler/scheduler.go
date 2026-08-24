// Package scheduler periodically selects feeds due for refresh and feeds them
// to the worker pool.
package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/fuegoio/sunred/go/api/internal/store"
	"github.com/fuegoio/sunred/go/api/internal/worker"
)

// Scheduler ticks at a fixed interval and dispatches due feeds to the worker pool.
type Scheduler struct {
	store     *store.Store
	worker    *worker.Pool
	interval  time.Duration
	batchSize int
}

// New returns a Scheduler that queries the store for due feeds every interval
// and dispatches them to the worker pool.
func New(st *store.Store, pool *worker.Pool, interval time.Duration, batchSize int) *Scheduler {
	return &Scheduler{
		store:     st,
		worker:    pool,
		interval:  interval,
		batchSize: batchSize,
	}
}

// Start runs the scheduler loop until ctx is cancelled. It ticks immediately
// on startup so due feeds are refreshed right away, then continues at the
// configured interval.
func (s *Scheduler) Start(ctx context.Context) {
	slog.Info("scheduler started", "interval", s.interval, "batch_size", s.batchSize)
	s.tick(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("scheduler stopped")
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	feeds, err := s.store.ListFeedsDueForRefresh(ctx, s.batchSize)
	if err != nil {
		slog.Error("scheduler: list due feeds", "err", err)
		return
	}
	if len(feeds) == 0 {
		return
	}
	slog.Info("scheduler: dispatching feeds", "count", len(feeds))
	for _, feed := range feeds {
		feed := feed
		s.worker.Submit(ctx, &feed)
	}
}
