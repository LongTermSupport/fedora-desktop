# Plan 017: Retire CCB and CCB-Browser — Single CCY Tool

**Status**: Complete
**Created**: 2026-02-21
**Owner**: Claude Code
**Priority**: High

## Overview

CCY (Claude Code YOLO) and CCB (Claude Code Browser) have diverged into two parallel systems that are expensive to maintain. Every fix applied to CCY must be manually ported to CCB, and drift consistently accumulates — the `installMethod` bug fixed today (CCY used `"native"`, CCB used `"npm"`) is a clear example.

The two systems share ~95% of their code and infrastructure. The only meaningful difference is that CCB adds Playwright, all browser binaries, the Playwright MCP server, and chrome-ws on top of the same CCY base. CCB's wrapper (`claude-browser`) is a near-copy of CCY's wrapper (`claude-yolo`) with identical logic.

This plan retires **both** CCB (Docker-based browser container) and `claude-yolo-browser` (distrobox-based Playwright tool), merging agent-browser capability directly into CCY. The result: one Dockerfile, one wrapper (`ccy`), one Ansible deployment section. No aliases, no compatibility shims — clean cut.

## Goals

- Single Dockerfile replacing both `Dockerfile` and `Dockerfile.browser`
- Single wrapper script `ccy` / `claude-yolo` — `ccb` deleted, not aliased
- Single entrypoint `entrypoint.sh` handling all cases
- Single Ansible playbook section (all browser-specific tasks removed)
- Wayland GUI mounts included in the merged `claude-yolo` wrapper
- All documentation updated to describe only CCY
- `claude-yolo-browser` / `ccyb` distrobox tool also removed (maintained experiment, never used)

## Non-Goals

- Keeping a `ccb` command, alias, or symlink — CCB is gone
- Keeping a `ccyb` / `ccy-browser` command — distrobox tool also removed
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

**Why delete claude-yolo-browser too?**
It's a maintained experiment that's never used. Removing it completes the simplification: CCY is the only thing.

**Safe rollback tag**: `stable-2026-02-20` (commit `a6ec2c9`) — last known-good state before today's work.

## Tasks

### Phase 1: Audit Current State ✅

- [x] ✅ **Task 1.1**: Diff `Dockerfile` vs `Dockerfile.browser` — list every addition CCB makes
- [x] ✅ **Task 1.2**: Diff `claude-yolo` vs `claude-browser` — list every CCB-specific block
- [x] ✅ **Task 1.3**: Diff `entrypoint.sh` vs `entrypoint-browser.sh` — list differences
- [x] ✅ **Task 1.4**: List all files under `files/opt/claude-browser/` and `files/var/local/claude-yolo/` that are CCB-only

### Phase 2: Merge Dockerfile ✅

- [x] ✅ **Task 2.1**: Add agent-browser installation to main `Dockerfile` (after Claude Code install)
  - [x] ✅ `npm install -g agent-browser && agent-browser install`
  - [x] ✅ Configure agent-browser: write `/root/.agent-browser/config.json` with Wayland/headless args
- [x] ✅ **Task 2.2**: Add browsing skills copy to main `Dockerfile`
  - [x] ✅ `COPY skills/browsing/ /root/.claude/skills/browsing/`
- [x] ✅ **Task 2.3**: Delete `files/var/local/claude-yolo/Dockerfile.browser`
- [x] ✅ **Task 2.4**: Bump `claude-yolo-version` label in main `Dockerfile` → 2.5

### Phase 3: Merge Wrapper Script ✅

- [x] ✅ **Task 3.1**: Add Wayland GUI mount logic to `claude-yolo`
  - [x] ✅ Detect `WAYLAND_DISPLAY` / `DISPLAY` / headless
  - [x] ✅ Add `-v "$XDG_RUNTIME_DIR:$XDG_RUNTIME_DIR"` and env vars when Wayland present
  - [x] ✅ Add `--device /dev/dri:/dev/dri` for GPU access
- [x] ✅ **Task 3.2**: Delete `files/var/local/claude-yolo/claude-browser`
- [x] ✅ **Task 3.3**: Bump `CCY_VERSION` in `claude-yolo` → 3.6.0, `REQUIRED_CONTAINER_VERSION` → 2.5

