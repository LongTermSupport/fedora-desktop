#!/usr/bin/bash
# Go QA — LLM-friendly
# stdout:  one GO-OK summary line (the payload)
# stderr:  the per-step diagnostics
#
# INTERIM GATE. Replaced by the go-qa-ci toolchain (Plan 00191) when that lands; until
# then it is the one place src-go/ is checked, whole (it is fast), by qa-all.bash and
# by qa-changed.bash for any changed src-go/ file.
#
# Steps, in order, each a hard failure: formatting (golangci-lint fmt --diff, any diff
# fails), go vet, go test -race, golangci-lint run. golangci-lint fmt exits 0 when it
# prints a diff, so the verdict is "printed nothing", not its exit code.
#
# Exit codes:
#   0  pass
#   1  a step failed
#   2  missing tool (never skipped: a Go gate that cannot run is not a pass)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GO_DIR="$REPO_ROOT/src-go"

if [[ ! -f "$GO_DIR/go.mod" ]]; then
    echo "✗ go: $GO_DIR/go.mod is missing — nothing to check" >&2
    exit 1
fi
if ! command -v go > /dev/null; then
    echo "✗ go: go not installed (install via playbooks/imports/optional/common/play-golang.yml)" >&2
    exit 2
fi
if ! command -v golangci-lint > /dev/null; then
    echo "✗ go: golangci-lint not installed (install via playbooks/imports/optional/common/play-golang.yml)" >&2
    exit 2
fi

cd "$GO_DIR"
OUT="$(mktemp)"
trap 'rm -f "$OUT"' EXIT

# run_step <name> <command...> — on failure print the step's output and exit 1.
run_step() {
    local name="$1"
    shift
    if ! "$@" > "$OUT" 2>&1; then
        echo "✗ go: $name failed" >&2
        cat "$OUT" >&2
        exit 1
    fi
}

run_step "format check" golangci-lint fmt --diff ./...
if [[ -s "$OUT" ]]; then
    echo "✗ go: formatting differs (fix with: cd src-go && golangci-lint fmt ./...)" >&2
    cat "$OUT" >&2
    exit 1
fi
run_step "go vet" go vet ./...
run_step "go test -race" go test -race ./...
PACKAGES="$(awk '/^ok/ {n++} END {print n + 0}' "$OUT")"
run_step "golangci-lint" golangci-lint run ./...

echo "GO-OK $PACKAGES package(s): fmt, vet, test -race, golangci-lint clean"
