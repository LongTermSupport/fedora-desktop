# Plan 00055: Container Process Watchdog

**Status**: In Progress
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
- [ ] ⬜ **Task 0.4**: Decide core/CLI split — **pre-resolved toward `helpers/`
  package + thin bin wrapper**: it is the only layout that gets the rich L1 unit
  matrix into `qa-helper-tests.bash` (collected from
  `tests/helpers/containerwatch/test_*.py`), and testing.md §3 already assumes it.
  **TDD-first constraint**: under `helpers/CLAUDE.md` the package is stdlib-only and
  tests must be written in `tests/helpers/containerwatch/test_*.py` **before** the
  source files, or the TDD handler blocks source creation. Confirm final file paths.

### Phase 1: Reporter core + CLI

- [ ] ⬜ **Task 1.1**: `/proc` enumeration + cgroup-based container attribution
  (engine + id/name + rootless uid), per the §6 marker table.
- [ ] ⬜ **Task 1.2**: Age (btime) + CPU-delta sampling + threshold logic →
  findings. **Guard PID reuse between the two CPU samples**: re-validate process
  identity across the ~1–3 s interval (compare `/proc/<pid>/stat` field 22
  `starttime`, or skip if the process vanished) before computing a delta — a recycled
  PID would otherwise yield a bogus `cpu_pct`. **Sustained-gate PID-reuse** (R5): if
  the optional "sustained over N consecutive timer ticks" gate (§4a) is built, the
  persisted per-PID record must include `starttime` (field 22) in its identity key —
  PID reuse is far more likely across the minutes-apart timer firings than across the
  ~1–3 s in-invocation samples, and a `starttime` mismatch must **reset** the
  consecutive-hot counter, not carry it over to an innocent recycled PID.
- [ ] ⬜ **Task 1.3**: Per-engine name resolution (rootless-Podman: native
  `podman inspect` **as the session user** under the chosen user-timer model — no
  `runuser` uid hop, which is root-only and reserved for the system-level fallback;
  Docker via group; LXC via sudo) + host→container PID (NSpid; only emit
  `container_pid` when attributed to a container **and** NSpid has ≥2 fields — see
  Task 1.3 NSpid notes / context.md §2c) + `exec_hint`.
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
  2 min) user units. **This is the repo's first `.timer`** (only `.service` user
  units exist today, e.g. qobuz `rescrobbled.service`) — so: model the `.service` on
  qobuz's `copy: content:` pattern; enable the **`.timer`** (not the `.service`) with
  `ansible.builtin.systemd: scope: user, enabled: yes, state: started, daemon_reload: true`, `WantedBy=timers.target`; verify with
  `systemd-analyze --user verify` (testing.md §4).

### Phase 3: GNOME Shell extension

- [ ] ⬜ **Task 3.1**: Panel indicator + DBus listener + notification + finding
  menu with copyable `exec_hint`.
- [ ] ⬜ **Task 3.2**: `metadata.json` with correct `shell-version`; pass
  `python3 -m helpers.gnome.check_extension_compat`.
- [ ] ⬜ **Task 3.3**: ESLint clean (`extensions/node_modules/.bin/eslint`).

### Phase 4: Ansible deployment

- [ ] ⬜ **Task 4.1**: `play-container-watch.yml` deploys files (copy extension
  from `{{ root_dir }}/extensions/container-watch@fedora-desktop/` like
  `play-speech-to-text.yml`), enables the user timer + extension. **Wiring**: do
  **not** assume a `playbook-main.yml` import — the comparable
  `play-speech-to-text.yml` has none. Pick the real integration point: either add
  the deploy tasks into `play-gnome-shell-extensions.yml` (always-on inline pattern)
  **or** ship an opt-in optional play; verify against `playbook-main.yml` at
  implementation time. **Deploy-file-list rule** (`extensions/CLAUDE.md`): a new
  extension's whole file set is new, so prefer a **recursive directory `copy:`** of
  `{{ root_dir }}/extensions/container-watch@fedora-desktop/` (matches
  `play-gnome-shell-extensions.yml`'s `workspace-names-overview` pattern, auto-includes
  every file). If instead using an explicit per-file loop (like `play-speech-to-text.yml`),
  **enumerate every file** (`extension.js` + `metadata.json` + any `prefs.js`/schemas) —
  any file omitted from the loop silently never deploys.
