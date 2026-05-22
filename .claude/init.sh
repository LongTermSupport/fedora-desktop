#!/bin/bash
#
# Claude Code Hooks Daemon - Init Script
#
# Provides shell functions for daemon lifecycle management:
# - is_daemon_running() - Check if daemon is running
# - start_daemon() - Start daemon in background
# - ensure_daemon() - Start daemon if not running (lazy startup)
# - send_request_stdin() - Send JSON from stdin to daemon via Unix socket
# - emit_hook_error() - Output valid hook error JSON to stdout (CRITICAL)
#
# This script is sourced by forwarder scripts (pre-tool-use, post-tool-use, etc.)
#
# CRITICAL: All errors MUST output valid JSON to stdout so Claude can see
# the error and take corrective action. Errors to stderr are invisible to the agent.
#

set -euo pipefail

# Flag set by ensure_daemon when ci_enabled: true and daemon can't start
_HOOKS_DAEMON_CI_ENFORCED=false

# Flag set by ensure_daemon when daemon directory/venv is absent (fresh clone)
_HOOKS_DAEMON_NOT_INSTALLED=false

#
# emit_hook_error() - Output a valid hook error response to stdout
#
# CRITICAL: This ensures the agent sees errors and can take action.
# Outputs JSON in Claude Code's expected hook response format.
#
# DRY: Uses Python utility to generate error responses - single source of truth.
#
# Args:
#   $1 - Event name (e.g., "PreToolUse", "Stop")
#   $2 - Error type (e.g., "daemon_startup_failed")
#   $3 - Error details
#
# Output:
#   Valid JSON hook response to stdout
#   Also logs to stderr for debugging
#
emit_hook_error() {
    local event_name="${1:-Unknown}"
    local error_type="${2:-unknown_error}"
    local error_details="${3:-No details available}"

    # Log to stderr for debugging (agent won't see this)
    echo "HOOKS DAEMON ERROR [$error_type]: $error_details" >&2

    # Build error context message based on CI enforcement policy
    local context_msg

    if [[ "$_HOOKS_DAEMON_CI_ENFORCED" == "true" ]]; then
        # CI ENFORCED: Loud STOP message — project requires daemon via ci_enabled: true
        context_msg=$(printf '%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s' \
            "STOP - DO NOT PROCEED" \
            "" \
            "The hooks daemon is REQUIRED for this project (ci_enabled: true in .claude/hooks-daemon.yaml) but is NOT installed or running in this environment." \
            "" \
            "ALL safety handlers, code quality checks, and workflow enforcement are INACTIVE." \
            "" \
            "ACTION REQUIRED:" \
            "1. STOP all work immediately - do not use any tools" \
            "2. Report to the user: 'Hooks daemon must be installed before running Claude Code agents in this environment'" \
            "3. Use the hooks-daemon skill to install (Skill tool: skill=hooks-daemon, args=install)" \
            "" \
            "DO NOT continue working without the daemon.")
    elif [[ "$_HOOKS_DAEMON_NOT_INSTALLED" == "true" ]]; then
        # NOT INSTALLED: Guide to install guide — project was cloned but daemon never set up
        context_msg=$(printf '%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s' \
            "HOOKS DAEMON: Not installed" \
            "" \
            "This project uses the Claude Code Hooks Daemon for safety enforcement," \
            "but the daemon is not installed in this environment." \
            "" \
            "ALL safety handlers, code quality checks, and workflow enforcement are INACTIVE." \
            "" \
            "TO INSTALL — use the hooks-daemon skill (do not improvise):" \
            "  Use the hooks-daemon skill to install (Skill tool: skill=hooks-daemon, args=install)" \
            "" \
            "After installing, restart your Claude session for hooks to activate.")
    else
        # Standard error message
        # NOTE: Language is intentionally measured to avoid triggering investigation loops
        # in LLM agents. Previous "STOP work immediately" wording caused agents to abandon
        # tasks and enter analysis cycles instead of simply restarting the daemon.
        context_msg=$(printf '%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s' \
            "HOOKS DAEMON: Not currently running" \
            "" \
            "Error: $error_type - $error_details" \
            "" \
            "Hook safety handlers are inactive until the daemon is restarted." \
            "If you are in the middle of an upgrade, this is expected and temporary." \
            "" \
            "TO FIX (usually takes a few seconds):" \
            "Use the hooks-daemon skill to restart the daemon." \
            "Then use the hooks-daemon skill to verify health." \
            "Invoke via Skill tool with skill=hooks-daemon and args=restart or args=health." \
            "" \
            "If restart fails, use the hooks-daemon skill to check logs (args=logs)." \
            "Then inform the user if the issue persists.")
    fi

    # Event-specific JSON formatting using jq (already a dependency)
    # Stop/SubagentStop: top-level decision only (deny to show error)
    # Other events: hookSpecificOutput with context (fail-open allow)
    if command -v jq &>/dev/null; then
        if [[ "$_HOOKS_DAEMON_CI_ENFORCED" == "true" ]]; then
            # CI enforced: hard deny/block for ALL event types to prevent work
            local ci_reason="Hooks daemon REQUIRED (ci_enabled: true) but not installed"
            if [[ "$event_name" == "PreToolUse" ]]; then
                jq -n --arg reason "$context_msg" \
                    '{"decision": "deny", "reason": $reason}'
            elif [[ "$event_name" == "Stop" || "$event_name" == "SubagentStop" ]]; then
                jq -n --arg reason "$ci_reason" \
                    '{"decision": "block", "reason": $reason}'
            else
                jq -n --arg event "$event_name" --arg context "$context_msg" \
                    '{"hookSpecificOutput": {"hookEventName": $event, "additionalContext": $context}}'
            fi
        elif [[ "$_HOOKS_DAEMON_NOT_INSTALLED" == "true" ]]; then
            # Not installed: Stop/SubagentStop block, others fail-open with install guidance
            if [[ "$event_name" == "Stop" || "$event_name" == "SubagentStop" ]]; then
                jq -n --arg reason "Hooks daemon not installed - protection not active" \
                    '{"decision": "block", "reason": $reason}'
            else
                jq -n --arg event "$event_name" --arg context "$context_msg" \
                    '{"hookSpecificOutput": {"hookEventName": $event, "additionalContext": $context}}'
            fi
        else
            # Standard: Stop/SubagentStop block, others fail-open with context
            if [[ "$event_name" == "Stop" || "$event_name" == "SubagentStop" ]]; then
                jq -n --arg reason "Hooks daemon not running - protection not active" \
                    '{"decision": "block", "reason": $reason}'
            else
                jq -n --arg event "$event_name" --arg context "$context_msg" \
                    '{"hookSpecificOutput": {"hookEventName": $event, "additionalContext": $context}}'
            fi
        fi
    else
        # Fallback if jq not available (should not happen - jq is required)
        cat <<FALLBACK_EOF
{"hookSpecificOutput":{"hookEventName":"$event_name","additionalContext":"HOOKS DAEMON ERROR: $error_type - $error_details"}}
FALLBACK_EOF
    fi
}

