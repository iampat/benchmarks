package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/time/rate"

	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/benchreport"
	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/pgcontainer"
	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/queue"
)

const queueName = "bench"

type config struct {
	mode           string
	stages         []queue.Stage
	workers        []int
	completers     int
	enqueuers      int
	queueSample    time.Duration
	slots          int
	shards         int
	batch          int
	createBatch    int
	durationMin    time.Duration
	durationMax    time.Duration
	producers      int
	targetBacklog  int64
	prefill        int
	warmup         time.Duration
	window         time.Duration
	repeat         int
	dsn            string
	image          string
	port           int
	keep           bool
	resultsDir     string
	maxConnections int
}

func main() {
	if err := run(); err != nil {
		slog.Error("bench failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	env := benchreport.Environment{
		Argv:      os.Args,
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
		NumCPU:    runtime.NumCPU(),
		GitCommit: gitCommit(),
	}

	dsn := cfg.dsn
	if dsn == "" {
		ctr, err := pgcontainer.Start(ctx, pgcontainer.Config{
			Image:          cfg.image,
			Name:           fmt.Sprintf("scaling-queue-postgres-bench-%d", cfg.port),
			Port:           cfg.port,
			Password:       "bench",
			MaxConnections: cfg.maxConnections,
		})
		if err != nil {
			return err
		}
		if cfg.keep {
			slog.Info("keeping container", "name", ctr.Config.Name, "dsn", ctr.Config.DSN())
		} else {
			defer func() {
				stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := ctr.Stop(stopCtx); err != nil {
					slog.Warn("stop container", "err", err)
				}
			}()
		}
		dsn = ctr.Config.DSN()
		env.Image = cfg.image
		env.VMCPUs = pgcontainer.MachineCPUs(ctx)
		if digest, err := ctr.ImageDigest(ctx); err == nil {
			env.ImageDigest = digest
		}
		if err := pgcontainer.WaitReady(ctx, dsn, 60*time.Second); err != nil {
			return err
		}
	}

	ctl, err := queue.Open(ctx, dsn, 8, nil)
	if err != nil {
		return err
	}
	defer ctl.Close()

	info, err := ctl.ServerInfo(ctx, "max_connections", "shared_buffers",
		"synchronous_commit", "fsync", "autovacuum", "autovacuum_naptime")
	if err != nil {
		return err
	}
	env.ServerVersion = info["version"]
	delete(info, "version")
	env.ServerSettings = info

	maxConns, err := strconv.Atoi(info["max_connections"])
	if err != nil {
		return fmt.Errorf("parse max_connections: %w", err)
	}
	maxWorkers := 0
	for _, w := range cfg.workers {
		maxWorkers = max(maxWorkers, w)
	}
	if need := maxWorkers + cfg.completers + cfg.producers + cfg.enqueuers + 16; need > maxConns {
		return fmt.Errorf("need %d connections but server max_connections is %d", need, maxConns)
	}

	for _, st := range cfg.stages {
		for _, w := range cfg.workers {
			for rep := 0; rep < cfg.repeat; rep++ {
				res, err := runCell(ctx, ctl, dsn, st, w, cfg, env)
				if err != nil {
					return fmt.Errorf("stage %s workers %d: %w", st.Name, w, err)
				}
				path, err := benchreport.WriteResult(cfg.resultsDir, res)
				if err != nil {
					return err
				}
				slog.Info("cell done",
					"mode", cfg.mode, "stage", st.Name, "workers", w, "repeat", rep+1,
					"tasks_per_sec", fmt.Sprintf("%.0f", res.ThroughputPerSec),
					"claim_p95_ms", fmt.Sprintf("%.2f", res.P95Millis),
					"in_flight", res.MeanInFlight, "little_err_pct",
					fmt.Sprintf("%.1f", res.LittleErrorPercent),
					"valid", res.Valid, "result", path)
			}
		}
	}
	return nil
}

func runCell(ctx context.Context, ctl *queue.Store, dsn string, st queue.Stage,
	workers int, cfg config, env benchreport.Environment,
) (benchreport.Result, error) {
	shards := 1
	if st.Sharded {
		shards = cfg.shards
		if shards == 0 || shards > workers {
			shards = workers
		}
	}
	res := benchreport.Result{
		Benchmark:       "scaling-queue-postgres",
		Mode:            cfg.mode,
		Stage:           st.Name,
		Workers:         workers,
		Completers:      cfg.completers,
		Producers:       cfg.producers,
		Slots:           cfg.slots,
		Shards:          shards,
		BatchSize:       cfg.batch,
		CreateBatch:     cfg.createBatch,
		DurationMinSecs: cfg.durationMin.Seconds(),
		DurationMaxSecs: cfg.durationMax.Seconds(),
		WarmupSeconds:   cfg.warmup.Seconds(),
		WindowSeconds:   cfg.window.Seconds(),
		CompletionBatch: st.CompletionBatch,
		StartedAt:       time.Now().UTC(),
		Env:             env,
	}

	if err := ctl.SetupStage(ctx, st); err != nil {
		return res, err
	}

	prefill := cfg.prefill
	if cfg.mode == "steady" {
		prefill = int(cfg.targetBacklog)
	}
	if err := fill(ctx, ctl, prefill, cfg.createBatch, shards); err != nil {
		return res, err
	}
	if err := ctl.Checkpoint(ctx); err != nil {
		return res, err
	}
	res.BacklogStart = int64(prefill)

	var params map[string]string
	if st.SyncCommit != "" {
		params = map[string]string{"synchronous_commit": st.SyncCommit}
	}
	wstore, err := queue.Open(ctx, dsn, int32(workers), params)
	if err != nil {
		return res, err
	}
	defer wstore.Close()
	cstore, err := queue.Open(ctx, dsn, int32(cfg.completers), params)
	if err != nil {
		return res, err
	}
	defer cstore.Close()
	pstore, err := queue.Open(ctx, dsn, int32(cfg.producers), params)
	if err != nil {
		return res, err
	}
	defer pstore.Close()

	if cfg.mode == "ops" {
		return runOpsCell(ctx, ctl, res, wstore, pstore, st, workers, shards, cfg)
	}

	var counters queue.Counters
	claimRec := benchreport.NewRecorder(workers)
	doneRec := benchreport.NewRecorder(cfg.completers)
	slots := queue.NewSlots(cfg.slots)
	sched := queue.NewScheduler(cfg.completers * 64)

	cellCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, workers+cfg.completers+cfg.producers)
	var wg sync.WaitGroup

	go sched.Run(cellCtx)

	for i := 0; i < workers; i++ {
		lane := claimRec.Lane(i)
		wq := queue.StageQueue{
			Store: wstore, Stage: st, Queue: queueName, Batch: cfg.batch,
			Shard: i % shards, Shards: shards,
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- queue.RunWorker(cellCtx, wq, queue.WorkerConfig{
				Backoff:           queue.BackoffConfig{Base: time.Millisecond, Max: 100 * time.Millisecond},
				EmptyPollInterval: 10 * time.Millisecond,
				MinDuration:       cfg.durationMin,
				MaxDuration:       cfg.durationMax,
				Slots:             slots,
				Sched:             sched,
				Record:            lane.Record,
			}, &counters)
		}()
	}

	for i := 0; i < cfg.completers; i++ {
		lane := doneRec.Lane(i)
		cq := queue.StageQueue{Store: cstore, Stage: st, Queue: queueName}
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- queue.RunCompleter(cellCtx, cq, queue.CompleterConfig{
				Batch:   st.CompletionBatch,
				MaxWait: 5 * time.Millisecond,
				Slots:   slots,
				Sched:   sched,
				Record:  lane.Record,
			}, &counters)
		}()
	}

	if cfg.mode == "steady" {
		limiter := rate.NewLimiter(rate.Limit(2_000_000), cfg.createBatch*2)
		backlog := func() int64 {
			return int64(prefill) + counters.Created.Load() - counters.Claimed.Load()
		}
		pq := queue.StageQueue{Store: pstore, Stage: st, Queue: queueName, Shards: shards}
		for i := 0; i < cfg.producers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs <- queue.RunProducer(cellCtx, pq, queue.ProducerConfig{
					BatchSize: cfg.createBatch,
					Limiter:   limiter,
				}, &counters)
			}()
		}
		go queue.RunController(cellCtx, limiter, queue.ControllerConfig{
			Target:   cfg.targetBacklog,
			Gain:     0.2,
			MinRate:  1,
			MaxRate:  2_000_000,
			Interval: 250 * time.Millisecond,
			Backlog:  backlog,
		}, &counters)
	}

	waitErr := wait(cellCtx, cfg.warmup)

	doneStart := counters.Done.Load()
	retriesStart := counters.Retries.Load()
	emptyStart := counters.EmptyClaims.Load()
	windowStart := time.Now()
	claimRec.SetRecording(true)
	doneRec.SetRecording(true)

	sampler := newSampler(&counters, func() int64 {
		return int64(prefill) + counters.Created.Load() - counters.Claimed.Load()
	})
	sampleCtx, stopSampler := context.WithCancel(cellCtx)
	go sampler.run(sampleCtx, 200*time.Millisecond)

	if waitErr == nil {
		waitErr = wait(cellCtx, cfg.window)
	}
	elapsed := time.Since(windowStart)
	stopSampler()
	claimRec.SetRecording(false)
	doneRec.SetRecording(false)

	res.CompletedTasks = counters.Done.Load() - doneStart
	res.Retries = counters.Retries.Load() - retriesStart
	res.EmptyClaims = counters.EmptyClaims.Load() - emptyStart
	res.CreatedTasks = counters.Created.Load()
	cancel()

	drained := make(chan struct{})
	go func() { wg.Wait(); close(drained) }()
	drainTimer := time.NewTimer(60 * time.Second)
	defer drainTimer.Stop()
	select {
	case <-drained:
	case <-drainTimer.C:
		return res, errors.New("workers did not stop within 60s")
	}
	if waitErr != nil {
		return res, waitErr
	}
	close(errs)
	for e := range errs {
		if e != nil {
			return res, e
		}
	}

	res.ThroughputPerSec = float64(res.CompletedTasks) / elapsed.Seconds()
	claim := benchreport.Percentiles(claimRec.Samples())
	res.SampleCount = claim.Count
	res.P50Millis = float64(claim.P50) / float64(time.Millisecond)
	res.P95Millis = float64(claim.P95) / float64(time.Millisecond)
	res.P99Millis = float64(claim.P99) / float64(time.Millisecond)
	done := benchreport.Percentiles(doneRec.Samples())
	res.DoneP95Millis = float64(done.P95) / float64(time.Millisecond)

	res.MeanInFlight = sampler.meanInFlight()
	res.MeanBacklog = sampler.meanBacklog()
	res.BacklogVariationPercent = sampler.backlogVariationPercent()

	end, err := ctl.Backlog(ctx, queueName)
	if err != nil {
		return res, err
	}
	res.BacklogEnd = end

	// Little's Law: tasks in flight = throughput x mean task duration. A run
	// that reached a steady state satisfies it. A run that did not is still
	// filling or draining its pipeline, whatever its throughput says.
	meanDuration := (cfg.durationMin.Seconds() + cfg.durationMax.Seconds()) / 2
	res.ExpectedInFlight = res.ThroughputPerSec * meanDuration
	if res.ExpectedInFlight > 0 {
		res.LittleErrorPercent = math.Abs(res.MeanInFlight-res.ExpectedInFlight) / res.ExpectedInFlight * 100
	}

	res.Valid = true
	invalid := func(reason string) {
		res.Valid = false
		res.InvalidReasons = append(res.InvalidReasons, reason)
	}
	if res.CompletedTasks < 1000 {
		invalid(fmt.Sprintf("only %d completions in the window, too few to measure",
			res.CompletedTasks))
	}
	if claims := res.CompletedTasks; claims > 0 && res.EmptyClaims*20 > claims {
		invalid("empty claims exceeded 5% of claim attempts")
	}
	if res.BacklogEnd < int64(workers*cfg.batch) {
		invalid("queue drained before the window ended")
	}
	// Counting noise on N tasks in flight falls off as 1/sqrt(N), so a cell
	// holding 50 tasks cannot be held to the same tolerance as one holding
	// 300,000. The floor stays at 10 percent, which binds above about 1,600.
	tolerance := 10.0
	if res.ExpectedInFlight > 0 {
		tolerance = math.Max(tolerance, 400/math.Sqrt(res.ExpectedInFlight))
	}
	res.LittleTolerancePercent = tolerance
	if res.LittleErrorPercent > tolerance {
		invalid(fmt.Sprintf("tasks in flight missed Little's Law by %.0f%%, tolerance %.0f%%",
			res.LittleErrorPercent, tolerance))
	}
	if sampler.peakInFlight() > float64(cfg.slots)*0.95 {
		invalid("task slots ran out, so slots capped throughput")
	}
	if cfg.mode == "steady" && res.BacklogVariationPercent > 20 {
		invalid(fmt.Sprintf("queue length varied by %.0f%%, so it was not steady",
			res.BacklogVariationPercent))
	}
	return res, nil
}

