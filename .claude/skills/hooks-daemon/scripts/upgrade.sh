#!/bin/bash
#
# DAEMON-OWNED FILE - do not edit. Deployed into your project by the
# claude-code-hooks-daemon installer and refreshed on every upgrade, so local
# changes are discarded. See the daemon clone's CLAUDE/LLM-INSTALL.md,
# "Which Files Under .claude/ Are Yours?", for the full list and the
# linter exclusions.
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
#   upgrade.sh [FLAGS] [VERSION]   — passes both through to the canonical script.
#

set -euo pipefail

if [ "${1:-}" = "--help" ] || [ "${1:-}" = "-h" ]; then
    printf '%s\n' "Usage: upgrade.sh [--skip-reading-confirmation=DIGEST] [--skip-config-optimisation] [VERSION]" \
        "  VERSION  Target git tag (default: latest). DIGEST is the value the pre-deploy gate's stop printed." \
        "  See HOOKS_DAEMON_UPGRADE_REF and HOOKS_DAEMON_UPGRADE_BASE_URL for fetch overrides."
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
    CLONE="$PROJECT_ROOT/.claude/hooks-daemon"
    # Plan 00114 F4: make the offline/network failure actionable instead of a
    # dead end. Plan 00376: the recovery runs the TARGET's own Layer 1 out of
    # the installed clone, because the installed Layer 1 may predate the
    # pre-deploy gate. A single printf keeps the shim under its line budget.
    printf 'Error: failed to fetch upgrade.sh from %s\nRecovery: run the target release'"'"'s own Layer 1 from the installed clone (VERSION = the tag to install):\n  git -C "%s" fetch --tags && git -C "%s" show VERSION:scripts/upgrade.sh > "%s/untracked/upgrade-target.sh" && bash "%s/untracked/upgrade-target.sh" --project-root "%s" VERSION\nor pin a reachable ref no older than the target: HOOKS_DAEMON_UPGRADE_REF=VERSION bash "%s"\n' "$URL" "$CLONE" "$CLONE" "$CLONE" "$CLONE" "$PROJECT_ROOT" "$0" >&2
    exit 1
fi
chmod +x "$TMP"
exec bash "$TMP" --project-root "$PROJECT_ROOT" "$@"
