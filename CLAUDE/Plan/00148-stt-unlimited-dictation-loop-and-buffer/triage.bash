#!/usr/bin/env bash
# triage.bash — Plan 00148 Task 1.2: gather FACTS for continuous dictation (research
# section 3.6). Fact-finding only: renders no verdict (R9).
#
# WHERE TO RUN: on the HOST, in a terminal, as the desktop user, after
# play-speech-to-text.yml has been deployed (it uses the deployed wsi-setting and
# wsi-resolve-model, and the Python packages the play installs).
#
# WHAT IT CHANGES: nothing. It records nothing, loads no model and needs nobody at the
# microphone: a probe's own copy of the model does not fit on the GPU beside the warm
# server's, so the speed of dictation is read from the figures the server logs for every
# dictation you make. Legs:
#   1. versions: installed RealtimeSTT, faster-whisper, CTranslate2, onnxruntime; which
#      Silero VAD faster-whisper ships and whether it loads (on the CPU); the CUDA count;
#   2. real dictation: per continuous dictation the server finished, the real-time factor
#      (mean and worst segment), the worst backlog, and how often a hard cut (no pause by
#      28 s) landed inside speech. Turn on Continuous Dictation and dictate first;
#   3. with --audio only: how the checkout's segmenter and Silero VAD adapter cut an earlier
#      recording (raw 16 kHz mono s16), segment by segment, on the CPU.
#
# Usage: ./CLAUDE/Plan/00148-stt-unlimited-dictation-loop-and-buffer/triage.bash
#            [--audio FILE.raw] [-h|--help]
set -euo pipefail
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
  if [[ -e "${repoRoot}/.git" ]]; then
    printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
    exit 1
  fi
  repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || { printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2; exit 1; }
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="usage: triage.bash [--audio FILE.raw] [-h|--help]"
plan_mode gather
plan_parse_common_flags "$@"

audio=""
set -- "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --audio)
      [[ $# -ge 2 ]] || { printf '[FATAL] --audio needs a file\n%s\n' "${PLAN_USAGE}" >&2; exit 2; }
      audio="$(realpath -e "$2")"
      shift 2
      ;;
    *)
      printf '[FATAL] unknown argument: %s\n%s\n' "$1" "${PLAN_USAGE}" >&2
      exit 2
      ;;
  esac
done

plan_require_host "it reads the host's installed speech packages and its speech server's log"
if [[ "${EUID}" -eq 0 ]]; then
  printf '[FATAL] run this as the desktop user, not root: the packages and settings are per user\n' >&2
  exit 1
fi
plan_start_log auto

REPORT="${PLAN_RUN_DIR}/triage-report.md"
printf '# Plan 00148 triage\n\n' >"${REPORT}"

plan_gather_leg "versions" python3 "${PLAN_SCRIPT_DIR}/probe-versions.py" "${REPORT}"
plan_gather_leg "real dictation, from the server's own figures" \
  python3 "${PLAN_SCRIPT_DIR}/probe-dictations.py" "${REPORT}"
if [[ -n "${audio}" ]]; then
  printf -- '- recording: %s\n\n' "${audio}" >>"${REPORT}"
  plan_gather_leg "how the segmenter cuts the recording" \
    python3 "${PLAN_SCRIPT_DIR}/probe-segments.py" "${REPORT}" "${audio}" "${PLAN_REPO_ROOT}"
fi
plan_finish