// runOpsCell measures enqueue and claim as bare operations. No task runs, so
// nothing writes DONE and nothing sits in flight. The queue length is the
// second result, because it shows which side of the queue is faster.
func runOpsCell(ctx context.Context, ctl *queue.Store, res benchreport.Result,
	wstore, pstore *queue.Store, st queue.Stage, workers, shards int, cfg config,
) (benchreport.Result, error) {
	var counters queue.Counters
	claimRec := benchreport.NewRecorder(workers)

	cellCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, workers+cfg.enqueuers)
	var wg sync.WaitGroup

	for i := 0; i < cfg.enqueuers; i++ {
		eq := queue.StageQueue{Store: pstore, Stage: st, Queue: queueName, Shards: shards}
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- queue.RunEnqueuer(cellCtx, eq, &counters)
		}()
	}
	for i := 0; i < workers; i++ {
		lane := claimRec.Lane(i)
		dq := queue.StageQueue{
			Store: wstore, Stage: st, Queue: queueName, Batch: 1,
			Shard: i % shards, Shards: shards,
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- queue.RunDequeuer(cellCtx, dq, queue.WorkerConfig{
				Backoff:           queue.BackoffConfig{Base: time.Millisecond, Max: 100 * time.Millisecond},
				EmptyPollInterval: time.Millisecond,
				Record:            lane.Record,
			}, &counters)
		}()
	}

	waitErr := wait(cellCtx, cfg.warmup)
	createdStart := counters.Created.Load()
	claimedStart := counters.Claimed.Load()
	emptyStart := counters.EmptyClaims.Load()
	retriesStart := counters.Retries.Load()
	windowStart := time.Now()
	claimRec.SetRecording(true)

	depth := func() int64 {
		return int64(res.BacklogStart) + counters.Created.Load() - counters.Claimed.Load()
	}
	s := newSampler(&counters, depth)
	sampleCtx, stopSampler := context.WithCancel(cellCtx)
	go s.run(sampleCtx, cfg.queueSample)

	if waitErr == nil {
		waitErr = wait(cellCtx, cfg.window)
	}
	elapsed := time.Since(windowStart)
	stopSampler()
	claimRec.SetRecording(false)

	res.EnqueueOps = counters.Created.Load() - createdStart
	res.DequeueOps = counters.Claimed.Load() - claimedStart
	res.EmptyClaims = counters.EmptyClaims.Load() - emptyStart
	res.Retries = counters.Retries.Load() - retriesStart
	cancel()

	drained := make(chan struct{})
	go func() { wg.Wait(); close(drained) }()
	timer := time.NewTimer(60 * time.Second)
	defer timer.Stop()
	select {
	case <-drained:
	case <-timer.C:
		return res, errors.New("loops did not stop within 60s")
	}
	if waitErr != nil {
		return res, waitErr
	}
	close(errs)
	for e := range errs {
		if e != nil {
			return res, e
		}
	}

	res.CompletedTasks = res.DequeueOps
	res.ThroughputPerSec = float64(res.DequeueOps) / elapsed.Seconds()
	res.EnqueuePerSec = float64(res.EnqueueOps) / elapsed.Seconds()
	res.OpsPerSec = res.ThroughputPerSec + res.EnqueuePerSec
	claim := benchreport.Percentiles(claimRec.Samples())
	res.SampleCount = claim.Count
	res.P50Millis = float64(claim.P50) / float64(time.Millisecond)
	res.P95Millis = float64(claim.P95) / float64(time.Millisecond)
	res.P99Millis = float64(claim.P99) / float64(time.Millisecond)
	res.MeanBacklog = s.meanBacklog()
	res.BacklogVariationPercent = s.backlogVariationPercent()

	end, err := ctl.Backlog(ctx, queueName)
	if err != nil {
		return res, err
	}
	res.BacklogEnd = end

	res.Valid = true
	if res.DequeueOps < 1000 || res.EnqueueOps < 1000 {
		res.Valid = false
		res.InvalidReasons = append(res.InvalidReasons,
			fmt.Sprintf("only %d enqueues and %d dequeues, too few to measure",
				res.EnqueueOps, res.DequeueOps))
	}
	return res, nil
}

