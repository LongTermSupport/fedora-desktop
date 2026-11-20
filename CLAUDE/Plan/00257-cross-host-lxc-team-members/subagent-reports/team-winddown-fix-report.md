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

| Item                                   | State | Commit    | Tests                                                                                                                                                                                                                          |
| -------------------------------------- | ----- | --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 4. tmux "not there" read loosely       | fixed | 2d2c7f042 | rule `project_tmux_gone_text` (`TestTmuxGoneText`); `tmuxccy` `TestListReadsOnlyTmuxsOwnNoServerAsEmpty`, `TestCapturePaneIsGoneOnlyOnTmuxsOwnAnswer`; `shutdown` `TestAV2SeatWhoseTmuxCannotBeReachedIsNotReachable`         |
| 2. v2 host seats closed                | fixed | 5cb21944e | `shutdown` `TestAV2HostSeatsSessionIsClosed`, `TestAV2HostSeatWithNoSessionHasNothingToShutDown`, `TestAV2HostMemberOfAnotherMachineIsOutOfReach`, `TestAnUnreadableDesiredMembershipIsNotReachable`; `seatsweep` `TestDesiredReadGivesTheMembership` |
| 3. lxc member with no LXC record entry | fixed | 5cb21944e | `shutdown` `TestAV2LxcMemberNoLxcRecordNamesIsNotReachable` (no record file; no entry for it)                                                                                                                                  |
| Nit: picker sudo calls per refresh     | fixed | 16413ab40 | `picker` `TestAV2TeamsStartLinesAreAskedOnceARefresh`; `lxcfreeze` `TestUnitStatesAsksEveryUnitInOneCall`                                                                                                                       |

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

| Item                                            | State | Tests                                                                                                                                                                                                      |
| ----------------------------------------------- | ----- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. leftover desired.json closes another session | fixed | `shutdown` `TestALeftoverDesiredMembershipDoesNotMakeASessionTheTeams`, `TestAV2HostSeatsSessionIsClosed` (now by holding); `seat` `TestHeldByIsTheSessionsCompleteHoldings`, `TestReadHoldingsIsHoldingsReadOnly` |
| 2. seat held outside ccy tmux read as idle      | fixed | `shutdown` `TestAV2HostSeatHeldOutsideCcyTmuxIsOutOfReach`                                                                                                                                                  |
| 3. unreadable membership reported twice         | fixed | `shutdown` `TestAnUnreadableHoldingIsNotReachableOnce`, `TestUnreadableHoldingsAreNotReachableOnce` (replace `TestAnUnreadableDesiredMembershipIsNotReachable`, whose case is gone)                         |
| 4. nit: UnitStates matched by position          | fixed | `lxcfreeze` `TestUnitStatesAsksEveryUnitInOneCall` (answers out of order), `TestUnitStatesRefusesAnAnswerForAnotherUnit`                                                                                   |
| 5. nit: red proof command not recorded          | done  | re-run below                                                                                                                                                                                               |

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
