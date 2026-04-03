# Plan 026: Repository Spring Cleaning (Non-CCY)

**Status**: 🔄 In Progress
**Created**: 2026-04-03
**Type**: Refactoring / Cleanup
**Priority**: Medium
**Related**: Plan 025 (CCY spring cleaning — separate, in-progress)

## Overview

The full repository (excluding CCY files covered by Plan 025) has accumulated technical debt across bash scripts, Ansible playbooks, and repo structure. A comprehensive audit identified 18 bash script issues, 20 Ansible playbook issues, and several repo structure problems including 2.4MB of tracked backup files.

The most notable findings are: 7 playbooks with duplicate shebangs, 3 playbooks piping curl to bash, 2 status scripts missing `set -e`, and tracked backup directories that should have been gitignored.

This plan addresses all findings systematically, ordered by impact and risk. Each phase is self-contained and independently committable.

## Goals

- Remove tracked backup files and orphaned documentation from git
- Fix all high-severity bash script issues (missing `set -e`, dead code)
- Fix all high-severity Ansible issues (duplicate shebangs, curl-to-bash, `state: latest`)
- ~~Address personal information exposure in `localhost.yml`~~ — false alarm, file not tracked
- Fix medium-severity issues where practical
- Maintain identical runtime behaviour throughout

## Non-Goals

- CCY files (covered by Plan 025)
- Adding new features or changing UX
- Rewriting playbooks from scratch
- Fixing low-severity cosmetic issues unless in files already being edited
- Modifying `entrypoint.sh` patterns (different execution context)

## Context & Background

The repository consists of:

| Area | Files | Role |
|------|-------|------|
| Bash scripts (non-CCY) | ~15 | QA scripts, status checks, profile tweaks |
| Ansible playbooks | ~50 | System configuration and deployment |
| Environment/vars | ~5 | Inventory, host vars, version vars |
| Repo structure | 278 tracked | Project files, docs, plans |

Key finding: The QA scripts (`qa-*.bash`, `run.bash`) are clean — all shellcheck passes. Issues are concentrated in status scripts (`nvidia-status.bash`, `check-displaylink-status.sh`), profile scripts (`zz_lts-fedora-desktop.bash`), and several playbooks.

## Tasks

### Phase 1: Repo Structure Cleanup

Lowest risk — removing tracked backups and reorganising orphaned files.

- [x] ~~**1.1: Remove tracked backup directories from git**~~ — FALSE ALARM
  - Backup files exist on disk but are already gitignored by `.claude/*.bak` rule
  - They were never tracked in git. Research agent incorrectly assumed they were committed.

- [x] ✅ **1.2: Remove orphaned documentation**
  - [x] ✅ Deleted `CCY-EXTRACTION-PLAN.md` from repo root (never executed, outdated)

- [x] ✅ **1.3: Fix plan directory organisation**
  - [x] ✅ Verified Plan 012 is at `012-fix-plugin-handlers/` and correctly listed as "Cancelled" in README — no fix needed

- [x] ✅ **1.4: Verify and commit Phase 1**
  - [x] ✅ QA passed
  - [x] ✅ Committed

### ~~Phase 2: Security — Personal Information Review~~ — CANCELLED

`localhost.yml` is NOT tracked in git — false alarm from the research agent. No action needed.

### Phase 3: Bash Script Fixes

Fix issues in non-CCY bash scripts. All changes are to deployed files — require Ansible deployment on host.

- [ ] ⬜ **3.1: Add `set -e` to status scripts**
  - [ ] ⬜ `scripts/nvidia-status.bash` — add `set -e` after shebang
  - [ ] ⬜ `scripts/check-displaylink-status.sh` — add `set -e` after shebang
  - [ ] ⬜ Verify scripts still work with fail-fast (some status checks may need `if/else` refactoring)

- [x] ✅ **3.2: Remove dead code from status scripts**
  - [x] ✅ `nvidia-status.bash` — removed unused `PACKAGES_OK` and `VAAPI_OK`
  - [x] ✅ `check-displaylink-status.sh` — removed unused `DKMS_OK` (3 locations)

