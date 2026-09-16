# Plan 00130: legacy plan scripts lose their last log chunk

**Status**: Not Started
**Created**: 2026-09-16
**Owner**: joseph
**Priority**: Medium

## Overview

`CLAUDE/PlanWorkflow.md` used to instruct every plan script to write its run log
with `mkdir -p "$PLAN_DIR/logs"` and `exec > >(tee "$LOG") 2>&1`. Both halves are
wrong, and `CLAUDE/PlanScriptStandards.md` R4 forbids them. The document has been
corrected (`78fe841a`), so no NEW script will carry the pattern — but nine scripts
across eight still-active plans were written from the old instruction and still do.

A `>(…)` process substitution cannot be waited on. The shell exits, the `tee` is
still draining, and the final buffered chunk can be lost — which is the part of the
file a failed run is read for. A script can therefore die, and its log can end
mid-sentence before saying why, with nothing anywhere indicating the report is
truncated. Second, a plan-local `logs/` tree is gitignored, so `git mv` into
`Completed/` leaves it behind as an untracked orphan at the old path; Plan 00099 had
exactly that, an empty file in a directory nothing would ever remove.

This plan converts the live ones. Scripts under `CLAUDE/Plan/Completed/` are
deliberately out of scope: they will not run again, and editing an archived plan's
tooling makes its recorded history disagree with what it ran.

## Goals

- Every plan script under `CLAUDE/Plan/NNNNN-*/` that writes a run log uses
  `plan_start_log auto`, and none creates a plan-local `logs/` directory.
- A gate fails on a re-introduction, so this cannot be a one-off tidy that decays —
  the pattern came back once already, from a document that has since been fixed.
- No plan-local `logs/` directory remains in the active plan tree.
- `untracked/meta-deploy.bash` takes **one** consent for the whole batch. Today it
  cannot: a pre-library `deploy.bash` prompts the operator itself, and the runner has to
  flag it as an exception the batch consent does not answer. Plan 00075's is the current
  case, and it is the same file this plan is already converting — `PLAN_ASSUME_YES` is
  part of the library these scripts do not use. The user asked for a one-shot run; a
  script that stops the batch to ask its own question is what stands in the way.

## Non-Goals

- Converting scripts under `CLAUDE/Plan/Completed/`. They will not run again.
- A full `_planlib.inc.bash` conversion of each script. Several of these scripts
  hand-roll the repo-root walk, the prompts and the ansible invocation too, and R1–R14
  would have things to say about all of it. That is a larger job and each plan's owner
  should judge it; this plan fixes the defect that silently destroys evidence.

## Tasks

### Phase 1: Establish

- [ ] ⬜ **Task 1.1**: `triage.bash` — enumerate every script under
  `CLAUDE/Plan/NNNNN-*/` matching `exec > >(tee`, and every existing plan-local
  `logs/` directory with whether it is empty. Enumerated by glob, not hand-listed, so
  the count cannot go stale. Known at filing: 00062, 00066, 00075, 00079 (×4), 00080,
  00098 (×2).
- [ ] ⬜ **Task 1.2**: For each, record whether it already sources
  `_planlib.inc.bash` — the conversion is a one-line swap where it does and a
  bootstrap where it does not.

### Phase 2: Convert

- [ ] ⬜ **Task 2.1**: Convert each script to `plan_start_log auto`, removing the
  `LOG=`/`mkdir -p` lines **and any consumer of `$LOG`**. That last part is not
  optional: the identical conversion in Plan 00099 removed `LOG=` and left one
  `echo "Full report: $LOG"`, and under `set -u` the script then died on its own last
  line on every run — `shellcheck -x` CLEAN and `qa-all.bash` green throughout,
  because no gate executes plan scripts.
- [ ] ⬜ **Task 2.2**: Run each converted script far enough to prove it reaches its
  own last line. Linting is exactly what missed this class before.
- [ ] ⬜ **Task 2.3**: Remove the orphaned plan-local `logs/` directories.
- [ ] ⬜ **Task 2.4**: Where a converted script also prompts for its own consent, adopt
  the library's `PLAN_ASSUME_YES` / `plan_gate_change` vocabulary so the batch runner's
  single consent covers it. Verify with `./untracked/meta-deploy.bash --list`, which
  names each pre-library script it cannot answer for — that list should empty out.

### Phase 3: Make it stick

- [ ] ⬜ **Task 3.1**: A QA gate rejecting `exec > >(tee` and plan-local `logs/` under
  `CLAUDE/Plan/NNNNN-*/`, wired into `qa-all.bash`. It must be falsified against a
  deliberately re-introduced occurrence, and against a clean tree, before it is trusted.
- [ ] ⬜ **Task 3.2**: Decide whether `.gitignore`'s `CLAUDE/Plan/**/logs/` entry stays.
  It stops an accident reaching the public repo, which is worth keeping; but it is also
  what made the orphan invisible. Both readings are defensible — record which and why.

## Success Criteria

- [ ] No script under `CLAUDE/Plan/NNNNN-*/` contains `exec > >(tee`
- [ ] No `logs/` directory remains under `CLAUDE/Plan/NNNNN-*/`
- [ ] Each converted script has been RUN and reaches its last line
- [ ] `./untracked/meta-deploy.bash --list` names no pre-library script — the batch is
  one consent, as the operator asked for
- [ ] The new gate fails on a re-introduced occurrence and passes on the clean tree
- [ ] QA passes (`./scripts/qa-all.bash`)
- [ ] `qa-reviewer` returns PASS

## Delivery & Milestones

- Filed from Plan 00099's round-3 closing work, which fixed `PlanWorkflow.md`
  (`78fe841a`) and found the instruction had already been followed nine times.
