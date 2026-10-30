# Defence Before Fix report: project_command_failure_dropped

Plan 00191 Task 4.9. The defence was committed red in `e47fc3a04` and widened to wrappers,
still red, in `010158bcd`. The origin was fixed in `62ccb796c` (CCY 3.104.1). The sweep is
five batches, merged in `31c8dc3b7`, `00ae4be6d`, `74423dadf`, `7e569bd86` and `4aca4eaad`.
The specs and the catalogue are in `2afcdb0af`. Full QA was green with the rule blocking at
`2afcdb0af`, and the work is on F44 at `969948482`.

## Specification followed

- Method specification 1.1.0, fetched by the dbf skill's refresh script.
- Detector specification 1.0.0 and toolchain specification 0.2.0, both fetched.

## Toolchain

The project has no `defence-before-fix` manifest declaration, and I added none. Its Go
detector is its own analyzer set, `src-go/qa/rules`, built into a `go vet` driver
(`src-go/qa/driver`). go-qa-ci runs that driver through the `go` gate
(`scripts/qa-go-qa.bash`), which is in `qa-all.bash`, `qa-changed.bash` and CI. The register
grades no tool for this, and the analyzer set declares no spec version.

- **Listing.** `All()` in `src-go/qa/rules/table.go` is the list. `CLAUDE/GoProjectRules.md`
  catalogues every entry, and its table and sections are the human-readable listing.
- **Resolution.** An identifier resolves to the `CLAUDE/GoProjectRules.md` heading of the
  same name. Here that is `CLAUDE/GoProjectRules.md#project_command_failure_dropped`.
- **Single-rule harness.** `go test ./rules -run CommandFailure` (from `src-go/qa`) runs the
  rule over its fixtures with `analysistest`. Over a tree, build the driver with
  `cd src-go/qa && go build -o <path> ./driver`, then run `go vet -vettool=<path> ./...`
  in either module.
- **Calibration.** The project records none. I assumed blocking, with no recorded
  exceptions, and the rule ships that way.

## Defect and class

- **Originating instance.** `src-go/internal/ccylaunch/tokenusage.go`, `ask()`, and its bash
  twin `_usage_fetch_one` in `files/var/local/claude-yolo/lib/token-management.bash`. Any
  failed curl (any exit code, a runner start or feed failure) was recorded as status 000 with
  no cause, shown as "could not reach the API", and cached for `CCY_USAGE_TTL` (900 s). The
  owner's incident of 2026-10-29: all five tokens showed it for 15 minutes while the network
  was fine.
- **Class.** A command's failure that is only tested, never carried. The code asks
  `err != nil` (or `errors.Is` against a sentinel) and then reports a fixed guess.
- **Hazard.** The person sees a guessed cause, and the guess can be false. The true cause
  (the exit code, stderr, the start failure) is thrown away, so nobody can tell a missing
  tool, a lost guest or an expired login from the guess.

## Rule

- **Detector.** A `go/analysis` analyzer in `src-go/qa/rules/commandfailure.go`.
- **Identifier.** `project_command_failure_dropped`.
- **Message.** "a command's failure is only tested, never carried: put its error (exit code,
  stderr) in what the caller sees, or return it".
- **Documentation.** `CLAUDE/GoProjectRules.md#project_command_failure_dropped`.
- **Width.** Any call of a function or method of `internal/execx` or `qa/internal/proc` that
  takes a `Cmd`, plus, through facts, any function that returns such a call's error, so a
  wrapper is judged as the seam is. The call is flagged when its error is only tested before
  the variable is next assigned. Carrying is any other use: returning it, passing it to a
  call, calling a method on it, or storing it.
- **Excluded, as not carrying the hazard.** `_test.go` files, which report through the test
  framework. The seam packages themselves, whose job is to return the error. A run whose
  call is a statement, or whose error goes to the blank identifier: these discard rather
  than guess, and other rules and review hold them.
- **The next wider rule, not built.** Any error, not only a command's, that is tested and
  then replaced by a fixed message. Most such errors are sentinels compared on purpose
  (`errors.Is(err, fs.ErrNotExist)`), and telling a dropped cause from a decided branch
  needs the meaning of each sentinel.

## Proof

- **Red, fixture.** The fixture is `src-go/qa/rules/testdata/src/fix/internal/badfailure`.
  `badfailure.go` holds the originating curl read and the tested-only forms, `wrapped.go` a
  wrapper that returns the seam's error, and `carried.go` each way of carrying, which must
  stay clean. It runs over `fix/internal/execx` and `fix/internal/wrapper`, through
  `go test ./rules -run CommandFailure`. The red proof survives on its own in `e47fc3a04`,
  and the wrapper widening in `010158bcd`, both before any instance was fixed.
- **Red, real tree.** The driver over both modules reported 107 findings at `010158bcd`.

## Independent search

The searcher ran before the rule existed and without sight of it. Its report is
`subagent-reports/261029-dbf-command-failure-search.md`.

- **It looked for** every way a command's failure can be tested and replaced with a guess,
  in Go and in the launcher's bash. It wrote its expected forms down before searching.