- [ ] ⬜ **3.3: Fix shellcheck warnings in status scripts**
  - [x] ✅ `nvidia-status.bash:192-194` — quoted variables in nvidia-smi output (SC2086)
  - [x] ✅ `nvidia-status.bash:186` — replaced indirect `$?` check with direct `if cmd` (SC2181)
  - [ ] ⬜ `check-displaylink-status.sh:83` — fix glob pattern with `-d` test (SC2144)
  - [ ] ⬜ `check-displaylink-status.sh:84,154,161` — replace `ls` with `find` (SC2012)

- [ ] ⬜ **3.4: Fix shellcheck warnings in profile script**
  - [ ] ⬜ `zz_lts-fedora-desktop.bash:98` — fix nested quoting in docker args (SC2027)
  - [ ] ⬜ `zz_lts-fedora-desktop.bash:100` — quote command substitution (SC2086)
  - [ ] ⬜ `zz_lts-fedora-desktop.bash:78-91` — fix function argument passing (SC2120)

- [x] ✅ **3.5: Clean up dead code in profile script**
  - [x] ✅ `zz_lts-fedora-desktop.bash:44-55` — removed commented-out 12-line prompt block

- [ ] ⬜ **3.6: Fix `ls` usage in GNOME script**
  - [ ] ⬜ `extensions/scripts/gnome-shell-extract-js.bash:95` — replace `ls` with glob/find

- [ ] ⬜ **3.7: Verify and commit Phase 3**
  - [ ] ⬜ Run `./scripts/qa-all.bash`
  - [ ] ⬜ Run `shellcheck -x` on all modified scripts — confirm zero warnings
  - [ ] ⬜ Commit: "fix(scripts): add set -e, remove dead code, fix shellcheck warnings"

### Phase 4: Ansible — Duplicate Shebangs

Mechanical fix across 7 playbooks. Low risk.

- [x] ✅ **4.1: Remove duplicate shebangs**
  - [x] ✅ `playbooks/imports/play-claude-yolo.yml`
  - [x] ✅ `playbooks/imports/play-podman.yml`
  - [x] ✅ `playbooks/imports/play-python.yml`
  - [x] ✅ `playbooks/imports/play-vscode.yml`
  - [x] ✅ `playbooks/imports/play-gnome-shell.yml`
  - [x] ✅ `playbooks/imports/play-systemd-user-tweaks.yml`
  - [x] ✅ `playbooks/imports/play-github-cli-multi.yml`

- [x] ✅ **4.2: Verify and commit Phase 4**
  - [x] ✅ QA passed
  - [x] ✅ Committed (bundled with Phase 1 and 3 zero-risk fixes)

### Phase 5: Ansible — Curl-to-Bash and State Latest

Higher-risk fixes — changes to installation behaviour.

- [ ] ⬜ **5.1: Fix curl-piped-to-bash installations**
  - [ ] ⬜ `play-nvm-install.yml:24` — download script with `get_url`, then execute
  - [ ] ⬜ `play-python.yml:62` — download pyenv-installer with `get_url`, then execute
  - [ ] ⬜ `play-rust-dev.yml:97` — download cargo-binstall installer with `get_url`, then execute

- [ ] ⬜ **5.2: Fix `state: latest` to `state: present`**
  - [ ] ⬜ `play-python.yml:51` — pipx pdm
  - [ ] ⬜ `play-python.yml:58` — pipx huggingface_hub
  - [ ] ⬜ `play-nvidia.yml:29` — dnf packages
  - [ ] ⬜ `play-cloudflare-warp.yml:21` — dnf packages

- [ ] ⬜ **5.3: Verify and commit Phase 5**
  - [ ] ⬜ Run `./scripts/qa-all.bash`
  - [ ] ⬜ Commit: "fix(ansible): replace curl-pipe-bash with get_url, fix state:latest"

### Phase 6: Ansible — Medium-Severity Fixes

