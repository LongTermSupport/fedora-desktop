#!/usr/bin/env bash
# Plan 00232 — acceptance-policy.bash: the pass/fail gate for issue #114 ask 4 and the owner's
# "do not break agent teams" requirement. It is written BEFORE the enforcement exists, so until
# Phase 3 is built it fails (red first). It is not named acceptance.bash yet on purpose: a plan
# folder entry in meta-deploy.list runs acceptance.bash after triage, and this gate needs the two
# sessions that Phase 3's deploy.bash launches. Phase 3 renames it when deploy.bash exists.
#
# It judges live sessions launched under the default policy, read from the host:
#   caps     nothing inside can change the session's network (not privileged; no NET_ADMIN or
#            SYS_ADMIN)
#   held     the policy is held outside the container, in its own network namespace
#   blocked  the host itself and a neighbouring container are refused at once, while a control
#            container without the policy reaches the same listeners
#   allowed  DNS, the forge and the Claude API still work
#   context  the session was told its network limits at start
#   bus      both sessions reach their homeserver, and a pingbus message sent each way arrives
#
# RUN ON THE HOST, as the desktop user:
#   ./CLAUDE/Plan/00232-ccy-network-policy-per-container/acceptance-policy.bash \
#       --session=<container> --peer=<container> --team=<team> [--image=<ref>]
# The two sessions must be THROWAWAY seated ccy sessions of <team>, launched for this check
# (Phase 3's deploy.bash launches them headless): each receives one probe message.
#
# EFFECT ON THE HOST: one throwaway container and a short-lived listener during `blocked`; one
# message each way in <team>'s room during `bus`. Nothing is changed in the sessions.
#
# EXIT CODES: 0 every check passed; 1 at least one failed (each names itself); 64 usage.
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

PLAN_USAGE="usage: acceptance-policy.bash --session=<container> --peer=<container> --team=<team> [--image=<ref>] [-h|--help]

The Plan 00232 gate: a ccy session under the default network policy cannot reach the host or a
neighbour, still reaches DNS, the forge and the Claude API, was told its limits, and still sends
and receives pingbus messages. Host-only, never prompts, no sudo.
  --session=<c>   a throwaway seated ccy session, launched under the default policy
  --peer=<c>      a second one, seated in the same team
  --team=<team>   the team both are seated in
  --image=<ref>   the image of the control container (default claude-yolo:latest)"

plan_mode gather
plan_parse_common_flags "$@"

session=""
peer=""
team=""
image="claude-yolo:latest"
for arg in "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"; do
    case "${arg}" in
        --session=?*) session="${arg#--session=}" ;;
        --peer=?*) peer="${arg#--peer=}" ;;
        --team=?*) team="${arg#--team=}" ;;
        --image=?*) image="${arg#--image=}" ;;
        *)
            printf '[FATAL] unknown argument: %s\n%s\n' "${arg}" "${PLAN_USAGE}" >&2
            exit 64
            ;;
    esac
done
if [[ -z "${session}" ]] || [[ -z "${peer}" ]] || [[ -z "${team}" ]]; then
    printf '[FATAL] --session, --peer and --team are all required\n%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it reads the sessions' network namespaces and runs a control container on the host"
plan_start_log auto

for c in "${session}" "${peer}"; do
    if [[ "$(podman inspect --format '{{.State.Running}} {{index .Config.Labels "ccy"}}' "${c}")" != "true true" ]]; then
        printf '[FATAL] %s is not a running ccy session (label ccy=true)\n' "${c}" >&2
        exit 1
    fi
done

checks="${PLAN_SCRIPT_DIR}/acceptance-checks.bash"
for c in "${session}" "${peer}"; do
    plan_gather_leg "caps: nothing inside ${c} can change its network" "${checks}" caps "${c}"
    plan_gather_leg "held: the policy is in ${c}'s namespace, outside the container" "${checks}" held "${c}"
    plan_gather_leg "context: ${c} was told its network limits" "${checks}" context "${c}"
done
plan_gather_leg "blocked: the host and a neighbour are refused at once" "${checks}" blocked "${session}" "${image}"
plan_gather_leg "allowed: DNS, the forge and the Claude API work" "${checks}" allowed "${session}"
plan_gather_leg "bus: the homeserver is reached and pingbus messages arrive both ways" "${checks}" bus "${session}" "${peer}" "${team}"
plan_finish
