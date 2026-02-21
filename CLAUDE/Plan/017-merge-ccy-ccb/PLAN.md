# Plan 017: Retire CCB — Merge Browser Capability into CCY

**Status**: In Progress
**Created**: 2026-02-21
**Owner**: Claude Code
**Priority**: High

## Overview

CCY (Claude Code YOLO) and CCB (Claude Code Browser) have diverged into two parallel systems that are expensive to maintain. Every fix applied to CCY must be manually ported to CCB, and drift consistently accumulates — the `installMethod` bug fixed today (CCY used `"native"`, CCB used `"npm"`) is a clear example.

The two systems share ~95% of their code and infrastructure. The only meaningful difference is that CCB adds Playwright, all browser binaries, the Playwright MCP server, and chrome-ws on top of the same CCY base. CCB's wrapper (`claude-browser`) is a near-copy of CCY's wrapper (`claude-yolo`) with identical logic.

This plan retires CCB entirely and merges its browser capability into CCY: one Dockerfile, one wrapper (`ccy`), one entrypoint, one Ansible deployment section. The merged image includes agent-browser (Chromium only). Playwright's full multi-browser suite, the Playwright MCP server, and chrome-ws are all removed. CCB as a concept — the command, the image, the files — is deleted.

## Goals

- Single Dockerfile replacing both `Dockerfile` and `Dockerfile.browser`
- Single wrapper script `ccy` / `claude-yolo` — `ccb` deleted, not aliased
- Single entrypoint `entrypoint.sh` handling all cases
- Single Ansible playbook section (all browser-specific tasks removed)
- Wayland GUI mounts included in the merged `claude-yolo` wrapper
- All documentation updated to describe only CCY

## Non-Goals

- Keeping a `ccb` command, alias, or symlink — CCB is gone
- Keeping Playwright framework or its full browser suite
- Keeping the Playwright MCP server (`@playwright/mcp@latest`) — context-window bloat
- Keeping chrome-ws — agent-browser covers the use case at a higher level
- Adding new browser automation features beyond agent-browser

## Context & Background

**Why agent-browser and not Playwright MCP?**
The Playwright MCP server exposes browser state via MCP protocol, consuming significant context window on every tool call. agent-browser is a standalone CLI that Claude calls as a shell command — zero MCP overhead, token-efficient interface.

**Why remove chrome-ws?**
chrome-ws is a raw Chrome DevTools Protocol (CDP) client requiring a browser already running with `--remote-debugging-port`. agent-browser manages the full browser lifecycle. No use case requires raw CDP that agent-browser cannot satisfy. YAGNI.

**Why delete CCB entirely rather than alias it?**
Keeping `ccb` as an alias perpetuates the mental model of two systems. The point is to simplify to one thing. Users adapt; clean cuts are better than compatibility shims.

**Safe rollback tag**: `stable-2026-02-20` (commit `a6ec2c9`) — last known-good state before today's work.

## Tasks

### Phase 1: Audit Current State

- [ ] ⬜ **Task 1.1**: Diff `Dockerfile` vs `Dockerfile.browser` — list every addition CCB makes
- [ ] ⬜ **Task 1.2**: Diff `claude-yolo` vs `claude-browser` — list every CCB-specific block
- [ ] ⬜ **Task 1.3**: Diff `entrypoint.sh` vs `entrypoint-browser.sh` — list differences
- [ ] ⬜ **Task 1.4**: List all files under `files/opt/claude-browser/` and `files/var/local/claude-yolo/` that are CCB-only

### Phase 2: Merge Dockerfile

- [ ] ⬜ **Task 2.1**: Add agent-browser installation to main `Dockerfile` (after Claude Code install)
  - [ ] ⬜ `npm install -g agent-browser && agent-browser install`
  - [ ] ⬜ Configure agent-browser: write `/root/.agent-browser/config.json` with Wayland/headless args
- [ ] ⬜ **Task 2.2**: Add browsing skills copy to main `Dockerfile`
  - [ ] ⬜ `COPY skills/browsing/ /root/.claude/skills/browsing/`
- [ ] ⬜ **Task 2.3**: Delete `files/var/local/claude-yolo/Dockerfile.browser`
- [ ] ⬜ **Task 2.4**: Bump `claude-yolo-version` label in main `Dockerfile`

### Phase 3: Merge Wrapper Script

- [ ] ⬜ **Task 3.1**: Add Wayland GUI mount logic to `claude-yolo`
  - [ ] ⬜ Detect `WAYLAND_DISPLAY` / `DISPLAY` / headless
  - [ ] ⬜ Add `-v "$XDG_RUNTIME_DIR:$XDG_RUNTIME_DIR"` and env vars when Wayland present
  - [ ] ⬜ Add `--device /dev/dri:/dev/dri` for GPU access
- [ ] ⬜ **Task 3.2**: Delete `files/var/local/claude-yolo/claude-browser`
- [ ] ⬜ **Task 3.3**: Bump `CCY_VERSION` in `claude-yolo`

### Phase 4: Merge Entrypoint

- [ ] ⬜ **Task 4.1**: Verify `entrypoint.sh` needs no changes (agent-browser is a CLI, no special init needed)
- [ ] ⬜ **Task 4.2**: Delete `files/var/local/claude-yolo/entrypoint-browser.sh`

### Phase 5: Update Ansible Playbook

