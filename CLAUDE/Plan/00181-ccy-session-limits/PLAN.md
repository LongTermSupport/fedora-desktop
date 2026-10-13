# Plan 00181: ccy session limits: memory, admission by free RAM, and age limits closed out

**Status**: Not Started
**Created**: 2026-10-13
**Owner**: joseph
**Priority**: Medium

Addresses #67 and #61.

## Overview

ccy puts no limit on a session container's memory and no limit on how many sessions run at
once (#67). On a host with modest RAM, enough sessions exhaust memory. The OOM killer can then
pick systemd's user manager before a large `claude` process. That takes down the tmux server
and every ccy session in it, while the containers keep running with no client: the next launch
reports them as orphaned and the work is lost together. #67 asks for four things. A
per-session `--memory`/`--memory-swap` limit (`CCY_MEMORY_LIMIT`, default and overrides). Host
admission control that refuses a new session when available RAM per session falls below a
threshold (default 3 GiB). A restore after reboot that applies the same check. And host-wide
totals in `ccy-sessions`.

#61 (restart after a maximum age, `--run-for`/`--until`) has shipped: `lib/session-lifecycle.bash`,
ccy 3.77.1 and later, with the hooks-daemon launcher accepted from ccy 3.82.1. The issue's
comments record a passing host live test. Three cases were handed back to this repository
because they exercise the ccy launcher: the key-file relaunch, the passphrase key, and the
deadline after a restart (`CCY_RELAUNCH_DEADLINE_EPOCH` with the plugin's per-session
"announced" marker). Both issues limit what a session may consume, so Phase 1 closes #61's
remaining checks here instead of opening a plan for three host tests.

The restore path is Plan 00135's. Section 3 of #67 changes `ccy-sessions restore`, so that
task must land with 00135's restore tests passing.

## Goals

- A runaway session is killed alone, inside its own cgroup, and says so.
- A new session is refused, with the numbers shown, when the host cannot afford it; an
  explicit override flag exists.
- A restore starts only the sessions that fit and lists the rest, never dropping a record.
- `ccy-sessions` shows each session's memory and a host-wide footer.
- #61's three remaining launcher cases pass on the host.

## Non-Goals

- Plan 00055's container process watchdog (a different problem).
- New lifecycle features for #61 beyond its three open checks.
- Changing the supervisor or its plugin API (hooks-daemon's).

## Tasks

### Phase 1: #61's remaining host checks

- [ ] ⬜ **Task 1.1**: `acceptance.bash` cases for the key-file relaunch, the passphrase key,
  and a `--run-for` deadline that survives a max-age restart (announced once, not twice).
  Anything needing a person at the screen is named NOT ESTABLISHABLE.
- [ ] ⬜ **Task 1.2**: Host run through `meta-deploy.bash`. A failure in the supervisor or
  plugin API is filed upstream on the hooks-daemon tracker, per #61's last comment.

### Phase 2: Triage for #67

- [ ] ⬜ **Task 2.1**: `triage.bash`: host total and available memory, swap, the running ccy
  containers (`label=ccy=true`) and their `podman stats` memory, and the user manager's
  `OOMScoreAdjust` against a `claude` process's score.

### Phase 3: Memory limit and admission (tests first)

- [ ] ⬜ **Task 3.1**: `CCY_MEMORY_LIMIT` (default, `ccy.env` per project, provisioner per host)
  passed as `--memory` and `--memory-swap` (no swap by default). A session killed by its
  limit says so.
- [ ] ⬜ **Task 3.2**: Admission check from available RAM and the running session count,
  threshold configurable, refusal with the numbers and a pointer to `ccy-sessions`, and an
  override flag.
- [ ] ⬜ **Task 3.3**: `ccy-sessions restore` applies the same check: sessions that do not fit
  stay recorded and are reported (with Plan 00135's restore tests green).
- [ ] ⬜ **Task 3.4**: `ccy-sessions` and `--list`: a memory column and a host footer (RAM,
  swap, load, ccy totals, the admission numbers).

### Phase 4: Ship

- [ ] ⬜ **Task 4.1**: `docs/ccy.md`, the changelog, the CCY version bump; `deploy.bash`
  (`play-claude-yolo.yml`) and an entry in `meta-deploy.bash`.
- [ ] ⬜ **Task 4.2**: `qa-all.bash`, then the `qa-reviewer` agent over the plan's diff.

## Success Criteria

- [ ] A session growing past `CCY_MEMORY_LIMIT` is killed alone; other sessions and the user
  manager survive.
- [ ] With available memory per session below the threshold, a launch is refused with the
  numbers shown.
- [ ] A restore with more records than fit starts only those that fit and lists the rest.
- [ ] #61's three launcher cases pass on the host.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00181-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed from #67 and #61's handed-back checks.
