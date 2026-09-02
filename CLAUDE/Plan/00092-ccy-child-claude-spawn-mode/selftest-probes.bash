#!/usr/bin/env bash
# selftest-probes.bash — prove every probe in probe-invariant.bash CAN FAIL.
#
# SECURITY-MODEL.md section 6 asks a reviewer to "break each one once and watch it go red".
# This script is that, automated, so the answer does not depend on anyone remembering to do
# it by hand. A probe that has only ever been seen passing is not evidence of anything: it
# may be checking nothing at all. That is not hypothetical here — the first draft of the I1
# probe folded grep's error status into its no-match branch, so a failed search reported a
# clean pass, and it was this exercise that found it.
#
# Each case plants a REAL violation, asserts the probe goes red, removes it, and asserts the
# probe goes green again. Everything is created under a temp path or a directory this script
# owns, and cleanup runs on EXIT so an interrupted run leaves nothing behind.
#
# WHERE TO RUN: INSIDE the CCY container, in either state. Cases needing the wrapper are
# reported as UNVERIFIED when it is absent — named and counted, never silently skipped.
#
# Usage: ./CLAUDE/Plan/00092-ccy-child-claude-spawn-mode/selftest-probes.bash
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
PROBE="${HERE}/probe-invariant.bash"
SKILL_DIR="/root/.claude/skills/child-claude"

PASSED=0
FAILED=0
UNVERIFIED=0
LEAK_FILE=""
LEAK_PID=""
PLANTED_SKILL=0

# stop_leak_process — terminate the planted process, tolerating the one status that is
# EXPECTED (143 = killed by SIGTERM, which is exactly what we asked for) and reporting any
# other. Existence is tested through /proc rather than `kill -0`, so nothing needs silencing.
stop_leak_process() {
    local rc=0
    [[ -n "${LEAK_PID}" ]] || return 0
    if [[ -d "/proc/${LEAK_PID}" ]]; then
        kill "${LEAK_PID}"
    fi
    wait "${LEAK_PID}" || rc=$?
    if [[ "${rc}" -ne 0 ]] && [[ "${rc}" -ne 143 ]]; then
        printf 'note: the planted process exited %d rather than by SIGTERM\n' "${rc}" >&2
    fi
    LEAK_PID=""
}

cleanup() {
    if [[ -n "${LEAK_FILE}" ]] && [[ -e "${LEAK_FILE}" ]]; then
        rm -f "${LEAK_FILE}"
    fi
    stop_leak_process
    if [[ "${PLANTED_SKILL}" -eq 1 ]] && [[ -d "${SKILL_DIR}" ]]; then
        rm -rf "${SKILL_DIR}"
        PLANTED_SKILL=0
    fi
}
trap cleanup EXIT

# The token, read the same way the probes read it, and never printed.
TOKEN="$(tr '\0' '\n' </proc/1/environ | grep -m1 '^CLAUDE_CODE_OAUTH_TOKEN=' | cut -d= -f2-)"
if [[ -z "${TOKEN}" ]]; then
    printf '[FATAL] no CLAUDE_CODE_OAUTH_TOKEN in PID 1 — run this inside a CCY container.\n' >&2
    exit 1
fi

# expect_red <label> <invariant> [VAR=value...] — run the probe with a violation in place and
# require a NON-ZERO exit. Probe output is captured, not shown, so a probe that misbehaves and
# echoes the token cannot leak it through this script either.
expect_red() {
    local label="$1" invariant="$2"
    shift 2
    local out="" rc=0
    out="$(env "$@" bash "${PROBE}" "${invariant}" 2>&1)" || rc=$?
    if [[ "${rc}" -eq 0 ]]; then
        printf 'FAIL: %s — the probe PASSED while the violation was in place\n' "${label}"
        FAILED=$((FAILED + 1))
        return 0
    fi
    if [[ "${out}" == *"${TOKEN}"* ]]; then
        printf 'FAIL: %s — the probe printed the token in its own failure output\n' "${label}"
        FAILED=$((FAILED + 1))
        return 0
    fi
    printf 'PASS: %s — went red as it should (exit %d)\n' "${label}" "${rc}"
    PASSED=$((PASSED + 1))
}

