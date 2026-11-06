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

- [ ] 🔄 **Task 2.1**: Build the in-repo defence the design recommends, red then green, blocking.
  Done: the detector (L2 files, L3 unpushed work), `scripts/qa-untracked-audit.bash`, proven red
  by fixtures and a mutation, run on the real checkout
  ([report](subagent-reports/261103-untracked-audit.md)). Not done: blocking. It is red on the
  pile, so it waits in the "red until swept" section of `CLAUDE/QA.md`; its row in
  `scripts/qa-gates.tsv` is added when Task 2.2 empties the pile. L4 to L6 remain.
- [ ] 🔄 **Task 2.2**: Move every VALUABLE-UNTRACKED item to its tracked home (anonymised
  where needed), then delete the untracked copy. Second sweep filed 163 files, guarded; the
  owner's list of what stays is in
  [the report](subagent-reports/261103-untracked-sweep-2.md). Deleting the untracked copies
  waits for the owner. Rescue and purge ran
  ([report](subagent-reports/261104-wip-rescue-purge.md)): 34252 files deleted, 7 branches and 39
  worktrees pushed. Wiring the audit as blocking is blocked on the owner's decision for what is
  left: 8 worktrees the commit hook refuses, 108 decision and 33 patch findings, the items
  listed for the owner, and the nested checkouts.
- [x] Task 2.3: the bulk-delete guard (issue #150, after F42 and F43 were deleted from a
  triage's verdicts). `scripts/branch-delete-list.bash` (Go, `src-go/qa/branchdelete`) refuses
  any exempt, parked, `F[0-9]*`, default or unarchived name and prints the delete commands only
  when all pass; `CLAUDE/QA.md` names it as the only way to build a delete list
  ([report](subagent-reports/261105-delete-guard.md)).

### Phase 3: the private-name guard

A sweep agent committed and pushed about 200 files to a public branch that named a client team,
its seats and its containers 22 times. The commit hook missed it: its private-identifier
denylist is built only from the gitignored `localhost.yml`, which is absent in the container
where agents commit, and which never held team, seat or container names. The class:
install-specific identifiers reaching a commit because no detector knows them.

- [x] Task 3.1: the term list. `src-go/qa/privatenames` learns the names from `pingbus status`
  and the machine's host names into `<git dir>/private-terms` (mode 0600, never tracked), keeps
  the terms of teams that left, and never prints a term. Short and generic terms are dropped
  by rule ([scripts/git-hooks/README.md](../../../scripts/git-hooks/README.md)).
- [x] Task 3.2: the hooks. `pre-commit` scans the added lines of the staged change; the new
  `pre-push` scans the message and added lines of every commit being pushed. In a CCY container a
  list that cannot be built is exit 2, never a skip.
- [x] Task 3.3: red proof. Run over the leaking commit, the rule reports 60 findings in 24 files.
- [x] Task 3.4: sweep of the default branch (329 findings in 98 files): 133 occurrences in 53
  current files replaced by placeholders, 202 findings left for the owner (journals, a product
  name that is also a play file name, one CCY-image helper)
  ([report](subagent-reports/261103-private-name-guard.md)).
- [x] Task 3.5: the owner decided the left-overs (product name allowed, journals redacted in
  place, history left); the whole-tree scan reports 0 findings and joins `scripts/qa-gates.tsv`
  as the `private-names` gate ([report](subagent-reports/261103-private-name-guard.md)).

## Success Criteria

- [ ] The triage's VALUABLE-UNTRACKED list is empty.
- [ ] Writing an asset under an untracked path is refused by a blocking check.

## Delivery & Milestones

- Plan opened.
