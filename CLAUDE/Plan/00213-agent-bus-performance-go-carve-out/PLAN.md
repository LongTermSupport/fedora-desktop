# Plan 00213: agent bus performance — Go carve-out

**Status**: Not Started
**Created**: 2026-10-21
**Owner**: joseph
**Priority**: High

## Overview

pingbus (`helpers/pingbus/`, about 11.8k lines of Python, shipped as a zipapp) starts a fresh
interpreter for every status-line refresh and every hook event of every bus session. The
member kit's `settings.json` (`files/opt/claude-yolo/optional/agent-bus/settings.json`, used by
`agent-bus-claude`) and a ccy seated session's own settings (written by
`helpers/pingbus/seat.py`) both set `statusLine` to `pingbus status-line` with
`refreshInterval: 1`. The plugin's
`hooks/hooks.json` runs `pingbus hook …` on SessionStart, UserPromptSubmit, PostToolUse, Stop and
SessionEnd.

A desktop triage with ten bus sessions open measured these costs:

- One `pingbus status-line` costs about 0.32 CPU-s.
- Each session renders about twice a second.
- A container with nothing running in it spends about half a CPU thread on its status line.
- Of 14 busy threads in total, about 9.4 were short-lived processes, and most of those were
  status-line chains.

The laptop's sustained package power is capped, so this overhead also lowers the clock for real
work.

The cost is the interpreter start, not the work, and Python cannot avoid it. Plan 00191 left
pingbus in Python as a non-goal; the owner's decision on 2026-10-21 moves its hot paths to Go
here, and 00191's scope is unchanged. The owner wants
agent-bus to move towards Go, the language of all new tooling here ([src-go](../../../src-go/CLAUDE.md)).
It starts with the paths that run on every tick and every event. The per-tick part that belongs
to the hooks daemon (its status-line forwarder, which chains after pingbus) is reported upstream
as hooks-daemon issue #131 and is not this plan's to fix.

## Goals

- An idle bus session costs a few percent of one thread, not half a thread. Measure it with the
  cgroup CPU of an idle member, before and after.
- `pingbus status-line` is a Go binary that matches the Python output exactly (a parity oracle
  test), and starts in milliseconds.
- The per-event `pingbus hook …` paths move to Go one at a time, each with a parity test, until
  no Python interpreter starts on a tick or a hook event.
- The host stops paying a pyenv shim on every `python3` start of the kit.

## Non-Goals

- Porting the whole of pingbus or agent-bus in this plan. The long-lived watcher, the Matrix
  client and the admin CLI stay Python until a later plan decides.
- The hooks daemon's own status-line forwarder (hooks-daemon issue #131).
- CPU power limits and other host tuning. That is separate desktop triage.

## Tasks

### Phase 1: Cheap wins and an exact measure

- [ ] ⬜ **Task 1.1**: Build a measure script in this folder, read-only and re-runnable. It
  records the cgroup CPU of one idle member and one idle podman ccy session over 60 s, and counts
  new `pingbus status-line` and `pingbus hook` pids per session per second. This gives the
  baseline every later task is judged against.
- [ ] ⬜ **Task 1.2**: Find out why a session renders about twice a second at
  `refreshInterval: 1`. Event-driven refreshes, or a render cancelled and restarted while it is
  still running (under load a render can take over 0.5 s wall), are the candidates. Check Claude
  Code's documented status-line behaviour before testing.
- [ ] ⬜ **Task 1.3**: Raise `refreshInterval` to the slowest value that still keeps the team and
  role segment useful (the owner decides; 5–10 s suggested). Change it in both places that set it:
  the member kit `settings.json` (agent-bus-claude) and the settings `helpers/pingbus/seat.py`
  writes for a ccy seated session. Make the ccy version bump that `helpers/pingbus` and
  `files/opt/claude-yolo/` changes need (ContainerRules.md). Measure again with 1.1.
- [ ] ⬜ **Task 1.4**: The pingbus zipapp's shebang (`helpers/pingbus/bundle.py`) is
  `#!/usr/bin/env python3`, so every start resolves `python3` through PATH: on the host that
  means the pyenv shim, `pyenv-exec` and friends, at about 0.16 CPU-s per start. Make the
  shebang and every kit entry point (the `pingbus` wrapper and the hook commands) name the
  system interpreter directly, never through PATH. Plan 00203 already does this for the declare helper
  (pinned to `/usr/bin/python3`), which is the precedent.

### Phase 2: `pingbus status-line` in Go

- [ ] ⬜ **Task 2.1**: Port `helpers/pingbus/statusline.py` to a Go command under `src-go/cmd/`.
  With no teams it imports no other pingbus module, but whenever teams are shown (every bus
  session) `read_roles` imports `config`, `inbox` and `protocol` to read each team's cached role.
  So the port covers that role read too, as well as: the inner
  status line run in its own process group with the timeout, the team and role rows wrapped to
  `COLUMNS`, the red `no record` and `(role?)` states, and the rule that it always exits 0.
- [ ] ⬜ **Task 2.2**: A parity test that runs the Python and Go versions on the same fixtures
  (team records, roles, widths, a hung or failing inner command) and requires identical stdout.
- [ ] ⬜ **Task 2.3**: Point the kit's `statusLine` at the Go binary, through the play that
  installs the kit, with the ccy version bump. Measure again with 1.1.

### Phase 3: per-event hooks in Go

- [ ] ⬜ **Task 3.1**: Measure each `pingbus hook …` event's rate and cost with 1.1. Port them in
  order of total cost (PostToolUse is the likely first), each with a parity test, and leave the
  Python in place behind them until the last one is ported.
- [ ] ⬜ **Task 3.2**: Record what remains in Python and why, as the input to a later plan for the
  watcher and the CLI.

## Success Criteria

- [ ] An idle bus session's cgroup CPU, measured by 1.1, is below 5% of one thread.
- [ ] No Python interpreter starts on a status-line refresh, measured by 1.1.
- [ ] Every ported path has a parity test that passes in `qa-all.bash`.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00213-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- (none yet)
