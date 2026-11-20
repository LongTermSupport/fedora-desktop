# B.13 review fixes: report (partial, handed off)

The review is `team-winddown-review.md`. This pass fixed finding 1 and the TC15 nit, then handed
the rest off because its context was running out. B.13 stays unticked.

| Item                                    | State | Commit    | Tests (each seen failing before the code)                                                                                                                                                                                 |
| --------------------------------------- | ----- | --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. v2 Start never restarts              | fixed | 1a16e4f78 | `host/lxc/start_test.go` (3 tests); `admin` `TestLXCRestart/start` and the verb table; `lxcsession` `TestAV2SeatsStartRunsTeamchatAdminLxcStart`, `TestAV2SeatsStartSaysTeamchatAdminsRefusal`; `apply_test` offline line |
| Nit: TC15 wording                       | fixed | 1a16e4f78 | (spec text only)                                                                                                                                                                                                          |
| 2. v2 host seats closed                 | open  |           | drafted                                                                                                                                                                                                                   |
| 3. lxc member with no LXC record entry  | open  |           | drafted                                                                                                                                                                                                                   |
| 4. tmuxccy reads attach failures        | open  |           | drafted                                                                                                                                                                                                                   |
| Nit: picker sudo calls on every refresh | open  |           | design noted                                                                                                                                                                                                              |

## Finding 1

`teamchat-admin lxc start --member <container>/<seat key>` is a new verb (SPEC-n3 SU4). It uses
`lxc restart`'s seat lookup, refusals and offline causes, in its own words, then calls
`ResetFailedUnit` and `StartUnit` over the guest's systemd. A start leaves a running unit as it is,
so the session cannot be ended whatever changed after the row's check. ccy-teams' v2 Start now
runs this verb, and its check beforehand only produces a message.

Every place that told someone to start a seat with `lxc restart` now names `lxc start`:

- apply's offline line and the unit docs
- SPEC-n3 SU1, SU4 and the offline table
- SPEC-team-close TC16
- DESIGN-v2-C-host
- `docs/agent-bus.md` and `docs/agent-bus-lxc-members.md` (whose claim that ccy-teams has no v2 start line was stale)
- `localhost.yml.dist`

CCY version 3.122.37. No container bump: the hook found no image source changed.

DBF: the class is a check-then-act on a unit's state across two calls. No detector can tell a
guarding read from an informative one, so the fix makes the act itself safe.

QA: the first `qa-changed` run failed only in `ccylaunch/difftest`
TestFrontParity/launch-selinux-enforcing. That is the GPU device listing failing under load,
already logged as issue #327, and the scenario passes when run alone. The rerun was green.

## Left

The design for items 2 to 4 and the picker nit, with drafts (in `untracked/scratch/`, not
tracked), is in the handoff `untracked/scratch/261120-aaaa5159f2f4c41e6-handoff.md`.

For item 2, the session name is verified: it is the ccy tmux session name.
`ccy_membership_args` passes `ccy_tmux_current_session`, or the container name outside tmux, to
`ccy-teams internal launch-args`, which writes `Store.DesiredFile(session)`.

## Fix round, part 2

Findings 2, 3 and 4 and the picker nit are fixed. B.13 stays ticked; its line now says how v2
host seats are found. Each test below was seen failing before its code.

| Item                                   | State | Commit    | Tests                                                                                                                                                                                                                                                 |
| -------------------------------------- | ----- | --------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 4. tmux "not there" read loosely       | fixed | 2d2c7f042 | rule `project_tmux_gone_text` (`TestTmuxGoneText`); `tmuxccy` `TestListReadsOnlyTmuxsOwnNoServerAsEmpty`, `TestCapturePaneIsGoneOnlyOnTmuxsOwnAnswer`; `shutdown` `TestAV2SeatWhoseTmuxCannotBeReachedIsNotReachable`                                 |
| 2. v2 host seats closed                | fixed | 5cb21944e | `shutdown` `TestAV2HostSeatsSessionIsClosed`, `TestAV2HostSeatWithNoSessionHasNothingToShutDown`, `TestAV2HostMemberOfAnotherMachineIsOutOfReach`, `TestAnUnreadableDesiredMembershipIsNotReachable`; `seatsweep` `TestDesiredReadGivesTheMembership` |
| 3. lxc member with no LXC record entry | fixed | 5cb21944e | `shutdown` `TestAV2LxcMemberNoLxcRecordNamesIsNotReachable` (no record file; no entry for it)                                                                                                                                                         |
| Nit: picker sudo calls per refresh     | fixed | 16413ab40 | `picker` `TestAV2TeamsStartLinesAreAskedOnceARefresh`; `lxcfreeze` `TestUnitStatesAsksEveryUnitInOneCall`                                                                                                                                             |

