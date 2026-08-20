# scaling-queue-postgres benchmark report

## Environment: darwin/arm64, 16 CPUs

- Server: PostgreSQL 18.6 (Homebrew) on aarch64-apple-darwin25.6.0, compiled by Apple clang version 21.0.0 (clang-2100.1.1.101), 64-bit
- Command: `bench -dsn=postgres://postgres@127.0.0.1:55444/postgres -mode=steady -stages=0-vanilla,1-skip-locked,2-read-committed -workers=8,16`
- Command: `bench -dsn=postgres://postgres@127.0.0.1:55444/postgres -mode=steady -stages=3-partial-index,4-async-commit,5-single-statement -workers=16`
- Settings: autovacuum=on, autovacuum_naptime=1min, fsync=on, max_connections=1200, shared_buffers=128MB, synchronous_commit=on

### Benchmark 2, task queue (insert, claim, and complete together)

#### 8 claim loops

| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0-vanilla | 13 | — | 565.06 ms | 668.21 ms | 0.83 ms | 201 | 0.3% | 202 | 1 |
| 1-skip-locked | 14 | +4% | 544.30 ms | 658.02 ms | 0.93 ms | 210 | 0.0% | 226 | 1 |
| 2-read-committed | 13 | -10% | 631.26 ms | 709.16 ms | 0.94 ms | 189 | 0.0% | 0 | 1 |

#### 16 claim loops

| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0-vanilla | 13 | — | 1178.69 ms | 2318.63 ms | 1.00 ms | 187 | 0.6% | 421 | 1 |
| 1-skip-locked | 13 | +5% | 1101.75 ms | 2176.51 ms | 1.10 ms | 199 | 0.6% | 416 | 1 |
| 2-read-committed | 14 | +4% | 1174.78 ms | 1278.94 ms | 1.03 ms | 206 | 0.4% | 0 | 1 |
| 3-partial-index | 14112 | +103060% | 1.02 ms | 1.95 ms | 1.23 ms | 209216 | 1.2% | 0 | 1 |
| 4-async-commit | 14854 | +5% | 0.99 ms | 1.86 ms | 0.75 ms | 220408 | 1.1% | 0 | 1 |

Latency percentiles are closed-loop service times, retries included.
"Little err" is how far tasks in flight sat from throughput times mean
task duration. A small number means the run reached a steady state.
