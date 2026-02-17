# Pre-Planning Research: claude-devtools Installation

**Date**: 2026-02-17
**Status**: Research / Pre-planning
**Repo**: https://github.com/matt1398/claude-devtools

---

## What Is claude-devtools?

A desktop/server application that reconstructs and visualizes Claude Code session logs. Addresses a specific pain point: recent Claude Code updates replaced detailed tool output with opaque summaries ("Read 3 files") without showing content. This tool restores that visibility **without modifying Claude Code itself**.

**Key capabilities:**
- Context window reconstruction with per-turn token attribution (7 categories)
- Compaction detection and visualization
- Tool call inspector with syntax-highlighted file reads, inline diffs, bash output
- Subagent/team visualization (expandable trees, colour-coded messages)
- Custom notification triggers (regex-based, e.g. alert on `.env` access)
- SSH remote session inspection
- Cross-session search (Cmd+K command palette)
- Multi-pane side-by-side comparison

**Stats**: 453 stars, 47 forks, MIT license.

---

## Installation Options

| Platform | Format |
|---|---|
| macOS | `.dmg` |
| Linux | `.AppImage`, `.deb`, `.rpm`, `.pacman` |
| Windows | `.exe` |
| Docker/Podman | `docker compose up` → `http://localhost:3456` |
| Node.js standalone | `pnpm install && pnpm standalone:build` |

**Config (Docker/standalone):**

| Variable | Default | Description |
|---|---|---|
| `CLAUDE_ROOT` | `~/.claude` | Path to `.claude` session directory |
| `HOST` | `0.0.0.0` | Bind address |
| `PORT` | `3456` | Listening port |

---

## Critical Architectural Finding: CCY Session Storage

**CCY does NOT use `~/.claude/` — it uses project-level `.claude/`.**

From the CCY script (lines 147, 191–195):
```
State persists in /workspace/.claude/ (project-level, part of your repo)
Desktop Claude Code (~/.claude/) is NEVER modified
Each project has its own isolated state
```

This means:
- **Host Claude Code sessions** → `~/.claude/`
- **CCY project sessions** → `/path/to/project/.claude/` (e.g. `~/Projects/fedora-desktop/.claude/`)

**These are completely separate.** claude-devtools out of the box (pointing at `~/.claude/`) will see host sessions but **miss all CCY container sessions entirely.**

---

## The CCY Integration Problem

The core issue: claude-devtools expects a single `CLAUDE_ROOT` directory. CCY creates session data scattered across potentially many project directories.

### What exists in each location:

**`~/.claude/`** (host global):
- Claude Desktop app sessions
- claude-devtools README says this is default target
- No CCY project sessions

**`<project>/.claude/`** (CCY project-level):
- Session logs for all CCY runs in that project
- Hooks config, CLAUDE.md, etc.
- E.g. `~/Projects/fedora-desktop/.claude/`

---

## Deployment Options Considered

### Option A: RPM/AppImage on host (Ansible-installed desktop app)

**How:** Install the `.rpm` release via Ansible to host system.
**Sees:** `~/.claude/` (host sessions only, no CCY)
**Pros:** Native desktop app, no extra services, simplest setup
**Cons:** Misses all CCY project sessions; no solution for the core problem

**Verdict:** Partial solution only.

### Option B: Podman container service on host, global `~/.claude/`

**How:** Run as a rootless Podman service via systemd user unit, mount `~/.claude/` read-only.
**Sees:** `~/.claude/` (host sessions only, no CCY)
**Pros:** Containerized, clean, Ansible-deployable as a service
**Cons:** Same CCY blind-spot as Option A

**Verdict:** Partial solution only.

### Option C: Per-project Podman container with project `.claude/`

**How:** Run claude-devtools container locally within each project, mounting `<project>/.claude/` via `CLAUDE_ROOT`.
**Sees:** That project's CCY sessions only
**Pros:** Sees CCY sessions; could be triggered as part of `ccy` startup
**Cons:** Multiple instances, port management complexity, per-project friction

**Verdict:** Works but awkward UX.

### Option D: Hybrid — host service for `~/.claude/` + CCY integration hook

