#!/usr/bin/env bash
# Plan 00234 — acceptance.bash
#
# PURPOSE: render the VERDICT on the installed speech-to-text model manager's GPU fit
# (CLAUDE/PlanScriptStandards.md R9): the installed wsi-resolve-model and wsi-model-manager
# are the checkout's, `wsi-resolve-model --fit` reads this machine's real GPU through the
# real nvidia-smi and CTranslate2, and the installed manager, given that answer, preselects
# an English-only model that fits and refuses every one that does not. The unit tests fake
# nvidia-smi and CTranslate2; this runs the real ones. HOST ONLY.
#
# Read-only: nothing is downloaded or removed. No sudo, no prompts. The manager's screen
# itself (the GPU memory column, the cursor on the preselected row, the refusal shown when a
# too-big model is chosen) needs a person: that is the owner's manual check in PLAN.md.
#
# Usage: ./acceptance.bash [-h|--help]
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

PLAN_USAGE="usage: acceptance.bash [-h|--help]

The Plan 00234 acceptance gate. Run play-speech-to-text.yml first (meta-deploy.bash does).
It asks the installed wsi-resolve-model which models fit this machine's GPU, and the
installed wsi-model-manager which it would preselect and refuse. Nothing is downloaded.

EXIT STATUS
  0  ACCEPTED — every check ran and passed
  1  REJECTED — a check failed, or did not run
 64  usage error"

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it checks the model manager and resolver the host's play installed, against the host's GPU"
plan_start_log auto

checker="${PLAN_SCRIPT_DIR}/check-fit.py"
plan_gather_leg "the installed resolver and manager are the checkout's" python3 "${checker}" deployed
plan_gather_leg "wsi-resolve-model --fit names this machine's GPU and its memory (or the CPU, with no NVIDIA device)" python3 "${checker}" fit
plan_gather_leg "the manager preselects an English-only model that fits and refuses each one that does not" python3 "${checker}" preselect
plan_finish
