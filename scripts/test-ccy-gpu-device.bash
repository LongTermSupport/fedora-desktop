#!/usr/bin/env bash
# Unit-test gpu_device_flags (Plan 00120, CCY 3.56.0).
#
# Sources lib/common-pure.bash from THIS repo (not the deployed /var/local copy).
#
# WHY THIS TEST EXISTS. ccy passed `--device /dev/dri:/dev/dri` to every container
# unconditionally, and podman aborts the run (exit 125, `stat /dev/dri: no such file or
# directory`) on a host with no DRM node — every headless server, every serial-console VM.
# Desktops never showed it because they all have a GPU. The flags are now a pure function
# of a path, so the no-GPU case can be driven here on a machine that has one, and the
# with-GPU case on a machine that does not.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PURE_LIB="$REPO_ROOT/files/var/local/claude-yolo/lib/common-pure.bash"
LAUNCHER="$REPO_ROOT/files/var/local/claude-yolo/claude-yolo"

if [ ! -f "$PURE_LIB" ]; then
    echo "FAIL: library not found at $PURE_LIB" >&2
    exit 1
fi
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../files/var/local/claude-yolo/lib/common-pure.bash
source "$PURE_LIB"

if ! declare -F gpu_device_flags >/dev/null; then
    echo "FAIL: gpu_device_flags is not defined after sourcing the library" >&2
    exit 1
fi

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

echo "=== gpu_device_flags ==="

mkdir -p "$work/dri"
got=$(gpu_device_flags "$work/dri")
check "a present DRM directory yields --device <path>:<path>, one flag per line" \
    "--device
$work/dri:$work/dri" "$got"

got=$(gpu_device_flags "$work/absent"); rc=$?
check "an absent path yields nothing" "" "$got"
check "…and is not an error (a missing GPU is the answer)" "0" "$rc"

: > "$work/file"
got=$(gpu_device_flags "$work/file")
check "a path that exists but is not a directory yields nothing" "" "$got"

# The launcher consumes the flags with mapfile, so the with-GPU shape must land as exactly
# two argv elements and the no-GPU shape as an empty array.
flags=(); mapfile -t flags < <(gpu_device_flags "$work/dri")
check "mapfile: present → 2 argv elements" "2" "${#flags[@]}"
flags=(); mapfile -t flags < <(gpu_device_flags "$work/absent")
check "mapfile: absent → empty array" "0" "${#flags[@]}"

# The launcher carries no unconditional device line and expands the array in its run argv;
# a helper that is defined but unconsumed would leave the abort in place.
check "launcher: no unconditional --device /dev/dri line" "0" \
    "$(grep -c -E '^\s*--device /dev/dri:/dev/dri' "$LAUNCHER")"
check "launcher: the run argv expands GPU_DEVICE_FLAGS" "1" \
    "$(grep -c -F -- "\"\${GPU_DEVICE_FLAGS[@]}\"" "$LAUNCHER")"
check "launcher: GPU_DEVICE_FLAGS is filled from gpu_device_flags /dev/dri" "1" \
    "$(grep -c -F 'mapfile -t GPU_DEVICE_FLAGS < <(gpu_device_flags /dev/dri)' "$LAUNCHER")"

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
