# Hooks daemon upgrade and config optimisation

## Upgrade

- v3.68.0 → v3.69.0 (latest release). Pre-deploy gate: no upgrade guides or pre-upgrade tasks for the range, so no stop and no owner approval needed.
- The daemon is RUNNING with 163 handlers loaded. Post-install checks passed, config-validate is clean, and the health check passes. "Effective handler set: unchanged by this upgrade" and `handler_changes=` is empty, so the regression in upstream #97 (opt-in handlers switched off) did not hit this config.
- Truth-changes: none. Post-upgrade task 01 (give strict-mode exclusions a reason) does not apply because `strict_mode` is not set.
- Config migrations:
  - `host_command_guard` is new and on by default; it is loaded at priority 10.
  - `root_recursion_guard` now defaults to priority 13. This config sets no priority, so it gets 13; confirmed in `handlers`.
  - `autonomy_notice` is silent here because the config has no `autonomy:` block.
- `settings.json` was merged against a baseline and came out unchanged.
- The old skill directory was backed up to `.claude/hooks-daemon-backups/skills/hooks-daemon-4`. It is byte-identical to HEAD, so no local edits were lost.
- The ccy supervisor gains a POSIX-sh launcher, `.claude/ccy/claude-supervise`, which picks Python 3.11+ and then execs `claude-supervise.py`. `ccy.env`'s `CCY_CLAUDE_WRAPPER` now points at that launcher. The new container sessions use it.
- Commit 2d1f3b07 contains only the files the daemon deployed. The metadata block has its host and paths left out because the repo is public. `CLAUDE/Plan/meta-deploy.bash` and `CLAUDE/Worktree.md` were not staged.

## Optimise (score 156/159, now 157/159)

- APPLIED: `secret_file_guard` is back on (commit e77aa7e8). It had been off since 0e93ebbb because it failed closed on ordinary shell text. In v3.69.0 it denies only on a positive finding. Probed through the daemon entry point:
  - `$?`, `printf "%s\n"`, line continuations, `${x:-c d}`, a recursive grep and a Write of shell text are all allowed.
  - A command naming a protected file is still denied.
  - No project doc said the guard was off, so no doc edit was needed.
- NOT applied: `daemon_stats`. The config comment records that the owner removed it from the status line.
- NOT applied: `github_issue_assignment_guard`. No PLAN.md carries a `**GitHub Issue**:` header, so it would have nothing to judge.
- The run is recorded at v3.69.0.
- Daemon auto-commit 0d3810e2 regenerated the CLAUDE.md handler block (R-SECRET rows) on restart. It touched only CLAUDE.md.

## Mislabelled git failure: still present in v3.69.0, filed

- Reproduced with a synthetic repo (format v1 plus `extensions.relativeWorktrees`) under container git 2.39.5:
  - git says `fatal: unknown repository extension found: relativeworktrees`.
  - The daemon says `Invalid configuration in .../hooks-daemon.yaml: ... not a git repository or has no remote origin ... Fix the configuration file`.
- Cause:
  - `core/project_context.py:328-331` logs only "Not a git repository" and discards git's stderr.
  - The ValueError is then caught by the generic `except Exception` at `daemon/cli.py:367`, which labels every failure as invalid configuration.
- Issue: https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon/issues/98. The body came from `hooks-daemon issue-report` and contains no host, user or path identifiers.
- The 15 s hook wait was not re-tested. v3.69.0's `init.sh` launcher loop now breaks out when the launcher has settled (`_hooks_daemon_launch_settled`), which suggests that part is addressed.

## Seen, not acted on (owner/coordinator decision)

- The worktree seed drift report lists `.claude/hooks-daemon.env` as MISSING and says it would "abort the NEXT worktree creation". The entry is declared `optional: true`, though, and `utils/worktree_seed_suggestions.py:202` ignores `optional`. So the report is wrong, not the config. This is a candidate upstream issue and was not filed.
- The same report lists `.claude/settings.local.json` as UNCONFIGURED, meaning worktrees will not get it. Whether to seed it (as a copy rather than a symlink) is the owner's call.

## For the coordinator

- Run `./scripts/qa-all.bash`; it is blocked for sub-agents. Targeted checks done: `bash -n` / `sh -n` on `mkplan.bash`, `init.sh`, `status-line` and `claude-supervise`; shellcheck on `mkplan.bash` and `claude-supervise`; py_compile on `claude-supervise.py`; config-validate.
- Push 2d1f3b07, 0d3810e2 and e77aa7e8 (not pushed).
- Nothing needs a host run, so meta-deploy is unchanged.
