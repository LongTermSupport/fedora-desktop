#!/usr/bin/env bash
# acceptance.bash — Plan 00203's server-side robustness fixes (gaps 3, 4, 9, 10), proven on the
# host on a throwaway team. ACCEPTANCE.md lists each check with the gap it proves.
#
# WHERE TO RUN: on the HOST, as the desktop user (sudo for each root step), after
# play-agent-bus.yml (the units, the installer, the admin zipapp) and play-claude-yolo.yml
# (ccy-teams) have run. Through meta-deploy.bash.
#
# WHAT IT CHANGES, and removes on the way out however the run ends (and first, if an
# interrupted run left it): the team `acc-robust` (loopback only, no member; built with
# `agent-bus-install team`, removed with `remove --purge`, which takes its health record too)
# and a runtime drop-in that delays its homeserver's start (under /run, gone at reboot
# anyway). Every wait is bounded; the longest is 150 s.
#
# CHECKS
#   R1 gap 4   the homeserver is Type=notify with a 30 s watchdog, and stays up past two
#              watchdog periods with no restart (READY=1 and the pings reach systemd)
#   R2 gap 10  the health check runs and agent-bus health status reports a new team healthy
#   R3 gap 4   SIGKILL: the homeserver is back and answering within 60 s
#   R4 gap 4   a clean exit (SIGTERM to Tuwunel, exit 0, no systemctl stop): back within 90 s
#   R5 gap 4   SIGSTOP (a hang): the watchdog aborts and kills it; back within 150 s
#   R6 gap 10  the health check notes the restarts systemd made, with what to do
#   R7 gap 10  a stale backup (the newest made three days old) shows in agent-bus health
#              status and on the team's row of ccy-teams --list
#   R8 gap 3   agent-bus seat check waits for a homeserver that is starting, saying so
#   R9 gap 9   every LXC member unit (service: true) is enabled, running, restarted on any
#              exit, in a container that starts at boot; none declared is said, not passed
#
# EXIT CODES: 0 ACCEPTED; 1 REJECTED (a check or a setup step failed, and names itself);
# 64 usage.
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

PLAN_USAGE="usage: acceptance.bash [-h|--help]

Plan 00203's server-side robustness acceptance (checks R1-R9, ACCEPTANCE.md) on the
throwaway team acc-robust, removed on the way out. Exit 0 ACCEPTED, 1 REJECTED, 64 usage.
Prompts for sudo once, before the run log opens."

plan_mode deploy
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument: %s\n%s\n' "${PLAN_REMAINING_ARGS[0]}" "${PLAN_USAGE}" >&2
    exit 64
fi
if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '[FATAL] --check has nothing to rehearse here: the checks need a real team\n' >&2
    exit 64
fi
plan_require_host "it builds a homeserver team and kills, freezes and restarts its systemd unit on the host"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: it uses sudo for each root step\n' >&2
    exit 1
fi
plan_prime_sudo
plan_start_log auto

readonly TEAM="acc-robust"
readonly HS="agent-bus-hs@${TEAM}.service"
readonly HEALTH_SVC="agent-bus-hs-health@${TEAM}.service"
readonly INSTALLER="/usr/local/sbin/agent-bus-install"
readonly AGENT_BUS="/usr/local/bin/agent-bus"
readonly DELAY_DIR="/run/systemd/system/${HS}.d"
readonly DELAY_DROPIN="${DELAY_DIR}/zz-acceptance-start-delay.conf"
readonly DELAY_S=25
readonly TEAM_FILE="${PLAN_RUN_DIR}/${TEAM}.team.json"
readonly REPORT="${PLAN_RUN_DIR}/acceptance-report.md"
PORT=""
printf '# Plan 00203 acceptance: server-side robustness\n\nRun directory: %s\n\n' "${PLAN_RUN_DIR}" >"${REPORT}"

# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-steps.inc.bash
source "${PLAN_SCRIPT_DIR}/_acceptance-steps.inc.bash"
plan_on_cleanup teardown_on_exit

check_leg() {
    local name="$1" status=0
    shift
    printf '\n==> [check] %s\n' "${name}"
    "$@" || status=$?
    if [[ "${status}" -eq 0 ]]; then
        printf 'PASS  %s\n' "${name}"
        printf '## PASS %s\n\n' "${name}" >>"${REPORT}"
        return 0
    fi
    printf 'FAIL  %s\n' "${name}" >&2
    printf '## FAIL %s\n' "${name}" >>"${REPORT}"
    exit 1
}

plan_deploy_leg "remove a ${TEAM} team left by an interrupted run" teardown
plan_deploy_leg "agent-bus-install team ${TEAM} (loopback only, no member)" install_team
check_leg "R1 Type=notify, a 30 s watchdog, Restart=always; no restart while healthy" check_notify_and_quiet_watchdog
check_leg "R2 the health check runs; agent-bus health status reports the new team healthy" check_healthy_status
check_leg "R3 SIGKILL: back and answering within 60 s" check_signal SIGKILL 60
check_leg "R4 a clean exit (SIGTERM, exit 0, no systemctl stop): back within 90 s" check_signal SIGTERM 90
check_leg "R5 SIGSTOP: the watchdog aborts and kills it, back within 150 s" check_watchdog_hang
check_leg "R6 the health check notes systemd's restarts, with what to do" check_restarts_noted
check_leg "R7 a stale backup shows in agent-bus health status and on ccy-teams' team row" check_stale_backup
check_leg "R8 agent-bus seat check waits for a starting homeserver, saying so" check_seat_check_waits
check_leg "R9 LXC member units (service: true) enabled, running, Restart=always, autostarted" check_lxc_members
plan_deploy_leg "remove the ${TEAM} team (--purge)" teardown
note ""
note "ACCEPTED: R1-R9 passed; nothing of ${TEAM} remains"
plan_finish
