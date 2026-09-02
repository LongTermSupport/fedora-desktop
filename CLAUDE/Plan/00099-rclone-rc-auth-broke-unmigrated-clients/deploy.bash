#!/usr/bin/env bash
#
# Plan 00099 — deploy the RC credential helper and every migrated client.
# HOST ONLY.
#
# Runs BOTH plays, and that pairing is the point. Plan 00094 changed
# files/home/.local/bin/ftp-camera but deployed only play-rclone.yml, so the
# repo fix never reached the host and the camera copy stayed broken for weeks.
# ftp-camera is deployed by play-ftp-camera.yml; the rclone helpers and the
# shared credential library by play-rclone.yml. Both, or the fix is partial.
#
# play-rclone.yml REWRITES the mount units and RESTARTS the mounts, which
# interrupts the VFS write-back queue. This script refuses to run while an
# ftp-camera process is in flight.
#
# Usage: deploy.bash [--help]

set -euo pipefail

for arg in "$@"; do
    case "$arg" in
        -h | --help)
            cat << 'EOF'
Plan 00099 — deploy the rclone RC client migration (HOST ONLY)

Usage: deploy.bash [--help]

Runs, in order:
  1. play-rclone.yml       — deploys rclone-rc-auth.bash and the migrated
                             rclone-cache-status / rclone-cache-warm /
                             rclone-tail; rewrites and restarts the mounts
  2. play-ftp-camera.yml   — deploys the migrated ftp-camera

REFUSES to run while an ftp-camera process is in flight, because restarting
the mount would interrupt the VFS write-back queue and can lose cached-but-
not-yet-uploaded data.

Run acceptance.bash afterwards to confirm the change landed.
EOF
            exit 0
            ;;
        *)
            echo "ERROR: unknown argument: $arg" >&2
            echo "  Try: deploy.bash --help" >&2
            exit 1
            ;;
    esac
done

PLAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "$PLAN_DIR" rev-parse --show-toplevel)"

# --- container guard: never run Ansible inside CCY ---------------------------
if [ "$REPO_ROOT" = "/workspace" ]; then
    echo "ERROR: this looks like a CCY container (/workspace)." >&2
    echo "  Ansible must run on the HOST, not in the container." >&2
    echo "  See CLAUDE/ContainerRules.md." >&2
    exit 1
fi

PLAY_RCLONE="$REPO_ROOT/playbooks/imports/optional/common/play-rclone.yml"
PLAY_CAMERA="$REPO_ROOT/playbooks/imports/optional/common/play-ftp-camera.yml"

for play in "$PLAY_RCLONE" "$PLAY_CAMERA"; do
    if [ ! -f "$play" ]; then
        echo "ERROR: playbook not found: $play" >&2
        exit 1
    fi
done

# --- in-flight copy guard ----------------------------------------------------
# Restarting the mount mid-copy interrupts the write-back queue. Refuse rather
# than risk cached-but-unuploaded data.
#
# Anchored to a path component or start-of-line, NOT a bare substring: a plain
# `pgrep -f ftp-camera` also matches any shell whose command line merely
# MENTIONS ftp-camera — including the one that invoked this script, and any
# agent or terminal discussing the problem. That false positive makes the guard
# refuse a deploy with nothing actually running. Self and parent are excluded
# for the same reason.
camera_pids=""
raw_pids=""
if raw_pids=$(pgrep -f '(^|/)ftp-camera([[:space:]]|$)'); then
    # grep exits 1 when EVERY hit was filtered out — which is the normal case
    # here (only this script and its parent matched). That is not an error, but
    # under `set -euo pipefail` an unguarded assignment from it aborts the whole
    # deploy silently, with no message and no plays run. Check the status.
    filtered=""
    if filtered=$(printf '%s\n' "$raw_pids" | grep -v -x -e "$$" -e "$PPID"); then
        camera_pids="$filtered"
    fi
fi
if [ -n "$camera_pids" ]; then
    echo "ERROR: an ftp-camera process is running." >&2
    echo "  Deploying now would restart the mount and interrupt the VFS" >&2
    echo "  write-back queue. Wait for the copy to finish, then re-run." >&2
    echo >&2
    echo "  Running processes:" >&2
    printf '%s\n' "$camera_pids" | while IFS= read -r pid; do
        ps -o pid=,args= -p "$pid" >&2
    done
    exit 1
fi

echo "=============================================================="
echo "Plan 00099 — deploying the rclone RC client migration"
echo "=============================================================="
echo
echo "This will rewrite the rclone mount units and RESTART the mounts."
echo

echo "--- 1/2: $PLAY_RCLONE"
ansible-playbook "$PLAY_RCLONE"

echo
echo "--- 2/2: $PLAY_CAMERA"
ansible-playbook "$PLAY_CAMERA"

echo
echo "=============================================================="
echo "Deploy finished. Now confirm it landed:"
echo "  $PLAN_DIR/acceptance.bash"
echo "=============================================================="
