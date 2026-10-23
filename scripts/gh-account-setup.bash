#!/usr/bin/env bash
# gh-account-setup.bash — GitHub multi-account setup: gh logins with the required scopes, the
# SSH keys, their upload and their test. Called by run.bash (--setup-all) or by hand
# (--add=ALIAS:USERNAME, --check); --help lists the modes.
#
# The tool is Go (src-go/internal/ghaccount, command src-go/cmd/gh-account-setup);
# lib/go-tool.bash builds it from this checkout on each run, so it needs no play first, and the
# command's exit code is this script's. Specification: CLAUDE/Plan/00192-.../SPEC-gh-account-setup.md.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
GH_ACCOUNT_SETUP_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
export GH_ACCOUNT_SETUP_ROOT
run_go_command gh-account-setup "$@"
