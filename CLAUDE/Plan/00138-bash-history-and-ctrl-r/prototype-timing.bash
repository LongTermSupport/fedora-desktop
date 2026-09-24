#!/usr/bin/env bash
# Plan 00138 — prototype-timing.bash (Task 2.3)
#
# PURPOSE: prove the prototype ranker orders a fixture correctly, then time it (and fzf's
# filtering of its output) on synthetic context files of 15k and 100k records.
#
# The synthetic records reuse the COMMANDS from a copy of the host's history file, so the
# lengths and variety are real, with invented directories and exit statuses. Every file is
# written under the run directory in untracked/; nothing prints a command — only timings
# and pass/fail lines. Read-only towards the live history.
#
# RUN ON THE HOST:
#   ./CLAUDE/Plan/00138-bash-history-and-ctrl-r/prototype-timing.bash
#
# Usage: ./prototype-timing.bash [-h|--help]
#
# EXIT CODES:
#   0  the fixture ranked correctly and every timing was taken
#   1  a fixture check failed or a timing could not be taken
#  64  usage error
set -euo pipefail

# ── R1 bootstrap: script-relative, filesystem-only, bounded at the repo boundary ──────────
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
    if [[ -e "${repoRoot}/.git" ]]; then
        printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
        exit 1
    fi
    repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || {
    printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2
    exit 1
}
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="usage: prototype-timing.bash [-h|--help]

Checks the prototype ranker's ordering on a fixture, then times it and
fzf --filter on synthetic 15k and 100k record files built from a copy of
this host's history. Prints timings only. Host-only, read-only."

plan_mode gather
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi
plan_require_host "it copies this host's history file to build realistic test data"

for tool in gawk fzf git sort cut; do
    if ! command -v "${tool}" >/dev/null; then
        printf '[FATAL] %s is not on PATH\n' "${tool}" >&2
        exit 1
    fi
done

plan_start_log auto

readonly RANKER="${PLAN_SCRIPT_DIR}/prototype-ranker.bash"
readonly WORK="${PLAN_RUN_DIR}/data"
readonly SOURCE_HISTORY="${HOME}/.bash_history"
mkdir -m 0700 "${WORK}"
FAILURES=0

check() {
    local label="$1" expected="$2" actual="$3"
    if [[ "${expected}" == "${actual}" ]]; then
        printf 'PASS  %s\n' "${label}"
    else
        printf 'FAIL  %s\n      expected: %s\n      actual:   %s\n' "${label}" "${expected}" "${actual}"
        FAILURES=$((FAILURES + 1))
    fi
}

# ── Fixture: a throwaway git repo, a context file and a history file with known answers ──
echo "== fixture ordering"
fixture_repo="${WORK}/fixture-repo"
mkdir -p "${fixture_repo}/sub" "${WORK}/elsewhere"
git -C "${fixture_repo}" init --quiet
fixture_context="${WORK}/fixture-context"
fixture_history="${WORK}/fixture-history"
{
    printf '1\t0\t%s\t%s\0' "${WORK}/elsewhere" "cmd-elsewhere-new"
    printf '2\t0\t%s\t%s\0' "${fixture_repo}" "cmd-repo-root"
    printf '3\t0\t%s\t%s\0' "${fixture_repo}/sub" "cmd-here-old"
    printf '4\t1\t%s\t%s\0' "${fixture_repo}/sub" "cmd-here-always-fails"
    printf '5\t0\t%s\t%s\0' "${fixture_repo}/sub" "cmd-here-new"
    printf '6\t0\t%s\t%s\0' "${WORK}/elsewhere" "cmd-also-here"
    printf '7\t0\t%s\t%s\0' "${fixture_repo}/sub" "cmd-also-here"
    printf '8\t0\t%s\t%s\0' "${WORK}/elsewhere" "cmd-elsewhere-newest"
    printf '9\t0\t%s\t%s\0' "${WORK}/elsewhere" "cmd	with	tabs"
} >"${fixture_context}"
printf '%s\n' "cmd-history-only" "#1700000000" "cmd-multi-line" "second line" "#1700000001" "cmd-here-old" >"${fixture_history}"

