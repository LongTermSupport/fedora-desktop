#!/usr/bin/env bash
# Plan 00235 — old-range.bash
#
# PURPOSE: the old-range files of the rootless podman store (Q6, Decision 4), triaged and
# decided item by item by the host agent; never a reset. HOST ONLY (R2).
#
# EFFECT ON THE HOST, by mode:
#   (no flags)        READ ONLY. Builds container-images from this checkout into the run
#                     directory, then: sudo `old-range scan` (root walks the store once and
#                     reads only), and `old-range triage` as you: old-range-report.md (each
#                     item, what owns it, its proposed action and why) and
#                     old-range-decisions.proposed.tsv in the run directory.
#   --decisions FILE  READ ONLY. The triage again, then the decisions checked against the store
#                     now (`old-range plan`, refused whole when an item is missing, still ask,
#                     or unsafe), and the dry runs of root's re-own and podman's removals:
#                     old-range-dry-run-report.txt. FILE must be under untracked/.
#   --decisions FILE --apply
#                     CHANGES THE STORE. Runs play-podman.yml with the decisions: behind the
#                     store's busy guard, root re-owns each planned directory into the current
#                     subuid range (setuid bits and file capabilities put back) and removes the
#                     stray directories decided remove; podman removes, unforced, the images,
#                     containers and volumes decided remove, each logged to
#                     ~/.local/state/fedora-desktop/container-images.log; the store repair
#                     runs after. Then the play's ownership probe: a clean store gets its
#                     store-clean marker, the daily prune timer and, the first time, the first
#                     prune; anything left (decided leave, or held by a running container)
#                     keeps the timer held off and fails the play on purpose, as before.
#
# Usage: ./old-range.bash [-h|--help] [--decisions FILE [--apply]]
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

PLAN_USAGE="usage: old-range.bash [-h|--help] [--decisions FILE [--apply]]

Plan 00235 Q6: the rootless podman store's files owned by an older subuid range.
  (no flags)        the triage, read only: old-range-report.md and
                    old-range-decisions.proposed.tsv in the run directory
  --decisions FILE  the triage, then FILE checked against the store and the dry runs of the
                    re-own and the removals (old-range-dry-run-report.txt); read only.
                    Copy the proposal under untracked/, replace every ask, keep it there.
  --apply           with --decisions: play-podman.yml applies FILE (root re-owns, podman
                    removes, each logged), then its probe judges the store
Prompts for sudo once, before the run log opens."

decisions=""
apply=0
plan_parse_common_flags "$@"
set -- "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"
while [[ $# -gt 0 ]]; do
    case "$1" in
        --decisions)
            [[ $# -ge 2 && -n "$2" ]] || {
                printf '[FATAL] --decisions needs a file\n%s\n' "${PLAN_USAGE}" >&2
                exit 64
            }
            decisions="$2"
            shift 2
            ;;
        --apply)
            apply=1
            shift
            ;;
        *)
            printf '[FATAL] unknown argument: %s\n%s\n' "$1" "${PLAN_USAGE}" >&2
            exit 64
            ;;
    esac
done
if [[ "${apply}" -eq 1 && -z "${decisions}" ]]; then
    printf '[FATAL] --apply needs --decisions FILE\n' >&2
    exit 64
fi
if [[ -n "${decisions}" ]]; then
    [[ -f "${decisions}" ]] || {
        printf '[FATAL] the decisions file %s does not exist\n' "${decisions}" >&2
        exit 64
    }
    decisions="$(realpath "${decisions}")"
    if [[ "${decisions}" != "${PLAN_REPO_ROOT}/untracked/"* ]]; then
        printf '[FATAL] %s is not under %s/untracked/: it names this install'"'"'s images and volumes\n' \
            "${decisions}" "${PLAN_REPO_ROOT}" >&2
        exit 64
    fi
fi

plan_require_host "it reads and changes the host user's rootless podman store"
plan_prime_sudo
plan_start_log auto

if [[ "${apply}" -eq 1 ]]; then
    plan_mode deploy
    plan_deploy_leg "play-podman.yml with the old-range decisions ${decisions}" \
        plan_ansible_playbook playbooks/imports/play-podman.yml \
        -e "podman_store_old_range_decisions=${decisions}" -e podman_store_old_range_apply=true
    plan_finish
fi

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
if [[ -n "${decisions}" ]]; then
    plan_gather_leg "the decisions checked, and the dry runs of the re-own and the removals" \
        bash "${steps}" dry-run "${bin}" "${scan}" "${decisions}" "${PLAN_RUN_DIR}/old-range-plan.json" \
        "${PLAN_RUN_DIR}/old-range-dry-run-report.txt"
fi
plan_finish
