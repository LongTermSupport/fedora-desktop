#!/usr/bin/env bash
#
# triage-signal.bash — gather grounded FACTS about the operator-signal channel
# the hooks daemon now ships, so the third piece of this plan is unblocked on
# evidence rather than on a release note.
#
# FACTS-ccy-mechanics.md F7 recorded, against the daemon installed at the time,
# that there was "no `notify`, no signal-raising command, and no
# `reboot-warning` anywhere in the daemon tree or the supervisor". A newer
# daemon is now installed. The question this answers is not "has a release
# happened" but "does the capability this plan needs exist, and does it have the
# shape the plan's security argument requires".
#
# Fact-finding only (R9): renders no verdict, ticks no task, and changes nothing
# in this repository. Safe to re-run.
#
# WHERE TO RUN: in the CCY container. Enforced by plan_require_container (R2).
# The facts gathered here are properties of the INSTALLED daemon and of the
# tracked supervisor script in this checkout — the same bytes the implementation
# will be written against. The one thing that genuinely needs the host, live
# delivery to an attached session, is named as NOT ESTABLISHED by the actuator
# probe rather than quietly left out; a container run that reported it as fine
# would be exactly the confident-wrong-answer R2 exists to prevent.
#
# Usage: ./CLAUDE/Plan/00123-ccy-session-registry-and-reboot-restore/triage-signal.bash [-h|--help]
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
[[ -e "${repoRoot}/ansible.cfg" ]] || {
    printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2
    exit 1
}
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

plan_mode gather
plan_require_container 'the installed daemon CLI and this checkout tracked supervisor are the subject; live delivery to a session is deliberately out of scope and reported as unestablished'
plan_start_log auto

REPORT="${PLAN_RUN_DIR}/triage-signal-report.md"

{
    printf '# Plan 00123 — operator-signal channel: gathered facts\n\n'
    printf 'Supersedes nothing on its own. F7 in FACTS-ccy-mechanics.md recorded the\n'
    printf 'signal command as absent; these are the readings taken against the daemon\n'
    printf 'installed now. Facts only, no verdict.\n'
} > "${REPORT}"

plan_gather_leg "daemon identity" bash "${PLAN_SCRIPT_DIR}/probe-signal-identity.bash" "${REPORT}"
plan_gather_leg "writer half — the signal CLI" bash "${PLAN_SCRIPT_DIR}/probe-signal-writer.bash" "${REPORT}"
plan_gather_leg "reader half — the ccy supervisor" bash "${PLAN_SCRIPT_DIR}/probe-signal-actuator.bash" "${REPORT}"

plan_finish
