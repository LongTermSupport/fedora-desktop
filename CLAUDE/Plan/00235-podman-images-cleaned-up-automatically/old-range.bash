#!/usr/bin/env bash
# Plan 00235 — old-range.bash
#
# PURPOSE: the old-range files of the rootless podman store (Q6, Decision 4), triaged and
# decided item by item by the host agent; never a reset. HOST ONLY (R2).
#
# EFFECT ON THE HOST: READ ONLY. Builds container-images from this checkout into the run
# directory, then: sudo `old-range scan` (root walks the store once and reads only), and
# `old-range triage` as you: old-range-report.md (each item, what owns it, its proposed action
# and why) and old-range-decisions.proposed.tsv in the run directory. When the decisions file
# exists at its one fixed path, untracked/plan-00235/old-range-decisions.tsv (it names this
# install's images and volumes, so it lives under untracked/), the triage is followed by the
# decisions checked against the store now (`old-range plan`, refused whole when an item is
# missing, still ask, or unsafe) and the dry runs of root's re-own and podman's removals:
# old-range-dry-run-report.txt. No flags, so meta-deploy.list can queue it as a plain entry.
#
# The apply is old-range-apply.bash, which reads the same fixed path.
#
# Usage: ./old-range.bash [-h|--help]
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
PLAN_USAGE="usage: old-range.bash [-h|--help]

Plan 00235 Q6: the rootless podman store's files owned by an older subuid range.
Read only: the triage (old-range-report.md and old-range-decisions.proposed.tsv in the run
directory), and, when ${decisions} exists, that file checked against the store and the dry
runs of the re-own and the removals (old-range-dry-run-report.txt). Copy the proposal to that
path, replace every ask, and run this again; old-range-apply.bash then applies it.
Prompts for sudo once, before the run log opens."

plan_parse_common_flags "$@"
set -- "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"
if [[ $# -gt 0 ]]; then
    printf '[FATAL] unknown argument: %s\n%s\n' "$1" "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it reads the host user's rootless podman store, as root"
plan_prime_sudo
plan_start_log auto

plan_mode gather
bin="${PLAN_RUN_DIR}/container-images"
scan="${PLAN_RUN_DIR}/old-range-scan.json"
login="$(id -un)"

plan_gather_leg "build container-images from this checkout" \
    env GOFLAGS=-mod=vendor GOTOOLCHAIN=local go build -C "${PLAN_REPO_ROOT}/src-go" -trimpath -buildvcs=false \
    -o "${bin}" ./cmd/container-images
steps="${PLAN_SCRIPT_DIR}/old-range-steps.bash"
plan_gather_leg "root's scan of the store (reads only)" \
    bash "${steps}" scan "${bin}" "${login}" "${scan}"
plan_gather_leg "the triage: report and proposed decisions" \
    bash "${steps}" triage "${bin}" "${scan}" "${PLAN_RUN_DIR}/old-range-decisions.proposed.tsv" \
    "${PLAN_RUN_DIR}/old-range-report.md"
if [[ -f "${decisions}" ]]; then
    plan_gather_leg "the decisions checked, and the dry runs of the re-own and the removals" \
        bash "${steps}" dry-run "${bin}" "${scan}" "${decisions}" "${PLAN_RUN_DIR}/old-range-plan.json" \
        "${PLAN_RUN_DIR}/old-range-dry-run-report.txt"
fi
if [[ -f "${decisions}" ]]; then
    printf 'decisions file: %s, checked above; old-range-apply.bash applies it\n' "${decisions}" >&2
else
    printf 'decisions file: %s is absent: copy the proposal there, decide each ask, run this again\n' "${decisions}" >&2
fi
plan_finish
