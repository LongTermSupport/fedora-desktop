# Plan 00191 Task 3.3: adversarial review of feat/pin-go-qa-ci-3 at 30b33d8d

Reviewed against F44 442143f5 (commits f3c93b6a, 1e93f265, b8626336, 01f69bce, 30b33d8d), read-only,
in a worktree checked out at 30b33d8d. The branch's own report:
`subagent-reports/261019-go-qa-ci-pin-opus.md`. go-qa-ci's source was read from the module cache at
the pinned pseudo-version, so every claim below about what go-qa does is from its code, not its docs.

## Verdict

**MERGE.** Nothing qa-go.bash checked was dropped; every check has a home in go-qa or in a project
rule, and the two checks switched off (record, suppression) are new to go-qa, were never in
qa-go.bash, and are an owner task (Task 3.8), not a cover for existing findings. The findings below
are small and none blocks the merge; 1 and 2 are worth a follow-up commit.

## What I ran (all green)

| Run                                                                                                                                                            | Result                                                                                                                                            |
| -------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| `scripts/qa-go-qa.bash`                                                                                                                                        | exit 0; fmt, summary, docs, golangci, analyzers, deadcode, vuln, mod, build, test, coverage passed; ast-grep and mutants skipped by configuration |
| `go test -count=1` over the touched qa packages (all, changed, cmd/..., internal/..., patterns, ghscopes, readywait, sttrules, rules, toolchain, driver, bash) | 43 packages ok                                                                                                                                    |
| `go test -count=1 ./internal/tmuxccy ./internal/podfreeze ./internal/registry` (src-go)                                                                        | ok                                                                                                                                                |
| `scripts/test-qa-changed.bash`                                                                                                                                 | passed: 127 failed: 0                                                                                                                             |
| `scripts/qa-bash-limits.bash`                                                                                                                                  | BASH-LIMITS-OK 467 bash files: 293 exempt, 174 checked (74 allowlisted, 100 within limits)                                                        |
| `scripts/qa-go-qa.bash --changed --base 01f69bce4` (the qa-changed path)                                                                                       | exit 0, widened to the whole project (a qa-module file changed); see finding 3                                                                    |

