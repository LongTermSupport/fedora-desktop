#!/usr/bin/env bash
# test-u01-plugin-probe-check.bash — table test for u01-plugin-probe-check.bash, the U01
# checker that turns one child-claude run's evidence into "which hooks fired" and the D13
# route (DESIGN.md section 6, "Install route, decided by probe U01").
#
# Each case plants a fake evidence directory (marker files, the child's exit code and
# stdout) under a scratch directory inside the repo, runs the checker and compares its
# stdout and exit code with the expected ones. Nothing outside the scratch directory is
# touched, and it is removed on exit.
#
# WHERE TO RUN: anywhere (pure file logic; no claude, no network).
#
# Usage: ./CLAUDE/Plan/00161-agent-team-bus-matrix/test-u01-plugin-probe-check.bash
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
CHECKER="${HERE}/u01-plugin-probe-check.bash"
repoRoot="${HERE}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/.git" ]]; do
    repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/.git" ]] || { printf '[FATAL] no repo root above %s\n' "${HERE}" >&2; exit 1; }
mkdir -p "${repoRoot}/untracked/scratch"
SCRATCH="$(mktemp -d -p "${repoRoot}/untracked/scratch" u01-check-test.XXXXXX)"

cleanup() {
    rm -rf "${SCRATCH}"
}
trap cleanup EXIT

PASSED=0
FAILED=0

# plant <name> <rc> <stdout text> <events fired...> — build one evidence directory.
plant() {
    local name="$1" rc="$2" out="$3"
    shift 3
    local dir="${SCRATCH}/${name}"
    mkdir -p "${dir}/marks"
    printf '%s\n' "${rc}" >"${dir}/child.rc"
    printf '%s' "${out}" >"${dir}/child.stdout"
    : >"${dir}/child.stderr"
    : >"${dir}/debug.log"
    local event
    for event in "$@"; do
        : >"${dir}/marks/${event}"
    done
    printf '%s\n' "${dir}"
}

# expect <case> <dir> <expected exit> <expected stdout> [extra checker args...]
expect() {
    local name="$1" dir="$2" wantRc="$3" want="$4"
    shift 4
    local got rc=0
    got="$("${CHECKER}" "${dir}" "$@" 2>"${SCRATCH}/stderr")" || rc=$?
    if [[ "${rc}" -eq "${wantRc}" ]] && [[ "${got}" == "${want}" ]]; then
        printf 'PASS %s\n' "${name}"
        PASSED=$((PASSED + 1))
    else
        printf 'FAIL %s: exit %s (want %s)\n--- got\n%s\n--- want\n%s\n' \
            "${name}" "${rc}" "${wantRc}" "${got}" "${want}"
        FAILED=$((FAILED + 1))
    fi
}

ALL_FIRED="hook SessionStart fired
hook UserPromptSubmit fired
hook Stop fired"
NONE_FIRED="hook SessionStart not-fired
hook UserPromptSubmit not-fired
hook Stop not-fired"

d="$(plant all-fired 0 OK SessionStart UserPromptSubmit Stop)"
expect "all three fire in a completed turn -> plugin" "${d}" 0 "${ALL_FIRED}
child rc=0 turn=complete
plugin-loaded yes
route plugin reason=all-hooks-fired"

d="$(plant all-fired-turn-failed 1 "" SessionStart UserPromptSubmit Stop)"
expect "all three fire even though the turn failed -> plugin" "${d}" 0 "${ALL_FIRED}
child rc=1 turn=incomplete
plugin-loaded yes
route plugin reason=all-hooks-fired"

d="$(plant none-fired-complete 0 OK)"
expect "none fire in a completed turn -> fallback" "${d}" 0 "${NONE_FIRED}
child rc=0 turn=complete
plugin-loaded no
route fallback reason=no-hook-fired-in-a-completed-turn"

d="$(plant none-fired-no-auth 1 "")"
expect "none fire and the turn failed -> undetermined" "${d}" 3 "${NONE_FIRED}
child rc=1 turn=incomplete
plugin-loaded unknown
route undetermined reason=child-turn-did-not-complete"

d="$(plant empty-stdout 0 "")"
expect "exit 0 with no reply is not a completed turn" "${d}" 3 "${NONE_FIRED}
child rc=0 turn=incomplete
plugin-loaded unknown
route undetermined reason=child-turn-did-not-complete"

d="$(plant partial-complete 0 OK SessionStart Stop)"
expect "some fire in a completed turn -> fallback" "${d}" 0 "hook SessionStart fired
hook UserPromptSubmit not-fired
hook Stop fired
child rc=0 turn=complete
plugin-loaded yes
route fallback reason=some-hooks-did-not-fire-in-a-completed-turn"