### Finding 4 (Defence Before Fix)

- **Class:** tmux's "not there" answer recognised by a loose match on a fragment ("No such file
  or directory", "can't find"). The hazard is that lxc-attach, runuser and env, which stand in
  front of a guest tmux, print the same words when tmux was never reached.
- **Rule:** `project_tmux_gone_text` in `src-go/qa/rules/tmuxgone.go`, catalogued in
  `CLAUDE/GoProjectRules.md`. It flags a string literal holding one of the four fragments, in
  `tmuxccy` or any package importing it, outside tmuxccy's `goneAnswer`. It runs in the Go gate
  as a hard step.
- **Red:** on its fixtures, and on the real tree at seven sites. Those are the five the handoff
  named (`tmuxccy.go` x2, `panes.go` x2, `submit.go`) and two more it had missed:
  `sessclose/sessclose.go:504` and `teams/winddown/windstep.go:220`. Both took any error saying
  "can't find" as the session gone.
- **Fix:** one classifier, `tmuxccy.goneAnswer`. It accepts only exit 1 where every stderr line
  is one of tmux's own forms, within the call's scope: server, session, or pane/window.
  `CapturePane` now returns a "not there" error matching `ErrSessionGone`, and the three
  callers ask for it with `errors.Is`.
- **Green:** the rule passes on both modules and the tagged `ccyteamstest` build.
- **Tests:** the shutdown test was shown red by putting the loose match back: the shutdown
  printed "no session running" and exited 0.

### Finding 2

A v2 team's shutdown now finds host seats through the owner's seat store:

- **Live session that names the team:** the shutdown takes each live ccy tmux session whose
  `desired.json`, filed under the session's name, names a seat of the team. It reads the file
  with `seatsweep.Desired.Read`, the reader the seat sweep uses, now exported. Each such session
  becomes one job and is closed as any host seat is.
- **Member with no live session:** "no session running on this host" when its seat is in this
  host's pool; otherwise not reachable (another machine).
- **Unreadable membership:** the session is named as not reachable. It is never closed.

This replaces `planForV2` and `whyV2Host`. The help text, `docs/agent-bus.md`, SPEC-team-close
TC2 and TC12, and the PLAN.md B.13 line were updated to match.

DBF: a logic gap. No detector can tell which members a resolver forgot.

### Finding 3

A v2 lxc-kind status member is not reachable, with `whyV2NoLxcEntry`, when no entry of the
team's LXC record that is not `left` names it. "Names it" means by handle, or by seat number for
a deferred entry that has no handle yet. A missing record names no member.

DBF: a logic gap, as in finding 2.

### Picker nit

`lxcsession.Starter.OffersAll` answers every row of a refresh in one round:

- **v1 rows:** read from their files, as before.
- **v2 rows:** the containers are listed once, the LXC path is read once, and each container's
  units are asked in one `systemctl show`. That call is the new `lxcfreeze.Lister.UnitStates`:
  one block per unit in argument order, with the block count checked. `UnitState` is its
  one-unit case.

The picker's `setSnap` calls it once per pass. The test counts one lxc-attach, two lxc-ls (the
running listing and the full one) and one lxc-config for two seats in one container. What the
board offers is unchanged.

DBF: a cost, not a defect class.

### Versions and QA

- **Versions:** CCY 3.122.38 (finding 4), 3.122.39 (findings 2 and 3), 3.122.40 (picker), one
  changelog entry each. The pre-commit hook asks for a bump on every commit that touches ccy
  code. The image is unchanged: no image-source path was touched.
- **QA:** `qa-changed` was green before each commit, with no flake rerun needed.

## Fix round, part 3

The independent review of part 2 (`team-winddown-fix2-review.md`) found one blocking defect,
two should-fix items and two nits. All five are done. B.13 stays ticked; its line, SPEC-team-close
TC2 and TC12, `docs/agent-bus.md` and the shutdown help text now say that v2 host seats are found
by the seat store's holdings. Each test below was seen failing before its code
(`untracked/scratch/b13-fix3-red.log` holds the shutdown run).

