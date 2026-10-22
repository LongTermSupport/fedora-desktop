# Plan 00209: review of the offline-members commits (727087da9..25e3e3e44)

FIX-FIRST

The code is sound and its tests pass. The spec text has four medium defects that would mislead
the n3b builder or contradict A. Fix them before n3b is built from it. Commits reviewed:
181390f52, f9dfa4c3b, d545eabcf, 6cf0dc7fa, 25e3e3e44.

## What was run and checked

- `go test -race -cover ./internal/teamchat/status/ ./internal/teamchat/health/... ./internal/teamchat/admin/...`:
  all ok. Coverage: status 94.7%, health 93.7%, health/view 93.1%, admin 95.8%, admin/wire 92.0%.
- The strict decode of `member` holds. `"member":""` decodes, then `sameWritten`
  (status.go:200) re-encodes without it (`omitempty`) and refuses the mismatch. A handle with no
  entry in the record is refused by `checkLines` (status.go:383). Both rows are in
  `TestAProblemNamesItsMember`.
- No current writer breaks. `status.Next` (status.go:418) copies `Members` together with
  `Problems`, so the restart path (probe.go:186) and the next tick keep `member` consistent.
  `online()` raises member problems only for handles in `f.view.Members`, and those become
  `rec.Members` (health.go:335). No golden or fixture holds a status record.
- VW5 still catches real faults. A rejected invite (`leave`), a ban, or no membership at all is
  still a problem (view.go:377). An absent coordinator is also VW4 drift ("would invite",
  view.go:301).
- Spot-checks of the audit's SHOWN and INTENDED rows. Each was confirmed:
  - `host/apply.go:749`: waitReady waits only for the team's own unit.
  - `rooms/rooms.go:412`: invites are never waited on.
  - `seat/release.go:247`: awaitFree is bounded to 60 s and covers only that seat.
  - `seat/release.go:276`: a held lock is "not yet".
  - `apply/apply.go:321`: the roles need roster state only.
  - `sweep/sweep.go:53`: each holding runs on its own.
  - `admin/wiring.go:63`: `health` exits 1 only on a failed team, never on a problem.
- Audit counts re-counted: 13 BLOCKS, 34 SHOWN, 17 INTENDED.
- Public-repo hygiene: the added lines hold no paths, hostnames, users or container names.

## Findings

01. **medium: LX2 and the FRAME exception name a scope that does not exist.** LX2 says "only
    `--only C` naming that container alone exits 1 for it". The FRAME exception lists "an apply
    scoped to its container alone". But `lxc apply` takes only `--only-teams T,...` (LX1,
    SPEC-n3.md:236; C 3.9.1, DESIGN-v2-C-host.md:1595). A builder would either invent a flag
    that sits outside the approval gate and the play, or skip the rule.
    Fix: drop the `--only C` clause. A missing container is `DEFERRED ... container missing`
    under every scope, exit 0. If a targeted refusal is wanted, it belongs to `lxc restart`
    (SU3), which already targets one container.

02. **medium: LX2's "`declare check` names a declared container that does not exist" has no
    exit code and may not be implementable.** `declare check` is the approval gate the play
    runs (C 3.14, DC18). If naming a missing container makes it exit non-zero, one missing
    container blocks the play, which is the very fault OF1 forbids. It is also unclear that
    `declare check` can run `lxc-info` at all.
    Fix: say it is a stderr line with exit unchanged, or leave the naming to `lxc apply`'s
    `DEFERRED` line alone.

03. **medium: a container that stops mid-phase after step 06 is left unspecified.** LX6's
    re-check covers attaches at steps 05, 07 and 08. At step 07 the join of step 06 has already
    made the account and token. C step 10 writes the LXC record "only after the phase
    succeeded" (DESIGN-v2-C-host.md:1757). That leaves three gaps:

    - the spec does not say that the deferred entry's handle is recorded, so the next apply
      could treat it as a first join and `member add` again;
    - the spec does not say that the entry's seat is left out of step 08's `seats`. LX7 says so
      only for the 75 case.
    - SU2's D-Bus calls (step 09) and LX5's kit writes into a container that stops meanwhile
      still fail the phase.

    Fix: in LX6 or LX9, a mid-phase `DEFERRED` entry takes no further step, keeps its handle in
    the record (`deferred` with handle), and is left out of `seats` as LX7's 75 case is. Extend
    the `lxc-info -sH` re-check to SU2's and LX5's failures.

