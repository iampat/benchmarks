# Scaling a Postgres task queue

**Status: benchmark 2 is complete. Benchmark 1 is still running.** Every
number below carries a full 5 minute window at the stage's best worker
count. Every cell passed the steady-state checks.

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
statements. Claiming one at a time is what a worker that runs one task does.

[METHOD.md](METHOD.md) covers the harness, the two benchmark modes, and the
checks a cell passes before it reports a number.

## Results

Benchmark 2, the steady-state task queue. Each stage runs at its best worker
count over a 5 minute window.

| Step | tasks/s | Gain | Workers | Claim p95 | Retries | From |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| 0. Naive claim query | 13.4 | — | 8 | 668 ms | 202 | article |
| 1. `SKIP LOCKED` | 14.0 | 1.0x | 8 | 658 ms | 226 | article |
| 2. `READ COMMITTED` | 13.7 | 1.0x | 16 | 1,279 ms | 0 | article |
| 3. Partial covering index | 14,112 | 1030x | 16 | 1.9 ms | 0 | article |
| 4. `synchronous_commit = off` | 14,854 | 1.05x | 16 | 1.9 ms | 0 | this benchmark |
| 5. One statement per claim | 16,296 | 1.10x | 16 | 1.9 ms | 0 | this benchmark |
| 6. Shard the queue head | 34,194 | 2.10x | 64 | 6.5 ms | 0 | this benchmark |
| 7. Batch the completions | 38,904 | 1.14x | 64 | 5.0 ms | 0 | this benchmark |

The article's path gives 1030x. The steps past it give another 2.8x. The
whole ladder is 2,900x.

```
                    log scale, three marks per doubling
  0 vanilla            ║░                                         13
  1 SKIP LOCKED        ║░                                         14
  2 READ COMMITTED     ║░                                         13
  3 partial index      ║░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░       14,112
  4 async commit       ║░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░       14,854
  5 one statement      ║░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░      16,296
  6 sharded            ║░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░   34,194
  7 batched completion ║░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  38,904
```

## A short run overstates the result

The same stages measured over a 30 second window read far higher.

| Stage | 30 seconds | 5 minutes | Overstatement |
| --- | ---: | ---: | ---: |
| 5. one statement | 23,501 | 16,296 | 44 percent |
| 6. sharded | 61,338 | 34,194 | 79 percent |
| 7. batched completion | 69,273 | 38,904 | 78 percent |

At 35,000 tasks per second a 5 minute window moves 10 million tasks, and
each task writes three row versions. The table carries 30 million row
versions by the end, autovacuum runs hard, and the index grows. A 30 second
window finishes before any of that starts.

The short numbers are not wrong. They measure a queue that has just started.
The 5 minute numbers measure a queue that has been running, which is the
number to plan with.

The peak worker count moves too. Over 30 seconds the sharded stage looked
fastest at 128 claim loops. Over 5 minutes 64 wins, and 128 is 20 percent
slower.

## The first two fixes buy nothing here

Stages 0, 1, and 2 measure 13.4, 14.0, and 13.7 tasks per second. Those
three numbers are the same number. The article's first two optimizations
bought 50 percent and 80 percent when a claim took 10 tasks. At one task per
claim they buy nothing.

The reason is `LIMIT 1`. Every worker asks for the single oldest row, so
`SKIP LOCKED` only moves a worker onto the row that the next worker already
holds. Skipping needs somewhere to skip to.

`READ COMMITTED` still does something real. It removes every retry, 226 of
them. It does not make the queue faster.

## The index carries the article's whole gain

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
claim every task pays that scan, so removing it matters more here than it
did at a batch of 10.

## Sharding is the largest gain past the article

Stages 4 and 5 buy 5 and 10 percent. Sharding the queue head buys 110
percent, and batching the completions buys another 14 percent.

A 30 second sweep over worker counts shows why. Every unsharded stage peaks
at 16 claim loops and then falls.

| Stage | 2 | 4 | 8 | 16 | 32 |
| --- | ---: | ---: | ---: | ---: | ---: |
| 3. partial index | 7,646 | 12,547 | 15,331 | **17,861** | 17,440 |
| 4. async commit | 7,811 | 12,223 | 15,465 | **19,051** | 16,952 |
| 5. one statement | 11,309 | 17,817 | 20,530 | **23,501** | 18,391 |

The queue has one head. Every worker locks the same few index pages, so
workers past the peak spend their time in each other's way. A `shard` column
splits that head, and the shape changes.

| Stage | 16 | 32 | 64 | 128 | 256 | 512 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 6. sharded | 46,531 | 53,626 | 59,313 | **61,338** | 46,873 | 35,375 |
| 7. batched completion | 55,790 | 63,315 | **69,273** | 67,406 | 51,720 | 36,677 |

Both sweeps run past their peak and come back down, so neither peak sits at
the edge of the range. These rows locate the peak. The 5 minute runs in the
results table measure it.

## Other engines

CockroachDB, on three nodes with replication factor 3, was not promising on
this machine. Throughput stayed two orders of magnitude below Postgres under
the same schema and workload. `SKIP LOCKED` made it slower, not faster.
CockroachDB needs its own query and schema design, so this benchmark did not
pursue it.