- [ ] ⬜ **Task 4.2**: (On HOST, not CCY) deploy + verify timer fires, report
  generates, panel + CLI both show a synthetic finding.

### Phase 5: Testing & acceptance (full process in [`testing.md`](testing.md))

- [ ] ⬜ **Task 5.1**: L0 static — add the **no-kill safety guard** as a small
  **`grep`-based bash gate**. **Wiring is a structural aggregator edit, not a
  drop-in** (R1): `qa-all.bash` is a hardcoded 6-stage pipeline (six `mktemp` vars,
  a 6-path `trap`, six `rc=0…||rc=$?` blocks, a `jq -s` merge keyed by positional
  index `.[0]`–`.[5]`). Adding a 7th top-level stage means editing all five points
  (new `TMP_*` var, extended `trap`, new rc-block, new `.[6]` key, new `checks.nokill`
  entry) — **or**, lower-risk, fold the no-kill grep into the existing
  `qa-patterns.bash`/`qa-bash.bash` stage instead of standing up a 7th stage. The
  grep must target **executable call sites** (`os.kill(`, `signal.SIG`,
  `.send_signal(`, `Gio.Subprocess` `force_exit`/`send_signal`, `pkill`/`kill ` as a
  spawned argv) and **exclude** `kill` inside `exec_hint`/guidance string literals,
  with a self-test fixture (a benign `kill`-in-a-hint string that must PASS) mirroring
  how `.semgrep/bash-conventions.bash` self-tests (R4).
  **Not** semgrep: the repo has no Python/JS semgrep ruleset, only the bash-scoped
  one (testing.md §2).
- [ ] ⬜ **Task 5.2**: L1 unit — `/proc`-fixture suite covering the full attribution
  matrix (podman rootless/rootful, docker systemd+cgroupfs drivers, lxc, host),
  detection logic, schema, and the `comm`/NSpid/allowlist regression cases; wired
  into `./scripts/qa-helper-tests.bash`. **Public-repo fixture hygiene** (R3): all
  committed fixtures (`tests/.../fixtures/proc/<pid>/cmdline`) and `--inject` sample
  finding JSON use reserved placeholders per `CLAUDE/ExampleValues.md` (`<project-a>`,
  `example.com` paths, synthetic container IDs) — **never** a real container name,
  workspace path, or argv captured from an actual incident. (`report.json` itself is a
  runtime artifact under `$XDG_RUNTIME_DIR`, never committed — the leak surface is the
  committed fixtures, which can bypass the email/IP-shaped commit scanner.)
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

- [ ] A synthetic long+hot container process is flagged **within one timer interval
  after the age threshold is met** (verified via threshold injection — `CW_AGE_S`).
  Detection latency = max(age_threshold, sustained-window) + ≤ 1 timer interval; the
  2-min cadence does **not** shorten the 15-min age gate.
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

