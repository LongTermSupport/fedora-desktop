# Plan 00158: ccy sessions take over

**Status**: In Progress
**Created**: 2026-10-05
**Owner**: joseph
**Priority**: Medium

## Overview

On a remote VM, `ccy-sessions` showed a session as "open elsewhere" and would not attach it.
The other terminal was a dead connection: an SSH session that dropped while its tmux client
stayed connected. A session can be attached from one terminal only (the single-attach hook
in `lib/tmux-session.bash`), so that dead client held the session hostage. The owner asked
for a way to take over: "Yeah, please add the takeover. That's what we want."

This plan adds **Ctrl-T "take over"** to the `ccy-sessions` picker. On an "open elsewhere"
row it asks first, naming the other terminal(s) by tty and how long each has been idle, and
saying the session keeps running. Yes detaches every other client from that session and
attaches this terminal through the normal `ccy_tmux_attach` path. Only the other tmux client
is detached; the session and everything in it are never touched.

Deployed by `playbooks/imports/play-claude-yolo.yml`, which installs the launcher, its
`lib/` (including `tmux-session.bash`) and `~/.local/bin/ccy-sessions`.

## Goals

- A session held by a dead or forgotten terminal can be reclaimed from `ccy-sessions` in
  one key and one confirmation.
- The take-over fails fast and attaches nothing when the session ended, tmux refused the
  detach, or the other terminal did not let go.

## Non-Goals

- The launch-time offer of `ccy`/`cc` (`ccy_tmux_offer`) gets no take-over choice. It lists
  only detached sessions on purpose: a project open in another terminal on purpose starts a
  second session without a question, and adding the open ones would put a picker in front
  of that every time. It now names `ccy-sessions` Ctrl-T as the way to take one over.
- No `detach-client -P` (SIGHUP to the other terminal's shell). A plain detach is enough to
  free the session; killing the other login is not asked for.

## Tasks

### Phase 1: Tests first

- [x] ✅ **Task 1.1**: `scripts/test-ccy-sessions-take-over.bash`: a fake tmux holding
  sessions and their clients in files, a fake fzf answering from a queue, the real
  `ccy-sessions` under `script`. Written red (the library functions did not exist; with the
  library in place the picker cases still failed), wired into `qa-all.bash` and
  `CLAUDE/QA.md`.

### Phase 2: Library

- [x] ✅ **Task 2.1**: `lib/tmux-session.bash`: `ccy_tmux_idle_words`,
  `ccy_tmux_client_words` (pure), `ccy_tmux_other_terminals`, `ccy_tmux_take_over` (detach
  with `detach-client -s =<name>`, wait up to `CCY_TMUX_TAKE_OVER_WAIT_TENTHS` for the
  session to show no client, then `ccy_tmux_attach`). Return 2 for a session that ended,
  1 for any other failure; never kills anything.
- [x] ✅ **Task 2.2**: `ccy_tmux_offer`'s two "open in other terminals" lines point at
  `ccy-sessions` Ctrl-T.

### Phase 3: Picker and docs

- [x] ✅ **Task 3.1**: `ccy-sessions`: Ctrl-T key, `confirm_take_over` (the question names
  the other terminal(s) and idle time, and says the session keeps running); Ctrl-T on a
  detached row attaches as Enter does; the header and Enter's refusal point at Ctrl-T.
  Declining does not use a try; a session taken or ended meanwhile does; the bounded retry
  loop is kept.
- [x] ✅ **Task 3.2**: `docs/ccy.md` (picker keys, launch offer, troubleshooting row),
  `docs/ccy-changelog.md` 3.79.0, `CCY_VERSION` 3.78.1 to 3.79.0 (container unchanged,
  2.42).

### Phase 4: Host

- [ ] ⬜ **Task 4.1 (HOST)**: the owner runs `play-claude-yolo.yml` on each machine that
  uses `ccy-sessions` (this plan's `deploy.bash`, through `meta-deploy.bash`), then on the
  VM: `ccy-sessions`, choose the "open elsewhere" session, Ctrl-T, Yes. Expected: the
  question names the other terminal and its idle time; this terminal shows the session; the
  session did not restart.

## Success Criteria

- [x] `scripts/test-ccy-sessions-take-over.bash` passes, and the existing ccy-sessions and
  session gates still pass.
- [ ] Ctrl-T reclaims the stuck session on the VM.

## Delivery & Milestones

- Phases 1-3 delivered on branch `ccy-sessions-take-over`.
