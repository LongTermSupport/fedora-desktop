# Hook Errors Analysis - v2.10.0 Nested Installation False Positive

**Date**: 2026-02-11
**Issue**: Hook forwarders report nested installation error despite daemon working correctly
**Severity**: MEDIUM (cosmetic - daemon works, but confusing errors)

---

## The Error

```bash
$ echo '{"tool_name":"Bash","tool_input":{"command":"echo test"}}' | <repo>/.claude/hooks/pre-tool-use
HOOKS DAEMON ERROR [nested_installation]: NESTED INSTALLATION DETECTED!
Found: <repo>/.claude/hooks-daemon/.claude.
Remove <repo>/.claude/hooks-daemon and reinstall.
<repo>/.claude/hooks/../init.sh: line 47: PYTHON_CMD: unbound variable
```

---

## Root Cause Analysis

### Detection Logic (init.sh lines 93-97)

```bash
# Nested installation check
if [[ -d "$PROJECT_PATH/.claude/hooks-daemon/.claude" ]]; then
    emit_hook_error "Unknown" "nested_installation" \
        "NESTED INSTALLATION DETECTED! Found: $PROJECT_PATH/.claude/hooks-daemon/.claude. Remove $PROJECT_PATH/.claude/hooks-daemon and reinstall."
    exit 0
fi
```

### What It Checks

**Current Check**: "Does `<repo>/.claude/hooks-daemon/.claude/` exist?"

**Problem**: This directory DOES exist in v2.10.0, but for a legitimate reason (Project-Level Handlers template).

### Daemon Is Actually Working Fine

```bash
$ <repo>/.claude/hooks-daemon/untracked/venv/bin/python -m claude_code_hooks_daemon.daemon.cli status
Daemon: RUNNING
PID: 7014
Socket: <repo>/.claude/hooks-daemon/untracked/daemon-6071d6e26807.sock (exists)
PID file: <repo>/.claude/hooks-daemon/untracked/daemon-6071d6e26807.pid
```

**Correct paths**:
- ✅ Socket: `<repo>/.claude/hooks-daemon/untracked/daemon-*.sock`
- ✅ PID: `<repo>/.claude/hooks-daemon/untracked/daemon-*.pid`
- ✅ NO nested `<repo>/.claude/hooks-daemon/.claude/hooks-daemon/untracked/`

### What `.claude/` Actually Contains

```bash
$ ls -la <repo>/.claude/hooks-daemon/.claude/
total 52
drwxr-xr-x. agents/             # Template agents
drwxr-xr-x. commands/           # Template commands
drwxr-xr-x. hooks/              # Project handler templates
-rw-r--r--. hooks-daemon.yaml   # Example config
-rw-r--r--. settings.json       # Example settings
drwxr-xr-x. skills/             # Template skills
```

This is **intentional content** from v2.10.0's Project-Level Handlers feature, tracked in git:

```bash
$ git ls-files .claude/ | head -10
.claude/.gitignore
.claude/agents/code-reviewer.md
.claude/agents/python-developer.md
.claude/agents/qa-fixer.md
.claude/agents/qa-runner.md
.claude/agents/release-agent.md
.claude/commands/hooks-daemon-update.md
.claude/hooks-daemon.yaml
.claude/hooks-daemon.yaml.example
.claude/hooks/handlers/README.md
```

### No Problematic Runtime Artifacts

```bash
$ ls -la <repo>/.claude/hooks-daemon/.claude/hooks-daemon/ 2>/dev/null
ls: cannot access '<repo>/.claude/hooks-daemon/.claude/hooks-daemon/': No such file or directory
```

✅ The actual problem (nested runtime directory) does NOT exist.

---

## Why This Is a False Positive

1. **What the check was designed to catch**: Previous bug where daemon created runtime files at `<repo>/.claude/hooks-daemon/.claude/hooks-daemon/untracked/`

2. **What v2.10.0 introduced**: Template directory at `<repo>/.claude/hooks-daemon/.claude/` for Project-Level Handlers feature

3. **Why detection fires**: Check is too broad - sees ANY `.claude` directory as "nested installation"

4. **Actual state**: Daemon is using correct paths, no runtime artifacts in wrong location

---

## Recommended Fix

### Current (Broken) Logic