### Phase 4: Merge Entrypoint ✅

- [x] ✅ **Task 4.1**: Verify `entrypoint.sh` needs no changes (agent-browser is a CLI, no special init needed)
- [x] ✅ **Task 4.2**: Delete `files/var/local/claude-yolo/entrypoint-browser.sh`

### Phase 5: Update Ansible Playbook ✅

- [x] ✅ **Task 5.1**: Remove `install_browser_mode` variable and all `when: install_browser_mode` tasks from `play-install-claude-yolo.yml`
- [x] ✅ **Task 5.2**: Add skills/browsing copy task to main CCY build context section
- [x] ✅ **Task 5.3**: Update installation summary — remove all CCB references
- [x] ✅ **Task 5.4**: Remove `files/home/bashrc-includes/claude-browser.bash` deploy tasks
- [x] ✅ **Task 5.5**: Add CCB desktop artifact cleanup section to playbook

### Phase 6: Clean Up CCB Source Files ✅

- [x] ✅ **Task 6.1**: Move `files/opt/claude-browser/skills/browsing/` → `files/opt/claude-yolo/skills/browsing/`
- [x] ✅ **Task 6.2**: Delete entire `files/opt/claude-browser/` directory tree
- [x] ✅ **Task 6.3**: Delete `files/home/bashrc-includes/claude-browser.bash`
- [x] ✅ **Task 6.4**: `files/home/bashrc-includes/claude-yolo.bash.j2` — confirmed no CCB references (clean)

### Phase 7: Update Documentation 🔄

All docs must be updated in **both** locations: the repo source files and the copies baked into containers / deployed to hosts.

- [x] ✅ **Task 7.1**: Rewrite `files/opt/claude-yolo/docs/CCY-CCB-GUIDE.txt` — CCY-only guide
- [x] ✅ **Task 7.3**: Rewrite `files/opt/claude-yolo/docs/CUSTOM-DOCKERFILES.txt` — CCY only
- [x] ✅ **Task 7.4**: Rewrite `files/opt/claude-yolo/skills/browsing/SKILL.md` — agent-browser only
- [x] ✅ **Task 7.5**: Rewrite `files/opt/claude-yolo/skills/browsing/COMMANDLINE-USAGE.md` — agent-browser CLI reference
- [x] ✅ **Task 7.6**: Rewrite `files/opt/claude-yolo/skills/browsing/EXAMPLES.md` — agent-browser examples only
- [x] ✅ **Task 7.7**: Update `files/opt/claude-yolo/ccy-startup-info.txt` — remove CCB reference
- [x] ✅ **Task 7.2**: `CLAUDE.md` — confirmed no CCB references (already clean)
- [x] ✅ **Task 7.8**: Clean up remaining CCB text in lib files:
  - [x] ✅ `lib/token-management.bash` — updated, shellcheck fixed
  - [x] ✅ `lib/ssh-handling.bash` — comment updated, SC2207/SC2034/SC2162/SC1091 fixed
  - [x] ✅ `lib/network-management.bash` — comment updated, SC2155/SC2162/SC2034/SC2178 fixed
  - [x] ✅ `lib/ui-helpers.bash` — comment updated
  - [x] ✅ `lib/common.bash` — ccy-browser refs removed, SC2034 fixed
  - [x] ✅ `lib/dockerfile-custom.bash` — all CCB conditionals and prompt text removed
- [x] ✅ **Task 7.9**: Grep sweep + user-facing docs cleanup:
  - [x] ✅ `docs/installation.md` — removed deleted `play-distrobox-playwright.yml` reference
  - [x] ✅ `docs/README.md` — removed Playwright references
  - [x] ✅ `.claude/README.md` — updated deleted file example
  - [x] ✅ `playbooks/imports/optional/common/play-install-distrobox.yml` — removed deleted playbook echo
  - [x] ✅ `playbooks/imports/optional/common/play-install-claude-yolo.yml` — updated DRY comment + distrobox cleanup tasks added
  - [x] ✅ `docs/containerization.md` — CCB section removed, Custom Dockerfiles section rewritten CCY-only
  - [x] ✅ `docs/playbooks.md` — CCY/CCB section rewritten, `play-distrobox-playwright` section deleted

