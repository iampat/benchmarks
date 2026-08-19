# scaling-queue-postgres

A Postgres task queue loses throughput to lock contention, serialization
failures, and index maintenance. The DBOS article
[Making Postgres Queues Scale](https://www.dbos.dev/blog/making-postgres-queues-scale)
removes these costs one at a time. This benchmark replicates that path, then
extends it to see how far one machine can go.

## Model

A task is a row in the `tasks` table. A producer inserts tasks with status
`ENQUEUED`. A worker runs a dequeue transaction: select the oldest `ENQUEUED`
rows, lock them, and set them to `PENDING`. A separate update then sets them
to `SUCCESS`. A stage is a named configuration of the dequeue path.

## Stages 0-3: the article

These four stages replicate the DBOS article's path. Each stage changes one
variable on top of the previous one.

| Stage              | Lock clause              | Isolation        | Index                                                            |
| ------------------ | ------------------------ | ---------------- | ---------------------------------------------------------------- |
| `0-vanilla`        | `FOR UPDATE`             | `REPEATABLE READ`| `(queue_name, created_at)`                                        |
| `1-skip-locked`    | `FOR UPDATE SKIP LOCKED` | `REPEATABLE READ`| same                                                              |
| `2-read-committed` | `FOR UPDATE SKIP LOCKED` | `READ COMMITTED` | same                                                              |
| `3-partial-index`  | `FOR UPDATE SKIP LOCKED` | `READ COMMITTED` | `(queue_name, status, priority, created_at) WHERE status = 'ENQUEUED'` |

All four share one Go code path. Only the lock clause, the isolation level,
and the index differ.

## Stages 4-6: past the article

These three stages are not in the article. They chase a higher rate under a
fixed constraint. Dequeue batch size stays at 10, rows stay locked, the
`PENDING` state stays, and the table stays logged (WAL-backed).

| Stage                | Change on top of the previous stage                          |
| -------------------- | ------------------------------------------------------------ |
| `4-async-commit`     | `synchronous_commit = off` on worker and producer sessions   |
| `5-single-statement` | Dequeue is one CTE (`WITH ... UPDATE ... RETURNING`), no explicit transaction |
| `6-sharded`          | A `shard` column gives the queue `-shards` heads, one pinned worker set each. Ordering weakens to per-shard FIFO. |

`synchronous_commit = off` keeps every write in the WAL but stops the commit
from waiting for the flush. A crash can lose the last moments of
acknowledged work. That trade is normal for a queue and it is stage 4's
whole point.

## Results

Numbers from this machine, one change at a time. Each step name links back
to the row above it. `hold` is simulated task duration: tasks stay `PENDING`
for that long while the worker keeps dequeuing.

| # | Source | Step | tasks/s | Where |
| - | --- | --- | ---: | --- |
| 1 | article | `0-vanilla` | 796 | podman, 4 VM CPUs |
| 2 | article | `3-partial-index` | 25,783 | podman, 4 VM CPUs |
| 3 | ours | + more VM CPUs (4 -> 8) | 31,510 | podman, 8 VM CPUs |
| 4 | ours | + `4-async-commit` | 33,149 | podman, 8 VM CPUs |
| 5 | ours | + `5-single-statement` | 40,122 | podman, 8 VM CPUs |
| 6 | ours | + native Postgres, unix socket, `hold=5s` | 60,055 | native, no VM |
| 7 | ours | + `6-sharded` (32 shards) | 165,700 | native, `hold=5s` |

Row 2 is the article's claim. `SKIP LOCKED`, `READ COMMITTED`, and a partial
covering index buy a 32x jump over the naive query on this machine. Rows
3-7 are this benchmark's own work, not in the article. The single biggest
step past the article is row 7. Splitting the queue into independent heads
removes the lock contention that a single FIFO head puts on every worker.
Full tables with p50/p95/p99 and retry counts are in `results/REPORT.md`.

## Other engines

CockroachDB (three nodes, podman, replication factor 3) was tried as a
drop-in alternative. It was not promising on this machine. Throughput was
two orders of magnitude below Postgres under the same schema and workload,
and the Postgres-specific `SKIP LOCKED` optimization made it worse instead
of better. CockroachDB needs its own query and schema design, not the
Postgres playbook, so it was not pursued further here.

## Workload

The benchmark measures a steady state. Producers enqueue as fast as they can
and pause above a backlog cap. Workers dequeue in batches in a closed loop. A
warm-up period runs first. The measurement window then counts completed tasks
and records dequeue latency. Latency samples include retry time.

A cell is one stage at one worker count. Each cell starts with a fresh table,
so bloat from one cell cannot reach the next. A cell is marked invalid when the
backlog almost drained. It is also invalid when more than 5 percent of
dequeue attempts found nothing to claim.

## Prerequisites

- Bazel through `bazelisk`. The build downloads the Go toolchain itself.
- `podman`, with a started machine on macOS: `podman machine start`.
- The `task` CLI, for the automation targets below. The raw `bazel run`
  commands work without it.

## Run

The bench command starts a throwaway `postgres:18` container with podman and
runs the selected cells. Each cell writes one JSON result file into
`scaling-queue-postgres/results/`. The measurement window is 2 minutes per cell, so a
full run takes about 30 minutes.

```sh
task scaling-queue-postgres:bench                                   # all stages, workers 4,16,64
task scaling-queue-postgres:bench -- -stages=0-vanilla              # one stage
task scaling-queue-postgres:bench -- -repeat=3                       # medians need 3 repeats
task scaling-queue-postgres:bench -- -dsn=postgres://...             # reuse a running server
task scaling-queue-postgres:bench -- -stages=6-sharded -shards=32 -workers=32 -hold=5s
task scaling-queue-postgres:bench:quick                              # 5s smoke run, results not kept
```

`task scaling-queue-postgres:bench` wraps `bazel run //scaling-queue-postgres/cmd/bench`, which
accepts the same flags after `--`.

The report command reads the accumulated result files and writes
`scaling-queue-postgres/results/REPORT.md`. The report shows one table per worker
count, with a delta column against the previous stage.

```sh
task scaling-queue-postgres:report
```

Results from different environments land in separate report sections. The
report never averages across environments.

## Integration test

The integration test needs a running Postgres. It skips without one, so
`bazel test //...` stays green everywhere. This target manages the container:

```sh
task scaling-queue-postgres:test:integration
```

To run it against your own server:

```sh
bazel test //scaling-queue-postgres/internal/queue:queue_test \
  --test_env=PGQUEUE_TEST_DSN=postgres://user:pass@host:5432/db
```

## Validity

Absolute numbers depend on the host. On macOS, Postgres in a podman
container runs inside a virtual machine, and its disk, network, and CPU
limits bound the result. Native Postgres on the host removes that layer.
Each result file records the image digest or native version, the server
settings, the hardware, and the exact command for reproduction.

Latency percentiles are closed-loop service times. They do not model
open-load response times.
