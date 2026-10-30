# Plan 00242: consumer install follow ups

**Status**: Not Started
**Created**: 2026-10-30
**Owner**: joseph
**Priority**: Medium

## Overview

Plan 00228 built the consumer install: `./run.bash --consumer` sets up fedora-desktop for
people who do not write code, with no GitHub account, no SSH key on GitHub, no private config
repo and none of the developer tooling. The public repository is fetched anonymously over
HTTPS and only the plays whose `audience` key is `all` are installed. The choice is recorded
in `localhost.yml` as `install_audience: consumer` and honoured by `run.bash`, every play, the
optional-play menu and later runs.

It closed with work it decided on but did not build, and with findings from its final review
([261030-qa-reviewer-final.md](../00228-consumer-install-without-github/subagent-reports/261030-qa-reviewer-final.md))
that it did not fix. This plan holds them. Each task names where in Plan 00228 it comes from.

## Goals

- A consumer can update without a terminal (Decision 5's launcher).
- A consumer box has a route to becoming a developer one, or the docs say it has none.
- The mixed plays, the `--changed` path and the kickstart treat a consumer box as one.
- The consumer VM scenario's re-run checks the optional play it installed.

## Non-Goals

- A developer box becoming a consumer one (Plan 00228 Decision 6(c): refused).
- Plan 00137's signed self-update timer for consumer desktops (Decision 5(b), raised against
  Plan 00137).

## Tasks

### Phase 1: Updating and changing audience

- [ ] ⬜ **Task 1.1**: The desktop launcher "Update fedora-desktop" (Plan 00228 Decision 5,
  its default (a): "re-run `run.bash` (or a desktop launcher that runs it), HTTPS pull ... in
  this plan, with a desktop launcher entry named 'Update fedora-desktop'"). Nothing builds it
  yet: the name appears only in Plan 00228 and its journal.
- [ ] ⬜ **Task 1.2**: A consumer box becoming a developer one (Plan 00228 Decision 6(a)). The
  decision says it is "supported by re-running with the developer flag", but no such flag
  exists, and `run.bash:1283-1298` sends every plain run on a box whose `localhost.yml` holds
  `install_audience: consumer` back to the consumer install. Decide: build the route (a flag
  that runs the GitHub steps and moves origin to SSH), or refuse it and say so in
  `docs/installation.md`.

### Phase 2: Moved from Plan 00228 Phase 6

- [ ] ⬜ **Task 2.1** (Plan 00228 Task 6.1): run.bash fully becomes a thin wrapper over Go, as
  its own plan: a fresh box has no Go and no checkout until the bash bootstrap has run, so
  that plan owns the bootstrap's shape. This may become its own plan; Task 2.3 depends on it.
- [ ] ⬜ **Task 2.2** (Plan 00228 Task 6.2): Gate the developer parts of mixed plays on a
  consumer box: linger (`play-systemd-user-tweaks.yml`) and any developer-only GNOME
  extension.
- [ ] ⬜ **Task 2.3** (Plan 00228 Task 6.3): A consumer `--changed` path in Go (today
  `--changed` on a consumer box runs run.bash's own play runner, which offers no GitHub issue
  there).
- [ ] ⬜ **Task 2.4** (Plan 00228 Task 6.4): A consumer choice in the kickstart
  (`fedora-install/ks.cfg`).

### Phase 3: The final review's open findings

- [ ] ⬜ **Task 3.1** (review finding 6): The consumer VM scenario's `rerun-changed-nothing`
  covers only the main playbook: the checker's re-run answers Enter at the app menu
  (`guest-acceptance-desktop-consumer-install.bash`), so a second run of the speech-to-text
  play is never checked, though eca1621dd rewrote how that play enables its extension. Pick
  the play again on the re-run, or say the limit in the pass line.
- [ ] ⬜ **Task 3.2** (review finding 7): `play-rclone.yml` is `audience: all`, so the consumer
  menu offers it, and when rclone is not configured its closing message tells the user to run
  `ansible-vault encrypt_string` and edit host_vars, a developer workflow. Make it
  `developer`, or give it consumer wording.
- [ ] ⬜ **Task 3.3** (review nit): `run.bash:1296` repeats the HTTPS URL that
  `fedora_desktop_https_url` already holds (defined later in the file). Use the one value;
  this changes run.bash, so it takes a `RUN_BASH_VERSION` bump.

### Phase 4: Review

- [ ] ⬜ **Task 4.1**: `qa-reviewer` over the plan diff; every BLOCK and FIX-BEFORE-MERGE
  finding resolved.

## Success Criteria

- [ ] Each task above is built, or decided against with the reason recorded here.
- [ ] `desktop-consumer-install` passes in the VM lab with the changes in.
- [ ] `./scripts/qa-all.bash` passes and `qa-reviewer` has no open BLOCK finding.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00242-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed.
