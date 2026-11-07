# Plan 00250 fact check (PLAN.md diff, TRIAGE.md)

Result: 9 claims: 7 verified, 1 refuted-in-part (stale), 1 unverifiable.

## REFUTED / stale (first)

1. PLAN.md Task 1.1 says `branches.cleanup_after_merge.mode: act` is still to do (unchecked, "Not started" wording).
   `.claude/hooks-daemon.yaml:54-55` already sets `cleanup_after_merge: mode: act`, added by commit feccc19f2
   (`git log -S'mode: act'`). Task 1.1's config edit is done; only the "prove it on a throwaway branch" part remains.
   Change: mark the config half done, keep the proof half.

## Table

| #   | Claim                                                                             | Verdict                     | Evidence                                                                                                                                                                                                                                                                                    |
| --- | --------------------------------------------------------------------------------- | --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | `rescue.bash` runs git add, commit, push across worktrees, no rollback            | VERIFIED                    | `untracked/scratch/rescue.bash` lines 8-12: `git -C $w add -A` / `add --ignore-removal .`, `commit -F`, `push -u origin`; loops over `wt-classify.txt`; no undo; `continue` on refused add/commit.                                                                                          |
| 2   | `cleanup_after_merge` defaults to mode advise                                     | VERIFIED                    | `config/models.py:2359` `mode: Literal["act","advise","off"] = Field(default="advise")`. Note: TRIAGE says it "ran at its default mode"; true for the daemon default, but the project config now says act (see refuted item).                                                               |
| 3   | It prints the remote delete but never runs it                                     | VERIFIED                    | `core/branch_cleanup.py:305-308` `remote_delete_command` only builds the string `git push <remote> --delete <name>`, appended to `commands` (lines 414-416, 484-486); handler's own text: "The remote delete is printed, never run." (`handlers/post_tool_use/cleanup_after_merge.py:189`). |
| 4   | `DEFAULT_REGENERABLE_STORES` never covers `untracked/scratch/`                    | VERIFIED                    | `utils/regenerable_paths.py:25-49` has no scratch entry, and `NEVER_REGENERABLE = "untracked/scratch/"` (line 52) is rejected by config load for any list. Note: `.claude/hooks-daemon/` and `__pycache__/` etc. are covered, none match scratch.                                           |
| 5   | Repo has `delete_branch_on_merge` true                                            | VERIFIED                    | `gh api repos/LongTermSupport/fedora-desktop --jq .delete_branch_on_merge` printed `true`.                                                                                                                                                                                                  |
| 6   | Eight worktrees have staged deletions (all `D ` lines)                            | VERIFIED                    | `git status --short` per `agent-*` worktree: exactly eight have changes, each with count equal to its `D ` count: a3763d51 (3352), a39bf698 (3188), a553be5b (1489), a5a7b0c0 (2903), ab771200 (3297), ad97cdfe (3025), addca160 (2709), ae57223a (1952). All lines `D `.       |
| 7   | Hooks-daemon version v3.72.1 upgrade                                              | VERIFIED                    | `git log`: "hooks daemon upgrade: v3.71.0 -> v3.72.1" (e394e5537).                                                                                                                                                                                                                          |
| 8   | 94 worktrees                                                                      | UNVERIFIABLE-HERE as stated | `git worktree list` now is ~88 lines by inspection (includes cleanup done since); count was taken before cleanup. Counts are dated snapshots; TRIAGE should say "at the time". Also 198 branches / 213 open / 92 origin were not re-counted (origin needs network).                         |
| 9   | Remote guard already allows deleting a branch merged into default (PLAN Task 1.2) | VERIFIED                    | Daemon guidance table, R-GIT-PUSH-DELETE-REMOTE: "A branch already merged into the default branch is deleted freely".                                                                                                                                                                       |

Not checked: the 11 "agent-\* live work" branches 1 ahead of F44, and the 12 scratch-only worktrees (state changed by the cleanup; no read-only way to prove the past).
No git command that writes was run; no worktree was changed.
