#!/bin/bash
#
# daemon-cli.sh - General wrapper for daemon CLI commands
#
# Usage:
#   ./daemon-cli.sh <command> [args...]
#
# Examples:
#   ./daemon-cli.sh logs
#   ./daemon-cli.sh status
#   ./daemon-cli.sh restart
#   ./daemon-cli.sh handlers
#

set -euo pipefail

# === SELF-BOOTSTRAP BEGIN (Plan 00104 Task 5.1 + Plan 00105 Phase 4) ===
# When this script is older than the latest release, replace ourselves
# with the freshly-downloaded version before doing any work. The
# 2026-05-01 field report (Issue #1) showed that a stale skill script
# ships the user a broken flow that no in-repo fix can save once the
# user already has the bad copy installed. Bootstrap from the GitHub
# release artifact, sha256-verify against the manifest, and re-exec with
# --already-bootstrapped to break recursion. Aborts loudly on any
# network or integrity failure — never silently falls back to the local
# stale copy (a silent fallback would mean a tampered or corrupted
# release reaches production).
#
# Plan 00105 Phase 4: parameterised by `$(basename "$0")` so the same
# stanza serves upgrade.sh, daemon-cli.sh, health-check.sh, and
# init-handlers.sh. A per-(basename, own-sha) marker under
# ${TMPDIR:-/tmp}/hooks-daemon-bootstrap caches verified-current bodies
# so subsequent invocations skip the network round-trip until either
# the script body changes (post-upgrade) or HOOKS_DAEMON_BOOTSTRAP_FORCE=1.
#
# Override base URL for testing via HOOKS_DAEMON_BOOTSTRAP_BASE_URL.
# Disable bootstrapping entirely via HOOKS_DAEMON_SKIP_BOOTSTRAP=1.
_HOOKS_DAEMON_BOOTSTRAP_BASE_URL_DEFAULT="https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon/releases/latest/download"
_HOOKS_DAEMON_BOOTSTRAP_BASE_URL="${HOOKS_DAEMON_BOOTSTRAP_BASE_URL:-$_HOOKS_DAEMON_BOOTSTRAP_BASE_URL_DEFAULT}"
_HOOKS_DAEMON_BOOTSTRAP_SCRIPT_NAME="$(basename "$0")"

if [ "${1:-}" = "--already-bootstrapped" ] || [ "${HOOKS_DAEMON_SKIP_BOOTSTRAP:-0}" = "1" ]; then
    if [ "${1:-}" = "--already-bootstrapped" ]; then
        shift
    fi