- [ ] ⬜ **6.1: Add missing file permissions to copy/blockinfile tasks**
  - [ ] ⬜ Audit all `blockinfile` and `copy` tasks for missing `owner:`, `group:`, `mode:`
  - [ ] ⬜ Add explicit permissions where missing

- [ ] ⬜ **6.2: Fix non-idempotent shell commands**
  - [ ] ⬜ `play-basic-configs.yml:219` — add idempotency guard to `grub2-editenv`
  - [ ] ⬜ `play-git-configure-and-tools.yml:39` — add `creates:` to git clone
  - [ ] ⬜ `play-vscode.yml:12-21` — add idempotency checks to repo/install shell

- [ ] ⬜ **6.3: Fix incorrect `changed_when` usage**
  - [ ] ⬜ `play-basic-configs.yml:220,240` — properly detect actual changes

- [ ] ⬜ **6.4: Fix socket permissions**
  - [ ] ⬜ `play-speech-to-text.yml:67` — change `socket-perm=0666` to `0660`

- [ ] ⬜ **6.5: Verify and commit Phase 6**
  - [ ] ⬜ Run `./scripts/qa-all.bash`
  - [ ] ⬜ Commit: "fix(ansible): add missing permissions, fix idempotency, fix changed_when"

## Dependencies

- Plan 025 (CCY spring cleaning) — independent, can run in parallel
- No other dependencies

## Technical Decisions

### Decision 1: Phase ordering — structure cleanup first
**Context**: Repo structure cleanup (removing backups) is zero-risk and makes the repo cleaner for all subsequent work.
**Decision**: Phase 1 handles structure, then security, then code fixes.

### Decision 2: Security finding was false alarm
**Context**: Research agent flagged `localhost.yml` as containing personal info in a public repo. Investigation confirmed the file is NOT tracked in git — it exists only locally. No action needed.
**Decision**: Phase 2 cancelled.

### Decision 3: Status scripts need careful `set -e` addition
**Context**: Adding `set -e` to status scripts that check hardware state could cause premature exits if hardware isn't present (e.g., `nvidia-smi` failing on non-NVIDIA systems).
**Decision**: Phase 3 may need to wrap some commands in `if/else` to maintain correct behaviour with `set -e`. Each script needs testing on the target system.

## Success Criteria

- [x] ~~Zero tracked backup files in `.claude/`~~ — were never tracked
- [ ] No orphaned documentation in repo root
- [ ] All bash scripts pass shellcheck (excluding SC1090/SC1091 source-following)
- [ ] All bash scripts have `set -e`
- [ ] Zero duplicate shebangs in playbooks
- [ ] No curl-piped-to-bash patterns in playbooks
- [ ] No `state: latest` in package installation tasks
- [x] ~~Security decision documented for `localhost.yml`~~ — not tracked in git, no action needed
- [ ] `./scripts/qa-all.bash` passes

## Risks & Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| `set -e` breaks status scripts on systems without hardware | Medium | Medium | Wrap hardware checks in `if/else`; test on target system |
| Removing curl-to-bash breaks installer scripts | Medium | Low | Download-then-execute pattern is equivalent; test installations |
| `state: present` leaves outdated packages | Low | Low | Users can manually update; `dnf upgrade` handles this |
| ~~Removing backup files loses useful reference~~ | N/A | N/A | FALSE ALARM — files were never tracked |
| ~~BFG history rewrite for localhost.yml~~ | ~~High~~ | N/A | CANCELLED — file not tracked in git |

## Notes & Updates

### 2026-04-03
- Plan created based on comprehensive audit by three research agents
- Bash scripts: 18 findings (2 High, 10 Medium, 6 Low) across 4 files
- Ansible: 19 findings (3 High, 5 Medium, 4 Low, 7 Informational) — H4 (personal info) was false alarm
- Repo structure: 5 backup items to remove, 1 orphaned doc, 1 plan location issue
- Supporting analysis: [bash-audit.md](bash-audit.md), [ansible-audit.md](ansible-audit.md), [repo-structure-audit.md](repo-structure-audit.md)
