# Plan 00055 — Context: Container Process Watchdog

**Created**: 2026-06-24
**Type**: Context / research / design brief (read this before touching `PLAN.md`)
**Status of doc**: Living — update as design decisions are made
**Companion**: [`testing.md`](testing.md) — the full acceptance-testing process. The
test seams it requires (injectable `proc_root`, env thresholds, `scan --inject`,
engine probe) are **implementation requirements**, not afterthoughts.

> **Public repo note**: every concrete identifier in this document (container
> names, project names, GitHub usernames, SSH key paths, home paths) has been
> replaced with placeholders per `CLAUDE/ExampleValues.md`. Container IDs are
> random and non-sensitive but are shown truncated/illustrative anyway. Do **not**
> paste real `host_vars`/project identifiers into this file — it is world-readable.

---

## 1. Why this plan exists — the triggering incident

A host became sluggish (load average ~13–17 on a 22-core machine). `htop` sorted
by CPU showed many `ugrep` rows pinned near the top. Several `claude-yolo` (CCY)
containers were running concurrently, so the immediate question was *"which
container is responsible?"* — there was no fast, reliable way to answer that.

### What it actually was

A **single** runaway process, heavily multithreaded (hence the *many* `ugrep`
rows in `htop` thread view — one process, ~11 worker threads):

| Field            | Value                                                                                         |
| ---------------- | --------------------------------------------------------------------------------------------- |
| Host PID         | `2124472`                                                                                     |
| Elapsed          | `6916s` (~1.9 hours)                                                                          |
| %CPU (lifetime)  | `1116%` (≈ 11 of 22 cores)                                                                    |
| RSS              | ~35 MB                                                                                        |
| `comm`           | `claude.exe` ← **misleading**, see gotcha below                                               |
| argv[0]          | `ugrep`                                                                                       |
| Full command     | `ugrep -G --ignore-files --hidden -I --exclude-dir=.git --exclude-dir=.svn … -rl <pattern> /` |
| Owning container | `<project-a>_yolo` (rootless Podman, CCY image)                                               |
| Engine           | Podman, **rootless**, uid 1000                                                                |

The damning detail: the search path was **`/`** — the Claude Code Grep tool fired
with an **unscoped filesystem root** instead of the workspace, recursing the whole
container filesystem (including `/workspace` bind-mount + the entire container OS)
for a literal placeholder-looking pattern, and never terminated. It ran ~1.9h
before being handled.

> The *upstream* question — "why did Claude's Grep run against `/`?" — is **out of
> scope** for this plan. This plan builds the **detector/reporter** so that the
> *next* time any container spawns a long-running CPU-pinned process, the human is
> told immediately and pointed at the right container. It is **reporting-only**;
> it never kills anything (an explicit decision — see §8).

---

## 2. How the process was attributed to a container (the reusable core)

This is the single most important technique to preserve — it is the heart of the
tool. Two independent methods agreed.

### 2a. cgroup attribution (authoritative, engine-agnostic, no engine CLI needed)

Every container runtime places its processes in a distinct cgroup. On cgroup v2
(Fedora default) read `/proc/<pid>/cgroup` — it is **world-readable**, so an
unprivileged monitor can attribute *any* process, including root-owned ones in
rootful Docker/LXC:

```
$ cat /proc/2124472/cgroup
0::/user.slice/user-1000.slice/user@1000.service/user.slice/libpod-<container-id>.scope/container
```

The cgroup path encodes **everything the tool needs**:

- **The engine** — from the path marker (`libpod-…` = Podman, `docker-…` = Docker,
  `lxc.payload.…` = LXC). See the table in §6.
