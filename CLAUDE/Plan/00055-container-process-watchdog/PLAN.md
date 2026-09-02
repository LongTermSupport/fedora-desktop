# Plan 00055: Container Process Watchdog

**Status**: Dormant (blocked on Task 4.2: HOST deploy and verify via the plan-local scripts, which cannot run in the CCY container)
**Created**: 2026-06-24
**Owner**: joseph
**Priority**: Medium

> Lean plan. The full original document, including every audit-round note and
> dated progress entry, is kept verbatim in
> [`PLAN_archive.md`](PLAN_archive.md). Durable design decisions and evidence
> extracted from it live in [`DECISIONS.md`](DECISIONS.md).

## Overview

A host-level, **reporting-only** watchdog that detects processes running inside a
container which are both long-running and CPU-pinned, attributes each to its
container (Podman / Docker / LXC), and surfaces an actionable report via a GNOME
Shell panel/notification **and** a CLI. The human is then guided to resolve the
issue *inside the container directly*. It never kills or throttles anything.

Triggered by a real incident: a single multithreaded `ugrep` (Claude Code Grep
tool with an unscoped `/` path) pinned about half the cores for close to two hours
inside one rootless-Podman CCY container, with no fast way to tell *which*
container was responsible.

**Read [`context.md`](context.md) first** for the incident forensics, the
container-attribution technique, the full-stack architecture, the multi-engine
cgroup-marker table and the repo-integration plan. The acceptance process is in
[`testing.md`](testing.md); the human visual pass is
[`testing-checklist.md`](testing-checklist.md).

## Goals

- Detect container processes that are long-running **and** CPU-pinned
  (configurable thresholds).
- Attribute every flagged process to its container across **Podman, Docker, LXC**.
- Surface findings via **GNOME Shell extension** (panel + notification) **and** a
  **CLI**, sharing one report data source.
- Guide the human to inspect/fix the offender *inside* the container
  (engine-correct `exec` hint + in-container PID).
- Deploy entirely via Ansible on a `systemd --user` timer.

## Non-Goals

- **No** auto-kill, auto-throttle, or CPU caps. Reporting only (caps were
  explicitly rejected as symptom-hiding).
- Not diagnosing *why* Claude's Grep ran against `/` (separate, upstream concern).
- Not a general container metrics dashboard. It is a targeted runaway alerter.

## Tasks

### Phase 0: Decisions (resolve §8 of context.md)

