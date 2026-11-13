#!/usr/bin/env bash
# qa-boot-hold.bash — the unit-holds-boot-target rule (.semgrep/boot-hold.yml; issue #263).
#
# A boot target waits for the start of every unit it wants. A unit whose declared start is longer
# than systemd's default 90 s, and that is not ordered after that target, holds boot open while
# it waits. This gate finds every such unit in every file git would publish. The rule's
# reasoning, and how to fix a finding, are in CLAUDE/QA.md under "unit-holds-boot-target".
#
# The rule is proven against .semgrep/boot-hold.fixture on every run. qa-changed runs this gate
# whole (check boot-hold); scripts/qa-routing.tsv has the rows.
#
# The tool is Go (src-go/qa/cmd/qa-boot-hold, the engine of src-go/qa/ghscopes run with its
# BootHold spec); semgrep stays the engine. lib/go-tool.bash builds it into a private directory
# under the cache and runs it, so the exit code reaches the caller.
#
# Exit: 0 clean, 1 findings or a rule self-test failure, 2 semgrep missing or broken, or a handed
# file that was not scanned.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-boot-hold --scripts "$SCRIPT_DIR" "$@"
