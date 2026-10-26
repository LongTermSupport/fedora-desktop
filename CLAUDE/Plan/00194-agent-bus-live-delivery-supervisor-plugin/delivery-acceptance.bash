#!/usr/bin/env bash
# Plan 00194 — delivery-acceptance.bash: the host checks of Tasks 2.0c (SPEC-inject H1-H6),
# 2.0d (E25, a homeserver restart under an idle session) and 2.0f (a typed @room reaches no
# agent and is counted), with a scripted test human (bus_human.py) posting where the spec says
# Element (journal 2026-10-23). It proves the server-side path and the envelope, not Element's
# rendering. Each check is named where it is run, at the bottom, and described in
# _acceptance-delivery.inc.bash. H7 (a session without crossSessionInbound: accept) cannot be
# made here: it is printed for the host agent, and the run ends COULD NOT ESTABLISH (2) when
# everything else passed.
#
# RUN ON THE HOST, as the desktop user (not root), after play-agent-bus.yml and
# play-claude-yolo.yml: through CLAUDE/Plan/meta-deploy.bash, or
#   ./CLAUDE/Plan/00194-agent-bus-live-delivery-supervisor-plugin/delivery-acceptance.bash [--bus-address=<ip>]
# The one prompt is sudo's, before the log opens (R3); every root step after it uses `sudo -n`.
# `--fake` runs only the verdict legs, over fixture files it writes, anywhere (the CCY container
# included); test_delivery_check.py runs it.
#
# WHAT IT CHANGES, all removed on the way out however the run ends (plan_on_cleanup), and first
# if an interrupted run left it: two throwaway teams, accept194c and accept194d, declared only
# in a host_vars file in the run directory, with their human `tester` (the test human; its
# password, set by `agent-bus human password`, is kept in memory only); a scratch checkout in
# the run directory holding seat 1 of both teams and seat 2 of accept194c (the peer, never
# launched); a tmux server of its own; one interactive ccy session on seat 1 of both
# (accept194-deliver), which TAKES MODEL TURNS on the owner's saved token: each delivery wakes
# it, and H1, H3 and H2 ask it for an answer, a file in the scratch checkout and a `sleep 60`;
# the test human's logins, 0600 files in the run directory, logged out at the end; one restart
# of accept194c's homeserver unit. The owner's teams, checkout, sessions, containers and tmux
# server are not touched.
#
# NEEDS from the owner: ccy launched interactively in this checkout once, with a token chosen.
#
# EXIT CODES: 0 ACCEPTED; 1 REJECTED (a check or a step failed, and names itself); 2 COULD NOT
# ESTABLISH (an owner step is missing, or a step is the host agent's: each printed exactly);
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
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_image-build.inc.bash
source "${repoRoot}/CLAUDE/Plan/_image-build.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="usage: delivery-acceptance.bash [--bus-address=<ip>] [--fake] [-h|--help]

Plan 00194 Tasks 2.0c, 2.0d and 2.0f, the delivery host checks: a scripted test human posts to
two throwaway teams and one idle ccy session on both is judged by its transcript, its pane, its
pingbus status and watch log, and the room (H1-H6, the homeserver restart, the typed @room).
H7 is printed for the host agent. Everything is removed at the end.
  --bus-address=<ip>   the teams' listen address; without it, agent_bus_address from the
                       host_vars localhost.yml
  --fake               only the verdict legs, over fixture files; runs anywhere, changes nothing
Exit 0 ACCEPTED, 1 REJECTED, 2 COULD NOT ESTABLISH (an owner step missing, or a step that is the
host agent's, printed), 64 usage. Prompts for sudo once, before the run log opens."

plan_mode deploy
plan_parse_common_flags "$@"

BUS_ADDRESS=""
FAKE=0
for arg in "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"; do
    case "${arg}" in
        --bus-address=?*) BUS_ADDRESS="${arg#--bus-address=}" ;;
        --fake) FAKE=1 ;;
        *)
            printf '[FATAL] unknown argument: %s\n%s\n' "${arg}" "${PLAN_USAGE}" >&2
            exit 64
            ;;
    esac
done
if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '[FATAL] --check has nothing to rehearse here: the checks need real teams and a session (--fake runs the verdicts alone)\n' >&2
    exit 64
fi

