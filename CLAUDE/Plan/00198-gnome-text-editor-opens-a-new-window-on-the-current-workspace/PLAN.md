# Plan 00198: gnome text editor opens a new window on the current workspace

**Status**: In Progress (triage queued in meta-deploy; waiting on its report)
**Created**: 2026-10-15
**Owner**: joseph
**Priority**: Medium

## Overview

The owner works across many GNOME workspaces. Opening a text file from Nautilus (a
double-click) or another app does not start a new GNOME Text Editor window: the file opens as a
tab in the editor's existing window, and GNOME Shell switches to the workspace that window is on.
The owner is pulled away from the workspace they were working in.

The cause is D-Bus activation. The editor's desktop file sets `DBusActivatable=true`, so
Nautilus never runs its `Exec=` line: it asks the running instance, over D-Bus, to open the
file, and the instance adds a tab to its last window. The editor itself has no "always open a
new window" preference.

The likely fix is a per-user override of `org.gnome.TextEditor.desktop` under
`~/.local/share/applications/` with D-Bus activation off and an `Exec=` that asks for a new
window (`--new-window`, if this version has it, or `--standalone`), deployed by a play. The
triage establishes the facts first: the RPM or the Flatpak, the flags this version has, and the
desktop files in play.

## Goals

- Opening a text file from Nautilus or any app opens it in a new editor window on the current
  workspace; the owner is never moved to another workspace.
- The change is IaC: a play installs it, and it survives editor upgrades.

## Non-Goals

- Changing the editor, or how GNOME Shell places windows in general.
- Other editors or file types beyond those GNOME Text Editor handles.

## Tasks

### Phase 1: Facts

- [ ] 🔄 **Task 1.1**: `triage.bash` on the host (queued in `meta-deploy.list`): RPM or Flatpak,
  `--help-all`, every `org.gnome.TextEditor.desktop` with its `Exec` and `DBusActivatable`, the
  default handler for `text/plain` and `text/markdown`, the editor's GSettings and mutter's
  workspace settings.

### Phase 2: The fix

- [ ] ⬜ **Task 2.1**: From the triage: choose the flag (`--new-window` or `--standalone`) and
  the override's path (RPM or Flatpak export). Add the task to the play that owns desktop
  settings, generating the override from the system desktop file so an upgrade's other changes
  are kept, and refresh the desktop database.
- [ ] ⬜ **Task 2.2**: Host run through `meta-deploy.bash`; the owner opens files from Nautilus on
  two workspaces and confirms each opens where they are. qa-reviewer over the plan.

## Success Criteria

- [ ] A double-click in Nautilus opens the file in a new editor window on the current workspace.
- [ ] The override is deployed by a play and is idempotent.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00198-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- <!-- milestone or delivery commit hash -->
