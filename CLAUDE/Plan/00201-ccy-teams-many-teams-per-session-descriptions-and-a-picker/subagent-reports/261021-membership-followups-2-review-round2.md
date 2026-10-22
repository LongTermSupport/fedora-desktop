# Review round 2: membership follow-ups 2 (`git diff 217435de...HEAD`, 697dce6d + 74e87285)

Scope: `src-go/internal/teams/membership`, `src-go/internal/teams/hot`. I ran
`go test -count=1 ./internal/teams/...` (every package ok) and `go vet` on membership and hot
(clean).

## (1) `checked`: still sound

74e87285 does not touch production code. Round 1 judged `desired.go` and `membership.go` as
they stand now and found them sound: only `validate` sets `checked.d`, and `writeDesired`
refuses a nil `d` and runs `checkWrite` again before `MkdirAll`. Nothing has changed since.

## (2) The six round-1 findings

1. Farewell limit (`problems_test.go:20-38`): fixed, and the test now catches the defect it is
   named for. The 257 farewells are now valid ones (`a`/`1`/`ReasonRemoved`), so the per-item
   check no longer rejects them first. I tested this with a mutation: in a scratch copy under
   `untracked/scratch/` I deleted the `len(d.Farewell) > maxFarewells` case at
   `desired.go:130-131`, and the test FAILS (`too many farewells: <nil>, want "more than 256 farewells"`).
   The nil-list case asserts `"seats or farewell is missing"`.
2. `TestAddSaysWhenNoSeatCanBeChosen` (`failures_test.go:136`): fixed. It asserts the full chain
   including the injected `no seats`.
3. `TestAChangeToACCSessionSaysWhenTmuxCannotBeAsked` (`failures_test.go:146`): fixed. It asserts
   `no server`.
4. "agent-bus refuses" (`failures_test.go:120`): mostly fixed. It now asserts the specific
   `role: proj.3+local.podman was not made a coordinator`, which no other role-failure path prints.
   It still does not assert the injected `refused`. In a mutation run, agent-bus's stderr appears
   as passthrough, before that line (`refused✗ role: ...`), so a regression that dropped the
   passthrough would not be caught. Minor; see N1.
5. The "no membership directory" case (`failures_test.go:68`): fixed. It asserts the
   `notKit` ErrNotExist wording `launched without the agent team bus kit` (`hot.go:509`), which
   only that path prints.
6. `TestInitRefusesWhatIsNotItsThreeArguments` (`membership_test.go:166-212`): fixed. All 26
   cases now carry a reason fragment specific to their kind of refusal. Shared fragments such as
   `is not a session name` are still specific enough, because each one comes from a different
   check.

## Other failure tests in the touched files

I checked every failure test in `hot/failures_test.go`, `membership/membership_test.go` and
`membership/problems_test.go`, and each one asserts a reason as well as the exit code. Four
tests use `code == term.ExitOK` rather than an exact exit code: `failures_test.go:80`, `:93`,
`:98` and `:189`. Each of those also asserts the message, so none is exit-code-only.

## Nits (non-blocking)

- N1 `src-go/internal/teams/hot/failures_test.go:120`: the test could also assert `refused`
  appears in stderr, so the agent-bus reason reaching the user is covered.
- N2 `src-go/internal/teams/membership/problems_test.go:22`:
  `tooManyFarewells.Farewell = make([]Farewell, maxFarewells+1)` is dead, because line 27
  overwrites it. Delete it.

Neither nit affects correctness, and every round-1 finding that was blocking is resolved.

VERDICT: MERGE
