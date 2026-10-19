# Plan 00191 Task 3.3: go-qa-ci pinned, qa-go.bash retired (interim report)

Branch `feat/pin-go-qa-ci-2` (continues `feat/pin-go-qa-ci`), not pushed, not merged. This report
is interim: the work stopped at the agent's context budget, and the handoff for the next agent is
`untracked/scratch/261019-a5492c2c369494521-handoff.md` in the worktree.

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
| record, suppression                | none                         | off until the owner signs a record (new Task 3.8)    |

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

## State at the stop

- **Not yet seen green.** The full go-qa run passed every detector, plus vuln, mod and build.
- **One test failure.** `go test` failed one test:
  `cmd/qa-patterns TestRealSemgrepScansTestsDirectoriesAndLargeScripts`.
  - semgrep's per-file timeout fires on its 1 MB fixture under host load (load average about 37
    on 22 cores).
  - It passes when run alone.
  - It needs a rerun on a quieter host.
- **Coverage did not run**, because a test failed.
- **Unfinished:**
  - the real `go` wording capture;
  - the plan and journal updates;
  - the targeted QA run listed in the handoff.
