#!/usr/bin/env bash
# Plan 00228 — deploy.bash
#
# PURPOSE: deploy Task 3.1's VM lab change, so acceptance.bash can run the consumer install
# scenario. HOST ONLY (CLAUDE/PlanScriptStandards.md R2) — Ansible never runs in the CCY
# container.
#
# EFFECT ON THE HOST: play-vm-test-lab.yml rebuilds the `vmtest` CLI from the checkout (its
# attended-run driver and `audience: consumer`), re-renders the scenario manifest and the
# bridge allowlist (now listing desktop-consumer-install), and deploys the new guest checker
# guest-acceptance-desktop-consumer-install.bash beside the others. The lab's packages,
# bases and runs are untouched: no base is rebuilt here (acceptance.bash ensures them).
#
# Usage: ./deploy.bash [-h|--help] [--check]
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

PLAN_USAGE="usage: deploy.bash [-h|--help] [--check]

Plan 00228 Task 3.2, the deploy half: runs play-vm-test-lab.yml on the HOST, which rebuilds
the vmtest CLI and deploys the desktop-consumer-install scenario and its guest checker.
  --check   the play as a dry run
Prompts for sudo once, before the run log opens."

plan_mode deploy
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -ne 0 ]]; then
    printf '[FATAL] unknown argument: %s\n%s\n' "${PLAN_REMAINING_ARGS[0]}" "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it deploys the vmtest lab's CLI, manifest and guest scripts on the host"
plan_prime_sudo
plan_start_log auto

plan_deploy_leg "play-vm-test-lab.yml (the vmtest CLI, the manifest and the consumer checker)" \
    plan_ansible_playbook playbooks/imports/optional/common/play-vm-test-lab.yml
# STANDARD-EXCEPTION(R9): no acceptance.bash leg here, where R9 puts it. meta-deploy.bash
# runs acceptance.bash after this script, so a leg here would run its three VM scenarios twice.
plan_finish
