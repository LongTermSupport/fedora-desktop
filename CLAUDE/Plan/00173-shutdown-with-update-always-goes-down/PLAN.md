# Plan 00173: shutdown with update always goes down

**Status**: Blocked
**Created**: 2026-10-12
**Owner**: joseph
**Priority**: High

## Overview

`shutdown-with-update` is the evening lifecycle command: update everything, then power
off. The owner found it refusing to do either. A `ccy` session still at ccy's startup
prompts has a tmux session but no container. `ccy-sessions notify going-down --dry-run`,
which the command runs before updating, counted that session as live, found no container
to reach its project's hooks-daemon through, fell back to the host CLI (which has no venv
for a ccy-only project) and refused. The command then exited before `dnf`, leaving the
machine up and not updated. This cause is inferred from the code and reproduced under
fakes; no host capture was taken, so Task 2.2 confirms it.

Two faults, two fixes. `ccy-sessions` treats a `ccy` session with no container as not a
live session: there is nothing in it to warn, so it is named as left out and never
refuses. And `shutdown-with-update` / `reboot-with-update` follow the existing
`provisioning_profile` (desktop or server, auto-detected in
`environment/localhost/group_vars/desktop.yml`), which `play-basic-configs.yml` now
records in `/etc/fedora-desktop/provisioning-profile`. On a desktop the update and the
power action always happen: a session that cannot be warned, a warning that fails or
hangs (each `ccy-sessions` call is bounded by `timeout`), and inhibitors are reported and
gone past. On a server, or with `--strict-sessions`, the session-strict behaviour stays.

## Goals

- A desktop `shutdown-with-update` always updates and shuts down, and nothing in it waits
  without a bound.
- A `ccy` session with no container never blocks any warning, reboot or shutdown.
- The session-strict behaviour stays available: the server profile's default, and
  `--strict-sessions` anywhere.

## Non-Goals

- Changing what a failed `dnf upgrade` does: it still stops the run (fail fast).
- Changing `ccy-sessions reboot`, beyond leaving container-less ccy sessions out.

## Tasks

### Phase 1: Fix

- [x] ✅ **Task 1.1**: `ccy-sessions` `check_projects` leaves out ccy sessions with no
  container, naming them on stderr; routes come from the same container table.
- [x] ✅ **Task 1.2**: `play-basic-configs.yml` records `provisioning_profile` in
  `/etc/fedora-desktop/provisioning-profile`.
- [x] ✅ **Task 1.3**: `shutdown-with-update` reads the profile; desktop is best-effort
  with sessions (rehearsal, warnings, inhibitors), server or `--strict-sessions` is
  strict; every `ccy-sessions` call is bounded by `timeout`.
- [x] ✅ **Task 1.4**: `scripts/test-ccy-sessions-reboot.bash` covers the reported case,
  the desktop paths and the strict paths; `docs/ccy.md` updated.

### Phase 2: Deploy and accept

- [ ] ⬜ **Task 2.1**: Run this plan's `deploy.bash` on the host, through meta-deploy:
  `play-basic-configs.yml` (the script and the profile file) and `play-claude-yolo.yml`
  (`ccy-sessions`).
- [ ] ⬜ **Task 2.2**: Owner runs `shutdown-with-update` in an evening with a ccy session
  left at its startup prompt; the machine updates and powers off.
- [x] ✅ **Task 2.3**: qa-reviewer pass on the branch; its findings fixed in f1ac2371.

## Open Question for the Owner

- When `dnf upgrade` fails on a desktop (a mirror or network failure), should
  `shutdown-with-update` still power off, not updated, or stay up (the current fail-fast
  behaviour) so the failure is seen?

## Success Criteria

- [ ] With a container-less ccy session open, `shutdown-with-update` on the desktop
  updates and powers off without asking anything.
- [x] The test suite proves the desktop and strict paths under fakes.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00173-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Fix, tests and review fixes on F44 (beada13c, f1ac2371), awaiting the host deploy.
