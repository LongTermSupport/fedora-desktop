#!/usr/bin/env bash
# Plan 00246 Task 8.8 — x30-triage.bash: what the X30 VM lab rests on that no container can check.
#
# PURPOSE: the lab verbs `vmtest link-up` and `member-up --link` join two lab guests with a
# libvirt `udp` interface: a pair of loopback UDP sockets, no libvirt network, bridge or root.
# That interface was built to a design, and nothing in the container shows that this host's
# virt-install spells it that way, that libvirt's session accepts it, or what QEMU command line
# libvirt makes of it. This script settles that before the first `member-up --link`:
#   versions   virt-install, libvirt, QEMU and passt versions; host memory and the lab's disk;
#   options    what `virt-install --network=?` says --network takes;
#   udp        the domain a member with a passt NIC and a udp NIC would have, printed (never
#              defined) with the design's spelling and, if that is refused, a shorter one;
#              checked against libvirt's schema, and converted to the QEMU command line it
#              would run (virsh domxml-to-native), with a check that no guest was defined;
#   routes     the controller's addresses, routes and policy rules, for choosing the lab's
#              subnets (which are input, never defaults).
# Fact-finding only: it renders no verdict (PlanScriptStandards R9). The legs are in
# x30-probe.bash beside this script.
#
# RUN ON THE HOST, as the desktop user (not root): through CLAUDE/Plan/meta-deploy.bash, or
#   ./CLAUDE/Plan/00246-agent-bus-v2-landing-and-cross-network/x30-triage.bash
# It never prompts and needs no sudo: the lab is rootless (qemu:///session).
#
# EFFECT ON THE HOST: none. It prints and reads; virt-install runs with --print-xml, so no
# guest, disk or network is made; the printed domains and a report stay in this run's directory,
# which is untracked.
#
# EXIT CODES: 0 every leg ran to the end; 1 at least one leg did not (it names itself; the
# fact-finding is incomplete, the lab is not judged); 64 usage.
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

PLAN_USAGE="usage: x30-triage.bash [-h|--help]

Host facts for the X30 VM lab: the virt-install, libvirt and QEMU versions, what --network
takes, the udp interface checked and converted without defining a guest, and the controller's
routes. Host-only, read-only, never prompts, no sudo.
Writes x30-triage-report.md and the printed domains into its run directory and names them."

plan_mode gather
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument: %s\n%s\n' "${PLAN_REMAINING_ARGS[0]}" "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it asks the host's own virt-install, libvirt session, QEMU and routes"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: the lab is rootless, its libvirt is the user session\n' >&2
    exit 1
fi
plan_start_log auto

REPORT="${PLAN_RUN_DIR}/x30-triage-report.md"
printf '# Plan 00246 X30 lab triage: host facts\n\nRun directory: %s\n\n' "${PLAN_RUN_DIR}" >"${REPORT}"

probe=(bash "${PLAN_SCRIPT_DIR}/x30-probe.bash")
plan_gather_leg "versions, memory and disk" "${probe[@]}" versions "${REPORT}"
plan_gather_leg "what virt-install --network takes" "${probe[@]}" options "${REPORT}"
plan_gather_leg "the udp interface" "${probe[@]}" udp "${REPORT}" "${PLAN_RUN_DIR}"
plan_gather_leg "the controller's routes" "${probe[@]}" routes "${REPORT}"
plan_finish
