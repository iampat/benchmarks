package queue

import (
	"container/heap"
	"context"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

type Counters struct {
	Created     atomic.Int64
	Claimed     atomic.Int64
	Done        atomic.Int64
	Retries     atomic.Int64
	EmptyClaims atomic.Int64
	InFlight    atomic.Int64
}

type Claimer interface {
	Claim(ctx context.Context) ([]int64, error)
}

type Completer interface {
	Done(ctx context.Context, ids []int64) error
}

type Creator interface {
	Create(ctx context.Context, n int) error
}

type StageQueue struct {
	Store  *Store
	Stage  Stage
	Queue  string
	Batch  int
	Shard  int
	Shards int
}

func (q StageQueue) Claim(ctx context.Context) ([]int64, error) {
	return q.Store.Claim(ctx, q.Stage, q.Queue, q.Shard, q.Batch)
}

func (q StageQueue) Done(ctx context.Context, ids []int64) error {
	return q.Store.Done(ctx, ids)
}

func (q StageQueue) Create(ctx context.Context, n int) error {
	return q.Store.Create(ctx, q.Queue, n, q.Shards)
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

// A task occupies a slot from the moment a worker claims it until a completer
// writes DONE. The slot count bounds the tasks in flight, the way a fixed
// executor pool does. A sleeping task holds a slot and no database connection.
type Slots chan struct{}

func NewSlots(n int) Slots { return make(Slots, n) }

func (s Slots) Acquire(ctx context.Context) bool {
	select {
	case s <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (s Slots) Release() { <-s }

type pending struct {
	id       int64
	deadline time.Time
}

type deadlineHeap []pending

func (h deadlineHeap) Len() int           { return len(h) }
func (h deadlineHeap) Less(i, j int) bool { return h[i].deadline.Before(h[j].deadline) }
func (h deadlineHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *deadlineHeap) Push(x any)        { *h = append(*h, x.(pending)) }
func (h *deadlineHeap) Pop() any          { old := *h; n := len(old); x := old[n-1]; *h = old[:n-1]; return x }

// Scheduler holds claimed tasks for their duration. One heap replaces one
// goroutine and one timer per task, which matters at 300,000 tasks in flight.
type Scheduler struct {
	mu  sync.Mutex
	h   deadlineHeap
	due chan int64
}

func NewScheduler(dueBuffer int) *Scheduler {
	return &Scheduler{due: make(chan int64, dueBuffer)}
}

func (s *Scheduler) Add(id int64, deadline time.Time) {
	s.mu.Lock()
	heap.Push(&s.h, pending{id: id, deadline: deadline})
	s.mu.Unlock()
}

func (s *Scheduler) Due() <-chan int64 { return s.due }

func (s *Scheduler) Depth() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.h)
}

// Run moves tasks whose duration elapsed onto the due channel. The 5 ms tick
// adds at most 5 ms to a task duration of 10 seconds or more.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			for {
				s.mu.Lock()
				if len(s.h) == 0 || s.h[0].deadline.After(now) {
					s.mu.Unlock()
					break
				}
				p := heap.Pop(&s.h).(pending)
				s.mu.Unlock()
				select {
				case s.due <- p.id:
				case <-ctx.Done():
					return
				}
			}
		}
	}
}

type WorkerConfig struct {
	Backoff           BackoffConfig
	EmptyPollInterval time.Duration
	MinDuration       time.Duration
	MaxDuration       time.Duration
	Slots             Slots
	Sched             *Scheduler
	Record            func(time.Duration)
	Sleep             func(context.Context, time.Duration) error
}

func (c WorkerConfig) duration() time.Duration {
	if c.MaxDuration <= c.MinDuration {
		return c.MinDuration
	}
	return c.MinDuration + rand.N(c.MaxDuration-c.MinDuration)
}

// RunWorker claims tasks one at a time and hands each to the scheduler. It
// never waits for a task to finish, so a claim loop is never idle while a
// task runs.
func RunWorker(ctx context.Context, q Claimer, cfg WorkerConfig, c *Counters) error {
	sleep := cfg.Sleep
	if sleep == nil {
		sleep = sleepCtx
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		if !cfg.Slots.Acquire(ctx) {
			return nil
		}
		start := time.Now()
		var ids []int64
		for attempt := 1; ; attempt++ {
			var err error
			ids, err = q.Claim(ctx)
			if err == nil {
				break
			}
			if ctx.Err() != nil {
				cfg.Slots.Release()
				return nil
			}
			if !Retryable(err) {
				cfg.Slots.Release()
				return err
			}
			c.Retries.Add(1)
			if sleep(ctx, cfg.Backoff.delay(attempt)) != nil {
				cfg.Slots.Release()
				return nil
			}
		}
		if len(ids) == 0 {
			cfg.Slots.Release()
			c.EmptyClaims.Add(1)
			if sleep(ctx, cfg.EmptyPollInterval) != nil {
				return nil
			}
			continue
		}
		if cfg.Record != nil {
			cfg.Record(time.Since(start))
		}
		c.Claimed.Add(int64(len(ids)))
		c.InFlight.Add(int64(len(ids)))
		now := time.Now()
		for i, id := range ids {
			if i > 0 && !cfg.Slots.Acquire(ctx) {
				return nil
			}
			cfg.Sched.Add(id, now.Add(cfg.duration()))
		}
	}
}

