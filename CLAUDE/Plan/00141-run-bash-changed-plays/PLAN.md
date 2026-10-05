# Plan 00141: run.bash --changed runs every play whose inputs changed since it ran here

**Status**: In Progress
**Created**: 2026-09-25
**Owner**: joseph
**Priority**: Medium

## Overview

The login report (Plan 00136) and the panel (Plan 00109) say which plays have changed
since they last ran here. Acting on that means running each play by hand. The owner wants
one command: `./run.bash --changed`.

Deciding what counts as "changed" is the real work. The play ledger watches only each play's
own file, so a change to something a play deploys, such as a ccy lib, `ccy-sessions` or a
template, leaves the play reading as current. Plan 00137 already solved that for the
server's self-update: `helpers/self_update/affected_plays.py` maps changed paths to the
plays that deploy them, from the play text. This plan joins the two. For each play the
ledger has seen, it takes the paths that changed between the commit the play last ran from
and the working tree, and asks whether any of them is one of that play's inputs.

## Goals

- `./run.bash --changed` lists the plays to run, asks once, then runs them one after
  another through the same become-aware runner a single play uses, under one play lock. It
  stops at the first failure and exits with that play's status.
- A play counts as changed if any of its inputs changed since the commit it last ran from.
  That includes its own file and uncommitted edits in the working tree.
- A play whose references cannot be followed is named as "cannot tell", never passed
  over silently. A play that no longer exists is named, not run.
- Nothing to run is a clean exit that says so.

## Non-Goals

- No plays that have never run here. Optional plays are opt-in, and `--changed` does not
  opt anyone in.
- No change to the login report's wording or its check. The report may name fewer plays
  than `--changed` runs; aligning the two is follow-up work.
- No headless or unattended mode. The server's self-update cycle already owns that path.

## Tasks

### Phase 1: which plays changed

- [x] ✅ **Task 1.1**: `helpers/play_ledger/changed_plays.py`. It reads the ledger's latest
  record per play, and diffs each record's commit against the working tree (one
  `git diff` per distinct commit). It matches the changed paths against
  `affected_plays.play_inputs` and prints marker lines:

  - `RUN <play>`;
  - `UNRESOLVED <play> <where>`;
  - `GONE <play>`.

  A broken ledger, an unreadable ledger or a failed git call answers exit 2 with nothing
  on stdout. Tests first.

### Phase 2: the command

- [x] ✅ **Task 2.1**: `run.bash --changed`. It uses the checkout (never a streamed
  run.bash), and refuses to combine with a playbook path or `--optional-only`. It shows
  the plan, confirms y/N, runs each play through the single-play runner, and stops at the
  first failure. Bump `RUN_BASH_VERSION`, add a changelog entry, and update `--help`.
- [x] ✅ **Task 2.2**: A test for the flag, against a stubbed helper and runner. It covers:
  - the order of the plays;
  - a stop at the first failure;
  - nothing to run;
  - a refusal to combine with other flags;
  - the "cannot tell" and "gone" lines.

### Phase 3: finish

- [x] ✅ **Task 3.1**: Docs (`docs/playbooks.md` or the run.bash doc), `qa-all.bash`, and
  the `qa-reviewer` agent.
- [ ] 🚫 **Task 3.2**: **HOST**: run `./run.bash --changed` for real, and read what it ran.
  `run-changed.bash` runs it, and meta-deploy carries it: meta-deploy runs in the owner's
  terminal, so run.bash's one question reaches them. Waiting on that meta-deploy run.

### Phase 4: `--rerun` and the one-row panel

The owner's call: a per-play listing in the panel drop-down is bloated and almost never
clicked. Instead one row opens a menu of the plays, where a person picks.

- [x] ✅ **Task 4.1**: `changed_plays.py --all` prints every play run here as
  `PLAY <state> <play>` (states `stale`, `failed`, `unresolved`, `current`), from the same
  judgement `--changed` uses. `run.bash --rerun` (1.29.0) shows it as a numbered menu, reads
  the pick from stdin (numbers, `a`, `q`) with strict validation and a three-try re-prompt,
  and runs the pick through the shared run loop. Tests: `test_changed_plays.py`,
  `scripts/test-run-bash-rerun.bash` (gate `run-bash-rerun`).
- [x] ✅ **Task 4.2**: The panel's plays section is one row, "Re-run a play…" (with
  "(N changed)" when any play is not fresh), launching `fedora-desktop-health --rerun --hold`, which runs the checkout's `run.bash --rerun`. The icon is unchanged: the
  section reads no check section. Tests: `tests/extensions/`.
- [x] ✅ **Task 4.3**: Docs (`docs/playbooks.md`, changelog, `QA.md`) and a pointer note in
  Plan 00109's `DESIGN-panel.md`.
- [ ] 🚫 **Task 4.4**: **HOST**: deploy `play-fedora-desktop-panel.yml` (the panel row) and
  `play-host-health-login-report.yml` (`fedora-desktop-health --rerun`), log out and in,
  open the row, and pick a play. Blocked on the owner: Ansible never runs in the ccy
  container, and the menu needs a person. Both plays ran in meta-deploy
  `20261002-094803`, `failed=0` (`changed=0`: already in place); the logout and the row
  itself remain.

## Success Criteria

- [x] After a commit that changes only a file a play deploys, `--changed` runs that play.
  Proven in two halves: `test_changed_plays.py` commits a deployed file in a real git
  checkout and the judge names the play (`test_a_play_whose_deployed_file_changed_runs`,
  `test_main_runs_the_play_whose_deployed_file_changed`); `test-run-bash-changed.bash`
  drives the real `run.bash` and runs what the judge names.
- [x] A play whose inputs did not change is not run. `test_an_unrelated_change_runs_nothing`,
  `test_an_unchanged_successful_play_is_current`, and the run.bash case "nothing changed".
- [x] A failed play stops the run, and the exit status is that play's. `test-run-bash-changed.bash`,
  case "a failure stops the run" (real `run.bash`, stubbed `ansible-playbook`).
- [x] `qa-all.bash` passes, and `qa-reviewer` findings are resolved.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00141-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan written
- `changed_plays.py` helper and `run.bash --changed` (1.28.0)
- Two `qa-reviewer` rounds resolved (eeae9b29, then run.bash 1.28.1)
