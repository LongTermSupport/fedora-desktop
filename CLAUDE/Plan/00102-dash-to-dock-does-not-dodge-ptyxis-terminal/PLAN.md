# Plan 00102: Dash to Dock does not dodge the Ptyxis terminal

**Status**: In Progress
**Created**: 2026-09-03
**Owner**: joseph
**Priority**: Medium

## Overview

An un-maximised terminal window overlapping the dock does not make the dock
hide. Every other application window does. Because a terminal prompt lives at
the bottom of its window, the dock sits on top of exactly the line being typed.

This desktop is Fedora 44, where the default GNOME terminal is **Ptyxis**, not
`gnome-terminal` (the repo already configures Ptyxis in `play-gsettings.yml`).
The dock is **Dash to Dock**, installed from the Fedora RPM by
`play-gnome-shell-extensions.yml` with **no** settings written, so it runs on its
defaults: intellihide on, `intellihide-mode = FOCUS_APPLICATION_WINDOWS`.

In that mode Dash to Dock only tests for overlap against the windows of the
application GNOME Shell's window tracker reports as focused. Any window the
tracker cannot associate with the focused application is simply not checked,
and the dock stays visible. Ptyxis is a known way to hit that gap: it is a
single-instance, D-Bus-activated app whose launcher process exits immediately,
so the tracker falls back to matching the Wayland app-id against a desktop
file, and a mismatch there leaves the window unassociated. The plan first
grounds that hypothesis with triage on the host, then fixes it in Ansible.

## Goals

- Establish, from host facts, why Dash to Dock's intellihide ignores Ptyxis.
- Configure the dock through Ansible so a terminal overlapping it hides it, the
  same as any other window.
- Keep the fix idempotent and re-runnable, in the play that owns the dock.

## Non-Goals

- Replacing Ptyxis with another terminal.
- Patching, forking or pinning Dash to Dock.
- Tuning any other dock behaviour (position, size, animation, autohide-on-edge).

## Context & Background

- Ptyxis is configured by [play-gsettings.yml](../../../playbooks/imports/play-gsettings.yml)
  via `community.general.dconf`.
- Dash to Dock is installed by [play-gnome-shell-extensions.yml](../../../playbooks/imports/play-gnome-shell-extensions.yml),
  which already holds one extension setting (Space Bar) and is therefore the
  play that owns dock settings too. The RPM installs its schema system-wide, so
  `community.general.dconf` works without the `dbus-run-session -- dconf write`
  detour Space Bar needs.
- Dash to Dock `intellihide-mode` values: `ALL_WINDOWS`,
  `FOCUS_APPLICATION_WINDOWS` (default), `MAXIMIZED_WINDOWS`, `ALWAYS_ON_TOP`.

### Facts

| ID  | Fact                                                                           | Source                       |
| --- | ------------------------------------------------------------------------------ | ---------------------------- |
| F1  | Repo writes no Dash to Dock dconf keys, so the extension runs on its defaults. | `rg dash-to-dock playbooks/` |
| F2  | The repo's terminal is Ptyxis (`/org/gnome/Ptyxis/tab-middle-click` is set).   | `play-gsettings.yml`         |
| F3  | Other, non-maximised windows do hide the dock.                                 | User report                  |

### Hypotheses

| ID  | Hypothesis                                                                                                                             | Confirmed by                                                                                                                           | Refuted by                                                                      |
| --- | -------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| H1  | Shell's window tracker does not associate the Ptyxis window with a focused app, so `FOCUS_APPLICATION_WINDOWS` finds nothing to dodge. | Introspect shows the focused Ptyxis window with an app-id / wm-class that matches no installed `.desktop` file, or a Flatpak/RPM split | app-id matches `org.gnome.Ptyxis.desktop` exactly and the RPM is the only build |
| H2  | `intellihide-mode` is not the default (for example `MAXIMIZED_WINDOWS`).                                                               | dconf read returns a non-default value                                                                                                 | F3 already makes this unlikely; dconf read confirms                             |
| H3  | Intellihide is disabled or the dock is fixed, and other windows "hide" it via a different path.                                        | `intellihide=false` or `dock-fixed=true`                                                                                               | both at default                                                                 |

### Unverified premises

