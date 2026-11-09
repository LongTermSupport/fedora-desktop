#!/usr/bin/env bash
# Plan 00234 — gpu-load-acceptance.bash
#
# PURPOSE: render the VERDICT on the fit rule against a real model load (PlanScriptStandards
# R9). The rule (wsi-resolve-model: 1.25 x the download size + 850 MiB) was calibrated on one
# published batch figure; this loads the model the manager preselects, distil-large-v3.5, on
# this machine's GPU twice, as wsi-stream-server does (beam 5) and with wsi-stream's own
# streaming settings (real-time transcription on, which loads its live preview model, tiny.en,
# too), and
# fails if the peak GPU memory either uses is more than the rule's figure. HOST ONLY.
#
# CHANGES: downloads distil-large-v3.5 (about 1.4 GB) if it is not already on disk. Nothing
# else is downloaded (the loads run with HF_HUB_OFFLINE=1; the live preview model is the
# play's download, and the streaming leg fails at once, naming the play, if it is missing)
# and nothing is removed. No sudo, no
# prompts. A warm dictation server's model stays loaded and is counted as already in use.
#
# Usage: ./gpu-load-acceptance.bash [-h|--help]
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

PLAN_USAGE="usage: gpu-load-acceptance.bash [-h|--help]

Plan 00234: loads distil-large-v3.5 on this machine's GPU as the recorders do and checks
the peak GPU memory against wsi-resolve-model's fit rule. Downloads that one model if it
is missing; nothing else. Run play-speech-to-text.yml first (meta-deploy.bash does).

EXIT STATUS
  0  ACCEPTED — both loads stayed within the fit rule's figure
  1  REJECTED — a load needed more, failed, or could not be measured
 64  usage error"

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it loads a model on the host's GPU"
plan_start_log auto

checker="${PLAN_SCRIPT_DIR}/check-gpu-load.py"
plan_deploy_leg "distil-large-v3.5 is on disk (downloaded now if it was not)" python3 "${checker}" download
plan_deploy_leg "a beam-5 transcription, as wsi-stream-server loads it, fits the rule's figure" python3 "${checker}" decode
plan_deploy_leg "wsi-stream's streaming load, live preview model included, fits the rule's figure" python3 "${checker}" streaming
plan_finish