- [ ] ⬜ **Task 5.1**: Remove `install_browser_mode` variable and all `when: install_browser_mode` tasks from `play-install-claude-yolo.yml`
  - [ ] ⬜ Remove: Create Claude Browser Base Directory
  - [ ] ⬜ Remove: Copy Dockerfile for Browser Container
  - [ ] ⬜ Remove: Copy Browser Entrypoint Script
  - [ ] ⬜ Remove: Create/copy chrome-ws directory and files
  - [ ] ⬜ Remove: Create Skills Directory (browser)
  - [ ] ⬜ Remove: Copy chrome-ws Skills Documentation
  - [ ] ⬜ Remove: Create Browser Documentation Directory
  - [ ] ⬜ Remove: Copy Documentation into Browser Docker Build Context
  - [ ] ⬜ Remove: Copy Browser Startup Info File
  - [ ] ⬜ Remove: Copy Browser Wrapper Script
  - [ ] ⬜ Remove: Deploy Bashrc for ccb Alias (root + user)
  - [ ] ⬜ Remove: Create CCB Root/Projects Directories
  - [ ] ⬜ Remove: Calculate Browser Dockerfile Hash
  - [ ] ⬜ Remove: Build Claude Browser Container Image with Hash
  - [ ] ⬜ Remove: Verify Browser Container Image
- [ ] ⬜ **Task 5.2**: Add skills/browsing copy task to main CCY build context section
- [ ] ⬜ **Task 5.3**: Update installation summary — remove all CCB references
- [ ] ⬜ **Task 5.4**: Remove `files/home/bashrc-includes/claude-browser.bash` deploy tasks

### Phase 6: Clean Up Files

- [ ] ⬜ **Task 6.1**: Move `files/opt/claude-browser/skills/browsing/` → `files/opt/claude-yolo/skills/browsing/`
- [ ] ⬜ **Task 6.2**: Delete entire `files/opt/claude-browser/` directory tree
- [ ] ⬜ **Task 6.3**: Delete `files/home/bashrc-includes/claude-browser.bash`
- [ ] ⬜ **Task 6.4**: Update `files/home/bashrc-includes/claude-yolo.bash.j2` — remove any ccb references

### Phase 7: Update Documentation

- [ ] ⬜ **Task 7.1**: Rename/rewrite `files/opt/claude-yolo/docs/CCY-CCB-GUIDE.txt` as CCY-only guide
- [ ] ⬜ **Task 7.2**: Update `CLAUDE.md` — remove all CCB sections
- [ ] ⬜ **Task 7.3**: Update `files/opt/claude-yolo/docs/CUSTOM-DOCKERFILES.txt` — CCY only
- [ ] ⬜ **Task 7.4**: Search for any remaining `ccb` / `claude-browser` references in docs and remove

### Phase 8: QA and Final Commit

- [ ] ⬜ **Task 8.1**: Run `./scripts/qa-all.bash` — all checks pass
- [ ] ⬜ **Task 8.2**: Run `./scripts/qa-ctrl-z-patch.bash` — ctrl+z patch still applies
- [ ] ⬜ **Task 8.3**: Grep entire repo for `ccb`, `claude-browser`, `claude-browser:latest` — confirm clean
- [ ] ⬜ **Task 8.4**: Commit all changes with reference to this plan

## Technical Decisions

### Decision 1: Remove Playwright MCP, Keep agent-browser
**Context**: CCB included `@playwright/mcp@latest` as an MCP server. MCP servers consume context window on every browser tool call. agent-browser is a CLI — no MCP overhead.
**Decision**: Remove Playwright MCP entirely. Keep `npm install -g agent-browser && agent-browser install` for Chromium browser automation via CLI.
**Date**: 2026-02-21

### Decision 2: Remove chrome-ws
**Context**: chrome-ws is a raw CDP client. agent-browser provides higher-level control and manages its own browser lifecycle.
**Decision**: Remove chrome-ws. YAGNI.
**Date**: 2026-02-21

### Decision 3: Delete CCB entirely — no alias, no symlink
**Context**: Keeping `ccb` as a compatibility alias perpetuates the two-system mental model and adds maintenance surface.
**Decision**: CCB is deleted outright. Only `ccy` / `claude-yolo` remains. Clean cut.
**Date**: 2026-02-21

### Decision 4: Wayland mounts conditional in merged wrapper
**Context**: CCB conditionally added Wayland mounts when `WAYLAND_DISPLAY` was set. Same logic moves into `claude-yolo`.
**Decision**: Add CCB's Wayland detection block to `claude-yolo`. Falls back gracefully when no display is available (headless/SSH sessions).
**Date**: 2026-02-21

## Success Criteria

- [ ] `ccy` launches container with agent-browser available and functional
- [ ] agent-browser can launch Chromium inside the merged container
- [ ] Wayland browser windows visible on host when using Wayland
- [ ] ctrl+z patch applied correctly in merged image
- [ ] No files, scripts, images, or tasks referencing `ccb` or `claude-browser` remain
- [ ] `./scripts/qa-all.bash` passes
- [ ] `./scripts/qa-ctrl-z-patch.bash` passes
- [ ] Ansible playbook deploys cleanly

## Risks & Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Merged image too large | Med | Low | agent-browser installs Chromium only, not Firefox/WebKit |
| agent-browser Wayland config broken | Med | Med | Config is copied verbatim from CCB; test on host before committing |
| ctrl+z patch incompatible with new npm packages | Low | Low | `qa-ctrl-z-patch.bash` catches this |
| Missed CCB reference in docs/scripts | Low | Med | Phase 8.3 grep sweep catches stragglers |

## Notes & Updates

### 2026-02-21
- Plan created after `installMethod` drift bug exposed cost of maintaining two systems
- Safe rollback tag `stable-2026-02-20` at commit `a6ec2c9`
- Today's commits already in: docs staging fix (`4203d7a`), installMethod fix (`f24b4a6`)
- Decision to delete CCB entirely (not alias) made for simplicity

Refs: CLAUDE/Plan/017-merge-ccy-ccb/PLAN.md
