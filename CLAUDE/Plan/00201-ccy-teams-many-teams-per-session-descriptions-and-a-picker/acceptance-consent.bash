#!/usr/bin/env bash
# Plan 00201 — acceptance-consent.bash: ACCEPTANCE-owner.md step 08, the Element consent of
# `cc --teams`, answered No and then Yes, driven whole by this script on two throwaway teams.
#
# RUN ON THE HOST, as the desktop user (not root), after play-agent-bus.yml and
# play-claude-yolo.yml: through CLAUDE/Plan/meta-deploy.bash, or
#   ./CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/acceptance-consent.bash [--bus-address=<ip>]
# The one prompt is sudo's, before the log opens (R3); every root step after it uses `sudo -n`.
#
# THE CONSENT RECORD (pingbus's element-accepted under the passwd home's .config/pingbus) is the
# one file outside the repository it touches: the question is asked only while the record does
# not exist, and no environment variable moves it. It is held aside (renamed in its own
# directory to element-accepted.held-by-plan00201-consent-check) and renamed back however the
# run ends: success, failure, Ctrl-C, SIGTERM (plan_on_cleanup, first). A run killed outright
# cannot, so every run first puts back what an earlier one held aside, over whatever stands at
# the record's name (kept in the run directory): the held copy is the owner's own record. With
# no record to begin with, an empty marker beside it says so and the way back removes the one the
# Yes wrote. Meanwhile (a few minutes) a new cc --teams asks the consent again; running sessions
# are not affected (pingbus reads the record when a host seat starts).
#
# WHAT ELSE IT CHANGES, all removed on the way out: two throwaway teams, consent201a and
# consent201b, made and purged as acceptance.bash makes its own (declared only in the run
# directory's host_vars, read through CCY_TEAMS_HOST_VARS; refused if the owner declares either
# name); a scratch git checkout under TMPDIR with no .claude/, its origin a repository of the
# GitHub owner the teams trust; a private tmux server (own socket, own systemd user scope)
# running the owner's cc there; cc's token record (~/.claude/.last-launch.conf), saved and put
# back. The Yes session runs `claude update` as every cc launch does, takes Desktop at the token
# chooser, holds the seats and is ended with Ctrl-C before any turn.
#
# A PERSON IS NEEDED ONLY IF sudo asks for a password in the pane (cc makes each new host seat
# with `sudo agent-bus add-member`; sudo's cached credential is per terminal): the run prints
# how to attach and waits.
#
# EXIT CODES: 0 ACCEPTED; 1 REJECTED (a check or a step failed, and names itself); 2 COULD NOT
# ESTABLISH (sudo asked in the pane and nobody typed the password); 64 usage.
set -euo pipefail

# CHECKS (each a consent_leg; the first that does not pass ends the run, after the teardown):
#   N1 cc --teams <seat>@consent201a,<seat>@consent201b in the scratch checkout asks the consent,
#      naming both handles (the seats agent-bus offers there); No ends it with exit 1 and "Not
#      started. Nothing was recorded", no record written, no .claude/ (so no .claude/ccy/) in
#      the checkout, and neither handle a member of its team;
#   Y1 again, Yes: the record is written (a plain owner-only file, by this run) and both seats
#      are held by those handles;
#   Y2 the seats are released once the session ends;
#   C1 after the purge: nothing of the teams remains, the consent record and cc's token record
#      are as they were, and the scratch checkout is gone.

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

PLAN_USAGE="usage: acceptance-consent.bash [--bus-address=<ip>] [-h|--help]

Plan 00201, ACCEPTANCE-owner.md step 08: cc --teams's Element consent answered No, then Yes, in
a private tmux on two throwaway teams (consent201a, consent201b), with the consent record held
aside and put back however the run ends; then everything removed.
  --bus-address=<ip>   the teams' listen address; without it, agent_bus_address from the
                       host_vars localhost.yml
Exit 0 ACCEPTED, 1 REJECTED, 2 COULD NOT ESTABLISH (sudo asked in the pane and nobody typed), 64 usage. Prompts for sudo once, before the run log opens."

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
    printf '[FATAL] --check has nothing to rehearse here: the check needs real teams and a real cc\n' >&2
    exit 64
fi

plan_require_host "it creates homeserver teams and firewalld rules on the host, runs the owner's cc and holds aside a record under the owner's home"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: it uses sudo for each root step\n' >&2
    exit 1
fi

plan_prime_sudo
plan_start_log auto

