#!/usr/bin/env bash
# qa-secret-screen.bash — the secret-on-normal-screen rules (.semgrep/secret-screen.yml; issues
# #255 and #261).
#
# A terminal keeps what its normal screen showed in the scrollback, and tmux in a pane's
# history: a password or token shown there "once" stays readable long after. This gate finds a
# secret written to a terminal's normal screen in every file git would publish (shell, Python,
# Ansible; Go is project_secret_scrollback's). The rules' reasoning, and how to fix a finding,
# are in CLAUDE/QA.md under "secret-on-normal-screen".
#
# The rules are proven against their fixtures (.semgrep/secret-screen.bash, .py and
# .fixture.yml) on every run. qa-changed runs this gate whole (check secret-screen);
# scripts/qa-routing.tsv has the rows.
#
# The tool is Go (src-go/qa/cmd/qa-secret-screen, the engine of src-go/qa/ghscopes run with its
# SecretScreen spec); semgrep stays the engine. lib/go-tool.bash builds it into a private
# directory under the cache and runs it, so the exit code reaches the caller.
#
# Exit: 0 clean, 1 findings or a rule self-test failure, 2 semgrep missing or broken, or a handed
# file that was not scanned.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-secret-screen --scripts "$SCRIPT_DIR" "$@"
