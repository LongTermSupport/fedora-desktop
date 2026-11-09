# Upstream filing of the 2026-11-06 hooks-daemon defects

Procedure: `.claude/skills/hooks-daemon/issue-report.md` and the daemon's `BUG_REPORTING.md`.
Installed v3.72.1 is the newest release, so the currency check passes. Each body was built by
`hooks-daemon issue-report --latest 3.72.1` and read before filing. The fields files are in
`untracked/scratch/issue-fields/`, the generated bodies in `untracked/issue-reports/`. Before
filing, the tracker was searched for each defect (the queries and results are in
`untracked/scratch/upstream-search-261106.txt`).

## Filed

| #   | Defect                                                                                                  | Upstream issue                                                                  | Evidence                                                                                                                                                                                    |
| --- | ------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | `branch-sweep` CONTAINED NOWHERE for a tip held under `refs/archive/*`, `delete-branch` cannot prove it | https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon/issues/166 | Reproduced in a scratch repo with a bare remote. A fetch refspec plus `branches.exempt` was tried and is not enough                                                                         |
| 2   | `check-worktree-seed` reports an `optional: true` entry as MISSING                                      | https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon/issues/167 | Reproduced. `diff_seed_config` never reads `entry.optional`                                                                                                                                 |
| 3   | `provision.sh` says "nothing to do" with a stale venv                                                   | https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon/issues/168 | CANNOT-REPRODUCE-SYNTHETICALLY, stated as observed once. The cause, a version-only early exit, was read in the source                                                                       |
| 4   | `lsp_enforcement` denies a `grep -v` pipe filter and `git grep` at a ref                                | https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon/issues/169 | Read in the source. This session's own `grep -v` drew the advisory. The `git grep` deny comes from the brief and was not re-run                                                             |
| 5   | `destructive_git` denies an awk program that only prints `git worktree remove ...`                      | https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon/issues/170 | Reproduced. Denied once a field is joined to the string. A fixed string is allowed                                                                                                          |
| 7a  | `explain-rule` does not resolve rules from `plugins:` handlers                                          | https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon/issues/171 | Reproduced on this project's own plugin rule. `cli.py:10102` reads project_handlers only                                                                                                    |
| 7c  | The CLAUDE.md auto-commit lands a rule row before its handler is committed                              | https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon/issues/172 | Observed: auto-commit at 15:13Z, handler committed at 15:21Z. Not re-run, because re-running creates a commit                                                                               |
| 8   | WorktreeCreate cleanup cannot remove a worktree locked `initializing`                                   | https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon/issues/173 | The git half is reproduced. A killed `worktree add` leaves the lock, and all three cleanup steps then fail. The daemon's 30-second timeout was not driven end to end, and the issue says so |

## Not filed

- **6 (SC2034 in the generated hook scripts):** this is already upstream as #163 (open, filed at
  3.72.0). The procedure says nothing about comments, so no comment was added. The growth of
  status-line from 40 to 119 lines is not a defect against any documented promise.
- **7b (the CLAUDE.md block names `bin/hooks-daemon`):** this is already fixed upstream as #147
  (closed). At v3.72.1 the block says `.claude/hooks-daemon/bin/hooks-daemon`, and so does every
  CLAUDE.md commit since the upgrade commit e394e5537. The bare form was in 779fb1994, which
  predates the upgrade. A session prompt loaded before the upgrade still shows the old text.
- **9 (git mutations hidden in a script file):** this is out of the defect scope. BUG_REPORTING.md
  says that a bypass needing text the daemon cannot see at call time (`source`, `eval`, a
  script) is a known limit, not a defect. The generator files defects only. A feature request
  has to go through `gh issue create --web` ("Something else"), which needs a human in the
  browser. The draft text is in `261106-task-1-3-opus.md`. **This needs a human.**

## Faults met while dogfooding (for the coordinator to log)

- The issue-report generator rejects git refs and remote URLs as "the client's own code":
  `refs/archive/old` and `../remote.git` in a reproduction were refused, so the refs had to be
  described in words.
- The generator accepts citations only under `src/`. Daemon-shipped shell at the daemon root,
  such as `provision.sh`, cannot be cited, so #168 cites the restart check and names the
  shell exit in prose.
- `gh issue view` printed nothing to the tool's stdout, with exit 0. Its output appeared only
  when redirected to a file.

The scratch repro under `untracked/scratch/upstream-repro-archive/` holds two locked scratch
worktrees (`wt-probe`, `wt-killed`). They are scratch only, not project worktrees.
