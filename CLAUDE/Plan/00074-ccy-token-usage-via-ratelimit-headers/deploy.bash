#!/usr/bin/env bash
#
# Plan 00074 — deploy the on-demand token usage display (CCY 3.34.0).
#
# HOST-ONLY. Never run Ansible inside a CCY container.
#
# The play below owns BOTH artifacts this plan changed. Naming it here rather
# than in chat is deliberate: Plan 00073 was told to deploy play-claude-code.yml,
# which only *asserts* the ccy lib exists (a preflight `stat`) and installs none
# of it. That ran green — ok=18, changed=1 — while the host kept the old library.

set -euo pipefail

usage() {
    cat <<'EOF'
Plan 00074 deploy — install the on-demand token usage display on this host

USAGE:
    deploy.bash [-h|--help]

WHAT IT RUNS:
    ansible-playbook playbooks/imports/play-claude-yolo.yml

    That play owns both artifacts this plan changed:
      /var/local/claude-yolo/lib/token-management.bash   (usage machinery, 1.9.0)
      /var/local/claude-yolo/claude-yolo                 (CCY_VERSION 3.34.0)

    It also drops any stale usage cache, and rebuilds the container image — a
    fast layer-cache hit when the Dockerfile is unchanged, as it is here.

AFTER DEPLOYING — start ccy and look at the token menu:

    A "u) Show usage limits (costs 1 small API call per account)" line
        -> the new library is running. Press u.

    No "u)" line at all
        -> the OLD library is still deployed. Re-run this script.

    Press u. Each row should gain a usage column:

      1) <account>  (expires: ...)  —  5h 37% r2h · wk 63% r3d

    "usage: not authorised" / "unreachable" on a row
        -> that account could not be read; the others still render. Not fatal.

    EVERY row showing 0% or <1% while the accounts are genuinely busy
        -> the utilisation scale is 0-1, not 0-100 (Q2 in PLAN.md). The fix is
           one function: multiply by 100 in _usage_pct() in token-management.bash.
           Nothing else in the renderer depends on the scale.
EOF
}

if [ $# -gt 0 ]; then
    case "$1" in
        -h|--help) usage; exit 0 ;;
        *) echo "ERROR: unknown option: $1" >&2; echo "Try: deploy.bash --help" >&2; exit 1 ;;
    esac
fi

if [ -f /.dockerenv ] || [ -d /workspace/.claude ]; then
    echo "ERROR: this looks like a CCY container — Ansible must never run here." >&2
    echo "  Run this script on your HOST system instead." >&2
    exit 1
fi

REPO_ROOT="$(git rev-parse --show-toplevel)"
PLAY="$REPO_ROOT/playbooks/imports/play-claude-yolo.yml"

if [ ! -f "$PLAY" ]; then
    echo "ERROR: playbook not found: $PLAY" >&2
    exit 1
fi

echo "Deploying Plan 00074 via play-claude-yolo.yml ..." >&2
echo "" >&2
ansible-playbook "$PLAY"

echo "" >&2
echo "Deployed. Start ccy and look for the 'u) Show usage limits' option." >&2
echo "If it is absent, the old library is still running — re-run this script." >&2
echo "Run deploy.bash --help for how to read the result." >&2
