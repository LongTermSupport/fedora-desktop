#!/usr/bin/env bash
# qa-teamchat-separate.bash — the gate teamchat-keeps-off-v1 (Plan 00209, DESIGN-v2-D-migration.md
# section 4.1 part 1): no file v2 of the agent-team bus (teamchat) owns holds a v1 name.
#
# The scope is what git would publish that v2 owns: src-go/internal/teamchat/ (bar the two named
# homes of deliberate v1 references, v1names and migrate/v1), src-go/cmd/teamchat*, files/,
# tasks/ and vars/ below a teamchat* component, and any play-teamchat*.yml or .yaml under
# playbooks/imports/. A v2 play or task file may name only those files and the shared ones the
# gate lists. The v1 names are src-go/qa/teamchatseparate/v1-names.txt; the remaining allowed
# references, each with its reason, are allowed.tsv beside it, one row per line.
#
# The tool is Go (src-go/qa/teamchatseparate, command src-go/qa/cmd/qa-teamchat-separate);
# lib/go-tool.bash builds it into a private directory under the cache and runs it, so the exit
# code reaches the caller.
#
# Exit codes: 0 clean, 1 a v1 reference in a v2 file, a v2 play or task file naming a file v2
# does not own, or an allowlist row that allows nothing or names on more than one line, 2 bad
# usage or the run could not judge (git failing, no v2 file, a missing or malformed list, an
# unreadable file).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-teamchat-separate --scripts "$SCRIPT_DIR" "$@"
