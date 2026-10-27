#!/usr/bin/env bash
# Plan 00232 — acceptance.bash: the pass/fail gate for issue #114 ask 4 and the owner's "do not
# break agent teams" requirement. meta-deploy.bash runs it after deploy.bash (the play that
# installs nftables and ccy-network-policy).
#
# It launches two THROWAWAY seated headless ccy sessions in this checkout, np232a and np232b of
# one team, under the default policy, judges them from the host, and removes them:
#   caps     nothing inside can change the session's network (not privileged; no NET_ADMIN or
#            SYS_ADMIN)
#   held     the policy is held outside the container, in its own network namespace
#   context  the session was told its network limits at start
#   blocked  the host itself and a neighbouring container are refused at once, while a control
#            container without the policy reaches the same listeners
#   allowed  DNS, the forge and the Claude API still work
#   bus      both sessions reach their homeserver, and a pingbus message sent each way arrives
#
# The sessions are started and ended here, not by deploy.bash: a session must not outlive the
# script that can remove it. Each runs claude reading stream-json from a fifo this script holds,
# and is sent no turn, so no model time is spent; closing the fifo ends it.
#
# The team is --team=<team>, else the team of this checkout's first seat (`agent-bus seat list`):
# a team that covers this checkout and whose homeserver runs here. With none, COULD NOT
# ESTABLISH (exit 2): launch one seated session here first (ccy --teams).
#
# RUN ON THE HOST, as the desktop user (through meta-deploy.bash):
#   ./CLAUDE/Plan/00232-ccy-network-policy-per-container/acceptance.bash [--team=<team>] [--image=<ref>]
#
# EFFECT ON THE HOST: two seats np232a and np232b made in the team (sudo agent-bus add-member,
# through the launch) and parked again at the end (agent-bus seat remove); two headless ccy
# sessions for the run's length; one throwaway control container and a short-lived listener
# during `blocked`; one probe message each way in the team's room during `bus`. The checkout's
# Quick Launch record is saved first and put back, since a seated launch rewrites it.
#
# EXIT CODES: 0 every check passed; 1 at least one failed (each names itself), or the sessions
# could not be started or removed; 2 COULD NOT ESTABLISH (no team, no saved Quick Launch); 64
# usage.
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
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_image-build.inc.bash
source "${PLAN_REPO_ROOT}/CLAUDE/Plan/_image-build.inc.bash"

PLAN_USAGE="usage: acceptance.bash [--team=<team>] [--image=<ref>] [-h|--help]

The Plan 00232 gate: two throwaway seated headless ccy sessions under the default network
policy cannot reach the host or a neighbour, still reach DNS, the forge and the Claude API,
were told their limits, and still send and receive pingbus messages; then both are removed.
Host-only; prompts for sudo once, before the run log opens (the seats are made and parked).
  --team=<team>   the team to seat them in (default: the team of this checkout's first seat)
  --image=<ref>   the image of the control container (default claude-yolo:latest)
Exit 0 every check passed, 1 a check failed, 2 COULD NOT ESTABLISH, 64 usage."

plan_mode deploy
plan_parse_common_flags "$@"

team=""
image="claude-yolo:latest"
for arg in "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"; do
    case "${arg}" in
        --team=?*) team="${arg#--team=}" ;;
        --image=?*) image="${arg#--image=}" ;;
        *)
            printf '[FATAL] unknown argument: %s\n%s\n' "${arg}" "${PLAN_USAGE}" >&2
            exit 64
            ;;
    esac
done
if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '[FATAL] --check has nothing to rehearse here: the checks need real sessions\n' >&2
    exit 64
fi

plan_require_host "it launches ccy sessions, reads their network namespaces and runs a control container on the host"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: the seats use sudo for each root step\n' >&2
    exit 1
fi

readonly SEAT_A="np232a"
readonly SEAT_B="np232b"
readonly CCY_LAUNCHER="/var/local/claude-yolo/claude-yolo"
readonly AGENT_BUS="/usr/local/bin/agent-bus"
readonly LAST_LAUNCH="${PLAN_REPO_ROOT}/.claude/ccy/.last-launch.conf"
readonly CHECKS="${PLAN_SCRIPT_DIR}/acceptance-checks.bash"
readonly START_S=600 # a launch's wait for its container, counted from the end of any image build
readonly READY_S=120 # the entrypoint's claim of the seat, once the container runs
readonly EXIT_S=60   # a session's end once its input is closed
readonly POLL_S=3