# Detect project path by walking up from init.sh's directory.
# (init.sh lives at .claude/init.sh, so its parent contains the project.)
_INIT_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_PATH="${_INIT_SCRIPT_DIR}"

# Walk up to find .claude directory
while [[ "$PROJECT_PATH" != "/" ]]; do
    if [[ -d "$PROJECT_PATH/.claude" ]]; then
        break
    fi
    PROJECT_PATH="$(dirname "$PROJECT_PATH")"
done

if [[ "$PROJECT_PATH" == "/" ]]; then
    # Output valid JSON error to stdout - event name unknown at this point
    emit_hook_error "Unknown" "init_path_error" "Could not find .claude directory in path hierarchy. Hooks daemon cannot initialize."
    exit 0  # Exit 0 so Claude Code processes the JSON response
fi

# Load environment overrides if present (for self-installation or custom setups)
if [[ -f "$PROJECT_PATH/.claude/hooks-daemon.env" ]]; then
    # shellcheck disable=SC1091
    source "$PROJECT_PATH/.claude/hooks-daemon.env"
fi

# Set daemon root directory (defaults to .claude/hooks-daemon, can be overridden)
HOOKS_DAEMON_ROOT_DIR="${HOOKS_DAEMON_ROOT_DIR:-$PROJECT_PATH/.claude/hooks-daemon}"

#
# Nested installation check
#
# Detects if hooks-daemon has been installed inside itself creating
# .claude/hooks-daemon/.claude/hooks-daemon structure
#
if [[ -d "$PROJECT_PATH/.claude/hooks-daemon/.claude/hooks-daemon" ]]; then
    emit_hook_error "Unknown" "nested_installation" \
        "NESTED INSTALLATION DETECTED! Found: $PROJECT_PATH/.claude/hooks-daemon/.claude/hooks-daemon. Remove $PROJECT_PATH/.claude/hooks-daemon and reinstall."
    exit 0
fi

#
# Git remote detection for self-install validation
#
# If this is the hooks-daemon repo itself (detected by git remote),
# require self_install_mode in config or HOOKS_DAEMON_ROOT_DIR override
#
is_hooks_daemon_repo() {
    local remote_url
    remote_url=$(git -C "$PROJECT_PATH" remote get-url origin 2>/dev/null || echo "")
    remote_url=$(echo "$remote_url" | tr '[:upper:]' '[:lower:]')

    if [[ "$remote_url" == *"claude-code-hooks-daemon"* ]] || \
       [[ "$remote_url" == *"claude_code_hooks_daemon"* ]]; then
        return 0  # true - is hooks-daemon repo
    fi
    return 1  # false - not hooks-daemon repo
}

