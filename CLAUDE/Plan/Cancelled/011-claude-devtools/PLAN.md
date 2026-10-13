# Plan 011: claude-devtools (ccdt) Installation

**Status**: Not Started
**Created**: 2026-02-17
**Owner**: Claude Code Agent
**Priority**: Medium
**Estimated Effort**: 3-4 hours

---

## Overview

This plan installs `ccdt` — a shell wrapper command that launches
[claude-devtools](https://github.com/matt1398/claude-devtools) as an on-demand Podman container.
claude-devtools reconstructs and visualises Claude Code session logs, restoring the detailed tool
output visibility that recent Claude Code updates removed (replacing it with opaque summaries such
as "Read 3 files").

The `ccdt` name follows the existing `cc`-prefix convention used in this project: `ccy` for
Claude YOLO mode, `ccb` for Claude Browser mode. The tool provides two distinct views: host
Claude Code sessions stored in `~/.claude/`, and CCY project sessions stored in
`<project-dir>/.claude/ccy/` (the location confirmed by live container inspection — the CCY
entrypoint symlinks `/root/.claude` → `/workspace/.claude/ccy`).

`ccdt` is deployed as an on-demand Podman container rather than a persistent service. There is no
reason to run a background service when sessions are simply files on disk; the container starts
instantly (image pre-pulled), opens the browser, and cleans up on exit. Ansible handles all
deployment: image pull and shell function installation. No manual steps are required or permitted.

---

## Goals

- Deploy a `ccdt` shell command that launches claude-devtools via Podman on demand.
- Auto-detect whether the current directory is a CCY project and select the correct `CLAUDE_ROOT`
  automatically.
- Support explicit path argument and `--host` flag for override cases.
- Pre-pull the `ghcr.io/matt1398/claude-devtools` container image during Ansible deployment so
  startup is fast.
- Deploy everything through a single Ansible playbook following all project conventions.
- Ensure no personal information, hardcoded paths, or credentials appear in committed files.

---

## Non-Goals

- Persistent background service or systemd user unit for claude-devtools.
- Modifying the `ccy` script itself (ccdt is a standalone tool).
- Support for Docker (only Podman, consistent with project default; Docker support not needed).
- Aggregating multiple project sessions into a single view simultaneously.
- Automatic notification or integration when a CCY session starts.
- Support for Windows or macOS (Fedora host only).

---

## Context & Background

Full research is documented in `CLAUDE/Plan/011-claude-devtools/pre-planning-research.md`.

**Key confirmed facts:**

| Session type | Host filesystem path | `CLAUDE_ROOT` for ccdt |
|---|---|---|
| Host Claude Code | `~/.claude/` | `~/.claude` |
| CCY project sessions | `<project-dir>/.claude/ccy/` | `<project-dir>/.claude/ccy` |

The CCY entrypoint symlinks `/root/.claude` → `/workspace/.claude/ccy`, so all session data
written inside the container lands on the host at `<project-dir>/.claude/ccy/`. The directory
structure is identical to `~/.claude/` — claude-devtools reads it without any compatibility
issues. No container networking is needed; this is a pure filesystem access problem.

**Deployment pattern** follows `play-install-claude-yolo.yml`:
- Ansible playbook at `playbooks/imports/optional/common/play-install-claude-devtools.yml`
- Shell function (bashrc include) deployed to `~/.bashrc-includes/claude-devtools.bash` for the
  user account (same pattern as `claude-browser.bash` and `claude-yolo.bash`)
- Container image pre-pulled by Ansible (not built — upstream image used directly)
- `vars/container-defaults.yml` provides `container_engine: podman`

**UX design for `ccdt`:**

```bash
ccdt                    # No args, not in CCY project → host sessions (~/.claude)
ccdt                    # No args, inside CCY project  → auto-detects .claude/ccy/
ccdt ~/Projects/foo     # Explicit path → smart-detects .claude/ccy/ or .claude/
ccdt --host             # Force host sessions regardless of CWD
```

The auto-detection logic: walk up from `$PWD` looking for `.claude/ccy/`; if found, use it as
`CLAUDE_ROOT`; otherwise fall back to `~/.claude`.

---

## Tasks

### Phase 1: Verify and Confirm Approach

- [ ] ⬜ **Task 1.1**: Confirm the upstream container image name and default port
  - [ ] ⬜ Subtask 1.1.1: Verify `ghcr.io/matt1398/claude-devtools` is pullable and current
  - [ ] ⬜ Subtask 1.1.2: Confirm default port is 3456 and `CLAUDE_ROOT` env var is supported
  - [ ] ⬜ Subtask 1.1.3: Check whether port 3456 conflicts with any existing service on the host
    (run `ss -tlnp` on the host system and check for 3456)
  - [ ] ⬜ Subtask 1.1.4: Confirm image is available on `ghcr.io` (preferred) or `docker.io` as
    fallback

- [ ] ⬜ **Task 1.2**: Confirm the auto-detection logic is correct
  - [ ] ⬜ Subtask 1.2.1: Verify `.claude/ccy/` exists in a real CCY project directory on the host
  - [ ] ⬜ Subtask 1.2.2: Verify `~/.claude/` exists and has the expected structure on the host

### Phase 2: Write the `ccdt` Shell Script

- [ ] ⬜ **Task 2.1**: Create `files/home/.local/bin/ccdt` (the shell script)
  - [ ] ⬜ Subtask 2.1.1: Add shebang (`#!/usr/bin/env bash`) and `set -euo pipefail`
  - [ ] ⬜ Subtask 2.1.2: Implement `--help` flag with clear usage output
  - [ ] ⬜ Subtask 2.1.3: Implement `--host` flag to force `~/.claude` as `CLAUDE_ROOT`
  - [ ] ⬜ Subtask 2.1.4: Implement explicit `<path>` argument: check for `.claude/ccy/` within
    the given path first, then `.claude/`, then error if neither exists
  - [ ] ⬜ Subtask 2.1.5: Implement no-arg auto-detection: walk up from `$PWD` looking for
    `.claude/ccy/`; if found, use it; otherwise fall back to `~/.claude`
  - [ ] ⬜ Subtask 2.1.6: Validate that the resolved `CLAUDE_ROOT` directory exists before
    launching the container; fail fast with a helpful error if not
  - [ ] ⬜ Subtask 2.1.7: Build and run the `podman run` command:
    - `--rm` (clean up on exit)
    - `-p 3456:3456`
    - `-v <CLAUDE_ROOT>:/data/.claude:ro` (read-only mount)
    - `-e CLAUDE_ROOT=/data/.claude`
    - `ghcr.io/matt1398/claude-devtools`
  - [ ] ⬜ Subtask 2.1.8: Print a clear startup message showing which session path is being used
    and the URL to open (`http://localhost:3456`)
  - [ ] ⬜ Subtask 2.1.9: No personal information or hardcoded paths in the script
  - [ ] ⬜ Subtask 2.1.10: Run `bash -n files/home/.local/bin/ccdt` to check syntax

- [ ] ⬜ **Task 2.2**: Run QA on the script
  - [ ] ⬜ Subtask 2.2.1: Run `./scripts/qa-all.bash` and fix any errors

### Phase 3: Write the bashrc Include

- [ ] ⬜ **Task 3.1**: Create `files/home/bashrc-includes/claude-devtools.bash`
  - [ ] ⬜ Subtask 3.1.1: Add alias `ccdt` pointing to `~/.local/bin/ccdt`
  - [ ] ⬜ Subtask 3.1.2: Follow the same pattern as `claude-browser.bash` (simple alias file,
    no complex logic)
  - [ ] ⬜ Subtask 3.1.3: Add a comment header explaining the file's purpose
  - [ ] ⬜ Subtask 3.1.4: Run `bash -n` to check syntax

- [ ] ⬜ **Task 3.2**: Run QA
  - [ ] ⬜ Subtask 3.2.1: Run `./scripts/qa-all.bash` and fix any errors

### Phase 4: Write the Ansible Playbook

- [ ] ⬜ **Task 4.1**: Create `playbooks/imports/optional/common/play-install-claude-devtools.yml`
  - [ ] ⬜ Subtask 4.1.1: Add shebang `#!/usr/bin/env ansible-playbook` as the first line
    (required per `playbooks/CLAUDE.md`)
  - [ ] ⬜ Subtask 4.1.2: Set `hosts: desktop`, `become: false`, include `vars/container-defaults.yml`
  - [ ] ⬜ Subtask 4.1.3: Task — check container engine is installed (same guard pattern as
    `play-install-claude-yolo.yml`)
  - [ ] ⬜ Subtask 4.1.4: Task — verify container engine service is accessible (`podman ps`)
  - [ ] ⬜ Subtask 4.1.5: Task — pull the `ghcr.io/matt1398/claude-devtools` image using
    `containers.podman.podman_image` module or `ansible.builtin.command` with `podman pull`
    (idempotent: use `force: false` or check image existence first)
  - [ ] ⬜ Subtask 4.1.6: Task — ensure `~/.local/bin/` directory exists for the user
  - [ ] ⬜ Subtask 4.1.7: Task — copy `files/home/.local/bin/ccdt` to
    `/home/{{ user_login }}/.local/bin/ccdt` with `mode: "0755"`, correct owner/group
  - [ ] ⬜ Subtask 4.1.8: Task — deploy bashrc include for the user account:
    copy `files/home/bashrc-includes/claude-devtools.bash` to
    `/home/{{ user_login }}/.bashrc-includes/claude-devtools.bash`
  - [ ] ⬜ Subtask 4.1.9: Task — display installation summary message (URL, example commands,
    reload instruction) following the same pattern as the CCY playbook summary
  - [ ] ⬜ Subtask 4.1.10: Make the playbook file executable: `chmod +x` on the host after
    deployment, or add a note in the summary for the user
  - [ ] ⬜ Subtask 4.1.11: Verify playbook syntax with `ansible-playbook --syntax-check`
    (run on host, not in CCY container)

- [ ] ⬜ **Task 4.2**: Run QA on playbook-adjacent files
  - [ ] ⬜ Subtask 4.2.1: Run `./scripts/qa-all.bash` and fix any errors

### Phase 5: Commit Changes

- [ ] ⬜ **Task 5.1**: Stage and review all changes before committing
  - [ ] ⬜ Subtask 5.1.1: Run `git diff --staged` and scan for any personal info, email
    addresses, hardcoded paths with usernames, or tokens (this is a public repo)
  - [ ] ⬜ Subtask 5.1.2: Confirm all three files are staged:
    - `files/home/.local/bin/ccdt`
    - `files/home/bashrc-includes/claude-devtools.bash`
    - `playbooks/imports/optional/common/play-install-claude-devtools.yml`
  - [ ] ⬜ Subtask 5.1.3: Run `./scripts/qa-all.bash` one final time before committing
  - [ ] ⬜ Subtask 5.1.4: Commit with message referencing this plan:
    `Plan 011: Add ccdt command for claude-devtools session viewer`

### Phase 6: Test on Host System

- [ ] ⬜ **Task 6.1**: Deploy via Ansible on the host system
  - [ ] ⬜ Subtask 6.1.1: Pull latest changes on the host: `git pull`
  - [ ] ⬜ Subtask 6.1.2: Run the playbook on the host (not in CCY container):
    `ansible-playbook playbooks/imports/optional/common/play-install-claude-devtools.yml`
  - [ ] ⬜ Subtask 6.1.3: Reload shell: `source ~/.bashrc`

- [ ] ⬜ **Task 6.2**: Test host session mode
  - [ ] ⬜ Subtask 6.2.1: Run `ccdt --help` and confirm output is clear and correct
  - [ ] ⬜ Subtask 6.2.2: Run `ccdt` from a non-CCY directory; confirm it uses `~/.claude/`
  - [ ] ⬜ Subtask 6.2.3: Confirm `http://localhost:3456` opens in the browser and shows host
    Claude Code sessions
  - [ ] ⬜ Subtask 6.2.4: Press Ctrl+C to exit; confirm the container is cleaned up (no
    dangling container with `podman ps -a --filter name=ccdt`)

- [ ] ⬜ **Task 6.3**: Test CCY session auto-detection mode
  - [ ] ⬜ Subtask 6.3.1: `cd ~/Projects/fedora-desktop` (or any project with `.claude/ccy/`)
  - [ ] ⬜ Subtask 6.3.2: Run `ccdt`; confirm it detects `.claude/ccy/` and prints the correct
    path in the startup message
  - [ ] ⬜ Subtask 6.3.3: Confirm `http://localhost:3456` shows the CCY session history for
    that project
  - [ ] ⬜ Subtask 6.3.4: Press Ctrl+C to exit; confirm clean container teardown

- [ ] ⬜ **Task 6.4**: Test explicit path argument
  - [ ] ⬜ Subtask 6.4.1: Run `ccdt ~/Projects/fedora-desktop` from a different directory;
    confirm it resolves to `.claude/ccy/` within that path
  - [ ] ⬜ Subtask 6.4.2: Run `ccdt ~/.claude`; confirm it uses `~/.claude` directly
  - [ ] ⬜ Subtask 6.4.3: Run `ccdt /nonexistent`; confirm it fails fast with a clear error
    message and non-zero exit code

- [ ] ⬜ **Task 6.5**: Test `--host` flag
  - [ ] ⬜ Subtask 6.5.1: `cd ~/Projects/fedora-desktop`; run `ccdt --host`; confirm it uses
    `~/.claude` despite the presence of `.claude/ccy/`

---

## Dependencies

- Depends on: Podman installed on the host (provided by `playbooks/imports/play-podman.yml`)
- Depends on: `~/.bashrc-includes/` directory existing for the user (set up by main playbook)
- Depends on: `vars/container-defaults.yml` (exists, provides `container_engine: podman`)
- Blocks: None
- Related: Plan 007 (CCY), Plan 008 (CCB) — same `cc`-prefix command family

---

## Technical Decisions

### Decision 1: On-demand Podman container vs persistent service vs native RPM

**Context**: claude-devtools can be deployed as a native RPM/AppImage, a persistent Podman
service, or an on-demand Podman container.

**Options Considered**:
1. Native RPM/AppImage — simple to install, but version pinning and update management are manual;
   no `CLAUDE_ROOT` switching from the command line without env var juggling
2. Persistent Podman service (systemd user unit) — Ansible-managed, but consumes resources at all
   times; switching `CLAUDE_ROOT` requires restart or a second instance
3. On-demand Podman container — zero persistent overhead; `CLAUDE_ROOT` passed at launch time;
   identical UX for host and CCY sessions; image pre-pulled so startup is fast; Ansible-deployable

**Decision**: On-demand Podman container (Option 3).
**Date**: 2026-02-17

### Decision 2: Where to deploy the `ccdt` script

**Context**: Shell scripts in this project are deployed to either `~/.local/bin/` (user scripts,
see `wsi`, `nord`, etc.) or `/var/local/` (system-wide scripts, see `claude-yolo`, `claude-browser`).
A bashrc include provides the alias.

**Options Considered**:
1. `/var/local/claude-devtools/ccdt` (system path, requires `become: true`) — consistent with
   `claude-yolo` and `claude-browser` placement
2. `~/.local/bin/ccdt` (user path, no sudo required) — simpler deployment, consistent with
   `wsi`, `nord`, and other user-only tools

**Decision**: `~/.local/bin/ccdt` (Option 2). claude-devtools is a personal developer tool
with no system-wide utility; user-path deployment is simpler, requires no privilege escalation for
the copy task, and is consistent with the other user tools in this directory.
**Date**: 2026-02-17

### Decision 3: Auto-detection logic for CCY projects

**Context**: The user should not need to remember to pass the path. The script should detect
whether the current directory is inside a CCY project.

**Options Considered**:
1. Check only `$PWD/.claude/ccy/` (no walking up) — simple but breaks if user is in a
   subdirectory of the project
2. Walk up from `$PWD` to find `.claude/ccy/` (same approach as git finding `.git/`) — works
   from any subdirectory of the project
3. Require explicit path always — no magic, but poor UX

**Decision**: Walk up from `$PWD` (Option 2). Mirrors the well-understood git repo detection
pattern. Fall back to `~/.claude` if no `.claude/ccy/` is found in the directory tree.
**Date**: 2026-02-17

### Decision 4: Read-only vs read-write mount

**Context**: claude-devtools needs to read session files. It may or may not write cache or state.

**Options Considered**:
1. Read-only mount (`:ro`) — safest; prevents any accidental modification of session data
2. Read-write mount — allows claude-devtools to cache data inside the session directory

**Decision**: Read-only mount (`:ro`) initially. If claude-devtools requires write access (e.g.,
for its own cache), this can be relaxed to `:z` (SELinux relabelling). Fail-safe default.
**Date**: 2026-02-17

---

## Success Criteria

- [ ] `ccdt --help` outputs clear usage instructions
- [ ] `ccdt` from a non-CCY directory opens host sessions at `http://localhost:3456`
- [ ] `ccdt` from within a CCY project auto-detects `.claude/ccy/` and opens project sessions
- [ ] `ccdt <path>` resolves the correct `CLAUDE_ROOT` from the given path
- [ ] `ccdt --host` forces host sessions regardless of CWD
- [ ] Invalid path argument produces a clear error and non-zero exit
- [ ] Container cleans up on exit (no dangling containers)
- [ ] Ansible playbook deploys all files correctly in a single run
- [ ] `./scripts/qa-all.bash` passes with no errors
- [ ] No personal information, hardcoded usernames, or credentials in any committed file
- [ ] Playbook file has shebang and is executable

---

## Risks & Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| `ghcr.io/matt1398/claude-devtools` image unavailable or renamed | High | Low | Verify image name before writing playbook; note fallback `docker.io` registry in script comments |
| Port 3456 already in use on host | Medium | Low | Document how to override port; add check in `ccdt` script that fails fast with clear error if port is busy |
| claude-devtools requires write access to `CLAUDE_ROOT` (breaks `:ro` mount) | Medium | Low | Test with `:ro` first; if it fails, change to `:z` (SELinux relabelling, read-write) |
| CCY project `.claude/ccy/` has restrictive permissions that block Podman | Low | Low | Podman runs as the user, so user-owned files should be readable |
| Walk-up detection climbs too high (e.g., finds unrelated `.claude/ccy/`) | Low | Low | Only detect `.claude/ccy/`, not just `.claude/`; this is CCY-specific and unlikely to exist by accident |

---

## Timeline

- Phase 1 (Verify approach): 2026-02-17
- Phase 2 (Write ccdt script): 2026-02-17
- Phase 3 (Write bashrc include): 2026-02-17
- Phase 4 (Write Ansible playbook): 2026-02-17
- Phase 5 (Commit): 2026-02-17
- Phase 6 (Test on host): When next on host system
- Target Completion: 2026-02-24

---

## Notes & Updates

### 2026-02-17
- Plan created following research documented in `pre-planning-research.md`
- Key architectural finding: CCY sessions are at `<project-dir>/.claude/ccy/` on the host
  filesystem (confirmed by live container inspection); no container networking needed
- On-demand Podman container selected as deployment approach
- `~/.local/bin/ccdt` selected as script location (user-path, following `wsi`/`nord` pattern)
- All research questions resolved before plan creation
- Plan initially written as 009; corrected to 011 after hook detected the next available number
  is 011 (highest existing plan was 010)
