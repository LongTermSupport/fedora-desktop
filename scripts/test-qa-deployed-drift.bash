#!/usr/bin/bash
# qa-deployed-drift.bash's verdicts on the deployed Go commands, asserted against the real
# script (Plan 00192).
#
# The script compares repo-owned scripts with their deployed copies and asks `go-source drift`
# about the deployed Go commands. Three things are pinned here, none of which the gate could
# show on its own in the CCY container (it skips there):
#   - a Go check that could not run is "could not run" (exit 2), never reported as drift;
#   - script pairs and Go commands are counted apart, so one kind of drift is never worded as
#     the other;
#   - drift in either kind fails the gate.
#
# The fixture is a throwaway directory holding a COPY of the script (its REPO_ROOT is where it
# lives, and the real checkout is skipped as a CCY container or a linked worktree) beside a
# SYMLINK to the real src-go, so the Go tool and the commands' sources are the real ones.
#
# Exit codes: 0 = every case passed; 1 = at least one case failed.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GOCACHE="$(go env GOCACHE)"
GOMODCACHE="$(go env GOMODCACHE)"
export GOCACHE GOMODCACHE XDG_CACHE_HOME="${XDG_CACHE_HOME:-$HOME/.cache}"

passed=0
failed=0
FIXTURE_ROOT=$(mktemp -d)
trap 'rm -rf "$FIXTURE_ROOT"' EXIT

# world <name> — a fixture checkout with its own HOME; prints its directory.
world() {
    local root="$FIXTURE_ROOT/$1"
    mkdir -p "$root/scripts/lib" "$root/files/home/.local/bin" "$root/home/.local/bin" "$root/playbooks"
    cp "$REPO_ROOT/scripts/qa-deployed-drift.bash" "$root/scripts/"
    cp "$REPO_ROOT/scripts/lib/go-tool.bash" "$root/scripts/lib/"
    ln -s "$REPO_ROOT/src-go" "$root/src-go"
    echo "$root"
}

# digest_of <root> — the checkout's source digest of ccy-teams.
digest_of() {
    go run -C "$REPO_ROOT/src-go/qa" ./cmd/go-source digest --source "$1" --command ccy-teams
}

# install_stub <root> <answer> — an installed ccy-teams that answers --version with <answer>,
# and the table row naming it.
install_stub() {
    local root="$1" answer="$2" stub="$1/home/.local/bin/ccy-teams"
    printf '#!/bin/sh\necho %s\n' "$answer" > "$stub"
    chmod 0755 "$stub"
    printf 'ccy-teams\t%s\tplaybooks/imports/play-claude-yolo.yml\n' "$stub" > "$root/scripts/go-binary-installs.tsv"
}

OUT=""
RC=0
run() {
    RC=0
    OUT="$(cd "$1" && HOME="$1/home" bash scripts/qa-deployed-drift.bash 2>&1)" || RC=$?
}

# expect <label> <exit> <present>... -- <absent>... — the exit code, what the output says, and
# what it must not say.
expect() {
    local label="$1" want="$2" needle absent=0
    shift 2
    for needle in "$@"; do
        if [ "$needle" = "--" ]; then
            absent=1
            continue
        fi
        if [ "$absent" -eq 0 ] && [[ "$OUT" != *"$needle"* ]]; then
            failed=$((failed + 1))
            printf '  FAIL  %s\n        missing: %s\n        output: %s\n' "$label" "$needle" "$OUT" >&2
            return
        fi
        if [ "$absent" -eq 1 ] && [[ "$OUT" == *"$needle"* ]]; then
            failed=$((failed + 1))
            printf '  FAIL  %s\n        must not say: %s\n        output: %s\n' "$label" "$needle" "$OUT" >&2
            return
        fi
    done
    if [ "$RC" -ne "$want" ]; then
        failed=$((failed + 1))
        printf '  FAIL  %s\n        exit %s, wanted %s\n        output: %s\n' "$label" "$RC" "$want" "$OUT" >&2
        return
    fi
    passed=$((passed + 1))
    printf '  PASS  %s -> exit %s\n' "$label" "$RC"
}

echo "=== qa-deployed-drift.bash, driven against fixture checkouts ==="

root="$(world match)"
install_stub "$root" "$(digest_of "$root")"
run "$root"
expect "a Go command that matches the checkout" 0 \
    "1 deployed Go command(s) match the checkout" "0 deployed script(s) match the repo"

root="$(world gostale)"
install_stub "$root" "0000000000000000000000000000000000000000000000000000000000000000"
run "$root"
expect "a Go command built from older source" 1 \
    "1 of 1 deployed Go command(s) differ from the checkout" "ansible-playbook playbooks/imports/play-claude-yolo.yml" \
    -- "could not run" "deployed script(s) differ"

root="$(world cannotrun)"
install_stub "$root" "x"
printf 'ccy-teams only-two-columns\n' > "$root/scripts/go-binary-installs.tsv"
run "$root"
expect "a Go check that could not run is not drift" 2 \
    "could not run" "go-binary-installs.tsv" \
    -- "differ"

root="$(world scriptdrift)"
install_stub "$root" "$(digest_of "$root")"
echo repo > "$root/files/home/.local/bin/tool"
echo deployed > "$root/home/.local/bin/tool"
run "$root"
expect "a script pair that differs, the Go command matching" 1 \
    "1 of 1 deployed script(s) differ from the repo" "1 deployed Go command(s) match the checkout" \
    -- "Go command(s) differ" "could not run"

root="$(world both)"
install_stub "$root" "0000000000000000000000000000000000000000000000000000000000000000"
echo repo > "$root/files/home/.local/bin/tool"
echo deployed > "$root/home/.local/bin/tool"
run "$root"
expect "both kinds of drift, each counted on its own" 1 \
    "1 of 1 deployed script(s) differ from the repo" "1 of 1 deployed Go command(s) differ from the checkout"

echo
echo "passed: $passed failed: $failed"
[ "$failed" -eq 0 ]
