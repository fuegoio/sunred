// Package worker implements a fixed-size worker pool that processes feed
// refresh jobs concurrently.
package worker

import (
	"context"
	"log/slog"
	"sync"

	"github.com/fuegoio/sunred/go/api/internal/reader/processor"
	"github.com/fuegoio/sunred/go/api/internal/store"
)

// Pool is a worker pool that processes feed refresh jobs.
type Pool struct {
	processor *processor.Processor
	jobs      chan *store.Feed
	wg        sync.WaitGroup
}

// New returns a worker pool with the given concurrency.
func New(proc *processor.Processor, concurrency int) *Pool {
	p := &Pool{
		processor: proc,
		jobs:      make(chan *store.Feed, concurrency*2),
	}
	for i := 0; i < concurrency; i++ {
		p.wg.Add(1)
		go p.worker()
	}
	return p
}

// Submit enqueues a feed for processing. It blocks until the feed is enqueued
// or ctx is cancelled, so a scheduler tick never silently drops due feeds —
// the pool applies backpressure instead. This matters at startup, where the
// first tick dispatches the full due set at once.
func (p *Pool) Submit(ctx context.Context, feed *store.Feed) {
	select {
	case p.jobs <- feed:
	case <-ctx.Done():
	}
}

func (p *Pool) worker() {
	defer p.wg.Done()
	for feed := range p.jobs {
		ctx := context.Background()
		p.process(ctx, feed)
	}
}

// process runs a single feed through the processor and isolates failures: a
// returned error is logged, and a panic is recovered so one bad feed never
// kills the worker goroutine (which would permanently shrink the pool).
func (p *Pool) process(ctx context.Context, feed *store.Feed) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("process feed panic recovered", "feed_id", feed.ID, "url", feed.FeedURL, "panic", r)
		}
	}()
	if err := p.processor.ProcessFeed(ctx, feed); err != nil {
		slog.Error("process feed", "feed_id", feed.ID, "url", feed.FeedURL, "err", err)
	}
}

// Stop waits for all workers to finish their current jobs.
func (p *Pool) Stop() {
	close(p.jobs)
	p.wg.Wait()
}
