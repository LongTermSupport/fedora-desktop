# Why accb2's hooks daemon failed to start (Plan 00161 U20/M2.7, host run 20261009-184826)

Read-only investigation. Nothing tracked was touched. The daemon and its venvs were not touched.

## Answer in one paragraph

At **17:55:13 UTC** a git worktree was created **on the host**, with **relative paths**:
`untracked/worktrees/worktree-plan-00166-lid`, branch `worktree-plan-00166-lid`. To store relative
paths, git (2.48 or later) also wrote **`extensions.relativeWorktrees = true`** (with
`repositoryformatversion = 1`) into the shared `.git/config`. The CCY image is Debian bookworm
(`node:lts-slim`), whose **git 2.39.5 does not know that extension and refuses the whole
repository**: `fatal: unknown repository extension found: relativeworktrees`. accb2's container
(f0860342889e) came up at 17:55:0x. Every daemon start it tried ran
`ProjectContext.initialize` → `git rev-parse --show-toplevel`, git refused, and the daemon CLI
exited with `FAIL FAST: Project at <repo> is not a git repository or has no remote origin`.
The forwarder then returned `daemon_startup_failed` for every hook. This is not a lock, pid,
socket, venv, python or container-id problem, and not a leftover of accb's session.

## Evidence

1. **The launcher's own output.** It appears in accb2's SessionStart hook stderr
   (`accb2/session.out` line 67, and `accb2/transcript.jsonl` entry 3, durationMs 14638, 17:55:10.5 → 17:55:25.1):
   ```
   ERROR: Daemon startup timeout (daemon not ready 15 seconds after its launcher finished)
   Launcher output (may explain the failure):
   ERROR: Invalid configuration in <repo>/.claude/hooks-daemon.yaml:
   FAIL FAST: Project at <repo> is not a git repository or has no remote origin. ...
   ```
   The same output came back on the PostToolBatch start attempt at 17:56:12 (transcript entry 27).
   The UserPromptSubmit and PreToolUse attempts in between also failed, so the cause was
   **persistent** inside that container and not a one-off timeout.
2. **The extension, and when it was written:**
   - `.git/config` lines 215-216 hold `[extensions] relativeWorktrees = true`, and `core.repositoryformatversion = 1`.
   - `.git/config` mtime is 17:55:13.305. `.git/worktrees/` mtime is 17:55:13.304.
   - `.git/worktrees/worktree-plan-00166-lid/` was created at 17:55:13.305.
   - Its `gitdir` is `../../../untracked/worktrees/worktree-plan-00166-lid/.git`, a relative path.
     Every one of the ~70 older worktrees in `.git/worktrees/` has an absolute `<worktree>` gitdir.
3. **The worktree was made on the host.** Its reflog stamps are `+0100`. Every container-made
   worktree's reflog is `+0000`. Container git 2.39 also cannot write this extension at all. The
   plan-00166 untracked scripts (`probe-lid.bash`, `triage.bash`) were present in the main
   checkout at 17:53 (`u20/after-launch/git-status`). They are gone from it after 17:55:20,
   which fits a host session on Plan 00166 moving into its new worktree.