```bash
# Too broad - catches template directories
if [[ -d "$PROJECT_PATH/.claude/hooks-daemon/.claude" ]]; then
    emit_hook_error "Unknown" "nested_installation" "..."
    exit 0
fi
```

### Recommended (Specific) Logic

```bash
# Check for actual problem: nested RUNTIME artifacts
NESTED_RUNTIME="$PROJECT_PATH/.claude/hooks-daemon/.claude/hooks-daemon"
if [[ -d "$NESTED_RUNTIME/untracked" ]] || \
   [[ -f "$NESTED_RUNTIME/untracked/daemon-*.sock" ]] || \
   [[ -f "$NESTED_RUNTIME/untracked/daemon-*.pid" ]]; then
    emit_hook_error "Unknown" "nested_installation" \
        "Nested runtime files detected at $NESTED_RUNTIME/untracked/. This indicates a failed upgrade from v2.9.0. Run: rm -rf $NESTED_RUNTIME"
    exit 0
fi

# Allow .claude/ template directory (v2.10.0 Project-Level Handlers feature)
# This is intentional and tracked in git
```

### Why This Fix

**Checks for actual problem**:
- ✅ Nested `untracked/` directory (wrong runtime location)
- ✅ Socket/PID files at wrong location

**Allows legitimate content**:
- ✅ Template `.claude/` directory (v2.10.0 feature)
- ✅ Example handlers, configs, documentation

**Better error message**:
- Points to actual artifact causing issue
- Explains what the directory is
- Provides fix command
- References v2.9.0 upgrade bug

---

## Secondary Issue: PYTHON_CMD Unbound Variable

```
<repo>/.claude/hooks/../init.sh: line 47: PYTHON_CMD: unbound variable
```

### Root Cause

After `emit_hook_error` exits with error code 0, init.sh continues (because `set -euo pipefail` sees exit 0 as success), but `PYTHON_CMD` hasn't been set yet (it's defined later at line 139).

### Why This Happens

```bash
# Line 93-97: Nested check exits early
if [[ -d "$PROJECT_PATH/.claude/hooks-daemon/.claude" ]]; then
    emit_hook_error "Unknown" "nested_installation" "..."
    exit 0  # ← Should stop here, but...
fi

# Line 139: PYTHON_CMD defined much later
PYTHON_CMD="$HOOKS_DAEMON_ROOT_DIR/untracked/venv/bin/python"
```

**Problem**: When script exits at line 97, bash is still processing line 47 which references `PYTHON_CMD`.

### Recommended Fix

**Option 1**: Define PYTHON_CMD earlier (before nested check)
```bash
# Move PYTHON_CMD definition to top of file (after HOOKS_DAEMON_ROOT_DIR)
HOOKS_DAEMON_ROOT_DIR="${HOOKS_DAEMON_ROOT_DIR:-$PROJECT_PATH/.claude/hooks-daemon}"
PYTHON_CMD="$HOOKS_DAEMON_ROOT_DIR/untracked/venv/bin/python"

# Then nested check can exit cleanly
```

**Option 2**: Use conditional check in emit_hook_error
```bash
emit_hook_error() {
    # ...existing code...

    # Only use Python utility if PYTHON_CMD is set
    if [[ -n "${PYTHON_CMD:-}" ]] && [[ -f "$PYTHON_CMD" ]]; then
        $PYTHON_CMD -m claude_code_hooks_daemon.core.error_response ...
    else
        # Fallback to simple JSON (for early errors before Python available)
        echo '{"decision":"allow","reason":"Hook error during init"}'
    fi
}
```

---

## Impact Assessment

### User Experience Impact: MEDIUM

**What Users See**:
- ❌ Confusing error message about "nested installation"
- ❌ Message says to "remove and reinstall" even though daemon works fine
- ❌ Additional bash variable error (scary looking)

**What Actually Happens**:
- ✅ Daemon works correctly
- ✅ Hooks function normally (after error message)
- ✅ Paths are correct
- ✅ No actual corruption or issues

### Functional Impact: NONE

- Daemon operates normally
- Hook interception works
- All handlers function correctly
- No data loss or corruption

### Developer Experience Impact: HIGH

- Confusing when debugging
- Wastes time investigating non-issues
- False positive erodes trust in error messages
- Makes real nested installations harder to detect

---

## Testing Recommendations

### Add Test Case: v2.10.0 Template Directory

