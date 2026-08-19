#!/usr/bin/env bash
#
# Plan 00076 — deploy the two rewritten rclone helpers. HOST ONLY.
#
# `rclone-tail` and `rclone-cache-status` were rewritten so semgrep can parse
# them (a heredoc fed straight to an `if` condition defeats tree-sitter-bash).
# The rewrite changes how both emit their result, so this is a real behaviour
# change in two production scripts — not a cosmetic one.
#
# play-rclone.yml REWRITES the mount units and RESTARTS the mounts, which
# interrupts the VFS write-back queue. This script refuses to run while an
# ftp-camera process is in flight — same hazard, same guard, as Plan 00072's
# deploy.
#
# Usage: deploy.bash [--help]

set -euo pipefail

for arg in "$@"; do
    case "$arg" in
        -h | --help)
            cat << 'EOF'
Plan 00076 — deploy the rewritten rclone helpers (HOST ONLY)

Usage: deploy.bash [--help]

Runs play-rclone.yml, which deploys:
  files/home/.local/bin/rclone-tail
  files/home/.local/bin/rclone-cache-status

Both had `query_stats` / `query_cache` rewritten to feed their Python heredoc
to a command SUBSTITUTION rather than straight to the `if` condition, and both
had their error-hiding numfmt fallbacks converted to the explicit form.

REFUSES to run while an ftp-camera process is in flight: the play restarts the
mounts, which can lose cached-but-not-yet-uploaded data.

Until this runs, `./scripts/qa-all.bash` FAILS on the host by design — the
deployed-drift gate compares the repo against ~/.local/bin/.

Run acceptance.bash afterwards to confirm both scripts still work.
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

mkdir -p "$PLAN_DIR/logs"
LOG="$PLAN_DIR/logs/deploy.log"
exec > >(tee "$LOG") 2>&1
echo "Logging this run to: $LOG" >&2

PLAY="$REPO_ROOT/playbooks/imports/optional/common/play-rclone.yml"
if [ ! -f "$PLAY" ]; then
    echo "ERROR: playbook not found: $PLAY" >&2
    exit 1
fi

# --- in-flight copy guard ----------------------------------------------------
# Anchored to a path component or start-of-line, NOT a bare substring: a plain
# `pgrep -f ftp-camera` also matches any shell whose command line merely
# MENTIONS ftp-camera — including the one that invoked this script. Self and
# parent are excluded for the same reason.
camera_pids=""
raw_pids=""
if raw_pids=$(pgrep -f '(^|/)ftp-camera([[:space:]]|$)'); then
    # grep exits 1 when EVERY hit was filtered out — the normal case here. That
    # is not an error, but under `set -euo pipefail` an unguarded assignment
    # from it aborts the deploy silently, with no message and no play run.
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
echo "Plan 00076 — deploying the rewritten rclone helpers"
echo "=============================================================="
echo
echo "This will rewrite the rclone mount units and RESTART the mounts."
echo

ansible-playbook "$PLAY"

echo
echo "=============================================================="
echo "Deploy finished. Now confirm both scripts still work:"
echo "  $PLAN_DIR/acceptance.bash"
echo "=============================================================="
