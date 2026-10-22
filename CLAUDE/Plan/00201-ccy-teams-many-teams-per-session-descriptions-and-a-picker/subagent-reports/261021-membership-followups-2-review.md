# Review: membership follow-ups 2 (`git diff 217435de...HEAD`, commit 697dce6d)

Scope: `src-go/internal/teams/membership`, `src-go/internal/teams/hot`. I ran
`go test ./internal/teams/...` (all ok) and `go test -count=1` on membership and hot (ok), plus
`go vet` (clean).

## (1) `checked` cannot carry an unchecked membership to disk: sound

- `checked{d *Desired}`. Only `validate` sets `d` (`desired.go:185-190`). `writeDesired`
  (`membership.go:211-215`) calls `checked.desired()` first. That refuses a nil `d` and runs
  `checkWrite` again before anything touches the filesystem. So a zero `checked`, or one filled
  by hand in the package, cannot write a membership that fails the check.
- Aliasing: `validate` has a value receiver, so `&d` points at a copy. `Seats` and `Farewell`
  still share their backing arrays with the caller, so a later change to an element is visible.
  The re-check in `desired()` runs at write time, though, and the code is single-threaded
  between that re-check and `json.Marshal` (`membership.go:231`). No change can slip past the
  check. Not a hole.
- Other write paths: the only `fsx.WriteAtomic` of `File` is at `membership.go:235`, inside
  `writeDesired`. `Write` (`desired.go:170-177`) goes through `validate` and then
  `writeDesired`. The only caller of `membership.Write` outside the package is
  `hot.go:593` (`Hot.writeDesired`). `desired.go:270` writes `RelaunchFile`, not `File`, so it
  is out of scope.
- `Init.Run` dereferences `*ready.d` (`membership.go:152`) only after `err == nil`, and `validate`
  then always returns a non-nil `d`. No nil panic is possible.
- F6 strictness is not lost: `checkWrite` and `check` are unchanged, and the read path is
  untouched.
- Ordering: the refusal now comes before `MkdirAll`. The new test checks this
  (`membership_test.go:239-249`), so a refused value creates no directory. For `Write` nothing
  changes (it already refused before writing). For the launcher the re-check cannot fail. No
  user-visible change.
- The behavioural test `TestWriteDesiredRefusesAMembershipValidateDidNotMake`
  (`membership_test.go:234-259`) covers the zero value, a hand-filled invalid value and a
  validated value. Accepted limitation, not a finding: like the source test it replaces, it
  cannot catch a future second `WriteAtomic(File)` written outside `writeDesired`.

## (2) Failure-path tests: remaining weak or exit-code-only assertions

In the files touched by 2df320d6, these failure tests still do not check the user-visible reason.
Each passes with any failure message, or with none:

1. `src-go/internal/teams/membership/problems_test.go:24-26`
   (`TestACheckedMembershipNeedsItsSeatsAndFarewellsAndTheirLimits`): checks only `err == nil`.
   This is worse than a missing message check. `make([]Farewell, maxFarewells+1)` builds
   zero-value farewells, and those fail the per-item farewell check (`desired.go:143-146`)
   anyway. Delete the `len(d.Farewell) > maxFarewells` limit at `desired.go:130` and this test
   still passes. Fix: assert `"more than 256 farewells"` and the nil-list message (`desired.go:126-127`).
2. `src-go/internal/teams/hot/failures_test.go:136` (`TestAddSaysWhenNoSeatCanBeChosen`): asserts
   only the prefix `"seat: "`. It should also assert the injected `"no seats"`.
3. `src-go/internal/teams/hot/failures_test.go:146` (`TestAChangeToACCSessionSaysWhenTmuxCannotBeAsked`):
   asserts only the prefix `"session cc.4242: "`. It should assert the tmux reason (`"no server"` or the
   wording for that path).
4. `src-go/internal/teams/hot/failures_test.go:120` (the "agent-bus refuses" case of
   `TestRoleSaysWhyARoleCouldNotBeChanged`): asserts only `"role: "`. It should assert `"refused"`.
5. `src-go/internal/teams/hot/failures_test.go:68` (the "no membership directory" case): the expected
   text is the bare session name, which appears in nearly every message about that session.
   It should assert the "no membership" wording.
6. `src-go/internal/teams/membership/membership_test.go:197-201`
   (`TestInitRefusesWhatIsNotItsThreeArguments`): 22 of its 25 refusal cases assert only that
   stderr contains `"membership-init"`, the generic prefix of every refusal. Only the 3 cases in
   `TestInitRefusalSaysWhatIsWrongAndWhatToDo` assert a reason. This predates 2df320d6, but the
   file was touched. Fix: give each case its expected reason fragment.

The tests the diff touched now assert reasons properly: `failures_test.go:98` and `:183-197`,
`problems_test.go:61-68`, and `membership_test.go:368-370`. The new rig at `failures_test.go:96`
also fixes a real defect: before it, the second sub-case reused a rig that the podman case had
already failed.

None of these findings is a production-code defect. Item 1 is a test that cannot detect the
defect it is named for. Items 2 to 5 fall short of this branch's own intent (2), which is cheap
to finish.

VERDICT: FIX-FIRST