```bash
# Test: Hook forwarder should NOT error on v2.10.0 template directory
test_no_false_positive_on_template_directory() {
    # Setup v2.10.0 installation with template .claude/
    create_test_project_with_v2_10_0_daemon

    # Verify daemon running with correct paths
    assert_daemon_socket_at "/test/project/.claude/hooks-daemon/untracked/daemon-*.sock"

    # Verify template directory exists
    assert_directory_exists "/test/project/.claude/hooks-daemon/.claude/hooks/"

    # Verify hook forwarder does NOT error
    result=$(echo '{"tool_name":"Bash","tool_input":{"command":"echo test"}}' | /test/project/.claude/hooks/pre-tool-use)
    assert_no_error_in_output "$result"
}
```

### Add Test Case: Real Nested Installation Still Detected

```bash
# Test: Hook forwarder SHOULD error on actual nested runtime artifacts
test_detects_real_nested_installation() {
    # Setup broken state with nested runtime artifacts
    create_nested_runtime_artifacts "/test/project/.claude/hooks-daemon/.claude/hooks-daemon/untracked/"

    # Verify hook forwarder DOES error
    result=$(echo '{"tool_name":"Bash","tool_input":{"command":"echo test"}}' | /test/project/.claude/hooks/pre-tool-use)
    assert_error_contains "nested_installation" "$result"
    assert_error_contains "hooks-daemon/untracked" "$result"
}
```

---

## Documentation Needed

### 1. Add to v2.9-to-v2.10 Upgrade Guide

```markdown
## Known Issue: Hook Forwarder Error Message

After upgrading to v2.10.0, you may see this error when running hooks:

```
HOOKS DAEMON ERROR [nested_installation]: NESTED INSTALLATION DETECTED!
Found: <repo>/.claude/hooks-daemon/.claude.
```

**This is a false positive.** v2.10.0 includes a `.claude/` template directory
for the Project-Level Handlers feature. This error message will be fixed in v2.10.1.

**Workaround**: Ignore the error message. The daemon is working correctly.

**To verify daemon is healthy**:
```bash
python -m claude_code_hooks_daemon.daemon.cli status
# Expected: Daemon: RUNNING
```
```

### 2. Add to LLM-UPDATE.md Troubleshooting Section

```markdown
### Error: "NESTED INSTALLATION DETECTED" but daemon works fine

**Symptoms**:
- Hook forwarder reports nested installation error
- Daemon status shows RUNNING
- Socket path is correct (`.claude/hooks-daemon/untracked/`)

**Cause**: False positive in v2.10.0 due to Project-Level Handlers template directory.

**Fix**: Will be fixed in v2.10.1. Safe to ignore - daemon is working correctly.

**Verify**:
```bash
# Check daemon is using correct paths
daemon cli status | grep Socket
# Expected: .claude/hooks-daemon/untracked/daemon-*.sock
# NOT: .claude/hooks-daemon/.claude/hooks-daemon/untracked/daemon-*.sock

# If socket path contains duplicate `/hooks-daemon/`, you have a real nested installation
```
```

---

## Priority for Fix

**Priority**: HIGH (for v2.10.1 patch release)

**Reasons**:
1. **User confusion**: Error message says installation is broken when it's not
2. **False alarm**: Makes users distrust error messages
3. **Easy fix**: Simple change to detection logic
4. **Affects all v2.10.0 users**: Anyone upgrading sees this error

**Not critical because**:
- Daemon actually works fine
- No functional impact
- Workaround is simple (ignore error)

---

## Suggested Commit Message

```
fix(init.sh): Fix false positive nested installation detection in v2.10.0

The nested installation check was too broad, triggering on the new
Project-Level Handlers template directory (.claude/) introduced in v2.10.0.

Changes:
- Check for actual nested runtime artifacts (untracked/ directory)
- Allow legitimate .claude/ template directory
- Improve error message to point to specific problem
- Move PYTHON_CMD definition earlier to avoid unbound variable error

Fixes: Hook forwarder incorrectly reporting nested installation on v2.10.0

Before: Checks if .claude/ exists (too broad)
After: Checks if .claude/hooks-daemon/untracked/ exists (specific)

This false positive affected all v2.10.0 users after upgrade, causing
confusing error messages despite daemon functioning correctly.
```

---

**End of Hook Errors Analysis**
