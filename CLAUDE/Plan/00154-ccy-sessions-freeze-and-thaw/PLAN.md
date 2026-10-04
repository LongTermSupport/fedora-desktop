# Plan 00154: ccy sessions freeze and thaw

**Status**: Not Started
**Created**: 2026-10-04
**Owner**: dev
**Priority**: Medium

## Overview

`ccy-sessions` lists every ccy and cc session and can attach, end or start one. `podfreeze`
can freeze (`podman pause`) and thaw a session's container, but it is a separate tool with
its own menu, so freezing "that idle session eating a CPU core" means leaving the picker,
finding the container's name and running a second command. This plan puts freeze and thaw
in the picker: a column that says which sessions are frozen and a key that toggles the
selected one.

A frozen session is a container whose processes are stopped but resident in RAM (the cgroup
freezer, `podfreeze` header). It does not survive a reboot, and its Claude cannot answer a
hooks-daemon signal, so `notify` and `reboot` must treat it as something the owner has to
resolve first, not skip it.

## Goals

- The table shows each session's frozen state: `frozen`, `running`, or `-` where there is
  no container to ask (a `cc` session, a ccy session whose container has exited),
  `unknown` when the engine could not be asked. Same rules as the network, token and key
  columns.
- One picker key toggles the selected ccy session: freeze when it is running, thaw when it
  is frozen, the verb derived from state exactly as `podfreeze` does. No confirmation
  prompt (it is reversible by the same key); the row names the result.
- The picker refuses to freeze the session it is being typed in, with a message that says
  why. `podfreeze` already refuses inside a container; this is the host-side equivalent
  (see Task 1.2).
- `ccy-sessions notify` and `reboot` name a frozen session and refuse, as they already do
  for a session whose daemon CLI cannot run, instead of hanging on a paused container.

## Non-Goals