### Phase 8: Remove claude-yolo-browser (Distrobox Tool) ✅

New scope: `claude-yolo-browser` (the `ccyb` / `ccy-browser` distrobox-based Playwright tool) is also being removed. It was a maintained experiment that's never used.

- [x] ✅ **Task 8.1**: Delete `files/var/local/claude-yolo/claude-yolo-browser` wrapper script
- [x] ✅ **Task 8.2**: Delete `files/home/bashrc-includes/claude-yolo-browser.bash` bashrc include
- [x] ✅ **Task 8.3**: Delete `playbooks/imports/optional/common/play-distrobox-playwright.yml`
- [x] ✅ **Task 8.4**: Add cleanup tasks to Ansible (`play-install-claude-yolo.yml`) for deployed distrobox artifacts on target hosts
- [x] ✅ **Task 8.5**: Remaining references found and being cleaned up (see Task 7.9)

### Phase 9: QA and Final Commit ✅

- [x] ✅ **Task 9.1**: Run `./scripts/qa-all.bash` — all checks pass (205 bash + 102 python files OK)
- [x] ✅ **Task 9.2**: Run `./scripts/qa-ctrl-z-patch.bash` — ctrl+z patch applies correctly (Claude Code 2.1.50)
- [x] ✅ **Task 9.3**: Grep sweep confirms clean — only intentional cleanup task names and gitignored session files
- [x] ✅ **Task 9.4**: Committed as `f99c056` — 37 files changed, 1356 insertions, 7205 deletions

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

### Decision 5: Remove claude-yolo-browser distrobox tool
**Context**: `claude-yolo-browser` (`ccyb`) is a Playwright-distrobox-based tool that was built as an experiment. It's never used in practice.
**Decision**: Delete it entirely. CCY with agent-browser covers all real browser automation needs. Complete simplification: one tool.
**Date**: 2026-02-21

## Success Criteria

- [ ] `ccy` launches container with agent-browser available and functional
- [ ] agent-browser can launch Chromium inside the merged container
- [ ] Wayland browser windows visible on host when using Wayland
- [ ] ctrl+z patch applied correctly in merged image
- [ ] No files, scripts, images, or tasks referencing `ccb`, `claude-browser`, `ccyb`, `ccy-browser`, or `claude-yolo-browser` remain
- [ ] `./scripts/qa-all.bash` passes
- [ ] `./scripts/qa-ctrl-z-patch.bash` passes
- [ ] Ansible playbook deploys cleanly

## Risks & Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Merged image too large | Med | Low | agent-browser installs Chromium only, not Firefox/WebKit |
| agent-browser Wayland config broken | Med | Med | Config is copied verbatim from CCB; test on host before committing |
| ctrl+z patch incompatible with new npm packages | Low | Low | `qa-ctrl-z-patch.bash` catches this |
| Missed CCB reference in docs/scripts | Low | Med | Phase 9.3 grep sweep catches stragglers |

## Notes & Updates

### 2026-02-21
- Plan created after `installMethod` drift bug exposed cost of maintaining two systems
- Safe rollback tag `stable-2026-02-20` at commit `a6ec2c9`
- Today's commits already in: docs staging fix (`4203d7a`), installMethod fix (`f24b4a6`)
- Decision to delete CCB entirely (not alias) made for simplicity
- Scope expanded: `claude-yolo-browser` (distrobox Playwright tool) also removed — never used, clean cut
- Phases 1–7 (partial) complete: Dockerfile, wrapper, entrypoint, Ansible, file cleanup, skills docs all done
- Remaining: lib file CCB text, CLAUDE.md check, distrobox removal, QA
- All phases complete. Final commit: `f99c056`
- Net result: 37 files changed, 7205 lines deleted, CCY is the single tool

Refs: CLAUDE/Plan/017-merge-ccy-ccb/PLAN.md
