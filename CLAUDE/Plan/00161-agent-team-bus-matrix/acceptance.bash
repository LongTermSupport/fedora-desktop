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
# M2 CHECKS, U20's row (_acceptance-u20.inc.bash): real ccy sessions in THIS checkout, run
# headless with stream-json input so each sits idle between turns, each launched with
# `ccy --teams <seat>@acceptance` onto a seat its first launch creates: acca (made
# orchestrator), accb and accc (workers). No checkout or owner setup step. Judged on room
# events, `agent-bus seat list`, `sudo agent-bus list`, each seat's `pingbus status`, the
# containers' labels and the sessions' transcripts (found by session ID), never on what the
# model says.
#   M2.0 the three launches create their seats: 0700/0600, git-ignored, three new members,
#        each handle's host the checkout's rule, all held; ccy.env.local unchanged;
#   M2.1 acca is told to send `review` to accb; accb, idle and never written to again, is
#        woken by its watcher's notice and acks it;
#   M2.2 a second review reaches accb once its inbox is empty: a second notice with the same
#        count and the next number, within 20 s of the first, and accb acks it;
#   M2.3 accc's watcher is stopped; the Stop guard blocks once, accc waits with a background
#        `pingbus wait`; a review ends it and accc acks, with no notice ever reaching it;
#   M2.4 the human's message mentioning acca only is acked by acca alone;
#   M2.5 a launch on a held seat (75), naming two seats of the team (64), or with a trailing
#        comma (64) is refused, nothing changed;
#   M2.6 the seats are free once their sessions end;
#   M2.7 a later session in accb is the same member and its `pingbus history` holds its past;
#   M2.8 accc removed (parked) and returned by a launch: the same member, its history read;
#   M2.9 a plain `ccy` is on no team;
#   M2.10 after `agent-bus seat remove` of the three: ccy.env.local, .claude/ccy/pingbus/,
#        git status and HEAD as before M2.0.
# M2 NEEDS from the owner: ccy installed with its image current (deploy.bash's last leg); ccy
# launched interactively here once at that version, saving the token and SSH choice the
# sessions reuse (the token by name; its value is never handled here) with every SSH key
# usable unattended (ssh-agent or no passphrase); and no launch of their own naming @acceptance
# here during the run. Every wait is bounded, the longest at 600 s for a ccy launch.
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
# M2 adds the acceptance seats to this checkout's git-ignored .claude/ccy/pingbus/seats/ (removed
# by `agent-bus seat remove`, and deleted first if an interrupted run left them), its sessions'
# containers (found by their ccy-seats label, removed first too) and transcripts (moved into the
# run directory), and three podman members of the team (gone with it).
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
the member it mentions, and a TIMEOUT for an unanswered ping. Slice M2 (unit U20): headless
ccy sessions in this checkout, each launched with --teams <seat>@acceptance; an idle one woken
by its socket (twice at the same count), one whose watcher is gone woken by pingbus wait, a
human message acked by the one it mentions, refused launches, a seat that outlives its
session and returns after removal, a plain ccy on no team, the checkout left as it was.
Then slice U23: an LXC, a docker and a VM member join the same team and exchange a review
and an ack with the desktop's member. Then removes all of it.
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
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-u20.inc.bash
source "${PLAN_SCRIPT_DIR}/_acceptance-u20.inc.bash"
plan_on_cleanup u20_teardown_after_stop
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
plan_deploy_leg "U20: ccy, its image and this checkout's saved launch choices for the M2 sessions; no acceptance seat or its container left by an interrupted run" u20_prerequisites
plan_deploy_leg "the references the pings carry" resolve_reference
plan_deploy_leg "write the ${TEAM} team file" write_acceptance_team_file
plan_deploy_leg "agent-bus-install team ${TEAM}" install_team team
plan_deploy_leg "agent-bus add-member: a (orchestrator) and b (worker)" add_members
plan_deploy_leg "members take their bundles, pass config check and join the team room" place_members
check_leg "M1.1 review sent, received by wait, acked" check_review_ack
check_leg "M1.2 a human message reaches only the member it mentions" check_human_addressed
check_leg "M1.3 TIMEOUT for an unanswered review, none for the acked one" check_timeout
plan_deploy_leg "U20: record this checkout and the ${TEAM} members before M2.0" u20_record
plan_deploy_leg "U20: ccy --teams acca@${TEAM}, accb@${TEAM}, accc@${TEAM} launched in turn; acca made orchestrator" u20_launch_seats
check_leg "M2.0 each launch created its seat: the owner's, git-ignored, a new member with the checkout's host, held" u20_check_seats_created
plan_deploy_leg "U20: each session given its orders, idle with a watcher" u20_give_orders
check_leg "M2.1 an idle ccy session woken by its socket acks a review from a sibling seat" u20_check_review_ack
check_leg "M2.2 a second notice with the same count wakes it again" u20_check_same_count
check_leg "M2.3 a ccy session whose watcher is gone is woken by pingbus wait" u20_check_wait_fallback
check_leg "M2.4 a human message reaches only the ccy session it mentions" u20_check_human_addressed
check_leg "M2.5 a held seat, two seats of one team and a trailing comma are refused, nothing changed" u20_check_refused
plan_deploy_leg "U20: end the three sessions, keep their watcher logs and transcripts" u20_end_seats
check_leg "M2.6 the seats are free once their sessions end" u20_check_seats_free
check_leg "M2.7 a later session in a seat is the same member and reads its history" u20_check_seat_returns
check_leg "M2.8 a seat removed and launched into again returns as the same member, with its history" u20_check_seat_removed_and_returned
check_leg "M2.9 a plain ccy is on no team" u20_check_plain
plan_deploy_leg "U20: agent-bus seat remove ${U20_SEAT_LIST}; the evidence scrubbed" u20_cleanup
check_leg "M2.10 the checkout is as it was before M2.0" u20_check_checkout_unchanged
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
printf '\nACCEPTED: M1 (host-to-host), M2 (ccy members) and U23 (LXC, docker and VM members) passed; nothing of the acceptance team remains\n' | tee -a "${REPORT}"
plan_finish
