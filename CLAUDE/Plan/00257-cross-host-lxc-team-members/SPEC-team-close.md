# SPEC: closing every active seat of a team (Task B.13)

The owner's request, relayed by the coordinator: "wind down a whole team… all active seats get
the compact and exit". One confirmed team-row action in `ccy-teams` that, for every active seat
of the team, does what a person would: `/compact`, wait for it to finish, then `/exit`. It
covers v1 and v2 teams, host seats and LXC seats. For an LXC seat it is `/compact` then `/exit`
like any seat: it never stops the container or the session's unit. At the end it reports every
seat that did not close, and why.

Each expectation below is numbered and held by a test, written first.

## The action

- **TC1.** The action is the team row's existing "Shut down every session of `<team>`" and
  `ccy-teams shutdown <team> [--yes] [--no-compact]` (Plan 00201 SPEC-shutdown.md), widened:
  it already closes a host seat by `/compact`, its end waited for, then `/exit`, through
  `internal/sessclose` (C1-C10). It is not a third team-row action beside "Shut down" and
  "Wind down the team's sessions" (which types an instruction and `/compact`, and never
  `/exit`): composing the one close keeps one wording, one confirmation and one report.
- **TC2.** It closes the team's seats in three places, each reached through `sessclose`'s one
  close with the tmux server the seat's session is on:
  - a host seat: a live ccy or cc tmux session that a session record or a desired membership
    names for the seat (discover), on ccy's tmux server (`tmux -L ccy`); for a v2 team, the
    seat store's holding: a live session whose name a complete holding of the team's seat
    records (`held/held.json`, `seat.HeldBy`, the same answer the launcher's exit acts on). A
    desired membership (`Store.DesiredFile`) is never asked: it outlives its session and tmux
    reuses names, so it could name a later, unrelated session. A v2 host seat held by a session
    that is not on ccy's tmux (one started outside tmux holds its seats under its container's
    name), or whose holding record cannot be read, is out of reach; a seat of this host's pool
    that nothing holds has no session; a member whose seat is not in the pool is out of reach
    (another machine);
  - a v1 LXC seat: the tmux session `agent-bus-claude` on the server of the same name inside
    the running container, as the member's user, through `sudo lxc-attach`
    (`lxcfreeze.Lister.InContainer`, as the wind-down reaches it);
  - a v2 LXC seat: the tmux session `paths.GuestTmuxSession(team, checkout, seat)` on the seat's
    own server `tmux -S paths.GuestTmuxSocket(key)` inside the running container, as the seat's
    user, through the same `lxc-attach` runner. The seat is an entry of the team's LXC record
    (`lxcstate`, `/var/lib/teamchat-status/lxc/<team>.json`) that is not `left`.

## What an active seat is

- **TC3.** A seat is active when its session runs now: a host seat whose tmux session is live; a
  v1 LXC member that the last apply joined (or held back as stopped) whose container runs and
  whose `agent-bus-claude` session exists; a v2 LXC entry `joined` or `offline` whose container
  runs and whose tmux session exists on its socket.
- **TC4.** Not active, named under "nothing to shut down" and never a failure: a free seat, a
  parked one, an LXC member whose container is stopped or that the last apply deferred, and an
  LXC seat whose container runs with no session in it ("no session running in container C";
  sessions start on demand only, Task B.12).

## Confirmation

