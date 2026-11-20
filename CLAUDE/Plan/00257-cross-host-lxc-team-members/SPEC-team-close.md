# SPEC: closing every active seat of a team (Task B.13)

The owner's request, relayed by the coordinator: "wind down a whole team… all active seats get
the compact and exit". One confirmed team-row action in `ccy-teams` that, for every active seat
of the team, does what a person would: `/compact`, wait for it to finish, then `/exit`. It
covers v1 and v2 teams, host seats and LXC seats. For an LXC seat it is `/compact` then `/exit`
like any seat: it never stops the container or the session's unit. At the end it reports every
seat that did not close, and why.

Each expectation below is numbered and held by a test, written first.

## The action

- **TC1.** The action is the team row's existing "Close every session of `<team>` (compact, then
  exit)" and
  `ccy-teams shutdown <team> [--yes] [--no-compact]` (Plan 00201 SPEC-shutdown.md), widened:
  it already closes a host seat by `/compact`, its end waited for, then `/exit`, through
  `internal/sessclose` (C1-C10). It is not a third team-row action beside "Close" and
  "Compact every session of `<team>` (stays open)" (the wind-down, which types an instruction
  and `/compact`, and never `/exit`): composing the one close keeps one wording, one confirmation and one report.
- **TC2.** It closes the team's seats in three places, each reached through `sessclose`'s one
  close with the tmux server the seat's session is on:
  - a host seat: a live ccy or cc tmux session that a session record or a desired membership
    names for the seat (discover), on ccy's tmux server (`tmux -L ccy`); for a v2 team, the
    seat store's holding: a live session whose name a complete holding of the team's seat
    records (`held/held.json`, `seat.HeldBy`, the same answer the launcher's exit acts on), and
    only while that session's seat lock of the team is held (`seat.KeeperOf` is
    `KeeperRunning`, the store's own live-keeper test, which a take asks first), asked again
    just before the close: a holder whose lock is no longer held then is not typed into and is
    out of reach, changed while confirming. A desired membership (`Store.DesiredFile`) is never
    asked, and a holding whose lock is not held may be a run's that ended: both outlive their
    session and tmux reuses names, so either could name a later, unrelated session. A holder
    whose seat lock is free and whose name no live ccy session has has no session. Any other
    holder whose lock is not held is out of reach: no lock yet (a v2 launch writes the holding
    before its keeper makes the lock, so the session may still be starting), a lock the probe
    refuses, or a free lock while a live ccy session has the name (a later session, or the same
    one restarting). A v2 host seat held by a live session that is not on ccy's tmux (one
    started outside tmux holds its seats under its container's name), or whose holding record
    cannot be read, is out of reach; a seat of this host's pool that nothing holds has no
    session; a member whose seat is not in the pool is out of reach (another machine);
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
  sessions start on demand only, Task B.12), and a v2 host seat whose holder's run ended
  without releasing it (TC12's crashed holder).

## Confirmation

- **TC5.** Before anything is typed, the preview lists every seat it will close, one line each:
  its session (or LXC label), the seat and handle, and where it runs (the checkout, or "in
  container C"); then the members it cannot reach and those with nothing to shut down.
- **TC6.** It asks once, default No: heading crumbs `<team> › Close`, context line `team <team> · N sessions running`, question `Close N sessions of <team>?`. No closes nothing
  ("Nothing was shut down.", exit 0). `--yes` skips the question; with no terminal and no
  `--yes` it refuses (exit 64).
- **TC7.** With no active seat it asks nothing: "Team T has no running session." (exit 0), with
  the offline and session-less members listed under their headings when there are any.

## Each seat

- **TC8.** A seat is closed by `sessclose.Close`: `/compact` with the close instruction at a
  ready, empty box (a busy box is never given Escape: its turn is waited for, up to 300 s; unsent
  text is never typed over), its end
  waited for up to 600 s, then `/exit`. A host seat with a recorded conversation is watched in
  its transcript; an LXC seat's conversation is in the container, so its compaction's end is
  read from its pane (C5), and the note says so.
- **TC9.** An LXC seat is never killed and nothing of it is stopped: no `tmux kill-session`, no
  `systemctl stop`, no `lxc-stop`. Where a host seat would be stopped (C8: its prompt not ready
  within 300 s, or not ended within 60 s of `/exit`, or at a launcher question), the LXC seat
  fails instead, named as still running in its container.
- **TC10.** Host seats keep every behaviour they have (SD1-SD16): stopped when they do not end,
  then their seats read again until released.

## The outcome lines and the report

- **TC11.** One line per closed seat: `<session>: exited cleanly[; compacted first …]`,
  `<session>: stopped: <why>`, or `<session>: failed: <why>`; a host seat's released seats
  follow (`; seat 1@alpha released`).
- **TC12.** Every seat that did not close is named with why, in words that say which case it
  was: not found (its tmux session is not there), busy past the bound (its prompt was not ready
  within 300 s, a turn still running), its compaction never finished (600 s), exit refused (it did not end within 60 s
  of `/exit`), unreachable (sudo, `lxc-ls` or `lxc-attach` failed, or an LXC record or the seat
  store's holdings or seat locks unreadable), out of reach (another machine, a seat no record names, a v2 LXC
  member no entry of the team's LXC record names, a missing record included, a v2 host seat held
  by a live session not on ccy's tmux (its seat lock held), named with that session, or held with
  a record that cannot be read, or a holder whose seat lock is not there yet, cannot be probed,
  is free while a live ccy session has its name, or changed while confirming; a pool entry that
  is not a checkout's pool is named as such, never skipped, and the other checkouts are still
  read, as the seat sweep does), or a crashed holder (a v2 host seat whose holding names a
  session whose seat lock is free, with no live ccy session of that name). A crashed holder's
  seat is named under "no session running", exit 0, since nothing runs: with the session the
  holding names, and that the seat stays held in the seat store until the seat sweep releases
  it (or a restore of that session resumes it); when the sweep does so is its own rule, not
  said here. Each member is named once,
  with one reason. "Not there" is tmux's own answer only (exit
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

- **D1.** The owner's "wind down" is TC1's widened close, not a new row; the existing wind-down
  keeps its meaning (instruction and compaction, no `/exit`). The owner confirmed it (Q52) and
  renamed both rows by what they do: "Close every session of `<team>` (compact, then exit)"
  (was "Shut down every session of `<team>`") and "Compact every session of `<team>` (stays
  open)" (was "Wind down the team's sessions"), their confirmations' crumbs `<team> › Close`
  and `<team> › Compact`. The commands keep their verbs: `ccy-teams shutdown` and `ccy-teams
  wind-down`.
- **D2.** A busy box is never given Escape, host and LXC seats alike (the owner's answer to Q52,
  which also changes the host close, sessclose C4 and Plan 00201 SPEC-shutdown SD10): the close
  waits for the box to be ready and empty, up to the prompt bound, 300 s, the same bound the
  team wind-down gives a busy session before it types (its Idle wait), long enough for most
  turns to end on their own. "Busy past the bound" is a box not ready within those 300 s: a host
  seat is then stopped (C8), an LXC seat fails (TC9). The wind-down never sends Escape either.
- **D3.** An LXC seat that will not end is left running and failed (TC9), never killed. The
  owner confirmed it (Q52).
