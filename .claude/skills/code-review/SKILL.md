---
name: code-review
description: Review Go code, a diff, or a pull request against this repo's conventions. Use when asked to review code or a PR.
allowed-tools: Read, Grep, Glob, Bash
---

# Code review

Cite `file:line` and give the concrete replacement text, not a description of one.
When the shorter fix is to restructure the code rather than patch it, suggest the
restructure instead.

Skip anything the toolchain already enforces. Formatting and unused-variable
comments are noise.

Report only what you can point at. An unverified suspicion stated as a finding
costs more than it saves.

Output findings only, ranked by severity. No summary of the diff, no praise, no
preamble.

## 1. Delete code that does not earn its place — highest priority

- Cut anything adding no value to the implementation or the test.
- Do not assert things you can assume already work; test the thing under test.
- Drop a nil check on a value that was just assigned.
- Do not export what nothing outside the package uses.
- Inline a single-use constant or struct field assignment.
- No wrapper type, generic, or helper introduced for one caller.

## 2. Dependencies

- A new third-party dependency needs agreement first. Flag any `go.mod` addition
  that was not discussed, including test-only ones.
- Prefer the standard library, assertions included.
- Never take a dependency to avoid writing five lines.

## 3. Errors

- Every error is handled. Never `_ = f()` when `f` failing invalidates what follows.
- Wrap only when the wrap adds information: `fmt.Errorf("read manifest: %w", err)`.
- No panic in library code — return an error and let the caller decide.
- Validate at the boundary, not deep in the call stack.
- Use `errors.As`/`errors.Is`; never a single-value type assertion on an error.

## 4. Concurrency and data ownership

- Default to `sync.Mutex`. Reach for `RWMutex` only when reads vastly outnumber
  writes or readers hold the lock a long time.
- No I/O while holding a lock.
- Clone before releasing a lock if the caller may mutate the value. Do not return
  a pointer aliasing lock-protected state.
- Prefer immutable values over shared mutable state.
- Every `<-ch` needs a `select` with `ctx.Done()`, or it can hang forever.
- A function that does I/O, or calls one that does, takes `context.Context`
  first and passes it down. Flag a call that drops one, and flag a `_ context.Context`
  parameter. Check `ctx.Err()` where there is work worth abandoning: once per
  item in a batch, and once more before a commit. Flag a context parameter that
  no implementation reads, which claims a cancellation that does not happen.

## 5. Benchmarks

Where this repository actually breaks. Look hardest here.

- The timed region measures only the operation under study. Flag setup,
  allocation, or teardown inside it — missing `b.ResetTimer` after setup is the
  common case.
- The compiler must not eliminate the measured work. A result nothing consumes
  is a finding; sink it into a package-level variable or use the loop value.
- Flag a comparison where the two sides differ in anything but the change under
  study: data, configuration, iteration shape.
- A benchmark that reports throughput sets `b.SetBytes`; one that studies
  allocation calls `b.ReportAllocs`.
- Flag a stated conclusion drawn from a single run or an unstated variance.

## 6. Tests

- Never `time.Sleep` to order events — use channels, `sync.WaitGroup`, or polling.
- Never assert from inside a goroutine. An assertion that fires after the test
  returns panics the whole binary. Send the error to a buffered channel and assert
  on the test goroutine.
- No writes to package-level variables; tests share one process.
- A goroutine holding a precondition in place must loop until `ctx.Done()` rather
  than run once — a single attempt that fails silently leaves the waiter hanging.
- Cover failure modes, not just the happy path.
- Table-driven with named cases.
- An external test package tests a package: `package index_test` for
  `package index`. Flag an internal test that reaches nothing unexported, and
  flag an identifier exported only to let a test see it.

## 7. Comments

The default is no comment. Two kinds earn a place: a workaround with the upstream
issue that forces it, and an invariant a reader would otherwise violate. Flag
every other comment for deletion. A doc comment on an exported name is not
exempt. Go convention alone does not justify one.

Flag a comment that:

- paraphrases the identifier it documents.
- restates the signature.
- explains a pattern the reader knows, or derives what follows from it.
- gives the reason for a choice no reader would question.
- names the caller, or describes how another package uses the code. That text is
  wrong after the first refactor.
- labels a section or narrates the change to the reviewer.

If a better name removes the need for the comment, give the name. No metaphor
and no counterfactual. Plain sentences. Several short ones beat one built from
stacked clauses.

## 8. Naming

- No `Get` prefix on getters, no `Impl` suffix.
- No stutter: `Status` in package `index`, not `IndexStatus`.
- `TestFoo`, not `Test_Foo`. `BenchmarkFoo`, not `Benchmark_Foo`.
