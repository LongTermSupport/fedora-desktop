#!/usr/bin/env bash
# Plan 00211 — deploy.bash --stage 1|2 (SPEC §5.3). HOST ONLY (R2).
#
# STAGE 1, the member side. Its legs, each failing fast:
#   1. play-agent-bus.yml, the WHOLE play: it rebuilds /usr/local/lib/agent-bus/pingbus.pyz
#      (the host's pingbus) and copies the member kit into every running LXC member, which only
#      its "Join the LXC members" task does. The admin code of that release made no room.
#   2. play-claude-yolo.yml, the whole play: it stages the new pingbus zipapp and the pingbus
#      skill into /opt/claude-yolo and rebuilds the ccy image.
#   3. acceptance.bash --stage 1 (S1, S2). Skipped under --check, which says so.
# Then the owner restarts the teams' sessions: a running session keeps the pingbus it started
# with until it is restarted.
#
# STAGE 2, the admin side, once deploy 1 is accepted and the sessions are restarted:
#   1. pre-flight: the newest spike report holds the PASS verdict, and acceptance.bash --stage 1
#      passes again (the host's and every declared LXC member's pingbus are 0.2.0, and no
#      declared container is down: containers can stop between deploys);
#   2. play-agent-bus.yml, the whole play: the agent-bus admin tool, whose `bootstrap` creates
#      each team's reports room, and its questions room once the team has a coordinator, writes
#      the index, and invites the humans and every coordinator; and the LXC members again;
#   3. play-claude-yolo.yml, the whole play: the ccy image carries the new admin tool;
#   4. the questions joins: up to 180 s for every coordinator to have JOINED its team's
#      questions room, read from `agent-bus list`'s ROOM lines. Only a coordinator running
#      pingbus 0.2.0 joins; one that has not is named with its membership;
#   5. acceptance.bash --stage 2 (A1-A4, then the test question for the owner).
# Under --check the plays rehearse, and legs 4 and 5 are skipped, saying so.
#
# meta-deploy.list queues deploy-stage-1.bash and deploy-stage-2.bash, never the plan folder:
# a folder entry would run this script and acceptance.bash with no --stage, which both refuse.
#
# Usage: ./deploy.bash --stage 1|2 [--check] [-h|--help]
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

PLAN_USAGE="usage: deploy.bash --stage 1|2 [--check] [-h|--help]

Plan 00211's deploys, on the HOST:
  --stage 1  the member side: play-agent-bus.yml (host pingbus 0.2.0, LXC member kits),
             play-claude-yolo.yml (the staged pingbus and skill; the ccy image), then
             acceptance.bash --stage 1. Then restart the teams' sessions.
  --stage 2  the admin side: the passed-spike and stage-1 pre-flight, play-agent-bus.yml (the
             rooms), play-claude-yolo.yml (the image's admin tool), the wait for every
             coordinator to join its questions room, then acceptance.bash --stage 2.
--check rehearses the plays and skips the waits and the acceptance."

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
if [[ "${STAGE}" != "1" ]] && [[ "${STAGE}" != "2" ]]; then
    printf '[FATAL] --stage 1 or --stage 2 is required\n%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

# Deploy 2's first pre-flight: the Phase 0 spike passed (spike.bash's newest report ends with
# the PASS verdict). meta-deploy ran deploy 1 whatever the spike found, so this is the gate.
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

# What the questions-join wait reads; the include's read_teams fills TEAMS, and its
# questions_joined is the wait.
AGENT_BUS=(sudo -n agent-bus)
TEAMS_JSON=""
REPORT=""
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-stage2.inc.bash
source "${PLAN_SCRIPT_DIR}/_acceptance-stage2.inc.bash"

if [[ "${STAGE}" == "2" ]]; then
    spike_passed
fi

plan_require_host "it runs Ansible to install pingbus and the admin tool on the host, in the LXC members and in the ccy image"
plan_prime_sudo
plan_start_log auto

if [[ "${STAGE}" == "2" ]]; then
    plan_deploy_leg "pre-flight: acceptance.bash --stage 1 (every pingbus 0.2.0, no declared container down)" \
        "${PLAN_SCRIPT_DIR}/acceptance.bash" --stage 1
fi
plan_deploy_leg "play-agent-bus.yml (the whole play)" \
    plan_ansible_playbook playbooks/imports/play-agent-bus.yml
plan_deploy_leg "play-claude-yolo.yml (the ccy image)" \
    plan_ansible_playbook playbooks/imports/play-claude-yolo.yml

if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '\n==> the questions-join wait and acceptance.bash --stage %s SKIPPED: --check deployed nothing to judge\n' "${STAGE}"
    plan_finish
fi

if [[ "${STAGE}" == "1" ]]; then
    plan_deploy_leg "acceptance.bash --stage 1" "${PLAN_SCRIPT_DIR}/acceptance.bash" --stage 1
    printf '\n==> NEXT (the owner): restart every team'\''s agent sessions, ccy and LXC alike. A running\n'
    printf '    session keeps the pingbus it started with; deploy 2 waits for each coordinator to join\n'
    printf '    the questions room, which only the new pingbus does.\n\n'
    plan_finish
fi

REPORT="${PLAN_RUN_DIR}/deploy-report.md"
TEAMS_JSON="${PLAN_RUN_DIR}/teams.json"
printf '# Plan 00211 deploy 2\n\n' >"${REPORT}"
plan_deploy_leg "read agent_bus_teams" read_teams
plan_deploy_leg "every coordinator joins its questions room (up to ${JOIN_WAIT_S} s)" questions_joined
plan_deploy_leg "acceptance.bash --stage 2" "${PLAN_SCRIPT_DIR}/acceptance.bash" --stage 2

printf '\n==> NEXT (the owner): the Element steps in ACCEPTANCE-owner.md, then the after-owner\n'
printf '    acceptance (A5), which an agent queues in meta-deploy.list once you say they are done.\n\n'
plan_finish
