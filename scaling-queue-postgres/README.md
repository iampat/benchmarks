# Scaling a Postgres task queue

**Status: measurements in progress.** Stages 0 to 4 below carry full
5 minute windows. Stages 5 to 7 are still running, and no stage has been
measured yet at its best worker count. Treat every number here as
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
statements. The earlier version of this benchmark claimed 10 tasks at a
time, which spread those two statements over 10 tasks. Claiming one at a
time is what a worker that runs one task does, and it costs ten times the
statements per task.

[METHOD.md](METHOD.md) covers the harness, the two benchmark modes, and the
checks a cell passes before it reports a number.

## Results so far

Benchmark 2, the steady-state task queue, at 16 claim loops. Every cell held
its queue length steady and matched Little's Law within 1.2 percent.

| Step | tasks/s | Gain | Claim p95 | Retries | From |
| --- | ---: | ---: | ---: | ---: | --- |
| 0. Naive claim query | 13 | — | 2,184 ms | 422 | article |
| 1. `SKIP LOCKED` | 13 | 1.0x | 2,191 ms | 410 | article |
| 2. `READ COMMITTED` | 15 | 1.2x | 1,178 ms | 0 | article |
| 3. Partial covering index | 14,202 | 950x | 2.0 ms | 0 | article |
| 4. `synchronous_commit = off` | 15,424 | 1.1x | 1.8 ms | 0 | this benchmark |

```
                 log scale, three marks per doubling
  0 vanilla        ║░                                      13
  1 SKIP LOCKED    ║░                                      13
  2 READ COMMITTED ║░░                                     15
  3 partial index  ║░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░    14,202
  4 async commit   ║░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░   15,424
```

## What changes when a worker claims one task

The article's first two fixes almost vanish, and its third one carries
everything.

`SKIP LOCKED` bought 50 percent when a claim took 10 tasks. Here it buys
nothing at all, 13 tasks per second either way, and it barely moves the
retry count. A claim of one task reads `LIMIT 1`, so every worker wants the
same single row. Skipping a locked row only moves a worker to the row the
next worker already holds.

`READ COMMITTED` removes every retry, 410 of them, and buys 15 percent. The
retries were real and the fix is real. It is also worth far less than the
1.8x it bought at a batch of 10.

The partial covering index buys 950x. The claim query stops reading finished
rows and stops sorting, and claim time falls from 2.2 seconds to 2
milliseconds. At one task per claim, every task pays that scan, so removing
it matters ten times more than it did before.

## More workers make the queue slower

Stage 3 runs at 14,202 tasks per second with 16 claim loops and 10,472 with
64. The queue has one head, and every worker locks the same few index pages.
This is the cost that stage 6 removes by splitting the head into shards.

Finding each stage's best worker count is the run in progress.

## Other engines

CockroachDB, on three nodes with replication factor 3, was not promising on
this machine. Throughput stayed two orders of magnitude below Postgres under
the same schema and workload. `SKIP LOCKED` made it slower, not faster.
CockroachDB needs its own query and schema design, so this benchmark did not
pursue it.
