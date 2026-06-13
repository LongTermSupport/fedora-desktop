# Restart Daemon

Restart the hooks daemon after configuration changes or to recover from issues.

## Usage

```bash
/hooks-daemon restart
```

## When to Restart

**Always restart after editing `.claude/hooks-daemon.yaml`** — the daemon reads
configuration once at startup and caches it in memory. Changes to the config file
are not picked up until the daemon is restarted.

Common reasons to restart:

- Edited `.claude/hooks-daemon.yaml` to enable/disable handlers or change priorities
- Added or modified project-level handlers in `.claude/project-handlers/`
- Daemon appears unresponsive or is producing unexpected results
- After updating the daemon version (upgrade also restarts automatically)

## What Happens

1. The running daemon process receives SIGTERM and shuts down gracefully
2. A new daemon process starts and loads the current configuration
3. All handlers are re-registered with their current settings

The restart completes in under 2 seconds. Hook events during restart fall back to
the bash scripts directly (no blocking, no errors).

## Verify After Restart

```bash
/hooks-daemon health
```

Expected output:

```
✅ Daemon Status: RUNNING (PID: 12345)
✅ Handlers Loaded: 67 (63 library + 4 project)
✅ Configuration: Valid
```

## Troubleshooting

If the daemon fails to restart:

```bash
# Check logs for startup errors
/hooks-daemon logs

# Common causes:
# - Syntax error in hooks-daemon.yaml → fix the config and retry
# - Import error in project handler → fix the handler and retry
# - Port/socket conflict → check for stale daemon processes
```

See [references/troubleshooting.md](references/troubleshooting.md) for detailed recovery steps.
