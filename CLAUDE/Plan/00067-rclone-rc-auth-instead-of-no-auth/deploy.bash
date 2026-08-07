#!/usr/bin/env bash
#
# Plan 00067 — deploy the rclone RC auth change. HOST ONLY.
#
# This REWRITES each rclone mount unit and RESTARTS the mount. A restart
# interrupts the VFS write-back queue, so it must NOT run while an
# `ftp-camera --copy` (or any other write through the mount) is in flight.
# The script refuses to run if it detects one.
#
# Usage: deploy.bash [--help]

set -euo pipefail

for arg in "$@"; do
    case "$arg" in
        -h | --help)
            cat << 'EOF'
Plan 00067 — deploy the rclone RC auth change (HOST ONLY)

Usage: deploy.bash [--help]

Runs play-rclone.yml, which rewrites the mount units (dropping --rc-no-auth,
adding EnvironmentFile) and restarts the mounts.

REFUSES to run while an ftp-camera copy is in flight, because restarting the
mount would interrupt the VFS write-back queue and can lose cached-but-not-yet-
uploaded data.

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

REPO_ROOT="$(git rev-parse --show-toplevel)"
PLAY="$REPO_ROOT/playbooks/imports/optional/common/play-rclone.yml"

# --- container guard: never run Ansible inside CCY ---------------------------
if [ "$REPO_ROOT" = "/workspace" ]; then
    echo "ERROR: this looks like a CCY container (/workspace)." >&2
    echo "  Ansible must run on the HOST, not in the container." >&2
    echo "  See CLAUDE/ContainerRules.md." >&2
    exit 1
fi

if [ ! -f "$PLAY" ]; then
    echo "ERROR: playbook not found: $PLAY" >&2
    exit 1
fi

# --- in-flight copy guard ----------------------------------------------------
# Restarting the mount mid-copy interrupts the write-back queue. Refuse rather
# than risk cached-but-unuploaded data.
if pgrep -af 'ftp-camera' > /dev/null; then
    echo "ERROR: an ftp-camera process is running." >&2
    echo "  Deploying now would restart the mount and interrupt the VFS" >&2
    echo "  write-back queue. Wait for the copy to finish, then re-run." >&2
    echo >&2
    echo "  Running processes:" >&2
    pgrep -af 'ftp-camera' >&2
    exit 1
fi

echo "=============================================================="
echo "Plan 00067 — deploying rclone RC authentication"
echo "=============================================================="
echo
echo "This will rewrite the rclone mount units and RESTART the mounts."
echo "Playbook: $PLAY"
echo

ansible-playbook "$PLAY"

echo
echo "=============================================================="
echo "Deploy finished. Now confirm it landed:"
echo "  CLAUDE/Plan/00067-rclone-rc-auth-instead-of-no-auth/acceptance.bash"
echo "=============================================================="
