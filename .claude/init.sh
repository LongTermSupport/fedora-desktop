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

    # Build error context message
    # NOTE: Language is intentionally measured to avoid triggering investigation loops
    # in LLM agents. Previous "STOP work immediately" wording caused agents to abandon
    # tasks and enter analysis cycles instead of simply restarting the daemon.
    local context_msg
    context_msg=$(printf '%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s' \
        "HOOKS DAEMON: Not currently running" \
        "" \
        "Error: $error_type - $error_details" \
        "" \
        "Hook safety handlers are inactive until the daemon is restarted." \
        "If you are in the middle of an upgrade, this is expected and temporary." \
        "" \
        "TO FIX (usually takes a few seconds):" \
        "1. Run: python -m claude_code_hooks_daemon.daemon.cli restart" \
        "2. Verify: python -m claude_code_hooks_daemon.daemon.cli status" \
        "" \
        "If restart fails, check logs:" \
        "  python -m claude_code_hooks_daemon.daemon.cli logs" \
        "Then inform the user if the issue persists.")

    # Event-specific JSON formatting using jq (already a dependency)
    # Stop/SubagentStop: top-level decision only (deny to show error)
    # Other events: hookSpecificOutput with context (fail-open allow)
    if command -v jq &>/dev/null; then
        if [[ "$event_name" == "Stop" || "$event_name" == "SubagentStop" ]]; then
            jq -n --arg reason "Hooks daemon not running - protection not active" \
                '{"decision": "block", "reason": $reason}'
        else
            jq -n --arg event "$event_name" --arg context "$context_msg" \
                '{"hookSpecificOutput": {"hookEventName": $event, "additionalContext": $context}}'
        fi
    else
        # Fallback if jq not available (should not happen - jq is required)
        cat <<FALLBACK_EOF
{"hookSpecificOutput":{"hookEventName":"$event_name","additionalContext":"HOOKS DAEMON ERROR: $error_type - $error_details"}}
FALLBACK_EOF
    fi
}

# Detect project path (should be called from .claude/hooks/ directory)
# Walk up directory tree to find .claude directory
HOOK_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_PATH="${HOOK_SCRIPT_DIR}"

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

# Venv Python (only needed for daemon startup, NOT for hot path)
PYTHON_CMD="$HOOKS_DAEMON_ROOT_DIR/untracked/venv/bin/python"

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
        local timestamp=$(date +%s.%N)
        local hash=$(echo -n "$timestamp" | md5sum | cut -c1-8)
        echo "-${hash}"
        return 0
    fi

    # Sanitize hostname for filesystem safety: lowercase, no spaces
    local sanitized=$(echo "$hostname" | tr '[:upper:]' '[:lower:]' | tr ' ' '-')
    echo "-${sanitized}"
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

# Daemon startup timeout (deciseconds - 1/10th second units)
DAEMON_STARTUP_TIMEOUT=50

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

    # Check venv Python binary exists
    if [[ ! -f "$PYTHON_CMD" ]]; then
        VENV_ERROR="Venv Python not found at $PYTHON_CMD. Run: cd $HOOKS_DAEMON_ROOT_DIR && uv sync"
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

    # Wait for socket to be ready (using deciseconds for integer arithmetic)
    local elapsed=0
    while [[ $elapsed -lt $DAEMON_STARTUP_TIMEOUT ]]; do
        if [[ -S "$SOCKET_PATH" ]]; then
            # Socket exists, daemon is ready
            return 0
        fi

        # Sleep 0.1 seconds (1 decisecond)
        sleep 0.1
        elapsed=$((elapsed + DAEMON_STARTUP_CHECK_INTERVAL))
    done

    # Timeout - daemon failed to create socket
    echo "ERROR: Daemon startup timeout (socket not created)" >&2
    rm -f "$PID_PATH"
    return 1
}

#
# ensure_daemon() - Start daemon if not running (lazy startup)
#
# Idempotent function safe to call on every hook invocation.
# Only starts daemon if not already running.
#
# Returns:
#   0 if daemon is running (started or already running)
#   1 if daemon failed to start
#
ensure_daemon() {
    if is_daemon_running; then
        return 0
    fi

    start_daemon
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
        '1. Run: python -m claude_code_hooks_daemon.daemon.cli restart',
        '2. Verify: python -m claude_code_hooks_daemon.daemon.cli status',
        '',
        'If restart fails, check logs:',
        '  python -m claude_code_hooks_daemon.daemon.cli logs',
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

# Export functions for use by forwarder scripts
export -f emit_hook_error
export -f validate_venv
export -f is_daemon_running
export -f start_daemon
export -f ensure_daemon
export -f send_request_stdin