# expect_green — the other half. A probe that always fails is as useless as one that always
# passes, and only the pair of assertions rules both out.
expect_green() {
    local label="$1" invariant="$2"
    shift 2
    local out="" rc=0
    out="$(env "$@" bash "${PROBE}" "${invariant}" 2>&1)" || rc=$?
    if [[ "${rc}" -ne 0 ]]; then
        printf 'FAIL: %s — the probe went red with no violation present:\n%s\n' "${label}" "${out}"
        FAILED=$((FAILED + 1))
        return 0
    fi
    printf 'PASS: %s — clean when nothing is wrong\n' "${label}"
    PASSED=$((PASSED + 1))
}

unverified() {
    printf 'UNVERIFIED: %s — %s\n' "$1" "$2"
    UNVERIFIED=$((UNVERIFIED + 1))
}

printf '== proving each probe can fail ==\n\n'

# ── I1: plant the token in a file the probe searches ──────────────────────────────────────
LEAK_FILE="$(mktemp /tmp/plan00092-leak-XXXXXX.txt)"
printf 'stray copy: %s\n' "${TOKEN}" >"${LEAK_FILE}"
expect_red "I1 detects a token planted in /tmp" I1
rm -f "${LEAK_FILE}"
LEAK_FILE=""
expect_green "I1 is clean once the plant is removed" I1

# ── I2: run a process carrying the token in its own argv ──────────────────────────────────
# The value gets there by expansion, so the literal never appears in this file.
#
# The trailing `; :` is load-bearing. Given a SINGLE simple command, `bash -c` optimises by
# exec'ing it, replacing itself — so the process becomes plain `sleep 30` and the extra
# argument is gone. Measured: `bash -c 'sleep 30' bash <tok>` has the command line
# "sleep 30", while `bash -c 'sleep 30; :' bash <tok>` keeps the argument. Without the second
# command this case planted no violation at all, and the probe was blamed for passing.
bash -c 'sleep 30; :' bash "${TOKEN}" &
LEAK_PID=$!
# Give the kernel a moment to publish the new cmdline before scanning for it.
sleep 1
expect_red "I2 detects a token in a process command line" I2
stop_leak_process
expect_green "I2 is clean once that process is gone" I2

# ── I3: export the token under a variable name and re-run the probe ───────────────────────
expect_red "I3 detects a token exported into the environment" I3 "STRAY_COPY=${TOKEN}"
expect_green "I3 is clean without that variable" I3

# ── I6: plant the skill directory a disabled session must not have ────────────────────────
if [[ "${CCY_CHILD_CLAUDE:-}" == "1" ]]; then
    unverified "I6 negative case" "the mode is enabled, so I6 does not apply in this container"
elif [[ -e "${SKILL_DIR}" ]]; then
    unverified "I6 negative case" "${SKILL_DIR} already exists, so this script will not touch it"
else
    mkdir -p "${SKILL_DIR}"
    PLANTED_SKILL=1
    expect_red "I6 detects a skill left behind by an earlier enabled session" I6
    rm -rf "${SKILL_DIR}"
    PLANTED_SKILL=0
    expect_green "I6 is clean once it is removed" I6
fi

# ── I4, I5, I7 need the wrapper, which only exists when the mode is enabled ────────────────
if command -v ccy-claude >/dev/null; then
    expect_red "I7 refuses at the depth limit" I7 "CCY_CLAUDE_DEPTH=${CCY_CHILD_CLAUDE_MAX_DEPTH:-1}"
    expect_green "I4 is clean on a correctly behaving wrapper" I4
    expect_green "I5 is clean on a correctly behaving wrapper" I5
else
    unverified "I4, I5 and I7 cases" \
        "ccy-claude is not on PATH — enable the mode in ccy.env and re-run this script"
fi

printf '\n== summary ==\n'
printf 'passed: %d   failed: %d   unverified: %d\n' "${PASSED}" "${FAILED}" "${UNVERIFIED}"
if [[ "${FAILED}" -gt 0 ]]; then
    printf 'VERDICT: FAIL — at least one probe does not detect the thing it claims to check.\n'
    exit 1
fi
printf 'VERDICT: PASS — every exercised probe detects its violation and is clean without it.\n'
