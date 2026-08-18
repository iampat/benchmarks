---
name: plan-feature
description: Plan a new benchmark, harness change, or design. Use when starting non-trivial work — before writing code — to surface unknowns, failure modes, and trade-offs while they are still cheap to fix.
---

# Planning a feature

The cheapest place to find an unknown is before any code is written. Work through
this before implementing; share the result concisely with the user (or as a design
note in `docs/design/` if the scope warrants one). Lead with the riskiest unknown
and the decisions that need input. Do not narrate the parts that are obvious.

## 1. Surface unknowns first

- Do a blindspot pass: what parts of this task touch areas neither you nor the
  request has pinned down? Name them explicitly rather than silently picking.
- Translate vague terms in the request into precise ones ("fast" → target
  latency/throughput; "representative" → which workload, which data shape,
  which scale).
- Workload definitions, data formats, and result schemas are the most likely
  tweaking points — flag them and keep them cheap to change.
- When two designs are genuinely competitive, sketch both briefly and compare
  instead of committing to the first.

## 2. Plan

1. Break the work into small, independently verifiable tasks.
2. Outline the code: package layout, key types, function names and purposes.
3. List the specific test cases you will write — happy path **and** failure modes
   (empty input, oversized input, interrupted runs, retries, context
   cancellation).
4. State the error-handling strategy per failure scenario.
5. State trade-offs: run time, noise sensitivity, complexity, portability.

## 3. Interrogate measurement validity

For a benchmark this is the part most likely to be wrong — answer explicitly:

- What could make the number a lie? Dead-code elimination, warm caches, setup
  cost inside the timed region, a workload smaller than the effect under study.
- Is the comparison fair? Same hardware, same data, same configuration on both
  sides. Name anything that differs.
- How is noise handled? State the iteration count, what is reported (median,
  mean, percentiles), and when a difference counts as real.
- Can someone else reproduce the result? Record the environment and the exact
  command alongside the numbers.

## 4. While implementing

- Keep a running note of where reality diverged from the plan; fold it back into
  the design note so the next task starts smarter.
- When an unknown forces a conservative choice mid-implementation, record *why*
  (a `CONSIDER(ali):` comment or a line in the design note).
