# Plan 00055: Container Process Watchdog

**Status**: Not Started
**Created**: 2026-06-24
**Owner**: joseph
**Priority**: Medium

## Overview

A host-level, **reporting-only** watchdog that detects processes running inside a
container which are both long-running (≥ 15 min) and CPU-pinned, attributes each to
its container (Podman / Docker / LXC), and surfaces an actionable report via a
GNOME Shell panel/notification **and** a CLI. The human is then guided to resolve
the issue *inside the container directly*. It never kills or throttles anything.

Triggered by a real incident: a single multithreaded `ugrep` (Claude Code Grep
tool with an unscoped `/` path) pinned ~11 of 22 cores for ~1.9 h inside one
rootless-Podman CCY container, and there was no fast way to tell *which* container
was responsible.

**Read [`context.md`](context.md) first** — it carries the full incident forensics,
the container-attribution technique (cgroup parsing, NSpid translation, ancestry
cross-check, the `comm` gotcha), the proposed full-stack architecture, the
multi-engine cgroup-marker table, repo-integration plan, and the open design
decisions.

## Goals

- Detect container processes that are long-running **and** CPU-pinned (configurable
  thresholds; defaults 15 min / sustained high CPU).
- Attribute every flagged process to its container across **Podman, Docker, LXC**.
- Surface findings via **GNOME Shell extension** (panel + notification) **and** a
  **CLI** — sharing one report data source.
- Guide the human to inspect/fix the offender *inside* the container (engine-correct
  `exec` hint + in-container PID).
- Deploy entirely via Ansible on a `systemd --user` timer.

## Non-Goals

- **No** auto-kill, auto-throttle, or CPU caps — reporting only (caps were
  explicitly rejected as symptom-hiding).
- Not diagnosing *why* Claude's Grep ran against `/` (separate concern, upstream).
- Not a general container metrics dashboard — it is a targeted runaway alerter.

## Tasks

### Phase 0: Decisions (resolve §8 of context.md)

- [ ] ⬜ **Task 0.1**: Decide DBus emission strategy (shell-out `gdbus`/`busctl` vs
  `gi` vs poll) to keep core stdlib-only.
- [ ] ⬜ **Task 0.2**: Confirm CPU threshold semantics (per-core %) + default, and
  the age default (900 s).
- [ ] ⬜ **Task 0.3**: Confirm reporting-only (no kill path at all) + allowlist
  config shape for legitimate long/hot jobs.
- [ ] ⬜ **Task 0.4**: Decide core/CLI split (single bin vs `helpers/` package +
  bin wrapper) and final file paths.

### Phase 1: Reporter core + CLI

- [ ] ⬜ **Task 1.1**: `/proc` enumeration + cgroup-based container attribution
  (engine + id/name + rootless uid), per the §6 marker table.
- [ ] ⬜ **Task 1.2**: Age (btime) + CPU-delta sampling + threshold logic →
  findings.
- [ ] ⬜ **Task 1.3**: Per-engine name resolution (rootless-Podman as owning uid;
  Docker via group; LXC via sudo) + host→container PID (NSpid) + `exec_hint`.
- [ ] ⬜ **Task 1.4**: Emit `report.json` (atomic) + human report + DBus signal.
- [ ] ⬜ **Task 1.5**: CLI subcommands: `scan`, `status`, `list`, `explain`,
  `watch`.
- [ ] ⬜ **Task 1.6**: Build the **test seams** (required by [`testing.md`](testing.md) §1):
  injectable `proc_root`, env-overridable thresholds (`CW_AGE_S`/`CW_CPU_PCT`),
  stubbable name-resolver, `scan --once --json`, `scan --inject <finding>`, and an
  engine-presence probe.
- [ ] ⬜ **Task 1.7**: Run QA: `./scripts/qa-all.bash` (+ `qa-helper-tests.bash`
  if a helper package is added).

### Phase 2: systemd timer

- [ ] ⬜ **Task 2.1**: `container-watch.service` (oneshot) + `.timer` (default
  2 min) user units.

### Phase 3: GNOME Shell extension

- [ ] ⬜ **Task 3.1**: Panel indicator + DBus listener + notification + finding
  menu with copyable `exec_hint`.
