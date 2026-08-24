# Hooks Daemon Health Check

Check the status and health of your Claude Code Hooks Daemon.

## Quick Health Check

```claude-code
/hooks-daemon health
```

Displays:

- Daemon status (running/stopped)
- Handler load counts (library + project handlers)
- Configuration validity
- Recent errors (if any)
- DEGRADED MODE status (if applicable)

## View Daemon Logs

```claude-code
/hooks-daemon logs           # Last 50 lines
/hooks-daemon logs --follow  # Stream in real-time
```

Logs show:

- Daemon startup messages
- Handler loading results
- Hook event processing
- Error messages and warnings
- Performance metrics

## Health Check Output

Example healthy output:

```
✅ Daemon Status: RUNNING (PID: 12345)
✅ Socket: /workspace/.claude/hooks-daemon/untracked/daemon-hostname.sock
✅ Handlers Loaded: 67 (63 library + 4 project)
✅ Configuration: Valid
✅ Mode: NORMAL
📋 Uptime: 2 hours, 34 minutes
```

Example degraded output:

```
⚠️  Daemon Status: RUNNING (PID: 12345) - DEGRADED MODE
⚠️  Socket: /workspace/.claude/hooks-daemon/untracked/daemon-hostname.sock
❌ Handlers Loaded: 65 (63 library + 2 project) - 2 FAILED
⚠️  Configuration: Valid with warnings
⚠️  Mode: DEGRADED (2 handlers failed to load)
📋 Uptime: 15 minutes

Failed Handlers:
  - custom_handler: Missing required abstract method get_acceptance_tests()
  - another_handler: Import error: ModuleNotFoundError

See logs for details: /hooks-daemon logs
```

## Common Health Issues

### Daemon Not Running

```
❌ Daemon Status: STOPPED
```

**Fix:**

```bash
.claude/hooks-daemon/bin/hooks-daemon restart
```

### DEGRADED MODE

```
⚠️  Mode: DEGRADED (handlers failed to load)
```

**Causes:**

- Plugin handler missing required abstract methods (e.g., `get_acceptance_tests()` in v2.13.0+)
- Handler import errors
- Invalid handler configuration

**Fix:**

1. Check logs: `/hooks-daemon logs`
2. Fix handler issues
3. Restart daemon

### Configuration Errors

```
❌ Configuration: Invalid (schema validation failed)
```

**Fix:**

1. Check config file: `.claude/hooks-daemon.yaml`
2. Compare with example: `.claude/hooks-daemon.yaml.example`
3. Fix syntax errors or invalid values
4. Restart daemon

## Diagnostic Information

For detailed diagnostics when reporting issues:

```bash
# Generate diagnostic report (health is the detailed view; status is a summary)
.claude/hooks-daemon/bin/hooks-daemon health

# Check handler registry
.claude/hooks-daemon/bin/hooks-daemon handlers

# Validate configuration (config-validate takes the config path)
.claude/hooks-daemon/bin/hooks-daemon config-validate .claude/hooks-daemon.yaml
```

## Troubleshooting

See [references/troubleshooting.md](references/troubleshooting.md) for:

- Common error messages
- Handler load failures
- DEGRADED MODE recovery
- Performance issues
- Socket connection problems