type CompleterConfig struct {
	Slots  Slots
	Sched  *Scheduler
	Record func(time.Duration)
}

// RunCompleter writes DONE for one task whose duration elapsed, and then the
// next. A claim takes one task, so a completion writes one task.
func RunCompleter(ctx context.Context, q Completer, cfg CompleterConfig, c *Counters) error {
	ids := make([]int64, 1)
	for {
		select {
		case <-ctx.Done():
			return nil
		case id := <-cfg.Sched.Due():
			ids[0] = id
			start := time.Now()
			if err := q.Done(ctx, ids); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			if cfg.Record != nil {
				cfg.Record(time.Since(start))
			}
			c.Done.Add(1)
			c.InFlight.Add(-1)
			cfg.Slots.Release()
		}
	}
}

type ProducerConfig struct {
	BatchSize int
	Limiter   *rate.Limiter
}

// RunProducer inserts CREATED rows at whatever rate the limiter allows. The
// controller moves that limit to hold the queue length steady.
func RunProducer(ctx context.Context, e Creator, cfg ProducerConfig, c *Counters) error {
	for {
		// Wait for at most a second at a time. A slow queue needs an insert
		// rate far below one batch per second, and an unbounded wait would
		// hold the reservation made under an old limit long after the
		// controller moved it.
		waitCtx, cancel := context.WithTimeout(ctx, time.Second)
		err := cfg.Limiter.WaitN(waitCtx, cfg.BatchSize)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		if err := e.Create(ctx, cfg.BatchSize); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		c.Created.Add(int64(cfg.BatchSize))
	}
}

type ControllerConfig struct {
	Target   int64
	Gain     float64
	MinRate  float64
	MaxRate  float64
	Interval time.Duration
	Backlog  func() int64
}

// RunController holds the queue length near the target. It sets the insert
// rate to the observed completion rate, plus a correction for the error in
// queue length. Queue length stays steady and arrivals stay smooth.
//
// The completion rate is smoothed, because a raw count over one short tick is
// too noisy to drive the limit with.
func RunController(ctx context.Context, l *rate.Limiter, cfg ControllerConfig, c *Counters) {
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	lastDone := c.Done.Load()
	last := time.Now()
	// One tick of a two second average.
	alpha := cfg.Interval.Seconds() / 2
	smoothed := 0.0
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			done := c.Done.Load()
			elapsed := now.Sub(last).Seconds()
			if elapsed <= 0 {
				continue
			}
			doneRate := float64(done-lastDone) / elapsed
			lastDone, last = done, now
			smoothed += alpha * (doneRate - smoothed)

			target := smoothed + cfg.Gain*float64(cfg.Target-cfg.Backlog())
			// A zero limit stalls every producer until the next tick, which
			// makes the queue length oscillate instead of settle.
			if target < cfg.MinRate {
				target = cfg.MinRate
			}
			if target > cfg.MaxRate {
				target = cfg.MaxRate
			}
			l.SetLimit(rate.Limit(target))
		}
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

// RunEnqueuer inserts one row per statement, as fast as the server allows.
// The operations benchmark measures enqueue and claim as bare operations, so
// nothing paces this loop.
func RunEnqueuer(ctx context.Context, e Creator, c *Counters) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := e.Create(ctx, 1); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		c.Created.Add(1)
	}
}

// RunDequeuer claims one row per statement and then forgets it. No task runs
// and nothing writes DONE, so a claim is the whole operation.
func RunDequeuer(ctx context.Context, q Claimer, cfg WorkerConfig, c *Counters) error {
	sleep := cfg.Sleep
	if sleep == nil {
		sleep = sleepCtx
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		start := time.Now()
		var ids []int64
		for attempt := 1; ; attempt++ {
			var err error
			ids, err = q.Claim(ctx)
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
			c.EmptyClaims.Add(1)
			if sleep(ctx, cfg.EmptyPollInterval) != nil {
				return nil
			}
			continue
		}
		if cfg.Record != nil {
			cfg.Record(time.Since(start))
		}
		c.Claimed.Add(int64(len(ids)))
	}
}
