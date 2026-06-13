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
$PYTHON -m claude_code_hooks_daemon.daemon.cli start
```

**2. Permission denied**

```bash
# Check permissions
ls -la .claude/hooks-daemon/untracked/
# Fix ownership if needed
```

**3. Python environment issues**

```bash
# Verify Python version (3.11+ required)
$PYTHON --version

# Verify venv exists
ls -la .claude/hooks-daemon/untracked/venv/

# Repair venv
$PYTHON -m claude_code_hooks_daemon.daemon.cli repair
```

**4. Port already in use**

```bash
# Check for existing daemon processes
ps aux | grep hooks-daemon

# Kill stale processes
pkill -f hooks-daemon
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
# Install missing dependency in daemon venv
.claude/hooks-daemon/untracked/venv/bin/pip install my_dependency

# Restart daemon
$PYTHON -m claude_code_hooks_daemon.daemon.cli restart
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

```bash
# Preferred:
/hooks-daemon upgrade

# Manual:
curl -sSL https://raw.githubusercontent.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon/main/scripts/upgrade.sh -o /tmp/hooks-daemon-upgrade.sh
bash /tmp/hooks-daemon-upgrade.sh
```

### Upgrade Hangs

**Symptom:** Upgrade command never completes

**Solution:**

```bash
# Check daemon logs
$PYTHON -m claude_code_hooks_daemon.daemon.cli logs

# If stuck, kill and retry
pkill -f hooks-daemon
/hooks-daemon upgrade
```

### Rollback After Failed Upgrade

**Symptom:** New version won't start, need to revert

**Solution:**
The upgrade script auto-rollsback, but if needed manually:

```bash
# Restore backed-up config
cp .claude/hooks-daemon.yaml.backup .claude/hooks-daemon.yaml

# Checkout previous version
cd .claude/hooks-daemon
git checkout v2.12.0  # Previous working version

# Restart
$PYTHON -m claude_code_hooks_daemon.daemon.cli restart
```

## Handler Not Triggering

### Symptom

Handler exists but never fires

### Debug Steps

**1. Verify handler loaded:**

```bash
$PYTHON -m claude_code_hooks_daemon.daemon.cli handlers | grep my_handler
```

If not listed, check:

- Handler registered in `.claude/hooks-daemon.yaml`
- Daemon restarted after adding handler
- No syntax errors in handler file

**2. Verify handler matches:**

```bash
# Enable debug logging
export HOOKS_DAEMON_LOG_LEVEL=DEBUG
$PYTHON -m claude_code_hooks_daemon.daemon.cli restart

# Check logs
$PYTHON -m claude_code_hooks_daemon.daemon.cli logs | grep my_handler
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
# Validate YAML syntax
$PYTHON -c "import yaml; yaml.safe_load(open('.claude/hooks-daemon.yaml'))"

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
$PYTHON -m claude_code_hooks_daemon.daemon.cli status

# Restart daemon to clear caches
$PYTHON -m claude_code_hooks_daemon.daemon.cli restart

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
$PYTHON -m claude_code_hooks_daemon.daemon.cli status

# If stopped, start it
$PYTHON -m claude_code_hooks_daemon.daemon.cli start
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
$PYTHON -m claude_code_hooks_daemon.daemon.cli restart
```

## Getting Help

### Generate Diagnostic Report

```bash
# Comprehensive diagnostics
$PYTHON -m claude_code_hooks_daemon.daemon.cli status --verbose > diagnostic-report.txt

# Include daemon logs
$PYTHON -m claude_code_hooks_daemon.daemon.cli logs >> diagnostic-report.txt

# Include configuration
cat .claude/hooks-daemon.yaml >> diagnostic-report.txt
```

### Report Issues

When reporting issues, include:

1. Daemon version: `$PYTHON -m claude_code_hooks_daemon.daemon.cli --version`
2. Python version: `$PYTHON --version`
3. OS: `uname -a`
4. Diagnostic report (above)
5. Steps to reproduce

**Report at:** https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon/issues

## Quick Reference

### Essential Commands

```bash
# Status
$PYTHON -m claude_code_hooks_daemon.daemon.cli status

# Restart
$PYTHON -m claude_code_hooks_daemon.daemon.cli restart

# Logs
$PYTHON -m claude_code_hooks_daemon.daemon.cli logs

# Repair
$PYTHON -m claude_code_hooks_daemon.daemon.cli repair

# Validate config
$PYTHON -m claude_code_hooks_daemon.daemon.cli validate-config
```

### Log Locations

- **Daemon log:** `.claude/hooks-daemon/untracked/daemon-*.log`
- **Hook events:** Captured by `./scripts/debug_hooks.sh`
- **Install log:** `.claude/hooks-daemon/untracked/install.log`

### Configuration Files

- **Config:** `.claude/hooks-daemon.yaml`
- **Example:** `.claude/hooks-daemon.yaml.example`
- **Backup:** `.claude/hooks-daemon.yaml.backup` (created during upgrades)
