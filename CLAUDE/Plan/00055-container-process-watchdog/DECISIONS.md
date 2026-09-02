# Plan 00055: Decisions and Evidence

Durable design decisions, their rationale, and the host verification evidence
for the container process watchdog. Extracted from the original plan document
(kept verbatim in [`PLAN_archive.md`](PLAN_archive.md)) so that
[`PLAN.md`](PLAN.md) stays a lean task list. Design background is in
[`context.md`](context.md); the acceptance process is in [`testing.md`](testing.md).

## D1: DBus via shell-out

`cli.emit_signal` runs `gdbus emit` as a subprocess, so the helper stays
stdlib-only (no `gi` dependency). This matches the speech-to-text precedent.
Emission is best-effort: a missing session bus is non-fatal. The DBus
namespace is all-lowercase `org.fedoradesktop.ContainerWatch` to match that
precedent (audit finding F3).

## D2: Thresholds

CPU is measured as **per-single-core %** (`compute_cpu_pct`), default
`DEFAULT_CPU_PCT = 50`; age default `DEFAULT_AGE_S = 900`. Both are
env-overridable (`CW_CPU_PCT` / `CW_AGE_S`) and then config-overridable.

Detection latency is max(age_threshold, sustained-window) plus at most one
timer interval. The 2-minute timer cadence does **not** shorten the 15-minute
age gate (audit finding F11).

## D3: Reporting-only and allowlist shape

There is **no** kill path anywhere in the package: no `os.kill`,
`send_signal` or `pkill`. CPU caps were rejected as symptom-hiding.

Allowlist config: `config.json` with
`{"allowlist": [{"container_name": "...", "cmd_pattern": "<glob>"}]}`.
`core.matches_allowlist` treats both fields as optional and ANDs them when both
are present.

## D4: Package layout and TDD-first

The code lives as a `helpers/containerwatch` package
(`core.py` pure attribution/detection/schema, `cli.py` real `/proc` scan,
engine resolution, atomic report, `gdbus` emit, subcommands and test seams)
plus a thin `files/home/.local/bin/container-watch` wrapper, mirroring the
`github-ssh-443` / `ccy-helpers` deploy pattern.

This is the only layout that gets the rich L1 unit matrix into
`qa-helper-tests.bash` (collected from `tests/helpers/containerwatch/test_*.py`).
Under `helpers/CLAUDE.md` the package is stdlib-only and tests must exist
**before** the source files, or the TDD handler blocks source creation.

## D5: PID-reuse guards and the deferred sustained gate

Between the two in-invocation CPU samples the process identity is re-validated
(`/proc/<pid>/stat` field 22 `starttime`; vanished process or mismatch yields
`None`; a negative delta yields `None`). Without this a recycled PID would
produce a bogus `cpu_pct`.

The optional "sustained over N consecutive timer ticks" gate is **deferred**
(YAGNI): the single-invocation sample ships first. If it is ever built, its
persisted per-PID record must key on `starttime` and **reset** the
consecutive-hot counter on mismatch. PID reuse is far more likely across the
minutes-apart timer firings than across the one-to-three-second in-invocation
sample (audit finding R5).

## D6: Name resolution and NSpid

Rootless Podman: the `systemd --user` timer **is** the owning uid, so
resolution is native `podman inspect` as the session user with no uid hop.
`runuser -u "#<uid>"` is root-only and reserved for a system-level fallback
(audit finding F2). Docker resolves via the `docker` group; LXC via sudo.

NSpid: emit `container_pid` only when the process is attributed to a container
**and** `/proc/<pid>/status` NSpid has at least two fields; take the second
field (first nested level); omit or `null`, never `0`, when untranslated
(audit findings F7/F10). Details in `context.md` §2c.

Attribution is cgroup-v2 only: a v1 line raises `CgroupV1Error` (fail fast);
kubepods/cri-o markers land in an `engine: "unknown"` bucket that is logged but
never treated as host (audit finding F6).

## D7: First timer unit in the repo

`container-watch.service` (oneshot, `ExecStart=%h/.local/bin/container-watch scan`)
and `container-watch.timer` (`OnBootSec` / `OnUnitActiveSec=2min`,
`WantedBy=timers.target`) under `files/home/.config/systemd/user/`. Only
`.service` user units existed before (for example qobuz `rescrobbled.service`),
so the `.service` follows qobuz's `copy: content:` pattern and the **`.timer`**
(not the `.service`) is enabled with
`ansible.builtin.systemd: scope: user, enabled: yes, state: started, daemon_reload: true`.
Verified with `systemd-analyze --user verify` (testing.md §4).

