#!/usr/bin/env bash
# qa-hidden-bindings.bash — the rule project_hidden_bindings outside Go.
#
# Every action is a visible, named choice; a key is only a shortcut to one. The Go analyzer of
# the same name (scripts/qa-go.bash) holds the Go tools; this gate holds shell (fzf --expect, and
# fzf --bind of a key to anything but navigation) and Python (a Textual binding whose action no
# Button runs). The rule's reasoning, and how to fix a finding, are in CLAUDE/GoProjectRules.md
# under project_hidden_bindings.
#
# Each half is proven against its fixture in tests/fixtures/hidden-bindings on every run. The tool
# is Go (src-go/qa/hiddenbindings, command src-go/qa/cmd/qa-hidden-bindings); the Python half is
# helpers/qa_environment/textual_bindings.py. lib/go-tool.bash builds the tool into a private
# directory under the cache and runs it, so the exit code reaches the caller.
#
# Usage: scripts/qa-hidden-bindings.bash [PATH...]   (paths: judge those files only)
# Exit: 0 clean, 1 findings or a rule that failed its fixture, 2 python3 missing, a file unread or
# unparsed.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-hidden-bindings --scripts "$SCRIPT_DIR" "$@"