# The prerequisites change nothing, so a missing one stops the run before sudo or the log.
for tool in "${CCY_LAUNCHER}" "${AGENT_BUS}" "${HOME}/.local/bin/ccy-network-policy"; do
    if [[ ! -x "${tool}" ]]; then
        printf '[FATAL] %s is not installed: deploy.bash (play-claude-yolo.yml) or play-agent-bus.yml installs it\n' "${tool}" >&2
        exit 1
    fi
done
if [[ ! -f "${LAST_LAUNCH}" ]]; then
    printf 'COULD NOT ESTABLISH: this checkout has no saved Quick Launch (%s), whose token and keys a headless launch takes; launch ccy here once with a terminal first\n' "${LAST_LAUNCH}" >&2
    exit 2
fi
seat_cmd() { (cd -- "${PLAN_REPO_ROOT}" && "${AGENT_BUS}" seat "$@"); }
if [[ -z "${team}" ]]; then
    # A SEAT line is: SEAT <team> <seat> <state> ... <handle>, tab-separated.
    team="$(seat_cmd list | awk -F '\t' -v a="${SEAT_A}" -v b="${SEAT_B}" '$1 == "SEAT" && $3 != a && $3 != b { print $2; exit }')"
fi
if [[ -z "${team}" ]]; then
    printf 'COULD NOT ESTABLISH: this checkout has no seat in any team; launch one seated session here (ccy --teams), or give --team=<team>\n' >&2
    exit 2
fi
readonly TEAM="${team}"

plan_prime_sudo
plan_start_log auto
printf '==> team %s, throwaway seats %s and %s\n' "${TEAM}" "${SEAT_A}" "${SEAT_B}"

# ── the two sessions ────────────────────────────────────────────────────────────────────
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-sessions.inc.bash
source "${PLAN_SCRIPT_DIR}/_acceptance-sessions.inc.bash"
plan_on_cleanup teardown

# check <name> <cmd...> — one check; every check runs, and any failure makes the run exit 1.
FAILED_CHECKS=""
check() {
    local name="$1"
    shift
    printf '\n==> [check] %s\n' "${name}"
    if "$@"; then
        printf 'PASS  %s\n' "${name}"
    else
        printf 'FAIL  %s\n' "${name}" >&2
        FAILED_CHECKS+="${FAILED_CHECKS:+; }${name}"
    fi
}

plan_deploy_leg "park throwaway seats an interrupted run left" park_seats
plan_deploy_leg "launch ${SEAT_A} and ${SEAT_B}, headless and seated in ${TEAM}, under the default policy" start_both

a="${CTR[${SEAT_A}]}"
b="${CTR[${SEAT_B}]}"
for c in "${a}" "${b}"; do
    check "caps: nothing inside ${c} can change its network" "${CHECKS}" caps "${c}"
    check "held: the policy is in ${c}'s namespace, outside the container" "${CHECKS}" held "${c}"
    check "context: ${c} was told its network limits" "${CHECKS}" context "${c}"
done
check "blocked: the host and a neighbour are refused at once" "${CHECKS}" blocked "${a}" "${image}"
check "allowed: DNS, the forge and the Claude API work" "${CHECKS}" allowed "${a}"
check "bus: the homeserver is reached and pingbus messages arrive both ways" "${CHECKS}" bus "${a}" "${b}" "${TEAM}" "${SEAT_A}" "${SEAT_B}"

plan_deploy_leg "end both sessions, park their seats, restore the Quick Launch record" teardown
if [[ -n "${FAILED_CHECKS}" ]]; then
    printf '\nREJECTED: %s\n' "${FAILED_CHECKS}" >&2
    exit 1
fi
printf '\nACCEPTED: both sessions held the policy, were refused the host and a neighbour, and reached DNS, the forge, the API and each other over the bus; both removed\n'
plan_finish
