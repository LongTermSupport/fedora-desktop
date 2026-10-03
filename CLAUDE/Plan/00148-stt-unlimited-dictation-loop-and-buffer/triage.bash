#!/usr/bin/env bash
# triage.bash — Plan 00148 Task 1.2: gather FACTS for continuous dictation (research
# section 3.6). Fact-finding only: renders no verdict (R9).
#
# WHERE TO RUN: on the HOST, in a terminal, as the desktop user, after
# play-speech-to-text.yml has been deployed (it uses the deployed wsi-setting and
# wsi-resolve-model, and the Python packages the play installs).
#
# WHAT IT CHANGES: nothing is configured. It records your microphone once (you read any
# text aloud for RECORD_SECONDS), into this run's directory under untracked/, and replays
# that one recording to every probe, so the probes measure the same speech:
#   1. versions: installed RealtimeSTT, faster-whisper, CTranslate2, onnxruntime; which
#      Silero VAD faster-whisper ships and whether it loads; the CUDA device count;
#   2. real-time factor of the streaming model per 20 s segment (the model `auto` or the
#      Whisper Model setting picks), and a whole-file reference transcript;
#   3. article mode: the recording fed in real time to RealtimeSTT with wsi-article's
#      settings, phrase by phrase as wsi-article reads it, and the words it lost against
#      a reference transcript from the same model.
# The recording is dictated speech: it stays in untracked/ and is never committed.
#
# Usage: ./CLAUDE/Plan/00148-stt-unlimited-dictation-loop-and-buffer/triage.bash
#            [-y|--yes] [--audio FILE.raw] [-h|--help]
#   --audio  reuse a recording from an earlier run (raw 16 kHz mono s16) instead of
#            recording a new one
#   -y       start recording without waiting for "record"
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

RECORD_SECONDS=90
PLAN_USAGE="usage: triage.bash [-y|--yes] [--audio FILE.raw] [-h|--help]"
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

plan_require_host "it measures the host's GPU, its installed speech packages and its microphone"
if [[ "${EUID}" -eq 0 ]]; then
  printf '[FATAL] run this as the desktop user, not root: the packages and settings are per user\n' >&2
  exit 1
fi
plan_start_log auto

REPORT="${PLAN_RUN_DIR}/triage-report.md"
printf '# Plan 00148 triage\n\n' >"${REPORT}"

if [[ -z "${audio}" ]]; then
  audio="${PLAN_RUN_DIR}/speech.raw"
  printf '\nThe probes need %s s of you speaking. Read any text aloud, at your normal pace and\n' "${RECORD_SECONDS}"
  printf 'with your normal pauses, as if dictating. Recording starts when you confirm.\n'
  plan_confirm "Ready to read aloud for ${RECORD_SECONDS} s?" record
  plan_gather_leg "record ${RECORD_SECONDS} s of speech" \
    python3 "${PLAN_SCRIPT_DIR}/probe-record.py" "${audio}" "${RECORD_SECONDS}"
fi
printf -- '- recording: %s\n\n' "${audio}" >>"${REPORT}"

plan_gather_leg "versions" python3 "${PLAN_SCRIPT_DIR}/probe-versions.py" "${REPORT}"
plan_gather_leg "real-time factor per 20 s segment" \
  python3 "${PLAN_SCRIPT_DIR}/probe-rtf.py" "${REPORT}" "${audio}"
plan_gather_leg "article mode word loss at phrase boundaries" \
  python3 "${PLAN_SCRIPT_DIR}/probe-article.py" "${REPORT}" "${audio}"
plan_finish
