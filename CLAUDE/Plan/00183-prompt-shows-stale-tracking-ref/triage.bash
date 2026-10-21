#!/usr/bin/env bash
# Plan 00183 — triage.bash (Task 1.1): why bash-git-prompt's own fetch did not keep the
# tracking ref fresh (#52).
#
# PURPOSE: record, on the host, the facts the cause rests on:
#   - the installed bash-git-prompt (commit, its fetch code) and every GIT_PROMPT_ setting the
#     start-up files give it;
#   - git, find -mmin and perl (the fetch timer), and the deployed git-fetch-age;
#   - for each checkout: every FETCH_HEAD of its repository (size, date, age; an empty one is
#     a fetch that failed or is running, because git empties it before contacting the remote),
#     the tracking refs and their reflog, and git-fetch-age's mark;
#   - the SSH agent this run sees and its keys;
#   - whether the remote answers as the prompt's background fetch would ask it, with the agent
#     and with none, with no terminal to prompt on.
# Fact-finding only: it renders no verdict (PlanScriptStandards R9). The probes are in
# triage-probe.bash beside this script.
#
# RUN ON THE HOST, as the desktop user, through CLAUDE/Plan/meta-deploy.bash, or
#   ./CLAUDE/Plan/00183-prompt-shows-stale-tracking-ref/triage.bash [checkout...]
# With no checkout it looks at this repository's checkout. It never prompts and needs no sudo.
#
# EFFECT ON THE HOST: nothing. It never fetches: the remote is asked with git ls-remote, which
# writes no ref and no FETCH_HEAD, run with no terminal and no askpass window, so it cannot ask
# for a passphrase. Its only output is the run log and report under untracked/plan-runs/.
#
# EXIT CODES: 0 every leg established its facts; 1 at least one did not (the leg names
# itself); 64 usage.
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

PLAN_USAGE="usage: triage.bash [-h|--help] [checkout...]

Read-only, on the HOST: the installed bash-git-prompt and its fetch settings, the fetch timer's
tools and the deployed git-fetch-age, then for each checkout (default: this repository's) every
FETCH_HEAD with its size and age, the tracking refs, the SSH agent, and whether the remote
answers with the agent and with none. It never fetches. The report is written to the run
directory."

plan_mode gather
plan_parse_common_flags "$@"

checkouts=("${PLAN_REMAINING_ARGS[@]}")
if [[ "${#checkouts[@]}" -eq 0 ]]; then
    checkouts=("${PLAN_REPO_ROOT}")
fi
for dir in "${checkouts[@]}"; do
    if [[ ! -d "${dir}" ]]; then
        printf '[FATAL] not a directory: %s\n' "${dir}" >&2
        printf '%s\n' "${PLAN_USAGE}" >&2
        exit 64
    fi
done

plan_require_host "it reads the desktop user's installed prompt, SSH agent and checkouts"
plan_start_log auto

PROBE="${PLAN_SCRIPT_DIR}/triage-probe.bash"
REPORT="${PLAN_RUN_DIR}/triage-report.md"
printf '# Plan 00183 triage\n\n' >"${REPORT}"

plan_gather_leg "installed bash-git-prompt and its settings" bash "${PROBE}" prompt "${REPORT}"
plan_gather_leg "fetch timer tools and git-fetch-age" bash "${PROBE}" tools "${REPORT}"
plan_gather_leg "SSH agent" bash "${PROBE}" agent "${REPORT}"
for dir in "${checkouts[@]}"; do
    plan_gather_leg "FETCH_HEAD and tracking refs: ${dir}" bash "${PROBE}" fetchhead "${REPORT}" "${dir}"
    plan_gather_leg "remote reachable as the prompt's fetch: ${dir}" bash "${PROBE}" reach "${REPORT}" "${dir}"
done

printf '\n==> READ THIS FOR the cause: an empty (0 bytes) FETCH_HEAD in "FETCH_HEAD and tracking\n'
printf '    refs" is a fetch that failed; "remote reachable" then says whether it fails with no key\n'
printf '    in the agent (passphrase) or for every route (network, remote, credentials).\n'
plan_finish
