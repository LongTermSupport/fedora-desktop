#!/usr/bin/env bash
# qa-vmtest-manifest.bash — the VM-test scenario manifest (Plan 00110) is valid and agrees with
# its guest scripts.
#
# vars/vm-test-scenarios.yml is the source of the acceptance bridge's scenario allowlist. The
# tool reads it as YAML, hands it as JSON to the validator the host itself runs
# (helpers/vmtest/validate_manifest.py), then checks that every runnable scenario's `planned`
# equals the PLANNED its guest checker declares and that every guest-prepare fixture runs before
# a declared reboot.
#
# The tool is Go (src-go/qa/vmtestmanifest, command src-go/qa/cmd/qa-vmtest-manifest);
# lib/go-tool.bash builds it into a private directory under the cache and runs it, so the exit
# code reaches the caller. Specification:
# CLAUDE/Plan/00192-.../SPEC-qa-vmtest-manifest.md.
#
# stdout: three lines on success (the validator's counts, then `N scenario(s) agree with their
# guest checker`, then `N scenario fixture(s) run before a declared reboot`); diagnostics on stderr.
# Exit: 0 valid and in agreement, 1 the manifest or a guest script is wrong, 2 the gate could not
# judge (an input unread or unparsed, python3 missing). Under qa-all either failing exit fails
# only this gate.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-vmtest-manifest --scripts "$SCRIPT_DIR" "$@"
