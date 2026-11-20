# Task B.13 report: closing every active seat of a team

Owner's request (through the coordinator): "wind down a whole team… all active seats get the
compact and exit". Spec: [SPEC-team-close.md](../SPEC-team-close.md) (TC1-TC17, D1-D3).

## Done (branch `plan-xhost-lxc-members`)

- **Spec** (TC1-TC17), and Task B.13 in PLAN.md.
- **`internal/sessclose`**: a Target may carry a `Guest` (the tmux server in an LXC container,
  reached through `lxc-attach`; the session there; where it is). G1-G4 in the package doc: its
  compaction's end is read from its pane, no session record is read, it is never killed (where
  a host session would be stopped it fails, "exit refused" or "busy past the bound", named as
  left running in its container), a container whose server cannot be listed fails only its own
  sessions as "unreachable". The live list is read per server. Tests:
  `TestAGuestIsCompactedOnItsPaneThenExited`, `TestAGuestThatDoesNotEndIsLeftRunning`,
  `TestAGuestBusyPastTheBoundIsLeftRunning`, `TestAnUnreachableGuestFailsOnlyItself`,
  `TestAGuestWhoseClaudeEndedIsNotKilled`.
- **`internal/tmuxccy`**: `Tmux.SocketPath` (`tmux -S <path>`), the form a v2 LXC seat's own
  server takes. Test: `TestSocketPathIsReachedWithS`.
- **`ccy-teams shutdown` / the team row's "Shut down every session of `<team>`"** closes v1 LXC
  members whose container runs in their `agent-bus-claude` session (TC1-TC14 for v1): preview
  line, the confirmation unchanged (once, default No), per-seat lines, the report and exit
  codes. A running container with no session has nothing to shut down (exit 0); an unreachable
  container fails the run. Tests in `internal/teams/shutdown/lxc_test.go`
  (`TestARunningLxcMemberIsCompactedThenExitedInItsContainer` and four more).
- Docs: shutdown help, `docs/agent-bus.md`, `docs/ccy-changelog.md` 3.122.35 (container
  unchanged), `scripts/test-ccy-teams-command.bash` (a record-less LXC row's new words).

DBF: a missing capability, not a defect class; no detector applies.

## Not done: the v2 half (TC15-TC17, v2 LXC seats)

The context budget ran out before it. Handoff for the next agent:

1. **v2 teams on the board (TC15), test first through teamtest's fake machine.** The board
   finds teams in `board.Collector.collectTeams` (`internal/teams/board/collect.go`) from v1
   `agent-bus-hs@*` units, the declaration and the stamps. A v2 team: the directory names under
   `paths.StatusRoot + "/teams/"`, each read with `status.Read(paths.StatusFile(team), team)`
   and shown through `status.Shown`; `View.TeamProblems()` is already documented as the board's
   team row. Unit state: `paths.ServerUnit(team)` through `internal/systemd`. Member rows:
   `rec.Members`. The record is the owner group's to read (0640): a file read, no new process.
   No v1 flow (apply, remove, restart, wind-down) may act on a v2 team: check every consumer of
   `snap.Teams`.
2. **v2 LXC seats in shutdown (TC2, TC3).** `lxcstate.ReadRecord("", team)` gives the entries
   (skip `left`); key `rec.SeatOf(e).Key()`; server `&tmuxccy.Tmux{Runner: lister.InContainer(lxcPath, container, user), SocketPath: paths.GuestTmuxSocket(key)}`;
   session `paths.GuestTmuxSession(team, checkout, seat)`; then the existing `addGuest` in
   `internal/teams/shutdown/lxc.go` does the rest. The unit runs as the entry's user, so the
   socket is theirs.
3. **The v2 Start its session line (TC16).** `teamchat-admin lxc restart --member <container>/<seat key>` (SU3) as root through `internal/sudo`, offered only when the
   container runs, the unit file is placed and no session runs; the v1 line is `picker/menu.go`
   `actStartLxc` and `internal/teams/lxcsession`.
4. Docs (the ccy-teams usage text, `docs/agent-bus.md`, a changelog entry with a CCY_VERSION
   bump), then B.13 done in PLAN.md and the journal.

## Decisions for the coordinator (the spec's D1-D3)

- **D1.** The owner's "wind down" is implemented as the existing Shut down widened, not a third
  team-row action; the existing "Wind down the team's sessions" (instruction plus `/compact`,
  no `/exit`) keeps its meaning. The owner may want a rename or a merge of the two rows.
- **D2.** A busy LXC box gets Escape once before `/compact`, as host seats already do (C4):
  the request's "busy past the bound" could instead mean waiting for the turn without
  interrupting it. Needs the owner's word.
- **D3.** An LXC session that will not end is left running and failed, never killed.

## v2 half

Done on `plan-xhost-lxc-members`, test first through teamtest's fake machine (each RED seen
before its code, except `lxcfreeze.Lister.UnitState`, whose test and code landed together).

