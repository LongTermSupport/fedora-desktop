# Plan 00166: laptop panel stays on with lid closed when docked

**Status**: Not Started
**Created**: 2026-10-09
**Owner**: joseph
**Priority**: Medium

## Overview

With the laptop on its DisplayLink dock, three external monitors attached and the lid
closed, GNOME still treats the built-in panel (eDP-1) as an active monitor. Settings lists
four displays, and a window dragged off the edge of an external monitor lands on a screen
nobody can see.

The cause is `IgnoreLid=true` in `/etc/UPower/UPower.conf`, set by
`playbooks/imports/play-suspend-and-lid-policy.yml` (task "Configure UPower to ignore lid
(let logind handle it)"). With it, UPower reports `LidIsPresent=false`. Mutter takes the
lid state from UPower, so it never learns the lid is shut and never switches the panel
off. logind is unaffected: it reads the lid switch from the kernel itself.

The setting dates from February, meant to remove a `handle-lid-switch` inhibitor held by
gnome-settings-daemon's power plugin. It does not do that: the inhibitor is still held with
`IgnoreLid=true` (handover evidence below), and while docked that inhibitor is what we
want anyway. The host had `IgnoreLid=false` until Plan 00104's deploy changed it (00104's
reviewer reports record the file at `false` before that run), so this regression arrived
with Plan 00104 moving the task into a play that `playbook-main.yml` imports.

## Goals

- With the lid closed while docked, eDP-1 is disabled and GNOME shows only the external
  monitors.
- Closing the lid on external power still does not suspend, sleep or change the power
  profile.
- The play sets `IgnoreLid=false` and asserts that UPower reports the lid, so a regression
  fails the deploy rather than showing up as a lost window.

## Non-Goals

- Changing logind's lid policy (`/etc/systemd/logind.conf.d/laptop-lid.conf`). It is
  correct as it is.
- Plan 00104's aborted-suspend recovery. Its open hardware tests stay in 00104.
- Monitor layout or DisplayLink hot-plug behaviour (Plan 00056).

## Context & Background

Evidence, verified live by the session that wrote the handover:

| ID  | Fact                                                                                                                            |
| --- | ------------------------------------------------------------------------------------------------------------------------------- |
| F1  | `/sys/class/drm/card1-eDP-1/enabled` = `enabled` while `/proc/acpi/button/lid/*/state` = `closed`                               |
| F2  | UPower reports `LidIsPresent=false`, `LidIsClosed=false` with `IgnoreLid=true`                                                  |
| F3  | `systemd-inhibit --list` shows gsd-power holding `handle-lid-switch` ("External monitor attached…") even with `IgnoreLid=true`  |
| F4  | logind: `LidClosed=true`, `Docked=true`, `HandleLidSwitchDocked=ignore`, plus this repo's `HandleLidSwitchExternalPower=ignore` |
| F5  | Power profile changes come from `power-saver-profile-on-low-battery`, not from the lid                                          |
| F6  | `rpm -V upower` shows `UPower.conf` modified; a `UPower.conf.rpmsave` exists beside it                                          |
| F7  | Plan 00104 reviewer reports (`subagent-reports/260908-*round2.md`, `*round5.md`) record `IgnoreLid=false` before 00104's deploy |

History: commit `19ce2c29` added the setting and `c8a627ed` switched it to `lineinfile`.
Plan 00104 moved the task into this play without revisiting it, and its F9 could not
reproduce the February "GSD blocks logind" premise (undocked). F3 is consistent with that:
gsd-power takes the inhibitor only while an external monitor is attached. What the February
symptom actually was is unknown. The test matrix below is meant to surface it if it returns.

## Tasks

### Phase 1: Triage (current, broken state)

- [ ] ⬜ **Task 1.1**: Write `triage.bash` on `_planlib.inc.bash`, read-only. It records:
  `IgnoreLid` in `UPower.conf`; UPower `LidIsPresent` and `LidIsClosed` (busctl); the ACPI
  lid state; every `/sys/class/drm/*-eDP-*/{enabled,status}`; `systemd-inhibit --list`;
  logind `LidClosed`, `Docked` and `HandleLidSwitch*`; the active power profile;
  `rpm -V upower`; whether `UPower.conf.rpmsave` exists and how it differs.
- [ ] ⬜ **Task 1.2**: Add the plan to `meta-deploy.bash`. The owner runs triage docked
  with the lid closed. Record the facts it establishes in the journal.

