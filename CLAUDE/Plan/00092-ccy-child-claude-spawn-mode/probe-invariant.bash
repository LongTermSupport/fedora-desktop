#!/usr/bin/env bash
# probe-invariant.bash <I1|I2|I3|I4|I5|I6|I7> — check ONE invariant from SECURITY-MODEL.md.
#
# Invoked as a leg by acceptance.bash, and independently runnable for debugging. Exits 0 when
# the invariant holds, non-zero with a named reason when it does not.
#
# WHERE TO RUN: INSIDE the CCY container. acceptance.bash enforces that with
# plan_require_container; this script is its leg, so it inherits the guard.
#
# THE ONE RULE IN THIS FILE: the token value is never printed, never exported, and never
# passed as an ARGUMENT to anything — not even to grep, because an argument is exactly what
# invariant I2 forbids, and a probe that violates the rule it is testing proves nothing.
# In-memory comparisons therefore use bash pattern matching, and the one real file search
# feeds its pattern over stdin.
set -euo pipefail

INVARIANT="${1:?usage: probe-invariant.bash <I1|I2|I3|I4|I5|I6|I7>}"

WRAPPER_NAME="ccy-claude"
SKILL_DIR="/root/.claude/skills/child-claude"
STUB_DIR=""

cleanup() {
    if [[ -n "${STUB_DIR}" ]] && [[ -d "${STUB_DIR}" ]]; then
        rm -rf "${STUB_DIR}"
    fi
}
trap cleanup EXIT

# ── the token, read once, never printed ───────────────────────────────────────────────────
#
# Sourced from PID 1 because that is where claude-yolo puts it (-e CLAUDE_CODE_OAUTH_TOKEN at
# claude-yolo:3021). Absence is a HARD FAILURE, not a skip: every probe below searches for
# this value, so without it each one would pass vacuously — "absence of a check is not a
# passing check" (PlanScriptStandards R11).
read_token() {
    local tok=""
    if [[ ! -r /proc/1/environ ]]; then
        printf '[FAIL] /proc/1/environ is not readable, so no probe can search for the token.\n' >&2
        printf '       Every check would pass vacuously. Refusing to report a vacuous pass.\n' >&2
        return 1
    fi
    # `|| tok=""` is required, not error hiding: grep exits 1 when the variable is absent,
    # pipefail propagates it, and set -e would kill the probe AT THE ASSIGNMENT, making every
    # message below unreachable. The empty case is classified and reported immediately.
    tok="$(tr '\0' '\n' </proc/1/environ | grep -m1 '^CLAUDE_CODE_OAUTH_TOKEN=' | cut -d= -f2-)" ||
        tok=""
    if [[ -z "${tok}" ]]; then
        printf '[FAIL] PID 1 holds no CLAUDE_CODE_OAUTH_TOKEN.\n' >&2
        printf '       Either this is not a CCY container, or it was launched without a token.\n' >&2
        printf '       Every probe searches for that value, so all of them would pass vacuously.\n' >&2
        return 1
    fi
    printf '%s' "${tok}"
}

require_wrapper() {
    if ! command -v "${WRAPPER_NAME}" >/dev/null; then
        printf '[FAIL] %s is not on PATH, so this probe cannot run.\n' "${WRAPPER_NAME}" >&2
        printf '       Enable the mode in /workspace/.claude/ccy/ccy.env and restart the container.\n' >&2
        return 1
    fi
}

