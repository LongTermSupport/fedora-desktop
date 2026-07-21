#!/usr/bin/env bash
set -euo pipefail

# deploy.bash — run the Plan 00062 podman-store repair play on the HOST and
# capture its FULL output into untracked/reports/ (gitignored, bind-mounted into
# the CCY container, so the assisting agent reads the result directly).
#
# HOST-ONLY: never run Ansible inside the CCY container. This wrapper just runs
# the play — it does not touch podman itself. Re-runnable and idempotent (the
# play removes only what podman names as an incomplete layer; no prune/reset).
#
# Pass-through args go to ansible-playbook, e.g.:
#   CLAUDE/Plan/00062-disk-reclaim-tui/deploy.bash
#   CLAUDE/Plan/00062-disk-reclaim-tui/deploy.bash -e podman_repair_remove_image=claude-yolo:latest
#   CLAUDE/Plan/00062-disk-reclaim-tui/deploy.bash -e podman_repair_max_passes=10
#
# stderr = human status; stdout = the play's own output (via tee). No caller
# captures stdout, so mirroring the play output there is correct.

REPO_ROOT="$(git rev-parse --show-toplevel)"
PLAY="$REPO_ROOT/CLAUDE/Plan/00062-disk-reclaim-tui/podman-store-repair.yml"
REPORTS_DIR="$REPO_ROOT/untracked/reports"
LOG="$REPORTS_DIR/podman-store-repair-run.log"

if [ ! -f "$PLAY" ]; then
    echo "deploy.bash: repair play not found at $PLAY" >&2
    exit 1
fi

mkdir -p "$REPORTS_DIR"

echo "Running podman-store repair play on the HOST." >&2
echo "  play: $PLAY" >&2
echo "  full output → $LOG" >&2
echo >&2

# Run the play and mirror its output to the log. pipefail makes the pipeline
# carry the play's non-zero exit (tee otherwise masks it); evaluating it as an
# `if` condition captures that rc WITHOUT tripping errexit, so we can report and
# re-propagate it. A failed play therefore still fails the wrapper — no hiding.
if ansible-playbook "$PLAY" "$@" 2>&1 | tee "$LOG"; then
    rc=0
else
    rc=$?
fi

echo >&2
echo "Repair play exit code: $rc" >&2
echo "Full log: $LOG" >&2
echo "  (repo-relative: untracked/reports/podman-store-repair-run.log — the agent reads it directly)" >&2

exit "$rc"
