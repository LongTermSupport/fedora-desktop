#!/usr/bin/env bash
# Plan 00228 — acceptance.bash
#
# PURPOSE: the verdict on the consumer install in the VM lab, and on the two paths it must
# not have changed. Runs three scenarios, each on a throwaway guest, from this checkout's
# HEAD (which must be pushed: the guest fetches it from the public repository):
#
#   desktop-consumer-install  an attended ./run.bash --consumer, its re-run, 17 guest checks
#   desktop-fresh-install     the developer desktop path, unchanged
#   server-fast-provision     the headless server path, unchanged
#
# HOST ONLY (R2): the lab is the host's rootless libvirt. Before the runs it ensures the two
# bases (vmtest ensure-base: built if absent, rebuilt if the freshness gate refuses it, which
# for the desktop base is an Anaconda install). Nothing else on the host changes; each run's
# guest is destroyed by vmtest, and a run that does not pass keeps its overlay for diagnosis.
#
# Exit 0 = ACCEPTED (all three pass), 1 = REJECTED (a run's verdict is fail or error),
# 2 = COULD NOT ESTABLISH (HEAD not pushed, vmtest absent, a base not ensured, or a run that
# gave no verdict).
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

Plan 00228 Task 3.2, the verdict: vmtest runs desktop-consumer-install, desktop-fresh-install
and server-fast-provision from this checkout's pushed HEAD, after ensuring the desktop and
server-fast bases. Exit 0 = ACCEPTED, 1 = REJECTED, 2 = COULD NOT ESTABLISH."

plan_mode gather
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -ne 0 || "${PLAN_CHECK}" == "1" ]]; then
    printf '[FATAL] unexpected argument (this script takes none, and --check has nothing to rehearse)\n%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it boots guests in the host's rootless libvirt lab"
plan_start_log auto

readonly VMTEST="${HOME}/.local/bin/vmtest"
readonly REPORT="${PLAN_RUN_DIR}/acceptance-report.md"
# scenario:base, in run order. The consumer run first: it is what this plan adds.
readonly SCENARIOS=(desktop-consumer-install:desktop desktop-fresh-install:desktop server-fast-provision:server-fast)

out() { printf '%s\n' "$1" | tee -a "${REPORT}"; }

could_not_establish() {
    out "**COULD NOT ESTABLISH** — $1"
    printf '==> report: %s\n' "${REPORT}"
    exit 2
}

out "# Plan 00228 acceptance"
out ""
if [[ ! -x "${VMTEST}" ]]; then
    could_not_establish "${VMTEST} is not deployed (run deploy.bash first)"
fi
head="$(git -C "${PLAN_REPO_ROOT}" rev-parse HEAD)"
remotes="$(git -C "${PLAN_REPO_ROOT}" branch -r --contains "${head}")"
if [[ -z "${remotes}" ]]; then
    could_not_establish "HEAD ${head:0:12} is on no remote branch; the guest fetches the commit from the public repository, so push it first"
fi
out "commit: ${head} (on $(printf '%s' "${remotes}" | tr -s ' \n' ' '))"
out ""

ensured=" "
for entry in "${SCENARIOS[@]}"; do
    base="${entry#*:}"
    if [[ "${ensured}" != *" ${base} "* ]]; then
        printf '==> vmtest ensure-base %s\n' "${base}"
        if ! "${VMTEST}" ensure-base "${base}"; then
            could_not_establish "vmtest ensure-base ${base} failed; see the run log"
        fi
        ensured="${ensured}${base} "
    fi
done

passed=0
rejected=0
for entry in "${SCENARIOS[@]}"; do
    scenario="${entry%%:*}"
    printf '==> vmtest run %s --commit %s\n' "${scenario}" "${head:0:12}"
    # vmtest exits 0 only on pass; its VMTEST-RUN line is the verdict either way.
    runOut=""
    runRc=0
    if runOut="$("${VMTEST}" run "${scenario}" --commit "${head}")"; then
        runRc=0
    else
        runRc=$?
    fi
    runLine=""
    if ! runLine="$(printf '%s\n' "${runOut}" | grep -E '^VMTEST-RUN ')"; then
        could_not_establish "${scenario}: vmtest run printed no VMTEST-RUN line (exit ${runRc}); see the run log"
    fi
    # VMTEST-RUN <run-id> verdict=<v> response=<path>
    read -r _ runId verdictField responseField <<<"${runLine}"
    verdict="${verdictField#verdict=}"
    if [[ "${verdict}" == "pass" && "${runRc}" -eq 0 ]]; then
        passed=$((passed + 1))
        out "- ${scenario}: **pass** (run ${runId}, ${responseField#response=})"
    else
        rejected=$((rejected + 1))
        out "- ${scenario}: **${verdict}** (exit ${runRc}, run ${runId}, ${responseField#response=})"
    fi
done

out ""
out "COVERAGE: ${passed} of ${#SCENARIOS[@]} scenarios passed, ${rejected} did not"
printf '==> report: %s\n' "${REPORT}"
if [[ "${rejected}" -ne 0 || "${passed}" -ne "${#SCENARIOS[@]}" ]]; then
    out "**REJECTED**"
    exit 1
fi
out "**ACCEPTED**"
exit 0
