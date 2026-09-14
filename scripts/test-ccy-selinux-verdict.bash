#!/usr/bin/env bash
# Unit-test selinux_enforcing_verdict (Plan 00118, CCY 3.55.0).
#
# Sources lib/common-pure.bash from THIS repo (not the deployed /var/local copy).
#
# WHY THIS TEST EXISTS. On an SELinux-enforcing host a ccy container cannot read
# the project it was handed: container_t is denied read on user_home_t, and the
# audit log said so. ccy had only ever run on desktops where nothing was
# enforced. The verdict decides whether the workspace and key mounts carry a
# relabel, and — like the rootless guard — it is a pure function of two raw
# answers so that every combination can be driven here, including the ones a
# developer machine will never produce.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports
# the full picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PURE_LIB="$REPO_ROOT/files/var/local/claude-yolo/lib/common-pure.bash"

if [ ! -f "$PURE_LIB" ]; then
    echo "FAIL: library not found at $PURE_LIB" >&2
    exit 1
fi
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../files/var/local/claude-yolo/lib/common-pure.bash
source "$PURE_LIB"

if ! declare -F selinux_enforcing_verdict >/dev/null; then
    echo "FAIL: selinux_enforcing_verdict is not defined after sourcing the library" >&2
    exit 1
fi

passed=0
failed=0
check() {
    local enforce="$1" report="$2" want="$3" label="$4" got
    got=$(selinux_enforcing_verdict "$enforce" "$report")
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        echo "  PASS: $label → $got"
    else
        failed=$((failed + 1))
        echo "  FAIL: $label → '$got' (wanted '$want')"
    fi
}

echo "=== selinux_enforcing_verdict ==="
check "Enforcing"   "true"   enforcing "Enforcing host, engine labelling"
check "Enforcing"   "true
"                            enforcing "trailing newline on the engine report is not a third state"
check " Enforcing " " true " enforcing "surrounding whitespace on both answers"
check "Enforcing"   "false"  off       "Enforcing host but the engine does not label (containers.conf label=false)"
check "Permissive"  "true"   off       "Permissive host: denials are logged, nothing is refused"
check "Disabled"    "true"   off       "Disabled host"
check "Disabled"    "false"  off       "Disabled host, engine not labelling"
check ""            "false"  off       "no getenforce (no SELinux userland) and the engine agrees"
check ""            "true"   unknown   "no getenforce but the engine claims labelling: cannot say, so relabel"
check ""            ""       unknown   "nothing readable from either"
check "Enforcing"   ""       unknown   "Enforcing host, engine report empty (podman absent label → zero bytes)"
check "Enforcing"   "Error: cannot connect" unknown "Enforcing host, engine error text captured as the report"
check "getenforce: command not found" "true" unknown "shell error text where getenforce output should be"
check "enforcing"   "true"   unknown   "lower-case is not getenforce's spelling — do not guess"

echo ""
echo "ccy selinux-verdict: passed: $passed  failed: $failed"
if [ "$failed" -ne 0 ]; then
    exit 1
fi
