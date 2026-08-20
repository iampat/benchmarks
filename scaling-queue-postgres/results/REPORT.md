# scaling-queue-postgres benchmark report

## Environment: darwin/arm64, 16 CPUs, 2 VM CPUs

- Server: PostgreSQL 18.6 (Debian 18.6-1.pgdg13+2) on aarch64-unknown-linux-gnu, compiled by gcc (Debian 14.2.0-19) 14.2.0, 64-bit
- Image: docker.io/library/postgres:18 (sha256:772ab753f714afefc07b096906b4961e2bb576938c7d007beaa9b62d80680c48)
- Command: `bench -mode=ops -step=0 -workers=4 -skip-recorded -window=5s -warmup=6s`
- Settings: autovacuum=on, autovacuum_naptime=1min, fsync=on, max_connections=400, shared_buffers=128MB, synchronous_commit=on

### Queue operations (enqueue and claim, one row per statement)

#### 4 claim loops

| stage | enqueue/s | dequeue/s | operations/s | mean queue | empty claims | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 0-vanilla | invalid: only 35457 enqueues and 3 dequeues, too few to measure | | | | | | | | 1 |

Latency percentiles are closed-loop service times, retries included.
"Little err" is how far tasks in flight sat from throughput times mean
task duration. A small number means the run reached a steady state.
