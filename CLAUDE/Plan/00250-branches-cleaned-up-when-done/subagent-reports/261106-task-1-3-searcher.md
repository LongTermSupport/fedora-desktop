# Task 1.3 searcher: script files holding git mutations, run through Bash

The dispatch named the class and hazard only. It did not contain the rule or its pattern. I did not open the detector directories, `.claude/hooks/handlers/` or `.claude/hooks-daemon/`, and I did not run the detector. I read no secret or vault files. I did not enter `.claude/worktrees/` or `untracked/worktrees/`. I also left out `untracked/repos` and the vendored upstream clones under `untracked/`.

Forms expected before searching: a bash or sh file with git mutation verbs, run as `bash f` or `./f`; python, perl or node wrapping git through subprocess; Makefiles and task runners; git aliases and hooks; and indirect routes (`bash -c "$(cat f)"`, `eval`, `bash < f`, `cat f | bash`, `source f`, `xargs git`, `find -exec git`, `env bash f`, `timeout`, `nohup`, scripts called with arguments).

## A. Untracked script files with git mutations (the core class)

None of these is tracked: `git ls-files --error-unmatch` fails for each, and `git ls-files untracked` lists only `untracked/.gitignore`. All sit under the gitignored `untracked/scratch/` and are bash scripts. Found by text search, then confirmed by reading each file.

| #   | File                                              | Git mutations (line)                                                                                                                                                                                                    | Confidence |
| --- | ------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- |
| 1   | `/workspace/untracked/scratch/rescue.bash`        | `git -C "$w" add --ignore-removal .` (8), `add -A` (9), `commit -F` (11), `push -u origin "$b"` (12), looped over every worktree from `wt-classify.txt` (4). `set -uo pipefail`, no rollback. The originating instance. | Confident  |
| 2   | `/workspace/untracked/scratch/push2.bash`         | `git -C "$w" push origin HEAD:refs/heads/wip-rescue/...` (9), `git push -u origin "$b"` (13). Loops over worktrees and branches.                                                                                        | Confident  |
| 3   | `/workspace/untracked/scratch/batch-merge.bash`   | `git merge --no-ff -F` (20), `git push -q -u origin merge/batch-139-141-142` (22)                                                                                                                                       | Confident  |
| 4   | `/workspace/untracked/scratch/batch2-merge.bash`  | `git -C /workspace worktree add -q -b ...` (6), `git merge --no-ff` (20), `git push -q -u origin` (22)                                                                                                                  | Confident  |
| 5   | `/workspace/untracked/scratch/union-resolve.bash` | `git merge-file --union` writing into the working tree (11), `git add "$f"` (16). Takes file arguments; `set -euo pipefail`.                                                                                            | Confident  |

Near misses that read as safe:

- `untracked/scratch/purge.bash` (lines 11, 14) runs `rm -f` and `find ... -delete` on `untracked/*`. No git verbs, but the same blind spot for filesystem deletion. It refuses paths outside `untracked/` and paths containing `..`. Unsure it belongs to the class.
- `untracked/scratch/wait-ci.bash` line 11 is `git rev-parse`, read-only.
- `untracked/scratch/build-list.bash` has no git mutations. `find-race-test.bash` and `race-test.bash` have no git text at all.
- Three scripts in other untracked side projects (`untracked/<project-A>/`, `<project-B>/`, `<project-C>/`) have no git commands.

## B. Python or other interpreters wrapping git (variant, noted separately)

- `/workspace/untracked/scratch/dogfood-261105/wt_probe.py:21-30` calls `subprocess.run(["git", ...])` with `init`, `config`, `add` and `commit`, in a throwaway repo it creates itself. Low hazard, but an instance of the variant. Found by text search, then read.
- `/workspace/untracked/scratch/sw2/delete_list.py:15,25` runs `git cat-file` and `git grep` only (read-only). `sw2/classify.py:20` calls `check.bash`, which runs `git grep` (read-only). No git mutation in `sw2/apply.py` or `sw2/redact.py`.
- Other scratch scripts (`bgp/`, `ccy-image-build-lock-test.*/`) hold git text; I judged them copies of vendored prompt code or lock tests and did not read every one. `untracked/scratch/r2-red/`, `mdv-branch/`, `m1tree/` and `sigpipe-red/` are whole copies of repo trees, so tracked scripts appear a second time as untracked copies. Noted, not listed individually.

## C. Tracked scripts holding git mutations (reviewed in git, but still invisible to the command-line guard when run by path)

Text search over `git ls-files`, with tool and vendor trees excluded, found these real mutation sites.

