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
# fails), go vet, go test -race with a whole-module statement coverage floor
# (GO_COVERAGE_FLOOR), golangci-lint run. golangci-lint fmt exits 0 when it
# prints a diff, so the verdict is "printed nothing", not its exit code.
#
# Two modules, because the shipped binaries are built with plain `go build` and src-go/go.mod
# must stay dependency-free: src-go/qa is the nested module holding the project rules
# (go/analysis analyzers, which need golang.org/x/tools). It gets the same steps, first, so
# the rules are proven before they judge anything. The rules then run over src-go as a final
# hard step, over both modules: the driver is built once and used as `go vet -vettool`. Catalogue:
# CLAUDE/GoProjectRules.md.
#
# Exit codes:
#   0  pass
#   1  a step failed
#   2  missing tool (never skipped: a Go gate that cannot run is not a pass)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GO_DIR="$REPO_ROOT/src-go"

for module in "$GO_DIR" "$GO_DIR/qa"; do
    if [[ ! -f "$module/go.mod" ]]; then
        echo "✗ go: $module/go.mod is missing — the module is part of this gate, nothing to check" >&2
        exit 1
    fi
done
if ! command -v go > /dev/null; then
    echo "✗ go: go not installed (install via playbooks/imports/play-golang.yml)" >&2
    exit 2
fi
if ! command -v golangci-lint > /dev/null; then
    echo "✗ go: golangci-lint not installed (install via playbooks/imports/play-python.yml, the QA toolchain play)" >&2
    exit 2
fi

# The floor sits a few points under the measured total, so it catches untested code landing,
# not ordinary drift. Raise it as coverage rises; never lower it to let a change in.
GO_COVERAGE_FLOOR=90

QA_DIR="$GO_DIR/qa"
OUT="$(mktemp)"
PROFILE="$(mktemp)"
VETTOOL="$(mktemp)"
trap 'rm -f "$OUT" "$PROFILE" "$VETTOOL"' EXIT

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

# check_module <dir> — the four steps over one module; sets PACKAGES and COVERAGE.
check_module() {
    local dir="$1"
    cd "$dir"
    run_step "format check ($dir)" golangci-lint fmt --diff ./...
    if [[ -s "$OUT" ]]; then
        echo "✗ go: formatting differs in $dir (fix with: cd $dir && golangci-lint fmt ./...)" >&2
        cat "$OUT" >&2
        exit 1
    fi
    run_step "go vet ($dir)" go vet ./...
    run_step "go test -race ($dir)" go test -race -coverprofile="$PROFILE" ./...
    PACKAGES="$(awk '/^ok/ {n++} END {print n + 0}' "$OUT")"
    # A package with no test files is untested code the coverage floor can hide behind the
    # rest of the module, so every package, cmd/ mains included, has tests. With -coverprofile
    # go prints such a package as "<tab>pkg<tab>coverage: 0.0% of statements" (no "ok" or "?"
    # prefix); without it, as "?  pkg  [no test files]".
    local untested
    untested="$(awk '/^\?/ && /\[no test files\]/ {print $2} /^[ \t]/ && /coverage: 0.0% of statements/ {print $1}' "$OUT")"
    if [[ -n "$untested" ]]; then
        echo "✗ go: package(s) with no test files in $dir:" >&2
        echo "$untested" >&2
        exit 1
    fi
    run_step "coverage report ($dir)" go tool cover -func="$PROFILE"
    COVERAGE="$(awk '$1 == "total:" {sub(/%/, "", $NF); print $NF}' "$OUT")"
    if [[ -z "$COVERAGE" ]]; then
        echo "✗ go: no total in the coverage report for $dir" >&2
        cat "$OUT" >&2
        exit 1
    fi
    if ! awk -v c="$COVERAGE" -v f="$GO_COVERAGE_FLOOR" 'BEGIN {exit !(c >= f)}'; then
        echo "✗ go: statement coverage ${COVERAGE}% of $dir is under the ${GO_COVERAGE_FLOOR}% floor" >&2
        exit 1
    fi
    run_step "golangci-lint ($dir)" golangci-lint run ./...
}

check_module "$QA_DIR"
QA_PACKAGES="$PACKAGES"
QA_COVERAGE="$COVERAGE"
check_module "$GO_DIR"

# The project rules: the driver is built once and run as go vet's tool over src-go (its
# ./... stops at the nested module, so a second pass runs over the qa module itself; the rules
# package is exempt from the one rule it cannot obey, in the rules' own table). Any finding
# fails the gate; a driver that does not build or a vet that cannot run is a failure.
run_step "project rules driver build" go build -C "$QA_DIR" -o "$VETTOOL" ./driver
run_step "project rules (CLAUDE/GoProjectRules.md)" go vet -vettool="$VETTOOL" ./...
run_step "project rules over the qa module" go vet -C "$QA_DIR" -vettool="$VETTOOL" ./...

echo "GO-OK $PACKAGES package(s), all tested: fmt, vet, test -race, coverage ${COVERAGE}% (floor ${GO_COVERAGE_FLOOR}%), golangci-lint clean; qa module $QA_PACKAGES package(s) at ${QA_COVERAGE}%; project rules clean"
