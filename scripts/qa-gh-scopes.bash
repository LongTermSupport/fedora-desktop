#!/usr/bin/env bash
# qa-gh-scopes.bash — the gh-scope-outside-ssot rule (.semgrep/gh-scopes.yml).
#
# vars/github-required-scopes.yml is the one statement of which scopes a gh token needs, and
# helpers/github_scopes/ is the one implementation of how scopes imply one another. This gate
# finds every other place a GitHub OAuth scope is named. The rule's reasoning, and how to fix a
# finding, are in CLAUDE/QA.md under "gh-scope-outside-ssot".
#
# The rule is proven against .semgrep/gh-scopes.fixture on every run, so a change to either file
# runs this gate (scripts/qa-routing.tsv selects a suite by the files its script names).
#
# The tool is Go (src-go/qa/ghscopes, command src-go/qa/cmd/qa-gh-scopes); semgrep stays the
# engine. lib/go-tool.bash builds it into a private directory under the cache and runs it, so the
# exit code reaches the caller. Specification: CLAUDE/Plan/00192-.../SPEC-qa-gh-scopes.md.
#
# Exit: 0 clean, 1 findings or a rule self-test failure, 2 semgrep missing or broken, or a handed
# file that was not scanned.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-gh-scopes --scripts "$SCRIPT_DIR" "$@"
