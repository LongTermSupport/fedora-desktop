#!/bin/bash
#
# upgrade.sh — thin shim for /hooks-daemon upgrade (Plan 00109 Phase 2).
#
# This script carries ZERO upgrade logic. It detects the client's project
# root, fetches the canonical ``scripts/upgrade.sh`` from the daemon repo
# on GitHub, and execs it. Every fix to the upgrade flow lives in the
# in-repo script — clients pick it up on the next invocation without
# needing to re-install the skill.
#
# Environment overrides:
#   HOOKS_DAEMON_UPGRADE_REF        git ref to fetch (default: main)
#   HOOKS_DAEMON_UPGRADE_BASE_URL   base URL override (default: GitHub raw)
#
# Usage:
#   upgrade.sh [VERSION]   — passes VERSION through to the canonical script.
#

set -euo pipefail

if [ "${1:-}" = "--help" ] || [ "${1:-}" = "-h" ]; then
    printf '%s\n' "Usage: upgrade.sh [VERSION]" \
        "  VERSION  Target git tag (default: latest). See HOOKS_DAEMON_UPGRADE_REF" \
        "           and HOOKS_DAEMON_UPGRADE_BASE_URL for fetch overrides."
    exit 0
fi

PROJECT_ROOT="$(pwd)"
while [ "$PROJECT_ROOT" != "/" ]; do
    if [ -f "$PROJECT_ROOT/.claude/hooks-daemon.yaml" ]; then break; fi
    PROJECT_ROOT="$(dirname "$PROJECT_ROOT")"
done
if [ ! -f "$PROJECT_ROOT/.claude/hooks-daemon.yaml" ]; then
    echo "Error: not in a hooks daemon project (no .claude/hooks-daemon.yaml found)" >&2
    exit 1
fi

REF="${HOOKS_DAEMON_UPGRADE_REF:-main}"
BASE_URL="${HOOKS_DAEMON_UPGRADE_BASE_URL:-https://raw.githubusercontent.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon}"
URL="$BASE_URL/$REF/scripts/upgrade.sh"

TMP="$(mktemp)"
if ! curl -fsSL --max-time 30 -o "$TMP" "$URL"; then
    rm -f "$TMP"
    # Plan 00114 F4: make the offline/network failure actionable instead of a
    # dead end. The installed daemon already ships a working Layer 1 upgrade.sh.
    # A single printf keeps the shim under its thin-shim line budget.
    printf 'Error: failed to fetch upgrade.sh from %s\nRecovery: run the installed daemon Layer 1 directly:\n  bash "%s/.claude/hooks-daemon/scripts/upgrade.sh" --project-root "%s"\nor pin a reachable ref: HOOKS_DAEMON_UPGRADE_REF=v3.16.0 bash "%s"\n' "$URL" "$PROJECT_ROOT" "$PROJECT_ROOT" "$0" >&2
    exit 1
fi
chmod +x "$TMP"
exec bash "$TMP" --project-root "$PROJECT_ROOT" "$@"
