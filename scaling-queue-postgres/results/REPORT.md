# scaling-queue-postgres benchmark report

## Environment: darwin/arm64, 16 CPUs

- Server: PostgreSQL 18.6 (Homebrew) on aarch64-apple-darwin25.6.0, compiled by Apple clang version 21.0.0 (clang-2100.1.1.101), 64-bit
- Command: `bench -dsn=postgres://postgres@/postgres?host=/tmp&port=55444 -stages=5-single-statement -workers=16 -producers=8 -enqueue-batch=100 -hold=5s`
- Command: `bench -dsn=postgres://postgres@/postgres?host=/tmp&port=55444 -stages=6-sharded -workers=16 -shards=16 -producers=8 -enqueue-batch=100 -hold=5s`
- Command: `bench -dsn=postgres://postgres@/postgres?host=/tmp&port=55444 -stages=6-sharded -workers=32 -shards=32 -producers=8 -enqueue-batch=100 -hold=5s`
- Command: `bench -dsn=postgres://postgres@/postgres?host=/tmp&port=55444 -stages=6-sharded -workers=64 -shards=64 -producers=8 -enqueue-batch=100 -hold=5s`
- Command: `bench -dsn=postgres://postgres@127.0.0.1:55444/postgres -stages=3-partial-index,4-async-commit,5-single-statement -workers=16,32,64 -hold=5s`
- Command: `bench -dsn=postgres://postgres@127.0.0.1:55444/postgres -stages=5-single-statement -workers=8,16,24 -producers=8 -enqueue-batch=100 -hold=5s`
- Settings: autovacuum=on, autovacuum_naptime=1min, fsync=on, max_connections=200, shared_buffers=128MB, synchronous_commit=on

### 8 workers

| stage | tasks/s | vs prev | p50 ms | p95 ms | p99 ms | retries | empty | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 5-single-statement (hold 5s) | 52097 | — | 1.50 | 2.60 | 4.19 | 0 | 0 | 1 |

### 16 workers

| stage | tasks/s | vs prev | p50 ms | p95 ms | p99 ms | retries | empty | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 3-partial-index (hold 5s) | 49465 | — | 2.62 | 4.31 | 6.28 | 0 | 0 | 1 |
| 4-async-commit (hold 5s) | 53328 | +8% | 2.78 | 5.23 | 8.17 | 0 | 0 | 1 |
| 5-single-statement (hold 5s) | 58556 | +10% | 2.51 | 4.82 | 8.13 | 0 | 0 | 3 |
| 6-sharded (shards 16) (hold 5s) | 68484 | +17% | 0.79 | 19.54 | 26.53 | 0 | 8616 | 1 |

### 24 workers

| stage | tasks/s | vs prev | p50 ms | p95 ms | p99 ms | retries | empty | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 5-single-statement (hold 5s) | 53987 | — | 3.05 | 9.10 | 28.87 | 0 | 0 | 1 |

### 32 workers

| stage | tasks/s | vs prev | p50 ms | p95 ms | p99 ms | retries | empty | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 3-partial-index (hold 5s) | 32610 | — | 3.58 | 35.73 | 87.29 | 0 | 0 | 1 |
| 4-async-commit (hold 5s) | 41578 | +27% | 3.84 | 19.83 | 109.42 | 0 | 0 | 1 |
| 5-single-statement (hold 5s) | 45522 | +9% | 3.37 | 16.62 | 116.36 | 0 | 0 | 1 |
| 6-sharded (shards 32) (hold 5s) | 165700 | +264% | 0.88 | 3.79 | 8.18 | 0 | 72361 | 1 |

### 64 workers

| stage | tasks/s | vs prev | p50 ms | p95 ms | p99 ms | retries | empty | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 3-partial-index (hold 5s) | invalid: workers saw an empty queue during the window | | | | | | | 1 |
| 4-async-commit (hold 5s) | invalid: workers saw an empty queue during the window | | | | | | | 1 |
| 5-single-statement (hold 5s) | invalid: workers saw an empty queue during the window | | | | | | | 1 |
| 6-sharded (shards 64) (hold 5s) | invalid: empty polls exceeded 5% of dequeue attempts | | | | | | | 1 |

## Environment: darwin/arm64, 16 CPUs, 4 VM CPUs