- **TC15, v2 teams on the board** (`internal/teams/board/v2.go`): the directories of
  `<root>/var/lib/teamchat-status/teams/` that are team ids are `Snapshot.V2Teams`, kept apart
  from `Teams`, so no v1 flow reading `snap.Teams` (apply, remove, restart, wind-down, login, the
  agent-bus reads, the picker's v1 menus) acts on one. Team row: `status.Read` then
  `status.Shown`, its team problems (an expired, missing or unreadable record) in WHERE, STATE
  from `systemctl is-active teamchat-server@<team>.service` (`systemd.UnitState`). Member rows:
  the record's members (handle, kind, role, state); an LXC member also whether it was on the bus at the last
  health check (the record's `online`, worded for its source: rule `project_liveness_word`),
  its container and the LXC record's why. `Row.V2` marks them. A v2 name that is a v1 team's is said
  on the v1 row, never a second team. The graph draws v2 teams too. `config.TeamchatRoot`
  (`CCY_TEAMS_TEAMCHAT_ROOT`, "" on a host) is the root the v2 records are read under; teamtest
  and `scripts/test-ccy-teams-command.bash` set a scratch one, so no test reads the machine's.
  Tests: `board/v2_test.go`, `teamtest/teamchat_test.go`.
- **Consumers of `snap.Teams`** checked: board (collect, fetch, graph), picker (menus, sessions,
  offers, add-running, heading), login, restart, wind-down, shutdown. The picker gives a v2 row
  only `v2Actions` (team: Shut down and help; LXC seat: Start its session where offered, and
  help); its heading counts v2 teams and a v2-only machine is not offered create. `restart` and
  `wind-down` refuse a v2 team by what it is (`board.V2Refusal`). Tests: `picker/v2_test.go`,
  `TestAV2TeamIsRefusedByWhatItIs` in restart and wind-down.
- **TC2, TC3, TC17, v2 LXC seats in shutdown** (`internal/teams/shutdown/v2.go`): a v2 team is
  found on the board; every entry of its LXC record not `left`: `deferred` is offline; otherwise,
  after sudo and `lxc-ls --running`, a stopped container is offline and a running one goes to the
  existing `addGuest` with `tmux -S paths.GuestTmuxSocket(key)` as the entry's user and session
  `paths.GuestTmuxSession`. A v2 member outside an LXC container is named not reachable from
  here (`whyV2Host`, exit 1): ccy-teams has no way yet to find a v2 host seat's session.
  Tests: `shutdown/v2_test.go` (four).
- **TC7 fix (v1 and v2)**: a shutdown whose only seats were session-less said "has no running
  session" without listing them, against TC7; it now lists them under their heading (the v2 test
  caught it).
- **TC16, the v2 Start its session line** (`internal/teams/lxcsession/v2.go`): offered when the
  seat is in the LXC record, its container runs, `systemctl show` (new
  `lxcfreeze.Lister.UnitState`, as root through lxc-attach) gives a LoadState other than
  `not-found` and an ActiveState not running; what root cannot answer without a password keeps
  the line. Start re-checks, refuses a running unit (a restart would end the session), then runs
  `sudo -n /usr/local/sbin/teamchat-admin lxc restart --member <container>/<seat key>` and says
  what it ran and how to attach. Tests: `lxcsession/v2_test.go`, `lxcfreeze/attach_test.go`.
- Docs: shutdown help, ccy-teams usage text, `docs/agent-bus.md`, `docs/ccy-changelog.md`
  3.122.36 (image unchanged), the `lxcsession` package doc. B.13 marked done in PLAN.md.

DBF: a missing capability, not a defect class; no detector applies. The TC7 listing gap is a
one-condition logic slip no analyzer can express; fixed with its regression test.

Left open, for the coordinator: D1-D3 unchanged; a v2 host (ccy) seat is reported, not closed;
the board's on-the-bus word for an LXC seat comes from the status record's `online`, not a
live probe of the container (a probe per seat per redraw would cost a sudo process each).
