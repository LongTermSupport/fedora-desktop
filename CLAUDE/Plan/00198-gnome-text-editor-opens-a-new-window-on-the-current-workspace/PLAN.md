# Plan 00198: gnome text editor opens a new window on the current workspace

**Status**: In Progress (fix implemented in play-gsettings.yml; deploy and the owner's workspace check queued in meta-deploy)
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

- [x] ✅ **Task 1.1**: `triage.bash` on the host. Facts established:
  - GNOME Text Editor is the RPM (`gnome-text-editor` 50.1, Fedora 44); the Flatpak is not
    installed, so the override path is `~/.local/share/applications/`.
  - `--help-all` has `-n, --new-window` (open the provided files in a new window), `-s,
    --standalone` (a new instance, implies `--ignore-session`) and `-i, --ignore-session`.
  - The one desktop file is `/usr/share/applications/org.gnome.TextEditor.desktop`:
    `DBusActivatable=true`, `Exec=gnome-text-editor %U`, `MimeType=text/plain;application/x-zerosize;`,
    and a `new-window` action whose Exec is `gnome-text-editor --new-window`.
  - `text/plain` and `text/markdown` both default to `org.gnome.TextEditor.desktop`.
  - The editor's GSettings have no window-placement or open-in-new-window key
    (`restore-session` is already false). `org.gnome.mutter` has no workspace-switch-on-activate
    key (`dynamic-workspaces` false, `workspaces-only-on-primary` false). The only related
    setting is `org.gnome.desktop.wm.preferences focus-new-windows` = `smart`, which concerns
    focus of new windows, not which window receives a file.
  - So no setting can do this; the workspace switch is the shell activating the existing
    window the D-Bus call targeted.

### Phase 2: The fix

- [x] ✅ **Task 2.1**: Decision: `--new-window` (it keeps the running instance, so no second
  process per file, unlike `--standalone`). `playbooks/imports/play-gsettings.yml` slurps the
  system desktop file, asserts it still has the two lines to change, writes the per-user copy
  with `DBusActivatable=false` and `Exec=gnome-text-editor --new-window %U` on every run (so
  upgrades are kept), and refreshes the desktop database when it changed. `acceptance.bash`
  checks the file and the `text/plain` default.
- [ ] 🔄 **Task 2.2**: Host run through `meta-deploy.bash`; the owner opens files from Nautilus on
  two workspaces and confirms each opens where they are. qa-reviewer over the plan.

## Success Criteria

- [ ] A double-click in Nautilus opens the file in a new editor window on the current workspace.
- [ ] The override is deployed by a play and is idempotent.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00198-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- <!-- milestone or delivery commit hash -->