4. **It is still broken now, in every container on this checkout.** In this (lead's) container:
   `git --version` → 2.39.5. `git status` → `fatal: unknown repository extension found: relativeworktrees`, rc 128.
   `git config --list` → `warning: ignoring git dir '.git'`.
5. **Where the error comes from in the code:**
   - `.claude/hooks/pre-tool-use:21-23` calls `emit_hook_error ... daemon_startup_failed` when
     `ensure_daemon` (`.claude/init.sh:2454`) fails.
   - That happens when `start_daemon` (`.claude/init.sh:1772`) fails. Its launcher runs
     `python -m claude_code_hooks_daemon.daemon.cli --project-root <repo> start`.
   - `cli._validate_installation` (`daemon/cli.py:346-372`) calls `ProjectContext.initialize`.
     That raises at `core/project_context.py:259-264` because `_get_git_repo_name`
     (`:328-331`) got a non-zero exit from `git rev-parse --show-toplevel`.
   - `run_git` captures git's stderr (`utils/git_repo.py:153-166`), but `_get_git_repo_name`
     drops it and logs only "Not a git repository". That is why the real reason never surfaced.

## Q2: Why the 17:53 containers were not affected

Their daemons started at 17:53:08, 17:53:19 and 17:53:31 (the `daemon-5a94e3762b2d`,
`daemon-f98f7b89d2f2` and `daemon-5b175835b52e` pid/sock files). That was before the extension
existed. The git check runs only once, at daemon startup, so an already-running daemon carried
on. accb2 was the first daemon start on this checkout after 17:55:13.

The two earlier passing host runs today were also before the extension. The image rebuild does
not matter: both images ship bookworm git 2.39. accb2's launch timing was simply unlucky. Its
first start attempt was at about 17:55:11-13, against the config written at 17:55:13.3, and any
later attempt would have failed the same way.

## Q3: Whose defect

- **Root cause: an environment incompatibility. It is not a hooks-daemon bug and not a harness
  race.** The host's git (2.48+) and the CCY container's git (2.39.5) share one `.git`. A single
  host-side `git worktree add` with relative paths makes the repository unreadable to every
  container git. That means every ccy session, every daemon start, and every `git` an agent
  runs in a container. It is a **ccy defect** in the sense that ccy bind-mounts a host checkout
  into a container whose git cannot read every repository format the host git can write. The
  repo has no rule or doc mentioning `relativeWorktrees`; I searched CLAUDE/, docs/, the ccy
  files and the plans.
- **Who chose relative paths: unknown.** Repo-side, nothing sets `worktree.useRelativePaths`
  or passes `--relative-paths`. I searched the playbooks, files/, scripts/, the plans and the
  daemon source, and the daemon's own WorktreeCreate handler runs a plain
  `git worktree add -b <branch> <abs path>`. So either the host session passed
  `--relative-paths`, or the host user's global git config sets `worktree.useRelativePaths`.
  The owner can check on the host:
  `git config --show-origin --get worktree.useRelativePaths | cat`.
  This is plausibly deliberate: an absolute host path is just as broken inside the container
  (`<repo>`), and relative paths are git's answer to exactly that.
- **The hooks daemon has a secondary upstream defect worth an issue: a misleading diagnostic.**
  The fail-fast message says "not a git repository or has no remote origin" and throws away
  git's stderr. Here git's stderr named the cause exactly. It also costs time: the bash
  forwarder waits a further 15 s after the launcher has already exited with a fatal config
  error, and it does this on every hook (`_hooks_daemon_launch_settled`, `init.sh:1922-1931`).
  Each turn of accb2 paid 15 s per hook.
- **The acceptance harness has nothing to wait for.** There was no stale container, lock or
  pid. Its one possible improvement is a preflight check, below.

### Facts for an upstream issue (generate with `hooks-daemon issue-report`; do not hand-file)

These must stay public-safe: no paths with usernames, no hostnames.

- Daemon version: v3.68.0 (the client's clone). Client: a project bind-mounted into a container
  whose git is 2.39.5. The repository carries `extensions.relativeWorktrees=true`, written by a
  newer git on the host.
- Observed: `cli start` exits with `ERROR: Invalid configuration in <project>/.claude/hooks-daemon.yaml: FAIL FAST: Project at <project> is not a git repository or has no remote origin.`
- Actual git error (dropped): `fatal: unknown repository extension found: relativeworktrees`.
- Code: `core/project_context.py` `_get_git_repo_name` / `_get_git_toplevel`. These ignore
  `result.stderr` from `utils/git_repo.run_git`, and the error is labelled as a configuration
  error by `daemon/cli.py:_validate_installation`.
- Asks:
  1. Include git's stderr (and exit code) in the FAIL FAST message, and tell "git refused the
     repository" apart from "not a repo" and from "no origin".
  2. Do not call it "Invalid configuration": the yaml was fine.
  3. In `init.sh start_daemon`, stop polling once the launcher has exited non-zero with no
     daemon pid. Today every hook waits the full 15 s settle window for a daemon that cannot
     come.

## Q4: Smallest correct fix, and where it belongs

1. **Unblock now. This is the owner's call, done on the host, because container git cannot
   touch the repo.** Return the repository to a format container git reads:
   - Finish or remove the plan-00166 worktree (`git worktree remove`), or re-point it with
     absolute paths (`git worktree repair` without relative paths).
   - Then `git config --unset extensions.relativeWorktrees`. Keep `repositoryformatversion` as
     is unless git complains.
   - Verify from a container with `git status`.
   - Until then, **every** ccy session on this checkout has a dead daemon and no git, the lead's
     included (its running daemon survives only until it restarts). Re-running U20 before this
     will fail M2.7 again.
   - Note: this changes `.git/config`, not tracked files or HEAD, but it should still wait
     until the running host acceptance has finished.
2. **Durable fix: in ccy (`files/var/local/claude-yolo/`, with a CCY version bump).** Make the
   container's git able to read what the host's git writes: ship git ≥ 2.48 in the base image
   (the release that introduced `extensions.relativeWorktrees`). Bookworm's 2.39 is far behind
   Fedora 44's. This is the one fix that also makes relative-path worktrees work in both
   places, which is the only kind of worktree that can.
3. **Fail-fast guard: in ccy's launcher, cheap, and worth doing even with (2).** Before starting
   Claude, run `git -C <repo> rev-parse --git-dir` inside the container. On failure, abort
   the launch, print git's own message, and name the fix. Today the session starts, and the
   failure shows up only as a hooks-daemon error that points the reader at the daemon.
4. **Optional host-side guard, IaC.** Until (2) lands, set `worktree.useRelativePaths=false` in
   the host git config the playbooks manage, so no host worktree silently switches the
   repository format. Add a line to `CLAUDE/Worktree.md`: worktrees for this checkout are made
   inside a ccy container, or not with `--relative-paths`.
5. **Upstream (hooks-daemon):** file the diagnostic issue above via `hooks-daemon issue-report`.
   It would have turned this into a one-line diagnosis.

Of these, (1) is needed to get U20 green again, and (2)+(3) are the smallest change that stops
it recurring. The harness needs no change for this failure.
