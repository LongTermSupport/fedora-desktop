#!/usr/bin/env bash
# qa-go-qa.bash — the Go gate: go-qa-ci's go-qa over both modules of src-go.
#
# go-qa is pinned by the tool directive of src-go/qa/go.mod (one require, the same that gives the
# project rules driver its driver.Main; nothing installed system-wide) and configured by
# src-go/qa/go-qa.toml. It installs its own pinned tools into its cache on the first run. It
# checks formatting (--no-fix: a gate never rewrites the tree), golangci-lint, go vet with the
# bundled analyzers and the project rules (CLAUDE/GoProjectRules.md), dead code, govulncheck, the
# module files, the build, go test -race and the coverage floor. Arguments are passed on
# (qa-changed adds --changed --base REF).
#
# Usage: scripts/qa-go-qa.bash [go-qa run options]
# Exit: 0 pass, 1 a defence or tool failed, 2 go missing, a usage or a configuration error.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if ! command -v go > /dev/null; then
    echo "✗ go: go not installed (install via playbooks/imports/play-golang.yml)" >&2
    exit 2
fi
exec go -C "$ROOT/src-go/qa" tool go-qa --root "$ROOT/src-go" run --no-fix "$@"
