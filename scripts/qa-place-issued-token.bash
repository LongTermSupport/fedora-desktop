#!/usr/bin/env bash
# qa-place-issued-token.bash — the token-bundle-into-place-issued rule
# (.semgrep/place-issued-token.yml; issue #248).
#
# teamchat seat place-issued places a ccy pool seat, which keeps a password, and refuses a bundle
# issued with --credential token. This gate finds every command, script or help text that pipes
# a token bundle into it, or offers the choice password|token for that pipe. The rule's
# reasoning, and how to fix a finding, are in CLAUDE/QA.md under "token-bundle-into-place-issued".
#
# The rule is proven against .semgrep/place-issued-token.fixture on every run. qa-changed runs
# this gate whole (check place-issued-token); scripts/qa-routing.tsv has the rows.
#
# The tool is Go (src-go/qa/cmd/qa-place-issued-token, the engine of src-go/qa/ghscopes run with
# its PlaceIssuedToken spec); semgrep stays the engine. lib/go-tool.bash builds it into a private
# directory under the cache and runs it, so the exit code reaches the caller.
#
# Exit: 0 clean, 1 findings or a rule self-test failure, 2 semgrep missing or broken, or a handed
# file that was not scanned.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-place-issued-token --scripts "$SCRIPT_DIR" "$@"
