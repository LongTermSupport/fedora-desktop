# WIP rescue and purge (Plan 00245, 2026-11-04)

Owner decision: rescue WIP, then purge the old scratch, then the check blocks. Counts only; no
file or folder name from `untracked/` appears here.

## Phase A: rescue

- 47 dirty worktrees were listed. The two live ones were excluded by name and were not dirty;
  none had a file modified in the last 6 hours.
- 39 were committed (WIP rescue subject) and pushed: 22 on their own branch, 17 were on a
  detached HEAD and were pushed to new `wip-rescue/<worktree-dir>` branches (a push needs a name).
- 6 of the 39 large ones carried thousands of tracked-file deletions (a worktree whose checkout
  was emptied). Those were committed with `git add --ignore-removal`: the deletions hold no
  work, HEAD still has the files, nothing is lost.
- 8 were refused by the commit hook: its Go dependency gate cannot build in a worktree whose
  `src-go` is gone. Not bypassed, content untouched. They stay dirty for the owner.
- All 7 unpushed branches were pushed. No worktree or branch was deleted.

## Phase B: purge

The list came from the check's own untracked-asset and untracked-unshaped output. Kept:
tool and daemon stores, the sweep's "left for the owner" items, tool-owned files, anything
modified in the last 24 hours, and everything under a nested git checkout (25158 files: these
may hold unpushed work). 34252 files were deleted, all under `untracked/` (the delete script
refuses any other path).

## Check counts, before to after

| Kind | Before | After |
| --- | --- | --- |
| untracked-asset | 6163 | 1894 |
| untracked-unshaped | 56959 | 26976 |
| untracked-decision | 108 | 108 |
| untracked-patch | 33 | 33 |
| unpushed-branch | 7 | 0 |
| dirty-worktree | 47 | 8 |

## Left for the owner (why the check cannot block yet)

- 8 dirty worktrees whose commit hook refuses (above).
- 108 decision and 33 patch findings: not judged by the sweep.
- The sweep's "left for the owner" list (about 250 files).
- About 25000 files inside nested git checkouts and the rest of scratch that only the owner can
  say are safe to drop.

The check stays non-blocking; it was not narrowed.