| Risk                                             | Impact | Probability | Mitigation                                                                                                                                  |
| ------------------------------------------------ | ------ | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| stdlib-only vs DBus (`gi`) conflict              | M      | M           | Shell out to `gdbus`/`busctl` (Task 0.1)                                                                                                    |
| False positives from legitimate long/hot jobs    | M      | H           | Allowlist config + "sustained" gate (Task 0.3 / 1.2)                                                                                        |
| Rootless-Podman name resolution needs owning uid | M      | M           | User timer **is** the owning uid → native `podman inspect`, no uid hop; `runuser`-as-uid reserved for root system-level fallback (Task 1.3) |
| GNOME Shell API drift / version gate             | L      | M           | `check_extension_compat` + ESLint in QA (Task 3.2/3.3)                                                                                      |

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
- **Audit round 1** complete — see [`plan-review-1.md`](plan-review-1.md). Started
  the audit/revise loop; Status moved Not Started → In Progress. Applied revisions
  resolving all 12 findings:
  - **F1/F4** (high/medium): extension source lives under `extensions/<uuid>/`, not
    `files/home/.local/share/gnome-shell/extensions/`; dropped the unfounded
    `playbook-main.yml` import claim — wiring is via `play-gnome-shell-extensions.yml`
    inline **or** an opt-in optional play (context.md §7, Task 4.1).
  - **F2** (high): resolved the user-timer vs cross-uid tension — the `systemd --user`
    timer **is** the rootless-Podman owning uid, so resolution is native
    `podman inspect` with no uid hop; `runuser -u "#<uid>"` is root-only and reserved
    for the system-level fallback (context.md §6, Task 1.3, Risks).
  - **F3** (medium): DBus namespace corrected to all-lowercase
    `org.fedoradesktop.ContainerWatch` to match the speech-to-text precedent.
  - **F5** (medium): no-kill guard scoped as a `grep`-based bash gate in
    `qa-all.bash` — the repo has no Python/JS semgrep infra (testing.md §2, Task 5.1).
  - **F6** (medium): cgroup-v2-only with explicit fail-fast on v1 lines and an
    `engine: "unknown"` logged-but-not-host bucket; added two L1 fixture rows.
  - **F7/F10** (medium/low): NSpid parsing specified — emit `container_pid` only when
    attributed AND ≥2 fields, take the 2nd field (first nested level),
    `null`/omit (never `0`) when untranslated; added single-field + 3-field L1 cases.
  - **F8** (low): PID-reuse guard between the two CPU samples (`starttime` re-check)
    - L1 cases (Task 1.2, testing.md §3).
  - **F9** (low): noted this is the repo's first `.timer`; enable the `.timer` with
    `daemon_reload` + `WantedBy=timers.target` (Task 2.1).
  - **F11** (low): documented detection latency =
    max(age_threshold, sustained-window) + ≤1 timer interval; reworded the success
    criterion (context.md §4a, Success Criteria).
  - **F12** (low): pre-resolved Task 0.4 toward the `helpers/` package + bin wrapper
    and flagged the TDD-first (tests-before-source) constraint.
- **Audit round 2** complete — see [`plan-review-2.md`](plan-review-2.md).
  **Converged**: all 12 round-1 findings verified resolved against repo reality, no
  regressions, no new high/medium issues. Folded the five residual LOW
  implementation-precision findings into the relevant tasks as one-line clarifications
  (no further full revision round needed):
  - **R1** (low): noted that wiring the no-kill gate into `qa-all.bash` is a
    structural five-point aggregator edit (`TMP_*` var + `trap` path + rc-block +
    positional `jq` `.[6]` key + `checks.nokill`), with the lower-risk option of
    folding it into the existing `qa-patterns.bash`/`qa-bash.bash` stage (Task 5.1,
    testing.md §2).
  - **R2** (low): surfaced the `extensions/CLAUDE.md` deploy-file-list rule in
    Task 4.1 — prefer a recursive directory `copy:` (auto-includes every file); if a
    per-file loop is used, enumerate every file or it silently never deploys.
  - **R3** (low): added public-repo fixture-hygiene note — all committed
    fixtures + `--inject` samples use `CLAUDE/ExampleValues.md` placeholders, never a
    real container name/workspace path/argv (Task 5.2, testing.md §3).
  - **R4** (low): scoped the no-kill grep to executable call sites
    (`os.kill(`/`signal.SIG`/`.send_signal(`/`Gio.Subprocess` force-exit/`pkill`),
    excluding `kill` inside `exec_hint` guidance literals, with a self-test fixture
    (Task 5.1, testing.md §2).
  - **R5** (low): noted that if the optional sustained gate is built, its persisted
    per-PID record must key on `starttime` (field 22) and reset the consecutive-hot
    counter on mismatch — PID reuse is likelier across the minutes-apart ticks
    (Task 1.2, context.md §4a).
