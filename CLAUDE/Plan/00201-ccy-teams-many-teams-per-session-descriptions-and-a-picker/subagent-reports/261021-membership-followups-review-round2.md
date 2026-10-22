Verdict: MERGE

Round 2 review of commit 2df320d6 on fix/membership-followups. It answers round 1's N3, N5 and
N7 and lifts the coverage in diff mode. No source was edited. Mutants were run with
`go test -overlay` from `untracked/scratch/r2mut/`.

## (1) The ENOENT exemptions are now pinned (round 1 N5): holds

| Mutant (overlay)                                                                  | Result                                                          |
| --------------------------------------------------------------------------------- | --------------------------------------------------------------- |
| hot/role.go:268 `err != nil && !errors.Is(..ErrNotExist)` -> ENOENT is not exempt | killed: TestRoleNotToldWithNoReportYetIsTheTimeoutNotAReadError |
| hot/add.go:431 `repErr != nil && !errors.Is(..)` -> `repErr != nil`               | killed: TestAddWhoseRelaunchedSessionHasNoReportYetWaitsForIt   |

Both tests check the message as well as the exit code. Each requires the timeout text
("not told within 10 s" at role_test.go:105, "not relaunched within 180 s" at add_test.go:366)
and the absence of "could not be read". The add test removes the report only once the relaunch
file exists (the `onPs` hook). That puts the missing report on the path where the container has
changed, and only that path, which is the code under test.

## (2) TestNoCheckedMembershipIsBuiltOutsideValidate (round 1 N3): holds for literals, with a narrow gap

- The test is not vacuous. It parses every non-test `*.go` file in the package
  (membership_test.go:286). A self-check (membership_test.go:298-302) proves that the detector
  flags a forged `checked{d}` and spares `validate`.

- I checked it against the real file. I injected `func forge(d Desired) checked { return checked{d} }`
  into membership.go and ran the detector over the mutated file through an overlaid test file.
  It reported membership.go:211:40. Keyed `checked{d: d}` is also caught, because `Elts > 0`.
  Note that a plain `-overlay` mutant of membership.go does NOT make this test fail. The test
  reads source from disk with `readFile`, not from the overlay. Do not mistake that for a
  vacuous guard.

- **Gap (non-blocking):** the guard matches composite literals only. These forms get past it:

  - `var c checked; c.d = d` (field assignment on the zero value). I confirmed this one: with
    it injected into desired.go, the detector reported `[]`.
  - `checked(struct{ d Desired }{d})` (a conversion from an identical anonymous struct).
  - `type alias = checked; alias{d}`.
  - `*(*checked)(unsafe.Pointer(&d))`.

  The `validate` exemption is also matched by name alone (membership_test.go:272), so any
  method or function called `validate`, on any receiver, is exempt. Every one of these is a
  deliberate act inside a small package, and none is present today. Closing the field-assignment
  route would mean also flagging `ast.SelectorExpr` assignments to `.d` on a `checked` value
  outside validate. This is optional hardening, not a merge blocker.

## (3) The launcher refusal (round 1 N7): holds

membership.go:149 now reads `ccy-teams: internal <sub>: <err>. Nothing was written. Correct that and launch again.` The `--help` pointer is gone and nothing else in src-go or helpers still
carries the old text (grep: no match). The remaining text is accurate. The error names the
failing value, and "Nothing was written" is true because `desired()` fails before any write.

## (4) The new failure-path tests

The tests are sound and deterministic:

- The rig's clock is fake (hot_test.go:148-149: `Now` and `Sleep` advance `r.now`), so no test
  reads the wall clock.
- The tables are slices, apart from one map in membership/problems_test.go, where the order is
  irrelevant.
- `go test -count=5 -shuffle=on` on both packages: green.
- None of the tests weakens an existing one. The commit only adds tests; its one production
  change is the refusal text.
- Most cases assert the stderr wording, not only the exit code: TestAddSaysWhichStepCouldNotBeRead
  (it also asserts the membership is unchanged), TestRoleSaysWhy..., TestStateWordSaysWhatIsWrongFirst
  (it pins the full priority order), and TestListOnATerminal....

The `report` field (hot_test.go:49, used in `writeReport` at 171-180) was already in the file
and this commit does not change it. Its three modes are: "" follows the desired file, "-"
removes the report while ignoring ENOENT, and any other value writes those bytes. It is sound.

Weaker spots, all non-blocking. They exist for coverage and assert little:

- membership/problems_test.go:60,65 `TestInitFailsWhen...` checks only that the exit code is
  not 0. It checks neither the message nor what was written. In the relaunch case the
  membership file may already be on disk when the removal of `relaunch` fails, and the test
  does not say whether that is intended.
- hot/failures_test.go:98 (records unreadable) and :179 `TestRemoveAndRoleSayWhenTheTeamCannotBeRead`
  check only the exit code. The "no membership directory" case (:68) expects only the session
  name in stderr, which almost any message would contain.

## (5) The runs

- `go test -race -count=1 ./internal/teams/hot/ ./internal/teams/membership/`: both ok.
- `./scripts/qa-go-qa.bash --changed -t coverage` against HEAD: skipped, with no changed
  packages, because the tree is clean and the commit is HEAD. That run proves nothing.
- With `-base 2f82a901` (this commit alone) and with `-base F44` (the whole branch), 23 packages
  were in scope, including hot and membership: `coverage passed`, exit 0.