# ── I1: no on-disk copy anywhere, and above all nothing under /workspace ──────────────────
probe_I1() {
    local token hits="" existing=() d
    token="$(read_token)"
    printf '== I1: the token is written to no file\n'
    # /workspace leads the list and is named for a reason: it is a HOST MOUNT, so a copy there
    # outlives the container on the user's real filesystem (threat T2). /proc is deliberately
    # NOT searched — PID 1's environ is the legitimate source, not a leak.
    for d in /workspace/.claude /root/.claude.json /etc /run /tmp /usr/local/bin /opt/claude-yolo; do
        if [[ -e "${d}" ]]; then
            existing+=("${d}")
        fi
    done
    if [[ "${#existing[@]}" -eq 0 ]]; then
        printf '[FAIL] none of the searched paths exist, so this probe proved nothing.\n' >&2
        return 1
    fi
    # Regular files are enumerated FIRST, and grep is never asked to recurse.
    #
    # `grep -r` walks into /run, meets the Wayland socket, and exits 2 with
    # "No such device or address". Measured: neither --devices=skip nor -D skip suppresses
    # that during recursion, so the search cannot complete. Handing grep an explicit list of
    # -type f paths sidesteps it entirely, and a file PATH in argv is not a secret — only the
    # token is, and it still arrives on stdin.
    local files=() batch=() f findRc=0
    while IFS= read -r -d '' f; do
        # -type f already excludes sockets and FIFOs, and the second test here is deliberate
        # belt-and-braces: handing grep a socket makes it exit 2, and an exit-2 search proves
        # nothing. `-f` re-tests through the same stat find used, `-r` drops anything grep
        # could not open anyway. Cheap, and it removes a whole class of "the search failed"
        # verdicts that look like findings but are not.
        if [[ -f "${f}" ]] && [[ -r "${f}" ]]; then
            files+=("${f}")
        fi
    done < <(find "${existing[@]}" -type f -print0)
    wait $! || findRc=$?
    if [[ "${findRc}" -ne 0 ]]; then
        printf '[FAIL] the file enumeration failed (find exit %d), so nothing was proved.\n' \
            "${findRc}" >&2
        return 1
    fi
    if [[ "${#files[@]}" -eq 0 ]]; then
        printf '[FAIL] the searched paths contain no regular files, so this proved nothing.\n' >&2
        return 1
    fi

    # grep's THREE exit statuses are all distinguished, and that is the point. 0 means found,
    # 1 means clean, 2 means grep itself failed. An earlier draft wrote
    # `if hits="$(grep ...)"`, which folds 2 into the same branch as 1 and reports a failed
    # SEARCH as a clean pass — the "a control silently becomes a no-op" failure that
    # PlanScriptStandards exists to prevent. selftest-probes.bash is what caught it.
    local i=0 rc=0 batchSize=200
    while [[ "${i}" -lt "${#files[@]}" ]]; do
        batch=("${files[@]:i:batchSize}")
        i=$((i + batchSize))
        rc=0
        hits="$(printf '%s\n' "${token}" | grep -lF --binary-files=without-match -f - \
            "${batch[@]}")" || rc=$?
        case "${rc}" in
            0)
                printf '[FAIL] the token was found in these files:\n' >&2
                printf '%s\n' "${hits}" >&2
                return 1
                ;;
            1) ;;
            *)
                printf '[FAIL] the search itself failed (grep exit %d), so nothing was proved.\n' \
                    "${rc}" >&2
                return 1
                ;;
        esac
    done
    printf 'searched %d regular file(s) under %d path(s), the token is in none of them\n' \
        "${#files[@]}" "${#existing[@]}"
}

# ── I2: never in any process's argv ───────────────────────────────────────────────────────
probe_I2() {
    local token dir pid cmdline scanned=0 vanished=0 found=0
    token="$(read_token)"
    printf '== I2: the token appears in no process command line\n'
    for dir in /proc/[0-9]*; do
        pid="${dir##*/}"
        # A process can exit between the glob and the read. That is a race, not a violation,
        # so it is COUNTED AND REPORTED rather than silently swallowed.
        if ! cmdline="$(tr '\0' ' ' <"${dir}/cmdline")"; then
            vanished=$((vanished + 1))
            continue
        fi
        scanned=$((scanned + 1))
        if [[ "${cmdline}" == *"${token}"* ]]; then
            printf '[FAIL] the token is in the command line of pid %s\n' "${pid}" >&2
            found=1
        fi
    done
    if [[ "${scanned}" -eq 0 ]]; then
        printf '[FAIL] no command line could be read at all, so this probe proved nothing.\n' >&2
        return 1
    fi
    [[ "${found}" -eq 0 ]] || return 1
    printf 'scanned %d command line(s), no match (%d exited mid-scan)\n' "${scanned}" "${vanished}"
}

# ── I3: not inheritable by the agent's shell ──────────────────────────────────────────────
#
# This script IS run from the agent's Bash tool, so its own environment is the exact thing the
# invariant is about — no proxy, no simulation. Only variable NAMES are ever reported.
probe_I3() {
    local token entry leaked=() count=0
    token="$(read_token)"
    printf '== I3: the token is in no variable this shell inherited\n'
    while IFS= read -r -d '' entry; do
        count=$((count + 1))
        if [[ "${entry#*=}" == *"${token}"* ]]; then
            leaked+=("${entry%%=*}")
        fi
    done </proc/self/environ
    if [[ "${count}" -eq 0 ]]; then
        printf '[FAIL] this shell reports an empty environment, so the probe proved nothing.\n' >&2
        return 1
    fi
    if [[ "${#leaked[@]}" -gt 0 ]]; then
        printf '[FAIL] the token is carried by these inherited variables: %s\n' "${leaked[*]}" >&2
        return 1
    fi
    printf 'checked %d inherited variable(s), the value is in none of them\n' "${count}"
}

