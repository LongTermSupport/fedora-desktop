# Plan 00184: one menu for the deployed user tools

**Status**: Not Started
**Created**: 2026-10-13
**Owner**: joseph
**Priority**: Low

Addresses #81.

## Overview

The plays deploy a growing set of user tools to `~/.local/bin` (`wsi-model-manager`,
`vmtest`, `reclaim` and others), and a person has to remember each command. #81 asks for
one front door. A small launcher deployed by a play (working name `fedora-desktop-tools`)
shows a numbered menu of the tools on this machine, each with one line of description, and
runs the one picked. Each play adds its own tool when it installs it, so the menu lists only
what is installed and there is no second list to fall out of step. The panel menu gets a
"Tools…" item next to "Re-run a play…" (`extensions/fedora-desktop@fedora-desktop/sections/plays.js`)
that opens it in a terminal. `run.bash` only points to it at the end of a run. Every listed
tool follows `CLAUDE/InteractiveScripts.md`, has a `--yes` or scripted mode, and gives
`--json` where useful.

The issue says "after the plans in progress are done", so this plan is filed Not Started at
low priority. Plan 00147 (the tmux F12 "Fedora Desktop" menu) builds a menu from
one-file-per-item drop-ins that a play adds with no shared-file edit. That is the same
pattern this menu needs, so the drop-in format is shared, not invented twice.

## Goals

- A launcher listing exactly the tools installed here, from per-play drop-ins.
- A "Tools…" item in the Fedora Desktop panel menu that opens it.
- One line at the end of `run.bash` pointing to it.

## Non-Goals

- Running tools from `meta-deploy.bash` (#81 excludes it).
- A tools menu inside `run.bash`.
- Rewriting existing tools beyond what the shared rules require for listing.

## Tasks

### Phase 1: Inventory and design

- [ ] ⬜ **Task 1.1**: Inventory the user-facing tools in `files/home/.local/bin` and which
  play deploys each; note which lack a scripted mode or `--json`.
- [ ] ⬜ **Task 1.2**: Agree the drop-in format with Plan 00147's (one shared format or a
  stated reason for two). Owner decision on the launcher's name.

### Phase 2: Build

- [ ] ⬜ **Task 2.1**: Tests, then the launcher (InteractiveScripts rules, bounded re-prompt,
  `--list --json`).
- [ ] ⬜ **Task 2.2**: Each play deploying a listed tool installs its drop-in.
- [ ] ⬜ **Task 2.3**: The panel item and the `run.bash` pointer; extension version bump;
  ESLint.
- [ ] ⬜ **Task 2.4**: `deploy.bash`, `acceptance.bash`, an entry in `meta-deploy.bash`;
  docs; `qa-all.bash`; the `qa-reviewer` agent.

## Success Criteria

- [ ] The launcher lists exactly the tools installed on this machine.
- [ ] Removing a play's tool removes its menu entry with no other edit.
- [ ] The panel item opens the launcher in a terminal.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00184-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed from #81.
