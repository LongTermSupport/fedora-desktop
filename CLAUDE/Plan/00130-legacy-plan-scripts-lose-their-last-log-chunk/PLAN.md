# Plan 00130: legacy plan scripts lose their last log chunk

**Status**: In Progress
**Created**: 2026-09-16
**Owner**: joseph
**Priority**: Medium

## Overview

`CLAUDE/PlanWorkflow.md` used to instruct every plan script to write its run log
with `mkdir -p "$PLAN_DIR/logs"` and `exec > >(tee "$LOG") 2>&1`. Both halves are
wrong, and `CLAUDE/PlanScriptStandards.md` R4 forbids them. The document has been
corrected (`78fe841a`), so no NEW script will carry the pattern — but **ten scripts
across seven still-active plans**, of 53 examined, were written from the old
instruction and still do. `triage.bash` establishes that by glob; the "nine across
eight" this plan was filed with was hand-counted and was wrong in both figures.

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
- `untracked/meta-deploy.bash` takes **one** consent for the whole batch. **It already
  does, and this plan's original claim that it did not was wrong** — see the
  correction below; the goal is kept so a future conversion cannot reintroduce the
  problem unnoticed.

## Non-Goals

- Converting scripts under `CLAUDE/Plan/Completed/`. They will not run again.
- A full `_planlib.inc.bash` conversion of each script. Several of these scripts
  hand-roll the repo-root walk, the prompts and the ansible invocation too, and R1–R14
  would have things to say about all of it. That is a larger job and each plan's owner
  should judge it; this plan fixes the defect that silently destroys evidence.

## Correction: the batch was never blocked on a prompt

This plan was filed asserting that `meta-deploy.bash` could not be one consent because a
pre-library `deploy.bash` prompts for itself, and naming Plan 00075's as the case. Both
halves are false, established by reading every in-progress plan's scripts:

| `deploy.bash`                     | sources library | actually prompts |
| --------------------------------- | --------------- | ---------------- |
| 00075                             | no              | **no**           |
| 00099, 00109, 00112, 00122, 00124 | yes             | yes              |

Plan 00075's is the **only** `deploy.bash` in the tree that prompts for nothing at all,
and every script that does prompt sources the library, so `PLAN_ASSUME_YES` answers it.
`meta-deploy.bash` was testing "does not source the library" as a proxy for "will block
on input" — so its warning fired on the one script that could not block, and was silent
about the property it claimed to check. That is this repo's recurring defect: a check
whose result is indistinguishable from not having checked. The runner now requires both
halves — a `read` prompt **and** no library — and `--list` prints no exception.

The log-chunk defect below is real and unaffected. It was simply never what stood
between the operator and a one-shot run.

## Tasks

### Phase 1: Establish

- [x] ✅ **Task 1.1**: `triage.bash` — enumerates every script under
  `CLAUDE/Plan/NNNNN-*/` carrying the pattern, and every plan-local `logs/` directory
  with whether it is empty. By glob, not hand-listed, and it states how many scripts it
  EXAMINED as well as what it found — "no occurrences" and "the glob matched nothing"
  print identically otherwise, and a leg fails when the glob matches nothing.
  Result: **10 occurrences across 10 live scripts of 53 examined** — 00062, 00066,
  00075, 00079 (×4), 00080, 00098 (×2). Three LIVE `logs/` directories remain (00079,
  00080, 00098), all non-empty; eight more are in the archived tree and out of scope.
  Comment lines are excluded, or an already-converted script that documents its own
  conversion would be counted as unconverted; the pattern's first character is bracketed
  so the scanner does not match its own text.
- [x] ✅ **Task 1.2**: Recorded per script by the same run. **None of the ten sources
  `_planlib.inc.bash`** — every one needs the R1 bootstrap first, so there is no
  one-line-swap subset and the conversion is the same shape ten times over.

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
- [x] ✅ **Task 2.4**: Was "adopt `PLAN_ASSUME_YES` so the batch's one consent covers
  these scripts". **No live `deploy.bash` needs it** — see the correction above. What the
  task actually produced is the fix to `meta-deploy.bash`'s test: it now requires a `read`
  prompt AND the absence of the library, rather than inferring one from the other.
  `./untracked/meta-deploy.bash --list` prints 9 plans, 14 units and no exception.
  Any script this plan converts inherits `PLAN_ASSUME_YES` as a side effect of gaining
  the library, so the guarantee holds through Phase 2 rather than needing separate work.

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
- [x] `./untracked/meta-deploy.bash --list` names no script the batch consent cannot
  answer — the batch is one consent, as the operator asked for. Met, and the criterion
  now tests the property rather than a proxy for it
- [ ] The new gate fails on a re-introduced occurrence and passes on the clean tree
- [ ] QA passes (`./scripts/qa-all.bash`)
- [ ] `qa-reviewer` returns PASS

## Delivery & Milestones

- Filed from Plan 00099's round-3 closing work, which fixed `PlanWorkflow.md`
  (`78fe841a`) and found the instruction had already been followed nine times.