# ── I4: never on a stream ─────────────────────────────────────────────────────────────────
probe_I4() {
    local token out
    token="$(read_token)"
    printf '== I4: the wrapper prints the token on neither stdout nor stderr\n'
    require_wrapper
    # --version reaches claude without an API call, so this exercises the real success path.
    out="$("${WRAPPER_NAME}" --version 2>&1)"
    if [[ "${out}" == *"${token}"* ]]; then
        printf '[FAIL] the token appeared in the wrapper success-path output\n' >&2
        return 1
    fi
    # The failure path matters more: error messages are where secrets get pasted.
    if out="$(CCY_CLAUDE_DEPTH=99 "${WRAPPER_NAME}" --version 2>&1)"; then
        printf '[FAIL] the depth guard did not refuse at depth 99\n' >&2
        return 1
    fi
    if [[ "${out}" == *"${token}"* ]]; then
        printf '[FAIL] the token appeared in the wrapper refusal output\n' >&2
        return 1
    fi
    printf 'success path and refusal path are both clean\n'
}

# ── I5: equal authority, never greater ────────────────────────────────────────────────────
#
# Proved by running the wrapper against a STUB `claude` that reports its own argv, rather than
# by reading the wrapper and believing it. The stub also reports whether the credential
# arrived, WITHOUT printing it.
probe_I5() {
    local out expected
    printf '== I5: arguments reach the child verbatim, and nothing is added\n'
    require_wrapper
    STUB_DIR="$(mktemp -d)"
    cat >"${STUB_DIR}/claude" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf 'ARGV:'
printf ' [%s]' "$@"
printf '\n'
if [[ -n "${CLAUDE_CODE_OAUTH_TOKEN:-}" ]]; then
    printf 'CREDENTIAL: present (%d chars)\n' "${#CLAUDE_CODE_OAUTH_TOKEN}"
else
    printf 'CREDENTIAL: absent\n'
fi
STUB
    chmod 755 "${STUB_DIR}/claude"
    out="$(PATH="${STUB_DIR}:${PATH}" "${WRAPPER_NAME}" -p 'hello world' --model haiku)"
    printf '%s\n' "${out}"
    expected='ARGV: [-p] [hello world] [--model] [haiku]'
    if [[ "${out}" != *"${expected}"* ]]; then
        printf '[FAIL] arguments were not passed through verbatim; expected: %s\n' "${expected}" >&2
        return 1
    fi
    if [[ "${out}" == *dangerously-skip-permissions* ]]; then
        printf '[FAIL] the wrapper injected a permission flag the caller did not pass\n' >&2
        return 1
    fi
    if [[ "${out}" != *'CREDENTIAL: present'* ]]; then
        printf '[FAIL] the child did not receive the credential\n' >&2
        return 1
    fi
    printf 'argv verbatim, nothing injected, credential delivered\n'
}

# ── I6: off means off, including after an enabled session ─────────────────────────────────
probe_I6() {
    local failed=0
    printf '== I6: with the mode off, neither artefact is present\n'
    if command -v "${WRAPPER_NAME}" >/dev/null; then
        printf '[FAIL] %s is on PATH at %s but the mode is not enabled\n' \
            "${WRAPPER_NAME}" "$(command -v "${WRAPPER_NAME}")" >&2
        failed=1
    fi
    if [[ -e "${SKILL_DIR}" ]]; then
        printf '[FAIL] %s still exists.\n' "${SKILL_DIR}" >&2
        printf '       That path is host-persisted (/root/.claude -> /workspace/.claude/ccy), so\n' >&2
        printf '       an earlier enabled session left it behind and the mode cannot be turned off.\n' >&2
        failed=1
    fi
    [[ "${failed}" -eq 0 ]] || return 1
    printf 'no wrapper on PATH, no child-claude skill on disk\n'
}

# ── I7: bounded depth ─────────────────────────────────────────────────────────────────────
probe_I7() {
    local out max="${CCY_CHILD_CLAUDE_MAX_DEPTH:-1}"
    printf '== I7: spawn depth is bounded (max %s)\n' "${max}"
    require_wrapper
    if out="$(CCY_CLAUDE_DEPTH="${max}" "${WRAPPER_NAME}" --version 2>&1)"; then
        printf '[FAIL] the wrapper ran at depth %s, which is already at the limit\n' "${max}" >&2
        return 1
    fi
    if [[ "${out}" != *[Dd]epth* ]]; then
        printf '[FAIL] it refused, but the message does not say depth was the reason:\n%s\n' \
            "${out}" >&2
        return 1
    fi
    printf 'refused at the limit, and said why\n'
}

case "${INVARIANT}" in
    I1) probe_I1 ;;
    I2) probe_I2 ;;
    I3) probe_I3 ;;
    I4) probe_I4 ;;
    I5) probe_I5 ;;
    I6) probe_I6 ;;
    I7) probe_I7 ;;
    *)
        printf '[FATAL] unknown invariant %s (expected I1..I7)\n' "${INVARIANT}" >&2
        exit 1
        ;;
esac