type sampler struct {
	counters *queue.Counters
	backlog  func() int64
	mu       sync.Mutex
	inFlight []float64
	depth    []float64
	peak     float64
}

func (s *sampler) peakInFlight() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peak
}

func newSampler(c *queue.Counters, backlog func() int64) *sampler {
	return &sampler{counters: c, backlog: backlog}
}

func (s *sampler) run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f := float64(s.counters.InFlight.Load())
			s.mu.Lock()
			s.inFlight = append(s.inFlight, f)
			s.depth = append(s.depth, float64(s.backlog()))
			s.peak = math.Max(s.peak, f)
			s.mu.Unlock()
		}
	}
}

func (s *sampler) meanInFlight() float64 { return mean(s.snapshot(true)) }
func (s *sampler) meanBacklog() float64  { return mean(s.snapshot(false)) }

func (s *sampler) backlogVariationPercent() float64 {
	d := s.snapshot(false)
	m := mean(d)
	if m == 0 || len(d) < 2 {
		return 0
	}
	var sum float64
	for _, v := range d {
		sum += (v - m) * (v - m)
	}
	return math.Sqrt(sum/float64(len(d))) / m * 100
}

func (s *sampler) snapshot(inFlight bool) []float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if inFlight {
		return append([]float64(nil), s.inFlight...)
	}
	return append([]float64(nil), s.depth...)
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	var sum float64
	for _, x := range v {
		sum += x
	}
	return sum / float64(len(v))
}

