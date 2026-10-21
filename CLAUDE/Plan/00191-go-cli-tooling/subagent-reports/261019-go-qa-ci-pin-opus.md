# Plan 00191 Task 3.3: go-qa-ci pinned, qa-go.bash retired

Branch `feat/pin-go-qa-ci-3` (continues `feat/pin-go-qa-ci-2` and `feat/pin-go-qa-ci`), not pushed,
not merged. Task 3.3 is done. The full go-qa gate is green, coverage included.

## What the Go gate is now

`scripts/qa-go-qa.bash` runs go-qa-ci's `go-qa run --no-fix` over both modules of `src-go`.
go-qa is pinned at go-qa-ci commit `7fc4547e92f2` by the `tool` directive of `src-go/qa/go.mod`,
and configured by `src-go/qa/go-qa.toml`. `scripts/qa-go.bash` is deleted, and so is its row in
the bash-limits allowlist.

| Check                              | qa-go.bash (before)          | go-qa (after)                                        |
| ---------------------------------- | ---------------------------- | ---------------------------------------------------- |
| format                             | `golangci-lint fmt --diff`   | `fmt --no-fix` (gofumpt, goimports)                  |
| build                              | `go build ./...` per module  | `build` per module                                   |
| vet                                | `go vet`                     | golangci govet, plus the vet tool with the analyzers |
| tests                              | `go test -race`              | `go test -race -shuffle=on -count=1`                 |
| coverage                           | 90% floor per module         | 90% floor per module                                 |
| every package tested               | an awk check                 | the `project_package_tests` rule                     |
| lint                               | golangci-lint, both modules  | golangci, both modules                               |
| project rules                      | the driver over both modules | the driver plus 9 bundled analyzers, both modules    |
| dead code, vulnerabilities, go.mod | none                         | `deadcode`, `vuln`, `mod`                            |
| record, suppression                | none                         | off until the owner signs a record (Task 3.8)        |

The last full run (`scripts/qa-go-qa.bash`) passed fmt, summary, docs, golangci, analyzers,
deadcode, vuln, mod, build, test and coverage. ast-grep and mutants are skipped by configuration.
`src-go/qa/all/testdata/wording/go.txt` is now the real output, captured with
`go test ./all -run 'TestCaptureTheRealWordingOfTheDetailGates/^go$' -capture-wording`. It
replaces the hand-written placeholder, and adds the `summary:` note line and the `mutants` row.

## The bundled analyzers' findings, all fixed with no suppression

- Map-order findings: three fixed in the first commit, f3c93b6a.
- `gqaci_test_clock`: the tmuxccy test no longer reads the wall clock. It shows that the real
  sleep ran by having a 90 ms timer fire first. The project rule's clock allowance list is now
  empty.