- No new freezing mechanism: the picker calls `podfreeze`, which calls `podman pause`.
- No checkpoint/restore (CRIU needs root; rootless here, see `podfreeze`'s header).
- No freezing of `cc` sessions (they run on the host, there is no container).
- No LXC (`lxcfreeze` is for LXC guests, not ccy sessions).
- No change to what `restore` does after a reboot: frozen containers do not survive one, so
  every restored session starts thawed.

## Facts established (read from the code, not run)

- The picker is `files/home/.local/bin/ccy-sessions` (`rows()`, the key loop at its end);
  its rows and heading come from `files/var/local/claude-yolo/lib/tmux-session.bash`
  (`ccy_tmux_row`, `_ccy_tmux_full_row`, `ccy_tmux_row_heading`), shared with `ccy` and
  `cc`. A new column therefore changes the ccy library and needs a CCY version bump.
- `ccy_tmux_detail_rows` already asks the engine once per engine for every CCY container
  (`ps --filter label=ccy=true --format ...`) and returns tab-separated columns per session;
  the frozen state belongs in that same single query, not a per-row lookup.
- `freeze-common.bash` is the shared menu layer of `podfreeze` and `lxcfreeze`: group menu,
  drill-down, derived verb, act loop, reached through named hooks. The picker needs none of
  that. The reuse is to call the deployed `podfreeze freeze|thaw NAME` with an explicit verb
  (an explicit verb wins over the derived one) and let it do the host guard, the partition
  of act/skip/vanished and the engine call. Sourcing the library would drag a second menu
  into a picker that has its own.
- `podfreeze` refuses inside a container (`assert_on_host`), which also stops the session
  issuing the command from freezing itself. The picker runs on the host, but a host
  terminal can itself be attached to a ccy session (the F12 menu popups), so this guard is
  not enough on its own.
- `ccy-sessions` is deployed by `play-claude-yolo.yml`; `podfreeze` and its library by
  `play-podfreeze.yml`. The picker depends on a tool another play deploys.

## Decisions (defaults proposed; the owner can overrule any)

1. **Column** `FROZEN` after STATE: `frozen` / `running` / `-` / `unknown`.
2. **Key** Ctrl-F toggles freeze/thaw. (Ctrl-X ends, Ctrl-N starts, Esc or q quits.)
3. **Enter on a frozen session** attaches, with a one-line note above the picker saying the
   session is frozen and Ctrl-F thaws it. Refusing would hide a session that can still be
   looked at.
4. **`podfreeze` is a hard dependency**: the picker checks it at use time and, if absent,
   says to run `play-podfreeze.yml` and does nothing else.

## Tasks

### Phase 1: Facts that need the host

- [ ] 🚫 **Task 1.1**: **HOST**: with one running and one frozen ccy session, report what
  `podman ps --filter label=ccy=true --format '{{.Names}}|{{.State}}'` prints for each
  (is a paused container listed by `ps` without `-a`, and what is its state word?). The
  state word `podfreeze` uses (`FREEZE_STATE_FROZEN`) is the one to match. Blocked on the
  owner at the desktop.
- [ ] ⬜ **Task 1.2**: Decide, by reading `ccy_tmux_*` and the F12 menu code
  (Plan 00147), how the picker learns which session its own terminal is attached to
  (`$TMUX` and `display-message -p '#{session_name}'` on CCY's server socket), and what it
  does when it cannot tell (refuse the freeze, never guess).

### Phase 2: Tests first

- [ ] ⬜ **Task 2.1**: `scripts/test-ccy-sessions-freeze.bash`, driven like
  `test-ccy-sessions-reboot.bash`: fake `tmux`, `podman` and `podfreeze` on PATH. Red first
  for: the FROZEN column per state (running, frozen, no container, engine failure showing
  `unknown`); Ctrl-F on a running session calls `podfreeze freeze <container>` and on a
  frozen one `podfreeze thaw <container>`; on a `cc` session or one without a container
  it refuses and calls nothing; the session the picker is attached to is refused; an absent
  `podfreeze` is a named error; `notify`/`reboot` refuse naming a frozen session and signal
  nothing.
- [ ] ⬜ **Task 2.2**: Register the test in `scripts/qa-all.bash` the way the other ccy
  suites are.

### Phase 3: Implementation

- [ ] ⬜ **Task 3.1**: `ccy_tmux_detail_rows`: the frozen word from the one engine query;
  `ccy_tmux_row` and `ccy_tmux_row_heading` take the new column; `ccy` and `cc` pickers
  that print the shorter forms keep working (a column not given is left out, as today).
- [ ] ⬜ **Task 3.2**: `ccy-sessions`: the key, the toggle through `podfreeze`, the
  attached-session refusal, the frozen note on Enter, the `notify`/`reboot` refusal.
- [ ] ⬜ **Task 3.3**: CCY version bump (minor: a new feature), `docs/ccy-changelog.md`,
  the header of `ccy-sessions`, `docs/ccy.md`.

### Phase 4: Verification

- [ ] ⬜ **Task 4.1**: `deploy.bash` (`play-claude-yolo.yml` and `play-podfreeze.yml`, in
  that order) and `acceptance.bash` (deployed scripts identical to the checkout, `podfreeze`
  present and executable; names what a person must confirm) in this folder;
  `./scripts/qa-all.bash` green; `qa-reviewer` agent over the full diff.
- [ ] 🚫 **Task 4.2**: **HOST**: run `deploy.bash` and `acceptance.bash`; freeze and thaw a
  real session from the picker; confirm the refusal when typing in the session being
  frozen. Blocked on the owner at the desktop.

## Success Criteria

- [ ] The table shows the frozen state for every session kind, from one engine query.
- [ ] Ctrl-F freezes a running ccy session and thaws a frozen one; nothing else is touched.
- [ ] The session being typed in cannot be frozen from the picker.
- [ ] `notify` and `reboot` never hang on a frozen session: they name it and refuse.
- [ ] `./scripts/qa-all.bash` green; `qa-reviewer` clean; CCY version bumped.

## Delivery & Milestones

- Plan created; nothing built yet.
