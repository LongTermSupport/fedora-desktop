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

Where it stands: deployed, and the owner's evening run updated and powered off (Task 2.2).
The failed-`dnf` question is settled (Technical Decisions). Open: merge and deploy Phase 3,
the owner's choice in Task 3.3, and confirming the success criterion's container-less case.

## Goals

- A desktop `shutdown-with-update` always updates and shuts down, and nothing in it waits
  without a bound.
- A `ccy` session with no container never blocks any warning, reboot or shutdown.
- The session-strict behaviour stays available: the server profile's default, and
  `--strict-sessions` anywhere.

## Non-Goals

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
- [x] ✅ **Task 1.5**: A desktop goes on past a failed update step, `dnf` included,
  journalling each failure and a summary before the power action; a strict run still
  stops on a failed `dnf` (Technical Decisions).
- [x] ✅ **Task 1.6**: Owner: "ensure any shutdown-with-update related issues do get
  reported in the extension on next boot". Every refusal that leaves the machine up is
  journalled too (a `refuse` helper, and the EXIT trap for one that did not go through it;
  not a usage error or Ctrl-C). The host-health post-boot probe
  (`helpers/host_health/probe.py`) reads the previous boot's error lines under the tag and
  reports them as one finding in the panel's post-boot-health section, with
  `journalctl -t shutdown-with-update -b -1 --no-pager` to read. Previous boot only, so it
  shows for one boot; an empty answer from a system journal this user cannot read is
  unchecked, not clean. Deployed by `play-basic-configs.yml` (the script); the collector
  runs from the checkout (`host-health.service`), so it needs no play.

### Phase 2: Deploy and accept

- [x] ✅ **Task 2.1**: This plan's `deploy.bash` ran on the host through meta-deploy:
  `play-basic-configs.yml` (the script and the profile file) and `play-claude-yolo.yml`
  (`ccy-sessions`), both failed=0.
- [x] ✅ **Task 2.2**: Owner runs `shutdown-with-update` in an evening with a ccy session
  left at its startup prompt; the machine updates and powers off. The owner ran it in full
  and rebooted next morning: "all worked fine". The report does not say whether a ccy
  session was at its startup prompt (Success Criteria).
- [x] ✅ **Task 2.3**: qa-reviewer pass on the branch; its findings fixed in f1ac2371.

### Phase 3: cc sessions are not shown the warning

The owner saw no warning in a `cc` session in this repo during the Task 2.2 run. By design
nothing on the host shows one: the daemon's `signal` writes a per-session file, and only
the ccy supervisor reads it; `cc` runs `claude` with no supervisor. The code and docs
claimed cc sessions were warned (journal, 26-10-13).

- [x] ✅ **Task 3.1**: Every warning from `ccy-sessions` names each cc session on stderr as
  not shown the warning; comments, `shutdown-with-update --help` and `docs/ccy.md` say what
  really happens; `scripts/test-ccy-sessions-reboot.bash` covers it.
- [ ] ⬜ **Task 3.2**: This plan's `deploy.bash` runs on the host through meta-deploy
  (`play-claude-yolo.yml` for `ccy-sessions`, `play-basic-configs.yml` for the help text).
- [ ] ⬜ **Task 3.3**: Owner decision: whether cc sessions should be shown the warning at all.
  The two routes are running the project supervisor in `cc`, as ccy's entrypoint does, or a
  daemon handler that shows a pending signal on the session's next hook event (upstream,
  and an idle session never gets it). Either is its own plan.

## Technical Decisions

- **A failed update step does not keep a desktop up.** The owner: "shutdown with update by
  default should ALWAYS do a shutdown — it can wait a grace period or whatever, but it
  MUST do the update and shutdown ... desktop mode should do what I tell it to do." So on
  a desktop a failed `dnf upgrade` (like every other update step) is printed, recorded in
  the journal (`journalctl -t shutdown-with-update`) and gone past, and a summary of the
  failed steps is printed and journalled just before the power action. A strict run
  (server, or `--strict-sessions`) still stops on a failed `dnf`. The startup refusals
  (no `SUDO_USER`, a root shell, no profile file, no `ccy-sessions`) stay fail-fast: they
  happen while the person who ran it is watching.

## Success Criteria

- [ ] With a container-less ccy session open, `shutdown-with-update` on the desktop
  updates and powers off without asking anything. The Task 2.2 run updated and powered
  off; whether such a session was open is the owner's to confirm.
- [x] The test suite proves the desktop and strict paths under fakes.
- [ ] Whoever runs `shutdown-with-update` is told which sessions will not see the warning
  (proved under fakes; Task 3.2 deploys it).

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00173-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Fix, tests and review fixes on F44 (beada13c, f1ac2371); deployed and accepted by the
  owner's evening run (Task 2.2).
- Phase 3 (cc sessions named as not shown the warning) on a work branch, awaiting merge
  and the host deploy.
