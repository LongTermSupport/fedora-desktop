# Plan 00180: a pragmatic QA and merge gate for pull requests merged from a desktop

**Status**: Not Started
**Created**: 2026-10-13
**Owner**: joseph
**Priority**: Medium

Addresses #94.

## Overview

Pull requests to this repository are reviewed and merged only from a desktop session (#94).
So the merge gate may include checks that need a desktop, such as the VM-lab scenarios
(`vmtest`), and does not have to fit a CI runner. Nothing written says which checks gate a
merge. Without that, a docs-only change and a provisioning change are either held to the
same cost or to no stated standard, and a reviewer cannot see what a merge was gated on.

#94 names the starting point: the default branch's QA workflow was red for reasons no
change introduced (#93). That cause has since been dealt with on `F44` (gitleaks fixture
built from parts, root-only `agent_bus` tests skipped when unprivileged; QA green on
`F44` since 2026-10-12). This plan therefore starts from a green branch. Its first task
confirms that, and does not rebuild that work.

## Goals

- A written gate: which checks block a merge, which run in CI, and which only on a desktop.
- A change-scope rule (by touched paths) that picks a cheap path for trivial changes (docs,
  comments, small config edits) and the full scenario matrix for provisioning logic.
- A record of the checks a merging session ran, visible to a reviewer on the PR.

## Non-Goals

- Rewriting `qa-all.bash` or the `qa.yml` workflow beyond what the gate needs.
- Automating merges: the merge stays a person's action from a desktop.
- The release flow (Plan 00153).

## Tasks

### Phase 1: Triage

- [ ] ⬜ **Task 1.1**: `triage.bash`: the recent `qa.yml` conclusions on `F44`, the current
  `qa-all.bash` gates, and the `vmtest` scenarios with what each one needs (desktop, VM,
  network). Confirm the #93 causes stay gone.

### Phase 2: Design and owner decisions (decision gate)

- [ ] ⬜ **Task 2.1**: Draft `DESIGN.md`: the check inventory split into CI, desktop-only
  and optional; the path-to-scope rule; the full path; how the record is written.
- [ ] ⬜ **Task 2.2**: Owner decides the gate, the scope rule and the record's form.

### Phase 3: Build

- [ ] ⬜ **Task 3.1**: A tested script that maps a PR's changed paths to the required check
  set, with the rule in one data file.
- [ ] ⬜ **Task 3.2**: A merge helper that runs the selected set from a desktop and writes the
  record to the PR (or the merge commit's message) so a reviewer can see it.
- [ ] ⬜ **Task 3.3**: Document the gate in `CLAUDE/QA.md` and `docs/`; a `qa-reviewer` pass.

## Success Criteria

- [ ] A docs-only PR is gated by the cheap path, and a provisioning PR by the full matrix,
  chosen by the script, not by memory.
- [ ] Every merged PR shows which checks gated it.
- [ ] The rule lives in one place and is documented.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00180-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed from #94.
