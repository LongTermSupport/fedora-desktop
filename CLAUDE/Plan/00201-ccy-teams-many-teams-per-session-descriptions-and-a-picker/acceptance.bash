#!/usr/bin/env bash
# Plan 00201 — acceptance.bash: the automated part of milestone M7, the two-team acceptance leg.
# The interactive part (the chooser, scope widening, cc's consent) is the owner's:
# ACCEPTANCE-owner.md.
#
# RUN ON THE HOST, as the desktop user (not root), after play-agent-bus.yml and
# play-claude-yolo.yml: through CLAUDE/Plan/meta-deploy.bash, or
#   ./CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/acceptance.bash [--bus-address=<ip>]
# The bus address is agent_bus_address from the host_vars. The one prompt is sudo's, before the
# log opens (R3); every root step after it uses `sudo -n`.
#
# WHAT IT CHANGES, all removed on the way out however the run ends (plan_on_cleanup), and first
# if an interrupted run left it: two throwaway teams, accept201a and accept201b, made by
# agent-bus-install from this checkout and purged (homeservers, units, firewalld rules, state);
# three hand-added host members of them; seat 1 of each team in this checkout's git-ignored
# .claude/ccy/pingbus/seats/ (parked by `agent-bus seat remove`); two headless ccy sessions; and
# this checkout's Quick Launch record (.claude/ccy/.last-launch.conf), which every ccy launch
# rewrites, saved first and put back. The owner's host_vars and teams are never touched: the
# teams are declared only in a host_vars file in the run directory, which ccy-teams and the
# launches read through CCY_TEAMS_HOST_VARS, and the run refuses to start if the owner declares
# a team of either name.
#
# NEEDS from the owner: ccy launched interactively in this checkout once (Quick Launch saves the
# token and SSH choice a headless launch reuses, every key usable unattended) and the hooks
# daemon installed in it. Each session runs one short turn on haiku.
#
# EXIT CODES: 0 ACCEPTED; 1 REJECTED (a check or a step failed, and names itself); 2 COULD NOT
# ESTABLISH (an owner step above is missing; nothing was changed); 64 usage.
set -euo pipefail

# CHECKS (each a check_leg; the first that fails ends the run, after the teardown):
#   D1 `ccy-teams --list` shows each team's description whole on its team row;
#   D2 each team room's m.room.topic is its description (read by the team human, by curl);
#   M1 one headless `ccy --teams 1@accept201a,1@accept201b`: its launch block names both teams,
#      their seats and descriptions;
#   D3 that session's `pingbus status` trusts both teams and prints each one's ABOUT line;
#   M2 its per-session record (pingbus SessionStart) lists both teams;
#   M3 the session's own settings make the guarded `pingbus status-line` its status line, and
#      run as its claude runs it (claude's environment) it prints the checkout's own line then
#      `👥 accept201a (<role>), accept201b (<role>)` on rows of its own, each role one it read;
#   R1 `pingbus say --to m7solo.1` (a member of accept201a only), no --team: SENT in accept201a;
#   R2 `pingbus say --to m7peer.1` (a member of both): exit 64, both teams listed with their
#      descriptions;
#   M4 SessionEnd removes the per-session record (seen from inside the container as it ends);
#   P1 a plain headless `ccy` is on no team: no seat label or PINGBUS_ variable, no pingbus, no
#      per-session record (so no bus status line);
#   C1 after the seats are parked and the teams purged: no recorded firewalld rule (permanent or
#      runtime), unit, state, stamp or seat left, and the Quick Launch record as it was.

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

PLAN_USAGE="usage: acceptance.bash [--bus-address=<ip>] [-h|--help]

Plan 00201 M7, the automated two-team acceptance: two throwaway teams with descriptions, one
headless ccy session on both (launch block, pingbus status ABOUT lines, the per-session record,
the pingbus status-line command, pingbus say team inference, SessionEnd), a plain ccy on no
team; then everything removed. The interactive part is ACCEPTANCE-owner.md.
  --bus-address=<ip>   the teams' listen address; without it, agent_bus_address from the
                       host_vars localhost.yml
Exit 0 ACCEPTED, 1 REJECTED, 2 COULD NOT ESTABLISH (an owner step missing), 64 usage. Prompts for sudo once, before the run log opens."

plan_mode deploy
plan_parse_common_flags "$@"

BUS_ADDRESS=""
for arg in "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"; do
    case "${arg}" in
        --bus-address=?*) BUS_ADDRESS="${arg#--bus-address=}" ;;
        *)
            printf '[FATAL] unknown argument: %s\n%s\n' "${arg}" "${PLAN_USAGE}" >&2
            exit 64
            ;;
    esac
done
if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '[FATAL] --check has nothing to rehearse here: the checks need real teams and sessions\n' >&2
    exit 64
fi

plan_require_host "it creates homeserver teams, firewalld rules and ccy sessions on the host and talks to them"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: it uses sudo for each root step\n' >&2
    exit 1
