# scaling-queue-postgres benchmark report

## Environment: darwin/arm64, 16 CPUs

- Server: PostgreSQL 18.6 (Homebrew) on aarch64-apple-darwin25.6.0, compiled by Apple clang version 21.0.0 (clang-2100.1.1.101), 64-bit
- Command: `bench -mode=ops -step=9 -workers=16,32,64 -skip-recorded -dsn=postgres://postgres@127.0.0.1:55444/postgres`
- Command: `bench -mode=steady -step=9 -workers=16,32,64 -skip-recorded -dsn=postgres://postgres@127.0.0.1:55444/postgres`
- Settings: autovacuum=on, autovacuum_naptime=1min, fsync=on, max_connections=1200, shared_buffers=128MB, synchronous_commit=on

### Queue operations (enqueue and claim, one row per statement)

#### 16 claim loops

| stage | enqueue/s | dequeue/s | operations/s | mean queue | empty claims | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 6-sharded | 39995 | 39995 | 79990 | 30 | 2288176 | 1 |

#### 32 claim loops

| stage | enqueue/s | dequeue/s | operations/s | mean queue | empty claims | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 6-sharded | 39363 | 39363 | 78726 | 778 | 7136009 | 1 |

#### 64 claim loops

| stage | enqueue/s | dequeue/s | operations/s | mean queue | empty claims | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 6-sharded | 25981 | 25981 | 51961 | 17 | 14257402 | 1 |

### The task queue (insert, claim, and complete together)

#### 16 claim loops

| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 6-sharded | 35935 | — | 0.31 ms | 1.14 ms | 1.29 ms | 534607 | 0.8% | 0 | 1 |

#### 32 claim loops

| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 6-sharded | 37676 | — | 0.33 ms | 2.26 ms | 1.69 ms | 563646 | 0.3% | 0 | 1 |

#### 64 claim loops

| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 6-sharded | 34689 | — | 0.66 ms | 6.72 ms | 4.22 ms | 510625 | 1.9% | 0 | 1 |

## Environment: darwin/arm64, 16 CPUs, 2 VM CPUs

- Server: PostgreSQL 18.6 (Debian 18.6-1.pgdg13+2) on aarch64-unknown-linux-gnu, compiled by gcc (Debian 14.2.0-19) 14.2.0, 64-bit
- Image: docker.io/library/postgres:18 (sha256:772ab753f714afefc07b096906b4961e2bb576938c7d007beaa9b62d80680c48)
- Command: `bench -mode=ops -step=0 -workers=4,8,16 -skip-recorded -ops-queue-target=10000`
- Command: `bench -mode=ops -step=1 -workers=4,8,16 -skip-recorded -ops-queue-target=10000`
- Command: `bench -mode=ops -step=2 -workers=4,8,16 -skip-recorded -ops-queue-target=10000`
- Command: `bench -mode=ops -step=3 -workers=16,32,64 -skip-recorded`
- Command: `bench -mode=ops -step=4 -workers=16,32,64 -skip-recorded`
- Command: `bench -mode=ops -step=5 -workers=16,32,64 -skip-recorded`
- Command: `bench -mode=ops -step=6 -workers=16,32,64 -skip-recorded`
- Command: `bench -mode=steady -step=0 -workers=4,8,16 -skip-recorded`
- Command: `bench -mode=steady -step=1 -workers=4,8,16 -skip-recorded`
- Command: `bench -mode=steady -step=2 -workers=4,8,16 -skip-recorded`
- Command: `bench -mode=steady -step=3 -workers=16,32,64 -skip-recorded`
- Command: `bench -mode=steady -step=4 -workers=16,32,64 -skip-recorded`
- Command: `bench -mode=steady -step=5 -workers=16,32,64 -skip-recorded`
- Command: `bench -mode=steady -step=6 -workers=16,32,64 -skip-recorded`
- Settings: autovacuum=on, autovacuum_naptime=1min, fsync=on, max_connections=400, shared_buffers=128MB, synchronous_commit=on

### Queue operations (enqueue and claim, one row per statement)

#### 4 claim loops

| stage | enqueue/s | dequeue/s | operations/s | mean queue | empty claims | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 0-vanilla | 138 | 138 | 276 | 19310 | 0 | 1 |
| 1-skip-locked | 147 | 146 | 293 | 19266 | 0 | 1 |
| 2-read-committed | 239 | 238 | 477 | 18813 | 0 | 1 |

#### 8 claim loops

| stage | enqueue/s | dequeue/s | operations/s | mean queue | empty claims | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 0-vanilla | 125 | 125 | 249 | 19375 | 0 | 1 |
| 1-skip-locked | 123 | 122 | 245 | 19385 | 0 | 1 |
| 2-read-committed | 238 | 237 | 474 | 18818 | 0 | 1 |

