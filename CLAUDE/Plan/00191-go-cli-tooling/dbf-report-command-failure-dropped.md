# Defence Before Fix report: project_command_failure_dropped

Plan 00191 Task 4.9. The defence was committed red in `e47fc3a04` and widened to wrappers,
still red, in `010158bcd`. The origin was fixed in `62ccb796c` (CCY 3.104.1). The sweep is
five batches, merged in `31c8dc3b7`, `00ae4be6d`, `74423dadf`, `7e569bd86` and `4aca4eaad`.
The specs and the catalogue are in `2afcdb0af`. A false positive was fixed in the rule in
`83772ce1c` and the identifier added to its message after the conformance review. Full QA was
green with the rule blocking at `2b8b79917`, which is on F44.

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
- **Message.** "a command's failure is only tested, never carried
  (project_command_failure_dropped): put its error (exit code, stderr) in what the caller
  sees, or return it". The identifier is in the text, as the sibling rules have it, because
  the vet driver prints no analyzer name; it was missing until the conformance review.
- **Documentation.** `CLAUDE/GoProjectRules.md#project_command_failure_dropped`.
- **Width.** Any call of a function or method of `internal/execx` or `qa/internal/proc` that
  takes a `Cmd`, plus, through facts, any function that returns such a call's error, so a
  wrapper is judged as the seam is. The call is flagged when its error is only tested before
  the variable is next assigned. Carrying is any other use: returning it, passing it to a
  call, calling a method on it, or storing it.
- **Excluded, as not carrying the hazard.** `_test.go` files, which report through the test
  framework, and the seam packages themselves, whose job is to return the error. A run whose
  call is a statement, or whose error goes to the blank identifier, is not excluded: the rule
  reports it (it carries nothing), and errcheck with `check-blank` reports it too.
- **The next wider rule, not built.** Any error, not only a command's, that is tested and
  then replaced by a fixed message. Most such errors are sentinels compared on purpose
  (`errors.Is(err, fs.ErrNotExist)`), and telling a dropped cause from a decided branch
  needs the meaning of each sentinel. That judgement is a cost the owner may weigh
  differently, so it is referred below. A second wider rule, the same class in bash, is also
  not built, and also referred.

## Proof

- **Red, fixture.** The fixture is `src-go/qa/rules/testdata/src/fix/internal/badfailure`.
  `badfailure.go` holds the originating curl read and the tested-only forms, `wrapped.go` a
  wrapper that returns the seam's error, and `carried.go` each way of carrying, which must
  stay clean. It runs over `fix/internal/execx` and `fix/internal/wrapper`, through
  `go test ./rules -run CommandFailure`. The red proof survives on its own in `e47fc3a04`,
  and the wrapper widening in `010158bcd`, both before any instance was fixed.
- **Red, real tree,** as the conformance reviewer reproduced it: the driver reports 43
  findings at `e47fc3a04`, and 108 at `010158bcd` (106 in `src-go`, 2 in `src-go/qa`).
  An earlier draft said 107.
- **Green:** the driver reports nothing in either module at `2b8b79917`.

## Independent search

The searcher was dispatched before the rule was written and was told to keep out of the rule
and fixture directories. Its report is `subagent-reports/261029-dbf-command-failure-search.md`.
That file was committed after the rule commit, so its independence rests on the dispatch,
not on commit order. The reviewer found the searcher read three lines of other rules'
fixture files, none of this rule's.

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
- **Count.** 108 findings at the widened red commit, plus 3 that appeared during the sweep
  as helpers became wrappers, for 111 in all. No narrowing was applied. The scope is my
  decision for the project, which recorded none. It is the scope the `go` gate already runs.
- **The other language, bash, is not covered by the rule.** The search found 9 instances
  present in the launcher's bash (its B1 to B9) and about 12 borderline entries. The
  project's bash detectors are semgrep and the Go gates over the `mvdan.cc/sh` syntax tree.
  No bash rule was attempted. The reason given here is a judgement, not a tested result:
  the search needed the surrounding message to tell a documented probe from a guessed cause.
  B1 is the origin and is fixed. B2 to B9 and the borderline entries are referred below.

## Fixes

- All 111 Go findings are fixed, in five batches. The reports are
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
- **A departure from the method's order.** The origin was fixed (`62ccb796c`) before the
  sweep began (`31c8dc3b7`), not last. It was the owner's live incident on every ccy launch,
  and it was fixed after the rule was red, with its own reproducing test.
- **A false positive found after the sweep, and fixed in the rule.** New code (vmtest's orphan
  removal, Plan 00192 Task 3.17) wrote `err = errors.Join(err, release())`. The rule ended
  an error's window at the start of the assignment that replaces it, so this read was missed
  and the code was reported. The fixture's `joinedIntoItself` was seen red, then the rule was
  fixed: a use on the right of the next assignment counts as carrying (`83772ce1c`). That
  makes the rule narrower only where the error is read, so no instance of the hazard
  escapes.

## Decisions referred to the owner

The owner is the repository's single maintainer. These are put to them in the session's
decisions list.

- **The bash instances B2 to B9** of the independent search. 1 is fixed (B1, the origin)
  and 8 remain. Each one is a small change once its file may grow:
  - B2: `lib/token-management.bash` `validate_token`. Any engine failure is "token invalid".
  - B3: `claude-yolo` network check. A failed `alpine wget` run is "no internet access".
  - B4: `entrypoint.sh`. A failed GitHub meta fetch is "offline?".
  - B5: `claude-yolo` version probes. The captured cause is dropped as "offline?".
  - B6: `claude-yolo` `network rm`. Any failure is "may still have containers".
  - B7: `lib/docker-health.bash` stops. Any failure is "may already be stopped".
  - B8: `claude-yolo` config `source`. A failure deletes the file on the guess that it is
    corrupt.
  - B9: `lib/docker-health.bash` listings. An engine failure is "no containers found".
- **The search's borderline bash entries** (about 12, listed in its report) need the same
  decision. Some are documented probes, and none has been judged one by one.
- **Leaving the bash class without a rule**, and **not building the wider any-error rule**:
  both are the owner's to accept.
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
at `2b8b79917` with the rule blocking, through the project's own entry point: "go: coverage
passed", "QA passed: 1768 files checked". The message change after the review was checked
with the rule's harness and `qa-changed.bash`.

## Conformance review

`defence-before-fix:conformance-reviewer` reproduced the red and green runs: 108 findings at
`010158bcd`, none at `2b8b79917`, and the harness passes. All seven of its findings are
resolved above:
- the identifier in the message;
- the departure in the order;
- the per-instance bash referral, with no bash rule attempted;
- the wider rules referred;
- the exclusions corrected;
- the counts and the green commit;
- the search's independence.

The full review is in the session's agent reports.

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
`scripts/test-ccy-token-usage-command.bash`, both changed in the fix's commit.