- The terminal in question is Ptyxis, not a second `gnome-terminal` install.
  Triage reports which terminal packages exist and which one is the default.
- The Fedora RPM ships Dash to Dock with the upstream default `intellihide-mode`.

## Tasks

### Phase 1: Triage on the host

- [x] ✅ **Task 1.1**: Run `triage.bash` on the host with a Ptyxis window focused
  and read the report under `triage-runs/`.
  - [x] ✅ Record which of H1–H3 the facts support, in the journal. H2 and H3
    refuted (dock on untouched defaults). H1 stands: GNOME 50 denies the
    window-introspection call, so it cannot be confirmed directly, but the fix
    does not depend on it.
  - [x] ✅ Settle the two unverified premises. Ptyxis RPM is the only terminal;
    the RPM ships the upstream default `intellihide-mode`.

### Phase 2: Fix in Ansible

- [x] ✅ **Task 2.1**: Add a `community.general.dconf` task to
  `play-gnome-shell-extensions.yml`, beside the Space Bar setting, that writes
  `/org/gnome/shell/extensions/dash-to-dock/intellihide-mode` = `'ALL_WINDOWS'`.
  This checks every window on the workspace for overlap and does not depend on
  the tracker's app association at all, so it fixes H1 and H2 alike.
- [x] ❌ **Task 2.2** (cancelled, H3 refuted by triage): If triage refutes H1 and H2 and instead shows H3, write
  the two keys that restore intellihide (`dock-fixed=false`,
  `intellihide=true`) in the same task block instead.
- [x] ✅ **Task 2.3**: Document the setting in `docs/playbooks.md` under the
  Gnome Shell Extensions entry.

### Phase 3: Deploy and verify

- [ ] ⬜ **Task 3.1**: User runs the play on the host (`deploy.bash`).
- [ ] ⬜ **Task 3.2**: `acceptance.bash` reads the key back and the user
  confirms an un-maximised Ptyxis window overlapping the dock hides it.
- [ ] ⬜ **Task 3.3**: `qa-reviewer` agent as the final step.

## Dependencies

- Depends on: nothing.
- Blocks: nothing.

## Technical Decisions

### Decision 1: `ALL_WINDOWS` instead of fixing the app association

**Context**: the tracker mismatch could be addressed by shipping a `.desktop`
override with a `StartupWMClass`, or by switching to `autohide`.
**Options considered**: A: `intellihide-mode=ALL_WINDOWS`, one dconf key,
independent of how Ptyxis identifies itself, survives Ptyxis updates. B: desktop
file override in `~/.local/share/applications`, fragile and duplicates a distro
file. C: `autohide` with edge reveal, changes dock behaviour for every window.
**Decision**: A. The behaviour change is that the dock also dodges non-focused
overlapping windows, which is the behaviour the user is asking for.
**Date**: 2026-09-03

### Decision 2: the setting lives in the extensions play, not the gsettings play

**Context**: `play-gsettings.yml` already holds dconf tasks.
**Decision**: the key is meaningless without the extension installed, and the
extensions play already carries the Space Bar setting for the same reason.

## Success Criteria

- [ ] `triage.bash` output names the confirmed hypothesis in the journal.
- [ ] `gsettings get org.gnome.shell.extensions.dash-to-dock intellihide-mode`
  returns `'ALL_WINDOWS'` after the play runs, and the play is idempotent on a
  second run.
- [ ] User confirms the dock hides behind an un-maximised terminal.
- [ ] QA passes (`./scripts/qa-all.bash`) and `qa-reviewer` has reviewed.

## Risks & Mitigations

| Risk                                                                             | Impact | Probability | Mitigation                                                     |
| -------------------------------------------------------------------------------- | ------ | ----------- | -------------------------------------------------------------- |
| Dock now hides under any overlapping background window, not just the focused one | L      | H           | This is the requested behaviour; revert is one key             |
| Fedora RPM schema key name differs from upstream                                 | M      | L           | `triage.bash` lists the schema keys before anything is written |
| Cause is H3, and `ALL_WINDOWS` alone changes nothing                             | M      | L           | Task 2.2                                                       |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00102-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan and triage script committed: ddbd8ce
- Triage run on the host; Ansible fix, docs, deploy and acceptance scripts committed.
