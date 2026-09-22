# Plan 00136: host health report in a terminal

**Status**: In Progress
**Created**: 2026-09-22
**Owner**: joseph
**Priority**: Medium

## Overview

Plan 00109 built the host health report and gave it two deliveries: a notification and
a GNOME panel on a desktop, a login-shell snippet on a server. The owner's verdict on
the panel is that it is not friendly, and that the clear text the server gets at a
terminal is what a desktop should get too, over SSH included, without every new shell
repeating the same wall of findings.

This plan makes the terminal the primary reading surface on both profiles. One command,
`fedora-desktop-health`, prints the full report on demand and is installed with the
report itself. The login snippet is deployed on both profiles and speaks in full once a
day, or whenever the findings change, and otherwise leaves a one-line reminder that
names the command. The panel gains one row that opens that command in a terminal, so the
panel becomes an indicator and the terminal carries the detail.

Nothing here re-runs a play. The runner that would list stale plays and offer to run
them is still Plan 00109 Task 4.3, and the command built here is where it will hang.

## Goals

- A desktop terminal shows the same report a server login shell does, over SSH or local
- The report is shown in full at most once a day per user unless the findings change,
  and a one-line reminder replaces it for the rest of that day
- `fedora-desktop-health` prints the full report on demand, says so plainly when the host
  is clean, and can hold its window open for a terminal launched from the panel
- The panel offers "Open the full report in a terminal" and launches exactly that

## Non-Goals

- Re-running plays, listing stale plays with a prompt, or anything that changes the host.
  That is Plan 00109 Task 4.3
- A MOTD. pam_motd is system-wide and root-written, and the report's state is per user.
  Recorded as a possible addition for the first login after a boot, not built here
- Changing what the checks collect, or when the collectors run

## Tasks

### Phase 1: the report, once a day, and on demand

- [x] ✅ **Task 1.1**: `login_message` gains a once-a-day mode. A stamp file in the
  host state directory records the day and a hash of the message last shown in full;
  the same message on the same day prints a one-line reminder naming the command.
  A changed message, a new day, or an unwritable stamp prints in full. Never raises
- [x] ✅ **Task 1.2**: `login_message` gains an on-demand mode that always prints
  something: the full report, or a positive statement that the host is clean and when it
  was collected. The stamp is neither read nor written
- [x] ✅ **Task 1.3**: `files/home/.local/bin/fedora-desktop-health.j2`, templated with
  the checkout path, runs the on-demand mode. `--hold` keeps the window open until Enter,
  `--help` works, unknown options fail fast, and a missing checkout is named on stderr

### Phase 2: both profiles read it at a terminal

- [x] ✅ **Task 2.1**: `play-host-health-login-report.yml` deploys the snippet, the
  bashrc-includes assertion and the command on both profiles. The desktop cleanup no longer
  removes the snippet. Headers and docs say the terminal is now a delivery on both
- [x] ✅ **Task 2.2**: The snippet passes the once-a-day mode. Its gate covers the reminder
  on a second shell, the full report again when the document changes, and the command's
  `--help`, `--hold` and unknown-option behaviour

### Phase 3: the panel opens the terminal

- [x] ✅ **Task 3.1**: The health section gains one row that launches
  `xdg-terminal-exec` on the user's `fedora-desktop-health --hold`, by argv, and notifies
  when the command is not installed or the terminal cannot start. The handoff row still
  copies and still launches nothing, and the tests now prove that behaviourally
- [x] ✅ **Task 3.2**: `play-fedora-desktop-panel.yml` installs `xdg-terminal-exec`, and
  the panel design notes that the terminal launch for the report is settled here
- [ ] ⬜ **HOST**: run both plays, log out and back in, open a terminal and confirm the
  full report then the one-line reminder; click the panel row and confirm a terminal opens
  and stays open; confirm over SSH

## Success Criteria

- [x] Two interactive shells on the same day with the same findings: the first prints the
  report, the second prints one line naming `fedora-desktop-health`
- [x] A rewritten status document prints the report in full again the same day
- [x] `fedora-desktop-health` on a clean host prints a positive statement, not nothing
- [x] The panel row spawns `xdg-terminal-exec` with the command and `--hold`, and nothing
  else in the health section spawns anything
- [ ] Seen on a host, desktop and over SSH

## Delivery & Milestones

- Phases 1 to 3 in the container, then a host run for the HOST items
