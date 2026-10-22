# N2 slice n2e: handoff 3

Branch `feat/bus-v2-n2e`, worktree `/workspace/untracked/worktrees/bus-v2-n2e`. The contract is
[SPEC-n2e.md](../SPEC-n2e.md); handoffs [1](261022-n2e-handoff.md) and
[2](261022-n2e-handoff-2.md) still hold (their gotchas and decisions). The third builder stopped
at its context budget during review round 1's fixes.

## Done since handoff 2 (each its own commit, each test seen red first)

| Commit     | What                                                                                                                                                                                         |
| ---------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `0410dc05` | `seat.Store.Holdings` (SW1): pool seats with a held/, complete or not; claim dirs and checkout.json skipped; any other name ErrInvalid                                                       |
| `366c441e` | `internal/teamchat/sweep` (SW2, SW3, SW5): over released, not desired released as a leave, otherwise `Announce`; incomplete released. Real-store tests: a container start is never announced |
| `07aebf21` | SW4/INT7: a launch-failed run is never announced (test only; red by removing runOf's refusal)                                                                                                |
| `0adb7a6f` | no error from any seat lock a member wrote (13 cases; test only; red by letting the probe error on a non-regular lock)                                                                       |
| `d1c7c96d` | `internal/teamchat/element` (EL1-EL5), 93.6%; `.golangci.yml` gosec G302/G304 exclusion for its tests                                                                                        |
| `d0eb1dd4` | review 1 findings 3 and 5: NT3's bound is keyed on the claim, not the member's run id; why is `matrix.Class(err)` + "; asked again until until_ms" (new `matrix.Class`)                      |
| `14fbb19b` | review 1 findings 1, 2, 4, 6, 7 in health (view failure keeps lxc-online.json; restart re-checks the stamp; HT3 class and fix; re-probe probe words; NRestarts re-read after restart)        |

Review round 1: [261022-n2e-review.md](261022-n2e-review.md), FIX-FIRST, 0 high, 2 medium, 6 low.
It is NOT committed yet: commit it with the next step.

## Left, in this order

1. **Review 1 finding 8** (low): `seat/holdings.go` `claimMark` duplicates `paths.claimSuffix`;
   export a `paths.IsClaimName(name) bool` (test first in `paths`) and use it in holdings.go and
   `paths.ParseSeatName`. Fix the comment in `health_test.go` `TestTheLXCDisconnectedNotice`
   ("a new run B ... failed" but its record is `lockRecord(runB, false)`): better, make it a launch
   failure and assert run A is still announced.
2. **Review rounds 2 and 3** (at most 3 rounds in all): a fresh Opus reviewer over
   `feat/bus-v2-n2..HEAD`, same brief as round 1 (SPEC-n2e.md, C 3.10-3.11, DC4, DC16, DC21,
   INT7, gates A and B, trust boundaries first), writing `261022-n2e-review-2.md` (then `-3`).
   Commit each review file. Anything open after round 3 goes in the report.
3. **The report** `subagent-reports/261022-n2e.md`: what was built, coverage per package
   (`go test -race -cover`: status 94.6, teamlock 93.1, health 91.5, health/view 92.9, notice
   90.8, seat 90.3, sweep 100, element 93.6, matrix 98.9), the decisions (handoffs 1-2 and below),
   design-text fixes, and the section **"Unify with host/lock.go"** (the lead's ask): compare
   `src-go/internal/teamchat/teamlock` with n2d's lock
   (`git -C /workspace show feat/bus-v2-n2d:src-go/internal/teamchat/host/lock.go`), say which
   design survives and the exact steps to port the other's callers and tests (done at merge by the
   build session; do NOT unify now). Then Task 2.4's progress in PLAN.md.
4. Final message to the lead under 700 characters: the tip, the review verdict, coverage, and the
   lock recommendation.

## Decisions taken since handoff 2 (for the report)

- Holdings: the pool is owner-only, so a name the store never makes is an error, not skipped.
- Sweep: an incomplete held/ is released with no session (its release quarantines it); a
  cancelled context stops the sweep; per-holding errors are in each `Result`.
- Sweep residual (report it): a member that rewrites a valid run id in a free lock of a kept seat
  makes `Announce` post again for each new run, as itself. No trust gain (it could post with its
  own token), but noted. And a release whose retire fails after a "not posted in time" record can
  start a fresh bound if the member names a new run; bounded by retire failing.
- Element: `Env{Root, Home, UID, Approvals, Names, Stdout, Stderr}`; the root refusal is
  `Root` (the caller sets it from the euid), owner checks use `UID`, so tests run as root or not.
  A team with no status record is named by its id (n2e-D10, propose in the SPEC). The launcher
  escapes a backslash in the display name (the name is declaration-sourced, not gated). Comment=
  keeps v1's form with the team id. The consent record is compact JSON with a newline; a consent
  directory or file with any group/other bit, a link, another owner, extra keys, another version or
  another text is no consent. `check` prints "element: <reason>; apply would refuse" on stderr.
  Parents of the profile are made 0700, the launcher's directory 0755.
- `matrix.Class(err)`: a fixed text per failure class, "cancelled", "timed out", or
  "not a homeserver answer"; used by the seat's NT3 why and health's HT3 problem.
- HT3's fix is `teamchat-admin health <team>, then systemctl status teamchat-server@<team>.service`.
- A restart whose post-restart state cannot be read keeps the pre-restart state.

## Gotchas

- Redirect logs only to `/workspace/untracked/worktrees/bus-v2-n2e/untracked/scratch/` by absolute
  path (a relative `untracked/` after `cd src-go` is denied as outside the repo).
- A Bash command joining statements with `;` is denied; chain with `&&` or `||`.
- The seat's first release moves the session seat directory to quarantine; a test that rewrites
  the member's lock afterwards writes it at `w.releasedSession(taken)+"/seat.lock"`.