04. **medium: "offline" includes "a member that has not synced for any length of time", which
    contradicts A R27.** The FRAME's definition says so, and OF1 repeats it as "its sync
    stale". A 6.6 (DESIGN-v2-A-protocol.md:721) says liveness is the seat lock and presence is
    display only. A member with a held seat and a quiet sync is live, and must not be "shown
    clearly as offline".
    Fix: split the two lists:

    - shown offline is seat lock free, container stopped or missing, unit down, or not yet
      created;
    - never blocked on is that list plus a link `failed`, a stale sync, or an only-invited
      coordinator.

05. **medium: the child's view wire drops `member` and refuses it.** `encodeView` writes only
    `what` and `fix` (admin/answerops.go:128), and `decodeProblem` uses
    `exactKeys("what","fix")` (answerops.go:260). Yet the comment says it is "the status
    record's own encoding". No view problem carries `member` today, so nothing breaks yet. But
    the audit's own fix for B4 ("make it a member problem once the handle exists") is built in
    the view. Through the child that member would be lost: the encoder drops it, and an encoder
    that kept it would meet a decoder that refuses it.
    Fix: carry `member` in both, optional on decode, with a round-trip test now.

06. **medium: the L3 probe (LP1) has no per-member bound.** `probeGuests` (online.go:53) calls
    `Guests.Probe` for each LXC member with the tick's context. LP1 runs `lxc-info` for every
    probe. A container caught mid-stop can make `lxc-info` hang, which stalls that team's tick.
    The record then expires and shows the team problem "health has not run since ...". That is
    one offline member turning the team red, by a wait.
    Fix: LP1 bounds each probe (for example 5 s). A probe past its bound is `Stopped`, or a
    member-scoped "could not be probed", never a stalled tick. Add a test with a hanging fake.

07. **low: VW5 now hides a live coordinator stuck at `invite`.** The view has no liveness, so a
    coordinator whose seat is held but whose link never accepts the invite is invisible. Fix:
    after finding 5, raise it as a problem with `member` set to the coordinator rather than
    dropping it.

08. **low: `TestAStoppedMemberIsShownOfflineAndIsNoTeamProblem` passes without f9dfa4c3b's
    code.** A stopped probe with a working announcer raised no problem before the change either
    (online.go:184-209). The proof of `member` is the three updated assertions
    (`membersProblem` in TestATransientFailureIsAskedAgain, TestALaunchFailureIsNeverAnnounced,
    TestAGuestProbeThatFailsIsAProblem). The commit message and the audit present the stopped
    test as proof. Fix: describe it as a regression guard, or make it cover a launch failure.

09. **low: `status.Shown` (status.go:502) puts member problems among the team's.** SR1 says a
    reader judging a team counts only problems with no `member`, but the one reader helper
    gives no split. The unbuilt `status` verb and N4 board will each have to re-derive it.
    Fix: add `Record.TeamProblems()` (or a field on `View`) with a test, and point SR1 at it.

10. **low: LX2 makes every missing container `DEFERRED`.** C 3.9.3's split is `SKIPPED` when a
    handle is recorded (its seat number stays claimed) and `DEFERRED` when none is. The record
    status for a missing container is also not named. Fix: missing follows stopped's split, and
    LX9 names its record status.

11. **low: the FRAME's A 3.2 remedy assumes an old kit refuses.** "the member's link refuses its
    own team on return until its kit is upgraded" only works if the raise's roster key
    fails closed in a 1-only kit. Under A 3.1 (DESIGN-v2-A-protocol.md:280-287), an unknown key
    is ignored unless it is listed under `features`. Fix: say that the `proto.send` change is
    listed as a feature, so a stale kit's link makes the team untrusted.

12. **low: the audit's "Left" for B5 cites only C 3.9.2 step 02.** C 3.9.1
    (DESIGN-v2-C-host.md:1601-1602) also says "a missing one is an error in the present
    phase". Fix: add it to B5's Left, so the C amendment changes both.

## Not found

- The audit missed no BLOCKS in built code. Checked: apply, host, admin, seat, sweep, notice,
  rooms, element, health, and every `Sleep` or wait loop in `src-go/internal/teamchat`. The only
  per-member wait is the one in finding 6.
