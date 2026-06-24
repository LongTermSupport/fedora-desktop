# Plan 00055 — Acceptance Testing Process

**Created**: 2026-06-24
**Type**: Test strategy (read alongside [`context.md`](context.md))

> **Design constraint that drives everything below**: the tool's *job* is to detect
> a process that is **(a) in a container**, **(b) old (≥ 15 min)**, and **(c)
> CPU-pinned**, then attribute and report it. Naively testing that means waiting 15
> minutes and manufacturing a real runaway — unacceptable. The tool must therefore
> be **built with test seams** so the same logic can be exercised in milliseconds
> from fixtures, in seconds against a real throwaway container, and only *visually*
> confirmed by a human at the very end. Those seams are listed in §1 and are a
> **requirement on the implementation**, not optional.

## 0. Testing philosophy & the human-effort budget

A 4-layer pyramid. Everything that *can* be automated *is*; the human is reserved
for the single irreducible thing a machine cannot assert — *"did the GNOME panel
visibly react and was the guidance usable?"*

| Layer                    | What it covers                                      | Automated? | Where it runs      | Human effort          |
| ------------------------ | --------------------------------------------------- | ---------- | ------------------ | --------------------- |
| **L0 Static / QA**       | syntax, lint, version gate, **no-kill** guard       | 100%       | CI + local         | none                  |
| **L1 Unit (fixtures)**   | attribution + detection + report logic, all engines | 100%       | CI + local         | none                  |
| **L2 Host integration**  | real container + synthetic load, DBus, systemd      | 100%       | HOST (one command) | none (read PASS/FAIL) |
| **L3 Human-in-the-loop** | GNOME panel/notification visual + one e2e walk      | scripted   | HOST nested shell  | ~3–5 min, scripted    |

Target: a developer runs **two commands** (`./scripts/qa-all.bash` and the L2
acceptance script) for full confidence in the backend, then a **~3-minute scripted
visual check** for the front-end. No 15-minute waits, no hand-crafted runaways.

## 1. Required test seams (implementation requirements)

These MUST be built into the tool or none of the automation below is possible:

1. **Injectable `proc_root`** — the core reads from a configurable root (default
   `/proc`). Unit tests point it at a fixture tree (`tests/.../fixtures/proc/`).
   This makes *all* attribution + detection logic testable offline, for every
   engine, with zero containers and zero privileges.
2. **Injectable thresholds** — age and CPU thresholds come from config/env
   (e.g. `CW_AGE_S`, `CW_CPU_PCT`), not hard-coded. Tests set `CW_AGE_S=1 CW_CPU_PCT=5` so a 5-second burner trips them. Production default stays
   `CW_AGE_S=900`.
3. **Injectable container-name resolver** — the engine `inspect`/`lookup` call is
   behind one function that can be stubbed (so unit tests assert name-resolution
   wiring without a live daemon; integration tests use the real one).
4. **`scan --inject <finding.json>`** (a.k.a. `--emit-fake-finding`) — a debug path
   that writes a *synthetic* finding to `report.json` and emits the DBus signal
   **without scanning**. This deterministically drives the CLI and GNOME front-ends
   for L3 — the human never has to manufacture a real runaway to see the panel
   react.
5. **`scan --once --json` to stdout** — a machine-readable single-pass mode the
   acceptance script can assert against directly (in addition to writing the report
   file).
6. **Engine-presence probe** — the tool (and the test harness) can ask "is
   podman/docker/lxc available here?" so integration tests **gate** per engine
   instead of failing on absent ones (fail-fast still applies to *present* engines).

## 2. L0 — Static / QA gates (CI, 100% automated)

Run by `./scripts/qa-all.bash` plus the extension/compat gates (`CLAUDE/QA.md`):

- `bash -n` + shellcheck (bin wrappers), `py_compile` + ruff (core/CLI), semgrep
  patterns, Ansible fail-fast grep + `--syntax-check` (the playbook).
- `node --check` + `eslint` on the extension (`extensions/node_modules/.bin/eslint`).
- `python3 -m helpers.gnome.check_extension_compat` — the new extension's
  `metadata.json` declares the GNOME major for this branch's Fedora
  (`vars/fedora-version.yml`; F44 → GNOME 50).
