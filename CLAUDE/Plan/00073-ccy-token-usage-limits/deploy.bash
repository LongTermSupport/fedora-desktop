#!/usr/bin/env bash
#
# Plan 00073 — deploy the ccy token-usage changes.
#
# HOST-ONLY. Never run Ansible inside a CCY container.
#
# WHY THIS SCRIPT EXISTS: the first attempt at this plan named
# play-claude-code.yml, which only *asserts* the ccy lib exists (a preflight
# `stat`) and deploys none of it. The play that actually installs
# /var/local/claude-yolo/lib/token-management.bash and the claude-yolo wrapper
# is play-claude-yolo.yml. The wrong play ran green — ok=18, changed=1 — while
# the host kept the old library, which is exactly the Plan 00072 failure mode
# (repo says fixed, machine runs the old build). Recording the correct play in
# a script means it cannot be misremembered again.

set -euo pipefail

usage() {
    cat <<'EOF'
Plan 00073 deploy — install the ccy token-usage changes on this host

USAGE:
    deploy.bash [-h|--help]

WHAT IT RUNS:
    ansible-playbook playbooks/imports/play-claude-yolo.yml

    That play owns BOTH artifacts this plan changed:
      /var/local/claude-yolo/lib/token-management.bash   (the usage code)
      /var/local/claude-yolo/claude-yolo                 (help text, CCY_VERSION)

    It also rebuilds the container image, which is a fast layer-cache hit when
    the Dockerfile is unchanged — as it is here.

AFTER DEPLOYING:
    ccy --list-tokens

    Real percentages  -> the stored token can read /api/oauth/usage (Q1 = yes).
    "usage: not authorised" on every row
                      -> the token is scoped to /v1/messages (Q1 = no); the
                         feature gets stripped back out, not left dim.
    No "Usage:" line at all
                      -> the new library is NOT running. Re-run this script, or
                         run triage.bash for the deployment-state report.
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

echo "Deploying Plan 00073 via play-claude-yolo.yml ..." >&2
echo "" >&2
ansible-playbook "$PLAY"

echo "" >&2
echo "Deployed. Now confirm the answer to Q1:" >&2
echo "    ccy --list-tokens" >&2
echo "" >&2
echo "If no 'Usage:' line appears at all, the new library is not running —" >&2
echo "run this plan's triage.bash for the deployment-state report." >&2
