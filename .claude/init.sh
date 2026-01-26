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
# Args:
#   $1 - Event name (e.g., "PreToolUse")
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

    # Output valid JSON to stdout (agent WILL see this)
    # Uses jq if available, falls back to cat for JSON construction
    if command -v jq &>/dev/null; then
        jq -n \
            --arg event "$event_name" \
            --arg error_type "$error_type" \
            --arg details "$error_details" \
            '{
                hookSpecificOutput: {
                    hookEventName: $event,
                    additionalContext: (
                        "⚠️ HOOKS DAEMON ERROR - PROTECTION NOT ACTIVE ⚠️\n\n" +
                        "ERROR TYPE: " + $error_type + "\n" +
                        "ERROR DETAILS: " + $details + "\n\n" +
                        "🛑 CRITICAL: You MUST stop work immediately.\n\n" +
                        "The hooks daemon is not functioning. This means:\n" +
                        "- Destructive git operations are NOT being blocked\n" +
                        "- Code quality checks are NOT running\n" +
                        "- Safety guardrails are NOT active\n\n" +
                        "RECOMMENDED ACTIONS:\n" +
                        "1. STOP all current tasks immediately\n" +
                        "2. Inform the user that hooks protection is down\n" +
                        "3. Run: python -m claude_code_hooks_daemon.daemon.cli status\n" +
                        "4. Run: python -m claude_code_hooks_daemon.daemon.cli logs\n" +
                        "5. Check daemon installation in .claude/hooks-daemon/\n" +
                        "6. Restart daemon: python -m claude_code_hooks_daemon.daemon.cli restart\n\n" +
                        "DO NOT continue work until hooks are verified working."
                    )
                }
            }'
    else
        # Fallback if jq not available - simple JSON output
        cat <<EOF
{"hookSpecificOutput":{"hookEventName":"$event_name","additionalContext":"⚠️ HOOKS DAEMON ERROR - PROTECTION NOT ACTIVE ⚠️\\n\\nERROR TYPE: $error_type\\nERROR DETAILS: $error_details\\n\\n🛑 CRITICAL: You MUST stop work immediately.\\n\\nThe hooks daemon is not functioning. Safety guardrails are NOT active.\\n\\nRun: python -m claude_code_hooks_daemon.daemon.cli status\\nRun: python -m claude_code_hooks_daemon.daemon.cli logs\\n\\nDO NOT continue work until hooks are verified working."}}
EOF
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

# Generate socket and PID paths using Python paths module
PYTHON_CMD="$HOOKS_DAEMON_ROOT_DIR/untracked/venv/bin/python"
DAEMON_MODULE="claude_code_hooks_daemon.daemon.paths"

# Import paths module and generate paths
SOCKET_PATH=$($PYTHON_CMD -c "
from $DAEMON_MODULE import get_socket_path
print(get_socket_path('$PROJECT_PATH'))
")

PID_PATH=$($PYTHON_CMD -c "
from $DAEMON_MODULE import get_pid_path
print(get_pid_path('$PROJECT_PATH'))
")

# Daemon startup timeout (deciseconds - 1/10th second units)
DAEMON_STARTUP_TIMEOUT=50

# Daemon startup check interval (deciseconds)
DAEMON_STARTUP_CHECK_INTERVAL=1

# Export paths for use by forwarder scripts
export HOOKS_DAEMON_ROOT_DIR
export SOCKET_PATH
export PID_PATH
export PROJECT_PATH
export PYTHON_CMD

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

    # Remove stale socket file
    rm -f "$SOCKET_PATH"

    # Start daemon using CLI (proper daemonization)
    $PYTHON_CMD -m claude_code_hooks_daemon.daemon.cli start \
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
    # Use Python for reliable JSON transport - no shell variable interpolation
    # This reads from stdin and sends to socket, avoiding all shell escaping issues
    # CRITICAL: On error, outputs valid JSON to stdout (not stderr) so agent sees it
    $PYTHON_CMD -c "
import json
import socket
import sys

def emit_error_json(event_name: str, error_type: str, error_details: str) -> None:
    '''Output valid hook error response to stdout.'''
    # Log to stderr for debugging (agent won't see this)
    print(f'HOOKS DAEMON ERROR [{error_type}]: {error_details}', file=sys.stderr)

    # Output valid JSON to stdout (agent WILL see this)
    error_response = {
        'hookSpecificOutput': {
            'hookEventName': event_name,
            'additionalContext': (
                '⚠️ HOOKS DAEMON ERROR - PROTECTION NOT ACTIVE ⚠️\\n\\n'
                f'ERROR TYPE: {error_type}\\n'
                f'ERROR DETAILS: {error_details}\\n\\n'
                '🛑 CRITICAL: You MUST stop work immediately.\\n\\n'
                'The hooks daemon is not functioning. This means:\\n'
                '- Destructive git operations are NOT being blocked\\n'
                '- Code quality checks are NOT running\\n'
                '- Safety guardrails are NOT active\\n\\n'
                'RECOMMENDED ACTIONS:\\n'
                '1. STOP all current tasks immediately\\n'
                '2. Inform the user that hooks protection is down\\n'
                '3. Run: python -m claude_code_hooks_daemon.daemon.cli status\\n'
                '4. Run: python -m claude_code_hooks_daemon.daemon.cli logs\\n'
                '5. Check daemon installation in .claude/hooks-daemon/\\n'
                '6. Restart daemon: python -m claude_code_hooks_daemon.daemon.cli restart\\n\\n'
                'DO NOT continue work until hooks are verified working.'
            )
        }
    }
    print(json.dumps(error_response))

# Read JSON from stdin (preserves all control characters)
request = sys.stdin.read()

# Try to extract event name from request for better error messages
event_name = 'Unknown'
try:
    req_data = json.loads(request)
    event_name = req_data.get('event', 'Unknown')
except:
    pass

# Add newline if not present (daemon expects newline-terminated JSON)
if not request.endswith('\\n'):
    request += '\\n'

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
    output = response.decode('utf-8').rstrip('\\n')
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
export -f is_daemon_running
export -f start_daemon
export -f ensure_daemon
export -f send_request_stdin
