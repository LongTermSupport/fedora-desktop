#!/usr/bin/env bash
# Plan 00169 — triage.bash (Task 1.1): the facts about git versions and the ccy image's base.
#
# PURPOSE: record, on the host, what the plan rests on:
#   - the host's git version;
#   - this repository's core.repositoryformatversion and every extensions.* key;
#   - each local claude-yolo image's git version and claude-yolo-version label;
#   - the local node:lts-slim's digests against the registry's current digest for the tag,
#     and whether each claude-yolo image is built on the local node:lts-slim's layers;
#   - whether the deployed launcher's builds and play-claude-yolo.yml's build pass --pull
#     (expected not, so FROM is never re-pulled).
# Fact-finding only: it renders no verdict (PlanScriptStandards R9). The probes are in
# triage-probe.bash beside this script.
#
# RUN ON THE HOST, as the desktop user: through CLAUDE/Plan/meta-deploy.bash, or
#   ./CLAUDE/Plan/00169-ccy-image-keeps-its-tools-current/triage.bash
# It never prompts and needs no sudo.
#
# EFFECT ON THE HOST: nothing persistent. It runs `git --version` in throwaway --rm
# containers with no network, and asks Docker Hub for one digest with a HEAD request. It
# pulls and builds nothing.
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

PLAN_USAGE="usage: triage.bash [-h|--help]

Read-only, on the HOST: git versions (host and every claude-yolo image), this repository's
extensions.* keys, the image's node:lts-slim base against the registry, and whether builds
pass --pull. The report is written to the run directory."

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it probes the host's git, its podman images and the deployed launcher"
plan_start_log auto

PROBE="${PLAN_SCRIPT_DIR}/triage-probe.bash"
REPORT="${PLAN_RUN_DIR}/triage-report.md"
printf '# Plan 00169 triage\n\n' >"${REPORT}"

plan_gather_leg "host git version" bash "${PROBE}" host-git "${PLAN_REPO_ROOT}" "${REPORT}"
plan_gather_leg "repository extensions" bash "${PROBE}" extensions "${PLAN_REPO_ROOT}" "${REPORT}"
plan_gather_leg "claude-yolo images: git and version label" bash "${PROBE}" images "${PLAN_REPO_ROOT}" "${REPORT}"
plan_gather_leg "node:lts-slim base against the registry" bash "${PROBE}" base-digest "${PLAN_REPO_ROOT}" "${REPORT}"
plan_gather_leg "builds and --pull" bash "${PROBE}" build-pull "${PLAN_REPO_ROOT}" "${REPORT}"

plan_finish