- **No-kill safety guard (static)** — a dedicated check (CI + a semgrep/grep rule)
  asserting the core contains **no** process-termination path aimed at findings:
  no `os.kill`, `signal.SIG*` sends, `.send_signal(`, `subprocess … kill/pkill`,
  `Gio.Subprocess … kill`. This is a first-class success criterion (reporting-only)
  and must fail the build if violated. Showing the human a kill *command string* in
  `exec_hint` is allowed; the tool executing one is not.

## 3. L1 — Unit tests over `/proc` fixtures (CI, 100% automated)

stdlib `unittest`, collected by `./scripts/qa-helper-tests.bash` (if the core lives
under `helpers/`, tests go in `tests/helpers/containerwatch/test_*.py`). Each case
is a fixture `proc_root` directory with `<pid>/{cgroup,stat,status,cmdline}` files.

**Attribution matrix — one fixture per row (covers §6 of context.md):**

| Fixture                     | `cgroup` content (abridged)                                    | Expect engine | Expect rootless | Expect owner_uid |
| --------------------------- | -------------------------------------------------------------- | ------------- | --------------- | ---------------- |
| podman-rootless             | `…/user-1000.slice/…/libpod-<id>.scope/container`              | podman        | true            | 1000             |
| podman-rootful              | `/machine.slice/libpod-<id>.scope/container`                   | podman        | false           | 0                |
| docker-systemd-driver       | `/system.slice/docker-<id>.scope`                              | docker        | false           | 0                |
| docker-cgroupfs-driver      | `/docker/<id>`                                                 | docker        | false           | 0                |
| lxc-payload                 | `…/lxc.payload.<name>/…`                                       | lxc           | false           | 0                |
| host-process (not in a ctr) | `0::/user.slice/user-1000.slice/user@1000.service/app.slice/…` | none          | n/a             | n/a              |

**Detection logic cases:**

- old + hot + in-container → **flagged**.
- old + idle → not flagged. hot + young → not flagged. host + old + hot → not
  flagged (must be in a container).
- CPU% computed correctly from two `stat` samples (fixture provides before/after
  utime+stime; assert the percentage).
- age computed from `starttime` vs a fixture `btime` (no wall-clock dependency).
- **`comm` gotcha regression test**: fixture where `comm`=`claude.exe` but
  `cmdline` argv[0]=`ugrep` → must still be enumerated/flagged (proves matching is
  cmdline-based, not comm-based). This pins the exact bug that broke triage.
- **NSpid translation**: `status` with `NSpid:\t<host>\t<container>` → finding's
  `container_pid` == the last field.
- **report schema**: emitted JSON validates against schema v1; `exec_hint` is
  engine-correct (`podman exec`/`docker exec`/`lxc-attach`) and contains the
  resolved name + container PID.
- **allowlist**: a finding matching an allowlist entry (by container name and/or
  cmd pattern) is suppressed.

## 4. L2 — Host integration acceptance script (HOST, 100% automated)

A single fail-fast script, e.g. `scripts/acceptance-container-watch.bash`
(host-only — **not** in the CCY container; it starts real containers). It prints a
PASS/FAIL line per assertion and exits non-zero on any failure. It overrides
thresholds (`CW_AGE_S=1 CW_CPU_PCT=5`) so nothing waits.

**Synthetic load generator (no real runaway needed):** start a throwaway container
running a bounded CPU spinner, e.g.

```bash
# podman example; duration-bounded so it self-cleans, trap also removes it
podman run -d --rm --name cw-test-burner <small-image> \
    sh -c 'timeout 15 sh -c "while :; do :; done & while :; do :; done & wait"'
```

(Use a couple of background spinners to ensure it reads as CPU-pinned; `timeout`
caps the lifetime; an `EXIT` trap force-removes the container even on failure.)

**Per available engine** (gated by the §1.6 presence probe — podman always;
docker if the daemon/group is present; lxc if `lxc-*` + sudo are present):

1. Start `cw-test-burner` with the spinner.
2. Capture ground truth: container name, host PID (`podman top` / `docker top` /
   `lxc-info`), and in-container PID (`exec … pgrep`).
