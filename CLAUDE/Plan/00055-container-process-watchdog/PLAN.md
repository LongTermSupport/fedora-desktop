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

- [x] ✅ **Task 0.1**: DBus via shell-out — `cli.emit_signal` runs `gdbus emit`
  (subprocess), so the helper stays stdlib-only (no `gi`). Matches the
  speech-to-text precedent; emission is best-effort (non-fatal if no session bus).
- [x] ✅ **Task 0.2**: CPU is **per-single-core %** (`compute_cpu_pct`), default
  `DEFAULT_CPU_PCT = 50`; age default `DEFAULT_AGE_S = 900`. Both env-overridable
  (`CW_CPU_PCT`/`CW_AGE_S`) then config-overridable.
- [x] ✅ **Task 0.3**: Reporting-only confirmed — **no** kill path anywhere in the
  package (no `os.kill`/`send_signal`/`pkill`). Allowlist config shape settled:
  `config.json` `{"allowlist": [{"container_name": "...", "cmd_pattern": "<glob>"}]}`
  (`core.matches_allowlist`; both fields optional, AND when both present).
- [x] ✅ **Task 0.4**: Decide core/CLI split — **pre-resolved toward `helpers/`
  package + thin bin wrapper**: it is the only layout that gets the rich L1 unit
  matrix into `qa-helper-tests.bash` (collected from
  `tests/helpers/containerwatch/test_*.py`), and testing.md §3 already assumes it.
  **TDD-first constraint**: under `helpers/CLAUDE.md` the package is stdlib-only and
  tests must be written in `tests/helpers/containerwatch/test_*.py` **before** the
  source files, or the TDD handler blocks source creation. Confirm final file paths.

### Phase 1: Reporter core + CLI

- [x] ✅ **Task 1.1**: `/proc` enumeration + cgroup-based container attribution
  (engine + id/name + rootless uid), per the §6 marker table —
  `core.parse_cgroup` + `core.scan_proc_root` (cgroup-v2; v1 → `CgroupV1Error`;
  `unknown` bucket for kubepods/cri-o). Full attribution matrix unit-tested.
- [x] ✅ **Task 1.2**: Age (btime) + CPU-delta sampling + threshold logic →
  findings (`core.compute_age_s`, `core.cpu_delta_pct`, `core.is_flagged`,
  `cli.make_cpu_sampler`). **PID-reuse guard between the two CPU samples** built
  (`starttime` re-check → `None` skip; negative delta → `None`); unit-tested. The
  **optional sustained N-tick gate is deferred** (YAGNI — start with the
  single-invocation sample per context.md §4a; add the persisted, `starttime`-keyed
  per-PID gate from R5 only if real noise appears). Original guard note retained
  below for when it is built: re-validate process
  identity across the ~1–3 s interval (compare `/proc/<pid>/stat` field 22
  `starttime`, or skip if the process vanished) before computing a delta — a recycled
  PID would otherwise yield a bogus `cpu_pct`. **Sustained-gate PID-reuse** (R5): if
  the optional "sustained over N consecutive timer ticks" gate (§4a) is built, the
  persisted per-PID record must include `starttime` (field 22) in its identity key —
  PID reuse is far more likely across the minutes-apart timer firings than across the
  ~1–3 s in-invocation samples, and a `starttime` mismatch must **reset** the
  consecutive-hot counter, not carry it over to an innocent recycled PID.
- [x] ✅ **Task 1.3**: Per-engine name resolution (rootless-Podman: native
  `podman inspect` **as the session user** under the chosen user-timer model — no
  `runuser` uid hop, which is root-only and reserved for the system-level fallback;
  Docker via group; LXC via sudo) + host→container PID (NSpid; only emit
  `container_pid` when attributed to a container **and** NSpid has ≥2 fields — see
  Task 1.3 NSpid notes / context.md §2c) + `exec_hint`.