mapfile -d '' ranked < <(bash "${RANKER}" "${fixture_context}" "${fixture_history}" "${fixture_repo}/sub")
check "tier 2 (this directory) comes first, newest first" \
    "cmd-also-here|cmd-here-new|cmd-here-old" "${ranked[0]}|${ranked[1]}|${ranked[2]}"
check "an always-failing command sinks to the bottom of its tier" "cmd-here-always-fails" "${ranked[3]}"
check "tier 1 (same repo) follows" "cmd-repo-root" "${ranked[4]}"
check "tier 0 is newest first and keeps tabs" "cmd	with	tabs|cmd-elsewhere-newest" "${ranked[5]}|${ranked[6]}"
check "history-only and multi-line entries are searchable" "3" \
    "$(printf '%s\n' "${ranked[@]}" | grep -c -E '^(cmd-history-only|cmd-multi-line|second line)$')"
check "each command appears once" "${#ranked[@]}" "$(printf '%s\0' "${ranked[@]}" | sort -zu | tr -cd '\0' | wc -c)"

# ── Timing on synthetic data built from a copy of the real history ─────────────────────────
echo "== timing"
history_copy="${WORK}/history-copy"
cp "${SOURCE_HISTORY}" "${history_copy}"
chmod 0600 "${history_copy}"
echo "history copy: $(wc -l <"${history_copy}") lines"

# 300 invented directories inside the fixture repo and outside it; the timed directory
# receives about 2% of the records, like one project among many.
make_context() {
    local rows="$1" out="$2"
    gawk -v rows="${rows}" -v here="${fixture_repo}/sub" -v repo="${fixture_repo}" -v other="${WORK}/elsewhere" '
        BEGIN { srand(138) }
        { cmds[++n] = $0 }
        END {
            for (i = 1; i <= rows; i++) {
                r = rand()
                dir = (r < 0.02) ? here : ((r < 0.10) ? repo "/d" int(rand() * 20) : other "/d" int(rand() * 280))
                printf "%d\t%d\t%s\t%s%c", 1700000000 + i, (rand() < 0.9 ? 0 : 1), dir, cmds[int(rand() * n) + 1], 0
            }
        }' "${history_copy}" >"${out}"
    chmod 0600 "${out}"
}

START_NS=0
start_clock() { START_NS=$(date +%s%N); }
stop_clock() { printf '%-58s %6d ms\n' "$1" $((($(date +%s%N) - START_NS) / 1000000)); }

rank_to_fzf() {
    bash "${RANKER}" "$1" "${history_copy}" "${fixture_repo}/sub" | fzf --read0 --print0 --tiebreak=index --filter "$2"
}

for rows in 15000 100000; do
    context="${WORK}/context-${rows}"
    make_context "${rows}" "${context}"
    start_clock
    bash "${RANKER}" "${context}" "${history_copy}" "${fixture_repo}/sub" >/dev/null
    stop_clock "rank ${rows} records + history copy"
    start_clock
    rank_to_fzf "${context}" "" >/dev/null
    stop_clock "rank ${rows} + fzf --filter '' (empty query)"
    start_clock
    rank_to_fzf "${context}" "git" >/dev/null
    stop_clock "rank ${rows} + fzf --filter 'git' (typed query)"
done
start_clock
git -C "${fixture_repo}/sub" rev-parse --show-toplevel >/dev/null
stop_clock "git rev-parse --show-toplevel alone"

if [[ "${FAILURES}" -ne 0 ]]; then
    printf 'VERDICT: %d fixture check(s) FAILED\n' "${FAILURES}"
    PLAN_FAILED_LEGS="fixture ordering"
else
    echo "VERDICT: fixture ordering correct; timings above"
fi
plan_finish
