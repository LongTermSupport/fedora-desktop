# Plan 00055 — Audit Round 1

**Created**: 2026-06-24

**Scope**: Adversarial technical + convention audit of `PLAN.md`, `context.md`, and
`testing.md` for Plan 00055 (Container Process Watchdog), verified against the actual
fedora-desktop codebase (extension layout, DBus pattern, systemd-user precedents,
playbook wiring, helper/QA infrastructure, `/proc`+cgroup technique).

## Overall Assessment

This is a strong, unusually thorough plan set. The reporting-only scope is crisp and
consistently enforced (Goals, Non-Goals, the L0 no-kill static guard, and the L2
behavioural survive-assert all agree). The detection technique is technically sound on
the points that matter: cgroup-v2 attribution from world-readable `/proc/<pid>/cgroup`,
the `comm`-lies/use-`cmdline`-argv0 gotcha, NSpid host→container translation, btime-based
age, and two-sample CPU delta are all correct and were the actual forensic basis of the
incident. The test strategy (injectable `proc_root`, env thresholds, `--inject`, engine
probe) is genuinely testable and avoids the 15-minute-wait trap. The DBus-from-CLI →
extension pattern the plan leans on really does exist in this repo (`wsi`/`wsi-stream`
`gdbus emit` → `Gio.DBus.session.signal_subscribe`).

The findings below are mostly **convention-alignment drift** between the design docs and
the repo's real layout/naming, plus a few **technical fragilities** the implementation
must not gloss over. None are fatal; most are concrete one-line fixes to the design docs.
One genuine internal tension (user-timer vs. cross-uid rootless resolution) needs an
explicit decision. The plan is close to solid and warrants one focused revision pass to
correct the drift before Phase 1 starts.

## Findings

| id  | severity | category              | title                                                               | actionable |
| --- | -------- | --------------------- | ------------------------------------------------------------------- | ---------- |
| F1  | high     | repo-convention       | Extension source path contradicts repo's `extensions/` layout       | yes        |
| F2  | high     | technical-correctness | User-timer recommendation conflicts with cross-uid rootless resolve | yes        |
| F3  | medium   | repo-convention       | DBus interface name casing drifts from repo convention              | yes        |
| F4  | medium   | repo-convention       | "Import from playbook-main.yml" mis-states how extensions are wired | yes        |
| F5  | medium   | feasibility           | No-kill "semgrep" guard assumes Python semgrep infra that's absent  | yes        |
| F6  | medium   | technical-correctness | No cgroup-v1 fallback / non-marker cgroup handling specified        | yes        |
| F7  | medium   | testability           | NSpid parsing under nested namespaces / single-field underspecified | yes        |
| F8  | low      | technical-correctness | PID reuse between the two CPU samples not guarded                   | yes        |
| F9  | low      | completeness          | No `.timer` precedent in repo — units are net-new, plan implies one | yes        |
| F10 | low      | completeness          | `container_pid: 0` placeholder collides with "no NSpid" sentinel    | yes        |
| F11 | low      | consistency           | Timer default (2 min) vs age default (900 s) interaction unstated   | yes        |
| F12 | low      | repo-convention       | Helper-split decision (Task 0.4) gates TDD path but left open       | yes        |

### F1 — Extension source path contradicts repo's `extensions/` layout (high, repo-convention)

**Detail**: `context.md` §7 (lines 344–345) lists the extension source as
`files/home/.local/share/gnome-shell/extensions/container-watch@fedora-desktop/`. The
repo does **not** keep extension source there — that path on disk is empty
(`files/home/.local/share/gnome-shell/extensions/` has no extension dirs). Real
extensions live under `extensions/<uuid>/` and are deployed *from* there:
`play-speech-to-text.yml:16` sets `extension_src: "{{ root_dir }}/extensions/{{ extension_name }}"`, and `play-gnome-shell-extensions.yml:101` copies
`extensions/workspace-names-overview@fedora-desktop/`. `extensions/CLAUDE.md` and
`CLAUDE/QA.md` (the `extensions/<uuid>/metadata.json` compat gate) both assume the
`extensions/` location. PLAN Task 3.2/3.3 already correctly reference
`extensions/node_modules/.bin/eslint`, so the docs are internally inconsistent.

