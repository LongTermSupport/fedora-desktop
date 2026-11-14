# Plan 00256: ccy session idle stand-down

**Status**: Not Started
**Created**: 2026-11-13
**Owner**: joseph
**Priority**: Medium

## Overview

GitHub issue #270, Problem 2. The owner's policy for permanent (declared) ccy sessions is that a
working session has its compose services up, and an idle session is stood down with its compose
services stood down too. The first half exists: a restore replays a record's `compose=` answer
as `--compose`. Nothing does the second. `--max-age` ends a session at its first idle point, but
only to restart it. The end-of-session compose question is asked of a person. A permanent session
with nothing to do keeps its container and its whole compose stack up indefinitely.

This plan adds an idle stand-down: a launch option `--idle-stand-down DURATION`. When a session
has been idle for DURATION, it ends the way `ccy --stop` does, stops its compose services, and
keeps its restore record, so that the next boot or `ccy-sessions wake NAME` brings it back to work.

Issue #270's two defects (Problem 1, and the never-took-a-turn restore) were fixed separately on
branch `ci/issue-270-restore-check`, as Defence Before Fix work. The second one blocks this plan,
because an idle declared session must survive the reboot after it.

## Goals

- `--idle-stand-down DURATION` (default from `CCY_IDLE_STAND_DOWN`), recorded in the session's
  restore record like `--max-age`, so a restore keeps it.
- Idleness is measured where `--max-age` already acts. A session is idle while all of these
  hold: an empty prompt, no turn running, no bus delivery pending, and no background task or
  subagent running. Typed input, a delivery or a turn resets the clock.
- A stand-down ends the session as `ccy --stop` does (`/compact` when due, then `/exit`). It
  answers the end-of-session compose question with "stop", so the services are stopped but not
  removed and volumes are kept. It writes a `stood-down` line, so nothing reports a crash.
- The record outlives a stand-down, with its `compose=` left as it was. `ccy-sessions wake NAME`
  replays it, as restore does for one session.
- Reporting: `ccy-sessions --list` shows `stood-down`. `verify-restore` reports a session that
  stood itself down after the restore as `STOOD-DOWN`, which counts as OK. `notify` leaves a
  stood-down session out.

## Non-Goals

- Waking a stood-down session when a bus delivery arrives for its seat. The issue puts this out
  of scope, and it needs its own design conversation.
- Standing down cc (host) or opencode sessions. They have no supervisor and no container.
- Changing `--max-age`.

## Context & Background

Facts read from the tree at the time of planning:

- The PTY supervisor is `.claude/ccy/claude-supervise.py`. It is **daemon-owned**: hooks-daemon
  upgrades replace it. `--max-age` is a ccy plugin (`files/var/local/claude-yolo/supervisor-plugins/ccy_lifecycle.py`).
  The supervisor calls the plugin's `on_idle(tick)` only at an idle point with an empty input box,
  after every built-in family has passed. Plugin API v1 lets `on_idle` return `ExitForRestart`
  (the host types `/exit`, writes `restart-request.json` and exits with
  `EXIT_STATUS_RESTART_REQUESTED`, and the launcher relaunches with `--resume`) or `Notify`.
  **There is no result that ends the session and keeps it down.** `IdleTick` carries only `now`
  and `session_id`.
- The end-of-session compose question is in the launcher (`claude-yolo`, after the session
  ends). It is asked only when this launch started the services (`CCY_COMPOSE_WAS_STARTED`). Its
  "yes" runs `$CCY_COMPOSE_CMD down`, which **removes** the containers. With nobody to ask, it
  leaves the services running. The issue asks for "stopped, not removed", which is `stop`, not
  `down`.
- A session's record is removed when its pane's command ends, whatever the exit status
  (`registry.Trampoline`: `"$@"; rc=$?; rm -f -- <record>`).
- `ccy --stop` and `ccy-sessions` Close go through `internal/sessclose` (`/compact`, the end
  waited for, then `/exit`).
- The restore manifest (`internal/restore`, `Going` values) and `verify-restore` (`internal/sessions/verify.go`)
  have no stood-down state.
- `notify` speaks only to live tmux sessions, so a stood-down session, which is not live, may
  already be left out. This is not established (Task 1.1).

## Tasks

### Phase 1: Establish what is not known (triage)

- [ ] ⬜ **Task 1.1**: Write `triage.bash` (read-only, in this folder, per
  [PlanTriage.md](../../PlanTriage.md)) to establish, on a live ccy session:
  - [x] ✅ The script: `triage.bash` with its legs in `probe-idle.bash`, read-only and
    anonymised, queued in `meta-deploy.list` with `--sample 180`. Item 4 is answered from
    version and help output only (no throwaway compose project: triage changes nothing).
    The findings below stay open until the host has run it.
  - [ ] ⬜ What the supervisor knows of the four idle conditions. Find out whether it can tell a
    running background task or subagent, and a pending bus delivery, from an empty idle prompt,
    or whether only the PTY's input and output quiet are visible. Probe it through the
    supervisor's own log or state files, not by reading code alone.
  - [ ] ⬜ How often `on_idle` is called on a session left alone, which bounds how finely
    DURATION can be measured.
  - [ ] ⬜ Whether `notify` and `--list` already leave a session with no tmux session out.
  - [ ] ⬜ Whether `<compose> stop` exists and behaves the same for podman-compose and
    docker-compose as the versions deployed here.
