#!/usr/bin/env bash
# Plan 00161 — acceptance.bash: the verdict on the agent team bus. First slice, unit U17,
# milestone M1 (DESIGN.md section 12): a real Tuwunel from agent-bus-install, two `host`
# members of the acceptance team, a ping and a human message. U20, U23, U24 and U27 add
# their slices and section 10's P1-P8.
#
# RUN ON THE HOST, as the desktop user (not root), after deploy.bash installed the software
# and agentbus0: through CLAUDE/Plan/meta-deploy.bash (which runs it after deploy.bash), or
#   ./CLAUDE/Plan/00161-agent-team-bus-matrix/acceptance.bash [--bus-address=<ip>]
# The bus address comes from where deploy.bash reads it (_bus-address.inc.bash). The one
# prompt is sudo's, before the log opens (R3); every root step after it uses `sudo -n`.
# GitHub's API must answer, with no credential: every ping's reference is checked there
# (protocol §6), at this checkout's upstream branch as last fetched.
#
# M1 CHECKS, in DESIGN.md section 12's U17 row (each step in _acceptance-steps.inc.bash):
#   M1.1 a (orchestrator) sends `review` (a path reference) to b (worker); b's `wait` prints
#        exactly that PING; b's `ack --re` it reaches a's `recv`;
#   M1.2 the human, by curl, posts an m.text mentioning a only: a's `recv` prints exactly
#        that HUMAN line, b's prints nothing of it;
#   M1.3 a sends `review` (a commit reference) to b, who never answers: a's `wait` prints one
#        TIMEOUT, for it, after a's 60 s ack deadline, and none for the review b acked.
#
# U23 CHECKS (milestone M3a, _acceptance-u23.inc.bash), after M1's, in the same team: an LXC,
# a docker and a VM member each join by its README (the kit, suggest-handle, add-member, the
# bundle, config check), their bridge networks added to allow_from as found at run time; a
# sends each a `review`, its `wait` prints exactly that PING, its `ack` reaches a's `recv`.
#   U23.lxc     a throwaway LXC container (Fedora, downloaded by lxc-create's template)
#   U23.docker  a throwaway container of a digest-pinned python image (pulled here)
#   U23.vm      a libvirt guest the owner names: --vm-ssh=<user>@<address>, or
#               agent_bus_acceptance_vm in the untracked host_vars
# Without LXC, rootful Docker or that guest, the leg reports SKIPPED-NEEDS-OWNER: the run goes
# on, but ends NOT ACCEPTED (exit 3) naming what the owner must provide; the plan cannot close.
#
# WHAT IT CREATES is removed on the way out however the run ends, and first if an interrupted
# run left it: the team `acceptance` (section 10's reserved name; `agent-bus-install team` and
# `remove --purge`, from this checkout as deploy.bash runs it), and the two members. The
# design says "dedicated test users"; useradd would be a persistent change an interrupted run
# leaves behind, so each pingbus command runs instead as a transient systemd service with
# DynamicUser=yes and one User= name per member: two UIDs that exist only while a command
# runs, neither the desktop user (who may hold a human's Element session, section 8). Each
# member's PINGBUS_HOME is its StateDirectory, /var/lib/private/agent-bus-acceptance-<a|b>.
# systemd creates /var/lib/private itself on a host that had none; the run removes it again
# when it was absent at the start and is empty at the end. The one thing that can outlive a
# run is that empty directory after an interrupted first run: the next run finds it present.
# The run directory keeps the evidence (each member's stdout and stderr, the team file, the
# human's message, acceptance-report.md); the tokens leave it once each member holds its own.
#
# EXIT CODES: 0 ACCEPTED; 1 REJECTED (a check or a setup step failed, and names itself);
# 2 COULD NOT ESTABLISH (GitHub's API did not answer a reference check; nothing on the bus
# failed); 3 NOT ACCEPTED, SKIPPED-NEEDS-OWNER (every check that ran passed, and at least one
# needs something only the owner can provide); 64 usage (an unknown argument, --check, no bus
# address, or a malformed one).
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

