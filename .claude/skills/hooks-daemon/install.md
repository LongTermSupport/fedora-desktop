# Install Hooks Daemon

Install the Claude Code Hooks Daemon into this project.

## Usage

```claude-code
/hooks-daemon install           # Install daemon
/hooks-daemon install --force   # Force reinstall (re-clones; every venv is kept)
```

## When to Use

This command is for **first-time installation** on a fresh clone. If the daemon is already installed, use `/hooks-daemon upgrade` instead.

You typically need this when:

- You cloned a project that uses the hooks daemon but the daemon isn't installed locally
- Hook scripts are firing "not installed" errors
- `.claude/hooks-daemon/` directory doesn't exist

## What Happens During Install

1. **Downloads installer** from GitHub to `/tmp/` (never piped to shell)
2. **Validates prerequisites** (git, Python 3.11+)
3. **Clones daemon repository** to `.claude/hooks-daemon/`
4. **Creates isolated venv** at `.claude/hooks-daemon/untracked/venv-{slug}-py{MM}-{fingerprint}/` (fingerprint-keyed; never hand-build it — see the "Venv layout" section in `CLAUDE/SELF_INSTALL.md` in the daemon repo)
5. **Deploys hook forwarder scripts** to `.claude/hooks/`
6. **Generates configuration** at `.claude/hooks-daemon.yaml`
7. **Starts daemon** and verifies it is running

## After Install

**CRITICAL: Restart your Claude session** after installation completes. Hooks won't activate until Claude reloads `.claude/settings.json`.

Then verify:

```claude-code
/hooks-daemon health
```

## If Already Installed

If the daemon is already installed and healthy, the command will tell you and suggest upgrade instead:

```claude-code
/hooks-daemon upgrade
```

Without `--force`, install never deletes an existing daemon directory on its own:

- **A clone with no working venv for this project path** (for example, the
  second view of a bind-mounted project) is repaired IN PLACE. The clone's own
  `bin/hooks-daemon repair` builds this path's venv, and nothing else changes.
  If that fails, install stops, and names the repair and the same-version
  upgrade as the next steps.
- **A directory that holds only the runtime folder hooks create** (no clone, no
  venv) is removed, and a normal install runs.
- **Anything else** (a damaged clone, a leftover venv with no clone) stops with
  an explanation. Nothing is changed.

Use `--force` to reinstall over an existing installation deliberately (it backs
up config first). The daemon directory is re-cloned, but every environment's
`untracked/venv-*` is moved aside first (into `.claude/.hooks-daemon-venvs.*`,
which git ignores) and put back afterwards. They only go back into a daemon
directory that holds a clone. If the install failed after removing the clone,
they stay aside and the output says where. If the run is killed outright,
the next `install` finds that directory once its run is gone (never while it
still runs) and puts the venvs back. A newer copy already in place is kept.
So another view's venv survives a forced reinstall.

## Troubleshooting

See [references/troubleshooting.md](references/troubleshooting.md) for common issues.
