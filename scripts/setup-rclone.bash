#!/usr/bin/env bash
# setup-rclone.bash — rclone remotes through rclone's own wizard, the configuration vaulted into
# host_vars (rclone_config), the rclone_mounts list edited, and the rclone play offered. Run it
# on the host; it takes no arguments (--help shows the usage).
#
# The tool is Go (src-go/internal/setuprclone, command src-go/cmd/setup-rclone);
# lib/go-tool.bash builds it from this checkout on each run, so it needs no play first, and the
# command's exit code is this script's. Specification: CLAUDE/Plan/00192-.../SPEC-setup-rclone.md.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
SETUP_RCLONE_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
export SETUP_RCLONE_ROOT
run_go_command setup-rclone "$@"