- **TC5.** Before anything is typed, the preview lists every seat it will close, one line each:
  its session (or LXC label), the seat and handle, and where it runs (the checkout, or "in
  container C"); then the members it cannot reach and those with nothing to shut down.
- **TC6.** It asks once, default No: heading crumbs `<team> › Shut down`, context line `team <team> · N sessions running`, question `Shut down N sessions of <team>?`. No closes nothing
  ("Nothing was shut down.", exit 0). `--yes` skips the question; with no terminal and no
  `--yes` it refuses (exit 64).
- **TC7.** With no active seat it asks nothing: "Team T has no running session." (exit 0), with
  the offline and session-less members listed under their headings when there are any.

## Each seat

- **TC8.** A seat is closed by `sessclose.Close`: `/compact` with the close instruction at a
  ready, empty box (a busy box is given Escape once; unsent text is never typed over), its end
  waited for up to 600 s, then `/exit`. A host seat with a recorded conversation is watched in
  its transcript; an LXC seat's conversation is in the container, so its compaction's end is
  read from its pane (C5), and the note says so.
- **TC9.** An LXC seat is never killed and nothing of it is stopped: no `tmux kill-session`, no
  `systemctl stop`, no `lxc-stop`. Where a host seat would be stopped (C8: its prompt not ready
  within 30 s, or not ended within 60 s of `/exit`, or at a launcher question), the LXC seat
  fails instead, named as still running in its container.
- **TC10.** Host seats keep every behaviour they have (SD1-SD16): stopped when they do not end,
  then their seats read again until released.

## The outcome lines and the report

- **TC11.** One line per closed seat: `<session>: exited cleanly[; compacted first …]`,
  `<session>: stopped: <why>`, or `<session>: failed: <why>`; a host seat's released seats
  follow (`; seat 1@alpha released`).
- **TC12.** Every seat that did not close is named with why, in words that say which case it
  was: not found (its tmux session is not there), busy past the bound (its prompt was not ready
  within 30 s), its compaction never finished (600 s), exit refused (it did not end within 60 s
  of `/exit`), unreachable (sudo, `lxc-ls` or `lxc-attach` failed, or an LXC record or the seat
  store's holdings unreadable), out of reach (another machine, a seat no record names, a v2 LXC
  member no entry of the team's LXC record names, a missing record included, a v2 host seat held
  by a session not on ccy's tmux, named with that session, or held with a record that cannot be
  read). Each member is named once, with one reason. "Not there" is tmux's own answer only (exit
  1 and its exact wording); the same words from lxc-attach, runuser or env are unreachable.
- **TC13.** The last line counts: `<team>: X exited cleanly, Y stopped, Z failed`, then `; N members not reachable from here` and `; N members offline` when there are any.

## Exit codes

- **TC14.** 0: every active seat exited cleanly or was stopped, or there was none, or the
  question was answered No. 1: a seat failed, or one could not be reached. 64: usage, or no
  terminal without `--yes`.

## v2 teams on the board

- **TC15.** A v2 team is on the board: a team row from its status record
  (`paths.StatusFile`, read with `status.Read`, the read `teamchat-admin status` makes), its
  state the homeserver unit's, an expired or missing record said in WHERE. Its member rows are
  the record's members (handle, kind, role, state, online); a v2 LXC member's row also names
  its container and whether its session was on the bus at the last health check (the record's
  `online`, not a live reading).
- **TC16.** A v2 LXC seat's row offers "Start its session in its LXC container" only where it can
  work: its container runs, its unit file is placed (`service: true`) and its session does not
  run. It runs `teamchat-admin lxc start --member <container>/<seat key>` (SPEC-n3 SU4), the
  one on-demand start of a v2 LXC session, and says what it ran. SU4 is systemd's start, which
  leaves a unit that already runs as it is, so the start never ends a running session, whatever
  changed between the row's check and the start; never `lxc restart` (SU3), which would.
- **TC17.** The shutdown's team must be on the board (TC1): a v2 team is found there too, so
  `ccy-teams shutdown <v2 team>` and its row's action reach it.

## Decisions taken here, for the coordinator

- **D1.** The owner's "wind down" is TC1's widened Shut down, not a new row; the existing "Wind
  down the team's sessions" keeps its meaning (instruction and compaction, no `/exit`).
- **D2.** A busy box is given Escape once before the `/compact`, as the host shutdown already
  does (C4), for LXC seats too. "Busy past the bound" is a box not ready within 30 s after it.
- **D3.** An LXC seat that will not end is left running and failed (TC9), never killed.