- **The container id/name** — the token after the marker.
- **The owning uid (rootless only)** — the `user-<uid>.slice` segment. Critical:
  to resolve a rootless-Podman id → friendly name you must query *as that user*
  (`podman inspect` run by root cannot see another user's rootless containers).

### 2b. Process-ancestry cross-check (confirmation / fallback)

Walk `PPID` up to PID 1. For this incident:

```
ugrep(2124472) → bash(2124468, a shell-snapshot eval) → claude --continue(44590)
              → tini(44588) → conmon(44586) → systemd --user(2520)
```

`conmon`/`tini` are the per-container supervisors. Mapping the `conmon` PID (or the
container's `State.Pid`) back to a name confirmed the cgroup result:

```
$ podman inspect --format '{{.Name}} {{.State.ConmonPid}} {{.State.Pid}}' <container>
<project-a>_yolo 44586 44588
```

Use **2a as primary** (works for stopped-CLI/orphaned trees, root processes, all
engines) and **2b as a sanity check**.

### 2c. Host PID → in-container PID translation (to guide the human)

The goal is to guide the human *inside* the container. The host PID is useless
there — the container sees a different PID. `/proc/<pid>/status` carries the
namespace PID chain:

```
$ grep NSpid /proc/2124472/status
NSpid:  2124472   <pid-as-seen-inside-container>
```

The fields are the PID at each namespace level, outermost (host) first. **Parsing
rules (resolves the single-field / nested-namespace ambiguity):**

- A **host** process has a **single-field** `NSpid:` (just its own PID). The
  "last field" heuristic would then wrongly report `container_pid == host_pid`, so:
  **only emit `container_pid` when the process is attributed to a container AND
  `NSpid` has ≥ 2 fields.**
- For an attributed container process, take the **second field** (the first nested
  level — the PID as seen one namespace in, which is where a plain
  `podman exec`/`docker exec`/`lxc-attach` into the container lands), **not**
  unconditionally the last field. For **deeper nesting** (3+ fields, e.g.
  container-in-container) the second field is still the level the human's `exec`
  reaches from the host engine; the innermost is only correct if exec'ing through
  every layer, which is not the guidance we give.
- When `container_pid` cannot be translated (host process, or NSpid absent / single
  field), **omit the field or set it to `null` — never `0`** (`0` is never a valid
  user PID and would collide with a "could not translate" sentinel; see §4b).

The report must surface the translated PID so the guidance can say e.g. *"in the
container run `kill <container-pid>`"*.

### 2d. Gotchas discovered (must be encoded in the tool)

1. **Do not match on `comm`.** The native Claude binary spawns `ugrep` without
   resetting the process name, so `/proc/<pid>/comm` (and therefore `pgrep -x ugrep`, `ps comm`, `pgrep -c ugrep`) reported `claude.exe` and returned **zero
   matches** during early triage. **Match on `/proc/<pid>/cmdline` argv[0]
   basename**, never `comm`.
2. **`ps %CPU` is a lifetime average**, not instantaneous — fine for "has it been
   hot for a long time" but for "is it hot *right now*" sample
   `/proc/<pid>/stat` (utime+stime) twice over a short interval and diff. (During
   triage `podman stats` showed ~123% instantaneous for the same container while
   `ps` showed 1116% lifetime — both true, different questions.)
3. **`/proc/<pid>/task` was unreadable** from the host user for a container
   process in a user namespace (returned 0 entries) — thread enumeration needs
   care; the per-process CPU figure is sufficient and avoids that.
4. **Elapsed time**: `ps -o etimes=` (seconds) or compute from
   `/proc/<pid>/stat` field 22 (`starttime`, in clock ticks) vs `/proc/stat`
   `btime`. The latter is dependency-free and the right choice for the tool.

---

## 3. Problem statement & goal

**Goal**: a host-level, **reporting-only** watchdog that periodically detects
processes which are **(a) running inside a container**, **(b) older than a
threshold (default 15 min)**, and **(c) sustaining high CPU**, attributes each to
its **container (Podman / Docker / LXC)**, and surfaces an **actionable report**
through both a **GNOME Shell panel/notification** and a **CLI**. The human is then
guided to resolve the issue *inside the container directly*.

**Non-negotiables from the requester**:

- Reporting / alerting only — **never** auto-kill or auto-throttle.
- Attribute every flagged process to its container.
- Support **Podman, Docker, and LXC** (the incident was Podman, but the tool must
  not be Podman-specific).
- Guide the human toward fixing it *in the container*.
- Both a **GNOME Shell extension UI** and a **CLI UI** (usable independently or
  together).
- Runs on a **systemd timer**.

**Explicitly rejected approach**: capping container CPU (`podman run --cpus …`).
The requester ruled this out — it *hides the symptom* rather than surfacing the
underlying runaway. This tool makes the symptom *loud and attributable* instead.

---

## 4. Proposed architecture (full stack)

```
            ┌──────────────────────────────────────────────────────────┐
 systemd    │ container-watch.timer  (every N minutes, default 2)       │
 timer  ───▶│   └─ container-watch.service (oneshot)                    │
            │        └─ ExecStart: container-watch scan                  │
            └──────────────────────────────────────────────────────────┘
                                   │ writes
                                   ▼
        ┌──────────────────────────────────────────────────────────────┐
        │ REPORTER core (Python, stdlib-only)                            │
        │  1. enumerate /proc/[0-9]*                                     │
        │  2. for each: read cgroup → is it in a container? which one?   │
        │  3. compute age (btime) + CPU delta (two /proc/stat samples)   │
        │  4. apply thresholds → build findings                          │
        │  5. resolve container id→name per engine (+rootless uid)       │
        │  6. translate host PID → in-container PID (NSpid)              │
        │  7. emit:  report.json  +  human report  +  DBus signal        │
        └──────────────────────────────────────────────────────────────┘
              │ report.json (state dir)        │ DBus signal
              ▼                                 ▼
   ┌─────────────────────┐        ┌────────────────────────────────────┐
   │ CLI UI              │        │ GNOME Shell extension               │
   │ container-watch …   │        │  • panel indicator (turns red)      │
   │  status / list /    │        │  • click → menu of current findings │
   │  watch / explain    │        │  • desktop notification on new flag │
   └─────────────────────┘        │  • each finding shows the exec hint │
                                   └────────────────────────────────────┘
```

### 4a. Detection logic

A process is **flagged** when ALL hold:

- It belongs to a container (cgroup contains a known engine marker — §6).
- `age ≥ AGE_THRESHOLD` (default **900 s** / 15 min).
- `cpu_pct ≥ CPU_THRESHOLD` (default e.g. **50 %** of one core, configurable;
  consider expressing as % of a single core so multi-core pinning scores high).

**cgroup format decision (fail-fast, no silent mis-bucketing).** Attribution targets
the **cgroup-v2 unified hierarchy** (`0::/…`), which is Fedora's default. The parser
must handle the off-nominal lines explicitly rather than silently treating them as
"host":

- A **v1-style** `/proc/<pid>/cgroup` (multiple `N:subsys:path` lines, no `0::` unified
  line) is **not** the documented target → **log a clear one-line diagnostic and treat
  that PID as unattributable** (per fail-fast: surface it, do not mis-bucket as host).
- A cgroup that is **clearly containerised** (e.g. under a `user@…/…/…scope` leaf) but
  carries **no recognised engine marker** (future/renamed runtime) is recorded as
  **`engine: "unknown"` — logged, not silently dropped to host**. It is **not a
  finding** (we can't safely attribute or build an `exec_hint`), but it must be
  distinguishable from a genuine host process so a new runtime can't masquerade as host.

CPU "right now" = diff of (`utime`+`stime`) from `/proc/<pid>/stat` across a short
in-invocation interval (e.g. 1–3 s), divided by elapsed clock ticks. For a
"**sustained**" signal across timer firings, optionally persist the last sample
per-PID in the state dir and require N consecutive hot readings before alerting
(reduces false positives from short legitimate bursts like a build). Capture both;
start simple (single-invocation sample) and add the persistent "sustained" gate if
noisy. **PID-reuse across ticks**: if this persistent gate is built, the per-PID
record must include `starttime` (stat field 22) in its identity key — across the
minutes-apart timer firings PID reuse is far likelier than across the ~1–3 s
in-invocation samples, so a `starttime` mismatch must **reset** the consecutive-hot
counter rather than letting a recycled PID inherit a prior tick's hot count.

**Detection latency.** Because the age gate (default 900 s) is checked on each timer
firing, the *first* alert for a runaway lands at
`max(age_threshold, sustained-window) + ≤ 1 timer interval` — i.e. the 2-min timer
cadence does **not** shorten the 15-min age threshold; it only bounds how soon after
the threshold is met the finding surfaces. Tests collapse this to milliseconds by
injecting a low `CW_AGE_S` rather than waiting.

### 4b. Report format (single source of truth for both UIs)

`report.json` (atomic write to a state dir — see §7 for path choice):

```jsonc
{
  "schema": 1,
  "generated_at": "<unix-ts>",          // stamp at runtime; not in this doc
  "host_cores": 22,
  "thresholds": { "age_s": 900, "cpu_pct": 50 },
  "findings": [
    {
      "host_pid": 2124472,
      "container_pid": 12,               // from NSpid 2nd field (first nested level);
                                         // null/omitted when untranslated — never 0 (§2c)
      "engine": "podman",               // podman | docker | lxc
      "rootless": true,
      "owner_uid": 1000,
      "container_id": "<id>",
      "container_name": "<project-a>_yolo",
      "argv0": "ugrep",
      "cmd": "ugrep … -rl <pattern> /",
      "age_s": 6916,
      "cpu_pct": 1116,
      "rss_kb": 35784,
      "exec_hint": "podman exec -it <project-a>_yolo ps -o pid,%cpu,args -p <container-pid>"
    }
  ]
}
```

The `exec_hint` is the "guide the human" payload — the exact command to inspect the
offender *inside* its container, engine-correct (see §6 for the exec command per
engine).

### 4c. Signalling the UI — DBus (repo already has the pattern)

This repo's `speech-to-text@fedora-desktop` extension already uses a **DBus signal
from a CLI backend → GNOME extension** (`StateChanged`/`Error`). Reuse that
pattern: the reporter emits a DBus signal on the **all-lowercase** namespace
`org.fedoradesktop.ContainerWatch` (path `/org/fedoradesktop/ContainerWatch`) —
matching the working precedent `org.fedoradesktop.SpeechToText` /
`/org/fedoradesktop/SpeechToText` (`files/home/.local/bin/wsi:74-75`,
`extensions/speech-to-text@fedora-desktop/extension.js:24-25`); **camelCase
`fedoraDesktop` would silently break signal subscription**. The signal (e.g.
`FindingsChanged`) carries a count + the report path; the extension listens and
updates the panel + raises a notification. The extension reads the full detail
from `report.json` (or the signal carries the JSON). The CLI reads the same
`report.json`. **One data source, two front-ends.**

### 4d. GNOME Shell extension

- Panel indicator: neutral when `findings == []`, attention-coloured when not.
- Click → popup menu listing each finding: container name, cmd (truncated), age,
  CPU%, and a copyable `exec_hint`.
- Desktop notification when a *new* finding appears (dedupe on
  host_pid+container_id so it doesn't re-notify every timer tick).
- Must declare the correct GNOME Shell major in `metadata.json` — gated by
  `python3 -m helpers.gnome.check_extension_compat` against `vars/fedora-version.yml`
  (F44 → GNOME 50). See `CLAUDE/GnomeShell.md`.

### 4e. CLI UI

`container-watch` subcommands (interactive-script UX rules in
`CLAUDE/InteractiveScripts.md` apply if any prompt is added; otherwise
non-interactive + fail-fast):

- `container-watch scan` — run a detection pass now, write report, emit signal
  (this is what the timer calls).
- `container-watch status` — one-line summary (N findings / OK).
- `container-watch list` — table of current findings (reads `report.json`).
- `container-watch explain <host_pid>` — full detail + the exec hint + ancestry.
- `container-watch watch` — live-refresh loop for terminal use.

---

## 5. Implementation language & repo conventions

- **Reporter core + CLI**: Python, **stdlib-only** (matches the `helpers/`
  convention — `helpers/CLAUDE.md`). `/proc` parsing, JSON, and DBus emission are
  all doable with stdlib (`socket`/`dbus` via `gi`? — note: DBus from Python may
  pull `gi`/`pydbus`; if stdlib-only must hold, emit the signal via a tiny
  `gdbus call`/`busctl` subprocess instead. **Open question — see §8.**)
- **GNOME extension**: JS (GNOME Shell 45+ ESM imports — `CLAUDE/GnomeShell.md`);
  ESLint via `extensions/node_modules/.bin/eslint`.
- **systemd units + thin wrappers**: deployed as files, installed by Ansible.

---

## 6. Multi-engine support — the cross-cutting detail

cgroup-v2 path markers and resolution per engine:

| Engine               | cgroup path marker (in `/proc/<pid>/cgroup`)                         | id/name token            | Resolve id → name                                                                                                                               | Inspect inside container   |
| -------------------- | -------------------------------------------------------------------- | ------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------- |
| **Podman** rootless  | `…/user-<uid>.slice/…/libpod-<id>.scope/container`                   | `<id>` (64-hex)          | user timer: `podman inspect --format '{{.Name}}' <id>` (native, as session user); root fallback only: `runuser -u "#<uid>" -- podman inspect …` | `podman exec -it <name> …` |
| **Podman** rootful   | `/machine.slice/libpod-<id>.scope/container`                         | `<id>`                   | `sudo podman inspect …`                                                                                                                         | `sudo podman exec …`       |
| **Docker** (rootful) | `/system.slice/docker-<id>.scope` (systemd driver) or `/docker/<id>` | `<id>`                   | `docker inspect --format '{{.Name}}' <id>`                                                                                                      | `docker exec -it <name> …` |
| **LXC / LXD**        | `…/lxc.payload.<name>/…` or `/lxc/<name>`                            | `<name>` (already plain) | name is in the path; verify `lxc-info -n <name>`                                                                                                | `lxc-attach -n <name>`     |

**Decision — timer model and rootless resolution (resolves the F2 tension).** The
chosen model is the **`systemd --user` timer running as the session user** (see the
design-consequence table below). Under that model the timer **is** the rootless-Podman
owning uid, so it resolves its **own** rootless containers natively with a plain
`podman inspect` and **no uid hop**. The cross-uid `runuser -u "#<uid>"` /
`machinectl shell` machinery requires **root** and a non-root user timer cannot use it
— so it is **out of scope for the user-timer path**. It is retained **only** in the
documented system-level (root) multi-uid fallback, where root makes it valid. The
"resolve id→name as `<uid>`" column in the §6 table is therefore **conditional on the
timer model**: native `podman inspect` for the user timer; `runuser`-as-uid only for
the root fallback.

Resolution-ownership matrix (this repo's reality, per `CLAUDE/ContainerEngines.md`),
under the **recommended user-timer model**:

- **Rootless Podman** containers belong to the **invoking user**, which **is** the
  user timer — resolve with a plain `podman inspect` **as the session user**, no uid
  hop. (Extracting `<uid>` from the cgroup is still useful to *report* `owner_uid`,
  and to assert the container is the session user's; cross-uid `runuser -u "#<uid>"`
  is only needed in the **root system-level fallback**, where root makes it valid.)
- **Docker is rootful**; the repo puts the primary user in the `docker` group, so
  `docker inspect` works **as the user** (no sudo needed in this single-user
  workstation context).
- **LXC is rootful**; the repo's user has passwordless sudo, so `sudo lxc-info` /
  `sudo lxc-attach` are available.

**Design consequence — where does the monitor run?**

| Option                       | Sees rootful Docker/LXC                    | Sees rootless Podman         | Can notify GUI         | Verdict                                           |
| ---------------------------- | ------------------------------------------ | ---------------------------- | ---------------------- | ------------------------------------------------- |
| **User-level** systemd timer | yes (user in `docker` group; sudo for lxc) | yes (own containers, native) | **yes** (in session)   | **Recommended** for this single-user workstation  |
| System-level (root) timer    | yes                                        | needs per-uid shell-out      | no (must hand to user) | More portable, but notification handoff is clunky |

Recommendation: **user-level** `systemd --user` timer. Detection reads
world-readable `/proc`, so it sees root-owned Docker/LXC processes too; name
resolution uses the group/sudo access the repo already grants; and notifications
reach the GNOME session trivially. Record the system-level option as the
multi-user fallback.

---

## 7. Repo integration (Ansible IaC)

Everything ships via Ansible — no manual installs (`CLAUDE/InfrastructureAsCode.md`).

Proposed file layout (final paths TBD in PLAN.md):

- `files/home/.local/bin/container-watch` — Python CLI + reporter core (or split
  core into a `helpers/` package + thin bin wrapper; decide in PLAN.md).
- `files/home/.config/systemd/user/container-watch.service` — oneshot
  `ExecStart=%h/.local/bin/container-watch scan`.
- `files/home/.config/systemd/user/container-watch.timer` —
  `OnUnitActiveSec`/`OnBootSec` (default 2 min).
- `extensions/container-watch@fedora-desktop/` — `metadata.json` (+ correct
  `shell-version`) and `extension.js`. **Extension source lives under `extensions/`,
  not `files/home/.local/share/gnome-shell/extensions/`** — that is the repo
  convention: `play-speech-to-text.yml` deploys from
  `{{ root_dir }}/extensions/{{ extension_name }}` and
  `play-gnome-shell-extensions.yml` copies `extensions/workspace-names-overview@…/`
  inline. The `extensions/<uuid>/metadata.json` compat gate
  (`helpers.gnome.check_extension_compat`) and `extensions/CLAUDE.md` both assume
  this location. The playbook copies from `{{ root_dir }}/extensions/…` to the
  user's `~/.local/share/gnome-shell/extensions/`.
- `playbooks/imports/optional/common/play-container-watch.yml` — deploys files,
  enables the user timer (`systemd: scope: user, enabled: yes, state: started`),
  enables the extension. **Integration point** (matching how the repo actually wires
  extensions, see F4 below): either add the deploy tasks into
  `play-gnome-shell-extensions.yml` (the always-on inline pattern used for
  `workspace-names-overview`), **or** ship this as an opt-in optional play — do
  **not** assume a `playbook-main.yml` import, which the comparable
  `play-speech-to-text.yml` does **not** have. Verify the chosen wiring against
  `playbook-main.yml` at implementation time.

State/report location: a user-writable runtime dir, e.g.
`$XDG_RUNTIME_DIR/container-watch/report.json` (tmpfs, cleared on logout) with the
last-sample state alongside. Avoids polluting `$HOME`; world-readable concerns are
moot since it is per-user under `/run/user/<uid>`.

QA before any commit (`CLAUDE/QA.md`):

- `./scripts/qa-all.bash` — Bash/Python/Ansible (py_compile + ruff, syntax-check,
  fail-fast grep, etc.).
- `cd extensions && node_modules/.bin/eslint <ext>/extension.js` — extension JS.
- `python3 -m helpers.gnome.check_extension_compat` — GNOME version gate for the
  new extension's `metadata.json`.
- If any helper package is added under `helpers/`: `./scripts/qa-helper-tests.bash`.

---

## 8. Open design decisions (resolve in PLAN.md before building)

1. **DBus from stdlib-only Python** — emitting a DBus signal may require `gi`
   (PyGObject), which breaks the `helpers/` stdlib-only rule. Options: (a) shell
   out to `gdbus emit` / `busctl emit` from the Python reporter (keeps Python
   stdlib-only); (b) allow `gi` for this specific tool and place it outside
   `helpers/`; (c) skip DBus and have the extension *poll* `report.json` on a
   timer. Leaning **(a)** — cleanest fit with conventions and the existing
   speech-to-text precedent. **Decide.**
2. **CPU threshold semantics** — % of a single core (so multi-core pinning >100%
   and scores obviously high) vs % of total host capacity. Leaning **per-core %**
   (the incident was 1116% — instantly legible). **Confirm default.**
3. **"Sustained" gate** — single-invocation CPU sample vs requiring N consecutive
   hot timer ticks (needs persisted per-PID state). Start simple, add if noisy.
4. **Reporting-only enforcement** — confirm the tool ships with **no** kill path
   at all (not even an opt-in flag), to keep it unambiguously safe. Guidance text
   may *show* the human the kill command, but the tool never runs it.
5. **Allowlist** — some long+hot container processes are legitimate (a long build,
   a training job, a dev server). Need a per-container or per-cmd allowlist
   (config file) so they don't alert forever. **Design the config.**
6. **CLI vs core split** — single `container-watch` script vs `helpers/` package +
   bin wrapper (better for unit tests via `qa-helper-tests.bash`). Leaning split.

---

## 9. Alternatives considered (and why bespoke is justified)

- `systemd-cgtop` / `ctop` / `cadvisor` / `podman stats` — all show *live* CPU per
  cgroup/container, but none combine **(age ≥ threshold) + (CPU pinned) +
  (per-process attribution within the container) + (guided in-container
  resolution) + (GNOME panel alert)**. They answer "what's hot now", not "alert me
  when a container has a stuck, long-running, CPU-pinned process and tell me how to
  fix it inside that container." The bespoke tool is the thin orchestration layer
  over the same `/proc`+cgroup data these tools read.
- Per-container CPU caps (`--cpus`) — **rejected** by requester: hides the symptom.

---

## 10. Appendix — sanitised commands used during triage (reproducible)

```bash
# Top processes by CPU (instant snapshot)
ps -eo pid,ppid,pcpu,etimes,rss,comm,args --sort=-pcpu
top -b -n1 -o %CPU

# Attribute a PID to its container (authoritative)
cat /proc/<pid>/cgroup                       # → libpod-<id>.scope = Podman, etc.
grep NSpid /proc/<pid>/status                # host PID → in-container PID (last field)

# Confirm via ancestry
ps -o ppid=,comm=,args= -p <pid>             # walk PPID to PID 1

# Resolve a Podman container id/conmon → name (rootless: as owning uid)
podman ps --format '{{.ID}} {{.Names}} {{.Image}}'
podman inspect --format '{{.Name}} {{.State.ConmonPid}} {{.State.Pid}}' <container>

# IMPORTANT: match the offender by argv[0], NOT comm (comm lied: "claude.exe")
for d in /proc/[0-9]*; do
    cmd=$(tr '\0' ' ' < "$d/cmdline")
    case "$cmd" in ugrep\ *) echo "$d: $cmd";; esac
done
```

Incident outcome: the runaway was handled manually. This plan ensures the *next*
one is detected, attributed, and surfaced automatically — reporting-only.
