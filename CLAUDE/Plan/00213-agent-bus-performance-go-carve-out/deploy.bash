#!/usr/bin/env bash
# Plan 00213 — deploy.bash: Task 2.3, the bus status line's cut-over to Go, on the HOST.
#
# WHAT IT CHANGES:
#   1. playbooks/imports/play-agent-bus.yml, the whole play: the Go toolchain is checked, the
#      agent-bus software is installed as on every run, then pingbus-status-line is built
#      static from this checkout into the member kit (/usr/local/share/agent-bus/kit/), whose
#      settings.json now runs it, and every running LXC member's kit is copied again with it.
#      Teams are not restarted unless the Tuwunel pin or the units changed.
#   2. playbooks/imports/play-claude-yolo.yml, the whole play: CCY 3.99.13.9, and the ccy build
#      context gains pingbus-status-line; the image is rebuilt once (container 2.78.9) at the
#      next ccy launch.
#   3. acceptance.bash (C1-C5), skipped under --check.
#
# Needs sudo once, before the log opens. Afterwards the owner restarts the teams' agent
# sessions: a running session keeps the status line command it started with.
#
# EXIT CODES: 0 deployed and accepted; 1 a leg failed (named); 64 usage.
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

PLAN_USAGE="usage: deploy.bash [--check] [-h|--help]

Plan 00213 Task 2.3 on the HOST:
  playbooks/imports/play-agent-bus.yml     (the member kit's Go status line, LXC member kits)
  playbooks/imports/play-claude-yolo.yml   (the ccy build context's Go status line; image 2.78.9)
  acceptance.bash                          (C1-C5)
--check rehearses the plays and skips the acceptance. Then restart the teams' sessions."

plan_mode deploy
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument: %s\n%s\n' "${PLAN_REMAINING_ARGS[0]}" "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible to install the Go status line in the member kit, the LXC members and the ccy image"
plan_prime_sudo
plan_start_log auto

plan_deploy_leg "play-agent-bus.yml (the whole play)" \
    plan_ansible_playbook playbooks/imports/play-agent-bus.yml
plan_deploy_leg "play-claude-yolo.yml (the ccy build context)" \
    plan_ansible_playbook playbooks/imports/play-claude-yolo.yml

if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '\n==> acceptance.bash SKIPPED: --check deployed nothing to judge\n'
else
    plan_deploy_leg "acceptance.bash" "${PLAN_SCRIPT_DIR}/acceptance.bash"
fi

printf '\n==> NEXT (the owner): restart every team'\''s agent sessions, ccy, cc and LXC alike. A\n'
printf '    running session keeps the Python status line it started with.\n\n'

plan_finish