## D8: Deployment wiring and file-list rule

`playbooks/imports/optional/common/play-container-watch.yml` is standalone and
opt-in, **not** imported by `playbook-main.yml` (the comparable
`play-speech-to-text.yml` has no import either). It deploys the helper to
`/usr/local/lib/ccy-helpers`, the wrapper, both user units and the extension,
then enables the user timer and the extension behind session probes.

Extension source lives under `extensions/<uuid>/`, not
`files/home/.local/share/gnome-shell/extensions/` (audit findings F1/F4).

Per `extensions/CLAUDE.md`, a new extension's whole file set is new, so the
play uses a **recursive directory `copy:`** of
`{{ root_dir }}/extensions/container-watch@fedora-desktop/` (the
`workspace-names-overview` pattern). An explicit per-file loop would have to
enumerate every file, because any file omitted silently never deploys
(audit finding R2).

## D9: No-kill gate design

`scripts/qa-nokill-containerwatch.bash` greps `helpers/containerwatch/*.py`
and the extension JS for **executable termination call sites** (`os.kill(`,
`signal.SIG`, `.send_signal(`, `Gio.Subprocess` `force_exit`, spawned
`kill` / `pkill`), excludes `kill` inside guidance / `exec_hint` string
literals, and self-tests both directions via `--self-test` (audit finding R4).

It is wired into `qa-all.bash` as a minimal **hard gate before the `jq`
merge**, not a seventh positional stage. `qa-all.bash` is a hardcoded six-stage
pipeline (six `mktemp` vars, a six-path `trap`, six rc-blocks, a `jq -s` merge
keyed `.[0]` to `.[5]`); adding a stage means editing all five points, so the
lower-risk gate placement was chosen (audit finding R1). Semgrep was not used
because the repo has no Python/JS semgrep ruleset (audit finding F5).

## D10: Public-repo fixture hygiene

All committed fixtures and `--inject` sample findings use reserved
placeholders per `CLAUDE/ExampleValues.md` (`<project-a>`, `example.com`
paths, synthetic container IDs), never a real container name, workspace path
or argv captured from an incident. Fixtures are built in-memory, so no
`fixtures/` tree is committed. `report.json` is a runtime artifact under
`$XDG_RUNTIME_DIR` and is never committed; the leak surface is the committed
fixtures, which can bypass the email/IP-shaped commit scanner (audit finding R3).

## D11: Plan-local scripts convention

The L2 script was relocated from `scripts/acceptance-container-watch.bash` to
`acceptance.bash` in this plan folder, and `deploy.bash` and `triage.bash` were
added alongside. Transient, plan-specific scripts live in the plan folder;
only persistently useful tooling (the `qa-nokill-containerwatch.bash` gate, the
`tests/helpers/containerwatch/` suite) stays in its normal home. The rule is
encoded in `CLAUDE/PlanWorkflow.md` and `CLAUDE/Plan/CLAUDE.md`.

## E1: HOST verification evidence

- `deploy.bash` ran the play cleanly on the HOST. `container-watch.timer` is
  registered and has fired; the oneshot service runs; `container-watch status`
  and `list` return `OK — 0 findings` (the healthy steady state).
- `acceptance.bash` is GREEN on the HOST for **podman, docker and lxc**:
  detection, attribution (engine, name, in-container NSpid), `cmd`/`age`/`cpu`,
  engine-correct `exec_hint`, the behavioural safety survive-assert, plus the
  allowlist, DBus and systemd cross-cutting checks.
- The lxc block runs against the operator's real `bl-admin` container
  (auto-picked from `lxc-ls`, overridable via `CW_LXC_TEST_CONTAINER`). It is
  never created or destroyed, only started-if-stopped and restored to its prior
  run-state. A `--lxc` flag runs only that block for fast iteration.
- Two LXC test-harness fixes, neither a watchdog bug: the spinner is launched
  by backgrounding `lxc-attach` on the HOST side (a minimal container lacks
  util-linux `setsid`); and the earlier safety false-fail was slow scans, since
  the CPU sampler sleeps about one second per aged candidate PID. `scan_json`
  now uses `--interval 0.25` and the burner TTL was raised from 30 s to 60 s.
  The deployed watchdog keeps the 1.0 s default.
- Still unverified: the L3 nested-GNOME visual pass in
  [`testing-checklist.md`](testing-checklist.md).
