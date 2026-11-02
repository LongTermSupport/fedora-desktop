#!/usr/bin/env bash
# qa-arch-pin.bash — the rule arch-pinned-artefact (Plan 00243, Defence Before Fix): no download,
# package or image reference fixed to one CPU architecture. The rule is .semgrep/arch-pin.yml,
# proven against .semgrep/arch-pin.fixture on every run; the instances known when it landed are
# the ratchet scripts/arch-pin.allowlist, one row per finding, which may only lose rows (it is
# compared with its copy at QA_BASH_LIMITS_BASE, else the merge-base). How to fix a
# finding: CLAUDE/QA.md under the identifier.
#
# The tool is Go (src-go/qa/archpin, command src-go/qa/cmd/qa-arch-pin); semgrep stays the engine.
# lib/go-tool.bash builds it into a private directory under the cache and runs it, so the exit
# code reaches the caller.
#
# Exit: 0 clean, 1 findings or a rule self-test failure, 2 semgrep missing or broken, or a handed
# file that was not scanned.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-arch-pin --scripts "$SCRIPT_DIR" "$@"
