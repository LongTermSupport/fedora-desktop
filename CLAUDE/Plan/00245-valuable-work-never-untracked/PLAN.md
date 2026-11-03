# Plan 00245: valuable work never untracked

**Status**: In Progress (triage of `untracked/` and the defence design are running)
**Created**: 2026-11-03
**Owner**: joseph
**Priority**: High

## Overview

The owner (2026-11-03): "untracked scratch is for true scratch - temp message writing etc. IT
IS NEVER SOMEWHERE TO LOG ACTUAL WORK OR ANY VALUABLE ASSETS... Any creation of untracked
assets needs to be assessed somehow. How can we build a proper defence."

**The defect class:** valuable work (research, reviews, decisions, owner sheets, rescued
work, reports) is written to an untracked or gitignored place and then lost. Untracked
places include `untracked/`, `/tmp`, a worktree's ignored files, a stash and a patch file.
Nothing lists such work, CI never sees it, it never reaches GitHub, and a cleanup or a lost
container deletes it.

Real incidents this week:

- permission-variable research and a pending owner decision existed only in
  `untracked/scratch` (now issue #127);
- a worker saved a diagnostic as a patch in scratch and force-removed its worktree;
- rescued branch work was put in `untracked/wip-rescue`;
- the owner-decision sheet lives in `untracked/`.

This is Defence Before Fix: name the class, build the detector, sweep, make it blocking,
then fix the instances. Branch hygiene (the same loss through forgotten branches) is
covered by the branch-hygiene CI job and hooks-daemon issue
Edmonds-Commerce-Limited/claude-code-hooks-daemon#152.

## Goals

- Nothing of value lives only under an untracked path.
- Creating an untracked file is judged when it happens: scratch passes, an asset is refused
  with the tracked home it belongs in.

## Non-Goals

- Banning scratch files: commit-message and issue-body drafts consumed in the same turn stay.

## Tasks

### Phase 1: assess

- [x] ✅ **Task 1.1**: Triage everything under `untracked/` (Sonnet):
  [RESEARCH-untracked-triage.md](RESEARCH-untracked-triage.md): 237 valuable files with no
  tracked copy (rescued Go code, about 137 saved sub-agent reports, about 52 specs and reviews, owner
  sheets); the biggest source is the daemon's own saved replies.
- [x] ✅ **Task 1.2**: Design the defence layers, here and upstream (Opus):
  [DESIGN-defence.md](DESIGN-defence.md): the class (an unrecorded asset), nine layers, the
  recommendation (consumption and age gates, then a write-time guard) and the upstream text.
- [x] ✅ **Task 1.3**: Filed as Edmonds-Commerce-Limited/claude-code-hooks-daemon#155, with the
  seven requested changes as its first comment.

### Phase 2: defend and sweep

- [ ] ⬜ **Task 2.1**: Build the in-repo defence the design recommends, red then green, blocking.
- [ ] ⬜ **Task 2.2**: Move every VALUABLE-UNTRACKED item to its tracked home (anonymised
  where needed), then delete the untracked copy.

## Success Criteria

- [ ] The triage's VALUABLE-UNTRACKED list is empty.
- [ ] Writing an asset under an untracked path is refused by a blocking check.

## Delivery & Milestones

- Plan opened.