func fill(ctx context.Context, s *queue.Store, total, batch, shards int) error {
	if total <= 0 {
		return nil
	}
	const parallel = 8
	batches := (total + batch - 1) / batch
	work := make(chan int, batches)
	for i := 0; i < batches; i++ {
		n := batch
		if rem := total - i*batch; rem < batch {
			n = rem
		}
		work <- n
	}
	close(work)

	errs := make(chan error, parallel)
	var wg sync.WaitGroup
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range work {
				if err := ctx.Err(); err != nil {
					errs <- err
					return
				}
				if err := s.Create(ctx, queueName, n, shards); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func wait(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func parseFlags(args []string) (config, error) {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	stages := fs.String("stages", "all", "comma-separated stage names, or all")
	workers := fs.String("workers", "16,32", "comma-separated claim-loop counts")
	cfg := config{}
	fs.StringVar(&cfg.mode, "mode", "steady", "drain or steady")
	fs.IntVar(&cfg.completers, "completers", 128, "goroutines that write DONE")
	fs.IntVar(&cfg.enqueuers, "enqueuers", 16, "enqueue loops, ops mode only")
	fs.DurationVar(&cfg.queueSample, "queue-sample", 10*time.Second, "queue length sampling interval, ops mode")
	fs.IntVar(&cfg.slots, "slots", 3_000_000, "maximum tasks in flight")
	fs.IntVar(&cfg.shards, "shards", 0, "shards for the sharded stages, 0 means one per claim loop")
	fs.IntVar(&cfg.batch, "batch", 1, "tasks per claim")
	fs.IntVar(&cfg.createBatch, "create-batch", 1000, "rows per insert")
	fs.DurationVar(&cfg.durationMin, "duration-min", 10*time.Second, "shortest task duration")
	fs.DurationVar(&cfg.durationMax, "duration-max", 20*time.Second, "longest task duration")
	fs.IntVar(&cfg.producers, "producers", 4, "producer goroutines, steady mode only")
	fs.Int64Var(&cfg.targetBacklog, "target-backlog", 1_000_000, "queue length the controller holds")
	fs.IntVar(&cfg.prefill, "prefill", 2_000_000, "rows inserted before a drain run")
	fs.DurationVar(&cfg.warmup, "warmup", 60*time.Second, "warm-up before measuring")
	fs.DurationVar(&cfg.window, "window", 5*time.Minute, "measurement window")
	fs.IntVar(&cfg.repeat, "repeat", 1, "repeats per stage and worker count")
	fs.StringVar(&cfg.dsn, "dsn", "", "use this Postgres instead of a managed container")
	fs.StringVar(&cfg.image, "image", "docker.io/library/postgres:18", "container image")
	fs.IntVar(&cfg.port, "port", 55432, "host port for the managed container")
	fs.BoolVar(&cfg.keep, "keep-container", false, "do not stop the managed container")
	fs.StringVar(&cfg.resultsDir, "results-dir", "scaling-queue-postgres/results", "where result JSON files land")
	fs.IntVar(&cfg.maxConnections, "max-connections", 400, "max_connections for the managed container")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}

	if cfg.mode != "drain" && cfg.mode != "steady" && cfg.mode != "ops" {
		return cfg, fmt.Errorf("mode must be drain, steady, or ops, got %q", cfg.mode)
	}
	if *stages == "all" {
		cfg.stages = queue.Stages()
	} else {
		for _, name := range strings.Split(*stages, ",") {
			st, ok := queue.StageByName(strings.TrimSpace(name))
			if !ok {
				return cfg, fmt.Errorf("unknown stage %q, valid: %s",
					name, strings.Join(queue.StageNames(), ", "))
			}
			cfg.stages = append(cfg.stages, st)
		}
	}
	for _, s := range strings.Split(*workers, ",") {
		w, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || w < 1 {
			return cfg, fmt.Errorf("bad worker count %q", s)
		}
		cfg.workers = append(cfg.workers, w)
	}
	// Tasks in flight need one full task duration to reach a steady state.
	if cfg.mode != "ops" && cfg.warmup <= cfg.durationMax {
		return cfg, fmt.Errorf("warmup %v must exceed the longest task duration %v",
			cfg.warmup, cfg.durationMax)
	}
	if cfg.shards < 0 {
		return cfg, fmt.Errorf("shards cannot be negative, got %d", cfg.shards)
	}
	for _, w := range cfg.workers {
		if cfg.shards > w {
			return cfg, fmt.Errorf(
				"%d shards with %d claim loops would leave shards with no worker", cfg.shards, w,
			)
		}
	}
	if ws := os.Getenv("BUILD_WORKSPACE_DIRECTORY"); ws != "" && !filepath.IsAbs(cfg.resultsDir) {
		cfg.resultsDir = filepath.Join(ws, cfg.resultsDir)
	}
	return cfg, nil
}

func gitCommit() string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	if ws := os.Getenv("BUILD_WORKSPACE_DIRECTORY"); ws != "" {
		cmd.Dir = ws
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