d="$(plant partial-no-auth 1 "" SessionStart)"
expect "SessionStart only, turn failed -> loaded but undetermined" "${d}" 3 "hook SessionStart fired
hook UserPromptSubmit not-fired
hook Stop not-fired
child rc=1 turn=incomplete
plugin-loaded yes
route undetermined reason=child-turn-did-not-complete"

# A control run (the same hooks as user-level settings) firing in the same failed turn
# shows hooks DO run without a reply, so the plugin's silence is evidence after all.
control="$(plant control-all 1 "" SessionStart UserPromptSubmit Stop)"
controlNone="$(plant control-none 1 "")"

d="$(plant none-fired-control-fired 1 "")"
expect "none fire, turn failed, control fired -> fallback" "${d}" 0 "${NONE_FIRED}
child rc=1 turn=incomplete
control hooks=3/3
plugin-loaded no
route fallback reason=no-hook-fired-while-control-fired" "${control}"

d="$(plant partial-control-fired 1 "" SessionStart)"
expect "some fire, turn failed, control fired -> fallback" "${d}" 0 "hook SessionStart fired
hook UserPromptSubmit not-fired
hook Stop not-fired
child rc=1 turn=incomplete
control hooks=3/3
plugin-loaded yes
route fallback reason=some-hooks-did-not-fire-while-control-fired" "${control}"

d="$(plant none-fired-control-silent 1 "")"
expect "none fire, control silent too -> undetermined" "${d}" 3 "${NONE_FIRED}
child rc=1 turn=incomplete
control hooks=0/3
plugin-loaded unknown
route undetermined reason=child-turn-did-not-complete" "${controlNone}"

# What an unauthenticated child really does: SessionStart and UserPromptSubmit are
# dispatched, Stop is not (the turn errors first). An event that fired in the control but
# not in the plugin run is a definite miss; an event neither fired proves nothing.
controlNoStop="$(plant control-no-stop 1 "" SessionStart UserPromptSubmit)"

d="$(plant none-fired-control-partial 1 "")"
expect "none fire, control fired SessionStart+UserPromptSubmit -> fallback" "${d}" 0 "${NONE_FIRED}
child rc=1 turn=incomplete
control hooks=2/3
plugin-loaded no
route fallback reason=no-hook-fired-while-control-fired" "${controlNoStop}"

d="$(plant same-as-control 1 "" SessionStart UserPromptSubmit)"
expect "plugin matches a partial control -> loaded, Stop unproven" "${d}" 3 "hook SessionStart fired
hook UserPromptSubmit fired
hook Stop not-fired
child rc=1 turn=incomplete
control hooks=2/3
plugin-loaded yes
route undetermined reason=child-turn-did-not-complete" "${controlNoStop}"

d="$(plant less-than-control 1 "" SessionStart)"
expect "plugin fired less than a partial control -> fallback" "${d}" 0 "hook SessionStart fired
hook UserPromptSubmit not-fired
hook Stop not-fired
child rc=1 turn=incomplete
control hooks=2/3
plugin-loaded yes
route fallback reason=some-hooks-did-not-fire-while-control-fired" "${controlNoStop}"

expect "all fired with a control -> plugin" "${control}" 0 "${ALL_FIRED}
child rc=1 turn=incomplete
control hooks=0/3
plugin-loaded yes
route plugin reason=all-hooks-fired" "${controlNone}"

expect "--hooks-only prints the evidence and no verdict" "${control}" 0 "${ALL_FIRED}
child rc=1 turn=incomplete" --hooks-only

expect "missing control directory -> 66" "${control}" 66 "" "${SCRATCH}/no-such-control"

expect "missing evidence directory -> 66" "${SCRATCH}/does-not-exist" 66 ""

d="${SCRATCH}/no-rc"
mkdir -p "${d}/marks"
expect "evidence without child.rc -> 66" "${d}" 66 ""

usageRc=0
"${CHECKER}" >/dev/null 2>"${SCRATCH}/usage.stderr" || usageRc=$?
if [[ "${usageRc}" -eq 64 ]]; then
    printf 'PASS no argument -> 64\n'
    PASSED=$((PASSED + 1))
else
    printf 'FAIL no argument: exit %s (want 64)\n' "${usageRc}"
    FAILED=$((FAILED + 1))
fi

printf '%d passed, %d failed\n' "${PASSED}" "${FAILED}"
[[ "${FAILED}" -eq 0 ]]
