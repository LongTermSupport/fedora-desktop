# N2 slice n2e: handoff 2

Branch `feat/bus-v2-n2e`, worktree `/workspace/untracked/worktrees/bus-v2-n2e`. The contract is
[SPEC-n2e.md](../SPEC-n2e.md); the first handoff is [261022-n2e-handoff.md](261022-n2e-handoff.md)
(its gotchas still hold). The second builder stopped at its context budget after three more green
commits.

## Done since handoff 1 (each its own commit, each test seen red first)

| Commit     | What                                                                                                                                                                                                                                                                                                                                                                                                                         |
| ---------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `0e1af6d1` | `internal/teamchat/health/view` (VW1-VW6): `Load(team, teamFile, rosterFile, roomsFile)` and `Read(ctx, Admin, Input) (health.View, error)`. Drift is `rooms.Converge` against a `dry` client and recorder (writes noted by room purpose, `CreateRoom` answers `errWouldCreate`). Tested on fakehs through `apply.Run`; `tuwunel_test.go` tests the DC16 watch. 92.9%                                                        |
| `9dda0ce1` | `internal/teamchat/notice` (NT1-NT2): `Disconnected(ctx, Client, Notice) (Outcome, error)`, `TxnID(run)`, `MaxPages`/`PageSize`. `Outcome{Posted, Found, Why}`: Found is "a goodbye found", posted. Main verified by `proto.VerifyMain` with Self the subject (a parked or absent subject is TrustError check `member`, so `whyInactive`); composed by `proto.Composer.Notice` to the humans and the open coordinator. 90.8% |
| `270bfc6a` | seat (NT3, n2e-D7): `NoticeRecord.UntilMS` (`until_ms`, not-posted only). `announce`: a transient failure writes `posted:false, why: whyTransient, until_ms` (kept across retries) and returns the error; at or after `until_ms` it writes `whyNotInTime` without asking and the release goes on to Retire. seat 90.3%                                                                                                       |

## Left, in this order

1. **`internal/teamchat/sweep`** (SW1-SW5) and `seat.Store.Holdings()`, as handoff 1 step 4 plans.
   `Store.Announce` already returns nil for a held or missing lock and a run with
   `launch_failed_ms` (`runOf`), so SW3/SW4 are tests over it.
2. **`internal/teamchat/element`** (EL1-EL5), as handoff 1 step 5 plans
   (`untracked/scratch/n2e-element-digest.md` in this worktree).
3. The adapters are NOT written: nothing yet maps `notice.Outcome` to `seat.Outcome` or
   `health.Notice` (seat's Outcome must have Why empty when Posted). The child's wiring is out of
   this slice, so a test-only adapter is enough if the reviewer asks.
4. **DUPLICATE LOCK** (the lead's message): after 1-2, message the lead "ready to unify"; then
   merge `feat/bus-v2-n2` and keep one team lock (`teamchat/teamlock` here vs n2d's
   `teamchat/host/lock.go`).
5. The review loop (Opus, at most 3 rounds) over `feat/bus-v2-n2..HEAD` against SPEC-n2e.md, C
   3.10-3.11, DC4, DC16, DC21, INT7, gates A and B, trust boundaries first; the report
   `subagent-reports/261022-n2e.md`; Task 2.4's progress in PLAN.md.

## Decisions taken since handoff 1 (for the report)

- The view reads `team.json` too (`DeriveRoster` needs the applied team); a missing `team.json`
  is an error, a missing roster file or rooms record is empty (as `team apply` starts).
- A member-written `m.room.member` event that does not read as one of the five memberships is
  skipped; a homeserver read failing is the view's error (health turns it into its problem).
- Questions room form is judged by its power levels against `proto.Desired`'s; "open" is the
  roster's coordinator active (not parked). The coordinator-not-joined fix is
  `ccy-teams restart <handle>`.
- A pending op's fix: `team.apply` is `ccy-teams apply <team>`; `member.<verb>` is
  `teamchat-admin member <verb> <handle>`; any other `a.b` is `teamchat-admin a b <team>`. An op
  that is not one line gives "an operation did not finish".
- The deactivated coordinator's fix names another coordinator first when the handle is the roster's
  coordinator or the declared one.
- `notice`: the scan stops at the first event older than `since_ms`; a `back` as the latest own
  notice posts (a later run); the probe is `whyInactive`. Why strings are fixed classes, never
  error or room text. A 429 is treated as transient whatever the client marked.
- NT3: at `until_ms` the announcer is not asked again (the bound is strict).

## Gotchas found

- The post-edit lint hook's output is large; batch edits and run `golangci-lint fmt` then `run`
  once.
- `golangci-lint fmt` takes no `--build-tags`; `golangci-lint run --build-tags tuwunel` does.
- fakehs `Fault.Method` must be set (empty matches nothing); a member client on fakehs needs
  `PlainHTTPHosts: []string{"127.0.0.1"}`; a transaction id may not hold `+`.