qa-all was not run (the coordinator's).

## 1. Nothing silently dropped: qa-go.bash's checks mapped to the new gate

From `git show 442143f5:scripts/qa-go.bash`, in its order:

| qa-go.bash check                                                          | Where it is now, and the evidence                                                                                                                                                                                                                                                                                                                                                                                                          |
| ------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| both `go.mod` files present, else exit 1                                  | `go-qa.toml` `modules = ["qa"]`: go-qa's mod, build, test and coverage runners each run in `src-go` and `src-go/qa`; a missing module errors the step (`runners.Mod`/`Build` return an error, `verdict` fails). Run output lists every step passed over both.                                                                                                                                                                              |
| `go` missing → exit 2                                                     | kept verbatim in `scripts/qa-go-qa.bash`                                                                                                                                                                                                                                                                                                                                                                                                   |
| `golangci-lint` missing → exit 2                                          | go-qa installs its own golangci-lint at `tools.lock`'s `2.14.0` (sha256 pinned in the lock, `internal/toolstore`); an install failure is a failed step, not a skip. The host binary is still asserted by the toolchain gate, and `TestTheGolangciLintPinIsTheOneGoQaCiInstalls` holds `.qa-versions` equal to `goqaci.ToolsLock`'s `[release] golangci-lint` (read from the embedded lock, so it moves with the pin).                      |
| `golangci-lint fmt --diff`, any diff fails                                | go-qa `fmt` with `--no-fix` (`internal/tools/golangci.go runFmt`: `check = !Fix && !CI`), same formatters (gofumpt, goimports) from the same `.golangci.yml`. Caveat: with `CI` set in the environment go-qa formats in place and then fails on the changed tree, so CI still fails on bad formatting but does rewrite its throwaway checkout.                                                                                             |
| `go build ./...` without test files, per module                           | `runners.Build`: `go build -o /dev/null ./...` per module, GOFLAGS minus `-mod=`, so src-go builds from `vendor/` as before                                                                                                                                                                                                                                                                                                                |
| `go vet ./...` per module                                                 | golangci `govet` with `enable-all: true` (shared config) over both modules, plus the analyzers stage (`go vet -vettool=<driver> -json ./...` per module, `internal/vet`)                                                                                                                                                                                                                                                                   |
| `go test -race -coverprofile` per module                                  | `runners.TestAndMeasure`: `go test -race -shuffle=on -count=1 -covermode=atomic -coverpkg=./... -coverprofile ./...` per module; a failed test fails `test` and `coverage` both                                                                                                                                                                                                                                                            |
| every package has tests (the awk over `no test files` / `coverage: 0.0%`) | the new `project_package_tests` rule in `src-go/qa/rules/packagetests.go`, in `All()` and the driver (driver test lists it), red/green on `testdata/src/fix/internal/{notests,withtests,externaltests}`, run by the analyzers stage over both modules. I checked the real tree: no package directory under `src-go` (vendor and testdata excluded) lacks a `_test.go`, so the stage passing is consistent with the rule firing on nothing. |
| coverage total >= 90 per module                                           | `[coverage] fail_under_statements = 90`, judged per module (`runCoverage` over `eachModuleInScope`). See finding 4 for the measurement change.                                                                                                                                                                                                                                                                                             |
| `golangci-lint run ./...` per module                                      | the golangci stage per module (`forEachModule`). See finding 2: the config is found by the new symlink `src-go/qa/.golangci.yml`, not by parent-directory search.                                                                                                                                                                                                                                                                          |
| driver built once; `go vet -vettool` over src-go and over the qa module   | `runAnalyzers`: `vet.Build` builds `./driver` in the qa module once, vets both modules; `driver.Main(rules.All()...)` registers go-qa-ci's nine bundled analyzers beside the eight rules                                                                                                                                                                                                                                                   |
| `GO-OK ...` payload line                                                  | the go-qa summary table; qa-all's reader is `detail:coverage +passed`. See finding 5: the line no longer carries the coverage percentage or package count.                                                                                                                                                                                                                                                                                 |

Added, not previously checked: `deadcode`, `vuln` (govulncheck over both modules), `mod`
(`go mod tidy -diff` and `go mod verify` per module).

**record and suppression off.** `go-qa.toml` sets `[tools] record = false`, `suppression = false`
with the reason in place, and PLAN.md Task 3.8 records the owner action (sign a record, then switch
both on). qa-go.bash had neither check, so nothing was lost. They are not hiding existing violations
of anything this repository enforced: `src-go/.golangci.yml` is unchanged on the branch, its
disabled linters and `exclusions.rules` are what the suppression detector would count, and each is
annotated in place as before. The branch's own analyzer findings (map order, the wall-clock test,
ten `os.Exit` sites) were fixed, not excused: the `clockAllowances` table is now empty, and no
`//nolint` or record entry was added.

## 2. The pin is exact and reproducible

- `src-go/qa/go.mod` requires `github.com/LongTermSupport/go-qa-ci v0.0.0-20261015133126-7fc4547e92f2`
  and declares `tool github.com/LongTermSupport/go-qa-ci/cmd/go-qa`; `go.sum` has both `h1:` lines
  for it and for the one new indirect (`go-toml/v2`). `go mod tidy -diff` is itself a step of the
  gate (`mod`) and passed, so the files are tidy.
- The qa module has no `vendor/`, so a clean checkout fetches it from the module proxy, as the
  module's other dependencies always were (CI comment in `.github/workflows/qa.yml` says so). go-qa
  then installs its external tools into its cache at `tools.lock`'s pins, each `[release]` tool
  verified against a sha256 in the lock, not against anything the release publishes. First run on a
  fresh host or CI runner therefore needs the network once; a tool it cannot install fails the step.
- CI: the `qa-all` job runs `./scripts/qa-all.bash`, so the `go` row runs in CI. `setup-go` reads
  `src-go/go.mod` (its `toolchain go1.26.9` line); the qa module's `go 1.26.9` directive is the same
  release. On this host `go version` is `go1.26.9` with `GOTOOLCHAIN=local` and the gate ran, so
  nothing beyond 1.26.9 is required. The toolchain gate now reports the higher of the two
  directives (`src-go/qa/go.mod`, 1.26.9) with the file that set it, tested in `pins_test.go`.
- Diff mode widens to the whole project on any `go.mod`, `go.sum`, `.golangci.yml`, the go-qa
  config file, the record path, or a file under the analyzer module/driver (`internal/changed/widen.go`,
  `cli/run.go`), which is what `CLAUDE/QA.md`'s qa-changed row says.