readonly TEAM_A="accept194c"
readonly TEAM_B="accept194d"
readonly DESC_A="Plan 00194 delivery acceptance team A: a throwaway team, removed when the run ends"
readonly DESC_B="Plan 00194 delivery acceptance team B: a throwaway team, removed when the run ends"
# The goodbye legs this run builds on, set for this run's teams and its one session (the
# include makes both read-only).
GB_DESC_PREFIX="Plan 00194 delivery acceptance"
readonly DL_SESSION="accept194-deliver"
GB_SESSIONS=("${DL_SESSION}")
readonly SEAT="1"
readonly PEER_SEAT="2"
readonly SEAT_LIST="${SEAT}@${TEAM_A},${SEAT}@${TEAM_B}"
readonly HUMAN="tester"
readonly INSTALLER="${PLAN_REPO_ROOT}/files/usr/local/sbin/agent-bus-install"
readonly AGENT_BUS="/usr/local/bin/agent-bus"
readonly CCY_LAUNCHER="/var/local/claude-yolo/claude-yolo"
readonly OWNER_HOST_VARS="${PLAN_REPO_ROOT}/environment/localhost/host_vars/localhost.yml"
readonly GB_CHECK=(python3 -I "${PLAN_SCRIPT_DIR}/goodbye_check.py")
readonly DC=(python3 -I "${PLAN_SCRIPT_DIR}/delivery_check.py")
readonly BH=(python3 -I "${PLAN_SCRIPT_DIR}/bus_human.py")
RUN_TAG="r$(date +%s)"
readonly RUN_TAG
# Plan 00201's two-team acceptance, whose team set-up this run reuses, wherever it is filed.
P201=""
for dir in "${PLAN_REPO_ROOT}/CLAUDE/Plan" "${PLAN_REPO_ROOT}/CLAUDE/Plan/Completed"; do
    if [[ -f "${dir}/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/_acceptance-m7.inc.bash" ]]; then
        P201="${dir}/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker"
    fi
done
if [[ -z "${P201}" ]]; then
    printf '[FATAL] Plan 00201'\''s acceptance (_acceptance-m7.inc.bash, m7_check.py) is in neither CLAUDE/Plan nor CLAUDE/Plan/Completed: this run builds its teams with it\n' >&2
    exit 1
fi
readonly P201
readonly CHECK=(python3 -I "${P201}/m7_check.py")

# check_leg <name> <function> — a check the rest needs: the first that does not pass ends the
# run (exit 1), after the teardown plan_on_cleanup runs.
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

# reject_leg <name> <function> — the fake mode's negative check: the verdict must refuse it.
reject_leg() {
    local name="$1"
    shift
    printf '\n==> [must be rejected] %s\n' "${name}"
    if "$@"; then
        printf 'FAIL  %s: the verdict accepted it\n' "${name}" >&2
        printf '## FAIL %s (accepted)\n' "${name}" >>"${REPORT}"
        exit 1
    fi
    printf 'PASS  %s (rejected, as it must be)\n' "${name}"
    printf '## PASS %s (rejected)\n\n' "${name}" >>"${REPORT}"
}

if [[ "${FAKE}" -eq 1 ]]; then
    # STANDARD-EXCEPTION(R2): --fake reads only the fixtures it writes, so where it runs does not matter
    plan_start_log auto
    readonly REPORT="${PLAN_RUN_DIR}/delivery-acceptance-fake-report.md"
    # shellcheck source-path=SCRIPTDIR
    # shellcheck source=_acceptance-delivery.inc.bash
    source "${PLAN_SCRIPT_DIR}/_acceptance-delivery.inc.bash"
    printf '# Plan 00194 delivery acceptance, fake mode\n\nRun directory: %s\n\n' "${PLAN_RUN_DIR}" >"${REPORT}"
    plan_deploy_leg "write the fixture transcripts, panes, status and watch logs" dl_fake_files
    check_leg "H1 (fake) the human's entry, closed, from the test human" dl_fake_entry good H1 HUMAN "${HUMAN}"
    reject_leg "H1 (fake) the same text arriving as a PEER entry" dl_fake_entry bad H1 HUMAN "${HUMAN}"
    check_leg "H1 (fake) the pane shows the delivery" dl_fake_pane good
    reject_leg "H1 (fake) a pane without it" dl_fake_pane bad
    check_leg "H1 (fake) no recv ran" dl_fake_no_recv good
    reject_leg "H1 (fake) a session that ran pingbus recv" dl_fake_no_recv bad
    check_leg "H3 (fake) the peer's entry is PEER from its handle" dl_fake_entry good H3P PEER fedora-desktop.2+host.podman
    check_leg "H5 (fake) both teams arrive, each naming its own" dl_fake_h5 good
    check_leg "H4 (fake) the long body is cut with the history pointer" dl_fake_truncated good
    check_leg "H4 (fake) the eight hostile bodies all arrive closed" dl_fake_hostile good
    reject_leg "H4 (fake) seven of eight" dl_fake_hostile bad
    check_leg "2.0f (fake) ignored=1 in team A's status line" dl_fake_ignored good
    reject_leg "2.0f (fake) ignored=0" dl_fake_ignored bad
    check_leg "H2 (fake) the busy turn's sleep 60 is seen" dl_fake_busy good
    check_leg "2.0d (fake) the watch log says the team syncs again" dl_fake_syncs good
    reject_leg "2.0d (fake) a watch log without it" dl_fake_syncs bad
    check_leg "E16 (fake) no delivery missing" dl_fake_missing good
    reject_leg "E16 (fake) a delivery the watch log calls missing" dl_fake_missing bad
    printf '\nFAKE ACCEPTED: the verdicts accept what a sound run leaves and reject the rest\n' | tee -a "${REPORT}"
    plan_finish
