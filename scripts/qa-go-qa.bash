#!/usr/bin/env bash
# qa-go-qa.bash — the Go gate: go-qa-ci's go-qa over both modules of src-go.
#
# go-qa is pinned by the tool directive of src-go/qa/go.mod (one require, the same that gives the
# project rules driver its driver.Main; nothing installed system-wide) and configured by
# src-go/qa/go-qa.toml. It installs its own pinned tools into its cache on the first run. It
# checks formatting (--no-fix; with CI set go-qa formats its checkout and then fails on the change), golangci-lint, go vet with the
# bundled analyzers and the project rules (CLAUDE/GoProjectRules.md), dead code, govulncheck, the
# module files, the build, go test -race and the coverage floor. Arguments are passed on
# (qa-changed adds --changed --base REF).
#
# go-qa runs under the QA lock, at nice 19 and ionice idle (src-go/qa/cmd/qa-lock): run on its own
# it waits while another QA run holds the lock; inside qa-all or qa-changed it inherits theirs.
# A leading --no-wait exits 2 at once instead of waiting.
#
# Usage: scripts/qa-go-qa.bash [--no-wait] [go-qa run options]
# Exit: 0 pass, 1 a defence or tool failed, 2 go missing, a usage or a configuration error, or
# the lock not taken.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(dirname "$SCRIPT_DIR")"
if ! command -v go > /dev/null; then
    echo "✗ go: go not installed (install via playbooks/imports/play-golang.yml)" >&2
    exit 2
fi
LOCK_ARGS=()
if [[ "${1:-}" == --no-wait ]]; then
    LOCK_ARGS+=(--no-wait)
    shift
fi
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-lock "${LOCK_ARGS[@]}" --name qa-go-qa --root "$ROOT" -- \
    go -C "$ROOT/src-go/qa" tool go-qa --root "$ROOT/src-go" run --no-fix "$@"
