// Package benchcfg declares the benchmark flags once. The bench command parses
// them and the report command renders them, so the documented table cannot
// drift from the flags the binary accepts.
package benchcfg

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/experiment"
	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/queue"
)

type Config struct {
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
	Step           int
	SkipRecorded   bool
}

// FlagSet declares the flags against a throwaway config, for documentation.
func FlagSet() *flag.FlagSet {
	fs, _, _ := register(&Config{})
	return fs
}

func register(cfg *Config) (*flag.FlagSet, *string, *string) {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	var stages, workers *string
	stages = fs.String("stages", "all", "comma-separated stage names, or all")
	workers = fs.String("workers", "16,32", "comma-separated claim-loop counts")
	fs.StringVar(&cfg.Mode, "mode", "steady", "drain or steady")
	fs.IntVar(&cfg.Completers, "completers", 128, "goroutines that write DONE")
	fs.IntVar(&cfg.Enqueuers, "enqueuers", 16, "enqueue loops, ops mode only")
	fs.IntVar(&cfg.OpsQueueTarget, "ops-queue-target", 0,
		"hold the queue at this length in ops mode, 0 enqueues without a limit")
	fs.DurationVar(&cfg.QueueSample, "queue-sample", 10*time.Second, "queue length sampling interval, ops mode")
	fs.IntVar(&cfg.Slots, "slots", 3_000_000, "maximum tasks in flight")
	fs.IntVar(&cfg.Shards, "shards", 0, "shards for the sharded stages, 0 means one per claim loop")
	fs.IntVar(&cfg.Batch, "batch", 1, "tasks per claim")
	fs.IntVar(&cfg.CreateBatch, "create-batch", 1000, "rows per insert")
	fs.DurationVar(&cfg.DurationMin, "duration-min", 10*time.Second, "shortest task duration")
	fs.DurationVar(&cfg.DurationMax, "duration-max", 20*time.Second, "longest task duration")
	fs.IntVar(&cfg.Producers, "producers", 4, "producer goroutines, steady mode only")
	fs.Int64Var(&cfg.TargetBacklog, "target-backlog", 1_000_000, "queue length the controller holds")
	fs.IntVar(&cfg.Prefill, "prefill", 2_000_000, "rows inserted before a drain run")
	fs.DurationVar(&cfg.Warmup, "warmup", 60*time.Second, "warm-up before measuring")
	fs.DurationVar(&cfg.Window, "window", 5*time.Minute, "measurement window")
	fs.IntVar(&cfg.Repeat, "repeat", 1, "repeats per stage and worker count")
	fs.StringVar(&cfg.DSN, "dsn", "", "use this Postgres instead of a managed container")
	fs.StringVar(&cfg.Image, "image", "docker.io/library/postgres:18", "container image")
	fs.IntVar(&cfg.Port, "port", 55432, "host port for the managed container")
	fs.BoolVar(&cfg.Keep, "keep-container", false, "do not stop the managed container")
	fs.StringVar(&cfg.ResultsDir, "results-dir", "scaling-queue-postgres/results", "where result JSON files land")
	fs.IntVar(&cfg.MaxConnections, "max-connections", 400, "max_connections for the managed container")
	fs.IntVar(&cfg.Step, "step", -1, "run one numbered step of the experiment, -1 uses -stages")
	fs.BoolVar(&cfg.SkipRecorded, "skip-recorded", false,
		"skip a step that already has a valid result, so a run resumes")
	return fs, stages, workers
}

// Parse declares every flag and then checks the combination.
func Parse(args []string) (Config, error) {
	cfg := Config{}
	fs, stages, workers := register(&cfg)
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}

	if cfg.Mode != "drain" && cfg.Mode != "steady" && cfg.Mode != "ops" {
		return cfg, fmt.Errorf("mode must be drain, steady, or ops, got %q", cfg.Mode)
	}
	// A step names its own stage, so -step and -stages cannot disagree.
	if cfg.Step >= 0 {
		step, ok := experiment.ByN(cfg.Step)
		if !ok {
			return cfg, fmt.Errorf("no step %d, the experiment has %d", cfg.Step, len(experiment.Steps()))
		}
		stage, ok := queue.StageByName(step.Stage)
		if !ok {
			return cfg, fmt.Errorf("step %d names stage %q, which does not exist", step.N, step.Stage)
		}
		cfg.Stages = []queue.Stage{stage}
	} else if *stages == "all" {
		cfg.Stages = queue.Stages()
	} else {
		for _, name := range strings.Split(*stages, ",") {
			st, ok := queue.StageByName(strings.TrimSpace(name))
			if !ok {
				return cfg, fmt.Errorf("unknown stage %q, valid: %s",
					name, strings.Join(queue.StageNames(), ", "))
			}
			cfg.Stages = append(cfg.Stages, st)
		}
	}
	for _, s := range strings.Split(*workers, ",") {
		w, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || w < 1 {
			return cfg, fmt.Errorf("bad worker count %q", s)
		}
		cfg.Workers = append(cfg.Workers, w)
	}
	// Tasks in flight need one full task duration to reach a steady state.
	if cfg.Mode != "ops" && cfg.Warmup <= cfg.DurationMax {
		return cfg, fmt.Errorf("warmup %v must exceed the longest task duration %v",
			cfg.Warmup, cfg.DurationMax)
	}
	if cfg.Shards < 0 {
		return cfg, fmt.Errorf("shards cannot be negative, got %d", cfg.Shards)
	}
	for _, w := range cfg.Workers {
		if cfg.Shards > w {
			return cfg, fmt.Errorf(
				"%d shards with %d claim loops would leave shards with no worker", cfg.Shards, w,
			)
		}
	}
	if ws := os.Getenv("BUILD_WORKSPACE_DIRECTORY"); ws != "" && !filepath.IsAbs(cfg.ResultsDir) {
		cfg.ResultsDir = filepath.Join(ws, cfg.ResultsDir)
	}
	return cfg, nil
}
