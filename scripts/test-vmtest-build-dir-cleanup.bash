#!/usr/bin/env bash
# Unit-test vmtest's build-directory cleanup across several builds in one process.
#
# WHY THIS EXISTS. `vmtest refresh-base all` builds every base in one process. The build
# directory used to be removed only by the EXIT trap, which saw just the LAST build's
# directory, so every earlier base left a `<base>.build` beside it (seen on a host after
# refreshing three bases: two left). And the success flag was never reset, so a later
# build that FAILED would have had its directory deleted instead of kept for diagnosis.
#
# The test sources the real vmtest (its main runs only when executed) and drives the two
# halves with directories in a throwaway VMTEST_HOME: a successful build removes its own
# directory at once, and the exit teardown keeps a failed build's.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VMTEST="$SCRIPT_DIR/../files/home/.local/bin/vmtest"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        echo "  PASS: $label"
    else
        failed=$((failed + 1))
        echo "  FAIL: $label → '$got' (wanted '$want')"
    fi
}

bases="$work/lab/bases"
mkdir -p "$bases"

# Two successful builds, then one that fails, in ONE shell, as refresh-base runs them.
# Prints each build directory's state after the teardown has run.
VMTEST_HOME="$work/lab" bash -c '
    . "$1"
    bases="$2"
    trap teardown_build EXIT INT TERM HUP
    for name in first second; do
        BUILD_DIR="${bases}/${name}.build"
        mkdir -p "${BUILD_DIR}"
        build_dir_finished
    done
    BUILD_DIR="${bases}/third.build"
    mkdir -p "${BUILD_DIR}"
    false
' _ "$VMTEST" "$bases" >"$work/run.log" 2>&1
rc=$?

state() { if [ -d "$bases/$1.build" ]; then echo kept; else echo removed; fi; }
check "the failed build exits non-zero" "1" "$rc"
check "first successful build: directory removed" "removed" "$(state first)"
check "second successful build: directory removed" "removed" "$(state second)"
check "the failed build after them: directory kept for diagnosis" "kept" "$(state third)"

if [ "$failed" -ne 0 ]; then
    echo "  run output:"
    cat "$work/run.log"
fi
echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
