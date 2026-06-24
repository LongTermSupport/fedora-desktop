#!/usr/bin/env bash
# Plan 00055 — deploy the container-process watchdog on the HOST.
#
# HOST-ONLY: this runs Ansible. NEVER run it inside the CCY container (/workspace)
# — per CLAUDE/ContainerRules.md the container is edit-only; deployment happens on
# the Fedora host. Runs the opt-in play that installs the helper, the
# container-watch CLI wrapper, the systemd --user timer, and the GNOME panel
# extension. Idempotent — safe to re-run. Extra args pass through to
# ansible-playbook (e.g. --check, --diff, -t <tag>).
#
#   ./CLAUDE/Plan/00055-container-process-watchdog/deploy.bash
#   ./CLAUDE/Plan/00055-container-process-watchdog/deploy.bash --check --diff
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if ! REPO_ROOT="$(cd "$SCRIPT_DIR" && git rev-parse --show-toplevel 2>&1)"; then
    echo "Cannot locate repo root (not a git checkout?): $REPO_ROOT" >&2
    exit 1
fi
cd "$REPO_ROOT"

exec ansible-playbook playbooks/imports/optional/common/play-container-watch.yml "$@"
