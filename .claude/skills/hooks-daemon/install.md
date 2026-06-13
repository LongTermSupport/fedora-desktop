# Install Hooks Daemon

Install the Claude Code Hooks Daemon into this project.

## Usage

```bash
/hooks-daemon install           # Install daemon
/hooks-daemon install --force   # Force reinstall (overwrites existing)
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
4. **Creates isolated venv** at `.claude/hooks-daemon/untracked/venv/`
5. **Deploys hook forwarder scripts** to `.claude/hooks/`
6. **Generates configuration** at `.claude/hooks-daemon.yaml`
7. **Starts daemon** and verifies it is running

## After Install

**CRITICAL: Restart your Claude session** after installation completes. Hooks won't activate until Claude reloads `.claude/settings.json`.

Then verify:

```bash
/hooks-daemon health
```

## If Already Installed

If the daemon is already installed, the command will tell you and suggest upgrade instead:

```bash
/hooks-daemon upgrade
```

Use `--force` to reinstall over an existing installation (backs up config first).

## Troubleshooting

See [references/troubleshooting.md](references/troubleshooting.md) for common issues.
