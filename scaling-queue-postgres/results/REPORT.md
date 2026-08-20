# scaling-queue-postgres benchmark report

## Environment: darwin/arm64, 16 CPUs

- Server: PostgreSQL 18.6 (Homebrew) on aarch64-apple-darwin25.6.0, compiled by Apple clang version 21.0.0 (clang-2100.1.1.101), 64-bit
- Command: `bench -dsn=postgres://postgres@127.0.0.1:55444/postgres -mode=drain -stages=0-vanilla,1-skip-locked,2-read-committed -workers=16 -prefill=1000000`
- Command: `bench -dsn=postgres://postgres@127.0.0.1:55444/postgres -mode=drain -stages=3-partial-index,4-async-commit,5-single-statement -workers=16 -prefill=8000000`
- Command: `bench -dsn=postgres://postgres@127.0.0.1:55444/postgres -mode=drain -stages=6-sharded,7-batched-completion -workers=64 -prefill=18000000`
- Command: `bench -dsn=postgres://postgres@127.0.0.1:55444/postgres -mode=steady -stages=0-vanilla,1-skip-locked,2-read-committed -workers=8,16`
- Command: `bench -dsn=postgres://postgres@127.0.0.1:55444/postgres -mode=steady -stages=3-partial-index,4-async-commit,5-single-statement -workers=16`
- Command: `bench -dsn=postgres://postgres@127.0.0.1:55444/postgres -mode=steady -stages=6-sharded,7-batched-completion -workers=64,128`
- Settings: autovacuum=on, autovacuum_naptime=1min, fsync=on, max_connections=1200, shared_buffers=128MB, synchronous_commit=on

### Benchmark 1, simple queue (insert everything, then consume)

#### 16 claim loops

| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0-vanilla | 14 | — | 998.82 ms | 2003.47 ms | 0.99 ms | 215 | 0.2% | 484 | 1 |
| 1-skip-locked | 15 | +1% | 993.94 ms | 1970.87 ms | 1.02 ms | 217 | 0.4% | 464 | 1 |
| 2-read-committed | 16 | +9% | 994.64 ms | 1115.14 ms | 1.03 ms | 239 | 0.1% | 0 | 1 |
| 3-partial-index | 13394 | +83982% | 1.08 ms | 1.96 ms | 1.10 ms | 202567 | 0.8% | 0 | 1 |
| 4-async-commit | 14026 | +5% | 1.06 ms | 1.88 ms | 0.71 ms | 210672 | 0.1% | 0 | 1 |
| 5-single-statement | 15394 | +10% | 0.94 ms | 1.91 ms | 0.87 ms | 230183 | 0.3% | 0 | 1 |

#### 64 claim loops

| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 6-sharded | 30864 | — | 0.68 ms | 7.24 ms | 5.30 ms | 448655 | 3.1% | 0 | 1 |

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
| 5-single-statement | 16296 | +10% | 0.88 ms | 1.88 ms | 0.91 ms | 241482 | 1.2% | 0 | 1 |

#### 64 claim loops

| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 6-sharded | 34193 | — | 0.65 ms | 6.50 ms | 3.93 ms | 508067 | 0.9% | 0 | 1 |
| 7-batched-completion | 38904 | +14% | 0.53 ms | 4.99 ms | 7.25 ms | 570358 | 2.3% | 0 | 1 |

#### 128 claim loops

| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 6-sharded | 27454 | — | 1.75 ms | 18.14 ms | 10.29 ms | 406153 | 1.4% | 0 | 1 |
| 7-batched-completion | 32322 | +18% | 1.21 ms | 15.79 ms | 12.22 ms | 473588 | 2.3% | 0 | 1 |

Latency percentiles are closed-loop service times, retries included.
"Little err" is how far tasks in flight sat from throughput times mean
task duration. A small number means the run reached a steady state.
