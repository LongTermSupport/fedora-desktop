#!/usr/bin/env bash
# qa-argv-leak.bash — the secret-in-argv rules (.semgrep/argv-leak.yml; issue #220).
#
# A process's command line is readable by every local user while it runs (/proc/<pid>/cmdline).
# This gate finds every passphrase or password put on one: an option such as ssh-keygen -P given
# an expanded value, and in Ansible a secret rendered into a task's command text or its
# environment:, both of which Ansible puts on a /bin/sh -c line. The rules' reasoning, and how
# to fix a finding, are in CLAUDE/QA.md under "secret-in-argv".
#
# The rules are proven against .semgrep/argv-leak.fixture.yml on every run. qa-changed runs
# this gate whole (check secret-in-argv) after any change; scripts/qa-routing.tsv has the rows.
#
# The tool is Go (src-go/qa/cmd/qa-argv-leak, the engine of src-go/qa/ghscopes run with its
# ArgvLeak spec); semgrep stays the engine. lib/go-tool.bash builds it into a private
# directory under the cache and runs it, so the exit code reaches the caller.
#
# Exit: 0 clean, 1 findings or a rule self-test failure, 2 semgrep missing or broken, or a handed
# file that was not scanned.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-argv-leak --scripts "$SCRIPT_DIR" "$@"