| Item                                            | State | Tests                                                                                                                                                                                                              |
| ----------------------------------------------- | ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1. leftover desired.json closes another session | fixed | `shutdown` `TestALeftoverDesiredMembershipDoesNotMakeASessionTheTeams`, `TestAV2HostSeatsSessionIsClosed` (now by holding); `seat` `TestHeldByIsTheSessionsCompleteHoldings`, `TestReadHoldingsIsHoldingsReadOnly` |
| 2. seat held outside ccy tmux read as idle      | fixed | `shutdown` `TestAV2HostSeatHeldOutsideCcyTmuxIsOutOfReach`                                                                                                                                                         |
| 3. unreadable membership reported twice         | fixed | `shutdown` `TestAnUnreadableHoldingIsNotReachableOnce`, `TestUnreadableHoldingsAreNotReachableOnce` (replace `TestAnUnreadableDesiredMembershipIsNotReachable`, whose case is gone)                                |
| 4. nit: UnitStates matched by position          | fixed | `lxcfreeze` `TestUnitStatesAsksEveryUnitInOneCall` (answers out of order), `TestUnitStatesRefusesAnAnswerForAnotherUnit`                                                                                           |
| 5. nit: red proof command not recorded          | done  | re-run below                                                                                                                                                                                                       |

### Items 1, 2 and 3

