# Plan 00176: dash to dock stays up over windows again

**Status**: Blocked
**Created**: 2026-10-13
**Owner**: joseph
**Priority**: Medium

Blocked on the owner: five observations at the screen (Task 1.1).

## Overview

Dash to Dock again stays visible over a Ptyxis terminal window instead of getting out of
the way. Plan 00102 fixed this on 2026-09-03 by setting Dash to Dock's
`intellihide-mode` to `ALL_WINDOWS`, so that hiding no longer depends on GNOME linking the
window to an app (D-Bus-activated Ptyxis has no `StartupWMClass`). That fix was confirmed on
a single screen.

Triage on 2026-10-12 found that the fix has **not** regressed, and that the mechanism Plan
00102 fixed cannot be the cause any more. The root cause is not yet known. This plan finds
it from the owner's observations, then fixes it in the play that owns Dash to Dock's
settings. The triage detail is in `JOURNAL/`.

## Goals

- Find which condition keeps the dock shown over a window that overlaps it.
- Fix it in `playbooks/imports/play-gnome-shell-extensions.yml`, where the dock's settings
  are applied.
- The dock hides for an overlapping terminal window on every monitor layout the host uses.

## Non-Goals

- Silencing Dash to Dock's `Can't update stage views` journal warnings (Plan 00134 leaves
  third-party extension warnings out of scope).
- The DisplayLink re-layouts themselves (Plans 00056, 00166).

## Tasks

### Phase 1: Find the cause

- [ ] 🚫 **Task 1.1**: The owner answers five questions, each settled by one look at the
  screen:
  1. Does a non-terminal window (Files, a browser) dragged over the dock hide it? This
     separates a Ptyxis-only problem from one affecting every window.
  2. Is the stuck dock on the primary external monitor? Does it also happen undocked, on the
     laptop panel only?
  3. Does unplugging and replugging the dock, or closing and opening the lid, start or stop
     the problem?
  4. When it fails, is the pointer resting on the bottom edge, over the dock?
  5. Does the dock stay up the whole time, or pop up for about 3 seconds and drop again?
- [ ] ⬜ **Task 1.2**: Map the answers to a cause and record the decision in the journal:
  - questions 1 to 3 point to a stale monitor index after a DisplayLink re-layout (every
    app, docked only, changing after a monitor event);
  - question 4 points to hover;
  - question 5 points to the urgent reveal (`show-dock-urgent-notify`).

### Phase 2: Fix in IaC

- [ ] ⬜ **Task 2.1**: Change Dash to Dock's settings in
  `playbooks/imports/play-gnome-shell-extensions.yml` for the cause found. For a stale
  monitor index, check first whether a Dash to Dock setting covers it, or whether the fix
  belongs with the DisplayLink re-layout handling.
- [ ] ⬜ **Task 2.2**: Deploy through `CLAUDE/Plan/meta-deploy.bash`. The owner confirms the
  dock hides over a terminal, docked and undocked.
- [ ] ⬜ **Task 2.3**: Run the `qa-reviewer` agent.

## Dependencies

- Plan 00102 (Completed): the `ALL_WINDOWS` setting this plan builds on.
- Plans 00056 and 00166: the DisplayLink and lid re-layouts that may trigger the fault.

## Success Criteria

- [ ] The cause is named, with the owner's observation that shows it.
- [ ] The fix is in the play and deployed.
- [ ] The dock hides for an overlapping terminal window docked on the external monitors and
  undocked on the laptop panel, including after a monitor change.

## Delivery & Milestones

- Triage: the earlier fix is intact; no IaC change made; cause not yet found.
