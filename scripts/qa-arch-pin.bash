#!/usr/bin/env bash
# qa-arch-pin.bash — the arch-pinned-artefact rule (.semgrep/arch-pin.yml; Plan 00243).
#
# fedora-desktop runs on x86_64 and on aarch64. A download, package or image reference that spells
# one architecture with no selection by the one the build or host runs on installs nothing, or
# the wrong binary, on the other. This gate finds every such reference in every file git would
# publish. The rule's reasoning, and how to fix a finding, are in CLAUDE/QA.md under
# "arch-pinned-artefact".
#
# The rule is proven against .semgrep/arch-pin.fixture on every run. qa-changed runs this gate
# whole (check arch-pin) when the rule, the fixture, this wrapper or the tool change;
# scripts/qa-routing.tsv has the rows.
#
# The tool is Go (src-go/qa/cmd/qa-arch-pin, the engine of src-go/qa/ghscopes run with its
# ArchPin spec); semgrep stays the engine. lib/go-tool.bash builds it into a private directory
# under the cache and runs it, so the exit code reaches the caller.
#
# Exit: 0 clean, 1 findings or a rule self-test failure, 2 semgrep missing or broken, or a handed
# file that was not scanned.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-arch-pin --scripts "$SCRIPT_DIR" "$@"
