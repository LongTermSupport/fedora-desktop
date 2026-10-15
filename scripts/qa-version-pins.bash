#!/usr/bin/env bash
# qa-version-pins.bash — Plan 00109: vars/version-pins.yml is well formed, and every pin in it
# still names a file that exists and still declares the var that holds the pin.
#
# The manifest says where every pinned upstream version lives. Its two consumers
# (check-pinned-versions.bash and the installed-against-pinned check) run neither in qa-all, so
# without this gate a manifest that had drifted from the playbooks would surface only when someone
# ran a review tool; a var that was renamed would report the old value for ever.
#
# The tool is Go (src-go/qa/versionpins, command src-go/qa/cmd/qa-version-pins); lib/go-tool.bash
# builds it into a private directory under the cache and runs it, so the exit code reaches the
# caller. Specification: CLAUDE/Plan/00192-.../SPEC-qa-version-pins.md.
#
# Usage: scripts/qa-version-pins.bash
# Exit: 0 clean, 1 the manifest or a pin is wrong, 2 the run could not judge.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-version-pins --scripts "$SCRIPT_DIR" "$@"
