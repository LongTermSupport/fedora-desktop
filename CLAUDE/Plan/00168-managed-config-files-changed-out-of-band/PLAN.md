# Plan 00168: managed config files changed out of band

**Status**: Not Started
**Created**: 2026-10-09
**Owner**: joseph
**Priority**: Medium
**Issue**: [#91](https://github.com/LongTermSupport/fedora-desktop/issues/91)

## Overview

The plays write files all over the system: drop-ins under `/etc`, lines inside package
config files, units, udev rules, scripts. Once a play has run, nothing watches those files.
If something else changes one, the repo still says the host is configured and every
existing check stays green. That something can be a package update replacing a config
file, a GNOME or vendor tool rewriting its settings, or a hand edit.

Plan 00166 is the case that prompted this. A upower update replaced `/etc/UPower/UPower.conf`
with the stock file and kept ours as `UPower.conf.rpmsave`. By luck the stock value was the
better one, so nothing visibly broke. When the play next ran it wrote the old value back, and
that is what broke. Either way round, a change to a managed file went unnoticed until a
human noticed a symptom.

This plan adds that missing axis: **what a play last left in a file vs what is in it now**.
The aim is to report each difference so it can be reviewed, not to put things back
automatically. An out-of-band change can be the right one, as it was here.

## Goals

- This host knows which files the plays manage, and in what state each play left them,
  learned from the runs themselves rather than by reading playbooks.
- A check reports every managed file whose current state differs from what its play left,
  naming the file, the play, when it was last applied, and what kind of change it is.
- Package updates that touched a managed file are reported, including any `.rpmnew` or
  `.rpmsave` beside it.
- The findings reach a human through the surface Plan 00109 built (the login health report
  and the fedora-desktop panel), not only a script nobody runs.

## Non-Goals

- Re-applying or reverting anything. Detection and review only; re-running a play stays a
  human decision (the same line Plan 00109 draws).
- Files the plays never touch. This is not a general `/etc` integrity monitor.
- Replacing the axes that already exist: play freshness and install state (Plan 00109),
  and repo vs deployed `~/.local/bin` (`qa-deployed-drift.bash`).

## Context & Background

| Axis                         | Compares                                       | Owned by                 |
| ---------------------------- | ---------------------------------------------- | ------------------------ |
| Pin freshness                | repo pin vs upstream latest                    | `check-pinned-versions`  |
| Script deployment            | repo script vs deployed `~/.local/bin` copy    | `qa-deployed-drift.bash` |
| Install state                | repo pin vs installed package                  | Plan 00109               |
| Play freshness               | play at its last run here vs play at HEAD      | Plan 00109 (play ledger) |
| **Managed file state** (new) | **what a play left in a file vs the file now** | **this plan**            |

Plan 00109's play ledger (`callback_plugins/play_ledger.py`, `helpers/play_ledger/`)
already records every play run on this host from an Ansible callback. That hook already
sees every task result, so it is the natural place to record what each file-writing task
left behind.

Whole-file modules (`copy`, `template`) leave a file whose checksum can be compared.
Partial-edit modules (`lineinfile`, `blockinfile`, `ini_file`) own only part of a file. A
package rewriting the rest is not drift, but our line or block going missing is. So the
check has to be per module type. The UPower case was a partial edit.

## Tasks

### Phase 1: Establish the facts

- [ ] ⬜ **Task 1.1**: `triage.bash`: count the file-writing tasks across the plays by
  module, and list which managed paths are package-owned (`rpm -qf`). That decides how
  much of the problem `rpm -V` and `.rpmnew` / `.rpmsave` detection alone would cover.
- [ ] ⬜ **Task 1.2**: Read what the callback sees for each file module's result (`dest`
  or `path`, `checksum`, `diff`, changed, and the check-mode result). Record which modules
  report enough to re-check later, and which need the task's arguments (the line, regexp
  or block) captured as well. Written up in `DESIGN-managed-files.md`.
- [ ] ⬜ **Task 1.3**: Decide where the records live and how they relate to the play-ledger
  records (same store, or beside it). Settle what "last applied" means when a later run of
  the same play skips a task.

### Phase 2: Record on every run

- [ ] ⬜ **Task 2.1**: A TDD'd helper under `helpers/` turns a file-module task result into a
  record: path, play, module, the expected state (checksum for whole files, the owned line
  or block for partial edits), commit, time.
- [ ] ⬜ **Task 2.2**: Wire it into the play-ledger callback so every real run (not
  `--check`) writes records for the files it applied.

### Phase 3: Check and report

- [ ] ⬜ **Task 3.1**: A TDD'd checker compares each record with the file on disk. It
  reports `changed`, `missing`, `partial edit lost`, plus any `.rpmnew` / `.rpmsave`
  beside the file and the package transaction that last touched it.
- [ ] ⬜ **Task 3.2**: Feed the findings into Plan 00109's login health report and panel.
- [ ] ⬜ **Task 3.3**: `acceptance.bash`: change a managed test file out of band, and check
  the report names it, its play and the kind of change. Then re-run the play and check the
  finding clears.

### Phase 4: Review and close

- [ ] ⬜ **Task 4.1**: `./scripts/qa-all.bash`, then the `qa-reviewer` agent over the plan's
  diff; resolve every BLOCK and FIX-BEFORE-MERGE finding.
- [ ] ⬜ **Task 4.2**: Docs: `docs/` for the user-facing report; mark the plan Complete.

## Dependencies

- Depends on: Plan 00109 (play ledger, health report, panel), In Progress.
- Prompted by: Plan 00166.

## Success Criteria

- [ ] A managed file changed by hand, and a managed line removed by a simulated package
  update, are both reported, each with its play, last-applied time and kind of change.
- [ ] A package update that leaves a `.rpmnew` or `.rpmsave` beside a managed file is
  reported.
- [ ] Nothing is reported for files no play manages, or for the parts of a file a partial
  edit does not own.
- [ ] Findings appear in the login health report.
- [ ] QA passes and the `qa-reviewer` findings are resolved.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00168-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan written
