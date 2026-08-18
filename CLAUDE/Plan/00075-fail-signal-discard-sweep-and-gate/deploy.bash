#!/usr/bin/env bash
#
# Plan 00075 — deploy the discarded-failure-signal fixes (CCY 3.37.0 + others).
#
# HOST-ONLY. Never run Ansible inside a CCY container.
#
# This plan touched deployed files across FOUR plays, which is exactly why the
# mapping belongs in a script rather than in a chat message. `qa-all.bash` on the
# host runs qa-deployed-drift.bash, which compares files/home/.local/bin/ against
# ~/.local/bin/ — so until the rclone and basic-configs plays below are run, QA
# will fail on this machine BY DESIGN. That is the documented
# edit -> playbook -> deploy -> test order being enforced, not a fault.

set -euo pipefail

usage() {
    cat <<'EOF'
Plan 00075 deploy — install the discarded-failure-signal fixes on this host

USAGE:
    deploy.bash [-h|--help]
    deploy.bash --list          Show the plays and what each one owns, then exit

WHAT IT RUNS (in this order):

  1. playbooks/imports/play-claude-yolo.yml
       /var/local/claude-yolo/claude-yolo                (CCY_VERSION 3.37.0)
       /var/local/claude-yolo/lib/docker-health.bash     (1.2.0)
       /var/local/claude-yolo/lib/network-management.bash(1.8.0)
       /var/local/claude-yolo/lib/common.bash
       /var/local/claude-yolo/lib/ssh-handling.bash
       /var/local/claude-yolo/entrypoint.sh
     Fixes: `ccy --top` no longer dies mid-table and the zombie scan no longer
     truncates when a container exits while being listed; the --connect failure
     diagnostic survives a container that has gone away.

  2. playbooks/imports/optional/common/play-rclone.yml
       ~/.local/bin/rclone-tail

  3. playbooks/imports/play-basic-configs.yml
       ~/.local/bin/ssh-keys-rekey

  4. playbooks/imports/optional/experimental/play-docker-in-lxc-support.yml
       /var/local/docker-in-lxc
     Fixes: the container name is no longer built from a silently-empty
     `git remote` capture, and a failed token selection is no longer reported as
     "no token selected".

  Steps 2 and 3 are what qa-deployed-drift.bash checks. Skipping them leaves
  `./scripts/qa-all.bash` failing on this host.

NOT DEPLOYED BY THIS SCRIPT (nothing to deploy — they are repo-side only):
    scripts/nvidia-status.bash, scripts/check-displaylink-status.sh, scripts/lint
    fedora-install/setup-netinstall-boot.bash
    .semgrep/ rules and fixture, scripts/qa-bash.bash
EOF
}

PLAYS=(
    "playbooks/imports/play-claude-yolo.yml"
    "playbooks/imports/optional/common/play-rclone.yml"
    "playbooks/imports/play-basic-configs.yml"
    "playbooks/imports/optional/experimental/play-docker-in-lxc-support.yml"
)

LIST_ONLY=0
if [ $# -gt 0 ]; then
    case "$1" in
        -h|--help) usage; exit 0 ;;
        --list)    LIST_ONLY=1 ;;
        *) echo "ERROR: unknown option: $1" >&2; echo "Try: deploy.bash --help" >&2; exit 1 ;;
    esac
fi

if [ -f /.dockerenv ] || [ -d /workspace/.claude ]; then
    echo "ERROR: this looks like a CCY container — Ansible must not run here." >&2
    echo "  Run this script on your HOST system instead." >&2
    exit 1
fi

REPO_ROOT="$(git rev-parse --show-toplevel)"

if [ "$LIST_ONLY" -eq 1 ]; then
    usage
    exit 0
fi

# Capture the run into this plan's own logs/ directory. Without it the only
# record of a deploy is the operator's terminal scrollback, which means a
# failure has to be copy-pasted back by hand — exactly what PlanTriage.md exists
# to avoid. Resolved from the script's own location (so it survives the move
# into Completed/) and before the cd below. CLAUDE/Plan/**/logs/ is gitignored:
# the log sits inside the repo, so it is readable at the same path from a CCY
# container, but never reaches this public repo — Ansible output names hosts,
# units and home directories.
PLAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
mkdir -p "$PLAN_DIR/logs"
LOG="$PLAN_DIR/logs/deploy.log"
exec > >(tee "$LOG") 2>&1
echo "Logging this run to: $LOG" >&2

cd "$REPO_ROOT"

# Every play is checked to exist BEFORE any of them runs. A missing play part-way
# through leaves the host half-deployed, which is the state this plan's own
# subject matter is about avoiding.
missing=0
for play in "${PLAYS[@]}"; do
    if [ ! -f "$play" ]; then
        echo "ERROR: play not found: $play" >&2
        missing=1
    fi
done
if [ "$missing" -ne 0 ]; then
    echo "Refusing to deploy a partial set." >&2
    exit 1
fi

for play in "${PLAYS[@]}"; do
    echo ""
    echo "════════════════════════════════════════════════════════════════════════"
    echo "  ansible-playbook $play"
    echo "════════════════════════════════════════════════════════════════════════"
    ansible-playbook "$play"
done

echo ""
echo "All four plays completed."
echo ""
echo "Now confirm the host and repo agree — this is the check that would have"
echo "caught Plan 00067's undeployed fix:"
echo ""
echo "    ./scripts/qa-all.bash"
echo ""
echo "Then spot-check the ccy fix that motivated CCY 3.37.0:"
echo ""
echo "    ccy --top      # should render its table and stay up"
echo ""