# Check if we're in the hooks-daemon repo without proper configuration
if [[ -d "$PROJECT_PATH/.git" ]]; then
    if is_hooks_daemon_repo; then
        # Check if self_install_mode is enabled in config or env override is set
        has_self_install=false

        # Check HOOKS_DAEMON_ROOT_DIR override (from hooks-daemon.env)
        if [[ "$HOOKS_DAEMON_ROOT_DIR" == "$PROJECT_PATH" ]]; then
            has_self_install=true
        fi

        # Check config file for self_install_mode (requires Python, done later)
        # For now, just trust the HOOKS_DAEMON_ROOT_DIR override

        if [[ "$has_self_install" != "true" ]] && [[ ! -f "$PROJECT_PATH/.claude/hooks-daemon.env" ]]; then
            emit_hook_error "Unknown" "hooks_daemon_repo_detected" \
                "This is the hooks-daemon repository. To install for development, run: python install.py --self-install"
            exit 0
        fi
    fi
fi

# Venv Python (only needed for daemon startup, NOT for hot path).
#
# Plan 00099: venv is keyed by Python-environment fingerprint so that
# concurrent containers from the same image share one venv while distinct
# Pythons (pyenv vs distro, different minor versions, cross-arch) are kept
# apart. The fingerprint computation invokes Python, so it is deferred to
# `_resolve_python_cmd()` which is called only from `start_daemon()` /
# `validate_venv()` — never on the hot path.
#
# Precedence (highest first):
#   1. $HOOKS_DAEMON_VENV_PATH (explicit override)
#   2. $HOOKS_DAEMON_ROOT_DIR/untracked/venv-{fingerprint}/ (fingerprint-keyed)
#   3. $HOOKS_DAEMON_ROOT_DIR/untracked/venv-*/ (any existing fingerprint venv)
#
# Plan 00103 Decision 2: when none of the above resolve, fail loudly with
# return 5 + stderr directive. The pre-v3.7.0 unversioned legacy
# `untracked/venv/bin/python` is no longer accepted as a silent fallback —
# it hid the v3.9.0 field-bug regression where operators saw "venv not
# found" while the real cause was a 3.9-vs-3.11 `import tomllib` crash.
#
# Plan 00103 Decision 3 Rule A: no `${VAR:-python3}` parameter expansion —
# the fingerprint helper is invoked under a venv-resident interpreter
# (HOOKS_DAEMON_PYTHON or a discovered venv-*/bin/python), never bare
# `python3`. The scan-fallback handles cross-fingerprint resolution.
PYTHON_CMD=""  # populated lazily by _resolve_python_cmd

# Plan 00104 Phase 4: delegate to canonical library at
# ${HOOKS_DAEMON_ROOT_DIR}/scripts/lib/resolve_venv.sh. The library invokes
# paths.py SSOT — including the metadata-authoritative step (Plan 00100
# Task 3.5) — so init.sh, install/venv_resolver.sh, _resolve-venv.sh, and
# venv-include.bash all converge on the same venv. This closes the drift
# the v3.9.x bash scan-fallback created: alphabetic ordering picked the
# wrong fingerprint when two venvs coexisted, while the SSOT correctly
# preferred the lock_hash-matching one.
_resolve_python_cmd() {
    local lib="${HOOKS_DAEMON_ROOT_DIR}/scripts/lib/resolve_venv.sh"
    if [ ! -f "$lib" ]; then
        echo "❌ _resolve_python_cmd: canonical library missing at $lib" >&2
        echo "   Reinstall the daemon so scripts/lib/resolve_venv.sh is present." >&2
        PYTHON_CMD=""
        return 5
    fi

    # shellcheck disable=SC1090  # path is computed at runtime
    source "$lib"

    if PYTHON_CMD="$(resolve_venv_python "$HOOKS_DAEMON_ROOT_DIR")"; then
        return 0
    fi

    local rv=$?
    PYTHON_CMD=""
    return "$rv"
}

#
# _get_hostname_suffix() - Get hostname-based suffix for runtime files
#
# Uses HOSTNAME environment variable directly to isolate daemon runtime
# files across different environments (containers, machines).
#
# Returns:
#   "-{sanitized-hostname}" or "-{time-hash}" if no hostname
#
# Example:
#   HOSTNAME="laptop" -> "-laptop"
#   HOSTNAME="506355bfbc76" -> "-506355bfbc76"
#   HOSTNAME="My-Server" -> "-my-server"
#   No HOSTNAME -> "-a1b2c3d4" (MD5 of timestamp)
#
_get_hostname_suffix() {
    local hostname="${HOSTNAME:-}"

    # No hostname? Use MD5 of current time for uniqueness
    if [[ -z "$hostname" ]]; then
        local timestamp
        timestamp=$(date +%s.%N)
        local hash
        hash=$(echo -n "$timestamp" | md5sum | cut -c1-8)
        echo "-${hash}"
        return 0
    fi

    # Sanitize hostname for filesystem safety: lowercase, no spaces
    local sanitized
    sanitized=$(echo "$hostname" | tr '[:upper:]' '[:lower:]' | tr ' ' '-')
    echo "-${sanitized}"
}