- [x] ✅ **Task 0.1**: DBus via shell-out (`gdbus emit` subprocess); helper stays
  stdlib-only. See [DECISIONS.md D1](DECISIONS.md#d1-dbus-via-shell-out).
- [x] ✅ **Task 0.2**: CPU is per-single-core %, default 50; age default 900 s;
  both env- then config-overridable. See [DECISIONS.md D2](DECISIONS.md#d2-thresholds).
- [x] ✅ **Task 0.3**: Reporting-only confirmed (no kill path in the package);
  allowlist config shape settled. See [DECISIONS.md D3](DECISIONS.md#d3-reporting-only-and-allowlist-shape).
- [x] ✅ **Task 0.4**: Core/CLI split: `helpers/containerwatch` package + thin bin
  wrapper, tests written first. See [DECISIONS.md D4](DECISIONS.md#d4-package-layout-and-tdd-first).

### Phase 1: Reporter core + CLI

- [x] ✅ **Task 1.1**: `/proc` enumeration + cgroup-based container attribution
  (`core.parse_cgroup`, `core.scan_proc_root`; cgroup-v2 only, v1 fails fast,
  `unknown` bucket). Full attribution matrix unit-tested.
- [x] ✅ **Task 1.2**: Age + CPU-delta sampling + threshold logic with a PID-reuse
  guard between samples. The sustained N-tick gate is deferred (YAGNI). See
  [DECISIONS.md D5](DECISIONS.md#d5-pid-reuse-guards-and-the-deferred-sustained-gate).
- [x] ✅ **Task 1.3**: Per-engine name resolution (rootless Podman natively as the
  session user; Docker via group; LXC via sudo) + host-to-container PID via NSpid
  - `exec_hint`. See [DECISIONS.md D6](DECISIONS.md#d6-name-resolution-and-nspid).
- [x] ✅ **Task 1.4**: Atomic `report.json` under `$XDG_RUNTIME_DIR/container-watch/`
  - human report + non-fatal DBus `FindingsChanged` signal.
- [x] ✅ **Task 1.5**: CLI subcommands `scan`/`status`/`list`/`explain`/`watch`;
  smoke-tested end-to-end via the `--inject` seam.
- [x] ✅ **Task 1.6**: Test seams: injectable `proc_root`, env thresholds, stubbable
  resolver + sampler, `scan --json`, `scan --inject`, `cli.engine_available`.
- [x] ✅ **Task 1.7**: QA green (`./scripts/qa-all.bash`, `./scripts/qa-helper-tests.bash`).

### Phase 2: systemd timer

- [x] ✅ **Task 2.1**: `container-watch.service` (oneshot) + `.timer` user units
  under `files/home/.config/systemd/user/`; the repo's first `.timer`. See
  [DECISIONS.md D7](DECISIONS.md#d7-first-timer-unit-in-the-repo).

### Phase 3: GNOME Shell extension

- [x] ✅ **Task 3.1**: Panel indicator + `FindingsChanged` listener (re-reads
  `report.json`) + deduped new-finding notification + per-finding clipboard copy
  of `exec_hint` + poll fallback + full `disable()` cleanup.
  `extensions/container-watch@fedora-desktop/extension.js`.
- [x] ✅ **Task 3.2**: `metadata.json` shell-version range covers GNOME 50;
  `check_extension_compat` passes.
- [x] ✅ **Task 3.3**: ESLint clean; async-only, no blocking calls.

### Phase 4: Ansible deployment

- [x] ✅ **Task 4.1**: `play-container-watch.yml` (opt-in, not imported by
  `playbook-main.yml`) deploys helper, wrapper, user units and the extension via
  a recursive directory copy, and enables the user timer + extension. See
  [DECISIONS.md D8](DECISIONS.md#d8-deployment-wiring-and-file-list-rule).
- [ ] 🔄 **Task 4.2**: (On HOST, not CCY) deploy + verify via `deploy.bash` then
  `triage.bash`. Deploy, timer, CLI and the L2 `acceptance.bash` run are
  confirmed GREEN on the HOST for podman, docker and lxc (see
  [DECISIONS.md E1](DECISIONS.md#e1-host-verification-evidence)). **Still to
  confirm**: the L3 visual pass per `testing-checklist.md`.

### Phase 5: Testing & acceptance (full process in [`testing.md`](testing.md))

- [x] ✅ **Task 5.1**: L0 static no-kill gate `scripts/qa-nokill-containerwatch.bash`,
  wired into `qa-all.bash` as a hard gate before the `jq` merge, with `--self-test`.
  See [DECISIONS.md D9](DECISIONS.md#d9-no-kill-gate-design).
- [x] ✅ **Task 5.2**: L1 unit suite `tests/helpers/containerwatch/` covering the
  attribution matrix, detection, schema and regressions; in-memory fixtures with
  reserved placeholders. See [DECISIONS.md D10](DECISIONS.md#d10-public-repo-fixture-hygiene).
- [x] ✅ **Task 5.3**: L2 host integration `acceptance.bash` (plan-local, engine-gated,
  throwaway burner per engine, behavioural survive-assert, `--lxc` fast path).
  Execution is HOST-only (part of Task 4.2).
- [x] ✅ **Task 5.4**: L3 human `testing-checklist.md` written. Execution is
  HOST-only (nested GNOME Shell), pending.

## Dependencies

- None blocking. Reuses the DBus-CLI to extension pattern from
  `speech-to-text@fedora-desktop`.

## Success Criteria

- [ ] A synthetic long+hot container process is flagged within one timer interval
  after the age threshold is met (verified via `CW_AGE_S` threshold injection).
- [x] Finding correctly names the container for a Podman **and** a Docker **and** an
  LXC container (L2 GREEN on HOST for all three).
- [ ] Report reachable from both the GNOME panel and the CLI.
- [x] No kill/throttle path exists in the shipped tool (L0 gate + L2 survive-assert).
- [ ] QA passes (`./scripts/qa-all.bash`, ESLint, `check_extension_compat`).
- [ ] Acceptance passes per [`testing.md`](testing.md) §6: L0 + L1 + L2 green, L3
  visual/walkthrough confirmed.

## Risks & Mitigations

| Risk                                             | Impact | Probability | Mitigation                                                              |
| ------------------------------------------------ | ------ | ----------- | ----------------------------------------------------------------------- |
| stdlib-only vs DBus (`gi`) conflict              | M      | M           | Shell out to `gdbus` (Task 0.1)                                         |
| False positives from legitimate long/hot jobs    | M      | H           | Allowlist config + optional sustained gate (Task 0.3 / 1.2)             |
| Rootless-Podman name resolution needs owning uid | M      | M           | User timer **is** the owning uid, so native `podman inspect` (Task 1.3) |
| GNOME Shell API drift / version gate             | L      | M           | `check_extension_compat` + ESLint in QA (Task 3.2/3.3)                  |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00055-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Two audit rounds converged ([`plan-review-1.md`](plan-review-1.md),
  [`plan-review-2.md`](plan-review-2.md)).
- Phases 0 to 5 authored and QA-green inside the CCY container.
- HOST deploy confirmed; L2 acceptance GREEN for podman, docker and lxc.
- Remaining: L3 visual pass on the HOST, then mark Task 4.2 ✅, set Complete and
  move to `CLAUDE/Plan/Completed/`.
- Plan slimmed and marked Dormant; history in [`PLAN_archive.md`](PLAN_archive.md)
  and [`JOURNAL/`](JOURNAL/00055-Journal-26-09-02.md).
