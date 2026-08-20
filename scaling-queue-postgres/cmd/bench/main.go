package main

import (
	"context"
	"errors"
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

	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/benchcfg"
	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/benchreport"
	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/experiment"
	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/pgcontainer"
	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/queue"
)

const queueName = "bench"

type config struct {
	Mode           string
	Stages         []queue.Stage
	Workers        []int
	Completers     int
	Enqueuers      int
	OpsQueueTarget int
	QueueSample    time.Duration
	Slots          int
	Shards         int
	Batch          int
	CreateBatch    int
	DurationMin    time.Duration
	DurationMax    time.Duration
	Producers      int
	TargetBacklog  int64
	Prefill        int
	Warmup         time.Duration
	Window         time.Duration
	Repeat         int
	DSN            string
	Image          string
	Port           int
	Keep           bool
	ResultsDir     string
	MaxConnections int
}

func main() {
	if err := run(); err != nil {
		slog.Error("bench failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := benchcfg.Parse(os.Args[1:])
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// A cell must not claim a machine it did not run on. Check before anything
	// starts, so a failed reconfiguration costs a second and not six minutes.
	var step experiment.Step
	if cfg.Step >= 0 {
		step, _ = experiment.ByN(cfg.Step)
		managed := cfg.DSN == ""
		observed := 0
		if managed {
			observed = pgcontainer.MachineCPUs(ctx)
		}
		if err := step.VerifyEnv(observed, managed); err != nil {
			return err
		}
		if cfg.SkipRecorded && recorded(cfg.ResultsDir, cfg.Mode, step.N) {
			slog.Info("skipping a step that already has a valid result",
				"mode", cfg.Mode, "step", step.N)
			return nil
		}
	}

	env := benchreport.Environment{
		StepEnv:   step.Env.Name,
		Argv:      os.Args,
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
		NumCPU:    runtime.NumCPU(),
		GitCommit: gitCommit(),
	}

	dsn := cfg.DSN
	if dsn == "" {
		ctr, err := pgcontainer.Start(ctx, pgcontainer.Config{
			Image:          cfg.Image,
			Name:           fmt.Sprintf("scaling-queue-postgres-bench-%d", cfg.Port),
			Port:           cfg.Port,
			Password:       "bench",
			MaxConnections: cfg.MaxConnections,
		})
		if err != nil {
			return err
		}
		if cfg.Keep {
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
		env.Image = cfg.Image
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
	for _, w := range cfg.Workers {
		maxWorkers = max(maxWorkers, w)
	}
	if need := maxWorkers + cfg.Completers + cfg.Producers + cfg.Enqueuers + 16; need > maxConns {
		return fmt.Errorf("need %d connections but server max_connections is %d", need, maxConns)
	}

	for _, st := range cfg.Stages {
		for _, w := range cfg.Workers {
			for rep := 0; rep < cfg.Repeat; rep++ {
				res, err := runCell(ctx, ctl, dsn, st, w, cfg, env)
				if err != nil {
					return fmt.Errorf("stage %s workers %d: %w", st.Name, w, err)
				}
				path, err := benchreport.WriteResult(cfg.ResultsDir, res)
				if err != nil {
					return err
				}
				slog.Info("cell done",
					"mode", cfg.Mode, "stage", st.Name, "workers", w, "repeat", rep+1,
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
	workers int, cfg benchcfg.Config, env benchreport.Environment,
) (benchreport.Result, error) {
	shards := 1
	if st.Sharded {
		shards = cfg.Shards
		if shards == 0 || shards > workers {
			shards = workers
		}
	}
	res := benchreport.Result{
		Benchmark:       "scaling-queue-postgres",
		Mode:            cfg.Mode,
		Step:            cfg.Step,
		EnvName:         env.StepEnv,
		Stage:           st.Name,
		Workers:         workers,
		Completers:      cfg.Completers,
		Producers:       cfg.Producers,
		Slots:           cfg.Slots,
		Shards:          shards,
		BatchSize:       cfg.Batch,
		CreateBatch:     cfg.CreateBatch,
		DurationMinSecs: cfg.DurationMin.Seconds(),
		DurationMaxSecs: cfg.DurationMax.Seconds(),
		WarmupSeconds:   cfg.Warmup.Seconds(),
		WindowSeconds:   cfg.Window.Seconds(),
		StartedAt:       time.Now().UTC(),
		Env:             env,
	}

	if err := ctl.SetupStage(ctx, st); err != nil {
		return res, err
	}

	// A run starts at the queue length it means to hold. Drain fills a backlog
	// it will consume, steady starts at its target, and ops starts empty unless
	// it holds the queue at a target.
	prefill := cfg.Prefill
	switch cfg.Mode {
	case "steady":
		prefill = int(cfg.TargetBacklog)
	case "ops":
		prefill = cfg.OpsQueueTarget
	}
	if err := fill(ctx, ctl, prefill, cfg.CreateBatch, shards); err != nil {
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
	cstore, err := queue.Open(ctx, dsn, int32(cfg.Completers), params)
	if err != nil {
		return res, err
	}
	defer cstore.Close()
	pstore, err := queue.Open(ctx, dsn, int32(cfg.Producers), params)
	if err != nil {
		return res, err
	}
	defer pstore.Close()

	if cfg.Mode == "ops" {
		return runOpsCell(ctx, ctl, res, wstore, pstore, st, workers, shards, cfg)
	}

	var counters queue.Counters
	claimRec := benchreport.NewRecorder(workers)
	doneRec := benchreport.NewRecorder(cfg.Completers)
	slots := queue.NewSlots(cfg.Slots)
	sched := queue.NewScheduler(cfg.Completers * 64)

	cellCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, workers+cfg.Completers+cfg.Producers)
	var wg sync.WaitGroup

	go sched.Run(cellCtx)

	for i := 0; i < workers; i++ {
		lane := claimRec.Lane(i)
		wq := queue.StageQueue{
			Store: wstore, Stage: st, Queue: queueName, Batch: cfg.Batch,
			Shard: i % shards, Shards: shards,
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- queue.RunWorker(cellCtx, wq, queue.WorkerConfig{
				Backoff:           queue.BackoffConfig{Base: time.Millisecond, Max: 100 * time.Millisecond},
				EmptyPollInterval: 10 * time.Millisecond,
				MinDuration:       cfg.DurationMin,
				MaxDuration:       cfg.DurationMax,
				Slots:             slots,
				Sched:             sched,
				Record:            lane.Record,
			}, &counters)
		}()
	}

	for i := 0; i < cfg.Completers; i++ {
		lane := doneRec.Lane(i)
		cq := queue.StageQueue{Store: cstore, Stage: st, Queue: queueName}
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- queue.RunCompleter(cellCtx, cq, queue.CompleterConfig{
				Slots:  slots,
				Sched:  sched,
				Record: lane.Record,
			}, &counters)
		}()
	}

	if cfg.Mode == "steady" {
		limiter := rate.NewLimiter(rate.Limit(2_000_000), cfg.CreateBatch*2)
		backlog := func() int64 {
			return int64(prefill) + counters.Created.Load() - counters.Claimed.Load()
		}
		pq := queue.StageQueue{Store: pstore, Stage: st, Queue: queueName, Shards: shards}
		for i := 0; i < cfg.Producers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs <- queue.RunProducer(cellCtx, pq, queue.ProducerConfig{
					BatchSize: cfg.CreateBatch,
					Limiter:   limiter,
				}, &counters)
			}()
		}
		go queue.RunController(cellCtx, limiter, queue.ControllerConfig{
			Target:   cfg.TargetBacklog,
			Gain:     0.2,
			MinRate:  1,
			MaxRate:  2_000_000,
			Interval: 250 * time.Millisecond,
			Backlog:  backlog,
		}, &counters)
	}

	waitErr := wait(cellCtx, cfg.Warmup)

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
		waitErr = wait(cellCtx, cfg.Window)
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
	meanDuration := (cfg.DurationMin.Seconds() + cfg.DurationMax.Seconds()) / 2
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
	if res.BacklogEnd < int64(workers*cfg.Batch) {
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
	if sampler.peakInFlight() > float64(cfg.Slots)*0.95 {
		invalid("task slots ran out, so slots capped throughput")
	}
	if cfg.Mode == "steady" && res.BacklogVariationPercent > 20 {
		invalid(fmt.Sprintf("queue length varied by %.0f%%, so it was not steady",
			res.BacklogVariationPercent))
	}
	return res, nil
}

// runOpsCell measures enqueue and claim as bare operations. No task runs, so
// nothing writes DONE and nothing sits in flight. The queue length is the
// second result, because it shows which side of the queue is faster.
func runOpsCell(ctx context.Context, ctl *queue.Store, res benchreport.Result,
	wstore, pstore *queue.Store, st queue.Stage, workers, shards int, cfg benchcfg.Config,
) (benchreport.Result, error) {
	var counters queue.Counters
	claimRec := benchreport.NewRecorder(workers)

	cellCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, workers+cfg.Enqueuers)
	var wg sync.WaitGroup

	limiter := rate.NewLimiter(rate.Inf, 1)
	if cfg.OpsQueueTarget > 0 {
		limiter = rate.NewLimiter(rate.Limit(cfg.OpsQueueTarget), 100)
	}
	for i := 0; i < cfg.Enqueuers; i++ {
		eq := queue.StageQueue{Store: pstore, Stage: st, Queue: queueName, Shards: shards}
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- queue.RunProducer(cellCtx, eq, queue.ProducerConfig{
				BatchSize: 1,
				Limiter:   limiter,
			}, &counters)
		}()
	}
	if cfg.OpsQueueTarget > 0 {
		go queue.RunController(cellCtx, limiter, queue.ControllerConfig{
			Target:   int64(cfg.OpsQueueTarget),
			Gain:     0.2,
			MinRate:  1,
			MaxRate:  2_000_000,
			Interval: 250 * time.Millisecond,
			Backlog: func() int64 {
				return counters.Created.Load() - counters.Claimed.Load()
			},
		}, &counters)
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

	waitErr := wait(cellCtx, cfg.Warmup)
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
	go s.run(sampleCtx, cfg.QueueSample)

	if waitErr == nil {
		waitErr = wait(cellCtx, cfg.Window)
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

// recorded reports whether this mode and step already hold a valid result, so a
// repeated run resumes instead of measuring the same cell again.
func recorded(dir, mode string, step int) bool {
	path := filepath.Join(dir, benchreport.ResultsFile)
	if _, err := os.Stat(path); err != nil {
		return false
	}
	results, err := benchreport.ReadResults([]string{path})
	if err != nil {
		return false
	}
	for _, r := range results {
		if r.Mode == mode && r.Step == step && r.Valid {
			return true
		}
	}
	return false
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