- [ ] ⬜ **Task 3.2**: `metadata.json` with correct `shell-version`; pass
  `python3 -m helpers.gnome.check_extension_compat`.
- [ ] ⬜ **Task 3.3**: ESLint clean (`extensions/node_modules/.bin/eslint`).

### Phase 4: Ansible deployment

- [ ] ⬜ **Task 4.1**: `play-container-watch.yml` deploys files, enables user
  timer + extension; import from `playbook-main.yml`.
- [ ] ⬜ **Task 4.2**: (On HOST, not CCY) deploy + verify timer fires, report
  generates, panel + CLI both show a synthetic finding.

### Phase 5: Testing & acceptance (full process in [`testing.md`](testing.md))

- [ ] ⬜ **Task 5.1**: L0 static — add the **no-kill safety guard** (CI + grep/semgrep
  rule asserting no `os.kill`/`send_signal`/`pkill` path aimed at findings).
- [ ] ⬜ **Task 5.2**: L1 unit — `/proc`-fixture suite covering the full attribution
  matrix (podman rootless/rootful, docker systemd+cgroupfs drivers, lxc, host),
  detection logic, schema, and the `comm`/NSpid/allowlist regression cases; wired
  into `./scripts/qa-helper-tests.bash`.
- [ ] ⬜ **Task 5.3**: L2 host integration — `scripts/acceptance-container-watch.bash`:
  threshold-overridden, engine-gated, spins a throwaway CPU-burner container per
  available engine and asserts flag + attribution + `exec_hint` + NSpid + the
  behavioural **safety** assert (burner survives) + allowlist + DBus + systemd;
  trap-cleans all `cw-test-*` containers.
- [ ] ⬜ **Task 5.4**: L3 human — scripted nested-GNOME visual check (panel +
  notification + dedupe, driven by `scan --inject`) plus one real guided-resolution
  walkthrough; captured as a numbered `testing-checklist.md`.

## Dependencies

- None blocking. Reuses the DBus-CLI↔extension pattern from
  `speech-to-text@fedora-desktop`.

## Success Criteria

- [ ] A synthetic long+hot container process is flagged within one timer interval.
- [ ] Finding correctly names the container for a Podman **and** a Docker **and** an
  LXC container.
- [ ] Report reachable from both the GNOME panel and the CLI.
- [ ] No kill/throttle path exists in the shipped tool (enforced by the L0 no-kill
  guard, confirmed by the L2 behavioural survive-assert).
- [ ] QA passes (`./scripts/qa-all.bash`, ESLint, `check_extension_compat`).
- [ ] Acceptance passes per [`testing.md`](testing.md) §6: L0 + L1 + L2 green, L3
  visual/walkthrough confirmed — with no 15-minute waits and no hand-crafted
  runaways (threshold-injection + `--inject` seam).

## Risks & Mitigations

| Risk                                             | Impact | Probability | Mitigation                                             |
| ------------------------------------------------ | ------ | ----------- | ------------------------------------------------------ |
| stdlib-only vs DBus (`gi`) conflict              | M      | M           | Shell out to `gdbus`/`busctl` (Task 0.1)               |
| False positives from legitimate long/hot jobs    | M      | H           | Allowlist config + "sustained" gate (Task 0.3 / 1.2)   |
| Rootless-Podman name resolution needs owning uid | M      | M           | Extract uid from cgroup, query as that user (Task 1.3) |
| GNOME Shell API drift / version gate             | L      | M           | `check_extension_compat` + ESLint in QA (Task 3.2/3.3) |

## Notes & Updates

### 2026-06-24

- Plan scaffolded as `00055-ccy-runaway-grep-cpu-containment`, then **renamed** to
  `00055-container-process-watchdog` — the direction is a reporting watchdog, not
  CPU containment (caps explicitly rejected).
- Authored [`context.md`](context.md) with full incident forensics, the
  container-attribution technique, full-stack design, and multi-engine support.
- Authored [`testing.md`](testing.md) — 4-layer acceptance process (L0 static / L1
  `/proc`-fixture units / L2 host integration with throwaway burner containers / L3
  scripted human visual). Drives human effort to ~3–5 min by building in test seams
  (injectable `proc_root`/thresholds, `scan --inject`) so there are no 15-minute
  waits and no hand-crafted runaways. Added Phase 5 tasks and acceptance criteria.