## 3. The cmdmaintest redesign keeps each test's meaning

Read in full: `cmdmaintest.go`, `cmdmaintest_test.go`, `cmdmain_test.go`, and the diffs of
`qa-bash/main_test.go`, `qa-docs/main_test.go`, `qa-python/real_test.go`,
`qa-hidden-bindings/main_test.go`, `qa-all/closedstdout_test.go`, `qa-discovery/closedstdout_test.go`.

- `Child` re-runs the test binary with `-test.run='^<name>$'` and the shell redirect, the child
  re-enters the same test, runs the closure, stores the code and `SkipNow`s; `Main` (the `TestMain`
  body) returns that code and prints a marker line the parent requires. A package that forgot its
  `TestMain` fails with "did not end through cmdmaintest.Main (no TestMain?)", so the report's
  "shown red before the TestMains were added" is the code's behaviour, not a claim.
- qa-bash and qa-python's closed-stdout tests used to pass `QA_JSON_OUT` and `--root` through the
  child's environment; now the re-entered test sets `QA_JSON_OUT` with `t.Setenv` and builds its own
  real git tree before calling `RefusesClosedStdout` with `--root`. Same assertion (exit cannot-run
  and the `ERROR: stdout is closed` line), same real tree.
- `cmdmain`'s two table-driven tests over `>&-` and `>/dev/null` became four tests, one per redirect,
  asserting the same codes and the same exact stderr strings (including the empty stderr for
  `/dev/null`).
- The 20-odd `closedstdout_test.go` files gained only the `TestMain` line.

## 4. The semgrep fixture, the flag and the deadlines

- Fixture: `14100` comment lines of 71 bytes plus the shebang and `cmd || true` is just over
  1,000,000 bytes; `semgrepDefaultMaxTargetBytes` guards it with `t.Fatalf` if it ever is not. The
  test's assertion (the discovered `scripts/big.bash` must have been scanned) is unchanged, so it
  still proves `--max-target-bytes=0` is passed; the report's red without the flag matches what the
  gate's "did not scan N discovered script(s)" path does.
- `semgrepreport.TimeoutFlag = "--timeout=120"` is passed by the five scan argvs (patterns,
  gh-scopes, ready-wait's Python scan, speech-to-text, qa-changed's semgrep) and each argv test
  requires it. The flag is semgrep's per-rule-per-file limit; the whole-run deadlines are untouched:
  `semgrepTimeout = 15 * time.Minute` in patterns, ghscopes, readywait and sttrules, and qa-changed's
  `QA_CHANGED_TIMEOUT` (30m). A real hang still fails on the whole-run deadline, so 120 s cannot
  hide one. The golden replays strip the flag (`semgrepArgvAsRecorded`) rather than re-record, with
  a rot test that the check still passes it.
- See finding 1 for the four `semgrep --test` self-test invocations that do not get the flag.

## 5. Goldens, allowlist, docs

