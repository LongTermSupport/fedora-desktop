## 16:42 · action · —   — Task 2.1: half-made worktrees repaired and removed through the tools

Task 2.1 done, and a deeper cause found. The 15 agent worktrees still git-locked 'initializing' were half-made: their git worktree add never finished, likely killed by the WorktreeCreate handler's 30-second timeout (not yet proven; Task 1.5). Eight held a cut-short checkout. Seven also held a 2026-11-04 rescue commit that only deleted 3,881 to 6,509 files.

Repaired with no forced removal and no unproven delete:
- Eight: git reset (index only), then git checkout-index -a without -f, which writes only files missing on disk. Each worktree came out clean. The stale lock was removed, and branch-sweep --clean removed the worktree and deleted the merged branch, behind a recovery bundle.
- Seven: each worktree was clean. The lock was removed, the worktree removed without --force, and the branch deleted by delete-branch, which proved every file version on it reachable from F44.

Worktrees went from 79 to 64, and local branches from 70 to 47.
