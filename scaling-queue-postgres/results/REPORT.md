# scaling-queue-postgres benchmark report

## Environment: darwin/arm64, 16 CPUs

- Server: PostgreSQL 18.6 (Homebrew) on aarch64-apple-darwin25.6.0, compiled by Apple clang version 21.0.0 (clang-2100.1.1.101), 64-bit
- Command: `bench -dsn=postgres://postgres@127.0.0.1:55444/postgres -mode=steady -stages=0-vanilla,1-skip-locked,2-read-committed -workers=16`
- Command: `bench -dsn=postgres://postgres@127.0.0.1:55444/postgres -mode=steady -stages=3-partial-index,4-async-commit,5-single-statement -workers=16,64`
- Settings: autovacuum=on, autovacuum_naptime=1min, fsync=on, max_connections=400, shared_buffers=128MB, synchronous_commit=on

### Benchmark 2, task queue (insert, claim, and complete together)

#### 16 claim loops

| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0-vanilla | 13 | — | 1103.96 ms | 2184.16 ms | 1.09 ms | 198 | 0.5% | 422 | 1 |
| 1-skip-locked | 13 | +0% | 1101.67 ms | 2190.64 ms | 1.10 ms | 198 | 0.3% | 410 | 1 |
| 2-read-committed | 15 | +14% | 1059.00 ms | 1178.31 ms | 1.17 ms | 226 | 0.4% | 0 | 1 |
| 3-partial-index | 14202 | +94474% | 0.96 ms | 1.96 ms | 1.43 ms | 210553 | 1.2% | 0 | 1 |
| 4-async-commit | 15424 | +9% | 0.94 ms | 1.80 ms | 0.75 ms | 229194 | 0.9% | 0 | 1 |

#### 64 claim loops

| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 3-partial-index | 10472 | — | 1.50 ms | 14.36 ms | 1.52 ms | 156553 | 0.3% | 0 | 1 |

Latency percentiles are closed-loop service times, retries included.
"Little err" is how far tasks in flight sat from throughput times mean
task duration. A small number means the run reached a steady state.