- Server: PostgreSQL 18.6 (Debian 18.6-1.pgdg13+2) on aarch64-unknown-linux-gnu, compiled by gcc (Debian 14.2.0-19) 14.2.0, 64-bit
- Image: docker.io/library/postgres:18 (sha256:772ab753f714afefc07b096906b4961e2bb576938c7d007beaa9b62d80680c48)
- Command: `bench`
- Settings: autovacuum=on, autovacuum_naptime=1min, fsync=on, max_connections=200, shared_buffers=128MB, synchronous_commit=on

### 4 workers

| stage | tasks/s | vs prev | p50 ms | p95 ms | p99 ms | retries | empty | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0-vanilla | 796 | — | 12.40 | 17.25 | 1331.13 | 5446 | 0 | 1 |
| 1-skip-locked | 1194 | +50% | 14.01 | 19.41 | 621.04 | 4853 | 0 | 1 |
| 2-read-committed | 2062 | +73% | 19.24 | 24.41 | 26.31 | 0 | 0 | 1 |
| 3-partial-index | 20882 | +912% | 1.29 | 1.99 | 2.47 | 0 | 0 | 1 |

### 16 workers

| stage | tasks/s | vs prev | p50 ms | p95 ms | p99 ms | retries | empty | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0-vanilla | 691 | — | 14.19 | 1587.60 | 4094.92 | 25017 | 0 | 1 |
| 1-skip-locked | 790 | +14% | 18.49 | 1278.96 | 2791.16 | 27618 | 0 | 1 |
| 2-read-committed | 2158 | +173% | 73.52 | 97.98 | 109.31 | 0 | 0 | 1 |
| 3-partial-index | 25783 | +1095% | 3.69 | 6.97 | 9.52 | 0 | 0 | 1 |

### 64 workers

| stage | tasks/s | vs prev | p50 ms | p95 ms | p99 ms | retries | empty | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0-vanilla | 214 | — | 110.15 | 14868.48 | 24033.79 | 45899 | 0 | 1 |
| 1-skip-locked | 385 | +80% | 568.79 | 6647.90 | 11715.63 | 40398 | 0 | 1 |
| 2-read-committed | 2121 | +451% | 284.10 | 419.38 | 484.11 | 0 | 0 | 1 |
| 3-partial-index | 22162 | +945% | 10.23 | 41.36 | 71.48 | 0 | 0 | 1 |

## Environment: darwin/arm64, 16 CPUs, 8 VM CPUs

- Server: PostgreSQL 18.6 (Debian 18.6-1.pgdg13+2) on aarch64-unknown-linux-gnu, compiled by gcc (Debian 14.2.0-19) 14.2.0, 64-bit
- Image: docker.io/library/postgres:18 (sha256:772ab753f714afefc07b096906b4961e2bb576938c7d007beaa9b62d80680c48)
- Command: `bench -stages=3-partial-index -workers=16,32,64`
- Command: `bench -stages=4-async-commit,5-single-statement -workers=16,32,64`
- Settings: autovacuum=on, autovacuum_naptime=1min, fsync=on, max_connections=200, shared_buffers=128MB, synchronous_commit=on

### 16 workers

| stage | tasks/s | vs prev | p50 ms | p95 ms | p99 ms | retries | empty | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 3-partial-index | 28548 | — | 3.28 | 5.48 | 7.44 | 0 | 0 | 1 |
| 4-async-commit | 32135 | +13% | 3.20 | 5.52 | 6.92 | 0 | 0 | 1 |
| 5-single-statement | 40122 | +25% | 2.43 | 5.40 | 8.07 | 0 | 0 | 1 |

### 32 workers

| stage | tasks/s | vs prev | p50 ms | p95 ms | p99 ms | retries | empty | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 3-partial-index | 31510 | — | 5.32 | 10.95 | 16.24 | 0 | 0 | 1 |
| 4-async-commit | 33149 | +5% | 5.30 | 11.93 | 18.64 | 0 | 0 | 1 |
| 5-single-statement | 38601 | +16% | 3.92 | 12.71 | 20.38 | 0 | 0 | 1 |

### 64 workers

| stage | tasks/s | vs prev | p50 ms | p95 ms | p99 ms | retries | empty | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 3-partial-index | 29767 | — | 7.85 | 28.96 | 48.46 | 0 | 0 | 1 |
| 4-async-commit | 29913 | +0% | 7.23 | 31.67 | 55.04 | 0 | 0 | 1 |
| 5-single-statement | 33247 | +11% | 4.43 | 38.35 | 84.54 | 0 | 0 | 1 |

Latency percentiles are closed-loop service times, retries included.
