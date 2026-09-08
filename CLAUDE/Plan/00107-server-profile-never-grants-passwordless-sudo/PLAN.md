# Plan 00107: server profile never grants passwordless sudo

**Status**: In Progress
**Created**: 2026-09-08
**Owner**: joseph
**Priority**: Medium

## Overview

`play-basic-configs.yml` writes a marker-wrapped `NOPASSWD: ALL` block for `user_login` into
`/etc/sudoers` on every run, regardless of `provisioning_profile`. That is the right default for
the product's original target, a personal workstation. It is the wrong default for a headless
server: the box is reached by automation, nobody is at a keyboard to be spared a prompt, and a
permanent `NOPASSWD:ALL` turns any process running as that user into root.

A downstream fleet that refuses passwordless sudo on its servers is left undoing this play's work
after every provisioning run, and racing it when a run aborts between the grant and the undo.
Provisioning a box and then reversing part of what the provisioner did is an asymmetry that
never stops costing: two repositories disagree about the box's steady state, and the one that
runs second wins.

The fix belongs where the grant is made. Under the server profile the same task now declares the
block `absent`, so a server ends up in the state its profile says, whether or not an earlier
desktop-profile run granted the block first. Only the play's own marker is touched; a sudoers rule
the operator wrote themselves stays theirs.

## Goals

- The server profile never leaves this play's `NOPASSWD` block in `/etc/sudoers`, including on a
  box where an earlier run wrote it.
- The desktop profile behaviour is unchanged.
- The headless docs state where a server's sudo credential comes from now that the playbook is
  not a source of one.

## Non-Goals

- Asserting that a server has no `NOPASSWD` rule at all. An operator-owned drop-in
  (`docs/headless-server-install.md` Step 0a) is a supported credential for the headless
  preflight and is not this play's to remove.
- Changing `run.bash`. Its preflight contract already accepts either an operator-owned
  `NOPASSWD` rule or `RUN_BASH_SUDO_PASSWORD_FILE`; nothing there depended on the play's block.
- The kickstart path (`fedora-install/ks.cfg`), which writes its own drop-in for a bare-metal
  desktop install.

## Tasks

### Phase 1: the grant follows the profile

- [x] ✅ **Task 1.1**: `play-basic-configs.yml` — the passwordless-sudo `blockinfile` takes
  `state: absent` under `provisioning_profile == 'server'` and `present` otherwise, with the
  rationale in the task comment.
- [x] ✅ **Task 1.2**: docs — `docs/playbooks.md` (play summary), `docs/architecture.md`
  (security model), `docs/headless-provisioning.md` (preconditions: the playbook is not a
  credential source on a server; a box that relied on the old block needs Step 0a or the
  password file).

### Phase 2: proof — BLOCKED BY Phase 1

- [ ] ⬜ **Task 2.1**: syntax check and lint of the changed play; `./scripts/qa-all.bash`.
- [ ] ⬜ **Task 2.2**: live proof on a real headless server box: a run with the profile set to
  `server` leaves no marker block in `/etc/sudoers` and `sudo -k -n true` as the user exits
  non-zero, with no downstream removal step in the picture. See the journal.

## Success Criteria

- [ ] A server-profile run of `playbook-main.yml` reports the sudoers task `ok` on a clean box
  and `changed` (block removed) on a box carrying the block from an earlier run.
- [ ] A desktop-profile run still writes the block.
- [x] The three docs name the behaviour and where a server's credential comes from.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00107-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Grant follows the profile; docs; live proof on a headless box — see journal for the commit.