- [ ] ⬜ **Task 1.2**: Record the triage findings in `JOURNAL/` and update this plan.

### Phase 2: Owner decisions

- [ ] ⬜ **Task 2.1**: Decision gates 1 to 5 below, answered by the owner.

### Phase 3: Build (test first, per decision)

- [ ] ⬜ **Task 3.1**: The launch option `--idle-stand-down DURATION` with `CCY_IDLE_STAND_DOWN`,
  using the same DURATION grammar and bounds checks as `--max-age`, recorded in the record and
  kept by `registry.ReplayArgs`, in the launcher (bash and Go front) and in `declare`.
- [ ] ⬜ **Task 3.2**: The idle measurement and the stand-down request in the supervisor plugin.
  Gate 1 decides how.
- [ ] ⬜ **Task 3.3**: The stand-down's end: the end-of-session compose question answered "stop"
  (gate 3), the record kept and not removed, and a `stood-down` line written (gate 4).
- [ ] ⬜ **Task 3.4**: `ccy-sessions wake NAME`, which replays one record as restore does (the
  same `conversation.StartPlan` and `judgeRestore`), refuses a live name, and has usage-text
  and tests.
- [ ] ⬜ **Task 3.5**: Reporting: `--list` shows `stood-down`, `verify-restore` reports
  `STOOD-DOWN` as OK, and `notify` leaves stood-down sessions out (tests for each).
- [ ] ⬜ **Task 3.6**: Docs (`docs/ccy.md`, ccy and ccy-sessions help), a CCY version bump with a
  changelog entry (and the container version if image inputs change), and a
  `meta-deploy.list` entry.

### Phase 4: Verify

- [ ] ⬜ **Task 4.1**: `acceptance.bash`. On the host, a session with a short DURATION stands
  itself down, its compose services are stopped and not removed, its record is kept, `--list`
  says `stood-down`, and `ccy-sessions wake` brings it back with its services started.
- [ ] ⬜ **Task 4.2**: Targeted QA (`./scripts/qa-changed.bash`), then the `qa-reviewer` agent.

## Decision gates (owner)

1. **Where the stand-down is decided.** Plugin API v1 has no "end and stay down" result.
   Options: (a) ask upstream hooks-daemon for a new `on_idle` result (for example
   `ExitForStandDown`) and wait for it; (b) the ccy plugin returns `ExitForRestart` with a
   stand-down marker that the ccy launcher reads instead of relaunching; (c) a host-side
   watcher (`ccy-sessions`) polls idleness and runs `ccy --stop`. **Recommendation: (a)**, with
   (b) as an interim only if upstream agrees to it. (b) overloads a restart contract the
   supervisor validates and logs as a restart. (c) cannot see the session's input box or turn
   state any better than the supervisor can.
2. **What counts as idle when the supervisor cannot see it** (depends on Task 1.1). If
   background tasks and subagents are invisible to it, either stand down on PTY quiet alone or
   take that state from a Claude Code hook. **Recommendation:** do not stand down on PTY quiet
   alone. A long background build is quiet. Take the state from hooks if the triage shows they
   report it, and otherwise stand down only after DURATION of PTY quiet plus no output.
3. **Stop or down.** The existing end-of-session "yes" runs `down` (removes containers). The
   issue asks for stopped, not removed. **Recommendation:** a stand-down runs `stop`, and the
   person's existing question keeps `down`.
4. **Where `stood-down` is written.** The restore manifest is per boot, but a stand-down can
   happen any time. Options: a field in the session record (`stood-down=<time>`), or a
   per-session journal file beside the registry. **Recommendation:** a record field, written
   through the registry's one writer. It travels with the record that `--list`, `wake` and
   restore already read, and `verify-restore` can compare its time with the boot's.
5. **Default for declared sessions.** Should `declare` write `--idle-stand-down` when
   `CCY_IDLE_STAND_DOWN` is set, or only when given among CCY-ARGS? **Recommendation:** only
   when given, as with `--compose`. A provisioner states it explicitly.

## Dependencies

- Depends on: the issue #270 defect fixes (branch `ci/issue-270-restore-check`). A session that
  never took a turn must restore fresh, or the reboot after an idle stand-down cannot restore it.
- Gate 1 (a) depends on an upstream hooks-daemon change to the supervisor's plugin API.

## Success Criteria

- [ ] An idle session with `--idle-stand-down` ends by itself after DURATION. Its compose
  services are stopped and not removed, and its record is kept and marked stood-down.
- [ ] `ccy-sessions wake NAME` and the next boot's restore bring it back with its services
  started.
- [ ] `--list`, `verify-restore` and `notify` report it as the goals say.
- [ ] Targeted QA passes, and the `qa-reviewer` agent passes the plan.

## Risks & Mitigations

| Risk                                                         | Impact | Probability | Mitigation                                                         |
| ------------------------------------------------------------ | ------ | ----------- | ------------------------------------------------------------------ |
| A session doing background work is stood down as idle        | H      | M           | Gate 2: never decide on PTY quiet alone                            |
| The daemon-owned supervisor is replaced by an upgrade        | H      | H           | Gate 1: build on the plugin API, never patch the supervisor        |
| A stood-down record is mistaken for a crash, or for a leak   | M      | M           | `stood-down` marker read by `--list`, `verify-restore` and restore |
| `stop` versus `down` differs between compose implementations | M      | L           | Task 1.1 establishes it before building                            |

## Delivery & Milestones

- Plan written; nothing built.
