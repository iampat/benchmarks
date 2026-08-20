# Scaling a Postgres task queue

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

## Queue operations

Before the task queue, measure the two operations it rests on. One group of
loops inserts a single row per statement. Another group claims a single row
per statement. No task runs, nothing writes `DONE`, and nothing sits in
flight, so a claim is the whole operation. The queue starts empty and finds
its own length.

**This run is in progress.** Six of the seven rows are still measuring.

| Step | operations/s | enqueue/s | dequeue/s | Claim p95 | Empty claims | Mean queue |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 0. Naive claim query | too slow to measure | 21,090 | 0.5 | 64 s | 0 | 3,301,284 |
| 1. `SKIP LOCKED` | | | | | | |
| 2. `READ COMMITTED` | | | | | | |
| 3. Partial covering index | | | | | | |
| 4. `synchronous_commit = off` | | | | | | |
| 5. One statement per claim | | | | | | |
| 6. Shard the queue head | | | | | | |

The naive claim query manages 164 claims in 5 minutes, against 6.3 million
inserts. It fails the rule that a window must hold 1000 completions, so it
reports no rate. Its queue grows to 3.3 million rows, and a larger queue
makes each claim slower still.

## The task queue benchmark

Producers, workers, and completers all run together, and a controller holds
the queue length at 1,000,000 tasks. A worker claims one task, and a
completer finishes one task. Only the inserts batch, at 1000 rows. Every
task runs for a random 10 to 20 seconds, and holds a slot rather than a
database connection while it runs.

One claim and one completion are one statement each, so a task costs two
statements. Claiming one at a time is what a worker that runs one task does.

Each stage runs a 5 minute window at its best worker count. Repeated runs of
the same cell vary by about 2 percent, so a difference under 4 percent is
not a difference.

[METHOD.md](METHOD.md) covers the rest. It describes the harness and its
modes, the flags, and the loop counts. It also lists the checks a cell
passes before it reports a number, and what those checks found.

## Results

| Step | tasks/s | Gain | Workers | Claim p95 | Retries | From |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| 0. Naive claim query | 13.4 | — | 8 | 668 ms | 202 | article |
| 1. `SKIP LOCKED` | 14.0 | none | 8 | 658 ms | 226 | article |
| 2. `READ COMMITTED` | 13.7 | none | 16 | 1,279 ms | 0 | article |
| 3. Partial covering index | 14,112 | 1030x | 16 | 1.9 ms | 0 | article |
| 4. `synchronous_commit = off` | 14,854 | 1.05x | 16 | 1.9 ms | 0 | this benchmark |
| 5. One statement per claim | 16,296 | 1.10x | 16 | 1.9 ms | 0 | this benchmark |
| 6. Shard the queue head | 34,194 | 2.10x | 64 | 6.5 ms | 0 | this benchmark |

The article's path gives 1030x. The steps past it give another 2.4x. The
whole ladder is 2,550x.

```
                    log scale, three marks per doubling
  0 vanilla          ║░                                         13
  1 SKIP LOCKED      ║░                                         14
  2 READ COMMITTED   ║░                                         13
  3 partial index    ║░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░       14,112
  4 async commit     ║░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░       14,854
  5 one statement    ║░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░      16,296
  6 sharded          ║░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░   34,194
```

## The partial covering index

The partial covering index accounts for the largest improvement in the
ladder. It takes the queue from 13.7 to 14,112 tasks per second, and claim
time falls from 1.3 seconds to 1.9 milliseconds.

```sql
CREATE INDEX tasks_claim_idx
  ON tasks (queue_name, status, priority, created_at)
  WHERE status = 'CREATED';
```

The index holds claimable rows only, and it returns them in claim order. The
claim query no longer reads finished rows, and it no longer sorts. A row
also leaves the index once it leaves `CREATED`, so Postgres stops
maintaining an entry for it.

Each task pays this cost once, because a claim takes one task. That is why
the index matters more here than it did at a batch of 10 tasks.

## Sharding the queue head

Stages 4 and 5 buy 5 and 10 percent each. Sharding the queue head buys 110
percent, the largest gain past the article.

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

The sweep runs past the peak and comes back down, so the peak does not sit
at the edge of the range.

## Window length

The same stages measured over a 30 second window read far higher.

| Stage | 30 seconds | 5 minutes | Overstatement |
| --- | ---: | ---: | ---: |
| 5. one statement | 23,501 | 16,296 | 44 percent |
| 6. sharded | 61,338 | 34,194 | 79 percent |

At 34,000 tasks per second a 5 minute window moves 10 million tasks, and
each task writes three row versions. The table carries 30 million row
versions by the end, autovacuum runs hard, and the index grows. A 30 second
window finishes before any of that starts.

The short numbers are not wrong. They measure a queue that has just started.
The 5 minute numbers measure a queue that has been running.

The best worker count moves too. Over 30 seconds the sharded stage looked
fastest at 128 claim loops. Over 5 minutes 64 wins, and 128 is 20 percent
slower.
