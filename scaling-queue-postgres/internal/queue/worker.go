package queue

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

type Counters struct {
	Enqueued      atomic.Int64
	Dequeued      atomic.Int64
	Completed     atomic.Int64
	Retries       atomic.Int64
	EmptyDequeues atomic.Int64
}

type Dequeuer interface {
	DequeueBatch(ctx context.Context) ([]int64, error)
	Complete(ctx context.Context, ids []int64) error
}

type Enqueuer interface {
	Enqueue(ctx context.Context, n int) error
}

type StageQueue struct {
	Store *Store
	Stage Stage
	Queue string
	Batch int
	// The shard this dequeuer is pinned to.
	Shard int
	// How many shards enqueued tasks spread over.
	Shards int
}

func (q StageQueue) DequeueBatch(ctx context.Context) ([]int64, error) {
	return q.Store.DequeueBatch(ctx, q.Stage, q.Queue, q.Shard, q.Batch)
}

func (q StageQueue) Complete(ctx context.Context, ids []int64) error {
	return q.Store.Complete(ctx, ids)
}

func (q StageQueue) Enqueue(ctx context.Context, n int) error {
	return q.Store.Enqueue(ctx, q.Queue, n, q.Shards)
}

type BackoffConfig struct {
	Base time.Duration
	Max  time.Duration
}

func (b BackoffConfig) delay(attempt int) time.Duration {
	d := b.Max
	if attempt < 20 {
		if scaled := b.Base << (attempt - 1); scaled > 0 && scaled < b.Max {
			d = scaled
		}
	}
	half := d / 2
	return half + rand.N(d-half+1)
}

type WorkerConfig struct {
	Backoff           BackoffConfig
	EmptyPollInterval time.Duration
	// Simulated task duration. Tasks stay PENDING this long. The worker
	// dequeues on while a goroutine completes the batch after the hold,
	// like an executor that holds a task but not a connection.
	Hold   time.Duration
	Record func(time.Duration)
	Sleep  func(context.Context, time.Duration) error
}

func RunWorker(ctx context.Context, q Dequeuer, cfg WorkerConfig, c *Counters) error {
	sleep := cfg.Sleep
	if sleep == nil {
		sleep = sleepCtx
	}
	var holds sync.WaitGroup
	defer holds.Wait()
	for {
		if ctx.Err() != nil {
			return nil
		}
		start := time.Now()
		var ids []int64
		for attempt := 1; ; attempt++ {
			var err error
			ids, err = q.DequeueBatch(ctx)
			if err == nil {
				break
			}
			if ctx.Err() != nil {
				return nil
			}
			if !Retryable(err) {
				return err
			}
			c.Retries.Add(1)
			if sleep(ctx, cfg.Backoff.delay(attempt)) != nil {
				return nil
			}
		}
		if len(ids) == 0 {
			c.EmptyDequeues.Add(1)
			if sleep(ctx, cfg.EmptyPollInterval) != nil {
				return nil
			}
			continue
		}
		c.Dequeued.Add(int64(len(ids)))
		if cfg.Record != nil {
			cfg.Record(time.Since(start))
		}
		if cfg.Hold > 0 {
			holds.Add(1)
			go func(ids []int64) {
				defer holds.Done()
				if sleep(ctx, cfg.Hold) != nil {
					return
				}
				if err := q.Complete(ctx, ids); err != nil {
					if ctx.Err() == nil {
						slog.Warn("complete after hold", "err", err)
					}
					return
				}
				c.Completed.Add(int64(len(ids)))
			}(ids)
			continue
		}
		if err := q.Complete(ctx, ids); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		c.Completed.Add(int64(len(ids)))
	}
}

type ProducerConfig struct {
	BatchSize    int
	CheckEvery   int
	HighWater    int64
	LowWater     int64
	Backlog      func() int64
	PollInterval time.Duration
	Sleep        func(context.Context, time.Duration) error
}

func RunProducer(ctx context.Context, e Enqueuer, cfg ProducerConfig, c *Counters) error {
	sleep := cfg.Sleep
	if sleep == nil {
		sleep = sleepCtx
	}
	for batches := 0; ; batches++ {
		if ctx.Err() != nil {
			return nil
		}
		if batches%cfg.CheckEvery == 0 && cfg.Backlog() > cfg.HighWater {
			for cfg.Backlog() > cfg.LowWater {
				if sleep(ctx, cfg.PollInterval) != nil {
					return nil
				}
			}
		}
		if err := e.Enqueue(ctx, cfg.BatchSize); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		c.Enqueued.Add(int64(cfg.BatchSize))
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