3. Run `container-watch scan --once --json`.
4. **Assert**: exactly one finding for `cw-test-burner`; `engine` correct;
   `container_name` correct; `container_pid` == ground-truth in-container PID;
   `cmd` contains the spinner; `age_s ≥ 1`; `cpu_pct` high; `exec_hint` is
   engine-correct and names the container.
5. **Safety assert (reporting-only, behavioural)**: the spinner process is **still
   alive** after the scan (the tool must not have killed it).
6. Stop/clean the container.

**Cross-cutting host assertions:**

- **Allowlist**: add `cw-test-burner` to a test allowlist, re-scan → **zero**
  findings (suppression works end-to-end).
- **DBus emission**: subscribe (`gdbus monitor` / a tiny listener) on a private
  session bus, trigger `scan --inject` (or a real burner), assert the
  `FindingsChanged` signal fires with the expected count.
- **systemd units**: `systemd-analyze --user verify container-watch.service container-watch.timer` passes; after `ansible` deploy, `systemctl --user list-timers` shows the timer; `systemctl --user start container-watch.service` produces a fresh `report.json`.
- **Idempotent / clean**: running the script twice leaves no stray `cw-test-*`
  containers (trap-verified).

> CI note: L0+L1 run in `.github/workflows/qa.yml` today. L2 needs a real container
> runtime (and ideally a session bus) — run it on the HOST as the gating acceptance
> step; wire into CI later only if a runner provides rootless podman. Document this
> split, don't fake L2 in CI.

## 5. L3 — Human-in-the-loop (HOST, scripted, ~3–5 min)

The only steps a machine can't fully assert. Each is scripted so the human just
*watches and confirms*, using the `--inject` seam so no real runaway is needed.

**5a. GNOME panel + notification (nested shell, no logout):**

1. `dbus-run-session -- gnome-shell --nested --wayland`
   (per `CLAUDE/GnomeShell.md`; enable the extension via the Extensions app, Alt+F1
   for the nested overview).
2. From a terminal: `container-watch scan --inject <fixture-finding.json>`.
3. **Human confirms**: panel indicator changes to the attention state; a desktop
   notification appears; clicking the indicator lists the finding with a copyable,
   engine-correct `exec_hint`.
4. `container-watch scan --inject empty` → **human confirms** the indicator returns
   to neutral (no stale finding).
5. Notification **dedupe**: inject the same finding twice → confirm it does **not**
   re-notify on the second identical tick.

**5b. One real end-to-end "guided resolution" walkthrough (once per release):**

1. Start a real `cw-test-burner` (the §4 spinner, but longer / no `timeout`).
2. Wait one real timer interval (default 2 min — *not* 15 min; the age threshold is
   what's 15 min, and for this manual walk you may temporarily lower `CW_AGE_S`).
3. **Human confirms** the panel/CLI flags it, copies the `exec_hint`, runs it,
   sees the offending process inside the container, and resolves it there. This
   validates the *actual goal* — guiding a human to fix it in the container — which
   no unit test can assert.
4. Tear down the burner.

A short **`testing-checklist.md`** (or a section appended here at implementation
time) captures 5a/5b as numbered tick-boxes so the human pass is repeatable and
auditable.

## 6. Definition of "acceptance passed"

- [ ] L0 green: `./scripts/qa-all.bash`, ESLint, `check_extension_compat`, **no-kill
  guard**.
- [ ] L1 green: unit suite via `./scripts/qa-helper-tests.bash` — full attribution
  matrix (all engines) + detection + schema + `comm`/NSpid/allowlist cases.
- [ ] L2 green: `scripts/acceptance-container-watch.bash` PASS for every **present**
  engine, incl. the behavioural **safety** assert (process survives) and DBus +
  systemd checks; no stray test containers left.
- [ ] L3 confirmed: GNOME panel/notification visual pass (5a) + one real guided
  resolution walkthrough (5b), recorded in the checklist.

## 7. Anti-goals for testing

- **No 15-minute waits** — age is threshold-injected; the real value is only
  exercised in the one optional 5b walkthrough.
- **No hand-crafted runaways** — the spinner generator and `--inject` seam replace
  them.
- **Don't fake L2 in CI** — if the runtime isn't present, *skip and say so*
  (engine-gated), never stub a green for an engine that wasn't actually exercised
  (fail-fast / no silent skip).
- **Never test the kill path** — there isn't one; the guard proves its absence.
