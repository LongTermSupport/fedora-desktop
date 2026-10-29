#!/usr/bin/env bash
# Plan 00235 — old-range-apply.bash
#
# PURPOSE: apply the host agent's decisions on the rootless podman store's old-range files (Q6,
# Decision 4); never a reset. HOST ONLY (R2).
#
# EFFECT ON THE HOST: CHANGES THE STORE. Runs play-podman.yml with the decisions file at its one
# fixed path, untracked/plan-00235/old-range-decisions.tsv (old-range.bash's triage prints it
# and dry-runs it): behind the store's busy guard, root re-owns each planned directory into the
# current subuid range (setuid bits and file capabilities put back) and removes the stray
# directories decided remove; podman removes, unforced, the images, containers and volumes
# decided remove, each logged to ~/.local/state/fedora-desktop/container-images.log; the store
# repair runs after. Then the play's ownership probe: a clean store gets its store-clean
# marker, the daily prune timer and, the first time, the first prune; anything left (decided
# leave, or held by a running container) keeps the timer held off and fails the play on
# purpose. No flags, so meta-deploy.list can queue it as a plain entry.
#
# Usage: ./old-range-apply.bash [-h|--help]
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

decisions="${PLAN_REPO_ROOT}/untracked/plan-00235/old-range-decisions.tsv"
PLAN_USAGE="usage: old-range-apply.bash [-h|--help]

Plan 00235 Q6: play-podman.yml applies ${decisions}
(root re-owns, podman removes, each logged), then its probe judges the store. Run
old-range.bash first: its dry run of the same file is what this applies.
Prompts for sudo once, before the run log opens."

plan_parse_common_flags "$@"
set -- "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"
if [[ $# -gt 0 ]]; then
    printf '[FATAL] unknown argument: %s\n%s\n' "$1" "${PLAN_USAGE}" >&2
    exit 64
fi
[[ -f "${decisions}" ]] || {
    printf '[FATAL] no decisions file at %s: run old-range.bash, copy its proposal there and decide each ask\n' \
        "${decisions}" >&2
    exit 64
}

plan_require_host "it changes the host user's rootless podman store"
plan_prime_sudo
plan_start_log auto
plan_mode deploy
plan_deploy_leg "play-podman.yml with the old-range decisions ${decisions}" \
    plan_ansible_playbook playbooks/imports/play-podman.yml \
    -e "podman_store_old_range_decisions=${decisions}" -e podman_store_old_range_apply=true
plan_finish
