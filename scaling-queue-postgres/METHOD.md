# How the queue benchmark runs

[README.md](README.md) reports what each optimization bought. This document
covers how to reproduce that, how the harness works, and how a run proves it
measured a steady state.

## The workload

A task moves through three states.

| State | Written by | Batch |
| --- | --- | --- |
| `CREATED` | a producer inserts it | 1000 rows per insert |
| `PENDING` | a worker claims it | one task per statement |
| `DONE` | a completer finishes it | one task per statement |

Every task runs for a random 10 to 20 seconds between `PENDING` and `DONE`.
Only the insert batches. A claim takes one task, because that is what a
worker that runs one task does.

A running task holds a slot and no database connection. A real worker
behaves the same way. It claims a task, releases the connection, runs the
task in its own process, then takes a connection again to finish. A task
that held its connection would cap the benchmark at `max_connections`
divided by 15 seconds, which measures the sleep instead of Postgres.

One deadline heap holds every running task. One goroutine and one timer per
task would cost more than the database at 300,000 tasks in flight.

## Modes

| Mode | Shape |
| --- | --- |
| `-mode=steady` | The benchmark. Inserts, claims, and completions run together while a controller holds the queue length near a target. |
| `-mode=drain` | A cross-check. Insert the whole backlog, then consume it with no producer and no controller running. |
| `-mode=ops` | Queue operations on their own. One group of loops enqueues a single row per statement, another claims a single row per statement. No task runs and nothing writes DONE. |

The report uses `steady`. `drain` reaches the same answer by a different
route. Agreement between them is evidence that neither the producer load nor
the controller shapes the result. A large gap would mean one is wrong.

In `steady` the controller sets the insert rate to the measured completion
rate, plus a correction for the error in queue length. The correction uses a
gain of 0.2 per second, and a rate limiter paces the inserts. A queue that
swings instead of settling shows up in the recorded queue variation.

`ops` measures the enqueue and claim statements alone. It reports the two
rates apart and together, and samples the queue length every 10 seconds.
The queue starts empty and finds its own length, so that length shows which
side of the queue is faster.

## Stages

A stage is a named configuration of the claim path. All stages share one Go
code path, so a comparison never measures different Go code.

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

Only the insert batches. A claim takes one task and a completion writes one
task, in every stage.

## Prerequisites

- Bazel through `bazelisk`. The build downloads the Go toolchain itself.
- `podman`. On macOS, start the machine first: `podman machine start`.
- The `task` CLI, for the targets below. The `bazel run` commands work
  without it.

## Run

The bench command starts a throwaway `postgres:18` container with podman and
runs the selected cells. Each cell appends one line to
`results/results.jsonl`.

```sh
task scaling-queue-postgres:bench -- -mode=steady         # the benchmark
task scaling-queue-postgres:bench -- -mode=drain          # the cross-check
task scaling-queue-postgres:bench -- -mode=ops            # queue operations
task scaling-queue-postgres:bench -- -dsn=postgres://…    # an existing server
task scaling-queue-postgres:bench:quick                   # 20 second smoke run
task scaling-queue-postgres:report                        # build results/REPORT.md
```

## Flags that shape a run

| Flag | Default | Meaning |
| --- | --- | --- |
| `-mode` | steady | `steady`, `drain`, or `ops` |
| `-stages` | all | Which stages to run |
| `-workers` | 16,32 | Claim loops to sweep |
| `-completers` | 128 | Goroutines that write DONE |
| `-shards` | 0 | Shards for the sharded stages, 0 means one per claim loop |
| `-slots` | 3000000 | Maximum tasks in flight |
| `-batch` | 1 | Tasks per claim |
| `-create-batch` | 1000 | Rows per insert |
| `-duration-min` | 10s | Shortest task duration |
| `-duration-max` | 20s | Longest task duration |
| `-target-backlog` | 1000000 | Queue length the controller holds |
| `-prefill` | 2000000 | Rows inserted before a drain run |
| `-warmup` | 60s | Discarded time before the window |
| `-window` | 5m | Measurement window |
| `-producers` | 4 | Producer goroutines, steady mode only |
| `-enqueuers` | 16 | Enqueue loops, ops mode only |
| `-queue-sample` | 10s | Queue length sampling interval, ops mode |

The warm-up must exceed the longest task duration, and the command refuses
to start otherwise. Tasks in flight need one full task duration to reach a
steady state. A shorter warm-up would measure a filling pipeline. The rule
does not apply to `ops`, where no task runs.

## How a cell proves it measured something

A cell reports a number only when the run reached a steady state. Each check
below marks the cell invalid, and the report shows invalid cells instead of
charting them.

- **A minimum sample.** A window must hold 1000 completions. The relative
  error on a count falls off as 1/sqrt(N), so 1000 holds it near 3 percent.
  Below that a cell cannot measure a rate.
- **Little's Law.** Tasks in flight must equal throughput times mean task
  duration, within 10 percent. A run that misses it was still filling or
  draining its pipeline, whatever its throughput says. This is the strongest
  check the harness has, because it fails for any error in the task
  lifetime, the counters, or the window boundaries.
- **Empty claims** must stay under 5 percent of claims. Above that the
  workers waited for tasks, so the number measures the producers.
- **Queue length** in steady mode must vary less than 20 percent. A queue
  that swings did not hold steady, whatever its mean.
- **Slots** must never reach 95 percent of the pool. At the cap, the slot
  count sets throughput rather than the database.
- **The backlog** must survive the window. A drained queue ends the
  measurement early.

## Sizing a drain run

A drain run consumes its prefill and never refills. The prefill must exceed
the rate times the warm-up plus the window. It must also leave the queue
deep enough that the claim cost does not change while the window runs.

One prefill cannot serve a stage claiming 13 tasks per second and one
claiming 39,000. Each stage group gets a prefill sized from its measured
rate. Stages 0 to 2 get 1 million rows, stages 3 to 5 get 8 million, and
stages 6 and 7 get 24 million. Every group starts from the same queue depth
of 1 million, and the rest is fuel.

Drain numbers compare inside a group. The `steady` benchmark holds the same
1 million queue in every stage, so it is the comparison across stages.

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

Results append one line per cell to `results/results.jsonl`. Each line
records the server version, the settings, the hardware, the git commit, and
the exact command. `results/sweep.jsonl` holds the 30 second sweeps that
locate each stage's best worker count.

[The experiment plan](../docs/design/scaling-queue-postgres-experiment.md)
fixes the steps, the environments, and the tables before the run starts.

Latency percentiles are closed-loop service times. The claim loops are the
system under test, so these numbers do not model open-load response times.

Postgres on macOS does not use `F_FULLFSYNC` by default. A native fsync
stops at the drive cache. This favours the native numbers a little over the
container numbers.

## Other engines

CockroachDB, on three nodes with replication factor 3, was not promising on
this machine. Throughput stayed two orders of magnitude below Postgres under
the same schema and workload. `SKIP LOCKED` made it slower, not faster.
CockroachDB needs its own query and schema design, so this benchmark did not
pursue it.
