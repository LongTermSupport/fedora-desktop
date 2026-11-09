#!/usr/bin/env bash
# consumer-install.bash — the install for someone who does not write code (Plan 00228): no
# GitHub account, no SSH key, no developer plays, the optional apps offered at the end.
# ./run.bash --consumer installs Go and git, fetches this checkout and execs this script.
#
# The tool is Go (src-go/internal/consumer, command src-go/cmd/consumer-install);
# lib/go-tool.bash builds it from this checkout on each run, so it needs no play first, and the
# command's exit code is this script's.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
CONSUMER_INSTALL_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
export CONSUMER_INSTALL_ROOT
run_go_command consumer-install "$@"