#
# _exec_bit_selfheal() - Restore +x on sibling hook scripts (Plan 00102 Phase 3).
#
# Defense-in-depth: even though the daemon's invocation form is `bash <abs-path>`
# (Phase 1, makes the bit irrelevant), defensively restore the executable bit on
# sibling hook wrappers if it has been dropped by core.fileMode=false, an IDE
# rewrite, a tarball/ZIP transfer, etc. Throttled once per hour via mtime on a
# fingerprint file so the cost is amortised across hook invocations.
#
# Variables required in scope:
#   HOOK_SCRIPT_DIR - directory containing hook wrapper scripts
#   _untracked_dir  - daemon's untracked dir (where the throttle file lives)
#
_exec_bit_selfheal() {
    local throttle="$_untracked_dir/.exec-bit-checked"
    local now mtime
    now=$(date +%s)

    if [[ -f "$throttle" ]]; then
        # Linux: stat -c %Y. macOS/BSD: stat -f %m. If both fail we fall
        # through to running the chmod (safer than silently skipping).
        if mtime=$(stat -c %Y "$throttle" 2>/dev/null); then
            :
        elif mtime=$(stat -f %m "$throttle" 2>/dev/null); then
            :
        else
            mtime=0
        fi

        if [[ "$mtime" =~ ^[0-9]+$ ]] && [[ $((now - mtime)) -lt 3600 ]]; then
            return 0
        fi
    fi

    local hooks=(
        pre-tool-use
        post-tool-use
        session-start
        session-end
        stop
        subagent-stop
        user-prompt-submit
        notification
        pre-compact
        permission-request
    )
    local h
    for h in "${hooks[@]}"; do
        local p="$HOOK_SCRIPT_DIR/$h"
        if [[ -f "$p" ]]; then
            chmod +x "$p"
        fi
    done

    touch "$throttle"
}

# Generate socket and PID paths using pure bash (no Python dependency)
# SECURITY: Paths stored in daemon's untracked directory, NOT /tmp
# Pattern: {project}/.claude/hooks-daemon/untracked/daemon.{sock|pid}
# Container: {project}/.claude/hooks-daemon/untracked/daemon-{hash}.{sock|pid}
# Must match Python paths module: claude_code_hooks_daemon.daemon.paths
_abs_project_path=$(realpath "$PROJECT_PATH")

# Determine untracked directory path
# Must match ProjectContext.daemon_untracked_dir() logic
# Use HOOKS_DAEMON_ROOT_DIR (set by .env in self-install, defaults to .claude/hooks-daemon)
_untracked_dir="${HOOKS_DAEMON_ROOT_DIR}/untracked"

# Create untracked directory if it doesn't exist
mkdir -p "$_untracked_dir"

# Plan 00102 Phase 3 (Tier 3a): defensively restore +x on sibling hook
# wrappers if dropped (core.fileMode=false, IDE rewrite, tarball transfer).
# Throttled once per hour via mtime on $_untracked_dir/.exec-bit-checked.
HOOK_SCRIPT_DIR="$PROJECT_PATH/.claude/hooks"
_exec_bit_selfheal

# Generate hostname-based suffix for path isolation
_hostname_suffix=$(_get_hostname_suffix)

# Allow environment variable overrides (for testing)
SOCKET_PATH="${CLAUDE_HOOKS_SOCKET_PATH:-$_untracked_dir/daemon${_hostname_suffix}.sock}"
PID_PATH="${CLAUDE_HOOKS_PID_PATH:-$_untracked_dir/daemon${_hostname_suffix}.pid}"

# Socket discovery file: when the default socket path exceeds the AF_UNIX
# length limit (108 bytes), the Python daemon falls back to a shorter path
# (XDG_RUNTIME_DIR, /run/user/, or /tmp) and writes the actual socket path
# to a discovery file. Read it if the default socket doesn't exist.
if [[ -z "${CLAUDE_HOOKS_SOCKET_PATH:-}" ]] && [[ ! -S "$SOCKET_PATH" ]]; then
    _discovery_file="$_untracked_dir/daemon${_hostname_suffix}.socket-path"
    if [[ -f "$_discovery_file" ]]; then
        _discovered_path=$(cat "$_discovery_file" 2>/dev/null)
        if [[ -n "$_discovered_path" ]] && [[ -S "$_discovered_path" ]]; then
            SOCKET_PATH="$_discovered_path"
        fi
    fi
fi

