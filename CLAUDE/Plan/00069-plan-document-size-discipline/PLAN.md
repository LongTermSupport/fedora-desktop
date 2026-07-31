# Plan 00069: plan document size discipline

**Status**: Not Started
**Created**: 2026-07-31
**Owner**: joseph
**Priority**: Medium

## Overview

Plan 00068's `PLAN.md` reached **1,894 lines / 137 KB**. Measured: **50% of it is blockquote
correction commentary**, **2% is task checkboxes** (56 lines), and its `## Success Criteria`
section alone is 314 lines. It grew across 34 commits from 42 KB, and **not one commit ever made
it smaller**.

That is not carelessness, and this plan will not fix it by asking anyone to try harder. It is the
predictable output of a rule the project already has and which is *correct in its own place*:
**corrections APPEND, never rewrite**, so that line-number citations stay valid. Applied to a
journal that is right. Applied to the task document it makes the file monotonically increasing in
review effort — seven hostile audit rounds produced 27 correction blocks and 953 lines of
commentary wrapped around 56 checkboxes.

The second-order cost is worse than the size. Corrections landed hundreds of lines from the tasks
they govern, so keeping the document coherent required a manual `grep` after every edit. Plan 00068
recorded **fifteen** failures of exactly that discipline — including two committed *while
cataloguing the defect*, and three found in ten minutes after the plan had already passed seven
hostile review rounds. A rule that reliable at being forgotten is a defect report about the method,
not about the people following it.

## Goals

- State in `CLAUDE/PlanWorkflow.md` where corrections belong: **`JOURNAL/` (append-only, size
  irrelevant)**, not `PLAN.md` (edited in place, kept small).
- Give `PLAN.md` a **size budget** with a stated remedy when it is exceeded (split the plan), so
  growth is a trigger rather than a trend nobody is watching.
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

- [ ] ⬜ **Task 1.1**: Add a "keep `PLAN.md` small" section to `CLAUDE/PlanWorkflow.md` — where
  corrections go, the size budget, and the split remedy. Cite 00068's measured numbers so the rule
  carries its own evidence.
- [ ] ⬜ **Task 1.2**: Reconcile the surrounding docs so one rule is stated once —
  `CLAUDE/Plan/CLAUDE.md` and `CLAUDE/PlanJournalling.md` currently describe the correction
  convention without saying which document absorbs it.

### Phase 2: Make it mechanical

- [ ] ⬜ **Task 2.1**: Decide where the coherence check can live. The plan-QA catalogue is in
  `.claude/hooks-daemon/`, which this repo must not edit — so this is either an upstream
  contribution or a local `scripts/` gate wired into `qa-all.bash`. **Decide before building.**
- [ ] ⬜ **Task 2.2**: Implement the checks chosen in 2.1: ✅ parent with a ⬜ child; a ⬜ box whose
  body says DEFERRED; `PLAN.md` over budget.
- [ ] ⬜ **Task 2.3**: Prove each check DISCRIMINATES, not merely that it fires — a fixture that
  should pass and one that should fail, per `.claude/rules/bash-standards.md` §9. A gate that
  flags everything is as useless as one that flags nothing.

## Success Criteria

- [ ] `CLAUDE/PlanWorkflow.md` states where corrections go and what the size budget is.
- [ ] The three coherence checks exist, run in `qa-all.bash` (or are filed upstream), and each is
  proven against a passing **and** a failing fixture.
- [ ] Re-running the checks against Plan 00068 reproduces D26, D27 and D28 — the three defects
  found by hand. If it does not, the checks do not encode what was actually learned.
- [ ] This plan's own `PLAN.md` is still under the budget it sets.

## Delivery & Milestones

- Created from Plan 00068's measured bloat; see that plan's `JOURNAL/00068-Journal-26-07-31.md`
  entries at 05:10 and later for the three defects that motivated the mechanical checks.