#### 16 claim loops

| stage | enqueue/s | dequeue/s | operations/s | mean queue | empty claims | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 0-vanilla | 78 | 78 | 156 | 19610 | 0 | 1 |
| 1-skip-locked | 79 | 78 | 156 | 19612 | 0 | 1 |
| 2-read-committed | 238 | 236 | 474 | 18820 | 0 | 1 |
| 3-partial-index | 2689 | 2689 | 5378 | 6 | 951081 | 1 |
| 4-async-commit | 5467 | 2483 | 7950 | 437269 | 725 | 1 |
| 5-single-statement | 6651 | 3841 | 10492 | 323905 | 415548 | 1 |
| 6-sharded | 7076 | 7076 | 14153 | 8 | 2231601 | 1 |

#### 32 claim loops

| stage | enqueue/s | dequeue/s | operations/s | mean queue | empty claims | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 3-partial-index | 1524 | 1524 | 3048 | 5 | 1724480 | 1 |
| 4-async-commit | 2564 | 2564 | 5129 | 10 | 951602 | 1 |
| 5-single-statement | 3193 | 3191 | 6384 | 26 | 2057271 | 1 |
| 6-sharded | 4057 | 4057 | 8114 | 5 | 4599875 | 1 |

#### 64 claim loops

| stage | enqueue/s | dequeue/s | operations/s | mean queue | empty claims | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 3-partial-index | 993 | 993 | 1986 | 11 | 1910631 | 1 |
| 4-async-commit | 1373 | 1373 | 2746 | 7 | 1621749 | 1 |
| 5-single-statement | 1902 | 1902 | 3803 | 3 | 4483559 | 1 |
| 6-sharded | 1290 | 1290 | 2579 | 4 | 6520400 | 1 |

### The task queue (insert, claim, and complete together)

#### 4 claim loops

| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0-vanilla | 7 | — | 532.13 ms | 564.90 ms | 3.04 ms | 106 | 1.3% | 99 | 1 |
| 1-skip-locked | 7 | -3% | 544.45 ms | 582.54 ms | 2.68 ms | 103 | 1.8% | 110 | 1 |
| 2-read-committed | 8 | +11% | 534.77 ms | 561.96 ms | 2.84 ms | 113 | 0.1% | 0 | 1 |

#### 8 claim loops

| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0-vanilla | 7 | — | 1007.31 ms | 1066.88 ms | 3.61 ms | 110 | 1.8% | 134 | 1 |
| 1-skip-locked | 7 | -3% | 1041.80 ms | 2052.59 ms | 3.83 ms | 106 | 1.4% | 166 | 1 |
| 2-read-committed | 8 | +13% | 1017.51 ms | 1063.99 ms | 4.14 ms | 118 | 0.4% | 0 | 1 |

#### 16 claim loops

| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0-vanilla | 7 | — | 2032.46 ms | 4107.01 ms | 57.28 ms | 103 | 2.9% | 288 | 1 |
| 1-skip-locked | 7 | -2% | 2092.45 ms | 4215.86 ms | 56.96 ms | 102 | 3.2% | 268 | 1 |
| 2-read-committed | 8 | +19% | 2047.09 ms | 2284.70 ms | 52.90 ms | 117 | 0.6% | 0 | 1 |
| 3-partial-index | 3716 | +47334% | 4.07 ms | 7.49 ms | 4.25 ms | 55767 | 0.1% | 0 | 1 |
| 4-async-commit | 3722 | +0% | 4.07 ms | 7.08 ms | 3.14 ms | 55941 | 0.2% | 0 | 1 |
| 5-single-statement | 5379 | +45% | 2.63 ms | 6.52 ms | 3.56 ms | 80718 | 0.0% | 0 | 1 |
| 6-sharded | 6850 | +27% | 1.96 ms | 5.04 ms | 4.28 ms | 102779 | 0.0% | 0 | 1 |

#### 32 claim loops

| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 3-partial-index | 3620 | — | 7.92 ms | 17.52 ms | 6.66 ms | 54348 | 0.1% | 0 | 1 |
| 4-async-commit | 3535 | -2% | 7.83 ms | 19.06 ms | 4.90 ms | 52963 | 0.1% | 0 | 1 |
| 5-single-statement | 4934 | +40% | 5.00 ms | 16.97 ms | 4.85 ms | 74024 | 0.0% | 0 | 1 |
| 6-sharded | 6841 | +39% | 3.90 ms | 10.80 ms | 6.45 ms | 102916 | 0.3% | 0 | 1 |

#### 64 claim loops

| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 3-partial-index | 2987 | — | 15.44 ms | 58.70 ms | 8.99 ms | 44774 | 0.1% | 0 | 1 |
| 4-async-commit | 2866 | -4% | 14.62 ms | 68.30 ms | 7.10 ms | 42788 | 0.5% | 0 | 1 |
| 5-single-statement | 4153 | +45% | 8.96 ms | 53.82 ms | 7.07 ms | 62184 | 0.2% | 0 | 1 |
| 6-sharded | 6441 | +55% | 7.58 ms | 26.46 ms | 11.66 ms | 96570 | 0.0% | 0 | 1 |

## Environment: darwin/arm64, 16 CPUs, 4 VM CPUs

- Server: PostgreSQL 18.6 (Debian 18.6-1.pgdg13+2) on aarch64-unknown-linux-gnu, compiled by gcc (Debian 14.2.0-19) 14.2.0, 64-bit
- Image: docker.io/library/postgres:18 (sha256:772ab753f714afefc07b096906b4961e2bb576938c7d007beaa9b62d80680c48)
- Command: `bench -mode=ops -step=7 -workers=16,32,64 -skip-recorded`
- Command: `bench -mode=steady -step=7 -workers=16,32,64 -skip-recorded`
- Settings: autovacuum=on, autovacuum_naptime=1min, fsync=on, max_connections=400, shared_buffers=128MB, synchronous_commit=on

### Queue operations (enqueue and claim, one row per statement)

#### 16 claim loops

| stage | enqueue/s | dequeue/s | operations/s | mean queue | empty claims | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 6-sharded | 7809 | 7809 | 15618 | 7 | 2311570 | 1 |

#### 32 claim loops

| stage | enqueue/s | dequeue/s | operations/s | mean queue | empty claims | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 6-sharded | 4628 | 4628 | 9256 | 5 | 4398021 | 1 |

#### 64 claim loops

| stage | enqueue/s | dequeue/s | operations/s | mean queue | empty claims | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 6-sharded | 1930 | 1930 | 3859 | 4 | 5813417 | 1 |

### The task queue (insert, claim, and complete together)

#### 16 claim loops

| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 6-sharded | 12257 | — | 1.05 ms | 2.87 ms | 3.51 ms | 184209 | 0.2% | 0 | 1 |

#### 32 claim loops

| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 6-sharded | 12428 | — | 2.13 ms | 5.47 ms | 5.02 ms | 186906 | 0.3% | 0 | 1 |

#### 64 claim loops

| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 6-sharded | 11819 | — | 4.23 ms | 14.56 ms | 9.06 ms | 173153 | 2.3% | 0 | 1 |

## Environment: darwin/arm64, 16 CPUs, 8 VM CPUs

- Server: PostgreSQL 18.6 (Debian 18.6-1.pgdg13+2) on aarch64-unknown-linux-gnu, compiled by gcc (Debian 14.2.0-19) 14.2.0, 64-bit
- Image: docker.io/library/postgres:18 (sha256:772ab753f714afefc07b096906b4961e2bb576938c7d007beaa9b62d80680c48)
- Command: `bench -mode=ops -step=8 -workers=16,32,64 -skip-recorded`
- Command: `bench -mode=steady -step=8 -workers=16,32,64 -skip-recorded`
- Settings: autovacuum=on, autovacuum_naptime=1min, fsync=on, max_connections=400, shared_buffers=128MB, synchronous_commit=on

### Queue operations (enqueue and claim, one row per statement)

#### 16 claim loops

| stage | enqueue/s | dequeue/s | operations/s | mean queue | empty claims | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 6-sharded | 7099 | 7099 | 14198 | 7 | 2249431 | 1 |

#### 32 claim loops

| stage | enqueue/s | dequeue/s | operations/s | mean queue | empty claims | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 6-sharded | 4887 | 4887 | 9773 | 5 | 4538243 | 1 |

#### 64 claim loops

| stage | enqueue/s | dequeue/s | operations/s | mean queue | empty claims | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 6-sharded | 2334 | 2334 | 4667 | 4 | 6560844 | 1 |

### The task queue (insert, claim, and complete together)

#### 16 claim loops

| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 6-sharded | 13255 | — | 0.92 ms | 2.59 ms | 3.40 ms | 199513 | 0.3% | 0 | 1 |

#### 32 claim loops

| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 6-sharded | 14200 | — | 2.04 ms | 4.20 ms | 4.64 ms | 214114 | 0.5% | 0 | 1 |

#### 64 claim loops

| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 6-sharded | 14284 | — | 3.67 ms | 10.05 ms | 10.29 ms | 214385 | 0.1% | 0 | 1 |

Latency percentiles are closed-loop service times, retries included.
"Little err" is how far tasks in flight sat from throughput times mean
task duration. A small number means the run reached a steady state.
