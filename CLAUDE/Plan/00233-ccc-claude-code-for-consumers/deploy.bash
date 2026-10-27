#!/usr/bin/env bash
# Plan 00233 — deploy.bash
#
# PURPOSE: deploy ccc. HOST ONLY (CLAUDE/PlanScriptStandards.md R2) — Ansible never runs in
# the CCY container.
#
# EFFECT ON THE HOST: play-claude-code-consumer.yml runs play-golang.yml and play-podman.yml
# (now for every audience; podman-compose and the Docker-API socket stay developer-only),
# stages and builds claude-yolo:latest from tasks/claude-yolo-image.yml (a cache hit where ccy
# already built it), builds ccc into ~/.local/bin with the launcher's
# REQUIRED_CONTAINER_VERSION, makes the token pool and ~/.local/share/ccc/claude private, and
# deploys the "Claude" app grid entry. play-claude-yolo.yml runs too, because its image tasks
# moved into the shared task file. No ~/CCC is made here: ccc makes it on first use.
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

Plan 00233, the deploy half: runs play-claude-yolo.yml (its image tasks moved into a shared
task file) and play-claude-code-consumer.yml (ccc, its image, its directories and the
Claude app grid entry) on the HOST.
  --check   the plays as a dry run
Prompts for sudo once, before the run log opens."

plan_mode deploy
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -ne 0 ]]; then
    printf '[FATAL] unknown argument: %s\n%s\n' "${PLAN_REMAINING_ARGS[0]}" "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it builds the ccy image and installs ccc on the host"
plan_prime_sudo
plan_start_log auto

plan_deploy_leg "play-claude-yolo.yml (ccy, its image now staged by tasks/claude-yolo-image.yml)" \
    plan_ansible_playbook playbooks/imports/play-claude-yolo.yml
plan_deploy_leg "play-claude-code-consumer.yml (podman, the image, ccc and its app grid entry)" \
    plan_ansible_playbook playbooks/imports/play-claude-code-consumer.yml
# STANDARD-EXCEPTION(R9): no acceptance.bash leg here, where R9 puts it. meta-deploy.bash
# runs acceptance.bash after this script.
plan_finish