- **One answer to "which seats does session X hold":** `seat.HeldBy(holdings, session)`, the
  complete holdings whose `held.json` names the session. `Verbs.end` (the launcher's exit) and
  the launcher's `heldBy` (a take already held) now call it, and so does the shutdown, so the
  three cannot drift apart.
- **Read-only holdings:** `seat.ReadHoldings(store, checkout)` is `Store.Holdings` without a
  store opened for allocation (no clock, credential or announcer; nothing made; no store means
  no holdings). The shutdown reads every checkout of the pool with it.
- **The shutdown's v2 host path** (`v2host.go`) no longer reads `desired.json`:
  - a live ccy tmux session that `HeldBy` gives seats of the team is one job, closed as before;
  - a seat held by a session not live on ccy's tmux is not reachable, naming that session
    (`whyV2HeldOffTmux`); this covers a session started outside tmux, whose seats are held
    under its container's name;
  - a held seat whose `held.json` cannot be read is not reachable (`whyV2HeldUnread`);
  - a free seat of the pool has no session; a seat not in the pool is elsewhere, as before;
  - holdings that cannot be read decide nothing: every member left is named once as not
    reachable, never also idle. Exit 1 in each out-of-reach case.
- **Image:** `src-go/internal/teamchat/seat/` is an image source path, so the container version
  moved with CCY: 3.122.41, container 2.84.41, one changelog entry.

DBF: a design gap (the wrong record taken as the deciding fact). No detector can tell which file
is the authority for a question; the shared `seat.HeldBy` is the structural guard instead.

### Item 4

`UnitStates` asks `systemctl show --property=Id,LoadState,ActiveState` and matches each block to
its unit by `Id`. A block with no Id, an Id named twice or not asked, or a unit with no block or
no state is an error. The fakes in `lxcsession` and `picker` now answer with Ids (the picker's
in reverse order). DBF: a nit in one parser; no class beyond it.

### Item 5: the rule's red proof on the real tree

```
cd src-go/qa && go build -o <dir>/driver ./driver
git archive 2d2c7f042^ src-go | tar -x -C <dir>/old      # 2d2c7f042^ = adfe1d2a1
cd <dir>/old/src-go && go vet -vettool=<dir>/driver ./...
cd <dir>/old/src-go && go vet -vettool=<dir>/driver -tags ccyteamstest ./...
```

Both runs printed the same fourteen `project_tmux_gone_text` findings at seven sites:
`tmuxccy/panes.go:23` (2), `:96` (3), `tmuxccy/submit.go:93` (2), `tmuxccy/tmuxccy.go:130` (2),
`:160` (3), `sessclose/sessclose.go:504` (1), `teams/winddown/windstep.go:220` (1). The command
is now in the rule's catalogue entry in `CLAUDE/GoProjectRules.md`.

## Fix round, part 4

The independent review of part 3 (`team-winddown-fix3-review.md`, committed with this round) found
one should-fix item, one wrong report and two nits. All four are done. B.13 stays ticked; its
line, SPEC-team-close TC2, TC4 and TC12, `docs/agent-bus.md` and the shutdown help text now say
that a holder is closed only while its run is live, and name the crashed-holder case. Each test
below was seen failing before its code (`untracked/scratch/b13-fix4-red-shutdown.log` and
`b13-fix4-red-seat.log`).

| Item                                                 | State | Tests                                                                                                                                  |
| ---------------------------------------------------- | ----- | -------------------------------------------------------------------------------------------------------------------------------------- |
| 1. a crashed holder's reused name is closed          | fixed | `shutdown` `TestALeftoverHoldingDoesNotMakeAReusedNameTheTeams`; `seat` `TestKeeperLiveIsTheSessionsSeatLockHeld`                      |
| 2. a crashed holder reported as outside tmux, exit 1 | fixed | `shutdown` `TestACrashedHoldersSeatHasNoSession`, `TestAV2HostSeatHeldOutsideCcyTmuxIsOutOfReach` (its holder now keeps its seat lock) |
| 3. nit: `seatsweep.Desired.Read` a dead export       | fixed | `seatsweep` `TestDesiredReadGivesTheMembership` (now of `read`)                                                                        |
| 4. nit: `checkouts()` skips a strange pool entry     | fixed | `shutdown` `TestAPoolEntryThatIsNotACheckoutIsNotSkipped` (a name no checkout has; a file under a checkout's name)                     |

### Items 1 and 2

- **One live-keeper test:** `seat.KeeperLive(store, session, team)` is the store's own
  `keeperLive` (the session's seat lock of the team held; missing, free or refused is not live),
  read without a store opened for allocation. A take's `sessionHolds` now asks `keeperLive` first,
  so the store and the shutdown share one test. `ReadHoldings` and `KeeperLive` share `readOnly`
  (no store means nothing, nothing made).
- **The shutdown's v2 host path** splits the team's complete holdings by `KeeperLive`
  (`byKeeper`). Only live holdings reach `closeHolders`, so a later session under a crashed
  holder's name is never typed into.
- **The four cases of a complete holding:**
  - live on ccy's tmux and lock held: closed;
  - lock held, no session on ccy's tmux: not reachable, exit 1 (`whyV2HeldOffTmux`, as before);
  - lock free: a crashed holder, listed under "No session running", exit 0 (`whyV2Ended`). It
    names the session, says when a live ccy session under that name is a later one and not the
    team's, and says that the seat sweep keeps the seat for that session's restore while its
    session record remains, then releases it;
  - a seat lock that cannot be probed: every member left is not reachable once, exit 1.
- **Why exit 0 for a crashed holder:** nothing runs, so there is nothing to shut down and nothing
  the person can do here; the seat store settles the holding itself (TC4, TC14).
- **Image:** `src-go/internal/teamchat/seat/` is an image source path, so CCY 3.122.42 and
  container 2.84.42 move together, with one changelog entry.

DBF: a design gap (a record taken as proof of a live holder without asking the keeper). No
detector can tell which fact decides liveness; the shared `seat.KeeperLive` is the structural guard.

### Items 3 and 4

- `Desired.Read` is now `read`, called only by `Desires` and its test.
- `checkouts()` returns an error naming each pool entry that is not a checkout's pool (a name no
  checkout has, or a non-directory), worded as the seat sweep words it. The holdings are then
  unreadable, and each member is named once as not reachable.

DBF: item 3 is YAGNI cleanup, with no class. Item 4 is the "skip what does not parse" pattern in
one function. The class (a `continue` on a parse error) is too general for a detector without
false positives, so it gets a test.

## Fix round, part 5

The review of part 4 (`team-winddown-fix4-review.md`, committed with the RED tests in ef7c28ebe)
found four should-fix items and two nits. The coordinator asked for a minimal, safe final round
that prefers "not reachable, exit 1" over any finer classification. All six are done. The RED
tests and the seat API came first (ef7c28ebe; `untracked/scratch/b13-fix5-red-shutdown.log`, and
`b13-fix5-red-seat.log` for the seat test, which did not build before `seat.Keeper` existed). The
fix, docs and plan follow in the next commit.

| Item                                                       | State | Tests                                                                                                                                                                                                                                                                                   |
| ---------------------------------------------------------- | ----- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. a starting session reported as crashed                  | fixed | `seat` `TestKeeperOfIsWhatTheSessionsSeatLockSays`; `shutdown` `TestAHolderWithNoSeatLockYetIsNotReachable` (two subtests), `TestAHolderWhoseSeatLockIsRefusedIsNotReachable`, `TestALeftoverHoldingDoesNotMakeAReusedNameTheTeams` (now exit 1), `TestACrashedHoldersSeatHasNoSession` |
| 2. the seat sweep's handling of the seat described wrongly | fixed | `shutdown` `TestACrashedHoldersSeatHasNoSession` (the new words, and the old ones absent)                                                                                                                                                                                               |
| 3. live check before the prompt only (TOCTOU)              | fixed | `shutdown` `TestAHolderThatEndsWhileConfirmingIsNotTypedInto` (a prompt runner that frees the real lock before it answers)                                                                                                                                                              |
| 4. unreadable seat locks untested                          | fixed | `shutdown` `TestUnreadableSeatLocksAreNotReachableOnce` (through the new `Shutdown.Keeper` seam)                                                                                                                                                                                        |
| 5. nit: one bad pool entry blocks every host seat          | fixed | `shutdown` `TestAPoolEntryThatIsNotACheckoutIsNotSkipped` (a live holder in a valid checkout is closed; exit 1)                                                                                                                                                                         |
| 6. nit: `KeeperLive` builds a zero Store                   | noted | not changed (see Item 6 below)                                                                                                                                                                                                                                                          |

- **Item 1.** `seat.KeeperOf(store, session, team)` replaces `KeeperLive` and returns what the
  probe found: `KeeperNoLock` (also the zero value, and the answer for a store that does not
  exist), `KeeperEnded` (free), `KeeperRunning` (held) or `KeeperRefused`. The store's own
  `keeperLive` (a take's `sessionHolds`) is now `keeper() == KeeperRunning`, so there is still
  one probe and its logic is not copied. Shutdown says "no session running" (exit 0) only for a
  free lock when no live ccy session has the holder's name. Every other holder whose lock is not
  held is not reachable, exit 1, and nothing is typed. The reason is "whose seat lock is not there
  yet (still starting?)", "cannot be probed", or "is free, while a live ccy session carries that
  name". The words "a crash or a reboot" and "not the team's" are gone.
- **Item 2.** Checked against `sweep.one()`: a complete holding is kept only while its session
  record is restorable and its desired membership names the seat; otherwise it is released (as
  a leave in the second case). So the message now says only that the seat stays held in this
  host's seat store until the seat sweep releases it, or a restore of that session resumes it.
  It makes no claim about when. The restore clause is there because a restore re-takes the
  holding, and the launcher's exit, not the sweep, then releases it. The same wording is in the
  help text, SPEC TC2 and TC12, `docs/agent-bus.md` and the changelog.
- **Item 3.**
  - Each v2 host job carries its holder (store, session, team).
  - `Shutdown.recheck` asks `Keeper` again after `confirm` and before `stopAll`, which also
    covers `--yes`.
  - A job whose lock is no longer held, or whose lock cannot be read, is dropped and reported
    not reachable: "whose seat lock changed while confirming: it is free now; it is not typed
    into".
  - If no job is left, the run goes straight to the report.
  - The test frees a real flock in the prompt runner, between plan and close.
- **Item 4.** `Shutdown.Keeper` is a new field (default `seat.KeeperOf`), following the package's
  rule that every dependency is a field. Running as root on tmpfs, no real probe error can be
  produced without one: EACCES and links are refused, not errors. The test makes the seam return
  an error. The member is named once as "the seat store's seat locks: …", exit 1, nothing typed.
- **Item 5.**
  - `checkouts()` now returns the valid checkout keys and one not-reachable entry for each bad
    pool entry, named by its path: "not a checkout's pool (…), so no seat in it is read; the
    seat sweep reports it too".
  - `teamHoldings` and `inPool` read only the valid keys, so a verified live holder is still
    closed.
  - The run still exits 1, because the bad entry is not reachable.
  - A checkout whose own holdings cannot be read (a bad team directory) still makes every member
    not reachable (`TestUnreadableHoldingsAreNotReachableOnce`, unchanged).
- **Item 6, not changed.**
  - `KeeperOf` cannot take only the lock's path. The probe opens the lock beneath the store root,
    through no link (`openat2` with `RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS` from the root fd), and
    that root fd is what makes it safe against a container-planted link.
  - So it still runs on a read-only `Store`. `readOnly`'s comment now says that store has no
    sessions, and a zero checkout when the caller reads no pool, so only code that reads neither
    (as `probe` does) may run on it.
- **Image:** CCY 3.122.43 and container 2.84.43; the pre-commit hook demanded the container bump
  (`seat/` is an image input). One changelog entry.
- **Coverage.** The first qa-changed run failed go-qa's 90% statement floor over the diff scope
  (89.2%; `shutdown` was already at 88.5% before this round). These tests cover the new and the
  old unread branches:
  - `TestEverySeatLockStateHasItsOwnWords`;
  - `TestARecheckThatCannotReadTheLockClosesNothing`, a lock that cannot be read at the recheck;
  - `TestASeatStoreThatCannotBeReadDecidesNothing`, no seat store, or a pool that is a file;
  - `TestTheMenuLineShutsDownAndGoesBackToTheList`, `Offer`, which had no test;
  - `TestAFailedQuestionShutsNothingDown`, a prompt that cannot run.

DBF: items 1 and 3 are design gaps (a probe state taken for a fact it does not prove; a check made
too early to cover the act). No detector can say which state proves liveness or when a check is
stale, so each gets regression tests and one shared answer, `seat.KeeperOf`. Item 2 is wrong
documentation, which has no detector. Item 4 is a missing test. Item 5 has the same class as
part 4's item 4 and is covered by its test.

## Q52 D1-D3

The owner's answer to Q52 (SPEC-team-close "Decisions taken here") was "all as recommended".
Journal: `JOURNAL/2026-11-20/092749Z-decision-c50a6c.md`.

### D1: the two team-row lines are named by what they do (commit 1177f2ea8)

- "Shut down every session of `<team>`" is now **Close every session of `<team>` (compact, then
  exit)**. Its confirmation's crumbs are `<team> › Close`, and it asks "Close N sessions of
  `<team>`?" (the one row: "Yes, close N sessions").
- "Wind down the team's sessions" is now **Compact every session of `<team>` (stays open)**,
  with `<team> › Compact` and "Compact N sessions of `<team>`?".
- The footers say "close/compact `<team>`'s sessions". The picker help now has a paragraph for
  each line. `ccy-teams --help` and docs/agent-bus.md are updated, as are the specs that name
  the rows: 00201 SPEC-shutdown SD2 and SD8 and its acceptance step 12; 00203
  SPEC-team-wind-down W4 and W12, SPEC-team-add-and-restart A2 and B1, and its PLAN Task 3.14;
  00257 SPEC-team-close TC1, TC6 and D1, and PLAN B.13.
- **Not renamed:** the CLI verbs `ccy-teams shutdown` and `ccy-teams wind-down`. The owner asked
  for the row names, and scripts call the verbs. The preview and report wording ("nothing to
  shut down", "Nothing was shut down.") also stays, because TC4, TC12 and SD8 name it.
- **Worth the owner's notice:** the new wind-down name says only "Compact". The action also
  types `/goal clear` and the stop-and-push instruction first. The help paragraph and the
  confirmation preview still say so.
- Tests: menu_test, order_test (a destructive line is now read as "Close every session "),
  v2_test, addrunning_test (help names both lines and their verbs), picker_test (footer),
  shutdown_test and winddown_test (question, row and crumbs). All were RED before the rename.
- CCY 3.122.44. The container is unchanged: no image input changed
  (`ccy-image-source-paths.bash` names neither `teams/` nor `sessclose/`).

### D2: a busy box is waited for, never given Escape (commit 8e60b9232)

- `sessclose` `ready()` no longer presses Escape at a busy box, in either the `/compact` or the
  `/exit` phase. The box is read every poll until it is ready and empty, up to `Waits.Prompt`.
- Unsent text is still never typed over (Failed, untouched).
- Busy past the bound keeps its outcome. A host session is stopped (C8: "its prompt was not
  ready within 300 s, so its tmux session was killed"). An LXC seat fails as "busy past the
  bound … left running in container C" (TC9).
- **Bound: `Waits.Prompt` is 300 s (was 30 s).** It is the same bound the team wind-down
  (`winddown.DefaultWaits.Idle`, 300 s) already gives a busy session before it types, so both
  team actions wait the same. It is long enough for most turns to end on their own, and short
  enough that a stuck box is reported within minutes. Host and LXC use the one bound.
- The new bound also applies to `ccy --stop` and ccy-sessions' Close, which share sessclose, and
  to the wait for a prompt that never draws: such a session is now stopped after 300 s, not
  30 s.
- The wind-down path never sends Escape: `waitTurn` waits (W2), and `winddown` has no Escape of
  its own. Nothing changed there.
- `tmuxccy.SendEscape` had no caller left, so it and its test are removed.
- Docs: sessclose C4 (package doc) and the `Prompt` field comment; Plan 00201 SPEC-shutdown
  SD10's busy bullet (marked as changed by Q52) and SD12's wording, and its acceptance step 12;
  SPEC-team-close TC8, TC9, TC12 and D2; docs/agent-bus.md; `ccy-teams --help` and the picker
  help.
- Note: "host C4" is the sessclose package doc's C4. Plan 00201 SPEC-shutdown has no C4; its
  text for this is SD10.
- Tests, all through the existing tmux-pane fakes, all RED before the fix:
  - sessclose: `TestABusySessionIsWaitedForNeverInterrupted` (a turn of 120 looks, past the old
    30 s: compacted, exited, no Escape); `TestABusySessionPastTheBoundIsStoppedNeverInterrupted`
    (stopped, 300 s wording, nothing typed, no Escape); `TestAGuestInATurnIsWaitedForThenClosed`;
    `TestAGuestBusyPastTheBoundIsLeftRunning` (no Escape, 300 s); and `TestLiveAndLimits`
    (`Prompt` is 300 s).
  - shutdown: the fake's busy pane can now end after N looks (`busyLooks`).
    `TestABusySessionIsWaitedForNeverInterrupted` replaces `TestABusySessionIsInterruptedFirst`.
    `TestABusySessionPastTheBoundIsStopped` is new, and `TestAPromptThatNeverDrawsIsStopped`
    uses the 300 s wording.
- CCY 3.122.45. The container is unchanged.

### D3

D3 is recorded in SPEC-team-close as confirmed by the owner (Q52). It needs no code.

### QA

- `go test -race` over `./internal/sessclose/... ./internal/teams/... ./internal/tmuxccy/... ./internal/sessions/... ./internal/ccylaunch/... ./cmd/...` passes.
- golangci-lint over sessclose, tmuxccy, teams/shutdown and teams/picker reports 0 issues.
- `qa-changed.bash --base 232bac0f6` over D1 and D2 passed every check except one assertion in
  the bash suite `test-ccy-teams-command.bash`, which still picked the old "Shut down every
  session of alpha" line. Commit 5c41c33ce makes it pick the renamed line, and the suite then
  passes on its own (397 checks). `qa-changed --files` on that file is green.

DBF: D1 and D2 are owner decisions (a rename and a behaviour change), not defects, so there is
no class to detect.

## Q52 review fixes

This round answers the review in `q52-review.md` (should-fix items 1-3 and the four nits). It is
in commit 03bedb9da, which also commits the review.

### Item 1: two deadlines

- **The bounds.** `Waits.Prompt` is 30 s again, and the new `Waits.Turn` is 300 s.
  - `Close` starts each wait at `waitFrom` with the deadline `waitFrom + Prompt`.
  - In `ready()`, the first `Busy` look sets `inTurn` and extends the deadline to
    `waitFrom + Turn`. The deadline is never shortened.
  - After a compaction, the `/exit` wait starts again from `Prompt`. A turn seen then extends it
    the same way.
- **The timeout note** comes from the last state seen (`notReady`):
  - a host session: "a turn was still running after 300 s", or "its prompt was not ready
    within 30 s" ("300 s" if a turn had been seen before it);
  - an LXC session: "busy past the bound: …" or "no prompt: …", and it is left running.
- **Busy, then NotDrawn, keeps 300 s.** That is deliberate. A box that disappears in the middle
  of a turn is mostly Claude's own dialog inside that turn (a permission question, say), so the
  session is working. Cutting the bound back to 30 s, which has already passed, would stop or
  fail it at once. The test `TestATurnThatLeavesNoPromptKeepsTheTurnBound` holds this. The
  timeout note then says "its prompt was not ready within 300 s", not "a turn was still
  running".
- **Tests** (sessclose). All were RED with the 30 s Prompt and no extension: 7 failed.
  - `TestAPaneWithNoPromptIsStoppedAtThePromptBound`: a NotDrawn pane is stopped at 30 s.
  - `TestABusySessionIsWaitedForNeverInterrupted`: a turn that ends at 200 s is closed.
  - `TestANoCompactCloseWaitsForARunningTurn`.
  - `TestABusySessionPastTheBoundIsStoppedNeverInterrupted`: stopped at 300 s, with the turn
    wording.
  - `TestATurnThatLeavesNoPromptKeepsTheTurnBound`.
  - `TestAGuestWithNoPromptFailsAtThePromptBound`.
  - `TestAGuestBusyPastTheBoundIsLeftRunning`: the new wording.
  - `TestLiveAndLimits`.
- **The 30 s wording** is restored in shutdown's `TestAPromptThatNeverDrawsIsStopped`.
  `TestABusySessionPastTheBoundIsStopped` now pins the turn wording.

### Item 2: the wait is said where people see it

`sessclose.TurnWait` returns one sentence: "A session in the middle of a turn is given up to
300 s to finish it; it is never interrupted." (`TestTurnWait`). It is shown in three places:

- `ccy --stop` prints it on every run, `--no-compact` included.
- ccy-sessions' Close puts it in its question and in its progress line, for both choices. A
  `--no-compact` close now prints a progress line too.
- ccy-teams shutdown's preview, for both choices. The `--no-compact` preview's "without
  compacting; One" becomes two sentences.

Tests: `TestStopClosesThroughSessclose`, `TestCloseOnASessionClosesItThroughSessclose`,
`TestEverySessionIsSentExitAndItsSeatReleased` and `TestShutdownClosesThroughSessclose`. All
four were RED before the callers changed.

### Item 3: changelog

CCY 3.122.46. Its changelog entry gives the full reach:

- which commands wait for a turn: shutdown, `ccy --stop` and ccy-sessions' Close, with or
  without `--no-compact`;
- that the wait is up to 300 s;
- that a pane with no prompt gets 30 s, which 3.122.45 had made 300 s;
- that a Busy-then-NotDrawn pane keeps 300 s;
- both sets of wording.

The container is unchanged.

### Item 4: nits

- `picker/help.txt`: the Compact paragraph names the compact floor
  (`ccy_restore_compact_floor_tokens`; a smaller session is not compacted), and says an LXC
  member is compacted whatever its size.
- The `v2_test.go` comment says "Close every session".
- `docs/agent-bus.md`: the bold lead is **Close every session of a team**. The CLI verb is still
  named in the next sentence.
- SPEC-team-close TC1 is rewrapped.

### Item 5: specs

- SPEC-team-close: TC8, TC9, TC12 (a new "no prompt" case) and D2 (the two bounds, and why a
  turn seen keeps 300 s).
- Plan 00201 SPEC-shutdown: SD10 (the busy and not-drawn bullets) and SD12 (the three wordings).
- `docs/agent-bus.md` and the sessclose package doc (C4, C8, G2).

DBF: which bound applies to which screen state is a design choice, not a defect class a
detector can express. It is fixed conventionally, with the regression tests above.

### QA

- `go test -race` over sessclose, teams, tmuxccy, sessions, ccylaunch and screen: 39 packages
  ok, none failed.
- golangci-lint 2.14.0 (the pinned version) over sessclose, teams/shutdown, teams/picker,
  sessions and ccylaunch: 0 issues.
- `bash scripts/test-ccy-teams-command.bash`: passed 397, failed 0.
- `qa-changed --base 262793bdb`, first run (on 03bedb9da): only the go-qa analyzers stage
  failed. The project rule `project_screen_state_compared` (fedora-desktop#288) refused
  `notReady`'s `j.last == screen.Busy`. Commit 01597b4fe makes it a switch over every
  `screen.State` with no behaviour change. It also bumps CCY to 3.122.47, because the
  pre-commit gate requires a bump for any change to CCY code. This class already has its
  detector, and the detector caught this instance, so no new rule is needed.
- `qa-changed --base 262793bdb`, second run (on 01597b4fe): exit 0, 72 checks passed, none
  failed. Among them were go-qa over both modules and these suites: `test-ccy-teams-command`
  (397), `test-ccy-sessions-reboot` (344), `test-ccy-sessions-take-over` (46) and
  `test-ccy-tmux-targets` (23).
