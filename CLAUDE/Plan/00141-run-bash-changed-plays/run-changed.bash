#!/usr/bin/env bash
# Plan 00141 — run-changed.bash
#
# PURPOSE: Task 3.2, the real run of `./run.bash --changed` on the HOST, carried by
# meta-deploy. HOST ONLY (CLAUDE/PlanScriptStandards.md R2): it runs Ansible, which never
# runs in the CCY container.
#
# run.bash lists the plays that changed since they last ran here and asks before running
# them; that question reaches the person at meta-deploy's terminal, so nothing runs
# unasked. Answering no is a valid outcome: the list it printed is the evidence Task 3.2
# wants ("read what it ran").
#
# Usage: ./run-changed.bash [-h|--help]
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

PLAN_USAGE="usage: run-changed.bash [-h|--help]

Runs ./run.bash --changed on the HOST: it lists the plays that changed since they last
ran here and asks before running them."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs run.bash, which runs Ansible"
plan_start_log auto

plan_deploy_leg "run.bash --changed" "${repoRoot}/run.bash" --changed

plan_finish