else
    _self_sha256() {
        if command -v sha256sum > /dev/null; then
            sha256sum "$1" | awk '{print $1}'
        elif command -v shasum > /dev/null; then
            shasum -a 256 "$1" | awk '{print $1}'
        else
            echo "Error: neither sha256sum nor shasum is available — cannot verify bootstrap integrity" >&2
            exit 1
        fi
    }

    _bootstrap_own_sha="$(_self_sha256 "$0")"
    _bootstrap_marker_dir="${TMPDIR:-/tmp}/hooks-daemon-bootstrap"
    _bootstrap_marker="$_bootstrap_marker_dir/$_HOOKS_DAEMON_BOOTSTRAP_SCRIPT_NAME-$_bootstrap_own_sha.ok"

    if [ "${HOOKS_DAEMON_BOOTSTRAP_FORCE:-0}" != "1" ] && [ -f "$_bootstrap_marker" ]; then
        : # cached: this exact body was already verified against the release manifest
    else
        _bootstrap_tmp_checksums="$(mktemp)"
        trap 'rm -f "$_bootstrap_tmp_checksums"' EXIT
        if ! curl -fsSL --max-time 30 -o "$_bootstrap_tmp_checksums" \
                "$_HOOKS_DAEMON_BOOTSTRAP_BASE_URL/bootstrap-checksums.txt"; then
            echo "Error: failed to download bootstrap-checksums.txt from" >&2
            echo "    $_HOOKS_DAEMON_BOOTSTRAP_BASE_URL/bootstrap-checksums.txt" >&2
            echo "Self-bootstrap aborted. Check network connectivity and retry." >&2
            exit 1
        fi

        _expected_sha="$(awk -v name="$_HOOKS_DAEMON_BOOTSTRAP_SCRIPT_NAME" '$2 == name {print $1; exit}' "$_bootstrap_tmp_checksums")"
        if [ -z "$_expected_sha" ]; then
            echo "Error: bootstrap-checksums.txt has no entry for $_HOOKS_DAEMON_BOOTSTRAP_SCRIPT_NAME" >&2
            echo "Self-bootstrap aborted. The release manifest is incomplete." >&2
            exit 1
        fi

        if [ "$_bootstrap_own_sha" != "$_expected_sha" ]; then
            _bootstrap_tmp_fresh="$(mktemp)"
            trap 'rm -f "$_bootstrap_tmp_checksums" "$_bootstrap_tmp_fresh"' EXIT
            if ! curl -fsSL --max-time 30 -o "$_bootstrap_tmp_fresh" \
                    "$_HOOKS_DAEMON_BOOTSTRAP_BASE_URL/$_HOOKS_DAEMON_BOOTSTRAP_SCRIPT_NAME"; then
                echo "Error: failed to download fresh $_HOOKS_DAEMON_BOOTSTRAP_SCRIPT_NAME from" >&2
                echo "    $_HOOKS_DAEMON_BOOTSTRAP_BASE_URL/$_HOOKS_DAEMON_BOOTSTRAP_SCRIPT_NAME" >&2
                echo "Self-bootstrap aborted. Check network connectivity and retry." >&2
                exit 1
            fi
            _fresh_sha="$(_self_sha256 "$_bootstrap_tmp_fresh")"
            if [ "$_fresh_sha" != "$_expected_sha" ]; then
                echo "Error: checksum mismatch for downloaded $_HOOKS_DAEMON_BOOTSTRAP_SCRIPT_NAME" >&2
                echo "    Expected: $_expected_sha" >&2
                echo "    Got:      $_fresh_sha" >&2
                echo "Self-bootstrap aborted. The download was tampered with or the" >&2
                echo "release manifest is inconsistent — do not run this script." >&2
                exit 1
            fi
            chmod +x "$_bootstrap_tmp_fresh"
            exec bash "$_bootstrap_tmp_fresh" --already-bootstrapped "$@"
        fi

        # Verified-current: cache so subsequent invocations of this exact
        # body skip the network round-trip. Cache write is best-effort —
        # if /tmp is read-only we surface the failure once via stderr but
        # do not abort, because the script body has already been verified.
        if ! mkdir -p "$_bootstrap_marker_dir" 2> /dev/null; then
            echo "Warning: could not create bootstrap cache dir $_bootstrap_marker_dir — will re-verify on next invocation" >&2
        elif ! : > "$_bootstrap_marker" 2> /dev/null; then
            echo "Warning: could not write bootstrap cache marker $_bootstrap_marker — will re-verify on next invocation" >&2
        fi

        trap - EXIT
        rm -f "$_bootstrap_tmp_checksums"
    fi
fi
# === SELF-BOOTSTRAP END ===

# Detect project root
PROJECT_ROOT="$(pwd)"
while [ "$PROJECT_ROOT" != "/" ]; do
    if [ -f "$PROJECT_ROOT/.claude/hooks-daemon.yaml" ]; then
        break
    fi
    PROJECT_ROOT="$(dirname "$PROJECT_ROOT")"
done

if [ ! -f "$PROJECT_ROOT/.claude/hooks-daemon.yaml" ]; then
    echo "❌ Not in a hooks daemon project (no .claude/hooks-daemon.yaml found)"
    exit 1
fi

DAEMON_DIR="$PROJECT_ROOT/.claude/hooks-daemon"
# shellcheck source=_resolve-venv.sh
source "$(dirname "$0")/_resolve-venv.sh"

# Check if Python exists
if [ ! -f "$PYTHON" ]; then
    echo "❌ Python venv not found: $PYTHON"
    echo ""
    echo "Daemon installation may be corrupted. Try reinstalling."
    exit 1
fi

# Forward all arguments to daemon CLI
"$PYTHON" -m claude_code_hooks_daemon.daemon.cli "$@"