**How:**
1. Ansible installs claude-devtools as a host Podman service on a fixed port (e.g. 3456), pointing at `~/.claude/`
2. Add a CCY hook or wrapper that spins up a second instance on a different port pointing at the project's `.claude/` when a ccy session starts, and tears it down on exit

**Pros:** Covers both use cases; host sessions always available; CCY sessions available when actively using ccy
**Cons:** More complex setup; two instances; need to manage port assignment

**Verdict:** Best coverage, moderate complexity.

### Option E: Symlink aggregation (symlink project `.claude/projects/` into `~/.claude/`)

**How:** CCY creates sessions in `<project>/.claude/projects/`. Symlink these into `~/.claude/projects/` per-project, so a single claude-devtools instance sees everything.
**Pros:** Single instance, no port management
**Cons:** Fragile (symlinks can break), creates cross-contamination between global and project state, CCY explicitly separates these for a reason, may confuse Claude Code itself

**Verdict:** Too fragile, bad design.

### Option F: AppImage/RPM on host, manual `CLAUDE_ROOT` switching

**How:** Install desktop app, user manually points it at `~/.claude/` or `<project>/.claude/` as needed via UI/env var.
**Pros:** Simplest deployment
**Cons:** Entirely manual; no automation; defeats the purpose

**Verdict:** Last resort fallback.

---

## Correction: CCY Sessions Are On The Host Filesystem

**Earlier analysis was overcomplicating this.**

CCY mounts the project directory as `/workspace` inside the container. Claude Code writes session files to `/workspace/.claude/`. When the container exits, those files remain on the **host** at `<project-dir>/.claude/`.

**The container is irrelevant after the fact.** claude-devtools doesn't need to talk to any running container, join a Podman network, or be a sibling container. It just needs to read files.

This is purely a **filesystem access problem**:

| Session type | Host path | `CLAUDE_ROOT` value |
|---|---|---|
| Host Claude Code | `~/.claude/` | `~/.claude` (default) |
| CCY project session | `~/Projects/myproject/.claude/` | `~/Projects/myproject/.claude` |

**No container networking needed. No sibling containers. No inter-container communication.**

---

## Revised Deployment Options

### Option A: Native RPM/AppImage on host

- Ansible installs the `.rpm` from GitHub releases
- For host sessions: open the app (uses `~/.claude/` by default)
- For CCY sessions: `CLAUDE_ROOT=~/Projects/myproject/.claude claude-devtools`
- Shell function `ccdt [path]` makes this ergonomic
- **Pros**: native desktop integration, Fedora-native, simplest
- **Cons**: version pinning, manual update process, no auto-update

### Option B: Podman container, always-on service

- Systemd user unit runs the container, mounts `~/.claude/` by default
- For CCY sessions: restart with different mount or second instance on different port
- **Pros**: isolated, Ansible-managed, easy to update image
- **Cons**: friction for CCY project switching; persistent service uses resources when idle

### Option C: Podman container, on-demand launcher (preferred)

- Shell function `ccdt [project-path]` spins up a fresh container, opens browser, cleans up on exit
- No persistent service needed
- Identical UX for both host and CCY sessions — just pass different path
- Ansible deploys: the shell function + pre-pulls the Docker image

```bash
# Host sessions (default)
ccdt

# CCY project sessions
ccdt ~/Projects/fedora-desktop
# internally: CLAUDE_ROOT=/data/.claude
#             podman run --rm -p 3456:3456 -v ~/Projects/fedora-desktop/.claude/ccy:/data/.claude:ro ...
```

- **Pros**: zero persistent overhead, works identically for all session types, most flexible
- **Cons**: slight startup delay per launch (image already pulled so minimal)

---

## Recommended Approach (Draft)

**Option C: On-demand Podman launcher** is the best fit for this project.

**Phase 1**: Ansible playbook deploys:
- Pre-pull the `ghcr.io/matt1398/claude-devtools` image (or build from source)
- Install a `ccdt` shell function (or script) in `~/.bashrc-includes/` or `/usr/local/bin/`
- Function mounts the right `.claude/` directory and opens the browser

