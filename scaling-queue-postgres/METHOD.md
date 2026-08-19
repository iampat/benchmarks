# How the queue benchmark runs

[README.md](README.md) reports what each optimization bought. This document
covers how to reproduce that, how the harness works, and what the full
parameter sweep shows.

## Prerequisites

- Bazel through `bazelisk`. The build downloads the Go toolchain itself.
- `podman`. On macOS, start the machine first: `podman machine start`.
- The `task` CLI, for the targets below. The `bazel run` commands work
  without it.

## Run

The bench command starts a throwaway `postgres:18` container with podman and
runs the selected cells. Each cell writes one JSON file into `results/`. The
measurement window is 2 minutes per cell, so a full run needs about 30
minutes.

```sh
task scaling-queue-postgres:bench                       # every stage, workers 4,16,64
task scaling-queue-postgres:bench -- -stages=0-vanilla  # one stage
task scaling-queue-postgres:bench -- -repeat=3          # medians need 3 repeats
task scaling-queue-postgres:bench -- -dsn=postgres://…  # an existing server
task scaling-queue-postgres:bench:quick                 # 5 second smoke run
```

The record run for step 8 of the report:

```sh
task scaling-queue-postgres:bench -- \
  '-dsn=postgres://postgres@/postgres?host=/tmp&port=55444' \
  -stages=6-sharded -workers=32 -shards=32 \
  -producers=8 -enqueue-batch=100 -hold=5s
```

`task scaling-queue-postgres:bench` wraps
`bazel run //scaling-queue-postgres/cmd/bench`, which takes the same flags
after `--`.

The report command reads every JSON file and writes
[results/REPORT.md](results/REPORT.md).

```sh
task scaling-queue-postgres:report
```

Results from different environments land in separate report sections. The
report never averages across environments.

## Flags that shape a run

| Flag | Default | Meaning |
| --- | --- | --- |
| `-stages` | all | Which stages to run |
| `-workers` | 4,16,64 | Worker counts to sweep |
| `-shards` | 1 | Shard count for the sharded stage |
| `-batch` | 10 | Tasks per claim |
| `-hold` | 0 | Time a task stays in `PENDING` |
| `-producers` | 4 | Producer goroutines |
| `-window` | 2m | Measurement window per cell |
| `-warmup` | 10s | Discarded time before the window |
| `-prefill` | 20000 | Tasks inserted before the workers start |
| `-backlog-cap` | 50000 | Backlog level where producers pause |
| `-dsn` | none | Target server. Empty starts a container |

## Stages

A stage is a named configuration of the claim path. All stages share one Go
code path, so a comparison never measures different Go code. Only these
fields differ.

Stages 0 to 3 replicate the DBOS article.

| Stage | Lock clause | Isolation | Index |
| --- | --- | --- | --- |
| `0-vanilla` | `FOR UPDATE` | `REPEATABLE READ` | `(queue_name, created_at)` |
| `1-skip-locked` | `FOR UPDATE SKIP LOCKED` | `REPEATABLE READ` | same |
| `2-read-committed` | `FOR UPDATE SKIP LOCKED` | `READ COMMITTED` | same |
| `3-partial-index` | `FOR UPDATE SKIP LOCKED` | `READ COMMITTED` | partial, covering |

Stages 4 to 6 go past the article. Each one adds to the stage above it.

| Stage | Addition |
| --- | --- |
| `4-async-commit` | `synchronous_commit = off` on every session |
| `5-single-statement` | The claim is one CTE, with no explicit transaction |
| `6-sharded` | A `shard` column, and one pinned worker set per shard |

## Workload

The benchmark measures a steady state. Producers insert tasks as fast as
they can, and pause above the backlog cap. Workers claim tasks in a closed
loop. The warm-up period runs first and its samples are discarded. The
measurement window then counts completed tasks and records claim latency.
A latency sample covers the first attempt to the successful commit, so it
includes retry time.

A cell is one stage at one worker count. Each cell starts with a fresh
table, so bloat from one cell never reaches the next. Completed rows stay in
the table for the whole window on purpose. Their index maintenance cost is
what stage 3 removes.

A cell is invalid when the backlog almost drained. It is also invalid when
more than 5 percent of claim attempts found nothing to take. An invalid cell
measures the producers, not the claim path. The report shows invalid cells
instead of charting them.

## Worker count sweep

Worker count is the parameter that changed the most between stages. Before
the shard column, more workers hurt past 16 workers. After it, 32 workers
win.

In the podman container with 4 virtual CPUs, no hold:

| Stage | 4 workers | 16 workers | 64 workers |
| --- | ---: | ---: | ---: |
| `0-vanilla` | 796 | 691 | 214 |
| `1-skip-locked` | 1,194 | 790 | 385 |
| `2-read-committed` | 2,062 | 2,158 | 2,121 |
| `3-partial-index` | 20,882 | 25,783 | 22,162 |

Stage 0 and stage 1 lose throughput as workers arrive, because the workers
contend. Stage 2 is flat. Stage 3 peaks at 16 workers.

On native Postgres with `-hold=5s`:

| Stage | 8 | 16 | 24 | 32 | 64 |
| --- | ---: | ---: | ---: | ---: | ---: |
| `3-partial-index` | | 49,465 | | 32,610 | invalid |
| `4-async-commit` | | 53,328 | | 41,578 | invalid |
| `5-single-statement` | 52,097 | 58,556 | 53,987 | 45,522 | invalid |
| `6-sharded` | | 68,484 | | 165,700 | invalid |

The sharded stage uses one shard per worker. It reverses the pattern. Every
earlier stage peaks at 16 workers and then falls. The sharded stage gains
2.4 times from 16 workers to 32, because each worker owns its own queue
head.

## What the hold changes

The `-hold` flag keeps a task in `PENDING` for a set time. A timer goroutine
then writes the completion, so the worker does not wait for that write. The
flag makes the workload realistic and it also raises throughput.

Stage `5-single-statement` at 16 workers isolates both effects:

| Server | Hold | tasks/s |
| --- | --- | ---: |
| podman, 8 CPUs, TCP | none | 40,122 |
| native, TCP | none | 49,205 |
| native, TCP | 5s | 58,556 |
| native, unix socket | 5s | 60,055 |

The container costs 23 percent. The hold adds 19 percent. The socket adds 3
percent, which is close to the spread between repeated runs. Steps 7, 8, and
9 of the report use these rows, one change at a time.

[results/REPORT.md](results/REPORT.md) holds the complete tables, with p50,
p95, and p99 latency, retry counts, and the exact command for each run.

## Integration test

The integration test needs a running Postgres. It skips without one, so
`bazel test //...` stays green everywhere. This target manages the
container:

```sh
task scaling-queue-postgres:test:integration
```

To use your own server:

```sh
bazel test //scaling-queue-postgres/internal/queue:queue_test \
  --test_env=PGQUEUE_TEST_DSN=postgres://user:pass@host:5432/db
```

The test claims 100 tasks from 4 goroutines in every stage. It asserts that
each task is claimed exactly once.

## Validity

Absolute numbers depend on the host. In a podman container on macOS,
Postgres runs inside a virtual machine, and the disk, network, and CPU
limits of that machine bound the result. A native server removes that layer.
Compare stages inside one environment section, never across two.

Each result file records the image digest or the native version, the server
settings, the hardware, the git commit, and the exact command.

Latency percentiles are closed-loop service times. The workers are the
system under test, so these numbers do not model open-load response times.

Postgres on macOS does not use `F_FULLFSYNC` by default. A native fsync
stops at the drive cache. This favours the native numbers a little over the
container numbers.