# Daemon startup timeout (deciseconds - 1/10th second units).
#
# 15 seconds matches Timeout.DAEMON_RESTART_VERIFY_TIMEOUT_SEC, the
# python-side ceiling used by scripts/install/daemon_control.sh::
# restart_daemon_verified (Plan 00100 Task 0.2). Cold-start Python
# with 50+ handler imports + config load + asyncio bind can take 5-10s
# on slow disks (containers, cold caches). The pre-Issue-1 ceiling of
# 50 deciseconds (5s) produced false `daemon_startup_failed` reports
# while the daemon was still binding — see Issue 1 in
# untracked/hooks-daemon-niggles.md (2026-05-14 field report).
DAEMON_STARTUP_TIMEOUT=150

# Daemon startup check interval (deciseconds)
DAEMON_STARTUP_CHECK_INTERVAL=1

# Export paths for use by forwarder scripts
export HOOKS_DAEMON_ROOT_DIR
export SOCKET_PATH
export PID_PATH
export PROJECT_PATH
# Note: PYTHON_CMD is intentionally NOT exported - only used internally
# by start_daemon() and validate_venv(). Hot path uses system python3.

#
# validate_venv() - Check if venv is healthy for daemon startup
#
# Returns:
#   0 if venv is healthy
#   1 if venv is broken (outputs diagnostic to stderr)
#
# Sets VENV_ERROR with human-readable error message on failure.
#
validate_venv() {
    VENV_ERROR=""

    # Plan 00099: lazy fingerprint-keyed venv resolution (paid only on daemon
    # startup, never on hot path). No-op on subsequent calls.
    #
    # Plan 00103 Decision 2: _resolve_python_cmd returns 5 + stderr on
    # failure instead of silently emitting the legacy path. Capture the
    # return code explicitly — a bare call would propagate via set -e and
    # kill init.sh sourcing before validate_venv's caller-friendly
    # VENV_ERROR diagnostic can be reported.
    if [ -z "$PYTHON_CMD" ]; then
        local resolve_rv=0
        _resolve_python_cmd || resolve_rv=$?
        if [ "$resolve_rv" -ne 0 ]; then
            VENV_ERROR="Venv Python could not be resolved (exit $resolve_rv). Run: cd $HOOKS_DAEMON_ROOT_DIR && uv sync"
            return 1
        fi
    fi

    # Check venv Python binary exists
    if [[ -z "$PYTHON_CMD" || ! -f "$PYTHON_CMD" ]]; then
        VENV_ERROR="Venv Python not found at ${PYTHON_CMD:-<unresolved>}. Run: cd $HOOKS_DAEMON_ROOT_DIR && uv sync"
        return 1
    fi

    # Check venv Python is executable
    if [[ ! -x "$PYTHON_CMD" ]]; then
        VENV_ERROR="Venv Python not executable at $PYTHON_CMD. Run: cd $HOOKS_DAEMON_ROOT_DIR && uv sync"
        return 1
    fi

    # Check key package is importable
    if ! "$PYTHON_CMD" -c "import claude_code_hooks_daemon" 2>/dev/null; then
        VENV_ERROR="Cannot import claude_code_hooks_daemon. Venv may be broken (stale .pth files or Python version mismatch). Run: cd $HOOKS_DAEMON_ROOT_DIR && uv sync"
        return 1
    fi

    return 0
}

#
# is_daemon_running() - Check if daemon is running
#
# Returns:
#   0 if daemon is running
#   1 if daemon is not running
#
is_daemon_running() {
    # Check if PID file exists
    if [[ ! -f "$PID_PATH" ]]; then
        return 1
    fi

    # Read PID from file
    local pid
    pid=$(cat "$PID_PATH" 2>/dev/null || echo "")

    if [[ -z "$pid" ]]; then
        return 1
    fi

    # Check if process is alive
    if kill -0 "$pid" 2>/dev/null; then
        return 0
    else
        # Stale PID file, clean up
        rm -f "$PID_PATH"
        return 1
    fi
}

