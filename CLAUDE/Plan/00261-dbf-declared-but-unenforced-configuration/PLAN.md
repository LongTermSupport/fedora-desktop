# Plan 00261: dbf declared but unenforced configuration

**Status**: Not Started
**Created**: 2026-11-20
**Owner**: joseph
**Priority**: Medium

## Overview

A configuration field that a reader accepts and validates but no code path acts on is worse than a
missing one: the owner sets it, nothing refuses it, and the behaviour it promises never happens.
Two studied projects carry this defect (a policy stored but never consulted; schema fields that are
validated, stored and inert). Our exposure is the team record and member config of ccy-teams and
the agent bus: role contracts such as Plan 00207's communication gates and minimum models are
fields a reader will accept before every enforcement path exists.

Under Defence Before Fix this is a defect class, so it gets a detector before any instance is
fixed: an analyzer in `src-go/qa/rules` that requires each team-record and member-config field to
have a consumer outside its parser and validator. Plan 00192 holds the general goal of project DBF
rules for defect classes found in the Go ports; this plan is one named class with its own design
question, so it stands alone and adds its rule to that catalogue.

**The question to decide:** what counts as a consumer (a read outside the package that parses the
struct; a read outside parse and validate functions; an explicit allow mark for fields that are
deliberately pass-through), and which structs are in scope.

## Goals

- A blocking analyzer in `src-go/qa/rules` that fails when a field of an in-scope config struct is
  read only by its parser or validator, proved red on a fixture and on any real instance.
- Every instance the sweep finds is fixed (wired to its consumer, or removed), or listed for the
  owner's decision; none is suppressed by the agent.
- The rule and its red/green proof are catalogued in `CLAUDE/GoProjectRules.md`.

## Non-Goals

- Config outside Go (Ansible variables, bash env files); a later class if this one proves useful.
- Designing the missing enforcement for Plan 00207's fields; that stays in Plan 00207.

## Tasks

### Phase 1: Define the class

- [ ] ⬜ **Task 1.1**: List the in-scope structs (team record, member config, seat bundle) and,
  by reading, each field's consumers; record the list in a supporting document
- [ ] ⬜ **Task 1.2**: Owner decision on the consumer definition and the allow mark for
  pass-through fields; recorded under Technical Decisions

### Phase 2: Defence Before Fix

- [ ] ⬜ **Task 2.1**: The analyzer with its testdata, red on a fixture field that is only parsed
- [ ] ⬜ **Task 2.2**: Sweep the repository; fix or list every instance
- [ ] ⬜ **Task 2.3**: Make the rule blocking in the Go gate; catalogue entry in
  `CLAUDE/GoProjectRules.md`

## Dependencies

- Related: Plan 00192 (DBF rule catalogue for the Go ports), Plan 00191 (Go CLI tooling and its
  QA gate), Plan 00207 (team roles and minimum models, the fields most exposed)

## Success Criteria

- [ ] The analyzer is blocking in QA and its red/green proof is recorded
- [ ] No in-scope field is read only by its parser or validator without an owner-approved mark
- [ ] QA passes; `qa-reviewer` run as the final step

## Sources

Watch-list run `baseline-20261120-1050` (Plan 00258), in `CLAUDE/WatchlistRuns/baseline-20261120-1050/`:

- `SYNTHESIS.md` section 4 "New plans" 4
- `teams.md` "Group section: the strongest takeaways, ranked" 3; the open-managed-agents and
  openrig sections

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00261-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Not started
