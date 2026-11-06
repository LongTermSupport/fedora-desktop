# Why branches were not cleaned up: the triage of 2026-11-06

The hooks daemon was upgraded to v3.72.1, and its new WIP cap counted 213 open work branches
against a cap of 10. These are the facts the cleanup turned up. Counts are from the checkout and
from `git ls-remote origin`. Names are left out where a count is enough.

## What the checkout held

- **Local branches:** 198. **Worktrees:** 94. **Origin work branches:** 92 (the release lines
  and their archives not counted).
- `hooks-daemon branch-sweep --clean` deleted the merged and patch-equivalent local branches,
  behind a recovery bundle. That took local branches from 198 to 83.
- Every branch the sweep skipped was a merged branch whose worktree it could not remove. The
  worktrees fell into two groups:
  - **Scratch only.** Twelve held only ignored `untracked/scratch/` or `untracked/agent-reports/`,
    the files a sub-agent writes in its own worktree. The destructive-git guard rightly refuses
    to remove a worktree holding them.
  - **Staged deletions.** Eight held thousands of staged deletions of tracked files (cause below).
- **Unpushed branches:** twenty local branches held commits that no remote ref held. They were
  15 to 50 days old: superseded designs, review branches, an early go-port, and workflow lanes.

## Cause 1: an ad-hoc script ran git mutations across every worktree

On 2026-11-04 an agent wrote `untracked/scratch/rescue.bash` for Plan 00245's untracked audit and
ran it over every worktree. In each one it ran `git add -A` (or `git add --ignore-removal .`),
`git commit` and `git push -u`. It had no rollback.

- **Eight indexes left wrong.** All eight were rewritten between 13:47:59 and 13:48:56Z. Each
  lost its tail of entries from the same sorted point, which is about half the tree. Files were
  missing on disk when `git add` ran, so the deletions were staged. The pre-commit gate then
  refused each commit, and the script went on to the next worktree with the deletions still
  staged. The files are back on disk, and the index still records them deleted.
- **Done agent branches made unmerged.** Where the commit passed, a "WIP rescue" commit of
  scratch went onto an agent branch that was already merged, and was pushed. Those branches are
  now one commit ahead of F44, so no sweep can ever call them merged. They are the eleven
  `agent-*` "live work" branches, 1 ahead, two days old.

**The class:** a script file runs git mutations, so the command guards never see them.
`destructive_git`, the force-delete and worktree-removal guards and the commit gates judge the
command line of a Bash call. `bash some-script.bash` hides every git command inside the script
from them. The script then turns one refused step into a partial state across many trees.

## Cause 1a, found during the repair: a worktree whose creation was killed mid-checkout

The rescue script did not create the partial worktrees; it found them. Fifteen agent worktrees
were still git-locked with reason `initializing`. That lock is the one `git worktree add`
holds while it runs and removes when it ends, so their creation never finished. The partial
state looks like this:

- **Eight worktrees: the checkout was cut short.** Half the files were missing on disk, and the
  index stopped at the same sorted point as the checkout. The agent then worked in that
  worktree and committed its own files, and its branch was merged into F44.
- **Seven worktrees: the rescue made it worse.** On 2026-11-04 the rescue script committed
  each partial tree, a "WIP rescue" commit that deleted 3,881 to 6,509 files and nothing else.
  No such branch is under `refs/heads` on origin.

The daemon's WorktreeCreate handler runs `git worktree add -b` with a 30-second timeout
(`Timeout.GIT_WORKTREE`). This repository checks out about 8,600 files, and the host is often
loaded by QA. The likely killer is that timeout, but it is not yet proven. If it is, a
creation that times out leaves a half-made, locked worktree, and the agent is still sent to
work in it. That is a daemon defect, Task 1.5.

**Repair (2026-11-06), with no forced removal and no unproven delete:**

- Eight worktrees: `git reset` put each index back to HEAD (index only). Then
  `git checkout-index -a`, without `-f`, wrote only the files missing on disk. Each worktree
  was then clean. The stale `initializing` lock was removed, and `branch-sweep --clean`
  removed the worktree and deleted its merged branch, behind a recovery bundle.
- Seven worktrees: each was clean. The lock was removed, the worktree removed without
  `--force`, and the branch deleted by `delete-branch`, which proved every file version on it
  reachable from F44 ("content-preserved").

## Cause 2: a merged branch is never deleted by anything

- **Local.** `cleanup_after_merge` ran at its default mode, `advise`. After each merge it
  printed the delete command, and nothing ran it. The coordinator merges agent branches with
  `git merge --no-ff` on a test branch, then fast-forwards F44. Each merged agent branch, and
  its worktree, stays.
- **Remote.** The repository has `delete_branch_on_merge: true`, but that fires only when a pull
  request is merged on GitHub. A pull request merged locally and pushed is closed as merged with
  its head branch left. That is how most of origin's 92 work branches came to be there.

## Cause 3: sub-agent scratch lives in the worktree

A sub-agent dispatched with `isolation: "worktree"` writes its notes and reports under its own
`untracked/`. That is ignored content no ref holds, so the guard refuses to remove the worktree
until someone moves or deletes the notes. Copying them elsewhere does not satisfy it either,
because the guard cannot see the copy.

## The cleanup so far, and what was done wrong

- Twelve scratch-only worktrees: scratch copied to `untracked/scratch/evacuated-worktrees/`,
  the worktrees then removed normally, and their merged branches deleted by the sweep.
- Two merged remote branches deleted.
- The twenty unpushed branches pushed to `refs/archive/<name>`, each tip checked against
  `git ls-remote`.
- **A workaround the owner refused.** Eight of the archived branches were then deleted with
  `git branch -D`, which the guard allows once a remote ref holds the tip. Nothing was lost:
  each tip is on origin. But it was a delete that bypassed `delete-branch`'s content check, and
  that check exists for this case. The owner stopped it, and the rest waits for this plan.
- Daemon defects met during the cleanup: #184 (the seed check ignores `optional`), #185
  (`provision.sh` reports success with a stale venv), #186 (the symbol-lookup rule judges a
  `grep -v` filter).