fi

plan_prime_sudo
plan_start_log auto

readonly TEAM_A="accept201a"
readonly TEAM_B="accept201b"
readonly DESC_A="Plan 00201 acceptance team A: a throwaway team, removed when the run ends"
readonly DESC_B="Plan 00201 acceptance team B"
readonly SEAT="1"
readonly SEAT_LIST="${SEAT}@${TEAM_A},${SEAT}@${TEAM_B}"
readonly HUMAN="tester"
readonly INSTALLER="${PLAN_REPO_ROOT}/files/usr/local/sbin/agent-bus-install"
readonly AGENT_BUS="/usr/local/bin/agent-bus"
readonly CCY_LAUNCHER="/var/local/claude-yolo/claude-yolo"
readonly CHECK=(python3 -I "${PLAN_SCRIPT_DIR}/m7_check.py")
readonly REPORT="${PLAN_RUN_DIR}/acceptance-report.md"
readonly HOST_VARS_FILE="${PLAN_RUN_DIR}/host_vars.yml"
readonly OWNER_HOST_VARS="${PLAN_REPO_ROOT}/environment/localhost/host_vars/localhost.yml"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-m7.inc.bash
source "${PLAN_SCRIPT_DIR}/_acceptance-m7.inc.bash"
plan_on_cleanup m7_teardown_after_stop

printf '# Plan 00201 M7 acceptance\n\nRun directory: %s\n\n' "${PLAN_RUN_DIR}" >"${REPORT}"

# check_leg <name> <function> — one check, a bare top-level statement like plan_deploy_leg. The
# checks build on each other, so the first that does not pass ends the run (exit 1), after the
# teardown plan_on_cleanup runs.
check_leg() {
    local name="$1"
    shift
    printf '\n==> [check] %s\n' "${name}"
    if ! "$@"; then
        printf 'FAIL  %s\n' "${name}" >&2
        printf '## FAIL %s\n' "${name}" >>"${REPORT}"
        exit 1
    fi
    printf 'PASS  %s\n' "${name}"
    printf '## PASS %s\n\n' "${name}" >>"${REPORT}"
}

# The prerequisites are not a plan_deploy_leg: a missing owner step (no saved Quick Launch, no
# hooks daemon) is COULD NOT ESTABLISH (2), which meta-deploy reports apart from a FAIL.
printf '\n==> [prerequisites] ccy, agent-bus, ccy-teams, the hooks daemon and a saved Quick Launch are here; the owner declares neither team\n'
prereq_status=0
m7_prerequisites || prereq_status=$?
case "${prereq_status}" in
    0) ;;
    2)
        printf 'COULD NOT ESTABLISH: an owner step is missing (above); nothing was changed\n' | tee -a "${REPORT}" >&2
        exit 2
        ;;
    *) exit 1 ;;
esac
plan_deploy_leg "remove what an interrupted run left: acceptance containers, seats and teams" m7_leftovers
plan_deploy_leg "the bus address" m7_resolve_bus_address
plan_deploy_leg "write the team files and the run's own host_vars" m7_write_teams
plan_deploy_leg "agent-bus-install team ${TEAM_A} and ${TEAM_B}" m7_install_teams
plan_deploy_leg "record each team's firewalld rules, present now" m7_record_rules
plan_deploy_leg "add ${M7_PEER_SHORT} to both teams and ${M7_SOLO_SHORT} to ${TEAM_A}" m7_add_members
check_leg "D1 ccy-teams --list shows each team's description" m7_check_list
check_leg "D2 each team room's topic is its description" m7_check_topics
plan_deploy_leg "save the Quick Launch record; ccy --headless --teams ${SEAT_LIST}, one turn" m7_start_multi
check_leg "M1 the launch block names both teams, seats and descriptions" m7_check_block
check_leg "D3 the session's pingbus status prints both ABOUT lines" m7_check_about
check_leg "M2 the per-session record lists both teams" m7_check_record
check_leg "M3 pingbus status-line shows the session's own line, then 👥 ${TEAM_A}, ${TEAM_B} with their roles below it" m7_check_segment
check_leg "R1 say --to a member of one team infers that team" m7_check_infer
check_leg "R2 say --to a name on both teams is refused with the choices" m7_check_ambiguous
check_leg "M4 SessionEnd removes the per-session record" m7_check_session_end
check_leg "P1 a plain ccy is on no team" m7_check_plain
plan_deploy_leg "park the seats, restore the Quick Launch record" m7_cleanup
plan_deploy_leg "purge ${TEAM_A} and ${TEAM_B}" m7_purge_teams
check_leg "C1 nothing of the teams remains; the Quick Launch record is as it was" m7_check_gone
printf '\nACCEPTED: descriptions, a session on two teams, team inference, SessionEnd and a plain session passed; nothing of the acceptance teams remains\n' | tee -a "${REPORT}"
plan_finish
