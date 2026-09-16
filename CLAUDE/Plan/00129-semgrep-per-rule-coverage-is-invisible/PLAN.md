# Plan 00129: The pattern gate reports a union and reads as per-rule coverage

**Status**: Not Started
**Created**: 2026-09-16
**Owner**: joseph
**Priority**: Medium

## Overview

`./scripts/qa-all.bash` prints `✓ patterns: N files OK`. That N is the **union** of
every rule's target set, and it reads as *"every rule ran on N files"*. Only one rule
ever did: `bash-capture-discards-status` — the rule for the exact defect class Plan
00075 exists to catch — was measured blind to **67 of 157 files**, while two more rules
saw 123 and only one saw all 157.

**The narrowing is deliberate and recorded in each rule.** What is wrong is the pass
line, which does not say so, and therefore claims a coverage nobody has. That is the
same shape as Plan 00076's founding defect — a number that reads as coverage and is not
— found while closing it, outside every one of its twelve success criteria, and a
feature rather than a fix. Hence this plan rather than holding a finished one open.

The per-rule measurements, the options, the rejected approaches and the falsification
trap are all in Plan 00076's Task 4.5 and its 26-09-16 journal:
[../Completed/00076-bash-gate-coverage-hole-nonexecutable-scripts/PLAN.md](../Completed/00076-bash-gate-coverage-hole-nonexecutable-scripts/PLAN.md).
Nothing below re-derives them.

## Goals

- The pattern gate reports **per-rule** coverage, measured rather than modelled.
- A rule whose target set silently shrinks becomes visible in the gate's output.

## Non-Goals

- Widening any rule's `paths.include`. What each rule looks at is its own decision;
  this plan is about reporting it truthfully.
- Changing the gate's verdict. A rule that ran on fewer files is not a failure — it is
  a fact the gate currently omits.

## Context & Background

Established under Plan 00076, so that it is not re-litigated:

- **Option A — one scan per rule.** Measures. A single-rule scan's `paths.scanned` does
  discriminate. Cost re-measured at **4.9×** this gate's scan time, +7.3s over 150
  files, on every QA run.
- **Option B — parse `paths.include` and match the globs in-gate.** No extra scan, but
  it *models* semgrep's targeting rather than measuring it.
- **The objection that was supposed to separate them does not.** `--include-rule-id`
  does not exist, so A must split the ruleset into temporary single-rule configs — so A
  needs a YAML parser too, which was B's recorded blocker. PyYAML ships with ansible,
  which this repo's QA already requires, so it is void for both. The alternative — five
  hand-maintained config files beside one ruleset — is the two-things-that-must-agree
  hazard this repo keeps being bitten by, so it is not an option.
- **`--time` and `--x-ls` are confirmed dead ends**, each checked by measurement rather
  than by reading the docs.

The recorded reading is **A**, on the ground that a modelled coverage number is worth
less than a measured one in a repo whose recurring defect is false coverage claims.

## Tasks

### Phase 1: The decision

- [ ] ⬜ **Task 1.1**: **OWNER DECISION — A or B.** All that is left is the price:
  ~4.9× on this gate's scan time, every QA run, for coverage numbers that are true
  instead of uniform

### Phase 2: Implementation

- [ ] ⬜ **Task 2.1**: Implement the chosen option in the pattern gate, reporting
  per-rule coverage in the pass line
- [ ] ⬜ **Task 2.2**: A test that FAILS against the current union-reporting gate — and
  **mind the corpus**. A test built on `files/` or `scripts/` cannot discriminate,
  because both are in every rule's include set, and `helpers/` cannot either because it
  is Python and contributes no bash. Only `CLAUDE/Plan/**` or `extensions/**` span the
  difference. A test on the wrong corpus passes while reporting uniform coverage — the
  exact defect this plan exists to remove, and Plan 00076 fell into it twice
- [ ] ⬜ **Task 2.3**: `CLAUDE/QA.md` says what the pattern gate's number now means

### Phase 3: Close

- [ ] ⬜ **Task 3.1**: `qa-reviewer` over the full plan diff, findings resolved

## Dependencies

- Supersedes Plan 00076's Task 4.5; that plan is Complete

## Success Criteria

- [ ] The gate reports per-rule coverage, not a union
- [ ] A rule narrowed in the ruleset changes the gate's reported numbers
- [ ] The test that proves it fails against the pre-change gate
- [ ] `./scripts/qa-all.bash` passes
- [ ] No release-bound consequences

## Risks & Mitigations

| Risk                                           | Impact | Probability | Mitigation                                                         |
| ---------------------------------------------- | ------ | ----------- | ------------------------------------------------------------------ |
| +7.3s on every QA run trains people to skip it | M      | M           | Task 1.1 is the owner deciding exactly this; B costs nothing extra |
| The test corpus cannot discriminate            | H      | H           | Named in Task 2.2 — only two trees span the split                  |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00129-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Carried over from Plan 00076 Task 4.5, which was measured to a decision point and
  then held a finished plan open