# The names the M7 legs read (_acceptance-m7.inc.bash). SEAT and SEAT_LIST are theirs; the
# seats here are the ones agent-bus offers the scratch checkout (consent_choose_seats).
readonly TEAM_A="consent201a"
readonly TEAM_B="consent201b"
readonly DESC_A="Plan 00201 acceptance (consent) team A: a throwaway team, removed when the run ends"
readonly DESC_B="Plan 00201 acceptance (consent) team B"
readonly SEAT="1"
readonly SEAT_LIST="${SEAT}@${TEAM_A},${SEAT}@${TEAM_B}"
readonly HUMAN="tester"
readonly INSTALLER="${PLAN_REPO_ROOT}/files/usr/local/sbin/agent-bus-install"
readonly AGENT_BUS="/usr/local/bin/agent-bus"
readonly CCY_LAUNCHER="/var/local/claude-yolo/claude-yolo"
readonly CC_LAUNCHER="/var/local/claude-code/cc"
readonly CHECK=(python3 -I "${PLAN_SCRIPT_DIR}/m7_check.py")
readonly CONSENT=(python3 -I "${PLAN_SCRIPT_DIR}/consent_check.py")
readonly REPORT="${PLAN_RUN_DIR}/acceptance-consent-report.md"
readonly HOST_VARS_FILE="${PLAN_RUN_DIR}/host_vars.yml"
readonly OWNER_HOST_VARS="${PLAN_REPO_ROOT}/environment/localhost/host_vars/localhost.yml"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-m7.inc.bash
source "${PLAN_SCRIPT_DIR}/_acceptance-m7.inc.bash"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-consent.inc.bash
source "${PLAN_SCRIPT_DIR}/_acceptance-consent.inc.bash"
plan_on_cleanup consent_after_stop

printf '# Plan 00201 step 08: the cc --teams consent\n\nRun directory: %s\n\n' "${PLAN_RUN_DIR}" >"${REPORT}"

# consent_leg <name> <function> — one check, a bare top-level statement like plan_deploy_leg.
# The first that does not pass ends the run, after the teardown plan_on_cleanup runs: 2 is
# COULD NOT ESTABLISH (a person was needed and did not come), anything else REJECTED.
consent_leg() {
    local name="$1" status=0
    shift
    printf '\n==> [check] %s\n' "${name}"
    "$@" || status=$?
    case "${status}" in
        0)
            printf 'PASS  %s\n' "${name}"
            printf '## PASS %s\n\n' "${name}" >>"${REPORT}"
            ;;
        2)
            printf 'COULD NOT ESTABLISH  %s\n' "${name}" >&2
            printf '## COULD NOT ESTABLISH %s\n' "${name}" >>"${REPORT}"
            exit 2
            ;;
        *)
            printf 'FAIL  %s\n' "${name}" >&2
            printf '## FAIL %s\n' "${name}" >>"${REPORT}"
            exit 1
            ;;
    esac
}

plan_deploy_leg "cc, agent-bus, ccy-teams, tmux and gum are here; the owner declares neither team" consent_prerequisites
plan_deploy_leg "put back a consent record an interrupted run held aside" consent_put_back_killed
plan_deploy_leg "remove teams of these names an interrupted run left" consent_leftovers
plan_deploy_leg "the bus address" m7_resolve_bus_address
plan_deploy_leg "write the team files and the run's own host_vars" m7_write_teams
plan_deploy_leg "agent-bus-install team ${TEAM_A} and ${TEAM_B}" m7_install_teams
plan_deploy_leg "record each team's firewalld rules, present now" m7_record_rules
plan_deploy_leg "a scratch checkout with no .claude/, of the owner the teams trust" consent_make_checkout
plan_deploy_leg "the host seat each team offers that checkout, and its handle" consent_choose_seats
plan_deploy_leg "hold the consent record aside; save cc's token record" consent_hold
consent_leg "N1 the consent names both handles; No starts nothing and records nothing" consent_check_no
consent_leg "Y1 Yes records the consent and both seats are held" consent_check_yes
plan_deploy_leg "put the consent record back" consent_restore_record
plan_deploy_leg "end the cc session and its tmux server" consent_end_session
consent_leg "Y2 the seats are released once the session ends" consent_check_released
plan_deploy_leg "put cc's token record back" consent_restore_cc_status
plan_deploy_leg "remove the scratch checkout" consent_remove_checkout
plan_deploy_leg "purge ${TEAM_A} and ${TEAM_B}" m7_purge_teams
consent_leg "C1 nothing of the teams remains; the consent record and cc's token record are as they were" consent_check_gone
printf '\nACCEPTED: step 08, cc --teams'\''s consent: No started and recorded nothing, Yes recorded it and seated both handles; the consent record is as it was and nothing of the teams remains\n' | tee -a "${REPORT}"
plan_finish
