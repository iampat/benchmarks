# Scaling a Postgres task queue

**No cell is measured yet.** [The experiment plan](../docs/design/scaling-queue-postgres-experiment.md)
fixes the steps, the environments, and these tables before the run starts.
Every table and chart below is empty until a cell passes its checks.

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

## The steps

| Step | Change | Source |
| --- | --- | --- |
| 0. Naive claim query | `FOR UPDATE`, `REPEATABLE READ`, btree on `(queue_name, created_at)` | article |
| 1. `SKIP LOCKED` | `FOR UPDATE SKIP LOCKED` | article |
| 2. `READ COMMITTED` | The claim runs at `READ COMMITTED` | article |
| 3. Partial covering index | `(queue_name, status, priority, created_at) WHERE status = 'CREATED'` | article |
| 4. `synchronous_commit = off` | The commit returns before the flush | this benchmark |
| 5. One statement per claim | One CTE replaces a transaction of two statements | this benchmark |
| 6. Shard the queue head | A `shard` column, one pinned worker set per shard | this benchmark |

Every step runs in four places: a virtual machine with 2, 4, and 8 CPUs, and
the host itself.

## Queue operations

One group of loops inserts a single row per statement. Another group claims a
single row per statement. No task runs, nothing writes `DONE`, and nothing
sits in flight, so a claim is the whole operation.

Operations per second, which counts one enqueue and one claim.

| Step | VM 2 CPUs | VM 4 CPUs | VM 8 CPUs | Metal |
| --- | ---: | ---: | ---: | ---: |
| 0. Naive claim query | | | | |
| 1. `SKIP LOCKED` | | | | |
| 2. `READ COMMITTED` | | | | |
| 3. Partial covering index | | | | |
| 4. `synchronous_commit = off` | | | | |
| 5. One statement per claim | | | | |
| 6. Shard the queue head | | | | |

```
                    log scale, three marks per doubling           1B tasks
  0 vanilla        ║                                          —          —
  1 SKIP LOCKED    ║                                          —          —
  2 READ COMMITTED ║                                          —          —
  3 partial index  ║                                          —          —
  4 async commit   ║                                          —          —
  5 one statement  ║                                          —          —
  6 sharded        ║                                          —          —
```

## The task queue

A task moves `CREATED`, `PENDING`, `DONE`. A worker claims one task and a
completer writes one task. Only the inserts batch, at 1000 rows. Every task
runs for a random 10 to 20 seconds, and holds a slot rather than a database
connection while it runs.

Tasks per second.

| Step | VM 2 CPUs | VM 4 CPUs | VM 8 CPUs | Metal |
| --- | ---: | ---: | ---: | ---: |
| 0. Naive claim query | | | | |
| 1. `SKIP LOCKED` | | | | |
| 2. `READ COMMITTED` | | | | |
| 3. Partial covering index | | | | |
| 4. `synchronous_commit = off` | | | | |
| 5. One statement per claim | | | | |
| 6. Shard the queue head | | | | |

```
                    log scale, three marks per doubling           1B tasks
  0 vanilla        ║                                          —          —
  1 SKIP LOCKED    ║                                          —          —
  2 READ COMMITTED ║                                          —          —
  3 partial index  ║                                          —          —
  4 async commit   ║                                          —          —
  5 one statement  ║                                          —          —
  6 sharded        ║                                          —          —
```

The last column of each chart divides one billion by the measured rate. It
is arithmetic, not a forecast. A queue that ran for hours would carry far
more dead rows than a 5 minute window builds, and
[METHOD.md](METHOD.md) records what that costs. Read the column as a floor.

## Method

[METHOD.md](METHOD.md) covers the rest. It describes the harness and its
modes, the flags, and the loop counts. It also lists the checks a cell
passes before it reports a number.