### Phase 2: Fix the play

- [ ] ⬜ **Task 2.1**: Change the task to `line: 'IgnoreLid=false'`, rename it, and rewrite
  its comment with the real reason: Mutter needs UPower's lid state to switch the panel off,
  and logind does not read UPower.
- [ ] ⬜ **Task 2.2**: After the existing `flush_handlers`, add a read-back:
  `busctl get-property org.freedesktop.UPower /org/freedesktop/UPower org.freedesktop.UPower LidIsPresent`
  must print `b true` on a host whose ACPI exposes a lid, and the play fails otherwise.
  Gate it on the lid's presence in `/proc/acpi/button/lid/`, not on the profile.
- [ ] ⬜ **Task 2.3**: Update `docs/playbooks.md` (the `IgnoreLid` line near 203) and the
  header comment in Plan 00104's `deploy.bash` (line 9).
- [ ] ⬜ **Task 2.4**: Run QA: `./scripts/qa-all.bash`; fix any findings.

### Phase 3: Deploy and accept

- [ ] ⬜ **Task 3.1**: `deploy.bash` runs `play-suspend-and-lid-policy.yml`.

- [ ] ⬜ **Task 3.2**: `acceptance.bash` checks `IgnoreLid=false`, `LidIsPresent` = true and,
  when docked with the lid closed, that every eDP connector is disabled, no suspend has
  been logged since the lid closed, and the power profile matches the one triage recorded.
  It prints `COVERAGE: n of m` and rejects an incomplete run. It lists as NOT ESTABLISHABLE
  the steps that need a person at the machine (rows 2 to 4 below).

- [ ] ⬜ **Task 3.3**: Add the plan to `meta-deploy.bash`. The owner runs deploy and
  acceptance docked with the lid closed, then works through the hardware matrix:

  | Scenario                                   | Expected                                                    |
  | ------------------------------------------ | ----------------------------------------------------------- |
  | Docked, AC, lid closed                     | no suspend; eDP-1 off; 3 monitors in Settings; same profile |
  | Undocked, AC, lid closed                   | no suspend                                                  |
  | Battery, lid closed                        | suspends                                                    |
  | Dock unplugged during suspend (Plan 00104) | suspend still completes or re-suspends                      |

### Phase 4: Review and close

- [ ] ⬜ **Task 4.1**: Run the `qa-reviewer` agent over the plan's diff; resolve every BLOCK
  and FIX-BEFORE-MERGE finding.
- [ ] ⬜ **Task 4.2**: Remove the plan from `meta-deploy.bash`, mark it Complete, and move it
  to `Completed/`.

## Dependencies

- Related: Plan 00104 (Blocked on hardware), which owns the dock-unplug recovery and shares
  matrix row 4.

## Technical Decisions

### Decision 1: Set `IgnoreLid=false` explicitly instead of restoring the package file

**Context**: F6 shows the file is already modified from the RPM default, and a `.rpmsave`
sits beside it.
**Options considered**: (A) keep `lineinfile` and write `IgnoreLid=false`: idempotent,
visible in the play, and survives package upgrades. (B) Restore the packaged file: drops
any other local change to `UPower.conf` without our knowing what it was, and leaves
nothing in the play to assert against.
**Decision**: A. Triage records the `.rpmsave` diff, so if it holds anything else of ours
we find out before choosing again.
**Date**: 2026-10-09

## Success Criteria

- [ ] Docked with the lid closed, Settings shows three monitors and eDP-1 is disabled.
- [ ] All four rows of the hardware matrix behave as expected.
- [ ] The play fails if UPower does not report the lid on a host that has one.
- [ ] QA passes (`./scripts/qa-all.bash`) and the `qa-reviewer` findings are resolved.

## Risks & Mitigations

| Risk                                                      | Impact | Probability | Mitigation                                                               |
| --------------------------------------------------------- | ------ | ----------- | ------------------------------------------------------------------------ |
| The unknown February symptom returns (lid close suspends) | H      | L           | Matrix rows 1–2 test exactly that; logind's own policy is unchanged (F4) |
| GNOME acts on the lid itself once UPower reports it       | M      | L           | Acceptance compares the power profile; the matrix covers suspend         |
| `restart-upower` briefly drops battery state in the panel | L      | M           | Expected and short-lived; already true of the current handler            |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00166-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan written from the handover