#
# start_daemon() - Start daemon in background
#
# Launches daemon process and waits for Unix socket to be ready.
# Daemon starts in background and detaches from terminal.
#
# Returns:
#   0 if daemon started successfully
#   1 if daemon failed to start
#
start_daemon() {
    # Check if already running
    if is_daemon_running; then
        return 0
    fi

    # Validate venv before attempting startup (fail-fast with actionable error)
    if ! validate_venv; then
        echo "ERROR: Venv validation failed: $VENV_ERROR" >&2
        return 1
    fi

    # Remove stale socket file
    rm -f "$SOCKET_PATH"

    # Start daemon using CLI (proper daemonization)
    # CRITICAL: Pass --project-root and export env vars so the CLI uses the
    # same paths we computed above. Without this, the CLI re-discovers the
    # project from CWD which may find a worktree's .claude/ instead of ours.
    CLAUDE_HOOKS_SOCKET_PATH="$SOCKET_PATH" \
    CLAUDE_HOOKS_PID_PATH="$PID_PATH" \
    $PYTHON_CMD -m claude_code_hooks_daemon.daemon.cli \
        --project-root "$PROJECT_PATH" start \
        > /dev/null 2>&1

    # Wait for daemon to be ready (using deciseconds for integer arithmetic).
    #
    # Issue 1 (untracked/hooks-daemon-niggles.md, 2026-05-14): the legacy
    # check polled socket existence alone. enforce_single_daemon_process can
    # leave a transient socket file on disk during a kill+respawn cycle, so
    # the socket file alone is not a reliable readiness signal. Combine with
    # is_daemon_running (PID alive) to guarantee the daemon we spawned is
    # the one we see.
    local elapsed=0
    while [[ $elapsed -lt $DAEMON_STARTUP_TIMEOUT ]]; do
        if is_daemon_running && [[ -S "$SOCKET_PATH" ]]; then
            return 0
        fi

        # Sleep 0.1 seconds (1 decisecond)
        sleep 0.1
        elapsed=$((elapsed + DAEMON_STARTUP_CHECK_INTERVAL))
    done

    # Final retry: the daemon may have bound the socket on the very tick
    # the loop's `elapsed < TIMEOUT` check went false. One more probe
    # before declaring failure closes the boundary race.
    if is_daemon_running && [[ -S "$SOCKET_PATH" ]]; then
        return 0
    fi

    # Genuine timeout. NOTE: do NOT unlink PID_PATH — if the daemon is still
    # coming up, the PID slot belongs to it. is_daemon_running() cleans
    # stale PID files on next call when the process is actually dead.
    echo "ERROR: Daemon startup timeout (daemon not ready after ${DAEMON_STARTUP_TIMEOUT}/10 seconds)" >&2
    return 1
}

#
# _is_ci_enforced() - Check if ci_enabled: true in daemon config
#
# Parses .claude/hooks-daemon.yaml for the ci_enabled flag under daemon section.
# Uses grep (universally available — no Python/yq dependency needed in CI).
#
# Returns:
#   0 if ci_enabled: true found (daemon is required)
#   1 otherwise (default: fail open)
#
_is_ci_enforced() {
    local config_file="$PROJECT_PATH/.claude/hooks-daemon.yaml"
    [[ -f "$config_file" ]] && grep -qE '^\s+ci_enabled:\s*true' "$config_file"
}

#
# _is_daemon_installed() - Check if daemon is installed (dir + venv Python present)
#
# Distinguishes "not installed" (fresh clone) from "installed but not running".
# Used by ensure_daemon() to set _HOOKS_DAEMON_NOT_INSTALLED for better error messages.
#
# Returns:
#   0 if daemon appears installed
#   1 if daemon directory or venv Python is absent
#
_is_daemon_installed() {
    [[ -d "$HOOKS_DAEMON_ROOT_DIR" ]] && [[ -f "$PYTHON_CMD" ]]
}

#
# _is_ci_environment() - Detect if running in any CI/CD environment
#
# Checks common CI environment variables across major platforms.
# Used to determine whether to enter passthrough mode on daemon failure.
#
# Returns:
#   0 if running in CI (passthrough allowed)
#   1 if not CI (fail with error)
#
_is_ci_environment() {
    # Standard flag — GitHub Actions, GitLab CI, CircleCI, Travis, Bitbucket, Buildkite...
    [[ -n "${CI:-}" ]] && return 0
    # GitHub Actions (belt-and-suspenders)
    [[ -n "${GITHUB_ACTIONS:-}" ]] && return 0
    # GitLab CI (belt-and-suspenders)
    [[ -n "${GITLAB_CI:-}" ]] && return 0
    # Jenkins (does not set CI)
    [[ -n "${JENKINS_URL:-}" ]] && return 0
    # Azure DevOps (does not set CI)
    [[ -n "${TF_BUILD:-}" ]] && return 0
    return 1
}

#
# _passthrough_flag_path() - Get path to passthrough state file
#
# State file prevents repeated config parsing and noise on every hook call.
# Created on first daemon failure when ci_enabled is NOT true.
# Cleaned up when daemon starts successfully (recovery).
#
_passthrough_flag_path() {
    local passthrough_dir="$HOOKS_DAEMON_ROOT_DIR/untracked"
    if [[ ! -d "$passthrough_dir" ]]; then
        if ! mkdir -p "$passthrough_dir" 2>/dev/null; then
            echo "HOOKS DAEMON: Could not create passthrough state directory: $passthrough_dir" >&2
        fi
    fi
    echo "$passthrough_dir/.hooks-passthrough"
}

#
# _enter_passthrough_mode() - Override send_request_stdin to return empty JSON
#
# When daemon is unavailable and ci_enabled is not set, hook events
# silently pass through with no blocking and no context injection.
#
_enter_passthrough_mode() {
    # shellcheck disable=SC2317
    send_request_stdin() {
        cat > /dev/null
        echo '{}'
    }
    export -f send_request_stdin
}

