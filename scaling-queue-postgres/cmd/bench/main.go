package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
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

	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/benchreport"
	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/pgcontainer"
	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/queue"
)

const queueName = "bench"

type config struct {
	stages         []queue.Stage
	workers        []int
	producers      int
	batch          int
	enqueueBatch   int
	warmup         time.Duration
	window         time.Duration
	prefill        int
	backlogCap     int64
	hold           time.Duration
	shards         int
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

	ctl, err := queue.Open(ctx, dsn, 2, nil)
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
	if need := maxWorkers + cfg.producers + 8; need > maxConns {
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
					"stage", st.Name, "workers", w, "repeat", rep+1,
					"tasks_per_sec", fmt.Sprintf("%.0f", res.ThroughputPerSec),
					"p95_ms", fmt.Sprintf("%.2f", res.P95Millis),
					"retries", res.Retries, "valid", res.Valid, "result", path)
			}
		}
	}
	return nil
}

func runCell(ctx context.Context, ctl *queue.Store, dsn string, st queue.Stage,
	workers int, cfg config, env benchreport.Environment,
) (benchreport.Result, error) {
	res := benchreport.Result{
		Benchmark:     "scaling-queue-postgres",
		Stage:         st.Name,
		Workers:       workers,
		Producers:     cfg.producers,
		BatchSize:     cfg.batch,
		WarmupSeconds: cfg.warmup.Seconds(),
		WindowSeconds: cfg.window.Seconds(),
		HoldSeconds:   cfg.hold.Seconds(),
		StartedAt:     time.Now().UTC(),
		BacklogStart:  int64(cfg.prefill),
		Env:           env,
	}
	shards := 1
	if st.Sharded {
		shards = cfg.shards
	}
	res.Shards = shards
	if err := ctl.SetupStage(ctx, st); err != nil {
		return res, err
	}
	if err := ctl.Checkpoint(ctx); err != nil {
		return res, err
	}
	if err := ctl.Enqueue(ctx, queueName, cfg.prefill, shards); err != nil {
		return res, err
	}

	var params map[string]string
	if st.SyncCommit != "" {
		params = map[string]string{"synchronous_commit": st.SyncCommit}
	}
	wstore, err := queue.Open(ctx, dsn, int32(workers), params)
	if err != nil {
		return res, err
	}
	defer wstore.Close()

	pstore, err := queue.Open(ctx, dsn, int32(cfg.producers), params)
	if err != nil {
		return res, err
	}
	defer pstore.Close()

	var counters queue.Counters
	rec := benchreport.NewRecorder(workers)

	cellCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, workers+cfg.producers)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		lane := rec.Lane(i)
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
				Hold:              cfg.hold,
				Record:            lane.Record,
			}, &counters)
		}()
	}

	prefill := int64(cfg.prefill)
	backlog := func() int64 { return prefill + counters.Enqueued.Load() - counters.Dequeued.Load() }
	pq := queue.StageQueue{Store: pstore, Stage: st, Queue: queueName, Shards: shards}
	for i := 0; i < cfg.producers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- queue.RunProducer(cellCtx, pq, queue.ProducerConfig{
				BatchSize:    cfg.enqueueBatch,
				CheckEvery:   10,
				HighWater:    cfg.backlogCap,
				LowWater:     cfg.backlogCap / 2,
				Backlog:      backlog,
				PollInterval: 20 * time.Millisecond,
			}, &counters)
		}()
	}

	waitErr := wait(cellCtx, cfg.warmup)
	completedStart := counters.Completed.Load()
	retriesStart := counters.Retries.Load()
	emptyStart := counters.EmptyDequeues.Load()
	rec.SetRecording(true)
	if waitErr == nil {
		waitErr = wait(cellCtx, cfg.window)
	}
	rec.SetRecording(false)
	completedEnd := counters.Completed.Load()
	res.Retries = counters.Retries.Load() - retriesStart
	res.EmptyDequeues = counters.EmptyDequeues.Load() - emptyStart
	cancel()

	drained := make(chan struct{})
	go func() { wg.Wait(); close(drained) }()
	drainTimer := time.NewTimer(30 * time.Second)
	defer drainTimer.Stop()
	select {
	case <-drained:
	case <-drainTimer.C:
		return res, errors.New("workers did not stop within 30s")
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

	res.CompletedTasks = completedEnd - completedStart
	res.EnqueuedTasks = counters.Enqueued.Load()
	res.ThroughputPerSec = float64(res.CompletedTasks) / cfg.window.Seconds()
	stats := benchreport.Percentiles(rec.Samples())
	res.SampleCount = stats.Count
	res.P50Millis = float64(stats.P50) / float64(time.Millisecond)
	res.P95Millis = float64(stats.P95) / float64(time.Millisecond)
	res.P99Millis = float64(stats.P99) / float64(time.Millisecond)

	end, err := ctl.Backlog(ctx, queueName)
	if err != nil {
		return res, err
	}
	res.BacklogEnd = end

	res.Valid = true
	invalid := func(reason string) {
		res.Valid = false
		res.InvalidReasons = append(res.InvalidReasons, reason)
	}
	// Transient empty polls happen on a healthy queue, for example on a
	// briefly empty shard head. They only invalidate the cell when workers
	// spent a meaningful share of their polls idle.
	if batches := res.CompletedTasks / int64(cfg.batch); res.EmptyDequeues*20 > batches {
		invalid("empty polls exceeded 5% of dequeue attempts")
	}
	if res.BacklogEnd < int64(workers*cfg.batch) {
		invalid("backlog nearly drained at window end")
	}
	if res.CompletedTasks == 0 {
		invalid("no completions in window")
	}
	return res, nil
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
	workers := fs.String("workers", "4,16,64", "comma-separated worker counts")
	cfg := config{}
	fs.IntVar(&cfg.producers, "producers", 4, "producer goroutines")
	fs.IntVar(&cfg.batch, "batch", 10, "dequeue batch size")
	fs.IntVar(&cfg.enqueueBatch, "enqueue-batch", 25, "enqueue batch size")
	fs.DurationVar(&cfg.warmup, "warmup", 10*time.Second, "warm-up before measuring")
	fs.DurationVar(&cfg.window, "window", 2*time.Minute, "measurement window")
	fs.IntVar(&cfg.prefill, "prefill", 20000, "tasks inserted before workers start")
	fs.Int64Var(&cfg.backlogCap, "backlog-cap", 50000, "producers pause above this backlog")
	fs.DurationVar(&cfg.hold, "hold", 0, "simulated task duration in PENDING")
	fs.IntVar(&cfg.shards, "shards", 1, "shard count for the sharded stage")
	fs.IntVar(&cfg.repeat, "repeat", 1, "repeats per stage and worker count")
	fs.StringVar(&cfg.dsn, "dsn", "", "use this Postgres instead of a managed container")
	fs.StringVar(&cfg.image, "image", "docker.io/library/postgres:18", "container image")
	fs.IntVar(&cfg.port, "port", 55432, "host port for the managed container")
	fs.BoolVar(&cfg.keep, "keep-container", false, "do not stop the managed container")
	fs.StringVar(&cfg.resultsDir, "results-dir", "scaling-queue-postgres/results", "where result JSON files land")
	fs.IntVar(&cfg.maxConnections, "max-connections", 200, "max_connections for the managed container")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}

	if *stages == "all" {
		cfg.stages = queue.Stages()
	} else {
		for _, name := range strings.Split(*stages, ",") {
			st, ok := queue.StageByName(strings.TrimSpace(name))
			if !ok {
				var names []string
				for _, s := range queue.Stages() {
					names = append(names, s.Name)
				}
				return cfg, fmt.Errorf("unknown stage %q, valid: %s", name, strings.Join(names, ", "))
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
	if cfg.hold > 0 && cfg.hold >= cfg.warmup {
		return cfg, fmt.Errorf("warmup %v must exceed hold %v to reach a steady state", cfg.warmup, cfg.hold)
	}
	if cfg.shards < 1 {
		return cfg, fmt.Errorf("shards must be at least 1, got %d", cfg.shards)
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
