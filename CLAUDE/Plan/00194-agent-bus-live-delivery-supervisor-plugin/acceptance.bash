#!/usr/bin/env bash
# Plan 00194 — acceptance.bash: the goodbye host checks of Task 2.0b, automated (the Success
# Criteria's host steps 1-4; step 5, a restart's baseline, judges nothing and is not here).
# Each ending of a ccy session on a team is driven for real, then each team's room is read as
# its human reads it in Element:
#   G1 Ctrl-C mashed until the launcher has exited: exactly one goodbye per team, no
#      disconnected, and no traceback or failed `pingbus seat disconnected` on its terminal;
#   G2 `podman stop` of the session's container: exactly one disconnected per team;
#   G3 its tmux window closed (the session holds two seats): exactly one disconnected per seat;
#   G4 in each, never both a goodbye and a disconnected; C1 nothing of the run left.
#
# RUN ON THE HOST, as the desktop user (not root), after play-agent-bus.yml and
# play-claude-yolo.yml: through CLAUDE/Plan/meta-deploy.bash, or
#   ./CLAUDE/Plan/00194-agent-bus-live-delivery-supervisor-plugin/acceptance.bash [--bus-address=<ip>]
# The one prompt is sudo's, before the log opens (R3); every root step after it uses `sudo -n`.
# `--fake` runs only the verdict legs, over fixture rooms it writes, anywhere (the CCY container
# included); test_goodbye_check.py runs it.
#
# WHAT IT CHANGES, all removed on the way out however the run ends (plan_on_cleanup), and first
# if an interrupted run left it: two throwaway teams, accept194a and accept194b, made and purged
# by Plan 00201's acceptance legs and declared only in a host_vars file in the run directory; a
# scratch checkout in the run directory holding seat 1 of each (its own git config turns commit
# and tag signing off: it never commits, so its sessions launch with --no-ssh and no key or
# agent of the owner's goes into them); a tmux server of its own at
# $XDG_RUNTIME_DIR/accept194-tmux/ccy; three idle interactive ccy sessions on both seats, one at
# a time (accept194-ctrlc, -stop, -window: containers labelled ccy-session=<name>, membership
# directories); a login of each team's human. Like every ccy launch, the launcher also removes
# STOPPED ccy containers an unclean shutdown left. The owner's teams, checkout, Quick Launch
# record, sessions, containers and tmux server are not touched: the run refuses a team name the
# owner declares, stops and judges only containers labelled with its own session names, and
# posts only into its own teams. No model turn is run.
#
# NEEDS from the owner: ccy launched interactively in this checkout once, with a token chosen
# (the sessions use the token name Quick Launch saved). Without it: COULD NOT ESTABLISH.
#
# EXIT CODES: 0 ACCEPTED; 1 REJECTED (a check or a step failed, and names itself); 2 COULD NOT
# ESTABLISH (an owner step above is missing; nothing was changed); 64 usage.
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

PLAN_USAGE="usage: acceptance.bash [--bus-address=<ip>] [--fake] [-h|--help]

Plan 00194 Task 2.0b, the goodbye host checks: on two throwaway teams, a ccy session ended by
mashing Ctrl-C posts one goodbye per team (G1), one ended by podman stop posts one disconnected
per team (G2), one whose tmux window is closed posts one disconnected per seat (G3), and no
ending posts both (G4); then everything is removed (C1).
  --bus-address=<ip>   the teams' listen address; without it, agent_bus_address from the
                       host_vars localhost.yml
  --fake               only the verdict legs, over fixture rooms; runs anywhere, changes nothing
Exit 0 ACCEPTED, 1 REJECTED, 2 COULD NOT ESTABLISH (an owner step missing), 64 usage. Prompts for sudo once, before the run log opens."

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
    printf '[FATAL] --check has nothing to rehearse here: the checks need real teams and sessions (--fake runs the verdicts alone)\n' >&2
    exit 64
fi

