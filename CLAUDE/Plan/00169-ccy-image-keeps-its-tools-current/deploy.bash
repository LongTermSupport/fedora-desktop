#!/usr/bin/env bash
# Plan 00169 — deploy.bash
#
# PURPOSE: Phase 1, a ccy session whose container git cannot read /workspace stops with
# git's own message. HOST ONLY (CLAUDE/PlanScriptStandards.md R2): Ansible never runs in the
# CCY container.
#
# THE LEGS:
#   1. play-claude-yolo.yml — installs the CCY 3.91.0 launcher and the container 2.50
#      entrypoint (the git preflight), and builds claude-yolo:latest.
#   2. acceptance.bash — launches ccy headless in two throwaway repositories: one the image's
#      git cannot read (must stop, printing git's message) and a clean one (must start).
#      Skipped under --check, since nothing was deployed to test.
#
# Usage: ./deploy.bash [-h|--help] [--check] [--token NAME]
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

PLAN_USAGE="usage: deploy.bash [-h|--help] [--check] [--token NAME]

Runs, on the HOST:

  playbooks/imports/play-claude-yolo.yml   (launcher 3.91.0, container 2.50)
  acceptance.bash                          (skipped under --check)

--check previews without changing anything. --token NAME is passed to acceptance.bash."

plan_mode deploy
plan_parse_common_flags "$@"

acceptanceArgs=()
expectToken=0
for arg in "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"; do
    if [[ "${expectToken}" -eq 1 ]]; then
        acceptanceArgs+=(--token "${arg}")
        expectToken=0
        continue
    fi
    case "${arg}" in
        --token) expectToken=1 ;;
        --token=*) acceptanceArgs+=("${arg}") ;;
        *)
            printf '[FATAL] unknown argument: %s\n' "${arg}" >&2
            printf '%s\n' "${PLAN_USAGE}" >&2
            exit 64
            ;;
    esac
done
if [[ "${expectToken}" -eq 1 ]]; then
    printf '[FATAL] --token needs a NAME\n%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible to install the ccy launcher and build its image"
plan_prime_sudo
plan_start_log auto

plan_deploy_leg "play-claude-yolo.yml" \
    plan_ansible_playbook playbooks/imports/play-claude-yolo.yml

if [[ "${PLAN_CHECK:-0}" == "1" ]]; then
    printf '\n==> --check: acceptance.bash skipped, nothing was deployed to test\n'
else
    plan_deploy_leg "acceptance.bash" \
        "${PLAN_SCRIPT_DIR}/acceptance.bash" "${acceptanceArgs[@]+"${acceptanceArgs[@]}"}"
fi

plan_finish
