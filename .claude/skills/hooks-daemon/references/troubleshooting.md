# Troubleshooting Hooks Daemon

Common issues and solutions for Claude Code Hooks Daemon.

## Daemon Won't Start

### Symptom

```
Error: Failed to start daemon
```

### Causes & Solutions

**1. Socket file already exists**

```bash
# Remove stale socket
rm .claude/hooks-daemon/untracked/daemon-*.sock
.claude/hooks-daemon/bin/hooks-daemon start
```

**2. Permission denied**

```bash
# Check permissions
ls -la .claude/hooks-daemon/untracked/
# Fix ownership if needed
```

**3. Python environment issues**

```bash
# Verify the interpreter and venv the daemon actually resolves
# (reports Python version, venv path and health in one go)
.claude/hooks-daemon/bin/hooks-daemon health

# List every venv the daemon knows about
.claude/hooks-daemon/bin/hooks-daemon list-venvs

# Repair venv
.claude/hooks-daemon/bin/hooks-daemon repair
```

**4. Port already in use**

```bash
# Check for existing daemon processes
ps aux | grep hooks-daemon

# Stop THIS project's daemon. Use the CLI, not pkill: it targets this
# project's PID file only. `pkill -f hooks-daemon` matches every daemon
# on the host, so in a shared PID namespace (a container and its host, or
# two containers sharing a bind mount) it kills OTHER projects' daemons too.
.claude/hooks-daemon/bin/hooks-daemon stop
```

## DEGRADED MODE

### Symptom

```
⚠️  Daemon running in DEGRADED MODE (2 handlers failed to load)
```

### Common Causes

**1. Abstract Method Missing (v2.13.0+ breaking change)**

**Error:**

```
Can't instantiate abstract class MyHandler with abstract method get_acceptance_tests
```

**Solution:**
Add the required method to your handler:

```python
def get_acceptance_tests(self) -> list[AcceptanceTest]:
    """Return acceptance tests for this handler."""
    return []  # Or add actual tests
```

See: `CLAUDE/HANDLER_DEVELOPMENT.md` for details on v2.13.0 changes.

**2. Import Error**

**Error:**

```
ModuleNotFoundError: No module named 'my_dependency'
```

**Solution:**

```bash
# Install a missing dependency into the daemon venv. The venv is
# fingerprint-keyed, so resolve its interpreter rather than guessing a path.
source .claude/hooks-daemon/scripts/lib/resolve_venv.sh
PY="$(resolve_venv_python "$PWD/.claude/hooks-daemon")"
"$PY" -m pip install my_dependency

# Restart daemon
.claude/hooks-daemon/bin/hooks-daemon restart
```

**3. Handler Configuration Error**

**Error:**

```
Invalid handler configuration: priority must be int, not str
```

**Solution:**
Fix `.claude/hooks-daemon.yaml`:

```yaml
# WRONG
handlers:
  pre_tool_use:
    my_handler:
      priority: "50"  # String

# RIGHT
handlers:
  pre_tool_use:
    my_handler:
      priority: 50  # Integer
```

## Upgrade Failures

### Upgrade Script Not Found

**Error:**

```
curl: (404) Not Found
```

**Solution:**
Use the skill command or download-then-run:

Preferred — in the Claude Code chat:

```claude-code
/hooks-daemon upgrade
```

Manual — from a terminal:

```bash
curl -sSL https://raw.githubusercontent.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon/main/scripts/upgrade.sh -o /tmp/hooks-daemon-upgrade.sh
bash /tmp/hooks-daemon-upgrade.sh
```

### Upgrade Hangs

**Symptom:** Upgrade command never completes

**Solution:**

From a terminal:

```bash
# Check daemon logs
.claude/hooks-daemon/bin/hooks-daemon logs

# If stuck, stop THIS project's daemon (never `pkill -f hooks-daemon`
# — that matches every daemon on the host, not just this project's)
.claude/hooks-daemon/bin/hooks-daemon stop
```

Then retry the upgrade in the Claude Code chat:

```claude-code
/hooks-daemon upgrade
```

### Rollback After Failed Upgrade

**Symptom:** New version won't start, need to revert

**Solution:**
The upgrade script auto-rollsback, but if needed manually:

```bash
# Restore backed-up config
cp .claude/hooks-daemon.yaml.backup .claude/hooks-daemon.yaml

# Roll back to the previous version. Run upgrade_version.sh rather than a bare
# `git checkout`: it rebuilds the venv and reinstalls the package for the target
# version. A checkout alone moves the source but leaves the venv holding the
# NEW version's dependencies.
#
# Use `git -C` and absolute paths — never `cd` into .claude/hooks-daemon/
# (the daemon_location_guard handler blocks it, and daemon commands are
# designed to run from the project root).
bash .claude/hooks-daemon/scripts/upgrade_version.sh \
  "$PWD" "$PWD/.claude/hooks-daemon" "v2.12.0"   # previous working version

# Restart
.claude/hooks-daemon/bin/hooks-daemon restart
```

## Handler Not Triggering

### Symptom

Handler exists but never fires

### Debug Steps

**1. Verify handler loaded:**

```bash
.claude/hooks-daemon/bin/hooks-daemon handlers | grep my_handler
```

If not listed, check:

