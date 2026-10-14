# Plan 00195: Slack reads LXC containers through project symlinks

**Status**: In Progress
**Created**: 2026-10-14
**Owner**: joseph
**Priority**: Medium

## Overview

Follow-up to Plan 00103 (`Completed/00103-slack-flatpak-rejects-md-dropped-from-nautilus`).
That plan granted the Slack Flatpak `home:ro`, so a file dragged from Nautilus can be read.
A drag from a project folder still fails with the same *"type of file not supported … try
a .zip"* error.

The cause is new, not a regression. Several `~/Projects/<name>` entries are symlinks into
an LXC container's web root, `/var/lib/lxc/<container>/rootfs/var/www/vhosts/…`. Inside
the sandbox the symlink is visible, through `home:ro`, but its target is not, so the
dropped path does not exist for Slack. A one-off `flatpak run --filesystem=<vhosts>:ro`
read the failing file at the host's byte count, which confirms the cause.

`play-comms.yml` grants Slack `/var/lib/lxc:ro`. A grant per container's `vhosts` was
considered and rejected: Flatpak has no globs, so it would need the containers listed when
the play runs, and a container created later would fail again. One fixed grant is generic.
Narrowing it to `vhosts` would need an event-driven sync (a systemd path unit on
`/var/lib/lxc`); the owner chose the plain grant for now.

## Goals

- A file reached through a `~/Projects` symlink into an LXC container drags into Slack and
  uploads, including for containers created after the play ran.
- The grant is read-only and idempotent: a second run reports `ok`.

## Non-Goals

- Narrowing the grant to each container's `/var/www/vhosts` (see Overview).
- `/tmp`, `/var/tmp` and `/run/media`, which the sandbox also does not see; no failure
  from them has been reported.
- Copy/paste of a file into Slack, which Plan 00103 never verified; a separate plan if it
  fails.

## Tasks

### Phase 1: Fix

- [x] ✅ **Task 1.1**: `play-comms.yml` grants Slack `/var/lib/lxc:ro` where the system
  override lacks it
- [x] ✅ **Task 1.2**: `deploy.bash` and `acceptance.bash`; `docs/playbooks.md` updated

### Phase 2: Verify and close

- [ ] ⬜ **Task 2.1**: Owner runs `meta-deploy.bash`; acceptance passes
- [ ] ⬜ **Task 2.2**: Owner relaunches Slack and drags the original file from the
  symlinked project folder; it uploads
- [ ] ⬜ **Task 2.3**: `qa-reviewer` agent run; plan moved to `Completed/`

## Success Criteria

- [ ] The original file drags into Slack from the symlinked folder and uploads
- [ ] `acceptance.bash` passes: the override records `/var/lib/lxc:ro`, and every symlink
  in the home directory that resolves into `/var/lib/lxc` resolves inside a fresh sandbox
- [ ] No manual change on the host; everything is in `play-comms.yml`

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00195-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Fix, deploy and acceptance scripts: (this commit)