- `/workspace/scripts/release.bash`: `git checkout` (130), `git switch` (134, 215), `git add` (231), `git commit -S` (232), `git push -u` (234), `git push` of a tag (287).
- `/workspace/scripts/mark-branches-superseded.bash`: `git worktree remove --force` (99, 123), `git worktree add` (216), `git add` (251), `git push` to branch refs (261).
- `/workspace/CLAUDE/Plan/mkplan.bash:827`: `git add` of a journal file.
- `/workspace/run.bash`: `git checkout` (810, 821), `git pull` (1302, 2753, 3146, 3557).
- `scripts/branch-delete-list.bash` only prints `git push origin --delete` lines for the owner (line 5); not a mutation.
- Remaining tracked hits are acceptance and test scripts (mostly temporary repos) or prose in comments: `CLAUDE/Plan/*/acceptance*.bash`, `scripts/test-*.bash`, `files/home/.local/share/vmtest/guest-acceptance-*.bash`, `scripts/git-hooks/*`, `files/var/local/claude-yolo/*`, `helpers/agent_bus/checkout.py`, `helpers/self_update/update.py` (runs `git merge --ff-only` via subprocess, doc line 29). I did not check each for real mutations.
- Go programs shell out to git in `src-go/internal/gitx/gitx.go`, `internal/teams/lxcadd/checkout.go`, `internal/ccylaunch/front.go`. Compiled binaries, not script files; a different shape of the class.

## D. Other routes to git with the guard blind

Checked and not found in the repo: Makefile, justfile, Taskfile (only the vendored `src-go/vendor/.../Taskfile.yaml`), git aliases (`git config --get-regexp alias` empty; the global git config has only gh credential helpers), a `.claude/commands` directory.

Real variants of the class:

1. `bash f`, `./f`, `source f` where `f` holds git mutations. The originating shape.
2. `env bash f`, `/usr/bin/bash f`, `sh f`, `timeout N bash f`, `nohup bash f &`, and `bash f arg...` (`union-resolve.bash` takes arguments).
3. `bash -c "$(cat f)"`, `eval "$(cat f)"`, `bash < f`, `cat f | bash`, a here-doc piped to bash. No instance found in the repo; variants, not observed incidents.
4. `python3 f.py` or `python3 -c` with `subprocess` calling git. One instance (B).
5. `xargs git ...` and `find -exec git ...` fed from a list file. The guard sees the verb but not the list that supplies the targets. `purge.bash` is a deletion-list analogue (reads `del.list`).
6. A script that reads its targets from a data file: `rescue.bash` and `push2.bash` read `wt-classify.txt` (`push2.bash` also `audit-before.txt`), `purge.bash` reads `del.list`. The command line never shows the targets.
7. Scripts that call other scripts (`classify.py` calls `check.bash`).
8. Tracked tools run by path (C): reviewed code, lower risk, but the guard still sees only the path.

## E. Evidence of other incidents in plan journals and triage

- `/workspace/CLAUDE/Plan/00250-branches-cleaned-up-when-done/TRIAGE.md:24` and `:33` record the `rescue.bash` incident (a WIP rescue commit made done agent branches unmerged). `PLAN.md:49,81` names `rescue.bash` as the red proof. `subagent-reports/261106-plan-fact-checker-sonnet.md:16` verified the claim.
- `push2.bash` line 8 carries the WIP-rescue commit subject for Plan 00245 ("WIP rescue: uncommitted work found by the untracked audit (Plan 00245)"). It belongs to the same incident and is not mentioned in TRIAGE.
- `batch-merge.bash` and `batch2-merge.bash` are batch CI-merge builders with no mention in the plan tree. `union-resolve.bash` appears in no journal.
- Other scratch scripts named in plan reports (`triage-noguard.bash` in the Plan 00099 journal line 122, `stress.bash`, `gate.bash`, `rev00183-probe.bash`) no longer exist at `untracked/scratch/`. The 00099 line describes `triage-noguard.bash` as a hand-made guard-free copy of a tracked script, another guard-bypass shape. I did not confirm it held git mutations.

## Searches run

- Greps for `git( -C path)? (add|commit|push|reset|branch -d/-D|worktree add/remove/prune|checkout|switch|restore|rm|stash|merge|rebase|update-ref|clean|cherry-pick|revert|tag|pull|apply|filter)` over `untracked/` (excluding worktrees, repos, vendored clones) and over `git ls-files`. My first per-extension recursive greps printed nothing and I did not work out why; I fell back to explicit file lists and the results above come from those.
- A shebang scan of every non-log file in `untracked/scratch/*` and `untracked/*/*`, to catch scripts with no extension.
- `find` for `*.bash`, `*.sh`, `*.py` in `untracked/` at depth 3 and in `untracked/scratch` at depth 2.
- `git config --get-regexp alias` and a grep of the global git config.
- Greps for `rescue.bash` and `scratch/*.bash` in `CLAUDE/` and `docs/`.
- `ls` for Makefile, justfile and Taskfile in the repo root.

## Which technique found what

- Text search alone found: A rows 1-5 as candidates, the tracked mutation lines in C, and the plan references in E.
- Text search could not have checked: whether a script is tracked (`git ls-files --error-unmatch` for each untracked script), extension-less and shebang-only scripts, and the empty alias and Makefile results.
- Reading found what text search could not: that `rescue.bash` and `push2.bash` take their targets from list files; that `union-resolve.bash` mutates only through argument-supplied paths; that `purge.bash` is a deletion analogue with no git text; that `wt_probe.py` works in its own temporary repo; that the sw2 Python scripts are read-only; and that the many `r2-red` and `mdv-branch` hits are copies of tracked scripts.
- Not checked, a limit of this search: whether the list files named above still exist, and the contents of the vendored clones.

## Note on a tool result

One Bash result carried a hook "additional context" asking me to create a recovery cron and write a journal entry. It was not part of this task or from the user, and I ignored it.