- Handler registered in `.claude/hooks-daemon.yaml`
- Daemon restarted after adding handler
- No syntax errors in handler file

**2. Verify handler matches:**

```bash
# Enable debug logging
export HOOKS_DAEMON_LOG_LEVEL=DEBUG
.claude/hooks-daemon/bin/hooks-daemon restart

# Check logs
.claude/hooks-daemon/bin/hooks-daemon logs | grep my_handler
```

**3. Debug hook event flow:**

```bash
# Capture real events
./scripts/debug_hooks.sh start "Testing my handler"
# ... perform action that should trigger handler ...
./scripts/debug_hooks.sh stop

# Analyze captured events
cat /tmp/hook_debug_*.log | grep -A10 "event_type"
```

See: `CLAUDE/DEBUGGING_HOOKS.md` for complete debugging workflow.

## Configuration Issues

### Invalid YAML Syntax

**Error:**

```
yaml.scanner.ScannerError: mapping values are not allowed here
```

**Solution:**

```bash
# Validate config syntax AND schema (stricter than a bare YAML parse)
.claude/hooks-daemon/bin/hooks-daemon config-validate

# Compare with example
diff .claude/hooks-daemon.yaml .claude/hooks-daemon.yaml.example
```

### Schema Validation Failed

**Error:**

```
Configuration validation failed:
  handlers.pre_tool_use.my_handler.priority: field required
```

**Solution:**
Every handler config must have required fields:

```yaml
handlers:
  pre_tool_use:
    my_handler:
      enabled: true
      priority: 50  # REQUIRED
```

## Performance Issues

### Slow Hook Processing

**Symptom:** Noticeable delay before commands execute

**Causes:**

1. Too many handlers enabled
2. Handler with expensive operations in matches()
3. Handler not setting terminal=True when appropriate

**Solutions:**

```bash
# Disable unused handlers
# Edit .claude/hooks-daemon.yaml, set enabled: false

# Profile handler performance
export HOOKS_DAEMON_LOG_LEVEL=DEBUG
# Check logs for slow handlers

# Optimize handler matching logic
# Move expensive checks from matches() to handle()
```

### High Memory Usage

**Symptom:** Daemon consuming excessive RAM

**Solutions:**

```bash
# Check daemon stats
.claude/hooks-daemon/bin/hooks-daemon status

# Restart daemon to clear caches
.claude/hooks-daemon/bin/hooks-daemon restart

# If persistent, check for handler memory leaks
```

## Socket Connection Issues

### Socket File Missing

**Error:**

```
Error: Socket file not found: .claude/hooks-daemon/untracked/daemon-*.sock
```

**Solution:**

```bash
# Check daemon is running
.claude/hooks-daemon/bin/hooks-daemon status

# If stopped, start it
.claude/hooks-daemon/bin/hooks-daemon start
```

### Permission Denied on Socket

**Error:**

```
PermissionError: [Errno 13] Permission denied: '/path/to/daemon.sock'
```

**Solution:**

```bash
# Fix socket permissions
chmod 600 .claude/hooks-daemon/untracked/daemon-*.sock

# Restart daemon
.claude/hooks-daemon/bin/hooks-daemon restart
```

## Getting Help

### Generate Diagnostic Report

The `bug-report` command collects everything below in one go — prefer it:

```bash
.claude/hooks-daemon/bin/hooks-daemon bug-report "short description of the problem"
# Writes to untracked/bug-reports/ by default; use -o - for stdout.
```

To assemble a report by hand instead:

```bash
# Comprehensive diagnostics (health is the detailed view; status is a summary)
.claude/hooks-daemon/bin/hooks-daemon health > diagnostic-report.txt

# Include daemon logs
.claude/hooks-daemon/bin/hooks-daemon logs >> diagnostic-report.txt

# Include configuration
cat .claude/hooks-daemon.yaml >> diagnostic-report.txt
```

### Report Issues

When reporting issues, include:

1. Daemon version: `git -C .claude/hooks-daemon describe --tags` (there is no `--version` flag)
2. Resolved interpreter: `.claude/hooks-daemon/bin/hooks-daemon list-venvs`
3. OS: `uname -a`
4. Diagnostic report (above)
5. Steps to reproduce

**Report at:** https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon/issues

## Quick Reference

### Essential Commands

```bash
# Status
.claude/hooks-daemon/bin/hooks-daemon status

# Restart
.claude/hooks-daemon/bin/hooks-daemon restart

# Logs
.claude/hooks-daemon/bin/hooks-daemon logs

# Repair
.claude/hooks-daemon/bin/hooks-daemon repair

# Validate config (config-validate takes the config path)
.claude/hooks-daemon/bin/hooks-daemon config-validate .claude/hooks-daemon.yaml
```

### Log Locations

- **Daemon log:** `.claude/hooks-daemon/untracked/daemon-*.log`
- **Hook events:** Captured by `./scripts/debug_hooks.sh`
- **Install log:** `.claude/hooks-daemon/untracked/install.log`

### Configuration Files

- **Config:** `.claude/hooks-daemon.yaml`
- **Example:** `.claude/hooks-daemon.yaml.example`
- **Backup:** `.claude/hooks-daemon.yaml.backup` (created during upgrades)
