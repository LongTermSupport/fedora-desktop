#!/usr/bin/env bash
# triage.bash — gather grounded FACTS about the suspend abort and the failure to
# re-suspend. Fact-finding only: renders no verdict (R9) and changes nothing. Read-only and
# safe to re-run on a live system, mid-incident.
#
# WHERE TO RUN: on the HOST, in a terminal. Enforced by plan_require_host (R2) — the CCY
# container has no host journal, no /sys/power and no logind, so a result obtained there is
# not evidence about the laptop.
#
# The run report is RAW host state and lands in a gitignored triage-runs/ directory.
# The sanitised, committed transcription is TRIAGE-EVIDENCE.md — that is the file to read
# from the container.
#
# Usage: ./CLAUDE/Plan/00104-suspend-aborts-on-dock-unplug-never-resuspends/triage.bash [-h]
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

PLAN_USAGE="usage: triage.bash [--watch-power [seconds]] [-h|--help]

Gathers suspend/resume, lid, wakeup-source and power-policy facts from this host.
Read-only. Writes a raw report into a gitignored triage-runs/<timestamp>/ directory.

  --watch-power [seconds]   Additionally watch a LIVE mains unplug/replug and record what
                            logind does across it (default timeout 120s per leg). Passive
                            by default because this leg waits on a human. Nothing suspends
                            and nothing is reconfigured — safe to run while working."

plan_mode gather
plan_parse_common_flags "$@"

# Active probes go behind an explicit flag so the common case stays instant
# (CLAUDE/PlanTriage.md, "Active probes get a flag").
WATCH_POWER=0
WATCH_POWER_TIMEOUT=120
_expect_timeout=0
for _arg in ${PLAN_REMAINING_ARGS+"${PLAN_REMAINING_ARGS[@]}"}; do
    if [[ "${_expect_timeout}" == "1" ]] && [[ "${_arg}" =~ ^[0-9]+$ ]]; then
        WATCH_POWER_TIMEOUT="${_arg}"
        _expect_timeout=0
        continue
    fi
    _expect_timeout=0
    case "${_arg}" in
        --watch-power) WATCH_POWER=1; _expect_timeout=1 ;;
        *)
            printf '[FATAL] unknown argument: %s\n\n%s\n' "${_arg}" "${PLAN_USAGE}" >&2
            exit 1
            ;;
    esac
done

plan_require_host "it reads the host journal, /sys/power, logind and the GNOME session bus"
plan_start_log auto

REPORT="${PLAN_RUN_DIR}/triage-report.md"      # listed by plan_finish (R10)
plan_gather_leg "suspend / lid / wakeup facts" \
    bash "${PLAN_SCRIPT_DIR}/probe-suspend.bash" "${REPORT}"

if [[ "${WATCH_POWER}" == "1" ]]; then
    plan_gather_leg "live AC power transition" \
        bash "${PLAN_SCRIPT_DIR}/probe-power-transition.bash" "${REPORT}" "${WATCH_POWER_TIMEOUT}"
fi

plan_finish