readonly TEAM_A="accept194a"
readonly TEAM_B="accept194b"
readonly DESC_A="Plan 00194 goodbye acceptance team A: a throwaway team, removed when the run ends"
readonly DESC_B="Plan 00194 goodbye acceptance team B: a throwaway team, removed when the run ends"
readonly SEAT="1"
readonly SEAT_LIST="${SEAT}@${TEAM_A},${SEAT}@${TEAM_B}"
readonly HUMAN="tester"
readonly INSTALLER="${PLAN_REPO_ROOT}/files/usr/local/sbin/agent-bus-install"
readonly AGENT_BUS="/usr/local/bin/agent-bus"
readonly CCY_LAUNCHER="/var/local/claude-yolo/claude-yolo"
readonly OWNER_HOST_VARS="${PLAN_REPO_ROOT}/environment/localhost/host_vars/localhost.yml"
readonly GB_CHECK=(python3 -I "${PLAN_SCRIPT_DIR}/goodbye_check.py")
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
    readonly REPORT="${PLAN_RUN_DIR}/acceptance-fake-report.md"
    # shellcheck source-path=SCRIPTDIR
    # shellcheck source=_acceptance-goodbye.inc.bash
    source "${PLAN_SCRIPT_DIR}/_acceptance-goodbye.inc.bash"
    printf '# Plan 00194 goodbye acceptance, fake mode\n\nRun directory: %s\n\n' "${PLAN_RUN_DIR}" >"${REPORT}"
    plan_deploy_leg "write the fixture rooms and panes" gb_fake_rooms
    check_leg "G1 (fake) one goodbye in each team, over two pages" gb_fake_case goodbye 1 0
    check_leg "G2/G3 (fake) one disconnected in each team" gb_fake_case disconnected 0 1
    reject_leg "G4 (fake) a goodbye and a disconnected from one ending" gb_fake_case both 1 0
    reject_leg "G4 (fake) a disconnected and a goodbye from one ending" gb_fake_case both 0 1
    reject_leg "G1 (fake) two goodbyes" gb_fake_case two-goodbyes 1 0
    reject_leg "G1 (fake) no goodbye" gb_fake_case silent 1 0
    reject_leg "G2/G3 (fake) no disconnected" gb_fake_case silent 0 1
    reject_leg "G1 (fake) a goodbye with another reason" gb_fake_case other-reason 1 0
    check_leg "G1 (fake) a quiet terminal" gb_fake_pane clean
    reject_leg "G1 (fake) a terminal showing a traceback" gb_fake_pane traceback
    check_leg "launch (fake) the scratch checkout's signing off passes ccy's signing check with no SSH identity" gb_fake_signing off
    reject_leg "launch (fake) a checkout leaving signing to ~/.gitconfig is refused by it" gb_fake_signing inherited
    check_leg "launch (fake) a launcher that ended early is reported by its ERROR line alone" gb_fake_launcher_died
    check_leg "launch (fake) a launcher waiting at a question is caught, by name" gb_fake_prompt waiting
    reject_leg "launch (fake) a launcher still starting is not taken for one waiting" gb_fake_prompt starting
    printf '\nFAKE ACCEPTED: the verdicts accept what each ending should leave and reject the rest\n' | tee -a "${REPORT}"
    plan_finish
fi

plan_require_host "it creates homeserver teams, firewalld rules, ccy sessions and a tmux server on the host and talks to them"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: it uses sudo for each root step\n' >&2
    exit 1
fi

plan_prime_sudo
plan_start_log auto

readonly REPORT="${PLAN_RUN_DIR}/acceptance-report.md"
readonly HOST_VARS_FILE="${PLAN_RUN_DIR}/host_vars.yml"
# The teams: Plan 00201's legs, which read the globals above (TEAM_A, DESC_A, CHECK, ...).
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/_acceptance-m7.inc.bash
source "${P201}/_acceptance-m7.inc.bash"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-goodbye.inc.bash
source "${PLAN_SCRIPT_DIR}/_acceptance-goodbye.inc.bash"
gb_paths
plan_on_cleanup gb_teardown

printf '# Plan 00194 goodbye acceptance\n\nRun directory: %s\n\n' "${PLAN_RUN_DIR}" >"${REPORT}"

# The prerequisites are not a plan_deploy_leg: a missing owner step (no saved token) is COULD
# NOT ESTABLISH (2), which meta-deploy reports apart from a FAIL.
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
plan_deploy_leg "a password for each team's human, while sudo is fresh" gb_human_passwords
plan_deploy_leg "the scratch checkout" gb_make_checkout
plan_deploy_leg "take ${SEAT_LIST} in the scratch checkout" gb_take_seats
plan_deploy_leg "log each team's human in and join its room" gb_human_logins
plan_deploy_leg "the run's own tmux server" gb_tmux_start
check_leg "G1+G4 Ctrl-C mashed: one goodbye per team, no disconnected, a clean exit" gb_check_ctrl_c
check_leg "G2+G4 podman stop: one disconnected per team, no goodbye" gb_check_podman_stop
check_leg "G3+G4 the tmux window closed: one disconnected per seat, no goodbye" gb_check_window_closed
plan_deploy_leg "log out, stop the tmux server, park the seats, delete the checkout" gb_cleanup
plan_deploy_leg "purge ${TEAM_A} and ${TEAM_B}" m7_purge_teams
check_leg "C1 nothing of the run remains" gb_check_gone
printf '\nACCEPTED: Ctrl-C says goodbye once, podman stop and a closed window say disconnected once per seat, no ending says both; nothing of the run remains\n' | tee -a "${REPORT}"
plan_finish