**Recommendation**: Change the context.md §7 extension path to
`extensions/container-watch@fedora-desktop/` (with `metadata.json` + `extension.js`), and
have the playbook copy from `{{ root_dir }}/extensions/...` exactly like
`play-speech-to-text.yml`/`play-gnome-shell-extensions.yml`.

### F2 — User-timer recommendation conflicts with the cross-uid rootless-resolve mechanism (high, technical-correctness)

**Detail**: §6 recommends a **`systemd --user`** timer (running as uid 1000) and, in the
same section + Task 1.3, prescribes resolving rootless-Podman names by extracting the uid
from the cgroup and querying "as that user", e.g. `runuser -u "#<uid>"` or
`machinectl shell`. Those cross-uid mechanisms require **root** — a non-root user timer
cannot `runuser` into another uid (and `machinectl shell` to another user also needs
privilege). On a genuinely single-user workstation the timer *is* uid 1000, so it resolves
its **own** rootless containers natively with a plain `podman inspect` and no uid hop at
all — the `runuser -u #<uid>` machinery is only meaningful for *other* users' containers,
which the recommended user timer fundamentally cannot reach. The doc currently presents
both as compatible. (Confirmed: `runuser` is the only one of `runuser`/`machinectl`/`podman`/`docker` present even in tooling terms here, and it is privileged for cross-uid.)

**Recommendation**: Pick one model explicitly in Phase 0. For the user-timer (recommended)
path, state that rootless resolution is **native `podman inspect` as the session user**
and drop the `runuser -u #<uid>` step from the user-timer flow (keep it only in the
documented system-level/multi-uid fallback, where root makes it valid). Make the §6 table's
"Resolve id→name as `<uid>`" column conditional on which timer model was chosen.

### F3 — DBus interface name casing drifts from the repo convention (medium, repo-convention)

**Detail**: §4c (line 249) proposes `org.fedoraDesktop.ContainerWatch`
(camelCase `fedoraDesktop`). The existing, working precedent uses **all-lowercase**:
`DBUS_INTERFACE="org.fedoradesktop.SpeechToText"` and
`DBUS_PATH="/org/fedoradesktop/SpeechToText"` (`files/home/.local/bin/wsi:74-75`,
`extensions/speech-to-text@fedora-desktop/extension.js:24-25`). Mismatched casing is an
easy copy-paste bug that silently breaks signal subscription.

**Recommendation**: Use `org.fedoradesktop.ContainerWatch` /
`/org/fedoradesktop/ContainerWatch` to match the established namespace casing.

### F4 — "Import from playbook-main.yml" mis-states how extensions are actually wired (medium, repo-convention)

**Detail**: PLAN Task 4.1 and context.md §7 say `play-container-watch.yml` is "imported
from `playbook-main.yml`". The reference extension (`play-speech-to-text.yml`) is **not**
imported by `playbook-main.yml` at all (grep returns nothing); extensions get wired either
inside `play-gnome-shell-extensions.yml` (which deploys `workspace-names-overview` inline)
or as an opt-in optional play. The plan should not assert a wiring path that doesn't match
how the existing extensions land.

**Recommendation**: Decide and state the real integration point: either (a) add the deploy
tasks into `play-gnome-shell-extensions.yml` (the pattern actually used for an always-on
extension), or (b) ship `playbooks/imports/optional/common/play-container-watch.yml` as an
opt-in play and say so — but don't claim a `playbook-main.yml` import that the comparable
extension doesn't have. Verify the chosen wiring against `playbook-main.yml` at
implementation time.

### F5 — No-kill "semgrep" guard assumes Python semgrep infra that doesn't exist (medium, feasibility)

