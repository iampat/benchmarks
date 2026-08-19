# Scaling a Postgres task queue: 796 to 165,700 tasks per second

## The problem

A task queue in Postgres is one table. A producer inserts a row with status
`ENQUEUED`. A worker claims the oldest such row and sets it to `PENDING`. The
worker runs the task and sets the row to `SUCCESS`.

The claim is the hard part. Every worker wants the same row, the oldest one.
Under load the workers contend for that row. The table also fills with
finished rows, and the claim query must still read them.

The DBOS article
[Making Postgres Queues Scale](https://www.dbos.dev/blog/making-postgres-queues-scale)
removes three of these costs. This benchmark replicates that path on one
machine, then goes past it.

Four rules hold for every step below. The claim batch stays at 10 tasks. Rows
stay locked during a claim. The `PENDING` state stays. The table stays
logged, so the write-ahead log (WAL) records every change.

## Results

Each step keeps the changes of the steps above it. Each number is the best
valid measurement for that step across worker counts.

| Step | tasks/s | Gain | From | Server |
| --- | ---: | ---: | --- | --- |
| 0. Naive claim query | 796 | — | article | podman, 4 CPUs |
| 1. `SKIP LOCKED` | 1,194 | 1.5x | article | podman, 4 CPUs |
| 2. `READ COMMITTED` | 2,158 | 1.8x | article | podman, 4 CPUs |
| 3. Partial covering index | 25,783 | 12x | article | podman, 4 CPUs |
| 4. Twice the server CPUs | 31,510 | 1.2x | this benchmark | podman, 8 CPUs |
| 5. `synchronous_commit = off` | 33,149 | 1.05x | this benchmark | podman, 8 CPUs |
| 6. One statement per claim | 40,122 | 1.2x | this benchmark | podman, 8 CPUs |
| 7. Postgres outside the container | 49,205 | 1.2x | this benchmark | native |
| 8. Task duration off the claim loop | 60,055 | 1.2x | this benchmark | native |
| 9. 32 queue shards | 165,700 | 2.8x | this benchmark | native |

The article's path gives 32x. The steps past it give another 6.4x.

```
                   log scale, one mark per doubling
  0 vanilla        ║░                                             796
  1 SKIP LOCKED    ║░░░░                                        1,194
  2 READ COMMITTED ║░░░░░░░░                                    2,158
  3 partial index  ║░░░░░░░░░░░░░░░░░░░░░░░░░░                 25,783
  4 more CPUs      ║░░░░░░░░░░░░░░░░░░░░░░░░░░░░               31,510
  5 async commit   ║░░░░░░░░░░░░░░░░░░░░░░░░░░░░               33,149
  6 one statement  ║░░░░░░░░░░░░░░░░░░░░░░░░░░░░░              40,122
  7 no container   ║░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░            49,205
  8 task duration  ║░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░           60,055
  9 32 shards      ║░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  165,700
```

[METHOD.md](METHOD.md) has the commands, the harness design, and the full
worker-count sweep.

[METHOD.md](METHOD.md) has the commands, the harness design, and the full
worker-count sweep.

## Step 0. The naive claim query

The first version uses the obvious query.

```sql
SELECT id FROM tasks
WHERE queue_name = $1 AND status = 'ENQUEUED'
ORDER BY priority, created_at LIMIT 10
FOR UPDATE;
```

Every worker reads the same 10 rows. One worker locks them. The others block
on those locks. The `REPEATABLE READ` isolation level then aborts them with a
serialization failure, so they retry and collide again.

At 4 workers the run records 5,446 retries in 2 minutes. More workers make
the queue slower. Throughput falls from 796 tasks/s at 4 workers to 214 at
64. The p95 claim time at 64 workers is 14.9 seconds.

## Step 1. Locks that skip

`FOR UPDATE SKIP LOCKED` ends the blocking. A worker that meets a locked row
skips it and reads the next one.

```sql
... ORDER BY priority, created_at LIMIT 10
FOR UPDATE SKIP LOCKED;
```

Throughput rises 50 percent. Retries stay high at 4,853. The isolation level
still aborts a transaction when another transaction changes a row it read.

## Step 2. Isolation level

`REPEATABLE READ` gives a queue nothing. Each claim is independent, so a
stable snapshot across statements has no value here. `READ COMMITTED` reads
fresh rows for each statement instead.

Retries fall from 4,853 to zero. Throughput also stops falling as workers
arrive. The queue holds 2,062 tasks/s at 4 workers, 2,158 at 16, and 2,121
at 64. The queue is stable, and still slow.

## Step 3. The claim index

The claim query still reads finished rows. An index on
`(queue_name, created_at)` holds every row ever inserted, and the query must
sort what it reads. A partial covering index removes both costs.

```sql
CREATE INDEX tasks_dequeue_idx
  ON tasks (queue_name, status, priority, created_at)
  WHERE status = 'ENQUEUED';
```

The index holds claimable rows only. It returns them in claim order, so the
sort disappears. A row leaves the index when it leaves `ENQUEUED`, so
Postgres stops the index maintenance for it.

Throughput rises 12 times, to 25,783 tasks/s. This is the largest single
gain in the ladder. It also ends the article's path, at 32 times the naive
query.

## Step 4. Server CPUs

Twice the CPUs buy 22 percent, not twice the throughput. The podman virtual
machine went from 4 CPUs to 8. A CPU-bound server would gain far more, so
something else shares the ceiling.

## Step 5. Commit durability

`synchronous_commit = off` buys 5 percent. The setting lets a commit return
before the WAL flush reaches the disk. The small gain answers the question
from step 4. The disk flush is not the wall.

The setting has a cost. A crash loses the last moments of acknowledged work,
about 0.6 seconds. It never corrupts data and it never loses part of a
transaction. A lost claim causes a re-run, which an at-least-once queue
already tolerates. A lost insert is worse, because the producer holds an
acknowledgement for a task that no longer exists.

## Step 6. Round trips

One statement replaces four messages. The claim was a transaction: `BEGIN`,
`SELECT ... FOR UPDATE SKIP LOCKED`, `UPDATE`, `COMMIT`. A common table
expression (CTE) does the same work in one statement.

```sql
WITH c AS (
  SELECT id FROM tasks
  WHERE queue_name = $1 AND status = 'ENQUEUED'
  ORDER BY priority, created_at LIMIT $2
  FOR UPDATE SKIP LOCKED
)
UPDATE tasks SET status = 'PENDING', started_at = now()
WHERE id IN (SELECT id FROM c) RETURNING id;
```

Throughput rises 21 percent. That is four times what the disk flush bought.
The cost of a claim sits in the statement path, not on the disk.

## Step 7. The container layer

Postgres on the host is 23 percent faster than Postgres in the container.
Both runs use TCP and the same workload, so the container is the only
difference. On macOS a container runs inside a virtual machine, and every
packet crosses a userspace network proxy.

The result agrees with step 6. Round trips set the ceiling, not the disk.

## Step 8. Task duration

A real task takes time. This step gives every task a duration of 5 seconds.
The row stays in `PENDING` for that time before the worker marks it
`SUCCESS`, so about 300,000 tasks are in flight at any moment.

Throughput rises 19 percent, to 58,556 tasks/s. A timer goroutine performs
the completion write, so the worker no longer waits for it. The claim loop
does less work per task and claims more of them.

A unix domain socket in place of TCP raises this to 60,055 tasks/s. That 3
percent is close to the spread between repeated runs, so treat it as small.

## Step 9. The queue head

Every step so far shares one bottleneck. The queue has a single head, so all
workers scan and lock the same few index pages. That is why 32 and 64
workers were slower than 16.

A `shard` column splits the head. The index leads with the shard, so each
shard owns a contiguous run of index pages.

```sql
CREATE INDEX tasks_dequeue_idx
  ON tasks (queue_name, shard, priority, created_at)
  WHERE status = 'ENQUEUED';
```

A producer assigns a random shard. A worker pins to one shard and never
reads another. The table, the disk, and the WAL stay single. Only the
contention splits.

With 32 shards and 32 workers the queue reaches 165,700 tasks/s. The p95
claim time falls to 3.8 ms, below the 4.8 ms of step 8, with twice the
workers. Order weakens from one queue-wide first-in-first-out (FIFO)
sequence to one FIFO sequence per shard.

## Where it stops

64 shards are worse than 32, at 158,553 tasks/s. The benchmark marks that
cell invalid. Each of the 64 heads held too few tasks, and 17 percent of
claims found an empty shard. The producers set that limit, not the claim
path.

The final number holds the four rules from the start. The claim batch is
still 10 tasks, rows are still locked, `PENDING` still exists, and the table
is still logged.

## Other engines

CockroachDB, on three nodes with replication factor 3, was not promising on
this machine. Throughput stayed two orders of magnitude below Postgres under
the same schema and workload. `SKIP LOCKED` made it slower, not faster.
CockroachDB needs its own query and schema design, so this benchmark did not
pursue it.
