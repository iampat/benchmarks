# scaling-queue-postgres benchmark report

## Environment: darwin/arm64, 16 CPUs, 2 VM CPUs

- Server: PostgreSQL 18.6 (Debian 18.6-1.pgdg13+2) on aarch64-unknown-linux-gnu, compiled by gcc (Debian 14.2.0-19) 14.2.0, 64-bit
- Image: docker.io/library/postgres:18 (sha256:772ab753f714afefc07b096906b4961e2bb576938c7d007beaa9b62d80680c48)
- Command: `bench -mode=ops -step=0 -workers=1 -skip-recorded -ops-queue-target=10000 -window=60s -warmup=10s`
- Command: `bench -mode=ops -step=0 -workers=1,2,4 -skip-recorded -ops-queue-target=10000 -window=45s -warmup=10s`
- Command: `bench -mode=ops -step=0 -workers=4,8,16 -skip-recorded`
- Command: `bench -mode=ops -step=0 -workers=8 -skip-recorded -ops-queue-target=10000 -window=60s -warmup=10s`
- Settings: autovacuum=on, autovacuum_naptime=1min, fsync=on, max_connections=400, shared_buffers=128MB, synchronous_commit=on

### Queue operations (enqueue and claim, one row per statement)

#### 1 claim loops

| stage | enqueue/s | dequeue/s | operations/s | mean queue | empty claims | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 0-vanilla | 195 | 169 | 364 | 19115 | 0 | 1 |

#### 2 claim loops

| stage | enqueue/s | dequeue/s | operations/s | mean queue | empty claims | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 0-vanilla | 204 | 176 | 381 | 19057 | 0 | 1 |

#### 4 claim loops

| stage | enqueue/s | dequeue/s | operations/s | mean queue | empty claims | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 0-vanilla | 198 | 169 | 367 | 19111 | 0 | 1 |

#### 8 claim loops

| stage | enqueue/s | dequeue/s | operations/s | mean queue | empty claims | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 0-vanilla | invalid: only 1481587 enqueues and 480 dequeues, too few to measure | | | | | | | | 2 |

#### 16 claim loops

| stage | enqueue/s | dequeue/s | operations/s | mean queue | empty claims | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 0-vanilla | invalid: only 1170379 enqueues and 534 dequeues, too few to measure | | | | | | | | 1 |

Latency percentiles are closed-loop service times, retries included.
"Little err" is how far tasks in flight sat from throughput times mean
task duration. A small number means the run reached a steady state.
