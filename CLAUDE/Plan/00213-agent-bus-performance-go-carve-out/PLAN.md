# Plan 00213: agent bus performance — Go carve-out

**Status**: In Progress
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

- [ ] 🔄 **Task 1.2**: Find out why a session renders about twice a second at
  `refreshInterval: 1`. Event-driven refreshes, or a render cancelled and restarted while it is
  still running (under load a render can take over 0.5 s wall), are the candidates. Check Claude
  Code's documented status-line behaviour before testing. Documented behaviour
  (code.claude.com/docs/en/statusline, "When the status line updates"): it runs at session
  start, then on a new assistant message, the end of `/compact`, a permission or vim mode
  change, a change to the `command`, a rate-limit or prompt-cache expiry, and each
  `refreshInterval` tick. Updates are debounced at 300 ms, and a new trigger cancels a run still
  in flight. The docs also say the event triggers "can go quiet when the main session is idle".
  Both candidates are therefore documented behaviour. Still to measure with 1.1: which of them
  causes the second render per second.

- [x] ✅ **Task 1.3**: Set `refreshInterval` to what the inner hooks-daemon status line and its
  context sidecar need. The bus pace does not set it; the bus segment gets its own cache
  (Task 2.1). Owner's ruling: the hooks daemon's refresh and the bus refresh must have separate
  TTLs. The need, from the two checks:

  - ccy `--supervise` (`.claude/ccy/claude-supervise.py`, owned by the daemon) treats a sidecar
    as stale when it is more than `_DEFAULT_FRESHNESS_SECONDS = 30` old. At that point it does
    not compact ("sidecar stale"). There is no setting to change this. Its 10 s
    foreground-ambiguity margin is sized at "roughly one status refresh interval".
  - In an idle session only the timer renders (Task 1.2).

  Value: **25 s**, defaulted by the coordinator as option A, and the owner can change it. It
  stays inside the 30 s window, so compaction is not delayed. It goes in both places that set
  it: the member kit `settings.json` and `seat.STATUS_LINE_REFRESH_S`, which a test pins equal
  and under 30. The watcher's `(down)` mark is unaffected. Its 300 s freshness is the mark
  file's age against the watcher's retry (30 s at most), not the render rate. The mark now
  shows or clears up to 25 s later. Going slower needs the hooks-daemon request in
  `subagent-reports/261021-hooks-daemon-sidecar-freshness-request.md`: a freshness that can be
  set, or one derived from `refreshInterval`. The inner line's own cost on each tick is
  upstream too (hooks-daemon issue #131). ccy version bump made. Measure again with 1.1.

- [x] ✅ **Task 1.4**: The pingbus zipapp's shebang (`helpers/pingbus/bundle.py`) is
  `#!/usr/bin/env python3`, so every start resolves `python3` through PATH: on the host that
  means the pyenv shim, `pyenv-exec` and friends, at about 0.16 CPU-s per start. Make the
  shebang and every kit entry point (the `pingbus` wrapper and the hook commands) name the
  system interpreter directly, never through PATH. Plan 00203 already does this for the declare helper
  (pinned to `/usr/bin/python3`), which is the precedent. Done: `bundle.SHEBANG` is
  `#!/usr/bin/python3`. It is tested by its value and by running the archive through its own
  shebang with a PATH whose only `python3` fails. Every kit entry point is that archive:
  `pingbus` on the host is a symlink to `pingbus.pyz`, the kit and image carry a copy, and the
  hook commands and the seat guard `exec pingbus` through PATH. So they look up `pingbus`, never
  `python3`, and need no change of their own. Fedora and the image (Debian `python3`) both
  have `/usr/bin/python3`. The host's copy is built by `play-agent-bus.yml` and the image's by
  `play-claude-yolo.yml`, both queued. ccy version bump made.

### Phase 2: `pingbus status-line` in Go

- [x] ✅ **Task 2.1**: Port `helpers/pingbus/statusline.py` to a Go command under `src-go/cmd/`.
  With no teams it imports no other pingbus module, but whenever teams are shown (every bus
  session) `read_roles` imports `config`, `inbox` and `protocol` to read each team's cached role.
  So the port covers that role read too, as well as: the inner
  status line run in its own process group with the timeout, the team and role rows wrapped to
  `COLUMNS`, the red `no record` and `(role?)` states, and the rule that it always exits 0.
  **The bus segment has its own TTL cache** (owner's requirement): about 60 s, in a file under
  `PINGBUS_HOME`. It is invalidated early when an input changes, so a membership change shows
  at once. The owner asked for mtime invalidation and a TTL. SPEC S32-S36 is the authority,
  and it is wider than mtimes: an lstat fingerprint (dev, ino, size, mtime and ctime in ns,
  mode, uid) of every file the render read. That covers the membership files, the session
  record, each bundle entry and its link, `member.json`, `token`, `team.json` and the down
  marks; ctime is what catches a `chmod` of the token. It also has an expiry at a down mark's
  age-out. The inner line is never cached. The Python stays uncached, because there the interpreter start
  costs more than the cache would save. Behaviour: `SPEC-status-line.md`.

  Done: `cmd/pingbus-status-line` over `internal/pingbus/statusline` (cache in `cache.go`). The
  golden passes 104/104 in-process and against the built binary; departures D2 to D4, the
  refusals and the racy rule are in SPEC section 9 and the journal.

- [ ] 🔄 **Task 2.2**: A parity test that runs the Python and Go versions on the same fixtures
  (team records, roles, widths, a hung or failing inner command) and requires identical stdout.
  It covers a fresh cache (a repeat render with no input changed) and an invalidated one (an
  input changed between renders: the output is Python's for the new inputs).

  Python side done. `SPEC-status-line.md` (S1-S40) states the behaviour and flags the parts
  that are not boilerplate. The oracle is `src-go/internal/pingbus/statusline/golden/testdata/`:
  104 scenarios and 122 steps, each recording stdout, stderr and exit, captured from the
  zipapp. They cover inner commands, settings, payloads, records, the membership, roles and
  refusals, down marks, widths, the timeout, SIGTERM/INT/HUP and the cache. The Go golden test
  (`internal/pingbus/statusline/golden`) reads them: `-update` re-records from the Python,
  `-impl=python` judges the zipapp, and the default judges the Go code. The Python passes all
  104 scenarios and a stand-in impl fails every step. The stderr shape rule (S24, S37) is the
  owner's call taken in SPEC section 4.

  Go side: the in-process run's clock is settled past the world's changes, so the `cache-`
  scenarios run through the real cache, and each step states stored or hit. A `lookup` that
  ignores the fingerprints fails ten of them. Re-recorded from the Python after Plan 00211
  merged: no golden diff. Still open: the per-format writer tests of SPEC section 8.

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