#
# ensure_daemon() - Start daemon if not running (lazy startup)
#
# Idempotent function safe to call on every hook invocation.
# Only starts daemon if not already running.
#
# When daemon cannot start:
#   - If ci_enabled: true in config: hard fail (return 1), forwarder blocks
#   - If CI environment detected: passthrough mode (daemon not installed in pipeline)
#   - Otherwise (non-CI dev environment): fail with error (return 1), forwarder
#     calls emit_hook_error so agent sees "Not currently running" and can restart
#
# Returns:
#   0 if daemon is running or CI passthrough mode active
#   1 if daemon failed and must report error to agent
#
ensure_daemon() {
    if is_daemon_running; then
        # Daemon running — clean up stale CI passthrough flag if present
        local passthrough_flag
        passthrough_flag=$(_passthrough_flag_path)
        rm -f "$passthrough_flag" 2>/dev/null
        return 0
    fi

    local passthrough_flag
    passthrough_flag=$(_passthrough_flag_path)

    # CI optimisation: skip start attempt if passthrough flag exists
    # (daemon not installed in CI — no point trying repeatedly)
    if _is_ci_environment && [[ -f "$passthrough_flag" ]] && ! _is_ci_enforced; then
        _enter_passthrough_mode
        return 0
    fi

    # Try to start daemon
    if start_daemon; then
        rm -f "$passthrough_flag" 2>/dev/null
        return 0
    fi

    # Daemon failed to start — determine response based on environment/config

    # ci_enabled: true — hard fail regardless of environment
    if _is_ci_enforced; then
        _HOOKS_DAEMON_CI_ENFORCED=true
        return 1
    fi

    # CI environment (but not enforced): passthrough mode — daemon simply not installed
    if _is_ci_environment; then
        echo "HOOKS DAEMON: Daemon unavailable in CI environment — passthrough mode active (handlers inactive)" >&2
        echo "HOOKS DAEMON: All operations will proceed without safety checks" >&2
        if ! touch "$passthrough_flag" 2>/dev/null; then
            echo "HOOKS DAEMON: Could not write passthrough state file (noise will repeat)" >&2
        fi

        # First call: return one-time advisory context so agent sees the warning once
        # shellcheck disable=SC2317
        send_request_stdin() {
            local input
            input=$(cat)
            local event_name
            event_name=$(echo "$input" | python3 -c "import sys,json; print(json.loads(sys.stdin.read()).get('event','Unknown'))" 2>/dev/null || echo "Unknown")
            if command -v jq >/dev/null; then
                jq -n --arg event "$event_name" \
                    --arg context "HOOKS DAEMON: Not installed in CI environment. Safety handlers are INACTIVE. All operations allowed without validation. This warning appears once." \
                    '{"hookSpecificOutput": {"hookEventName": $event, "additionalContext": $context}}'
            else
                echo '{}'
            fi
        }
        export -f send_request_stdin
        return 0
    fi

    # Non-CI environment: fail with error so agent sees it and can act
    # Distinguish not-installed (fresh clone) from installed-but-not-starting
    if ! _is_daemon_installed; then
        _HOOKS_DAEMON_NOT_INSTALLED=true
    fi
    return 1
}

