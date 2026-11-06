# Plan 00250: branches cleaned up when done

**Status**: In Progress
**Created**: 2026-11-06
**Owner**: joseph
**Priority**: High

## Overview

A branch whose work is done must be deleted, with its worktree, as part of finishing it. In this
repository nothing did that. The daemon's WIP cap counted 213 open work branches against a cap
of 10. `TRIAGE.md` has the facts and their three causes:

1. An ad-hoc script ran git mutations across every worktree, out of sight of the command guards.
2. A merged branch, local or remote, is never deleted by anything.
3. Sub-agent scratch is kept inside the worktree it must not outlive.

This plan builds a defence for each class first (Defence Before Fix), then cleans up every
instance through the normal tools, with no bypass. A cause that sits in the hooks daemon is
fixed there.

## Goals

- A merged branch is deleted by the merge itself, locally and on origin, with its worktree.
- No agent can run git mutations the command guards cannot see.
- A sub-agent's worktree holds nothing a removal would lose once its branch is merged.
- Open work below the cap of 10, reached through `branch-sweep --clean` and `delete-branch`,
  never through `git branch -D` or a forced removal.

## Non-Goals

- Changing the cap (10) or loosening any branch guard.
- Deciding the content of an unmerged branch without a review: each one gets an outcome.

## Tasks

### Phase 1: defences

- [ ] 🔄 **Task 1.1**: `branches.cleanup_after_merge.mode: act`, so a merge into F44 deletes the
  merged local branch. Set, and the daemon restarted. Still to do: prove it on a throwaway
  branch, which the WIP cap refuses to create until open work is under 10.
- [ ] ⬜ **Task 1.2**: Remote deletes after a local merge. Today `cleanup_after_merge` prints the
  remote delete but never runs it. Decide between (a) a daemon change that runs the delete
  for a branch merged into the default branch, which the remote guard already allows, and
  (b) a coordinator merge tool that merges, pushes and deletes. File (a) upstream with
  `/hooks-daemon issue-report` if chosen.
- [ ] ⬜ **Task 1.3**: Hidden git mutations (TRIAGE cause 1). Defence Before Fix: name the
  class, write the detector (a hooks-daemon guard that refuses to run a script file holding
  git mutation verbs unless it is a tracked tool), prove it red on `rescue.bash`, and make it
  blocking. The guard belongs upstream; file it with `issue-report`, and add a project
  handler under `.claude/hooks/handlers/` until it ships.
- [ ] ⬜ **Task 1.4**: Sub-agent scratch (TRIAGE cause 3). Make a worktree's scratch survive its
  removal by design. One option is to seed `untracked/scratch/` as a symlink to the main
  checkout's (worktree seed config). Another is to send reports to the main checkout, as
  `subagent_report_persistence` already does. Choose one, and prove that a merged agent
  worktree is then removed by `branch-sweep --clean`.

### Phase 2: clean up every instance, through the tools

- [ ] ⬜ **Task 2.1**: The eight worktrees with a script-damaged index: put each index back to
  HEAD (`git reset` without `--hard` touches only the index), confirm `git status` is clean
  and the files on disk match HEAD, then let `branch-sweep --clean` remove them.
- [ ] ⬜ **Task 2.2**: The eleven agent branches made unmerged by a rescue commit: read each
  commit. Content that belongs in a plan is committed there. Then the branch either goes back
  to merged (its own lane's commit), or gets an outcome in Task 2.3.
- [ ] ⬜ **Task 2.3**: Every remaining unmerged branch, local and on origin, gets an outcome:
  merged, content already in F44 (deleted behind `delete-branch`'s proof), superseded
  (archived, then deleted with the owner's consent at a terminal), or parked with a reason
  (`branch-park`). Plan 00245's `BRANCH-OUTCOMES` method, done by a reviewer agent.
- [ ] ⬜ **Task 2.4**: Origin's merged and patch-equivalent work branches deleted. Merged ones
  are deleted by the coordinator; the ones that are only patch-equivalent are the owner's.

### Phase 3: acceptance

- [ ] ⬜ **Task 3.1**: `branch-sweep` reports open work at or under 10, with no merged branch
  left. The qa-reviewer agent reviews the plan's changes.

## Success Criteria

- [ ] A merge into F44 leaves no merged branch or worktree behind, without anyone running a delete.
- [ ] The hidden-mutation guard refuses `rescue.bash` and passes a tracked tool.
- [ ] Open work at or under the cap, reached with no forced removal and no unproven delete by an agent.

## Delivery & Milestones

- Triage: `TRIAGE.md`.
