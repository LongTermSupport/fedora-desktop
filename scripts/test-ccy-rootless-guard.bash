#!/usr/bin/env bash
# Unit-test ccy's rootless-engine verdict (Plan 00072).
#
# Sources common-pure.bash from THIS repo (not the deployed /var/local copy) so a
# fix can be verified before running the playbook. That library is engine-free by
# contract, so these tests run anywhere — no podman, no docker, no daemon.
#
# What is under test is the DECISION, not the query: engine_rootless_verdict takes
# the engine's raw report as a string and returns rootless | rootful | unknown.
# Keeping the decision pure is what makes these negative controls possible at all —
# you cannot ask a real engine to be rootful just to prove the guard notices.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the
# full picture, and each result is checked explicitly. (Same reason, same shape as
# scripts/test-ccy-ssh-probe.bash.)
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB="$REPO_ROOT/files/var/local/claude-yolo/lib/common-pure.bash"

if [ ! -f "$LIB" ]; then
    echo "FAIL: library not found at $LIB" >&2
    exit 1
fi
# source-path makes the relative source= resolve from THIS script's directory rather
# than the caller's cwd — without it shellcheck -x reports SC1091 "does not exist".
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../files/var/local/claude-yolo/lib/common-pure.bash
source "$LIB"

if ! declare -F engine_rootless_verdict >/dev/null; then
    echo "FAIL: engine_rootless_verdict is not defined after sourcing $LIB" >&2
    echo "      (the guard is absent, not merely broken)" >&2
    exit 1
fi

PASSED=0
FAILED=0

# check <description> <engine> <raw-report> <expected-verdict>
check() {
    local desc="$1" engine="$2" report="$3" expected="$4"
    local actual
    actual=$(engine_rootless_verdict "$engine" "$report")
    if [ "$actual" = "$expected" ]; then
        printf '  PASS  %-58s -> %s\n' "$desc" "$actual"
        PASSED=$((PASSED + 1))
    else
        printf '  FAIL  %-58s -> got %-8s want %s\n' "$desc" "$actual" "$expected"
        printf '        raw report was: %q\n' "$report"
        FAILED=$((FAILED + 1))
    fi
}

echo ""
echo "=== podman: the field ccy actually asks for ==="
# Verified live on the owner's host, Plan 00068 triage run 20260731-225344:
#   podman info --format '{{.Host.Security.Rootless}}'  ->  true
check "rootless daemon reports true"          podman 'true'    rootless
check "trailing newline is not a third state" podman $'true\n' rootless
check "leading/trailing spaces tolerated"     podman '  true ' rootless

echo ""
echo "=== podman: THE case this guard exists for ==="
check "rootful daemon reports false"          podman 'false'   rootful

echo ""
echo "=== docker: SecurityOptions carries name=rootless only when rootless ==="
check "rootless daemon" docker '[name=rootless name=seccomp,profile=builtin name=cgroupns]' rootless
check "rootful daemon"  docker '[name=seccomp,profile=builtin name=cgroupns]'               rootful

echo ""
echo "=== unknown is NOT a pass ==="
# The load-bearing block. Plan 00068's group-F probe measured the failure mode this
# defends: asking podman for an ABSENT label returns exit 0 and ZERO BYTES, so a
# naive comparison of two unknowns comes back "equal" and reports the safe-sounding
# answer having measured nothing. A guard that treats silence as safety passes
# hardest exactly when it can see least.
check "empty output"                    podman ''                                      unknown
check "whitespace-only output"          podman $'  \n\t '                              unknown
check "info failed, error in capture"   podman 'Error: unable to connect to Podman socket' unknown
check "field moved (Go <no value>)"     podman '<no value>'                            unknown
check "unparseable value"               podman 'maybe'                                 unknown
check "docker daemon unreachable"       docker 'Cannot connect to the Docker daemon.'  unknown
check "docker empty"                    docker ''                                      unknown
check "engine ccy does not know"        nerdctl 'true'                                 unknown

echo ""
echo "=== discrimination check ==="
# A control that FIRES is not necessarily a control that DISCRIMINATES. If the
# function returned `unknown` unconditionally, every negative case above would pass
# and the guard would still be worthless. These two assert the positive and negative
# verdicts are actually reachable and distinct.
if [ "$(engine_rootless_verdict podman 'true')" = "$(engine_rootless_verdict podman 'false')" ]; then
    echo "  FAIL  verdicts for true/false are identical — the function does not discriminate"
    FAILED=$((FAILED + 1))
else
    echo "  PASS  rootless and rootful verdicts are distinct"
    PASSED=$((PASSED + 1))
fi

echo ""
echo "──────────────────────────────────────────────────────────────"
printf 'passed: %d   failed: %d\n' "$PASSED" "$FAILED"

if [ "$PASSED" -eq 0 ]; then
    echo "ERROR: zero tests ran — discovery is broken, not the code clean" >&2
    exit 1
fi
if [ "$FAILED" -ne 0 ]; then
    exit 1
fi
echo "OK"