- `gqaci_exit_outside_main`: there were 10 sites, all in the closed-stdout re-exec tests.
  - `qa/internal/cmdmaintest` now has `Main` (the body of each package's `TestMain`) and
    `Child`, and `RefusesClosedStdout` is built on `Child`.
  - Every QA command package declares a `TestMain`. A package that does not is reported, not
    silently passed: this was shown red before the `TestMain`s were added.
  - The five hand-rolled tests and `cmdmain`'s own two tests now use it. Each test keeps what it
    checks.

## The semgrep timeout (the last red step), fixed at the cause

**The finding.** `cmd/qa-patterns TestRealSemgrepScansTestsDirectoriesAndLargeScripts` failed
under host load in three agents' runs. A qa-all on F44 then aborted in the patterns gate itself
with the same error: a Timeout on `files/var/local/claude-yolo/claude-yolo` (twice) and on
`scripts/test-ccy-sessions-reboot.bash`, at load 28-32 on 22 cores.

**The cause.** semgrep limits one rule on one file to 5 s by default, parse included, and it
measures wall time. A scan starved of CPU therefore reports a clean file as a Timeout, and the
gate is right to answer that with exit 2 (SPEC-qa-patterns P14: the file was not analysed). The
gate's own 15-minute context was never involved.

**The test fixture.**

- Measured with `semgrep --time`: the test's 1.1 MB of `echo` lines took 2 to 4.5 s to parse at
  load ~28, just under the limit. The same size of comment lines took under 0.5 s.
- The fixture is now comment lines, just over semgrep's default 1,000,000-byte size limit. Size
  is the only thing that limit looks at.
- A guard fails the test if the fixture is ever not over the limit. It was red at 994,028 bytes.
- The test still proves what it was written for: with `--max-target-bytes=0` removed it fails
  ("semgrep did not scan 1 discovered shell script(s): scripts/big.bash").

**The gates.**

- Every semgrep scan now passes `semgrepreport.TimeoutFlag` (`--timeout=120`), defined once in
  `src-go/qa/internal/semgrepreport`. The gates that pass it: patterns, gh-scopes,
  ready-wait-rules, speech-to-text-rules, and qa-changed's `semgrep` check.
- Why 120 s: the largest tracked scripts take well under a second, so 120 s is far beyond any
  honest file. A runaway rule still fails on the file it stalls on, inside the whole-run
  deadline.
- Each gate's argv test requires the flag. All five were red before the code changed.
- qa-changed's golden replay drops the flag from the logged argv (`semgrepArgvAsRecorded`), so
  there is no deviation.
- The four gates pass over the real tree, and their real-semgrep command tests pass.
- The SPEC argv lines are updated: patterns P11, gh-scopes, ready-wait R10, speech-to-text and
  qa-changed 12.1.
- `--timeout-threshold` is left at its default. Any Timeout already fails the gate, so how many
  rules may time out before a file is skipped changes nothing.

**Evidence.**

- The patterns test passed `-count=5` on its own.
- It passed `-count=5` again at load 67, while `go test ./... -p 22` ran in both modules at the
  same time. Both modules' suites also passed in that run.

## Gate wiring

- **qa-all**: the go row runs the wrapper, with the reader `detail:coverage +passed`. The golden
  replay restores the recorded `qa-go.bash` row through `replacedAfterPort` and does not
  re-record the goldens. A test fails if the restored row and the current row ever become the
  same again; it was shown red first.
- **qa-changed**:
  - When every file routed to the go check is under `src-go/`, it runs the wrapper with
    `--changed --base <base>`.
  - Otherwise it runs the whole gate. go-qa's diff mode sees only `src-go/`, so a parity
    reference or a table the Go tests read, if changed, would otherwise select nothing. A
    `--files` run is also whole.
  - golangci-lint is no longer a tool this check needs.
  - The golden replay maps the new script and summary back to the recorded ones. A test fails
    once that mapping no longer changes anything.
- **toolchain gate**:
  - The Go floor is now the higher `go` directive of the two `go.mod` files. The qa module's
    1.26.9 is the higher one.
  - A new test requires the golangci-lint pin in `.qa-versions` to equal the one in go-qa-ci's
    `tools.lock`.

## Plans

- Plan 00191: Task 3.3 is done, and its Phase 1 note now says `qa-go.bash` is retired.
- Plan 00191: a new **Task 3.8**. The owner signs a go-qa project record; then `[tools] record`
  and `suppression` are switched on in `src-go/qa/go-qa.toml`.
- Plan 00191: success criterion 3 is met.
- Plan 00192: the status line and Task 2.2 are updated.
- SPEC-qa-all and SPEC-qa-changed 12.1: one paragraph each on the replaced row.
- Both plans have a journal entry.

## Targeted QA

All green:

- `scripts/test-qa-changed.bash`: 127 passed, 0 failed.
- `scripts/qa-bash-limits.bash`.
- `scripts/qa-docs.bash`.
- `scripts/qa-changed.bash`.
- `go test` of the touched packages.

qa-all was not run; that is the coordinator's.

## Follow-up: Fable's non-blocking findings (feat/pin-go-qa-ci-4)

Review: `261019-go-qa-ci-pin-review-fable.md` (verdict MERGE). Applied:

- **Finding 1**: the four `semgrep --test` self-tests (patterns, ghscopes, readywait, sttrules)
  now pass `semgrepreport.TimeoutFlag`. The argv tests were changed first and failed, then passed.
- **Finding 2**: `TestTheQaModuleLintsAgainstTheProjectGolangciConfig` (toolchain package) requires
  `src-go/qa/.golangci.yml` to exist and resolve to `src-go/.golangci.yml`; red with the link moved
  away.
- **Finding 3**: the `CLAUDE/QA.md` qa-changed row says any change under `src-go/qa` widens to a
  whole run, and that a narrowed root-module run judges the 90% floor over the changed packages'
  `-coverpkg` subset.
- **Finding 5**: not restored. go-qa-ci (pinned version) prints the coverage percentage only in
  the failure message (`statement coverage N% is below ...`); on a pass its summary says `passed`
  and no profile is kept, so the wrapper has nothing to parse without running the tests a second
  time.
- **Finding 6**: the `.qa-versions` header now says why golangci-lint is pinned (the hooks daemon
  runs it on every Go edit) and that it must equal go-qa-ci's `tools.lock`.
- **Finding 7**: the wrapper comment and the `CLAUDE/QA.md` row no longer say a gate never rewrites
  the tree; they say what `CI` does.
- **Finding 4**: left as the new Plan 00191 Task 3.9, with Fable's reasoning.
