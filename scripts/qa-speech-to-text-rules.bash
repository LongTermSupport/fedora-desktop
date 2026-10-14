#!/usr/bin/env bash
# qa-speech-to-text-rules.bash — the rules in .semgrep/speech-to-text.yml (Plan 00156).
#
#   model-present-without-weights        a model judged present from its cache snapshot
#   dropdown-label-carries-explanation   a dropdown option that explains itself, truncated
#
# Both came from the same failure: an `auto` model that was never fully downloaded, listed
# as installed under labels too long to read. Each rule's reasoning, and how to fix a
# finding, is in CLAUDE/QA.md under its identifier.
#
# The tool is Go (src-go/qa/sttrules, command src-go/qa/cmd/qa-speech-to-text-rules);
# lib/go-tool.bash builds it into a private directory under the cache and runs it, so the exit
# code reaches the caller. It proves each rule against its fixture (.semgrep/speech-to-text.js
# and .py) on every run, then judges every JavaScript and Python file git would publish, by name.
# Specification: CLAUDE/Plan/00192-.../SPEC-qa-speech-to-text-rules.md.
#
# Usage: scripts/qa-speech-to-text-rules.bash [PATH...]   (paths: run on those files only)
# Exit: 0 clean, 1 findings or a rule that failed its fixture, 2 semgrep missing or broken, or
# a handed file that was not scanned.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-speech-to-text-rules --scripts "$SCRIPT_DIR" "$@"
