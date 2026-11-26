#!/usr/bin/env bash
# vault-list.bash — add values to a host_vars list whose every item is vault-encrypted, such as
# private_name_terms: `scripts/vault-list.bash add VARIABLE [--import FILE]`. Each value is asked
# with the echo off and only counts are printed (--help shows the usage). Run it on the host.
#
# The tool is Go (src-go/internal/vaultlist, command src-go/cmd/vault-list; V1-V6 in its package
# doc); lib/go-tool.bash builds it from this checkout on each run, so it needs no play first, and
# the command's exit code is this script's.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
VAULT_LIST_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
export VAULT_LIST_ROOT
run_go_command vault-list "$@"
