# Scaling a Postgres task queue

**Status: measurements in progress.** Stages 0 to 4 carry full 5 minute
windows at their best worker count. Stages 5 to 7 are still running, and
their numbers below come from the 30 second scouting sweep. Treat those as
preliminary.

## The problem

A task queue in Postgres is one table. A producer inserts a row with status
`CREATED`. A worker claims the oldest such row and sets it to `PENDING`. The
worker runs the task and sets the row to `DONE`.

The claim is the hard part. Every worker wants the same row, the oldest one.
Under load the workers contend for that row. The table also fills with
finished rows, and the claim query must still read them.

The DBOS article
[Making Postgres Queues Scale](https://www.dbos.dev/blog/making-postgres-queues-scale)
removes three of these costs. This benchmark replicates that path on one
machine, then goes past it.

## The workload

A worker claims one task, and a completer finishes one task. Only the
inserts batch, at 1000 rows. Every task runs for a random 10 to 20 seconds,
and holds a slot rather than a database connection while it runs.

One claim and one completion are one statement each, so a task costs two
statements. An earlier version of this benchmark claimed 10 tasks at a time,
which spread those two statements over 10 tasks. Claiming one at a time is
what a worker that runs one task does, and it costs ten times the statements
per task.

[METHOD.md](METHOD.md) covers the harness, the two benchmark modes, and the
checks a cell passes before it reports a number.

## Results

Benchmark 2, the steady-state task queue, each stage at its best worker
count. Every 5 minute cell held its queue length steady and matched Little's
Law within 1.2 percent.

| Step | tasks/s | Claim p95 | Retries | Window | From |
| --- | ---: | ---: | ---: | --- | --- |
| 0. Naive claim query | 13.4 | 668 ms | 202 | 5 min | article |
| 1. `SKIP LOCKED` | 14.0 | 658 ms | 226 | 5 min | article |
| 2. `READ COMMITTED` | 13.7 | 1,279 ms | 0 | 5 min | article |
| 3. Partial covering index | 14,112 | 1.9 ms | 0 | 5 min | article |
| 4. `synchronous_commit = off` | 14,854 | 1.9 ms | 0 | 5 min | this benchmark |
| 5. One statement per claim | 23,501 | — | 0 | 30 s | this benchmark |
| 6. Shard the queue head | 61,338 | — | 0 | 30 s | this benchmark |
| 7. Batch the completions | 69,273 | — | 0 | 30 s | this benchmark |

## The first two fixes buy nothing here

Stages 0, 1, and 2 measure 13.4, 14.0, and 13.7 tasks per second. Those
three numbers are the same number. The article's first two optimizations
bought 50 percent and 80 percent when a claim took 10 tasks. At one task per
claim they buy nothing at all.

The reason is `LIMIT 1`. Every worker asks for the single oldest row, so
`SKIP LOCKED` only moves a worker onto the row that the next worker already
holds. Skipping needs somewhere to skip to.

`READ COMMITTED` still does something real. It removes every retry, 226 of
them, and a queue that never retries is easier to reason about. It does not
make the queue faster.

## The index carries everything

The partial covering index takes the queue from 13.7 to 14,112 tasks per
second, a factor of 1,030. Claim time falls from 1.3 seconds to 1.9
milliseconds.

```sql
CREATE INDEX tasks_claim_idx
  ON tasks (queue_name, status, priority, created_at)
  WHERE status = 'CREATED';
```

The index holds claimable rows only, and returns them in claim order. The
claim query stops reading finished rows and stops sorting. At one task per
claim, every task pays that scan, so removing it matters more here than it
did at a batch of 10.

## More workers make an unsharded queue slower

A 30 second sweep over worker counts locates each stage's best operating
point. Every unsharded stage peaks at 16 claim loops and then falls.

| Stage | 2 | 4 | 8 | 16 | 32 |
| --- | ---: | ---: | ---: | ---: | ---: |
| 3. partial index | 7,646 | 12,547 | 15,331 | **17,861** | 17,440 |
| 4. async commit | 7,811 | 12,223 | 15,465 | **19,051** | 16,952 |
| 5. one statement | 11,309 | 17,817 | 20,530 | **23,501** | 18,391 |

The queue has one head. Every worker locks the same few index pages, so
workers past the peak spend their time in each other's way.

A `shard` column splits that head, and the shape reverses. The sharded
stages keep climbing to 64 and 128 claim loops.

| Stage | 16 | 32 | 64 | 128 | 256 | 512 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 6. sharded | 46,531 | 53,626 | 59,313 | **61,338** | 46,873 | 35,375 |
| 7. batched completion | 55,790 | 63,315 | **69,273** | 67,406 | 51,720 | 36,677 |

Above 128 claim loops both stages fall away again, so the peak is measured
and not an edge of the sweep. Cells above 32 claim loops did not always
reach a steady state inside a 30 second window. These rows locate the peak.
The 5 minute runs measure it.

## Other engines

CockroachDB, on three nodes with replication factor 3, was not promising on
this machine. Throughput stayed two orders of magnitude below Postgres under
the same schema and workload. `SKIP LOCKED` made it slower, not faster.
CockroachDB needs its own query and schema design, so this benchmark did not
pursue it.
