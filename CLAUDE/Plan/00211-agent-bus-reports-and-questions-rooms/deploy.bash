#!/usr/bin/env bash
# Plan 00211 — deploy.bash --stage 1|2 (SPEC §5.3). HOST ONLY (R2). Stage 2 is not written
# yet (Phase 2, Task 2.6); its pre-flight, the passed-spike check, is.
#
# STAGE 1, the member side. Its legs, each failing fast:
#   1. play-agent-bus.yml, the WHOLE play: it rebuilds /usr/local/lib/agent-bus/pingbus.pyz
#      (the host's pingbus) and copies the member kit into every running LXC member, which only
#      its "Join the LXC members" task does. No room is created: the admin code that would
#      create one is Phase 2 and not on F44 yet.
#   2. play-claude-yolo.yml, the whole play: it stages the new pingbus zipapp and the pingbus
#      skill into /opt/claude-yolo and rebuilds the ccy image (container 2.78.7).
#   3. acceptance.bash --stage 1 (S1, S2). Skipped under --check, which says so.
# Then the owner restarts the teams' sessions: a running session keeps the pingbus it started
# with until it is restarted.
#
# meta-deploy.list queues deploy-stage-1.bash, never the plan folder: a folder entry would run
# this script and acceptance.bash with no --stage, which both refuse.
#
# Usage: ./deploy.bash --stage 1 [--check] [-h|--help]
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

PLAN_USAGE="usage: deploy.bash --stage 1 [--check] [-h|--help]

Plan 00211 deploy 1, the member side, on the HOST:
  playbooks/imports/play-agent-bus.yml     (the whole play: host pingbus 0.2.0, LXC member kits)
  playbooks/imports/play-claude-yolo.yml   (the staged pingbus and skill; ccy image 2.78.7)
  acceptance.bash --stage 1                (S1, S2)
--check rehearses the plays and skips the acceptance. Then restart the teams' sessions."

plan_mode deploy
plan_parse_common_flags "$@"

STAGE=""
args=("${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}")
i=0
while [[ "${i}" -lt "${#args[@]}" ]]; do
    case "${args[i]}" in
        --stage)
            i=$((i + 1))
            STAGE="${args[i]:-}"
            ;;
        --stage=*) STAGE="${args[i]#--stage=}" ;;
        *)
            printf '[FATAL] unknown argument: %s\n%s\n' "${args[i]}" "${PLAN_USAGE}" >&2
            exit 64
            ;;
    esac
    i=$((i + 1))
done
# Deploy 2's pre-flight: the Phase 0 spike passed (spike.bash's newest report ends with the
# PASS verdict). meta-deploy runs deploy 1 whatever the spike found, so this is the gate.
spike_passed() {
    local runs dirs report
    runs="${PLAN_REPO_ROOT}/untracked/plan-runs/$(basename "${PLAN_SCRIPT_DIR}")/spike"
    dirs=()
    if [[ -d "${runs}" ]]; then
        dirs=("${runs}"/*/)
    fi
    if [[ "${#dirs[@]}" -eq 0 ]] || [[ ! -d "${dirs[0]}" ]]; then
        printf '[FATAL] deploy 2 needs a passed spike: no spike.bash run under %s\n' "${runs}" >&2
        exit 1
    fi
    report="${dirs[${#dirs[@]} - 1]}spike-report.md"
    if [[ ! -f "${report}" ]] || ! grep -qxF 'Verdict: every fact holds' "${report}"; then
        printf '[FATAL] deploy 2 needs a passed spike: %s has no PASS verdict; amend the SPEC and re-run spike.bash\n' \
            "${report}" >&2
        exit 1
    fi
    printf '==> the spike passed: %s\n' "${report}"
}

case "${STAGE}" in
    1) ;;
    2)
        spike_passed
        printf '[FATAL] --stage 2 is not written yet (Plan 00211 Task 2.6)\n' >&2
        exit 64
        ;;
    *)
        printf '[FATAL] --stage 1 is required\n%s\n' "${PLAN_USAGE}" >&2
        exit 64
        ;;
esac

plan_require_host "it runs Ansible to install pingbus on the host, in the LXC members and in the ccy image"
plan_prime_sudo
plan_start_log auto

plan_deploy_leg "play-agent-bus.yml (the whole play)" \
    plan_ansible_playbook playbooks/imports/play-agent-bus.yml
plan_deploy_leg "play-claude-yolo.yml (the staged pingbus, the ccy image)" \
    plan_ansible_playbook playbooks/imports/play-claude-yolo.yml

if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '\n==> acceptance.bash --stage 1 SKIPPED: --check deployed nothing to judge\n'
else
    plan_deploy_leg "acceptance.bash --stage 1" "${PLAN_SCRIPT_DIR}/acceptance.bash" --stage 1
fi

printf '\n==> NEXT (the owner): restart every team'\''s agent sessions, ccy and LXC alike. A running\n'
printf '    session keeps the pingbus it started with; deploy 2 waits for each coordinator to join\n'
printf '    the questions room, which only the new pingbus does.\n\n'

plan_finish
