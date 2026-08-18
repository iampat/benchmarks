# benchmarks

Benchmark suites and measurements. Measurement validity matters more than
feature velocity: a wrong number is worse than no number.

## Build — Bazel only

Never invoke `go build`/`go test` or run scripts directly. Go through Bazel so
the toolchain and the dependency graph stay authoritative.

| Plain Go | Here |
| --- | --- |
| `go build ./...` | `bazel build //...` |
| `go test ./...` | `bazel test //...` (race: `--config=race`) |
| `go test -run TestX ./pkg` | `bazel test //pkg:pkg_test --test_filter=TestX` |
| `go test -v` | `bazel test //... --test_output=all` |
| `go test -bench=.` | `bazel test //... --test_arg=-test.bench=. --test_output=all` |
| `go get <module>` | `bazel run @io_bazel_rules_go//go -- get <module>` |
| `go mod tidy` | `bazel mod tidy` |
| `go fmt ./...` | `bazel run //:format` (check-only: `//:format.check`) |
| `go vet ./...` | nogo, which runs inside `bazel build` |
| `go run ./cmd/x -- a b` | `bazel run //cmd/x -- a b` |
| `go run <tool>@<version>` | `bazel run @io_bazel_rules_go//go -- run <tool>@<version>` |
| anything else | `bazel run @io_bazel_rules_go//go -- <args>` |

### Useful commands

```sh
bazel run //:gazelle                           # regenerate BUILD files
bazel run //:buildifier                        # format BUILD files
rm <pkg>/BUILD.bazel && bazel run //:gazelle   # regenerate rather than merge
bazel query "somepath(//a, //b)"               # why does A depend on B
bazel build //... --verbose_failures --keep_going
```

## Gotchas & conventions

- Generated code is not ours. Protoc output lives in `bazel-bin` and never
  reaches the repository. It stays outside review, outside lint, and outside the
  comment rules.
- `BUILD.bazel` files are gazelle output. Regenerate them. Do not hand-edit.
- No new third-party Go dependencies unless we agree first — prefer stdlib,
  including for test assertions. This covers "convenience" deps like testify.
- A function that does I/O, or calls one that does, takes `context.Context` as
  its first parameter and passes it down. Check `ctx.Err()` where there is work
  worth abandoning: once per item in a batch, and once more before a commit.
  Never add a context that no implementation reads.
- An external test package tests a package: `package index_test` for
  `package index`. The test then proves the exported API is enough. Use an
  internal test only to reach something unexported, and say why in the commit.
- Structured logging via stdlib `log/slog`; prefer returning errors over
  fatal-level logging.
- Never `time.Sleep` to synchronize a test — poll or use channels.
- A benchmark records the environment and the exact command alongside its
  numbers, so someone else can reproduce the result.
- Explicit beats clever. Prefer the obvious solution a reader can follow without
  reconstructing your reasoning, even when a terser one exists.
- Default to no comment. Two kinds earn a place: a workaround with the upstream
  issue that forces it, and an invariant a reader would otherwise violate. Write
  nothing else, doc comments on exported names included. Go convention alone
  does not justify a doc comment. Delete a comment that paraphrases the
  identifier, restates the signature, explains a pattern the reader knows, gives
  the reason for an obvious choice, or names the caller. Applies to config and
  build files as much as to Go.
- Flag open design questions with `CONSIDER(ali):` comments.
- Write prose in ASD-STE100 Simplified Technical English: active voice, simple
  tenses, one idea per sentence, one term per concept. Maximum 20 words in an
  instruction, 25 in a description. No semicolons, no phrasal verbs. Spell out
  an acronym at first use. Write "for example", not "e.g.".
- Write for a working software engineer. Do not explain a language feature, a
  protocol, or a pattern that audience already knows. Explain only what is
  specific to this project.
- Replies to the user follow the same rules. Lead with the result. Add detail
  only when it changes what the reader does next. To explain structure or
  flow, prefer a small sketch — a file tree, a call tree, pseudocode, or a
  diff — over prose.
- Design notes: Markdown in `docs/design/`; update them alongside the code they
  describe.
- A branch name always starts with `ali/`. The type of the change goes in the PR
  title instead, as `feat:`, `fix:`, `chore:`, or `docs:`.
- Report the URL when you open a PR, then stop. Do not wait for the checks and
  do not poll them. I read them myself.
- Never put a Claude session link in a commit message or a PR body. Drop the
  `Claude-Session:` trailer, and drop the `https://claude.ai/code/session_...`
  URL under the generated-with line. The link opens for one person, and the
  repository keeps it forever. Keep the `Co-Authored-By:` trailer and the
  generated-with line, which name the tool without a private URL.
- Before declaring done: `bazel build //...` and `bazel run //:format.check` pass.
- Before opening a PR: run `bazel mod tidy`. A `go.mod` change leaves
  `MODULE.bazel` stale, and a stale `use_repo` call breaks the build.

For planning a new benchmark or design, use the `plan-feature` skill. For any
documentation or technical prose — `docs/`, READMEs, PR descriptions — use the
`design-note` skill.
