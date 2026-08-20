# The scaling-queue-postgres experiment

## Status

Agreed, 2026-08-19. No cell is measured yet. This document fixes the shape
of the experiment before it runs, so the tables cannot drift while results
arrive.

## Problem

Earlier runs answered different questions with different shapes. The stages
changed, the machine changed, and the two benchmarks reported different
columns. A reader could not put two numbers beside each other and trust the
comparison.

This document fixes the steps, the environments, and the two benchmarks. It
also fixes the tables and the charts, which are the same for both
benchmarks.

## Steps

Seven steps. Each keeps every change of the step above it.

| Step | Change | Source |
| --- | --- | --- |
| 0. Naive claim query | `FOR UPDATE`, `REPEATABLE READ`, btree on `(queue_name, created_at)` | article |
| 1. `SKIP LOCKED` | `FOR UPDATE SKIP LOCKED` | article |
| 2. `READ COMMITTED` | The claim runs at `READ COMMITTED` | article |
| 3. Partial covering index | `(queue_name, status, priority, created_at) WHERE status = 'CREATED'` | article |
| 4. `synchronous_commit = off` | The commit returns before the flush | ours |
| 5. One statement per claim | One CTE replaces a transaction of two statements | ours |
| 6. Shard the queue head | A `shard` column, one pinned worker set per shard | ours |

## Environments

Four, in this order. Every step runs in every environment.

| Environment | Postgres runs in |
| --- | --- |
| 1. VM, 2 CPUs | a podman container, virtual machine set to 2 CPUs |
| 2. VM, 4 CPUs | a podman container, virtual machine set to 4 CPUs |
| 3. VM, 8 CPUs | a podman container, virtual machine set to 8 CPUs |
| 4. Metal | the host, no container and no virtual machine |

## Benchmarks

Two. They report the same columns and the same charts.

**Queue operations.** One group of loops inserts a single row per statement.
Another group claims a single row per statement. No task runs, nothing
writes `DONE`, and nothing sits in flight.

**Task queue.** A task moves `CREATED`, `PENDING`, `DONE`. A worker claims
one task and a completer writes one task. Inserts batch at 1000 rows. Every
task runs for a random 10 to 20 seconds and holds a slot, not a connection.

## The matrix

2 benchmarks x 7 steps x 4 environments = 56 cells.

Each cell runs a 5 minute window after its warm-up, so a cell costs about
6 minutes. The run costs about 6 hours, plus the time to restart the virtual
machine between environments.

CONSIDER(ali): the worker count stays fixed per step across environments,
taken from the sweep. A machine with 2 CPUs may peak at a different count
than one with 8. Sweeping the count in every environment would multiply the
matrix by 4 again.

## The tables

Both benchmarks use this table, one row per step and one column per
environment.

| Step | VM 2 CPUs | VM 4 CPUs | VM 8 CPUs | Metal |
| --- | ---: | ---: | ---: | ---: |

## The charts

Both benchmarks use this chart, one per environment. The bar is a log scale
at three marks per doubling. The last column is one billion divided by the
rate, which is arithmetic and not a forecast.

```
                    log scale, three marks per doubling           1B tasks
  0 vanilla        ║
  1 SKIP LOCKED    ║
  2 READ COMMITTED ║
  3 partial index  ║
  4 async commit   ║
  5 one statement  ║
  6 sharded        ║
```

## Rules that do not change

- A cell reports a number only after it passes the checks in
  [METHOD.md](../../scaling-queue-postgres/METHOD.md).
- A claim takes one task. A completion writes one task. Only inserts batch.
- Results append one line per cell to `results/results.jsonl`.
- A step that cannot reach a rate says so, and does not report a number.

## Open questions

- CONSIDER(ali): the worker count per step, fixed across environments.
- CONSIDER(ali): whether the 2 CPU virtual machine can hold the connections
  that step 6 needs at 64 claim loops.