#
# send_request_stdin() - Send JSON from stdin to daemon via Unix socket
#
# CRITICAL: Reads JSON from stdin and sends directly to daemon.
# NEVER pass JSON through shell variables - control characters break.
#
# On error, outputs valid JSON hook response to stdout so the agent can
# see the error and take corrective action. This is CRITICAL for safety.
#
# Returns:
#   0 always (errors output JSON to stdout, not exit codes)
#
# Usage:
#   cat input.json | send_request_stdin
#   echo '{"key":"value"}' | send_request_stdin
#
send_request_stdin() {
    # Use system python3 for reliable JSON transport - no venv dependency
    # Only uses stdlib: socket, sys, json (no venv packages needed)
    # CRITICAL: On error, outputs valid JSON to stdout (not stderr) so agent sees it
    python3 -c "
import json
import socket
import sys

def emit_error_json(event_name, error_type, error_details):
    '''Output valid hook error response to stdout.

    Inlines JSON generation using only stdlib (no venv dependency).
    Handles event-specific formatting: Stop/SubagentStop vs other events.
    '''
    print(f'HOOKS DAEMON ERROR [{error_type}]: {error_details}', file=sys.stderr)

    context_lines = [
        'HOOKS DAEMON: Not currently running',
        '',
        f'Error: {error_type} - {error_details}',
        '',
        'Hook safety handlers are inactive until the daemon is restarted.',
        'If you are in the middle of an upgrade, this is expected and temporary.',
        '',
        'TO FIX (usually takes a few seconds):',
        'Use the hooks-daemon skill to restart the daemon.',
        'Then use the hooks-daemon skill to verify health.',
        'Invoke via Skill tool with skill=hooks-daemon and args=restart or args=health.',
        '',
        'If restart fails, use the hooks-daemon skill to check logs (args=logs).',
        'Then inform the user if the issue persists.',
    ]
    context = chr(10).join(context_lines)

    # Stop/SubagentStop: top-level decision only (deny to show error)
    if event_name in ('Stop', 'SubagentStop'):
        response = {
            'decision': 'block',
            'reason': 'Hooks daemon not running - protection not active',
        }
    else:
        # Other events: hookSpecificOutput with context (fail-open allow)
        response = {
            'hookSpecificOutput': {
                'hookEventName': event_name,
                'additionalContext': context,
            }
        }
    print(json.dumps(response))

# Read JSON from stdin (preserves all control characters)
request = sys.stdin.read()

# Try to extract event name from request for better error messages
event_name = 'Unknown'
try:
    req_data = json.loads(request)
    event_name = req_data.get('event', 'Unknown')
except Exception:
    pass

# Add newline if not present (daemon expects newline-terminated JSON)
if not request.endswith('\n'):
    request += '\n'

socket_path = '$SOCKET_PATH'

try:
    sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    sock.settimeout(30)  # 30 second timeout
    sock.connect(socket_path)
    sock.sendall(request.encode('utf-8'))
    sock.shutdown(socket.SHUT_WR)

    response = b''
    while True:
        chunk = sock.recv(4096)
        if not chunk:
            break
        response += chunk

    sock.close()

    # Output response (strip trailing newline for clean output)
    output = response.decode('utf-8').rstrip('\n')
    print(output)
    sys.exit(0)

except socket.timeout:
    emit_error_json(event_name, 'socket_timeout',
        f'Socket timeout (30s) connecting to daemon at {socket_path}. '
        'Daemon may be hung or overloaded.')
    sys.exit(0)  # Exit 0 so Claude processes the JSON response

except FileNotFoundError:
    emit_error_json(event_name, 'socket_not_found',
        f'Daemon socket not found at {socket_path}. '
        'Daemon may not be running or socket was deleted.')
    sys.exit(0)

except ConnectionRefusedError:
    emit_error_json(event_name, 'connection_refused',
        f'Daemon refusing connections at {socket_path}. '
        'Daemon may be shutting down or in error state.')
    sys.exit(0)

except Exception as e:
    emit_error_json(event_name, type(e).__name__,
        f'{type(e).__name__}: {e}')
    sys.exit(0)
"
    return $?
}

#
# forward_stop_event() - Forward a Stop / SubagentStop event to the daemon
#                        and translate decision=block into exit-code-2 + stderr
#
# Plan 00101 Phase 9: Claude Code v2.1.114 silently demotes JSON-via-stdout
# `{"decision":"block"}` to `level: suggestion, preventedContinuation: false`,
# breaking the auto_continue_stop contract. The daemon CANNOT control the
# hook subprocess exit code from inside its own Python process — only the
# bash wrapper that Claude Code spawns can set it. This helper centralises
# the translation so both .claude/hooks/stop and .claude/hooks/subagent-stop
# stay one-liners and the JSON-to-exit-code mapping lives in one place.
#
# Behaviour:
#   1. Pipe stdin JSON → jq wrap → send_request_stdin
#   2. Capture daemon response, echo to stdout (back-compat for agent JSON
#      visibility + existing test invariants).
#   3. Parse `.decision`:
#        - "block" → print `.reason` to stderr, exit 2 (hard re-entry).
#        - other  → exit 0 (allow stop).
#
# Args:
#   $1 - event_name: "Stop" or "SubagentStop"
#
# Reads:
#   stdin: Claude Code hook input JSON
#
# Returns:
#   2 if daemon emits decision=block
#   0 otherwise (including daemon socket errors — those are handled by
#     send_request_stdin's emit_error_json which already returns a block
#     payload and we DO want hard re-entry on daemon-down).
#
forward_stop_event() {
    local event_name="$1"
    if [ -z "$event_name" ]; then
        echo '{"error":"forward_stop_event: event_name required"}' >&2
        return 1
    fi

    local response_file
    response_file="$(mktemp)"
    # shellcheck disable=SC2064  # intentional early-binding of file path
    trap "rm -f '$response_file'" EXIT

    jq -c --arg event "$event_name" '{event: $event, hook_input: .}' \
        | send_request_stdin > "$response_file"
    cat "$response_file"

    local decision
    decision="$(jq -r '.decision // ""' < "$response_file" 2>/dev/null || echo "")"
    if [ "$decision" = "block" ]; then
        local reason
        reason="$(jq -r '.reason // ""' < "$response_file" 2>/dev/null || echo "")"
        if [ -n "$reason" ]; then
            printf '%s\n' "$reason" >&2
        fi
        return 2
    fi
    return 0
}

# Export functions for use by forwarder scripts
export -f emit_hook_error
export -f validate_venv
export -f is_daemon_running
export -f start_daemon
export -f ensure_daemon
export -f send_request_stdin
export -f forward_stop_event
