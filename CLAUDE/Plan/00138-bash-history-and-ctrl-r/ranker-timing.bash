#!/usr/bin/env bash
# Plan 00138 — ranker-timing.bash (Tasks 2.3, 3.5)
#
# PURPOSE: time files/home/.local/bin/bash-history-rank, and fzf's filtering of its output,
# on synthetic recorder files of 15k and 100k records. Its ORDERING is tested by
# scripts/test-bash-history-search.bash; this measures only how long it takes.
#
# The synthetic records reuse the COMMANDS from a copy of the host's history file, so the
# lengths and variety are real, with invented directories and exit statuses. Every file is
# written under the run directory in untracked/; nothing prints a command — only timings.
# Read-only towards the live history.
#
# RUN ON THE HOST:
#   ./CLAUDE/Plan/00138-bash-history-and-ctrl-r/ranker-timing.bash
#
# Usage: ./ranker-timing.bash [-h|--help]
#
# EXIT CODES:
#   0  every timing was taken
#   1  a timing could not be taken
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

PLAN_USAGE="usage: ranker-timing.bash [-h|--help]

Times bash-history-rank and fzf --filter on synthetic 15k and 100k record
files built from a copy of this host's history. Prints timings only.
Host-only, read-only."

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

readonly RANKER="${PLAN_REPO_ROOT}/files/home/.local/bin/bash-history-rank"
readonly WORK="${PLAN_RUN_DIR}/data"
# The history file in use: the new location once deployed, the old one before.
SOURCE_HISTORY="${HOME}/.local/state/bash/history"
if [[ ! -r "${SOURCE_HISTORY}" ]]; then
    SOURCE_HISTORY="${HOME}/.bash_history"
fi
readonly SOURCE_HISTORY
mkdir -m 0700 "${WORK}"

# A throwaway repository, so the timed directory has a repository tier to compute.
repo="${WORK}/repo"
mkdir -p "${repo}/sub" "${WORK}/elsewhere"
git -C "${repo}" init --quiet

history_copy="${WORK}/history-copy"
cp "${SOURCE_HISTORY}" "${history_copy}"
chmod 0600 "${history_copy}"
echo "history copy: $(wc -l <"${history_copy}") lines"

# 300 invented directories inside the repository and outside it; the timed directory
# receives about 2% of the records, like one project among many.
make_context() {
    local rows="$1" out="$2"
    gawk -v rows="${rows}" -v here="${repo}/sub" -v repo="${repo}" -v other="${WORK}/elsewhere" '
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
    "${RANKER}" "$1" "${history_copy}" "${repo}/sub" | fzf --read0 --tiebreak=index --filter "$2"
}

for rows in 15000 100000; do
    context="${WORK}/context-${rows}"
    make_context "${rows}" "${context}"
    start_clock
    "${RANKER}" "${context}" "${history_copy}" "${repo}/sub" >/dev/null
    stop_clock "rank ${rows} records + history copy"
    start_clock
    rank_to_fzf "${context}" "" >/dev/null
    stop_clock "rank ${rows} + fzf --filter '' (empty query)"
    start_clock
    rank_to_fzf "${context}" "git" >/dev/null
    stop_clock "rank ${rows} + fzf --filter 'git' (typed query)"
done
start_clock
git -C "${repo}/sub" rev-parse --show-toplevel >/dev/null
stop_clock "git rev-parse --show-toplevel alone"

plan_finish