- **Text search found what reading could not have checked:** the population. That is every
  `Runner`, `Foreground`, `Exec`, `Combined` and `Capture` call site (about 330 in the main
  module, 47 in qa), and every `|| X=`, `2>/dev/null` and `if ! x=$(` form in the bash. It
  also found the files that reach the runner only indirectly.
- **Reading found what text search could not have.** It showed whether the error is used
  in the branch at all, and it found the findings behind wrappers
  (`systemd.HomeserverUnits` and `State`, `sessprobe.ProcessTable`, vmtest's
  `helperExitCode`). It found errors reduced to another type (an exit-code int, a
  `notStarted` constant, "unknown" sentinels), and it told a documented probe from a
  guessed cause in bash.
- **What the rule missed at first.** The search's Go findings behind wrappers (its items
  12 to 14 and about 25 borderline entries) were beyond the first rule, which looked only
  at direct seam calls. The rule was widened to wrappers through facts (`010158bcd`), and
  then reached them. As the sweep turned helpers into wrappers, 3 more findings appeared,
  and all were fixed.

## Sweep and count

- **Scope, recorded as the project's decision:** first-party Go in both modules (`src-go`
  and `src-go/qa`), with `vendor/` and `testdata` excluded.
- **Count.** 107 findings at the red commit, plus 3 that appeared during the sweep, for 110
  in all. No narrowing was applied.
- **The other language, bash, is not covered by the rule.** The search found 9 instances
  present in the launcher's bash (its B1 to B9) and about 12 borderline entries. The
  project's only bash detector is semgrep, and no semgrep pattern can tell a documented
  probe from a guessed cause: the search needed the surrounding message to tell them apart.
  B1 is the origin and is fixed. B2 to B9 are referred below.

## Fixes

- All 110 Go findings are fixed, in five batches. The reports are
  `subagent-reports/261030-dbf-cfd-1.md` to `-5.md`.
- **Examined individually:** every site. Each batch report lists each site, the message the
  person now sees, and its test.
- **One answer by pattern:** the probes whose failure is the answer (does the file exist,
  is the unit active). These ask `execx.ExitedNonZero(err)`: a program that ran and exited
  non-zero is the answer, and any other error is reported with its cause. I checked the
  sample by hand across all five batches. I rejected one worker's classing of vmtest's
  `run.exit` wait, which read a lost guest as "not yet"; it was fixed red first
  (`f9f1b9e88`).
- No instance was satisfied by suppression, by an exclusion, or by leaving the hazard in
  place. The specs that quote the changed messages now quote them as they are.
- **A false positive found after the sweep, and fixed in the rule.** New code (vmtest's orphan
  removal, Plan 00192 Task 3.17) wrote `err = errors.Join(err, release())`. The rule ended
  an error's window at the start of the assignment that replaces it, so this read was missed
  and the code was reported. The fixture's `joinedIntoItself` was seen red, then the rule was
  fixed: a use on the right of the next assignment counts as carrying (`83772ce1c`). That
  makes the rule narrower only where the error is read, so no instance of the hazard
  escapes.

## Decisions referred to the owner

- **The bash instances B2 to B9** of the independent search are in
  `files/var/local/claude-yolo/` (the launcher, `token-management.bash`,
  `docker-health.bash` and `entrypoint.sh`). 1 is fixed (B1, the origin) and 8 remain.
- **What stopped the fixing.** They are bash files on the bash-limits ratchet allowlist,
  which may shrink and never grow, and carrying each cause adds lines. The owner's direction
  is also to retire this bash for Go (Plan 00209 direction 4, and Plan 00191 itself).
- **What to try next.** Port the launcher paths they sit in. The Go front already has the
  rule, so a port is defended on arrival. The other route is to fix them in bash after
  raising those files' allowlist entries, which only the owner can allow.
- The Go rule is merged at full width, and nothing is baselined. The bash instances are
  outside its language, so no recorded exception was needed.

## Permanence

The rule is in `All()`, so the `go` gate's `go vet` driver runs it in `qa-all.bash`,
`qa-changed.bash` and CI, and a finding fails the gate. Full `./scripts/qa-all.bash` passed
at `2afcdb0af` with the rule blocking, through the project's own entry point: "go: coverage
passed", "QA passed: 1768 files checked".

## Toolchain and detector gaps

- There is no listing command. The list is a Go slice and a markdown catalogue, so clause
  8.5 is met by reading, not by a command.
- No detector in the project can judge the class in bash, as above.

## The original defect

Fixed in `62ccb796c` (CCY 3.104.1). The cause is now the cache entry's fourth line and part
of the reason the person sees. An entry with status 000 is never fresh, so the next launch
reads again, in Go and in the bash menu alike. The tests are
`src-go/internal/ccylaunch/tokenusage_test.go`, which reproduces a failed curl with its exit
code and stderr and shows the reason carries them and the entry is not reused, and
`scripts/test-ccy-token-usage-command.bash`. Both failed before the fix and pass after it.
