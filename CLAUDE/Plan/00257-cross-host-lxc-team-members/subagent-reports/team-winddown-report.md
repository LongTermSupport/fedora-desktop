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
