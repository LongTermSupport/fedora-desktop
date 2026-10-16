#!/usr/bin/env bash
# acceptance.bash — Plan 00203's host acceptance, one milestone per sourced file. This is the
# dispatcher: bootstrap, flags, the run log, the report and check_leg. Each milestone's
# _acceptance-<milestone>.inc.bash holds its own constants, legs, checks, cleanup and runner;
# ACCEPTANCE.md has a section per milestone.
#   m2      _acceptance-m2.inc.bash      the bus kit in every ccy session (M2 a-f)
#   m1      _acceptance-m1.inc.bash      the seat keeper (M1 a-b); uses M2's session launch
#   robust  _acceptance-robust.inc.bash  server-side robustness (gaps 3, 4, 9, 10), R1-R9
#
# WHERE TO RUN: on the HOST, as the desktop user (sudo for each root step), on an SELinux
# Enforcing host, after play-claude-yolo.yml (ccy, ccy-teams, the image) and play-agent-bus.yml
# (the units, the installer, the admin zipapp) have run. Through meta-deploy.bash.
#
# EXIT CODES: 0 ACCEPTED; 1 REJECTED (a check or a setup step failed, and names itself);
# 64 usage.
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

Plan 00203's host acceptance, each milestone in turn (ACCEPTANCE.md): M2, the bus kit in every
ccy session, on two throwaway headless sessions in ccy's tmux (plain and --no-bus); M1, the
seat keeper (the helper tests, then a third throwaway plain session); then server-side
robustness, R1-R9, on the throwaway team acc-robust. Both are removed on the way
out. Exit 0 ACCEPTED, 1 REJECTED, 64 usage. Prompts for sudo once, before the run log opens."

plan_mode deploy
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument: %s\n%s\n' "${PLAN_REMAINING_ARGS[0]}" "${PLAN_USAGE}" >&2
    exit 64
fi
if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '[FATAL] --check has nothing to rehearse here: the checks need real sessions and a real team\n' >&2
    exit 64
fi
plan_require_host "it starts ccy sessions, builds a homeserver team and kills, freezes and restarts its systemd unit on the host"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: it uses sudo for each root step\n' >&2
    exit 1
fi
plan_prime_sudo
plan_start_log auto

readonly REPORT="${PLAN_RUN_DIR}/acceptance-report.md"
printf '# Plan 00203 acceptance\n\nRun directory: %s\n\n' "${PLAN_RUN_DIR}" >"${REPORT}"

# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-m2.inc.bash
source "${PLAN_SCRIPT_DIR}/_acceptance-m2.inc.bash"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-m1.inc.bash
source "${PLAN_SCRIPT_DIR}/_acceptance-m1.inc.bash"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-robust.inc.bash
source "${PLAN_SCRIPT_DIR}/_acceptance-robust.inc.bash"

check_leg() {
    local name="$1" status=0
    shift
    printf '\n==> [check] %s\n' "${name}"
    "$@" || status=$?
    if [[ "${status}" -eq 0 ]]; then
        printf 'PASS  %s\n' "${name}"
        printf '## PASS %s\n\n' "${name}" >>"${REPORT}"
        return 0
    fi
    printf 'FAIL  %s\n' "${name}" >&2
    printf '## FAIL %s\n' "${name}" >>"${REPORT}"
    exit 1
}

acceptance_m2
acceptance_m1
acceptance_robust
printf '\nACCEPTED: every milestone passed\n' | tee -a "${REPORT}"
plan_finish