```bash
ccdt                              # host sessions (~/.claude/)
ccdt ~/Projects/fedora-desktop    # CCY sessions for that project
```

**Phase 2**: Investigate whether `ccdt` should be integrated into the `ccy` workflow at all — e.g. print a reminder "run ccdt ~/Projects/foo to view this session" after ccy starts. This is optional UX polish.

---

## Open Questions — RESOLVED

All questions resolved by inspecting the live CCY container.

### Q1: Where exactly are CCY session logs stored?

**Answer confirmed from live container inspection:**

```
/root/.claude -> /workspace/.claude/ccy  (symlink created by entrypoint)
```

The CCY container entrypoint symlinks `/root/.claude` → `/workspace/.claude/ccy`. All Claude Code session data goes through this symlink, landing on the **host** at:

```
<project-dir>/.claude/ccy/
```

For this project: `~/Projects/fedora-desktop/.claude/ccy/`

Contents (50MB of session data confirmed):
```
cache/  debug/  file-history/  history.jsonl  paste-cache/  plans/  plugins/
projects/  session-env/  settings.json  shell-snapshots/  stats-cache.json
statsig/  tasks/  teams/  telemetry/  todos/
```

### Q2: Same structure as `~/.claude/`?

**Yes — it IS `~/.claude/` format.** It's literally the same directory structure (the symlink makes `/root/.claude` point to `/workspace/.claude/ccy`, so Claude Code writes identically to how it would write `~/.claude/` on a host install). claude-devtools will read it without any compatibility issues.

### Q3: Does claude-devtools support multiple roots in the UI?

**Needs verification** — but the `CLAUDE_ROOT` env var handles this at launch time. A shell wrapper function is the correct approach regardless.

### Q4: Release format for Fedora 42?

Options: `.rpm`, `.AppImage`, or Podman container image. Podman container preferred — no packaging, Ansible just pulls the image and the `ccdt` wrapper handles `CLAUDE_ROOT` and mounts.

### Q5: Always-on service or on-demand?

**On-demand** — no reason to run a persistent service. A `ccdt` shell function spins it up as needed.

### Q6: Port conflicts?

To verify on host (not answerable from container). 3456 is the default. Can be overridden.

### Q7: CCY integration approach?

No need to modify the CCY script. A standalone `ccdt` function is sufficient.

Name follows the `cc` (Claude Code) convention: `ccy` (YOLO), `ccdt` (devtools).

```bash
ccdt                              # host sessions → CLAUDE_ROOT=~/.claude
ccdt ~/Projects/fedora-desktop    # CCY sessions  → CLAUDE_ROOT=<project>/.claude/ccy
```

---

## Definitive Architecture

| Session type | Host path | `CLAUDE_ROOT` value |
|---|---|---|
| Host Claude Code | `~/.claude/` | `~/.claude` |
| CCY project sessions | `<project-dir>/.claude/ccy/` | `<project-dir>/.claude/ccy` |

**Verified from live container**: `/root/.claude` is a symlink to `/workspace/.claude/ccy`. Sessions persist on the host. No container networking needed. Pure filesystem access.

---

## Key Constraint: Ansible-Only Deployment

Per CLAUDE.md, ALL deployment must go through Ansible. This means:
- No manual `podman run` or `docker run` commands
- Create/update a playbook: `playbooks/imports/optional/common/play-install-claude-devtools.yml`
- The playbook should install + configure as a systemd user service
- File-based config in `files/` directory structure

---

## Suggested Plan Structure (for full PLAN.md)

- **Phase 1**: Research & decisions (answer open questions above)
- **Phase 2**: Ansible playbook for host service deployment (Podman container)
- **Phase 3**: CCY integration (per-project devtools or wrapper approach)
- **Phase 4**: Test & document

---

## References

- Repo: https://github.com/matt1398/claude-devtools
- CCY script: `files/var/local/claude-yolo/claude-yolo` (line 147: project-level `.claude/`)
- CCY install playbook: `playbooks/imports/optional/common/play-install-claude-yolo.yml`
- Plan workflow: `CLAUDE/PlanWorkflow.md`
