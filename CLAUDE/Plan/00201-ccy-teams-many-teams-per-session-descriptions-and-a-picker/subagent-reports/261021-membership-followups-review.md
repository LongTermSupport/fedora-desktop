Verdict: MERGE

Review of `git diff 922fbf79...HEAD` on fix/membership-followups (c35257b6, 9f421c23).
Tests run: `go test ./internal/teams/hot/ ./internal/teams/membership/` green; mutants run
with `go test -overlay` from `untracked/scratch/mut/`; base tree extracted with `git archive`
into `untracked/scratch/base/` for the coverage comparison. No source edited.

## Claim 1: the report read error is no longer dropped, ErrNotExist stays quiet

Holds. Every caller of `membership.ReadReport` (src-go/internal/teams/membership/report.go:80):

- hot/list.go:176-183: ErrNotExist is set to nil, anything else reaches `stateWord`
  (list.go:231-232), after the processes/unguarded/seats cases and before down/unheld. Correct
  priority.
- hot/role.go:257 `waitTold`: a non-ENOENT error is said on timeout or session end
  (role.go:268-272).
- hot/add.go:430 `relaunch`: a non-ENOENT error fails right away (add.go:431-435). The keeper
  writes the report atomically (helpers/pingbus/membership.py:400-402, temp file then rename),
  so the new keeper cannot leave a torn read that would trip this.
- hot/add.go:387 `waitHeld` and add.go:466 `alreadyIn`: these were not changed and do not need
  to be. `waitHeld` already says `repErr` through `whyNot` (add.go:406-409) on timeout.
  `alreadyIn` sends any `repErr` into `waitHeld`. Nothing is silently dropped.

There are no other callers outside tests. The statusline reads the desired file, not the report.

Non-blocking:

- N1. `alreadyIn` (add.go:467) with a corrupt report waits the full `Waits.Apply` before
  saying so, when it could fail at once the way `relaunch` now does. This is not silent, and the
  behaviour was there before this diff.
- N2. In `waitHeld`/`whyNot`, an ENOENT report is still printed as the raw open error ("the
  seat keeper's report P: open P: no such file or directory"). That is accurate but not
  friendly, and it was there before this diff.

## Claim 2: writeDesired can only be reached with a validated value

Holds. `checked` is unexported (desired.go:181). The only non-zero value comes from `validate`
(desired.go:184-189). The zero `checked{}` is returned only together with a non-nil error
(membership.go:185-201, desired.go:186), and both callers check that error first (desired.go:172,
membership.go:147). Both disk writers go through it: `Write` (desired.go:171-176) and the
launcher `Init.Run` (membership.go:161). `hot.(*Hot).writeDesired` (hot/hot.go:588-593) goes
through `membership.Write`, so it is validated. No other code writes `membership.json`: the
only `fsx.WriteAtomic` of `File` is membership.go:233, and the relaunch and restart files are
different files.

Non-blocking:

- N3. The AST test (membership_test.go:239-258) checks only that `writeDesired`'s second
  parameter is spelled `checked`. It is not vacuous: reverting the signature to `Desired` fails
  it. But it would not catch a new `checked{d}` literal elsewhere in the package. That literal
  is the one remaining way around the check inside the package. A second `ast.Inspect` that
  allows `checked{...}` composite literals only inside `validate` (and `checked{}` with no
  fields) would close it. The tail of the test (a valid launch exits 0) proves nothing about
  the claim.
- N4. `checked{d}` shares slice backing arrays with the caller's `Desired`, so code in the
  package could change `d.Seats[i]` between `validate` and `writeDesired`. No current caller
  does this (`Write` hands the value straight on, and hot clones its slices). Theoretical only.

## Claim 3: F6 strictness not loosened

Holds. `checkWrite` (desired.go:195-203) and `check` are unchanged: UTF-8, control characters,
clean absolute path, session/seat/team grammar, `V`, generation and `written_ms`. The
`checkEntries` split (desired.go:128-131) keeps both limits and only separates the messages.
No test, testdata or Python file matched the old combined text ("seats or ... farewells").
`Init.desired` still runs `ParseSeats` and the clean-absolute check before `validate`.

## Claim 4: tests fail when the fix is reverted

Mutants (overlay of one file each, `go test ./internal/teams/hot/`):

| Mutant                                                        | Result                                                          |
| ------------------------------------------------------------- | --------------------------------------------------------------- |
| list.go: drop the `reportErr` case of `stateWord`             | killed (TestListSaysWhenTheKeepersReportCannotBeRead)           |
| list.go: stop treating ErrNotExist as quiet                   | killed (TestListIsQuietAboutAReportTheKeeperHasNotWrittenYet)   |
| add.go relaunch: disable the new check                        | killed (TestAddWhoseRelaunchedSessionReportCannotBeReadSaysWhy) |
| add.go relaunch: `if repErr != nil` (ENOENT no longer exempt) | SURVIVES                                                        |
| role.go: disable the new check                                | killed (TestRoleNotToldBecauseTheReportCannotBeReadSaysWhy)     |
| role.go: ENOENT no longer exempt                              | SURVIVES                                                        |
| add.go reverted whole to 922fbf79                             | only the relaunch test fails                                    |

- N5. The ErrNotExist exemptions at add.go:431 and role.go:268 have no test. If the add.go one
  were dropped, `add` would abort the moment a relaunched session's keeper had not written its
  report yet, which is a real regression path. Suggested tests: in the relaunch test, set
  `r.m.report = "-"` and expect the "not relaunched within" path; in the role test, expect the
  "its watcher tells it" text.
- N6. `TestAddSaysWhenTheKeepersReportCannotBeRead` (add_test.go:340) passes against the base
  add.go. It pins the `waitHeld`/`whyNot` behaviour that was already there, so it is not
  evidence for this fix.
- Membership: `TestInitRefusalSaysWhatIsWrongAndWhatToDo` fails on the base, which lacks
  "Nothing was written". The AST test fails if the `checked` parameter is reverted.

## Claim 5: error text accurate and friendly

Mostly holds.

- N7. membership.go:149-150 says "see ccy-teams --help for what a session, a checkout and a
  seat list may be". The help (src-go/internal/teams/usage.txt:219-231) gives no session-name
  grammar and says nothing about the checkout rules (clean absolute path, UTF-8, no control
  characters). The pointer promises more than it gives. Either drop the clause or name the
  rules in the error itself; the error already names the failing value. "Correct that and
  launch again" fits the user who launched ccy or cc. The `--kind`/argument-count errors are
  launcher bugs, not something the user can correct, but they are rare.
- relaunch/role: "the session's report could not be read: <path and cause>; fix or remove that
  file, then run the same command again" is accurate. The cause carries the path, and removing
  the file is safe because the keeper rewrites it. In role.go:269, when `x.ended` cut the wait
  short, the text still says "not told within N s". That was there before this diff.

## qa-changed coverage 86.5% < 90

This is a diff-scope artefact and was not caused by this diff. Measuring the changed packages
with only their own tests (no `-coverpkg=./...`, unlike the full-mode figure the floor in
src-go/qa/go-qa.toml:22-26 was set against):

- base 922fbf79: hot 84.0%, membership 93.5%, total 86.0%
- head: hot 84.5%, membership 93.3%, total 86.3%

The diff raises the total. The 0% functions named in the failure (list.go:119 `table`,
lock.go:30 `Error`, role.go:20 `noUndo`) were there before this diff and are reached by other
packages' tests under `-coverpkg=./...`. The full gate is the arbiter. Its result is not
reported here because sub-agents may not run qa-all.
