package queue_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/time/rate"

	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/queue"
)

type claimStep struct {
	ids []int64
	err error
}

type fakeQueue struct {
	claims chan claimStep
	dones  chan []int64
}

func newFakeQueue() *fakeQueue {
	return &fakeQueue{claims: make(chan claimStep), dones: make(chan []int64, 64)}
}

func (f *fakeQueue) Claim(ctx context.Context) ([]int64, error) {
	select {
	case s := <-f.claims:
		return s.ids, s.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *fakeQueue) Done(ctx context.Context, ids []int64) error {
	select {
	case f.dones <- append([]int64(nil), ids...):
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

func newWorkerConfig(slots queue.Slots, sched *queue.Scheduler) queue.WorkerConfig {
	return queue.WorkerConfig{
		Backoff:     queue.BackoffConfig{Base: time.Millisecond, Max: 50 * time.Millisecond},
		MinDuration: time.Millisecond,
		MaxDuration: 2 * time.Millisecond,
		Slots:       slots,
		Sched:       sched,
	}
}

// A claimed task travels through the scheduler and reaches a completer, which
// writes DONE and gives the slot back.
func TestWorkerAndCompleterMoveATask(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f := newFakeQueue()
	var c queue.Counters
	slots := queue.NewSlots(4)
	sched := queue.NewScheduler(16)
	go sched.Run(ctx)

	go queue.RunWorker(ctx, f, newWorkerConfig(slots, sched), &c)
	go queue.RunCompleter(ctx, f, queue.CompleterConfig{Slots: slots, Sched: sched}, &c)

	f.claims <- claimStep{ids: []int64{7}}
	select {
	case ids := <-f.dones:
		if len(ids) != 1 || ids[0] != 7 {
			t.Errorf("Done got %v, want [7]", ids)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("task never reached the completer")
	}

	deadline := time.Now().Add(5 * time.Second)
	for c.Done.Load() != 1 && time.Now().Before(deadline) {
		select {
		case f.claims <- claimStep{}:
		default:
		}
	}
	if c.Claimed.Load() != 1 || c.Done.Load() != 1 {
		t.Errorf("claimed=%d done=%d, want 1 and 1", c.Claimed.Load(), c.Done.Load())
	}
	if c.InFlight.Load() != 0 {
		t.Errorf("in flight = %d after completion, want 0", c.InFlight.Load())
	}
}

func TestWorkerRetriesSerializationFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f := newFakeQueue()
	var c queue.Counters
	sleeps := make(chan time.Duration, 10)
	sched := queue.NewScheduler(16)
	cfg := newWorkerConfig(queue.NewSlots(4), sched)
	cfg.Sleep = instantSleep(sleeps)
	go queue.RunWorker(ctx, f, cfg, &c)

	f.claims <- claimStep{err: &pgconn.PgError{Code: "40001"}}
	if d := <-sleeps; d <= 0 || d > 50*time.Millisecond {
		t.Errorf("backoff delay = %v, want in (0, 50ms]", d)
	}
	f.claims <- claimStep{ids: []int64{1}}

	deadline := time.Now().Add(5 * time.Second)
	for c.Claimed.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if c.Retries.Load() != 1 {
		t.Errorf("retries = %d, want 1", c.Retries.Load())
	}
}

func TestWorkerStopsOnNonRetryableError(t *testing.T) {
	f := newFakeQueue()
	var c queue.Counters
	boom := errors.New("boom")
	sched := queue.NewScheduler(4)
	done := make(chan error, 1)
	go func() {
		done <- queue.RunWorker(t.Context(), f, newWorkerConfig(queue.NewSlots(2), sched), &c)
	}()

	f.claims <- claimStep{err: boom}
	if err := <-done; !errors.Is(err, boom) {
		t.Errorf("RunWorker = %v, want %v", err, boom)
	}
}

// An empty claim gives the slot back. Without that, a queue that runs dry
// would leak every slot and stall the run.
func TestEmptyClaimReleasesItsSlot(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f := newFakeQueue()
	var c queue.Counters
	slots := queue.NewSlots(1)
	sleeps := make(chan time.Duration, 4)
	sched := queue.NewScheduler(4)
	cfg := newWorkerConfig(slots, sched)
	cfg.EmptyPollInterval = time.Millisecond
	cfg.Sleep = instantSleep(sleeps)
	go queue.RunWorker(ctx, f, cfg, &c)

	for i := 0; i < 3; i++ {
		f.claims <- claimStep{}
		<-sleeps
	}
	if c.EmptyClaims.Load() != 3 {
		t.Errorf("empty claims = %d, want 3", c.EmptyClaims.Load())
	}
}

func TestSchedulerHoldsUntilDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sched := queue.NewScheduler(4)
	go sched.Run(ctx)

	sched.Add(1, time.Now().Add(300*time.Millisecond))
	if d := sched.Depth(); d != 1 {
		t.Errorf("depth = %d, want 1", d)
	}
	start := time.Now()
	select {
	case <-sched.Due():
		if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
			t.Errorf("task came due after %v, want at least 250ms", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("task never came due")
	}
}

// A limiter that refuses a batch is a pause. The producer that treats the
// refusal as a stop leaves the queue to starve for the rest of the run.
func TestProducerSurvivesARefusedBatch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	created := make(chan int, 4)
	var c queue.Counters
	// A burst below the batch size makes every WaitN return an error.
	limiter := rate.NewLimiter(rate.Limit(100000), 10)
	go queue.RunProducer(ctx, creatorFunc(func(ctx context.Context, n int) error {
		select {
		case created <- n:
		case <-ctx.Done():
		}
		return nil
	}), queue.ProducerConfig{BatchSize: 1000, Limiter: limiter}, &c)

	select {
	case <-created:
		t.Fatal("producer inserted a batch the limiter refused")
	case <-time.After(100 * time.Millisecond):
	}

	limiter.SetBurst(2000)
	select {
	case n := <-created:
		if n != 1000 {
			t.Errorf("created batch of %d, want 1000", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("producer never resumed once the limiter allowed a batch")
	}
}

type creatorFunc func(context.Context, int) error

func (f creatorFunc) Create(ctx context.Context, n int) error { return f(ctx, n) }
