#!/usr/bin/env bash
# Plan 00235 — triage.bash: re-measure the rootless podman store the plan's evidence rests on.
#
# PURPOSE: record, on the host, the numbers PLAN.md's facts F1-F9 cite, so they can be
# reproduced and compared before and after the automated cleanup lands:
#   - disk: free space on / and /home, the filesystem they sit on, the store's graph root;
#   - store: image count, `podman system df` (its exit status is a fact: 125 on an unmapped
#     volume), and with --du the store's size on disk;
#   - images: total, dangling, dangling used by no container, their combined size;
#   - dangling images by month of creation;
#   - what the newest dangling images were (title and claude-yolo-version labels);
#   - every tagged image, and which are in use by a container (running or stopped);
#   - containers, and buildah working containers an interrupted build left;
#   - volumes, and which are dangling;
#   - user timers, and whether the deployed launcher removes any image.
# Fact-finding only: it renders no verdict (PlanScriptStandards R9). The probes are in
# triage-probe.bash beside this script.
#
# RUN ON THE HOST, as the desktop user: through CLAUDE/Plan/meta-deploy.bash, or
#   ./CLAUDE/Plan/00235-podman-images-cleaned-up-automatically/triage.bash [--du]
# It never prompts and needs no sudo.
#
# EFFECT ON THE HOST: none. It lists and inspects; it removes, prunes, builds and pulls
# nothing. --du walks the store inside `podman unshare` (read-only, and slow on a large store).
#
# EXIT CODES: 0 every leg established its facts; 1 at least one did not (the leg names
# itself); 64 usage.
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

PLAN_USAGE="usage: triage.bash [--du] [-h|--help]

Read-only, on the HOST: disk free, the rootless podman store (images, dangling images by
month, tags in use, containers, volumes), user timers, and whether the deployed launcher
removes images. --du also measures the store's size on disk (slow). The report is written
to the run directory."

plan_mode gather
plan_parse_common_flags "$@"

WANT_DU=0
for arg in "${PLAN_REMAINING_ARGS[@]}"; do
    case "${arg}" in
        --du) WANT_DU=1 ;;
        *)
            printf '[FATAL] unknown argument: %s\n' "${arg}" >&2
            printf '%s\n' "${PLAN_USAGE}" >&2
            exit 64
            ;;
    esac
done

plan_require_host "it measures the host's disk and its rootless podman store"
plan_start_log auto

PROBE="${PLAN_SCRIPT_DIR}/triage-probe.bash"
REPORT="${PLAN_RUN_DIR}/triage-report.md"
printf '# Plan 00235 triage\n\n' >"${REPORT}"

plan_gather_leg "disk free and the store's location" bash "${PROBE}" disk "${REPORT}"
plan_gather_leg "store summary (podman system df)" bash "${PROBE}" store "${REPORT}"
if [[ "${WANT_DU}" -eq 1 ]]; then
    plan_gather_leg "store size on disk (du)" bash "${PROBE}" du "${REPORT}"
fi
plan_gather_leg "image counts" bash "${PROBE}" images "${REPORT}"
plan_gather_leg "dangling images by month" bash "${PROBE}" by-month "${REPORT}"
plan_gather_leg "newest dangling images: labels" bash "${PROBE}" labels "${REPORT}"
plan_gather_leg "tagged images and their use" bash "${PROBE}" tags "${REPORT}"
plan_gather_leg "containers and buildah leftovers" bash "${PROBE}" containers "${REPORT}"
plan_gather_leg "volumes" bash "${PROBE}" volumes "${REPORT}"
plan_gather_leg "user timers and the launcher's removals" bash "${PROBE}" timers "${REPORT}"

printf '\nREAD THIS FOR: "image counts" (dangling used by no container: what a prune would take)\n'
printf 'and "dangling images by month" (the rate the automation has to keep up with).\n'
plan_finish
