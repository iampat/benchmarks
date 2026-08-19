package queue_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/queue"
)

type dequeueStep struct {
	ids []int64
	err error
}

type fakeQueue struct {
	dequeues  chan dequeueStep
	completes chan []int64
}

func newFakeQueue() *fakeQueue {
	return &fakeQueue{
		dequeues:  make(chan dequeueStep),
		completes: make(chan []int64),
	}
}

func (f *fakeQueue) DequeueBatch(ctx context.Context) ([]int64, error) {
	select {
	case s := <-f.dequeues:
		return s.ids, s.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *fakeQueue) Complete(ctx context.Context, ids []int64) error {
	select {
	case f.completes <- ids:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func instantSleep(sleeps chan<- time.Duration) func(context.Context, time.Duration) error {
	return func(ctx context.Context, d time.Duration) error {
		select {
		case sleeps <- d:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func startWorker(ctx context.Context, q queue.Dequeuer, cfg queue.WorkerConfig, c *queue.Counters) chan error {
	done := make(chan error, 1)
	go func() { done <- queue.RunWorker(ctx, q, cfg, c) }()
	return done
}

func TestWorkerCompletesBatch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	f := newFakeQueue()
	var c queue.Counters
	var samples atomic.Int64
	cfg := queue.WorkerConfig{
		Backoff: queue.BackoffConfig{Base: time.Millisecond, Max: time.Second},
		Record:  func(time.Duration) { samples.Add(1) },
	}
	done := startWorker(ctx, f, cfg, &c)

	f.dequeues <- dequeueStep{ids: []int64{1, 2, 3}}
	got := <-f.completes
	if len(got) != 3 {
		t.Errorf("Complete got %d ids, want 3", len(got))
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("RunWorker = %v, want nil", err)
	}
	if c.Completed.Load() != 3 {
		t.Errorf("Completed = %d, want 3", c.Completed.Load())
	}
	if samples.Load() != 1 {
		t.Errorf("recorded %d samples, want 1", samples.Load())
	}
}

func TestWorkerRetriesSerializationFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	f := newFakeQueue()
	var c queue.Counters
	sleeps := make(chan time.Duration, 10)
	cfg := queue.WorkerConfig{
		Backoff: queue.BackoffConfig{Base: time.Millisecond, Max: 50 * time.Millisecond},
		Sleep:   instantSleep(sleeps),
	}
	done := startWorker(ctx, f, cfg, &c)

	f.dequeues <- dequeueStep{err: &pgconn.PgError{Code: "40001"}}
	d := <-sleeps
	if d <= 0 || d > 50*time.Millisecond {
		t.Errorf("backoff delay = %v, want in (0, 50ms]", d)
	}
	f.dequeues <- dequeueStep{ids: []int64{7}}
	<-f.completes

	cancel()
	<-done
	if c.Retries.Load() != 1 {
		t.Errorf("Retries = %d, want 1", c.Retries.Load())
	}
	if c.Completed.Load() != 1 {
		t.Errorf("Completed = %d, want 1", c.Completed.Load())
	}
}

func TestWorkerHoldDefersCompletion(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	f := newFakeQueue()
	var c queue.Counters
	sleeps := make(chan time.Duration, 10)
	cfg := queue.WorkerConfig{
		Hold:  5 * time.Second,
		Sleep: instantSleep(sleeps),
	}
	done := startWorker(ctx, f, cfg, &c)

	f.dequeues <- dequeueStep{ids: []int64{1, 2}}
	if d := <-sleeps; d != 5*time.Second {
		t.Errorf("hold sleep = %v, want 5s", d)
	}
	if got := <-f.completes; len(got) != 2 {
		t.Errorf("Complete got %d ids, want 2", len(got))
	}
	// The worker kept dequeuing while the hold ran.
	f.dequeues <- dequeueStep{ids: []int64{3}}
	<-sleeps
	<-f.completes

	cancel()
	<-done
	if c.Dequeued.Load() != 3 {
		t.Errorf("Dequeued = %d, want 3", c.Dequeued.Load())
	}
	if c.Completed.Load() != 3 {
		t.Errorf("Completed = %d, want 3", c.Completed.Load())
	}
}

func TestWorkerStopsOnNonRetryableError(t *testing.T) {
	f := newFakeQueue()
	var c queue.Counters
	boom := errors.New("boom")
	done := startWorker(t.Context(), f, queue.WorkerConfig{}, &c)

	f.dequeues <- dequeueStep{err: boom}
	if err := <-done; !errors.Is(err, boom) {
		t.Errorf("RunWorker = %v, want %v", err, boom)
	}
}

func TestWorkerCountsEmptyDequeues(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	f := newFakeQueue()
	var c queue.Counters
	sleeps := make(chan time.Duration, 10)
	cfg := queue.WorkerConfig{
		EmptyPollInterval: 25 * time.Millisecond,
		Sleep:             instantSleep(sleeps),
		Record:            func(time.Duration) { t.Error("recorded a sample for an empty dequeue") },
	}
	done := startWorker(ctx, f, cfg, &c)

	f.dequeues <- dequeueStep{}
	if d := <-sleeps; d != 25*time.Millisecond {
		t.Errorf("empty-poll sleep = %v, want 25ms", d)
	}
	cancel()
	<-done
	if c.EmptyDequeues.Load() != 1 {
		t.Errorf("EmptyDequeues = %d, want 1", c.EmptyDequeues.Load())
	}
}

type fakeEnqueuer struct {
	batches chan int
	err     error
}

func (f *fakeEnqueuer) Enqueue(ctx context.Context, n int) error {
	if f.err != nil {
		return f.err
	}
	select {
	case f.batches <- n:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestProducerPausesAboveHighWater(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	f := &fakeEnqueuer{batches: make(chan int)}
	var c queue.Counters
	var backlog atomic.Int64
	backlog.Store(100)
	polls := make(chan time.Duration, 1)
	cfg := queue.ProducerConfig{
		BatchSize:    25,
		CheckEvery:   1,
		HighWater:    50,
		LowWater:     10,
		Backlog:      backlog.Load,
		PollInterval: 5 * time.Millisecond,
		Sleep: func(ctx context.Context, d time.Duration) error {
			backlog.Store(5)
			select {
			case polls <- d:
			default:
			}
			return nil
		},
	}
	done := make(chan error, 1)
	go func() { done <- queue.RunProducer(ctx, f, cfg, &c) }()

	<-polls
	if n := <-f.batches; n != 25 {
		t.Errorf("enqueued batch of %d, want 25", n)
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("RunProducer = %v, want nil", err)
	}
	if c.Enqueued.Load() != 25 {
		t.Errorf("Enqueued = %d, want 25", c.Enqueued.Load())
	}
}

func TestProducerReturnsEnqueueError(t *testing.T) {
	boom := errors.New("boom")
	f := &fakeEnqueuer{err: boom}
	var c queue.Counters
	cfg := queue.ProducerConfig{
		BatchSize:  1,
		CheckEvery: 1,
		HighWater:  10,
		Backlog:    func() int64 { return 0 },
	}
	if err := queue.RunProducer(t.Context(), f, cfg, &c); !errors.Is(err, boom) {
		t.Errorf("RunProducer = %v, want %v", err, boom)
	}
}