- [x] ✅ **Task 1.4**: Emit `report.json` (atomic `tmp`+`os.replace` to
  `$XDG_RUNTIME_DIR/container-watch/`) + human report (`render_list`/`render_explain`)
  - DBus `FindingsChanged` signal (`cli.emit_signal`, non-fatal).
- [x] ✅ **Task 1.5**: CLI subcommands `scan`/`status`/`list`/`explain`/`watch`
  (`cli.build_parser`); smoke-tested end-to-end via the `--inject` seam.
- [x] ✅ **Task 1.6**: Test seams built — injectable `proc_root`
  (`CW_PROC_ROOT` env / `scan_proc_root` arg), env thresholds
  (`CW_AGE_S`/`CW_CPU_PCT`), stubbable name-resolver + cpu-sampler callables,
  `scan --json`, `scan --inject <file|empty>`, and `cli.engine_available` probe.
- [x] ✅ **Task 1.7**: QA green — `./scripts/qa-all.bash` (334 files) and
  `./scripts/qa-helper-tests.bash` (148 tests incl. 60 new containerwatch tests).

### Phase 2: systemd timer

- [x] ✅ **Task 2.1**: `container-watch.service` (oneshot) + `.timer` (default
  2 min) user units created under
  `files/home/.config/systemd/user/`. **This is the repo's first `.timer`** (only `.service` user
  units exist today, e.g. qobuz `rescrobbled.service`) — so: model the `.service` on
  qobuz's `copy: content:` pattern; enable the **`.timer`** (not the `.service`) with
  `ansible.builtin.systemd: scope: user, enabled: yes, state: started, daemon_reload: true`, `WantedBy=timers.target`; verify with
  `systemd-analyze --user verify` (testing.md §4).

### Phase 3: GNOME Shell extension

- [x] ✅ **Task 3.1**: Panel indicator (neutral/attention) + session-bus
  `FindingsChanged` listener (re-reads `report.json` as the source of truth) +
  new-finding notification (deduped on `host_pid:container_id`) + per-finding menu
  item that copies the `exec_hint` to the clipboard + 60 s poll fallback + full
  `disable()` cleanup. `extensions/container-watch@fedora-desktop/extension.js`.
- [x] ✅ **Task 3.2**: `metadata.json` declares `shell-version`
  `["45"…"50"]`; `python3 -m helpers.gnome.check_extension_compat` passes
  (F44 → GNOME 50 covered).
- [x] ✅ **Task 3.3**: ESLint clean
  (`extensions/node_modules/.bin/eslint container-watch@fedora-desktop/extension.js`
  → 0 findings; async-only, no blocking calls).

### Phase 4: Ansible deployment

