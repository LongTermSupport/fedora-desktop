# Plan 00069: PLAN.md is edited in place, not appended to

**Status**: Not Started
**Created**: 2026-07-31
**Owner**: joseph
**Priority**: Medium

## Overview

Plan 00068's `PLAN.md` reached **1,894 lines / 137 KB**. Measured: **50% of it is blockquote
correction commentary**, **2% is task checkboxes** (56 lines), and its `## Success Criteria`
section alone is 314 lines. It grew across 34 commits from 42 KB, and **not one commit ever made
it smaller**.

The cause is a convention **invented inside Plan 00068 and imposed on itself**. Its Task 6.3 says:
*"Corrections **append**; never rewrite a section a reviewer has already reviewed, or their finding
stops referring to a real document."* Seven audit rounds then produced 27 append-only correction
blocks — 953 lines of commentary wrapped around 56 checkboxes.

**No project rule ever said this.** Every append-only statement in the tracked docs scopes itself
to `JOURNAL/` — `CLAUDE/PlanJournalling.md:72-78` ("A journal is append-only… Corrections are new
entries") and `CLAUDE/PlanWorkflow.md:230`,`:235`, which mention it only to describe journals.
Nothing applies it to `PLAN.md`.

And the justification was already solved: **`PLAN.md` is a living, git-tracked document.** A
reviewer's finding does not stop referring to real text when the document is edited — it refers to
a commit, and git holds every prior state. Appending to preserve history duplicates what version
control exists to do, at the cost of a document nobody can read.

This is Plan 00068's own signature failure mode, one more time and at the level of its process
rather than its content: a true statement (`JOURNAL/` is append-only) promoted into a stronger
statement about the world (therefore `PLAN.md` must grow too).

The second-order cost is worse than the size. Corrections landed hundreds of lines from the tasks
they govern, so keeping the document coherent required a manual `grep` after every edit. Plan 00068
recorded **fifteen** failures of exactly that discipline — including two committed *while
cataloguing the defect*, and three found in ten minutes after the plan had already passed seven
hostile review rounds. A rule that reliable at being forgotten is a defect report about the method,
not about the people following it.

## Goals

- State in `CLAUDE/PlanWorkflow.md`, in as few words as it takes: **`PLAN.md` is a living,
  git-tracked document — edit it in place. Git is the history. Do not append corrections to it.**
  Say explicitly that append-only is a `JOURNAL/` rule and does not extend to `PLAN.md`, since one
  plan has already generalised it and nothing contradicted that for 34 commits.
- Say where narration goes instead: `JOURNAL/`, which is append-only and where size does not
  matter. A correction worth *explaining* is a journal entry; the task text itself just gets fixed.
- Make the coherence checks **mechanical rather than remembered** — a ✅ parent with a ⬜ child, or
  a body reading DEFERRED above a ⬜ box, should be caught by a tool.

## Non-Goals

- **Rewriting or shrinking Plan 00068.** Its bulk *is* its audit trail, and that trail is why the
  design is trustworthy — it invalidated the original thesis, killed two decisions, and caught
  fifteen propagation defects. The distilled version already exists at
  `CLAUDE/Plan/00068-ccy-ci-runner-variant/reports/one-page-restatement.md` (83 lines). Nothing
  here touches 00068.
- **Weakening the append-only rule for `JOURNAL/`.** It is correct there and stays untouched.
- Changing anything under `.claude/hooks-daemon/` — upstream, must not be edited in this repo.

## Tasks

### Phase 1: Write the rule down where it is enforced

- [ ] ⬜ **Task 1.1**: Add a short section to `CLAUDE/PlanWorkflow.md`: `PLAN.md` is a living
  git-tracked document, **edited in place**; git carries the history; append-only is `JOURNAL/`'s
  rule and stops there. Cite 00068's measured numbers so the rule carries its own evidence.
- [ ] ⬜ **Task 1.2**: Check whether `CLAUDE/PlanJournalling.md` and `CLAUDE/Plan/CLAUDE.md` invite
  the over-generalisation. `PlanJournalling.md:72-78` states the journal rule correctly but never
  says it is *only* a journal rule — which is how a whole plan came to apply it to `PLAN.md`
  unchallenged. State the boundary once, in one place, and link rather than restate.

### Phase 2: Make it mechanical

- [ ] ⬜ **Task 2.1**: Decide where the coherence check can live. The plan-QA catalogue is in
  `.claude/hooks-daemon/`, which this repo must not edit — so this is either an upstream
  contribution or a local `scripts/` gate wired into `qa-all.bash`. **Decide before building.**
- [ ] ⬜ **Task 2.2**: Implement the checks chosen in 2.1: ✅ parent with a ⬜ child; a ⬜ box whose
  body says DEFERRED. A raw size threshold is **not** in the set — size was the symptom, appending
  was the cause, and a line-count gate would nag correctly-written long plans while saying nothing
  about the defect that produced this one.
- [ ] ⬜ **Task 2.3**: Prove each check DISCRIMINATES, not merely that it fires — a fixture that
  should pass and one that should fail, per `.claude/rules/bash-standards.md` §9. A gate that
  flags everything is as useless as one that flags nothing.

## Success Criteria

- [ ] `CLAUDE/PlanWorkflow.md` says `PLAN.md` is edited in place and that append-only stops at
  `JOURNAL/`. A reader who has just read `PlanJournalling.md` cannot come away believing what
  Plan 00068 believed.
- [ ] The coherence checks exist, run in `qa-all.bash` (or are filed upstream), and each is proven
  against a passing **and** a failing fixture.
- [ ] Re-running the checks against Plan 00068 reproduces D26, D27 and D28 — the three defects
  found by hand. If it does not, the checks do not encode what was actually learned.

## Delivery & Milestones

- Created from Plan 00068's measured bloat; see that plan's `JOURNAL/00068-Journal-26-07-31.md`
  entries at 05:10 and later for the three defects that motivated the mechanical checks.