readonly TEAM="acceptance"
readonly HUMAN="tester"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_bus-address.inc.bash
source "${PLAN_SCRIPT_DIR}/_bus-address.inc.bash"

PLAN_USAGE="usage: acceptance.bash [--bus-address=<ip>] [--vm-ssh=<user>@<address>] [-h|--help]

Plan 00161's acceptance, slice M1 (unit U17): creates the acceptance team with
agent-bus-install, two host members as transient systemd DynamicUser services, and a human
login; checks a review ping received by wait and acked, a human message delivered only to
the member it mentions, and a TIMEOUT for an unanswered ping; then removes all of it.
Then slice U23: an LXC, a docker and a VM member join the same team and exchange a review
and an ack with the desktop's member.
  --bus-address=<ip>   the team's listen address; without it, ${BUS_ADDRESS_VAR} from the
                       host_vars localhost.yml (as deploy.bash)
  --vm-ssh=<dest>      U23's VM member: an SSH destination for a libvirt guest on a bridge of
                       this host; without it, agent_bus_acceptance_vm from the same host_vars
Exit 0 ACCEPTED, 1 REJECTED, 2 COULD NOT ESTABLISH (GitHub did not answer),
3 NOT ACCEPTED (SKIPPED-NEEDS-OWNER: a leg needs the owner), 64 usage.
Prompts for sudo once, before the run log opens."

plan_mode deploy
plan_parse_common_flags "$@"

BUS_ADDRESS=""
VM_SSH=""
for arg in "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"; do
    case "${arg}" in
        --bus-address=?*) BUS_ADDRESS="${arg#--bus-address=}" ;;
        --vm-ssh=?*) VM_SSH="${arg#--vm-ssh=}" ;;
        *)
            printf '[FATAL] unknown argument: %s\n%s\n' "${arg}" "${PLAN_USAGE}" >&2
            exit 64
            ;;
    esac
done
if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '[FATAL] --check has nothing to rehearse here: the checks need a real team and members\n' >&2
    exit 64
fi

plan_require_host "it creates a homeserver team, systemd services and firewalld rules on the host and talks to them"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: it uses sudo for each root step\n' >&2
    exit 1
fi
resolve_bus_address || exit $?

plan_prime_sudo
plan_start_log auto

readonly INSTALLER="${PLAN_REPO_ROOT}/files/usr/local/sbin/agent-bus-install"
readonly AGENT_BUS="/usr/local/bin/agent-bus"
readonly PINGBUS="/usr/local/bin/pingbus"
readonly CHECK=(python3 -I "${PLAN_SCRIPT_DIR}/acceptance_check.py")
readonly TEAM_FILE="${PLAN_RUN_DIR}/${TEAM}.team.json"
readonly MEMBER_DIR="${PLAN_RUN_DIR}/members"
readonly REPORT="${PLAN_RUN_DIR}/acceptance-report.md"
TEAM_PRESENT=0
#: What the owner must provide, one entry per check that returned SKIPPED-NEEDS-OWNER (3);
#: appended by needs_owner (_acceptance-u23.inc.bash), read by finish_needs_owner below.
OWNER_NEEDS=()
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_deploy-steps.inc.bash
source "${PLAN_SCRIPT_DIR}/_deploy-steps.inc.bash"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-steps.inc.bash
source "${PLAN_SCRIPT_DIR}/_acceptance-steps.inc.bash"
plan_on_cleanup teardown_after_stop
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-u23.inc.bash
source "${PLAN_SCRIPT_DIR}/_acceptance-u23.inc.bash"
plan_on_cleanup u23_teardown_after_stop

printf '# Plan 00161 acceptance, slice M1\n\nRun directory: %s\n\n' "${PLAN_RUN_DIR}" >"${REPORT}"

