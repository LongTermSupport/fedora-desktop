#!/usr/bin/env bash
# qa-find-race.bash — the find-race-negated-stat-test rule (.semgrep/find-race.yml; issue #152).
#
# find -ignore_readdir_race with a negated stat test matches an entry that vanished between
# readdir and stat, so a probe over a tree that changes while it is walked reports what is not
# there. This gate finds every such find in every file git would publish. The rule's reasoning,
# and how to fix a finding, are in CLAUDE/QA.md under "find-race-negated-stat-test".
#
# The rule is proven against .semgrep/find-race.fixture on every run. qa-changed runs this gate
# whole (check find-race) when the rule, the fixture, this wrapper or the tool change;
# scripts/qa-routing.tsv has the rows.
#
# The tool is Go (src-go/qa/cmd/qa-find-race, the engine of src-go/qa/ghscopes run with its
# FindRace spec); semgrep stays the engine. lib/go-tool.bash builds it into a private directory
# under the cache and runs it, so the exit code reaches the caller.
#
# Exit: 0 clean, 1 findings or a rule self-test failure, 2 semgrep missing or broken, or a handed
# file that was not scanned.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-find-race --scripts "$SCRIPT_DIR" "$@"
