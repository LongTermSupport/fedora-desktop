#!/usr/bin/env bash
# Plan 00232 — triage.bash: gather the facts a per-container ccy network policy rests on (issue
# #114). Fact-finding only: it renders no verdict (PlanScriptStandards R9).
#
# Legs:
#   host facts (always, PASSIVE, read-only): the podman network stack and its configuration, the
#     host's addresses and routes, the agent bus address and seat bundles, every live ccy
#     session's capabilities, networks and nameservers, the pasta processes, the deployed
#     launcher's network default, and whether this host's nft parses the candidate rulesets.
#   enforcement probes (--active only): on THROWAWAY containers, whether the host can apply nft
#     rules inside a created-but-not-started container's own network namespace (and needs
#     `podman unshare` for it), what drop, ICMP admin-prohibited and TCP-reset look like to curl,
#     nc and ssh inside, whether the container can lift them, the create-init-apply-start launch
#     sequence with --rm, ccy --connect after the policy is in place, the shared rootless
#     namespace's ruleset (listed only) and IPv6 inside. Its header lists every side effect.
#
# RUN ON THE HOST, as the desktop user (not root), through CLAUDE/Plan/meta-deploy.bash or:
#   ./CLAUDE/Plan/00232-ccy-network-policy-per-container/triage.bash [--active]
#       [--image=<ref>] [--bus-url=<url>]...
# It never prompts and needs no sudo.
#
# EFFECT ON THE HOST: none without --active. With --active, only throwaway containers, one
# throwaway network and a short-lived host listener, all removed on the way out (see
# triage-enforce-probes.bash); no running session is touched, and nothing is written to the
# host's own firewall or to the shared rootless network namespace.
#
# EXIT CODES: 0 every leg gathered its facts; 1 a leg did not (it names itself; the fact-finding
# is incomplete, nothing is judged); 64 usage.
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

PLAN_USAGE="usage: triage.bash [--active] [--image=<ref>] [--bus-url=<url>]... [-h|--help]

Facts for Plan 00232 (a per-container ccy network policy). Host-only, never prompts, no sudo.
  --active          also run the enforcement probes on throwaway containers
  --image=<ref>     the image the throwaway containers run (default claude-yolo:latest, the
                    image every ccy session runs; it must be present)
  --bus-url=<url>   a team homeserver base_url to probe under each candidate policy (repeat for
                    more); default: every base_url in this checkout's seat bundles
Writes host-facts-report.txt (and enforce-report.txt with --active) into its run directory."

plan_mode gather
plan_parse_common_flags "$@"

active=0
image="claude-yolo:latest"
busUrls=()
for arg in "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"; do
    case "${arg}" in
        --active) active=1 ;;
        --image=?*) image="${arg#--image=}" ;;
        --bus-url=http://?* | --bus-url=https://?*) busUrls+=("${arg#--bus-url=}") ;;
        *)
            printf '[FATAL] unknown or malformed argument: %s\n%s\n' "${arg}" "${PLAN_USAGE}" >&2
            exit 64
            ;;
    esac
done

plan_require_host "it probes the host's rootless podman, its network namespaces and its addresses"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: ccy sessions are rootless podman containers of that user\n' >&2
    exit 1
fi

# Every throwaway this run makes carries the ccy-netpolicy-triage label; this sweep is the
# backstop for the leg's own cleanup.
sweep_triage_objects() {
    local -a ids=()
    mapfile -t ids < <(podman ps -aq --filter label=ccy-netpolicy-triage)
    if [[ "${#ids[@]}" -gt 0 ]]; then
        printf 'removing %d leftover triage container(s)\n' "${#ids[@]}"
        podman rm -f "${ids[@]}"
    fi
    mapfile -t ids < <(podman network ls -q --filter label=ccy-netpolicy-triage)
    if [[ "${#ids[@]}" -gt 0 ]]; then
        printf 'removing %d leftover triage network(s)\n' "${#ids[@]}"
        podman network rm "${ids[@]}"
    fi
}
plan_on_cleanup sweep_triage_objects

plan_start_log auto
# A run killed hard (SIGKILL, a host crash) left its throwaways; remove them before starting.
sweep_triage_objects

# The checkout's seat directory is the product's (helpers/pingbus/seat.py CHECKOUT_SEATS); read, never copied.
seatsDir="${PLAN_REPO_ROOT}/$(python3 -P "${PLAN_REPO_ROOT}/CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/m7_check.py" product-value helpers/pingbus/seat.py CHECKOUT_SEATS)"
if [[ "${#busUrls[@]}" -eq 0 ]] && [[ -d "${seatsDir}" ]]; then
    mapfile -t busUrls < <(find "${seatsDir}" -name member.json -type f \
        -exec jq -r '.base_url // empty' {} + | sort -u)
fi
busUrlCsv="$(IFS=,; printf '%s' "${busUrls[*]+"${busUrls[*]}"}")"
printf 'bus URLs to probe: %s\n' "${busUrlCsv:-none}"

plan_gather_leg "host facts (passive)" "${PLAN_SCRIPT_DIR}/triage-host-facts.bash" \
    "${PLAN_RUN_DIR}/host-facts-report.txt" "${PLAN_REPO_ROOT}"
if [[ "${active}" -eq 1 ]]; then
    plan_gather_leg "enforcement probes (active, throwaway containers)" "${PLAN_SCRIPT_DIR}/triage-enforce-probes.bash" \
        "${PLAN_RUN_DIR}/enforce-report.txt" "${image}" "$(date +%s)-$$" "${busUrlCsv}"
else
    printf '\n(--active not given: the enforcement probes did not run)\n'
fi

printf '\nREAD FIRST: enforce-report.txt sections A3 (what each verdict looks like inside), A4\n'
printf '(whether the container can lift it) and A10 (whether a read-only bound file can be\n'
printf 'replaced), then host-facts-report.txt P5 and P6 (what every session must keep reaching:\n'
printf 'the bus address, the nameservers, the joined networks).\n'
plan_finish