- [x] ✅ **Task 4.1**: `play-container-watch.yml` deploys files (copy extension
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
- [ ] 🔄 **Task 4.2**: (On HOST, not CCY) deploy + verify. Use the plan-local
  `deploy.bash` (runs the play) then `triage.bash` (forces a scan, shows the timer
  schedule + findings). **Confirmed on HOST**: deploy succeeded, the
  `container-watch.timer` is registered and has fired, the oneshot service runs,
  `container-watch status`/`list` work (`OK — 0 findings`, the healthy steady state),
  **and the L2 `acceptance.bash` run is fully GREEN for all three engines** — podman,
  docker, and lxc (the last against the real `bl-admin` container): detection,
  attribution (engine/name/in-container NSpid), `cmd`/`age`/`cpu`, engine-correct
  `exec_hint`, and the behavioural **safety** survive-assert all pass; allowlist +
  DBus + systemd cross-cutting checks pass. **Still to confirm**: the L3 visual pass
  (panel attention state + notification + dedupe via `container-watch scan --inject`,
  in a nested GNOME Shell, per `testing-checklist.md`).

### Phase 5: Testing & acceptance (full process in [`testing.md`](testing.md))

- [x] ✅ **Task 5.1**: L0 static — `scripts/qa-nokill-containerwatch.bash` greps
  `helpers/containerwatch/*.py` + the extension JS for executable termination
  call sites (`os.kill(`/`signal.SIG`/`.send_signal(`/`force_exit`/spawned
  `kill`/`pkill`), excludes `kill` inside guidance/`exec_hint` literals, and
  self-tests both directions (`--self-test`). Wired into `qa-all.bash` as a
  minimal **hard gate** before the `jq` merge (not a 7th positional stage), so it
  can't corrupt the `.[0]`…`.[5]` merge. Gate + self-test green on current code.
  The chosen "minimal hard gate before the merge" follows the R1 analysis (a 7th
  positional stage was deliberately avoided). Original design notes for reference:
  **Wiring is a structural aggregator edit, not a drop-in** (R1): `qa-all.bash` is a hardcoded 6-stage pipeline (six `mktemp` vars,
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
- [x] ✅ **Task 5.2**: L1 unit — `/proc`-fixture suite covering the full attribution
  matrix (podman rootless/rootful, docker systemd+cgroupfs drivers, lxc, host,
  **unknown**), detection logic, schema, and the `comm`/NSpid/allowlist/PID-reuse
  regression cases; wired into `./scripts/qa-helper-tests.bash`
  (`tests/helpers/containerwatch/test_core.py` + `test_cli.py`, 60 tests). Fixtures
  built in-memory with reserved placeholders — no committed `fixtures/` tree, so no
  real argv/paths land in git. **Public-repo fixture hygiene** (R3): all
  committed fixtures (`tests/.../fixtures/proc/<pid>/cmdline`) and `--inject` sample
  finding JSON use reserved placeholders per `CLAUDE/ExampleValues.md` (`<project-a>`,
  `example.com` paths, synthetic container IDs) — **never** a real container name,
  workspace path, or argv captured from an actual incident. (`report.json` itself is a
  runtime artifact under `$XDG_RUNTIME_DIR`, never committed — the leak surface is the
  committed fixtures, which can bypass the email/IP-shaped commit scanner.)
- [x] ✅ **Task 5.3**: L2 host integration — `acceptance.bash` (in this plan folder)
  written (threshold-overridden `CW_AGE_S=1 CW_CPU_PCT=5`, engine-gated, spins a
  throwaway CPU-burner per available engine and asserts flag + attribution +
  `exec_hint` + NSpid + the behavioural **safety** assert (burner survives) +
  allowlist + DBus + systemd; trap-cleans all `cw-test-*`). `bash -n` + shellcheck
  clean. **Execution is HOST-only** (it starts real containers + a session bus) —
  run it on the HOST as the gating acceptance step (pending, part of Task 4.2).
- [x] ✅ **Task 5.4**: L3 human — `testing-checklist.md` written: numbered
  nested-GNOME visual checks (panel attention state + notification + dedupe, driven
  by `scan --inject`) plus the real guided-resolution walkthrough and a run-record
  table. **Execution is HOST-only** (nested GNOME Shell) — pending on the HOST.

## Dependencies

- None blocking. Reuses the DBus-CLI↔extension pattern from
  `speech-to-text@fedora-desktop`.

## Success Criteria

- [ ] A synthetic long+hot container process is flagged **within one timer interval
  after the age threshold is met** (verified via threshold injection — `CW_AGE_S`).
  Detection latency = max(age_threshold, sustained-window) + ≤ 1 timer interval; the
  2-min cadence does **not** shorten the 15-min age gate.
- [x] Finding correctly names the container for a Podman **and** a Docker **and** an
  LXC container. (L2 `acceptance.bash` GREEN on HOST for all three; lxc verified
  against the real `bl-admin` container.)
- [ ] Report reachable from both the GNOME panel and the CLI.
- [x] No kill/throttle path exists in the shipped tool (enforced by the L0 no-kill
  guard, confirmed by the L2 behavioural survive-assert — burner survives the scan
  on podman, docker, and the real `bl-admin` LXC container).
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

### 2026-06-24 (implementation — Phase 0, 1 + Phase 5 L1)

- **Phase 0 decisions all resolved** in code: DBus via `gdbus` subprocess
  (stdlib-only); per-core CPU% default 50, age default 900 s, both env- then
  config-overridable; reporting-only (zero kill paths); `helpers/containerwatch`
  package + `~/.local/bin/container-watch` wrapper.
- **Phase 1 complete**: `helpers/containerwatch/core.py` (pure attribution +
  detection + schema + NSpid + exec_hint + allowlist + PID-reuse-guarded
  `cpu_delta_pct`) and `helpers/containerwatch/cli.py` (real `/proc` scan, engine
  name resolution, atomic `report.json`, `gdbus` emit, `scan/status/list/explain/watch`
  subcommands, the `--inject`/`CW_PROC_ROOT`/threshold-env test seams). Bin wrapper
  `files/home/.local/bin/container-watch` mirrors the `github-ssh-443` /
  `ccy-helpers` deploy pattern.
- **Phase 5 L1 complete**: 60 stdlib-`unittest` tests
  (`tests/helpers/containerwatch/test_core.py` + `test_cli.py`) cover the full
  attribution matrix, detection gates, schema, and the `comm`/NSpid/allowlist/
  PID-reuse regressions. `qa-all.bash` (334 files) and `qa-helper-tests.bash`
  (148 tests) both green.
- **Optional sustained gate deferred** (YAGNI) — single-invocation CPU sample
  ships first; R5's persisted `starttime`-keyed gate added later only if noisy.
- Remaining: Phase 2 (systemd user units), Phase 3 (GNOME extension), Phase 4
  (Ansible play), Phase 5 L0 no-kill gate / L2 acceptance script / L3 checklist.

### 2026-06-24 (implementation — Phases 2, 3, 4 + Phase 5 L0)

- **Phase 2**: `files/home/.config/systemd/user/container-watch.{service,timer}`
  (oneshot `ExecStart=%h/.local/bin/container-watch scan`; timer `OnBootSec`/
  `OnUnitActiveSec=2min`, `WantedBy=timers.target`) — the repo's first `.timer`.
- **Phase 3**: `extensions/container-watch@fedora-desktop/{metadata.json,extension.js}`
  — thin read-only panel front-end; `FindingsChanged` signal is a re-read trigger,
  `report.json` is the source of truth; new-finding notifications deduped on
  `host_pid:container_id`; per-finding clipboard-copy of `exec_hint`; async file
  reads only. ESLint clean; `check_extension_compat` green (GNOME 50).
- **Phase 4**: `playbooks/imports/optional/common/play-container-watch.yml` —
  standalone/opt-in (NOT imported into `playbook-main.yml`), deploys the helper to
  `/usr/local/lib/ccy-helpers`, the wrapper, both user units, and the extension
  (recursive dir copy), enables the user **timer** + extension behind session
  probes. `ansible-playbook --syntax-check` clean (2.19 hazards avoided).
- **Phase 5 L0**: `scripts/qa-nokill-containerwatch.bash` + `qa-all.bash` hard-gate
  wiring (see Task 5.1). `qa-all.bash` green over 339 files; `qa-helper-tests.bash`
  148 tests green.
- Built via parallel opus agents (frozen-contract hand-off), then integrated and
  re-verified here. Remaining: Phase 4.2 (HOST deploy/verify), Phase 5 L2
  acceptance script + L3 checklist.

### 2026-06-24 (implementation — Phase 5 L2 + L3 authored; all in-container work done)

- **Phase 5 L2**: `acceptance.bash` (in this plan folder) — host-only,
  engine-gated, fail-fast acceptance test (burner per engine, full assertion set
  incl. the behavioural survive-assert, allowlist, DBus, `systemd-analyze --user verify`, trap cleanup). `bash -n` + shellcheck clean; in `qa-all.bash` bash gate.
- **Phase 5 L3**: `testing-checklist.md` — numbered nested-GNOME visual checklist
  - guided-resolution walkthrough + run-record table (reserved placeholders, no
    time estimates).
- **Status**: every task that can be done inside the CCY container is complete and
  verified — `qa-all.bash` (341 files), `qa-helper-tests.bash` (148 tests), ESLint,
  `check_extension_compat`, and the no-kill gate are all green. The plan stays
  **In Progress** because the remaining work is HOST-only and cannot run in this
  container: **Task 4.2** (`ansible-playbook play-container-watch.yml`, then confirm
  the timer fires + panel/CLI show a finding) and the **HOST execution** of L2
  (`acceptance.bash`) and L3 (`testing-checklist.md`). Once
  those pass on the HOST, mark Task 4.2 ✅, set the plan Complete, and move it to
  `CLAUDE/Plan/Completed/`.

### 2026-06-24 (HOST deploy confirmed + plan-local-scripts convention)

- **Deployed on HOST**: `deploy.bash` ran the play cleanly; `container-watch.timer`
  is registered and has fired; the oneshot service runs; `container-watch status`/`list` return `OK — 0 findings` (correct healthy state — no runaway
  containers present). Task 4.2 moved to 🔄 (deploy/timer/CLI confirmed; panel +
  acceptance run still pending on HOST).
- **New convention — plan-local scripts (set in stone)**: the L2 script was
  relocated `scripts/acceptance-container-watch.bash` →
  `acceptance.bash` (this folder), and `deploy.bash` + `triage.bash` were added
  here. Transient, plan-specific scripts/artifacts now live in the plan folder, not
  the repo root; only persistently-useful tooling (e.g. the permanent
  `scripts/qa-nokill-containerwatch.bash` gate, the `tests/helpers/containerwatch/`
  regression suite) stays in its normal home. The rule is encoded in
  `CLAUDE/PlanWorkflow.md` and `CLAUDE/Plan/CLAUDE.md`. All three plan scripts
  resolve the repo root via `git rev-parse --show-toplevel` and pass `bash -n` +
  shellcheck (error-level).

### 2026-06-25 (HOST L2 acceptance GREEN for all three engines)

- **L2 `acceptance.bash` passes on the HOST for podman, docker, AND lxc.** The lxc
  block runs against the operator's real `bl-admin` LXC container (auto-picked from
  `lxc-ls`, overridable via `CW_LXC_TEST_CONTAINER`); it is never created or
  destroyed, only started-if-stopped and restored to its prior run-state. All
  detection/attribution assertions plus the behavioural safety survive-assert pass.
- **`--lxc` flag added** to `acceptance.bash` — runs ONLY the lxc block (skips the
  confirmed-good podman/docker OCI blocks + DBus/systemd cross-cutting checks) for
  fast iteration on the LXC path.
- **Two LXC test-harness fixes** (not watchdog bugs — the OCI engines and the L0
  no-kill gate prove the tool never kills): (1) the spinner is launched by
  backgrounding the lxc-attach on the HOST side (an in-container `setsid` detach
  failed because a minimal container lacks util-linux `setsid`); (2) the earlier
  safety false-fail was slow scans — the CPU sampler sleeps ~1s per aged candidate
  PID, so on a busy host a default-interval scan outlived the burner. `scan_json`
  now uses `--interval 0.25` (a pegged spinner still reads ~100%) and the burner TTL
  is 30→60s. The deployed watchdog keeps the 1.0s default; this is a test speed-up.
- **Remaining**: only the L3 nested-GNOME visual pass (`testing-checklist.md`).
  Once that is confirmed on the HOST, mark Task 4.2 ✅, set the plan Complete, and
  move it to `CLAUDE/Plan/Completed/`.