fi

plan_require_host "it creates homeserver teams, firewalld rules, a ccy session and a tmux server on the host and talks to them"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: it uses sudo for each root step\n' >&2
    exit 1
fi

plan_prime_sudo
plan_start_log auto

readonly REPORT="${PLAN_RUN_DIR}/delivery-acceptance-report.md"
readonly HOST_VARS_FILE="${PLAN_RUN_DIR}/host_vars.yml"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/_acceptance-m7.inc.bash
source "${P201}/_acceptance-m7.inc.bash"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-goodbye.inc.bash
source "${PLAN_SCRIPT_DIR}/_acceptance-goodbye.inc.bash"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-delivery.inc.bash
source "${PLAN_SCRIPT_DIR}/_acceptance-delivery.inc.bash"
gb_paths
dl_paths
plan_on_cleanup dl_teardown
plan_on_cleanup gb_teardown

printf '# Plan 00194 delivery acceptance (run %s)\n\nRun directory: %s\n\n' "${RUN_TAG}" "${PLAN_RUN_DIR}" >"${REPORT}"

printf '\n==> [prerequisites] the launcher, agent-bus, tmux and a saved token are here; the owner declares neither team\n'
prereq_status=0
gb_prerequisites || prereq_status=$?
case "${prereq_status}" in
    0) ;;
    2)
        printf 'COULD NOT ESTABLISH: an owner step is missing (above); nothing was changed\n' | tee -a "${REPORT}" >&2
        exit 2
        ;;
    *) exit 1 ;;
esac
plan_deploy_leg "remove what an interrupted run left: its tmux server, containers, checkouts and teams" gb_leftovers
plan_deploy_leg "the bus address" m7_resolve_bus_address
plan_deploy_leg "write the team files and the run's own host_vars" m7_write_teams
plan_deploy_leg "agent-bus-install team ${TEAM_A} and ${TEAM_B}" m7_install_teams
plan_deploy_leg "record each team's firewalld rules, present now" m7_record_rules
plan_deploy_leg "a password for the test human ${HUMAN} in each team, while sudo is fresh" gb_human_passwords
plan_deploy_leg "the scratch checkout" gb_make_checkout
plan_deploy_leg "take ${SEAT_LIST} and the peer ${PEER_SEAT}@${TEAM_A} in the scratch checkout" dl_take_seats
plan_deploy_leg "log the test human in on each team and join its room" dl_human_logins
plan_deploy_leg "the run's own tmux server" gb_tmux_start
plan_deploy_leg "the idle session ${DL_SESSION} on ${SEAT_LIST}" dl_launch
check_leg "H1 an idle session: the human's envelope in the thread within seconds, no recv" dl_check_h1
soft_check "H6 the agent's reply: line answer reaches the room under its handle" dl_check_h6
soft_check "H5 two teams: both arrive, each naming its team" dl_check_h5
soft_check "H4 a 9 KiB body is cut with the history pointer; eight hostile bodies arrive whole" dl_check_h4
soft_check "2.0f a typed @room reaches no agent and pingbus status counts it ignored" dl_check_unheard
soft_check "H3 the human's request is acted on; a peer's same request is not" dl_check_h3
soft_check "H2 a busy session: the message is shown when the turn ends" dl_check_h2
soft_check "2.0d the homeserver restarted under the idle session: the watcher syncs again and the next message arrives" dl_check_restart
soft_check "E16 no delivery of this run is missing" dl_check_none_missing
dl_manual_h7
plan_deploy_leg "log the test human out" dl_human_logouts
plan_deploy_leg "stop the tmux server, park the seats, delete the checkout" gb_cleanup
plan_deploy_leg "purge ${TEAM_A} and ${TEAM_B}" m7_purge_teams
check_leg "C1 nothing of the run remains" gb_check_gone
verdict=0
dl_verdict || verdict=$?
if [[ "${verdict}" -ne 0 ]]; then
    exit "${verdict}"
fi
plan_finish