- SPEC-n3 sections 4-6 (n3c, n3d, n3e) hold no blocking text: sends do not wait on receipts,
  TIMEOUTs are asynchronous (A 8.3), and `wait`, removal and Stop hooks are each the member's
  own.

## Round 2

FIX-FIRST

Reviewed c88b3eacd and 21b29d2cd (`git diff a4a905368..21b29d2cd`). The code is sound. One
medium defect is left in LX6's mid-phase deferral, which the n3b builder would build as written.

Run: `go test -race -cover` on `status`, `health/...` and `admin`: all ok. Coverage: status
94.8%, health 93.7%, health/view 93.1%, admin 96.0%.

Round 1, finding by finding: 01, 02, 04, 05, 07, 08, 09, 10, 11 and 12 are resolved. 06 is
resolved in the text (LP1; n3b is unbuilt). 03 is resolved only in part (R2-01, R2-02).

Code, checked:

- The new tests fail without the code. The `member's problem` wire case fails at encode
  (`member` was dropped), and its round trip fails at decode (`exactKeys` refused it). The VW5 test
  fails (no problem was raised). `TeamProblems` did not exist.
- A member problem from the child naming a handle not in the view is refused. `decodeProblem`
  accepts the key, then `validate` -> `validView` -> `checkLines` (status.go:383) refuses it. An
  empty or non-string `member` is refused in `decodeProblem`.
- VW5's handle is always among the view's members. `open` needs `f.Members[f.Coordinator]`, and
  `memberList` (view.go:200) lists every roster member. `memberships` runs before `questions`.
  The tick copies the view's members into the record, so `checkLines` holds.
- The FRAME split is consistent with A 6.6/R27. B 2.9's `(down)` and `(failed)` marks are the
  member's own status line, and the FRAME shows them "as what they are". There is no
  contradiction. The A 3.1 `features` remedy matches DESIGN-v2-A-protocol.md:278-287.

Findings:

1. **medium: LX6's "`deferred` with the handle" contradicts C 3.9.3 and loses the unplaced
   bundle.** In C 3.9.3 (DESIGN-v2-C-host.md:1811-1815), `deferred` means no known handle. A
   known handle on a container that is not running is `SKIPPED`/`stopped`, and LX2 applies the
   same split. So at the next apply, if the container is still stopped, the entry becomes
   `SKIPPED` with status `stopped`. The fact that step 07 never placed the bundle is then gone.
   Once the container runs, the member is `active` with a recorded handle, so step 06 "as the
   state requires" (3.6.3 table, :871-873) does nothing. Nothing reissues, and the guest never
   gets a token. `member return` cannot apply either: it takes only `parked`/`lost`.
   Fix: record the fact somewhere that outlives the SKIPPED pass. Either mark the roster member
   `needs: reissue`, which C 3.6.1 already has and the board already shows, or give the record
   entry a `why` that survives status changes. Step 06 then runs `reissue` for that need. Say
   `reissue` only, and add a test: a stop after 06, a second apply while still stopped, and a
   third apply running.
2. **low: the SU2 case cannot be "left out of step 08's seats".** SU2 is step 09, after
   step 08 has written the host copy and the guest's `desired.json`. Fix: limit the
   seats clause to entries deferred before step 08. A step-09 deferral keeps its seat, since
   its bundle is placed.
3. **low: the audit's B5 row still holds the removed fix.** Its Fix column says "Only `--only C`
   for that container exits 1. `declare check` names it." That contradicts LX2 and the audit's
   own "Review round 1" paragraph. Fix: replace it with LX2's current text.
4. **low: LP1's 5 s bound is per probe, and `probeGuests` (online.go:55) runs the probes in
   sequence.** k hanging containers add k x 5 s to one tick. Past 60 of them the 300 s
   `Lifetime` (status.go:33) expires, and the team shows "health has not run". This is remote,
   but the FRAME says "never a stalled operation". Fix: LP1 bounds the tick's probes in all
   (or runs them in parallel), or the spec states the limit.
5. **low: the decode refusal rows `problem member none` and `problem member empty` pass without
   the code** (the old decoder refused the unknown key). They guard the new path but do not
   prove it. The round trip does. Fix: none needed. Optionally assert the error text names
   the member rule.

Not found: no new blocking path. `TeamProblems` has no caller yet; SR1/SR5 point the unbuilt
`status` verb and board at it. `teams/board/health.go` reads v1's health record, not this one.