# finish_needs_owner — after the final teardown: with any owner step recorded, the run is NOT
# ACCEPTED (exit 3), however much else passed, and says what the owner must provide.
finish_needs_owner() {
    local need
    if [[ "${#OWNER_NEEDS[@]}" -eq 0 ]]; then
        return 0
    fi
    printf '\nNOT ACCEPTED: SKIPPED-NEEDS-OWNER. Every check that ran passed and nothing of the acceptance team remains, but the plan cannot close until the owner provides:\n' | tee -a "${REPORT}"
    for need in "${OWNER_NEEDS[@]}"; do
        printf -- '- %s\n' "${need}" | tee -a "${REPORT}"
    done
    plan_list_reports
    exit 3
}

# check_leg <name> <function> — one check, a bare top-level statement like plan_deploy_leg.
# The checks build on each other, so the first that does not pass ends the run: FAIL exits 1,
# COULD NOT ESTABLISH exits 2, both after the teardown plan_on_cleanup runs. SKIPPED-NEEDS-OWNER
# (3) is recorded and the run goes on; finish_needs_owner ends it on that.
check_leg() {
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
            printf 'COULD NOT ESTABLISH  %s: GitHub did not answer a reference check; nothing on the bus failed\n' "${name}" >&2
            printf '## COULD NOT ESTABLISH %s (GitHub did not answer)\n' "${name}" >>"${REPORT}"
            exit 2
            ;;
        3)
            printf 'SKIPPED-NEEDS-OWNER  %s\n' "${name}" >&2
            printf '## SKIPPED-NEEDS-OWNER %s: %s\n\n' "${name}" "${OWNER_NEEDS[-1]}" >>"${REPORT}"
            ;;
        *)
            printf 'FAIL  %s\n' "${name}" >&2
            printf '## FAIL %s\n' "${name}" >>"${REPORT}"
            exit 1
            ;;
    esac
}

plan_deploy_leg "remove acceptance members and a ${TEAM} team left by an interrupted run" teardown leftover
plan_deploy_leg "remove U23 members left by an interrupted run" u23_teardown leftover
plan_deploy_leg "the references the pings carry" resolve_reference
plan_deploy_leg "write the ${TEAM} team file" write_acceptance_team_file
plan_deploy_leg "agent-bus-install team ${TEAM}" install_team team
plan_deploy_leg "agent-bus add-member: a (orchestrator) and b (worker)" add_members
plan_deploy_leg "members take their bundles, pass config check and join the team room" place_members
check_leg "M1.1 review sent, received by wait, acked" check_review_ack
check_leg "M1.2 a human message reaches only the member it mentions" check_human_addressed
check_leg "M1.3 TIMEOUT for an unanswered review, none for the acked one" check_timeout
plan_deploy_leg "U23: an LXC member's throwaway container" u23_prepare lxc
plan_deploy_leg "U23: a docker member's throwaway container" u23_prepare docker
plan_deploy_leg "U23: a VM member's guest, as the owner gave it" u23_prepare vm
plan_deploy_leg "U23: the members' bridge networks join allow_from; agent-bus-install team ${TEAM}" u23_widen_team
check_leg "U23.lxc an LXC member joins by README.lxc and exchanges a review and an ack" u23_check lxc
check_leg "U23.docker a docker member joins by README.docker and exchanges a review and an ack" u23_check docker
check_leg "U23.vm a VM member joins by README.vm and exchanges a review and an ack" u23_check vm
plan_deploy_leg "U23: remove the LXC, docker and VM members" u23_teardown final
plan_deploy_leg "remove the members and the ${TEAM} team (--purge)" teardown final
finish_needs_owner
printf '\nACCEPTED: M1 (host-to-host) and U23 (LXC, docker and VM members) passed; nothing of the acceptance team remains\n' | tee -a "${REPORT}"
plan_finish
