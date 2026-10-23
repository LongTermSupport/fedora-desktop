# n3e-10c handoff: the link test hang (Plan 00209 N3 n3e-10 fix)

Worktree /workspace/untracked/worktrees/bus-v2-n3e-10, branch feat/bus-v2-n3e-10. Report:
[261022-n3e-10-fix.md](261022-n3e-10-fix.md) (DBF template filled; its "Package proof" says what
is still open).

## Done

- The lead's hang is found and fixed. TestAnOpBeforeThePIDIsNotAnswered waited on a fake-clock
  waiter COUNT. The server's read limit, armed on accept, made the count before the hook
  goroutine read its deadline, so the test advanced first and the op's deadline never fired.
- DBF: rule `project_fakeclock_count` (`src-go/qa/rules/fakeclockcount.go`, fixtures under
  `testdata/src/fix/internal/{badcount,fakeclock,othertimers}`, a GoProjectRules.md section).
  Red commit `7923769c1` (28 findings). New API: `clock.(*Fake).AwaitDue(ctx, d)`. The other 27
  instances are fixed in `6050bcbe3`. Independent search:
  [261023-n3e-10-fix-search.md](261023-n3e-10-fix-search.md).
- The commit after those holds: the original's fix (`hooks_test.go` waits with
  `AwaitDue(hookDeadline)`), `BlockUntil` deleted from `clock/fake.go`, gofmt of keeper
  `kp_test`/`rc_test`, the GoProjectRules Green line, the reports.
- Verified: 900 stressed runs of the original test are green (before the fix it hung within a
  minute). The whole of `internal/teamchat` passed once under race, shuffle and cover, with every
  package at 90% or more. golangci is clean in all three builds. The rule sweep finds 0 in all
  three builds.

## Left

1. A SECOND hang, not this class. Stress with `-shuffle=1 -count=20 -cpu=1,4` on the link
   package hung in `TestTheWatchFiresWhenTheProcessEnds` (`parent_test.go:24`): after
   `p.Kill()`, `parent.go` `watch`'s `rc.Read` (:62) never returned. The dump is
   `untracked/scratch/stress/link-none-1.log` in the worktree. It needs its own DBF; the cause
   is unknown (check how reexec's Kill reaps, and pidfd readiness).
2. A goroutine LEAK in production code. The same dump has about 145 `(*link).read` goroutines
   parked at `link.go:217` (a select on `l.msgs <-` against `l.quit`): ended links never close
   quit or drain msgs. This is the lead's named class, a send nobody answers once the link
   stops. DBF it.
3. Rerun `untracked/scratch/stress.bash` (worktree) to the end: the link package with seeds
   1792717137451862071, 1, 42 and 20261023, `-count 20 -cpu 1,4`, untagged and with
   busfailpoints, plus the other changed packages at `-count 5`. Fill in the report's Package
   proof.
4. Dispatch `defence-before-fix:conformance-reviewer` with the defence commit `7923769c1`, the
   final commit, the entry point `./scripts/qa-changed.bash`, the report, and the SPEC at
   `/root/.cache/defence-before-fix/spec/SPEC.md`. Resolve its findings.
