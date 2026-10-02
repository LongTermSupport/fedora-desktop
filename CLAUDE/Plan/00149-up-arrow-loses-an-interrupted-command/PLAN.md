# Plan 00149: up arrow loses an interrupted command

**Status**: Dormant (parked by the owner: not reproduced in the container or on the host. If
the owner sees it again, reopen, rerun `triage.bash` and record the exact steps in Task 1.5)
**Created**: 2026-10-02
**Owner**: joseph
**Priority**: Medium

## Overview

The owner reports that in a local terminal, a command that is run and then stopped with
Ctrl+C is not there when up-arrow is pressed. They saw the same inside LXC containers
earlier. This repo owns the history setup: `files/etc/profile.d/zz_lts-fedora-desktop.bash`
(sizes, `histappend`, `__history_append` at each prompt) and, for the desktop user,
`files/home/bashrc-includes/history-search.bash` (Plan 00138), which gives each terminal's
up-arrow only that terminal's own commands by starting the shell with `HISTFILE=/dev/null`
and pointing it back at the shared file at the first prompt.

It could not be reproduced in the ccy container. The same two files, loaded into a real
interactive bash 5.2 in a pty as a non-root user, recall the interrupted command, the
finished one and the one before an abandoned line. So the difference lies on the host:
its bash (Fedora 44 ships 5.3), or another startup file overriding a history setting, or
the terminal. The triage script measures which.

## Goals

- Up-arrow in a local terminal brings back a command that was stopped with Ctrl+C.
- The cause is established by measurement on the host before anything is changed.
- A regression test drives a real interactive bash through the failing case.

## Non-Goals

- Changing the per-terminal up-arrow design of Plan 00138, unless the triage shows it is
  the cause.
- The LXC containers' own shells, unless the triage shows the same cause there.

## Tasks

### Phase 1: Establish the cause

- [x] ✅ **Task 1.1**: Try to reproduce in the ccy container: a pty-driven `bash -i`, as a
  non-root user, with the two deployed files. Not reproduced on bash 5.2: all three cases
  recall the right line.
- [x] ✅ **Task 1.2**: `triage.bash` (read-only): facts about bash, drift between the deployed
  files and this checkout, every startup line that touches history, prompts, traps or
  bindings, and the up-arrow probe (`probe-uparrow.py`) run twice: with the full startup and
  with only this repo's two history files.
- [x] ✅ **Task 1.3**: **HOST (owner)**: run `CLAUDE/Plan/meta-deploy.bash`, which runs the
  triage, as the desktop user in a local terminal.
- [x] ✅ **Task 1.4**: Read the report and record the cause in the journal. **Not
  reproduced on the host either** (bash 5.3.9, deployed files identical to the checkout).
  With only this repo's history files, all three cases recall the right line. With the full
  startup, up-arrow recalled the right line in all three too: after Ctrl+A the shell moved
  the cursor back exactly the expected command's length (20, 26, 26 characters). The probe
  then printed nothing for those runs because its output filter ran past systemd's
  `ESC \`-terminated OSC 3008 sequence to the next BEL, swallowing the command output. The
  filter is fixed.
- [ ] ⏸️ **Task 1.5**: **Owner**: the exact steps that lose the line: which command, which
  terminal app or tab, whether up-arrow is pressed in the same tab, and whether it happens
  every time. On hold until it happens again.

### Phase 2: Fix

- [ ] ⬜ **Task 2.1**: A failing test that drives a real interactive bash through the case
  the triage found, then the fix, in the play that owns the file at fault.
- [ ] ⬜ **Task 2.2**: QA, the qa-reviewer, and a host run through meta-deploy.

## Success Criteria

- [ ] On the host, the triage's case B (a running command stopped with Ctrl+C) brings back
  that command, with the full startup.
- [ ] A regression test fails before the fix and passes after it.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00149-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Triage: `triage.bash`, `probe-facts.bash`, `probe-uparrow.py`