**Detail**: testing.md §2 / PLAN Task 5.1 specify a no-kill guard via "a semgrep/grep
rule". The repo's only semgrep ruleset is **bash-scoped**: `.semgrep/bash-conventions.yml`
with a bash self-test fixture (`bash-conventions.bash`), run by `qa-patterns.bash` against
bash files only. There is no Python semgrep ruleset wired into `qa-all.bash`, and the
no-kill assertions (`os.kill`, `.send_signal(`, `signal.SIG*`, `Gio.Subprocess … kill`)
are Python/JS-targeted. Implementing this as semgrep means adding new Python-semgrep
infrastructure + a self-test fixture and wiring it into the QA aggregator — non-trivial and
currently unscoped.

**Recommendation**: Either (a) scope the guard as a **grep-based** check in a small bash
gate (matches the repo's lightweight style and the "grep rule" wording) wired into
`qa-all.bash`, or (b) explicitly add a task to stand up a Python semgrep ruleset +
fixture alongside the existing bash one. Don't leave "semgrep" implying infra that isn't
there.

### F6 — No cgroup-v1 fallback or non-marker cgroup handling specified (medium, technical-correctness)

**Detail**: The whole attribution rests on cgroup-**v2** unified-hierarchy paths
(`0::/…libpod-…`). Fedora defaults to v2 (confirmed: host technique is v2), but the design
never states what happens if a `/proc/<pid>/cgroup` line is v1-style (multiple `N:subsys:`
lines) or simply contains **no known engine marker** that is nonetheless a container (e.g.
a future/again-renamed runtime, Docker under cgroupfs vs systemd driver — the latter is in
the matrix, the former edge cases are not). Fail-fast convention means an unparseable
cgroup must do something explicit, not silently mis-bucket as "host".

**Recommendation**: Add an explicit decision: assert cgroup-v2 (the documented target) and
fail-fast/log clearly on a v1 line rather than silently treating it as host; enumerate the
"unknown marker but clearly containerised" case as **not-a-finding-but-logged** so it can't
masquerade as host. Add a fixture row for "unrecognised cgroup" to the L1 matrix.

### F7 — NSpid parsing under nested namespaces / single-field case underspecified (medium, testability)

**Detail**: context.md §2c says "the **last** field is the PID inside the container's PID
namespace", and the L1 NSpid test (testing.md §3) uses a two-field
`NSpid:\t<host>\t<container>`. Two real edge cases aren't covered: (1) a **host** process
has a single-field `NSpid:` (just its own PID) — the "last field" rule would then wrongly
report `container_pid == host_pid`; (2) **nested** namespaces (e.g. CCY-in-something) yield
3+ fields, where "last" is the innermost, which may not be the level the human's
`podman exec` lands in. The schema's `container_pid: 0` default (§4b line 222) also implies
a "no translation" sentinel that the parsing rule doesn't define.

**Recommendation**: Specify: only emit `container_pid` when the process is attributed to a
container *and* `NSpid` has ≥2 fields; take the **second** field (first nested level), not
unconditionally the last, OR document why innermost is correct for the `exec` target. Add
L1 fixtures for single-field (host) and 3-field (nested) NSpid.

### F8 — PID reuse between the two CPU samples not guarded (low, technical-correctness)

**Detail**: The CPU-delta method samples `/proc/<pid>/stat` twice over ~1–3 s (§4a). If
the PID exits and the number is recycled between samples, the delta is computed across two
different processes, producing a bogus cpu_pct. Cheap to guard.

**Recommendation**: Note in Task 1.2 that the sampler must re-validate identity between
samples (compare `starttime` field 22, or skip if the process vanished) before computing a
delta. Add an L1 case where the second sample is missing → no finding, no crash.

### F9 — No `.timer` precedent in the repo; the units are net-new (low, completeness)

**Detail**: The repo deploys `systemd --user` **`.service`** units (qobuz
`rescrobbled.service` via `copy: content:` with `WantedBy=default.target`; rclone services)
and uses `ansible.builtin.systemd: scope: user`, but there is **no `.timer`** anywhere
(grep for `OnUnitActiveSec`/`[Timer]` returns nothing). The plan treats the timer as
routine; it is actually the first timer in the repo, so `daemon_reload`, enabling the
`.timer` (not the `.service`), and `WantedBy=timers.target` need to be gotten right with no
local template to copy.

**Recommendation**: Note in Task 2.1 that this is the repo's first user timer; model the
`.service` on qobuz's `copy: content:` pattern, enable the **`.timer`** (`enabled: yes, state: started, scope: user`) with `daemon_reload: true`, and verify with
`systemd-analyze --user verify` (already in testing.md §4 — good).

### F10 — `container_pid: 0` placeholder collides with a "no NSpid" sentinel (low, completeness)

**Detail**: The §4b example sets `"container_pid": 0` as an illustrative placeholder, but
`0` is also the natural "could not translate" value. Combined with F7, consumers can't
distinguish "PID 0" (never valid for a user process) from "untranslated".

**Recommendation**: Use `null`/omit for untranslated, never `0`; state this in the schema
description so both UIs and the L1 schema test assert it.

### F11 — Timer interval (2 min) vs age threshold (900 s) interaction unstated (low, consistency)

**Detail**: Timer default is 2 min (§4 diagram, §7), age threshold 900 s (15 min). With a
single-invocation CPU sample, a process is only ever flagged once it's already ≥15 min old,
so the *first* alert lands ~15 min after the runaway starts regardless of the 2-min cadence
— and the optional "sustained over N ticks" gate (§4a) further delays it. This is arguably
correct (matches "long-running") but the docs never state the expected detection latency,
and Success-Criterion "flagged within one timer interval" (PLAN line 120) is only true once
the age threshold is *already* met (or injected), which could read as contradictory.

**Recommendation**: Add one sentence clarifying detection latency = max(age_threshold,
sustained-window) + ≤1 timer interval, and reword the success criterion to "within one
timer interval **after the age threshold is met** (verified via threshold injection)".

### F12 — Helper-split decision (Task 0.4) gates the TDD path but is left open (low, repo-convention)

**Detail**: Whether the core lives under `helpers/` vs a single `files/home/.local/bin`
script is left to Task 0.4, but that decision determines whether the stdlib-only +
test-mirroring + TDD-first rules (`helpers/CLAUDE.md`: tests in
`tests/helpers/containerwatch/test_*.py`, written **before** source or the TDD handler
blocks it) and `qa-helper-tests.bash` collection apply. testing.md §3 already *assumes* the
helper layout ("if the core lives under `helpers/`… tests go in
`tests/helpers/containerwatch/`"). Given the rich unit-test matrix the plan wants, the
helper split is effectively required, not optional.

**Recommendation**: Pre-resolve Task 0.4 toward the **helper package + thin bin wrapper**
split (it's the only layout that gets the unit matrix into `qa-helper-tests.bash`), and
flag the TDD-first constraint (write `tests/helpers/containerwatch/test_*.py` before the
source files) so implementation doesn't trip the TDD handler.

## Convergence

**Not yet converged — one focused revision pass recommended.** The plan's bones are solid
(scope, technique, test seams), and there are no blocking design errors. But two **high**
findings are real convention/correctness drift that would mislead implementation if left:
F1 (extension lives in `extensions/`, not `files/home/...`) and F2 (the user-timer vs
cross-uid rootless-resolution tension needs an explicit decision). F3–F7 are concrete
medium fixes (DBus casing, wiring claim, semgrep-vs-grep guard, cgroup-v1 fallback, NSpid
edge cases). All findings are actionable as targeted doc edits — none require rethinking the
architecture. After a revision that lands F1/F2 and the medium items, this plan should be
solid enough to start Phase 0/1.