- qa-all: the recorded `qa-go.bash` row is restored by `replacedAfterPort` in `addedgates_test.go`,
  so no golden was re-recorded and no deviation added; `TestEveryGateReplacedAfterThePortStillDiffersFromItsRecordedRow`
  fails once the rows coincide again. `testdata/wording/go.txt` is a real capture (it matches my run
  byte for byte, including go-qa's stderr `summary:` line, which qa-all merges into the capture).
- qa-changed: `goGateAsRecorded` maps the new script and summary back to the recording, with
  `TestTheGoGateMappedBackToTheRecordingStillDiffersFromIt`. `TestTheGoGateRunsGoQaOverTheSameChange`
  pins the three argv cases (narrowed, outside-src-go whole, `--files` whole). SPEC-qa-all 12.1 and
  SPEC-qa-changed 12.1 record both in prose.
- `scripts/bash-limits.allowlist` lost exactly one row (`scripts/qa-go.bash`); nothing was added or
  raised. `scripts/qa-go-qa.bash` is 7 code lines and within limits.
- `CLAUDE/QA.md` (gate row, toolchain row, the pin section, the qa-changed table), `src-go/CLAUDE.md`
  (QA section, Project rules), `CLAUDE/GoProjectRules.md` (fit with go-qa-ci, the new rule, the
  empty clock allowance), `.claude/rules/go-code.md`, `CLAUDE.md`'s rules table, SPEC-qa-toolchain
  T6/T9/T12 and PLAN 00191/00192 all describe the code as it is. No `qa-go.bash` reference remains
  outside history prose. No install-specific identifier in the added lines.

## Findings (numbered; none blocks the merge)

1. **"Every semgrep scan" is overstated: the four `semgrep --test` self-tests do not pass
   `--timeout=120`.** `patterns/semgrep.go:48`, `ghscopes/scan.go:41`, `readywait/scan.go:63`,
   `sttrules/rules.go:197` run the rule self-test against the fixture without `semgrepreport.TimeoutFlag`.
   The fixtures are tiny, so a 5 s wall-clock starve is far less likely than on the big scripts, but
   it is the same class on a loaded host and a failed self-test is exit 2 for the gate. Low; one-line
   fix each plus the argv tests, or narrow the claim in the report, PLAN and QA.md.
2. **The `src-go/qa/.golangci.yml` symlink is load-bearing and nothing pins it.** go-qa's
   `golangci.Config(dir)` looks only in the module directory; when no `.golangci.*` is there it
   writes go-qa-ci's embedded baseline and lints with that. qa-go.bash ran `golangci-lint run` from
   `src-go/qa`, whose own parent-directory search found `src-go/.golangci.yml`. Today the symlink
   makes the two modules share the project config; if it is ever deleted, the qa module is linted
   against a different ruleset with every step still "passed". Low; a test that `src-go/qa/.golangci.yml`
   resolves to `src-go/.golangci.yml` would hold it.
3. **qa-changed's narrowed run judges the 90% floor over the changed packages only.** In diff mode
   `runCoverage` passes the changed packages and their importers to `TestAndMeasure`, which sets
   `-coverpkg` to those patterns; the floor is then the coverage of that subset, not the module. A
   change inside a low-coverage package can therefore fail qa-changed's coverage step while the full
   gate passes, and the converse (a subset above 90 while the module is under) cannot pass the push
   gate because qa-all runs whole. Probe: `--changed --base 01f69bce4` ran whole, "widened:
   qa/all/testdata/wording/go.txt changed", because `[analyzers] module = "qa"` puts the entire qa
   module in go-qa's widening directories (`cli/run.go widenSources`): any change under `src-go/qa`
   is a full run, so the narrowing (and this finding) applies only to root-module changes.
   `CLAUDE/QA.md`'s qa-changed row should say that too ("everything when ... a rule source changed"
   understates it). Not a correctness gap in the gate; a possible false red before a commit, worth
   knowing when it appears.
4. **The coverage measurement changed shape.** qa-go.bash measured each package by its own tests
   (`-coverprofile` with no `-coverpkg`); go-qa measures `-coverpkg=./...` across the module, so
   statements a package's tests execute in another package now count. The floor stayed at 90 and
   passed, but the number it is compared to is a different, generally higher, number. Consider
   re-deriving the floor from the new measurement (the old GO-OK line said 94.8% / 94.4%; the new
   gate prints no percentage on a pass, see 5).
5. **The gate's pass payload lost its numbers.** `GO-OK 40 package(s) ... coverage 94.8% (floor 90%) ... qa module 10 package(s) at 94.4%` is now a table of `passed` words; qa-all's stage line
   for `go` is the matched text `coverage     passed`. Coverage drift toward the floor is no longer
   visible in a green run. go-qa's run summary is what it is; a follow-up could print the
   `go tool cover -func` total on stderr from the wrapper, or accept the loss knowingly.
6. **`.qa-versions`' header is now slightly wrong.** It says "Only tools a gate RUNS are pinned
   here", and no gate runs the host golangci-lint any more (go-qa installs its own; the host copy
   is the hooks daemon's edit-time linter). `CLAUDE/QA.md` and `play-python.yml` were updated to say
   so; the pin file's own comment was not. Doc-only.
7. **In CI, `fmt` rewrites before it fails.** go-qa's `runFmt` treats `CI=true` as "format, then
   fail if the tree changed" regardless of `--no-fix`; the wrapper's comment ("a gate never rewrites
   the tree") is true locally and false on a GitHub runner. The verdict is the same (red), the
   checkout is disposable; a comment-accuracy note only.
